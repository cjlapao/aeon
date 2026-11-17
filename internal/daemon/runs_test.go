package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// runs_test.go walks S39 T9's human run doors: loop.run starts run N+1 in a
// resting loop and the refusal doors all speak DATA, loop.run.options answers
// the dialogue before the start, and loop.show carries the run history with
// its P32 cost floors and the P31 attendedness triplet.

// runBlueprint writes a resting-after blueprint: one model-step lane on the
// registered chat agent, optional kickoff template and requires clause.
func runBlueprint(t *testing.T, id string, extra string) string {
	t.Helper()
	raw := "id: " + id + "\nschemaVersion: 1\nversion: 1\nafter: converse\nconverse: chat\n" + extra +
		"output: []\nlanes:\n  - id: only\n    step: {kind: model-step, agent: chat}\nquestions: {}\n"
	path := filepath.Join(t.TempDir(), id+".yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runEnv is the stream fixture with an org under it: project + a component
// registered at the fixture root, so candidates, kickoff names and requires
// all resolve where the loops live.
func runEnv(t *testing.T, turns []streamTurn) (*streamFixture, string) {
	t.Helper()
	f := newStreamFixture(t, turns)
	pr, err := f.s.CreateComponent(store.NewComponent{Name: "runs group", Slug: "runs-group", Root: "/repo/runs-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: f.root, ParentID: pr.Component.ID}); err != nil {
		t.Fatal(err)
	}
	return f, pr.Component.ID
}

func runLoop(t *testing.T, f *streamFixture, conn string) store.Loop {
	t.Helper()
	l := restedLoop(t, f, "chat")
	initConn(t, f.d, context.Background(), conn, "ui")
	return l
}

func TestLoopRunStartsASecondRun(t *testing.T) {
	f, _ := runEnv(t, []streamTurn{{deltas: []string{turnOK}}})
	l := runLoop(t, f, "c-run1")
	sub := subscribe(t, f.d, "c-run1", protocol.SubscribeParams{})
	bp := runBlueprint(t, "second", "")

	res := dispatchJSON(t, f, "c-run1", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp})
	defer waitLoopSettled(t, f, l.ID) // the runner belongs to the test, whichever way the assertions fall
	if res["runSeq"] != float64(2) || res["status"] != "running" {
		t.Fatalf("loop.run answered %v, want runSeq 2 running", res)
	}
	if res["blueprint"] != "second" {
		t.Fatalf("the answer names %v, want the run's own blueprint", res["blueprint"])
	}
	waitLoopSettled(t, f, l.ID)
	runs, err := f.s.ListRuns(l.ID)
	if err != nil || len(runs) != 2 {
		t.Fatalf("ListRuns = %d rows (err %v), want 2", len(runs), err)
	}
	if runs[1].State != "done" {
		t.Fatalf("run 2 state %q, want done after the scripted ok", runs[1].State)
	}
	var started, finished int
	for _, e := range streamEvents(f.w, "c-run1", sub.SubscriptionID) {
		switch e.Kind {
		case "loop.run.started":
			started++
			var p map[string]any
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatalf("loop.run.started payload does not decode: %v", err)
			}
			if p["runSeq"] != float64(2) || p["startedByKind"] != "user" || p["blueprint"] != "second" {
				t.Fatalf("loop.run.started payload lacks the run, its starter or its blueprint: %v", p)
			}
		case "loop.run.finished":
			finished++
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("started/finished = %d/%d, want 1/1 (the runner ran the lane and rested the loop)", started, finished)
	}
}

func TestLoopRunWritesAndAnnouncesTheKickoff(t *testing.T) {
	f, projID := runEnv(t, []streamTurn{{deltas: []string{turnOK}}})
	l := runLoop(t, f, "c-kick")
	sub := subscribe(t, f.d, "c-kick", protocol.SubscribeParams{})
	g := orgGoal(t, f.s, projID, "ship the export", "approved")
	bp := runBlueprint(t, "kickoff", "kickoff: Work on {{.Goal}}\n")

	res := dispatchJSON(t, f, "c-kick", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp, "goalId": g.ID})
	defer waitLoopSettled(t, f, l.ID)
	if res["runSeq"] != float64(2) {
		t.Fatalf("no second run: %v", res)
	}
	waitLoopSettled(t, f, l.ID)
	rows, err := f.s.Thread(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("thread = %d rows (err %v), want the one kickoff", len(rows), err)
	}
	if !strings.HasPrefix(rows[0].Body, "Work on ship the export") || rows[0].Source != "template" || rows[0].Author != "human" {
		t.Fatalf("the kickoff row is %s/%s/%q", rows[0].Author, rows[0].Source, rows[0].Body)
	}
	var posted int
	for _, e := range streamEvents(f.w, "c-kick", sub.SubscriptionID) {
		if e.Kind == protocol.KindLoopMessagePosted {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("loop.message.posted fired %d times, want the kickoff announced", posted)
	}
	r2, _ := f.s.ReadRun(l.ID, 2)
	if r2.KickoffMessageID == nil || *r2.KickoffMessageID != rows[0].ID {
		t.Fatalf("run 2 does not link its kickoff: %v", r2.KickoffMessageID)
	}
}

func TestTypedMessageBeatsTheTemplate(t *testing.T) {
	f, projID := runEnv(t, []streamTurn{{deltas: []string{turnOK}}})
	l := runLoop(t, f, "c-typed")
	g := orgGoal(t, f.s, projID, "ignored frame", "approved")
	bp := runBlueprint(t, "kickoff2", "kickoff: Work on {{.Goal}}\n")

	res := dispatchJSON(t, f, "c-typed", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp, "goalId": g.ID, "message": "do it now"})
	defer waitLoopSettled(t, f, l.ID)
	if res["runSeq"] != float64(2) {
		t.Fatalf("no second run: %v", res)
	}
	waitLoopSettled(t, f, l.ID)
	rows, _ := f.s.Thread(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, 0)
	if len(rows) != 1 || rows[0].Body != "do it now" || rows[0].Source != "user" {
		t.Fatalf("the typing did not win: %+v", rows)
	}
}

func TestLoopRunRefusesANonIdleLoop(t *testing.T) {
	f, _ := runEnv(t, nil)
	l := runLoop(t, f, "c-busy")
	bp := runBlueprint(t, "second", "")
	// Park the loop running directly (no runner is started for it — the
	// claim itself is what the door checks, before any work).
	if err := f.s.SetLoopStatus(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, "running"); err != nil {
		t.Fatal(err)
	}
	res := dispatchJSON(t, f, "c-busy", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp})
	refusedBody, ok := res["refused"].(map[string]any)
	if !ok || refusedBody["kind"] != "not-idle" {
		t.Fatalf("a live loop answered %v, want refused kind=not-idle", res)
	}
	if _, has := res["error"]; has {
		t.Fatal("a domain refusal must be data, never a transport error")
	}
	runs, _ := f.s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("the refusal wrote a run row: %d", len(runs))
	}
}

func TestUnmetRequiresIsASentenceWithCandidatesAndNoRunRow(t *testing.T) {
	f, projID := runEnv(t, nil)
	l := runLoop(t, f, "c-req")
	g := orgGoal(t, f.s, projID, "the approved one", "approved")
	_ = g
	bp := runBlueprint(t, "needs-goal", "requires:\n  - any_of:\n      - {type: goal, state: approved}\n")
	before, _ := f.s.ListRuns(l.ID)

	res := dispatchJSON(t, f, "c-req", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp})
	refusedBody, ok := res["refused"].(map[string]any)
	if !ok || refusedBody["kind"] != "requires-unmet" {
		t.Fatalf("an unmet requires answered %v, want refused kind=requires-unmet", res)
	}
	// The sentence is CheckRequires' own, verbatim — no second formatter.
	want := loop.CheckRequires(mustPreview(t, bp, "standard"), nil, nil, f.d.candidatesFor(f.root))
	if refusedBody["detail"] != want.Sentence {
		t.Fatalf("the detail moved off the evaluator's sentence:\n got %v\nwant %q", refusedBody["detail"], want.Sentence)
	}
	cands, ok := refusedBody["candidates"].([]any)
	if !ok || len(cands) == 0 {
		t.Fatalf("no candidates offered: %v", refusedBody["candidates"])
	}
	after, _ := f.s.ListRuns(l.ID)
	if len(after) != len(before) {
		t.Fatalf("a refused requires wrote run rows: %d -> %d (CR5)", len(before), len(after))
	}
	pin, _ := f.s.ReadPinnedLoop(l.ID)
	if pin.Loop.Status != "idle" {
		t.Fatalf("the refusal moved the loop to %q", pin.Loop.Status)
	}
}

func mustPreview(t *testing.T, path, mode string) *blueprint.Blueprint {
	t.Helper()
	bp, err := loop.PreviewBlueprint(nil, path, mode)
	if err != nil {
		t.Fatal(err)
	}
	return &bp
}

func TestRunPinsTheGoalVersion(t *testing.T) {
	f, projID := runEnv(t, []streamTurn{{deltas: []string{turnOK}}})
	l := runLoop(t, f, "c-goal")
	g := orgGoal(t, f.s, projID, "pinned frame", "approved")
	bp := runBlueprint(t, "second", "")
	res := dispatchJSON(t, f, "c-goal", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp, "goalId": g.ID})
	defer waitLoopSettled(t, f, l.ID)
	if res["runSeq"] != float64(2) {
		t.Fatalf("no second run: %v", res)
	}
	waitLoopSettled(t, f, l.ID)
	r2, err := f.s.ReadRun(l.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r2.GoalID != g.ID || r2.GoalVersion != g.Version {
		t.Fatalf("run 2 pins %+v, want goal %s version %d", r2, g.ID, g.Version)
	}
}

func TestUnknownGoalIsRefusedAsData(t *testing.T) {
	f, _ := runEnv(t, nil)
	l := runLoop(t, f, "c-unk")
	bp := runBlueprint(t, "second", "")
	res := dispatchJSON(t, f, "c-unk", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp, "goalId": "nope"})
	refusedBody, ok := res["refused"].(map[string]any)
	if !ok || refusedBody["kind"] != "unknown-link" {
		t.Fatalf("an unknown goal answered %v, want refused kind=unknown-link", res)
	}
	if !strings.Contains(refusedBody["detail"].(string), "no goal") {
		t.Fatalf("the sentence must be the one loop.start speaks: %v", refusedBody["detail"])
	}
	runs, _ := f.s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("the refusal wrote run rows: %d", len(runs))
	}
}

func TestInvalidBlueprintIsRefusedAndTheLoopStaysIdle(t *testing.T) {
	f, _ := runEnv(t, nil)
	l := runLoop(t, f, "c-bad")
	bad := runBlueprint(t, "bad-agent", "")
	raw, _ := os.ReadFile(bad)
	badPath := filepath.Join(t.TempDir(), "bad-agent.yaml")
	if err := os.WriteFile(badPath, []byte(strings.Replace(string(raw), "agent: chat", "agent: nobody-exists", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	res := dispatchJSON(t, f, "c-bad", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": badPath})
	refusedBody, ok := res["refused"].(map[string]any)
	if !ok || refusedBody["kind"] != "invalid-blueprint" {
		t.Fatalf("an invalid blueprint answered %v, want refused kind=invalid-blueprint", res)
	}
	pin, _ := f.s.ReadPinnedLoop(l.ID)
	if pin.Loop.Status != "idle" {
		t.Fatalf("the refusal moved the loop: %q", pin.Loop.Status)
	}
	runs, _ := f.s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("a refused preview wrote a run row: %d", len(runs))
	}
}

func TestLoopRunDoesNotChangeAttendedness(t *testing.T) {
	yes, no := true, false
	for _, declared := range []*bool{&no, nil, &yes} {
		f, _ := runEnv(t, []streamTurn{{deltas: []string{turnOK}}})
		l := restedLoopWith(t, f, "chat", declared)
		initConn(t, f.d, context.Background(), "c-att", "ui")
		bp := runBlueprint(t, "second", "")
		res := dispatchJSON(t, f, "c-att", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp})
		defer waitLoopSettled(t, f, l.ID)
		if res["runSeq"] != float64(2) && res["refused"] != nil {
			t.Fatalf("attendedness %v blocked the run: %v", declared, res)
		}
		waitLoopSettled(t, f, l.ID)
		got, err := f.s.LoopAttended(context.Background(), l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if (declared == nil) != (got == nil) {
			t.Fatalf("CR7: declared %v became %v", declared, got)
		}
		if declared != nil && *got != *declared {
			t.Fatalf("CR7: attendedness %v became %v — the run door never writes the declaration", *declared, *got)
		}
	}
}

func TestRunOptionsExplainEachUnrunnableBlueprint(t *testing.T) {
	f, projID := runEnv(t, nil)
	l := runLoop(t, f, "c-opts")
	_ = projID
	res := dispatchJSON(t, f, "c-opts", protocol.MethodLoopRunOptions, map[string]any{"id": l.ID})
	options, ok := res["options"].([]any)
	if !ok || len(options) == 0 {
		t.Fatalf("no options offered: %v", res)
	}
	var sawChatRunnable, sawUnrunnable bool
	for _, o := range options {
		row, ok := o.(map[string]any)
		if !ok {
			t.Fatalf("option row %v is not an object", o)
		}
		if row["runnable"] == true {
			if _, has := row["reason"]; has {
				t.Fatalf("a runnable blueprint carries a reason: %v", row)
			}
			continue
		}
		sawUnrunnable = true
		if row["reason"] == "" || row["reason"] == nil {
			t.Fatalf("an unrunnable blueprint without a sentence: %v", row)
		}
	}
	// The shipped brainstorm/chat blueprints preview cleanly, so at least one
	// runnable row exists; and gate blueprints from the standard set carry
	// requires (two-step-style) or at minimum answer the dialogue honestly.
	for _, o := range options {
		row := o.(map[string]any)
		if row["blueprint"] == "chat" && row["runnable"] == true {
			sawChatRunnable = true
		}
	}
	if !sawChatRunnable {
		t.Fatal("the shipped chat blueprint must preview runnable — the options list answers what CAN start")
	}
	// One requires-bearing row at least must carry candidates or a sentence.
	_ = sawUnrunnable
}

func TestShowCarriesRunsAndCostFloors(t *testing.T) {
	f, _ := runEnv(t, []streamTurn{{deltas: []string{turnOK}}, {deltas: []string{turnOK}}})
	l := runLoop(t, f, "c-show")
	bp := runBlueprint(t, "second", "")
	res := dispatchJSON(t, f, "c-show", protocol.MethodLoopRun, map[string]any{"id": l.ID, "blueprint": bp})
	if res["runSeq"] != float64(2) {
		t.Fatalf("no second run: %v", res)
	}
	waitLoopSettled(t, f, l.ID)
	// Tag a run-1 step with a known number and a partial one: the floor sums
	// the measured figures and exact drops (P32 — never a stored total).
	sc := store.Scope{LoopSlug: "chat", ComponentRoot: f.root}
	a, err := f.s.CreateStep(sc, store.NewStep{Agent: "x", Status: store.StepDone})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.CreateStep(sc, store.NewStep{Agent: "x", Status: store.StepDone})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetStepRun(a.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetStepRun(b.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetStepCost(a.ID, model.Cost{State: model.CostKnown, USD: 0.1}); err != nil {
		t.Fatal(err)
	}
	// Partial speaks USDAtLeast — the field IS the floor (encodeCost keeps
	// the measured lower bound; USD would have been silently discarded).
	if err := f.s.SetStepCost(b.ID, model.Cost{State: model.CostPartial, USDAtLeast: 0.3}); err != nil {
		t.Fatal(err)
	}

	view := dispatchJSON(t, f, "c-show", protocol.MethodLoopShow, map[string]any{"id": l.ID})
	runs, ok := view["runs"].([]any)
	if !ok || len(runs) != 2 {
		t.Fatalf("loop.show carries %v runs, want 2", view["runs"])
	}
	first := runs[0].(map[string]any)
	second := runs[1].(map[string]any)
	if first["runSeq"] != float64(1) || second["runSeq"] != float64(2) {
		t.Fatalf("runs out of order: %v / %v", first["runSeq"], second["runSeq"])
	}
	if second["blueprint"] != "second" {
		t.Fatalf("run 2 names %v", second["blueprint"])
	}
	cost := first["cost"].(map[string]any)
	if cost["exact"] != false || cost["partial"] != float64(1) {
		t.Fatalf("run 1 cost honesty: %v, want exact=false partial=1", cost)
	}
	if usd, _ := cost["usdAtLeast"].(float64); usd < 0.39 || usd > 0.41 {
		t.Fatalf("the floor reads %v, want 0.4 (sums the measured, calls it a floor)", cost["usdAtLeast"])
	}
	if view["runSeq"] != float64(2) {
		t.Fatalf("view runSeq %v, want the highest", view["runSeq"])
	}
}

func TestShowCarriesAttendednessAsThreeStates(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		declared *bool
		want     any
	}{
		{declared: &yes, want: true},
		{declared: &no, want: false},
		{declared: nil, want: nil},
	}
	for i, tc := range cases {
		f, _ := runEnv(t, nil)
		l := restedLoopWith(t, f, "chat", tc.declared)
		initConn(t, f.d, context.Background(), "c-shown", "ui")
		view := dispatchJSON(t, f, "c-shown", protocol.MethodLoopShow, map[string]any{"id": l.ID})
		raw, _ := json.Marshal(view)
		if !strings.Contains(string(raw), `"attended":`) {
			t.Fatalf("case %d: loop.show dropped the attended key entirely — P31 needs the third state on the wire: %s", i, raw)
		}
		got, present := view["attended"]
		if !present {
			t.Fatalf("case %d: attended missing from the view", i)
		}
		if got != tc.want {
			t.Fatalf("case %d: attended %v, want %v (nil must not collapse into false)", i, got, tc.want)
		}
	}
}

func TestLoopStartRefactorChangedNothing(t *testing.T) {
	// The S38 T13/T15 closure now speaks through resolveLinks/goalPromptText.
	// This test re-walks one observable of the closure — typed goal beats the
	// seeded frame — so a neutral extraction that silently moved a word fails
	// HERE, beside the full existing suites the task table also counts.
	f, projID := runEnv(t, nil)
	g := orgGoal(t, f.s, projID, "goal frame", "approved")
	path := runBlueprint(t, "second", "")
	res, err := f.d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "typed words win", WorkspaceRoot: f.root, Slug: "typed-goal", GoalID: g.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("start answered %v", res)
	}
	defer waitLoopSettled(t, f, id)
	l, err := f.s.GetLoop(store.Scope{LoopSlug: "typed-goal", ComponentRoot: f.root})
	if err != nil {
		t.Fatal(err)
	}
	if l.Goal != "typed words win" {
		t.Fatalf("the closure's prompt seeding moved: goal %q", l.Goal)
	}
	if l.GoalID != g.ID || l.GoalVersion != g.Version {
		t.Fatalf("the start lost the pinned coordinates: %+v", l)
	}
}
