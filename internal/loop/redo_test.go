package loop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T3 — the redo carries its reason (L32, D4, O3). A send-back that
// re-runs without knowing why it was sent back reproduces the same output
// and spends a round to fail the same way; the reason travels with the redo,
// and the attempt number travels with it so the handler can tell "first
// try" from "last try" (L34: the handler does not count its own attempts).
//
// The ladder used here is three deep — write, implement, verify — so
// "rework: implement" cannot be confused with "continue the ladder": the
// step after a verify rejection is neither `write` (top of the lane) nor the
// end of the lane.

func redoBlueprint(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "redo.yaml")
	writeBlueprint(t, path, "  - id: build\n    iterate: tasks\n    rework: implement\n    on_reject: { counter: retries, max: 2, then: escalate }\n    ladder:\n      - {id: write, kind: model-step, agent: default}\n      - {id: implement, kind: model-step, agent: default}\n      - {id: verify, kind: model-step, agent: default}\n")
	return path, root
}

func startRedoLoop(t *testing.T, s *store.Store, path, root, slug string, tasks []string) string {
	t.Helper()
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "the --format flag", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = tasks
	// The per-task budget rows (FINDING T4: loop.Start does not author
	// them — internal/agentsvc does; the harness seeds what the engine's
	// bump needs, as production would have it).
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	for i, id := range tasks {
		if _, err := s.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: "task " + id}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	return st.ID
}

// TestRedoCarriesTheReason — the second implement dispatch reads the
// rejection's text IN ITS OWN INPUT (D4, C4). The spy is the handler's seat:
// it asserts on what it was handed, not on what the engine logged.
func TestRedoCarriesTheReason(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-reason", []string{"T1"})

	const defect = "json output omits 'generated_at'"
	var prompts []string
	var seen []string
	verifies := 0
	dep := Deps{Store: s, Model: func(_ context.Context, state LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		seen = append(seen, step.ID)
		prompts = append(prompts, prompt)
		if step.ID == "verify" {
			verifies++
			if verifies == 1 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: defect}, nil
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	// The second implement is dispatch #4: write, implement, verify,
	// implement.
	if len(prompts) < 4 || seen[3] != "implement" {
		t.Fatalf("the rework dispatch never happened: %v", seen)
	}
	if !strings.Contains(prompts[3], defect) {
		t.Fatalf("the re-entered handler cannot see why it was sent back — L32's bare retry:\n%s", prompts[3])
	}
	// The first implement's prompt carries NO rejection text (the reason
	// must not leak backwards either).
	if strings.Contains(prompts[1], defect) {
		t.Fatalf("the first dispatch saw a rejection that had not happened yet:\n%s", prompts[1])
	}
}

// TestRedoCarriesTheAttemptNumber — the second redo says "retry 2 of 2".
// The handler sizes its effort with it (L34), and §4's exhausted sentence
// reads the same numbers.
func TestRedoCarriesTheAttemptNumber(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-attempt", []string{"T1"})

	const defect = "json output omits 'generated_at'"
	var prompts []string
	var seen []string
	verifies := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		seen = append(seen, step.ID)
		prompts = append(prompts, prompt)
		if step.ID == "verify" {
			verifies++
			if verifies <= 2 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: defect}, nil
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	// The third verify rejects with no budget left: the engine stops at the
	// cap (the then: escalation is T4's), so Run ends blocked — that is the
	// expected walk for this counter.
	_ = Run(context.Background(), dep, id)

	implements := 0
	for i, name := range seen {
		if name != "implement" {
			continue
		}
		implements++
		if implements == 1 && strings.Contains(prompts[i], "retry") {
			t.Fatalf("the first dispatch announced a retry (L34: the handler does not count attempts; neither does the first run):\n%s", prompts[i])
		}
		if implements == 2 && !strings.Contains(prompts[i], "retry 1 of 2") {
			t.Fatalf("first redo must say 'retry 1 of 2':\n%s", prompts[i])
		}
		if implements == 3 && !strings.Contains(prompts[i], "retry 2 of 2") {
			t.Fatalf("second redo must say 'retry 2 of 2' — the handler sizes the last try differently:\n%s", prompts[i])
		}
	}
	if implements != 3 {
		t.Fatalf("%d implement dispatches, want the original plus two redos: %v", implements, seen)
	}
}

// TestRejectRoutesToRework — a send-back re-enters the step that can fix
// it, not the top of the lane (O3). With write before implement in the
// ladder, landing on `write` again is the visible failure this pins.
func TestRejectRoutesToRework(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-route", []string{"T1"})

	var seen []string
	verifies := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		seen = append(seen, step.ID)
		if step.ID == "verify" {
			verifies++
			if verifies == 1 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: "missing field"}, nil
			}
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	want := "write,implement,verify,implement,verify"
	if got := strings.Join(seen, ","); got != want {
		t.Fatalf("dispatch order = %s, want %s — after a reject the next step is the REWORK step, and verify re-checks the fix", got, want)
	}
}

// TestBareRetryIsNotPossible — a reject with no reason cannot route: the
// loop blocks naming the absence, and NO dispatch happens without a reason
// (L32: a bare retry is the failure mode, not a permitted shape).
func TestBareRetryIsNotPossible(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-bare", []string{"T1"})

	var seen []string
	verifies := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		seen = append(seen, step.ID)
		if step.ID == "verify" {
			verifies++
			if verifies == 1 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: ""}, nil // no reason — the forbidden shape
			}
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status == "done" {
		t.Fatal("a reject with no reason completed the loop — a bare retry happened")
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, "reason") {
		t.Fatalf("expected blocked naming the missing reason, got %s / %q", saved.Status, saved.LastReason)
	}
	// Implement ran exactly once (the original): no reasonless rework was
	// dispatched.
	implements := 0
	for _, name := range seen {
		if name == "implement" {
			implements++
		}
	}
	if implements != 1 {
		t.Fatalf("%d implement dispatches after a reasonless reject — the bare retry ran anyway: %v", implements, seen)
	}
}

// TestRedoIsPresentedOnce — the pending redo is the re-entry's input, once.
// A later dispatch of the same step (a SECOND genuine rework) carries the
// second rejection's text, not a stale copy of the first — the reason
// travels with its redo, it does not live on the step forever.
func TestRedoIsPresentedOnce(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-once", []string{"T1"})

	var prompts []string
	var seen []string
	verifies := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		seen = append(seen, step.ID)
		prompts = append(prompts, prompt)
		if step.ID == "verify" {
			verifies++
			if verifies == 1 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: "first defect"}, nil
			}
			if verifies == 2 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: "second defect"}, nil
			}
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	_ = Run(context.Background(), dep, id)

	var firstRedo, secondRedo string
	// Capture by CONTENT, not by position: the original implement dispatch
	// is also a non-first index, and a capture that counts it is an
	// off-by-one wearing an assertion.
	for i, name := range seen {
		if name == "implement" && strings.Contains(prompts[i], "defect") {
			if firstRedo == "" {
				firstRedo = prompts[i]
			} else {
				secondRedo = prompts[i]
			}
		}
	}
	if !strings.Contains(firstRedo, "first defect") || strings.Contains(firstRedo, "second defect") {
		t.Fatalf("first redo wrong (seen=%v):\n%s", seen, firstRedo)
	}
	if !strings.Contains(secondRedo, "second defect") {
		t.Fatalf("second redo did not carry the NEW rejection (seen=%v):\n%s", seen, secondRedo)
	}
	// The first reason is gone from the second redo's context: reasons do
	// not accumulate into a transcript the handler has to archaeology
	// through. (The first dispatch's row still holds it — history lives in
	// step rows, the input carries only what sent THIS dispatch back.)
	if strings.Contains(secondRedo, "first defect") {
		t.Fatalf("stale reason rode the second redo — the pending entry was not consumed:\n%s", secondRedo)
	}
}

// TestRedoSentenceIsSelfDescribing — the pure sentence, checked apart from
// the engine so a formatting drift is caught at the unit level. It says
// which step sent it back, the attempt, and the reason — a handler reading
// it cold needs nothing else (C4's one-renderer discipline, applied to
// redo text).
func TestRedoSentenceIsSelfDescribing(t *testing.T) {
	r, err := NewRedo("verify", "json output omits 'generated_at'", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := r.InputSentence()
	for _, want := range []string{"retry 2 of 2", "json output omits 'generated_at'", "verify"} {
		if !strings.Contains(s, want) {
			t.Fatalf("redo sentence %q lacks %q", s, want)
		}
	}
	if _, err := NewRedo("verify", "   ", 1, 2); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("a reasonless redo was constructed, or the refusal does not name the step: %v", err)
	}
	if _, err := NewRedo("verify", "reason", 0, 2); err == nil {
		t.Fatal("attempt 0 constructed — attempt numbers start at 1")
	}
	if _, err := NewRedo("verify", "reason", 3, 2); err == nil {
		t.Fatal("attempt past the max constructed — the engine asked for something the budget forbids")
	}
}

// TestRedoSurvivesTheColdPath — the pending redo is a ROW, not memory:
// suspension between the reject and the rework (S6) must not lose the
// reason. Here the state is written, the runner restarts cold (a fresh
// Deps against the same store — resume is from rows, never from memory),
// and the rework dispatch still carries the sentence.
func TestRedoSurvivesTheColdPath(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "redo-cold", []string{"T1"})

	// Plant the pending redo exactly as routeReject would have left it,
	// then "crash": no in-process map carries it; the next Run reads rows.
	st, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	redo, err := NewRedo("verify", "json output omits 'generated_at'", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	st.Redos = map[string]Redo{"build/T1/implement": redo}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(id, "running", raw); err != nil {
		t.Fatal(err)
	}

	var implementPrompt string
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		if step.ID == "implement" && implementPrompt == "" {
			implementPrompt = prompt
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(implementPrompt, "retry 1 of 2") || !strings.Contains(implementPrompt, "generated_at") {
		t.Fatalf("the cold path lost the redo — resume from rows dropped the reason:\n%s", implementPrompt)
	}
}
