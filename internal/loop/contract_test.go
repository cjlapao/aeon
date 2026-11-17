package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T12 — the record contract at run time (OL5/OL6).

// fakeRecords answers the two seam calls from a table the test sets.
type fakeRecords struct {
	counts     map[string]int   // kind → count
	countErr   map[string]error // kind → error (unreadable)
	attached   []string
	attachErr  error
	countCalls []string
}

func (f *fakeRecords) CountByStep(kind string, stepID int64) (int, error) {
	f.countCalls = append(f.countCalls, fmt.Sprintf("%s/%d", kind, stepID))
	if err, bad := f.countErr[kind]; bad {
		return 0, err
	}
	return f.counts[kind], nil
}

func (f *fakeRecords) AttachedByStep(stepID int64) ([]string, error) {
	return f.attached, f.attachErr
}

func emitStep(produces string, emits ...blueprint.Emit) blueprint.Step {
	return blueprint.Step{ID: "draft", Kind: blueprint.ModelStep, Agent: "default", Produces: blueprint.Paths{produces}, Emits: emits}
}

func TestMissingRecordNamesTheToolToCall(t *testing.T) {
	dep := Deps{Records: &fakeRecords{counts: map[string]int{"goal": 0}}}
	step := emitStep("DRAFT.md", blueprint.Emit{Type: "goal"})
	why := missingRecord(dep, step, 7)
	if !strings.Contains(why, "goal_create") {
		t.Fatalf("the refusal must name the call to make: %q", why)
	}
	if !strings.Contains(why, "must create 1 goal record(s), found 0") {
		t.Fatalf("the refusal must state the gap: %q", why)
	}
	// The min is honoured (2 declared, 1 found).
	dep = Deps{Records: &fakeRecords{counts: map[string]int{"goal": 1}}}
	step = emitStep("DRAFT.md", blueprint.Emit{Type: "goal", Min: 2})
	if why := missingRecord(dep, step, 7); !strings.Contains(why, "must create 2 goal record(s), found 1") {
		t.Fatalf("min ignored: %q", why)
	}
	// An unreadable count is refused AS unreadable, never as absent.
	dep = Deps{Records: &fakeRecords{countErr: map[string]error{"goal": fmt.Errorf("disk")}}}
	step = emitStep("DRAFT.md", blueprint.Emit{Type: "goal"})
	if why := missingRecord(dep, step, 7); !strings.Contains(why, "could not be read") {
		t.Fatalf("unreadable read as absent: %q", why)
	}
}

func TestMissingAttachmentIsNamed(t *testing.T) {
	dep := Deps{Records: &fakeRecords{counts: map[string]int{"goal": 1}, attached: nil}}
	step := emitStep("DRAFT.md", blueprint.Emit{Type: "goal", Attach: []string{"brief.md"}})
	why := missingRecord(dep, step, 7)
	if !strings.Contains(why, "brief.md") || !strings.Contains(why, "attach") {
		t.Fatalf("the refusal must name the document and the tool: %q", why)
	}
	if strings.Contains(why, "goal_create") {
		t.Fatalf("a satisfied count must not be in the gap list: %q", why)
	}
}

func TestSatisfiedContractPasses(t *testing.T) {
	dep := Deps{Records: &fakeRecords{counts: map[string]int{"goal": 2}, attached: []string{"brief.md", "plan.md"}}}
	step := emitStep("DRAFT.md", blueprint.Emit{Type: "goal", Min: 2, Attach: []string{"brief.md", "plan.md"}})
	if why := missingRecord(dep, step, 7); why != "" {
		t.Fatalf("a satisfied contract said %q", why)
	}
}

// advanceContractLoop starts a one-lane loop whose step writes its file
// and (by default) reports ok.
func advanceContractLoop(t *testing.T, raw string, dep Deps) (LoopState, error) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "contract.yaml")
	writeBlueprint(t, path, raw)
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "contract", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep.Store = s
	if dep.Event == nil {
		dep.Event = func(string, any) {}
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		return LoopState{}, err
	}
	return Pinned(context.Background(), s, st.ID)
}

// The witness handler is the REAL proposal-author (granted goal_create and
// attach in its body) — the load-time gate of T11 is what forces honesty
// here: `default` would be refused before the runtime ever saw the lane.
const contractLane = "  - id: draft\n    step: {kind: model-step, agent: proposal-author, produces: [DRAFT.md, brief.md], emits: [{type: goal, attach: [brief.md]}]}\n"

func TestFilesAloneDoNotSatisfyTheContract(t *testing.T) {
	dep := Deps{
		Model: func(_ context.Context, state LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
			// The model does the FILES and only the files — written into
			// the LOOP's own root (state carries it; capturing a local root
			// here would test the artifact door, not this one).
			for _, p := range step.Produces {
				if err := os.WriteFile(filepath.Join(state.Root, p), []byte("x\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			return ModelResult{StepID: 9, Kind: "completed", Outcome: "ok"}, nil
		},
		Records: &fakeRecords{counts: map[string]int{"goal": 0}},
	}
	saved, err := advanceContractLoop(t, contractLane, dep)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, "declared record was not created") {
		t.Fatalf("files alone passed the contract: %+v", saved)
	}
	if saved.StepStates["draft"].Outcome != "reject" {
		t.Fatalf("step outcome %q — rejection must route like a missing artifact", saved.StepStates["draft"].Outcome)
	}
}

func TestNoRecordsReaderBlocks(t *testing.T) {
	dep := Deps{Model: func(_ context.Context, state LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		// Files written — the ONLY gap left is the records themselves.
		for _, p := range step.Produces {
			if err := os.WriteFile(filepath.Join(state.Root, p), []byte("x\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return ModelResult{StepID: 3, Kind: "completed", Outcome: "ok"}, nil
	}}
	saved, err := advanceContractLoop(t, contractLane, dep)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, contractUnverifiableKind) {
		t.Fatalf("an unchecked contract passed: %+v", saved)
	}
}

func TestPromptContainsTheContractBlock(t *testing.T) {
	step := emitStep("DRAFT.md",
		blueprint.Emit{Type: "goal", Attach: []string{"brief.md"}},
		blueprint.Emit{Type: "task", Min: 2})
	got := contractPrompt(step.Emits)
	want := "\nThis step is not complete until these records exist (they are separate from the files): " +
		"call goal_create at least 1 goal record(s) and attach brief.md to the goal with `attach`; " +
		"call task_create at least 2 task record(s)."
	if got != want {
		t.Fatalf("golden prompt drift:\n got %q\nwant %q", got, want)
	}
	// And it rides the real modelPrompt (after the produces sentence).
	full := TaskFrame(LoopState{Goal: "g", RuntimeState: RuntimeState{Position: Position{Lane: "draft"}}}, step)
	if !strings.Contains(full, want) || !strings.Contains(full, "File the declared artifact(s) with `doc_write`") {
		t.Fatalf("modelPrompt lost the contract block: %q", full)
	}
	if contractPrompt(nil) != "" {
		t.Fatal("a step without emits grew a paragraph")
	}
}

// TestStepsWithoutEmitsAreByteIdentical proves the DoD for every shipped
// blueprint: none declares emits today, so the prompt grows nothing and an
// unwired reader changes no outcome.
func TestStepsWithoutEmitsAreByteIdentical(t *testing.T) {
	for _, name := range blueprint.StandardNames() {
		raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, l := range bp.Lanes {
			steps := l.Ladder
			if l.Step != nil {
				steps = append([]blueprint.Step{*l.Step}, steps...)
			}
			for _, s := range steps {
				// S38 T14: the pair DECLARES the contract — that is the
				// point of them. The UNCHANGED claim now belongs to the
				// eight that predate the keys; the contract half of this
				// test (prompt bytes, events) still walks every file.
				if name != "brainstorm" && name != "goal-to-brief" && (len(s.Emits) != 0 || s.Decides != "") {
					t.Fatalf("%s lane %s step %s already declares the new keys — this test owns the UNCHANGED claim", name, l.ID, s.ID)
				}
				if len(s.Emits) == 0 && contractPrompt(s.Emits) != "" {
					t.Fatalf("%s grew a contract paragraph without emitting anything", name)
				}
				if len(s.Emits) > 0 && !strings.Contains(contractPrompt(s.Emits), "not complete until") {
					t.Fatalf("%s lane %s emits but the prompt does not say so", name, l.ID)
				}
			}
		}
	}
	// The behavioural twin: two-step runs exactly as before, reader unwired.
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "two-step.yaml")
	raw := "id: two-step\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default, produces: SURVEY.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "unchanged", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) }, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, frame string) (ModelResult, error) {
		if strings.Contains(prompt+frame, "not complete until") {
			t.Fatal("a step without emits was lectured about records")
		}
		if err := os.WriteFile(filepath.Join(root, step.Produces[0]), []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return ModelResult{StepID: 1, Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "loop.advanced,loop.finished" {
		t.Fatalf("events moved for a blueprint that declares nothing: %v", events)
	}
}

func TestResumedStepAppliesTheSameCheck(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "resume-contract.yaml")
	writeBlueprint(t, path, contractLane)
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "resume-contract", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Event: func(string, any) {}, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
		return ModelResult{StepID: 42, Kind: "suspended", QuestionID: 7}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"DRAFT.md", "brief.md"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The resumed ok arrives with no records filed.
	dep.Records = &fakeRecords{counts: map[string]int{"goal": 0}}
	if err := CompleteSuspendedStep(context.Background(), dep, st.ID, ModelResult{StepID: 42, Kind: "completed", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.LastReason, "declared record was not created") || saved.StepStates["draft"].Outcome != "reject" {
		t.Fatalf("the resume door forgot the contract: %+v", saved)
	}
	// And the resume door blocks too when the reader vanished mid-suspension.
	// (A fresh loop — a rejected step stays parked at the same position, and
	// one suspension belongs to one step.)
	st2, err := Start(context.Background(), s, store.NewLoop{Slug: "resume-contract-2", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st2.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DRAFT.md"), []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dep.Records = nil
	if err := CompleteSuspendedStep(context.Background(), dep, st2.ID, ModelResult{StepID: 42, Kind: "completed", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	saved2, err := Pinned(context.Background(), s, st2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved2.Status != "blocked" || !strings.Contains(saved2.LastReason, contractUnverifiableKind) {
		t.Fatalf("the resume door passed an unchecked contract: %+v", saved2)
	}
}
