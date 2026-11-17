package protocol

import (
	"context"
	"encoding/json"
	"github.com/cjlapao/aeon/internal/runtime"
)

// The store's protocol surface (S4).
//
// # Why there is an interface here and not an import
//
// `internal/protocol` imports nothing transport-shaped and nothing
// storage-shaped (T11/I1). That is what lets S19 mount the same method table on
// HTTP and MCP-stdio, and it is what keeps this package testable with no
// database.
//
// So the store arrives as `Memory`, an interface declared HERE, in the terms
// this package already speaks: views rather than rows, strings rather than
// scopes. The adapter over `internal/store` lives in `internal/daemon`, which
// already knows both sides.
//
// # Why views and not the store's own types
//
// The same reason `RouteView` exists: a wire type and a row have different
// obligations. A row gains a column when the schema does; a wire type gains a
// field when a client needs one, and the two happen at different times. The
// adapter is where they meet, and it is one file.

// Memory is the store, as the protocol needs it.
//
// Nil until a daemon wires one, and every handler says so rather than
// panicking — a daemon with no store is a configuration this build cannot
// produce, but "cannot" is not "will not".
type Memory interface {
	Info() (StoreInfoResult, error)

	CreateLoop(p LoopCreateParams) (LoopView, error)
	ListLoops(componentRoot string, all bool, startedBy, archived string) ([]LoopView, error)

	WriteDocument(p DocumentWriteParams) (DocumentWriteResult, error)
	ReadDocument(slug, componentRoot, name string) (DocumentView, error)
	ListDocuments(slug, componentRoot string) ([]DocumentView, error)

	// S69: the attachment doors. PutAttachment writes bytes once and answers
	// the receipt; ReadAttachmentForView brings a pinned revision back for
	// RENDERING only; ResolveAttachment is what the message and steer doors
	// consult before filing a row — receipts are filed from the store's
	// facts, never the caller's claims (AT3).
	PutAttachment(p AttachmentPutParams) (AttachmentPutResult, error)
	ReadAttachmentForView(p AttachmentReadParams) (AttachmentReadResult, error)
	ResolveAttachment(slug, componentRoot, document string, rev int) (AttachmentReceiptView, error)

	ShowStep(id int64) (StepView, error)
	VerifyStep(id int64) (StepVerifyResult, error)
	ListSteps(slug, componentRoot string) ([]StepView, error)
	ReadLoopTranscript(p LoopTranscriptParams) (LoopTranscriptResult, error)
	// ReadLoopEvents is `loop.events`: one loop's journal as a window.
	ReadLoopEvents(p LoopEventsParams) (LoopEventsResult, error)
	// ReadLoopSteps is `loop.steps`: one loop's step summaries, newest first, with the loop's totals on the first page.
	ReadLoopSteps(p LoopStepsParams) (LoopStepsResult, error)

	Awaiting(componentRoot string, all bool) (AwaitingResult, error)

	// PendingQuestion returns the one unanswered question a loop waits on —
	// kind, prompt, and the declared answer set. Read-only by construction:
	// the answering half lives behind loop.answer and can never be reached
	// from a projection of this method (S19 T5, N16 — permanent).
	PendingQuestion(slug, componentRoot string) (QuestionView, bool, error)

	// SaveAsk persists what an `ask` produced, for `--save-step`. It is the
	// bridge between S3 and this slice, and the reason the demo can restart the
	// daemon and still find something.
	SaveAsk(p AskSaveParams) (int64, error)
}

// # An empty collection is `[]` on the wire, never `null`
//
// Go marshals a nil slice as `null`, so a field that is "a list of things" comes
// out as `null` whenever there are none — and `pending` has none on every
// healthy store, so it was `null` EVERY TIME.
//
// That is a bad contract. A field that is sometimes `null` and sometimes `[]`
// makes every client in every language write a null-guard forever, and the one
// that forgets crashes on the happy path. **Found by the app crashing on a
// fresh store**: `TypeError: null is not an object (evaluating
// 'info.pending.length')`, and a blank Store tab.
//
// So every collection on these types is normalised to an empty slice before it
// goes out, by `emptyIfNil` below, and `TestNoWireTypeEverSendsNullForACollection`
// marshals each one and fails on the string `null`. The hand-written conformance
// fixture said `[]` and therefore proved nothing — which is the real lesson: a
// fixture asserts what somebody BELIEVED the shape was.

// --- store.info --------------------------------------------------------------

// StoreInfoResult is what `store.info` answers, and what the app's Store tab
// renders.
//
// The PATH is first, because every later slice's debugging starts by finding
// the file (B10) — and because the app showing a path a person can paste into
// `sqlite3` is the difference between a dashboard and a tool.
type StoreInfoResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	// WALBytes is the write-ahead log beside it. Reported separately because
	// in WAL mode committed data lives there until a checkpoint, so a busy
	// store's main file looks implausibly small.
	WALBytes    int64  `json:"walBytes"`
	JournalMode string `json:"journalMode"`

	// ForeignKeys is read from the connection, never inferred from the DSN. The
	// wrong DSN spelling produces a store that LOOKS configured and runs
	// without cascades (measured), so the app shows the answer rather than the
	// intention.
	ForeignKeys bool `json:"foreignKeys"`

	Applied []MigrationView `json:"applied"`
	Pending []MigrationView `json:"pending"`
	Tables  []string        `json:"tables"`

	// Vec reports the statically linked extension, per pool. There is no
	// extension-missing state to render (DB18) — a failure here means the
	// binary was built without cgo, which is a different problem from a
	// misconfigured machine and reads differently.
	VecWriterVersion string `json:"vecWriterVersion"`
	VecReaderVersion string `json:"vecReaderVersion"`

	// The pool shape, so the app can show what DB15 decided. The writer is
	// always 1 and says why in its own tooltip.
	ReadPoolSize   int `json:"readPoolSize"`
	WriterPoolSize int `json:"writerPoolSize"`
	BusyTimeoutMS  int `json:"busyTimeoutMs"`
}

// MigrationView is one ledger entry.
type MigrationView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	AppliedAt   string `json:"appliedAt,omitempty"`
}

// --- loops -------------------------------------------------------------------

// LoopCreateParams creates a loop.
type LoopCreateParams struct {
	Slug string `json:"slug"`
	// ComponentRoot is required, and there is no default. A loop created against
	// "wherever the daemon happens to be" is a loop in a project nobody chose
	// — and the daemon's working directory is not the client's.
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title,omitempty"`
	Goal          string `json:"goal,omitempty"`
	Mode          string `json:"mode,omitempty"`
}

// LoopListParams lists loops.
type LoopListParams struct {
	ComponentRoot string `json:"componentRoot,omitempty"`
	// All spans every project. Explicit, because a daemon multiplexes every
	// project on the machine and an unscoped default would show a person every
	// repository's loops whenever they were in one.
	All bool `json:"all,omitempty"`
	// StartedBy filters to the loops a trigger or a webhook started
	// (S20 T2, R-3): "trigger", "webhook", or "" for nobody-nothing…
	// "" means unfiltered. The filter rides the socket because the rule
	// is the store's — the column lives there, and a client filtering
	// locally would be a second place for the rule to be wrong (DB6's
	// argument, applied).
	StartedBy string `json:"startedBy,omitempty"`
	// Archived says what to do with hidden loops: "" leaves them out, "include" lists them with the rest, "only" lists just them.
	Archived string `json:"archived,omitempty"`
}

// LoopView is a loop on the wire.
type LoopView struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Goal          string `json:"goal,omitempty"`
	Mode          string `json:"mode"`
	Phase         string `json:"phase"`
	Status        string `json:"status"`
	StartedAt     string `json:"startedAt"`
	UpdatedAt     string `json:"updatedAt"`

	// StartedBy is S20's provenance on the wire: "trigger" or "webhook"
	// beside which schedule, or "" (omitted) for a person's own loop. It is
	// NOT a rendering of attendedness — the two questions keep two fields
	// here exactly as they keep two columns in the row (R-3).
	StartedBy string `json:"startedBy,omitempty"`

	// Awaiting is DERIVED, never stored (DB6). It is on the view because it is
	// the first thing a person wants to know about a loop, and computing it in
	// the client would mean the client holding the rule.
	Awaiting bool `json:"awaiting"`
	// ArchivedAt is when the loop was hidden; empty for a loop in the lists.
	ArchivedAt string `json:"archivedAt,omitempty"`
}

// LoopListResult carries the loops and what was asked for.
//
// Echoing the scope back is not redundant: a list that came back empty is a
// different fact depending on whether it was scoped, and the client should not
// have to remember what it sent.
type LoopListResult struct {
	Loops         []LoopView `json:"loops"`
	ComponentRoot string     `json:"componentRoot,omitempty"`
	All           bool       `json:"all"`
}

// --- documents ---------------------------------------------------------------

// DocumentWriteParams writes a loop document.
type DocumentWriteParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	// Name is a friendly name (`plan`) or a confined path (`tasks/T1.md`).
	// Anything else is REFUSED, never normalised into range.
	Name      string `json:"name"`
	Content   string `json:"content"`
	WrittenBy string `json:"writtenBy,omitempty"`
}

// DocumentReadParams reads one.
type DocumentReadParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Name          string `json:"name,omitempty"`
}

// DocumentView is a document on the wire. Content is present on a read and
// empty in a listing.
type DocumentView struct {
	File      string `json:"file"`
	Rev       int    `json:"rev"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	WrittenBy string `json:"writtenBy,omitempty"`
	WrittenAt string `json:"writtenAt"`
	Content   string `json:"content,omitempty"`
}

// DocumentWriteResult reports what happened, including the third outcome.
//
// `unchanged` is not a success with a side note: a write whose content matches
// the head is dropped rather than stored twice, and a client that believed it
// created a revision would show a history with a duplicate in it.
type DocumentWriteResult struct {
	Document  DocumentView `json:"document"`
	Unchanged bool         `json:"unchanged"`
}

// DocumentListResult carries a loop's documents, head revisions only.
type DocumentListResult struct {
	Documents []DocumentView `json:"documents"`
}

// --- steps -------------------------------------------------------------------

// StepShowParams names a step.
type StepShowParams struct {
	ID int64 `json:"id"`
}

// StepView is a step and, on `step.show`, its transcript.
type StepView struct {
	ID            int64  `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	TaskID        string `json:"taskid,omitempty"`
	Phase         string `json:"phase"`
	Agent         string `json:"agent"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome,omitempty"`
	// PauseReason is set only when `Status` is `suspended` because a PERSON
	// stopped the step, never because the model asked a real question — the
	// two share a status (P2: both are "no process, resumable") and this is
	// the one field that tells a reader which happened. Empty either way the
	// step isn't paused for that reason.
	PauseReason string `json:"pauseReason,omitempty"`
	StartedAt   string `json:"startedAt"`
	EndedAt     string `json:"endedAt,omitempty"`

	// Cost carries all three states. A surface that cannot show `unknown` is a
	// surface that will show a confident wrong answer.
	Cost CostView `json:"cost"`

	// Messages is the canonical transcript, present on `step.show`.
	Messages json.RawMessage `json:"messages,omitempty"`

	// ContextManifest is the frozen context, when one exists.
	ContextManifest json.RawMessage          `json:"contextManifest,omitempty"`
	Question        *runtime.PendingQuestion `json:"question,omitempty"`
	Recovery        *RecoveryView            `json:"recovery,omitempty"`
	Receipts        []ReceiptView            `json:"receipts,omitempty"`
}

type RecoveryView struct {
	Case        string               `json:"case"`
	Reissued    bool                 `json:"reissued"`
	Reason      string               `json:"reason,omitempty"`
	UpdatedAt   string               `json:"updatedAt"`
	Failure     *RecoveryFailureView `json:"failure,omitempty"`
	PriorCost   *CostView            `json:"priorCost,omitempty"`
	ReissueCost *CostView            `json:"reissueCost,omitempty"`
}

type RecoveryFailureView struct {
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Detail    string `json:"detail"`
}

type ReceiptView struct {
	ToolUseID      string  `json:"toolUseId"`
	Tool           string  `json:"tool"`
	ArgDigest      string  `json:"argDigest"`
	StartedAt      string  `json:"startedAt"`
	CompletedAt    *string `json:"completedAt,omitempty"`
	ResultCaptured bool    `json:"resultCaptured"`
}

// CostView is S3's cost on the wire, with the state as a field rather than as
// something to infer from the numbers.
type CostView struct {
	State      string   `json:"state"`
	USD        float64  `json:"usd,omitempty"`
	USDAtLeast float64  `json:"usdAtLeast,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// StepVerifyResult is `step.verify`: the human-visible form of the transcript
// fidelity test.
//
// Somebody diagnosing a rejected replay at S7 runs this first, which is why the
// signature's LENGTH is reported: "present" and "the right length" are
// different claims, and a truncated signature is the failure that reads as
// present.
type StepVerifyResult struct {
	OK              bool   `json:"ok"`
	Messages        int    `json:"messages"`
	SignatureLength int    `json:"signatureLength"`
	Digest          string `json:"digest"`
	Mismatch        string `json:"mismatch,omitempty"`
}

// StepListResult carries a loop's steps, without transcripts.
type StepListResult struct {
	Steps []StepView `json:"steps"`
}

// --- awaiting ----------------------------------------------------------------

// AwaitingParams scopes the derived query.
type AwaitingParams struct {
	ComponentRoot string `json:"componentRoot,omitempty"`
	All           bool   `json:"all,omitempty"`
}

// AwaitingResult carries the loops waiting for an answer AND THE QUERY.
//
// # Why the SQL is on the wire
//
// Anyone arriving from v1 will look for an `awaiting_user` column and not find
// one. Showing the query is how they learn that the answer is DERIVED (DB6) —
// a stored flag and an unanswered question can disagree, and when they do
// nothing says which is right.
//
// It comes from the store's own published constant, so what is shown is what
// ran.
type AwaitingResult struct {
	SQL   string      `json:"sql"`
	Loops []LoopRefer `json:"loops"`
}

// LoopRefer is the smallest thing that names a loop.
type LoopRefer struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
}

// --- the handlers ------------------------------------------------------------

func (t *Table) requireMemory() (Memory, error) {
	if t.Memory == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Memory, nil
}

func (t *Table) handleStoreInfo(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	info, err := mem.Info()
	if err != nil {
		return nil, err
	}
	return info.normalised(), nil
}

func (t *Table) handleLoopCreate(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p LoopCreateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("a loop needs both a slug and a componentRoot")
	}
	return mem.CreateLoop(p)
}

func (t *Table) handleLoopList(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p LoopListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" && !p.All {
		return nil, ErrInvalidScope.WithWhat("listing loops needs either a componentRoot or all: true, " +
			"because a daemon serves every component on the machine")
	}
	loops, err := mem.ListLoops(p.ComponentRoot, p.All, p.StartedBy, p.Archived)
	if err != nil {
		return nil, err
	}
	return LoopListResult{Loops: emptyIfNil(loops), ComponentRoot: p.ComponentRoot, All: p.All}, nil
}

func (t *Table) handleLoopWrite(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p DocumentWriteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("writing a document needs both a slug and a componentRoot")
	}
	return mem.WriteDocument(p)
}

func (t *Table) handleLoopRead(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p DocumentReadParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("reading a document needs both a slug and a componentRoot")
	}
	if p.Name == "" {
		docs, err := mem.ListDocuments(p.Slug, p.ComponentRoot)
		if err != nil {
			return nil, err
		}
		return DocumentListResult{Documents: emptyIfNil(docs)}, nil
	}
	return mem.ReadDocument(p.Slug, p.ComponentRoot, p.Name)
}

func (t *Table) handleStepShow(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p StepShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrInvalidScope.WithWhat("a step id is required")
	}
	if t.Recovering != nil && t.Recovering(p.ID) {
		return nil, ErrStepRecovering.WithWhat("step %d is recovering", p.ID)
	}
	st, err := mem.ShowStep(p.ID)
	if err != nil {
		return nil, err
	}
	return st.normalised(), nil
}

func (t *Table) handleStepVerify(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p StepShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrInvalidScope.WithWhat("a step id is required")
	}
	return mem.VerifyStep(p.ID)
}

func (t *Table) handleStepList(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p DocumentReadParams // slug + componentRoot, the same two fields
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("listing steps needs both a slug and a componentRoot")
	}
	steps, err := mem.ListSteps(p.Slug, p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	for i := range steps {
		steps[i] = steps[i].normalised()
	}
	return StepListResult{Steps: emptyIfNil(steps)}, nil
}

func (t *Table) handleStoreAwaiting(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p AwaitingParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	res, err := mem.Awaiting(p.ComponentRoot, p.All || p.ComponentRoot == "")
	if err != nil {
		return nil, err
	}
	res.Loops = emptyIfNil(res.Loops)
	return res, nil
}

// --- question.pending (S19 T5) -------------------------------------------------

// QuestionView is one question on the wire, readable by the narrow surface.
//
// # Why the KIND is here (and why that is not S9's U14)
//
// U14 forbids distinguishing the asking mechanism TO THE PERSON ANSWERING
// inline — a person deciding whether to answer must not be steered by
// whether the asker was a model, a gate, or a permission check. A MONITOR
// has the opposite need: the human deciding WHETHER to go and answer
// benefits from knowing what is asking (a gate that will decide a phase
// and a clarify question are different visits). Different audience,
// different surface, same honesty.
type QuestionView struct {
	ID     int64  `json:"id"`
	StepID *int64 `json:"stepId,omitempty"`
	// Kind is the store's four-valued kind (clarify · gate · permission ·
	// free), carried as a string on the wire — the projection reports what
	// the row says rather than re-deciding the vocabulary.
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
	// Answers is the DECLARED answer set — the options the asker offered.
	// Empty means free text. Declaring the set is the asker's; the surface
	// reads it and never posts to it.
	Answers []string `json:"answers"`
	AskedAt string   `json:"askedAt,omitempty"`
}

type QuestionPendingParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
}

type QuestionPendingResult struct {
	Pending  bool          `json:"pending"`
	Question *QuestionView `json:"question,omitempty"`
}

func (t *Table) handleQuestionPending(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p QuestionPendingParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The loop is the unit, and the loop is addressed by BOTH keys: a slug
	// alone is ambiguous across checkouts (the reasoning behind -31105,
	// carried from loop addressing to question addressing without a new
	// code — same refusal, same shape).
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("a pending question needs both a slug and a componentRoot")
	}
	q, found, err := mem.PendingQuestion(p.Slug, p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	if !found {
		return QuestionPendingResult{Pending: false}, nil
	}
	q.Answers = emptyIfNil(q.Answers) // `[]`, never null — the standing rule
	return QuestionPendingResult{Pending: true, Question: &q}, nil
}

// --- the wire contract: an empty collection is `[]`, never `null` -------------

// emptyIfNil normalises a nil slice to an empty one.
//
// Go treats nil and empty slices as equivalent everywhere except
// `encoding/json`, which is exactly where it matters: a nil slice marshals as
// `null`.
func emptyIfNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

// normalised fills in every collection, so no Memory implementation can leak a
// `null` onto the wire.
//
// # Why this is in the HANDLER and not only in the adapter
//
// The adapter is one implementation of `Memory`. The handler is the last thing
// that touches a result before it is marshalled, so normalising here covers
// every implementation — including a test double, and including whatever S19
// mounts on HTTP. Fixing only the adapter would leave the contract depending on
// a convention rather than on a gate.
func (r StoreInfoResult) normalised() StoreInfoResult {
	r.Applied = emptyIfNil(r.Applied)
	r.Pending = emptyIfNil(r.Pending)
	r.Tables = emptyIfNil(r.Tables)
	return r
}

func (v StepView) normalised() StepView {
	v.Cost.Missing = emptyIfNil(v.Cost.Missing)
	return v
}

// # A note on params decoding
//
// These handlers use the package's existing `decodeParams` (handshake.go),
// which IGNORES unknown fields. That was S2's decision (T3) and is asserted by
// a test that nothing in this package calls DisallowUnknownFields — so the
// strict decoder written here first was deleted rather than the guard
// loosened. The reason S2 gave still holds: a client built against a newer
// protocol must be able to send a field this build has not heard of, which is
// what makes the method table additive.
