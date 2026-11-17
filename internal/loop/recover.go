package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// recover.go — any blocked loop can be taken back a step and run again.
//
// A block is not the end of a loop: a model that sent a bad tool call, a provider that failed, an action a policy refused
// that the person has since allowed. The remedy that always exists is to go back to the step that blocked and run it
// again from a clean receipt. The old step's rows stay as history; this commits ONE transition (blocked → running, the
// blocked step's receipt cleared, the reason cleared), emits `loop.recovered` with the reason it recovered from, and lets
// the ordinary ladder dispatch the step, the same dispatcher everything else uses, so a recovery is never a second
// way to run work.

// CheckRecover is the eligibility function (the daemon renders it in loop.show and the op runs the same check first, so
// the button and the engine cannot disagree). Nil means eligible.
func CheckRecover(st LoopState) *RetryRefusal {
	if st.Status != "blocked" {
		return &RetryRefusal{Kind: "not-blocked", Detail: fmt.Sprintf("loop %s is %s, not blocked — there is nothing to recover", st.ID, st.Status)}
	}
	step := stepAt(st.Blueprint, st.Position)
	if step == nil {
		return &RetryRefusal{Kind: "no-step", Detail: "the blocked position names no step — there is nothing to run again"}
	}
	if step.Kind == blueprint.Gate {
		return &RetryRefusal{Kind: "gate", Detail: "the loop is at a gate, which waits for a person's decision; answer it instead of recovering"}
	}
	if st.StepInFlight {
		return &RetryRefusal{Kind: "step-in-flight", Detail: "the step is already in flight — recovering is not a second dispatch of the same work"}
	}
	return nil
}

// RecoverBlockedStep runs the blocked step again. Every refusal leaves the loop byte-unchanged: the check runs before any
// save, and the save happens once.
func RecoverBlockedStep(ctx context.Context, dep Deps, id string) (Result, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return Result{}, err
	}
	if r := CheckRecover(st); r != nil {
		return Result{}, r
	}
	was := st.LastReason
	st.Status = "running"
	st.LastReason = ""
	if st.StepStates != nil {
		delete(st.StepStates, stepKey(st.Position))
	}
	if err := save(dep, st); err != nil {
		return Result{}, err
	}
	emit(dep, "loop.recovered", map[string]any{"loopId": id, "lane": st.Position.Lane, "step": stepAt(st.Blueprint, st.Position).ID, "from": was})
	return AdvanceLoop(ctx, dep, id)
}

// BlockedStepID is the step row the blocked position last ran, or 0 when it never started one.
func BlockedStepID(st LoopState) int64 {
	if st.StepStates == nil {
		return 0
	}
	return st.StepStates[stepKey(st.Position)].StepID
}

// RecoverNote is what the agent is told when its last step is tried again: something went wrong, what, and that its
// conversation is intact. Written once here so the app, the CLI and a person's voice all say the same thing.
func RecoverNote(reason string) string {
	if reason == "" {
		reason = "the step stopped without a recorded reason"
	}
	return "[Aeon] Your last step stopped because: " + reason + ". Nothing you did was lost; your conversation is intact. " +
		"Work out what went wrong, correct it, and try the last step again. Do not start over."
}

// RecoverInPlace puts a blocked loop back to running WITHOUT touching the blocked step's receipt: the step row (its
// transcript already carrying the recovery note) is what the runner picks up and runs again from its checkpoint, so the
// agent keeps its context. The daemon prepares the row and starts the runner; this is the loop's one transition.
func RecoverInPlace(ctx context.Context, dep Deps, id string) error {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return err
	}
	if r := CheckRecover(st); r != nil {
		return r
	}
	was := st.LastReason
	st.Status = "running"
	st.LastReason = ""
	st.LastOutcome = ""
	if err := save(dep, st); err != nil {
		return err
	}
	emit(dep, "loop.recovered", map[string]any{"loopId": id, "lane": st.Position.Lane, "step": stepAt(st.Blueprint, st.Position).ID, "from": was, "mode": "in-place"})
	return nil
}
