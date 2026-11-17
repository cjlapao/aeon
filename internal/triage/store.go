package triage

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Triage is the one door every other task calls. It wraps the store's bug
// methods the way internal/roadmap's Roadmap wraps its pair: the handle
// stays in internal/store, the decisions live here.
type Triage struct {
	s *store.Store
}

// New wires the triage to the store. One instance per daemon is the intent
// (the socket door and the dispatch files share it), though the type holds
// no state beyond the handle, so tests may open as many as they like.
func New(s *store.Store) *Triage { return &Triage{s: s} }

// Refusal is DB19 again: the discriminated answer to "may this move". OK
// with a refreshed bug, or a Reason that names the thing refused and the
// alternative (house refusal rule), never a bare error for a refusal.
type Refusal struct {
	Reason string
}

// Error lets a Refusal satisfy error for callers that must thread one
// (the socket layer); it is not how callers branch.
func (r *Refusal) Error() string { return r.Reason }

// NewBug is what a person (CLI file, the socket verb) or an agent (file_bug
// tool, T3) offers when filing. It carries NO validationState — that is the
// store's own default (`unvalidated`), so a crafted call cannot pre-label a
// claim as already reproduced (D7's whole point). Priority may be supplied
// by a human who has decided it; an empty state defaults to unknown.
type NewBug struct {
	Title         string
	Body          string
	PriorityState PriorityState // empty means unknown — D2's honest absence
	PriorityLabel string        // must be empty when PriorityState is unknown (D2's contradiction)
	FiledBy       string        // an agent name or 'person' (D7)
}

// Create adds one bug, entering at `unvalidated` / `unknown`. The entry
// validation state is not an argument on purpose — letting a caller pick it
// would smuggle a pre-looked-at claim in through the front door, the D7
// collapse the whole slice exists to prevent.
func (t *Triage) Create(componentRoot string, in NewBug) (Bug, *Refusal) {
	return t.create(componentRoot, in, nil)
}

// create is the ONE filing flow: every check runs the same whether or not
// an actor is present; an actor means the audited twin at the store (bug
// row + 'file' audit row in one transaction, S51 T1), no actor means the
// plain store writer (fixture paths and the pre-T1 tests, unchanged).
func (t *Triage) create(componentRoot string, in NewBug, actor *Actor) (Bug, *Refusal) {
	if componentRoot == "" {
		return Bug{}, &Refusal{Reason: "a bug names the component root it was filed in"}
	}
	if in.Title == "" {
		return Bug{}, &Refusal{Reason: "a bug has a title"}
	}
	if in.Body == "" {
		return Bug{}, &Refusal{Reason: "a bug has a body — the report itself is the body, not a note about it"}
	}
	if in.FiledBy == "" {
		return Bug{}, &Refusal{Reason: "a bug names who filed it — an agent name or 'person' (D7)"}
	}
	priority := in.PriorityState
	if priority == "" {
		priority = PriorityUnknown
	}
	if !ValidPriorityState(priority) {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("priority %q is not one of the three — known, assumed, unknown (D2; a number is never one)", in.PriorityState)}
	}
	if priority == PriorityUnknown && in.PriorityLabel != "" {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("a priority label (%q) on an unknown priority contradicts itself (D2) — set the state or drop the label", in.PriorityLabel)}
	}
	var lp *string
	if in.PriorityLabel != "" {
		lp = &in.PriorityLabel
	}
	rowIn := store.NewBugRow{
		ComponentRoot: componentRoot,
		Title:         in.Title,
		Body:          in.Body,
		PriorityState: string(priority),
		PriorityLabel: lp,
		FiledBy:       in.FiledBy,
	}
	var row store.BugRow
	var err error
	if actor == nil {
		row, err = t.s.CreateBug(rowIn)
	} else {
		// The 'file' summary is the store writer's (the id only exists after
		// the INSERT lands); the actor is the caller's.
		row, err = t.s.CreateBugAudited(rowIn, auditOf(*actor, ""))
	}
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the store refused the create: %v", err)}
	}
	b, err := BugFromRow(row)
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the row that came back is not a legal bug: %v", err)}
	}
	return b, nil
}

// ListFilter narrows a read; empty string means no filter on that column.
type ListFilter struct {
	Validation ValidationState
	Priority   PriorityState
}

// List reads a root's bugs, newest first — the triage list walks the
// backlog the way a person triages it. Terminal and in-flight rows are
// both included: a `could-not-tell` bug is a record of what an attempt
// found, not a bug that should vanish.
func (t *Triage) List(componentRoot string, f ListFilter) ([]Bug, error) {
	rows, err := t.s.ListBugs(componentRoot, store.BugFilters{
		ValidationState: string(f.Validation),
		PriorityState:   string(f.Priority),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Bug, 0, len(rows))
	for _, rw := range rows {
		b, err := BugFromRow(rw)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// Show reads one bug by id; false is honest absence, not a refusal.
func (t *Triage) Show(id int64) (Bug, bool, error) {
	row, ok, err := t.s.BugByID(id)
	if err != nil || !ok {
		return Bug{}, false, err
	}
	b, err := BugFromRow(row)
	return b, err == nil, err
}

// Count is the honest count a list with a zero answer must show.
func (t *Triage) Count(componentRoot string) (int, error) {
	return t.s.CountBugs(componentRoot)
}

// Transition is THE validation mover (T2's demand: one function every other
// task calls). It takes an id and the target, re-reads the row FIRST (the
// fresh row is the moment), then the table refuses, then the claimed-FROM
// update lands so racing dispatchers lose honestly rather than corrupting.
func (t *Triage) Transition(id int64, to ValidationState) (Bug, *Refusal) {
	return t.transition(id, to, nil)
}

// transition is the ONE validation mover: the fresh read, the table's
// refusal, the claimed-FROM race — identical with or without an actor; an
// actor means the move and its 'transition' audit row land in one
// transaction (S51 T1; the reactor passes SystemReactor, so a person can
// see WHAT MOVED ON ITS OWN).
func (t *Triage) transition(id int64, to ValidationState, actor *Actor) (Bug, *Refusal) {
	fresh, ok, err := t.Show(id)
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: read bug %d: %v", id, err)}
	}
	if !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d does not exist", id)}
	}
	res := ValidateTransition(fresh.Validation, to)
	if !res.OK {
		return Bug{}, &Refusal{Reason: res.Reason}
	}
	var claimed bool
	if actor == nil {
		claimed, err = t.s.TransitionBug(id, string(fresh.Validation), string(to))
	} else {
		claimed, err = t.s.TransitionBugAudited(id, string(fresh.Validation), string(to),
			auditOf(*actor, summaryTransition(id, fresh.Validation, to)))
	}
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the store refused the move: %v", err)}
	}
	if !claimed {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d moved under the %q → %q transition — re-read it and decide again", id, fresh.Validation, to)}
	}
	moved, ok, err := t.Show(id)
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the move landed but the row could not be re-read: %v", err)}
	}
	if !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d vanished after the move — say so loudly", id)}
	}
	return moved, nil
}

// RecordHandoff records the S26 hand-off's result (D8/T6): the one roadmap
// item a validated, prioritized bug produced. It is called only after
// internal/roadmap's Create has landed, and only once (the caller refuses a
// second hand-off on a bug already carrying a roadmap_item_id). The write
// is scoped to the caller's root so another project's bug cannot be
// re-pointed.
func (t *Triage) RecordHandoff(id int64, componentRoot string, roadmapItemID int64) (Bug, *Refusal) {
	return t.recordHandoff(id, componentRoot, roadmapItemID, nil)
}

// recordHandoff is the one hand-off record; an actor pairs it with its
// 'handoff' audit row in one transaction (S51 T1).
func (t *Triage) recordHandoff(id int64, componentRoot string, roadmapItemID int64, actor *Actor) (Bug, *Refusal) {
	if _, ok, err := t.Show(id); err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: read bug %d: %v", id, err)}
	} else if !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d does not exist", id)}
	}
	var claimed bool
	var err error
	if actor == nil {
		claimed, err = t.s.SetBugRoadmapItemID(id, componentRoot, roadmapItemID)
	} else {
		claimed, err = t.s.SetBugRoadmapItemIDAudited(id, componentRoot, roadmapItemID,
			auditOf(*actor, summaryHandoff(id, roadmapItemID)))
	}
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the store refused the hand-off record: %v", err)}
	}
	if !claimed {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d is not in %s — the record is scoped to the caller's root", id, componentRoot)}
	}
	fresh, ok, err := t.Show(id)
	if err != nil || !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the hand-off record landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// SetPriority writes state and label together (D2): one decision, not two
// writes. It is independent of validation state entirely — callable at any
// validation state, including `unvalidated`, because the two fields never
// gate each other. The store writes the pair in one write so the D2
// contradiction cannot exist even briefly.
func (t *Triage) SetPriority(id int64, componentRoot string, state PriorityState, label string) (Bug, *Refusal) {
	return t.setPriority(id, componentRoot, state, label, nil)
}

// setPriority is the one priority write; an actor pairs it with its
// 'priority' audit row in one transaction (S51 T1).
func (t *Triage) setPriority(id int64, componentRoot string, state PriorityState, label string, actor *Actor) (Bug, *Refusal) {
	if !ValidPriorityState(state) {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("priority %q is not one of the three — known, assumed, unknown (D2; a number is never one)", state)}
	}
	if state == PriorityUnknown && label != "" {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("a priority label (%q) on an unknown priority contradicts itself (D2) — set the state or drop the label", label)}
	}
	if _, ok, err := t.Show(id); err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: read bug %d: %v", id, err)}
	} else if !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d does not exist", id)}
	}
	var lp *string
	if label != "" {
		lp = &label
	}
	var claimed bool
	var err error
	if actor == nil {
		claimed, err = t.s.SetBugPriority(id, componentRoot, string(state), lp)
	} else {
		claimed, err = t.s.SetBugPriorityAudited(id, componentRoot, string(state), lp,
			auditOf(*actor, summaryPriority(id, state)))
	}
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the store refused the priority: %v", err)}
	}
	if !claimed {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d is not in %s — the write is scoped to the caller's root", id, componentRoot)}
	}
	fresh, ok, err := t.Show(id)
	if err != nil || !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the priority landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}
