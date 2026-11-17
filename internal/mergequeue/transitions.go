package mergequeue

import "fmt"

// transitions.go — MQ5's edges as data. The state machine's ONLY authority:
// the driver, the service, and every door ask Legal() before anything moves,
// and the refusal sentence names the legal moves (MQ5: "anything else is
// refused with a sentence naming the legal moves").
//
// The hand-written matrix in transitions_test.go is the lock — an edit here
// that drifts from MQ5's table fails there, not in review.

// Move is one legal edge out of a status, with its one-sentence meaning.
// Describe(from) is also the source of the protocol's `mergequeue.vocabulary`
// read (T8) — nobody re-types these sentences downstream.
type Move struct {
	To      Status
	Meaning string
}

// edges is MQ5's table. Terminal statuses (merged, removed) have no row;
// failed's only exit is a person's retry. Dequeue is legal from every open
// status (MQ5's "open | person dequeues" row), which is why removed appears
// under each of the six.
var edges = map[Status][]Move{
	StatusQueued: {
		{StatusCatchingUp, "head of its base with the landing slot free: the landing loop starts and merges the base forward"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusCatchingUp: {
		{StatusVerifying, "the integration branch is built clean and the gates run against its sha"},
		{StatusConflicted, "merging the base in hit conflicts — the hunks stay in the worktree, never in the database"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusVerifying: {
		{StatusReady, "the gates are green for the integration sha the verification is bound to"},
		{StatusFailed, "a gate went red"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusReady: {
		{StatusMerging, "mode auto with the merge grant and the merge rule satisfied, or a person lands it"},
		{StatusQueued, "the base moved — the item catches up again, bounded by max_catch_ups"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusMerging: {
		{StatusMerged, "the forge landed it and the merge sha is an ancestor of the base tip"},
		{StatusFailed, "the landing was refused or failed — protection, a denial, or a phantom merge"},
		{StatusQueued, "the base moved under the landing — catch up again, bounded by max_catch_ups"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusConflicted: {
		{StatusQueued, "the resolution loop finished done — the item re-enters the queue and re-verifies from scratch"},
		{StatusFailed, "the person answered abort, or resolution gave up"},
		{StatusRemoved, "a person dequeues it"},
	},
	StatusFailed: {
		{StatusQueued, "a person retried it"},
	},
}

// Legal reports whether from→to is one of MQ5's edges. Self-transitions are
// NOT legal here: a self-write is SetMergeItemFields' job (facts, not
// status), and the guarded store write pins the status it wrote.
func Legal(from, to Status) bool {
	for _, m := range edges[from] {
		if m.To == to {
			return true
		}
	}
	return false
}

// Moves lists the legal moves out of from — empty for a terminal status.
// (The task file spelled this Describe; describe.go owns that name for
// items — this one was renamed to Moves, the deviation recorded in T3's
// Notes.)
func Moves(from Status) []Move {
	return edges[from]
}

// RefusalSentence is the MQ5 sentence: the pair refused, named, with the
// legal moves beside it. Built from the enums only (SEC-033 — no caller
// prose can ride into it; the item id is passed in by the caller as `what`).
func RefusalSentence(what string, from, to Status) string {
	if len(Moves(from)) == 0 {
		return fmt.Sprintf("refused: %s cannot move %s → %s — %s is terminal", what, from, to, from)
	}
	moves := ""
	for i, m := range Moves(from) {
		if i > 0 {
			moves += ", "
		}
		moves += string(m.To)
	}
	return fmt.Sprintf("refused: %s cannot move %s → %s — the legal moves from %s are: %s", what, from, to, from, moves)
}
