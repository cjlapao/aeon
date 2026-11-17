package store

import (
	"strings"
	"testing"
)

// taskEnv: a group, one component with a registered root, and the actors.
type taskEnv struct {
	s           *Store
	componentID string
	user        Actor
	agent       Actor
}

func newTaskEnv(t *testing.T) taskEnv {
	t.Helper()
	s := openTemp(t)
	e := taskEnv{s: s, user: Actor{Kind: "user", ID: "cj"}, agent: Actor{Kind: "agent", ID: "planner"}}
	// A component with a registered root. The old "group" tier is gone — the
	// root IS the answer (CT1: a root with no child reads as a component).
	c, err := s.CreateComponent(NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha"})
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	e.componentID = c.Component.ID
	return e
}

func mustManual(t *testing.T, e taskEnv, title, typ string, approve bool) Task {
	t.Helper()
	tn, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: title, Type: typ,
		By: e.user, RequireApproval: approve})
	if err != nil || r != nil {
		t.Fatalf("manual %q: %v %+v", title, err, r)
	}
	return tn
}

func TestCreateManualTaskNeedsNoLoop(t *testing.T) {
	e := newTaskEnv(t)
	tn := mustManual(t, e, "triage inbox", "chore", true)
	if tn.Manual != true || tn.ApprovalState != "pending" || tn.Status != string(StatusPending) {
		t.Fatalf("manual create: %+v", tn)
	}
	got, err := e.s.getTaskByID(tn.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if got.LoopSlug != "" || got.ComponentRoot != "" {
		t.Fatalf("a manual task carries loop coordinates it never had: %+v", got)
	}
	// The engine's scope door cannot see it — manual tasks live outside every
	// (root, slug) pair until a loop links.
	if _, err := e.s.GetTask(Scope{LoopSlug: "", ComponentRoot: "/repo/alpha"}, tn.TaskID); err == nil {
		t.Fatal("a loop-scoped read found a manual task")
	}
	// Without RequireApproval the task is born actionable, not waiting.
	free := mustManual(t, e, "write changelog", "chore", false)
	if free.ApprovalState != "none" {
		t.Fatalf("unapproved-by-request task: %q, want none", free.ApprovalState)
	}
	// Refusals at the door.
	if _, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "x", Type: "daydream", By: e.user}); err != nil || r == nil || r.Kind != "type-unknown" {
		t.Fatalf("bad type: %v %+v", err, r)
	}
	if _, _, err := e.s.CreateManualTask(NewManualTask{ComponentID: "c-nope", Title: "x", Type: "chore", By: e.user}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("ghost component: %v", err)
	}
	if _, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "x", Type: "chore", By: Actor{Kind: "ghost", ID: "g"}}); err != nil || r == nil || r.Kind != "actor-unknown" {
		t.Fatalf("ghost author: %v %+v", err, r)
	}
}

func TestManualFlagIsDerivedNotSettable(t *testing.T) {
	// No exported path flips manual: the edit struct has no field for it
	// (compile-time), and no SQL in the package writes it (grep).
	src := string(mustRead(t, "tasks_manual.go"))
	for _, probe := range []string{"SET manual", "SET  manual", "manual = ?"} {
		if strings.Contains(src, probe) {
			t.Errorf("tasks_manual.go writes the manual flag (%s) — it is stamped at creation only (OL14)", probe)
		}
	}
	if strings.Contains(string(mustRead(t, "tasks.go")), "SET manual") || strings.Contains(string(mustRead(t, "tasks.go")), "SET  manual") {
		t.Error("tasks.go writes the manual flag")
	}
	// CreateManualTask always stamps manual=1 (the INSERT is a literal).
	if !strings.Contains(src, "?,1,?,") {
		t.Error("CreateManualTask does not stamp manual as a literal 1")
	}
}

func TestLoopTaskIsReadOnlyToEdits(t *testing.T) {
	e := newTaskEnv(t)
	sc := Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "walk", ComponentRoot: "/repo/alpha", Title: "W", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	lt, err := e.s.CreateTask(sc, NewTask{TaskID: "T1", Title: "engine's own", Engineer: "eng"})
	if err != nil {
		t.Fatalf("loop task: %v", err)
	}
	if lt.ID == "" || !strings.HasPrefix(lt.ID, "tsk-") || lt.Assignee != "eng" || lt.AssigneeKind != "agent" || lt.ApprovalState != "none" {
		t.Fatalf("loop task lost its new fields: %+v", lt)
	}
	// The loop task's owner row exists.
	rows, err := e.s.ListTaskRows(TaskFilter{LoopSlug: "walk"})
	if err != nil || len(rows) != 1 || rows[0].ApprovalState != "none" {
		t.Fatalf("loop task not on the board by loop: %+v %v", rows, err)
	}
	// Every manual door refuses it, naming the owning loop.
	if _, r, err := e.s.UpdateManualTask(ManualTaskEdit{ID: lt.ID, Title: "hijacked"}); err != nil || r == nil || r.Kind != "loop-task-read-only" {
		t.Fatalf("update on a loop task: %v %+v", err, r)
	} else if !strings.Contains(r.Sentence, "walk") {
		t.Errorf("the sentence must name the loop: %q", r.Sentence)
	}
	if _, r, err := e.s.SetManualTaskStatus(lt.ID, StatusInReview); err != nil || r == nil || r.Kind != "loop-task-read-only" {
		t.Fatalf("status door on a loop task: %v %+v", err, r)
	}
	if _, r, err := e.s.DecideTask(lt.ID, "approved", e.user, ""); err != nil || r == nil || r.Kind != "loop-task-not-approvable" {
		t.Fatalf("approval door on a loop task: %v %+v", err, r)
	}
	// The engine's door still works (Do-NOT: SetTaskStatus unchanged).
	res, err := e.s.SetTaskStatus(sc, "T1", StatusInProgress)
	if err != nil || !res.OK {
		t.Fatalf("engine door broke: %+v %v", res, err)
	}
}

func TestDecideTaskIsHumanAndClaimedFrom(t *testing.T) {
	e := newTaskEnv(t)
	tn := mustManual(t, e, "needs approval", "feature", true)
	if _, r, err := e.s.DecideTask(tn.ID, "approved", e.agent, ""); err != nil || r == nil || r.Kind != "decider-not-human" {
		t.Fatalf("agent approved a task: %v %+v", err, r)
	}
	if _, r, err := e.s.DecideTask(tn.ID, "parked", e.user, ""); err != nil || r == nil || r.Kind != "decision-unknown" {
		t.Fatalf("parked is not a task verdict: %v %+v", err, r)
	}
	first, r, err := e.s.DecideTask(tn.ID, "approved", e.user, "good enough")
	if err != nil || r != nil || !first.Changed || first.State != "approved" {
		t.Fatalf("first decide: %+v %+v %v", first, r, err)
	}
	second, r, err := e.s.DecideTask(tn.ID, "rejected", Actor{Kind: "user", ID: "second"}, "I dissent")
	if err != nil || r != nil {
		t.Fatalf("second decide: %v %+v", err, r)
	}
	if second.Changed || second.State != "approved" || second.DecidedBy != "cj" || second.DecidedAt == "" {
		t.Fatalf("the loser must be told who decided: %+v", second)
	}
	revs, err := e.s.ListReviews("task", tn.ID)
	if err != nil || len(revs) != 1 || revs[0].Decision != "approved" || revs[0].ByKind != "user" {
		t.Fatalf("task ledger: %+v %v", revs, err)
	}
}

func TestParentMustBeInTheSameComponent(t *testing.T) {
	e := newTaskEnv(t)
	other, err := e.s.CreateComponent(NewComponent{Name: "beta", Slug: "beta", Root: "/repo/beta"})
	if err != nil {
		t.Fatalf("second component: %v", err)
	}
	parent := mustManual(t, e, "the what", "feature", false)
	// Same component: fine.
	child, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "the how", Type: "chore",
		By: e.user, ParentTaskID: parent.ID})
	if err != nil || r != nil || child.ParentTaskID != parent.ID {
		t.Fatalf("same-component child: %+v %+v %v", child, r, err)
	}
	// Cross-component: refused by the store (there is no FK to lean on).
	if _, _, err := e.s.CreateManualTask(NewManualTask{ComponentID: other.Component.ID, Title: "impossible", Type: "chore",
		By: e.user, ParentTaskID: parent.ID}); err == nil || !strings.Contains(err.Error(), "decomposition stays within one component") {
		t.Fatalf("cross-component parent accepted: %v", err)
	}
	// Loop tasks take the same rule.
	sc := Scope{LoopSlug: "feat", ComponentRoot: "/repo/beta"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "feat", ComponentRoot: "/repo/beta", Title: "F", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if _, err := e.s.CreateTask(sc, NewTask{TaskID: "T1", Title: "nope", ParentTaskID: parent.ID}); err == nil ||
		!strings.Contains(err.Error(), "decomposition stays within one component") {
		t.Fatalf("loop task took a cross-component parent: %v", err)
	}
	// And the same-component loop-task parent works.
	if _, err := e.s.CreateTask(sc, NewTask{TaskID: "T2", Title: "ok"}); err != nil {
		t.Fatalf("plain loop task: %v", err)
	}
	if _, err := e.s.CreateTask(sc, NewTask{TaskID: "T3", Title: "child", ParentTaskID: "tsk-missing"}); err == nil {
		t.Fatal("a parent that is nobody was accepted")
	}
}

func (e taskEnv) projectOf(t *testing.T, componentID string) string {
	t.Helper()
	// The "project" of a component is its ROOT (CT1: a root with no child
	// reads as a component; a root with a child reads as a project).
	root, err := e.s.RootOf(componentID)
	if err != nil {
		t.Fatalf("root of %s: %v", componentID, err)
	}
	return root
}

func TestLinkTaskToLoop(t *testing.T) {
	e := newTaskEnv(t)
	tn := mustManual(t, e, "worked by a loop", "feature", false)
	sc := Scope{LoopSlug: "runner", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "runner", ComponentRoot: "/repo/alpha", Title: "R", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if err := e.s.LinkTaskToLoop(tn.ID, sc, "worker"); err != nil {
		t.Fatalf("worker link: %v", err)
	}
	if err := e.s.LinkTaskToLoop(tn.ID, sc, "worker"); err != nil {
		t.Fatalf("retried worker link must be the same no-op: %v", err)
	}
	if err := e.s.LinkTaskToLoop(tn.ID, sc, "owner"); err == nil {
		t.Fatal("a manual task cannot GAIN an owner row from the door")
	}
	rows, err := e.s.ListTaskRows(TaskFilter{LoopSlug: "runner"})
	if err != nil || len(rows) != 1 || len(rows[0].LoopSlugs) != 1 || rows[0].LoopSlugs[0] != "runner" {
		t.Fatalf("board does not see the link: %+v %v", rows, err)
	}
	if err := e.s.LinkTaskToLoop(tn.ID, Scope{LoopSlug: "ghost", ComponentRoot: "/repo/alpha"}, "worker"); err == nil {
		t.Fatal("a link to a loop that never was is not a fact")
	}
}

func TestListTaskRowsFilters(t *testing.T) {
	e := newTaskEnv(t)
	sc := Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "walk", ComponentRoot: "/repo/alpha", Title: "W", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if _, err := e.s.CreateTask(sc, NewTask{TaskID: "T1", Title: "loop task", Engineer: "eng"}); err != nil {
		t.Fatalf("loop task: %v", err)
	}
	goal, r, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectOf(t, e.componentID), Title: "under goal", Type: "bug",
		SourceKind: "manual", By: e.user})
	if err != nil || r != nil {
		t.Fatalf("goal: %v %+v", err, r)
	}
	m1, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "manual bug", Type: "bug",
		AssigneeKind: "user", Assignee: "cj", GoalID: goal.ID, By: e.user})
	if err != nil || r != nil {
		t.Fatalf("m1: %+v %v", r, err)
	}
	if _, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "manual chore", Type: "chore",
		AssigneeKind: "agent", Assignee: "bot", ParentTaskID: m1.ID, By: e.user}); err != nil || r != nil {
		t.Fatalf("m2: %+v %v", r, err)
	}
	// A manual task in ANOTHER component, to prove ComponentID/ProjectID
	// filters bite.
	// A manual task in ANOTHER component (a separate root), to prove the
	// ComponentID/Root filters bite.
	other, err := e.s.CreateComponent(NewComponent{Name: "gamma", Slug: "gamma", Root: "/repo/gamma"})
	if err != nil {
		t.Fatalf("other component: %v", err)
	}
	if _, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: other.Component.ID, Title: "elsewhere", Type: "bug", By: e.user}); err != nil || r != nil {
		t.Fatalf("elsewhere: %+v %v", r, err)
	}

	yes, no := true, false
	cases := []struct {
		name string
		f    TaskFilter
		want int
	}{
		{"everything", TaskFilter{}, 4},
		{"manual only", TaskFilter{Manual: &yes}, 3},
		{"loop only", TaskFilter{Manual: &no}, 1},
		{"assignee user", TaskFilter{AssigneeKind: "user"}, 1},
		{"assignee named", TaskFilter{Assignee: "bot"}, 1},
		{"type bug", TaskFilter{Type: "bug", Manual: &yes}, 2},
		{"goal", TaskFilter{GoalID: goal.ID}, 1},
		{"loop walk", TaskFilter{LoopSlug: "walk"}, 1},
		{"component", TaskFilter{ComponentID: e.componentID}, 3},
		{"root", TaskFilter{Root: e.projectOf(t, e.componentID)}, 3},
		{"parent", TaskFilter{ParentTaskID: m1.ID}, 1},
		{"status", TaskFilter{Status: string(StatusPending)}, 4},
	}
	for _, c := range cases {
		rows, err := e.s.ListTaskRows(c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(rows) != c.want {
			names := []string{}
			for _, rw := range rows {
				names = append(names, rw.Title)
			}
			t.Errorf("filter %s returned %d rows %v, want %d", c.name, len(rows), names, c.want)
		}
	}
}

func TestTaskCostIsAFloorWhenAStepIsPartial(t *testing.T) {
	e := newTaskEnv(t)
	sc := Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "walk", ComponentRoot: "/repo/alpha", Title: "W", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	known, err := e.s.CreateTask(sc, NewTask{TaskID: "Tk", Title: "known cost", Engineer: "e"})
	if err != nil {
		t.Fatalf("task k: %v", err)
	}
	partial, err := e.s.CreateTask(sc, NewTask{TaskID: "Tp", Title: "partial cost", Engineer: "e"})
	if err != nil {
		t.Fatalf("task p: %v", err)
	}
	// Steps through the raw door — cost_usd and cost_state are the engine's
	// fields; the board only reads them.
	if _, err := e.s.w.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at,cost_state,cost_usd)
		VALUES ('walk','/repo/alpha','build','e','done','t','known',2.5),
		       ('walk','/repo/alpha','build','e','done','t','known',1.5),
		       ('walk','/repo/alpha','build','e','done','t','partial',NULL)`); err != nil {
		t.Fatalf("steps: %v", err)
	}
	// Give the partial task the third step by name — taskid binds per task.
	if _, err := e.s.w.Exec(`UPDATE step SET taskid='Tk' WHERE taskid IS NULL AND cost_state='known'`); err != nil {
		t.Fatalf("bind known: %v", err)
	}
	if _, err := e.s.w.Exec(`UPDATE step SET taskid='Tp' WHERE taskid IS NULL AND cost_state='partial'`); err != nil {
		t.Fatalf("bind partial: %v", err)
	}
	rows, err := e.s.ListTaskRows(TaskFilter{LoopSlug: "walk"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %d %v", len(rows), err)
	}
	byID := map[string]TaskRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	k := byID[known.ID]
	if k.CostUSD != 4.0 || k.CostIsFloor {
		t.Fatalf("known cost: %+v, want 4.0 not-a-floor", k)
	}
	p := byID[partial.ID]
	if !p.CostIsFloor || p.CostUSD != 0 {
		t.Fatalf("partial cost: %+v — an unknown step must FLAG the floor, never silently count as zero", p)
	}
	// A manual task costs what its children cost.
	parent := mustManual(t, e, "the what", "feature", false)
	if _, err := e.s.CreateTask(sc, NewTask{TaskID: "Tc", Title: "the how", Engineer: "e", ParentTaskID: parent.ID}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := e.s.w.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at,cost_state,cost_usd,taskid)
		VALUES ('walk','/repo/alpha','build','e','done','t','known',7,'Tc')`); err != nil {
		t.Fatalf("child step: %v", err)
	}
	manualOnly := true
	rows, err = e.s.ListTaskRows(TaskFilter{ComponentID: e.componentID, Manual: &manualOnly})
	if err != nil {
		t.Fatalf("manual rows: %v", err)
	}
	for _, r := range rows {
		if r.ID == parent.ID {
			if r.CostUSD != 7 || r.CostIsFloor {
				t.Fatalf("parent cost over children: %+v, want 7.0", r)
			}
			return
		}
	}
	t.Fatal("parent not in its own component's manual listing")
}

func TestLoopTaskStatusPathIsUnchanged(t *testing.T) {
	// The cartesian product: for every (from, to) pair, SetTaskStatus on a
	// loop task answers EXACTLY what ValidateTransition says — the machine
	// table is the only truth, and T7's columns did not bend it.
	e := newTaskEnv(t)
	sc := Scope{LoopSlug: "cart", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.CreateLoop(NewLoop{Slug: "cart", ComponentRoot: "/repo/alpha", Title: "C", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	for i, from := range TaskStatuses() {
		id := "CX" + string(rune('A'+i))
		if _, err := e.s.CreateTask(sc, NewTask{TaskID: id, Title: "cart " + string(from), Engineer: "e", Status: from}); err != nil {
			t.Fatalf("task at %s: %v", from, err)
		}
		for _, to := range TaskStatuses() {
			want := ValidateTransition(from, to, ModeStandard)
			// Reset the row to `from` before each probe (the machine is
			// stateless per call, but the row is not).
			if _, err := e.s.w.Exec(`UPDATE task SET status=? WHERE loop_slug='cart' AND component_root='/repo/alpha' AND taskid=?`, string(from), id); err != nil {
				t.Fatalf("reset: %v", err)
			}
			got, err := e.s.SetTaskStatus(sc, id, to)
			if err != nil {
				t.Fatalf("%s→%s: %v", from, to, err)
			}
			if got.OK != want.OK {
				t.Errorf("%s→%s: machine said %v, store answered %v", from, to, want.OK, got.OK)
			}
		}
	}
}

func TestManualTaskBodyIsAnAttachedDocument(t *testing.T) {
	e := newTaskEnv(t)
	tn, r, err := e.s.CreateManualTask(NewManualTask{ComponentID: e.componentID, Title: "with a body",
		Type: "feature", Body: "# the long prose\nthe author's words", By: e.user})
	if err != nil || r != nil {
		t.Fatalf("body task: %+v %v", r, err)
	}
	body, err := e.s.ReadManualTaskBody(tn.ID)
	if err != nil || body != "# the long prose\nthe author's words" {
		t.Fatalf("body round trip: %q %v", body, err)
	}
	// OL4: the task row is the POINTER — the bytes are a document row, and the
	// attachment is the reference, not a copy.
	views, err := e.s.ListAttachments("task", tn.ID)
	if err != nil || len(views) != 1 || views[0].Currency != CurrencyCurrent {
		t.Fatalf("body attachment: %+v %v", views, err)
	}
	if !strings.HasPrefix(views[0].Document, "tasks/"+tn.ID) {
		t.Fatalf("body document namespace: %q, want tasks/%s…", views[0].Document, tn.ID)
	}
	// A task with no body reads empty — absent is not missing.
	plain := mustManual(t, e, "no body", "chore", false)
	if b, err := e.s.ReadManualTaskBody(plain.ID); err != nil || b != "" {
		t.Fatalf("absent body: %q %v", b, err)
	}
}
