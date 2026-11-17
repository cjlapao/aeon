package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// S75 T1: the loop's todo list, persisted.

func todoItems(pairs ...string) []TodoItem {
	var out []TodoItem
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, TodoItem{ID: pairs[i], Content: "do " + pairs[i], Status: pairs[i+1]})
	}
	return out
}

func TestTodoReplaceOpensAndRewritesTheActiveList(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sc := seedLoop(t, s, "chat-1", "/repo")

	l, err := s.TodoReplace(ctx, sc, 0, 0, todoItems("a", "pending", "b", "in_progress"))
	if err != nil || l.Revision != 1 || !l.Active() {
		t.Fatalf("first set: %+v err=%v", l, err)
	}
	l, err = s.TodoReplace(ctx, sc, 0, 1, todoItems("a", "done", "b", "in_progress", "c", "pending"))
	if err != nil || l.Revision != 2 || !l.Active() {
		t.Fatalf("second set: %+v err=%v", l, err)
	}
	got, items, found, err := s.TodoRead(ctx, sc)
	if err != nil || !found || got.Revision != 2 || len(items) != 3 || items[0].Status != "done" || items[2].ID != "c" {
		t.Fatalf("read: %+v %+v found=%v err=%v", got, items, found, err)
	}
}

func TestTodoStaleRevisionIsRefusedNamingBoth(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sc := seedLoop(t, s, "chat-1", "/repo")
	if _, err := s.TodoReplace(ctx, sc, 0, 0, todoItems("a", "pending")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TodoReplace(ctx, sc, 0, 1, todoItems("a", "in_progress")); err != nil {
		t.Fatal(err)
	}
	_, err := s.TodoReplace(ctx, sc, 0, 1, todoItems("a", "done"))
	if !errors.Is(err, ErrTodoStale) || !strings.Contains(err.Error(), "revision 1") || !strings.Contains(err.Error(), "revision 2") {
		t.Fatalf("a stale write must be refused naming both revisions: %v", err)
	}
}

func TestTodoClosesWhenAllDoneOrCleared(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sc := seedLoop(t, s, "chat-1", "/repo")
	if _, err := s.TodoReplace(ctx, sc, 0, 0, todoItems("a", "pending")); err != nil {
		t.Fatal(err)
	}
	l, err := s.TodoReplace(ctx, sc, 0, 1, todoItems("a", "done"))
	if err != nil || l.Active() || l.ClosedBy != TodoClosedAllDone {
		t.Fatalf("all done must close: %+v err=%v", l, err)
	}
	// A new set starts a NEW list at revision 1.
	l, err = s.TodoReplace(ctx, sc, 0, 0, todoItems("x", "pending"))
	if err != nil || l.Revision != 1 || !l.Active() {
		t.Fatalf("a set after a close opens a new list: %+v err=%v", l, err)
	}
	l, err = s.TodoReplace(ctx, sc, 0, 1, nil)
	if err != nil || l.Active() || l.ClosedBy != TodoClosedCleared {
		t.Fatalf("an empty set clears and closes: %+v err=%v", l, err)
	}
	// The most recent closed list is what a read answers when none is active.
	got, _, found, _ := s.TodoRead(ctx, sc)
	if !found || got.Active() || got.ClosedBy != TodoClosedCleared {
		t.Fatalf("read after close: %+v found=%v", got, found)
	}
}

func TestTodoPersonCloseAndNoOpAndNeverHadAList(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sc := seedLoop(t, s, "chat-1", "/repo")
	if _, _, found, _ := s.TodoRead(ctx, sc); found {
		t.Fatal("a loop that never had a list reports none")
	}
	if _, err := s.TodoReplace(ctx, sc, 0, 0, nil); err != nil {
		t.Fatalf("clearing nothing writes nothing: %v", err)
	}
	if _, _, found, _ := s.TodoRead(ctx, sc); found {
		t.Fatal("an empty set on a loop with no list must not create one")
	}
	if _, err := s.TodoReplace(ctx, sc, 0, 0, todoItems("a", "pending")); err != nil {
		t.Fatal(err)
	}
	l, changed, err := s.TodoClose(ctx, sc, TodoClosedPerson)
	if err != nil || !changed || l.ClosedBy != TodoClosedPerson {
		t.Fatalf("person close: %+v changed=%v err=%v", l, changed, err)
	}
	if _, changed, _ := s.TodoClose(ctx, sc, TodoClosedPerson); changed {
		t.Fatal("closing an already closed list is a no-op that says so")
	}
}

func TestTodoListsAreScopedToTheirLoop(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")
	if _, err := s.TodoReplace(ctx, a, 0, 0, todoItems("only-a", "pending")); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := s.TodoRead(ctx, b); found {
		t.Fatal("the same slug under another component root must not see the list")
	}
}

func TestTodoRefusesStructurallyInvalidRows(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "chat-1", "/repo")
	for _, bad := range [][]TodoItem{
		{{ID: "", Content: "x", Status: "pending"}},
		{{ID: "a", Content: "", Status: "pending"}},
		{{ID: "a", Content: "x", Status: "blocked"}},
	} {
		if _, err := s.TodoReplace(context.Background(), sc, 0, 0, bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
