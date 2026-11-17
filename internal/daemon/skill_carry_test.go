package daemon

import (
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
)

// T0 carry: the skill target and its params ride from the pinned blueprint step
// to the dispatch params. A plain step gains NOTHING — the wire stays
// byte-identical for every pre-S14 lane.

func TestStepParamsForCarriesSkillTarget(t *testing.T) {
	st := loop.LoopState{}
	st.Root = "/repo"
	st.Route = "r1"
	st.Slug = "ask"
	step := blueprint.Step{
		Kind:   blueprint.ModelStep,
		Agent:  "planner",
		Skill:  "reddit-agent/summarise-sentiment",
		Params: map[string]any{"query": "aeon", "max": 5},
	}
	p := stepParamsFor(st, step, "ask the reddit agent", "the frame")
	if p.Prompt != "ask the reddit agent" || p.Agent != "planner" || p.WorkspaceRoot != "/repo" || p.Route != "r1" || p.LoopSlug != "ask" {
		t.Fatalf("pre-existing carry broken: %#v", p)
	}
	if p.Skill != "reddit-agent/summarise-sentiment" {
		t.Fatalf("skill target not carried: %#v", p)
	}
	if p.Params["query"] != "aeon" {
		t.Fatalf("params not carried: %#v", p.Params)
	}
	// S68: the instruction frame rides `taskFrame` — the prompt stays the
	// operator's words alone.
	if p.TaskFrame != "the frame" {
		t.Fatalf("the frame did not ride taskFrame: %#v", p)
	}
}

func TestStepParamsForPlainStepStaysPlain(t *testing.T) {
	st := loop.LoopState{}
	st.Root = "/repo"
	p := stepParamsFor(st, blueprint.Step{Kind: blueprint.ModelStep, Agent: "golang-engineer"}, "write go", "")
	if p.Skill != "" || p.Params != nil || p.TaskFrame != "" {
		t.Fatalf("a plain step grew S14/S68 fields on the wire: %#v", p)
	}
}
