package daemon

// # The steer wiring (S31 T4)
//
// Three seams connect the socket's steer door to the durable queue and the
// runtime's turn-boundary drain:
//
//   - `protocol.SteerStore` over the daemon's StepStore — queue, list,
//     cancel, each answered from the store's own discriminated results. The
//     store already names every refusal precisely (`Found` vs state-found vs
//     the whole `Reason`); the adapter's job is to CARRY that naming into the
//     protocol's three-part errors, not to re-guess it from prose.
//   - the RunOpts factory (`Table.Steers`) — the runtime's `SteerSource`,
//     which resolves each row's route NAME against the live registry before
//     handing the runtime a model object (I2: the runtime never sees a
//     string that names a provider).
//   - `Table.StepSteerState` — the one row-read LW4's refusals need.
//
// A daemon without a store wires none of it: the handlers refuse by name and
// the loop runs exactly as it did before T4.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// ── protocol.SteerStore over the daemon's StepStore ──────────────────────────

func steerViewOf(st store.Steer) protocol.SteerView {
	v := protocol.SteerView{
		SteerID: st.ID, Text: st.Text, Route: st.Route, ReasoningEffort: st.ReasoningEffort,
		State: string(st.State), CreatedAt: st.CreatedAt,
		DeliveredAt: st.DeliveredAt, Iteration: st.DeliveredIteration,
		Skills: st.Skills,
	}
	for _, r := range st.Attachments {
		v.Attachments = append(v.Attachments, protocol.AttachmentReceiptView{
			Name: r.Name, Document: r.Document, Rev: r.Rev,
			MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
		})
	}
	// The user case is the ABSENT case on the wire (D7): frames that
	// steers always had stay byte-identical; a label rides only when the
	// steer did not come from a person typing (S39 CR6).
	if st.Source != "" && st.Source != "user" {
		v.Source = st.Source
	}
	return v
}

// QueueSteer stores the row. Validation already happened at the door
// (protocol/steer.go: bounds with remedies attached); the store re-checks its
// own invariants and this adapter carries its refusals through.
//
// The S68 half (B6/B7): chosen skill NAMES are validated HERE, against the
// live catalog — unknown names refuse the call (ErrBadParams, every offender
// named, which handleSteer passes through); known-but-unpickable names are
// skipped and REPORTED on the view (the caller sees what did not ride); the
// row stores the names that rode beside the operator's PURE text.
func (s *StepStore) QueueSteer(stepID int64, text, route, effort string, skills []string, attachments []protocol.AttachmentRef) (protocol.SteerView, error) {
	var res *chosenSkillResolution
	if hasAnyName(skills) {
		if s.resolveChosen == nil {
			return protocol.SteerView{}, protocol.ErrBadParams{Err: fmt.Errorf(
				"skills were chosen for a steer, but no skills catalog is wired into this daemon — nothing was chosen")}
		}
		var err error
		if res, err = s.resolveChosen(skills); err != nil {
			return protocol.SteerView{}, protocol.ErrBadParams{Err: err}
		}
	}
	// S69 AT3: every reference resolves against the store BEFORE the row —
	// a steer claiming a file its loop never put is refused naming every
	// offender, and the filed receipts are the store's facts (the caller's
	// claims decide nothing). The scope is the step's own row's.
	var receipts []store.AttachmentReceipt
	if attachments != nil {
		step, err := s.store.ReadStep(stepID)
		if err != nil {
			return protocol.SteerView{}, err
		}
		resolved, err := protocol.ResolveAttachmentRefs(storeAttachmentResolver{s.store},
			step.LoopSlug, step.ComponentRoot, attachments)
		if err != nil {
			return protocol.SteerView{}, err
		}
		for _, r := range resolved {
			receipts = append(receipts, store.AttachmentReceipt{
				Name: r.Name, Document: r.Document, Rev: r.Rev,
				MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
			})
		}
	}
	st, err := s.store.QueueSteer(stepID, store.NewSteer{Text: text, Route: route, ReasoningEffort: effort,
		Skills: rodeNames(skills, res), Attachments: receipts})
	if err != nil {
		return protocol.SteerView{}, err
	}
	view := steerViewOf(st)
	if res != nil {
		view.Skipped = res.Skipped
	}
	return view, nil
}

// hasAnyName is the third-state guard: a list of blanks is a list of nothing.
func hasAnyName(names []string) bool {
	for _, n := range names {
		if strings.TrimSpace(n) != "" {
			return true
		}
	}
	return false
}

// rodeNames are the chosen names minus the skipped, deduped in the chosen
// order — what the row can honestly say rode. res nil means nothing was
// chosen at all.
func rodeNames(names []string, res *chosenSkillResolution) []string {
	if res == nil {
		return nil
	}
	skip := map[string]bool{}
	for _, n := range res.Skipped {
		skip[n] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] || skip[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// ListSteers answers `step.steers` — every state, queue order.
func (s *StepStore) ListSteers(stepID int64) ([]protocol.SteerView, error) {
	rows, err := s.store.ListSteers(stepID)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.SteerView, 0, len(rows))
	for _, st := range rows {
		out = append(out, steerViewOf(st))
	}
	return out, nil
}

// CancelSteer withdraws a pending row and reports the OWNING step, which the
// store alone knows: the cancel params name only a steerId, and the
// announcement is a step event.
func (s *StepStore) CancelSteer(steerID int64) (protocol.SteerView, int64, error) {
	res, err := s.store.CancelSteer(steerID)
	if err != nil {
		return protocol.SteerView{}, 0, err
	}
	if !res.OK {
		// Three refusals, three names (P31): the store already separated
		// no-such-row from state-found — carry the separation, do not
		// re-derive it from prose.
		if !res.Found {
			return protocol.SteerView{}, 0, protocol.ErrSteerUnknown.WithWhat(
				"no steer row carries steerId %d", steerID)
		}
		return protocol.SteerView{}, 0, protocol.ErrSteerAlreadySettled.WithWhat(
			"steer %d could not be cancelled: %s", steerID, res.Reason)
	}
	// The row is cancelled; its step rides along for the event. A read that
	// fails here is infrastructure, not refusal — and a cancel that committed
	// must not be reported as anything other than what it was.
	st, err := s.store.ReadSteer(steerID)
	if err != nil {
		return protocol.SteerView{}, 0, fmt.Errorf("daemon: steer %d was cancelled but its owning step "+
			"could not be read for the announcement: %w", steerID, err)
	}
	return steerViewOf(st), st.StepID, nil
}

// CheckpointWithSteers is the delivery half of D4: the checkpoint and the
// steer flips commit together.
//
// A committed checkpoint that delivered FEWER rows than named is not a failed
// write — it is the race the store documents (the person cancelled between
// the drain and the commit), and the transcript still carries what the model
// will read. So this returns nil: the step must not die because a withdrawal
// won a race the person authored. The rows' true states remain readable
// through `step.steers`, which is where the record lives.
func (s *StepStore) CheckpointWithSteers(ctx context.Context, stepID int64, msgs []model.Message,
	manifest json.RawMessage, deliveredIDs []int64, iteration int) error {
	res, err := s.store.CheckpointWithSteers(ctx, stepID, msgs, manifest, deliveredIDs, iteration)
	if err != nil {
		return err
	}
	if !res.OK {
		// The checkpoint COMMITTED; some rows said `cancelled` by then. The
		// short is the person's own doing and the record of it is the rows'
		// state — `step.steers` will show them. Not the step's failure.
		return nil
	}
	return nil
}

// ── the runtime's drain source (I2: names resolve HERE) ──────────────────────

// stepSteers is one step's SteerSource. Each Pending call reads the durable
// queue and resolves every row's route NAME against the live registry, so
// the runtime handles model objects only.
//
// B7 (S68) adds the drain COMPOSITION: the stored `text` is the operator's
// pure words (the bubble reads it alone, B8 keeps it that way forever); the
// wire message the model reads is the chosen skills' canonical lines, a
// blank line, then those words — joined HERE, the one place that still
// joins the two, because a row written before this slice has no skills and
// a row read after a catalog change must not lose the person's instruction.
type stepSteers struct {
	steps   *store.Store
	routes  protocol.Routes
	resolve func(names []string) (*chosenSkillResolution, error)
	stepID  int64
}

func (ss stepSteers) Pending(ctx context.Context) []runtime.Steer {
	rows, err := ss.steps.PendingSteers(ss.stepID)
	if err != nil {
		// The queue could not be read THIS time. Draining nothing is the
		// honest state — the rows stay pending and a later boundary (or the
		// next run) reads them — and it beats failing a step that is busy
		// doing useful work over a transient read.
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	// The scope for attachment expansion is the STEP's — the steer rows
	// hang off it; read once per boundary, and only when something has
	// attachments to resolve (a step steering with words alone pays nothing
	// extra, byte-identical to pre-S69).
	var sc store.Scope
	var haveScope bool
	for _, st := range rows {
		if len(st.Attachments) > 0 {
			step, err := ss.steps.ReadStep(ss.stepID)
			if err != nil {
				return nil
			}
			sc, haveScope = store.Scope{LoopSlug: step.LoopSlug, ComponentRoot: step.ComponentRoot}, true
			break
		}
	}
	out := make([]runtime.Steer, 0, len(rows))
	for _, st := range rows {
		s := runtime.Steer{ID: st.ID, Text: steerWireText(st, ss.resolve), ReasoningEffort: st.ReasoningEffort}
		// S69 AT5: the steer's pinned revisions become content blocks at
		// this, their last moment — read from the store now, base64 on the
		// wire, gone nowhere else. A revision the store no longer has speaks
		// its absence as its own text block beside the words (ruling 3);
		// the steer is never diminished into silence by a missing file.
		if len(st.Attachments) > 0 && haveScope {
			for _, r := range st.Attachments {
				doc, sniff, err := ss.steps.ReadAttachment(sc, r.Document, r.Rev)
				if err != nil {
					s.Attachments = append(s.Attachments, model.TextBlock(fmt.Sprintf(
						"(the attachment %s at revision %d is no longer in this loop's store: %v — the steer's words ride without it)",
						r.Document, r.Rev, err)))
					continue
				}
				data := base64.StdEncoding.EncodeToString([]byte(doc.Content))
				if strings.HasPrefix(sniff, "image/") {
					s.Attachments = append(s.Attachments, model.ImageDataBlock(r.Name, sniff, data))
				} else {
					s.Attachments = append(s.Attachments, model.DocumentDataBlock(r.Name, sniff, data))
				}
			}
		}
		if st.Route != "" {
			if ss.routes != nil {
				if route, ok := ss.routes.Route(st.Route); ok {
					s.Route = &route
					continue
				}
				// The route a steer named has since been deleted. Delivering
				// the steer WITHOUT the override keeps the person's words —
				// the instruction still matters — where dropping the whole
				// row would silently lose them. `step.steers` shows the row
				// it was: text plus a route that no longer resolves.
			}
		}
		out = append(out, s)
	}
	return out
}

// steerWireText is B7's composition. Nothing chosen is today's byte-for-byte
// text; a catalog that moved since the queue (names gone, scan failing)
// delivers the PURE text — the operator's instruction outranks its rider,
// and the row still shows what was asked (`step.steers` carries the names).
func steerWireText(st store.Steer, resolve func(names []string) (*chosenSkillResolution, error)) string {
	if len(st.Skills) == 0 || resolve == nil {
		return st.Text
	}
	res, err := resolve(st.Skills)
	if err != nil || res == nil || len(res.Lines) == 0 {
		return st.Text
	}
	if strings.TrimSpace(st.Text) == "" {
		return strings.Join(res.Lines, "\n")
	}
	return strings.Join(res.Lines, "\n") + "\n\n" + st.Text
}

// steerSourceFor builds the RunOpts factory: one source per step, bound to
// the store, the table's own route registry, and the chosen-skills resolver
// that renders names to lines at the drain (nil renders pure text).
func steerSourceFor(steps *store.Store, routes protocol.Routes,
	resolve func(names []string) (*chosenSkillResolution, error)) func(int64) runtime.SteerSource {
	if steps == nil {
		return nil
	}
	return func(stepID int64) runtime.SteerSource {
		return stepSteers{steps: steps, routes: routes, resolve: resolve, stepID: stepID}
	}
}

// ── the LW4 read ─────────────────────────────────────────────────────────────

// stepSteerState is the one row-read the steer door needs: status and
// pause_reason decide the refusal and its remedy; the identity fields
// decorate `step.steer.queued` (D1).
func stepSteerState(steps *store.Store) func(int64) (string, string, string, string, string, bool) {
	if steps == nil {
		return nil
	}
	return func(stepID int64) (string, string, string, string, string, bool) {
		st, err := steps.ReadStep(stepID)
		if err != nil {
			// A read that failed and a row that never was are different
			// facts — but both leave the door unable to promise anything,
			// and the refusal that reads "the step is running" would be the
			// dangerous guess. found=false, and the door refuses.
			return "", "", "", "", "", false
		}
		return st.Status, st.PauseReason, st.Agent, st.ComponentRoot, st.LoopSlug, true
	}
}
