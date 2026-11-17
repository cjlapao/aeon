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

func TestTwoStepReplayStopsAtGateAndFinishesAfterAnswer(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "two-step.yaml")
	raw := "id: two-step\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n  - id: approve\n    step: {kind: gate, question: approve-survey}\n  - id: summarise\n    step: {kind: model-step, agent: default, produces: SUMMARY.md}\nquestions:\n  approve-survey:\n    prompt: Approve?\n    answers: [approve, abort]\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "two", ComponentRoot: root, Root: root, Goal: "survey files", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	var events []string
	var gateAnswerEvent map[string]any
	dep := Deps{Store: s, Event: func(kind string, payload any) {
		events = append(events, kind)
		if fields, ok := payload.(map[string]any); ok && fields["answer"] != nil {
			gateAnswerEvent = fields
		}
	}, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		name := step.Produces[0]
		calls = append(calls, name)
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return ModelResult{StepID: int64(len(calls)), Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	waiting, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waiting" || waiting.Position.Lane != "approve" || waiting.QuestionID == 0 || len(calls) != 1 {
		t.Fatalf("waiting=%+v calls=%v", waiting, calls)
	}
	if _, err := AnswerGate(context.Background(), dep, st.ID, "wrong"); err == nil {
		t.Fatal("accepted an answer outside the closed set")
	}
	if _, err := AnswerGate(context.Background(), dep, st.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if gateAnswerEvent["questionId"] != waiting.QuestionID || gateAnswerEvent["answer"] != "approve" {
		t.Fatalf("gate answer event: %+v", gateAnswerEvent)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "done" || strings.Join(calls, ",") != "SURVEY.md,SUMMARY.md" {
		t.Fatalf("done=%+v calls=%v", done, calls)
	}
	if strings.Join(events, ",") != "loop.advanced,loop.waiting,loop.advanced,loop.advanced,loop.finished" {
		t.Fatal(events)
	}
}

func TestSuspendedStepContinuationStartsTheFollowingGate(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "suspended-gate.yaml")
	raw := "id: suspended-gate\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n  - id: approve\n    step: {kind: gate, question: approve-survey}\nquestions:\n  approve-survey:\n    prompt: Approve?\n    answers: [approve, abort]\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "resume-gate", ComponentRoot: root, Root: root, Goal: "survey files", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) }, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		return ModelResult{StepID: 42, Kind: "suspended", QuestionID: 7}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SURVEY.md"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CompleteSuspendedStep(context.Background(), dep, st.ID, ModelResult{StepID: 42, Kind: "completed", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	advanced, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Status != "running" || advanced.Position.Lane != "approve" || advanced.QuestionID != 0 {
		t.Fatalf("continued step left a non-runnable next position: status=%s position=%+v question=%d", advanced.Status, advanced.Position, advanced.QuestionID)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	gate, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Status != "waiting" || gate.Position.Lane != "approve" || gate.QuestionID == 0 || gate.QuestionID == 7 {
		t.Fatalf("next gate was not created: status=%s position=%+v question=%d", gate.Status, gate.Position, gate.QuestionID)
	}
	if strings.Join(events, ",") != "loop.waiting,loop.advanced,loop.waiting" {
		t.Fatal(events)
	}
}

func TestMissingProducedFileRejectsStep(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "missing.yaml")
	writeBlueprint(t, path, "  - id: write\n    step: {kind: model-step, agent: default, produces: RESULT.md}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "missing", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, "RESULT.md") || saved.StepStates["write"].Outcome != "reject" {
		t.Fatal(saved)
	}
}

func TestIterateRunsEveryLadderStepPerStableItem(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "iterate.yaml")
	writeBlueprint(t, path, "  - id: build\n    iterate: tasks\n    ladder:\n      - {id: inspect, kind: model-step, agent: default}\n      - {id: implement, kind: model-step, agent: default}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "iterate", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = []string{"task-b", "task-a", "task-c"}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	var seen []string
	dep := Deps{Store: s, Model: func(_ context.Context, state LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		seen = append(seen, state.Position.Item+":"+step.ID)
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen, ","); got != "task-b:inspect,task-b:implement,task-a:inspect,task-a:implement,task-c:inspect,task-c:implement" {
		t.Fatal(got)
	}
}

func TestCodeActionBlocksAtItsPositionWithoutDispatch(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "action.yaml")
	writeBlueprint(t, path, "  - id: publish\n    step: {kind: code-action, action: forge.openPR}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "action", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		called = true
		return ModelResult{}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	// T10 evolved this expectation: the unimplemented-action refusal is
	// retired (D16). What remains blocked-and-pinned-in-place is the
	// honest missing-instruments answer — the registry exists, this Deps
	// carries no git/forge to run it with. The POSITION still stays put:
	// a refused action is not skipped.
	if called || saved.Status != "blocked" || saved.Position.Lane != "publish" || !strings.Contains(saved.LastReason, "action services") {
		t.Fatal(called, saved)
	}
}

func TestRecoveredInFlightStepDoesNotCallModelAgain(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "recover.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default, produces: RESULT.md}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "recover", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "RESULT.md"), []byte("already written"), 0600); err != nil {
		t.Fatal(err)
	}
	st.LastStepID, st.StepInFlight = 17, true
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	models, recovered := 0, 0
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		models++
		return ModelResult{}, nil
	},
		RecoveredStep: func(_ context.Context, got LoopState) (ModelResult, error) {
			recovered++
			if got.LastStepID != 17 {
				t.Fatal(got.LastStepID)
			}
			return ModelResult{StepID: 17, Kind: "complete", Outcome: "ok"}, nil
		}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if models != 0 || recovered != 1 || saved.Status != "done" || saved.StepInFlight {
		t.Fatal(models, recovered, saved)
	}
}

func TestGateReviseRoutesBackAndAbortReachesTerminalHandler(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "choices.yaml")
	raw := "id: choices\nschemaVersion: 1\nversion: 1\non_abort: cleanup\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default}\n  - id: approve\n    step: {kind: gate, question: choice}\n    on_reject: {goto: survey, counter: revisions, max: 2, then: block}\n  - id: cleanup\n    when: {field: flags.aborted, op: eq, value: true}\n    step: {kind: code-action, action: audit}\nquestions:\n  choice:\n    prompt: Continue?\n    answers: [approve, revise, abort]\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "choices", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	models := 0
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		models++
		return ModelResult{Kind: "complete", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerGate(context.Background(), dep, st.ID, "revise"); err != nil {
		t.Fatal(err)
	}
	back, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != "running" || back.Position.Lane != "survey" {
		t.Fatal(back)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if models != 2 {
		t.Fatal(models)
	}
	if _, err := AnswerGate(context.Background(), dep, st.ID, "abort"); err != nil {
		t.Fatal(err)
	}
	terminal, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != "running" || terminal.Position.Lane != "cleanup" || !terminal.Flags["aborted"] {
		t.Fatal(terminal)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != "blocked" || blocked.Position.Lane != "cleanup" || models != 2 {
		t.Fatal(blocked, models)
	}
}

func TestSkippedLaneKeepsItsDistinctStatus(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "skip.yaml")
	writeBlueprint(t, path, "  - id: optional\n    when: {field: flags.needed, op: eq, value: true}\n    step: {kind: model-step, agent: default}\n  - id: always\n    step: {kind: model-step, agent: default}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "skip", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		return ModelResult{Kind: "complete", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.LaneStates["optional"] != "not-needed" || done.LaneStates["always"] != "ok" {
		t.Fatal(done.LaneStates)
	}
}
