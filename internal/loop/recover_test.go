package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// Any blocked loop, whatever the block was, can be taken back to the step that blocked and run again.
func TestAnyBlockedLoopCanRecover(t *testing.T) {
	s, id := startRetryLoop(t, "golang-engineer")
	blockLoop(t, s, id, "golang-engineer", func(st *LoopState) {
		st.LastReason = `invalid question: json: cannot unmarshal string into Go struct field .questions`
	})

	var events []string
	dispatches := 0
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) },
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			dispatches++
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}}
	if _, err := RecoverBlockedStep(context.Background(), dep, id); err != nil {
		t.Fatalf("a blocked loop was refused its recovery: %v", err)
	}
	if dispatches != 1 {
		t.Fatalf("the recovery dispatched %d steps, want exactly 1", dispatches)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status == "blocked" || saved.LastReason != "" {
		t.Fatalf("the block was not cleared: %s %q", saved.Status, saved.LastReason)
	}
	found := false
	for _, e := range events {
		found = found || e == "loop.recovered"
	}
	if !found {
		t.Fatalf("no loop.recovered event: %v", events)
	}
}

// A loop that is not blocked has nothing to recover, and the refusal changes nothing.
func TestRecoveryRefusesALoopThatIsNotBlocked(t *testing.T) {
	s, id := startRetryLoop(t, "golang-engineer")
	_, err := RecoverBlockedStep(context.Background(), Deps{Store: s}, id)
	var r *RetryRefusal
	if !errors.As(err, &r) || r.Kind != "not-blocked" {
		t.Fatalf("want a not-blocked refusal, got %v", err)
	}
}

// Recovering in place keeps the blocked step's receipt (the runner re-runs that same step from its checkpoint, so the
// agent keeps its context), clears the block, and dispatches nothing itself.
func TestRecoverInPlaceKeepsTheStepAndDispatchesNothing(t *testing.T) {
	s, id := startRetryLoop(t, "golang-engineer")
	blocked := blockLoop(t, s, id, "golang-engineer", func(st *LoopState) {
		st.LastReason = "invalid question: json: cannot unmarshal string"
		st.StepStates = map[string]StepState{stepKey(st.Position): {StepID: 77, Status: "blocked"}}
	})
	if BlockedStepID(blocked) != 77 {
		t.Fatalf("BlockedStepID = %d", BlockedStepID(blocked))
	}
	dispatches := 0
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		dispatches++
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := RecoverInPlace(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if dispatches != 0 {
		t.Fatalf("recovering in place dispatched %d steps; the runner does that", dispatches)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "running" || saved.LastReason != "" {
		t.Fatalf("the block was not cleared: %s %q", saved.Status, saved.LastReason)
	}
	if BlockedStepID(saved) != 77 {
		t.Fatalf("the step's receipt must be kept so the same step is continued; got %d", BlockedStepID(saved))
	}
}

func TestTheRecoveryNoteSaysWhatWentWrongAndNotToStartOver(t *testing.T) {
	n := RecoverNote("invalid question: json: cannot unmarshal string")
	for _, want := range []string{"invalid question", "conversation is intact", "try the last step again", "Do not start over"} {
		if !strings.Contains(n, want) {
			t.Errorf("the note lacks %q: %s", want, n)
		}
	}
}

// A loop run one agent at a time reached its build lane with an empty task list and skipped it as "not needed", then
// everything after it, even though the planner had filed its tasks. The serial path now reads the task rows.
func TestTheBuildLaneRunsOverTheTasksThePlanFiled(t *testing.T) {
	s := openLoopStore(t)
	path := t.TempDir() + "/build.yaml"
	writeBlueprint(t, path, "  - id: build\n    iterate: tasks\n    ladder:\n      - {id: write, kind: model-step, agent: golang-engineer}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "serialbuild", ComponentRoot: "/repo", Goal: "ship it"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "serialbuild", ComponentRoot: "/repo"}
	for i, id := range []string{"T1", "T2", "T3"} {
		if _, err := s.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	// T1 is already finished: it is not queued again.
	if _, err := s.SetTaskStatus(sc, "T1", store.StatusSkipped); err != nil {
		t.Fatal(err)
	}
	var dispatched []string
	dep := Deps{Store: s, Model: func(_ context.Context, st LoopState, _ blueprint.Step, _, _ string) (ModelResult, error) {
		dispatched = append(dispatched, st.Position.Item)
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if _, err := AdvanceLoop(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 {
		t.Fatalf("the build lane was skipped or ran %d steps; want one step on the first open task", len(dispatched))
	}
	if dispatched[0] != "T2" {
		t.Fatalf("the step ran on %q; T1 is finished, so the first open task is T2", dispatched[0])
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tasks) == 0 {
		t.Fatalf("the task list was not kept in the loop's state")
	}
}
