package loop

import (
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

func toy() blueprint.Blueprint {
	return blueprint.Blueprint{ID: "toy", SchemaVersion: 1, Lanes: []blueprint.Lane{{ID: "read", Step: &blueprint.Step{Kind: blueprint.ModelStep, Agent: "default"}}, {ID: "approve", Step: &blueprint.Step{Kind: blueprint.Gate, Question: "approve"}}, {ID: "push", Step: &blueprint.Step{Kind: blueprint.CodeAction, Action: "forge.openPR"}}}, Questions: map[string]blueprint.Question{"approve": {Prompt: "Continue?", Answers: []string{"yes", "no"}}}}
}

func TestDecideActionTable(t *testing.T) {
	bp := toy()
	cases := []struct {
		lane, status string
		want         ActionKind
	}{{"read", "running", ActRunModelStep}, {"approve", "running", ActAskGate}, {"push", "running", ActRunCodeAction}, {"approve", "waiting", ActWaiting}, {"", "running", ActDone}, {"read", "blocked", ActBlocked}}
	for _, tc := range cases {
		st := LoopState{Status: tc.status, Mode: "standard", RuntimeState: InitialState(bp)}
		st.Position.Lane = tc.lane
		got := Decide(&bp, st)
		if got.Kind != tc.want {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
}
func TestDecideIsPure(t *testing.T) {
	bp := toy()
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	before := st
	first := Decide(&bp, st)
	second := Decide(&bp, st)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(before, st) {
		t.Fatal("Decide mutated or drifted")
	}
}
func TestDecideNeverPanics(t *testing.T) {
	for _, st := range []LoopState{{}, {Status: "running", RuntimeState: RuntimeState{Position: Position{Lane: "missing"}}}, {Status: "running", RuntimeState: RuntimeState{Position: Position{Lane: "build", Item: "missing", Step: "missing"}, Tasks: []string{"real"}}}} {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("panic: %v", recovered)
				}
			}()
			bp := toy()
			_ = Decide(&bp, st)
		}()
	}
}
func TestContradictoryStateIsBlockedNotPanic(t *testing.T) {
	bp := toy()
	st := LoopState{Status: "running", RuntimeState: InitialState(bp)}
	st.Position.Lane = "removed"
	a := Decide(&bp, st)
	if a.Kind != ActBlocked || a.Blocked == nil || a.Blocked.Kind != "contradictory-state" {
		t.Fatal(a)
	}
}
func TestIterateNestingOrder(t *testing.T) {
	bp := blueprint.Blueprint{ID: "iter", Lanes: []blueprint.Lane{{ID: "build", Iterate: "tasks", Ladder: []blueprint.Step{{ID: "a", Kind: blueprint.ModelStep}, {ID: "b", Kind: blueprint.ModelStep}, {ID: "c", Kind: blueprint.ModelStep}}}}}
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	st.Tasks = []string{"one", "two"}
	var got []string
	for i := 0; i < 6; i++ {
		a := Decide(&bp, st)
		if a.Kind != ActRunModelStep {
			t.Fatal(a)
		}
		got = append(got, a.Position.Item+":"+a.Position.Step)
		next, err := NextPosition(&bp, st, a.Position)
		if err != nil {
			t.Fatal(err)
		}
		st.Position = next
	}
	want := []string{"one:a", "one:b", "one:c", "two:a", "two:b", "two:c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
func TestSkipDistinguishesNotNeededFromSkipped(t *testing.T) {
	bp := toy()
	bp.Lanes[0].When = &blueprint.Condition{Field: "flags.optional", Op: blueprint.Eq, Value: true}
	st := LoopState{Status: "running", Mode: "standard", RuntimeState: InitialState(bp)}
	a := Decide(&bp, st)
	if a.Kind != ActSkip || a.SkipAs != "not-needed" {
		t.Fatal(a)
	}
	bp.Lanes[0].When = &blueprint.Condition{Field: "mode", Op: blueprint.Eq, Value: "poc"}
	a = Decide(&bp, st)
	if a.Kind != ActSkip || a.SkipAs != "skipped" {
		t.Fatal(a)
	}
}
