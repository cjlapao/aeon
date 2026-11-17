package loop

import (
	"errors"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// rest.go is S39 CR1's resting state: a blueprint that declares
// `after: converse` ends its RUN and leaves the LOOP idle — conversation
// open, no finished event, recovery and startup walking past it (they
// select status='running'; the idle_loop tests prove the claim, D9). One
// status write path (endStatus), one run-settling path (emitIfOver), one
// reader of what a finished run cost (RunCost, the P32 floor query).

// StatusIdle is the loop's resting status: not running (nothing schedules
// it), not terminal (the conversation is open). It is a LOOP status only —
// a run is never idle (DB6: waiting/blocked/idle are loop conditions, run
// state stays running|done|failed|cancelled).
const StatusIdle = "idle"

// endStatus is what a walk that has run out of lanes writes. An abort stays
// terminal (CR1: a declined plan is a fact, not a rest — and only the
// engine's endStatus call is allowed to decide a terminal; the DoD greps
// `Status = "done"` into nothing precisely so this stays the one sentence
// that ends a walk). after: converse rests; everything else is today's
// done, byte-for-byte.
func endStatus(bp *blueprint.Blueprint, aborted bool) string {
	switch {
	case aborted:
		return "aborted"
	case bp != nil && bp.After == blueprint.AfterConverse:
		return StatusIdle
	default:
		return "done"
	}
}

// runStateFor maps the loop's final state to the loop_run state. The loop
// resting idle does NOT make the run idle — the run ended; the loop is what
// rests (CR1). `failed` arrives only via the run-failed flag (failItem on a
// resting loop: CR1 — a run that fails rests the loop AND says failed).
func runStateFor(st LoopState) string {
	switch {
	case st.Status == "aborted":
		return "cancelled"
	case st.Flags["run-failed"]:
		return "failed"
	default:
		return "done"
	}
}

// emitIfOver settles the run row and publishes the end-of-walk event. Call
// it AFTER save() — the run row's snapshot is the runtime JSON that was
// just persisted, not a second serialization that could disagree (one
// truth). It is a no-op unless st.Status is done, aborted or idle; absent
// run rows (loops predating T1's backfill in a fixture) and an
// already-ended run are ignored, so a double arrival is harmless.
func emitIfOver(dep Deps, st LoopState, id string) error {
	switch st.Status {
	case "done", "aborted", StatusIdle:
	default:
		return nil
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		return err
	}
	run, err := dep.Store.CurrentRun(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // no run rows (old fixture): the loop event still rides below
		}
		return err
	}
	if err := dep.Store.EndRun(id, run.RunSeq, runStateFor(st), raw); err != nil && !errors.Is(err, store.ErrRunNotRunning) {
		return err
	}
	if st.Status == StatusIdle {
		cost, err := dep.Store.RunCost(id, run.RunSeq)
		if err != nil {
			return err
		}
		emit(dep, "loop.run.finished", map[string]any{
			"loopId": id, "runSeq": run.RunSeq, "outcome": runStateFor(st), "status": StatusIdle, "cost": cost,
		})
		return nil
	}
	// The non-resting payload is byte-for-byte today's: a loop that ends
	// says loop.finished with exactly {loopId, status} (D1 — T4 changes who
	// rests, never what a finished loop says).
	emit(dep, "loop.finished", map[string]any{"loopId": id, "status": st.Status})
	return nil
}
