package triage

// ════════════════════════════════════════════════════════════════════════════
// The reads that render the closed sets — S51 T2, TM5 (D1's two sets stay
// two, always).
//
// Describe(bug) is the ONE renderer of a bug's state words for every
// surface that shows a backlog row (the board first). Its whole reason to
// exist is D2's rule with teeth: validation state and priority state are
// TWO separate sentences and NEVER one merged badge — `unvalidated` (no
// attempt has run) and `could-not-tell` (an attempt ran and was
// inconclusive) must read differently wherever a person sees them, because
// merging them was the exact confusion S29's D2 refused. Fixed templates,
// %q values (SEC-033): nothing a bug carries travels into the render
// beyond the state words themselves.
//
// Vocabulary is the same discipline one level up: the two closed sets and
// D1's legal next states are ASSEMBLED from their sources —
// ValidationStates(), PriorityStates(), AllowedTransitions() — never
// restated into a second list that could drift.
// ════════════════════════════════════════════════════════════════════════════

import "fmt"

// Described is a bug's state in words: the two sets as two fields, each
// with its own sentence, plus the duplicate and hand-off facts when they
// exist (an empty fact is the honest absence, not a zero).
type Described struct {
	ValidationSentence string
	PrioritySentence   string
	Duplicate          string
	Handoff            string
}

// Describe renders a bug's two state sets SEPARATELY (TM5/D2). Each
// validation state says what it means — a person comparing `unvalidated`
// with `could-not-tell` sees the difference without consulting a table.
func Describe(b Bug) Described {
	var v string
	switch b.Validation {
	case ValidationUnvalidated:
		v = fmt.Sprintf("validation: %q — no attempt has run (D1)", string(b.Validation))
	case ValidationReproduced:
		v = fmt.Sprintf("validation: %q — an attempt reproduced it (D1)", string(b.Validation))
	case ValidationNotReproduced:
		v = fmt.Sprintf("validation: %q — an attempt ran and did not reproduce it (D1)", string(b.Validation))
	case ValidationCouldNotTell:
		v = fmt.Sprintf("validation: %q — an attempt ran and was inconclusive (D1)", string(b.Validation))
	default:
		// An impossible state reaching the renderer is said, not hidden:
		// the type's closed set says this cannot happen, and if it does,
		// the surface must show the word it was given rather than a blank.
		v = fmt.Sprintf("validation: %q (outside the closed set — the store said so)", string(b.Validation))
	}
	var p string
	switch b.PriorityState {
	case PriorityKnown:
		p = fmt.Sprintf("priority: %q (%q)", string(b.PriorityState), b.PriorityLabel)
	case PriorityAssumed:
		p = fmt.Sprintf("priority: %q (%q)", string(b.PriorityState), b.PriorityLabel)
	case PriorityUnknown:
		p = fmt.Sprintf("priority: %q", string(b.PriorityState))
	default:
		p = fmt.Sprintf("priority: %q (outside the closed set — the store said so)", string(b.PriorityState))
	}
	d := Described{ValidationSentence: v, PrioritySentence: p}
	if b.DuplicateOf != nil {
		d.Duplicate = fmt.Sprintf("linked as a duplicate of bug %d (a person confirmed it, D5)", *b.DuplicateOf)
	}
	if b.RoadmapItemID != nil {
		d.Handoff = fmt.Sprintf("handed off to roadmap item %d (D8)", *b.RoadmapItemID)
	}
	return d
}

// Vocabulary is the closed sets AS ANSWERED, not restated: the validation
// set, the priority set, and D1's legal next states, each pulled live from
// the function that OWNS it (TM5 — a second list would be a second truth).
type Vocabulary struct {
	ValidationStates []string            `json:"validationStates"`
	PriorityStates   []string            `json:"priorityStates"`
	LegalNext        map[string][]string `json:"legalNext"`
	// ValidateStatement and HandoffStatement are TM9's authority
	// statements — the words the daemon puts BEFORE an enabling verb, so
	// the CLI and the UI quote ONE string (S50 RM9's law, carried). The
	// bug rides its own line under the statement; the statement carries no
	// slot (S51 T6 deviation, recorded in the task Notes).
	ValidateStatement string `json:"validateStatement"`
	HandoffStatement  string `json:"handoffStatement"`
}

// The two statements, spelled once. Each names what starts and what stays
// true until it finishes — a person reads a FACT, not a warning colour.
const (
	ValidateStatement = "This starts a validation attempt — an ordinary loop under the blueprint triage.validationBlueprint names, run by the daemon; the bug stays unvalidated until its reactor lands the outcome (S51 TM9)."
	HandoffStatement  = "This hands the bug off to the roadmap — a validated, prioritized bug becomes a roadmap item under provenance triaged, through S26's own door (S51 TM9, D8)."
)

// Vocabulary assembles the answer from the three owners of the vocabulary.
// (The JSON tags live here because this IS the wire shape — the door's
// reads have no second projection, and S50's vocabulary read set the
// precedent of answering straight from the plane type.)
// Vocabulary assembles the plane's closed sets from the three owners of
// the vocabulary — code, not rows: a *Triage receiver reads nothing, so
// the CLI's direct door answers the SAME bytes on a zero value without
// opening anything (S50's roadmap.Vocabulary() shape, carried honestly:
// the type name is taken here, so the sharing is by receiver, not by a
// package function).
func (t *Triage) Vocabulary() Vocabulary {
	vs := ValidationStates()
	ps := PriorityStates()
	v := Vocabulary{
		ValidationStates: make([]string, len(vs)),
		PriorityStates:   make([]string, len(ps)),
		LegalNext:        map[string][]string{},
	}
	for i, s := range vs {
		v.ValidationStates[i] = string(s)
	}
	for i, s := range ps {
		v.PriorityStates[i] = string(s)
	}
	for from, tos := range AllowedTransitions() {
		next := make([]string, len(tos))
		for i, s := range tos {
			next[i] = string(s)
		}
		v.LegalNext[string(from)] = next
	}
	v.ValidateStatement = ValidateStatement
	v.HandoffStatement = HandoffStatement
	return v
}
