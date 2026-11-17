package trigger

import (
	"fmt"
	"sort"
	"time"
)

// preview.go — S49 T2's thin read-only window onto the clock's OWN walk.
// PreviewFires adds no rule: it runs the same `expr.Next` the tick runs, the
// same collapseDoubled/findMissing the two-days-a-year code runs, and the
// same InWindow the fire walk runs, then hands the instants to a preview in
// which every Note is the engine's OWN reason sentence (dst.go's two, and
// InWindow's). The clock's tests stay green and unedited — nothing in
// clock.go changed; the differential test (internal/automationview) drives
// the real Scheduler over a year of ticks and compares.
//
// What a preview can honestly say and what it cannot: the schedule's own
// contract (clock, DST, window) is decidable now; overlap, governance and
// the starter are the engine's state at the instant, so a due instant says
// Kind "due" with no Note — it promises the clock will ASK, not that a loop
// will start. The health read (S49 T3) says what else is true right now.

// PreviewKind is the preview's classification of one instant:
// "due" (the walk would ask to fire at it), "window" (the schedule's own
// contract excludes it — the engine would record did-not-fire with this
// very sentence), "dst-collapsed" (the second pass of a doubled wall —
// recorded already-fired), "dst-missing" (a wall time that never happened).
type PreviewKind string

const (
	PreviewDue          PreviewKind = "due"
	PreviewWindow       PreviewKind = "window"
	PreviewDSTCollapsed PreviewKind = "dst-collapsed"
	PreviewDSTMissing   PreviewKind = "dst-missing"
)

// FirePreview is one instant on the schedule's own horizon.
type FirePreview struct {
	At   time.Time
	Kind PreviewKind
	Note string // "" for due; otherwise the engine's own reason sentence, verbatim
	Wall string // the zone's wall-clock reading of At (what a person reads)
}

// PreviewLimit is the horizon cap; a longer preview is a different product.
const PreviewLimit = 20

// PreviewFires walks the schedule's next n instants (n in [1, PreviewLimit],
// strictly after `from`) and classifies each with the engine's own words.
// A webhook has no instants — the refusal says so rather than lying empty.
func PreviewFires(s Schedule, from time.Time, n int) ([]FirePreview, error) {
	if s.Kind == KindWebhook {
		return nil, fmt.Errorf("trigger: %q is a webhook — it fires on a signed POST, never on a clock", s.ID)
	}
	if n < 1 || n > PreviewLimit {
		return nil, fmt.Errorf("trigger: the next-fire preview answers between 1 and %d instants (this asked %d)", PreviewLimit, n)
	}
	loc, err := LoadZone(s.Zone)
	if err != nil {
		return nil, err
	}
	expr, err := s.ScheduleExpr()
	if err != nil {
		return nil, err
	}
	// The raw walk (the same expr.Next walkInstants walks), grown until the
	// KEPT list fills the preview — a collapsed or windowed instant still
	// spends one of the n, exactly as the history spends one row per
	// instant. walkCap bounds it; a schedule that cannot fill n inside the
	// cap (yearly expressions do this) returns what exists.
	var raw []time.Time
	t := from
	for len(raw) < walkCap {
		next := expr.Next(t.In(loc))
		if next.IsZero() || !next.After(t) {
			break // exhausted, or broken data — the walk says nothing more
		}
		raw = append(raw, next)
		t = next
		if len(raw) >= n*64 { // a doubled-hour + window-heavy schedule spends more than n raws; 64× is generous and bounded
			break
		}
	}
	kept, dropped := collapseDoubled(raw, loc)
	var missing []missingWall
	pairs := raw
	if pre := lastBefore(expr, loc, from.Add(-26*time.Hour), from); !pre.IsZero() {
		pairs = append([]time.Time{pre}, raw...)
	}
	for i := 1; i < len(pairs); i++ {
		missing = append(missing, findMissing(pairs[i-1], pairs[i], loc)...)
	}
	out := make([]FirePreview, 0, n)
	for _, k := range kept {
		p := FirePreview{At: k, Kind: PreviewDue, Wall: wallOf(k, loc)}
		if ok, reason := s.InWindow(k, loc); !ok {
			p.Kind, p.Note = PreviewWindow, reason
		}
		out = append(out, p)
	}
	for _, d := range dropped {
		wall := d.In(loc).Format("2006-01-02 15:04")
		out = append(out, FirePreview{
			At: d, Kind: PreviewDSTCollapsed, Wall: wallOf(d, loc),
			// dst.go's recordDSTAnswers sentence, verbatim: the preview
			// speaks the history's words or says nothing.
			Note: "daylight saving: wall time " + wall +
				" occurs twice in " + s.Zone + " — fired once (TR6); this pass is recorded as already-fired",
		})
	}
	for _, m := range missing {
		out = append(out, FirePreview{
			At: m.Nominal, Kind: PreviewDSTMissing, Wall: m.Wall,
			Note: "daylight saving: wall time " + m.Wall + " does not exist in " + s.Zone +
				" (clocks skipped forward) — the schedule is skipped and recorded (TR6)",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func wallOf(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04 MST")
}
