package history

// ════════════════════════════════════════════════════════════════════════════
// The vocabulary (S53 T1, HV2): the closed words this package can speak, served
// as data so no client types a lane or an outcome word. Every word here is a
// constant declared IN this package and never restated: the lanes are the
// GroupBy constants (lanes.go), the outcome words are the statuses the loop
// engine itself writes (named at each origin below), the filter keys are the
// params the reads take. `history.vocabulary` serves this view; the UI renders
// from it (HV7); TestVocabularyEqualsTheConstants mutates a constant at the
// seam to prove the view is derived, not a second list that could rot.
//
// The pass-through rule (S25's own law, design line 65 — "the surface that
// decides how to phrase an outcome is a second truth"): a stored status OUTSIDE
// this set — a word some older row carries — renders VERBATIM on a row and is
// never collapsed into a known word. The vocabulary is what a surface may
// LABEL; it is never a filter that hides an honest row.
// ════════════════════════════════════════════════════════════════════════════

// The outcome words: every status value the loop engine writes, at its origin.
// running — loop start and resume (loop_control.go's CAS writes 'running');
// waiting — a park: stop, gate, budget (ParkLoopStopped writes 'waiting');
// blocked — a stuck or cap-hit halt (the raise door reads 'blocked' by name);
// done | aborted | idle — the ONE terminal writer, endStatus (loop/rest.go:29:
// the aborted flag first, a converse-blueprint loop rests 'idle', else 'done');
// failed — the run-failed flag on a resting loop (loop/rest.go:48).
const (
	OutcomeRunning = "running"
	OutcomeWaiting = "waiting"
	OutcomeBlocked = "blocked"
	OutcomeDone    = "done"
	OutcomeIdle    = "idle"
	OutcomeAborted = "aborted"
	OutcomeFailed  = "failed"
)

// OutcomeWords is the closed outcome set, in the order a person reads a run:
// still moving, parked, halted, then the endings. It is a VIEW over the
// constants above (the same five laws this house holds elsewhere: AllPolicyTypes
// et al. — widen a word HERE and the vocabulary moves with it; restate it in a
// client and the equality test cannot save you).
var OutcomeWords = []string{
	OutcomeRunning, OutcomeWaiting, OutcomeBlocked,
	OutcomeDone, OutcomeIdle, OutcomeAborted, OutcomeFailed,
}

// LaneWords is the closed lane set — the four GroupBy constants of lanes.go,
// served in H1's order. ByLoop is the default and the merge itself.
var LaneWords = []string{string(ByLoop), string(ByPhase), string(ByTask), string(ByAgent)}

// FilterKeys are the params the history reads take: projectRoot (CT15's wire
// name for a root component's scope, carried from S25 unchanged), componentRoot
// (coverage's second scope key, HV1), groupBy (the lane switch) and since (the
// RFC3339 lower bound). A key not here is refused by name.
var FilterKeys = []string{"projectRoot", "componentRoot", "groupBy", "since"}

// VocabularyView is history.vocabulary's answer: the closed sets as data.
type VocabularyView struct {
	Lanes    []string `json:"lanes"`
	Outcomes []string `json:"outcomes"`
	Filters  []string `json:"filters"`
}

// Vocabulary is the ONE answer history.vocabulary gives: fresh copies of the
// derived sets (the caller cannot corrupt the package's own slices), read from
// the constants at call time — never a cached literal.
func Vocabulary() VocabularyView {
	lanes := make([]string, len(LaneWords))
	copy(lanes, LaneWords)
	outcomes := make([]string, len(OutcomeWords))
	copy(outcomes, OutcomeWords)
	filters := make([]string, len(FilterKeys))
	copy(filters, FilterKeys)
	return VocabularyView{Lanes: lanes, Outcomes: outcomes, Filters: filters}
}
