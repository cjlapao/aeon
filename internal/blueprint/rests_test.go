package blueprint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// The two blueprints S39 T6 opts into the rest (CR1/CR3). The allow-list in
// chat_keys_test.go permits them; these tests assert WHY they are permitted
// — the keys are worn exactly as T6 gave them, and the conversation agents
// they name are real registered bodies.

func TestBrainstormRestsAfterItsRun(t *testing.T) {
	b := loadShipped(t, "brainstorm")
	if b.After != AfterConverse {
		t.Fatalf("brainstorm after=%q, want converse — the capture run must END in rest, not death (CR1)", b.After)
	}
	if b.Converse != "brainstormer" {
		t.Fatalf("brainstorm converse=%q, want brainstormer — the conversation that made the goal CONTINUES", b.Converse)
	}
	r := Analyze(mustShippedRaw(t, "brainstorm"), ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet, AgentTools: handler.ToolsFor})
	if _, invalid, _ := r.Counts(); invalid != 0 {
		t.Fatalf("the rested brainstorm carries %d invalid diagnostics: %+v", invalid, r.Diagnostics)
	}
}

func TestChatBlueprintValidatesAndRests(t *testing.T) {
	b := loadShipped(t, "chat")
	if b.After != AfterConverse || b.Converse != "chat" {
		t.Fatalf("chat wears after=%q converse=%q, want converse/chat (CR3)", b.After, b.Converse)
	}
	if len(b.Output) != 0 {
		t.Fatalf("a conversation is not a deliverable; output=%v", b.Output)
	}
	if len(b.Lanes) != 1 || b.Lanes[0].Step.Agent != "chat" || b.Lanes[0].Step.Kind != "model-step" {
		t.Fatalf("chat must be one model-step lane by the chat agent: %+v", b.Lanes)
	}
	r := Analyze(mustShippedRaw(t, "chat"), ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet, AgentTools: handler.ToolsFor})
	if _, invalid, _ := r.Counts(); invalid != 0 {
		t.Fatalf("chat carries %d invalid diagnostics: %+v", invalid, r.Diagnostics)
	}
}

func mustShippedRaw(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("blueprints", name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
