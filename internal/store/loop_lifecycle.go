package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// # Archiving and deleting a loop
//
// Archive hides a loop and keeps everything. Delete removes the loop and what hangs from it (steps, chat, events,
// documents, questions, memory) but keeps what was already counted and the work: the component's telemetry rollups
// are never touched, a tombstone keeps the loop's own totals, and a task, roadmap item or queue item stays with its
// link to the loop cut and a detached_link row saying which loop it was.

// SetLoopArchived hides or shows a loop. Hiding it closes its open work (as closed, not done, "loop archived") in the
// same transaction; showing it again opens that work again. Hiding an already hidden loop keeps its first time.
func (s *Store) SetLoopArchived(ctx context.Context, loopID string, archived bool, note string) error {
	if loopID == "" {
		return fmt.Errorf("store: SetLoopArchived needs a loop id")
	}
	l, err := s.ReadLoopByID(loopID)
	if err != nil {
		return err
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: archive loop %s: %w", loopID, err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	if !archived {
		if _, err := tx.ExecContext(ctx, `DELETE FROM loop_archived WHERE loop_id = ?`, loopID); err != nil {
			return err
		}
		if err := reopenLoopWork(ctx, tx, l, now); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO loop_archived (loop_id, archived_at, note) VALUES (?,?,?)
		ON CONFLICT(loop_id) DO UPDATE SET note = excluded.note`, loopID, now, note); err != nil {
		return fmt.Errorf("store: archive loop %s: %w", loopID, err)
	}
	if err := closeLoopWork(ctx, tx, l, ClosedLoopArchived, now); err != nil {
		return fmt.Errorf("store: archive loop %s: %w", loopID, err)
	}
	return tx.Commit()
}

// ArchivedLoops maps each hidden loop's id to when it was hidden.
func (s *Store) ArchivedLoops(ctx context.Context) (map[string]string, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT loop_id, archived_at FROM loop_archived`)
	if err != nil {
		return nil, fmt.Errorf("store: list archived loops: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// LoopTombstone is what a deleted loop left: who it was and its totals.
type LoopTombstone struct {
	LoopID        string   `json:"loopId"`
	Slug          string   `json:"slug"`
	ComponentRoot string   `json:"componentRoot"`
	Title         string   `json:"title"`
	Goal          string   `json:"goal,omitempty"`
	Status        string   `json:"status,omitempty"`
	StartedAt     string   `json:"startedAt,omitempty"`
	DeletedAt     string   `json:"deletedAt"`
	DeletedBy     string   `json:"deletedBy,omitempty"`
	Note          string   `json:"note,omitempty"`
	Steps         int      `json:"steps"`
	Turns         int      `json:"turns"`
	ToolCalls     int      `json:"toolCalls"`
	InputTokens   int64    `json:"inputTokens"`
	OutputTokens  int64    `json:"outputTokens"`
	WallMs        int64    `json:"wallMs"`
	CostUSD       *float64 `json:"costUsd,omitempty"`
	CostState     string   `json:"costState"`
}

// DetachedLink says a task, roadmap item or queue item lost its loop to a deletion.
type DetachedLink struct {
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	LoopSlug      string   `json:"loopSlug"`
	ComponentRoot string   `json:"componentRoot"`
	DetachedAt    string   `json:"detachedAt"`
	CostUSD       *float64 `json:"costUsd,omitempty"`
	CostFloor     bool     `json:"costFloor,omitempty"`
}

// DeleteLoopKeepingStats removes a loop. In ONE transaction: a tombstone with the loop's totals is written; its tasks
// are cut loose (they become standalone tasks, keeping the cost they had spent); roadmap and queue items lose their
// link; then the loop's documents (except those a goal owns or an attachment pins), events and summaries go, and the
// loop row with everything that cascades from it. Telemetry rollups are not part of this and do not move. A
// failure anywhere leaves the loop exactly as it was.
func (s *Store) DeleteLoopKeepingStats(ctx context.Context, loopID, by, note string) (LoopTombstone, error) {
	l, err := s.ReadLoopByID(loopID)
	if err != nil {
		return LoopTombstone{}, err
	}
	totals, err := s.ReadLoopTotals(loopID)
	if err != nil {
		return LoopTombstone{}, err
	}
	sc := Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}
	tasks, err := s.ListTasks(sc)
	if err != nil {
		return LoopTombstone{}, err
	}
	type spent struct {
		cost  float64
		floor bool
	}
	costs := make(map[string]spent, len(tasks))
	for _, t := range tasks {
		c, floor, err := s.taskCost(t)
		if err != nil {
			return LoopTombstone{}, err
		}
		costs[t.ID] = spent{c, floor}
	}

	// The step rows are the source of truth for how many steps ran and what they cost; the summaries add turns, calls
	// and tokens for the loops that ran after they existed.
	var nSteps, unknown int
	var spentUSD sql.NullFloat64
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(cost_state IN ('partial','unknown')),0)
		FROM step WHERE component_root = ? AND loop_slug = ?`, l.ComponentRoot, l.Slug).Scan(&nSteps, &spentUSD, &unknown); err != nil {
		return LoopTombstone{}, fmt.Errorf("store: cost of loop %s: %w", sc, err)
	}
	if nSteps > totals.Steps {
		totals.Steps = nSteps
	}
	if nSteps > 0 {
		usd := spentUSD.Float64
		totals.CostUSD = &usd
		switch {
		case unknown == 0:
			totals.CostState = "known"
		case unknown == nSteps:
			totals.CostState = "unknown"
			totals.CostUSD = nil // nothing was reported: not a zero
		default:
			totals.CostState = "partial"
		}
	}

	now := s.Now()
	tomb := LoopTombstone{LoopID: l.ID, Slug: l.Slug, ComponentRoot: l.ComponentRoot, Title: l.Title, Goal: l.Goal, Status: l.Status,
		StartedAt: l.StartedAt, DeletedAt: now, DeletedBy: by, Note: note, Steps: totals.Steps, Turns: totals.Turns, ToolCalls: totals.ToolCalls,
		InputTokens: totals.InputTokens, OutputTokens: totals.OutputTokens, WallMs: totals.WallMs, CostUSD: totals.CostUSD, CostState: totals.CostState}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return LoopTombstone{}, fmt.Errorf("store: delete loop %s: %w", sc, err)
	}
	defer func() { _ = tx.Rollback() }()
	fail := func(what string, err error) (LoopTombstone, error) {
		return LoopTombstone{}, fmt.Errorf("store: delete loop %s: %s: %w", sc, what, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO loop_tombstone (loop_id, slug, component_root, title, goal, status, started_at, deleted_at, deleted_by, note,
			steps, turns, tool_calls, input_tokens, output_tokens, wall_ms, cost_usd, cost_state)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tomb.LoopID, tomb.Slug, tomb.ComponentRoot, tomb.Title, tomb.Goal, tomb.Status, tomb.StartedAt, tomb.DeletedAt, tomb.DeletedBy, tomb.Note,
		tomb.Steps, tomb.Turns, tomb.ToolCalls, tomb.InputTokens, tomb.OutputTokens, tomb.WallMs, tomb.CostUSD, tomb.CostState); err != nil {
		return fail("write the tombstone", err)
	}

	// Open work is closed (not done: loop deleted) before it is cut loose, so nothing stays on the board that nobody can
	// finish; what an earlier archive already closed now says it was deleted.
	if err := closeLoopWork(ctx, tx, l, ClosedLoopDeleted, now); err != nil {
		return fail("close the open work", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE work_closure SET reason = ? WHERE component_root = ? AND loop_slug = ?`, ClosedLoopDeleted, l.ComponentRoot, l.Slug); err != nil {
		return fail("update the closures", err)
	}

	// Tasks stay. A loop task must name its loop unless it is manual, so a detached task becomes a standalone one.
	for _, t := range tasks {
		c := costs[t.ID]
		floor := 0
		if c.floor {
			floor = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO detached_link (entity_kind, entity_id, loop_slug, component_root, detached_at, cost_usd, cost_floor)
			VALUES ('task',?,?,?,?,?,?)`, t.ID, l.Slug, l.ComponentRoot, now, c.cost, floor); err != nil {
			return fail("remember a task's loop", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE task SET loop_slug = NULL, manual = 1, updated_at = ? WHERE id = ?`, now, t.ID); err != nil {
			return fail("detach a task", err)
		}
	}
	for _, kind := range []struct{ name, table string }{{"roadmap", "roadmap_item"}, {"queue", "queue_item"}} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT OR REPLACE INTO detached_link (entity_kind, entity_id, loop_slug, component_root, detached_at)
			SELECT '%s', CAST(id AS TEXT), loop_slug, component_root, ? FROM %s WHERE component_root = ? AND loop_slug = ?`, kind.name, kind.table),
			now, l.ComponentRoot, l.Slug); err != nil {
			return fail("remember a "+kind.name+" item's loop", err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET loop_slug = NULL, updated_at = ? WHERE component_root = ? AND loop_slug = ?`, kind.table),
			now, l.ComponentRoot, l.Slug); err != nil {
			return fail("detach a "+kind.name+" item", err)
		}
	}

	// What hangs from the loop but has no foreign key to it. A document a goal owns, or one an attachment pins, is
	// the goal's or the attachment's and stays.
	if _, err := tx.ExecContext(ctx, `DELETE FROM loop_document WHERE component_root = ? AND loop_slug = ? AND owner_goal_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM attachment a WHERE a.component_root = loop_document.component_root AND a.loop_slug = loop_document.loop_slug
			AND a.document = loop_document.file AND a.rev = loop_document.rev)`, l.ComponentRoot, l.Slug); err != nil {
		return fail("remove the documents", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM event WHERE loop_id = ? OR (loop_id IS NULL AND loop_slug = ? AND component_root = ?)`, l.ID, l.Slug, l.ComponentRoot); err != nil {
		return fail("remove the journal", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM step_summary WHERE loop_id = ?`, l.ID); err != nil {
		return fail("remove the step summaries", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM loop WHERE id = ?`, l.ID); err != nil {
		return fail("remove the loop", err)
	}
	if err := tx.Commit(); err != nil {
		return fail("commit", err)
	}
	return tomb, nil
}

// ListLoopTombstones lists the loops deleted from a component (all of them when root is empty), newest first.
func (s *Store) ListLoopTombstones(ctx context.Context, componentRoot string) ([]LoopTombstone, error) {
	q := `SELECT loop_id, slug, component_root, title, goal, status, started_at, deleted_at, deleted_by, note, steps, turns, tool_calls,
		input_tokens, output_tokens, wall_ms, cost_usd, cost_state FROM loop_tombstone`
	args := []any{}
	if componentRoot != "" {
		q += ` WHERE component_root = ?`
		args = append(args, componentRoot)
	}
	rows, err := s.r.QueryContext(ctx, q+` ORDER BY deleted_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list deleted loops: %w", err)
	}
	defer rows.Close()
	var out []LoopTombstone
	for rows.Next() {
		var t LoopTombstone
		if err := rows.Scan(&t.LoopID, &t.Slug, &t.ComponentRoot, &t.Title, &t.Goal, &t.Status, &t.StartedAt, &t.DeletedAt, &t.DeletedBy, &t.Note,
			&t.Steps, &t.Turns, &t.ToolCalls, &t.InputTokens, &t.OutputTokens, &t.WallMs, &t.CostUSD, &t.CostState); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DetachedLinkFor reads the link a task, roadmap item or queue item lost, if it lost one.
func (s *Store) DetachedLinkFor(ctx context.Context, kind, id string) (DetachedLink, bool, error) {
	var d DetachedLink
	var floor int
	err := s.r.QueryRowContext(ctx, `SELECT entity_kind, entity_id, loop_slug, component_root, detached_at, cost_usd, cost_floor
		FROM detached_link WHERE entity_kind = ? AND entity_id = ?`, kind, id).
		Scan(&d.Kind, &d.ID, &d.LoopSlug, &d.ComponentRoot, &d.DetachedAt, &d.CostUSD, &floor)
	if errors.Is(err, sql.ErrNoRows) {
		return DetachedLink{}, false, nil
	}
	if err != nil {
		return DetachedLink{}, false, fmt.Errorf("store: read the detached link of %s %s: %w", kind, id, err)
	}
	d.CostFloor = floor == 1
	return d, true, nil
}

// ListDetachedLinks lists every detached link of one kind.
func (s *Store) ListDetachedLinks(ctx context.Context, kind string) (map[string]DetachedLink, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT entity_id, loop_slug, component_root, detached_at, cost_usd, cost_floor FROM detached_link WHERE entity_kind = ?`, kind)
	if err != nil {
		return nil, fmt.Errorf("store: list detached links: %w", err)
	}
	defer rows.Close()
	out := map[string]DetachedLink{}
	for rows.Next() {
		d := DetachedLink{Kind: kind}
		var floor int
		if err := rows.Scan(&d.ID, &d.LoopSlug, &d.ComponentRoot, &d.DetachedAt, &d.CostUSD, &floor); err != nil {
			return nil, err
		}
		d.CostFloor = floor == 1
		out[d.ID] = d
	}
	return out, rows.Err()
}

// ListAllDetachedLinks lists every detached link of every kind, optionally for one component.
func (s *Store) ListAllDetachedLinks(ctx context.Context, componentRoot string) ([]DetachedLink, error) {
	q := `SELECT entity_kind, entity_id, loop_slug, component_root, detached_at, cost_usd, cost_floor FROM detached_link`
	args := []any{}
	if componentRoot != "" {
		q += ` WHERE component_root = ?`
		args = append(args, componentRoot)
	}
	rows, err := s.r.QueryContext(ctx, q+` ORDER BY detached_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list detached links: %w", err)
	}
	defer rows.Close()
	var out []DetachedLink
	for rows.Next() {
		var d DetachedLink
		var floor int
		if err := rows.Scan(&d.Kind, &d.ID, &d.LoopSlug, &d.ComponentRoot, &d.DetachedAt, &d.CostUSD, &floor); err != nil {
			return nil, err
		}
		d.CostFloor = floor == 1
		out = append(out, d)
	}
	return out, rows.Err()
}
