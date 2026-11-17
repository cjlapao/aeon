package loop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S31 T2 (D6): a PERSON stopped a loop step. The loop parks as `waiting` with
// reason `stopped` — never `failed`, never `blocked` — and the step's later
// completion is handed back through CompleteSuspendedStep exactly as a
// question-suspended step's is. These tests are the state machine's side of
// that; the daemon's stop plumbing is tested in internal/daemon.

// stoppedBlueprint is two model lanes: stop the first, complete it by hand,
// and the second must run afterwards with no other push.
const stoppedBlueprint = "id: stop-me\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n  - id: summarise\n    step: {kind: model-step, agent: default, produces: SUMMARY.md}\n"

func startStoppedLoop(t *testing.T, s *store.Store) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "stop-me.yaml")
	if err := os.WriteFile(path, []byte(stoppedBlueprint), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "stop-me", ComponentRoot: root, Root: root, Goal: "two files", Mode: "standard",
	}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return st.ID, root
}

func TestStoppedModelStepParksTheLoopWaitingNotFailedNotBlocked(t *testing.T) {
	s := openLoopStore(t)
	id, root := startStoppedLoop(t, s)

	var waitEvents []map[string]any
	calls := 0
	dep := Deps{Store: s, Event: func(kind string, payload any) {
		if kind == "loop.waiting" {
			if fields, ok := payload.(map[string]any); ok {
				waitEvents = append(waitEvents, fields)
			}
		}
	}, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		calls++
		if calls == 1 {
			return ModelResult{StepID: 7, Kind: "stopped"}, nil
		}
		if err := os.WriteFile(filepath.Join(root, "SUMMARY.md"), []byte("summary\n"), 0600); err != nil {
			return ModelResult{}, err
		}
		return ModelResult{StepID: 8, Kind: "completed", Outcome: "ok"}, nil
	}}

	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("model called %d times; a stopped step must park, not retry", calls)
	}
	st, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	// The three-state wrong answers this task exists to prevent: `failed` and
	// `blocked` both read as "something broke"; a person stopped it.
	if st.Status != "waiting" {
		t.Fatalf("status = %q, want waiting (a person stop is never failed/blocked)", st.Status)
	}
	if st.LastStepID != 7 || st.LastReason != "stopped" {
		t.Fatalf("lastStep=%d lastReason=%q, want 7/stopped", st.LastStepID, st.LastReason)
	}
	if st.StepStates[stepKey(st.Position)].Status != "waiting" {
		t.Fatalf("step state = %+v, want waiting at the stopped position", st.StepStates[stepKey(st.Position)])
	}
	if len(waitEvents) != 1 {
		t.Fatalf("loop.waiting emitted %d times, want exactly 1", len(waitEvents))
	}
	if waitEvents[0]["reason"] != "stopped" || waitEvents[0]["stepId"] != int64(7) {
		t.Fatalf("loop.waiting payload = %+v, want reason=stopped stepId=7", waitEvents[0])
	}

	// Waiting holds no goroutine and re-running changes nothing: the loop only
	// moves when the STEP reports, not when somebody pokes the ladder.
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("Run on a stopped loop called the model again (%d calls)", calls)
	}

	// Re-stopped after a continue (the hand-back says stopped again): it parks
	// again — still not blocked, still not done.
	if err := CompleteSuspendedStep(context.Background(), dep, id, ModelResult{StepID: 7, Kind: "stopped"}); err != nil {
		t.Fatal(err)
	}
	again, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "waiting" || len(waitEvents) != 2 || waitEvents[1]["reason"] != "stopped" {
		t.Fatalf("re-stop did not re-park: status=%q waits=%+v", again.Status, waitEvents)
	}

	// Completed by hand (what `step.continue`'s OnStepContinued does): the
	// ladder moves on its own from there.
	if err := os.WriteFile(filepath.Join(root, "SURVEY.md"), []byte("survey\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CompleteSuspendedStep(context.Background(), dep, id, ModelResult{StepID: 7, Kind: "completed", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || done.Status != "done" {
		t.Fatalf("after hand-back: calls=%d status=%q, want 2/done", calls, done.Status)
	}
}
