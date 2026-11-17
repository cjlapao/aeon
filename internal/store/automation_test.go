package store

// automation_test.go — S40 T2: the four emit sites and the one helper. The
// property under test is AU4's transaction, and the tests force it from both
// ends: a RAISE(ABORT) trigger on `review` proves the outbox row cannot
// survive a failed decision, and one on `automation_event` proves the
// decision cannot survive a failed outbox row. Everything else — the closed
// set, the manual-only rule, the depth derivation, the cursor's monotonicity
// — is asserted at the door that owns it.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var autoUser = Actor{Kind: "user", ID: "op"}

// autoWorldSeq keeps each seeded world's workspace root unique — the store
// pins one component per root, and a test that seeds two worlds is saying
// "two components", not "the same directory twice".
var autoWorldSeq int

// seedAutomationWorld makes the smallest real footing: one root component
// (through the store's own door, so every column a later derivation reads is a
// column a door wrote). In the tree model the old "project" tier is derived,
// not stored, so a world is one root; its id stands in for both the old
// project id and the component id a manual task hangs on.
func seedAutomationWorld(t *testing.T, s *Store) (rootID, compID string) {
	t.Helper()
	autoWorldSeq++
	name := fmt.Sprintf("web%d", autoWorldSeq)
	w, err := s.CreateComponent(NewComponent{Name: name, Slug: name, Root: "/r/" + name})
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	return w.Component.ID, w.Component.ID
}

func seedAutomationComponent(t *testing.T, s *Store, slug, root string) string {
	t.Helper()
	w, err := s.CreateComponent(NewComponent{Name: slug, Slug: slug, Root: root})
	if err != nil {
		t.Fatalf("create component %s: %v", slug, err)
	}
	return w.Component.ID
}

// seedGoalAndComponents files a goal through the real door, submitted, with
// n components at its version 1. in_review is what DecideGoal claims FROM.
// The goal hangs on its first component (a root); the rest are roots it stakes
// in — the tree model has no project to hold many components, so each is its
// own root.
func seedGoalAndComponents(t *testing.T, s *Store, n int) (goalID, rootID string, compIDs []string) {
	t.Helper()
	autoWorldSeq++
	compIDs = []string{seedAutomationComponent(t, s, fmt.Sprintf("c%d", autoWorldSeq), "/r/c"+fmt.Sprint(autoWorldSeq))}
	for i := 2; i <= n; i++ {
		slug := fmt.Sprintf("c%d-%d", autoWorldSeq, i)
		compIDs = append(compIDs, seedAutomationComponent(t, s, slug, "/r/"+slug))
	}
	gcs := make([]GoalComponent, 0, len(compIDs))
	for _, c := range compIDs {
		gcs = append(gcs, GoalComponent{ComponentID: c, Rationale: "because", Basis: []string{"measured"}})
	}
	g, r, err := s.CreateGoal(NewGoal{ComponentID: compIDs[0], Title: "g", Type: "feature",
		SourceKind: "manual", Components: gcs, By: autoUser, Submit: true})
	if err != nil || r != nil {
		t.Fatalf("create goal: refusal=%v err=%v", r, err)
	}
	return g.ID, compIDs[0], compIDs
}

// seedManualTask files a manual task through the real door.
func seedManualTask(t *testing.T, s *Store, compID, typ string) string {
	t.Helper()
	task, r, err := s.CreateManualTask(NewManualTask{ComponentID: compID, Title: "m", Type: typ, By: autoUser})
	if err != nil || r != nil {
		t.Fatalf("create manual task: refusal=%v err=%v", r, err)
	}
	return task.ID
}

func autoCount(t *testing.T, s *Store) int {
	t.Helper()
	return countRows(t, s, `SELECT count(*) FROM automation_event`)
}

func mustForce(t *testing.T, s *Store, ddl string) {
	t.Helper()
	if _, err := s.w.Exec(ddl); err != nil {
		t.Fatalf("force failure: %v", err)
	}
}

func TestAFailedGoalDecisionLeavesNoOutboxRow(t *testing.T) {
	s := openTemp(t)
	gid, _, _ := seedGoalAndComponents(t, s, 1)
	mustForce(t, s, `CREATE TRIGGER force_review_fail BEFORE INSERT ON review BEGIN SELECT RAISE(ABORT,'forced'); END`)
	_, _, err := s.DecideGoal(gid, 1, "approved", autoUser, "")
	if err == nil {
		t.Fatal("the decision succeeded despite the forced failure")
	}
	if n := autoCount(t, s); n != 0 {
		t.Errorf("%d outbox rows survived a failed decision — the event was written outside the transaction", n)
	}
	var state string
	if err := s.r.QueryRow(`SELECT review_state FROM goal WHERE id=? AND version=1`, gid).Scan(&state); err != nil || state != "in_review" {
		t.Errorf("the goal moved to %q (err %v), want it still in_review — the decision outlived its review row", state, err)
	}
	// The control: the same call, trigger gone, works and writes exactly one.
	if _, err := s.w.Exec(`DROP TRIGGER force_review_fail`); err != nil {
		t.Fatal(err)
	}
	res, r, err := s.DecideGoal(gid, 1, "approved", autoUser, "")
	if err != nil || r != nil || !res.Changed {
		t.Fatalf("the control decision failed: refusal=%v err=%v", r, err)
	}
	if n := autoCount(t, s); n != 1 {
		t.Errorf("the control wrote %d outbox rows, want 1", n)
	}
}

func TestAFailedOutboxWriteRollsBackTheGoalDecision(t *testing.T) {
	s := openTemp(t)
	gid, _, _ := seedGoalAndComponents(t, s, 1)
	mustForce(t, s, `CREATE TRIGGER force_event_fail BEFORE INSERT ON automation_event BEGIN SELECT RAISE(ABORT,'forced'); END`)
	_, _, err := s.DecideGoal(gid, 1, "approved", autoUser, "")
	if err == nil {
		t.Fatal("the decision succeeded despite the forced outbox failure — the event write is being swallowed")
	}
	var state string
	if err := s.r.QueryRow(`SELECT review_state FROM goal WHERE id=? AND version=1`, gid).Scan(&state); err != nil || state != "in_review" {
		t.Errorf("the goal moved to %q (err %v), want in_review — a committed decision whose event failed", state, err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM review`); n != 0 {
		t.Errorf("%d review rows survived the failed outbox write — the ledger and the outbox disagreed", n)
	}
}

func TestApprovingAGoalWritesOneOutboxRowWithItsFacts(t *testing.T) {
	s := openTemp(t)
	gid, projID, comps := seedGoalAndComponents(t, s, 1)
	res, r, err := s.DecideGoal(gid, 1, "approved", autoUser, "")
	if err != nil || r != nil || !res.Changed {
		t.Fatalf("approve: refusal=%v err=%v", r, err)
	}
	var ev AutomationEvent
	var root string
	err = s.r.QueryRow(`SELECT id, event, subject_kind, subject_id, subject_version, root_id, component_id, goal_type, source_kind, depth, caused_by_trigger
		FROM automation_event`).Scan(&ev.ID, &ev.Event, &ev.SubjectKind, &ev.SubjectID, &ev.SubjectVersion,
		&root, &ev.ComponentID, &ev.GoalType, &ev.SourceKind, &ev.Depth, &ev.CausedByTrigger)
	if err != nil {
		t.Fatalf("read the goal.approved row: %v", err)
	}
	if ev.Event != EventGoalApproved || ev.SubjectKind != "goal" || ev.SubjectID != gid {
		t.Errorf("row = %s/%s/%s, want goal.approved/goal/%s", ev.Event, ev.SubjectKind, ev.SubjectID, gid)
	}
	if ev.SubjectVersion != 1 || ev.GoalType != "feature" || ev.SourceKind != "manual" {
		t.Errorf("facts = v%d/%s/%s, want v1/feature/manual", ev.SubjectVersion, ev.GoalType, ev.SourceKind)
	}
	if root != projID || ev.ComponentID != comps[0] {
		t.Errorf("address = %s/%s, want %s/%s", root, ev.ComponentID, projID, comps[0])
	}
	if ev.Depth != 0 || ev.CausedByTrigger != "" {
		t.Errorf("a person's decision carried depth %d/cause %q, want 0/\"\"", ev.Depth, ev.CausedByTrigger)
	}
	// Two components: the goal belongs to neither for matching purposes.
	gid2, _, _ := seedGoalAndComponents(t, s, 2) // its own project+components
	if _, r, err := s.DecideGoal(gid2, 1, "approved", autoUser, ""); err != nil || r != nil {
		t.Fatalf("approve the two-component goal: refusal=%v err=%v", r, err)
	}
	var comp string
	if err := s.r.QueryRow(`SELECT component_id FROM automation_event WHERE subject_id=?`, gid2).Scan(&comp); err != nil {
		t.Fatal(err)
	}
	if comp != "" {
		t.Errorf("a two-component goal emitted component %q, want \"\" — singleGoalComponent is not single", comp)
	}
}

func TestASecondGoalDecisionWritesNoSecondOutboxRow(t *testing.T) {
	s := openTemp(t)
	gid, _, _ := seedGoalAndComponents(t, s, 1)
	if _, r, err := s.DecideGoal(gid, 1, "approved", autoUser, ""); err != nil || r != nil {
		t.Fatalf("first decision: refusal=%v err=%v", r, err)
	}
	out, r, err := s.DecideGoal(gid, 1, "approved", autoUser, "again")
	if err != nil || r != nil {
		t.Fatalf("second decision: refusal=%v err=%v", r, err)
	}
	if out.Changed {
		t.Error("the second decision claims it changed the row")
	}
	if n := autoCount(t, s); n != 1 {
		t.Errorf("%d outbox rows after a second decision, want 1 — the emit sits outside the claimed-FROM branch", n)
	}
}

func TestCreatingAManualTaskWritesTaskCreatedAndAPreApprovedOneAlsoWritesApproved(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	task, r, err := s.CreateManualTask(NewManualTask{ComponentID: comp, Title: "m", Type: "bug", By: autoUser, RequireApproval: true})
	if err != nil || r != nil {
		t.Fatalf("create: refusal=%v err=%v", r, err)
	}
	rows, err := s.ReadAutomationEvents(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Event != EventTaskCreated || rows[0].SubjectID != task.ID {
		t.Fatalf("outbox = %+v, want exactly one task.created for %s", rows, task.ID)
	}
	if rows[0].TaskType != "bug" || rows[0].ComponentID != comp {
		t.Errorf("task.created facts = type %q component %q, want bug/%s", rows[0].TaskType, rows[0].ComponentID, comp)
	}
	// The pre-approved half is DEVIATION, recorded: this door cannot open it
	// — birth is 'none' or 'pending' (S38's rule, verified in this task), so
	// the defensive branch above CreateManualTask's commit has no input. The
	// second event's real door is DecideTask, tested next.
}

func TestApprovingAManualTaskWritesTaskApproved(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	taskID := seedManualTask(t, s, comp, "chore")
	// pending it was not (seedManualTask files without approval) — file one
	// that waits, then approve it through the human door.
	waiting, r, err := s.CreateManualTask(NewManualTask{ComponentID: comp, Title: "w", Type: "chore", By: autoUser, RequireApproval: true})
	if err != nil || r != nil {
		t.Fatalf("create waiting: refusal=%v err=%v", r, err)
	}
	_ = taskID
	res, r, err := s.DecideTask(waiting.ID, "approved", autoUser, "ok")
	if err != nil || r != nil || !res.Changed {
		t.Fatalf("approve: refusal=%v err=%v", r, err)
	}
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM automation_event WHERE event=? AND subject_id=?`, EventTaskApproved, waiting.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("task.approved rows = %d (err %v), want 1", n, err)
	}
	// A rejection is silence (AU3).
	rejected, r, err := s.CreateManualTask(NewManualTask{ComponentID: comp, Title: "x", Type: "chore", By: autoUser, RequireApproval: true})
	if err != nil || r != nil {
		t.Fatalf("create rejectable: refusal=%v err=%v", r, err)
	}
	if _, r, err := s.DecideTask(rejected.ID, "rejected", autoUser, "no"); err != nil || r != nil {
		t.Fatalf("reject: refusal=%v err=%v", r, err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM automation_event WHERE event=? AND subject_id=?`, EventTaskApproved, rejected.ID); n != 0 {
		t.Errorf("a rejected task emitted %d task.approved rows, want 0", n)
	}
}

// walkToComplete drives a manual task up the standard ladder to complete.
func walkToComplete(t *testing.T, s *Store, id string) {
	t.Helper()
	for _, to := range []TaskStatus{StatusInProgress, StatusVerified, StatusSecure, StatusPROpen, StatusInReview, StatusMerged, StatusComplete} {
		res, r, err := s.SetManualTaskStatus(id, to)
		if err != nil || r != nil || !res.OK {
			t.Fatalf("walk to %s: ok=%v refusal=%v err=%v", to, res.OK, r, err)
		}
	}
}

func TestCompletingAManualTaskWritesTaskCompletedAndOtherTransitionsDoNot(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	id := seedManualTask(t, s, comp, "feature")
	for _, to := range []TaskStatus{StatusInProgress, StatusVerified, StatusSecure} {
		if res, r, err := s.SetManualTaskStatus(id, to); err != nil || r != nil || !res.OK {
			t.Fatalf("%s: %v %v", to, r, err)
		}
	}
	if n := countRows(t, s, `SELECT count(*) FROM automation_event WHERE event=?`, EventTaskCompleted); n != 0 {
		t.Errorf("the ladder's intermediate rungs wrote %d task.completed rows, want 0 — completion is one rung, not the whole walk", n)
	}
	walkToComplete(t, s, id)
	if n := countRows(t, s, `SELECT count(*) FROM automation_event WHERE event=? AND subject_id=?`, EventTaskCompleted, id); n != 1 {
		t.Errorf("task.completed rows = %d, want exactly 1 for the one rung that means done", n)
	}
	// complete is terminal, but from==to is the machine's idempotent REPLAY
	// path (machine.go:244) — the answer says OK, and the outbox must say
	// once. This is the claimed-FROM rung doing its job under replay.
	if res, _, err := s.SetManualTaskStatus(id, StatusComplete); err != nil || !res.OK {
		t.Errorf("re-complete returned ok=%v err=%v, want the replay's OK", res.OK, err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM automation_event WHERE event=? AND subject_id=?`, EventTaskCompleted, id); n != 1 {
		t.Errorf("after a replayed completion there are %d task.completed rows, want 1 — the rung is missing from the UPDATE's WHERE", n)
	}
}

func TestLoopTasksNeverEmit(t *testing.T) {
	s := openTemp(t)
	if _, err := s.w.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		VALUES ('l1','l1','t','g','/r','/r','standard','preflight','running','2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	sc := Scope{LoopSlug: "l1", ComponentRoot: "/r"}
	task, err := s.CreateTask(sc, NewTask{TaskID: "1", Ord: 1, Title: "t", Tech: "x", Engineer: "e"})
	if err != nil {
		t.Fatalf("create loop task: %v", err)
	}
	for _, to := range []TaskStatus{StatusIssueOpen, StatusInProgress, StatusVerified, StatusSecure, StatusPROpen, StatusInReview, StatusMerged, StatusComplete} {
		if _, err := s.SetTaskStatus(sc, task.TaskID, to); err != nil {
			t.Fatalf("engine walk to %s: %v", to, err)
		}
	}
	// And the person's doors refuse the loop task — a refusal is not an emit.
	if _, r, _ := s.DecideTask(task.ID, "approved", autoUser, ""); r == nil {
		t.Error("DecideTask approved a loop task — the door it should refuse")
	}
	if _, r, _ := s.SetManualTaskStatus(task.ID, StatusComplete); r == nil {
		t.Error("SetManualTaskStatus moved a loop task — the door it should refuse")
	}
	if n := autoCount(t, s); n != 0 {
		t.Errorf("the loop task's whole life wrote %d outbox rows, want 0 (AU3)", n)
	}
}

func TestTheHelperRefusesANonManualTaskEvent(t *testing.T) {
	s := openTemp(t)
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = insertAutomationEvent(tx, s.Now(), AutomationEventIn{
		Event: EventTaskCreated, SubjectKind: "task", SubjectID: "whatever", TaskType: "bug", Manual: false,
	})
	if !errors.Is(err, ErrNotAutomatable) {
		t.Errorf("a non-manual task event returned %v, want ErrNotAutomatable", err)
	}
	if err := insertAutomationEvent(tx, s.Now(), AutomationEventIn{Event: "goal.rejected", SubjectKind: "goal", SubjectID: "g"}); err == nil {
		t.Error("the helper accepted an event outside the closed set")
	}
}

func seedAutomationLoop(t *testing.T, s *Store, id, startedBy, triggerID string, depth int) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id,automation_depth)
		VALUES (?,'slug-'||?,'t','g','/r','/r','standard','preflight','running','2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z',?,?,?)`,
		id, id, startedBy, triggerID, depth); err != nil {
		t.Fatalf("seed loop %s: %v", id, err)
	}
}

func TestDepthAndCauseComeFromTheOriginLoopNeverFromInput(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	seedAutomationLoop(t, s, "l-ev", "event", "tr1", 2)
	seedAutomationLoop(t, s, "l-trg", "trigger", "w1", 1)
	mk := func(origin string) string {
		task, r, err := s.CreateManualTask(NewManualTask{ComponentID: comp, Title: "m", Type: "bug", By: autoUser, OriginLoopID: origin})
		if err != nil || r != nil {
			t.Fatalf("create (origin %q): refusal=%v err=%v", origin, r, err)
		}
		return task.ID
	}
	ev := func(subject string) (int, string) {
		var d int
		var c string
		if err := s.r.QueryRow(`SELECT depth, caused_by_trigger FROM automation_event WHERE event=? AND subject_id=?`, EventTaskCreated, subject).Scan(&d, &c); err != nil {
			t.Fatalf("read depth/cause for %s: %v", subject, err)
		}
		return d, c
	}
	if d, c := ev(mk("l-ev")); d != 2 || c != "tr1" {
		t.Errorf("event-started origin: depth %d cause %q, want 2/tr1", d, c)
	}
	if d, c := ev(mk("l-trg")); d != 1 || c != "" {
		t.Errorf("S20-fired origin: depth %d cause %q, want 1/\"\" — a clock fire is not a cascade link (AU5)", d, c)
	}
	if d, c := ev(mk("")); d != 0 || c != "" {
		t.Errorf("a person's write: depth %d cause %q, want 0/\"\"", d, c)
	}
	// An unknown origin loop fails CLOSED — and the task itself rides out
	// with the event (one transaction), so the board does not keep a task
	// whose depth nobody can vouch for.
	_, _, err := s.CreateManualTask(NewManualTask{ComponentID: comp, Title: "ghost", Type: "bug", By: autoUser, OriginLoopID: "nope"})
	if err == nil {
		t.Fatal("the unknown origin loop was accepted — depth reset to 0 on a loop nobody has")
	}
	if n := countRows(t, s, `SELECT count(*) FROM task WHERE title='ghost'`); n != 0 {
		t.Errorf("the rolled-back task left %d rows, want 0", n)
	}
}

func TestTheClosedEventSetMatchesTheMigrationCheck(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	mustTask := seedManualTask(t, s, comp, "feature")
	// Every Go name inserts (task events need a manual task's existence for
	// the database's own trigger); every name NOT in the set is refused by
	// 0039's CHECK — the list and the migration held to the same four.
	for _, e := range AutomationEvents() {
		kind, sub := "goal", "g-any"
		if strings.HasPrefix(e, "task.") {
			kind, sub = "task", mustTask
		}
		if _, err := s.w.Exec(`INSERT INTO automation_event (event,subject_kind,subject_id,created_at) VALUES (?,?,?,'2026-10-28T06:00:00.000Z')`, e, kind, sub); err != nil {
			t.Errorf("the Go name %q failed its own CHECK: %v", e, err)
		}
	}
	for _, bad := range []string{"goal.rejected", "task.completed.", "task", ""} {
		if _, err := s.w.Exec(`INSERT INTO automation_event (event,subject_kind,subject_id,created_at) VALUES (?,?,?,'2026-10-28T06:00:00.000Z')`, bad, "goal", "g"); err == nil {
			t.Errorf("event %q was inserted — the CHECK is wider than AutomationEvents()", bad)
		}
	}
}

func TestTheHelperIsTheOnlyWriterOfTheOutbox(t *testing.T) {
	// AU4's one-writer rule, grepped: a second INSERT INTO automation_event
	// would be a second place that has to remember AU3/AU4/AU5 at once.
	hits := grepRepo(t, regexp.MustCompile(`INSERT INTO automation_event`))
	var files []string
	for _, h := range hits {
		files = append(files, h.file)
		if !strings.HasSuffix(h.file, "automation.go") {
			t.Errorf("a second writer of the outbox: %s — %s", h.file, h.line)
		}
	}
	if len(hits) != 1 {
		t.Errorf("%d INSERT INTO automation_event in module sources (%v), want exactly 1 in automation.go", len(hits), files)
	}
	// The walker is not vacuous: a known-second writer pattern still hits.
	if n := len(grepRepo(t, regexp.MustCompile(`INSERT INTO review \(`))); n < 1 {
		t.Errorf("the control grep found %d writers of review — the walker is reading nothing", n)
	}
}

func TestTheCursorNeverMovesBackward(t *testing.T) {
	s := openTemp(t)
	_, comp := seedAutomationWorld(t, s)
	var ids []int64
	for i := 0; i < 5; i++ {
		seedManualTask(t, s, comp, "chore")
	}
	rows, err := s.ReadAutomationEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("read %d events, want 5", len(rows))
	}
	for _, e := range rows {
		ids = append(ids, e.ID)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("ReadAutomationEvents returned %v — not ascending", ids)
		}
	}
	if lim, err := s.ReadAutomationEvents(0, 2); err != nil || len(lim) != 2 {
		t.Errorf("limit honoured? %d rows (err %v), want 2", len(lim), err)
	}
	head, err := s.AutomationHead()
	if err != nil || head != ids[len(ids)-1] {
		t.Errorf("head = %d (err %v), want %d", head, err, ids[len(ids)-1])
	}
	if n, err := s.AutomationCursor("dispatcher"); err != nil || n != 0 {
		t.Errorf("a cursor that never ran reads %d (err %v), want 0", n, err)
	}
	at := time.UnixMilli(1_800_000_000_000).UTC()
	if err := s.AdvanceAutomationCursor("dispatcher", ids[4], at); err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceAutomationCursor("dispatcher", ids[2], at); err != nil {
		t.Fatal(err)
	}
	if n, err := s.AutomationCursor("dispatcher"); err != nil || n != ids[4] {
		t.Errorf("the cursor moved BACKWARD to %d (err %v), want %d — replay re-opens dispatched events", n, err, ids[4])
	}
	if _, err := s.ReadAutomationEvent(999_999); !errors.Is(err, ErrNotFound) {
		t.Errorf("an absent event read as %v, want ErrNotFound", err)
	}
	var stored string
	if err := s.r.QueryRow(`SELECT updated_at FROM automation_cursor WHERE name='dispatcher'`).Scan(&stored); err != nil || !strings.HasSuffix(stored, "Z") {
		t.Errorf("cursor updated_at = %q (err %v), want the store's isoUTC form", stored, err)
	}
}
