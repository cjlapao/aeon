package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// TestSeedValidatorSeamIsTheOneValidator is the MG10 wiring proof: the
// seam handed to the seed engine (mcpSeedValidator) reaches Config.Validate
// — the ONE validator — and its sentence arrives verbatim (MG4's law at
// the seed door). A store-legal row whose GRAMMAR the validator refuses
// must be refused by the seam with the validator's own words, never a
// reword, never silence.
func TestSeedValidatorSeamIsTheOneValidator(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	good := store.McpAll{Servers: []store.McpServerRow{{
		Name: "gh", Tier: "public", TransportType: "stdio", Command: "gh-mcp",
		Args: "[]", Env: `["GITHUB_TOKEN"]`, Enabled: false, Origin: "system",
		CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
		Tools: []store.McpToolRow{{Name: "list_issues", Rerun: "safe", Effect: "local"}},
	}}}
	if err := mcpSeedValidator(s)(ctx, good); err != nil {
		t.Fatalf("a valid candidate was refused: %v — the seam must pass what Config.Validate passes", err)
	}

	bad := good
	bad.Servers = append([]store.McpServerRow{}, good.Servers...)
	bad.Servers[0].Tools = []store.McpToolRow{{Name: "list_issues", Rerun: "sometimes", Effect: "local"}}
	err = mcpSeedValidator(s)(ctx, bad)
	if err == nil {
		t.Fatal("an invalid rerun class passed the seam — the validator was bypassed (MG10: the one validator gates every record)")
	}
	if !strings.Contains(err.Error(), "sometimes") || !strings.Contains(err.Error(), "safe") {
		t.Fatalf("the sentence must be the validator's own, naming the value it refused, got %q", err.Error())
	}
}
