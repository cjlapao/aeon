package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// writeServerShare turns "every agent declares this server's tools" on or off.
// It is a declaration, never a permission: the tools stay unavailable until a
// policy allow exists, the same as a declaration made for one agent (MC5).
func (a *mcpAdmin) writeServerShare(ctx context.Context, p protocol.MCPServerShareParams, kind, id string) (any, string, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	var cur *store.McpServerRow
	for i := range all.Servers {
		if all.Servers[i].Name == p.Name {
			cur = &all.Servers[i]
		}
	}
	if cur == nil {
		return nil, "", unknownServerError(a, ctx, p.Name)
	}
	if cur.Tier != "public" {
		return nil, fmt.Sprintf("refused: server %q is private — it belongs to one agent and has no one to share with", p.Name), nil
	}
	if p.Shared && len(cur.Tools) == 0 {
		return nil, fmt.Sprintf("refused: server %q has no tools to share", p.Name), nil
	}
	already := false
	for _, n := range all.Shared {
		if n == p.Name {
			already = true
		}
	}
	agents, err := a.st.AgentsAll()
	if err != nil {
		return nil, "", err
	}
	var affected []string
	for _, ag := range agents {
		if key, ok := a.mapKey(ag.Slug); ok {
			affected = append(affected, key)
		}
	}
	sort.Strings(affected)
	name, shared := p.Name, p.Shared
	summary := fmt.Sprintf("shared %s with every agent (a declaration — the policy still decides use)", name)
	op := "grant_declare"
	if !shared {
		summary, op = fmt.Sprintf("stopped sharing %s with every agent", name), "grant_revoke"
	}
	spec := writeSpec{
		affected:  affected,
		serverRow: ptrStr(name),
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if already == shared {
				return false, nil
			}
			return store.McpShareTx(ctx, tx, name, shared)
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{Op: op, Server: ptrStr(name), Qualified: ptrStr(name + ".*"), Summary: summary}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}
