package store

import (
	"context"
	"database/sql"
	"fmt"
)

// # Closing the work of a loop that goes away
//
// A loop that is archived or deleted must not leave its open work behind: the next agents read the board, and a task
// nobody can finish is a trap. So the work is CLOSED, as closed-not-done, with the reason:
//
//	task        open -> skipped      (terminal; the machine's own "not done" rung)
//	roadmap     open -> cancelled
//	queue       open -> cancelled
//	goal        approved/draft/in_review -> parked, when this loop was the only live one working it and it still has
//	            unfinished tasks
//
// work_closure remembers each one (reason, prior status) so the board can say why, and so restoring an archived loop
// opens again exactly what its archive closed.

// Reasons a piece of work is closed.
const (
	ClosedLoopArchived = "loop-archived"
	ClosedLoopDeleted  = "loop-deleted"
)

// WorkClosure says a task, roadmap item, queue entry or goal was closed because its loop went away.
type WorkClosure struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Reason        string `json:"reason"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	PriorStatus   string `json:"priorStatus"`
	ClosedAt      string `json:"closedAt"`
}

// sqlRunner is what *sql.DB and *sql.Tx share, so closing runs inside a caller's transaction or alone.
type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// closeLoopWork closes the loop's open tasks, roadmap items, queue entries and (when nothing else is working it) goal.
func closeLoopWork(ctx context.Context, q sqlRunner, l Loop, reason, now string) error {
	type row struct{ id, status string }
	collect := func(query string, args ...any) ([]row, error) {
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.status); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	record := func(kind, id, prior string) error {
		_, err := q.ExecContext(ctx, `INSERT INTO work_closure (entity_kind, entity_id, reason, loop_slug, component_root, prior_status, closed_at)
			VALUES (?,?,?,?,?,?,?) ON CONFLICT(entity_kind, entity_id) DO UPDATE SET reason = excluded.reason, closed_at = excluded.closed_at`,
			kind, id, reason, l.Slug, l.ComponentRoot, prior, now)
		return err
	}

	tasks, err := collect(`SELECT id, status FROM task WHERE component_root = ? AND loop_slug = ? AND status NOT IN ('complete','merged','skipped','superseded')`, l.ComponentRoot, l.Slug)
	if err != nil {
		return fmt.Errorf("read the loop's open tasks: %w", err)
	}
	for _, t := range tasks {
		if err := record("task", t.id, t.status); err != nil {
			return fmt.Errorf("record a closed task: %w", err)
		}
		if _, err := q.ExecContext(ctx, `UPDATE task SET status = 'skipped', updated_at = ? WHERE id = ?`, now, t.id); err != nil {
			return fmt.Errorf("close a task: %w", err)
		}
	}
	for _, k := range []struct{ kind, table string }{{"roadmap", "roadmap_item"}, {"queue", "queue_item"}} {
		items, err := collect(fmt.Sprintf(`SELECT CAST(id AS TEXT), status FROM %s WHERE component_root = ? AND loop_slug = ? AND status NOT IN ('done','cancelled')`, k.table), l.ComponentRoot, l.Slug)
		if err != nil {
			return fmt.Errorf("read the loop's open %s items: %w", k.kind, err)
		}
		for _, it := range items {
			if err := record(k.kind, it.id, it.status); err != nil {
				return fmt.Errorf("record a closed %s item: %w", k.kind, err)
			}
			if _, err := q.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET status = 'cancelled', updated_at = ? WHERE id = ?`, k.table), now, it.id); err != nil {
				return fmt.Errorf("close a %s item: %w", k.kind, err)
			}
		}
	}
	if l.GoalID != "" {
		// The goal closes only when this loop was the one working it and work remains: another live loop on the same
		// goal keeps it open, and a goal whose tasks are all done is finished, not abandoned.
		goals, err := collect(`SELECT g.id, g.review_state FROM goal g WHERE g.id = ? AND g.review_state IN ('draft','in_review','approved')
			AND NOT EXISTS (SELECT 1 FROM loop o WHERE o.goal_id = g.id AND o.id <> ? AND NOT EXISTS (SELECT 1 FROM loop_archived a WHERE a.loop_id = o.id))
			AND EXISTS (SELECT 1 FROM task t WHERE t.goal_id = g.id AND t.status NOT IN ('complete','merged','skipped','superseded'))`, l.GoalID, l.ID)
		if err != nil {
			return fmt.Errorf("read the loop's goal: %w", err)
		}
		for _, g := range goals {
			if err := record("goal", g.id, g.status); err != nil {
				return fmt.Errorf("record a closed goal: %w", err)
			}
			if _, err := q.ExecContext(ctx, `UPDATE goal SET review_state = 'parked', updated_at = ? WHERE id = ?`, now, g.id); err != nil {
				return fmt.Errorf("close a goal: %w", err)
			}
		}
	}
	return nil
}

// reopenLoopWork opens again what an ARCHIVE of this loop closed, to the status each had, when it is still closed.
// Work closed by a deletion is never reopened (there is no loop to come back to).
func reopenLoopWork(ctx context.Context, q sqlRunner, l Loop, now string) error {
	rows, err := q.QueryContext(ctx, `SELECT entity_kind, entity_id, prior_status FROM work_closure WHERE component_root = ? AND loop_slug = ? AND reason = ?`,
		l.ComponentRoot, l.Slug, ClosedLoopArchived)
	if err != nil {
		return fmt.Errorf("read the closures of loop %s: %w", l.Slug, err)
	}
	type c struct{ kind, id, prior string }
	var closures []c
	for rows.Next() {
		var x c
		if err := rows.Scan(&x.kind, &x.id, &x.prior); err != nil {
			_ = rows.Close()
			return err
		}
		closures = append(closures, x)
	}
	_ = rows.Close()
	for _, x := range closures {
		var stmt string
		switch x.kind {
		case "task":
			stmt = `UPDATE task SET status = ?, updated_at = ? WHERE id = ? AND status = 'skipped'`
		case "roadmap":
			stmt = `UPDATE roadmap_item SET status = ?, updated_at = ? WHERE CAST(id AS TEXT) = ? AND status = 'cancelled'`
		case "queue":
			stmt = `UPDATE queue_item SET status = ?, updated_at = ? WHERE CAST(id AS TEXT) = ? AND status = 'cancelled'`
		case "goal":
			stmt = `UPDATE goal SET review_state = ?, updated_at = ? WHERE id = ? AND review_state = 'parked'`
		}
		if _, err := q.ExecContext(ctx, stmt, x.prior, now, x.id); err != nil {
			return fmt.Errorf("reopen a %s: %w", x.kind, err)
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM work_closure WHERE entity_kind = ? AND entity_id = ?`, x.kind, x.id); err != nil {
			return err
		}
	}
	return nil
}

// ListWorkClosures lists the closed-not-done work, optionally for one component.
func (s *Store) ListWorkClosures(ctx context.Context, componentRoot string) ([]WorkClosure, error) {
	q := `SELECT entity_kind, entity_id, reason, loop_slug, component_root, prior_status, closed_at FROM work_closure`
	args := []any{}
	if componentRoot != "" {
		q += ` WHERE component_root = ?`
		args = append(args, componentRoot)
	}
	rows, err := s.r.QueryContext(ctx, q+` ORDER BY closed_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list closed work: %w", err)
	}
	defer rows.Close()
	var out []WorkClosure
	for rows.Next() {
		var c WorkClosure
		if err := rows.Scan(&c.Kind, &c.ID, &c.Reason, &c.LoopSlug, &c.ComponentRoot, &c.PriorStatus, &c.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
