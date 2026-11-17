package daemon

// doc_service.go — the daemon's half of the `doc_*` tools: a step's door to its
// loop's documents. The loop is read from the step on the context the runtime
// stamps at dispatch, never from the model's arguments, so a model can name a
// document and nothing that addresses another loop. A document the step's loop
// did not write is readable when the goal the loop works owns it — that is how
// the goal's loop reads what the brainstorm produced.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// DocService implements tools.DocStore over the store.
type DocService struct{ store *store.Store }

// NewDocService builds the service over the store.
func NewDocService(s *store.Store) *DocService { return &DocService{store: s} }

func docServiceOrNil(s *DocService) tools.DocStore {
	if s == nil {
		return nil
	}
	return s
}

// docScope resolves the step on the context to its loop, its agent and its goal.
func (d *DocService) docScope(ctx context.Context) (sc store.Scope, agent, goal string, err error) {
	id, ok := stepctx.StepID(ctx)
	if !ok {
		return store.Scope{}, "", "", errors.New("documents need a step on the context, and this call carries none")
	}
	st, err := d.store.ReadStep(id)
	if err != nil {
		return store.Scope{}, "", "", fmt.Errorf("read the step %d that owns the document: %w", id, err)
	}
	sc = store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot}
	goal, err = d.store.LoopGoal(sc)
	return sc, st.Agent, goal, err
}

func docEntry(doc store.Document, own store.Scope) tools.DocEntry {
	return tools.DocEntry{Name: doc.File, Rev: doc.Rev, Bytes: doc.Bytes, SHA256: doc.SHA256,
		WrittenBy: doc.WrittenBy, WrittenAt: doc.WrittenAt, Loop: doc.LoopSlug, Goal: doc.OwnerGoal,
		Inherited: doc.LoopSlug != own.LoopSlug || doc.ComponentRoot != own.ComponentRoot}
}

// Write implements tools.DocStore.
func (d *DocService) Write(ctx context.Context, name, content string) (tools.DocEntry, bool, error) {
	sc, agent, _, err := d.docScope(ctx)
	if err != nil {
		return tools.DocEntry{}, false, err
	}
	res, err := d.store.WriteDocument(sc, name, content, agent)
	if err != nil {
		return tools.DocEntry{}, false, err
	}
	if !res.OK {
		return tools.DocEntry{}, false, &tools.DocRefused{Reason: res.Reason}
	}
	return docEntry(res.Document, sc), res.Unchanged, nil
}

// Read implements tools.DocStore: the step's own loop first, then its goal.
func (d *DocService) Read(ctx context.Context, name string) (tools.DocEntry, string, error) {
	sc, _, goal, err := d.docScope(ctx)
	if err != nil {
		return tools.DocEntry{}, "", err
	}
	doc, err := d.store.ReadDocument(sc, name)
	if err != nil && errors.Is(err, store.ErrNotFound) && goal != "" {
		doc, err = d.store.ReadGoalDocument(goal, name)
	}
	if errors.Is(err, store.ErrNotFound) {
		return tools.DocEntry{}, "", &tools.DocRefused{Reason: fmt.Sprintf("this loop has no %s%s; doc_list shows what exists", name, goalClause(goal))}
	}
	if err != nil {
		if resolved := store.ResolveDocumentName(name); !resolved.OK() {
			return tools.DocEntry{}, "", &tools.DocRefused{Reason: resolved.Reason}
		}
		return tools.DocEntry{}, "", err
	}
	return docEntry(doc, sc), doc.Content, nil
}

func goalClause(goal string) string {
	if goal == "" {
		return ""
	}
	return " and neither does goal " + goal
}

// List implements tools.DocStore: the loop's own heads, then the goal's it did
// not write itself (a name the loop wrote shadows the goal's).
func (d *DocService) List(ctx context.Context) ([]tools.DocEntry, error) {
	sc, _, goal, err := d.docScope(ctx)
	if err != nil {
		return nil, err
	}
	own, err := d.store.ListDocuments(sc)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []tools.DocEntry
	for _, doc := range own {
		seen[doc.File] = true
		out = append(out, docEntry(doc, sc))
	}
	if goal == "" {
		return out, nil
	}
	owned, err := d.store.GoalDocuments(goal)
	if err != nil {
		return nil, err
	}
	for _, doc := range owned {
		if !seen[doc.File] {
			out = append(out, docEntry(doc, sc))
		}
	}
	return out, nil
}
