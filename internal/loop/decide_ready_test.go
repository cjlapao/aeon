package loop

import (
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S32 T5 (PA8): DecideReady is the sibling of Decide. At cap 1 it returns
// exactly Decide's result (D8 byte-identity — the serial path is unchanged);
// at cap > 1 it returns the declared-independent set's ready lanes, bounded
// by the cap.

func parallelGroupBP() blueprint.Blueprint {
	return blueprint.Blueprint{ID: "pg", SchemaVersion: 1, Lanes: []blueprint.Lane{
		{ID: "a", ParallelGroup: "g", Step: &blueprint.Step{Kind: blueprint.ModelStep, Agent: "default"}},
		{ID: "b", ParallelGroup: "g", Step: &blueprint.Step{Kind: blueprint.ModelStep, Agent: "default"}},
	}}
}

func TestDecideReadyCapOneIsExactlyDecide(t *testing.T) {
	bp := parallelGroupBP()
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	got := DecideReady(&bp, st, 1)
	want := []Action{Decide(&bp, st)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cap 1 must be exactly Decide: got %+v want %+v", got, want)
	}
}

func TestDecideReadyCapTwoRunsTheWholeGroup(t *testing.T) {
	bp := parallelGroupBP()
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	got := DecideReady(&bp, st, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 ready actions, got %d: %+v", len(got), got)
	}
	lanes := map[string]bool{}
	for _, a := range got {
		lanes[a.Position.Lane] = true
	}
	if !lanes["a"] || !lanes["b"] {
		t.Fatalf("both group lanes must be ready: %+v", got)
	}
}

// A cap that is lower than the group size still returns the reachable lanes,
// bounded — the cap is a ceiling the engine never overshoots (PA2).
func TestDecideReadyCapBoundsTheSet(t *testing.T) {
	bp := parallelGroupBP()
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	got := DecideReady(&bp, st, 1) // cap 1 falls through to Decide, never the set
	if len(got) != 1 || got[0].Kind != ActRunModelStep {
		t.Fatalf("cap 1 must be one serial decision, got %+v", got)
	}
}

// S32 T5 (PA4 Shape B): an iterate lane declared Parallel decides readiness
// per item from task.depends_on plus status, not "the next in ord". A task is
// ready when its status is pending and every dependency reached a successful
// terminal (complete/merged/skipped/superseded); a pending OR a failed
// dependency is NOT settled, so it blocks. This is the walk the serial path
// already uses (escalate.go), applied to the ready set.
func TestDecideReadyShapeBDependsOn(t *testing.T) {
	bp := blueprint.Blueprint{
		ID: "iter-bp",
		Lanes: []blueprint.Lane{{
			ID:       "iter",
			Iterate:  "tasks",
			Parallel: true,
			Ladder: []blueprint.Step{{
				ID:   "m",
				Kind: blueprint.ModelStep,
			}},
		}},
	}
	rows := map[string]store.Task{
		"t1": {TaskID: "t1", Status: string(store.StatusComplete)},                          // a dep already settled
		"b":  {TaskID: "b", Status: string(store.StatusPending), DependsOn: []string{"t1"}}, // ready: dep settled
		"c":  {TaskID: "c", Status: string(store.StatusPending), DependsOn: []string{"d"}},  // blocked: dep pending
		"d":  {TaskID: "d", Status: string(store.StatusPending)},                            // ready: no deps
		"e":  {TaskID: "e", Status: string(store.StatusPending), DependsOn: []string{"f"}},  // blocked: dep FAILED (not a successful terminal)
		"f":  {TaskID: "f", Status: string(store.StatusFailed)},
	}
	st := LoopState{
		ID: "l", Slug: "l", Status: "running",
		Blueprint: &bp,
		RuntimeState: RuntimeState{
			Position: Position{Lane: "iter"},
			Tasks:    []string{"t1", "b", "c", "d", "e", "f"},
		},
		TaskRows: rows,
	}
	ready := DecideReady(&bp, st, 8) // big cap: every ready unit, unbounded by cap

	got := map[string]bool{}
	for _, a := range ready {
		if a.Kind == ActRunModelStep {
			got[a.Position.Item] = true
		}
	}
	// b is ready (its dep t1 is complete); c is not (dep d is still pending);
	// d is ready (no deps); e is not (dep f FAILED, which is not a successful
	// terminal — a failed dependency blocks, it does not release); f is not
	// (it is already failed, not pending); t1 is not (complete, not pending).
	if !got["b"] {
		t.Errorf("Shape B: task b (pending, dep t1 complete) must be ready; ready=%v", got)
	}
	if got["c"] {
		t.Errorf("Shape B: task c (pending, dep d pending) must NOT be ready; ready=%v", got)
	}
	if !got["d"] {
		t.Errorf("Shape B: task d (pending, no deps) must be ready; ready=%v", got)
	}
	if got["e"] {
		t.Errorf("Shape B: task e (pending, dep f FAILED) must NOT be ready — a failed dep blocks; ready=%v", got)
	}
	if got["f"] || got["t1"] {
		t.Errorf("Shape B: terminal tasks must not be re-dispatched; ready=%v", got)
	}
}
