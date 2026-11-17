package loop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// TestDeferredRouteSurvivesASuspendingStep (S18 T8, found by T9's §3 walk) —
// the refusal says a mid-step switch "will take effect at the next step", and
// a step that suspends HAS exited to report it (P2). The steer that carried
// the switch drained once and will never drain again, so a loop engine that
// applied pending routes only on the done path would silently lose the
// switch — the refusal's sentence becomes a lie exactly on the round trip's
// middle leg, where the answer door reads the loop's CURRENT route (Q6).
// Suspension is exit; every hand-back is the boundary.
func TestDeferredRouteSurvivesASuspendingStep(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "stop-me.yaml")
	if err := os.WriteFile(path, []byte(stoppedBlueprint), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "defer-route", ComponentRoot: root, Root: root, Goal: "two files", Mode: "standard",
	}, path, nil, nil, "claude")
	if err != nil {
		t.Fatal(err)
	}
	var applied []map[string]any
	calls := 0
	dep := Deps{Store: s, Event: func(kind string, payload any) {
		if kind == "loop.route.applied" {
			if fields, ok := payload.(map[string]any); ok {
				applied = append(applied, fields)
			}
		}
	}, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		calls++
		if calls == 1 {
			// The run that suspends carries the switch the runtime deferred.
			return ModelResult{StepID: 7, Kind: "suspended", QuestionID: 3, PendingRoute: "gpt"}, nil
		}
		if err := os.WriteFile(filepath.Join(root, "SUMMARY.md"), []byte("summary\n"), 0600); err != nil {
			return ModelResult{}, err
		}
		return ModelResult{StepID: 8, Kind: "completed", Outcome: "ok"}, nil
	}}

	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	now, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if now.Status != "waiting" {
		t.Fatalf("status %q, want waiting (the step suspended)", now.Status)
	}
	if now.Route != "gpt" {
		t.Fatalf("route %q after the suspending hand-back — the deferred switch was LOST with the run that drained it", now.Route)
	}
	if len(applied) != 1 || applied[0]["from"] != "claude" || applied[0]["to"] != "gpt" {
		t.Fatalf("loop.route.applied = %+v, want exactly one claude→gpt", applied)
	}

	// The answer's completion carries no new switch; the loop moves on the
	// route the switch chose, and the applied event does not repeat.
	if err := os.WriteFile(filepath.Join(root, "SURVEY.md"), []byte("survey\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CompleteSuspendedStep(context.Background(), dep, st.ID, ModelResult{StepID: 7, Kind: "completed", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "done" || done.Route != "gpt" || len(applied) != 1 {
		t.Fatalf("after completion: status=%q route=%q applied=%d, want done/gpt/1", done.Status, done.Route, len(applied))
	}
}
