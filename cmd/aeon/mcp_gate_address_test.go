package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// The step's gate asks with only the tool's name. An all-agents address
// pattern on an MCP server must still decide that call.
func TestTheGateResolvesAnMCPToolsAddressSoAPatternRuleDecidesIt(t *testing.T) {
	_, _ = writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, sharedFixture))
	tools.SetExternalToolAddress(mcpQualifiedAddress)
	t.Cleanup(func() { tools.SetExternalToolAddress(nil) })

	if got := mcpQualifiedAddress("gitforge.pr"); got != "mcp.gitforge.pr.read" {
		t.Fatalf("address = %q, want mcp.gitforge.pr.read", got)
	}
	if got := mcpQualifiedAddress("gitforge.nope"); got != "" {
		t.Fatalf("an unconfigured tool has no address, got %q", got)
	}
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: "mcp.gitforge.*.*", State: "allow"})
	rule := []policy.Policy{{ID: "mcp-gitforge-all", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Allow, Spec: spec}}
	v := policy.Evaluate(rule, policy.Request{Type: policy.ToolAccess, Agent: "chat", Tool: "gitforge.pr"}, time.Now())
	if !v.Allowed || v.DefaultDeny {
		t.Fatalf("verdict = %+v, want allowed by the pattern rule", v)
	}
}
