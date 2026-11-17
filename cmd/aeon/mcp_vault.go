package main

// mcp_vault.go — where an MCP server's environment NAMES get their values.
//
// A server's config carries names only (never values). When the daemon starts
// one, each declared name is resolved through the variable manager first — the
// project's own value, then the global one — and then through the daemon's own
// environment. Nothing is injected that the server did not name: the manager
// answers a lookup, it does not push its contents anywhere.

import (
	"os"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

var (
	mcpVault      *vault.Vault
	mcpVaultStore *store.Store
)

// setMCPVault is called once, by serve, when the manager opened.
func setMCPVault(v *vault.Vault, st *store.Store) { mcpVault, mcpVaultStore = v, st }

// mcpGetenv is the lookup handed to mcp.Dial / mcp.Probe. workspaceRoot picks the
// project whose override applies ("" = global values only).
func mcpGetenv(workspaceRoot string) func(string) string {
	if mcpVault == nil {
		return os.Getenv
	}
	root := vault.RootFor(mcpVaultStore, workspaceRoot)
	return func(name string) string {
		if v, ok := mcpVault.Resolve(name, root); ok {
			return v
		}
		return os.Getenv(name)
	}
}
