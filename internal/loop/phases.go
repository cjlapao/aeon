package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// Phases — L27: a lane is the declaration, a loop_phase row is its state.
// v1's protocol BRACKETED each phase: in-progress before dispatch, then
// done/skipped/failed after. That was ritual in all 26 agent bodies; S13
// deleted it there — correctly, because the engine should do it. This
// file is the engine doing it. If it stopped doing it, the behaviour was
// deleted rather than moved.
//
// What is NOT recorded here, and why:
//
//   - An ITERATING lane gets no single status row (L27, D11). One
//     `build: in-progress` cannot say which of five tasks is in review;
//     it would be a third description of the same progress, drifting from
//     both the task rows and the step states. The tasks' rows are the
//     per-item truth.
//   - Lanes outside store.TrackedPhases() — the durable `## Phases`
//     ladder, Q2's recorded answer — are the blueprint's private
//     scaffolding (cleanup, notify, whatever a loop adds). The tracker
//     records what the ladder tracks, by name, and nothing else.
//   - `pending` is never WRITTEN: it is the absence of a row, which is
//     what keeps "still to come" from being fabricated for a lane the
//     loop has not reached.

// trackedLane reports whether a blueprint lane is one the tracker records
// — tracked by name AND not iterating (the two exclusions above).
func trackedLane(st *LoopState, laneID string) (lane *blueprint.Lane, ok bool) {
	for i := range st.Blueprint.Lanes {
		if st.Blueprint.Lanes[i].ID != laneID {
			continue
		}
		l := &st.Blueprint.Lanes[i]
		if l.Iterate != "" {
			return nil, false
		}
		for _, tracked := range store.TrackedPhases() {
			if tracked == laneID {
				return l, true
			}
		}
		return nil, false
	}
	return nil, false
}

// phaseOrd places a lane among the tracked ladder so the tracker reads
// in ladder order, not arrival order.
func phaseOrd(laneID string) int {
	for i, tracked := range store.TrackedPhases() {
		if tracked == laneID {
			return i
		}
	}
	return len(store.TrackedPhases())
}

func phaseRowStatus(ctx context.Context, dep Deps, st *LoopState, laneID string) (string, error) {
	rows, err := dep.Store.Phases(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return "", err
	}
	for _, p := range rows {
		if p.Phase == laneID {
			return p.Status, nil
		}
	}
	return "", nil
}

// openPhase brackets a lane BEFORE its first dispatch: in-progress. It
// only moves a row that is absent or pending — a resumed loop re-enters
// its lane with the bracket already standing, and re-writing it would
// rewrite history the tracker already holds.
func openPhase(ctx context.Context, dep Deps, st *LoopState, laneID string) error {
	if _, ok := trackedLane(st, laneID); !ok {
		return nil
	}
	current, err := phaseRowStatus(ctx, dep, st, laneID)
	if err != nil {
		return err
	}
	if current != "" && current != "pending" {
		return nil
	}
	if err := dep.Store.SetPhase(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, laneID, "in-progress", phaseOrd(laneID)); err != nil {
		return err
	}
	emit(dep, "loop.phase", map[string]any{"loopId": st.ID, "lane": laneID, "status": "in-progress"})
	return nil
}

// closePhase ends a bracket with a terminal status. It closes ONLY an
// open (in-progress) bracket: skipping is recorded by the skip path,
// failure by the block path, and a later close must not paper over the
// earlier truth. Closing a lane that never opened is a no-op: untracked
// lanes and iterating lanes have no bracket to close (and an untracked
// helper lane must stay out of the tracker entirely).
func closePhase(ctx context.Context, dep Deps, st *LoopState, laneID, status string) error {
	if _, ok := trackedLane(st, laneID); !ok {
		return nil
	}
	current, err := phaseRowStatus(ctx, dep, st, laneID)
	if err != nil {
		return err
	}
	if current != "in-progress" {
		return nil
	}
	if err := dep.Store.SetPhase(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, laneID, status, phaseOrd(laneID)); err != nil {
		return err
	}
	emit(dep, "loop.phase", map[string]any{"loopId": st.ID, "lane": laneID, "status": status})
	return nil
}

// recordSkip persists a lane-level skip as decided (O8): not-needed (its
// when was false) and skipped (a mode-driven skip) are DIFFERENT facts —
// a bypassed lane rendered as pending reads "still to come" for something
// that will never happen. The status arrives from decide, already
// distinguished; this writes it, never re-derives it.
func recordSkip(ctx context.Context, dep Deps, st *LoopState, laneID, as string) error {
	if _, ok := trackedLane(st, laneID); !ok {
		return nil
	}
	switch as {
	case "not-needed", "skipped":
	default:
		return fmt.Errorf("loop: lane-level skip %q is not a phase status decide produces (not-needed, skipped)", as)
	}
	if err := dep.Store.SetPhase(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, laneID, as, phaseOrd(laneID)); err != nil {
		return err
	}
	emit(dep, "loop.phase", map[string]any{"loopId": st.ID, "lane": laneID, "status": as})
	return nil
}

// closeTrackedExit closes the lane the position just LEFT, when the
// bracket is still open: a lane the loop walked out of finished. (Failed
// and skipped lanes were closed by their own paths; closePhase keeps any
// row that is not standing open.)
func closeTrackedExit(ctx context.Context, dep Deps, st *LoopState, from Position) error {
	if from.Lane == "" || from.Lane == st.Position.Lane {
		return nil
	}
	return closePhase(ctx, dep, st, from.Lane, "done")
}
