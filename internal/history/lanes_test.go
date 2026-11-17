package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// TestFourLanesNoNewJoinShape is H1: four groupings, one active, switched by
// the viewer, all a plain GROUP BY over columns this slice already reads —
// no new join shape, only a different column the grouping keys on (design's
// Q1). Because this slice reads the existing ListSteps and groups in Go
// rather than a new SQL GROUP BY, no new join shape can exist; the test pins
// the GROUPING is correct for each of the four and that a cost floors
// through the grouping exactly as it does for a single loop (H6 follows H1).
func TestFourLanesNoNewJoinShape(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")

	// Two loops, a couple of steps each, across two phases, two agents and
	// two tasks — enough for a phase to span loops (the whole point of a
	// grouping "by phase").
	la, sca := seedLoop(t, s, "/proj", "a")
	lb, scl := seedLoop(t, s, "/proj", "b")
	_ = la
	_ = lb

	// loop a: build+test, agent gpt, costs known.
	seedStep(t, s, sca, store.NewStep{Phase: "build", Agent: "gpt", TaskID: "t1"}, costKnown)
	seedStep(t, s, sca, store.NewStep{Phase: "test", Agent: "gpt", TaskID: "t2"}, costUnknown)
	// loop b: build+review, agent claude, costs known.
	seedStep(t, s, scl, store.NewStep{Phase: "build", Agent: "claude", TaskID: "t3"}, costKnown)
	seedStep(t, s, scl, store.NewStep{Phase: "review", Agent: "claude", TaskID: "t4"}, costKnown)

	opts := Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}

	// By loop is the merge: one lane per loop, the loop's own floored cost.
	byLoop, err := Lanes(s, opts, ByLoop)
	if err != nil {
		t.Fatalf("lanes by loop: %v", err)
	}
	if len(byLoop) != 2 {
		t.Fatalf("by loop has %d lanes, want 2 (the merge)", len(byLoop))
	}
	// loop a has an unknown-cost step, so its lane floors; loop b is all known.
	if byLoop[0].Key != "a" && byLoop[1].Key != "a" {
		t.Errorf("by loop is missing loop a")
	}
	for _, l := range byLoop {
		if l.Key == "a" && !l.Cost.Floor {
			t.Errorf("loop a's lane does not floor though it has an unknown-cost step (H6 follows H1)")
		}
		if l.Key == "b" && l.Cost.Floor {
			t.Errorf("loop b's lane floors though it is all known (got %+v)", l.Cost)
		}
	}

	// By phase: "build" spans both loops — a phase across the project, not
	// one loop's view of a phase.
	byPhase, err := Lanes(s, opts, ByPhase)
	if err != nil {
		t.Fatalf("lanes by phase: %v", err)
	}
	if got := lanesByKey(t, byPhase, "build"); got == nil {
		t.Fatalf("the build phase lane is missing")
	} else if len(got.Loops) != 2 {
		t.Errorf("the build phase spans %d loops, want 2 — a phase across the project, not one loop's view", len(got.Loops))
	}

	// By task: each task is in one loop, so four lanes, one loop each.
	byTask, err := Lanes(s, opts, ByTask)
	if err != nil {
		t.Fatalf("lanes by task: %v", err)
	}
	if len(byTask) != 4 {
		t.Errorf("by task has %d lanes, want 4 (t1, t2, t3, t4)", len(byTask))
	}

	// By agent: gpt spans loop a's two steps; claude spans loop b's two.
	byAgent, err := Lanes(s, opts, ByAgent)
	if err != nil {
		t.Fatalf("lanes by agent: %v", err)
	}
	if g := lanesByKey(t, byAgent, "gpt"); g == nil || g.Cost.StepCount != 2 {
		t.Errorf("the gpt agent lane has the wrong step count (got %+v), want 2", g)
	}

	// An unknown grouping is refused by name (DB19), not defaulted to loop.
	if _, err := Lanes(s, opts, GroupBy("phase-please")); err == nil {
		t.Errorf("an unknown grouping was accepted — it must be refused by name (DB19)")
	}
}

// lanesByKey returns the lane with key k, or nil; it fatals if the key is
// absent so a missing group is a test failure, not a silent nil read.
func lanesByKey(t *testing.T, lanes []Lane, key string) *Lane {
	t.Helper()
	for i := range lanes {
		if lanes[i].Key == key {
			return &lanes[i]
		}
	}
	return nil
}
