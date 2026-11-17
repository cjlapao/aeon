package automationview_test

// nextfires_diff_test.go — rule 3: the preview equals the engine. The real
// trigger.Scheduler is driven over a full year of hourly ticks (2026,
// containing every DST shape in three zones plus a window), and every row
// it records is compared, instant-for-instant and sentence-for-sentence,
// against what automationview.NextFires promised for the same year. Hourly
// ticks are honest for daily/weekly fixtures: each scheduled instant is
// the NEWEST in its tick's window, so the engine answers every one (the
// middle-instant skip only hides instants finer than the tick).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/trigger"
)

type diffStore struct {
	rows  map[string][]trigger.Fire
	cov   map[string]time.Time
	fix   []trigger.Schedule
	calls int
}

type diffStarter struct{ id int }

func (d *diffStore) ActiveSchedules(ctx context.Context) ([]trigger.Schedule, error) {
	d.calls++
	out := make([]trigger.Schedule, 0, len(d.fix))
	for _, s := range d.fix {
		s.CoveredUntil = d.cov[s.ID].UTC().Format(time.RFC3339Nano)
		out = append(out, s)
	}
	return out, nil
}
func (d *diffStore) SetCovered(ctx context.Context, id string, coveredUntil string) error {
	t, err := time.Parse(time.RFC3339Nano, coveredUntil)
	if err != nil {
		return err
	}
	d.cov[id] = t
	return nil
}
func (d *diffStore) RecordFire(ctx context.Context, f trigger.Fire) error {
	d.rows[f.TriggerID] = append(d.rows[f.TriggerID], f)
	return nil
}
func (d *diffStore) RecordGap(ctx context.Context, id, from, to string, missed int, at time.Time) error {
	return nil
}
func (d *diffStore) SaveTriggerSchedule(ctx context.Context, s trigger.Schedule) error {
	return nil
}
func (s *diffStarter) StartFromTrigger(ctx context.Context, f trigger.Fire) (string, error) {
	s.id++
	return fmt.Sprintf("loop-%d", s.id), nil
}

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

func TestNextFiresEqualsTheClockAcrossAYear(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 1) // 366 days: a leap year, every zone's transition inside

	fixtures := []struct {
		s      trigger.Schedule
		window *automationview.ScheduleSpec
	}{
		{s: sched("lisbon-30-1", "30 1 * * *", "Europe/Lisbon")},
		{s: sched("ny-30-2", "30 2 * * *", "America/New_York")},
		{s: sched("ny-30-1", "30 1 * * *", "America/New_York")},
		{s: sched("lhowe-15-2", "15 2 * * *", "Australia/Lord_Howe")},
		{s: sched("ny-noon", "0 12 * * *", "America/New_York")},
		{s: schedWin("ny-window", "0 10 * * *", "America/New_York", "09:00", "18:00", "1,2,3,4,5")},
	}

	d := &diffStore{rows: map[string][]trigger.Fire{}, cov: map[string]time.Time{}}
	d.fix = make([]trigger.Schedule, 0, len(fixtures))
	for i, f := range fixtures {
		s := f.s
		s.CoveredUntil = start.UTC().Format(time.RFC3339Nano)
		d.cov[s.ID] = start
		d.fix = append(d.fix, s)
		_ = i
	}
	starter := &diffStarter{}
	sc := trigger.NewScheduler(d, starter, fakeClock{t: start})

	ctx := context.Background()
	for at := start.Add(time.Hour); !at.After(end); at = at.Add(time.Hour) {
		if err := sc.Tick(ctx, at); err != nil {
			t.Fatalf("tick at %s: %v", at, err)
		}
	}

	for _, f := range fixtures {
		spec := automationview.ScheduleSpec{
			ID: f.s.ID, Expr: f.s.Expr, Zone: f.s.Zone,
			WindowStart: f.s.WindowStart, WindowEnd: f.s.WindowEnd, WindowDays: f.s.WindowDays,
		}
		// The preview's year, chunked at 20 (the cap), cursor = last At.
		var prev []automationview.FireInstant
		from := start
		for {
			batch, err := automationview.NextFires(spec, from, 20)
			if err != nil {
				t.Fatalf("%s preview: %v", spec.ID, err)
			}
			if len(batch) == 0 {
				break
			}
			stop := false
			for _, b := range batch {
				if !b.At.Before(end) {
					stop = true
					break
				}
				prev = append(prev, b)
				from = b.At
			}
			if stop || len(batch) < 20 {
				break
			}
		}
		// The engine's year: every row it recorded for this schedule.
		rows := d.rows[f.s.ID]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Instant.Before(rows[j].Instant) })
		if len(rows) != len(prev) {
			t.Fatalf("%s: engine recorded %d rows, preview promised %d instants", f.s.ID, len(rows), len(prev))
		}
		for i := range rows {
			r, p := rows[i], prev[i]
			if !r.Instant.Equal(p.At) {
				t.Fatalf("%s row %d: engine %s, preview %s", f.s.ID, i, r.Instant, p.At)
			}
			switch p.Kind {
			case "due":
				if r.Outcome != trigger.OutcomeFired {
					t.Errorf("%s %s: preview said due, engine answered %q", f.s.ID, p.At, r.Outcome)
				}
			case "window":
				if r.Outcome != trigger.OutcomeDidNotFire || r.Reason != p.Note {
					t.Errorf("%s %s: preview note %q, engine %q %q", f.s.ID, p.At, p.Note, r.Outcome, r.Reason)
				}
			case "dst-collapsed", "dst-missing":
				if r.Outcome != trigger.OutcomeSkipped || r.Reason != p.Note {
					t.Errorf("%s %s: preview note %q, engine %q %q", f.s.ID, p.At, p.Note, r.Outcome, r.Reason)
				}
			default:
				t.Errorf("%s: unknown preview kind %q", f.s.ID, p.Kind)
			}
		}
	}
	// The tick actually ran the schedules (a silent skip would pass
	// vacuously otherwise): one answer per scheduled instant.
	if d.calls == 0 {
		t.Fatal("the scheduler never read the store")
	}
}

// TestNextFiresNotesDstGapOverlapAndWindow is the pointed version of the
// same promise, naming the three notes the task lists, on the exact dates
// (tzdata 2026 measured: Lisbon gaps 29 Mar, folds 25 Oct; New York gaps
// 8 Mar, folds 1 Nov; Lord Howe halves 4 Oct / 5 Apr).
func TestNextFiresNotesDstGapOverlapAndWindow(t *testing.T) {
	lisbon := automationview.ScheduleSpec{ID: "l", Expr: "30 1 * * *", Zone: "Europe/Lisbon"}
	got, err := automationview.NextFires(lisbon, time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC), 8)
	if err != nil {
		t.Fatal(err)
	}
	var gap bool
	for _, g := range got {
		if g.Kind == "dst-missing" {
			if !contains(g.Note, "does not exist in Europe/Lisbon") || !contains(g.Note, "2026-03-29 01:30") {
				t.Errorf("gap note wrong: %+v", g)
			}
			gap = true
		}
	}
	if !gap {
		t.Errorf("no missing-wall note across the 29 Mar 2026 Lisbon gap: %+v", got)
	}
	ny := automationview.ScheduleSpec{ID: "n", Expr: "30 1 * * *", Zone: "America/New_York"}
	got, err = automationview.NextFires(ny, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), 8)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, g := range got {
		if g.Kind == "dst-collapsed" && contains(g.Note, "2026-11-01 01:30") {
			seen = true
		}
	}
	if !seen {
		t.Errorf("no occurs-twice note across the 1 Nov 2026 New York fold: %+v", got)
	}
	win := automationview.ScheduleSpec{ID: "w", Expr: "0 8 * * *", Zone: "UTC",
		WindowStart: "09:00", WindowEnd: "18:00", WindowDays: "1,2,3,4,5"}
	got, err = automationview.NextFires(win, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range got {
		if g.Kind != "window" || !contains(g.Note, "outside the working-hours window") {
			t.Errorf("an 08:00 instant under a 09:00 window must be excluded, explained: %+v", g)
		}
	}
}

func TestNextFiresRefusesWebhookAndBadN(t *testing.T) {
	_, err := automationview.NextFires(automationview.ScheduleSpec{ID: "h", Zone: "UTC"}, time.Now(), 5)
	if err == nil {
		t.Fatal("an empty-expr schedule must not preview")
	}
	if _, err := automationview.NextFires(automationview.ScheduleSpec{ID: "s", Expr: "0 3 * * *", Zone: "UTC"}, time.Now(), 21); err == nil {
		t.Fatal("n over the cap must be refused, not truncated")
	}
	if _, err := automationview.NextFires(automationview.ScheduleSpec{ID: "s", Expr: "0 3 * * *", Zone: "Mars/Olympus"}, time.Now(), 5); err == nil {
		t.Fatal("a zone that does not load must refuse")
	}
}

func sched(id, expr, zone string) trigger.Schedule {
	return trigger.Schedule{ID: id, Kind: trigger.KindSchedule, Blueprint: "b",
		Workspace: "/ws", Expr: expr, Zone: zone}
}

func schedWin(id, expr, zone, ws, we, wd string) trigger.Schedule {
	s := sched(id, expr, zone)
	s.WindowStart, s.WindowEnd, s.WindowDays = ws, we, wd
	return s
}

func contains(s, want string) bool { return strings.Contains(s, want) }
