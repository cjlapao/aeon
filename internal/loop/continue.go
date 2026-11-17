package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/budget"
	"github.com/cjlapao/aeon/internal/store"
)

// MarkStepStarted is called after the S5 step row is inserted and before its
// model call. A restart can then join S7's recovered result to this position.
func MarkStepStarted(ctx context.Context, dep Deps, id string, pos Position, stepID int64) error {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return err
	}
	if st.Status != "running" || st.StepInFlight || st.Position.Lane != pos.Lane {
		return fmt.Errorf("loop %s cannot attach step %d at %+v", id, stepID, pos)
	}
	st.Position = pos
	st.LastStepID = stepID
	st.StepInFlight = true
	// S39 CR2: the step belongs to the RUN, not merely the loop. Tagged here
	// — the one place a step attaches to a position — so cost per run is a
	// group-by over run_seq (P32 floor) and an old fixture with no run row
	// keeps working (ErrNotFound is silence, not failure).
	if run, err := dep.Store.CurrentRun(id); err == nil {
		if err := dep.Store.SetStepRun(stepID, run.RunSeq); err != nil {
			return err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return save(dep, st)
}

// CompleteSuspendedStep joins S6's step continuation back to the durable S8
// position. A gate answer uses AnswerGate instead and never enters a step.
func CompleteSuspendedStep(ctx context.Context, dep Deps, id string, result ModelResult) error {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return err
	}
	if st.Status != "waiting" || st.LastStepID != result.StepID {
		return fmt.Errorf("loop %s is not waiting on step %d", id, result.StepID)
	}
	action := Decide(st.Blueprint, st)
	if action.Kind != ActWaiting {
		return fmt.Errorf("loop %s has contradictory waiting state", id)
	}
	step := stepAt(st.Blueprint, st.Position)
	if step == nil || step.Kind != blueprint.ModelStep {
		return fmt.Errorf("loop %s has no model step at its saved position", id)
	}

	// S18 T8 (completed at T9's walk): apply the step's deferred switch at
	// EVERY hand-back — the step already exited to report (P2), and a switch
	// dropped on a suspend/stop branch is lost forever (that queue drained
	// once; nothing drains it again). The mid-step protection lives in the
	// runtime's refusal, not in which branch the loop engine took afterwards.
	applyPendingRoute(dep, id, &st, result)
	if result.Kind == "suspended" {
		st.QuestionID = result.QuestionID
		if err := save(dep, st); err != nil {
			return err
		}
		emit(dep, "loop.waiting", map[string]any{"loopId": id, "questionId": result.QuestionID})
		return nil
	}
	if result.Kind == "stopped" {
		// S31 D6 — the continued step was stopped AGAIN (this is the
		// OnStepContinued hand-back of a second person-stop). Re-park exactly
		// as the first stop did: waiting, reason `stopped`, same position.
		// Recording it as done would move a ladder nobody completed.
		st.LastReason = "stopped"
		st.StepStates[stepKey(st.Position)] = StepState{Status: "waiting", StepID: result.StepID}
		if err := save(dep, st); err != nil {
			return err
		}
		emit(dep, "loop.waiting", map[string]any{"loopId": id, "reason": "stopped", "stepId": result.StepID})
		return nil
	}
	if result.Kind == "capped" {
		// S55 T7 — the raised, continued step hit the cap AGAIN: the raise
		// bought one re-run of the parked step, and that re-run spent its way
		// back into the same wall (or a lower one). Spending the raised cap
		// without another raise is not a step the loop may continue, so
		// re-block exactly as the first hit parked: blocked, reason cap-hit,
		// the NEW hit carried so the next raise knows which budget to raise
		// again. (The phase was closed 'failed' by the first block; a
		// re-run does not reopen it, so closePhase here is its no-op.)
		if result.CapHit == nil {
			return fmt.Errorf("loop %s reported a capped step with no hit", id)
		}
		st.Status = "blocked"
		st.LastReason = "cap-hit"
		st.CapHit = result.CapHit
		st.StepStates[stepKey(st.Position)] = StepState{Status: "waiting", StepID: result.StepID}
		if cerr := closePhase(ctx, dep, &st, st.Position.Lane, "failed"); cerr != nil {
			return cerr
		}
		if dep.BudgetEvent != nil {
			if eerr := dep.BudgetEvent(budget.Hit{BudgetID: result.CapHit.BudgetID, Scope: result.CapHit.Scope, SubjectID: result.CapHit.Subject,
				Dimension: result.CapHit.Dimension, Window: result.CapHit.Window, SpendFloor: result.CapHit.SpendFloor, Cap: result.CapHit.Cap, Reason: result.CapHit.Reason,
				Exact: result.CapHit.Exact}, id); eerr != nil {
				return eerr
			}
		}
		if err := save(dep, st); err != nil {
			return err
		}
		emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "cap-hit", Detail: result.CapHit.Reason}})
		return nil
	}
	outcome := result.Outcome
	reason := result.Reason
	if outcome == "ok" {
		if missing := missingProduced(dep, st.Slug, st.ComponentRoot, st.Root, step.Produces); missing != "" {
			outcome = "reject"
			reason = "declared artifact was not written: " + missing
		}
	}
	// S38 T12: the resumed path applies the SAME contract check — one
	// contract, two doors, no door that forgets it.
	if outcome == "ok" && len(step.Emits) > 0 && !contractVerifiable(dep, *step) {
		st.Status = "blocked"
		st.LastReason = contractUnverifiableKind + ": " + contractUnverifiableDetail
		if cerr := closePhase(ctx, dep, &st, st.Position.Lane, "failed"); cerr != nil {
			return cerr
		}
		return save(dep, st)
	}
	if outcome == "ok" {
		if why := missingRecord(dep, *step, result.StepID); why != "" {
			outcome = "reject"
			reason = contractRejection(why)
		}
	}
	// T6: stored outcome is the honest third value; routing still runs on
	// `outcome` below (an unknown routes as the reject it is).
	st.StepStates[stepKey(st.Position)] = StepState{Status: "done", Outcome: storedOutcome(outcome, reason), Reason: reason, StepID: result.StepID, Cost: result.Cost}
	st.LastOutcome, st.LastReason, st.LastStepID, st.QuestionID = outcome, reasonOrNone(outcome, reason), result.StepID, 0
	// The step's question is settled. The next position must be runnable so
	// the runner can create a gate question or start another model step.
	st.Status = "running"
	from := st.Position
	switch outcome {
	case "ok":
		if err = moveNext(&st, from); err == nil {
			err = closeTrackedExit(ctx, dep, &st, from)
		}
	case "reject":
		if advisory(st.Blueprint, from.Lane) {
			if err = moveNext(&st, from); err == nil {
				st.LaneStates[from.Lane] = "advisory"
				err = closeTrackedExit(ctx, dep, &st, from)
			}
		} else {
			err = routeReject(ctx, dep, id, &st, *step, from)
		}
	default:
		st.Status = "blocked"
		if cerr := closePhase(ctx, dep, &st, from.Lane, "failed"); cerr != nil {
			return cerr
		}
	}
	if err != nil {
		var ex *exhaustionError
		var ew *escalationWait
		switch {
		case errors.As(err, &ex):
			// handleExhaustion already blocked the loop with the exact O1
			// sentence (T4); the rejection text must not be prefixed over
			// the clauses a reader searches for.
		case errors.As(err, &ew):
			// Attended escalation committed its question; the loop waits
			// exactly as a gate would.
			st.Status = "waiting"
		default:
			st.Status = "blocked"
			st.LastReason = strings.TrimSpace(reason + "; " + err.Error())
			if cerr := closePhase(ctx, dep, &st, from.Lane, "failed"); cerr != nil {
				return cerr
			}
		}
	}
	if err := save(dep, st); err != nil {
		return err
	}
	emit(dep, "loop.advanced", map[string]any{"loopId": id, "lane": from.Lane, "outcome": outcome, "cost": result.Cost})
	if st.Status == "blocked" {
		emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "step-" + outcome, Detail: st.LastReason}})
	}
	if err := emitIfOver(dep, st, id); err != nil {
		return err
	}
	return nil
}

func stepAt(bp *blueprint.Blueprint, pos Position) *blueprint.Step {
	if bp == nil {
		return nil
	}
	for i := range bp.Lanes {
		lane := &bp.Lanes[i]
		if lane.ID != pos.Lane {
			continue
		}
		if pos.Step == "" {
			return lane.Step
		}
		for j := range lane.Ladder {
			if lane.Ladder[j].ID == pos.Step {
				return &lane.Ladder[j]
			}
		}
	}
	return nil
}

// BlockCorruptPin preserves a visible domain state when the stored YAML can
// no longer be parsed. The source file is never consulted.
func BlockCorruptPin(dep Deps, id string, cause error) error {
	pin, err := dep.Store.ReadPinnedLoop(id)
	if err != nil {
		return err
	}
	if pin.Loop.Status != "running" && pin.Loop.Status != "waiting" {
		return nil
	}
	if err := dep.Store.SavePinnedRuntime(id, "blocked", pin.RuntimeJSON); err != nil {
		return err
	}
	emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "pin-unparseable", Detail: cause.Error()}})
	return nil
}
