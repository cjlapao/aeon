package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// S74 T2 through the REAL resolver: internal/tools installs it at init, so a
// wildcard row decides over the actual registry, with no address handed in.
func TestAWildcardRowDecidesOverTheRealRegistry(t *testing.T) {
	spec, _ := json.Marshal(map[string]string{"toolName": "workspace.*.read", "state": "allow"})
	row := policy.Policy{
		ID: "reads", Type: policy.ToolAccess, Action: policy.Allow,
		Target: policy.Target{Kind: policy.AgentKind, ID: "brainstormer"},
		Source: policy.Operator, Spec: spec,
	}
	at := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for tool, want := range map[string]bool{"glob": true, "read": true, "grep": true, "write": false, "bash": false, "webfetch": false} {
		v := policy.Evaluate([]policy.Policy{row}, policy.Request{Type: policy.ToolAccess, Agent: "brainstormer", Tool: tool}, at)
		if v.Allowed != want {
			t.Errorf("%s: allowed=%v, want %v", tool, v.Allowed, want)
		}
	}
}
