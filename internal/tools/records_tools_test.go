package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNoCatalogToolIsDefaultGranted(t *testing.T) {
	// Registered is not granted (S11's rule, OL2's stake): the default grant
	// stays the three workspace file tools. A management catalog that arrived
	// granted-by-default would hand every naive step the power to file goals.
	grant := DefaultGrant()
	if len(grant) != 3 {
		t.Fatalf("DefaultGrant() is %v — S38 must not widen it", grant)
	}
	for _, g := range grant {
		if _, ok := recordTools()[g]; ok {
			t.Errorf("%s is a catalog tool AND default-granted — a grant leak", g)
		}
	}
	for name := range recordTools() {
		for _, g := range grant {
			if name == g {
				t.Errorf("catalog tool %s hides in the default grant", name)
			}
		}
	}
}

func TestUngrantedCatalogToolIsRefused(t *testing.T) {
	// The grant list decides, not the registry: goal_create exists, is not
	// granted, and calling it answers an error Result — never reaches the
	// injected Records door (the recorder below would catch any attempt).
	hit := false
	set, err := BuildToolset([]string{"read"}, ToolContext{WorkspaceRoot: t.TempDir(), Records: func(context.Context, string, json.RawMessage) (string, bool) {
		hit = true
		return "{}", false
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res := set.Call(context.Background(), "goal_create", json.RawMessage(`{"title":"x","type":"chore","components":[{"component":"a"}]}`))
	if !res.IsError {
		t.Fatal("an ungranted catalog tool ran — the grant list is decoration")
	}
	if hit {
		t.Fatal("the invoker was reached for an ungranted call")
	}
	// Granted, it reaches the door with the schema-shaped input untouched.
	set2, err := BuildToolset([]string{"goal_create"}, ToolContext{WorkspaceRoot: t.TempDir(), Records: func(_ context.Context, tool string, in json.RawMessage) (string, bool) {
		hit = true
		if tool != "goal_create" || !strings.Contains(string(in), `"title":"x"`) {
			t.Errorf("projection mangled the call: %s %s", tool, in)
		}
		return `{"goal":{"id":"goal-1"}}`, false
	}})
	if err != nil {
		t.Fatalf("build granted: %v", err)
	}
	res = set2.Call(context.Background(), "goal_create", json.RawMessage(`{"title":"x","type":"chore","components":[{"component":"a"}]}`))
	if res.IsError || !hit || !strings.Contains(res.Content, "goal-1") {
		t.Fatalf("granted call: %+v hit=%v", res, hit)
	}
	if res.Summary != "goal_create" {
		t.Errorf("summary %q, want the action name", res.Summary)
	}
}
