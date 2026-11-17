package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestQuestionSpecAndStructuredOptions(t *testing.T) {
	tool := Question()
	if tool.Spec.Name != "question" {
		t.Fatalf("name %q", tool.Spec.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Spec.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"text", "options", "kind"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	free, err := ParseQuestion(json.RawMessage(`{"text":"Which approach?"}`))
	if err != nil || free.Options != nil || free.Kind != "free" {
		t.Fatalf("free form: %+v, %v", free, err)
	}
	choice, err := ParseQuestion(json.RawMessage(`{"text":"Which approach?","options":["a","b"],"kind":"gate"}`))
	if err != nil || len(choice.Options) != 2 || choice.Kind != "gate" {
		t.Fatalf("choice: %+v, %v", choice, err)
	}
}

func TestQuestionRejectsInvalidKind(t *testing.T) {
	if _, err := ParseQuestion(json.RawMessage(`{"text":"Choose","kind":"surprise"}`)); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestQuestionExecutePanicsWithK18(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "K18") {
			t.Errorf("panic = %v", r)
		}
	}()
	Question().Run(context.Background(), ToolContext{}, json.RawMessage(`{"text":"x"}`))
}

func TestQuestionDescriptionExplainsWhenAndHowToAsk(t *testing.T) {
	description := strings.ToLower(Question().Spec.Description)
	for _, phrase := range []string{"decision is theirs", "trade-off", "irreversible", "code, task, or another tool", "already requested", "suspends", "hours", "tool's result"} {
		if !strings.Contains(description, phrase) {
			t.Errorf("description omits %q", phrase)
		}
	}
}

func TestQuestionIsGrantableButNotDefault(t *testing.T) {
	if _, ok := Registry()["question"]; !ok {
		t.Fatal("question missing from registry")
	}
	for _, name := range DefaultGrant() {
		if name == "question" {
			t.Fatal("question granted by default")
		}
	}
	set, err := BuildToolset([]string{"question"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err != nil || !set.Has("question") {
		t.Fatalf("cannot grant question: %v", err)
	}
}
