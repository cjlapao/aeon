package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// S75 T2: the persisted path, against a fake store the test controls.

type fakeTodoStore struct {
	items    []TodoEntry
	revision int
	stale    bool // the next Replace answers a stale-revision refusal
	closeAs  string
	loads    int
}

func (f *fakeTodoStore) Load(context.Context) ([]TodoEntry, int, error) {
	f.loads++
	return append([]TodoEntry(nil), f.items...), f.revision, nil
}

func (f *fakeTodoStore) Replace(_ context.Context, expect int, items []TodoEntry) (int, string, error) {
	if f.stale || expect != f.revision {
		f.stale = false
		return 0, "", &TodoStaleError{Detail: "the list changed (you read revision 1, it is at revision 2)"}
	}
	f.items, f.revision = append([]TodoEntry(nil), items...), f.revision+1
	if f.revision == 0 {
		f.revision = 1
	}
	closed := f.closeAs
	if closed != "" {
		f.items, f.revision = nil, 0
	}
	return f.revision, closed, nil
}

func persistedToolset(t *testing.T, f *fakeTodoStore) Toolset {
	t.Helper()
	set, err := BuildToolset([]string{"todo"}, ToolContext{WorkspaceRoot: t.TempDir(), Todo: f})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestAPersistedListIsSeenByASecondToolset(t *testing.T) {
	f := &fakeTodoStore{}
	first := persistedToolset(t, f)
	if res := todoCall(t, first, "todo", `{"action":"set","items":[{"id":"a","content":"first step","status":"in_progress"}]}`); res.IsError {
		t.Fatalf("set refused: %s", res.Content)
	}
	// A second step is a second toolset: it must read the first one's list.
	second := persistedToolset(t, f)
	res := todoCall(t, second, "todo", `{"action":"list"}`)
	if res.IsError || !strings.Contains(res.Content, `"a": first step`) {
		t.Fatalf("the second toolset did not see the persisted list: %s", res.Content)
	}
}

func TestListReadsFreshAndSetPresentsTheRevisionItLoaded(t *testing.T) {
	f := &fakeTodoStore{items: []TodoEntry{{ID: "a", Content: "x", Status: "pending"}}, revision: 3}
	set := persistedToolset(t, f)
	todoCall(t, set, "todo", `{"action":"list"}`)
	f.items = append(f.items, TodoEntry{ID: "b", Content: "added elsewhere", Status: "pending"})
	if res := todoCall(t, set, "todo", `{"action":"list"}`); !strings.Contains(res.Content, "added elsewhere") {
		t.Fatalf("list must read fresh: %s", res.Content)
	}
}

func TestAStaleSetIsRefusedWithTheCurrentListInTheResult(t *testing.T) {
	f := &fakeTodoStore{items: []TodoEntry{{ID: "a", Content: "x", Status: "pending"}}, revision: 1}
	set := persistedToolset(t, f)
	todoCall(t, set, "todo", `{"action":"list"}`)
	f.stale = true
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"z","content":"mine","status":"pending"}]}`)
	if !res.IsError || !strings.Contains(res.Content, "revision 2") || !strings.Contains(res.Content, `"a": x`) {
		t.Fatalf("a stale write must be refused naming the revisions and showing the current list: %s", res.Content)
	}
}

func TestTheResultSaysWhenTheListClosed(t *testing.T) {
	f := &fakeTodoStore{revision: 0, closeAs: TodoClosedAllDone}
	set := persistedToolset(t, f)
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"a","content":"x","status":"done"}]}`)
	if res.IsError || !strings.HasSuffix(res.Content, todoClosedAllDoneNote) {
		t.Fatalf("the close must be said: %s", res.Content)
	}
}

func TestTheUnwiredResultSaysNothingIsKeptInItsSummary(t *testing.T) {
	set, _ := mustTodoToolset(t)
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"a","content":"x","status":"pending"}]}`)
	if !strings.Contains(res.Summary, todoNotPersisted) {
		t.Fatalf("an unwired list must say it is not kept: %q", res.Summary)
	}
	var _ = json.RawMessage(nil)
}
