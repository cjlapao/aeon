package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// `step.stop` and `step.continue` — a PERSON interrupting a step, and
// resuming one they stopped. Added for the chat view (S9): watching a step
// stream is not useful if there is no way to pause it, and picking the
// provider or the reasoning effort only makes sense once it is paused —
// changing either while a model call is in flight would not be "the next
// call reasons differently", it would be changing the ground under a call
// already running.
//
// # Why this needed no new package, and touches none of S11's
//
// Everything here reuses machinery S6/S7 already built: `ResumeRequest`'s
// `Recovery: true` path (which already skips the question-tool validation a
// real answer needs — see migration `0019`'s doc comment for why this is the
// right door and a new `question` kind would not be), `LoadResume`,
// `MarkRunning`, and the checkpoint a running step already saves every turn
// (so a stop between turns always has something to resume from). The one
// genuinely new piece is `internal/daemon/stepcancel.go`'s per-step cancel
// registry, which lives in the daemon because it needs state shared across
// RPC calls on different connections — not in `internal/loop`,
// `internal/tools` or `internal/policy`, which is why this can land next to
// S11 without touching anything S11 owns.
//
// # What this also does since S31 T2 — loop steps (D6)
//
// A step running INSIDE a blueprint loop (`loop.start`) is stopped and
// resumed through these same two methods. `handleStep` tracks every run —
// loop included — under its own cancellable context, so `step.stop` reaches
// a loop step by id; the loop's Model callback maps a stopped step to the
// ladder's `stopped` kind, which PARKS the loop (`waiting`, reason
// `stopped`, carrying the `stepId`) rather than accusing it of failing or
// blocking; and `step.continue` on such a step resumes it cold, with the
// `OnStepContinued` handback moving the ladder on exactly as a first run
// would (the loop is found by its pinned `LastStepID`, the same seam the
// question path uses).
//
// `message?` is the second half of D6's stop-and-inject: `step.stop` then
// `step.continue {message}`. The text is checkpointed into the transcript in
// the SAME transaction that flips the row back to running, so it is
// delivered exactly once — a retry cannot double it (the row is no longer
// suspended), a crash between continue and the first model call cannot lose
// it (the running row already carries it for recovery to replay).

// StepStopParams names the step to interrupt.
type StepStopParams struct {
	StepID int64 `json:"stepId"`
}

// StepStopResult says whether anything was actually running.
type StepStopResult struct {
	StepID  int64 `json:"stepId"`
	Stopped bool  `json:"stopped"`
}

func (t *Table) handleStepStop(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p StepStopParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.StepID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("stepId must be positive")}
	}
	if t.CancelStep == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon cannot stop a running step")
	}
	if !t.CancelStep(p.StepID) {
		// Not an error: "already stopped" and "never running" both land here,
		// and a client racing its own double-click is ordinary (T3's rule for
		// `unsubscribe`, applied the same way here).
		return StepStopResult{StepID: p.StepID, Stopped: false}, nil
	}
	return StepStopResult{StepID: p.StepID, Stopped: true}, nil
}

// StepContinueParams resumes a step a person stopped, optionally moving it to
// a different route or reasoning effort now that nothing is mid-flight.
type StepContinueParams struct {
	StepID int64 `json:"stepId"`

	// Route overrides the step's own for this continuation. Empty keeps it —
	// the exact convention `answer`'s resume path already uses (replaying a
	// suspended step through "the route's CURRENT provider", by id).
	Route string `json:"route,omitempty"`

	// ReasoningEffort overrides it for this continuation. Empty keeps the
	// step's own, which may itself have been lowered by assembly (`compile.go`)
	// if the step offers tools and its route's API refuses tools with
	// reasoning on — that rule runs again on resume for the same reason, so an
	// override here is taken literally the same way CompileInput's is.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// Message is S31 D6's stop-and-inject half: text from the PERSON, joined
	// to the transcript as a user turn before the resumed model call and
	// checkpointed in the same transaction that flips the row back to
	// running — delivered exactly once across a crash. Whitespace-only is
	// treated as absent.
	Message string `json:"message,omitempty"`
}

func (t *Table) handleStepContinue(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p StepContinueParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.StepID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("stepId must be positive")}
	}
	if !model.ValidReasoningEffort(strings.TrimSpace(p.ReasoningEffort)) {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"reasoningEffort %q is not one of %s", p.ReasoningEffort,
			strings.Join(model.ReasoningEfforts(), ", "))}
	}
	if t.Questions == nil || t.Steps == nil {
		return nil, ErrStoreUnavailable
	}
	if t.Recovering != nil && t.Recovering(p.StepID) {
		return nil, ErrStepRecovering.WithWhat("step %d is recovering", p.StepID)
	}

	// The precondition this method exists to enforce: a step that is not
	// suspended-by-a-stop gets a NAMED refusal rather than a resume attempt
	// that corrupts a step actually running, or answers a real question with
	// no answer at all.
	status, pauseReason, err := t.Questions.StepState(p.StepID)
	if err != nil {
		return nil, ErrResumeUnavailable.WithWhat("step %d's state could not be read: %v", p.StepID, err)
	}
	if status != "suspended" || (pauseReason != "user" && pauseReason != "cap") {
		return nil, ErrStepNotPaused.WithWhat(
			"step %d has status %q and pause reason %q; step.continue only resumes a step a "+
				"person stopped", p.StepID, status, pauseReason)
	}
	if pauseReason == "cap" {
		// S55 T7: a cap has no one to answer — the door for this shape is
		// loop.raise (T8), which raises the budget and then re-runs the step
		// through Table.ContinueCapStep in-process. The socket door stays
		// user-only: a bare continue on a cap step would spend the step's
		// way back into the same wall nobody raised.
		return nil, ErrStepNotPaused.WithWhat(
			"step %d stopped on a budget — raise it with loop.raise (a cap has no one to answer)", p.StepID)
	}
	// Defensive second check against the live registry, not just the stored
	// row: a step whose row says "suspended" but is SOMEHOW still tracked
	// (a bug elsewhere, or a race this guard is the net for) must not be
	// started a second time.
	if t.StepIsRunning != nil && t.StepIsRunning(p.StepID) {
		return nil, ErrStepNotPaused.WithWhat(
			"step %d is still tracked as running; refusing to start a second run on it", p.StepID)
	}

	runner, ok := t.Runner.(resumeRunner)
	if !ok {
		return nil, ErrStepUnavailable
	}
	resp, err := t.continueStepCore(p.StepID, p.Route, p.ReasoningEffort, p.Message, runner)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ContinueCapStep is the S55 T7 in-process door for re-running a step a cap
// parked (E2): the daemon's loop.raise op calls it AFTER it has raised the
// budget and moved the loop to waiting/cap-hit. It is deliberately NOT a
// socket method — a bare step.continue on a cap step is refused above, so
// the only path that flips a cap step back to running is one whose caller
// has just paid for the re-run by raising the cap. The validation mirrors
// handleStepContinue's preconditions; the body is the shared core.
func (t *Table) ContinueCapStep(stepID int64) error {
	if t.Questions == nil || t.Steps == nil {
		return ErrStoreUnavailable
	}
	if t.Recovering != nil && t.Recovering(stepID) {
		return ErrStepRecovering.WithWhat("step %d is recovering", stepID)
	}
	status, pauseReason, err := t.Questions.StepState(stepID)
	if err != nil {
		return ErrResumeUnavailable.WithWhat("step %d's state could not be read: %v", stepID, err)
	}
	if status != "suspended" || pauseReason != "cap" {
		return ErrStepNotPaused.WithWhat(
			"step %d has status %q and pause reason %q; a cap re-run needs a step a budget capped",
			stepID, status, pauseReason)
	}
	if t.StepIsRunning != nil && t.StepIsRunning(stepID) {
		return ErrStepNotPaused.WithWhat(
			"step %d is still tracked as running; refusing to start a second run on it", stepID)
	}
	runner, ok := t.Runner.(resumeRunner)
	if !ok {
		return ErrStepUnavailable
	}
	// No route/effort override, no injected message: a raise buys the same
	// step back, exactly as it was — the person's stop-and-inject affordances
	// belong to the user-stop door, not to a budget.
	_, err = t.continueStepCore(stepID, "", "", "", runner)
	return err
}

// continueStepCore is the shared resume body: load the saved request and
// manifest, settle the route and effort, rebuild the tool grant, flip the
// row back to running (with the person's message when the door carries one),
// and queue the continued run. It is handleStepContinue's body after its
// preconditions, and ContinueCapStep's whole body — one resume path, two
// doors, so the exactly-once flip (ContinueWithMessages/MarkRunning) and the
// tracked queueing cannot drift between them.
func (t *Table) continueStepCore(stepID int64, routeOverride, effortOverride, message string, runner resumeRunner) (AnswerResponse, error) {
	snapshot, err := t.Questions.LoadResume(stepID)
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d cannot be resumed: %v", stepID, err)
	}
	var saved stepSnapshot
	if err := json.Unmarshal(snapshot.Manifest, &saved); err != nil || saved.Request.WorkspaceRoot == "" {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d has no usable saved request and manifest", stepID)
	}

	// Route: an explicit override wins; otherwise the step's own id is
	// re-resolved through the CURRENT route config — the exact thing that
	// lets a `routes.set` edit (a new base URL, a corrected key) reach a
	// step that was already running when it was made. A cap re-run passes
	// no override: the raised step keeps its route and effort.
	routeID := saved.Request.Route.ID
	if strings.TrimSpace(routeOverride) != "" {
		routeID = strings.TrimSpace(routeOverride)
	}
	route, err := t.routeForStep(routeID)
	if err != nil {
		return AnswerResponse{}, err
	}
	saved.Request.Route = withThinking(route, saved.Request.Thinking)

	effort := saved.Request.ReasoningEffort
	if strings.TrimSpace(effortOverride) != "" {
		effort = strings.TrimSpace(effortOverride)
	}
	saved.Request.ReasoningEffort = effort

	granted := saved.Request.Handler.Tools
	manifest := saved.ContextManifest
	toolset, _, err := t.Runner.BuildTools(granted, saved.Request.WorkspaceRoot, saved.Request.Handler.Name,
		runtime.FrozenManifest{Entries: manifest.Policies, Hash: manifest.PoliciesHash, Frozen: manifest.PoliciesFrozen})
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d cannot rebuild its tool grant: %v", stepID, err)
	}
	// MD5: the resumed toolset may carry cached MCP server children (a composite
	// caller); close them when resume finishes — not immediately after
	// BuildTools. A no-op when no MCP bundle is wired (MD8).
	defer func() {
		if c, ok := toolset.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	req := runtime.ResumeRequest{
		Request: saved.Request, Messages: snapshot.Messages, Manifest: saved.ContextManifest,
		Usage: saved.Usage, Compactions: saved.Compactions,
		// No Question, no Answer: this is NOT answering anything (DB19 — the
		// absence is the fact, not an omission). `Recovery: true` is what
		// makes that valid — see migration 0019's doc comment. The cap
		// re-run is the same: it resumes the row, it does not answer it.
		Recovery: true,
	}

	// D6 stop-and-inject: the person's text joins the transcript as a user
	// turn and reaches the row in the SAME transaction that marks the step
	// running (QuestionStore.ContinueWithMessages) — which is what makes it
	// exactly-once: a retried continue is refused because the row is no
	// longer suspended, and a crash before the first model call leaves the
	// running row already carrying the message for recovery to replay. A
	// cap re-run carries no message, so it takes the plain MarkRunning flip.
	if msg := strings.TrimSpace(message); msg != "" {
		req.Messages = appendUserText(req.Messages, msg)
		if err := t.Questions.ContinueWithMessages(stepID, req.Messages); err != nil {
			return AnswerResponse{}, ErrResumeUnavailable.WithWhat(
				"step %d could not be continued with its message: %v", stepID, err)
		}
	} else if err := t.Questions.MarkRunning(stepID); err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d could not be marked running: %v", stepID, err)
	}
	if t.OnEvent != nil {
		// Brief §5: `step.continued` carries stepId (+ loopId?). It is filed
		// under the step's project root like every other step event (D1).
		// The route/effort the continuation settled on are the RESPONSE's
		// and `step.show`'s facts — the event does not restate them.
		evp := StepEventPayload{StepID: stepID, Agent: saved.Request.Handler.Name,
			LoopID: t.loopIDFor(saved.LoopSlug, saved.Request.WorkspaceRoot)}
		evp.componentRoot = saved.Request.WorkspaceRoot
		t.OnEvent("step.continued", evp)
	}
	// Queued the same way `continueAnsweredStep` is: nothing in THIS
	// connection's lifetime should decide whether the step keeps running. Tracked
	// (continueAnsweredStepAsync) rather than spawned bare so a store close can
	// wait on it — the goroutine must not outlive the store, only the connection.
	t.continueAnsweredStepAsync(stepID, saved.LoopSlug, runner, req, toolset)

	return AnswerResponse{StepID: stepID, AnsweredAt: "", Status: "running"}, nil
}

// appendUserText joins the person's message to a resumed transcript
// (S31 D6): merged into the trailing user message when one exists — the same
// merge rule S31's steer injection follows, keeping provider alternation
// clean — and appended as its own user turn otherwise. The caller trims.
func appendUserText(msgs []model.Message, text string) []model.Message {
	if n := len(msgs); n > 0 && msgs[n-1].Role == model.RoleUser {
		merged := make([]model.Message, n)
		copy(merged, msgs)
		merged[n-1].Content = append(append([]model.Block{}, merged[n-1].Content...), model.TextBlock(text))
		return merged
	}
	return append(msgs, model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(text)}})
}
