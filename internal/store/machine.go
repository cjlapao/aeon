package store

// The task status machine, ported from v1's `packages/loop-state-mcp/src/validation.ts`.
//
// # This file encodes experience, not design
//
// `AllowedTransitions` is not something anyone reasoned out from first
// principles. It is the accumulated record of which task-status moves turned
// out to be legal in practice, and `PocTransitionOverrides` exists because one
// loop mode needed to bypass some of them. It is ported with its names kept —
// later slices' specs were written against them — and re-deriving it would
// discard what was learned.
//
// It has NO IMPORTS, not even the standard library, and nothing in it touches
// the database. A pure table plus three functions over it, which is why its
// test can enumerate the table rather than list cases: a hand-written list of
// legal transitions passes forever after somebody adds one.
//
// `Caps` is published as DATA because later slices read the same numbers. Two
// copies of a cap eventually disagree, and the disagreement presents as a loop
// stopping for no visible reason.

// TaskStatus is a rung on the task ladder. The type is derived from the table
// rather than declared beside it, so a status cannot exist without an entry.
type TaskStatus string

// The task ladder, in order. v1's spec §6 task.status enum.
const (
	StatusPending          TaskStatus = "pending"
	StatusIssueOpen        TaskStatus = "issue-open"
	StatusInProgress       TaskStatus = "in-progress"
	StatusVerified         TaskStatus = "verified"
	StatusSecure           TaskStatus = "secure"
	StatusPROpen           TaskStatus = "pr-open"
	StatusInReview         TaskStatus = "in-review"
	StatusChangesRequested TaskStatus = "changes-requested"
	StatusMerged           TaskStatus = "merged"
	StatusComplete         TaskStatus = "complete"
	StatusFailed           TaskStatus = "failed"
	StatusBlocked          TaskStatus = "blocked"
	StatusSkipped          TaskStatus = "skipped"
	StatusSuperseded       TaskStatus = "superseded"
)

// LoopMode is how a loop runs. `simple` and `standard` differ only in whether
// discovery runs, which is the orchestrator's behaviour and not enforced here.
// `poc` is STRUCTURAL: it removes the PR lifecycle from the ladder.
type LoopMode string

const (
	ModeSimple   LoopMode = "simple"
	ModeStandard LoopMode = "standard"
	ModePoc      LoopMode = "poc"
)

// LoopPhase is the loop's phase pointer. v1's spec §6 loop.phase enum.
type LoopPhase string

const (
	PhasePreflight    LoopPhase = "preflight"
	PhaseClarify      LoopPhase = "clarify"
	PhaseDiscovery    LoopPhase = "discovery"
	PhaseLegal        LoopPhase = "legal"
	PhasePlan         LoopPhase = "plan"
	PhaseGate         LoopPhase = "gate"
	PhaseBuild        LoopPhase = "build"
	PhaseQA           LoopPhase = "qa"
	PhaseFindingsGate LoopPhase = "findings-gate"
	PhaseMemory       LoopPhase = "memory"
	PhaseFeatureGate  LoopPhase = "feature-gate"
	PhaseDone         LoopPhase = "done"
	PhaseBlocked      LoopPhase = "blocked"
)

// TaskStatuses is every rung, in ladder order.
//
// Order matters for presentation and for the cartesian-product test, which
// walks this against itself.
func TaskStatuses() []TaskStatus {
	return []TaskStatus{
		StatusPending, StatusIssueOpen, StatusInProgress, StatusVerified,
		StatusSecure, StatusPROpen, StatusInReview, StatusChangesRequested,
		StatusMerged, StatusComplete, StatusFailed, StatusBlocked,
		StatusSkipped, StatusSuperseded,
	}
}

// LoopPhases is every phase, in order.
func LoopPhases() []LoopPhase {
	return []LoopPhase{
		PhasePreflight, PhaseClarify, PhaseDiscovery, PhaseLegal, PhasePlan,
		PhaseGate, PhaseBuild, PhaseQA, PhaseFindingsGate, PhaseMemory,
		PhaseFeatureGate, PhaseDone, PhaseBlocked,
	}
}

// TrackedPhases is the durable `## Phases` ladder.
//
// A SUPERSET of LoopPhases: it deliberately includes `agents-sync` and
// `feature-merge`, which are tracked but are not phase-pointer values. Ported
// as-is; collapsing the two lists would lose a distinction v1 found it needed.
func TrackedPhases() []string {
	return []string{
		"clarify", "discovery", "legal", "plan", "build", "qa",
		"findings-gate", "memory", "agents-sync", "feature-merge",
	}
}

// PhaseStatuses is the per-phase status set for that tracker.
func PhaseStatuses() []string {
	return []string{
		"pending", "in-progress", "done", "skipped",
		"failed", "blocked", "not-needed", "left-open",
	}
}

// AllowedTransitions is the forward ladder.
//
// From v1, unchanged. Two properties worth naming because they are the ones a
// reader checks: `complete` is reachable ONLY from `merged`, and `merged` only
// from `in-review` — that is, only after approval.
//
// Returned as a fresh map rather than exposed as a package variable: a caller
// holding the package's own map could mutate the machine for every other
// caller in the process, and the symptom would be a transition that is legal
// in one goroutine and not another.
func AllowedTransitions() map[TaskStatus][]TaskStatus {
	return map[TaskStatus][]TaskStatus{
		StatusPending:          {StatusIssueOpen, StatusInProgress, StatusSkipped, StatusBlocked},
		StatusIssueOpen:        {StatusInProgress, StatusBlocked},
		StatusInProgress:       {StatusVerified, StatusChangesRequested, StatusBlocked, StatusFailed},
		StatusVerified:         {StatusSecure, StatusFailed, StatusInProgress},
		StatusSecure:           {StatusPROpen, StatusInProgress, StatusBlocked},
		StatusPROpen:           {StatusInReview, StatusBlocked},
		StatusInReview:         {StatusChangesRequested, StatusMerged, StatusBlocked},
		StatusChangesRequested: {StatusInProgress, StatusBlocked},
		StatusMerged:           {StatusComplete},
		StatusComplete:         {},
		StatusFailed:           {StatusInProgress, StatusBlocked},
		StatusBlocked:          {StatusInProgress, StatusSkipped, StatusPending},
		StatusSkipped:          {},
		StatusSuperseded:       {},
	}
}

// PocTransitionOverrides is the `poc` overlay: entries that REPLACE their
// AllowedTransitions counterparts when a loop runs in `poc` mode.
//
// # Q5: still needed, ported
//
// A POC loop builds on one branch with no forge ceremony — no issue, no PR, no
// review, no merge — so `pr-open`, `in-review` and `merged` describe events
// that never happen. Without the overlay the ladder strands every POC task at
// `secure`, because `complete` is otherwise reachable only from `merged`.
//
// What it does NOT relax: `secure` still requires passing `verified` first, and
// both still require evidence. Only the PR rungs are skippable, and they stay
// REACHABLE — a POC loop that does open a PR is not refused.
//
// A silently dropped override would reappear as a loop mode that suddenly
// cannot advance, with nothing naming this file.
func PocTransitionOverrides() map[TaskStatus][]TaskStatus {
	return map[TaskStatus][]TaskStatus{
		StatusSecure: {StatusPROpen, StatusInProgress, StatusBlocked, StatusComplete},
	}
}

// TransitionsFor is the table in force for a mode.
//
// Any mode other than `poc` — including the empty string, which is every
// caller that does not care — gets AllowedTransitions unchanged.
func TransitionsFor(mode LoopMode) map[TaskStatus][]TaskStatus {
	table := AllowedTransitions()
	if mode != ModePoc {
		return table
	}
	for from, to := range PocTransitionOverrides() {
		table[from] = to
	}
	return table
}

// Caps are the counter bounds, published as data.
//
// From v1's dev-loop-protocol bounds: retries ≤ 2, review_rounds > 2
// escalates, security_rounds > 2 escalates. The cap is INCLUSIVE — reaching it
// is fine, exceeding it signals the caller to escalate.
func Caps() map[string]int {
	return map[string]int{
		"retries":         2,
		"security_rounds": 2,
		"review_rounds":   2,
		"qa_rounds":       2,
	}
}

// BumpCounter adds to a counter and reports whether the cap was exceeded.
//
// An unknown counter name reports exceeded=false rather than panicking: this is
// called on a path that is already handling a failure, and a panic there would
// replace a recoverable problem with a crash.
func BumpCounter(current int, counter string, by int) (value int, capExceeded bool) {
	value = current + by
	limit, known := Caps()[counter]
	if !known {
		return value, false
	}
	return value, value > limit
}

// TransitionResult is the answer to "may this move happen".
//
// A RESULT, never an error (DB19). "This transition is not allowed" and "the
// disk is full" need different handling, and a single error return makes every
// caller guess which it got.
type TransitionResult struct {
	OK bool

	// From and To are echoed back so a refusal names BOTH states. A message
	// saying only "transition refused" sends the reader back to the call site
	// to find out what it refused.
	From TaskStatus
	To   TaskStatus

	// Allowed is what IS legal from From, so the caller can offer a choice
	// rather than a dead end.
	Allowed []TaskStatus

	// Path is the shortest legal route to To, excluding From, or nil when To is
	// unreachable. This is what turns "transition refused" into "you need to go
	// through in-progress first" — the difference between an error a caller can
	// act on and one it can only log.
	Path []TaskStatus

	// Reason is the whole thing in a sentence, for a human.
	Reason string
}

// ValidateTransition decides whether from → to is legal in mode.
//
// A self-transition is permitted and is a no-op, matching v1: a caller
// re-asserting the status it already has is idempotent rather than wrong.
func ValidateTransition(from, to TaskStatus, mode LoopMode) TransitionResult {
	if from == to {
		return TransitionResult{OK: true, From: from, To: to, Allowed: TransitionsFor(mode)[from]}
	}

	table := TransitionsFor(mode)
	allowed, known := table[from]
	if !known {
		// An unknown `from` is a row that should not exist — but it does, so it
		// is reported rather than panicked over. A store reading a status no
		// build understands should say so, not die holding the answer.
		return TransitionResult{
			OK:     false,
			From:   from,
			To:     to,
			Reason: "unknown task status " + quote(string(from)) + ", so no transition from it can be checked",
		}
	}

	for _, candidate := range allowed {
		if candidate == to {
			return TransitionResult{OK: true, From: from, To: to, Allowed: allowed}
		}
	}

	res := TransitionResult{OK: false, From: from, To: to, Allowed: allowed}
	res.Path = ShortestStatusPath(from, to, mode)

	next := "none"
	if len(allowed) > 0 {
		next = joinStatuses(allowed, ", ")
	}
	res.Reason = "illegal task status transition: " + string(from) + " -> " + string(to) +
		" (allowed next: " + next + ")."
	switch {
	case len(res.Path) > 0:
		res.Reason += " To reach " + quote(string(to)) + " from " + quote(string(from)) +
			", walk: " + string(from) + " -> " + joinStatuses(res.Path, " -> ")
		if to == StatusComplete {
			if mode == ModePoc {
				res.Reason += " (in poc mode, complete is valid from 'secure' or 'merged')."
			} else {
				res.Reason += " (complete is only valid from 'merged')."
			}
		} else {
			res.Reason += "."
		}
	default:
		res.Reason += " " + quote(string(to)) + " is not reachable from " + quote(string(from)) + "."
	}
	return res
}

// CanTransition is the boolean form, for a caller that only needs the answer.
func CanTransition(from, to TaskStatus, mode LoopMode) bool {
	return ValidateTransition(from, to, mode).OK
}

// ShortestStatusPath is the shortest legal route from → to, by breadth-first
// search over the table in force.
//
// It returns the sequence AFTER `from`, up to and including `to`, or nil when
// `to` is unreachable. Never a PARTIAL path: a route that stops short would
// read as "walk these states" and leave the caller somewhere they did not
// intend, so unreachable is reported as nothing rather than as progress.
func ShortestStatusPath(from, to TaskStatus, mode LoopMode) []TaskStatus {
	if from == to {
		return nil
	}
	table := TransitionsFor(mode)
	if _, known := table[from]; !known {
		return nil
	}

	queue := [][]TaskStatus{{from}}
	seen := map[TaskStatus]bool{from: true}

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		last := path[len(path)-1]
		for _, next := range table[last] {
			if seen[next] {
				continue
			}
			extended := append(append([]TaskStatus{}, path...), next)
			if next == to {
				return extended[1:]
			}
			seen[next] = true
			queue = append(queue, extended)
		}
	}
	return nil
}

// quote and joinStatuses keep this file import-free.
//
// `strconv.Quote` and `strings.Join` would do, and importing them would be
// harmless — but the test that asserts this file has no imports is what keeps
// it a pure table, and a pure table is the thing two packages can both read
// without either owning the other.
func quote(s string) string { return "'" + s + "'" }

func joinStatuses(in []TaskStatus, sep string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += sep
		}
		out += string(s)
	}
	return out
}
