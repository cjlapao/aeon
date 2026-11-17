package store

// ════════════════════════════════════════════════════════════════════════════
// The `roadmap_change` table — the SQL text, and nothing but the SQL text (I2).
//
// # What this table is (S50 T1, RM2)
//
// The roadmap's authoring audit: one append-only row per roadmap_item WRITE
// — add, update, cancel, estimate, priority, ideate, plan, materialize,
// transition — naming the actor that made it. S45's policy_change and S49's
// automation_change carry the same shape for their planes; this is the
// roadmap's. It is a separate table from roadmap_item on purpose: the item
// is the CURRENT state (one row, moved in place); the audit is the HISTORY
// (append-only, never trimmed — nothing in this store retains a path to it).
//
// # actor_kind widens to carry 'system'
//
// The reactor-driven transitions (brief-agreed, done, the materialization
// link) are writes too, and RM2's whole point is that a person can see WHAT
// MOVED ON ITS OWN: those rows carry actor 'system:reactor'. The socket
// person writes 'user' (the kernel's uid, stamped by table.ActorFor — never
// a param), and 'cli' belongs to any future offline door. 'agent' is in
// NEITHER the CHECK nor any caller: an agent never writes the roadmap
// (RM4), and the audit says so at the schema.
//
// # Append-only by trigger
//
// The migration installs BEFORE UPDATE / BEFORE DELETE triggers that RAISE
// ABORT, and this file offers no Update or Delete accessor to go around
// them (the setting_event / policy_change / automation_change precedent).
//
// # No FK to roadmap_item
//
// A cancelled-then-forgotten item's history must OUTLIVE any pruning of the
// item (policy_change's argument, carried verbatim): the record that
// disappears when you ask about it is the failure this table exists to
// prevent.
//
// # Summaries are fixed templates
//
// Ids, statuses and enums only — never raw user text (SEC-033, the rule
// every audit table here keeps). The CALLER composes a summary from the
// templates the roadmap door owns; the store only refuses the blank.
//
// Errors are plain `error` here: these are infrastructure outcomes. Domain
// refusals are *Refusal upstairs (roadmap's DB19 division).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RoadmapChange is one row of `roadmap_change`, shaped as the table stores
// it. The fields are the columns, field for field. OccurredAt arrives from
// the caller and travels byte for byte.
type RoadmapChange struct {
	ID         int64
	ItemID     int64
	Op         string // add|update|cancel|estimate|priority|ideate|plan|materialize|transition
	Summary    string
	ActorKind  string // user | cli | system
	ActorID    string
	OccurredAt string
}

// RoadmapAudit is the audit half an audited write carries: what op, which
// item, the composed summary, and WHO. The audited store writers refuse a
// malformed audit before any SQL — an audit row that names nothing answers
// nothing an auditor asks.
type RoadmapAudit struct {
	ItemID  int64
	Op      string
	Summary string
	Actor   RoadmapActor
}

// RoadmapActor is the audit's author vocabulary for the roadmap plane:
// user | cli | system, plus a non-blank id. It is deliberately NOT the
// records tier's Actor (user/agent/trigger) — the roadmap has no agent
// author at all (RM4), and sharing the type would let the records
// vocabulary leak a Kind an audit row could never lawfully carry.
type RoadmapActor struct {
	Kind string
	ID   string
}

// Valid reports whether the actor names someone the audit may record. The
// refusal sentence is the door's to print; this is the shape law.
func (a RoadmapActor) Valid() bool {
	switch a.Kind {
	case "user", "cli", "system":
	default:
		return false
	}
	return a.ID != ""
}

func checkRoadmapAudit(a RoadmapAudit) error {
	if !a.Actor.Valid() {
		return fmt.Errorf("store: roadmap audit for item %d carries no lawful actor (kind %q, id %q): the audit vocabulary is user/cli/system with a named id, and an anonymous row is the audit gap this table exists to close",
			a.ItemID, a.Actor.Kind, a.Actor.ID)
	}
	switch a.Op {
	case "add", "update", "cancel", "estimate", "priority", "ideate", "plan", "materialize", "transition":
	default:
		return fmt.Errorf("store: roadmap audit op %q is not in the nine the schema compiled (add, update, cancel, estimate, priority, ideate, plan, materialize, transition)", a.Op)
	}
	if a.Summary == "" {
		return fmt.Errorf("store: roadmap audit for item %d carries no summary: an audit row that does not say what happened answers nothing an auditor asks", a.ItemID)
	}
	return nil
}

// insertRoadmapChangeSQL is the one INSERT.
const insertRoadmapChangeSQL = `
		INSERT INTO roadmap_change (item_id, op, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?)`

// RoadmapChangeInsertTx appends one audit row inside the CALLER's
// transaction — the audit lands in the SAME transaction as the write it
// records (RM2, the setting_event / policy_change / automation_change rule):
// row and audit commit together or not at all, so an audit can never name a
// write the table did not make, and a refused audit rolls the write back
// with it. The table's own BEFORE UPDATE/DELETE triggers are the
// append-only net; there is deliberately no update or delete accessor.
func RoadmapChangeInsertTx(tx *sql.Tx, a RoadmapAudit, occurredAt string) error {
	if tx == nil {
		return errors.New("store: roadmap_change needs the write's own transaction; a nil there is the missing commit, not an absent one")
	}
	if err := checkRoadmapAudit(a); err != nil {
		return err
	}
	if _, err := tx.Exec(insertRoadmapChangeSQL,
		a.ItemID, a.Op, a.Summary, a.Actor.Kind, a.Actor.ID, occurredAt); err != nil {
		return fmt.Errorf("store: append roadmap_change %s for item %d: %w", a.Op, a.ItemID, err)
	}
	return nil
}

// RoadmapChangeList answers one item's history newest-first (id is the
// clock here — AUTOINCREMENT, so id order IS time order); limit clamps to
// [1,500] and defaults to 50, the same discipline every listing carries.
func (s *Store) RoadmapChangeList(ctx context.Context, itemID int64, limit int) ([]RoadmapChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, item_id, op, summary, actor_kind, actor_id, occurred_at
		FROM roadmap_change WHERE item_id = ? ORDER BY id DESC LIMIT ?`, itemID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: roadmap change list for item %d: %w", itemID, err)
	}
	defer rows.Close()
	var out []RoadmapChange
	for rows.Next() {
		var c RoadmapChange
		if err := rows.Scan(&c.ID, &c.ItemID, &c.Op, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: roadmap change list for item %d: %w", itemID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RoadmapMoveTime is when an item moved INTO a status, read from the audit's own rows: the first
// such move (last=false) or the latest (last=true), "" when it never did. `like` is the caller's
// two LIKE patterns for the fixed move sentence — the sentence is the roadmap package's, so the
// package that writes it also says how to find it. Only the four ops that carry a move are read.
func (s *Store) RoadmapMoveTime(ctx context.Context, itemID int64, like [2]string, last bool) (string, error) {
	order := "ASC"
	if last {
		order = "DESC"
	}
	var at string
	err := s.r.QueryRowContext(ctx, `SELECT occurred_at FROM roadmap_change
		WHERE item_id = ? AND op IN ('transition','plan','ideate','cancel','materialize') AND (summary LIKE ? OR summary LIKE ?)
		ORDER BY id `+order+` LIMIT 1`, itemID, like[0], like[1]).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: roadmap move time for item %d: %w", itemID, err)
	}
	return at, nil
}

// RoadmapWithTx opens one transaction on the single writer and runs fn over
// it, committing on success and rolling back on error — the store's own
// one-write-transaction idiom (the SeedWithTx/BlueprintWithTx shape, DB15).
// EVERY audited roadmap write commits here: it is the seam
// TestAuditAndWriteAreOneTransaction exercises by forcing the audit INSERT
// to fail and demanding the item did not move.
func (s *Store) RoadmapWithTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: roadmap write transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: roadmap write commit: %w", err)
	}
	return nil
}
