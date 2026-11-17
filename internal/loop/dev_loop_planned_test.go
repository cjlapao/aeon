package loop

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// The shipped dev-loop, walked the way production runs it: nothing pre-seeds the loop's task list. The planner files its
// tasks as store rows while its step runs, a person approves the plan, and the build lane must then run over those rows.
// (TestEveryLaneRuns hands the loop its task list up front, which hides exactly this gap.)
func TestDevLoopBuildRunsOverTheTasksThePlannerFiled(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t)
	slug := "planned"
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "plan then build", Mode: "standard"}, devLoopPath, yes(),
		map[string]string{"task_title": "t1", "pr_summary": "t1 lands.", "commit_message": "t1: land"})
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	var fake *forge.Fake
	model := func(ctx context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		devArtifacts(root, step)
		if step.Agent == "code-reviewer" && fake != nil {
			// devApproveReview approves PR #1 only; this walk opens one PR per task.
			for n := 1; n <= 2; n++ {
				_, _ = fake.PostReview(ctx, n, "reads clean", forge.VerdictApprove)
			}
		}
		switch step.Agent {
		case "project-planner":
			for i, id := range []string{"t1", "t2"} {
				if _, err := s.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: id}); err != nil {
					t.Fatal(err)
				}
			}
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, f := devDeps(s, model)
	fake = f
	final, err := devDrive(t, dep, st.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := final.LaneStates["build"]; got != "ok" {
		t.Fatalf("build lane verdict %q, want ok: the planner filed two tasks and the lane must run over them (status %q, reason %q)", got, final.Status, final.LastReason)
	}
}
