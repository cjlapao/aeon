package runtime

// # Steering a running step (S31 T4 — D3, D5, LW1–LW3, LW6)
//
// A steer is one instruction the person types into a step that is ALREADY
// running. The invariant it has to respect is LW1: the in-flight model call
// always finishes — a step never yanks a request out of the air. So a steer
// waits for the next turn BOUNDARY, and the two places a boundary exists are
// the two drain points in loop.go:
//
//   - before a model call (the resumed step's first iteration rides this one);
//   - after the assistant turn is saved and before any of its tools run —
//     where a steer means those calls are NOT made (LW2), each answered with
//     a synthetic `isError` result saying so, and the steer's own text placed
//     AFTER the results in the same user message, so the model reads "these
//     did not happen" and then "here is what I actually want".
//
// The `question` tool rides the same rule (LW3): a turn that ended in a
// question, steered, does NOT suspend — the question is answered as not-run
// like any other call, and the model decides what to do next. Suspension is
// the model's act, not a trapdoor the person's timing can fall through.
//
// # Why the queue lives outside this package
//
// The runtime never reads a store (I2): `Steers` is an injected source the
// daemon wires to the durable queue, and `nil` means the feature is off —
// every pre-existing caller runs exactly the code it ran before, with one
// interface check per boundary. A source that answers nil (or errors into
// nil, the adapter's honest reading of "I could not read the queue this
// turn") delivers nothing THIS boundary and nothing is lost: the rows are
// pending in the store, and the next boundary asks again.
//
// # Why delivery is a checkpoint call and not a separate write
//
// The steers a turn contains are recorded DELIVERED in the same transaction
// that saves the transcript containing them (`Checkpoint.SteerIDs` → the
// writer's `CheckpointWithSteers`). A design that flipped the rows after the
// checkpoint committed would leave the window this slice exists to close:
// `kill -9` between the two would show a pending steer whose text is already
// in the transcript, and the replay would inject the same instruction twice.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

// Steer is one queued instruction as the RUNTIME sees it — deliberately not
// the store row.
type Steer struct {
	// ID is the durable row's identity, echoed into `step.steer.delivered` and
	// named to the checkpoint so the writer flips exactly these rows.
	ID int64

	// Text is the person's instruction, empty for a route-only steer.
	Text string

	// Route is a RESOLVED route: the name the person gave was looked up by the
	// adapter that built this value, against the same registry the step was
	// compiled from. The runtime does not know route names exist — it applies
	// model objects, which is everything it knew before steering too. A nil
	// Route means no provider change.
	Route *model.Route

	// ReasoningEffort is the literal the person asked for; empty means no
	// change. The compile rule (tools-vs-reasoning) runs again on the new
	// pair — a steer must not smuggle through a combination the route
	// refuses (LW6).
	ReasoningEffort string

	// Attachments are the EXPANDED content blocks of this steer's receipts
	// (S69 AT5): references are resolved to bytes by the daemon's drain —
	// the last moment before the wire — and an attachment the store no
	// longer has arrives as a spoken absence: a text block saying so beside
	// the words, never a silent hole and never a forged zero.
	Attachments []model.Block
}

// SteerSource is the drain, narrowed to the one question the loop asks.
//
// It is called at turn boundaries only — never mid-stream (LW1) — and once
// more after each assistant turn before any tool runs. Returning an empty
// slice is the common case and costs nothing beyond the source's own read.
//
// The contract with the caller: a boundary asks ONCE and uses the answer for
// everything that boundary decides (text placement, tool skipping, the ids
// the checkpoint flips). Asking twice could deliver two different worlds at
// one boundary.
type SteerSource interface {
	Pending(ctx context.Context) []Steer
}

// AppendUserText is the ONE fold that puts the person's words into a
// transcript when they arrive at a plain boundary: text joins a trailing
// user message (the tool-results turn) as another text block — a provider
// reads a turn as "the environment spoke again" only if everything said
// since the assistant is in one place — and otherwise opens a new user turn.
// Copy-on-merge: the caller's slice is never mutated under it.
//
// `step.continue {message}` folds through this same rule (S31 T2), which is
// why it lives here rather than beside either caller: one definition of what
// "the person said something" looks like in a transcript.
func AppendUserText(msgs []model.Message, text string) []model.Message {
	return AppendUserTurn(msgs, text, nil)
}

// AppendUserTurn is AppendUserText widened for S69: the person's words and
// their attachments enter the transcript as ONE turn — the text block first
// (B3: the words are what they typed, alone and leading), the expanded
// content blocks beside them. nil blocks is byte-identical to the old fold
// (D7's promise kept by shape: the nil path never allocates a block).
func AppendUserTurn(msgs []model.Message, text string, blocks []model.Block) []model.Message {
	if text == "" && len(blocks) == 0 {
		return msgs
	}
	var block model.Block
	hasText := text != ""
	if hasText {
		block = model.TextBlock(text)
	}
	lead := 0
	if hasText {
		lead = 1
	}
	if n := len(msgs); n > 0 && msgs[n-1].Role == model.RoleUser {
		out := make([]model.Message, n)
		copy(out, msgs)
		merged := make([]model.Block, 0, len(msgs[n-1].Content)+lead+len(blocks))
		merged = append(merged, msgs[n-1].Content...)
		if hasText {
			merged = append(merged, block)
		}
		merged = append(merged, blocks...)
		out[n-1] = model.Message{Role: model.RoleUser, Content: merged}
		return out
	}
	content := make([]model.Block, 0, lead+len(blocks))
	if hasText {
		content = append(content, block)
	}
	content = append(content, blocks...)
	out := make([]model.Message, 0, len(msgs)+1)
	out = append(out, msgs...)
	return append(out, model.Message{Role: model.RoleUser, Content: content})
}

// steeredNotRun is LW2's exact sentence. It is the text every un-run tool
// call answers with — the model must be able to tell a steer-closure from a
// tool that ran and failed, and a watcher must be able to grep for it.
const steeredNotRun = "not run: the person steered before this tool ran — " +
	"read their message below and decide whether to call it again"

// applySteerOverrides applies the provider/effort changes a drained batch
// carries and re-runs the compile rule on the new pair (LW6): an effort the
// person named passes through `reasoningFor` explicitly (the rule's own
// "explicit wins" clause); a route change without one RE-DERIVES from the
// new route's default under the step's tools — which is the lowering that
// keeps a route refusing tools-with-reasoning from reaching the wire because
// a steer changed one field. It emits `step.route.changed` when the pair
// moved, and nothing else: the caller owns text placement, tool skipping and
// the delivered events, because only the caller knows which boundary this is.
func (st *LoopState) applySteerOverrides(d LoopDeps, steers []Steer, iteration int, midStep bool) {
	var routeTo *model.Route
	effortTo, effortSteered := "", false
	for _, s := range steers {
		if s.Route != nil {
			routeTo = s.Route
		}
		if s.ReasoningEffort != "" {
			effortTo, effortSteered = s.ReasoningEffort, true
		}
	}
	if routeTo == nil && !effortSteered {
		return
	}
	// S18 T8 (O5, N9): a route change takes effect at the NEXT step. Both
	// drain boundaries live INSIDE a step, so a steer-borne route is always
	// deferred here — a tool call produced in one provider's format cannot
	// have its result attached in another's, and the provenance of such a
	// turn would describe neither. The steer is still delivered (its TEXT
	// is the body; the route was the attachment), the refusal is spoken as
	// an event naming the step, and the switch rides out on StepResult for
	// the LOOP to apply before the next dispatch. Effort rides with its
	// route when they came together; effort alone is the same route's dial
	// and applies now, as it always did.
	if midStep && routeTo != nil {
		st.PendingRoute = routeTo
		if effortSteered {
			st.PendingEffort = effortTo
		}
		st.emit(d, StepEvent{Kind: EventRouteDeferred, Route: routeTo.ID, Iteration: iteration,
			Text: fmt.Sprintf("route switch to %q refused mid-step (step %d) — it will take effect "+
				"at the next step: a tool call from one provider cannot be answered in another's format",
				routeTo.ID, st.Request.StepID)})
		return
	}
	from := st.Request.Route
	fromEffort := st.Request.ReasoningEffort
	if routeTo != nil {
		st.Request.Route = *routeTo
	}
	explicit := ""
	if effortSteered {
		explicit = effortTo
	}
	eff, _ := reasoningFor(CompileInput{Route: st.Request.Route, ToolSpecs: st.Request.Tools,
		ReasoningEffort: explicit})
	st.Request.ReasoningEffort = eff
	if st.Request.Route.ID != from.ID || st.Request.ReasoningEffort != fromEffort {
		st.emit(d, StepEvent{Kind: EventRouteChanged, Route: st.Request.Route.ID,
			ReasoningEffort: eff, FromIteration: iteration})
	}
}

// emitSteersDelivered announces each drained steer once. `skipped` is the
// batch's skipped tool_use ids at a tool-skipping boundary, absent at a plain
// one — the payload says what the delivery cost, because a steer that
// cancelled two calls is not the same event as one that cancelled none.
func (st *LoopState) emitSteersDelivered(d LoopDeps, steers []Steer, iteration int, skipped []string) {
	for _, s := range steers {
		st.emit(d, StepEvent{Kind: EventSteerDelivered, SteerID: s.ID, Iteration: iteration,
			SkippedTools: skipped})
	}
}

// steerIDs names the rows the boundary's checkpoint must flip to delivered.
func steerIDs(steers []Steer) []int64 {
	ids := make([]int64, 0, len(steers))
	for _, s := range steers {
		ids = append(ids, s.ID)
	}
	return ids
}

// skippedToolUseIDs names the tool calls LW2 is closing, in emission order.
func skippedToolUseIDs(blocks []model.Block) []string {
	var out []string
	for _, b := range blocks {
		if b.Kind == model.BlockToolUse {
			out = append(out, b.ID)
		}
	}
	return out
}

// steeredResults is LW2's shape: the assistant's tool calls, none made, each
// answered with the not-run result, and the steers' own text AFTER the
// results in the SAME user message — one turn, in reading order: what did not
// happen, then what the person now wants. A turn with no text and no tools
// yields nil, and the caller adds no empty message.
func steeredResults(blocks []model.Block, steers []Steer) []model.Block {
	var out []model.Block
	for _, b := range blocks {
		if b.Kind == model.BlockToolUse {
			out = append(out, model.ToolResultBlock(b.ID, steeredNotRun, true))
		}
	}
	for _, s := range steers {
		if s.Text != "" {
			out = append(out, model.TextBlock(s.Text))
		}
		// S69 AT5: the steer's expanded content blocks ride immediately
		// beside the person's words, in the same turn — the wire reads the
		// steer as one speaking, not two.
		out = append(out, s.Attachments...)
	}
	return out
}
