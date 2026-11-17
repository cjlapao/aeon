package trigger

import (
	"context"
	"strings"
	"testing"
	"time"
)

// reconcile_test.go — the burst and the silence, both answered with
// injected instants (D14: not one sleep in this file; the "week" is a
// timestamp, not a wait).

func TestMissedWeekFiresExactlyOnce(t *testing.T) {
	now := london(t, "2026-10-27 09:00:00") // 22:00 London — UTC instants are stable
	covered := time.Date(2026, 10, 20, 3, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-catch"}
	sc := newSched(st, fa)
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("TR3: a week of dailies caught up with %d starts, want exactly 1 (the newest instant)", fa.call)
	}
	want := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	if !fa.got[0].Instant.Equal(want) {
		t.Fatalf("the catch-up fired %s, want the NEWEST missed instant %s", fa.got[0].Instant, want)
	}
	if len(st.gaps) != 1 || st.gaps[0].Missed != 6 {
		t.Fatalf("the six earlier dailies are ONE range row of six, got %+v", st.gaps)
	}
	if got := st.covered["nightly"]; got == "" || got == covered.Format(time.RFC3339Nano) {
		t.Fatalf("the frontier never advanced: %q", got)
	}
}

func TestMissedMinuteWeekFiresExactlyOnce(t *testing.T) {
	// The burst case from the design, in its worst credible shape: seven
	// days of a MINUTELY schedule — 10,080 missed instants — caught up by
	// ONE fire, recorded by ONE row. If a burst ever returns to this file,
	// this test is the thunder it makes.
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	covered := now.Add(-7 * 24 * time.Hour)
	s := sched("heartbeat", "* * * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-one"}
	sc := newSched(st, fa)
	start := time.Now()
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if fa.call != 1 {
		t.Fatalf("10,080 missed instants started %d loops — TR3 exists to make this number 1", fa.call)
	}
	if len(st.gaps) != 1 {
		t.Fatalf("10,079 could-not-say instants are ONE range row, got %d rows", len(st.gaps))
	}
	if st.gaps[0].Missed != 10079 {
		t.Fatalf("the count must be the honest 10,079 (10,080 missed minus the one catch-up), got %d", st.gaps[0].Missed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("recording the outage took %s — 0034 exists so the record is never the slow part", elapsed)
	}
}

func TestMissedFireRecordsCouldNotSay(t *testing.T) {
	// Two missed dailies: the newest gets a fired row, the earlier one a
	// could-not-say RANGE row — and the two answers are as different from
	// each other (and from did-not-fire) as the facts they record.
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	covered := time.Date(2026, 10, 22, 3, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	sc := newSched(st, &fakeStarter{id: "loop-x"})
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeFired {
		t.Fatalf("exactly one fire row: %+v", st.fires)
	}
	if len(st.gaps) != 1 || st.gaps[0].Missed != 1 {
		t.Fatalf("the one skipped middle is a gap row: %+v", st.gaps)
	}
	first := time.Date(2026, 10, 23, 3, 30, 0, 0, time.UTC)
	if st.gaps[0].CoveredFrom != first.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("the gap names its first unanswered instant %s, got %q", first, st.gaps[0].CoveredFrom)
	}
	gapSaid := ReconcileSummary(st.gaps[0], nil)
	if !strings.Contains(gapSaid, "could not be said") {
		t.Fatalf("the gap renders as could-not-say: %q", gapSaid)
	}
	if gapSaid == OutcomeDidNotFire.Render() || gapSaid == OutcomeFired.Render() {
		t.Fatal("could-not-say collapsed into another answer's words")
	}
}

func TestThreeAnswersNeverCollapse(t *testing.T) {
	// The cosmetic-total failure, tested not conserved: every answer —
	// including the five-value closure's skipped and refused — renders to a
	// DISTINCT sentence. A renderer that collapses two is indistinguishable
	// from a scheduler that did not make the distinction.
	answers := []string{
		OutcomeFired.Render(), OutcomeDidNotFire.Render(), OutcomeCouldNotSay.Render(),
		OutcomeSkipped.Render(), OutcomeRefused.Render(),
	}
	seen := map[string]string{}
	for i, a := range answers {
		if a == "" {
			t.Fatalf("answer %d renders empty", i)
		}
		if _, dup := seen[a]; dup {
			t.Fatalf("two answers both render %q — the distinction is the record", a)
		}
		seen[a] = answers[i]
	}
	if len(seen) != 5 {
		t.Fatalf("want five distinct renderings, got %d", len(seen))
	}
}

func TestOutcomeSetIsClosedByEnumeration(t *testing.T) {
	// The three answers are THE point; the fifth value is the unauthored
	// sentinel. Enumeration is the closure: a sixth string must not validate,
	// and the three answers must be present by name.
	want := map[Outcome]bool{
		OutcomeFired: true, OutcomeDidNotFire: true, OutcomeCouldNotSay: true,
		OutcomeSkipped: true, OutcomeRefused: true,
	}
	if len(want) != 5 {
		t.Fatal("the enumeration itself collapsed")
	}
	for o := range want {
		if !o.Valid() {
			t.Fatalf("%q must validate", o)
		}
	}
	for _, bad := range []Outcome{"", "maybe", "FIRED", "fired ", "unknown", "did_not_fired"} {
		if bad.Valid() {
			t.Fatalf("%q validated — the set is not closed", bad)
		}
	}
	// The could-not-say answer exists as a VALUE, which is what two
	// booleans could never be (design scope 3): the fourth combination
	// (fired and not-fired) has no representation here at all.
	if OutcomeCouldNotSay == OutcomeFired || OutcomeCouldNotSay == OutcomeDidNotFire {
		t.Fatal("the third answer is not a third value")
	}
}

func TestPausedScheduleRecordsNothing(t *testing.T) {
	// Q3, asserted the way the design answered it: paused means it would
	// NOT have fired, so there is nothing to be uncertain about — a week of
	// pause records NO fire, NO gap, and does not move the frontier.
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	covered := time.Date(2026, 10, 20, 3, 30, 0, 0, time.UTC)
	s := sched("holiday", "30 3 * * *")
	s.Paused = true
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{"holiday": covered.Format(time.RFC3339Nano)}}
	fa := &fakeStarter{id: "nope"}
	sc := newSched(st, fa)
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 || len(st.fires) != 0 || len(st.gaps) != 0 {
		t.Fatalf("Q3: a paused schedule records NOTHING — fires %d, gaps %d", len(st.fires), len(st.gaps))
	}
	if st.covered["holiday"] != covered.Format(time.RFC3339Nano) {
		t.Fatalf("the frontier of a paused schedule stands where it stood: %q", st.covered["holiday"])
	}
}

func TestReconcileTwiceRecordsOnce(t *testing.T) {
	// Two restarts, one catch-up: the frontier the first run advanced is
	// the second run's whole story — "down again for ten seconds" must not
	// invent a second gap over answered instants.
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	covered := time.Date(2026, 10, 25, 3, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-a"}
	sc := newSched(st, fa)
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := sc.Reconcile(context.Background(), now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 || len(st.fires) != 1 || len(st.gaps) != 1 {
		t.Fatalf("second startup re-answered answered instants: starts %d fires %d gaps %d", fa.call, len(st.fires), len(st.gaps))
	}
}

func TestReconcileWithoutGapAnswersNothing(t *testing.T) {
	now := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC) // before today's 03:30
	covered := time.Date(2026, 10, 26, 3, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{}
	sc := newSched(st, fa)
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 || len(st.fires) != 0 || len(st.gaps) != 0 {
		t.Fatalf("a fresh frontier answered nothing and wrote %d fires/%d gaps", len(st.fires), len(st.gaps))
	}
}

func TestCatchUpWalksEveryGate(t *testing.T) {
	// The one catch-up goes through the SAME walk a tick takes — a gate
	// that refused at the newest instant is not reason to try the missed
	// middles (they cannot start work either; they are recorded, not
	// retried). Window first: today's instant outside the window answers
	// did-not-fire for the ONE catch-up, gap row still honest.
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	covered := time.Date(2026, 10, 25, 3, 30, 0, 0, time.UTC)
	s := sched("office-hours", "30 3 * * *")
	s.WindowStart, s.WindowEnd, s.WindowDays = "09", "17", "1,2,3,4,5"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{}
	sc := newSched(st, fa)
	if err := sc.Reconcile(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 {
		t.Fatal("the window refused the catch-up and it started anyway")
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeDidNotFire {
		t.Fatalf("the newest instant answers did-not-fire once: %+v", st.fires)
	}
	if len(st.gaps) != 1 || st.gaps[0].Missed != 1 {
		t.Fatalf("the middle is still a recorded gap: %+v", st.gaps)
	}
}
