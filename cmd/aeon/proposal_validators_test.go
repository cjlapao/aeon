package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// The test's own name follows the task spec's wording
// (TestValidatorRegistryCoversExactlyFourKinds), though the real closed set
// is FIVE kinds (policy_add, policy_remove, seed_corpus, mcp_server_add,
// mcp_server_remove — store.ProposalKinds, migration 0042's CHECK) — the
// task doc's own count was off by one; the assertion below is against the
// real set, not the number in the name.
func TestValidatorRegistryCoversExactlyFourKinds(t *testing.T) {
	if len(proposalValidators) != len(store.ProposalKinds) {
		t.Fatalf("registry has %d entries, store.ProposalKinds has %d — they must match exactly", len(proposalValidators), len(store.ProposalKinds))
	}
	for _, k := range store.ProposalKinds {
		if _, ok := proposalValidators[k]; !ok {
			t.Errorf("kind %q is in the closed set but has no validator", k)
		}
	}
}

// writeConfig marshals cfg and writes it through this file's own
// writeMCPConfig helper (mcp_services_test.go), which already takes a raw
// JSON body — reused rather than redeclared.
func writeConfig(t *testing.T, cfg mcp.Config) string {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return writeMCPConfig(t, string(raw))
}

func TestMcpServerAddValidatesAgainstWouldBeConfig(t *testing.T) {
	writeConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{
			{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}},
		},
	})
	payload, err := json.Marshal(mcpServerAddPayload{
		Tier: "public",
		Server: mcp.ServerConfig{
			Name:      "github", // collides with the existing public server (MC12)
			Transport: mcp.TransportConfig{Type: "stdio", Command: "other-mcp"},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	err = validateMcpServerAddProposal(payload)
	if err == nil {
		t.Fatal("a candidate colliding with an existing server name must be refused")
	}
	// S46 T6 validator move (recorded deviation): the collision is now
	// caught by the candidate gate — Rows.With(AddServer)'s own named
	// refusal (MC8, "already exists in") fires before Assemble reaches
	// Validate's cross-tier MC12. Either sentence is THE validator
	// refusing; the test's claim (a colliding name must refuse, loudly,
	// by name) is unchanged.
	if !strings.Contains(err.Error(), "configured twice") && !strings.Contains(err.Error(), "already exists in") {
		t.Fatalf("expected the validator's own collision message (MC12 or MC8), got: %v", err)
	}
}

func TestMcpServerAddAcceptsAGoodCandidate(t *testing.T) {
	writeConfig(t, mcp.Config{})
	payload, err := json.Marshal(mcpServerAddPayload{
		Tier: "public",
		Server: mcp.ServerConfig{
			Name:      "github",
			Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := validateMcpServerAddProposal(payload); err != nil {
		t.Fatalf("a valid candidate must be accepted, got: %v", err)
	}
}

func TestMcpServerAddWorksWithNoExistingConfig(t *testing.T) {
	// MC11: no config file at all is legitimate — the candidate is one
	// server added to an otherwise-empty config, not a refusal.
	t.Setenv(paths.MCPConfigEnv, t.TempDir()+"/does-not-exist.json")
	payload, err := json.Marshal(mcpServerAddPayload{
		Tier: "public",
		Server: mcp.ServerConfig{
			Name:      "github",
			Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := validateMcpServerAddProposal(payload); err != nil {
		t.Fatalf("an absent config must not refuse a good candidate, got: %v", err)
	}
}

func TestMcpServerRemoveValidatesMembership(t *testing.T) {
	writeConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}}},
	})
	good, _ := json.Marshal(mcpServerRemovePayload{Name: "github"})
	if err := validateMcpServerRemoveProposal(good); err != nil {
		t.Fatalf("removing a server that exists must validate, got: %v", err)
	}
	bad, _ := json.Marshal(mcpServerRemovePayload{Name: "ghost"})
	if err := validateMcpServerRemoveProposal(bad); err == nil {
		t.Fatal("removing a server that does not exist must be refused")
	}
}

func scratchBoundary(t *testing.T) *policy.Store {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	boundary, closeFn, err := policyBoundary()
	if err != nil {
		t.Fatalf("open boundary: %v", err)
	}
	t.Cleanup(closeFn)
	return boundary
}

// TestPolicyAddValidatorMatchesCLI is test map #3's parity proof: the exact
// payload a `policy add` refuses is refused by the registry with the same
// reason. True by construction since boundary.Add now calls the same
// policy.ValidateForAdd this validator calls (T2's extraction) — the test
// still exists because that construction is exactly what a future edit to
// one call site and not the other would break silently.
func TestPolicyAddValidatorMatchesCLI(t *testing.T) {
	boundary := scratchBoundary(t) // PL1 needs no existing rows; the boundary proves the CLI's own refusal path
	bad := policy.Policy{
		ID:     "bad-allow",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow,
		Source: policy.Source("agent"), // PL1: only operator may author Allow
	}
	cliRej := boundary.Add(context.Background(), bad, time.Now())
	if cliRej == nil {
		t.Fatal("the CLI's own boundary.Add must refuse an agent-authored Allow (PL1)")
	}

	payload, err := json.Marshal(bad)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	regErr := validatePolicyAddProposal(payload)
	if regErr == nil {
		t.Fatal("the registry's policy_add validator must refuse the identical payload")
	}
	if regErr.Error() != cliRej.Reason {
		t.Fatalf("registry refusal %q does not match boundary.Add's refusal %q", regErr.Error(), cliRej.Reason)
	}
}

func TestSeedCorpusValidatesWhole(t *testing.T) {
	payload, err := json.Marshal(seedCorpusPayload{Agents: []string{"nonexistent-agent-xyz"}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := validateSeedCorpusProposal(payload); err == nil {
		t.Fatal("an agent the corpus does not carry must refuse the whole payload")
	} else if !strings.Contains(err.Error(), "nonexistent-agent-xyz") {
		t.Fatalf("refusal must name the bad agent, got: %v", err)
	}
}

func TestSeedCorpusRefusesEmptyAgentList(t *testing.T) {
	payload, err := json.Marshal(seedCorpusPayload{})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := validateSeedCorpusProposal(payload); err == nil {
		t.Fatal("seed_corpus with no agents must be refused (D4)")
	}
}

func TestPolicyRemoveValidatesMembership(t *testing.T) {
	boundary := scratchBoundary(t)
	bad, _ := json.Marshal(policyRemovePayload{ID: "ghost"})
	if err := validatePolicyRemoveProposal(bad); err == nil {
		t.Fatal("removing an id that does not exist must be refused")
	}
	p := policy.Policy{
		ID:     "real-one",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny,
		Source: policy.Source("agent"),
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	if rej := boundary.Add(context.Background(), p, time.Now()); rej != nil {
		t.Fatalf("seed a real row: %v", rej.Reason)
	}
	good, _ := json.Marshal(policyRemovePayload{ID: "real-one"})
	if err := validatePolicyRemoveProposal(good); err != nil {
		t.Fatalf("removing an id that exists must validate, got: %v", err)
	}
}
