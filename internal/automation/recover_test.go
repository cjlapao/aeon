package automation

// recover_test.go — AU13's table, row by row. A crash is simulated exactly
// as it happens in the field: ClaimEventFire without DecideEventFire, then a
// NEW Dispatcher over the same store (the restarted daemon). Nothing here
// sleeps; the Run test drives the tick by hand.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// crashClaim leaves a claim open — the field state between claim and start.
func crashClaim(t *testing.T, s *store.Store, tr store.Trigger, ev store.AutomationEvent, clk *fakeClock) store.Fire {
	t.Helper()
	f, fresh, err := s.ClaimEventFire(store.NewEventFire{
		TriggerID: tr.ID, OutboxID: ev.ID,
		SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID, Depth: ev.Depth,
	}, clk.Now())
	if err != nil || !fresh {
		t.Fatalf("crash claim: fresh=%v err=%v", fresh, err)
	}
	return f
}

// lastEvent reads the newest outbox row — the tests emit exactly one or two.
func lastEvent(t *testing.T, s *store.Store) store.AutomationEvent {
	t.Helper()
	evs, err := s.ReadAutomationEvents(0, 1000)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events: n=%d err=%v", len(evs), err)
	}
	return evs[len(evs)-1]
}

func TestAnOpenClaimWithALoopIsCompletedAsStarted(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	// The loop the original start committed lives at the root ResolveRoot
	// names, under the deterministic slug AutoSlug would have passed.
	slug := AutoSlug(tr.ID, ev.ID)
	pinned, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: "/ws/" + comps[0],
		Title: "the crashed start", StartedBy: "event", TriggerID: tr.ID, AutomationDepth: ev.Depth + 1},
		"dev-loop", 1, "raw", `{"route":""}`)
	if err != nil {
		t.Fatal(err)
	}
	st := &fakeStarter{}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Open != 1 || sum.Started != 1 || st.calls != nil {
		t.Fatalf("summary %+v starter calls %d — row 1 completes the record and NEVER starts", sum, len(st.calls))
	}
	fs := firesFor(t, s, tr.ID)
	if len(fs) != 1 || fs[0].Outcome != store.FireStarted || fs[0].LoopID != pinned.ID || fs[0].LoopSlug != slug {
		t.Fatalf("fire rows %+v — the decided record must carry the loop that exists", fs)
	}
}

// seedProjectID reads back the one project seedWorld created.
func seedProjectID(t *testing.T, s *store.Store) string {
	t.Helper()
	ps, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(ps) != 1 {
		t.Fatalf("projects: n=%d err=%v", len(ps), err)
	}
	return ps[0].ID
}

func TestAnOpenClaimWithNoLoopIsReRunOnce(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	st := &fakeStarter{}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Open != 1 || sum.Rerun != 1 || len(st.calls) != 1 {
		t.Fatalf("summary %+v calls %d — one claim, one re-run, one start", sum, len(st.calls))
	}
	if st.calls[0].Slug != AutoSlug(tr.ID, ev.ID) {
		t.Errorf("re-run slug %q — the crash recovery must name the SAME slug the original start would have", st.calls[0].Slug)
	}
	fs := firesFor(t, s, tr.ID)
	if len(fs) != 1 || fs[0].Outcome != store.FireStarted {
		t.Fatalf("fire rows %+v", fs)
	}
}

func TestAnOpenClaimOnADisabledTriggerIsSkipped(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	clk.Advance(time.Hour) // the disable lands after the claim
	if _, err := s.SetEventTriggerEnabled(opActor, tr.ID, false, "sunset"); err != nil {
		t.Fatal(err)
	}
	st := &fakeStarter{}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 1 || len(st.calls) != 0 {
		t.Fatalf("summary %+v calls %d — stale authority must never start anything", sum, len(st.calls))
	}
	fs := firesFor(t, s, tr.ID)
	if fs[0].Outcome != store.FireSkipped || !strings.HasPrefix(fs[0].Reason, "disabled") {
		t.Fatalf("fire %+v — skipped with a spoken disabled reason", fs[0])
	}
}

func TestAnOpenClaimOnAnEditedTriggerIsCouldNotSay(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	clk.Advance(time.Hour) // the edit lands after the claim: the definition changed under it
	cap50 := 50
	if _, err := s.UpdateEventTrigger(opActor, tr.ID, store.EventTriggerEdit{MaxRunsPerHour: &cap50}); err != nil {
		t.Fatal(err)
	}
	st := &fakeStarter{}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.CouldNotSay != 1 || len(st.calls) != 0 {
		t.Fatalf("summary %+v calls %d — an edited definition is never run", sum, len(st.calls))
	}
	fs := firesFor(t, s, tr.ID)
	if fs[0].Outcome != store.FireCouldNotSay || !strings.HasPrefix(fs[0].Reason, "trigger-edited-since-claim") {
		t.Fatalf("fire %+v", fs[0])
	}
}

func TestAnOpenClaimWhoseTriggerIsGoneIsRefusedAsSubjectMissing(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	// Both unreadable arms: the trigger never existed, and the outbox row
	// does not exist. The ledger has no FK to lean on — that is the point:
	// recovery re-reads the subjects or refuses in their names.
	ghost, _, err := s.ClaimEventFire(store.NewEventFire{TriggerID: "tr-ghost", OutboxID: ev.ID,
		SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID}, clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	orphan, _, err := s.ClaimEventFire(store.NewEventFire{TriggerID: "tr-ghost2", OutboxID: 999999,
		SubjectKind: "goal", SubjectID: "nope"}, clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = ghost
	st := &fakeStarter{}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Refused != 2 || len(st.calls) != 0 {
		t.Fatalf("summary %+v calls %d", sum, len(st.calls))
	}
	fs := firesFor(t, s, "tr-ghost")
	if fs[0].Reason == "" || !strings.Contains(fs[0].Reason, "tr-ghost") || !strings.Contains(fs[0].Reason, "outbox") {
		t.Fatalf("refusal %+v — subject-missing names BOTH vanished ids", fs[0])
	}
	fs2 := firesFor(t, s, "tr-ghost2")
	if fs2[0].Outcome != store.FireRefused || !strings.HasPrefix(fs2[0].Reason, "subject-missing") {
		t.Fatalf("orphan fire %+v — the outbox arm refuses too", fs2[0])
	}
	_ = orphan
}

func TestReconcileIsIdempotent(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	st := &fakeStarter{}
	d := newDispatcher(s, st, clk)
	if sum, err := d.Reconcile(ctx()); err != nil || sum.Open != 1 {
		t.Fatalf("first reconcile %+v %v", sum, err)
	}
	sum, err := d.Reconcile(ctx())
	if err != nil || sum.Open != 0 || len(st.calls) != 1 {
		t.Fatalf("second reconcile %+v %v calls %d — a decided fire is never re-processed", sum, err, len(st.calls))
	}
}

func TestDrainAfterReconcileNeverRestartsAClaimedEvent(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	st := &fakeStarter{}
	d := newDispatcher(s, st, clk)
	if _, err := d.Reconcile(ctx()); err != nil {
		t.Fatal(err)
	}
	// The cursor never passed this event (the crash beat it), so Drain
	// re-reads it — the claim is what makes the replay harmless.
	if n := drain(t, d); n != 1 {
		t.Fatalf("drain read %d events — the cursor was still behind it", n)
	}
	if fs := firesFor(t, s, tr.ID); len(fs) != 1 {
		t.Fatalf("%d fire rows — replay wrote a second one", len(fs))
	}
	if len(st.calls) != 1 {
		t.Fatalf("%d starter calls — the claim's loser must not start again", len(st.calls))
	}
}

func TestAReconcileThatHitsAnExistingLoopSurvivesAStarterThatWouldHaveFailed(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	approvedGoal(t, s, seedProjectID(t, s), "feature", comps)
	ev := lastEvent(t, s)
	crashClaim(t, s, tr, ev, clk)
	slug := AutoSlug(tr.ID, ev.ID)
	if _, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: "/ws/" + comps[0],
		Title: "committed before the crash", StartedBy: "event", TriggerID: tr.ID},
		"dev-loop", 1, "raw", `{"route":""}`); err != nil {
		t.Fatal(err)
	}
	st := &fakeStarter{next: func(EventStartRequest) (StartResult, error) {
		return StartResult{}, context.DeadlineExceeded // any failure: row 1 must not see it
	}}
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Started != 1 || len(st.calls) != 0 {
		t.Fatalf("summary %+v calls %d — the existing loop is completed by LOOKUP, not by another start", sum, len(st.calls))
	}
}

// chanStarter signals each call without sharing a slice across goroutines —
// the Run test runs under -race.
type chanStarter struct {
	calls chan EventStartRequest
}

func (c *chanStarter) Start(_ context.Context, r EventStartRequest) (StartResult, error) {
	c.calls <- r
	return StartResult{LoopID: "L" + r.Slug, Slug: r.Slug}, nil
}

func TestRunReconcilesThenDrainsOnEachTickAndStopsOnCancel(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	projectID := seedProjectID(t, s)
	tr := humanTrigger(t, s, store.Match{Event: "goal.approved"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	goalID := approvedGoal(t, s, projectID, "feature", comps)
	ev1 := lastEvent(t, s)
	crashClaim(t, s, tr, ev1, clk)
	// The frontier sits AT ev1 — the crash beat the decision, not the read.
	if err := s.AdvanceAutomationCursor("dispatcher", ev1.ID, clk.Now()); err != nil {
		t.Fatal(err)
	}
	approveAgain(t, s, goalID) // ev2 sits above the frontier, waiting for a tick

	st := &chanStarter{calls: make(chan EventStartRequest, 8)}
	tick := make(chan time.Time)
	d := &Dispatcher{Store: s, Starter: st, Clock: clk.Now,
		ResolveRoot: func(id string) (string, error) { return "/ws/" + id, nil },
		Tick:        tick, BatchSize: 10}
	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(runCtx) }()

	// Reconcile's re-run answers the open claim before any tick arrives.
	select {
	case r := <-st.calls:
		if r.Slug != AutoSlug(tr.ID, ev1.ID) {
			t.Fatalf("reconcile started %q — it must re-run the claimed slug", r.Slug)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run never reconciled the open claim")
	}

	// Run drains once right after Reconcile, with no tick sent, and that first
	// Drain may already have taken the event above the frontier by the time
	// this goroutine reads the cursor — so the cursor is only pinned from
	// BELOW: it never sits behind the frontier. (Pinning it AT the frontier
	// raced the first Drain and failed under load.) Then a tick; the second
	// event is dispatched exactly once, by whichever pass reaches it first.
	if n := cursor(t, s); n < ev1.ID {
		t.Fatalf("cursor %d — the cursor must never sit behind the frontier at %d", n, ev1.ID)
	}
	tick <- time.Time{}
	select {
	case r := <-st.calls:
		ev2 := lastEvent(t, s)
		if r.Slug != AutoSlug(tr.ID, ev2.ID) {
			t.Fatalf("tick started %q, want the second event's slug", r.Slug)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tick never dispatched the new event")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Run returned %v — cancel must answer context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored cancel — the ticker owns the daemon's shutdown join")
	}
}

// TestReconcileOverAThousandOpenClaims is the DoD's measured line: the
// number is LOGGED, not promised — a generous ceiling only catches a
// catastrophe (an N+1 that quadratic-ates), not a regression of milliseconds.
func TestReconcileOverAThousandOpenClaims(t *testing.T) {
	clk := newFakeClock()
	s := openStore(t, clk.Now)
	_, comps := seedWorld(t, s, 1)
	tr := humanTrigger(t, s, store.Match{Event: "task.created"}, func(in *store.NewEventTrigger) {
		in.ComponentID = comps[0]
	})
	const n = 1000
	// n tasks, n task.created events, n open claims — the worst crash day.
	for range n {
		manualTask(t, s, comps[0], "chore", false, "")
	}
	evs, err := s.ReadAutomationEvents(0, n+10)
	if err != nil || len(evs) != n {
		t.Fatalf("events n=%d err=%v — want %d task.created rows", len(evs), err, n)
	}
	for _, ev := range evs {
		if _, _, err := s.ClaimEventFire(store.NewEventFire{TriggerID: tr.ID, OutboxID: ev.ID,
			SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID, Depth: ev.Depth}, clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	st := &fakeStarter{}
	wall := time.Now() // REAL wall time on purpose: this line is a measurement
	sum, err := newDispatcher(s, st, clk).Reconcile(ctx())
	elapsed := time.Since(wall)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reconciled %d open claims in %s (%+v)", sum.Open, elapsed.Round(time.Millisecond), sum)
	if sum.Open != n || sum.Rerun != n {
		t.Fatalf("summary %+v — every claim answered once through execute", sum)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("reconcile of %d claims took %s — something turned quadratic", n, elapsed)
	}
}
