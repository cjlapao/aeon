package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// This file is S32 T1's store surface: the cap on component (PA14) and the
// daemon-made worktree ledger (PA5). Both are the only Go doors for their
// rows; the daemon (P34) and the app read through them, never the other way.

// capMin/capMax are the PA14 bounds. The schema enforces them with a CHECK
// (a second, database-level truth); this re-checks on the way in so a bad
// value is a named refusal, never a silent pass, and the sentence names the
// number it refused (the DB19 / "a refusal names its reason" convention).
const (
	capMin = 1
	capMax = 16
)

// ComponentMaxParallelAgents reads only the cap. A component that has no cap
// column yet (an old row) is answered as the schema default, 1 — the
// byte-identical serial behaviour (PA1/D8), never a 0 that would stop every
// loop.
func (s *Store) ComponentMaxParallelAgents(id string) (int, error) {
	var n int
	err := s.r.QueryRow(`SELECT COALESCE(max_parallel_agents, 1) FROM component WHERE id = ?`, id).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: no component %s: %w", id, ErrNotFound)
		}
		return 0, fmt.Errorf("store: read cap for %s: %w", id, err)
	}
	return n, nil
}

// SetComponentMaxParallelAgents is the cap's own write door (PA14): it
// changes only the ceiling and the updated_at, leaving every other component
// field exactly as it was. A value outside 1..16 is refused before any write
// — the schema's CHECK is the backstop, the sentence here is the thing a
// person reads.
func (s *Store) SetComponentMaxParallelAgents(id string, n int) (Component, error) {
	if n < capMin || n > capMax {
		return Component{}, fmt.Errorf("store: max_parallel_agents must be between %d and %d, got %d", capMin, capMax, n)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Component{}, fmt.Errorf("store: set cap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	result, err := tx.Exec(`UPDATE component SET max_parallel_agents=?, updated_at=? WHERE id=?`, n, now, id)
	if err != nil {
		return Component{}, fmt.Errorf("store: set cap for %s: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Component{}, fmt.Errorf("store: count cap update: %w", err)
	}
	if changed == 0 {
		return Component{}, fmt.Errorf("store: component %q does not exist", id)
	}
	if err := tx.Commit(); err != nil {
		return Component{}, fmt.Errorf("store: commit cap for %s: %w", id, err)
	}
	return s.ReadComponent(id)
}

// LoopWorktree is PA5's ledger row: the daemon's intent to make (and later
// remove) one ephemeral worktree for one concurrently-dispatched unit. It is
// deliberately NOT component_root (that table is a human-made permanent
// second component) — this is scratch space that is gone before the loop's
// next person-visible state change, never surfaced on any socket method or
// CLI listing.
//
// OwnerKind/OwnerID generalise over both PA4 shapes: a Shape-A lane has no
// task row to hang a worktree on, a Shape-B item does — so the owner is a
// 'lane' id or a 'task' id, named in the row, never a second table.
type LoopWorktree struct {
	ID            int64
	ComponentRoot string
	LoopSlug      string
	OwnerKind     string // 'lane', 'task', or 'integration' (S60 MQ6: the queue's merge-forward worktree)
	OwnerID       string
	Path          string
	Branch        string
	BaseSHA       string
	CreatedAt     string
	RemovedAt     *string // NULL while the worktree is live — the recovery signal (PA12)
}

// CreateLoopWorktree is PA5's write-ahead step 1: the intent is recorded
// BEFORE the filesystem operation (step 2 is the git worktree add). A crash
// between the two leaves a live row with no directory, which recovery (PA12)
// tells apart by asking git — never by trusting the row alone.
func (s *Store) CreateLoopWorktree(w LoopWorktree) (LoopWorktree, error) {
	if w.ComponentRoot == "" || w.LoopSlug == "" || w.OwnerKind == "" || w.OwnerID == "" || w.Path == "" {
		return LoopWorktree{}, fmt.Errorf("store: a loop_worktree needs a component, a loop, an owner, and a path")
	}
	if w.OwnerKind != "lane" && w.OwnerKind != "task" && w.OwnerKind != "integration" {
		return LoopWorktree{}, fmt.Errorf("store: loop_worktree owner_kind must be 'lane', 'task' or 'integration', got %q", w.OwnerKind)
	}
	now := s.Now()
	w.CreatedAt = now
	res, err := s.w.Exec(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, w.ComponentRoot, w.LoopSlug, w.OwnerKind, w.OwnerID, w.Path, w.Branch, w.BaseSHA, now)
	if err != nil {
		return LoopWorktree{}, fmt.Errorf("store: create loop_worktree: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LoopWorktree{}, fmt.Errorf("store: insert loop_worktree: %w", err)
	}
	w.ID = id
	return w, nil
}

// ListLiveLoopWorktrees returns every worktree row that has not been removed
// (removed_at IS NULL) for one loop — the PA12 reconciliation pass asks git
// about each of these. A loop with no live rows returns an empty slice, not
// an error (a loop with no concurrent work has no worktrees).
func (s *Store) ListLiveLoopWorktrees(scope Scope) ([]LoopWorktree, error) {
	rows, err := s.r.Query(`SELECT id, component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at
		FROM loop_worktree WHERE component_root = ? AND loop_slug = ? AND removed_at IS NULL`, scope.ComponentRoot, scope.LoopSlug)
	if err != nil {
		return nil, fmt.Errorf("store: list live loop_worktrees: %w", err)
	}
	defer rows.Close()
	out := []LoopWorktree{}
	for rows.Next() {
		var w LoopWorktree
		if err := rows.Scan(&w.ID, &w.ComponentRoot, &w.LoopSlug, &w.OwnerKind, &w.OwnerID, &w.Path, &w.Branch, &w.BaseSHA, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan loop_worktree: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list live loop_worktrees: %w", err)
	}
	return out, nil
}

// RemoveLoopWorktree is PA5's write-ahead step 4: after the git worktree is
// gone, the row's removed_at is set. The guard (WHERE removed_at IS NULL) is
// the settle-once discipline every other settle-once table in this codebase
// uses — a second call is a no-op, never a second removal, so a crashed
// recovery cannot remove the same worktree twice.
func (s *Store) RemoveLoopWorktree(id int64) error {
	if id == 0 {
		return fmt.Errorf("store: cannot remove loop_worktree 0")
	}
	now := s.Now()
	result, err := s.w.Exec(`UPDATE loop_worktree SET removed_at=? WHERE id=? AND removed_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("store: remove loop_worktree %d: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: count loop_worktree removal: %w", err)
	}
	if changed == 0 {
		// Already removed (or never existed). The caller treats this as the
		// settled state, not a failure — a row that is already removed is the
		// success the guarded UPDATE was meant to guarantee.
		return nil
	}
	return nil
}
