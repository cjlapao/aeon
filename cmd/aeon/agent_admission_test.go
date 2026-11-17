package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// S37 T3 — the create-time preview (agentImportPreviewFor). It composes the
// three read-only steps against the policy set in force and renders the
// consent; it writes nothing. The tests below prove that: the preview fills
// its fields, refuses exactly what the CLI refuses (byte for byte), stores
// nothing, and registers nothing.

// TestPreviewReturnsConsentAndWritesNothing is the core (criterion 2): the
// preview fills agent/consent/carriedDeny, and writes nothing — the plugin
// ledger is empty and the policy list is unchanged (a carried denial is
// shown, not stored).
func TestPreviewReturnsConsentAndWritesNothing(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	dir := writeAgentPackage(t, "reviewer-plus", carriedFile("reviewer-plus"))
	before := mustPolicyList(t, s)

	got, refusal, err := agentImportPreviewFor(s)(context.Background(), dir)
	if err != nil {
		t.Fatalf("preview infra error: %v", err)
	}
	if refusal != "" {
		t.Fatalf("preview refused (it should not): %s", refusal)
	}
	if got.Agent != "reviewer-plus" {
		t.Fatalf("preview agent = %q, want reviewer-plus", got.Agent)
	}
	if len(got.Consent) == 0 {
		t.Fatal("the preview must carry the consent")
	}
	if len(got.CarriedDeny) != 1 {
		t.Fatalf("the preview must show the one carried denial, got %d", len(got.CarriedDeny))
	}
	// Writes nothing: the plugin ledger is empty.
	rows, err := s.PluginAgentsAll(true)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the preview must register nothing, got %d rows", len(rows))
	}
	// The policy list is unchanged (the carried denial is shown, not stored).
	after := mustPolicyList(t, s)
	if len(before) != len(after) {
		t.Fatalf("the preview must store no policy row: before %v after %v", before, after)
	}
}

// TestPreviewRefusesExactlyWhatImportRefuses is criterion 2: for each fixture,
// the preview's refusal string equals the CLI's error string, byte for byte.
// Run the preview first, the CLI second, on a fresh store each time.
func TestPreviewRefusesExactlyWhatImportRefuses(t *testing.T) {
	cases := []struct {
		name  string
		input func(t *testing.T, s *store.Store) string
	}{
		{"url", func(t *testing.T, s *store.Store) string { return "https://example.com/some-agent" }},
		{"missing dir", func(t *testing.T, s *store.Store) string { return filepath.Join(t.TempDir(), "absent") }},
		{"no card", func(t *testing.T, s *store.Store) string {
			dir := writeAgentPackage(t, "nocard", nil)
			if err := os.Remove(filepath.Join(dir, "card.json")); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"stray file", func(t *testing.T, s *store.Store) string {
			return writeAgentPackage(t, "stray", map[string][]byte{"payload.sh": []byte("echo hi")})
		}},
		{"exec bit", func(t *testing.T, s *store.Store) string {
			dir := writeAgentPackage(t, "execbit", nil)
			if err := os.Chmod(filepath.Join(dir, "agent.md"), 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"carried allow", func(t *testing.T, s *store.Store) string {
			return writeAgentPackage(t, "allower", map[string][]byte{"policies/allow.json": []byte(`{"id":"allow-allower","type":"tool_access","target":{"kind":"agent","id":"allower"},"action":"allow","spec":{"toolName":"webfetch"},"source":"operator"}`)})
		}},
		{"blocklisted", func(t *testing.T, s *store.Store) string {
			// Seed an operator Deny for the tool the package requests (read).
			now := time.Now().UTC()
			p := policy.Policy{ID: "deny-read", Type: "tool_access", Target: policy.Target{Kind: "all-agents", ID: ""}, Action: "deny", Spec: json.RawMessage(`{"toolName":"read","state":"deny"}`), Source: "operator"}
			if rj := policy.NewStore(policyRepo(s)).Add(context.Background(), p, now); rj != nil {
				t.Fatalf("seed deny: %s", rj.Reason)
			}
			return writeAgentPackage(t, "blocklisted", nil)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agentHome(t)
			s := openAgentStore(t)
			in := c.input(t, s)
			// Preview first.
			_, refusal, err := agentImportPreviewFor(s)(context.Background(), in)
			if err != nil {
				t.Fatalf("preview infra error: %v", err)
			}
			if refusal == "" {
				t.Fatalf("the preview should refuse %s", c.name)
			}
			// CLI second.
			_, cliErr := runAgent(t, "import", in)
			if cliErr == nil {
				t.Fatalf("the CLI should refuse %s", c.name)
			}
			if refusal != cliErr.Error() {
				t.Fatalf("refusal must equal the CLI error, byte for byte:\n  preview: %s\n  cli:     %s", refusal, cliErr.Error())
			}
		})
	}
}

// TestPreviewCarriedDenyIsEmptyArrayNotNull pins that the JSON marshals
// carriedDeny as [] not null when empty (a null would break the TS type).
func TestPreviewCarriedDenyIsEmptyArrayNotNull(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	dir := writeAgentPackage(t, "nopolicies", nil)
	got, refusal, err := agentImportPreviewFor(s)(context.Background(), dir)
	if err != nil || refusal != "" {
		t.Fatalf("preview: %v %s", err, refusal)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"carriedDeny":[]`) {
		t.Fatalf("carriedDeny must marshal as [] not null: %s", b)
	}
}

// TestPreviewDoesNotRegisterTheAgent pins the register-nothing half (criterion
// 5): a good preview leaves the agent unregistered.
func TestPreviewDoesNotRegisterTheAgent(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	dir := writeAgentPackage(t, "peeker", carriedFile("peeker"))
	got, refusal, err := agentImportPreviewFor(s)(context.Background(), dir)
	if err != nil || refusal != "" {
		t.Fatalf("preview: %v %s", err, refusal)
	}
	_ = got
	// The preview must not have registered the agent.
	rows, err := s.PluginAgentsAll(true)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a preview must not register the agent, got %d rows", len(rows))
	}
}
