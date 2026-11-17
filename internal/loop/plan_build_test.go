package loop

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// docs/aeon/blueprints/plan-build.yaml is the dev-loop cut to plan -> approve -> build, for diagnosing the hand-off quickly.
// Walked through the real engine: the planner files two task rows (nothing seeds the loop's task list), a person approves,
// and the build lane runs implement then verify on each task, and finishes.
func TestPlanBuildFlowRunsTheBuildLaneOverThePlannedTasks(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	slug := "planbuild"
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "plan then build", Mode: "standard"},
		"../../docs/aeon/blueprints/plan-build.yaml", yes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	var ran []string
	model := func(_ context.Context, cur LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		devArtifacts(root, step)
		if step.Agent == "project-planner" {
			for i, id := range []string{"t1", "t2"} {
				if _, err := s.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: id}); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			ran = append(ran, cur.Position.Item+"/"+cur.Position.Step)
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, _ := devDeps(s, model)
	final, err := devDrive(t, dep, st.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "done" || final.LaneStates["build"] != "ok" {
		t.Fatalf("status %q, build %q (reason %q): the build lane did not run over the planned tasks", final.Status, final.LaneStates["build"], final.LastReason)
	}
	want := []string{"t1/implement", "t1/verify", "t2/implement", "t2/verify"}
	if len(ran) != len(want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	for i := range want {
		if ran[i] != want[i] {
			t.Fatalf("ran %v, want %v", ran, want)
		}
	}
}
