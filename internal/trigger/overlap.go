package trigger

// overlap.go — TR5 made a predicate, and Q2 answered in this file so the
// answer sits where the behaviour is read.
//
// **Q2, answered: a previous run suspended on a question BLOCKS the next
// fire — and on the real schema, that state is `waiting`, not `suspended`.**
// The design's Q2 says "a loop suspended on a question is neither running
// nor finished"; measured against the code (T3's audit before its first
// commit), S6's suspension lives on the STEP (the `suspended` step status,
// the reason held in `question`), and the LOOP row the scheduler reads says
// `waiting` while a person owes that answer. `suspended` and `orphaned` are
// STEP-table vocabulary; they never appear in loop.status (the column has
// no CHECK at all — the engine's SavePinnedRuntime owns the set: running,
// waiting, blocked, done, aborted). A predicate that blocked on the design's
// literal word would have blocked nothing at all — the trap the task file
// warned about, arriving from the other direction: a test written against
// the guess rather than the row.
//
// The ruling, on the loop row's own vocabulary:
//
//   - `running`   — TR5's core: the previous run owns the one-at-a-time
//     slot; an overlap is a signal (S5 is serial) and the skip records it.
//   - `waiting`   — Q2's named case: a human owes this loop an answer.
//     Starting a second copy would make the question they eventually answer
//     belong to a loop gone stale beside the new one.
//   - `blocked`   — Q2's sibling, same reasoning: the loop is parked at a
//     wall (outward denial, policy refusal) for a human to find in the
//     morning. A second copy duplicates the wall, the confusion and the
//     work.
//   - `done`, `aborted` — finished states own nothing; the next fire goes.
//   - anything else (`orphaned`, `suspended`, `stopped`, a typo) — does NOT
//     block and deserves a loud log elsewhere. The asymmetry is deliberate:
//     a status the engine no longer writes must not silence a schedule
//     forever, and the fire's own start path consults no status at all, so
//     an unknown cannot be smuggled into permission the way attendedness
//     was the danger (R-3).

// StillGoing is the Q2 ruling as a function: which LOOP-row statuses mean
// the previous run still owns the schedule's one-at-a-time slot.
func StillGoing(status string) bool {
	switch status {
	case "running", "waiting", "blocked":
		return true
	}
	return false
}
