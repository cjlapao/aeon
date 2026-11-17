package store

// loop_todo.go — S75 T1: a loop's todo list, persisted.
//
// Reverses W20 for the loop (operator ruling 2026-11-07): the list is the
// loop's, not the step's. One active list per loop (the partial unique index is
// the rule), closed lists stay as history, and every write is one transaction
// that also decides whether the list just closed. The store is not a second
// validator: item bounds are the tool's, and the store refuses only structurally
// invalid rows (empty id or content, a status outside the closed set).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The closed set of reasons a list closes (the migration's CHECK).
const (
	TodoClosedAllDone = "all-done"
	TodoClosedCleared = "cleared"
	TodoClosedPerson  = "person"
)

// TodoItem is one checklist entry.
type TodoItem struct {
	ID      string
	Content string
	Status  string
}

// TodoList is a list's header.
type TodoList struct {
	ID        int64
	Revision  int
	OpenedAt  string
	UpdatedAt string
	LastStep  int64
	ClosedAt  string // "" while active
	ClosedBy  string // "" while active
}

// Active reports whether the list is still open.
func (l TodoList) Active() bool { return l.ClosedAt == "" }

// ErrTodoStale is returned by TodoReplace when the revision the caller read is
// no longer the list's. The message names both numbers.
var ErrTodoStale = errors.New("store: the todo list changed since it was read")

func validTodoStatus(s string) bool { return s == "pending" || s == "in_progress" || s == "done" }

// TodoRead returns the loop's active list, or — when none is active — the most
// recent closed one. found is false when the loop never had a list.
func (s *Store) TodoRead(ctx context.Context, sc Scope) (list TodoList, items []TodoItem, found bool, err error) {
	if !sc.Valid() {
		return TodoList{}, nil, false, fmt.Errorf("store: TodoRead needs both a slug and a component root, got %s", sc)
	}
	var lastStep sql.NullInt64
	var closedAt, closedBy sql.NullString
	row := s.r.QueryRowContext(ctx, `SELECT id, revision, opened_at, updated_at, last_step, closed_at, closed_by
		FROM loop_todo_list WHERE component_root=? AND loop_slug=?
		ORDER BY (closed_at IS NULL) DESC, id DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug)
	if err := row.Scan(&list.ID, &list.Revision, &list.OpenedAt, &list.UpdatedAt, &lastStep, &closedAt, &closedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TodoList{}, nil, false, nil
		}
		return TodoList{}, nil, false, fmt.Errorf("store: read todo list of %s: %w", sc, err)
	}
	list.LastStep, list.ClosedAt, list.ClosedBy = lastStep.Int64, closedAt.String, closedBy.String
	rows, err := s.r.QueryContext(ctx, `SELECT item_id, content, status FROM loop_todo WHERE list_id=? ORDER BY position`, list.ID)
	if err != nil {
		return TodoList{}, nil, false, fmt.Errorf("store: read todo items of %s: %w", sc, err)
	}
	defer rows.Close()
	for rows.Next() {
		var it TodoItem
		if err := rows.Scan(&it.ID, &it.Content, &it.Status); err != nil {
			return TodoList{}, nil, false, err
		}
		items = append(items, it)
	}
	return list, items, true, rows.Err()
}

// TodoReplace writes the whole list in one transaction. expectRevision is the
// revision the caller loaded (0 when it saw no active list). A stale revision is
// refused with ErrTodoStale. With no active list a new one is opened. The list
// closes in the same transaction when the new items are empty ("cleared") or all
// done ("all-done"); the returned header says which. An empty set on a loop with
// no active list writes nothing and returns a zero list.
func (s *Store) TodoReplace(ctx context.Context, sc Scope, step int64, expectRevision int, items []TodoItem) (TodoList, error) {
	if !sc.Valid() {
		return TodoList{}, fmt.Errorf("store: TodoReplace needs both a slug and a component root, got %s", sc)
	}
	allDone := len(items) > 0
	for i, it := range items {
		if it.ID == "" || it.Content == "" || !validTodoStatus(it.Status) {
			return TodoList{}, fmt.Errorf("store: todo item %d is not structurally valid (id %q, status %q)", i+1, it.ID, it.Status)
		}
		if it.Status != "done" {
			allDone = false
		}
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return TodoList{}, fmt.Errorf("store: begin todo write of %s: %w", sc, err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()

	var id int64
	var revision int
	err = tx.QueryRowContext(ctx, `SELECT id, revision FROM loop_todo_list WHERE component_root=? AND loop_slug=? AND closed_at IS NULL`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&id, &revision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expectRevision != 0 {
			return TodoList{}, fmt.Errorf("%w: it was revision %d and the list has since closed", ErrTodoStale, expectRevision)
		}
		if len(items) == 0 {
			return TodoList{}, tx.Commit() // clearing nothing writes nothing
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO loop_todo_list (loop_slug, component_root, revision, opened_at, updated_at, last_step)
			VALUES (?,?,1,?,?,?)`, sc.LoopSlug, sc.ComponentRoot, now, now, nullStep(step))
		if err != nil {
			return TodoList{}, fmt.Errorf("store: open todo list of %s: %w", sc, err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return TodoList{}, err
		}
		revision = 1
	case err != nil:
		return TodoList{}, fmt.Errorf("store: read active todo list of %s: %w", sc, err)
	default:
		if expectRevision != revision {
			return TodoList{}, fmt.Errorf("%w: the caller read revision %d and the list is at revision %d", ErrTodoStale, expectRevision, revision)
		}
		revision++
		if _, err := tx.ExecContext(ctx, `DELETE FROM loop_todo WHERE list_id=?`, id); err != nil {
			return TodoList{}, err
		}
	}
	for i, it := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO loop_todo (list_id, position, item_id, content, status) VALUES (?,?,?,?,?)`,
			id, i+1, it.ID, it.Content, it.Status); err != nil {
			return TodoList{}, fmt.Errorf("store: write todo item %d of %s: %w", i+1, sc, err)
		}
	}
	var closedAt, closedBy any
	switch {
	case len(items) == 0:
		closedAt, closedBy = now, TodoClosedCleared
	case allDone:
		closedAt, closedBy = now, TodoClosedAllDone
	}
	if _, err := tx.ExecContext(ctx, `UPDATE loop_todo_list SET revision=?, updated_at=?, last_step=?, closed_at=?, closed_by=? WHERE id=?`,
		revision, now, nullStep(step), closedAt, closedBy, id); err != nil {
		return TodoList{}, fmt.Errorf("store: update todo list of %s: %w", sc, err)
	}
	if err := tx.Commit(); err != nil {
		return TodoList{}, fmt.Errorf("store: commit todo write of %s: %w", sc, err)
	}
	out := TodoList{ID: id, Revision: revision, UpdatedAt: now, LastStep: step}
	if closedBy != nil {
		out.ClosedAt, out.ClosedBy = now, closedBy.(string)
	}
	return out, nil
}

// TodoClose closes the active list by a stated closer. changed is false when no
// list was active (already closed or never opened): a no-op that says so.
func (s *Store) TodoClose(ctx context.Context, sc Scope, by string) (list TodoList, changed bool, err error) {
	if !sc.Valid() {
		return TodoList{}, false, fmt.Errorf("store: TodoClose needs both a slug and a component root, got %s", sc)
	}
	if by != TodoClosedPerson && by != TodoClosedAllDone && by != TodoClosedCleared {
		return TodoList{}, false, fmt.Errorf("store: %q is not a todo closer", by)
	}
	now := s.Now()
	res, err := s.w.ExecContext(ctx, `UPDATE loop_todo_list SET revision=revision+1, updated_at=?, closed_at=?, closed_by=?
		WHERE component_root=? AND loop_slug=? AND closed_at IS NULL`, now, now, by, sc.ComponentRoot, sc.LoopSlug)
	if err != nil {
		return TodoList{}, false, fmt.Errorf("store: close todo list of %s: %w", sc, err)
	}
	n, _ := res.RowsAffected()
	l, _, found, err := s.TodoRead(ctx, sc)
	if err != nil || !found {
		return TodoList{}, false, err
	}
	return l, n > 0, nil
}

func nullStep(step int64) any {
	if step <= 0 {
		return nil
	}
	return step
}
