package continuous

import (
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/trigger"
)

// loadTestZone reads a zone for the test side (tests may read the zone
// database; the parity here is precisely that BOTH doors resolve zones
// through the trigger's one loader).
func loadTestZone(t *testing.T, z string) (*time.Location, error) {
	t.Helper()
	return trigger.LoadZone(z)
}

// TestWindowParityWithTrigger drives the SAME truth table through the
// continuous Window and through the trigger's Schedule.InWindow — the one
// definition CN10 promised (F15's "the same validation function" law). If
// the two ever answer differently, one of them is a second window, and
// that is exactly the drift this table exists to catch: midnight spans,
// weekday lists, and a DST transition taken from the zone code itself.
func TestWindowParityWithTrigger(t *testing.T) {
	ny, err := loadTestZone(t, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		w      Window
		at     time.Time
		wantIn bool
	}{
		{"open window is always in", Window{}, time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC), true},
		{"inside the hours", Window{Start: "09:00", End: "18:00", Zone: "UTC"},
			time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), true},
		{"before the start", Window{Start: "09:00", End: "18:00", Zone: "UTC"},
			time.Date(2026, 11, 5, 8, 59, 0, 0, time.UTC), false},
		{"the end minute is out (start-inclusive, end-exclusive)", Window{Start: "09:00", End: "18:00", Zone: "UTC"},
			time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC), false},
		{"day list excludes the day", Window{Start: "09:00", End: "18:00", Days: "1,2,3", Zone: "UTC"},
			time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), false}, // Nov 5 2026 is Thursday (4)
		{"day list includes the day", Window{Start: "09:00", End: "18:00", Days: "4", Zone: "UTC"},
			time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), true},
		{"a midnight-span window is closed all day (TR8 refuses it at the door; the read never guesses it open)",
			Window{Start: "22:00", End: "06:00", Zone: "UTC"}, time.Date(2026, 11, 5, 23, 30, 0, 0, time.UTC), false},
		{"a midnight-span window at 3am is still closed",
			Window{Start: "22:00", End: "06:00", Zone: "UTC"}, time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC), false},
		{"zone honesty: 12:00Z is 07:00 in New York, inside 06:00–09:00 there",
			Window{Start: "06:00", End: "09:00", Zone: "America/New_York"},
			time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), true},
		{"zone honesty: the same instant is 12:00 UTC, outside 06:00–09:00 UTC",
			Window{Start: "06:00", End: "09:00", Zone: "UTC"},
			time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), false},
		// DST: 2026-03-08 is the spring-forward in New York (02:00 jumps to 03:00).
		// 06:30Z is 01:30 EST; 07:30Z is 03:30 EDT.
		{"DST spring: the half hour before the jump is EST and inside a 01:00–02:00 window",
			Window{Start: "01:00", End: "02:00", Zone: "America/New_York"},
			time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC), true},
		{"DST spring: 02:30 does not exist that day — the instant that IS real (03:30 EDT) is outside the same window",
			Window{Start: "01:00", End: "02:00", Zone: "America/New_York"},
			time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := c.w.InWindow(c.at)
			if got != c.wantIn {
				t.Fatalf("InWindow(%s) = %v (%s), want %v", c.at, got, why, c.wantIn)
			}
			// The trigger's own code, by hand: the parity clause.
			sched := c.w.Schedule()
			loc := ny
			if c.w.Zone != "" && c.w.Zone != "America/New_York" {
				var err error
				loc, err = loadTestZone(t, c.w.Zone)
				if err != nil {
					t.Fatal(err)
				}
			}
			wantIn, wantWhy := sched.InWindow(c.at, loc)
			if c.w.Open() {
				wantIn, wantWhy = true, ""
			}
			if got != wantIn || why != wantWhy {
				t.Fatalf("the two doors drifted: continuous said (%v,%q), trigger says (%v,%q)", got, why, wantIn, wantWhy)
			}
		})
	}
}

// TestNextOpenNamesTheRealInstant pins CN10's sentence ingredient: the
// next window start, computed in the window's own zone — including when
// DST eats the wall time (the step lands on a real instant, not on the
// fiction) and the ordinary cases (later today, next listed day).
func TestNextOpenNamesTheRealInstant(t *testing.T) {
	nyLoc, err := loadTestZone(t, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("later today", func(t *testing.T) {
		w := Window{Start: "14:00", End: "18:00", Zone: "UTC"}
		now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
		got, ok := w.NextOpen(now)
		if !ok || !got.Equal(time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)) {
			t.Fatalf("NextOpen = %v (%v), want 14:00Z today", got, ok)
		}
	})
	t.Run("next listed day, not tomorrow", func(t *testing.T) {
		w := Window{Start: "09:00", End: "18:00", Days: "6,7", Zone: "UTC"} // Sat,Sun
		now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)                // Thursday
		got, ok := w.NextOpen(now)
		if !ok || got.Weekday() != time.Saturday || got.Day() != 7 {
			t.Fatalf("NextOpen = %v (%v), want Saturday Nov 7", got, ok)
		}
	})
	t.Run("DST spring-forward: 02:15 does not exist on 2026-03-08 — the step lands on the real wall instant", func(t *testing.T) {
		w := Window{Start: "02:15", End: "03:45", Zone: "America/New_York"}
		now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
		got, ok := w.NextOpen(now)
		if !ok {
			t.Fatal("no next open computed")
		}
		local := got.In(nyLoc)
		if local.Hour() != 3 || local.Minute() != 0 {
			t.Fatalf("NextOpen local = %s, want 03:00 — the transition instant, when the window really opens; %s never names a fiction", local, "NextOpen")
		}
		if in, why := w.InWindow(got); !in {
			t.Fatalf("the instant NextOpen named is not inside the window: %s", why)
		}
	})
	t.Run("a zone that does not load computes nothing — and InWindow says which half broke", func(t *testing.T) {
		w := Window{Start: "09:00", End: "18:00", Zone: "Mars/Phobos"}
		if _, ok := w.NextOpen(time.Now()); ok {
			t.Fatal("NextOpen claimed an instant from a zone that does not load")
		}
		in, why := w.InWindow(time.Now())
		if in || why == "" {
			t.Fatalf("a broken zone answered (%v,%q) — it must be closed and self-describing", in, why)
		}
	})
}
