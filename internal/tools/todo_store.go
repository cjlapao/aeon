package tools

// todo_store.go — S75 T2: the persisted half of the `todo` tool.
//
// W20 kept the list inside one step; the operator reversed it for the loop
// (2026-11-07): a list stays active until it is closed. The tools package still
// must not reach a store, so the persistence arrives the way every other
// step-scoped door does — as a field of the step's ToolContext, filled by the
// daemon, identity read from the context the runtime stamps at dispatch
// (stepctx), never from the model's arguments. No package variable: W20's guard
// stands unamended.

import "context"

// TodoEntry is one persisted checklist row, in the tool's own vocabulary.
type TodoEntry struct {
	ID      string
	Content string
	Status  string
}

// The reasons a list closed, as the store spells them.
const (
	TodoClosedAllDone = "all-done"
	TodoClosedCleared = "cleared"
)

// TodoStaleError reports that the list changed between the tool's read and its
// write (another step, or the person). The tool answers with the CURRENT list so
// the model can reapply its change.
type TodoStaleError struct{ Detail string }

func (e *TodoStaleError) Error() string { return e.Detail }

// TodoStore is the step's door to the loop's persisted list. Nil on a
// ToolContext means the daemon was built without a store: the tool then keeps the
// list for this step only and SAYS so (todoNotPersisted).
type TodoStore interface {
	// Load returns the loop's active list and its revision (0 when none is active).
	Load(ctx context.Context) (items []TodoEntry, revision int, err error)
	// Replace writes the whole list. expectRevision is what Load answered. The
	// answer carries the new revision and, when the write closed the list, why.
	Replace(ctx context.Context, expectRevision int, items []TodoEntry) (revision int, closedBy string, err error)
}

// todoNotPersisted is the fixed sentence an unwired result ends with (TD3):
// absent is not zero.
const todoNotPersisted = "(not persisted: this daemon keeps the list for this step only)"

// The sentences a persisted result may append. Fixed strings, one place.
const (
	todoClosedAllDoneNote = "List closed: all items done."
	todoClosedClearedNote = "List closed: cleared."
)

// RenderTodoEntries draws persisted rows with the tool's own renderer, so a list
// the daemon puts in front of the model reads exactly as the tool returns it.
func RenderTodoEntries(entries []TodoEntry) string {
	items := make([]todoItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, todoItem{ID: e.ID, Content: e.Content, Status: e.Status})
	}
	return renderTodoList(items)
}
