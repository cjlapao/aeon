package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// A step that ends blocked without filing its declared output gets one more go, told what is missing and what it
// reported; when that go files it, the loop carries on instead of stopping on work that was in fact done.
func TestAStepThatLostItsDeliverableGetsOneRecoveryAttempt(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "recover.yaml")
	raw := "id: recover\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "rec", ComponentRoot: root, Root: root, Goal: "survey", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	var events []string
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) },
		Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, _ string) (ModelResult, error) {
			prompts = append(prompts, prompt)
			if len(prompts) == 1 {
				return ModelResult{StepID: 1, Kind: "completed", Outcome: "blocked", Reason: "I found X and Y but was cut off before filing"}, nil
			}
			if err := os.WriteFile(filepath.Join(root, step.Produces[0]), []byte("filed\n"), 0600); err != nil {
				t.Fatal(err)
			}
			return ModelResult{StepID: 2, Kind: "completed", Outcome: "ok"}, nil
		}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("%d attempts, want the step and one recovery", len(prompts))
	}
	if !strings.Contains(prompts[1], "RECOVERY") || !strings.Contains(prompts[1], "SURVEY.md") || !strings.Contains(prompts[1], "cut off before filing") {
		t.Errorf("the recovery prompt must name what is missing and carry what the attempt reported: %q", prompts[1])
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "done" {
		t.Errorf("the recovered loop must finish, got %q (%s)", done.Status, done.LastReason)
	}
	if !strings.Contains(strings.Join(events, ","), "loop.step.recovering") {
		t.Errorf("the recovery must be announced in the history: %v", events)
	}
}

// A step that does file its output is never second-guessed, and a recovery that still files nothing is judged as the
// first attempt was.
func TestRecoveryIsOnlyForAMissingDeliverableAndOnlyOnce(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "recover2.yaml")
	raw := "id: recover2\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "rec2", ComponentRoot: root, Root: root, Goal: "survey", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _, _ string) (ModelResult, error) {
		calls++
		return ModelResult{StepID: int64(calls), Kind: "completed", Outcome: "blocked", Reason: "still nothing"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("%d attempts: one go and one recovery, then the loop is judged on the result", calls)
	}
	blocked, _ := Pinned(context.Background(), s, st.ID)
	if blocked.Status != "blocked" {
		t.Errorf("a recovery that files nothing leaves the loop blocked, got %q", blocked.Status)
	}
}
