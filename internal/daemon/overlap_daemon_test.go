package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// The overlap task's gates, run against the WIRED hook — the one New
// attaches, reading the last fired loop's live row through
// LastFiredLoopBefore. The trigger package's tests prove the predicate;
// these prove the scheduler consults it through the store, per schedule,
// and starts nothing while the previous run still owns the slot.

func overlapScheduler(t *testing.T, s *store.Store, d *Daemon, at time.Time) *trigger.Scheduler {
	t.Helper()
	sc := trigger.NewScheduler(triggerRows{s}, d, &fixedClock{t: at})
	// The PRODUCTION hook, copied from the daemon's own scheduler — the
	// test cannot pass by wiring a friendlier one.
	sc.Gate = d.triggers.Gate
	if sc.Gate.Overlap == nil {
		t.Fatal("New did not wire the overlap hook — T3's whole claim")
	}
	return sc
}

func seedOverlapSchedule(t *testing.T, s *store.Store, id, ws string, covered time.Time) {
	t.Helper()
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: id, Kind: "schedule", Blueprint: "two-step", Workspace: ws,
		Expr: "30 3 * * *", Zone: "UTC", CreatedAt: covered.Format(time.RFC3339),
		CoveredUntil: covered.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOverlappingFireIsSkippedAndRecorded(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "slow-walk") // T7's wired hook; PL5 refuses otherwise
	ctx := context.Background()
	ws := t.TempDir()
	day1 := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	seedOverlapSchedule(t, s, "slow-walk", ws, day1.Add(-time.Hour))

	sc := overlapScheduler(t, s, d, day1)
	if err := sc.Tick(ctx, day1); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TriggerFireHistory(ctx, "slow-walk", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Outcome != "fired" {
		t.Fatalf("first instant: %+v", hist)
	}
	previous := hist[0].LoopID
	awaitSettled(t, s, previous)
	// Scenario pin: the previous run is RUNNING (the engine moved it on;
	// the predicate reads the row live, and the row is the scenario).
	if err := s.SetLoopStatus(store.Scope{LoopSlug: "slow-walk-" + day1.UTC().Format("20060102-1504"), ComponentRoot: ws}, "running"); err != nil {
		t.Fatal(err)
	}
	before, err := s.ListLoops(store.ComponentScope{ComponentRoot: ws})
	if err != nil {
		t.Fatal(err)
	}

	// Next day, same instant: the overlap gate must skip and name the
	// previous loop, and NO second loop may exist afterwards (N2 — skipping
	// is the decision; nothing queued, nothing started).
	day2 := day1.Add(24 * time.Hour)
	if err := overlapScheduler(t, s, d, day2).Tick(ctx, day2); err != nil {
		t.Fatal(err)
	}
	hist, err = s.TriggerFireHistory(ctx, "slow-walk", 10)
	if err != nil {
		t.Fatal(err)
	}
	var skip *store.TriggerFireRow
	for i := range hist {
		if hist[i].Instant == day2.Format("2006-01-02T15:04:05Z") || strings.HasPrefix(hist[i].Instant, "2026-10-28") {
			skip = &hist[i]
		}
	}
	if skip == nil || skip.Outcome != "skipped" {
		t.Fatalf("day 2 answer: %+v", hist)
	}
	if !strings.Contains(skip.Reason, previous) {
		t.Fatalf("TR5: the skip names the previous loop %q: %q", previous, skip.Reason)
	}
	after, err := s.ListLoops(store.ComponentScope{ComponentRoot: ws})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the overlap started %d extra loops — a skip starts nothing (N2)", len(after)-len(before))
	}

	// And the skip is NOT did-not-fire: same instant, distinct answer.
	if skip.Outcome == "did_not_fire" {
		t.Fatal("a skip collapsed into did-not-fire")
	}

	// Once the previous run is done, the next instant fires again.
	if err := s.SetLoopStatus(store.Scope{LoopSlug: "slow-walk-" + day1.UTC().Format("20060102-1504"), ComponentRoot: ws}, "done"); err != nil {
		t.Fatal(err)
	}
	day3 := day1.Add(48 * time.Hour)
	if err := overlapScheduler(t, s, d, day3).Tick(ctx, day3); err != nil {
		t.Fatal(err)
	}
	hist, err = s.TriggerFireHistory(ctx, "slow-walk", 10)
	if err != nil {
		t.Fatal(err)
	}
	var fired3 bool
	for _, h := range hist {
		if strings.HasPrefix(h.Instant, "2026-10-29") && h.Outcome == "fired" {
			fired3 = true
			awaitSettled(t, s, h.LoopID)
		}
	}
	if !fired3 {
		t.Fatalf("day 3 should fire once the previous run is done: %+v", hist)
	}
}

func TestSuspendedLoopOverlapBehaviourAsserted(t *testing.T) {
	// Q2, named and asserted ONE way (the task file's rule): the design's
	// "suspended" is step-speak; on the loop row the owed-answer state is
	// `waiting`. This test proves `waiting` BLOCKS and `done` does not —
	// flip the predicate and it fails.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "slow-walk") // T7's wired hook; PL5 refuses otherwise
	ctx := context.Background()
	ws := t.TempDir()
	day1 := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	seedOverlapSchedule(t, s, "slow-walk", ws, day1.Add(-time.Hour))
	if err := overlapScheduler(t, s, d, day1).Tick(ctx, day1); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TriggerFireHistory(ctx, "slow-walk", 10)
	if err != nil {
		t.Fatal(err)
	}
	previous := hist[0].LoopID
	awaitSettled(t, s, previous)
	slug := "slow-walk-" + day1.UTC().Format("20060102-1504")
	if err := s.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: ws}, "waiting"); err != nil {
		t.Fatal(err)
	}
	day2 := day1.Add(24 * time.Hour)
	if err := overlapScheduler(t, s, d, day2).Tick(ctx, day2); err != nil {
		t.Fatal(err)
	}
	hist, err = s.TriggerFireHistory(ctx, "slow-walk", 10)
	if err != nil {
		t.Fatal(err)
	}
	var answered bool
	for _, h := range hist {
		if strings.HasPrefix(h.Instant, "2026-10-28") {
			answered = true
			if h.Outcome != "skipped" || !strings.Contains(h.Reason, previous) {
				t.Fatalf("Q2: a WAITING previous run blocks the next fire — got %+v", h)
			}
		}
	}
	if !answered {
		t.Fatal("the second instant was never answered")
	}
}
