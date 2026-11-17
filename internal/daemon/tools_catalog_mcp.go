// The MCP half of the catalog read (S47 T5, MC11): the entry point's seam is
// the only way in, a nil seam means the build has no MCP half at all, and the
// per-request memo below is not a cache — nothing survives the call.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// --- the MCP half --------------------------------------------------------------

// enabledServers reads the unscoped `mcp.servers` through the entry point's seam
// and keeps the enabled ones. This is the PERSON'S read (S46 T3), which is why the
// catalog may list a peer's server at all; `enabled` is the boundary for a
// CALLABLE tool, because a tool of a disabled server cannot be called by anyone
// and listing it as available would advertise a call that can only fail.
//
// A refusal from the seam is an ERROR here, not an empty MCP half: the half exists
// in this build (the seam is wired), so reporting it empty would be V11's
// fabrication.
func (sc *toolsCatalogService) enabledServers(ctx context.Context) ([]protocol.MCPServerView, error) {
	raw, refusal, err := sc.mcpAdmin(ctx, "mcp.servers", json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("tools catalog: the MCP servers could not be read: %w", err)
	}
	if refusal != "" {
		return nil, fmt.Errorf("tools catalog: the MCP management read refused: %s", refusal)
	}
	res, ok := raw.(protocol.MCPServersResult)
	if !ok {
		return nil, fmt.Errorf("tools catalog: the MCP seam answered %T where the servers list belongs", raw)
	}
	out := make([]protocol.MCPServerView, 0, len(res.Servers))
	for _, s := range res.Servers {
		if !s.Enabled {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// mcpGrantsView is `toolcatalog.MCPView` over S46's scoped `mcp.grants` read.
//
// Why through the seam and not the rows: MC11 — `internal/mcp` is importable only
// by `cmd/aeon`, and the authority view (S12's `GrantView`, whose `Reason` is a
// verdict sentence) lives there. Reading `mcp_declare` directly would give this
// file the declarations and not the DECISION, and the decision is the half the
// catalog needs.
//
// `memo` is per-request, not per-process (N8): one read per agent per call,
// discarded with the call. The first read failure is recorded in `err` and every
// door checks it, because the interface's signatures cannot report failure — and a
// view that quietly said "nothing to say" after a failed read would render held
// MCP tools as not-held, a fabrication wearing a plausible face.
type mcpGrantsView struct {
	sc   *toolsCatalogService
	ctx  context.Context
	memo map[string]*protocol.MCPGrantsResult
	err  error
}

func (v *mcpGrantsView) grants(agent string) *protocol.MCPGrantsResult {
	if v.err != nil {
		return nil
	}
	if g, seen := v.memo[agent]; seen {
		return g
	}
	body, err := json.Marshal(map[string]string{"agent": agent})
	if err != nil {
		v.err = fmt.Errorf("tools catalog: the MCP grants read could not be asked for %q: %w", agent, err)
		return nil
	}
	raw, refusal, err := v.sc.mcpAdmin(v.ctx, "mcp.grants", body)
	if err != nil {
		v.err = fmt.Errorf("tools catalog: the MCP grants for %q could not be read: %w", agent, err)
		return nil
	}
	if refusal != "" {
		v.err = fmt.Errorf("tools catalog: the MCP grants read refused for %q: %s", agent, refusal)
		return nil
	}
	res, ok := raw.(protocol.MCPGrantsResult)
	if !ok {
		v.err = fmt.Errorf("tools catalog: the MCP seam answered %T where the grants for %q belong", raw, agent)
		return nil
	}
	v.memo[agent] = &res
	return &res
}

// ToolsFor lists the qualified names this agent may see. They come from
// `GrantView`, so a peer's private server is invisible here exactly as at call
// time (MC2) — including in an error, which is why no door in this file can
// enumerate another agent's private tools by asking for them.
func (v *mcpGrantsView) ToolsFor(agent string) []string {
	g := v.grants(agent)
	if g == nil {
		return nil
	}
	out := make([]string, 0, len(g.Grants))
	for _, gr := range g.Grants {
		out = append(out, gr.Qualified)
	}
	return out
}

// State maps S12's four authority words onto the catalog's four (T4's
// `TestMCPStatesMapOneToOne` pins the mapping). An unrecognised word is not-held
// rather than a guess, and the reason is `GrantView.Reason` verbatim — a verdict
// sentence or the effect-ask sentence, never a paraphrase (this package holds no
// second formatter).
func (v *mcpGrantsView) State(agent, qualified string) (toolcatalog.MCPState, string, bool) {
	g := v.grants(agent)
	if g == nil {
		return "", "", false
	}
	for _, gr := range g.Grants {
		if gr.Qualified != qualified {
			continue
		}
		st := toolcatalog.MCPState(gr.State)
		switch st {
		case toolcatalog.MCPAvailable, toolcatalog.MCPAsk, toolcatalog.MCPUnavailable, toolcatalog.MCPRefused:
			return st, gr.Reason, true
		}
		return "", "", false
	}
	return "", "", false
}

// Declares reports a `mcp_declare` row behind this agent for that name. Declaring
// grants nothing (MC5) — it is a source in `GrantedBy`, not a verdict.
func (v *mcpGrantsView) Declares(agent, qualified string) bool {
	g := v.grants(agent)
	if g == nil {
		return false
	}
	for _, d := range g.Declares {
		if d == qualified {
			return true
		}
	}
	return false
}

// checkView is each door's guard against the failure the view's signatures cannot
// report.
func checkView(w *catalogWorld) error {
	if w.view == nil {
		return nil
	}
	return w.view.err
}
