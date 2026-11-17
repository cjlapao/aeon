package loop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T5 — phases: what a lane RECORDS. S8 has lanes (the declaration);
// S4's loop_phase rows are the state (L27). Two names for what looks like
// one thing — and if they were one thing, one would be redundant and
// become a second source of truth about progress. The engine brackets
// each tracked lane: in-progress before dispatch, a terminal status after.
// v1 made that ritual in every one of the 26 agent bodies; S13 deleted it
// there — correctly, because the engine should do it. This task is that
// "or the behaviour was lost rather than moved".

// phaseBlueprint: tracked names only, so every assertion is about the
// tracker, not about a lane the tracker does not know. build ITERATES —
// its progress is the tasks' rows, never a single status (L27).
func phaseBlueprint(t *testing.T, clarifyReject bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "phases.yaml")
	clarifyOutcome := "ok"
	if clarifyReject {
		clarifyOutcome = "reject"
	}
	lanes := "  - id: clarify\n    step: {kind: model-step, agent: default}\n" +
		"  - id: legal\n    when: {field: flags.needs_legal, op: eq, value: true}\n    step: {kind: model-step, agent: default}\n" +
		"  - id: memory\n    when: {field: mode, op: eq, value: poc}\n    step: {kind: model-step, agent: default}\n" +
		"  - id: plan\n    step: {kind: model-step, agent: default}\n" +
		"  - id: build\n    iterate: tasks\n    rework: implement\n    on_reject: { counter: retries, max: 2, then: fail-item }\n    ladder:\n      - {id: write, kind: model-step, agent: default}\n      - {id: implement, kind: model-step, agent: default}\n      - {id: verify, kind: model-step, agent: default}\n" +
		"  - id: qa\n    step: {kind: model-step, agent: default}\n"
	writeBlueprint(t, path, lanes)
	if clarifyReject {
		// The spy rejects clarify; see the model func.
		_ = clarifyOutcome
	}
	return path, root
}

func startPhaseLoop(t *testing.T, s *store.Store, path, root, slug string, tasks ...string) string {
	t.Helper()
	if len(tasks) == 0 {
		tasks = []string{"T1"}
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "phases", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = tasks
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

func phaseRows(t *testing.T, s *store.Store, root, slug string) map[string]string {
	t.Helper()
	rows, err := s.Phases(store.Scope{LoopSlug: slug, ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range rows {
		out[p.Phase] = p.Status
	}
	return out
}

// TestQ2Recorded — Q2 answered FROM THE CODE: TRACKED_PHASES is the
// durable `## Phases` ladder, a deliberate SUPERSET of the phase-pointer
// enum (agents-sync and feature-merge are tracked but are not pointer
// values), and an ITERATING lane appears in it as a NAME but never gets a
// single status row — its progress is the tasks' rows (L27). Pinning the
// set turns "a lane added later" into a deliberate change.
func TestQ2Recorded(t *testing.T) {
	want := []string{"clarify", "discovery", "legal", "plan", "build", "qa", "findings-gate", "memory", "agents-sync", "feature-merge"}
	got := store.TrackedPhases()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("TrackedPhases = %v, want exactly %v — this is Q2's recorded answer", got, want)
	}
	vocab := strings.Join(store.PhaseStatuses(), ",")
	for _, status := range []string{"pending", "in-progress", "done", "skipped", "failed", "not-needed"} {
		if !strings.Contains(vocab, status) {
			t.Fatalf("PHASE_STATUS vocabulary lacks %q: %v", status, store.PhaseStatuses())
		}
	}
}

// TestEveryTrackedLaneRecordsAStatus — every tracked, non-iterating lane
// that runs gets a row: in-progress before dispatch, terminal after.
func TestEveryTrackedLaneRecordsAStatus(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, false)
	id := startPhaseLoop(t, s, path, root, "phases-all")
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	rows := phaseRows(t, s, root, "phases-all")
	for _, lane := range []string{"clarify", "plan", "qa"} {
		if rows[lane] != "done" {
			t.Fatalf("tracked lane %s recorded %q, want done (bracketed by the engine)", lane, rows[lane])
		}
	}
}

// TestIteratingLaneHasNoSinglePhaseStatus — L27/D11: a single `build`
// status cannot say which of five tasks is in review; a build row would
// be a THIRD description of the same thing, drifting from both the tasks
// and the step states. The iterating lane records NO single phase row.
func TestIteratingLaneHasNoSinglePhaseStatus(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, false)
	id := startPhaseLoop(t, s, path, root, "phases-build", "T1", "T2")
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	rows := phaseRows(t, s, root, "phases-build")
	if status, present := rows["build"]; present {
		t.Fatalf("the iterating lane recorded a single phase status %q — its progress is the tasks' rows (L27)", status)
	}
	// The per-item state IS recorded — as task rows, the one home it has.
	tasks, err := s.ListTasks(store.Scope{LoopSlug: "phases-build", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("%d task rows, want the two items the lane iterates", len(tasks))
	}
}

// TestNotNeededAndSkippedAreDistinct — both persisted, distinguishably,
// and NEITHER reads as pending: a bypassed lane rendered as pending reads
// "still to come" for something that will never happen (O8, recorded here
// rather than only rendered).
func TestNotNeededAndSkippedAreDistinct(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, false)
	id := startPhaseLoop(t, s, path, root, "phases-skip")
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	rows := phaseRows(t, s, root, "phases-skip")
	// legal's when names a false flag: not-needed. memory's when names a
	// mode the loop does not run: skipped. The distinction is decide's
	// (skipName); this pins that it REACHES the row.
	if rows["legal"] != "not-needed" {
		t.Fatalf("when-false lane recorded %q, want not-needed", rows["legal"])
	}
	if rows["memory"] != "skipped" {
		t.Fatalf("mode-skip lane recorded %q, want skipped", rows["memory"])
	}
	for lane, status := range rows {
		if status == "pending" {
			t.Fatalf("lane %s persisted as pending — a bypassed lane must not read as still-to-come", lane)
		}
	}
	if rows["legal"] == rows["memory"] {
		t.Fatal("the two skips collapsed into one status — the distinction decide produces was lost on the way to the row")
	}
}

// TestBracketingHappensInTheEngine — the model spy is a HANDLER's seat:
// it writes no phase row, and yet when clarify's handler is dispatched,
// the row already says in-progress. S13 deleted "loop_setPhaseStatus
// before dispatch" from all 26 bodies as ritual — correctly, because the
// ENGINE should do it. If this fails, the behaviour was deleted, not
// moved.
func TestBracketingHappensInTheEngine(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, false)
	id := startPhaseLoop(t, s, path, root, "phases-bracket")
	sawInProgress := false
	dep := Deps{Store: s, Model: func(_ context.Context, st LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		// Keyed on the POSITION's lane, not step.ID: a direct-step lane's
		// step carries no id of its own (measured: the id is ""), so a
		// spy keyed on the step name would silently never fire — and a
		// spy that never fires asserts nothing.
		if st.Position.Lane == "clarify" {
			// Mid-dispatch, from the handler's seat: the engine bracketed
			// BEFORE this call, not after it.
			rows := phaseRows(t, s, root, "phases-bracket")
			sawInProgress = rows["clarify"] == "in-progress"
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if !sawInProgress {
		t.Fatal("the handler was dispatched with no in-progress row — S13's deleted ritual was never picked up by the engine")
	}
}

// TestPhaseStatusMatchesLoopShow — one source, two readers: the row and
// the runtime lane state must not disagree. loop show renders from the
// runtime; the tracker holds the durable answer; where both speak, they
// must say the SAME thing (modulo the one documented alias: a completed
// lane is ok in the runtime map and done on the row).
func TestPhaseStatusMatchesLoopShow(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, false)
	id := startPhaseLoop(t, s, path, root, "phases-match")
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	st, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	for lane, rowStatus := range phaseRows(t, s, root, "phases-match") {
		runtime := st.LaneStates[lane]
		agree := runtime == rowStatus
		if rowStatus == "done" && runtime == "ok" {
			agree = true // the one documented alias, said once here
		}
		if !agree {
			t.Fatalf("lane %s: tracker says %q, loop show renders %q — two readers of two sources", lane, rowStatus, runtime)
		}
	}
}

// TestFailedPhaseIsTerminal — a failed lane's status is terminal, not
// left in-progress: a tracker frozen at in-progress forever is the
// exact lie bracketing exists to prevent, one step later.
func TestFailedPhaseIsTerminal(t *testing.T) {
	s := openLoopStore(t)
	path, root := phaseBlueprint(t, true)
	id := startPhaseLoop(t, s, path, root, "phases-fail")
	dep := Deps{Store: s, Model: func(_ context.Context, st LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		// Lane-keyed for the same reason as the bracket spy: a direct
		// step's ID is "", and a reject that never fires asserts nothing.
		if st.Position.Lane == "clarify" {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "the spec is unclear"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	rows := phaseRows(t, s, root, "phases-fail")
	if rows["clarify"] != "failed" {
		t.Fatalf("clarify recorded %q after an unresolvable reject — in-progress left standing is the lie bracketing exists to prevent", rows["clarify"])
	}
	if _, later := rows["plan"]; later {
		t.Fatal("plan ran after the blocked lane — the loop kept advancing")
	}
}
