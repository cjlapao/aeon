package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
)

// A custom agent joins dispatch: it is found, built from its row, and named alongside the shipped
// ones — while the shipped set is served exactly as before and always wins a name.
func TestCustomAgentJoinsDispatchAndTheShippedSetKeepsItsNames(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	actor := store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}
	mk := func(a store.Agent) {
		t.Helper()
		a.Version = "1"
		if err := st.CreateAgentAudited(context.Background(), actor, a, "2026-11-11T09:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	mk(store.Agent{Slug: "release-notes-writer", Name: "Release notes writer", Prompt: "You write release notes.", Tools: `["read","grep"]`})
	mk(store.Agent{Slug: "bad-tool", Name: "Bad tool", Prompt: "x", Tools: `["wrtie"]`})

	InstallAgentHandlerSource(st)
	t.Cleanup(func() { handler.ReplaceSource(handler.EmbeddedSource()) })

	h, ok := handler.Lookup("release-notes-writer")
	if !ok {
		t.Fatal("a custom agent must be dispatchable")
	}
	if h.Name != "release-notes-writer" || h.SystemPrompt != "You write release notes." || strings.Join(h.Tools, ",") != "read,grep" {
		t.Fatalf("the handler must come from the row, named by its slug: %+v", h)
	}
	names := handler.Names()
	if !hasName(names, "release-notes-writer") || !hasName(names, "default") {
		t.Fatalf("Names must hold the custom agent and the shipped ones: %v", names)
	}
	// A row that fails the guards is refused, not served: dispatch never runs a tool the registry lacks.
	if _, ok := handler.Lookup("bad-tool"); ok {
		t.Fatal("a row naming an unregistered tool must not be served")
	}
	// A shipped name is the shipped agent's, whatever a row says.
	if h, ok := handler.Lookup("default"); !ok || h.SystemPrompt == "" {
		t.Fatalf("the shipped set is served as before: %+v %v", h, ok)
	}
	if handler.Preset("release-notes-writer") {
		t.Fatal("a custom agent is not a preset")
	}
	// ToolsFor — what validation checks grants against — sees the custom agent too.
	if tools, ok := handler.ToolsFor("release-notes-writer"); !ok || len(tools) != 2 {
		t.Fatalf("ToolsFor must answer for a custom agent: %v %v", tools, ok)
	}
}

func hasName(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
