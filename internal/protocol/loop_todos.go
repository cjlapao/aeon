package protocol

// loop_todos.go — S75 T3: the loop's todo list, on the socket.
//
// Two doors over one seam (`Table.LoopTodos`, filled by the daemon):
//
//   - `loop.todos {id}` (read) answers the loop's active list or, when none is
//     active, the most recent closed one with `active: false`. A loop that never
//     had a list answers an empty list and a statement, never an error (P31: the
//     absence is said, not dressed as a failure or as a fact).
//   - `loop.todos.close {id}` (write, human door only) closes the active list as
//     the person's act. An agent has no road to it: the actor is the connection's
//     peer credential, a params-supplied actor is refused by name, and an agent
//     closes only by finishing or clearing the list through its own tool.
//
// The seam is absent when the daemon has no store (V13): the methods are ABSENT,
// not registered-and-refusing.

import (
	"context"
	"encoding/json"
	"fmt"
)

// TodoNoListStatement is the daemon's sentence for a loop that never had a list.
const TodoNoListStatement = "this loop has never had a todo list"

// TodoItemView is one checklist row on the wire.
type TodoItemView struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// TodoCounts is the three-term count a panel shows (never a headline the rows
// could contradict: it is counted from them).
type TodoCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Done       int `json:"done"`
}

// LoopTodosResult answers both doors.
type LoopTodosResult struct {
	Active    bool           `json:"active"`
	Revision  int            `json:"revision"`
	Items     []TodoItemView `json:"items"`
	Counts    TodoCounts     `json:"counts"`
	ClosedAt  string         `json:"closedAt,omitempty"`
	ClosedBy  string         `json:"closedBy,omitempty"`
	Statement string         `json:"statement,omitempty"`
}

// LoopTodosParams names the loop by the id `loop.list` prints.
type LoopTodosParams struct {
	ID string `json:"id"`
	// Actor is never read; present to be refused by name on the write.
	Actor string `json:"actor,omitempty"`
}

// LoopTodos is the daemon's adapter. An unknown loop id comes back as the
// store's not-found error and is rendered by the handler.
type LoopTodos interface {
	TodosRead(ctx context.Context, loopID string) (LoopTodosResult, error)
	// TodosClose closes the active list; changed is false when none was active.
	TodosClose(ctx context.Context, loopID string) (res LoopTodosResult, changed bool, err error)
}

// RegisterLoopTodos registers the two doors when — and only when — the seam is
// wired. Returns whether they are in the table.
func (t *Table) RegisterLoopTodos() bool {
	if t.LoopTodos == nil {
		return false
	}
	t.register(&Method{Name: MethodLoopTodos, RequiresInitialize: true, Handle: t.handleLoopTodos})
	t.register(&Method{Name: MethodLoopTodosClose, RequiresInitialize: true, Handle: t.handleLoopTodosClose})
	return true
}

func (t *Table) handleLoopTodos(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.LoopTodos == nil {
		return nil, ErrNotWired.WithWhat("the loop todo list is not wired — the daemon started without a store")
	}
	var p LoopTodosParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop whose todo list to read")}
	}
	return t.LoopTodos.TodosRead(ctx, p.ID)
}

func (t *Table) handleLoopTodosClose(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.LoopTodos == nil {
		return nil, ErrNotWired.WithWhat("the loop todo list is not wired — the daemon started without a store")
	}
	var p LoopTodosParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop whose todo list to close")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	// The person is the connection; no identity means no close.
	if _, err := t.controlActor(ctx, sess); err != nil {
		return nil, err
	}
	res, _, err := t.LoopTodos.TodosClose(ctx, p.ID)
	return res, err
}
