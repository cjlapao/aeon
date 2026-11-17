package trigger

import (
	"context"
	"time"
)

// Clock is the slice's testability seam, and it arrived in the first line
// rather than retro-fitted: a clock added later for tests is a clock with
// two code paths, and the one the tests do not use is the one that runs at
// 3am. Every test in S20 moves time explicitly through this interface;
// TestNoTimeNowOutsideTheSystemClock keeps `time.Now` to exactly one
// occurrence in this package — the SystemClock's own line.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production wiring, and the ONE honest home of
// time.Now here (pinned by count, not by convention).
type SystemClock struct{}

// Now is the sole time.Now in the package. TestNoTimeNowOutsideTheSystemClock
// counts occurrences of it across the non-test sources and fails the day a
// second one appears — a schedule path reading the wall directly is a path
// no test can move.
func (SystemClock) Now() time.Time { return time.Now() }

// Starter is the fire path's ONLY door to loop creation, and its signature
// is D1's guard: a Fire carries blueprint, workspace, goal and inputs —
// there is no parameter through which "treat this as attended" or an
// outward grant could be smuggled, and TestStarterCarriesNoHint asserts
// that against the struct. The implementation pins attendedness to
// unattended itself (the daemon's own hard-coded false), on the same path a
// person's start walks.
type Starter interface {
	StartFromTrigger(ctx context.Context, f Fire) (loopID string, err error)
}

// RowStore is the scheduler's side of the store: read what is active,
// answer the fire questions, advance the frontier. An interface so every
// tick test is a fake with no database — and so the daemon can log which
// door it is answering from.
type RowStore interface {
	ActiveSchedules(ctx context.Context) ([]Schedule, error)
	SetCovered(ctx context.Context, id string, coveredUntil string) error
	RecordFire(ctx context.Context, f Fire) error
	// RecordGap writes the range-and-count answer for the instants between
	// the frontier and the newest one (reconcile, T4) — one row per outage,
	// never one row per instant (0034's stated shape).
	RecordGap(ctx context.Context, id, coveredFrom, coveredTo string, missed int, at time.Time) error
	// SaveTriggerSchedule writes a row back — control-plane use only (T8:
	// pause/resume flip Paused on a row already read from ActiveSchedules).
	// The upsert KEEPS the covered_until the row was loaded with, so a
	// pause cannot move the frontier by rewriting the row (Q3's frontier
	// stands for the paused schedule in both directions).
	SaveTriggerSchedule(ctx context.Context, s Schedule) error
}

// Gate holds the two checks this task places in the order and later tasks
// fill. Nil means NOT WIRED, and the scheduler says so out loud at Run —
// it never pretends a check ran. T3 replaces Overlap; T7 replaces Access
// (whose honest pre-T7 answer is TriggerAccess's stub: could-not-determine).
type Gate struct {
	// Overlap is asked before the fire: is this schedule's previous run
	// still going (running or suspended — Q2's stated answer, T3)?
	Overlap func(s Schedule, at time.Time) (blocked bool, previousLoopID string)
	// Access is asked last, immediately before starting: does governance
	// let this kind fire at all? Authentication is not authorization (T7):
	// a verified webhook answers this too. The trigger id rides beside the
	// kind so the verdict's subject NAMES the actor ("refused: agent:ship")
	// and an operator may author a deny for one trigger, not only a kind.
	Access func(provenance, triggerID string) (allow bool, reason string)
}

// Scheduler walks the active schedules and answers "did this fire?" for
// every instant that has arrived. It owns no goroutine of its own beyond
// Run, holds no store handle family of its own (the RowStore is injected),
// and takes its time from the Clock — so every scenario in this slice
// (missed weeks, doubled hours, overlaps, windows) is a function call in a
// test, not a sleep in a suite.
type Scheduler struct {
	Clock   Clock
	Store   RowStore
	Starter Starter
	Gate    Gate
	// TickEvery is the production poll interval; a minute-granular clock
	// never misses because 15 s < the finest schedule. It is a field, not
	// a constant, so Run tests move it rather than the clock.
	TickEvery time.Duration
	// Speak reports what the scheduler did and what it could not do
	// (startup says which gates are unwired; refusals and broken rows are
	// spoken, never fatal — a poisoned schedule row must not stop the
	// other schedules from ticking).
	Speak func(format string, args ...any)
}

// NewScheduler wires the defaults: the speak function is absent (silent),
// TickEvery is 15 seconds, and both gates are nil — the honest pre-T3/T7
// state, announced at Run.
func NewScheduler(store RowStore, starter Starter, clock Clock) *Scheduler {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Scheduler{Clock: clock, Store: store, Starter: starter, TickEvery: 15 * time.Second}
}

// Tick evaluates every active schedule against the instant `at`, answering
// the fire walk in the order the design lays it — pause is not a fire-walk
// step (a paused schedule answers nothing; it is not firing), then the
// schedule's own window, then overlap (T3), then governance (T7), then the
// start. An instant that passes any gate is RECORDED with the reason; the
// frontier advances for every instant the scheduler answered.
func (sc *Scheduler) Tick(ctx context.Context, at time.Time) error {
	scheds, err := sc.Store.ActiveSchedules(ctx)
	if err != nil {
		return err
	}
	for _, s := range scheds {
		if s.Kind != KindSchedule || s.Paused {
			continue
		}
		loc, err := LoadZone(s.Zone)
		if err != nil {
			sc.speak("schedule %s: %v — skipped this tick (it stored against a zone that does not load; fix the row)", s.ID, err)
			continue
		}
		expr, err := s.ScheduleExpr()
		if err != nil {
			sc.speak("schedule %s: %v — skipped this tick (write-time validation would refuse this row today; it predates the check)", s.ID, err)
			continue
		}
		// The due frontier: the LATEST scheduled instant in (covered, at].
		// The middle instants of a gap are reconcile's answer (T4), written
		// at startup; a tick's contract is "answer the newest instant,
		// advance to it" — and fireAt below is the SAME walk reconcile
		// takes, so the two paths cannot drift on what "fire" means.
		covered := parseTime(s.CoveredUntil)
		kept, dropped, missing, capped := walkInstants(expr, loc, covered, at)
		if capped {
			sc.speak("schedule %s: the gap behind the frontier is unrealistically wide — left untouched; reconcile speaks at startup", s.ID)
			continue
		}
		if len(kept) == 0 {
			// Nothing left to fire after the DST collapse — the window WAS
			// all second-passes of a doubled wall time. Record the
			// explanations once and advance past them, or every 15 s tick
			// re-walks the same hour and re-explains it (the UNIQUE index
			// would turn the explanation into an argument).
			if last, ok := lastExplanation(dropped, missing); ok && last.After(covered) {
				sc.recordDSTAnswers(ctx, s, covered, dropped, missing)
				sc.advance(ctx, s.ID, last)
			}
			continue
		}
		due := kept[len(kept)-1]
		if !due.After(covered) {
			continue
		}
		sc.fireAt(ctx, s, loc, expr, due, at) // the clock reads the record, not the answer
		sc.recordDSTAnswers(ctx, s, covered, dropped, missing)
		if last, ok := lastExplanation(dropped, missing); ok && last.After(due) {
			sc.advance(ctx, s.ID, last) // the frontier answers the explanations too
		}
	}
	return nil
}

// fireAt is THE fire walk, in one place: window (the schedule's own
// contract), overlap (T3's hook), governance (T7's hook), then the start
// through the human's own path. Tick and Reconcile both end here — one
// definition of "fire" cannot be smuggled past a gate by a second caller.
// fireAt returns the fire as it answered, plus the reason it gave — the
// clock walk does not care (Tick and Reconcile ignore both; the frontier
// is what they read from the store), but fire-now (T8) answers a PERSON
// and must report the same outcome the record carries, in the same words.
func (sc *Scheduler) fireAt(ctx context.Context, s Schedule, loc *time.Location, expr cronLike, due, at time.Time) (Fire, string) {
	{
		fire := Fire{
			TriggerID: s.ID, Blueprint: s.Blueprint, Workspace: s.Workspace,
			Goal: s.Goal, Provenance: ProvenanceTrigger,
			Instant: due, At: at,
		}
		// 1. The schedule's own contract: the working-hours window (TR8,
		//    D7). Excluded, it did not fire — and the reason is the
		//    sentence, not a shrug.
		if ok, reason := s.InWindow(due, loc); !ok {
			sc.answer(ctx, s, fire, OutcomeDidNotFire, reason)
			fire.Outcome = OutcomeDidNotFire // the ANSWER, for the caller who reports to a person (T8)
			return fire, reason
		}
		// 2. Overlap (T3 fills the hook; unwired means the check has not
		//    been written yet, and Run said so — the scheduler does not
		//    infer "no overlap" from an absent hook).
		if sc.Gate.Overlap != nil {
			if blocked, prev := sc.Gate.Overlap(s, due); blocked {
				reason := "the previous run is still going (loop " + prev + ") — an overlapping fire is skipped and recorded (TR5)"
				sc.answer(ctx, s, fire, OutcomeSkipped, reason)
				fire.Outcome = OutcomeSkipped
				return fire, reason
			}
		}
		// 3. Governance (T7 fills the hook): a denied kind cannot start a
		//    loop — signature-verified or not, schedule-timed or not.
		if sc.Gate.Access != nil {
			// s.Kind, not the word "schedule": fire-now can reach here
			// with a MANUAL row, and governance must be asked about the
			// kind that actually fires (the closed three mean something).
			if allow, reason := sc.Gate.Access(string(s.Kind), s.ID); !allow {
				sc.answer(ctx, s, fire, OutcomeRefused, reason)
				fire.Outcome = OutcomeRefused
				return fire, reason
			}
		}
		// 4. The start — through the human's own path, with the starter's
		//    own hard-coded unattendedness. A start that is REFUSED (the
		//    blueprint is invalid, the workspace moved) is an answered
		//    question: record the sentence and advance. Re-starting a
		//    refused fire on a timer would restart a blocked loop forever,
		//    and "no retry of a fire" guards S17's caps (design's not-do).
		loopID, err := sc.Starter.StartFromTrigger(ctx, fire)
		if err != nil {
			reason := "the start was refused: " + err.Error()
			sc.answer(ctx, s, fire, OutcomeRefused, reason)
			fire.Outcome = OutcomeRefused
			return fire, reason
		}
		fire.Outcome, fire.LoopID = OutcomeFired, loopID
		if err := sc.Store.RecordFire(ctx, fire); err != nil {
			sc.speak("schedule %s: the fire at %s started loop %s but the record failed: %v", s.ID, due.Format(time.RFC3339), loopID, err)
		}
		sc.advance(ctx, s.ID, due)
		return fire, ""
	}
}

// Run is the daemon's loop: tick on TickEvery with the clock's now. Nothing
// here consults the wall directly — the clock is asked, so a test that
// wants the production loop moves the fake and drives Tick itself; Run
// exists for the one caller that has a real clock and a context.
func (sc *Scheduler) Run(ctx context.Context) {
	if sc.Speak != nil {
		sc.speak("trigger scheduler running (tick %v, zone-true schedules)", sc.TickEvery)
		for _, line := range sc.WiredReport() {
			sc.speak("  %s", line)
		}
	}
	every := sc.TickEvery
	if every <= 0 {
		every = 15 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := sc.Tick(ctx, sc.Clock.Now()); err != nil && sc.Speak != nil {
				sc.speak("trigger tick failed: %v", err)
			}
		}
	}
}

// WiredReport states, for the startup log and any future health surface,
// which gates are NOT wired. The scheduler never infers "no overlap" or
// "governance passed" from an absent hook — and it does not hide the
// absence either (P31: the honest third state, printed).
func (sc *Scheduler) WiredReport() []string {
	var out []string
	if sc.Gate.Overlap == nil {
		out = append(out, "overlap NOT wired — overlapping fires will not be skipped until T3 lands")
	}
	if sc.Gate.Access == nil {
		out = append(out, "access NOT wired — TriggerAccess is not consulted until T7 lands")
	}
	return out
}

// answer records a non-fired outcome and advances the frontier: the
// question was ASKED and ANSWERED, and an unanswered-looking gap is
// exactly what the record exists to prevent.
func (sc *Scheduler) answer(ctx context.Context, s Schedule, f Fire, o Outcome, reason string) {
	f.Outcome, f.Reason = o, reason
	if err := sc.Store.RecordFire(ctx, f); err != nil {
		sc.speak("schedule %s: recording %s for %s failed: %v", s.ID, o.Render(), f.Instant.Format(time.RFC3339), err)
	}
	sc.advance(ctx, s.ID, f.Instant)
}

func (sc *Scheduler) advance(ctx context.Context, id string, until time.Time) {
	if err := sc.Store.SetCovered(ctx, id, until.UTC().Format(time.RFC3339Nano)); err != nil {
		sc.speak("schedule %s: advancing covered_until failed: %v", id, err)
	}
}

func (sc *Scheduler) speak(format string, args ...any) {
	if sc.Speak != nil {
		sc.Speak(format, args...)
	}
}

// latestDue walks the schedule's instants from the frontier to `at` and
// returns the newest one due. The walk is bounded: an absurdly stale
// frontier cannot hang the tick (the loop counts, and the caller learns the
// gap was huge — T4's reconcile is where gaps are ANSWERED with
// could-not-say rows; T2 only advances past them).
func latestDue(expr cronLike, loc *time.Location, covered, at time.Time) (time.Time, bool) {
	if covered.IsZero() {
		covered = at.Add(-24 * time.Hour)
	}
	instants, _, _, capped := walkInstants(expr, loc, covered, at)
	if capped || len(instants) == 0 {
		// Capped means the frontier is not merely old, it is wrong — no
		// real outage produced it. Guessing a newest instant (or hanging the
		// tick walking to it) is not an option; the tick answers nothing and
		// says so (T4's reconcile owns loud gap answers).
		return time.Time{}, false
	}
	due := instants[len(instants)-1]
	return due, due.After(covered)
}

// walkInstants is THE bounded walk — one loop, shared by the tick and
// reconcile so the two can never disagree about which instants were missed
// or drift on what "due" means. It returns every instant in (from, to] and
// says when it gave up: walkCap instants is ~14 days of a minutely
// schedule, and past that the frontier is corrupt data, not an outage. A
// non-advancing expression (robfig's one degenerate shape) breaks as
// broken data, never as a loop.
func walkInstants(expr cronLike, loc *time.Location, from, to time.Time) (kept, dropped []time.Time, missing []missingWall, capped bool) {
	var raw []time.Time
	t := expr.Next(from.In(loc))
	for !t.IsZero() && !t.After(to) {
		raw = append(raw, t)
		if len(raw) > walkCap {
			return raw, nil, nil, true
		}
		next := expr.Next(t)
		if next.IsZero() || !next.After(t) {
			break // a non-advancing schedule is broken data, not a loop
		}
		t = next
	}
	// The two days a year (T5/dst.go): doubled wall times collapse to
	// their first pass, and a pinned wall time that vanished between two
	// consecutive instants is found, not guessed. The scan needs the
	// instant BEFORE the frontier — the missing day usually sits between
	// the last ANSWERED fire and the next one, and the window starts after
	// the frontier — so it looks back one bounded day for its left edge.
	kept, dropped = collapseDoubled(raw, loc)
	pairs := raw
	if pre := lastBefore(expr, loc, from.Add(-26*time.Hour), from); !pre.IsZero() {
		pairs = append([]time.Time{pre}, raw...)
	}
	for i := 1; i < len(pairs); i++ {
		missing = append(missing, findMissing(pairs[i-1], pairs[i], loc)...)
	}
	return kept, dropped, missing, false
}

// lastBefore walks forward from start and returns the last instant at or
// before end — the bounded way to get the step BEHIND a frontier (cronLike
// has no Prev; a day of look-back costs at most 1,440 minutely steps, and
// the caller only wants the newest).
func lastBefore(expr cronLike, loc *time.Location, start, end time.Time) time.Time {
	var last time.Time
	t := expr.Next(start.In(loc))
	for !t.IsZero() && !t.After(end) {
		last = t
		next := expr.Next(t)
		if next.IsZero() || !next.After(t) {
			break
		}
		t = next
	}
	return last
}

// cronLike is robfig's Schedule narrowed to the one method used — so tests
// can hand in scripted instants (DST's doubled and missing hours in T5
// arrive as scripted walks, not as calendar tricks).
type cronLike interface {
	Next(time.Time) time.Time
}

// parseTime reads a covered_until stamp; "" and unparseable both read as
// "no frontier" (the first tick's job is then to establish one).
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
