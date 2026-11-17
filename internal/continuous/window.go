package continuous

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/trigger"
)

// Window is CN10's working-hours declaration — one TR8-shaped value
// (start/end pair or absent, ISO weekday list, an IANA zone) that renders
// itself through the TRIGGER's code, never its own copy: InWindow builds a
// trigger.Schedule and asks it, so Aeon has exactly one definition of a
// window and TestWindowParityWithTrigger pins the two doors' agreement,
// midnight and DST included.
type Window struct {
	Start string // "HH:MM"; "" with End == "" is an open window (CN10's default)
	End   string
	Days  string // ISO list "1,3,5"; "" is every day
	Zone  string // an IANA zone, per trigger.LoadZone's door words
}

// Schedule renders the value as the one Schedule shape the trigger reads.
// The ID is not a schedule row — it is the name a broken window's honest
// sentence will carry.
func (w Window) Schedule() trigger.Schedule {
	return trigger.Schedule{
		ID:          "continuous window",
		WindowStart: w.Start,
		WindowEnd:   w.End,
		WindowDays:  w.Days,
		Zone:        w.Zone,
	}
}

// Open reports whether the window declares any hours at all.
func (w Window) Open() bool { return w.Start == "" && w.End == "" }

// InWindow answers CN10's gate for one instant, in the trigger's own
// words. A zone that will not load is NOT guessed open — same rule the
// trigger applies to an unparseable window: the honest answer for a broken
// window is closed, and the sentence says which half broke.
func (w Window) InWindow(t time.Time) (bool, string) {
	if w.Open() {
		return true, ""
	}
	loc, err := trigger.LoadZone(w.zoneOrUTC())
	if err != nil {
		return false, fmt.Sprintf("the window names zone %q, which does not load: %s", w.Zone, err)
	}
	return w.Schedule().InWindow(t, loc)
}

// NextOpen is the instant CN10's sentence must carry: the next window
// start after `now`, computed by stepping day by day in the window's OWN
// zone — which is exactly how the DST honesty arrives: a spring-forward
// that eats 02:00 lands the step on the next real day the window opens,
// and a fall-back day is checked at its real wall time. The horizon is
// 8 days: any valid TR8 window (forward-within-day, at least one day in
// the list) opens inside a week.
func (w Window) NextOpen(now time.Time) (time.Time, bool) {
	if w.Open() {
		return now, true
	}
	loc, err := trigger.LoadZone(w.zoneOrUTC())
	if err != nil {
		return time.Time{}, false
	}
	if _, _, err := parseHM(w.Start); err != nil {
		return time.Time{}, false // a start that does not parse opens nothing honest
	}
	// Step day by day (the horizon is a week plus a day), and within each
	// day scan at five-minute resolution from local midnight. The scan,
	// not arithmetic, is what makes the answer DST-honest: when the
	// transition EATS the nominal start (spring forward through 02:15,
	// say), the first instant that is really inside the window — the
	// transition itself — is what the window opens at, and no arithmetic
	// on a wall clock that does not exist could have named it. When the
	// window never opens on a day, the day simply fails every probe.
	t := now.In(loc)
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	for i := 0; i <= 8; i++ {
		day := dayStart.AddDate(0, 0, i)
		for j := 0; j < 24*12; j++ {
			cand := day.Add(time.Duration(j) * 5 * time.Minute)
			if !cand.After(now) {
				continue
			}
			if ok, _ := w.InWindow(cand); ok {
				return cand, true
			}
		}
	}
	return time.Time{}, false
}

// zoneOrUTC keeps an empty zone from failing the whole gate: a window with
// hours and no zone is half a TR8 pair, which the write door refuses; a
// snapshot that carried one anyway is read as UTC and its InWindow
// sentence will say so through the schedule.
func (w Window) zoneOrUTC() string {
	if w.Zone == "" {
		return "UTC"
	}
	return w.Zone
}

// windowDetail renders CN10's detail: the trigger's own closed-window
// sentence plus the next open instant — the two facts the person needs,
// both from code that already owns them.
func windowDetail(w Window, now time.Time, why string) string {
	next, ok := w.NextOpen(now)
	if !ok {
		return fmt.Sprintf("%s — and no next open instant could be computed (the window does not load)", why)
	}
	return fmt.Sprintf("%s — %s", why, next.UTC().Format(time.RFC3339))
}

// parseHM reads "HH:MM" — the same shape the schedule door accepts.
func parseHM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%q is not HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("%q has no hour in 00..23", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("%q has no minute in 00..59", s)
	}
	return h, m, nil
}
