package store

// ════════════════════════════════════════════════════════════════════════════
// The `platform_change` table — the SQL text, and nothing but the SQL text (I2).
//
// # What this table is (S54 T1, SP3)
//
// The audit of the four derived-store OPERATIONS — retrieval.reindex,
// graph.build, graph.reparse, graph.delete: one append-only row per STARTED
// operation, naming the actor that started it. The roadmap_change /
// automation_change / policy_change precedent in full: fixed templates, ids
// only, append-only by trigger, no FK, no update or delete accessor here.
//
// # What an audit row is, and is not
//
// A row marks a START: it rides the SAME transaction as the operation's
// start record (the async_worker insert for the two ledger operations; the
// first store mutation for the two inline ones — see the audited writers
// upstairs), so a row can never name an operation whose start did not
// commit, and a refused or rolled-back start leaves no row. Refusals,
// plans, reads and "already running" answers write NOTHING — the door
// composes nothing and calls nothing in those branches.
//
// # Vocabulary at the schema
//
// op is the closed four; actor_kind is the human pair ('user' from the
// kernel peer, 'cli' from an offline direct door). There is no 'system':
// the write-hook pass is the daemon's act, not an audited operation, and
// no agent reaches any of the four doors (SP3/SP4) — the CHECK and the
// refusal agree, mirroring RM4's argument one plane over.
//
// # subject is a composed id, never prose
//
// 'root\x00corpus' for a reindex (the dedupe key of its flight), the
// absolute root for a graph operation — bounded at 256 by the schema and
// checked before SQL. A hostile path lands in the PARAMETER only, never in
// the summary: summaries are the fixed templates the doors compose, ids
// and enums only (SEC-033).
//
// # outcome_ref
//
// The async worker id ('aw_…') for reindex and graph.build — the same
// handle asyncworkers.get takes; '' for reparse and delete, which run
// inline and mint no row (T0: no start record exists to name). The
// "already running — started by X at T" answer reads THIS column: the
// starter is whoever's audit row carries the flying worker's id.
//
// Errors are plain `error` here: infrastructure outcomes. Domain refusals
// are *Refusal upstairs.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PlatformChange is one row of `platform_change`, shaped as the table
// stores it: the fields are the columns, field for field.
type PlatformChange struct {
	ID         int64
	Op         string // reindex | graph_build | graph_reparse | graph_delete
	Subject    string
	OutcomeRef string
	Summary    string
	ActorKind  string // user | cli
	ActorID    string
	OccurredAt string
}

// PlatformActor is the audit's author vocabulary for this plane: user or
// cli, plus a non-blank id. Deliberately narrower than the roadmap's trio
// — nothing on this plane moves on its own audited — and narrower than
// records' Actor for the same RM4 reason: sharing the type would let the
// vocabulary carry a Kind no platform row could lawfully hold.
type PlatformActor struct {
	Kind string
	ID   string
}

// Valid reports whether the actor names someone the audit may record.
func (a PlatformActor) Valid() bool {
	switch a.Kind {
	case "user", "cli":
	default:
		return false
	}
	return a.ID != ""
}

// PlatformAudit is the audit half an audited start carries: which op, the
// composed subject, the outcome ref (may be empty — the two inline ops
// have no worker to name), the composed summary, and WHO. The store
// refuses a malformed audit before any SQL — an audit row that names
// nothing answers nothing an auditor asks.
type PlatformAudit struct {
	Op         string // reindex | graph_build | graph_reparse | graph_delete
	Subject    string
	OutcomeRef string // 'aw_…' or '' — never prose, never a path beyond the subject rule
	Summary    string
	Actor      PlatformActor
}

// ValidOp is the closed four, exported for the doors that compose summaries.
func PlatformOpValid(op string) bool {
	switch op {
	case "reindex", "graph_build", "graph_reparse", "graph_delete":
		return true
	}
	return false
}

func checkPlatformAudit(a PlatformAudit) error {
	if !a.Actor.Valid() {
		return fmt.Errorf("store: platform audit for %s carries no lawful actor (kind %q, id %q): the audit vocabulary is user/cli with a named id, and an anonymous row is the audit gap this table exists to close",
			a.Op, a.Actor.Kind, a.Actor.ID)
	}
	if !PlatformOpValid(a.Op) {
		return fmt.Errorf("store: platform audit op %q is not in the four the schema compiled (reindex, graph_build, graph_reparse, graph_delete)", a.Op)
	}
	if a.Subject == "" {
		return fmt.Errorf("store: platform audit for %s carries no subject: an audit row that does not say WHAT was rebuilt answers nothing an auditor asks", a.Op)
	}
	if len(a.Subject) > 256 {
		return fmt.Errorf("store: platform audit subject for %s is %d bytes, over the 256 the schema bounds (a subject is a composed id, not a narrative)", a.Op, len(a.Subject))
	}
	if len(a.OutcomeRef) > 64 {
		return fmt.Errorf("store: platform audit outcome_ref %q is %d bytes, over the 64 an 'aw_'-prefixed worker id can reach", a.OutcomeRef, len(a.OutcomeRef))
	}
	if a.Summary == "" {
		return fmt.Errorf("store: platform audit for %s carries no summary: an audit row that does not say what happened answers nothing an auditor asks", a.Op)
	}
	if len(a.Summary) > 1024 {
		return fmt.Errorf("store: platform audit summary for %s is %d bytes, over the 1024 the schema bounds", a.Op, len(a.Summary))
	}
	return nil
}

// insertPlatformChangeSQL is the one INSERT.
const insertPlatformChangeSQL = `
		INSERT INTO platform_change (op, subject, outcome_ref, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

// PlatformChangeInsertTx appends one audit row inside the CALLER's
// transaction — the audit lands in the SAME transaction as the operation's
// start record (SP3, the setting_event / policy_change / automation_change /
// roadmap_change rule): row and audit commit together or not at all, so an
// audit can never name a start the table did not make, and a refused audit
// rolls the start back with it. The table's own BEFORE UPDATE/DELETE
// triggers are the append-only net; there is deliberately no update or
// delete accessor.
func PlatformChangeInsertTx(tx *sql.Tx, a PlatformAudit, occurredAt string) error {
	if tx == nil {
		return errors.New("store: platform_change needs the start's own transaction; a nil there is the missing commit, not an absent one")
	}
	if err := checkPlatformAudit(a); err != nil {
		return err
	}
	if occurredAt == "" {
		return errors.New("store: platform audit carries no occurred_at: an untimed row can name an operation but never WHEN")
	}
	if _, err := tx.Exec(insertPlatformChangeSQL,
		a.Op, a.Subject, a.OutcomeRef, a.Summary, a.Actor.Kind, a.Actor.ID, occurredAt); err != nil {
		return fmt.Errorf("store: append platform_change %s for %q: %w", a.Op, a.Subject, err)
	}
	return nil
}

// PlatformChangeList answers the audit newest-first (id is the clock here —
// AUTOINCREMENT, so id order IS time order), optionally filtered to one
// exact subject — the filter is the CALLER's string matched byte for byte,
// never a prefix, never a pattern; the platform.changes read owns the
// "nothing matched" sentence, this answers rows. limit clamps to [1,500]
// and defaults to 50, the discipline every listing carries.
func (s *Store) PlatformChangeList(ctx context.Context, subject string, limit int) ([]PlatformChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	q := `SELECT id, op, subject, outcome_ref, summary, actor_kind, actor_id, occurred_at
		FROM platform_change`
	args := []any{}
	if subject != "" {
		q += ` WHERE subject = ?`
		args = append(args, subject)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: platform change list: %w", err)
	}
	defer rows.Close()
	var out []PlatformChange
	for rows.Next() {
		var c PlatformChange
		if err := rows.Scan(&c.ID, &c.Op, &c.Subject, &c.OutcomeRef, &c.Summary,
			&c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: platform change list scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PlatformChangeStarter finds the audit row naming who started the worker
// with this outcome ref — the "already running — started by X at T"
// answer's only honest source (SP3). No row is NOT an error: a
// write-hook-started pass has no audit row, and the caller says so.
func (s *Store) PlatformChangeStarter(ctx context.Context, workerID string) (*PlatformChange, error) {
	if workerID == "" {
		return nil, errors.New("store: platform starter lookup needs a worker id — the flying pass's, never a guess")
	}
	var c PlatformChange
	err := s.r.QueryRowContext(ctx, `SELECT id, op, subject, outcome_ref, summary, actor_kind, actor_id, occurred_at
		FROM platform_change WHERE outcome_ref = ? ORDER BY id DESC LIMIT 1`, workerID).
		Scan(&c.ID, &c.Op, &c.Subject, &c.OutcomeRef, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: platform starter lookup for %s: %w", workerID, err)
	}
	return &c, nil
}

// PlatformWithTx opens one transaction on the single writer and runs fn
// over it, committing on success and rolling back on error — the store's
// one-write-transaction idiom (RoadmapWithTx's shape, DB15). Every
// audited platform start commits here; it is the seam
// TestAuditAndStartAreOneTransaction exercises by forcing the audit INSERT
// to fail and demanding the ledger row did not land.
func (s *Store) PlatformWithTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: platform write transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: platform write commit: %w", err)
	}
	return nil
}
