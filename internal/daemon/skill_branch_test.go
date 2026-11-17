package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
)

// T4 — the daemon's skill branch is testable without agentsvc, a store or a
// socket, because the branch only KNOWS THE HOOK EXISTS. The nil case is
// AS11's deletability walked: with the hook absent (the state of any daemon
// built without cmd/aeon's wiring — including `rm -r internal/agentsvc`),
// the step says which lane, which skill, and what to do about it.

func TestDispatchSkillWithoutTheHookRefusesByName(t *testing.T) {
	d := &Daemon{}
	_, err := d.dispatchSkill(context.Background(), loop.LoopState{ID: "l1"},
		blueprint.Step{Agent: "project-planner", Skill: "reddit-agent/summarise-sentiment"}, "ask", "")
	if err == nil {
		t.Fatal("an unwired daemon must refuse a skill step")
	}
	for _, want := range []string{"project-planner", "reddit-agent/summarise-sentiment", "not wired"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the step, the skill and the state — missing %q in %q", want, err)
		}
	}
}

func TestDispatchSkillDelegatesWithoutReinterpreting(t *testing.T) {
	var got blueprint.Step
	d := &Daemon{agentInvoke: func(_ context.Context, st loop.LoopState, step blueprint.Step, prompt string, _ string) (loop.ModelResult, error) {
		got = step
		if st.ID != "l1" || prompt != "ask about aeon" {
			t.Fatalf("the branch lost the loop or the prompt: %+v %q", st, prompt)
		}
		return loop.ModelResult{StepID: 7, Kind: "suspended", QuestionID: 3}, nil
	}}
	res, err := d.dispatchSkill(context.Background(), loop.LoopState{ID: "l1"},
		blueprint.Step{Agent: "project-planner", Skill: "reddit-agent/summarise-sentiment", Params: map[string]any{"query": "aeon"}},
		"ask about aeon", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Skill != "reddit-agent/summarise-sentiment" || got.Params["query"] != "aeon" {
		t.Fatalf("the branch reinterpreted the step: %#v", got)
	}
	if res.StepID != 7 || res.Kind != "suspended" || res.QuestionID != 3 {
		t.Fatalf("the hook's result must pass through untouched: %#v", res)
	}
}
