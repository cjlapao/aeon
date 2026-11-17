package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

// An MCP server's env NAMES resolve through the variable manager first, then the
// daemon's environment — and a name the manager does not hold is untouched.
func TestAnMCPServersEnvNamesResolveThroughTheManagerThenTheDaemonEnvironment(t *testing.T) {
	_, st := writeFixture(t)
	v, err := vault.Open(st, filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	val := "from-the-vault"
	if _, err := v.Set(context.Background(), vault.SetInput{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: &val}, store.Actor{Kind: "user", ID: "1000"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "from-the-daemon-env")
	t.Setenv("ONLY_IN_THE_ENV", "daemon-value")

	prevV, prevS := mcpVault, mcpVaultStore
	t.Cleanup(func() { mcpVault, mcpVaultStore = prevV, prevS })
	setMCPVault(v, st)

	getenv := mcpGetenv("")
	if got := getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); got != "from-the-vault" {
		t.Fatalf("the manager wins over the daemon's own environment: %q", got)
	}
	if got := getenv("ONLY_IN_THE_ENV"); got != "daemon-value" {
		t.Fatalf("a name the manager does not hold keeps the old behaviour: %q", got)
	}
	setMCPVault(nil, nil)
	if got := mcpGetenv("")("GITHUB_PERSONAL_ACCESS_TOKEN"); got != "from-the-daemon-env" {
		t.Fatalf("without a manager nothing changes: %q", got)
	}
}
