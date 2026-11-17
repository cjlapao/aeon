package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// The rule the settings page writes for "every agent, every tool of a server"
// is an address pattern on all agents; it must make a declared tool available.
func TestAllAgentsAddressRuleMakesADeclaredToolAvailable(t *testing.T) {
	cfg := declaredConfig()
	r, _ := wireAlpha(t, &cfg)
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: "mcp.alpha.*.*", State: "allow"})
	rule := []policy.Policy{{
		ID: "mcp-alpha-all", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Allow, Spec: spec,
	}}
	if why := policy.ValidateToolPattern("mcp.alpha.*.*"); why != "" {
		t.Fatalf("the pattern is refused: %s", why)
	}
	v := grantViewOf(r.GrantView("agent-a", rule, time.Now()), "alpha.search")
	if v == nil || v.State != GrantAvailable {
		t.Fatalf("alpha.search = %+v, want available under the all-agents pattern rule", v)
	}
}
