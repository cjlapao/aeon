package automation

// dispatcher_test.go — the dispatcher as a pure function of injected time
// and an injected Starter: no sleeps, no wall clock, no goroutine. Every
// fire row is read back through the store's own readers, because the fire
// row IS the answer (AU12) — if the row says something the call did not,
// the test says so.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// ── scaffolding ─────────────────────────────────────────────────────────────

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeStarter struct {
	calls []EventStartRequest
	next  func(EventStartRequest) (StartResult, error)
}

func (f *fakeStarter) Start(_ context.Context, r EventStartRequest) (StartResult, error) {
	f.calls = append(f.calls, r)
	if f.next != nil {
		return f.next(r)
	}
	return StartResult{LoopID: "L" + r.Slug, Slug: r.Slug}, nil
}

// realStarter additionally PINS a real loop row at a root whose component
// exists, so guards that read loop status (overlap) see loops the
// dispatcher could really overlap.
type realStarter struct {
	s    *store.Store
	root string
	fakeStarter
}

func (r *realStarter) Start(ctx context.Context, req EventStartRequest) (StartResult, error) {
	if _, err := r.fakeStarter.Start(ctx, req); err != nil {
		return StartResult{}, err
	}
	l, err := r.s.CreatePinnedLoop(store.NewLoop{Slug: req.Slug, ComponentRoot: r.root,
		Title: req.TriggerName, StartedBy: "event", TriggerID: req.TriggerID,
		AutomationDepth: req.Depth}, req.Blueprint, 1, "raw bytes", `{"route":""}`)
	if err != nil {
		return StartResult{}, err
	}
	// A pin rests idle until a runner takes it (S39) — a started loop that
	// is still going is what overlap must see, and a fake that never took
	// the loop would fake the guard away.
	if err := r.s.SetLoopStatus(store.Scope{LoopSlug: l.Slug, ComponentRoot: r.root}, "running"); err != nil {
		return StartResult{}, err
	}
	return StartResult{LoopID: l.ID, Slug: l.Slug}, nil
}

var triggerSeq atomic.Int64

func openStore(t *testing.T, clock func() time.Time) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newDispatcher(s *store.Store, st Starter, clk *fakeClock) *Dispatcher {
	return &Dispatcher{Store: s, Starter: st, Clock: clk.Now,
		ResolveRoot: func(id string) (string, error) { return "/ws/" + id, nil }}
}

var opActor = store.Actor{Kind: "user", ID: "u1"}

// seedWorld: one project, n components with real roots /repo/c<i>.
func seedWorld(t *testing.T, s *store.Store, n int) (string, []string) {
	t.Helper()
	p, err := s.CreateComponent(store.NewComponent{Name: "the-project", Slug: "the-project", Root: "/repo/the-project"})
	if err != nil || !p.OK() {
		t.Fatal(err)
	}
	var ids []string
	for i := range n {
		c, err := s.CreateComponent(store.NewComponent{Name: fmt.Sprintf("c%d", i),
			Slug: fmt.Sprintf("c%d", i), Root: fmt.Sprintf("/repo/c%d", i), ParentID: p.Component.ID})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.Component.ID)
	}
	return p.Component.ID, ids
}

// approvedGoal: a goal with the named components, submitted and decided
// approved by a person — the door that emits goal.approved (the ONLY
// emitter, T2). gtype routes typed triggers.
func approvedGoal(t *testing.T, s *store.Store, projectID, gtype string, comps []string) string {
	t.Helper()
	var gc []store.GoalComponent
	for _, c := range comps {
		gc = append(gc, store.GoalComponent{ComponentID: c})
	}
	g, r, err := s.CreateGoal(store.NewGoal{ComponentID: projectID, Title: "a goal",
		Type: gtype, SourceKind: "manual", By: opActor, Submit: true, Components: gc})
	if err != nil || r != nil {
		t.Fatalf("create goal: refusal=%v err=%v", r, err)
	}
	res, r, err := s.DecideGoal(g.ID, g.Version, "approved", opActor, "ok")
	if err != nil || r != nil || !res.Changed {
		t.Fatalf("decide goal: refusal=%v err=%v changed=%v", r, err, res.Changed)
	}
	return g.ID
}

// approveAgain mints a second goal.approved for the SAME subject: an edit
// bumps the version back into review, the second approval is the
// claimed-FROM branch emitting again — same subject_id, new event row.
func approveAgain(t *testing.T, s *store.Store, goalID string) {
	t.Helper()
	g, err := s.GetGoal(goalID)
	if err != nil {
		t.Fatal(err)
	}
	edited, r, err := s.UpdateGoal(store.GoalEdit{ID: goalID, Title: g.Title + " (again)",
		Type: g.Type, By: opActor})
	if err != nil || r != nil {
		t.Fatalf("edit goal: refusal=%v err=%v", r, err)
	}
	res, r, err := s.DecideGoal(edited.ID, edited.Version, "approved", opActor, "again")
	if err != nil || r != nil || !res.Changed {
		t.Fatalf("re-decide goal: refusal=%v err=%v changed=%v", r, err, res.Changed)
	}
}

// manualTask files a manual task; approved decides it as a person (the
// task.approved emitter). originLoopID is AU5's only depth carrier.
func manualTask(t *testing.T, s *store.Store, compID, typ string, approve bool, originLoopID string) string {
	t.Helper()
	task, r, err := s.CreateManualTask(store.NewManualTask{ComponentID: compID, Title: "m task",
		Type: typ, By: opActor, RequireApproval: approve, OriginLoopID: originLoopID})
	if err != nil || r != nil {
		t.Fatalf("create task: refusal=%v err=%v", r, err)
	}
	if approve {
		res, r, err := s.DecideTask(task.ID, "approved", opActor, "ok")
		if err != nil || r != nil || !res.Changed {
			t.Fatalf("decide task: refusal=%v err=%v changed=%v", r, err, res.Changed)
		}
	}
	return task.ID
}

func humanTrigger(t *testing.T, s *store.Store, match store.Match, mods ...func(*store.NewEventTrigger)) store.Trigger {
	t.Helper()
	in := store.NewEventTrigger{Name: fmt.Sprintf("tr-%d", triggerSeq.Add(1)),
		Blueprint: "dev-loop", Match: match, MaxRunsPerHour: 100}
	for _, m := range mods {
		m(&in)
	}
	tr, err := s.CreateEventTrigger(opActor, in)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	return tr
}

func firesFor(t *testing.T, s *store.Store, triggerID string) []store.Fire {
	t.Helper()
	fs, err := s.ListEventFires(store.EventFireFilter{TriggerID: triggerID})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func cursor(t *testing.T, s *store.Store) int64 {
	t.Helper()
	n, err := s.AutomationCursor("dispatcher")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func drain(t *testing.T, d *Dispatcher) int {
	t.Helper()
	n, err := d.Drain(ctx())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	return n
}

func ctx() context.Context { return context.Background() }

func pinEventLoop(t *testing.T, s *store.Store, slug, triggerID string, depth int) string {
	t.Helper()
	l, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: "/repo/c0",
		Title: slug, StartedBy: "event", TriggerID: triggerID, AutomationDepth: depth},
		"bp", 1, "raw", `{"route":""}`)
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}

// ── the cursor and the pass ─────────────────────────────────────────────────

func TestAnApprovedGoalStartsOneLoopThroughTheStarter(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	gid := approvedGoal(t, s, pid, "feature", comps)
	if n := drain(t, d); n != 1 {
		t.Fatalf("drain = %d, want 1", n)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fs.calls))
	}
	c := fs.calls[0]
	if c.Slug != AutoSlug(tr.ID, 1) || c.Depth != 1 || c.SubjectID != gid {
		t.Errorf("call = %+v, want slug %s depth 1 subject %s", c, AutoSlug(tr.ID, 1), gid)
	}
	if c.Blueprint != "dev-loop" || c.WorkspaceRoot != "/ws/"+comps[0] || c.Event != store.EventGoalApproved {
		t.Errorf("call routing = %+v — blueprint, workspace and event must ride the request", c)
	}
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireStarted || fires[0].LoopID == "" {
		t.Fatalf("fires = %+v, want one started with a loop named", fires)
	}
}

func TestAlreadyDispatchedEventsAreNeverReRead(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	approvedGoal(t, s, pid, "feature", comps)
	if n := drain(t, d); n != 2 {
		t.Fatalf("drain = %d, want 2", n)
	}
	if len(fs.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fs.calls))
	}
	if n := drain(t, d); n != 0 || len(fs.calls) != 2 {
		t.Fatalf("second drain = %d calls %d — the cursor must not re-read dispatched events", n, len(fs.calls))
	}
}

func TestANewTriggerNeverSeesTheBacklog(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	approvedGoal(t, s, pid, "feature", comps) // the backlog: an event before any trigger exists
	late := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	if n := drain(t, d); n != 1 {
		t.Fatalf("drain = %d, want 1", n)
	}
	if len(fs.calls) != 0 {
		t.Errorf("the late trigger started %+v — from_outbox_id means a trigger never replays history", fs.calls)
	}
	if len(firesFor(t, s, late.ID)) != 0 {
		t.Error("a trigger created after the event wrote a fire row")
	}
	if cursor(t, s) != 1 {
		t.Errorf("cursor = %d, want the event's own id 1", cursor(t, s))
	}
}

func TestAnEventThatMatchesNothingAdvancesTheCursorAndWritesNoFire(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved, GoalType: []string{"bug"}})
	approvedGoal(t, s, pid, "feature", comps)
	if n := drain(t, d); n != 1 {
		t.Fatalf("drain = %d, want 1 — an unmatched event is dispatched (nothing to do), not stuck", n)
	}
	if len(fs.calls) != 0 || len(firesFor(t, s, tr.ID)) != 0 {
		t.Errorf("no match must mean no start and no fire row: calls %d fires %d", len(fs.calls), len(firesFor(t, s, tr.ID)))
	}
	if cursor(t, s) != 1 {
		t.Errorf("cursor = %d, want 1", cursor(t, s))
	}
}

func TestEventsAreHandledInOrderAndTheCursorAdvancesPerEvent(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	d.BatchSize = 2
	pid, comps := seedWorld(t, s, 1)
	humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	var gids []string
	for range 3 {
		gids = append(gids, approvedGoal(t, s, pid, "feature", comps))
	}
	if n := drain(t, d); n != 2 {
		t.Fatalf("first drain = %d, want the batch of 2", n)
	}
	if len(fs.calls) != 2 || fs.calls[0].SubjectID != gids[0] || fs.calls[1].SubjectID != gids[1] {
		t.Fatalf("starter order = %+v, want %q then %q", fs.calls, gids[0], gids[1])
	}
	if n := drain(t, d); n != 1 || len(fs.calls) != 3 || fs.calls[2].SubjectID != gids[2] {
		t.Fatalf("tail: drain %d calls %d — the backlog must keep its order across batches", n, len(fs.calls))
	}
}

func TestDrainStopsWhenTheContextEnds(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := d.Drain(cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n != 0 || len(fs.calls) != 0 || cursor(t, s) != 0 {
		t.Errorf("partial handle: events %d calls %d cursor %d — a cancelled pass starts nothing and moves nothing", n, len(fs.calls), cursor(t, s))
	}
}

func TestTheSameEventNeverStartsTwice(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	ev, err := s.ReadAutomationEvent(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.fire(ctx(), tr, ev); err != nil {
		t.Fatal(err)
	}
	if err := d.fire(ctx(), tr, ev); err != nil {
		t.Fatalf("the losing claim returned %v — a loser returns nil and says nothing", err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("starter calls = %d, want 1 (the claim is the dedup)", len(fs.calls))
	}
	if just := firesFor(t, s, tr.ID); len(just) != 1 {
		t.Fatalf("fire rows = %d, want exactly 1", len(just))
	}
}

// ── the guards ──────────────────────────────────────────────────────────────

func TestADisabledTriggerRecordsSkippedDisabledWithItsReason(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	if _, err := s.SetEventTriggerEnabled(opActor, tr.ID, false, "operator pause for the release"); err != nil {
		t.Fatal(err)
	}
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	if len(fs.calls) != 0 {
		t.Fatal("a disabled trigger started a loop")
	}
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireSkipped ||
		!strings.HasPrefix(fires[0].Reason, "disabled: operator pause") {
		t.Fatalf("fires = %+v, want one skipped/disabled naming the reason — silence is the Do-Not", fires)
	}
	// The same trigger, re-enabled, dispatches the NEXT event (AU8).
	if _, err := s.SetEventTriggerEnabled(opActor, tr.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	if len(fs.calls) != 1 {
		t.Fatalf("after enabling, calls = %d, want 1", len(fs.calls))
	}
}

func TestAnOverlappingFireIsSkippedUntilTheFirstLoopEnds(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	pid, comps := seedWorld(t, s, 1)
	rs := &realStarter{s: s, root: "/repo/c0"}
	d := newDispatcher(s, rs, clk)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	gid := approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	if len(rs.calls) != 1 {
		t.Fatalf("first approval calls = %d, want 1", len(rs.calls))
	}
	approveAgain(t, s, gid) // second event, SAME subject
	drain(t, d)
	fires := firesFor(t, s, tr.ID) // ORDER BY id DESC — newest first
	if len(fires) != 2 || fires[0].Outcome != store.FireSkipped || !strings.HasPrefix(fires[0].Reason, "overlap: loop") {
		t.Fatalf("fires = %+v, want the newest skipped as an overlap naming the running loop", fires)
	}
	if !strings.Contains(fires[0].Reason, "is still running") {
		t.Errorf("overlap reason %q should name the status too", fires[0].Reason)
	}
	// The loop ends; a third event on the same subject starts.
	if err := s.SetLoopStatus(store.Scope{LoopSlug: fires[1].LoopSlug, ComponentRoot: "/repo/c0"}, "done"); err != nil {
		t.Fatal(err)
	}
	approveAgain(t, s, gid)
	drain(t, d)
	if len(rs.calls) != 2 {
		t.Fatalf("after the first loop ended, calls = %d, want 2 — overlap guards the living, not the dead", len(rs.calls))
	}
}

func TestStillGoingAgreesBetweenSQLAndTriggerPackage(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	pid, comps := seedWorld(t, s, 1)
	rs := &realStarter{s: s, root: "/repo/c0"}
	d := newDispatcher(s, rs, clk)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	statuses := []string{"running", "waiting", "blocked", "done", "aborted", "idle"}
	var gids []string
	for range statuses {
		gids = append(gids, approvedGoal(t, s, pid, "feature", comps))
	}
	drain(t, d)
	if len(rs.calls) != len(statuses) {
		t.Fatalf("calls = %d, want 6 distinct subjects started", len(rs.calls))
	}
	fires := firesFor(t, s, tr.ID) // DESC: reverse for start order
	if len(fires) != len(statuses) {
		t.Fatalf("fires = %d, want 6", len(fires))
	}
	for i, f := range fires { // DESC, so index i belongs to statuses[len-1-i]
		want := statuses[len(fires)-1-i]
		if err := s.SetLoopStatus(store.Scope{LoopSlug: f.LoopSlug, ComponentRoot: "/repo/c0"}, want); err != nil {
			t.Fatalf("status %s on %s: %v", want, f.LoopSlug, err)
		}
	}
	// One more event per subject: the guard must skip exactly when S20's
	// StillGoing says the loop is still going — the rule is imported by
	// test, never re-decided here.
	for i, gid := range gids {
		approveAgain(t, s, gid)
		drain(t, d)
		fresh := firesFor(t, s, tr.ID)[0] // the newest is this subject's second
		if fresh.SubjectID != gid {
			t.Fatalf("newest fire is for %q, want %q", fresh.SubjectID, gid)
		}
		want := trigger.StillGoing(statuses[i])
		got := fresh.Outcome == store.FireSkipped
		if got != want {
			t.Errorf("loop status %q: overlap=%v, StillGoing=%v — the SQL IN-list and S20 drifted", statuses[i], got, want)
		}
	}
}

func TestADepthFourEventIsRefusedAndRecorded(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventTaskCreated})
	// A depth-3 loop's agent files a task: the event is depth 3, the loop
	// this fire starts records 4 — AU5's last legal generation.
	manualTask(t, s, comps[0], "chore", false, pinEventLoop(t, s, "origin-3", "tr-prev", 3))
	drain(t, d)
	if len(fs.calls) != 1 || fs.calls[0].Depth != 4 {
		t.Fatalf("calls = %+v, want one with Depth 4", fs.calls)
	}
	// Depth 4 origin → event depth 4 → refused, no start, reason named.
	manualTask(t, s, comps[0], "chore", false, pinEventLoop(t, s, "origin-4", "tr-prev", 4))
	drain(t, d)
	if len(fs.calls) != 1 {
		t.Fatalf("a depth-4 event started %+v — the line is hard", fs.calls)
	}
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 2 || fires[0].Outcome != store.FireRefused || !strings.HasPrefix(fires[0].Reason, "depth-exceeded:") {
		t.Fatalf("fires = %+v, want the newest refused depth-exceeded", fires)
	}
	if !strings.Contains(fires[0].Reason, `caused by trigger "tr-prev"`) {
		t.Errorf("the refusal must name the cause it blames: %q", fires[0].Reason)
	}
}

func TestDepthAndCauseRideTheEmittingWrites(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	origin := pinEventLoop(t, s, "causing", "tr-real", 2)
	manualTask(t, s, comps[0], "chore", false, origin)
	evs, err := s.ReadAutomationEvents(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want the one task.created", len(evs))
	}
	if evs[0].Depth != 2 || evs[0].CausedByTrigger != "tr-real" {
		t.Errorf("agent event depth=%d cause=%q, want 2 / tr-real — derived from the write, never passed", evs[0].Depth, evs[0].CausedByTrigger)
	}
	// The human door carries the honest zero: a person approving a task
	// that a deep loop filed is NOT that loop causing a chain (AU5/AU1).
	manualTask(t, s, comps[0], "chore", true, origin)
	evs, err = s.ReadAutomationEvents(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("events = %d, want created+created+approved", len(evs))
	}
	last := evs[2]
	if last.Event != store.EventTaskApproved || last.Depth != 0 || last.CausedByTrigger != "" {
		t.Errorf("human approval event: %s depth=%d cause=%q, want task.approved 0 \"\" — no human decision chains automation", last.Event, last.Depth, last.CausedByTrigger)
	}
}

// ── routing, target, limits ─────────────────────────────────────────────────

func TestTaskTypeToBlueprintIsJustATrigger(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	_, comps := seedWorld(t, s, 1)
	a := humanTrigger(t, s, store.Match{Event: store.EventTaskApproved, TaskType: []string{"bug"}},
		func(in *store.NewEventTrigger) { in.Blueprint = "fix-bug" })
	b := humanTrigger(t, s, store.Match{Event: store.EventTaskApproved, TaskType: []string{"feature"}},
		func(in *store.NewEventTrigger) { in.Blueprint = "dev-loop" })
	manualTask(t, s, comps[0], "bug", true, "")
	drain(t, d)
	if len(fs.calls) != 1 || fs.calls[0].Blueprint != "fix-bug" || fs.calls[0].TriggerID != a.ID {
		t.Fatalf("calls = %+v, want only A's fix-bug for a bug task", fs.calls)
	}
	if len(firesFor(t, s, b.ID)) != 0 {
		t.Error("the feature trigger wrote a fire row for a bug task")
	}
	manualTask(t, s, comps[0], "feature", true, "")
	drain(t, d)
	if len(fs.calls) != 2 || fs.calls[1].Blueprint != "dev-loop" || fs.calls[1].TriggerID != b.ID {
		t.Fatalf("calls = %+v, want B's dev-loop for a feature task", fs.calls)
	}
}

func TestAMultiComponentGoalWithoutATriggerComponentIsRefused(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 3)
	// Two-component goal, trigger names no component → nobody can say
	// where the work lives; a guess would run it in the wrong repo.
	trNo := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved, GoalType: []string{"feature"}})
	approvedGoal(t, s, pid, "feature", comps[:2])
	drain(t, d)
	fires := firesFor(t, s, trNo.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireRefused || !strings.HasPrefix(fires[0].Reason, "no-target-component:") {
		t.Fatalf("fires = %+v, want refused no-target-component", fires)
	}
	// The started arm, measured shape: a component-scoped trigger admits
	// only events naming the same component (T3's Admits, fail closed) —
	// so the trigger-rooted case is a SINGLE-component goal whose event
	// carries the trigger's component.
	trC := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved, GoalType: []string{"bug"}},
		func(in *store.NewEventTrigger) { in.ComponentID = comps[1] })
	approvedGoal(t, s, pid, "bug", comps[1:2])
	drain(t, d)
	if len(fs.calls) != 1 || fs.calls[0].WorkspaceRoot != "/ws/"+comps[1] {
		t.Fatalf("calls = %+v, want one rooted at the trigger's component", fs.calls)
	}
	if f := firesFor(t, s, trC.ID); len(f) != 1 || f[0].Outcome != store.FireStarted {
		t.Fatalf("trigger-named fires = %+v, want one started", f)
	}
	// No component on the trigger but the event names ONE component → the
	// event's own component is the target (AU10's fallback order).
	trE := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved, GoalType: []string{"chore"}})
	approvedGoal(t, s, pid, "chore", comps[0:1])
	drain(t, d)
	if len(fs.calls) != 2 || fs.calls[1].WorkspaceRoot != "/ws/"+comps[0] {
		t.Fatalf("calls = %+v, want the second rooted at the EVENT's component", fs.calls)
	}
	if f := firesFor(t, s, trE.ID); len(f) != 1 || f[0].Outcome != store.FireStarted {
		t.Fatalf("event-fallback fires = %+v, want one started", f)
	}
	// A root reader that fails is workspace-invalid, not a crash and not a
	// silent pass.
	d2 := newDispatcher(s, fs, clk)
	d2.ResolveRoot = func(string) (string, error) { return "", errors.New("component vanished from the registry") }
	trErr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved, GoalType: []string{"refactor"}},
		func(in *store.NewEventTrigger) { in.ComponentID = comps[0] })
	approvedGoal(t, s, pid, "refactor", comps[0:1])
	n, derr := d2.Drain(ctx())
	if derr != nil || n != 1 { // business answers are not errors
		t.Fatalf("drain = %d", n)
	}
	fires = firesFor(t, s, trErr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireRefused || !strings.HasPrefix(fires[0].Reason, "workspace-invalid: ") {
		t.Fatalf("fires = %+v, want refused workspace-invalid quoting the reader", fires)
	}
}

func TestRunsPerHourIsAHardBoundAndRollsWithTheClock(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved},
		func(in *store.NewEventTrigger) { in.MaxRunsPerHour = 2 })
	for range 3 {
		approvedGoal(t, s, pid, "feature", comps)
	}
	drain(t, d)
	if len(fs.calls) != 2 {
		t.Fatalf("calls = %d, want the hour's two", len(fs.calls))
	}
	fires := firesFor(t, s, tr.ID)
	if fires[0].Outcome != store.FireLimited || !strings.HasPrefix(fires[0].Reason, "max-runs-per-hour:") {
		t.Fatalf("newest fire = %+v, want limited max-runs-per-hour", fires[0])
	}
	// The limit is a window, not a verdict: an hour later it runs again.
	clk.Advance(61 * time.Minute)
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	if len(fs.calls) != 3 {
		t.Fatalf("after advancing the clock, calls = %d, want 3 — the window rolls", len(fs.calls))
	}
}

// seedSpend pins an event loop for the trigger and files one step per cost.
func seedSpend(t *testing.T, s *store.Store, slug, triggerID string, costs ...model.Cost) {
	t.Helper()
	pinEventLoop(t, s, slug, triggerID, 0)
	for i, c := range costs {
		st, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: "/repo/c0"},
			store.NewStep{TaskID: fmt.Sprintf("T%d", i), Phase: "build", Agent: "coder", Attempt: 1,
				Status: store.StepDone, Source: "lane"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetStepCost(st.ID, c); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheCostLimitHonoursKnownPartialAndUnknown(t *testing.T) {
	known := func(usd float64) model.Cost { return model.Cost{State: model.CostKnown, USD: usd} }
	floor := func(usd float64) model.Cost { return model.Cost{State: model.CostPartial, USDAtLeast: usd} }
	unknown := func() model.Cost { return model.Cost{State: model.CostUnknown} }
	capOf := func(usd float64) func(*store.NewEventTrigger) {
		return func(in *store.NewEventTrigger) { in.MaxCostPerDayUSD = &usd }
	}
	t.Run("known spend reaches the cap", func(t *testing.T) {
		clk := newFakeClock()
		s := openStore(t, clk.Now)
		fs := &fakeStarter{}
		d := newDispatcher(s, fs, clk)
		pid, comps := seedWorld(t, s, 1)
		tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved}, capOf(2))
		seedSpend(t, s, "spend-a", tr.ID, known(3))
		approvedGoal(t, s, pid, "feature", comps)
		drain(t, d)
		if len(fs.calls) != 0 {
			t.Fatal("a trigger at $3 of $2 spent started a loop")
		}
		fires := firesFor(t, s, tr.ID)
		if len(fires) != 1 || fires[0].Outcome != store.FireLimited || !strings.HasPrefix(fires[0].Reason, "max-cost-per-day:") {
			t.Fatalf("fires = %+v, want limited max-cost-per-day", fires)
		}
	})
	t.Run("a floor that reaches the cap proves it", func(t *testing.T) {
		clk := newFakeClock()
		s := openStore(t, clk.Now)
		fs := &fakeStarter{}
		d := newDispatcher(s, fs, clk)
		pid, comps := seedWorld(t, s, 1)
		tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved}, capOf(2))
		seedSpend(t, s, "spend-b", tr.ID, floor(3))
		approvedGoal(t, s, pid, "feature", comps)
		drain(t, d)
		fires := firesFor(t, s, tr.ID)
		if len(fires) != 1 || fires[0].Outcome != store.FireLimited || fires[0].CostNote != "cost-floor" {
			t.Fatalf("fires = %+v, want limited with cost_note=cost-floor", fires)
		}
	})
	t.Run("unknown cannot be capped and is never summed as zero", func(t *testing.T) {
		clk := newFakeClock()
		s := openStore(t, clk.Now)
		fs := &fakeStarter{}
		d := newDispatcher(s, fs, clk)
		pid, comps := seedWorld(t, s, 1)
		tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved}, capOf(1))
		// $0.50 measured + an unpriced step: the cap CANNOT be proven
		// reached, so the fire runs — and says so. (Summing the unknown as
		// $0 would also start it — with the honest record the difference
		// is the cost_note the person reads.)
		seedSpend(t, s, "spend-c", tr.ID, floor(0.5), unknown())
		approvedGoal(t, s, pid, "feature", comps)
		drain(t, d)
		if len(fs.calls) != 1 {
			t.Fatal("unprovable spend blocked a fire — unknown is not over-cap either")
		}
		fires := firesFor(t, s, tr.ID)
		if len(fires) != 1 || fires[0].Outcome != store.FireStarted || fires[0].CostNote != "cost-unknown" {
			t.Fatalf("fires = %+v, want started with cost_note=cost-unknown", fires)
		}
	})
	t.Run("no cap no note", func(t *testing.T) {
		clk := newFakeClock()
		s := openStore(t, clk.Now)
		fs := &fakeStarter{}
		d := newDispatcher(s, fs, clk)
		pid, comps := seedWorld(t, s, 1)
		tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
		seedSpend(t, s, "spend-d", tr.ID, known(99))
		approvedGoal(t, s, pid, "feature", comps)
		drain(t, d)
		fires := firesFor(t, s, tr.ID)
		if len(fires) != 1 || fires[0].Outcome != store.FireStarted || fires[0].CostNote != "" {
			t.Fatalf("fires = %+v, want started with no cost_note — no cap, no commentary", fires)
		}
	})
}

// ── the starter's answers ───────────────────────────────────────────────────

func TestAStarterRefusalIsRecordedVerbatim(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{next: func(EventStartRequest) (StartResult, error) {
		return StartResult{}, &Refusal{Reason: "requires-unmet", Sentence: "needs an approved goal"}
	}}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireRefused ||
		fires[0].Reason != "requires-unmet: needs an approved goal" {
		t.Fatalf("fires = %+v, want the word and the sentence VERBATIM", fires)
	}
}

func TestAnUnclassifiedStarterErrorIsCouldNotSayAndNotRetried(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{next: func(EventStartRequest) (StartResult, error) {
		return StartResult{}, errors.New("boom")
	}}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireCouldNotSay || fires[0].Reason != "start-error: boom" {
		t.Fatalf("fires = %+v, want could-not-say start-error: boom", fires)
	}
	// No retry: a second pass does not re-call the starter. The cursor has
	// moved and the row is decided — AU12's third answer is final.
	drain(t, d)
	if len(fs.calls) != 1 {
		t.Fatalf("starter calls = %d, want 1 — the dispatcher never retries a fire", len(fs.calls))
	}
}

func TestAnUnknownRefusalReasonIsNotTrusted(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	fs := &fakeStarter{next: func(EventStartRequest) (StartResult, error) {
		return StartResult{}, &Refusal{Reason: "made-up", Sentence: "invented vocabulary"}
	}}
	d := newDispatcher(s, fs, clk)
	pid, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: store.EventGoalApproved})
	approvedGoal(t, s, pid, "feature", comps)
	drain(t, d)
	fires := firesFor(t, s, tr.ID)
	if len(fires) != 1 || fires[0].Outcome != store.FireCouldNotSay ||
		!strings.HasPrefix(fires[0].Reason, "start-error: unclassified refusal made-up") {
		t.Fatalf("fires = %+v, want could-not-say — an unlisted code is never believed", fires)
	}
}

// ── the bridges and the pin ─────────────────────────────────────────────────

func TestEveryEventOutcomeMapsOntoAnS20Outcome(t *testing.T) {
	want := map[string]trigger.Outcome{
		store.FireStarted:     trigger.OutcomeFired,
		store.FireLimited:     trigger.OutcomeDidNotFire,
		store.FireRefused:     trigger.OutcomeRefused,
		store.FireCouldNotSay: trigger.OutcomeCouldNotSay,
	}
	if got, ok := S20Outcome(store.FireStarted, ""); !ok || got != trigger.OutcomeFired || !got.Valid() {
		t.Errorf("started → %q ok=%v, want fired", got, ok)
	}
	for outcome, codes := range store.EventFireReasons {
		for _, code := range codes {
			got, ok := S20Outcome(outcome, code)
			if !ok || !got.Valid() {
				t.Fatalf("(%s, %s) → %q ok=%v — the mapping must be total over the closed sets", outcome, code, got, ok)
			}
			expect := want[outcome]
			if outcome == store.FireSkipped {
				switch code {
				case "disabled":
					expect = trigger.OutcomeDidNotFire
				case "overlap":
					expect = trigger.OutcomeSkipped
				default:
					t.Fatalf("unexpected skipped code %q", code)
				}
			}
			if got != expect {
				t.Errorf("(%s, %s) → %q, want %q", outcome, code, got, expect)
			}
		}
	}
	if _, ok := S20Outcome("started-but-invented", ""); ok {
		t.Error("an outcome outside the five mapped — ok=false is the only honest answer")
	}
}

func TestTheDispatcherPackageNeverSleepsOrReadsTheWallClock(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	nowTotal := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		seen++
		// The needles are split so this test does not match itself: a grep
		// that contains its own pattern is a grep that can never pass.
		sleepNeedle := "time." + "Sleep("
		nowNeedle := "time." + "Now("
		if strings.Contains(string(src), sleepNeedle) {
			t.Errorf("%s sleeps — AU8's pass is driven by injected time only", e.Name())
		}
		if !strings.HasSuffix(e.Name(), "_test.go") {
			nowTotal += strings.Count(string(src), nowNeedle)
			if e.Name() == "dispatcher.go" {
				// T6's bargain: Run may own real time, exactly once, in
				// defaultClock — the grep pins the function, not just the file.
				if !strings.Contains(string(src), "func defaultClock() "+
					"time.Time { return time."+"Now() }") {
					t.Errorf("dispatcher.go's one wall-clock read is not the single-expression defaultClock")
				}
			}
		}
	}
	if nowTotal != 1 {
		t.Errorf("%d wall-clock reads in production files — exactly one (defaultClock) is the bargain", nowTotal)
	}
	if seen < 7 {
		t.Fatalf("only %d files seen — the grep tested nothing", seen)
	}
}
