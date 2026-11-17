package trigger

import (
	"context"
	"time"
)

// dst.go — the two days a year, handled explicitly because Go does not
// refuse them. Measured on this box BEFORE any handling was written (the
// task's scope-5 requirement, tzdata 2026):
//
//   - time.Date(2026, 3, 29, 1, 30, 0, 0, Europe/London) — a wall time that
//     never happens (clocks jump 01:00→02:00) — resolved WITHOUT ERROR to
//     2026-03-29T02:30+01:00, the shifted instant. Go picked a neighbour.
//   - time.Date(2026, 10, 25, 1, 30, 0, 0, Europe/London) — the doubled hour
//     — resolved WITHOUT ERROR to 2026-10-25T01:30:00Z: the SECOND pass
//     (GMT), not the first (BST).
//   - time.Date(2026, 4, 4, 23, 30, 0, 0, America/Santiago) — the southern
//     doubled hour — resolved to 23:30-03:00: the FIRST pass (-03). The two
//     zones resolved the same kind of ambiguity in OPPOSITE directions, so
//     "which one Go picks" is not a rule to build on — it is the reason the
//     scheduler asks no question of time.Date on ambiguous walls.
//   - robfig/cron v3.0.1 (measured): walks "30 1 * * *" Europe/London across
//     the spring gap by SKIPPING the day (03-28 → 03-30, no instant lands in
//     the missing hour — a silent hole in the history), and across the
//     autumn fold by returning BOTH passes (01:30 BST and 01:30 GMT) — the
//     naive double-fire the task names, live in both hemispheres.
//
// The handling, then, owns both edges:
//
//   - Doubled fires ONCE (TR6): the walk collapses a wall time to its FIRST
//     chronological pass (drop t when an instant one DST step earlier — 1 h,
//     or 45/30 min for the half-hour zones — carries the identical wall
//     clock); the dropped pass is recorded `skipped`, reason
//     already-fired, so the duplicate's absence is explained, not inferred.
//   - Non-existent is SKIPPED and RECORDED (TR6): when consecutive instants
//     of a pinned wall time are two wall-days apart, the day between is
//     probed with time.Date and found shifted (fields differ from the wall
//     asked for) — the record says "wall time 01:30 on 2026-03-29 does not
//     exist in Europe/London", the only message that is useful here.

// dstProbes are the DST step sizes to look backwards with when collapsing a
// doubled wall time: the hour is universal, the half and three-quarter hours
// exist in the real world (Lord Howe +30, Chatham +45).
var dstProbes = []time.Duration{time.Hour, 45 * time.Minute, 30 * time.Minute}

// sameWall compares wall clocks at second precision (a schedule's finest
// named unit). Two instants have the same wall when their In(loc) readings
// agree on date and time — the abbreviation may differ (BST vs GMT), which is
// exactly what makes the pair a doubled hour rather than a bug.
func sameWall(a, b time.Time, loc *time.Location) bool {
	x, y := a.In(loc), b.In(loc)
	xa, xb := x.Format("2006-01-02 15:04:05"), y.Format("2006-01-02 15:04:05")
	return xa == xb
}

// isDoubledLater reports whether t's wall clock occurs again after t — the
// second pass of the autumn fold is the mirror image: an instant one DST
// step LATER carries the same wall.
func isDoubledLater(t time.Time, loc *time.Location) bool {
	for _, d := range dstProbes {
		if sameWall(t, t.Add(d), loc) {
			return true
		}
	}
	return false
}

// collapseDoubled drops every pass of a doubled wall time except the first:
// t is dropped when an instant one DST step EARLIER carries the same wall.
// Returns (kept, dropped) in chronological order.
func collapseDoubled(instants []time.Time, loc *time.Location) (kept, dropped []time.Time) {
	for _, t := range instants {
		dup := false
		for _, d := range dstProbes {
			if sameWall(t, t.Add(-d), loc) {
				dup = true
				break
			}
		}
		if dup {
			dropped = append(dropped, t)
		} else {
			kept = append(kept, t)
		}
	}
	return kept, dropped
}

// missingWall is a scheduled wall time that never happened: the nominal
// instant (the UTC the PRE-transition offset would have given — a stamp to
// sort by and read, never a moment that existed) and the wall clock as the
// schedule named it.
type missingWall struct {
	Nominal time.Time
	Wall    string
}

// findMissing probes the day between two consecutive walk instants that
// should have been a day apart in wall time: a pinned wall time (same
// clock, two wall-days apart) whose middle date resolves SHIFTED under
// time.Date never existed. Only pinned-wall pairs are judged — a wildcard
// schedule has no named wall to have lost, and saying so honestly beats
// inventing gaps under it.
func findMissing(prev, next time.Time, loc *time.Location) []missingWall {
	p, n := prev.In(loc), next.In(loc)
	if p.Year() == n.Year() && p.Month() == n.Month() && n.Day()-p.Day() != 2 {
		return nil // adjacent days (or other gaps) are not the missing-day case
	}
	if p.Year() != n.Year() || p.Month() != n.Month() {
		return nil // month-crossings are not this shape; do not guess
	}
	if p.Format("15:04:05") != n.Format("15:04:05") {
		return nil // wildcard-ish walk: no pinned wall was lost — say nothing
	}
	// The probe asks the CALANDAR, never AddDate: p.AddDate(0,0,1) on a
	// wall time beside a gap normalizes through the INSTANT and can land
	// the whole reading back inside the previous day (measured: Santiago
	// Sep 6 00:30 resolved to Sep 5 23:30, and the naive probe then
	// re-asked Sep 5 23:30 — a wall that exists — and concluded nothing
	// was missing). time.Date with day+1 asks the one question that
	// matters: does THIS wall resolve to itself?
	h, m, sec := p.Clock()
	probe := time.Date(p.Year(), p.Month(), p.Day()+1, h, m, sec, 0, loc)
	if probe.In(loc).Format("15:04:05") != p.Format("15:04:05") || probe.In(loc).Day() != p.Day()+1 {
		// The wall did NOT resolve to itself on the day the calendar
		// promises — it never happened.
		return []missingWall{{
			Nominal: probe.UTC(),
			Wall: time.Date(p.Year(), p.Month(), p.Day()+1, 12, 0, 0, 0, time.UTC).Format("2006-01-02") +
				" " + p.Format("15:04"),
		}}
	}
	return nil // the wall resolved to itself — it existed; something else is up
}

// recordDSTAnswers writes the two DST explanations as records — collapsed
// second passes and missing walls — WITHOUT touching the frontier (the
// fire's own answer owns it; these rows explain gaps in the history, they
// are not instants the frontier owes). Record failures speak, never abort:
// a fire that happened outranks the explanation of one that did not.
func (sc *Scheduler) recordDSTAnswers(ctx context.Context, s Schedule, covered time.Time, dropped []time.Time, missing []missingWall) {
	loc, err := LoadZone(s.Zone)
	if err != nil {
		return // the walk could not have run either; the tick spoke about it
	}
	for _, t := range dropped {
		if !t.After(covered) {
			continue // the frontier has already answered this pass once
		}
		wall := t.In(loc).Format("2006-01-02 15:04")
		f := Fire{
			TriggerID: s.ID, Blueprint: s.Blueprint, Workspace: s.Workspace, Goal: s.Goal,
			Provenance: ProvenanceTrigger, Instant: t,
		}
		f.Reason = "daylight saving: wall time " + wall +
			" occurs twice in " + s.Zone + " — fired once (TR6); this pass is recorded as already-fired"
		f.Outcome = OutcomeSkipped
		if err := sc.Store.RecordFire(ctx, f); err != nil {
			sc.speak("schedule %s: the already-fired note for %s failed to record: %v", s.ID, wall, err)
			continue
		}
		sc.speak("schedule %s: %s occurred twice — recorded already-fired (the fire was the first pass)", s.ID, wall)
	}
	for _, m := range missing {
		if !m.Nominal.After(covered) {
			continue
		}
		f := Fire{
			TriggerID: s.ID, Blueprint: s.Blueprint, Workspace: s.Workspace, Goal: s.Goal,
			Provenance: ProvenanceTrigger, Instant: m.Nominal,
		}
		f.Reason = "daylight saving: wall time " + m.Wall + " does not exist in " + s.Zone +
			" (clocks skipped forward) — the schedule is skipped and recorded (TR6)"
		f.Outcome = OutcomeSkipped
		if err := sc.Store.RecordFire(ctx, f); err != nil {
			sc.speak("schedule %s: the does-not-exist note for %s failed to record: %v", s.ID, m.Wall, err)
			continue
		}
		sc.speak("schedule %s: %s does not exist in %s — skipped and recorded", s.ID, m.Wall, s.Zone)
	}
}

// lastExplanation is the newest instant any DST answer explains (a collapsed
// pass or a missing wall) — the frontier advances PAST the explanations so
// the next walk starts where the story ended, not where the last fire did.
func lastExplanation(dropped []time.Time, missing []missingWall) (time.Time, bool) {
	var last time.Time
	for _, t := range dropped {
		if t.After(last) {
			last = t
		}
	}
	for _, m := range missing {
		if m.Nominal.After(last) {
			last = m.Nominal
		}
	}
	return last, !last.IsZero()
}
