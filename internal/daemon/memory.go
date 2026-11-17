package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/store"
)

// The adapter between the store and the protocol.
//
// # Why this file exists at all
//
// `internal/store` imports `internal/model` and nothing else under `internal/`
// (S4's I1). `internal/protocol` imports nothing transport-shaped and nothing
// storage-shaped (S2's T11). Both are leaves on purpose: it is what lets S19
// mount the same method table on HTTP and MCP-stdio, and what lets the store's
// whole test suite run with no protocol in sight.
//
// So neither can know the other, and something has to. This package already
// knows both — it is the only one that knows what a socket is — so the
// conversion lives here, in one file, where a reader can see the whole seam at
// once.
//
// It is dull code by design. Dull conversion in one place beats a shared type
// that two packages both have an opinion about.

// Memory adapts *store.Store to protocol.Memory.
type Memory struct {
	store *store.Store
}

// NewMemory wraps a store for the protocol. A nil store yields a nil Memory, so
// a daemon without one wires nothing rather than wiring something that panics.
func NewMemory(s *store.Store) *Memory {
	if s == nil {
		return nil
	}
	return &Memory{store: s}
}

// Info gathers the store's state for `store.info`.
func (m *Memory) Info() (protocol.StoreInfoResult, error) {
	in, err := m.store.Info()
	if err != nil {
		return protocol.StoreInfoResult{}, err
	}

	out := protocol.StoreInfoResult{
		// Collections are initialised, never left nil. Go marshals a nil slice
		// as `null`, and `pending` is empty on every healthy store — so this
		// one was `null` every time, and the app crashed on it.
		Applied:          []protocol.MigrationView{},
		Pending:          []protocol.MigrationView{},
		Tables:           emptyIfNil(in.Tables),
		Path:             in.Path,
		Bytes:            in.Bytes,
		WALBytes:         in.WALBytes,
		JournalMode:      in.JournalMode,
		ForeignKeys:      in.ForeignKeys,
		VecWriterVersion: in.VecWriterVersion,
		VecReaderVersion: in.VecReaderVersion,
		ReadPoolSize:     in.ReadPoolSize,
		WriterPoolSize:   in.WriterPoolSize,
		BusyTimeoutMS:    in.BusyTimeoutMS,
	}
	for _, a := range in.Applied {
		out.Applied = append(out.Applied, protocol.MigrationView{
			Name: a.Name, Status: a.Status, AppliedAt: a.AppliedAt,
		})
	}
	for _, p := range in.Pending {
		out.Pending = append(out.Pending, protocol.MigrationView{
			Name: p.Name, Description: p.Description,
		})
	}
	return out, nil
}

// CreateLoop creates a loop.
func (m *Memory) CreateLoop(p protocol.LoopCreateParams) (protocol.LoopView, error) {
	l, err := m.store.CreateLoop(store.NewLoop{
		Slug:          p.Slug,
		ComponentRoot: p.ComponentRoot,
		Title:         p.Title,
		Goal:          p.Goal,
		Mode:          p.Mode,
	})
	if err != nil {
		return protocol.LoopView{}, m.wrap(err)
	}
	return m.loopView(l)
}

// ListLoops lists loops, scoped unless `all`.
func (m *Memory) ListLoops(componentRoot string, all bool, startedBy, archived string) ([]protocol.LoopView, error) {
	scope := store.ComponentScope{ComponentRoot: componentRoot}
	if all {
		scope = store.ComponentScope{}
	}
	loops, err := m.store.ListLoops(scope)
	if err != nil {
		return nil, m.wrap(err)
	}
	// `make(…, 0, n)` rather than a nil slice: an empty list must marshal as
	// `[]`. See protocol/memory.go's note on the wire contract.
	hidden, err := m.store.ArchivedLoops(context.Background())
	if err != nil {
		return nil, m.wrap(err)
	}
	out := make([]protocol.LoopView, 0, len(loops))
	for _, l := range loops {
		_, isHidden := hidden[l.ID]
		if (archived == "" && isHidden) || (archived == "only" && !isHidden) {
			continue
		}
		if startedBy != "" && l.StartedBy != startedBy {
			// The filter is the STORE's column, read server-side (the
			// rule lives with the data, DB6's argument).
			continue
		}
		view, err := m.loopView(l)
		if err != nil {
			return nil, err
		}
		view.ArchivedAt = hidden[l.ID]
		out = append(out, view)
	}
	return out, nil
}

// loopView converts a row, deriving `awaiting` (DB6).
//
// The derivation happens HERE rather than in the client, because the rule is
// the store's: "a loop is waiting when it has an unanswered question". A client
// holding that rule would be a second place for it to be wrong.
func (m *Memory) loopView(l store.Loop) (protocol.LoopView, error) {
	awaiting, err := m.store.IsAwaiting(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot})
	if err != nil {
		return protocol.LoopView{}, m.wrap(err)
	}
	status := l.Status
	if status == "waiting" {
		status = m.resumedStatus(l)
	}
	return protocol.LoopView{
		ID:            l.ID,
		Slug:          l.Slug,
		ComponentRoot: l.ComponentRoot,
		Title:         l.Title,
		Goal:          l.Goal,
		Mode:          l.Mode,
		StartedBy:     l.StartedBy,
		Phase:         l.Phase,
		Status:        status,
		StartedAt:     l.StartedAt,
		UpdatedAt:     l.UpdatedAt,
		Awaiting:      awaiting,
	}, nil
}

// resumedStatus is loop.show's resumedAsRunning for the list: a loop parked `waiting` on a question stays that way in its
// saved row until the resumed step ends, so once the person has answered (the parked step's own row is running again) the
// list says running too, not "answer requested" for the step's whole run. The saved row is left alone.
func (m *Memory) resumedStatus(l store.Loop) string {
	pin, err := m.store.ReadPinnedLoop(l.ID)
	if err != nil {
		return l.Status
	}
	var rt struct {
		LastReason    string          `json:"lastReason"`
		LastStepID    int64           `json:"lastStepId"`
		PendingAction json.RawMessage `json:"pendingAction"`
	}
	if json.Unmarshal([]byte(pin.RuntimeJSON), &rt) != nil {
		return l.Status
	}
	if len(rt.PendingAction) > 0 && string(rt.PendingAction) != "null" || rt.LastReason == "stopped" || rt.LastReason == "cap-hit" || rt.LastStepID == 0 {
		return l.Status
	}
	if step, err := m.store.ReadStep(rt.LastStepID); err != nil || step.Status != string(store.StepRunning) {
		return l.Status
	}
	return "running"
}

// WriteDocument writes a loop document, turning a refusal into the error whose
// three parts explain it.
func (m *Memory) WriteDocument(p protocol.DocumentWriteParams) (protocol.DocumentWriteResult, error) {
	sc := store.Scope{LoopSlug: p.Slug, ComponentRoot: p.ComponentRoot}
	res, err := m.store.WriteDocument(sc, p.Name, p.Content, p.WrittenBy)
	if err != nil {
		return protocol.DocumentWriteResult{}, m.wrap(err)
	}
	if !res.OK {
		// The store returns a discriminated RESULT (DB19) and the protocol
		// speaks errors, so the conversion is here. `what` carries the store's
		// own sentence, which names the specific reason — "a .. segment", "an
		// extension outside the allowed set" — rather than a generic refusal.
		return protocol.DocumentWriteResult{}, protocol.ErrInvalidDocumentName.WithWhat("%s", res.Reason)
	}
	return protocol.DocumentWriteResult{
		Document:  documentView(res.Document),
		Unchanged: res.Unchanged,
	}, nil
}

// ReadDocument reads the head revision of one document.
func (m *Memory) ReadDocument(slug, componentRoot, name string) (protocol.DocumentView, error) {
	doc, err := m.store.ReadDocument(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot}, name)
	if err != nil {
		return protocol.DocumentView{}, m.wrap(err)
	}
	return documentView(doc), nil
}

// ListDocuments lists head revisions, without content.
func (m *Memory) ListDocuments(slug, componentRoot string) ([]protocol.DocumentView, error) {
	docs, err := m.store.ListDocuments(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot})
	if err != nil {
		return nil, m.wrap(err)
	}
	out := make([]protocol.DocumentView, 0, len(docs))
	for _, d := range docs {
		out = append(out, documentView(d))
	}
	return out, nil
}

// emptyIfNil normalises a nil slice to an empty one.
//
// One helper rather than six `if x == nil` blocks, and named for what it is for:
// the wire contract, not Go's convenience. Go treats nil and empty slices as
// equivalent everywhere except `encoding/json`, which is exactly where it
// matters.
func emptyIfNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func documentView(d store.Document) protocol.DocumentView {
	return protocol.DocumentView{
		File:      d.File,
		Rev:       d.Rev,
		Bytes:     d.Bytes,
		SHA256:    d.SHA256,
		WrittenBy: d.WrittenBy,
		WrittenAt: d.WrittenAt,
		Content:   d.Content,
	}
}

// ShowStep returns a step and its canonical transcript.
func (m *Memory) ShowStep(id int64) (protocol.StepView, error) {
	st, err := m.store.ReadStep(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.StepView{}, protocol.ErrStepNotFound.WithWhat("no step %d is in this store", id)
		}
		return protocol.StepView{}, m.wrap(err)
	}

	view := stepView(st)
	if record, ok, err := m.store.ReadRecovery(id); err != nil {
		return protocol.StepView{}, m.wrap(err)
	} else if ok {
		view.Recovery = &protocol.RecoveryView{Case: record.Case, Reissued: record.Reissued, Reason: record.Reason, UpdatedAt: record.UpdatedAt}
		if record.PriorCost != nil {
			cost := wireCost(*record.PriorCost)
			view.Recovery.PriorCost = &cost
		}
		if record.ReissueCost != nil {
			cost := wireCost(*record.ReissueCost)
			view.Recovery.ReissueCost = &cost
		}
		if record.Failure != nil {
			view.Recovery.Failure = &protocol.RecoveryFailureView{Kind: record.Failure.Kind, Tool: record.Failure.Tool,
				ToolUseID: record.Failure.ToolUseID, Detail: record.Failure.Detail}
		}
	}
	if rows, err := m.store.Receipts(context.Background(), id); err != nil {
		return protocol.StepView{}, m.wrap(err)
	} else {
		view.Receipts = make([]protocol.ReceiptView, 0, len(rows))
		for _, row := range rows {
			view.Receipts = append(view.Receipts, protocol.ReceiptView{ToolUseID: row.ToolUseID,
				Tool: row.Tool, ArgDigest: row.ArgDigest, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, ResultCaptured: row.Result != nil})
		}
	}
	if st.Status == string(store.StepSuspended) {
		q, ok, err := m.store.PendingStepQuestion(id)
		if err != nil {
			return protocol.StepView{}, m.wrap(err)
		}
		if ok {
			view.Question = &runtime.PendingQuestion{ID: q.ID, StepID: id, Kind: q.Kind, Body: q.Body, Options: q.Options, ToolUseID: q.ToolUseID}
		}
	}

	// The transcript is attached only when it reads back. A step with an
	// unreadable transcript still answers — with the step and without the
	// messages — because "the step exists and its transcript is refused" is a
	// more useful answer than a bare failure, and it is exactly the state a
	// future format_version produces.
	res := m.store.ReadTranscript(id)
	if res.OK {
		encoded, err := json.Marshal(res.Messages)
		if err != nil {
			return protocol.StepView{}, fmt.Errorf("daemon: encode the transcript of step %d: %w", id, err)
		}
		view.Messages = encoded
	}
	return view, nil
}

func wireCost(cost model.Cost) protocol.CostView {
	return protocol.CostView{State: string(cost.State), USD: cost.USD, USDAtLeast: cost.USDAtLeast,
		Missing: emptyIfNil(cost.Missing), Reason: cost.Reason}
}

// VerifyStep is `step.verify`.
func (m *Memory) VerifyStep(id int64) (protocol.StepVerifyResult, error) {
	v, err := m.store.VerifyTranscript(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.StepVerifyResult{}, protocol.ErrStepNotFound.WithWhat("no step %d is in this store", id)
		}
		return protocol.StepVerifyResult{}, m.wrap(err)
	}
	out := protocol.StepVerifyResult{
		OK:              v.OK,
		Messages:        v.Messages,
		SignatureLength: v.SignatureLength,
		Mismatch:        v.Mismatch,
	}
	// The digest is best-effort: a transcript that will not verify may still
	// digest, and a transcript that is absent does neither. Either way the
	// verification above is the answer, so a failure here is not one.
	if digest, err := m.store.TranscriptDigest(id); err == nil {
		out.Digest = digest
	}
	return out, nil
}

// ListSteps lists a loop's steps without their transcripts.
func (m *Memory) ListSteps(slug, componentRoot string) ([]protocol.StepView, error) {
	steps, err := m.store.ListSteps(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot})
	if err != nil {
		return nil, m.wrap(err)
	}
	out := make([]protocol.StepView, 0, len(steps))
	for _, st := range steps {
		out = append(out, stepView(st))
	}
	return out, nil
}

// ReadLoopEvents is `loop.events` — see internal/protocol/loop_events.go.
func (m *Memory) ReadLoopEvents(p protocol.LoopEventsParams) (protocol.LoopEventsResult, error) {
	var exclude []string
	if !p.IncludeDeltas {
		exclude = []string{"model.delta"}
	}
	win, err := m.store.ReadStepEvents(p.ID, p.StepID, p.Before, p.After, p.Limit, p.Kinds, exclude)
	if err != nil {
		return protocol.LoopEventsResult{}, m.wrap(err)
	}
	out := protocol.LoopEventsResult{Events: make([]protocol.LoopEventView, 0, len(win.Events)), HasOlder: win.HasOlder, HasNewer: win.HasNewer}
	for _, ev := range win.Events {
		out.Events = append(out.Events, protocol.LoopEventView{Seq: ev.Seq, Kind: ev.Kind, At: ev.CreatedAt, ComponentRoot: ev.ComponentRoot, StepID: ev.StepID, Payload: ev.Payload})
	}
	if head, err := m.store.HeadSeq(); err == nil {
		out.Head = head
	}
	return out, nil
}

// ReadLoopSteps is `loop.steps` — see internal/protocol/loop_steps.go.
func (m *Memory) ReadLoopSteps(p protocol.LoopStepsParams) (protocol.LoopStepsResult, error) {
	win, err := m.store.ReadStepSummaries(p.ID, p.Before, p.Limit)
	if err != nil {
		return protocol.LoopStepsResult{}, m.wrap(err)
	}
	out := protocol.LoopStepsResult{Steps: make([]protocol.StepSummaryView, 0, len(win.Steps)), HasOlder: win.HasOlder}
	windows := map[string]int64{}
	for _, x := range win.Steps {
		if x.Route != "" {
			w, seen := windows[x.Route]
			if !seen {
				if r, err := m.store.ReadRoute(x.Route); err == nil {
					w = int64(r.ContextWindow)
				}
				windows[x.Route] = w
			}
			x.ContextWindow = w
		}
		out.Steps = append(out.Steps, protocol.StepSummaryView(x))
	}
	// The totals ride only the first page (no cursor): older pages add steps, never change the loop's sum.
	if p.Before == 0 {
		totals, err := m.store.ReadLoopTotals(p.ID)
		if err != nil {
			return protocol.LoopStepsResult{}, m.wrap(err)
		}
		v := protocol.LoopTotalsView(totals)
		out.Totals = &v
	}
	return out, nil
}

// ReadLoopTranscript is `loop.transcript` — see internal/protocol/loop_transcript.go
// for why this reads `step_message` directly rather than the live ring.
func (m *Memory) ReadLoopTranscript(p protocol.LoopTranscriptParams) (protocol.LoopTranscriptResult, error) {
	sc := store.Scope{LoopSlug: p.Slug, ComponentRoot: p.ComponentRoot}
	page, err := m.store.ReadLoopTranscriptPage(sc, p.Before, p.After, p.Limit)
	if err != nil {
		return protocol.LoopTranscriptResult{}, m.wrap(err)
	}
	out := protocol.LoopTranscriptResult{
		Messages: make([]protocol.LoopTranscriptMessageView, 0, len(page.Messages)),
		HasOlder: page.HasOlder,
		HasNewer: page.HasNewer,
	}
	for _, msg := range page.Messages {
		out.Messages = append(out.Messages, protocol.LoopTranscriptMessageView{
			ID: msg.ID, StepID: msg.StepID, Agent: msg.Agent, Phase: msg.Phase, Role: msg.Role,
			CreatedAt: msg.CreatedAt, Content: json.RawMessage(msg.Content), Unreadable: msg.Unreadable,
		})
	}
	return out, nil
}

func stepView(st store.Step) protocol.StepView {
	view := protocol.StepView{
		ID:            st.ID,
		LoopSlug:      st.LoopSlug,
		ComponentRoot: st.ComponentRoot,
		TaskID:        st.TaskID,
		Phase:         st.Phase,
		Agent:         st.Agent,
		Status:        st.Status,
		Outcome:       st.Outcome,
		PauseReason:   st.PauseReason,
		StartedAt:     st.StartedAt,
		EndedAt:       st.EndedAt,
		Cost: protocol.CostView{
			// The STATE is a field, carried as stored. A client that inferred
			// it from whether `usd` is present would collapse `partial` into
			// `known` — a partial cost has a number; it is a floor.
			State:      string(st.Cost.State),
			USD:        st.Cost.USD,
			USDAtLeast: st.Cost.USDAtLeast,
			Missing:    emptyIfNil(st.Cost.Missing),
			Reason:     st.Cost.Reason,
		},
	}
	if len(st.ContextManifestRaw) > 0 {
		// THE STORED BYTES, not a re-encoding of the store's view. Re-encoding
		// here would drop every manifest field the store does not declare —
		// the same loss the write path had, in the other direction.
		view.ContextManifest = st.ContextManifestRaw
	} else if st.ContextManifest != nil {
		if encoded, err := json.Marshal(st.ContextManifest); err == nil {
			view.ContextManifest = encoded
		}
	}
	return view
}

// Awaiting answers the derived query, and returns the query itself.
func (m *Memory) Awaiting(componentRoot string, all bool) (protocol.AwaitingResult, error) {
	scope := store.ComponentScope{ComponentRoot: componentRoot}
	if all {
		scope = store.ComponentScope{}
	}
	loops, err := m.store.AwaitingLoops(scope)
	if err != nil {
		return protocol.AwaitingResult{}, m.wrap(err)
	}
	// The SQL comes from the store's own published constant, so what is shown
	// is what ran. A second copy here would eventually print something the
	// store does not execute.
	out := protocol.AwaitingResult{SQL: store.AwaitingSQL, Loops: []protocol.LoopRefer{}}
	for _, sc := range loops {
		out.Loops = append(out.Loops, protocol.LoopRefer{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot})
	}
	return out, nil
}

// PendingQuestion is S19 T5's read (protocol.Memory): the one question a
// loop waits on, with the kind and the declared answer set.
//
// The read exists so a monitor can see WHY a loop stopped. The write —
// answering — deliberately has no counterpart here: loop.answer stays on
// the socket, where the answering is a person's act in an app or a
// terminal. This method cannot answer anything even if the surface grows
// (S19 T5, N16 — permanent, not deferred).
func (m *Memory) PendingQuestion(slug, componentRoot string) (protocol.QuestionView, bool, error) {
	q, found, err := m.store.PendingQuestion(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot})
	if err != nil {
		return protocol.QuestionView{}, false, m.wrap(err)
	}
	if !found {
		return protocol.QuestionView{}, false, nil
	}
	return protocol.QuestionView{
		ID:      q.ID,
		StepID:  q.StepID,
		Kind:    string(q.Kind),
		Prompt:  q.Body,
		Answers: q.Options,
		AskedAt: q.AskedAt,
	}, true, nil
}

// SaveAsk persists what an `ask` produced (`--save-step`).
//
// # What this writes, and the one thing it cannot
//
// A step, a transcript, and the cost — in that order, all through the store's
// own API. The transcript is the canonical shape: a system message when one was
// sent, the user's prompt, and the assistant's reply.
//
// The thinking block it writes is UNSIGNED, because S3's streaming surface
// carries no signature (`Delta` has `Kind` and `Text`). That is a limitation of
// this bridge rather than of the store — the store's tests write and read a
// signed block byte-identically — and it is stated here, and in the demo,
// because the alternative is somebody discovering it at S7.
//
// The loop is created if it does not exist. A save that failed because nobody
// had run `loop create` first would lose the reply over bookkeeping, and the
// tokens were already bought.
func (m *Memory) SaveAsk(p protocol.AskSaveParams) (int64, error) {
	sc := store.Scope{LoopSlug: p.LoopSlug, ComponentRoot: p.ComponentRoot}

	if _, err := m.store.GetLoop(sc); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return 0, m.wrap(err)
		}
		if _, err := m.store.CreateLoop(store.NewLoop{
			Slug:          p.LoopSlug,
			ComponentRoot: p.ComponentRoot,
			Title:         p.LoopSlug,
			Goal:          "created by `ask --save-step`",
		}); err != nil {
			return 0, m.wrap(err)
		}
	}

	st, err := m.store.CreateStep(sc, store.NewStep{
		Phase:  "build",
		Agent:  "ask:" + p.Route,
		Status: store.StepRunning,
	})
	if err != nil {
		return 0, m.wrap(err)
	}

	var msgs []model.Message
	if p.System != "" {
		msgs = append(msgs, model.Message{
			Role:    model.RoleSystem,
			Content: []model.Block{model.TextBlock(p.System)},
		})
	}
	msgs = append(msgs, model.Message{
		Role:    model.RoleUser,
		Content: []model.Block{model.TextBlock(p.Prompt)},
	})

	var reply []model.Block
	if p.Thinking != "" {
		reply = append(reply, model.ThinkingBlock(p.Thinking, ""))
	}
	if p.Text != "" {
		reply = append(reply, model.TextBlock(p.Text))
	}
	if len(reply) > 0 {
		// The reply is the assistant turn of a call that went out on p.Route
		// (M26) — the caller that made the call stamps the triple, and
		// AppendMessages refuses the row without it.
		prov := p.Provenance
		msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: reply, Provenance: &prov})
	}

	if err := m.store.AppendMessages(st.ID, msgs); err != nil {
		return 0, m.wrap(err)
	}
	// The cost is written even when the call FAILED (M15): a failed ask that
	// records nothing loses the spend, and those tokens were still bought.
	if err := m.store.SetStepCost(st.ID, p.Cost); err != nil {
		return 0, m.wrap(err)
	}
	// What the wire withheld rides the same record (S18 T5, O1): the
	// transcript kept every thinking block and the request carried fewer,
	// and that asymmetry is only visible if the step says so. Zero is left
	// unrecorded — the column's NULL already answers "nothing fell".
	if p.ThinkingDropped > 0 {
		if err := m.store.SetStepThinkingDropped(st.ID, p.ThinkingDropped); err != nil {
			return 0, m.wrap(err)
		}
	}

	outcome := p.StopReason
	if p.Failed {
		outcome = "failed"
	}
	if err := m.store.SetStepStatus(st.ID, store.StepDone, outcome); err != nil {
		return 0, m.wrap(err)
	}
	return st.ID, nil
}

// wrap turns an infrastructure failure into a protocol error.
//
// A missing row becomes the store-unavailable error ONLY when the store itself
// is gone; otherwise the store's own sentence travels as `what`, because it
// names the scope and the table and is the actionable part.
func (m *Memory) wrap(err error) error {
	if err == nil {
		return nil
	}
	return protocol.ErrProviderFailed.WithWhat("%v", err)
}

// --- the event backing (T8) --------------------------------------------------

// EventBacking adapts the store to protocol.Backing, so `seq` comes from the
// `event` table and survives a restart.
//
// This is the whole of T8's seam: the protocol allocates nothing, the store
// allocates everything (DB20), and the conversion between a protocol Filter and
// the store's primitive filter happens here — which is why neither leaf had to
// learn about the other.
type EventBacking struct {
	store *store.Store
	// stepLoops caches step id -> its loop, so attributing a step event to its loop costs one read per step, not
	// one per event (a streaming step publishes hundreds).
	stepLoops sync.Map
}

type loopRef struct{ id, slug string }

// NewEventBacking wraps a store as the event allocator. A nil store yields nil,
// which leaves the in-memory counter in place — the mode tests use.
func NewEventBacking(s *store.Store) *EventBacking {
	if s == nil {
		return nil
	}
	return &EventBacking{store: s}
}

// Append persists the event and returns it with its allocated seq.
func (b *EventBacking) Append(kind, loopSlug, componentRoot string, payload json.RawMessage) (protocol.Event, error) {
	loopID, slug, stepID := b.attribute(payload)
	if loopSlug == "" {
		loopSlug = slug
	}
	ev, err := b.store.AppendEvent(store.NewEvent{
		LoopSlug:      loopSlug,
		ComponentRoot: componentRoot,
		LoopID:        loopID,
		StepID:        stepID,
		Kind:          kind,
		Payload:       payload,
	})
	if err != nil {
		return protocol.Event{}, err
	}
	return protocolEvent(ev), nil
}

// attribute names the loop and step an event belongs to, from the ids in its payload: `loopId` directly, or the
// step's loop through the step row. It is how the journal becomes readable per loop (ReadLoopEvents). An event that
// names neither (a daemon lifecycle event) comes back empty, and a lookup that fails leaves the event
// unattributed rather than lost: the journal never refuses an event for a missing label.
func (b *EventBacking) attribute(payload json.RawMessage) (loopID, slug string, stepID int64) {
	if len(payload) == 0 || (!bytes.Contains(payload, []byte(`"loopId"`)) && !bytes.Contains(payload, []byte(`"stepId"`))) {
		return "", "", 0
	}
	var ids struct {
		LoopID string `json:"loopId"`
		StepID int64  `json:"stepId"`
	}
	if json.Unmarshal(payload, &ids) != nil {
		return "", "", 0
	}
	stepID = ids.StepID
	if ids.LoopID != "" {
		if lp, err := b.store.ReadLoopByID(ids.LoopID); err == nil {
			return lp.ID, lp.Slug, stepID
		}
		return ids.LoopID, "", stepID
	}
	if stepID == 0 {
		return "", "", 0
	}
	if cached, ok := b.stepLoops.Load(stepID); ok {
		ref := cached.(loopRef)
		return ref.id, ref.slug, stepID
	}
	st, err := b.store.ReadStep(stepID)
	if err != nil || st.LoopSlug == "" {
		return "", "", stepID
	}
	lp, err := b.store.GetLoop(store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return "", st.LoopSlug, stepID
	}
	b.stepLoops.Store(stepID, loopRef{id: lp.ID, slug: lp.Slug})
	return lp.ID, lp.Slug, stepID
}

// Range serves a cursor from the table.
func (b *EventBacking) Range(after uint64, filter protocol.Filter, limit int) ([]protocol.Event, error) {
	events, err := b.store.ReadEvents(int64(after), store.EventFilter{
		Kinds:         filter.Kinds,
		ComponentRoot: filter.ComponentRoot,
	}, limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Event, 0, len(events))
	for _, ev := range events {
		out = append(out, protocolEvent(ev))
	}
	return out, nil
}

// Head is the highest allocated seq.
func (b *EventBacking) Head() (uint64, error) {
	head, err := b.store.HeadSeq()
	if err != nil {
		return 0, err
	}
	if head < 0 {
		return 0, nil
	}
	return uint64(head), nil
}

func protocolEvent(ev store.Event) protocol.Event {
	return protocol.Event{
		Seq:           uint64(ev.Seq),
		Kind:          ev.Kind,
		ComponentRoot: ev.ComponentRoot,
		At:            ev.CreatedAt,
		Payload:       ev.Payload,
	}
}

// --- route persistence (S4, closing S3's promise) -----------------------------

// RouteStore persists routes: the configuration in the store, the credential in
// a dedicated 0600 file.
//
// # Why the two halves are split, and why the split lives HERE
//
// C17 is one credential rule for the whole product: a credential comes from
// **the environment, or a dedicated file path — never a config file, never
// argv, never a log.** So a route's key does not go in the database, and
// `internal/store` has no column for one.
//
// Something has to know both halves, and it is this package — the same reason
// the event backing and the protocol's `Memory` live here. `internal/protocol`
// knows only the `RoutePersistence` interface; `internal/store` knows only
// rows; `internal/secrets` knows only files.
//
// # What S3 promised and S4 owed
//
// S3 added route management with `persisted: false`, and the app said
// "persistence arrives with the store (S4)". **S4 shipped without it** — the
// same unkept-promise shape T8 fixed for S2's `seq`, missed here. This is it.
type RouteStore struct {
	store *store.Store
}

// NewRouteStore wraps a store as the route persistence. A nil store yields nil,
// which leaves routes in memory — the mode tests use, and the mode S3 shipped.
func NewRouteStore(s *store.Store) *RouteStore {
	if s == nil {
		return nil
	}
	return &RouteStore{store: s}
}

// ProbeCache is the store's probe verdicts behind the protocol's seam
// (S18 T3). It is deliberately THIN — the verdict rules (which states are
// storable, what replaces what) live in the store, and the probe itself
// lives in internal/model; this is only the door.
//
// It returns the INTERFACE type so a nil store yields a nil interface, not
// a typed nil pointer wearing a non-nil interface — `routes.probe` must
// read "no cache" honestly, not panic inside a method on nothing.
type ProbeCache struct {
	store *store.Store
}

func NewProbeCache(s *store.Store) protocol.ProbeStore {
	if s == nil {
		return nil
	}
	return &ProbeCache{store: s}
}

func (p *ProbeCache) PutProbeResults(routeID string, results []model.ProbeResult) error {
	return p.store.PutProbeResults(routeID, results)
}

func (p *ProbeCache) ProbeResults(routeID string) ([]model.ProbeResult, error) {
	return p.store.ProbeResults(routeID)
}

// Save writes the route's configuration and its credential, separately.
//
// The credential is written FIRST. If the row landed and the key did not, a
// restart would bring back a route that cannot be called and whose `hasKey`
// says it can — and the person would have no reason to suspect the key. In the
// other order the failure is "the route was not saved", which is what they are
// told.
func (r *RouteStore) Save(route model.Route) error {
	if route.APIKey != "" {
		if err := secrets.SaveRouteKey(route.ID, route.APIKey); err != nil {
			return err
		}
	}
	// The key is NOT passed on. `UpsertRoute` ignores `APIKey` and a test reads
	// the database's raw bytes to prove it, but handing it one anyway would be
	// relying on that rather than on this.
	route.APIKey = ""
	return r.store.UpsertRoute(route)
}

// Remove deletes the route and its credential.
//
// Both, always. A key left behind for a route nobody can call is a secret on
// disk with no owner — and on the next `routes.set` of the same id it would
// come back as "the existing key", which is a credential resurrected from a
// route that was deleted.
func (r *RouteStore) Remove(id string) error {
	if _, err := secrets.DeleteRouteKey(id); err != nil {
		return err
	}
	if _, err := r.store.DeleteRoute(id); err != nil {
		return err
	}
	return nil
}

// Load reads the stored routes and attaches each credential.
//
// A route whose key file is missing is returned WITHOUT a key rather than
// skipped: it is a real state (a route configured before its key, or one whose
// key was cleared), the app renders it as `hasKey: false`, and dropping it
// would make a configured route vanish for a reason nothing displays.
func (r *RouteStore) Load() ([]model.Route, error) {
	routes, err := r.store.ListRoutes()
	if err != nil {
		return nil, err
	}
	for i := range routes {
		key, err := secrets.ReadRouteKey(routes[i].ID)
		if err != nil {
			// Reported, not skipped: a key that exists and cannot be read is a
			// permission problem, and starting the daemon as though the route
			// had no credential would present it as a provider rejection later.
			return nil, err
		}
		routes[i].APIKey = key
	}
	return routes, nil
}
