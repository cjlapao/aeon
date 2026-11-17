package trigger

import (
	"context"
	"fmt"
	"time"
)

// reconcile.go — TR3 and TR4 meeting at daemon start, where the naive
// implementation is a thundering herd and the quiet one is silence.
//
// TR3: **at most one catch-up, never a burst.** A daily job missed for a
// week starts ONE loop (the newest instant — the one whose answer is still
// worth giving), not seven; a minutely job missed for a week starts ONE,
// not ten thousand. The missed middle is not a backlog to work through:
// each of those loops would believe it is today's (design's burst
// paragraph), and S27 — not this file — decides what to do when work piles
// up.
//
// TR4: **the missed instants are RECORDED as could-not-say.** The daemon was
// down; whether each one would have fired is unknowable (the gates run with
// the live store, and the live store was not live). "Absent from the log"
// is the failure mode this task exists to kill: a gap that reads as
// never-should-have-happened makes the nightly job look unreliable when the
// daemon was the thing that was down.
//
// **Q3, answered: a PAUSED schedule records nothing.** Paused means it
// would not have fired — there is nothing to be uncertain about, and a
// schedule accumulating uncertainty rows while switched off is noise that
// hides the real gaps (the design's answer, adopted and stated here because
// ActiveSchedules' pause filter is where it becomes true).

// walkCap bounds the gap walk: a corrupt frontier pointing centuries back
// cannot hang startup. It must sit ABOVE the largest honest outage — the
// design's own example is 10,079 minutes of a week, so the cap is 20,000
// (~14 days of a minutely schedule). Past the cap the frontier is not old,
// it is WRONG: the schedule is left untouched, loudly, for a human, rather
// than firing a guessed catch-up or hanging startup counting to the epoch.

const walkCap = 20000

// Reconcile is the startup catch-up, called by the daemon BEFORE Run: for
// every active (not-paused) schedule with missed instants behind its
// frontier, it fires exactly one — the newest, through the SAME fireAt walk
// a tick takes (one definition of "fire", no second path past the gates) —
// and records the middle as one range-and-count gap row.
func (sc *Scheduler) Reconcile(ctx context.Context, now time.Time) error {
	scheds, err := sc.Store.ActiveSchedules(ctx)
	if err != nil {
		return err
	}
	for _, s := range scheds {
		if s.Kind != KindSchedule {
			continue // webhooks have no clock to reconcile
		}
		if s.Paused {
			// Q3, enforced HERE and not only by the store's active-set
			// filter: a paused schedule records NOTHING. It would not have
			// fired, so there is nothing to be uncertain about, and
			// uncertainty rows accumulating on a switched-off schedule are
			// noise hiding the real gaps. Defence in depth — the filter is
			// the policy, this line is the promise.
			continue
		}
		loc, err := LoadZone(s.Zone)
		if err != nil {
			sc.speak("reconcile %s: zone does not load: %v — left untouched", s.ID, err)
			continue
		}
		expr, err := s.ScheduleExpr()
		if err != nil {
			sc.speak("reconcile %s: expression rejected: %v — left untouched", s.ID, err)
			continue
		}
		covered := parseTime(s.CoveredUntil)
		if covered.IsZero() {
			// No frontier at all: the schedule has never been answered, but
			// it was never DUE either — the record begins from its first
			// real instant; back-filling from the epoch is inventing gaps.
			continue
		}
		// The SAME bounded walk the tick takes (walkInstants): missed =
		// every instant in (covered, now]. Past walkCap the frontier is not
		// old, it is WRONG — no real outage walks 14 days of minutes — and
		// the honest startup answer is loud silence: leave the schedule
		// untouched for a human to look at the row, rather than firing a
		// guessed catch-up or hanging startup counting to the epoch.
		kept, dropped, missing, capped := walkInstants(expr, loc, covered, now)
		if capped {
			sc.speak("reconcile %s: the gap behind the frontier is unrealistically wide (%d+ instants) — left untouched; the covered_until row needs a human look", s.ID, walkCap)
			continue
		}
		if len(kept) == 0 {
			// The whole outage fell inside a doubled wall time: nothing to
			// catch up, the explanations are the answer (recorded once,
			// frontier past them).
			if last, ok := lastExplanation(dropped, missing); ok && last.After(covered) {
				sc.recordDSTAnswers(ctx, s, covered, dropped, missing)
				sc.advance(ctx, s.ID, last)
			}
			continue
		}
		newest := kept[len(kept)-1]
		// The middle: could-not-say, as ONE range+count row (0034's shape).
		if gapCount := len(kept) - 1; gapCount > 0 {
			from := kept[0].UTC().Format(time.RFC3339Nano)
			to := newest.UTC().Format(time.RFC3339Nano)
			if err := sc.Store.RecordGap(ctx, s.ID, from, to, gapCount, now); err != nil {
				sc.speak("reconcile %s: gap record failed: %v — the fire still proceeds", s.ID, err)
			}
			sc.speak("reconcile %s: %d instants could not be said (%s … %s) — the daemon was silent",
				s.ID, gapCount, kept[0].Format(time.RFC3339), newest.Add(-time.Second).Format(time.RFC3339))
		}
		// One catch-up, through the tick's own walk.
		sc.speak("reconcile %s: one catch-up for %s", s.ID, newest.Format(time.RFC3339))
		sc.fireAt(ctx, s, loc, expr, newest, now)
		sc.recordDSTAnswers(ctx, s, covered, dropped, missing)
		if last, ok := lastExplanation(dropped, missing); ok && last.After(newest) {
			sc.advance(ctx, s.ID, last)
		}
	}
	return nil
}

// ReconcileSummary renders what startup found, for the walk and the log
// (S10's T11 precedent: the renderer lives beside the type it renders, and
// surfaces print it verbatim).
func ReconcileSummary(gap TriggerGapView, fired *Fire) string {
	if fired != nil {
		return fmt.Sprintf("fired %s at %s%s", fired.TriggerID, fired.Instant.Format(time.RFC3339), gapSuffix(gap))
	}
	return fmt.Sprintf("%s: %d instants could not say%s", gap.ScheduleID, gap.Missed, gapSuffix(gap))
}

func gapSuffix(g TriggerGapView) string {
	if g.Missed == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d earlier instants could not be said (%s … %s)", g.Missed, g.CoveredFrom, g.CoveredTo)
}

// TriggerGapView is the read-side of the gap row (the store's row shape
// narrowed to what history renders — kept here so the renderer and its
// input are one page apart).
type TriggerGapView struct {
	ScheduleID  string
	CoveredFrom string
	CoveredTo   string
	Missed      int
	At          string
}
