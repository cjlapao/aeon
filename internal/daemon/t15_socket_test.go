package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// S38 T15 — the socket's org doors. Every test here speaks the protocol the
// way a client does: initialize, dispatch, read the result; and reads the
// rows afterwards to prove the event only ever announced what already
// committed.

// t15PeerUID is the uid the fake transport reports for "c1" — the number
// the ledger must show as the decider, because the KERNEL put it there.
const t15PeerUID = 4321

func t15Daemon(t *testing.T) (*Daemon, *wire, *store.Store, string) {
	t.Helper()
	s, root := t14Env(t)
	w := &wire{}
	d := New(Options{
		Store:       s,
		Logger:      w,
		Notify:      w.notify,
		Connections: func() []localrpc.ConnInfo { return w.conns },
	})
	w.conns = []localrpc.ConnInfo{{ID: "c1", PeerUID: t15PeerUID, PeerPID: 99}}
	initConn(t, d, context.Background(), "c1", "board-app")
	return d, w, s, root
}

func t15Dispatch(t *testing.T, d *Daemon, method protocol.MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return d.Table.Dispatch(context.Background(), "c1", method, raw)
}

func t15ApprovedGoal(t *testing.T, s *store.Store) store.Goal {
	t.Helper()
	p, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(p) == 0 {
		t.Fatalf("no project seeded: %v", err)
	}
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: p[0].ID, Title: "judged frame", Type: "feature",
		SourceKind: "manual", SourceRef: "t15", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	return g
}

// (1) The decider is the peer the kernel reported — never a params field.
func TestDecideRecordsThePeerAsUser(t *testing.T) {
	d, _, s, _ := t15Daemon(t)
	g := t15ApprovedGoal(t, s)

	res, err := t15Dispatch(t, d, protocol.MethodGoalDecide, protocol.GoalDecideParams{
		ID: g.ID, Version: 1, Decision: "approved", Reason: "the frame is right"})
	if err != nil {
		t.Fatalf("goal.decide: %v", err)
	}
	view, ok := res.(protocol.DecideView)
	if !ok || !view.Changed || view.State != "approved" {
		t.Fatalf("goal.decide answered %+v", res)
	}
	reviews, err := s.ListReviews("goal", g.ID)
	if err != nil || len(reviews) == 0 {
		t.Fatalf("reviews: %v (%d)", err, len(reviews))
	}
	last := reviews[len(reviews)-1]
	if last.ByKind != "user" || last.ByID != "4321" {
		t.Fatalf("the ledger says %s/%s, want user/4321 — the decider is the peer credential", last.ByKind, last.ByID)
	}
}

// (2) A second decision is not an error: it names the FIRST decider.
func TestDecideTwiceNamesTheFirstDecider(t *testing.T) {
	d, _, s, _ := t15Daemon(t)
	g := t15ApprovedGoal(t, s)
	if _, err := t15Dispatch(t, d, protocol.MethodGoalDecide, protocol.GoalDecideParams{ID: g.ID, Version: 1, Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	res, err := t15Dispatch(t, d, protocol.MethodGoalDecide, protocol.GoalDecideParams{ID: g.ID, Version: 1, Decision: "rejected", Reason: "changed my mind"})
	if err != nil {
		t.Fatalf("a second decision is not an error: %v", err)
	}
	view := res.(protocol.DecideView)
	if view.Changed || view.State != "approved" || view.DecidedBy != "4321" {
		t.Fatalf("second decide answered %+v, want changed=false approved by 4321", view)
	}
	reviews, _ := s.ListReviews("goal", g.ID)
	if len(reviews) != 1 {
		t.Fatalf("the ledger has %d rows, want 1 — claimed-FROM keeps the verdict stable", len(reviews))
	}
}

// (3) An actor field in params has nowhere to land (OL3): it is not
// rejected (forward-compat, T3) and not obeyed (identity is stamped) — it
// is INERT, and that is what the test pins.
func TestTheActorFieldIsInertOnDecideDoors(t *testing.T) {
	d, _, s, _ := t15Daemon(t)
	g := t15ApprovedGoal(t, s)
	raw := json.RawMessage(`{"id":"` + g.ID + `","version":1,"decision":"approved","actor":{"kind":"agent","id":"evil"}}`)
	if _, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodGoalDecide, raw); err != nil {
		t.Fatalf("decide with an actor field: %v", err)
	}
	reviews, _ := s.ListReviews("goal", g.ID)
	if len(reviews) != 1 || reviews[0].ByKind != "user" || reviews[0].ByID != "4321" {
		t.Fatalf("the smuggled actor moved: %+v — params do not choose the decider", reviews)
	}
}

// (4) The read half of the projection: a person with no step reads the
// whole org through the catalog's methods (the allow-list side of the door
// rule) — project.list answers rows, not the step refusal.
func TestProjectedReadServesAPersonWithoutAStep(t *testing.T) {
	d, _, _, _ := t15Daemon(t)
	res, err := t15Dispatch(t, d, protocol.MethodName("goal.list"), map[string]any{})
	if err != nil {
		t.Fatalf("goal.list as a person: %v", err)
	}
	if _, ok := res.([]store.Goal); !ok {
		t.Fatalf("goal.list answered %T, want the rows", res)
	}
}

// (5) The step-scoped acts are refused BY NAME with the door that answers
// a person — never answered zero.
func TestProjectedDoorsRefuseStepActsToPeople(t *testing.T) {
	d, _, _, _ := t15Daemon(t)
	for _, c := range []struct{ method, input, want string }{
		{"brief.create", `{"goal_id":"goal-x","scope":"s"}`, "decision"},
		{"task.bump_counter", `{"id":"task-x","counter":"qa_rounds"}`, "counters"},
		{"attachment.attach", `{"subject":{"kind":"goal","id":"goal-x"},"document":"d.md"}`, "person's doors"},
		{"epic.create", `{"title":"an epic","objective":"o"}`, "a person decides"},
	} {
		_, err := t15Dispatch(t, d, protocol.MethodName(c.method), json.RawMessage(c.input))
		if err == nil {
			t.Errorf("%s as a person: expected the named refusal, got success", c.method)
			continue
		}
		if !strings.Contains(err.Error(), "person's door") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s refused with %q — the sentence must name the person's door (%s)", c.method, err.Error(), c.want)
		}
	}

	// goal.create is an OPENED door (S38 T16): the person authors, but the
	// coordinates a step would have stamped still have to be said — naming
	// no group is a refusal, never a guessed one.
	if _, err := t15Dispatch(t, d, protocol.MethodName("goal.create"), json.RawMessage(`{"title":"mine","type":"feature"}`)); err == nil || !strings.Contains(err.Error(), "names its root") {
		t.Errorf("goal.create with no root: %v — the refusal must name the missing coordinate", err)
	}
}

// (6) A person moves a manual task's rung and decides it; both fire the
// board's one kind AFTER the row committed.
func t15ManualTask(t *testing.T, s *store.Store, root string, requireApproval bool) store.Task {
	t.Helper()
	comps, err := s.ListComponents(store.ComponentFilter{ParentID: func() string {
		p, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
		return p[0].ID
	}()})
	if err != nil || len(comps) == 0 {
		t.Fatalf("components: %v", err)
	}
	_ = root
	task, rf, err := s.CreateManualTask(store.NewManualTask{
		ComponentID: comps[0].ID, Title: "board rung", Type: "feature",
		AssigneeKind: "agent", Assignee: "golang-engineer",
		By: store.Actor{Kind: "user", ID: "operator"}, RequireApproval: requireApproval,
	})
	if err != nil || rf != nil {
		t.Fatalf("manual task: %v %v", rf, err)
	}
	return task
}

func TestTaskChangedFiresAfterCommit(t *testing.T) {
	d, w, s, root := t15Daemon(t)
	task := t15ManualTask(t, s, root, false)
	sub := subscribe(t, d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{Kinds: []string{protocol.KindTaskChanged}}})
	_ = sub

	res, err := t15Dispatch(t, d, protocol.MethodName("task.set_status"), map[string]any{"id": task.ID, "status": "in-progress"})
	if err != nil {
		t.Fatalf("task.set_status as the author: %v", err)
	}
	_ = res
	fresh, err := s.GetTaskByID(task.ID)
	if err != nil || fresh.Status != "in-progress" {
		t.Fatalf("row says %q (%v) — the dispatch must have committed", fresh.Status, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, sent := range w.sent {
		if sent.Method != "event" || sent.Event.Kind != protocol.KindTaskChanged {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(sent.Event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["id"] != task.ID || payload["status"] != "in-progress" || payload["previous"] != "pending" {
			t.Fatalf("task.changed announced %+v, want this task pending→in-progress", payload)
		}
		if payload["manual"] != true {
			t.Fatalf("manual flag lost: %+v", payload)
		}
		return
	}
	t.Fatal("no task.changed was delivered — the board would sit frozen while its rows moved")
}

// (7) The board read mirrors the store filter; the cost a row carries is
// the store's floor-flagged figure (P32), never a client-side sum.
func TestTaskListFilterMatrix(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	manual := t15ManualTask(t, s, root, true)
	g := t15ApprovedGoal(t, s)
	v := g.Version
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "on-goal", ComponentRoot: root, Title: "G", Goal: "g",
		GoalID: g.ID, GoalVersion: &v}, "goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: pin.Slug, ComponentRoot: pin.ComponentRoot}
	loopTask, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "engineered", Tech: "go", Engineer: "golang-engineer"})
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(sc, store.NewStep{TaskID: "T1", Phase: "build", Agent: "golang-engineer", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(step.ID, model.Cost{State: model.CostPartial, USDAtLeast: 0.25}); err != nil {
		t.Fatal(err)
	}

	// The floor flag rides the row.
	res, err := t15Dispatch(t, d, protocol.MethodTaskList, protocol.TaskListParams{})
	if err != nil {
		t.Fatal(err)
	}
	all := res.(protocol.TaskListResult)
	if len(all.Tasks) != 2 {
		t.Fatalf("board shows %d rows, want 2: %+v", len(all.Tasks), all.Tasks)
	}
	var sawLoop, sawManual bool
	for _, row := range all.Tasks {
		switch row.ID {
		case loopTask.ID:
			sawLoop = true
			if !row.CostIsFloor || row.CostUSD != 0.25 {
				t.Errorf("engineered row cost %+v/%+v — a partial step makes the total a FLOOR", row.CostUSD, row.CostIsFloor)
			}
		case manual.ID:
			sawManual = true
			if row.ApprovalState != "pending" {
				t.Errorf("manual row approval %q, want pending", row.ApprovalState)
			}
		}
	}
	if !sawLoop || !sawManual {
		t.Fatalf("matrix missed rows: %+v", all.Tasks)
	}

	// The filters mirror the store's.
	only, err := t15Dispatch(t, d, protocol.MethodTaskList, protocol.TaskListParams{Manual: boolp(true), ApprovalState: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if got := only.(protocol.TaskListResult).Tasks; len(got) != 1 || got[0].ID != manual.ID {
		t.Fatalf("manual+pending answered %+v", got)
	}
	none, err := t15Dispatch(t, d, protocol.MethodTaskList, protocol.TaskListParams{Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if got := none.(protocol.TaskListResult).Tasks; len(got) != 0 || got == nil {
		t.Fatalf("a filter that matches nothing answers [], never nil: %+v", got)
	}
	byAssignee, err := t15Dispatch(t, d, protocol.MethodTaskList, protocol.TaskListParams{Assignee: "golang-engineer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAssignee.(protocol.TaskListResult).Tasks) != 2 {
		t.Fatal("both rows are assigned to the engineer")
	}
	// Names cross the wire.
	byComponentName, err := t15Dispatch(t, d, protocol.MethodTaskList, protocol.TaskListParams{Component: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byComponentName.(protocol.TaskListResult).Tasks) != 2 {
		t.Fatal("the component NAME must resolve as well as any id")
	}
}

func boolp(b bool) *bool { return &b }

// (8) task.decide is a person's door; the ledger stamps the peer; and the
// board event fires with it.
func TestTaskDecideStampsThePeerAndAnnounces(t *testing.T) {
	d, w, s, root := t15Daemon(t)
	task := t15ManualTask(t, s, root, true)
	subscribe(t, d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{Kinds: []string{protocol.KindTaskChanged}}})

	res, err := t15Dispatch(t, d, protocol.MethodTaskDecide, protocol.TaskDecideParams{ID: task.ID, Decision: "approved", Reason: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	view := res.(protocol.DecideView)
	if !view.Changed || view.State != "approved" || view.DecidedBy != "4321" {
		t.Fatalf("task.decide answered %+v", view)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, sent := range w.sent {
		if sent.Method == "event" && sent.Event.Kind == protocol.KindTaskChanged {
			return
		}
	}
	t.Fatal("task.decide moved the row and said nothing — the board cannot see approvals land")
}

// (9) The person's edit rule and the agent's edit rule, mirrored (T7's
// sentence is the same sentence either way it is reached).
func TestUserCannotEditALoopTask(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "engined", ComponentRoot: root, Title: "E", Goal: "g"},
		"goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: pin.Slug, ComponentRoot: pin.ComponentRoot}
	task, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "engine's own", Tech: "go", Engineer: "e"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = t15Dispatch(t, d, protocol.MethodName("task.update"), map[string]any{"id": task.ID, "title": "hijacked"})
	if err == nil || !strings.Contains(err.Error(), "the engine's door") {
		t.Fatalf("a person editing a loop task got %v — T7's sentence must answer", err)
	}
	_, err = t15Dispatch(t, d, protocol.MethodName("task.set_status"), map[string]any{"id": task.ID, "status": "done"})
	if err == nil || !strings.Contains(err.Error(), "the engine's door") {
		t.Fatalf("a person moving a loop task's rung got %v — the manual door speaks T7's sentence itself", err)
	}
}

func TestOwningAgentCanEditItsLoopTask(t *testing.T) {
	_, _, s, root := t15Daemon(t)
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "engined", ComponentRoot: root, Title: "E", Goal: "g"},
		"goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: pin.Slug, ComponentRoot: pin.ComponentRoot}
	task, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "engine's own", Tech: "go", Engineer: "e"})
	if err != nil {
		t.Fatal(err)
	}
	stepID := startLaneStep(t, s, pin.Slug, root, "planner")
	svc := NewRecordsService(s)
	ctx := stepctx.WithStepID(context.Background(), stepID)
	out, f, err := svc.Do(ctx, Actor{Kind: "agent", ID: "planner"}, "task_update", json.RawMessage(`{"id":"`+task.ID+`","title":"refined by the plan"}`))
	if err != nil || f != nil {
		t.Fatalf("the owning agent editing its own task: %v %v", f, err)
	}
	if got, ok := out.(store.Task); !ok || got.Title != "refined by the plan" {
		t.Fatalf("agent edit answered %+v", out)
	}
}

// (10) goal.show composes the picture: rows, ledger, attachments, the
// DERIVED state and the floor-flagged cost — none of it stored.
func TestGoalShowDerivesExecutionStateAndCost(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	g := t15ApprovedGoal(t, s)
	v := g.Version
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "on-goal", ComponentRoot: root, Title: "G", Goal: "g",
		GoalID: g.ID, GoalVersion: &v, Status: "waiting"}, "goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: pin.Slug, ComponentRoot: pin.ComponentRoot}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "x", Tech: "go", Engineer: "e"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(sc, store.NewStep{TaskID: "T1", Phase: "build", Agent: "e", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(step.ID, model.Cost{State: model.CostKnown, USD: 0.1}); err != nil {
		t.Fatal(err)
	}

	res, err := t15Dispatch(t, d, protocol.MethodGoalShow, protocol.GoalShowParams{ID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	show := res.(protocol.GoalShowResult)
	if show.ExecutionState != "blocked" {
		t.Fatalf("a loop waiting on its gate is %q, want blocked", show.ExecutionState)
	}
	if len(show.Loops) != 1 || show.Loops[0].Slug != "on-goal" {
		t.Fatalf("loops: %+v", show.Loops)
	}
	if show.Cost.USD != 0.1 || show.Cost.IsFloor {
		t.Fatalf("a fully known cost came back %+v — no floor flag when every step is priced", show.Cost)
	}
	// The pinned goal's reviews ride along.
	if _, err := t15Dispatch(t, d, protocol.MethodGoalDecide, protocol.GoalDecideParams{ID: g.ID, Version: 1, Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	res, err = t15Dispatch(t, d, protocol.MethodGoalShow, protocol.GoalShowParams{ID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	show = res.(protocol.GoalShowResult)
	if len(show.Reviews) != 1 || show.Reviews[0].ByID != "4321" || show.Reviews[0].Decision != "approved" {
		t.Fatalf("reviews: %+v", show.Reviews)
	}
	// An unknown goal is a sentence, not an error channel (DB19).
	_, err = t15Dispatch(t, d, protocol.MethodGoalShow, protocol.GoalShowParams{ID: "goal-nope"})
	if err == nil || !strings.Contains(err.Error(), "goal-nope") {
		t.Fatalf("unknown goal: %v — the sentence names it", err)
	}
}

// (11) The derivation is composed from rows EVERY run — a column storing it
// would be a second truth (criterion 10).
func TestExecutionStateIsNeverStored(t *testing.T) {
	hits := 0
	err := filepath.WalkDir("../store", func(path string, ent fs.DirEntry, err error) error {
		if err != nil || ent.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range []string{"executionState", "execution_state"} {
			if strings.Contains(string(raw), needle) {
				t.Errorf("%s stores %q — execution state is DERIVED (goal.show composes it from loop rows every read)", path, needle)
				hits++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// (12) The projected door on a storeless daemon answers the THIRD state,
// inert by design (fail-closed), never inventing an answer of zero.
func TestProjectedDoorsAnswerTheThirdStateWithoutAStore(t *testing.T) {
	w := &wire{}
	d := New(Options{Notify: w.notify, Logger: w})
	initConn(t, d, context.Background(), "c1", "board-app")
	_, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodName("task.show"), json.RawMessage(`{"id":"task-x"}`))
	if err == nil || !strings.Contains(storelessWhat(t, err), "records are unavailable in this runtime") {
		t.Fatalf("storeless projection answered %v, want the third state in Data.What", err)
	}
	if _, err := t15Dispatch(t, d, protocol.MethodGoalDecide, protocol.GoalDecideParams{ID: "g", Version: 1, Decision: "approved"}); err == nil || !strings.Contains(storelessWhat(t, err), "records are unavailable in this runtime") {
		t.Fatalf("storeless decide answered %v, want the third state in Data.What", err)
	}
}

func storelessWhat(t *testing.T, err error) string {
	t.Helper()
	var ae *protocol.Error
	if errors.As(err, &ae) {
		return ae.Data.What
	}
	return err.Error()
}

// (13) Grep guards, in-product: a decision is never a tool, and the three
// decide methods are the ONLY callers of the store's decide doors outside
// the gate join T14 wrote.
func TestNoToolOrActionIsNamedADecision(t *testing.T) {
	for _, h := range tools.Registry() {
		if strings.Contains(h.Spec.Name, "decide") {
			t.Errorf("tool %q is a decision — reviews are human acts and their doors are socket-only (OL11)", h.Spec.Name)
		}
	}
	for _, sp := range records.Catalog() {
		if strings.Contains(sp.Name, "decide") || strings.HasSuffix(sp.Method, ".decide") {
			t.Errorf("catalog action %q (%s) is a decision", sp.Name, sp.Method)
		}
	}
}

func TestDecideDoorsHaveExactlyTheDeclaredCallers(t *testing.T) {
	allowed := map[string]bool{
		// The person's socket door (this task) and the gate's join (T14).
		"records_socket.go": true,
		"answer_decides.go": true,
	}
	for _, dir := range []string{"../tools", "../handler", "."} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, ent := range ents {
			name := ent.Name()
			if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if allowed[name] {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, needle := range []string{"DecideGoal(", "DecideBrief(", "DecideTask(", "goal.decide", "brief.decide", "task.decide"} {
				if strings.Contains(string(raw), needle) {
					t.Errorf("%s/%s mentions %q — the decide doors are the three declared methods and the gate join, nowhere else", dir, name, needle)
				}
			}
		}
	}
}
