package daemon

// loop_control.go — S55 T3's daemon side of the control core. The in-process
// operations live in internal/loop/control.go (Stop/Resume/Cancel, one
// eligibility function each, shared with loop.show); this file is what the
// daemon supplies to reach the running steps and the runner, and the human-door
// wrappers that answer the socket (T5) and the CLI (T11).
//
// # The park
//
// A stop's park is a compare-and-set (the store's ParkLoopStopped), and the
// control core's Stop is its single writer — it runs the park AFTER the runner
// has settled (Stop → Wait → ParkLoopStopped), so the person's actor and reason
// ride the one control event, and a lost race is a no-op that names the status
// the loop already moved to (LC2, LC10). The runner's park hook is therefore a
// no-op safety net: a stop that reaches the runner by some other path just lets
// it exit; the loop is parked by the operation, never twice.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// loopControlDeps builds the loop.Deps the control core operations run on. The
// Event seam is the loop's publication door (the daemon's publishLoop, which
// resolves the loop's root and the durable post-commit view), so a stop's
// loop.waiting and a cancel's loop.finished ride the SAME emit as every other
// loop event. Records/Model/Actions are nil here — a control operation reads
// and parks; it dispatches no model step (a resume's parked-step path is the
// daemon's step-continue door, below).
func (d *Daemon) loopControlDeps() loop.Deps {
	return loop.Deps{
		Store: d.store,
		Event: func(kind string, payload any) { d.publishLoop(kind, payload) },
	}
}

// stopLoopSteps is ControlHooks.StopSteps (LC2's fixed order): gather the
// loop's running step ids from the pinned state — every StepState that is
// running and named a StepID (parallel units, S32), plus LastStepID when
// StepInFlight (the serial path) — and stop each through the per-step registry
// FIRST, which records the deliberate-stop bit so the step parks
// suspended/pause_reason=user and the engine maps it to Kind stopped.
// Cancelling the runner context before the step would race the step into
// `failed` (the runtime fails a step on a cancelled context,
// runtime/loop.go:230-236), so the order is steps, then the runner.
//
// Each step is then waited for with a bounded poll (isRunning), and the runner
// is interrupted last with the typed cause ErrStopRequested — a loop with no
// runner (between steps) is not an error. The return is the parked step ids,
// so the operation's loop.waiting can name the first one.
func (d *Daemon) stopLoopSteps(ctx context.Context, loopID string) ([]int64, error) {
	st, err := loop.Pinned(ctx, d.store, loopID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool)
	var running []int64
	add := func(id int64) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		running = append(running, id)
	}
	for _, ss := range st.StepStates {
		if ss.Status == "running" && ss.StepID > 0 {
			add(ss.StepID)
		}
	}
	if st.StepInFlight && st.LastStepID > 0 {
		add(st.LastStepID)
	}
	// Stop each running step through the per-step registry (records the
	// deliberate-stop bit). A step not found running is a no-op — already done,
	// never started, or a different loop.
	for _, id := range running {
		d.stepCancels.stop(id)
	}
	// Wait for each to settle (poll isRunning with a 5 s bound). On timeout the
	// step's next advance sees the parked loop and exits — a stopped step is not
	// a failed one.
	deadline := time.Now().Add(5 * time.Second)
	for _, id := range running {
		for d.stepCancels.isRunning(id) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	// Then the runner: interrupt it with the typed cause (the runner goroutine,
	// on seeing ErrStopRequested, exits; the control core's Stop parks the loop).
	// A loop with no runner (between steps, or a parked one) is not an error.
	if d.loopRunner != nil {
		d.loopRunner.Interrupt(loopID, loop.ErrStopRequested)
	}
	return running, nil
}

// controlHooks is the set of seams the control core (Stop/Resume/Cancel)
// reaches to stop running steps, settle the runner, and start a fresh runner.
// A nil daemon field degrades to a no-op, never a panic.
func (d *Daemon) controlHooks() loop.ControlHooks {
	return loop.ControlHooks{
		StopSteps: func(loopID string) ([]int64, error) {
			return d.stopLoopSteps(context.Background(), loopID)
		},
		Wait: func(loopID string, dur time.Duration) bool {
			if d.loopRunner == nil {
				return true
			}
			return d.loopRunner.Wait(loopID, dur)
		},
		StartRunner: func(ctx context.Context, loopID string) bool {
			if d.loopRunner == nil {
				return false
			}
			return d.loopRunner.Start(ctx, loopID)
		},
	}
}

// StopLoop is loop.stop's daemon door (LC2): the eligibility gate, the fixed
// order, and the compare-and-set park, with the person's actor and reason. A
// non-running loop is a no-op that names the remedy (a result, not an error);
// a refused transition renders verbatim (DB19).
//
// The stop cascade (LC15): when the person's stop took effect — the pin is
// waiting/stopped, whether the engine's walk-end or the core's CAS wrote the
// park — every stop request that was open is settled honored in the desk:
// the person did what the agent asked, and the decider is the stopper. A
// lost race (the loop went terminal under the stop) settles nothing: a
// request asks for a stop, and a cancel is not a stop a person granted.
// Every person stop reaches this door (the socket door, and the CLI's
// loop stop is a socket client of it), and the system's stops (the T9
// watchdog, the T2 CAS with a system actor) do not: a stop is honored by a
// person, never by a watcher.
func (d *Daemon) StopLoop(ctx context.Context, id string, actor store.Actor, reason string) (loop.LoopStopResult, *store.ControlRefusal, error) {
	result, refusal, err := loop.Stop(ctx, d.loopControlDeps(), id, actor, reason, d.controlHooks())
	if err != nil || refusal != nil {
		return result, refusal, err
	}
	if st, perr := loop.Pinned(ctx, d.store, id); perr == nil && st.Status == "waiting" && st.LastReason == "stopped" {
		d.honorOpenStopRequests(id, actor)
	}
	return result, nil, nil
}

// honorOpenStopRequests is the stop cascade's desk side: each open request
// through DecideControlRequest, the store's guarded open→honored transition
// (the trigger refuses anything else). A request a person dismissed in the
// race between the list and the decide is already settled — the stop stands,
// the row says who decided, and nothing here overrides the person's call.
func (d *Daemon) honorOpenStopRequests(loopID string, actor store.Actor) {
	requests, err := d.store.ListControlRequests(store.ControlRequestFilter{LoopID: loopID, Status: "open"})
	if err != nil {
		d.log.Infof("stop cascade: list the open stop requests of %s: %v", loopID, err)
		return
	}
	for _, r := range requests {
		if _, err := d.store.DecideControlRequest(r.ID, "honored", actor.Kind+":"+actor.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue // settled in the race: a dismissal the stop cannot override
			}
			d.log.Infof("stop cascade: honor stop request %d on %s: %v", r.ID, loopID, err)
		}
	}
}

// CancelLoop is loop.cancel's daemon door (LC4): stop the steps, interrupt the
// runner, then the control core's Cancel sets the aborted flag and lets
// endStatus/emitIfOver produce the terminal. The blueprint's on_abort lane is
// NOT run (LC4.3); a second cancel is refused (the flag is already set).
func (d *Daemon) CancelLoop(ctx context.Context, id string, actor store.Actor, reason string) (loop.LoopCancelResult, *store.ControlRefusal, error) {
	return loop.Cancel(ctx, d.loopControlDeps(), id, actor, reason, d.controlHooks())
}

// ResumeLoop is loop.resume's daemon door (LC3). It is the one that decides
// WHICH resume a parked loop needs, from the pin's LastReason and the live
// step row — the daemon reads the store, so a UI-side guess would be a second
// dispatcher's opinion:
//
//   - a parked step (the row's status='suspended') re-runs cold from its
//     checkpoint through the SAME exactly-once seam step.continue uses (S31
//     D6), then the control core's Resume CAS's waiting→running and a fresh
//     runner re-runs it. The person's message (if any) reaches the transcript
//     exactly once.
//   - a between-steps stop (no suspended step) or a watchdog stuck-block goes
//     through the control core's Resume — a compare-and-set waiting/blocked →
//     running, then a fresh runner.
//
// The discriminator is the step ROW's status, NOT the loop's LastStepID: a
// between-steps stop leaves LastStepID pointing at a completed step (the park
// CAS sets last_reason='stopped' but not lastStepId, so the loop field is
// ambiguous), and ContinueWithMessages on a completed step would refuse (and,
// on a bare resume, wipe the transcript).
func (d *Daemon) ResumeLoop(ctx context.Context, id string, actor store.Actor, message string) (loop.LoopResumeResult, *store.ControlRefusal, error) {
	st, err := loop.Pinned(ctx, d.store, id)
	if err != nil {
		return loop.LoopResumeResult{}, nil, err
	}
	parkedStep := false
	if st.LastReason == "stopped" && st.LastStepID > 0 && d.Table.Questions != nil {
		status, _, err := d.Table.Questions.StepState(st.LastStepID)
		if err != nil {
			return loop.LoopResumeResult{LoopID: id}, nil, err
		}
		if status == "suspended" {
			// Cold-restart the step through step.continue's exactly-once seam,
			// then let the control core's Resume (below) CAS the loop and start
			// a fresh runner that re-runs it from the checkpoint (LC3).
			if err := d.resumeParkedStep(ctx, st.LastStepID, message); err != nil {
				return loop.LoopResumeResult{LoopID: id}, nil, err
			}
			parkedStep = true
		}
	}
	// A between-steps stop (no suspended step), or a watchdog stuck-block: the
	// control core's Resume — a compare-and-set to running, then a fresh
	// runner. For a parked step the flip above + the CAS + the fresh runner
	// re-run the step cold (the step-continue path, LC3).
	result, refusal, err := loop.Resume(ctx, d.loopControlDeps(), id, actor, d.controlHooks())
	if err == nil && parkedStep && result.Resumed {
		// The core's How says "runner" (a fresh runner DID start), but the
		// app-facing path is the one the daemon decided: a parked step was
		// cold-restarted, so the resume the person sees is "step".
		result.How = "step"
	}
	return result, refusal, err
}

// resumeParkedStep is the exactly-once flip of a step a person stopped (S31
// D6), the same seam step.continue uses: the flip to running and the
// (optional) message reach the row in ONE transaction, so the message is
// neither lost by a crash nor doubled by a retry. A bare resume (no message)
// flips the row with MarkRunning and does NOT touch the transcript —
// ContinueWithMessages would wipe it (the seam DELETEs the transcript before
// re-inserting). The control core's Resume then re-runs the step cold.
func (d *Daemon) resumeParkedStep(ctx context.Context, stepID int64, message string) error {
	if strings.TrimSpace(message) == "" {
		return d.Table.Questions.MarkRunning(stepID)
	}
	snapshot, err := d.Table.Questions.LoadResume(stepID)
	if err != nil {
		return err
	}
	return d.Table.Questions.ContinueWithMessages(stepID, appendResumeUserText(snapshot.Messages, message))
}

// appendResumeUserText joins the person's resume message to a resumed
// transcript (S31 D6): merged into the trailing user message when one exists
// — keeping provider alternation clean, the same merge rule S31's steer
// injection follows — and appended as its own user turn otherwise.
func appendResumeUserText(msgs []model.Message, text string) []model.Message {
	if n := len(msgs); n > 0 && msgs[n-1].Role == model.RoleUser {
		merged := make([]model.Message, n)
		copy(merged, msgs)
		merged[n-1].Content = append(append([]model.Block{}, merged[n-1].Content...), model.TextBlock(text))
		return merged
	}
	return append(msgs, model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(text)}})
}
