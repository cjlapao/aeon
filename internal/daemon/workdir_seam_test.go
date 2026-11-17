package daemon

// S60 T5 (MQ7): the dispatch seam for the integration worktree. The promise
// has two halves and both are pinned: a loop that never integrated dispatches
// EXACTLY the params it dispatched before S60 (every pre-S60 wire byte still
// stands), and a loop whose WorkDir the integrate evidence moved dispatches
// inside that worktree.

import (
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
)

func TestWorkDirEmptyCaseIsByteIdentical(t *testing.T) {
	st := loop.LoopState{}
	st.Root = "/repo"
	st.Route = "r1"
	st.Slug = "land"
	step := blueprint.Step{Kind: blueprint.ModelStep, Agent: "golang-engineer", Skill: "x/y"}
	p := stepParamsFor(st, step, "resolve the conflict", "the frame")
	if p.WorkspaceRoot != "/repo" {
		t.Fatalf("an empty WorkDir must dispatch at the loop root, got %q", p.WorkspaceRoot)
	}
	if p.Prompt != "resolve the conflict" || p.Agent != "golang-engineer" || p.Route != "r1" ||
		p.LoopSlug != "land" || p.TaskFrame != "the frame" || p.Skill != "x/y" {
		t.Fatalf("the pre-S60 carry moved: %#v", p)
	}
}

func TestWorkDirMovesTheDispatchedWorkspace(t *testing.T) {
	st := loop.LoopState{}
	st.Root = "/repo"
	st.WorkDir = "/repo/.worktrees/integrate-mq-0123456789abcdef0123456789abcdef"
	st.Slug = "land"
	p := stepParamsFor(st, blueprint.Step{Kind: blueprint.ModelStep, Agent: "golang-engineer"}, "run the suite", "")
	if p.WorkspaceRoot != st.WorkDir {
		t.Fatalf("the integrate worktree is the workspace after MQ7, got %q", p.WorkspaceRoot)
	}
	if p.Prompt != "run the suite" || p.LoopSlug != "land" {
		t.Fatalf("only the workspace moved: %#v", p)
	}
}
