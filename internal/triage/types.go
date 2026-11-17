// Package triage is S29's core: the bug's vocabulary and its one machine —
// the four validation states (D1), their closed transition table, D2's
// priority split, and the door every other task calls instead of touching
// bug rows directly.
//
// It follows internal/roadmap's and internal/queue's precedent deliberately:
// the SQL lives in internal/store (the house rule: the store owns the
// handle), this package owns the MEANING. Rows cross the boundary as
// store.BugRow and are converted once, in BugFromRow.
//
// The whole argument of the slice is that validation STATE and priority
// STATE are two separate closed sets, never one crossed enum. This package
// keeps them as two fields, and a second renderer that "just wants a status
// badge" cannot flatten them because the door itself never merges them.
package triage

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// ValidationState is D1's closed four. `unvalidated` is the state no
// attempt has run yet — a self-filed claim (D7) and any human report both
// arrive there — and it must never collapse into `could-not-tell` (that
// means an attempt ran and was inconclusive; this means none has). The
// three validated states are terminal for the bug's own state: a re-attempt
// is a new attempt record (D3), not a reset of what the bug reads.
// MaxOpenClaimsPerAgent is TM6's rate law, counted at the bug_file door:
// how many UNVALIDATED, non-duplicate claims one agent may hold open at
// once (S51 T5, 2026-11-04). The refusal names this constant and how to
// list them (bug_list with validation=unvalidated); the cap exists so one
// chatty agent cannot flood a human backlog — filing is cheap, triaging is
// not.
const MaxOpenClaimsPerAgent = 8

type ValidationState string

const (
	ValidationUnvalidated   ValidationState = "unvalidated"
	ValidationReproduced    ValidationState = "reproduced"
	ValidationNotReproduced ValidationState = "not-reproduced"
	ValidationCouldNotTell  ValidationState = "could-not-tell"
)

// ValidationStates is D1's closed four, in the order a triage list walks
// them: the pre-attempt state first (a backlog of never-looked-at claims),
// then the three an attempt landed on.
func ValidationStates() []ValidationState {
	return []ValidationState{
		ValidationUnvalidated,
		ValidationReproduced,
		ValidationNotReproduced,
		ValidationCouldNotTell,
	}
}

// ValidValidationState reports whether s is one of D1's four. The DATABASE
// also refuses a fifth (0044's CHECK); this is the door's vocabulary so a
// caller gets a sentence instead of a driver error.
func ValidValidationState(s ValidationState) bool {
	for _, v := range ValidationStates() {
		if v == s {
			return true
		}
	}
	return false
}

// PriorityState is D2's three states, copied verbatim from S26-roadmap's
// roadmap_item (design §2 T1): known carries a human's word, assumed
// carries an unverified one, unknown is the honest absence — never
// collapsed into a number, a blank, or a sort-last that reads as low.
// D2 is the constraint this slice imposes on S26 and S27, so matching S26's
// own columns means a hand-off copies two fields with no translation.
type PriorityState string

const (
	PriorityKnown   PriorityState = "known"
	PriorityAssumed PriorityState = "assumed"
	PriorityUnknown PriorityState = "unknown"
)

// PriorityStates is the closed three, in honesty order.
func PriorityStates() []PriorityState {
	return []PriorityState{PriorityKnown, PriorityAssumed, PriorityUnknown}
}

// ValidPriorityState reports whether p is one of the three.
func ValidPriorityState(p PriorityState) bool {
	for _, v := range PriorityStates() {
		if v == p {
			return true
		}
	}
	return false
}

// Bug is the Go mirror of 0044's row. ValidationState and PriorityState are
// TWO fields (D2) and the door never merges them into one. PriorityLabel is
// empty when PriorityState is unknown — the contradiction D2 makes
// unwritable at the database, and BugFromRow refuses a row that got through
// anyway. DuplicateOf and RoadmapItemID are absent (nil) until a human
// confirmation and the S26 hand-off set them, respectively; absence is the
// honest "not yet".
type Bug struct {
	ID            int64
	ComponentRoot string
	Title         string
	Body          string
	Validation    ValidationState
	PriorityState PriorityState
	PriorityLabel string
	FiledBy       string
	DuplicateOf   *int64
	RoadmapItemID *int64
	CreatedAt     string
	UpdatedAt     string
}

// BugFromRow is the ONE conversion, the way roadmap.ItemFromRow is the one
// for its rows — a second row-shaping site would be a second truth about
// what the columns mean. A row carrying vocabulary Go cannot name is said
// loudly, not guessed at; a row that broke D2's contradiction at the
// database means something bypassed the CHECK, so the door reports it
// rather than rendering a label on an unknown priority.
func BugFromRow(r store.BugRow) (Bug, error) {
	b := Bug{
		ID:            r.ID,
		ComponentRoot: r.ComponentRoot,
		Title:         r.Title,
		Body:          r.Body,
		Validation:    ValidationState(r.ValidationState),
		PriorityState: PriorityState(r.PriorityState),
		FiledBy:       r.FiledBy,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
	if r.PriorityLabel != nil {
		b.PriorityLabel = *r.PriorityLabel
	}
	if r.DuplicateOf != nil {
		b.DuplicateOf = r.DuplicateOf
	}
	if r.RoadmapItemID != nil {
		b.RoadmapItemID = r.RoadmapItemID
	}
	if !ValidValidationState(b.Validation) {
		return Bug{}, fmt.Errorf("triage: bug %d carries validation_state %q, which is not one of D1's four", r.ID, r.ValidationState)
	}
	if !ValidPriorityState(b.PriorityState) {
		return Bug{}, fmt.Errorf("triage: bug %d carries priority_state %q, which is not one of D2's three", r.ID, r.PriorityState)
	}
	if b.PriorityState == PriorityUnknown && b.PriorityLabel != "" {
		return Bug{}, fmt.Errorf("triage: bug %d is unknown priority AND carries label %q — D2's contradiction is unwritable at the database, so a row like this means the CHECK was bypassed", r.ID, b.PriorityLabel)
	}
	return b, nil
}

// Validated reports whether an attempt has landed — the bug is no longer
// `unvalidated`. The hand-off (D8) is gated on this AND a non-unknown
// priority, and the two are checked separately because D2 keeps them
// separate; a helper that merged them would be the collapse D2 forbids.
func (b Bug) Validated() bool { return b.Validation != ValidationUnvalidated }
