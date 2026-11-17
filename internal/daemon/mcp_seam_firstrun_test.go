package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/runtime"
)

// A first run arrives with the zero manifest. The MCP offer must be judged
// against the policy set in force, not against an empty set that withholds
// every tool for want of an allow rule.
func TestAFirstRunOffersMCPAgainstThePolicySetInForce(t *testing.T) {
	var got runtime.FrozenManifest
	runner := StepRunner{
		PolicyList: func(context.Context, time.Time) ([]runtime.PolicyEntry, error) {
			return []runtime.PolicyEntry{{ID: "mcp-gh-all", Type: "tool_access", Action: "allow", TargetKind: "all-agents"}}, nil
		},
		StepMCP: func(_ context.Context, _ string, frozen runtime.FrozenManifest, _ time.Time) (*MCPBundle, error) {
			got = frozen
			return nil, nil
		},
	}
	if _, _, err := runner.BuildTools(nil, t.TempDir(), "chat", runtime.FrozenManifest{}); err != nil {
		t.Fatal(err)
	}
	if !got.Frozen || len(got.Entries) != 1 || got.Entries[0].ID != "mcp-gh-all" {
		t.Fatalf("the offer was judged against %+v, want the set in force", got)
	}

	// A resume keeps the set its step froze; the live set is never consulted.
	frozen := runtime.FrozenManifest{Frozen: true, Entries: []runtime.PolicyEntry{{ID: "kept"}}}
	if _, _, err := runner.BuildTools(nil, t.TempDir(), "chat", frozen); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].ID != "kept" {
		t.Fatalf("a resumed step's offer used %+v, want its own frozen set", got)
	}
}
