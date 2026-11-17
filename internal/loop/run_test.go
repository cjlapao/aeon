package loop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// run_test.go walks S39 T8 (CR2/CR6/CR7): an idle loop runs a SECOND
// blueprint, run 1 stays immutable history, every step knows its run, and the
// first message reaches the thread with its provenance — while the loop's
// attended declaration stands untouched (CR7).

const yamlA = "id: A\nschemaVersion: 1\nversion: 1\nafter: converse\nconverse: chat\noutput: [A.md]\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default, produces: A.md}\nquestions: {}\n"
const yamlB = "id: B\nschemaVersion: 1\nversion: 1\nafter: converse\nconverse: chat\noutput: [B.md]\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default, produces: B.md}\nquestions: {}\n"

func writeRunBlueprints(t *testing.T, root string) (pathA, pathB string) {
	t.Helper()
	dir := t.TempDir()
	pathA = filepath.Join(dir, "A.yaml")
	pathB = filepath.Join(dir, "B.yaml")
	if err := os.WriteFile(pathA, []byte(yamlA), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(yamlB), 0600); err != nil {
		t.Fatal(err)
	}
	return pathA, pathB
}

// runModel is the fake the advance tests use: it writes whatever the step
// declares, files the step row the identity stamps from, and — for the run
// tag — attaches the step to its run through the real MarkStepStarted.
func runModel(t *testing.T, s *store.Store, root string, events *[]string) func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
	return func(_ context.Context, st LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		name := step.Produces[0]
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		row, err := s.CreateStep(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, store.NewStep{Agent: step.Agent, Status: store.StepRunning})
		if err != nil {
			t.Fatal(err)
		}
		if err := MarkStepStarted(context.Background(), Deps{Store: s}, st.ID, st.Position, row.ID); err != nil {
			t.Fatal(err)
		}
		return ModelResult{StepID: row.ID, Kind: "completed", Outcome: "ok"}, nil
	}
}

func startRunLoop(t *testing.T, s *store.Store, root, pathA string, attended *bool, route string) LoopState {
	t.Helper()
	var rt []string
	if route != "" {
		rt = []string{route}
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "seq", ComponentRoot: root, Root: root, Goal: "two runs", Mode: "standard"}, pathA, attended, nil, rt...)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStartRunPinsTheSecondBlueprintAndLeavesRunOneIntact(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	dep := Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusIdle {
		t.Fatalf("run 1 did not rest: %s", done.Status)
	}
	got, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user", StartedByID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.RunSeq != 2 {
		t.Fatalf("run_seq=%d, want the second run", got.Run.RunSeq)
	}
	live, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != "running" || live.Blueprint.ID != "B" || live.Position.Lane != "only" {
		t.Fatalf("the loop did not start run 2 on B: %s %s %+v", live.Status, live.Blueprint.ID, live.Position)
	}
	if live.Position.Item != "" || len(live.Tasks) != 0 || live.LastStepID != 0 {
		t.Fatalf("run 2 wears run 1's history: %+v", live)
	}
	runs, err := s.ListRuns(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRuns = %d rows, want 2", len(runs))
	}
	r1, err := s.ReadRun(st.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != yamlA {
		t.Fatalf("run 1's pinned copy moved (history is immutable): %q", r1.Content)
	}
	if r1.State != "done" {
		t.Fatalf("run 1 state %q, want done", r1.State)
	}
	if runs[0].RunSeq != 1 || runs[1].RunSeq != 2 {
		t.Fatalf("run numbers drifted: %d, %d", runs[0].RunSeq, runs[1].RunSeq)
	}
}

func TestSecondRunWalksAndRestsLikeTheFirst(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	var events []string
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) }, Model: runModel(t, s, root, &events)}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	done, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusIdle {
		t.Fatalf("run 2 did not rest: %s", done.Status)
	}
	n := 0
	for _, k := range events {
		if k == "loop.run.finished" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("loop.run.finished fired %d times, want one per run (events %v)", n, events)
	}
	r1, _ := s.ReadRun(st.ID, 1)
	r2, _ := s.ReadRun(st.ID, 2)
	if r1.State != "done" || r2.State != "done" {
		t.Fatalf("runs %s/%s, want done/done", r1.State, r2.State)
	}
}

func TestStepsAreTaggedWithTheirRun(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	dep := Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	// ReadStep carries the tag (S39 CR2 exposure): the two steps, oldest
	// first, must say run 1 and run 2 — 0 would mean MarkStepStarted did
	// not consult CurrentRun.
	steps, err := s.ListSteps(store.Scope{LoopSlug: "seq", ComponentRoot: root})
	if err != nil || len(steps) != 2 {
		t.Fatalf("steps %d (err %v), want 2", len(steps), err)
	}
	if steps[0].RunSeq != 1 || steps[1].RunSeq != 2 {
		t.Fatalf("step run tags %d/%d, want 1/2 — MarkStepStarted tags with the CURRENT run (CR2)", steps[0].RunSeq, steps[1].RunSeq)
	}
}

func TestStartRunRefusesALoopThatIsNotIdle(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	before, _ := s.ListRuns(st.ID)
	_, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user"})
	if !errors.Is(err, store.ErrLoopNotIdle) {
		t.Fatalf("a running loop answered %v, want ErrLoopNotIdle", err)
	}
	after, _ := s.ListRuns(st.ID)
	if len(after) != len(before) {
		t.Fatalf("the refusal added a run row: %d -> %d", len(before), len(after))
	}
}

func TestStartRunWithAnInvalidBlueprintLeavesTheLoopIdle(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, _ := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	// Run 1 must finish first — the loop has to be RESTING for a run to be
	// refused on its blueprint, not on its status.
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("id: C\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: nobody-exists, produces: C.md}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: bad, StartedByKind: "user"})
	var ref PinRefusal
	if !errors.As(err, &ref) || ref.Kind != "invalid-blueprint" {
		t.Fatalf("an invalid blueprint answered %v, want PinRefusal invalid-blueprint", err)
	}
	pin, _ := s.ReadPinnedLoop(st.ID)
	if pin.Loop.Status != StatusIdle {
		t.Fatalf("the refusal moved the loop: %s — order IS the contract: pin before claim (claim BEFORE write)", pin.Loop.Status)
	}
	runs, _ := s.ListRuns(st.ID)
	if len(runs) != 1 {
		t.Fatalf("a refused start wrote run rows: %d", len(runs))
	}
	rows, err := s.Thread(store.Scope{LoopSlug: "seq", ComponentRoot: root}, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a refused start wrote %d thread rows", len(rows))
	}
}

func TestStartRunWritesTheTypedFirstMessage(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	got, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user", Typed: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Thread(store.Scope{LoopSlug: "seq", ComponentRoot: root}, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("thread %d rows (err %v), want the one kickoff", len(rows), err)
	}
	last := rows[0]
	if last.Author != "human" || last.Source != "user" || last.Body != "do it" {
		t.Fatalf("the kickoff row: %s/%s/%q", last.Author, last.Source, last.Body)
	}
	if got.Run.KickoffMessageID == nil || *got.Run.KickoffMessageID != last.ID {
		t.Fatalf("the run does not link its message: %v vs %d", got.Run.KickoffMessageID, last.ID)
	}
	live, _ := Pinned(context.Background(), s, st.ID)
	if live.Kickoff != "do it" {
		t.Fatalf("the saved state lost the kickoff: %q", live.Kickoff)
	}
}

func TestStartRunRendersTheTemplateAsTheFirstMessage(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, _ := writeRunBlueprints(t, root)
	dir := t.TempDir()
	pathB := filepath.Join(dir, "B.yaml")
	if err := os.WriteFile(pathB, []byte(yamlB[:len(yamlB)-len("questions: {}\n")]+
		"kickoff: Work on {{.Goal}}\nquestions: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	st := startRunLoop(t, s, root, pathA, nil, "")
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user", Data: blueprint.KickoffData{Goal: "X"}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Thread(store.Scope{LoopSlug: "seq", ComponentRoot: root}, 0)
	if len(rows) != 1 || rows[0].Body != "Work on X" || rows[0].Source != "template" || rows[0].Author != "human" {
		t.Fatalf("templated kickoff: %+v", rows)
	}
}

func TestStartRunWithNothingToSayWritesNoMessage(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	// The goal body WOULD be the default; "nothing to say" needs empty Data
	// — the request's Data is what the default reads, not the loop's goal.
	got, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user"})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Thread(store.Scope{LoopSlug: "seq", ComponentRoot: root}, 0)
	if len(rows) != 0 {
		t.Fatalf("nothing to say wrote %d rows: %+v", len(rows), rows)
	}
	if got.Kickoff.ID != 0 || got.Run.KickoffMessageID != nil {
		t.Fatalf("the zero kickoff rode the run row: %+v", got)
	}
}

func TestStartRunKeepsTheLoopsAttendedDeclaration(t *testing.T) {
	yes, no := true, false
	for _, attended := range []*bool{&yes, &no, nil} {
		s := openLoopStore(t)
		root := t.TempDir()
		pathA, pathB := writeRunBlueprints(t, root)
		st := startRunLoop(t, s, root, pathA, attended, "")
		if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "trigger", StartedByID: "cron"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoopAttended(context.Background(), st.ID)
		if err != nil {
			t.Fatal(err)
		}
		// Three states, none collapsed (P31): nil stays nil, each value stands.
		if (attended == nil) != (got == nil) {
			t.Fatalf("CR7: attendedness moved (nil=%v -> %v)", attended == nil, got == nil)
		}
		if attended != nil && *got != *attended {
			t.Fatalf("CR7: attended %v -> %v — StartRun never touches the declaration", *attended, *got)
		}
	}
}

func TestStartRunInheritsTheRouteAndCarriesWith(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "r1")
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "user", With: map[string]string{"tech": "go"}}); err != nil {
		t.Fatal(err)
	}
	live, _ := Pinned(context.Background(), s, st.ID)
	if live.Route != "r1" {
		t.Fatalf("route %q, want the loop's own inherited", live.Route)
	}
	if live.With["tech"] != "go" {
		t.Fatalf("operator with not carried: %v", live.With)
	}
	// Run 2 ends, run 3 overrides.
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathA, StartedByKind: "user", Route: "r2"}); err != nil {
		t.Fatal(err)
	}
	live, _ = Pinned(context.Background(), s, st.ID)
	if live.Route != "r2" {
		t.Fatalf("asked-for route lost: %q", live.Route)
	}
}

func TestStartRunPinsTheGoalVersion(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	if err := Run(context.Background(), Deps{Store: s, Event: func(string, any) {}, Model: runModel(t, s, root, nil)}, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRun(context.Background(), s, RunRequest{LoopID: st.ID, Blueprint: pathB, StartedByKind: "agent", StartedByID: "planner", GoalID: "g1", GoalVersion: 3, TaskID: "t7"}); err != nil {
		t.Fatal(err)
	}
	r2, err := s.ReadRun(st.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r2.GoalID != "g1" || r2.GoalVersion != 3 || r2.TaskID != "t7" {
		t.Fatalf("run coordinates: %+v", r2)
	}
}

func TestPreviewBlueprintDoesNotPin(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	pathA, pathB := writeRunBlueprints(t, root)
	st := startRunLoop(t, s, root, pathA, nil, "")
	before, _ := s.ListRuns(st.ID)
	bp, err := PreviewBlueprint(s, pathB, "standard")
	if err != nil {
		t.Fatal(err)
	}
	if bp.ID != "B" {
		t.Fatalf("previewed %q", bp.ID)
	}
	after, _ := s.ListRuns(st.ID)
	if len(after) != len(before) {
		t.Fatalf("a PREVIEW wrote run rows: %d -> %d", len(before), len(after))
	}
	pin, _ := s.ReadPinnedLoop(st.ID)
	if pin.BlueprintID != "A" {
		t.Fatalf("a preview re-pinned the loop: %s", pin.BlueprintID)
	}
}
