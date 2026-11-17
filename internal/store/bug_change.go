package store

// ════════════════════════════════════════════════════════════════════════════
// The `bug_change` table — the SQL text, and nothing but the SQL text (I2).
//
// # What this table is (S51 T1, TM2)
//
// The triage's authoring audit: one append-only row per bug WRITE — file,
// update, priority, duplicate, validate, handoff, transition — naming the
// actor that made it. S45's policy_change, S49's automation_change and
// S50's roadmap_change carry the same shape for their planes; this is the
// bug's. The bug row is the CURRENT state (one row, moved in place); this
// table is the HISTORY (append-only, never trimmed).
//
// # actor_kind carries 'agent' — and the CHECK says where
//
// Unlike its siblings, `agent` is a legal actor kind HERE, because D7 lets
// an agent file a claim — and the migration's final CHECK ties
// actor_kind='agent' to op='file': the one op a machine may carry. An
// agent setting priority, confirming a duplicate, validating, handing off
// or editing is refused by the database itself, not by convention — TM6's
// guarantee with a schema net under it. Reactor-driven transitions carry
// {system, reactor}: a person can see WHAT MOVED ON ITS OWN.
//
// # Append-only by trigger
//
// The migration installs BEFORE UPDATE / BEFORE DELETE triggers that RAISE
// ABORT, and this file offers no Update or Delete accessor to go around
// them (the setting_event / policy_change precedent).
//
// # No FK to bug
//
// History outlives the row (policy_change's argument, carried verbatim).
//
// # Summaries are fixed templates
//
// Ids and enums only — never raw user text (SEC-033). The CALLER composes
// a summary from the templates the triage door owns; the store only
// refuses the blank.
//
// Errors are plain `error` here: these are infrastructure outcomes. Domain
// refusals are *Refusal upstairs (triage's DB19 division).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// BugChange is one row of `bug_change`, shaped as the table stores it. The
// fields are the columns, field for field. OccurredAt arrives from the
// caller and travels byte for byte.
type BugChange struct {
	ID         int64
	BugID      int64
	Op         string // file|update|priority|duplicate|validate|handoff|transition
	Summary    string
	ActorKind  string // user | cli | agent | system (agent only with op=file — the CHECK)
	ActorID    string
	OccurredAt string
}

// BugAudit is the audit half an audited write carries: what op, which bug,
// the composed summary, and WHO. The audited store writers refuse a
// malformed audit before any SQL — an audit row that names nothing answers
// nothing an auditor asks.
type BugAudit struct {
	BugID   int64
	Op      string
	Summary string
	Actor   BugActor
}

// BugActor is the audit's author vocabulary for the bug plane:
// user | cli | agent | system, plus a non-blank id. It is deliberately NOT
// the records tier's Actor and NOT the roadmap's: the bug plane is the one
// audit table in this store where an AGENT legitimately appears (D7's
// self-filing), and only ever on `file` — ValidFor is that rule in Go, the
// migration's CHECK is that rule in the database.
type BugActor struct {
	Kind string
	ID   string
}

// Valid reports whether the actor names someone the audit may record.
func (a BugActor) Valid() bool {
	switch a.Kind {
	case "user", "cli", "agent", "system":
	default:
		return false
	}
	return a.ID != ""
}

// ValidFor adds the op law: an agent may ONLY file (TM2/TM6 — the Go twin
// of the migration's CHECK, so the refusal arrives as a sentence before
// SQL ever sees the pair).
func (a BugActor) ValidFor(op string) bool {
	return a.Valid() && !(a.Kind == "agent" && op != "file")
}

func checkBugAudit(a BugAudit) error {
	if !a.Actor.Valid() {
		return fmt.Errorf("store: bug audit for bug %d carries no lawful actor (kind %q, id %q): the audit vocabulary is user/cli/agent/system with a named id, and an anonymous row is the audit gap this table exists to close",
			a.BugID, a.Actor.Kind, a.Actor.ID)
	}
	if !a.Actor.ValidFor(a.Op) {
		return fmt.Errorf("store: bug audit for bug %d carries actor kind %q on op %q — an agent may only FILE (D7); priority, duplicate, validate, handoff, update and transition are a person's or the system's acts (TM2/TM6)",
			a.BugID, a.Actor.Kind, a.Op)
	}
	switch a.Op {
	case "file", "update", "priority", "duplicate", "validate", "handoff", "transition":
	default:
		return fmt.Errorf("store: bug audit op %q is not in the seven the schema compiled (file, update, priority, duplicate, validate, handoff, transition)", a.Op)
	}
	if a.Summary == "" {
		return fmt.Errorf("store: bug audit for bug %d carries no summary: an audit row that does not say what happened answers nothing an auditor asks", a.BugID)
	}
	return nil
}

// insertBugChangeSQL is the one INSERT.
const insertBugChangeSQL = `
		INSERT INTO bug_change (bug_id, op, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?)`

// BugChangeInsertTx appends one audit row inside the CALLER's transaction —
// the audit lands in the SAME transaction as the write it records (TM2, the
// policy_change / automation_change / roadmap_change rule): row and audit
// commit together or not at all, so an audit can never name a write the
// table did not make, and a refused audit rolls the write back with it.
// The table's own BEFORE UPDATE/DELETE triggers are the append-only net;
// there is deliberately no update or delete accessor.
func BugChangeInsertTx(tx *sql.Tx, a BugAudit, occurredAt string) error {
	if tx == nil {
		return errors.New("store: bug_change needs the write's own transaction; a nil there is the missing commit, not an absent one")
	}
	if err := checkBugAudit(a); err != nil {
		return err
	}
	if _, err := tx.Exec(insertBugChangeSQL,
		a.BugID, a.Op, a.Summary, a.Actor.Kind, a.Actor.ID, occurredAt); err != nil {
		return fmt.Errorf("store: append bug_change %s for bug %d: %w", a.Op, a.BugID, err)
	}
	return nil
}

// BugChangeAppend adds one audit row OUTSIDE any pair — the exception the
// validate dispatch needs: starting a validation attempt changes no bug row
// (the embedding rides bug_vec, the attempt is a loop), yet the act still
// gets its one audit row (TM2 names `validate` an op). Nothing else uses
// this; every real write pairs itself with BugChangeInsertTx.
func (s *Store) BugChangeAppend(a BugAudit) error {
	if a.Op != "validate" {
		return fmt.Errorf("store: BugChangeAppend carries op %q — this path exists for the validate dispatch ALONE (the one audit with no bug-row write to pair with); every real write pairs itself through BugChangeInsertTx inside BugWithTx", a.Op)
	}
	if err := checkBugAudit(a); err != nil {
		return err
	}
	if _, err := s.w.Exec(insertBugChangeSQL,
		a.BugID, a.Op, a.Summary, a.Actor.Kind, a.Actor.ID, s.Now()); err != nil {
		return fmt.Errorf("store: append bug_change %s for bug %d: %w", a.Op, a.BugID, err)
	}
	return nil
}

// BugChangeList answers one bug's history newest-first (id is the clock
// here — AUTOINCREMENT, so id order IS time order); limit clamps to
// [1,500] and defaults to 50, the discipline every listing carries.
func (s *Store) BugChangeList(ctx context.Context, bugID int64, limit int) ([]BugChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, bug_id, op, summary, actor_kind, actor_id, occurred_at
		FROM bug_change WHERE bug_id = ? ORDER BY id DESC LIMIT ?`, bugID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: bug change list for bug %d: %w", bugID, err)
	}
	defer rows.Close()
	var out []BugChange
	for rows.Next() {
		var c BugChange
		if err := rows.Scan(&c.ID, &c.BugID, &c.Op, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: bug change list for bug %d: %w", bugID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// BugWithTx opens one transaction on the single writer and runs fn over it,
// committing on success and rolling back on error — the store's own
// one-write-transaction idiom (RoadmapWithTx's shape, DB15). EVERY audited
// bug write commits here: it is the seam TestAuditAndWriteAreOneTransaction
// exercises by forcing the audit INSERT to fail and demanding the bug did
// not move.
func (s *Store) BugWithTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: bug write transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: bug write commit: %w", err)
	}
	return nil
}

// BugExists answers the one question a read needs before it trusts an id:
// does the bug row exist. History of a row that never was is a different
// answer than history that is empty (an empty history is legal; a history
// lookup of a nothing is a refusal), and the door must be able to tell them
// apart without reading the audit.
func (s *Store) BugExists(id int64) (bool, error) {
	var n int
	err := s.r.QueryRow(`SELECT count(*) FROM bug WHERE id = ?`, id).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: bug exists %d: %w", id, err)
	}
	return n > 0, nil
}

// BugChangeStat is the board's per-bug digest of its audit rows: how many
// validation ATTEMPTS the bug has (the 'validate' rows — the attempt record
// is the audit row, TM2), when the last one started, and how many rows the
// history holds in all. One grouped read, because the board reads a backlog,
// not a row.
type BugChangeStat struct {
	Attempts      int
	LastAttemptAt string
	HistoryRows   int
}

// BugChangeStats reads the digest for a set of bugs in ONE query. Bugs with
// no rows simply do not appear in the map — an absent key is the honest
// zero, not an error.
func (s *Store) BugChangeStats(ids []int64) (map[int64]BugChangeStat, error) {
	out := map[int64]BugChangeStat{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.r.Query(`
		SELECT bug_id,
		       count(*),
		       sum(CASE WHEN op = 'validate' THEN 1 ELSE 0 END),
		       max(CASE WHEN op = 'validate' THEN occurred_at ELSE '' END)
		FROM bug_change WHERE bug_id IN (`+ph+`) GROUP BY bug_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: bug change stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st BugChangeStat
		var attempts int
		var last string
		if err := rows.Scan(&id, &st.HistoryRows, &attempts, &last); err != nil {
			return nil, fmt.Errorf("store: scan bug change stats: %w", err)
		}
		st.Attempts = attempts
		st.LastAttemptAt = last
		out[id] = st
	}
	return out, rows.Err()
}

// CountOpenClaimsByFiler counts ONE filer's OPEN claims: unvalidated rows
// that are not duplicates (S51 T5, TM6 — the thing the bug_file door caps
// at triage.MaxOpenClaimsPerAgent). Counted by SQL, never filtered in Go,
// so the cap refuses against a number the store itself stands behind.
func (s *Store) CountOpenClaimsByFiler(filedBy string) (int, error) {
	var n int
	err := s.r.QueryRow(
		`SELECT COUNT(*) FROM bug
		  WHERE filed_by = ? AND validation_state = 'unvalidated'
		    AND duplicate_of IS NULL`, filedBy).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting open claims of filer %q: %w", filedBy, err)
	}
	return n, nil
}
