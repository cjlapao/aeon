package store

// tasks_board.go — the ONE read the board asked for (OL14's "Board reads").
// One filtered, ordered, cost-carrying read — filters by project, component,
// manual, assignee, status, type, goal, loop and parent — because a kanban
// that polls six narrower endpoints invents six ways to disagree.
//
// The cost is a FLOOR and always says so (P32): `CostIsFloor` is true when
// any contributing step's cost was partial or unknown, and a surface that
// drops the flag is reporting a lower bound as a total. Nothing here is
// stored — the sum is computed per read (OL11), which is also why no
// migration ever has to remember to update it.

import (
	"database/sql"
	"errors"
	"fmt"
)

// TaskFilter narrows the board read. Empty fields mean "any"; Manual is a
// pointer because "show me only loop tasks" and "show me either" are
// different questions an empty string cannot tell apart. Root is the tree's
// root (CT10): a task in the root OR its single child is in scope, because
// the tree is at most two levels.
type TaskFilter struct {
	Root         string
	ComponentID  string
	Manual       *bool
	AssigneeKind string
	Assignee     string
	Status       string
	Type         string
	// ApprovalState asks for manual tasks the PERSON has approved
	// (S38 T13's offer(): only a person-approved manual task can carry a
	// requires clause). "" means unfiltered.
	ApprovalState string
	GoalID        string
	LoopSlug      string
	ParentTaskID  string
	Limit         int
}

// TaskRow is one board line: the task, its cost with the floor flag, and
// every loop working it.
type TaskRow struct {
	Task
	CostUSD     float64  `json:"costUsd"`
	CostIsFloor bool     `json:"costIsFloor"`
	LoopSlugs   []string `json:"loopSlugs"`
}

// boardColumns is taskColumns plus nothing — the same const, so the board can
// never show a shape the ordinary reads do not.
const boardDefaultLimit = 200
const boardMaxLimit = 1000

// ListTaskRows is the board read.
func (s *Store) ListTaskRows(f TaskFilter) ([]TaskRow, error) {
	var where []string
	var args []any
	if f.Root != "" {
		// The root's whole tree, not two levels: a board scoped to a group
		// shows the group, root and every descendant.
		where = append(where, `component_id IN (SELECT id FROM component WHERE id = ?
			UNION ALL
			SELECT c.id FROM component c JOIN component t ON c.parent_id = t.id
			WHERE t.id IN (SELECT id FROM component WHERE id = ?))`)
		args = append(args, f.Root, f.Root)
	}
	if f.ComponentID != "" {
		where = append(where, `component_id = ?`)
		args = append(args, f.ComponentID)
	}
	if f.Manual != nil {
		where = append(where, `manual = ?`)
		args = append(args, *f.Manual)
	}
	if f.AssigneeKind != "" {
		where = append(where, `assignee_kind = ?`)
		args = append(args, f.AssigneeKind)
	}
	if f.Assignee != "" {
		where = append(where, `assignee = ?`)
		args = append(args, f.Assignee)
	}
	if f.Status != "" {
		where = append(where, `status = ?`)
		args = append(args, f.Status)
	}
	if f.Type != "" {
		where = append(where, `type = ?`)
		args = append(args, f.Type)
	}
	if f.GoalID != "" {
		where = append(where, `goal_id = ?`)
		args = append(args, f.GoalID)
	}
	if f.ApprovalState != "" {
		where = append(where, `approval_state = ?`)
		args = append(args, f.ApprovalState)
	}
	if f.ParentTaskID != "" {
		where = append(where, `parent_task_id = ?`)
		args = append(args, f.ParentTaskID)
	}
	if f.LoopSlug != "" {
		where = append(where, `EXISTS (SELECT 1 FROM task_loop tl JOIN loop l ON l.component_root = tl.component_root AND l.slug = tl.loop_slug
			WHERE tl.task_id = task.id AND l.slug = ?)`)
		args = append(args, f.LoopSlug)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + join(where, " AND ")
	}
	limit := f.Limit
	switch {
	case limit <= 0:
		limit = boardDefaultLimit
	case limit > boardMaxLimit:
		limit = boardMaxLimit
	}
	rows, err := s.r.Query(`SELECT `+taskColumns+` FROM task`+clause+` ORDER BY updated_at DESC, id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: board read: %w", err)
	}
	var out []TaskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("store: board read scan: %w", err)
		}
		out = append(out, TaskRow{Task: t})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("store: board read: %w", err)
	}
	_ = rows.Close() // drained before the per-row reads — one writer connection, no nesting

	for i := range out {
		slugs, err := s.taskLoopSlugs(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].LoopSlugs = slugs
		cost, floor, err := s.taskCost(out[i].Task)
		if err != nil {
			return nil, err
		}
		out[i].CostUSD, out[i].CostIsFloor = cost, floor
	}
	return out, nil
}

// taskLoopSlugs reads every loop working a task, owners first.
func (s *Store) taskLoopSlugs(taskID string) ([]string, error) {
	rows, err := s.r.Query(`SELECT loop_slug FROM task_loop WHERE task_id = ? ORDER BY role, loop_slug`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: loops of task %s: %w", taskID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("store: loops of task %s: %w", taskID, err)
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// taskCost computes the floor-flagged cost. Loop tasks sum their own steps
// (P32's group-by over step.taskid); manual tasks sum their CHILDREN's
// costs — the *what* costs what its *how* cost. An unknown-cost step makes
// the whole thing a floor; it is never silently zero.
func (s *Store) taskCost(t Task) (float64, bool, error) {
	var sum sql.NullFloat64
	var floor int
	var err error
	// A task whose loop was deleted keeps what it had spent: its steps went with the loop, so the amount was written
	// down when the link was cut.
	if t.LoopSlug == "" {
		var frozen sql.NullFloat64
		var frozenFloor int
		if e := s.r.QueryRow(`SELECT cost_usd, cost_floor FROM detached_link WHERE entity_kind = 'task' AND entity_id = ?`, t.ID).Scan(&frozen, &frozenFloor); e == nil && frozen.Valid {
			return frozen.Float64, frozenFloor == 1, nil
		}
	}
	if !t.Manual && t.LoopSlug != "" {
		err = s.r.QueryRow(`SELECT COALESCE(SUM(s.cost_usd),0), COALESCE(MAX(s.cost_state IN ('partial','unknown')),0)
			FROM step s WHERE s.component_root = ? AND s.loop_slug = ? AND s.taskid = ?`,
			t.ComponentRoot, t.LoopSlug, t.TaskID).Scan(&sum, &floor)
	} else if t.Manual {
		err = s.r.QueryRow(`SELECT COALESCE(SUM(st.cost_usd),0), COALESCE(MAX(st.cost_state IN ('partial','unknown')),0)
			FROM task c JOIN step st ON st.component_root = c.component_root AND st.loop_slug = c.loop_slug AND st.taskid = c.taskid
			WHERE c.parent_task_id = ?`, t.ID).Scan(&sum, &floor)
		// Children whose loop was deleted still count, by the cost written down when they were cut loose.
		if err == nil {
			var kept sql.NullFloat64
			var keptFloor int
			if e := s.r.QueryRow(`SELECT COALESCE(SUM(d.cost_usd),0), COALESCE(MAX(d.cost_floor),0) FROM detached_link d JOIN task c ON c.id = d.entity_id
				WHERE d.entity_kind = 'task' AND c.parent_task_id = ?`, t.ID).Scan(&kept, &keptFloor); e == nil && kept.Valid && kept.Float64 > 0 {
				sum = sql.NullFloat64{Float64: sum.Float64 + kept.Float64, Valid: true}
				if keptFloor == 1 {
					floor = 1
				}
			}
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("store: cost of task %s: %w", t.ID, err)
	}
	if sum.Valid {
		return sum.Float64, floor == 1, nil
	}
	return 0, false, nil
}
