package scorecard

// vocabulary.go — what the metrics MEAN, published (RT7: the vocabulary
// is surfaced, not folklore). Descriptions live HERE, once: the socket's
// scorecard.vocabulary and every tool spec quote these words rather
// than re-inventing them (one formatter's law, applied to meanings).

// MetricInfo names one metric and what it measures.
type MetricInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"` // component|blueprint|agent|shared
}

// Metrics is the published vocabulary. It is a function returning a
// fresh slice — the package keeps NO state across calls (N8), so there
// is nothing to mutate behind a caller's back.
func Metrics() []MetricInfo {
	return []MetricInfo{
		{"loops", "distinct loops with a recorded outcome in the window", "shared"},
		{"finishedDone", "loops whose latest outcome is done", "shared"},
		{"aborted", "loops whose latest outcome is aborted", "shared"},
		{"blocked", "loops that ever blocked, window-free (rule 1)", "shared"},
		{"doneRate", "finishedDone / loops", "shared"},
		{"abortedRate", "aborted / loops", "shared"},
		{"blockedRate", "ever-blocked / loops", "shared"},
		{"firstPassRate", "done loops with zero retries, zero unverified steps and no block ever / judged done loops", "shared"},
		{"reworkPerItem", "mean of retries+review+security+qa rounds per judged loop", "shared"},
		{"reviewRounds", "mean review rounds per judged loop", "shared"},
		{"securityRounds", "mean security rounds per judged loop", "shared"},
		{"unverifiedRate", "unverified steps / verdict-carrying steps", "shared"},
		{"escalations", "escalation questions recorded over judged loops", "shared"},
		{"escalationRate", "escalations / judged loops", "shared"},
		{"costPerCompletedItem", "a floor: Σ cost floors over done loops with cost data / their count; never 0 when unknown", "shared"},
		{"wallPerItem", "median (and p90 in detail) of wall_ms, nearest rank, never a mean", "shared"},
		{"gatePassRate", "needs S57 (gate results) — cannot-say until wired", "shared"},
		{"mergedThenRevertedRate", "needs S57 (revert facts) — cannot-say until wired", "shared"},
		{"completionByVersion", "done rate split by blueprint_version (blueprint cards)", "blueprint"},
		{"stepsRun", "steps attributed to this agent in the window", "agent"},
		{"stepRejectRate", "rejected / run steps", "agent"},
		{"verifyFailRate", "failed / verdict-carrying steps", "agent"},
		{"verifyUnverifiedRate", "unverified / all verification decisions", "agent"},
		{"costPerStep", "a floor: Σ cost floors / Σ steps, with unknown counts", "agent"},
	}
}

// Milestones is the same ladder the milestone learner writes its rows at
// (T3): loops one has completed at which the card says something. The
// GENERATION lives here, not imported from internal/retro — importing
// the write-side package from the read-side one would drag learnings
// into scorecard's import set (its Do-NOT), so the ladder is generated
// by the same formula and pinned EQUAL by TestMilestonesMatchTheLearner.
func Milestones() []int {
	out := []int{1}
	for out[len(out)-1] < 1000000 {
		next := float64(out[len(out)-1]) * 3.33
		v := roundOneSig(next)
		if v <= out[len(out)-1] {
			break
		}
		out = append(out, v)
	}
	return out
}

// roundOneSig rounds to one significant digit, the learner's own rule
// (3.33 → 3, 9.99 → 10).
func roundOneSig(v float64) int {
	if v < 10 {
		return int(v + 0.5)
	}
	mag := 1.0
	for mag*10 <= v {
		mag *= 10
	}
	return int(v/mag+0.5) * int(mag)
}

// WindowPreset is one suggested window the doors and screens offer
// (RT10's `windows` answer). Days is the lookback; the names are wire
// words, fixed here so a client never invents one.
type WindowPreset struct {
	Name string `json:"name"`
	Days int    `json:"days"`
}

// Windows publishes the presets. A function, not a var — the package
// keeps no state across calls (N8).
func Windows() []WindowPreset {
	return []WindowPreset{{"7d", 7}, {"30d", 30}, {"90d", 90}}
}
