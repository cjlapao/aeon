package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// retry.go — the historical missing-handler block, retried ONCE (T10).
//
// S9-era loops blocked on `unknown handler "…"` when the registry held two
// entries; S13 shipped twenty-five. A blocked loop is durable history — the
// retry does not rewrite the block, it commits ONE transition and dispatches
// ONE step, and refuses everything that is not exactly the historical case:

// RetryRefusal is the structured no. Like every Aeon refusal it names the
// thing refused and the remedy; the app renders it as-is (the daemon hands
// out eligibility and refusal, so the UI shows the DAEMON's answer, not the
// browser's guess).
type RetryRefusal struct {
	Kind   string // stable routing names: see the checks below
	Detail string // one sentence, actionable
}

func (r *RetryRefusal) Error() string { return r.Kind + ": " + r.Detail }

// CheckHandlerRetry is the eligibility function, exported for the daemon:
// `loop.show` renders eligibility from it, and the retry op runs the SAME
// check before touching state — one definition, so the button and the
// daemon can never disagree. Nil means eligible.
func CheckHandlerRetry(st LoopState) *RetryRefusal {
	if st.Status != "blocked" {
		return &RetryRefusal{Kind: "not-blocked", Detail: fmt.Sprintf("loop %s is %s, not blocked — there is nothing to retry", st.ID, st.Status)}
	}
	agent, ok := st.MissingHandlerBlock()
	if !ok {
		return &RetryRefusal{Kind: "not-missing-handler", Detail: "the recorded block is not a missing-handler block — this retry is for the historical `unknown handler` refusal only; every other block has its own remedy (" + st.LastReason + ")"}
	}
	step := stepAt(st.Blueprint, st.Position)
	if step == nil {
		return &RetryRefusal{Kind: "no-step", Detail: "the blocked position names no step — there is nothing to re-dispatch"}
	}
	if step.Kind != blueprint.ModelStep {
		return &RetryRefusal{Kind: "not-model-step", Detail: fmt.Sprintf("the blocked position is a %s step, not a model step — retry dispatches a model step", step.Kind)}
	}
	if step.Agent != agent {
		return &RetryRefusal{Kind: "agent-mismatch", Detail: fmt.Sprintf("the block names agent %q but the position holds %q — an unrelated block stays untouched, exactly as recorded", agent, step.Agent)}
	}
	if _, found := handler.Lookup(agent); !found {
		return &RetryRefusal{Kind: "handler-still-missing", Detail: fmt.Sprintf("the registry still has no handler %q — retry becomes possible when it registers; nothing dispatches meanwhile", agent)}
	}
	if st.StepInFlight {
		return &RetryRefusal{Kind: "step-in-flight", Detail: "the step is already in flight — a retry is not a second dispatch of the same work"}
	}
	if ss := st.StepStates[stepKey(st.Position)]; ss.StepID != 0 {
		return &RetryRefusal{Kind: "receipt-exists", Detail: fmt.Sprintf("the position already holds step row %d — a completed or in-flight receipt is never replayed; resume it, do not re-dispatch it", ss.StepID)}
	}
	return nil
}

// RetryMissingHandlerBlock commits the one transition (blocked → running,
// the blocked step's state cleared, the reason cleared) and dispatches the
// one step through the ordinary ladder. Every refusal leaves the loop
// byte-unchanged: the checks run BEFORE any save, and the save happens
// exactly once.
func RetryMissingHandlerBlock(ctx context.Context, dep Deps, id string) (Result, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		// Unreadable pinned source: nothing is changed, and the error is
		// the store's own words — the loop stays exactly as blocked.
		return Result{}, err
	}
	if r := CheckHandlerRetry(st); r != nil {
		return Result{}, r
	}
	agent, _ := st.MissingHandlerBlock()
	st.Status = "running"
	st.LastReason = ""
	if st.StepStates != nil {
		delete(st.StepStates, stepKey(st.Position))
	}
	if err := save(dep, st); err != nil {
		return Result{}, err
	}
	emit(dep, "loop.retried", map[string]any{"loopId": id, "lane": st.Position.Lane, "step": stepAt(st.Blueprint, st.Position).ID, "agent": agent})
	// The ordinary ladder dispatches the one step — same recovery, same
	// receipts, same blocked-on-error path as any other advance. A retry
	// that "worked" by bypassing the ladder would be a second dispatcher,
	// and second dispatchers are where double-runs live.
	return AdvanceLoop(ctx, dep, id)
}
