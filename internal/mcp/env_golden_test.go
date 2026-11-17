package mcp

// env_golden_test.go — S59 T1 (characterization, P2 Phase A).
//
// The MCP stdio transport is the one spawn in the codebase that already
// does what S59's environment records generalise: the child gets PATH,
// HOME and the variables its config NAMED — nothing else. This test pins
// that boundary byte for byte, with a controlled `getenv` so the assertion
// is deterministic and needs no real credentials:
//
//   - PATH and HOME ride when present, and are OMITTED when absent
//     (they are conditional, not hardcoded),
//   - every name the config names rides with its value,
//   - a named variable whose value is EMPTY is omitted, not carried as
//     `NAME=`,
//   - nothing that is neither PATH, HOME nor named is visible — the
//     sentinel in the parent environment must not appear.
//
// T5 turns the host-scrubbed default into an allowlist of the same shape
// as this one; this test is the pin that says the MCP boundary already
// had it, so the change lands on a measured precedent rather than a claim.

import (
	"context"
	"slices"
	"testing"
)

// TestMCPEnvGolden — the exact environment a stdio MCP server child
// receives, read off the constructed transport before the first message
// (the environment is fixed at spawn and never changes after).
func TestMCPEnvGolden(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "PATH":
			return "/usr/bin"
		case "HOME":
			return "/home/s59-gold"
		case "AEON_S59_MCP_NAMED":
			return "s59-named-value"
		case "AEON_S59_MCP_SECRET":
			return "sk-s59-fake-not-real"
		case "AEON_S59_MCP_EMPTY":
			return ""
		}
		return ""
	}

	t.Run("named-vars-ride-others-stay-out", func(t *testing.T) {
		srv := ServerConfig{
			Name: "s59-golden",
			Transport: TransportConfig{
				Type:    TransportStdio,
				Command: fakeServerPath(t),
				Args:    []string{"-name", "s59-golden"},
				Env:     []string{"AEON_S59_MCP_NAMED", "AEON_S59_MCP_SECRET", "AEON_S59_MCP_EMPTY"},
			},
		}
		tr, err := newStdioTransport(context.Background(), srv, getenv, DialOptions{})
		if err != nil {
			t.Fatalf("the stdio transport did not spawn: %v", err)
		}
		killFake(t, tr)

		want := []string{
			"PATH=/usr/bin",
			"HOME=/home/s59-gold",
			"AEON_S59_MCP_NAMED=s59-named-value",
			"AEON_S59_MCP_SECRET=sk-s59-fake-not-real",
		}
		got := tr.cmd.Cmd.Env
		if !slices.Equal(got, want) {
			t.Fatalf("the MCP child environment drifted:\n  now:  %v\n  gold: %v", got, want)
		}
		for _, kv := range got {
			if kv == "AEON_S59_MCP_EMPTY=" {
				t.Fatal("an empty named variable was carried as NAME= — omission is the contract")
			}
		}
	})

	t.Run("path-and-home-are-conditional", func(t *testing.T) {
		getenv := func(key string) string {
			if key == "AEON_S59_MCP_NAMED" {
				return "s59-named-value"
			}
			return "" // no PATH, no HOME
		}
		srv := ServerConfig{
			Name: "s59-golden",
			Transport: TransportConfig{
				Type:    TransportStdio,
				Command: fakeServerPath(t),
				Args:    []string{"-name", "s59-golden"},
				Env:     []string{"AEON_S59_MCP_NAMED"},
			},
		}
		tr, err := newStdioTransport(context.Background(), srv, getenv, DialOptions{})
		if err != nil {
			t.Fatalf("the stdio transport did not spawn: %v", err)
		}
		killFake(t, tr)

		want := []string{"AEON_S59_MCP_NAMED=s59-named-value"}
		got := tr.cmd.Cmd.Env
		if !slices.Equal(got, want) {
			t.Fatalf("without PATH/HOME the child got:\n  now:  %v\n  gold: %v", got, want)
		}
	})
}

func killFake(t *testing.T, tr *stdioTransport) {
	t.Helper()
	t.Cleanup(func() {
		_ = tr.cmd.Cmd.Process.Kill()
		_ = tr.cmd.Cmd.Wait()
	})
}
