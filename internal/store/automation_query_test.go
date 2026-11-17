package store

// automation_query_test.go — S40 T5's three dispatcher reads, tested at the
// door they live behind: spend (AU11's three-state honesty), the hourly
// count (started + open only, inside the window), and the overlap (S20's
// StillGoing set copied into SQL — the agreement with trigger.StillGoing is
// pinned on the automation side, here it is the SQL's own behaviour).

import (
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// movableClock lets ONE store speak two eras in the same test: the store's
// timestamps come from its clock, so aging a row honestly means moving the
// clock, not editing rows.
type movableClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *movableClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func openAgedStore(t *testing.T, at time.Time) (*Store, *movableClock) {
	t.Helper()
	c := &movableClock{t: at}
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, c
}

// pinEventLoop files a loop as an event fire would have: the same door
// (CreatePinnedLoop) with the provenance the starter passes.
func pinEventLoop(t *testing.T, s *Store, root, slug, triggerID string) Loop {
	t.Helper()
	l, err := s.CreatePinnedLoop(NewLoop{Slug: slug, ComponentRoot: root, Title: slug,
		StartedBy: "event", TriggerID: triggerID}, "bp-q", 1, "raw bytes", `{"route":""}`)
	if err != nil {
		t.Fatalf("pin %s: %v", slug, err)
	}
	return l
}

// addStep files one step with the cost state under test (SetStepCost is the
// store's own cost door; a never-priced step is `unknown` by the schema
// default and needs no call at all).
func addStep(t *testing.T, s *Store, root, slug string, state model.CostState, usd float64) {
	t.Helper()
	sc := Scope{LoopSlug: slug, ComponentRoot: root}
	st, err := s.CreateStep(sc, NewStep{TaskID: "T1", Phase: "build", Agent: "coder", Attempt: 1, Status: StepDone, Source: "lane"})
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	// One call per state, the fields encodeCost expects: known rides USD,
	// partial rides USDAtLeast (the floor goes in cost_usd), unknown carries
	// no number at all.
	cost := model.Cost{State: state}
	switch state {
	case model.CostKnown:
		cost.USD = usd
	case model.CostPartial:
		cost.USDAtLeast = usd
	}
	if err := s.SetStepCost(st.ID, cost); err != nil {
		t.Fatalf("cost: %v", err)
	}
}

func TestTriggerSpendNeverCountsUnknownAsZero(t *testing.T) {
	base := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	s, clk := openAgedStore(t, base)
	_, compID := seedAutomationWorld(t, s)
	comp, err := s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	pinEventLoop(t, s, comp.Root, "sp1", "tr1")
	addStep(t, s, comp.Root, "sp1", model.CostKnown, 1.00)
	addStep(t, s, comp.Root, "sp1", model.CostPartial, 0.50)
	addStep(t, s, comp.Root, "sp1", model.CostUnknown, 0)

	sp, err := s.EventTriggerSpend("tr1", base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sp.USD != 1.50 {
		t.Errorf("USD = %v, want 1.50 (known 1.00 + partial floor 0.50; the unknown step is NEVER summed as zero)", sp.USD)
	}
	if !sp.Floor {
		t.Error("Floor = false, want true — a partial step means the total is a floor")
	}
	if !sp.Unknown {
		t.Error("Unknown = false, want true — AU11's third state must survive the sum")
	}

	// The unknown step gone, the third state is honestly absent.
	if _, err := s.w.Exec(`DELETE FROM step WHERE cost_state='unknown'`); err != nil {
		t.Fatal(err)
	}
	sp, err = s.EventTriggerSpend("tr1", base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Unknown {
		t.Error("Unknown = true with no unknown step")
	}

	// Another trigger's loops and a pre-window loop are excluded: age the
	// clock, pin a second event loop under tr2, then check tr1's window.
	clk.Advance(30 * time.Hour)
	pinEventLoop(t, s, comp.Root, "sp2", "tr2") // other trigger
	addStep(t, s, comp.Root, "sp2", model.CostKnown, 9)
	// and a tr1 loop started before the window: age back is not possible, so
	// the window itself moves past sp1 instead — querying since AFTER sp1's
	// started_at excludes it entirely.
	sp, err = s.EventTriggerSpend("tr1", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sp.USD != 0 || sp.Floor || sp.Unknown {
		t.Errorf("a window after every tr1 loop still sees %+v — started_at >= ? must exclude it, and another trigger's $9 must never appear", sp)
	}
	sp2, err := s.EventTriggerSpend("tr2", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sp2.USD != 9 {
		t.Errorf("tr2's spend = %v, want 9 — the trigger_id filter must find its own rows", sp2.USD)
	}
}

func TestCountRecentFiresCountsStartedAndOpenOnlyInTheWindow(t *testing.T) {
	s := openTemp(t)
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	seed := func(trig string, idOff int64, outcome, at string) int64 {
		t.Helper()
		d := EventFireDecision{}
		if outcome != "" {
			d.Outcome = outcome
			switch outcome {
			case FireStarted:
				d.LoopID, d.LoopSlug = "loop-"+at, "slug"
			case FireRefused:
				d.Reason = "subject-missing" // a legal code — validateDecision checks before any SQL
			case FireSkipped:
				d.Reason = "disabled"
			}
		}
		nf := NewEventFire{TriggerID: trig, OutboxID: 4000 + idOff, SubjectKind: "goal", SubjectID: "g1"}
		f, _, err := s.RecordEventFire(nf, d, now)
		if err != nil {
			t.Fatalf("record %s: %v", outcome, err)
		}
		return f.ID
	}
	started := seed("trC", 1, FireStarted, "now")
	_ = started
	refused := seed("trC", 2, FireRefused, "now")
	_ = refused
	skipped := seed("trC", 3, FireSkipped, "now")
	_ = skipped
	// An open claim: RecordEventFire decides at once, so the NULL arm rides
	// the claim door itself.
	open, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "trC", OutboxID: 4004, SubjectKind: "goal", SubjectID: "g1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	// Outside the window: claimed an hour before the cutoff.
	old, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "trC", OutboxID: 4005, SubjectKind: "goal", SubjectID: "g1"}, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_ = old

	n, err := s.CountRecentEventFires("trC", now.Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 (started + open; refused/skipped spent nothing, the hour-old claim is outside the window)", n)
	}
	n, err = s.CountRecentEventFires("trC", now.Add(-time.Hour), open.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("count with the caller's own claim excluded = %d, want 1 (a claim must not reserve a slot against itself)", n)
	}
}

func TestOverlappingFireFollowsS20sStillGoingRule(t *testing.T) {
	s := openTemp(t)
	_, compID := seedAutomationWorld(t, s)
	comp, err := s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	var ob int64
	startedFire := func(slug, status, subject, trig string) {
		t.Helper()
		l := pinEventLoop(t, s, comp.Root, slug, trig)
		if err := s.SetLoopStatus(Scope{LoopSlug: slug, ComponentRoot: comp.Root}, status); err != nil {
			t.Fatalf("status %s on %s: %v", status, slug, err)
		}
		ob++
		if _, _, err := s.RecordEventFire(NewEventFire{TriggerID: trig, OutboxID: 5000 + ob, SubjectKind: "goal", SubjectID: subject},
			EventFireDecision{Outcome: FireStarted, LoopID: l.ID, LoopSlug: slug}, time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	startedFire("run-a", "running", "goal-alive", "trO")
	startedFire("run-w", "waiting", "goal-wait", "trO")
	startedFire("run-b", "blocked", "goal-block", "trO")
	startedFire("run-d", "done", "goal-done", "trO")
	startedFire("run-x", "aborted", "goal-abort", "trO")
	startedFire("run-i", "idle", "goal-rest", "trO")

	for _, going := range []struct{ subject, status string }{
		{"goal-alive", "running"}, {"goal-wait", "waiting"}, {"goal-block", "blocked"},
	} {
		_, status, found, err := s.OverlappingEventFire("trO", "goal", going.subject)
		if err != nil {
			t.Fatal(err)
		}
		if !found || status != going.status {
			t.Errorf("subject %s: found=%v status=%q, want a still-%s loop named", going.subject, found, status, going.status)
		}
	}
	for _, over := range []string{"goal-done", "goal-abort", "goal-rest"} {
		if _, _, found, err := s.OverlappingEventFire("trO", "goal", over); err != nil || found {
			t.Errorf("subject %s: found=%v (err %v), want false — done/aborted/idle release the slot (S39's rest is NOT still going)", over, found, err)
		}
	}
	// Another subject or trigger does not shadow the fire.
	if _, _, found, err := s.OverlappingEventFire("trO", "goal", "goal-other"); err != nil || found {
		t.Errorf("unknown subject found=%v (err %v), want false", found, err)
	}
	if _, _, found, err := s.OverlappingEventFire("trOther", "goal", "goal-alive"); err != nil || found {
		t.Errorf("other trigger found=%v (err %v), want false — overlap is per trigger", found, err)
	}
}
