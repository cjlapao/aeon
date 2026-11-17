package runtime

import (
	"strings"
	"testing"
)

// S75 T4: the loop's open todo list reaches the model through the system half.

func TestAnOpenTodoListRidesTheSystemHalfAndNotTheUserHalf(t *testing.T) {
	in := skillsTestInput()
	in.TodoSection = &TodoSection{Text: "[x] \"a\": read the repo\n[~] \"b\": ask scope\n1 pending, 1 in progress, 1 done", Open: 2, Total: 3}
	res := compileWithSkills(t, in)
	system := res.Request.Messages[0].Content[0].Text
	user := res.Request.Messages[1].Content[0].Text
	if !strings.Contains(system, TodoSectionTitle) || !strings.Contains(system, `"b": ask scope`) || !strings.Contains(system, TodoRuleSentence) {
		t.Fatalf("the list and its rule must reach the system half:\n%s", system)
	}
	if strings.Contains(user, TodoSectionTitle) || strings.Contains(user, "ask scope") {
		t.Fatalf("the person's half must stay the person's words alone:\n%s", user)
	}
	found := false
	for _, n := range res.Notes {
		if n == "todo: 2 open of 3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the opening note must say how many items are open: %v", res.Notes)
	}
}

func TestNoTodoSectionLeavesTheCompileByteIdentical(t *testing.T) {
	a := compileWithSkills(t, skillsTestInput())
	in := skillsTestInput()
	in.TodoSection = nil
	b := compileWithSkills(t, in)
	for i := range a.Request.Messages {
		if a.Request.Messages[i].Content[0].Text != b.Request.Messages[i].Content[0].Text {
			t.Fatalf("message %d changed with no todo section", i)
		}
	}
	for _, n := range b.Notes {
		if strings.HasPrefix(n, "todo:") {
			t.Fatalf("a compile with no list must not claim one: %v", b.Notes)
		}
	}
}
