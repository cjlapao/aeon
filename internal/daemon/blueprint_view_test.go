package daemon

import (
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

func TestLoopViewKeepsGateOptionsAfterAnEarlierStepQuestion(t *testing.T) {
	bp := &blueprint.Blueprint{ID: "two-step", Questions: map[string]blueprint.Question{
		"approve-survey": {Prompt: "Approve?", Answers: []string{"approve", "revise", "abort"}},
	}, Lanes: []blueprint.Lane{{ID: "approve", Ladder: []blueprint.Step{{ID: "approve", Kind: blueprint.Gate, Question: "approve-survey"}}}}}
	st := loop.LoopState{Status: "waiting", Blueprint: bp, RuntimeState: loop.RuntimeState{
		Position: loop.Position{Lane: "approve"}, QuestionID: 2,
	}}
	view := viewLoop(st, false)
	if view.Lanes[0].Structure != "ladder" {
		t.Fatalf("ladder gate structure lost: %+v", view.Lanes[0])
	}
	if !reflect.DeepEqual(view.GateAnswers, []string{"approve", "revise", "abort"}) {
		t.Fatalf("gate options disappeared from the durable snapshot: %v", view.GateAnswers)
	}
	if view.GatePrompt != "Approve?" {
		t.Fatalf("gate prompt disappeared from the durable snapshot: %q", view.GatePrompt)
	}
	if view.Lanes[0].Steps[0].Status != "waiting" {
		t.Fatalf("waiting gate rendered as %q", view.Lanes[0].Steps[0].Status)
	}
	st.Position.Step = "approve"
	view = viewLoop(st, false)
	if !reflect.DeepEqual(view.GateAnswers, []string{"approve", "revise", "abort"}) || view.GatePrompt != "Approve?" {
		t.Fatalf("explicit ladder position lost gate question: %+v", view)
	}
}

func TestLoopViewKeepsEveryIteratedItemState(t *testing.T) {
	bp := &blueprint.Blueprint{ID: "items", Lanes: []blueprint.Lane{{ID: "build", Iterate: "tasks", Ladder: []blueprint.Step{
		{ID: "write", Kind: blueprint.ModelStep}, {ID: "verify", Kind: blueprint.ModelStep},
	}}}}
	st := loop.LoopState{Status: "running", Blueprint: bp, RuntimeState: loop.RuntimeState{
		Position: loop.Position{Lane: "build", Item: "second", Step: "verify"}, Tasks: []string{"first", "second"},
		StepStates: map[string]loop.StepState{
			"build/first/write": {Status: "done", StepID: 1}, "build/first/verify": {Status: "done", StepID: 2},
			"build/second/write": {Status: "done", StepID: 3},
		},
	}}
	view := viewLoop(st, false)
	if view.Lanes[0].Structure != "iterate" {
		t.Fatalf("iterated lane structure lost: %+v", view.Lanes[0])
	}
	items := view.Lanes[0].Items
	if len(items) != 2 || items[0].Status != "done" || items[1].Status != "running" {
		t.Fatalf("iterated items lost their position: %+v", items)
	}
	if items[0].Steps[1].StepID != 2 || items[1].Steps[0].StepID != 3 || items[1].Steps[1].Status != "pending" {
		t.Fatalf("iterated step state was flattened or lost: %+v", items)
	}
}

func TestLoopViewMarksActiveLaneBlockedAfterACompletedGateAnswer(t *testing.T) {
	bp := &blueprint.Blueprint{ID: "two-step", Lanes: []blueprint.Lane{{ID: "survey"}, {ID: "approve", Step: &blueprint.Step{Kind: blueprint.Gate}}, {ID: "summarise"}}}
	st := loop.LoopState{Status: "blocked", StartedAt: "2026-10-19T08:00:00Z", UpdatedAt: "2026-10-19T08:01:00Z", Blueprint: bp, RuntimeState: loop.RuntimeState{
		Position: loop.Position{Lane: "approve"}, LastReason: "gate answer revise: reject has no rework target",
		LaneStates: map[string]string{"survey": "ok", "approve": "ok", "summarise": "pending"},
		StepStates: map[string]loop.StepState{"approve": {Status: "done", Outcome: "revise"}},
	}}
	v := viewLoop(st, false)
	if v.Lanes[1].Structure != "direct" {
		t.Fatalf("direct gate structure lost: %+v", v.Lanes[1])
	}
	if v.Lanes[1].Status != "blocked" || v.Lanes[1].Steps[0].Status != "done" || v.Lanes[1].Steps[0].Outcome != "revise" {
		t.Fatalf("blocked gate rendered inaccurately: %+v", v.Lanes[1])
	}
	if v.StartedAt != st.StartedAt || v.UpdatedAt != st.UpdatedAt {
		t.Fatalf("loop timing absent: %+v", v)
	}
	st.Status = "waiting"
	v = viewLoop(st, false)
	if v.Lanes[1].Status != "waiting" || v.Lanes[1].Steps[0].Status != "waiting" {
		t.Fatalf("approval request rendered as %+v", v.Lanes[1])
	}
	st.Status = "running"
	v = viewLoop(st, false)
	if v.Lanes[1].Status != "running" {
		t.Fatalf("rework at a completed lane rendered as %q", v.Lanes[1].Status)
	}
}

func TestLoopViewUsesPersistedStepTiming(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	if _, err := s.CreateLoop(store.NewLoop{Slug: "timed", ComponentRoot: root, Goal: "measure"}); err != nil {
		t.Fatal(err)
	}
	row, err := s.CreateStep(store.Scope{LoopSlug: "timed", ComponentRoot: root}, store.NewStep{Agent: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepStatus(row.ID, store.StepDone, "ok"); err != nil {
		t.Fatal(err)
	}
	st := loop.LoopState{Slug: "timed", ComponentRoot: root, Status: "done", Blueprint: &blueprint.Blueprint{ID: "timed", Lanes: []blueprint.Lane{{ID: "survey", Step: &blueprint.Step{Kind: blueprint.ModelStep}}}}, RuntimeState: loop.RuntimeState{
		Position: loop.Position{Lane: "survey"}, LaneStates: map[string]string{"survey": "ok"}, StepStates: map[string]loop.StepState{"survey": {Status: "done", Outcome: "ok", StepID: row.ID}},
	}}
	view := New(Options{Store: s}).viewLoop(st, false)
	step := view.Lanes[0].Steps[0]
	if step.StartedAt == "" || step.EndedAt == "" || step.WallMS == nil || *step.WallMS < 0 {
		t.Fatalf("persisted step time absent: %+v", step)
	}
}

// TestDaemonViewCarriesTheTwoLevelCounters — the app's counters table and
// `aeon loop show --counters` read THESE rows and recompute nothing. D2
// from the app's seat: task 3 shows 0 of 2 while task 1 shows 2 of 2 —
// a view-side tally invented by the surface would be the prettiest lie in
// the bench (the panel's QAC cites this test for exactly that reason).
func TestDaemonViewCarriesTheTwoLevelCounters(t *testing.T) {
	sstore, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer sstore.Close()
	sc := store.Scope{LoopSlug: "budgets", ComponentRoot: "/p"}
	if _, err := sstore.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Root: "/p", Goal: "g", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"T1", "T2", "T3"} {
		if _, err := sstore.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	c := loop.NewCounters(sstore)
	for i := 0; i < 2; i++ {
		if _, err := c.Bump(sc, "T1", "review_rounds"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Bump(sc, "T2", "retries"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bump(sc, "", "total_failed_gates"); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{store: sstore}
	st := loop.LoopState{ID: "l1", Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Status: "running",
		Blueprint:    &blueprint.Blueprint{ID: "dev-loop"},
		RuntimeState: loop.RuntimeState{Tasks: []string{"T1", "T2", "T3"}}}
	view := d.viewLoop(st, false)

	if view.LoopCounters["total_failed_gates"] != 1 {
		t.Fatalf("loop counters missing from the view: %v — the §4 question is answered from these rows", view.LoopCounters)
	}
	rows := map[string]TaskCounterView{}
	for _, r := range view.TaskCounters {
		rows[r.Task+"/"+r.Counter] = r
	}
	if got := rows["T1/review_rounds"]; got.Value != 2 || !got.CapExceeded || got.Cap != 2 || got.Level != "task" {
		t.Fatalf("T1 at the cap renders wrong: %+v", got)
	}
	if got := rows["T3/review_rounds"]; got.Value != 0 || got.CapExceeded {
		t.Fatalf("D2 in the view: task 3 lost task 1's budget — %v", got)
	}
	// Three tasks × three task counters, no loop counters mixed into the
	// per-task rows: the level rode along, and the view did not guess.
	if len(view.TaskCounters) != 9 {
		t.Fatalf("%d task-counter rows, want 9: %v", len(view.TaskCounters), view.TaskCounters)
	}
	for i := 1; i < len(view.TaskCounters); i++ {
		a, b := view.TaskCounters[i-1], view.TaskCounters[i]
		if a.Task > b.Task || (a.Task == b.Task && a.Counter > b.Counter) {
			t.Fatalf("counter rows are not in a stable order (map iteration reached the view): %v then %v", a, b)
		}
	}
}

// TestLoopCounterCapsRideTheView — S17 T10: the app's counters table must
// render the loop ceiling WITHOUT the page knowing it. A UI-side constant
// would be a second ceiling that can disagree with the one that stops the
// loop; the caps are the engine's data (loop.Table) and they ride the view.
func TestLoopCounterCapsRideTheView(t *testing.T) {
	sstore, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer sstore.Close()
	sc := store.Scope{LoopSlug: "caps", ComponentRoot: "/p"}
	if _, err := sstore.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Root: "/p", Goal: "g", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.NewCounters(sstore).Bump(sc, "", "total_failed_gates"); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: sstore}
	st := loop.LoopState{ID: "l1", Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Status: "running",
		Blueprint: &blueprint.Blueprint{ID: "dev-loop"}}
	view := d.viewLoop(st, false)
	if got := view.LoopCounterCaps["total_failed_gates"]; got != loop.LoopCeiling {
		t.Fatalf("the ceiling must ride the view (the panel renders 1/%d from THIS, never its own constant): got %d", loop.LoopCeiling, got)
	}
	if got := view.LoopCounterCaps["qa_rounds"]; got != 2 {
		t.Fatalf("qa_rounds cap = %d, want 2 (loop.Table is the one source)", got)
	}
}
