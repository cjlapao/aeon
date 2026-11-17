package loop

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// control.go is S55's loop control core (T3). It holds the one eligibility
// function per operation (CheckStop/CheckResume/CheckCancel) that the daemon
// runs as a gate and loop.show renders, so the button and the engine cannot
// disagree (the CheckHandlerRetry shape), and the operations that orchestrate
// the store CAS (T2) and the daemon-supplied hooks (T3).
//
// It never writes a terminal status. The engine's endStatus is the one writer
// of a walk's terminal (LC4); a cancel here sets the aborted flag and the
// engine's emitIfOver produces the terminal. A stop is a compare-and-set park
// (the store's ParkLoopStopped), not a second truth.

// ControlRefusal is a refused control transition, reused from the store so the
// daemon renders it verbatim (DB19: a refusal is a result, not an error). Kind
// is machine-readable; Detail is the person's sentence (what / why / do).
// A nil *ControlRefusal means the operation is eligible.

// CheckStop is the one stop eligibility (LC2): nil for a running loop; a
// *ControlRefusal naming the remedy for every other state (LC2.4 — a
// non-running loop is a no-op with a pointer, not an error). The daemon runs it
// as the Stop gate and loop.show renders it; the two cannot disagree.
func CheckStop(st LoopState) *store.ControlRefusal {
	if st.Status == "running" {
		return nil
	}
	kind, detail := stopRefusal(st.Status, st.LastReason)
	return &store.ControlRefusal{Kind: kind, Detail: detail}
}

// CheckResume is the one resume eligibility (LC3): nil for the states the
// engine and loop.show read — a stopped-waiting loop (LastReason="stopped") and
// a watchdog stuck-block (LastReason="stuck"). A cap-hit BLOCK is not resumable
// (it is loop.raise, LC9); a cap-hit WAITING is the one exception to that: it
// is the crash window between a raise that committed its CAS and the step kick
// it owed (T7's raise ordering), and a resume there redoes the step from a
// fresh row (S31's tolerance — the checkpoint re-run is abandoned, the raised
// budget is what the re-dispatch sees). Every other state is refused with its
// own remedy.
func CheckResume(st LoopState) *store.ControlRefusal {
	if (st.Status == "waiting" && (st.LastReason == "stopped" || st.LastReason == "cap-hit")) || (st.Status == "blocked" && (st.LastReason == "stuck" || (st.LastReason != "cap-hit" && store.ResumableStepBlock(st.LastReason, st.LastOutcome)))) {
		return nil
	}
	kind, detail := resumeRefusal(st.Status, st.LastReason)
	return &store.ControlRefusal{Kind: kind, Detail: detail}
}

// CheckRaise is the one raise eligibility (LC9, T7): nil for a loop parked on
// a cap — blocked/cap-hit, the normal shape, and waiting/cap-hit, the crash
// window a raise's CAS committed before its step kick (re-raising it re-does
// the flip and kick). A raise needs a cap, so every other state is refused
// with the door that does apply — the same one-sentence-per-state discipline
// as CheckStop/CheckResume, so the gate and loop.show cannot disagree.
func CheckRaise(st LoopState) *store.ControlRefusal {
	if (st.Status == "blocked" && st.LastReason == "cap-hit") || (st.Status == "waiting" && st.LastReason == "cap-hit") {
		return nil
	}
	kind, detail := raiseRefusal(st.Status, st.LastReason)
	return &store.ControlRefusal{Kind: kind, Detail: detail}
}

// raiseRefusal names the remedy for a state a raise cannot apply to. A raise
// is for a cap, so every non-cap state points at the door that does apply.
func raiseRefusal(status, lastReason string) (string, string) {
	switch status {
	case "running":
		return "running", "the loop is running — a raise is for a loop parked on a budget cap"
	case "waiting":
		if lastReason == "stopped" {
			return "stopped", "the loop is stopped — resume it (a raise is for a cap, not a stop)"
		}
		return "waiting-question", "the loop is waiting on a question — answer it, or cancel the loop"
	case "blocked":
		return "blocked", "the loop is blocked — it has its own remedy, not a raise"
	case "idle":
		return "idle", "the loop is idle — close it (loop.close)"
	default:
		return "terminal", "the loop is " + status + " — it is finished; a raise has nothing to do"
	}
}

// CheckCancel is the one cancel eligibility (LC4): nil for running, waiting,
// blocked or idle (an idle loop's gentler door stays loop.close); a refusal for
// a terminal loop (it has no control to apply). A second cancel is not decided
// here — the store's flag read (T2) refuses it.
func CheckCancel(st LoopState) *store.ControlRefusal {
	switch st.Status {
	case "running", "waiting", "blocked", "idle":
		return nil
	}
	return &store.ControlRefusal{Kind: "terminal", Detail: "the loop is " + st.Status + " — it has no control to apply"}
}

// stopRefusal names the remedy for a state a stop cannot apply to. It is the
// one sentence the daemon renders verbatim (DB19: a refusal names the reason
// and the thing refused). A stop is for running work, so every other state
// points at the door that does apply.
func stopRefusal(status, lastReason string) (string, string) {
	switch status {
	case "waiting":
		if lastReason == "stopped" {
			return "stopped", "the loop is stopped — resume it (or cancel it)"
		}
		if lastReason == "cap-hit" {
			return "cap-hit", "the loop is blocked on a budget cap — raise it (loop.raise)"
		}
		return "waiting-question", "the loop is waiting on a question — answer it, or cancel the loop"
	case "blocked":
		switch lastReason {
		case "cap-hit":
			return "cap-hit", "the loop is blocked on a budget cap — raise it (loop.raise)"
		case "stuck":
			return "stuck", "the loop is blocked (stuck) — resume it"
		default:
			return "blocked", "the loop is blocked — it has its own remedy, not a stop"
		}
	case "idle":
		return "idle", "the loop is idle — close it (loop.close)"
	default:
		return "terminal", "the loop is " + status + " — it is finished; a stop has nothing to do"
	}
}

// resumeRefusal names the remedy for a state a resume cannot apply to. Only a
// stopped-waiting loop and a stuck-block are resumable (CheckResume); every
// other state points at the door that does apply.
func resumeRefusal(status, lastReason string) (string, string) {
	switch status {
	case "running":
		return "running", "the loop is running — stop it first (a resume needs a parked loop)"
	case "waiting":
		if lastReason == "cap-hit" {
			// CheckResume admits this (the raise crash window); the sentence
			// exists so any surface that renders the refusal for the state
			// names the cap, not a question.
			return "cap-hit", "the loop is parked on a budget cap after a raise — resume it to re-run the step"
		}
		return "waiting-question", "the loop is waiting on a question — answer it, or cancel the loop"
	case "blocked":
		if lastReason == "cap-hit" {
			return "cap-hit", "the loop is blocked on a budget cap — raise it (loop.raise)"
		}
		return "blocked", "the loop is blocked — it has its own remedy, not a resume"
	case "idle":
		return "idle", "the loop is idle — close it (loop.close)"
	default:
		return "terminal", "the loop is " + status + " — it has no control to apply"
	}
}

// --- The operations --------------------------------------------------------
//
// Stop, Resume and Cancel are the in-process operations T3 ships; the socket
// doors (T5) call them, and loop.show renders the same eligibility the gate
// runs (CheckStop|Resume|Cancel, above), so the button and the engine cannot
// disagree (the CheckHandlerRetry shape). Each takes the daemon-supplied
// ControlHooks it needs to reach the running steps and the runner, and the
// store CAS the transition is. A nil seam is a refusal, never a panic.

// ControlHooks are the daemon-supplied seams the control core reaches to stop
// running steps, settle the runner, and start a fresh runner. The daemon wires
// them in daemon/loop_control.go; a nil seam degrades to a no-op (the op
// still commits its CAS), not a panic.
type ControlHooks struct {
	// StopSteps stops the loop's running steps through the per-step registry
	// (stepCancels), waits for each to settle, then interrupts the runner with
	// ErrStopRequested — the fixed order (LC2: steps BEFORE runner, so a step
	// parks suspended/user rather than failing on a cancelled context). It
	// returns the parked step ids.
	StopSteps func(loopID string) ([]int64, error)
	// Wait blocks until the loop's runner goroutine exits or d elapses (a
	// stop/cancel must not hang on a hung runner — LC4.1: on timeout the op
	// still commits and the runner's next advance sees the new state).
	Wait func(loopID string, d time.Duration) bool
	// StartRunner starts a fresh runner for the loop (a between-steps resume,
	// or a stop that parked a between-steps loop). Returns whether a runner was
	// started (false when one already holds the loop).
	StartRunner func(ctx context.Context, loopID string) bool
}

// stopWaitBound is how long a stop waits for the runner to settle before the
// park CAS runs anyway (the runner's next advance sees the parked loop and
// exits — the park is a compare-and-set, so a late arrival is a no-op).
const stopWaitBound = 5 * time.Second

// cancelWaitBound is the named bound a cancel waits for the runner to exit
// (LC4.1): on timeout the cancel still commits, and the runner's next advance
// sees the terminal loop and exits — a hung runner never blocks a cancel.
const cancelWaitBound = 10 * time.Second

// LoopStopResult is loop.stop's answer (LC2): a person-visible outcome in every
// case, never an error for "nothing was running". Stopped false with a note is
// a no-op that names the remedy (LC2.4), not a failure.
type LoopStopResult struct {
	LoopID      string
	Stopped     bool
	Status      string
	ParkedSteps []int64
	Note        string
}

// Stop parks a running loop to waiting/stopped (LC2). It is a compare-and-set
// park (the store's ParkLoopStopped), NOT a second terminal: the engine's
// endStatus is the only writer of a walk's terminal (LC4). The order is fixed
// — stop the running steps first (each parks suspended/user), then let the
// runner settle, then park — so a stop never races a step into `failed` (the
// runtime fails a step on a cancelled context, runtime/loop.go:230-236).
//
// actor is the person's identity (never from a request); reason is person
// text, stored in the control event's own column, never folded into a
// summary (SEC-033). A non-running loop is a no-op that names the remedy —
// the same CheckStop the daemon runs as a gate and loop.show renders.
func Stop(ctx context.Context, dep Deps, id string, actor store.Actor, reason string, hooks ControlHooks) (LoopStopResult, *store.ControlRefusal, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return LoopStopResult{}, nil, err
	}
	// One eligibility, shared with loop.show: a non-running loop is a no-op
	// with a pointer, not an error (LC2.4).
	if refusal := CheckStop(st); refusal != nil {
		return LoopStopResult{LoopID: id, Status: st.Status, Note: refusal.Detail}, refusal, nil
	}
	result := LoopStopResult{LoopID: id}
	// The fixed order (LC2): stop the running steps FIRST (each parks
	// suspended/user), then let the runner settle. A nil seam is a no-op —
	// there are no steps to stop and the park below still stands.
	if hooks.StopSteps != nil {
		parked, err := hooks.StopSteps(id)
		if err != nil {
			return result, nil, err
		}
		result.ParkedSteps = parked
	}
	if hooks.Wait != nil {
		hooks.Wait(id, stopWaitBound)
	}
	// The compare-and-set park: status='running' → waiting, last_reason='stopped'
	// in one transaction. A lost race (the loop moved to waiting/blocked/done
	// meanwhile) is a no-op, Changed false, with NO event — and the status the
	// loop already has is named so the daemon can render the remedy (LC2, LC10).
	parked, err := dep.Store.ParkLoopStopped(id, actor.Kind, actor.ID, reason)
	if err != nil {
		return result, nil, err
	}
	result.Stopped = parked.Changed
	result.Status = parked.Status
	if !parked.Changed {
		// A lost race: the no-op names the status the loop already moved to,
		// with the remedy, so the caller answers honestly (LC2.4).
		result.Note = stopRefusalNote(parked.Status)
		return result, nil, nil
	}
	// The one loop.waiting a changing stop emits. A mid-step stop carries the
	// first parked step's id; a between-steps stop carries none — the key is
	// omitted, never a zero that reads as "the first step" (P31).
	payload := map[string]any{"loopId": id, "reason": "stopped"}
	if len(result.ParkedSteps) > 0 {
		payload["stepId"] = result.ParkedSteps[0]
	}
	emit(dep, "loop.waiting", payload)
	return result, nil, nil
}

// stopRefusalNote is the remedy a lost-race stop names: the status the loop
// already moved to, pointed at the door that does apply (the same sentence
// CheckStop renders, so the no-op and the button cannot disagree).
func stopRefusalNote(status string) string {
	switch status {
	case "waiting":
		return "the loop is waiting — answer it, or cancel the loop"
	case "blocked":
		return "the loop is blocked — it has its own remedy, not a stop"
	case "idle":
		return "the loop is idle — close it (loop.close)"
	default:
		return "the loop is " + status + " — it is finished; a stop has nothing to do"
	}
}

// LoopResumeResult is loop.resume's answer (LC3). How names the path the
// resume took, as decided by the door that ran it: "runner" is set here (a
// compare-and-set to running plus a fresh runner — the between-steps shape),
// and the daemon's loop.resume door (T5) overrides it to "step" when it cold-
// restarted a parked step through the step-continue seam first. It rides the
// socket's loop.resume result so the app reports the daemon's decision
// rather than guessing which resume a parked loop got.
type LoopResumeResult struct {
	LoopID  string
	Resumed bool
	Status  string
	How     string
}

// Resume moves a parked loop back to running (LC3). Eligible only for a
// stopped-waiting loop and a watchdog stuck-block (CheckResume) — a
// cap-hit is loop.raise, a gate-waiting loop is loop.answer, and every other
// state is refused with its own remedy. A between-steps resume (no parked
// step) CAS's waiting→running and starts a fresh runner; a parked-step
// resume is cold from the checkpoint through the daemon's step-continue path
// (daemon/loop_control.go decides which, from the pin's LastReason/LastStepID).
func Resume(ctx context.Context, dep Deps, id string, actor store.Actor, hooks ControlHooks) (LoopResumeResult, *store.ControlRefusal, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return LoopResumeResult{}, nil, err
	}
	if refusal := CheckResume(st); refusal != nil {
		return LoopResumeResult{LoopID: id}, refusal, nil
	}
	result := LoopResumeResult{LoopID: id}
	// The compare-and-set resume: only the two eligible states move. A lost
	// race is a refusal (the status the loop moved to), never a wrong resume.
	parked, refusal, err := dep.Store.ResumeLoop(id, actor.Kind, actor.ID)
	if err != nil {
		return result, nil, err
	}
	if !parked.Changed {
		return result, &refusal, nil
	}
	result.Resumed = true
	result.Status = parked.Status
	result.How = "runner"
	if hooks.StartRunner != nil {
		hooks.StartRunner(ctx, id)
	}
	return result, nil, nil
}

// LoopRaiseResult is loop.raise's answer (LC9, T7): a person-visible outcome
// in every case, the way LoopStopResult/LoopResumeResult are. How names the
// shape the raise took — "runner" is the E1 re-dispatch (blocked → running,
// a fresh runner picks the position up), "step" is the E2 same-row re-run
// (blocked → waiting, the parked step flipped back to running and kicked
// through the step-continue seam). Raised false with a Note is a refused
// raise naming its reason, never an error.
type LoopRaiseResult struct {
	LoopID  string
	Raised  bool
	Status  string
	How     string
	Note    string
	Budget  string // the budget id the cap was on (from the pin's CapHit)
	HowHint string
}

// LoopCancelResult is loop.cancel's answer (LC4).
type LoopCancelResult struct {
	LoopID    string
	Cancelled bool
	Status    string
}

// Cancel ends a loop terminal through the engine's one writer (LC4). It sets
// the aborted FLAG and lets endStatus/emitIfOver produce `aborted` (and the
// run's `cancelled` state) — no code writes the terminal a second time, so the
// one-writer rule and the loop_run state hold. It does NOT run the blueprint's
// on_abort lane (LC4.3): a cancel is certain and free, so a person who wants
// the abort lane answers the gate with `abort` instead.
//
// From running it stops the steps then interrupts the runner (LC2's order) and
// waits for the runner to exit (≤ cancelWaitBound; on timeout the cancel still
// commits — the runner's next advance sees the terminal loop and exits). A
// second cancel is refused (the flag is already set, T2).
func Cancel(ctx context.Context, dep Deps, id string, actor store.Actor, reason string, hooks ControlHooks) (LoopCancelResult, *store.ControlRefusal, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return LoopCancelResult{}, nil, err
	}
	if refusal := CheckCancel(st); refusal != nil {
		return LoopCancelResult{LoopID: id}, refusal, nil
	}
	result := LoopCancelResult{LoopID: id}
	// A running loop: stop the steps then interrupt the runner (LC2's order),
	// and wait for the runner to settle. On timeout the cancel still commits —
	// the runner's next advance sees the terminal loop and exits (LC4.1).
	if hooks.StopSteps != nil {
		if _, err := hooks.StopSteps(id); err != nil {
			return result, nil, err
		}
	}
	if hooks.Wait != nil {
		hooks.Wait(id, cancelWaitBound)
	}
	// The cancel records its intent: the aborted flag in the runtime JSON, the
	// control event with the reason in its own column. The store never writes
	// the terminal status itself. A second cancel (the flag already set) is
	// refused here — the zero-value ControlRefusal (Kind == "") means the
	// cancel went through.
	_, refusal, err := dep.Store.CancelLoop(id, actor.Kind, actor.ID, reason)
	if err != nil {
		return result, nil, err
	}
	if refusal.Kind != "" {
		return result, &refusal, nil
	}
	// The engine's endStatus is the ONE writer of a walk's terminal: the cancel
	// set the aborted flag above, and endStatus + emitIfOver now produce
	// `aborted` (and the run's `cancelled` state). The position is cleared so a
	// recovery never re-walks a cancelled loop; on_abort is NOT run (LC4.3).
	st, err = Pinned(ctx, dep.Store, id)
	if err != nil {
		return result, nil, err
	}
	st.Status = endStatus(st.Blueprint, true)
	st.Position = Position{}
	if err := save(dep, st); err != nil {
		return result, nil, err
	}
	if err := emitIfOver(dep, st, id); err != nil {
		return result, nil, err
	}
	result.Cancelled = true
	result.Status = st.Status
	return result, nil, nil
}
