// Package queue owns the queue item's vocabulary and its one machine: the
// seven statuses (CG1), the closed transition table over them, and the
// behaviour that decides position and eligibility. It follows
// internal/store/machine.go's precedent — a pure table plus functions over
// it, enumerated never hand-listed in tests — because a second place that
// knows whether `pending → running` is legal is a second truth the day
// somebody edits one and not the other (the task's own words, adopted).
//
// The SQL lives in internal/store (house rule: the store owns the handle);
// this package owns the MEANING. Rows cross the boundary as
// store.QueueItemRow and are converted once, here.
package queue

import (
	"fmt"
	"sort"
	"strings"
)

// Status is a queue item's rung. Seven values, CG1's conclusion: the
// brief's candidate list carried an eighth (`skipped`) and the design
// dropped it — D12 asks for exactly one behaviour (take a pending item out
// of the queue), and `skipped` vs `cancelled` would be two labels for it
// with no decision anywhere that reads one and not the other.
type Status string

const (
	StatusPending   Status = "pending"
	StatusSizing    Status = "sizing"
	StatusReady     Status = "ready"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusBlocked   Status = "blocked"
	StatusCancelled Status = "cancelled"
)

// Statuses is every status, in the machine's order (CG1's diagram, read
// left to right). Order matters for the cartesian-product test.
func Statuses() []Status {
	return []Status{
		StatusPending, StatusSizing, StatusReady, StatusRunning,
		StatusDone, StatusBlocked, StatusCancelled,
	}
}

// ValidStatus reports whether s is one of the seven. The DATABASE also
// refuses the eighth (T1's CHECK); this is the door's vocabulary so a
// caller gets a sentence instead of a driver error.
func ValidStatus(s Status) bool {
	for _, v := range Statuses() {
		if v == s {
			return true
		}
	}
	return false
}

// AllowedTransitions is CG1's machine, edge for edge:
//
//	pending → sizing   (skill mode; T5 drives it)
//	pending → ready    (manual mode, nothing to wait for)
//	sizing  → ready    (size reported through report_size)
//	sizing  → pending  (the sizing run failed — an ordinary step failing;
//	                    CG5 says why this does not recurse)
//	ready   → running  (materialized and started — CG4 says WHEN)
//	running → done
//	running → blocked  (CG2: any blocked is terminal FOR THE QUEUE, and the
//	                    queue never diagnoses which kind of blocked it is)
//	{pending, sizing, ready, blocked} → cancelled
//
// Two edges are deliberately absent. running → cancelled is refused (D11:
// a loop in flight is the loop's own record, not queue history — the
// attempt is an error the caller must see, not a silent no-op). done →
// anything is absent: the ledger keeps its completed rows.
//
// blocked → cancelled is the edge CG2 REQUIRES: "if the person instead
// cancels the queue item (D12), the next item runs" is only true if a
// blocked item can be cancelled. (The design's own documents disagree on
// this — CG1's prose says `{pending, ready}`, its diagram admits sizing,
// T2.md counts "four" edges. The four are pending/sizing/ready/blocked:
// only that set honours the diagram, T2.md's count, and CG2's escape hatch
// at once; it is recorded here rather than chosen in silence.)
func AllowedTransitions() map[Status][]Status {
	return map[Status][]Status{
		StatusPending:   {StatusSizing, StatusReady, StatusCancelled},
		StatusSizing:    {StatusReady, StatusPending, StatusCancelled},
		StatusReady:     {StatusRunning, StatusCancelled},
		StatusRunning:   {StatusDone, StatusBlocked},
		StatusDone:      {},
		StatusBlocked:   {StatusCancelled},
		StatusCancelled: {},
	}
}

// TransitionResult is the DB19 shape machine.go set: a discriminated
// result, not a bare error — the caller branches on OK and quotes Reason.
type TransitionResult struct {
	OK     bool
	Reason string
	// Legal carries the targets the table allows from `from`, so a refusal
	// can tell the caller what CAN be done (the house refusal rule: name
	// the alternative).
	Legal []Status
}

// ValidateTransition answers "may this item move" without touching a row.
// A self-transition is idempotent, not wrong — machine.go's carried rule,
// which is what makes a retried reactor write safe.
func ValidateTransition(from, to Status) TransitionResult {
	if !ValidStatus(from) {
		return TransitionResult{Reason: fmt.Sprintf("queue status %q is not one of the seven", from)}
	}
	if !ValidStatus(to) {
		return TransitionResult{Reason: fmt.Sprintf("queue status %q is not one of the seven", to)}
	}
	legal := AllowedTransitions()[from]
	if from == to {
		return TransitionResult{OK: true, Legal: legal}
	}
	for _, s := range legal {
		if s == to {
			return TransitionResult{OK: true, Legal: legal}
		}
	}
	reason := fmt.Sprintf("queue item cannot go from %q to %q", from, to)
	if from == StatusRunning && to == StatusCancelled {
		reason += " — a loop in flight is the loop's own record, not queue history (D11); the queue item has no meaning to cancel while its loop runs"
	}
	if len(legal) == 0 {
		reason += fmt.Sprintf(" — %q is terminal", from)
	}
	return TransitionResult{Reason: reason, Legal: legal}
}

// Size is a range with its basis, never a bare number (P31/P32's shape,
// D8). T1 made an incomplete one UNWRITABLE at the database; the type
// keeps the three facts together at the door too.
type Size struct {
	Min   int
	Max   int
	Basis string
}

// QueueItem is the Go mirror of T1's row (CG3's payload fields ride
// verbatim: blueprint, goal, workspace_root, tech, with, attended).
type QueueItem struct {
	ID            int64
	ComponentRoot string
	Position      int
	Status        Status
	Blueprint     string
	Goal          string
	WorkspaceRoot string
	Tech          string
	With          map[string]string
	// Attended keeps loop.attended's three states (D5): declared-true,
	// declared-false, never-declared. It is copied onto the loop when the
	// item materializes and NEVER inferred from the daemon starting the
	// loop with nobody watching.
	Attended *bool
	Size     *Size
	// LoopSlug is unset until materialization — absence is the empty
	// string here and NULL in the row (D10: item status and loop status
	// are never one field).
	LoopSlug  string
	CreatedAt string
	UpdatedAt string
}

// Materialized reports whether this item has produced a loop.
func (q QueueItem) Materialized() bool { return q.LoopSlug != "" }

// SortedTargets orders a refusal's legal targets for stable sentences.
func SortedTargets(from Status) []Status {
	legal := append([]Status(nil), AllowedTransitions()[from]...)
	sort.Slice(legal, func(i, j int) bool { return legal[i] < legal[j] })
	return legal
}

// LegalSentence renders the table's answer for a refusal message:
// "pending may go to ready, sizing, cancelled" — the alternative named,
// not just the refusal.
func LegalSentence(from Status) string {
	legal := SortedTargets(from)
	if len(legal) == 0 {
		return fmt.Sprintf("%q is terminal", from)
	}
	parts := make([]string, len(legal))
	for i, s := range legal {
		parts[i] = string(s)
	}
	return fmt.Sprintf("%q may go to %s", from, strings.Join(parts, ", "))
}
