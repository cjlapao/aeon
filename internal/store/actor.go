package store

// actor.go — who did it, as one closed shape (OL7's actor pair: every record
// row carries `created_by_kind/created_by_id`, and a decision carries the
// same pair under `by_kind/by_id`).
//
// Kind is a closed set enforced HERE, in the store, before any SQL: the
// schema's CHECK is the net, this is the door. An unknown kind never becomes
// a row — a row nobody can name the author of is a row nobody can audit.

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/match"
)

// Refusal names why a records write did not happen. Like DocumentRefusal it
// is a discriminated RESULT (DB19): a refusal is a transition the caller has
// to render, not an infrastructure failure it has to retry. Every Refusal
// carries a Sentence a surface can print verbatim — the one-formatter rule
// the policy verdict already lives by.
type Refusal struct {
	// Kind is the branch a caller switches on: "decider-not-human",
	// "decision-unknown", "goal-not-draft", …
	Kind string

	// Sentence is the human rendering, printed verbatim by CLI and UI alike.
	Sentence string
}

// Actor names the author of a record or the decider of a review.
type Actor struct {
	// Kind is user, agent, or trigger — the only three authors the records
	// tier admits.
	Kind string

	// ID names the author within its kind: a user handle, an agent name, a
	// trigger id. Empty is never legal: an anonymous author is the audit gap
	// this pair exists to close.
	ID string
}

// validActor reports whether the actor is well-formed. Infrastructure-free:
// a bad actor is a caller bug, answered as a refusal before SQL, never as an
// error.
func (a Actor) validActor() bool {
	switch a.Kind {
	case "user", "agent", "trigger":
	default:
		return false
	}
	return a.ID != ""
}

// human reports whether this actor may decide. The answer is exactly `user`
// — a review is a person's act (OL8), and an agent approving its own work is
// the failure the whole review tier exists to prevent.
func (a Actor) human() bool { return a.Kind == "user" && a.ID != "" }

// allSourceKinds is the closed set goal.source_kind's CHECK carries — 0036's
// five, widened by S61 T1's migration with 'intake' (T0's Finding F-2: the
// intake engine files goals whose provenance names itself). The store refuses
// values outside it so a typo becomes a sentence, not a constraint violation
// naming a column.
var allSourceKinds = map[string]bool{
	"manual": true, "brainstorm": true, "webhook": true, "cron": true, "event": true,
	"intake": true,
}

// allGoalTypes is the closed set goal.type and task.type share — OL14's
// five. The words live in internal/match (S49 T2: the match grammar's
// task_type key tests the same five, and one list is one truth); this is a
// copy, not an alias, so nothing outside this package can widen the set by
// writing into the map.
var allGoalTypes = func() map[string]bool {
	out := make(map[string]bool, len(match.GoalTypes))
	for k, v := range match.GoalTypes {
		out[k] = v
	}
	return out
}()

// SourceUntrusted reports whether a source kind arrived from outside the
// machine's own intentions. It is a Go function, never a stored column
// (OL8): untrusted-ness is a property of the source, and a stored copy would
// drift the day the set changes.
//
// webhook is the only true value today — an inbound webhook is a stranger
// talking to the backlog. cron, event, and brainstorm run under Aeon's own
// authorship, and manual is a person typing.
func SourceUntrusted(kind string) bool { return kind == "webhook" }

// decisionsFor returns the sentence a caller must not use: the closed
// decision set every Decide* shares. 'approved' appears only here and in the
// two Decide bodies — records_guard_test.go pins that.
func validDecision(decision string) bool {
	switch decision {
	case "approved", "rejected", "parked":
		return true
	}
	return false
}

// decideRefusal builds the two refusals every Decide* answers before
// touching SQL, in the order OL8 demands: the person first (before even the
// row is read — a non-human caller learns the rule, not the row), then the
// vocabulary.
func decideRefusal(by Actor, decision string) *Refusal {
	if !by.human() {
		return &Refusal{Kind: "decider-not-human",
			Sentence: fmt.Sprintf("refused: only a user can decide a review · caller kind %q is not a person — a review is a human act (OL8)", by.Kind)}
	}
	if !validDecision(decision) {
		return &Refusal{Kind: "decision-unknown",
			Sentence: fmt.Sprintf("refused: decision %q is not one of approved, rejected, parked", decision)}
	}
	return nil
}
