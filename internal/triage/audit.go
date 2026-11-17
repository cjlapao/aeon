package triage

// ════════════════════════════════════════════════════════════════════════════
// The audit half of the door (S51 T1, TM2).
//
// Every production bug write now travels an *Audited method: the same
// decisions as the plain door (the transitions table, the D2 pair, the D5
// human-confirmation gate, the root scoping — all reused, none weakened)
// and one bug_change row in the SAME transaction. The plain door methods
// stay for the store-level tests and any reader-built fixture; production
// callers (protocol handlers, the reactor, the gate filer) all moved.
//
// Actor is store.BugActor (user | cli | agent | system, named id). The
// agent rule — an agent may ONLY appear on `file` (D7/TM6) — is enforced
// twice: the store refuses it before SQL and the schema CHECK refuses it
// in the row. The two named system actors:
//
//	SystemReactor — the validation reactor's own moves (TM2: a person can
//	              see WHAT MOVED ON ITS OWN), and
//	SystemGates   — the S57 gate filer, whose filed_by is 'system:gates'.
//
// Summaries are FIXED TEMPLATES — ids and enums only, never raw user text
// (SEC-033, the rule every audit table here keeps; a bug's title never
// travels into its history row). The `file` template lives with the store
// writer (the id is only known after the INSERT); the rest are composed
// here, and only here — one formatter, one truth.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Actor is the audit's author vocabulary for the bug plane — see the store
// half (BugActor) for the two-phase law it carries.
type Actor = store.BugActor

// SystemReactor files the validation reactor's own transitions: the moves
// out of `unvalidated` that no person made (TM2).
var SystemReactor = Actor{Kind: "system", ID: "reactor"}

// SystemGates is the S57 gate filer's audit identity — the twin of the
// filed_by literal 'system:gates' that migration (TM3) exempts beside
// 'person'.
var SystemGates = Actor{Kind: "system", ID: "gates"}

// auditOf composes the audit row the store writers take: actor and summary
// in, op and id set by the writer itself (facts of the writer, not options).
func auditOf(a Actor, summary string) store.BugAudit {
	return store.BugAudit{Actor: a, Summary: summary}
}

// auditRefusal renders a store audit refusal as the door's Refusal — the
// DB19 division says the door speaks refusals, not errors.
func auditRefusal(err error) *Refusal {
	return &Refusal{Reason: fmt.Sprintf("triage: the audit refused the write: %v", err)}
}

// CreateAudited is Create with its author: the same refusals, then the bug
// row and its 'file' audit row land in one transaction. This is where D7's
// agent appears — bug_file's twin (S51 T5) passes the AGENT actor, and
// `file` is the one op that lawfully carries it.
func (t *Triage) CreateAudited(componentRoot string, in NewBug, a Actor) (Bug, *Refusal) {
	return t.create(componentRoot, in, &a)
}

// TransitionAudited is Transition with its author. The reactor passes
// SystemReactor (TM2); an agent actor here is refused before SQL (TM6's
// net — an agent that could move a bug's validation state could validate
// its own claims), and so is one at the schema CHECK.
func (t *Triage) TransitionAudited(id int64, to ValidationState, a Actor) (Bug, *Refusal) {
	return t.transition(id, to, &a)
}

// SetPriorityAudited is SetPriority with its author. D2's pair law is
// unchanged; TM6's guard means the callers here are persons (user/cli) —
// an agent actor is refused before SQL, on op `priority`.
func (t *Triage) SetPriorityAudited(id int64, componentRoot string, state PriorityState, label string, a Actor) (Bug, *Refusal) {
	return t.setPriority(id, componentRoot, state, label, &a)
}

// RecordHandoffAudited is RecordHandoff with its author (D8: a person's
// act — an agent actor is refused before SQL, on op `handoff`).
func (t *Triage) RecordHandoffAudited(id int64, componentRoot string, roadmapItemID int64, a Actor) (Bug, *Refusal) {
	return t.recordHandoff(id, componentRoot, roadmapItemID, &a)
}

// AppendValidationAudit records the DISPATCH of a validation attempt
// (op `validate`, TM2). It is the one audit that travels outside a pair:
// starting an attempt writes no bug row — the attempt is a loop and the
// embedding rides bug_vec — yet the act still earns its row, so it lands
// after the dispatch succeeded and names who dispatched it. A reactor's
// OUTCOME is a different row: the TransitionAudited `transition` row.
func (t *Triage) AppendValidationAudit(id int64, a Actor) *Refusal {
	if err := t.s.BugChangeAppend(store.BugAudit{
		BugID:   id,
		Op:      "validate", // this audit's op is a fact, not an option
		Summary: fmt.Sprintf("started a validation attempt on bug %d", id),
		Actor:   a,
	}); err != nil {
		return auditRefusal(err)
	}
	return nil
}

// --- fixed templates (TM2; ids and enums only, never raw user text) ------

func summaryTransition(id int64, from, to ValidationState) string {
	return fmt.Sprintf("moved bug %d from %q to %q", id, from, to)
}

func summaryPriority(id int64, state PriorityState) string {
	return fmt.Sprintf("set bug %d priority to %q", id, state)
}

func summaryDuplicate(id, of int64) string {
	return fmt.Sprintf("marked bug %d a duplicate of bug %d", id, of)
}

func summaryDuplicateCleared(id int64) string {
	return fmt.Sprintf("cleared bug %d's duplicate mark", id)
}

func summaryHandoff(id, roadmapItemID int64) string {
	return fmt.Sprintf("handed bug %d off to roadmap item %d", id, roadmapItemID)
}

// BugChange is the door's read shape for one audit row (the store type
// alias keeps the door from handing its callers a store import).
type BugChange = store.BugChange

// History reads a bug's audit rows newest-first (TM5: `triage.history`),
// bounded the way BugChangeList bounds (limit 0 ⇒ the 50-row default,
// capped at 500 — the same honest read bound everywhere else). The id
// must have a row — a history of a nothing is a NAMED refusal, while a
// real bug with no rows answers empty, because "no history" and "no bug"
// are different facts (T2).
func (t *Triage) History(id int64, limit int) ([]BugChange, *Refusal) {
	exists, err := t.s.BugExists(id)
	if err != nil {
		return nil, &Refusal{Reason: fmt.Sprintf("triage: the store refused the read: %v", err)}
	}
	if !exists {
		return nil, &Refusal{Reason: unknownBugReason(id)}
	}
	rows, err := t.s.BugChangeList(context.Background(), id, limit)
	if err != nil {
		return nil, &Refusal{Reason: fmt.Sprintf("triage: the store refused the history: %v", err)}
	}
	return rows, nil
}
