package trigger

import (
	"context"
	"strings"
	"testing"
	"time"
)

// dst_test.go — the two days a year, with the instants injected (D14) and
// REAL zones (scope 3): every zone below is loaded through LoadZone from
// the system tzdata, never a FixedZone — a fixed offset has no DST and
// would make all of these pass while testing nothing.
//
// The dates are the measured ones (see dst.go's header for what Go and
// robfig did with each BEFORE handling): London spring 2026-03-29
// (01:00→02:00, so 01:30 never happens — the task file's "02:30" is the
// US shape; measured on this box, London's missing hour is the first),
// London autumn 2026-10-25 (01:30 twice), Santiago autumn 2026-04-04
// (23:30 twice) and Santiago spring 2026-09-06 (00:30 never happens —
// the southern hemisphere turns the other way, scope 4).

func TestNonExistentWallClockTimeIsSkipped(t *testing.T) {
	covered := time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC) // 01:30 GMT, answered
	s := sched("nightly", "30 1 * * *")
	s.Zone = "Europe/London"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-mar"}
	sc := newSched(st, fa)
	at := time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("the missing day started %d loops — it does not exist; it cannot fire anything", fa.call)
	}
	// The fire is the next REAL day, at the schedule's wall time in the
	// zone — 01:30 BST on the 30th (00:30 UTC), not an arbitrary neighbour.
	want := time.Date(2026, 3, 30, 1, 30, 0, 0, londonLoc(t))
	if !fa.got[0].Instant.Equal(want) {
		t.Fatalf("fired %s, want %s", fa.got[0].Instant, want)
	}
	// And the skip is RECORDED, not silence: a row exists for the 29th.
	var said bool
	for _, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "does not exist") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the missing 01:30 vanished without a record: %+v", st.fires)
	}
}

func TestSkippedNonExistentFireRecordedWithReason(t *testing.T) {
	// The message IS the task: "that time does not exist in this zone" —
	// not a generic did-not-fire, which would read as a window exclusion.
	covered := time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 1 * * *")
	s.Zone = "Europe/London"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	sc := newSched(st, &fakeStarter{id: "x"})
	if err := sc.Tick(context.Background(), time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for _, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "does not exist") {
			for _, want := range []string{"2026-03-29", "01:30", "Europe/London"} {
				if !strings.Contains(f.Reason, want) {
					t.Fatalf("the reason must name %q: %q", want, f.Reason)
				}
			}
			if f.Instant.Before(covered) || !f.Instant.Before(time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("the nominal stamp must sort on its day (29th), got %s", f.Instant)
			}
			return
		}
	}
	t.Fatal("no does-not-exist record at all")
}

func TestDoubledWallClockTimeFiresOnce(t *testing.T) {
	covered := time.Date(2026, 10, 24, 0, 30, 0, 0, time.UTC) // 01:30 BST on the 24th
	s := sched("nightly", "30 1 * * *")
	s.Zone = "Europe/London"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-oct"}
	sc := newSched(st, fa)
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	// robfig hands BOTH 01:30 BST and 01:30 GMT (measured, dst.go); the
	// naive scheduler's duplicate post is exactly what this test forbids.
	if fa.call != 1 {
		t.Fatalf("01:30 occurred twice and the scheduler started %d loops — TR6: fires once", fa.call)
	}
}

func TestSecondOccurrenceRecordedAsAlreadyFired(t *testing.T) {
	covered := time.Date(2026, 10, 24, 0, 30, 0, 0, time.UTC)
	s := sched("nightly", "30 1 * * *")
	s.Zone = "Europe/London"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	sc := newSched(st, &fakeStarter{id: "x"})
	if err := sc.Tick(context.Background(), time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var second *Fire
	for i, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "occurs twice") {
			second = &st.fires[i]
		}
	}
	if second == nil {
		t.Fatalf("the duplicate's absence must be EXPLAINED, not inferred: %+v", st.fires)
	}
	if !strings.Contains(second.Reason, "already-fired") || !strings.Contains(second.Reason, "2026-10-25 01:30") {
		t.Fatalf("the note must say already-fired and name the wall time: %q", second.Reason)
	}
	// The second pass is the GMT one: 01:30Z.
	if !second.Instant.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("second pass recorded at %s, want 01:30Z", second.Instant)
	}
}

func TestTestsUseARealIANAZone(t *testing.T) {
	// Scope 3 asserted, not assumed: a FixedZone has no DST and every test
	// above would pass while testing nothing. The zones the DST tests use
	// must load from tzdata, carry Area/Location names, and ACTUALLY shift.
	for _, zone := range []string{"Europe/London", "America/Santiago"} {
		loc, err := LoadZone(zone)
		if err != nil {
			t.Fatalf("%s: %v", zone, err)
		}
		if loc.String() != zone {
			t.Fatalf("loaded %q, not the named zone", loc.String())
		}
		if !strings.Contains(zone, "/") {
			t.Fatalf("%q is not an Area/Location name", zone)
		}
		_, off1 := time.Date(2026, 1, 15, 12, 0, 0, 0, loc).Zone()
		_, off2 := time.Date(2026, 7, 15, 12, 0, 0, 0, loc).Zone()
		if off1 == off2 {
			t.Fatalf("%s has no DST on this machine (both halves %d) — these tests would prove nothing", zone, off1)
		}
	}
}

func TestSouthernHemisphereDoubledHourFiresOnce(t *testing.T) {
	// Scope 4: the southern hemisphere turns the OTHER way — Chile's autumn
	// fold lands in April, so a March assumption cannot survive this test.
	covered := time.Date(2026, 4, 3, 2, 30, 0, 0, time.UTC) // 23:30 -03 on the 3rd
	s := sched("nightly", "30 23 * * *")
	s.Zone = "America/Santiago"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-chile"}
	sc := newSched(st, fa)
	if err := sc.Tick(context.Background(), time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("23:30 Santiago on 2026-04-04 happened twice and %d loops started — fires once, measured: robfig returned both passes", fa.call)
	}
	var noted bool
	for _, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "America/Santiago") && strings.Contains(f.Reason, "occurs twice") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("the southern duplicate needs its own already-fired note: %+v", st.fires)
	}
}

func TestSouthernHemisphereMissingHourRecorded(t *testing.T) {
	// The missing southern wall is SUNDAY 2026-09-06 00:30 — and the shape
	// is not the northern one's twin: this transition resolves a missing
	// wall BACKWARD (measured: time.Date(2026,9,6,0,30,Santiago) lands on
	// Sep 5 23:30, while London's spring shifted FORWARD to 02:30), the
	// Chilean rule runs on the first Saturday of September at 24:00
	// (energy-crisis rule kept), and the missing day is a Sunday. The
	// guard below is what caught both of my wrong assumptions (a midnight
	// northern template, and an AddDate-based probe).
	loc := santiagoLoc(t)
	if time.Date(2026, 9, 6, 0, 30, 0, 0, loc).In(loc).Format("15:04") == "00:30" {
		t.Skip("tzdata no longer skips 00:30 on 2026-09-06 in Santiago — the dates are a calendar fact, re-measure")
	}
	covered := time.Date(2026, 9, 5, 4, 30, 0, 0, time.UTC) // 00:30 -04 Saturday
	s := sched("nocturno", "30 0 * * *")
	s.Zone = "America/Santiago"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-sep"}
	sc := newSched(st, fa)
	if err := sc.Tick(context.Background(), time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("the missing Sunday 00:30 started %d loops", fa.call)
	}
	var said bool
	for _, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "does not exist") && strings.Contains(f.Reason, "2026-09-06 00:30") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the skipped southern Sunday must be recorded: %+v", st.fires)
	}
}

func TestMinutelyAcrossDoubledHourFiresEachWallMinuteOnce(t *testing.T) {
	// The fold under a minutely schedule: 60 wall minutes happen twice;
	// the fire-record must hold sixty FIRES-worth of wall minutes answered
	// once and sixty already-fired notes — the doubled hour explained
	// minute by minute, not silently halved.
	covered := time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC) // 00:59 BST
	s := sched("heartbeat", "* * * * *")
	s.Zone = "Europe/London"
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-fold"}
	sc := newSched(st, fa)
	if err := sc.Tick(context.Background(), time.Date(2026, 10, 25, 2, 59, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("tick answers the newest instant once, got %d", fa.call)
	}
	var notes int
	for _, f := range st.fires {
		if f.Outcome == OutcomeSkipped && strings.Contains(f.Reason, "occurs twice") {
			notes++
		}
	}
	if notes != 60 {
		t.Fatalf("the fold's second pass is 60 wall minutes — got %d already-fired notes", notes)
	}
}

func londonLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := LoadZone("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func santiagoLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := LoadZone("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
