package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const sharedFixture = `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`

func TestSharingAServerDeclaresItForEveryAgentIncludingLaterOnesAndGrantsNothing(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, sharedFixture))
	policies := countQuery(t, st, `SELECT COUNT(*) FROM policy`)

	res, refusal, err := call("mcp.server.share", map[string]any{"name": "gitforge", "shared": true})
	if err != nil || refusal != "" {
		t.Fatalf("share: %v %s", err, refusal)
	}
	if !res.(protocol.MCPWriteResult).Changed {
		t.Fatal("sharing a server changes it")
	}
	for _, agent := range []string{"golang-engineer", "manny", "peer"} {
		if g := callGrants(t, st, agent); !strings.Contains(g, "gitforge.pr") || !strings.Contains(g, "unavailable") {
			t.Errorf("%s sees %s — a shared tool is declared and still unavailable until a policy allows it", agent, g)
		}
	}
	if got := countQuery(t, st, `SELECT COUNT(*) FROM policy`); got != policies {
		t.Fatalf("policy rows moved %d → %d — sharing is a declaration, never an allow", policies, got)
	}
	if got := countQuery(t, st, `SELECT COUNT(*) FROM mcp_declare`); got != 0 {
		t.Fatalf("%d mcp_declare rows — sharing stores one row, not one per agent", got)
	}

	// An agent created afterwards is covered without any further write.
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "latecomer", Name: "latecomer", Version: "1"}, "2026-11-02T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if g := callGrants(t, st, "latecomer"); !strings.Contains(g, "gitforge.pr") {
		t.Errorf("a later agent sees %s", g)
	}

	// The second share is a no-op with no audit row; the list says shared.
	before := auditCount(t, st, "grant_declare")
	res, _, _ = call("mcp.server.share", map[string]any{"name": "gitforge", "shared": true})
	if res.(protocol.MCPWriteResult).Changed || auditCount(t, st, "grant_declare") != before {
		t.Error("sharing twice changes nothing and writes no audit row")
	}
	list, _, _ := call("mcp.servers", map[string]any{})
	if !list.(protocol.MCPServersResult).Servers[0].Shared {
		t.Error("the server list says shared")
	}

	// Stopping puts everyone back to nothing.
	if _, refusal, err := call("mcp.server.share", map[string]any{"name": "gitforge", "shared": false}); err != nil || refusal != "" {
		t.Fatalf("unshare: %v %s", err, refusal)
	}
	if g := callGrants(t, st, "manny"); strings.Contains(g, "gitforge.pr") {
		t.Errorf("manny still sees %s after sharing stopped", g)
	}
}

func TestOnlyAPublicServerWithToolsCanBeShared(t *testing.T) {
	call, _ := writeFixture(t)
	if _, refusal, _ := call("mcp.server.add", map[string]any{"name": "mine", "tier": "private", "agent": "manny",
		"transport": map[string]any{"type": "stdio", "command": "x"},
		"tools":     []any{map[string]any{"name": "t", "rerun": "safe", "effect": "local"}}}); refusal != "" {
		t.Fatalf("add private: %s", refusal)
	}
	if _, refusal, _ := call("mcp.server.share", map[string]any{"name": "mine", "shared": true}); !strings.Contains(refusal, "private") {
		t.Errorf("refusal = %q, want a private-server sentence", refusal)
	}
	if _, _, err := call("mcp.server.share", map[string]any{"name": "nope", "shared": true}); err == nil {
		t.Error("an unknown server is an error that names the known ones")
	}
}
