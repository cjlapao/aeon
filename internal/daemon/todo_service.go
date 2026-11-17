package daemon

// todo_service.go — S75 T2/T3: the daemon's half of the loop's persisted todo
// list. One service, three doors: the tools' TodoStore seam (a step's door, the
// step identity read from the stepctx the runtime stamps at dispatch), the
// `loop.todos` read, and the person's close. Every write publishes
// `todo.updated`, so a live client never parses a tool result to learn the list.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/runtime"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// TodoService is the one implementation behind the three doors.
type TodoService struct {
	store *store.Store
	// publish is bound after the Daemon exists (the recordsSvc.taskChanged
	// pattern): a service built before it has nowhere to announce, and an
	// unbound publish simply does not announce.
	publish func(kind, componentRoot string, payload any)
}

// NewTodoService builds the service over the store.
func NewTodoService(s *store.Store) *TodoService { return &TodoService{store: s} }

// todoScope resolves the step on the context to its loop.
func (t *TodoService) todoScope(ctx context.Context) (store.Scope, int64, error) {
	id, ok := stepctx.StepID(ctx)
	if !ok {
		return store.Scope{}, 0, errors.New("the todo list needs a step on the context, and this call carries none")
	}
	st, err := t.store.ReadStep(id)
	if err != nil {
		return store.Scope{}, 0, fmt.Errorf("read the step %d that owns the todo list: %w", id, err)
	}
	return store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot}, id, nil
}

// Load implements tools.TodoStore.
func (t *TodoService) Load(ctx context.Context) ([]tools.TodoEntry, int, error) {
	sc, _, err := t.todoScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	list, items, found, err := t.store.TodoRead(ctx, sc)
	if err != nil || !found || !list.Active() {
		return nil, 0, err
	}
	out := make([]tools.TodoEntry, 0, len(items))
	for _, it := range items {
		out = append(out, tools.TodoEntry{ID: it.ID, Content: it.Content, Status: it.Status})
	}
	return out, list.Revision, nil
}

// Replace implements tools.TodoStore.
func (t *TodoService) Replace(ctx context.Context, expect int, items []tools.TodoEntry) (int, string, error) {
	sc, step, err := t.todoScope(ctx)
	if err != nil {
		return 0, "", err
	}
	rows := make([]store.TodoItem, 0, len(items))
	for _, it := range items {
		rows = append(rows, store.TodoItem{ID: it.ID, Content: it.Content, Status: it.Status})
	}
	list, err := t.store.TodoReplace(ctx, sc, step, expect, rows)
	if errors.Is(err, store.ErrTodoStale) {
		return 0, "", &tools.TodoStaleError{Detail: err.Error()}
	}
	if err != nil {
		return 0, "", err
	}
	t.announce(sc, step, list, rows)
	return list.Revision, list.ClosedBy, nil
}

// TodoView is the protocol shape of one list read.
func (t *TodoService) Read(ctx context.Context, sc store.Scope) (protocol.LoopTodosResult, error) {
	list, items, found, err := t.store.TodoRead(ctx, sc)
	if err != nil {
		return protocol.LoopTodosResult{}, err
	}
	if !found {
		return protocol.LoopTodosResult{Items: []protocol.TodoItemView{}, Statement: protocol.TodoNoListStatement}, nil
	}
	return todoResult(list, items), nil
}

// Close is the person's close (TD8): the human door checks the actor; this only
// writes. changed is false when no list was active.
func (t *TodoService) Close(ctx context.Context, sc store.Scope) (protocol.LoopTodosResult, bool, error) {
	list, changed, err := t.store.TodoClose(ctx, sc, store.TodoClosedPerson)
	if err != nil {
		return protocol.LoopTodosResult{}, false, err
	}
	_, items, _, err := t.store.TodoRead(ctx, sc)
	if err != nil {
		return protocol.LoopTodosResult{}, false, err
	}
	if changed {
		t.announce(sc, 0, list, items)
	}
	return todoResult(list, items), changed, nil
}

// ActiveOpen lists the ids of the items still open on the loop's active list;
// ok is false when no list is active. The runtime's nudge and guard read this
// through the daemon (TD9, TD10).
func (t *TodoService) ActiveOpen(ctx context.Context, sc store.Scope) (open []string, ok bool, err error) {
	list, items, found, err := t.store.TodoRead(ctx, sc)
	if err != nil || !found || !list.Active() {
		return nil, false, err
	}
	for _, it := range items {
		if it.Status != "done" {
			open = append(open, it.ID)
		}
	}
	return open, true, nil
}

func (t *TodoService) announce(sc store.Scope, step int64, list store.TodoList, items []store.TodoItem) {
	if t.publish == nil {
		return
	}
	res := todoResult(list, items)
	// loopId is what every other loop event carries and what a chat filters on.
	loopID := ""
	if l, err := t.store.GetLoop(sc); err == nil {
		loopID = l.ID
	}
	t.publish(protocol.KindTodoUpdated, sc.ComponentRoot, map[string]any{
		"loopId": loopID, "loop": sc.LoopSlug, "step": step, "revision": res.Revision, "items": res.Items,
		"counts": res.Counts, "active": res.Active, "closedBy": res.ClosedBy,
	})
}

func todoResult(list store.TodoList, items []store.TodoItem) protocol.LoopTodosResult {
	out := protocol.LoopTodosResult{
		Active: list.Active(), Revision: list.Revision, Items: make([]protocol.TodoItemView, 0, len(items)),
		ClosedAt: list.ClosedAt, ClosedBy: list.ClosedBy,
	}
	for _, it := range items {
		out.Items = append(out.Items, protocol.TodoItemView{ID: it.ID, Content: it.Content, Status: it.Status})
		switch it.Status {
		case "pending":
			out.Counts.Pending++
		case "in_progress":
			out.Counts.InProgress++
		case "done":
			out.Counts.Done++
		}
	}
	return out
}

// loopTodosAdapter is the protocol seam over the service: it resolves a loop id
// (what `loop.list` prints) to the loop's scope.
type loopTodosAdapter struct{ svc *TodoService }

func (a loopTodosAdapter) scope(id string) (store.Scope, error) {
	l, err := a.svc.store.ReadLoopByID(id)
	if err != nil {
		return store.Scope{}, err
	}
	return store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, nil
}

func (a loopTodosAdapter) TodosRead(ctx context.Context, loopID string) (protocol.LoopTodosResult, error) {
	sc, err := a.scope(loopID)
	if err != nil {
		return protocol.LoopTodosResult{}, err
	}
	return a.svc.Read(ctx, sc)
}

func (a loopTodosAdapter) TodosClose(ctx context.Context, loopID string) (protocol.LoopTodosResult, bool, error) {
	sc, err := a.scope(loopID)
	if err != nil {
		return protocol.LoopTodosResult{}, false, err
	}
	return a.svc.Close(ctx, sc)
}

// todoSvcOrNil keeps a nil *TodoService from becoming a non-nil interface.
func todoSvcOrNil(s *TodoService) tools.TodoStore {
	if s == nil {
		return nil
	}
	return s
}

// ActiveEntries is the compile's read: the active list's rows in tool form.
func (t *TodoService) ActiveEntries(ctx context.Context, loopSlug, componentRoot string) ([]tools.TodoEntry, bool) {
	list, items, found, err := t.store.TodoRead(ctx, store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot})
	if err != nil || !found || !list.Active() {
		return nil, false
	}
	out := make([]tools.TodoEntry, 0, len(items))
	for _, it := range items {
		out = append(out, tools.TodoEntry{ID: it.ID, Content: it.Content, Status: it.Status})
	}
	return out, true
}

// todoActiveOrNil is the compile seam, nil without a store.
func todoActiveOrNil(s *TodoService) func(context.Context, string, string) ([]tools.TodoEntry, bool) {
	if s == nil {
		return nil
	}
	return s.ActiveEntries
}

// todoNudgeSetting is the declared key (config/settings_decl_todo.go).
const (
	todoNudgeSetting = "todo.nudgeAfter"
	todoNudgeDefault = 8
)

// stepTodoPolicy is one step's runtime.TodoPolicy (TD9/TD10).
type stepTodoPolicy struct {
	svc      *TodoService
	settings *config.Store
	stepID   int64
	lane     string
}

// todoPolicyFactory builds the Table's per-step factory over the service.
func todoPolicyFactory(svc *TodoService, settings *config.Store) func(int64, string) runtime.TodoPolicy {
	if svc == nil {
		return nil
	}
	return func(stepID int64, lane string) runtime.TodoPolicy {
		return stepTodoPolicy{svc: svc, settings: settings, stepID: stepID, lane: lane}
	}
}

// ActiveList renders the active list for the post-compaction restore.
func (p stepTodoPolicy) ActiveList(ctx context.Context) (string, bool) {
	sc, _, err := p.svc.todoScope(stepctx.WithStepID(ctx, p.stepID))
	if err != nil {
		return "", false
	}
	items, ok := p.svc.ActiveEntries(ctx, sc.LoopSlug, sc.ComponentRoot)
	if !ok || len(items) == 0 {
		return "", false
	}
	return tools.RenderTodoEntries(items), true
}

func (p stepTodoPolicy) ActiveOpen(ctx context.Context) ([]string, bool) {
	sc, _, err := p.svc.todoScope(stepctx.WithStepID(ctx, p.stepID))
	if err != nil {
		return nil, false
	}
	open, ok, err := p.svc.ActiveOpen(ctx, sc)
	if err != nil {
		return nil, false
	}
	return open, ok
}

// NudgeAfter reads the setting fresh; an unreadable setting is the declared
// default, never a reason to stop nudging.
func (p stepTodoPolicy) NudgeAfter() int {
	if p.settings == nil {
		return todoNudgeDefault
	}
	n, err := p.settings.Int(context.Background(), todoNudgeSetting)
	if err != nil || n < 0 {
		return todoNudgeDefault
	}
	return n
}

// GuardApplies exempts a conversation: a turn has no lane, and the chat
// blueprint's lane is `converse`. A resumed or recovered step carries no lane on
// hand and stays unguarded.
func (p stepTodoPolicy) GuardApplies() bool { return p.lane != "" && p.lane != "converse" }

// recoveredTodoPolicy is the recovery path's policy: lane unknown, guard off.
func recoveredTodoPolicy(t *protocol.Table, stepID int64) runtime.TodoPolicy {
	if t == nil || t.TodoPolicy == nil {
		return nil
	}
	return t.TodoPolicy(stepID, "")
}
