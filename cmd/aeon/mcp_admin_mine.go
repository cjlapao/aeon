package main

// mcp_admin_mine.go — S46 T8 (MG8, MG9, MC2, 2026-11-02): the SCOPED half of
// the MCP admin seam, the five `mcp.mine.*` entries the daemon's records arms
// enter with self stamped through protocol.WithMCPActor (the import door's
// channel, reused — a STAMP, never a param).
//
// MC2 lives HERE, structurally. Every answer is built from the caller's own
// view: private rows are selected by the stamp, public servers qualify only
// through the stamp's own declarations, the grant view is the registry's
// per-agent view, and the status is the cached probe filtered to the same
// visible set. A peer's private server is not filtered out of an answer — it
// is never in the answer's INPUT. The refusal texts are built to leak
// nothing either: an invisible name and a name that never existed get the
// SAME sentence, so the doors cannot probe existence (no nearest-name hints
// across the boundary either — the view is the only source of names).
//
// The two proposes answer MineProposeReady {kind, payload}: visibility
// checked, payload canonical (self stamped into the payload's own agent
// field — the input cannot name anyone), validator run over the same bytes
// the create door and the commit door run (TD3). Writing the row belongs to
// the daemon's ONE inert-proposal path (caps, the stamped author, the shared
// store) — this file answers shape and refusal, and touches nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mineSelf resolves the agent stamp. A missing stamp or a PERSON stamp is
// refused with the pointer sentence — these entries exist for the daemon's
// stamped agent calls (the socket already turns a person session back at
// the records door; this is the seam's own defense-in-depth, so no caller
// can use the agent-scoped entries as a person-side oracle either).
func mineSelf(method string, ctx context.Context) (string, string, error) {
	kind, id, ok := protocol.MCPActor(ctx)
	if !ok || kind != "agent" || id == "" || id == "0" {
		return "", fmt.Sprintf("refused: %s is agent-scoped; this call carried no agent stamp — a person uses the unscoped mcp.* doors", method), nil
	}
	return id, "", nil
}

// mineVisibleServers is THE filter, once, for all five entries: the agent's
// own private servers, plus the public servers it declares tools from. No
// row else (another agent's private; a public server the agent declares
// nothing from) crosses into an answer's input.
func mineVisibleServers(all store.McpAll, self string) []store.McpServerRow {
	declaredFrom := map[string]bool{}
	for _, d := range all.Declares {
		if d.AgentSlug != self {
			continue
		}
		if server, _, ok := strings.Cut(d.Qualified, "."); ok && server != "" {
			declaredFrom[server] = true
		}
	}
	var out []store.McpServerRow
	for _, s := range all.Servers {
		if s.Tier == "private" {
			if s.AgentSlug != nil && *s.AgentSlug == self {
				out = append(out, s)
			}
			continue
		}
		if declaredFrom[s.Name] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mineJSONArray decodes a row's JSON-array column, nil-tolerant (an empty
// column is the empty list, never a fault in a read that only summarizes).
func mineJSONArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// --- mcp.mine.servers -----------------------------------------------------------

func mineServersRead(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	self, refusal, err := mineSelf("mcp.mine.servers", ctx)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	if err := json.Unmarshal(normalizeParams(params), &struct{}{}); err != nil {
		return nil, fmt.Sprintf("refused: mcp.mine.servers takes no params (%v) — the view IS the caller", err), nil
	}
	all, rerr := admin.st.McpRows(ctx)
	if rerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.servers: the rows could not be read (%w) — an own view is never answered from memory", rerr)
	}
	res := protocol.MineServersResult{Servers: []protocol.MineServer{}}
	for _, s := range mineVisibleServers(all, self) {
		ms := protocol.MineServer{
			Name:    s.Name,
			Tier:    s.Tier,
			Enabled: s.Enabled,
			Transport: protocol.MineTransport{
				Type:    s.TransportType,
				Command: s.Command,
				Args:    mineJSONArray(s.Args),
				URL:     s.URL,
				Env:     mineJSONArray(s.Env), // NAMES — the values were never rows (§4c, MCP's half)
			},
			Tools: []protocol.MineTool{},
		}
		tools := append([]store.McpToolRow(nil), s.Tools...)
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			ms.Tools = append(ms.Tools, protocol.MineTool{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
		}
		res.Servers = append(res.Servers, ms)
	}
	return res, "", nil
}

// --- mcp.mine.grants ------------------------------------------------------------

func mineGrantsRead(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	self, refusal, err := mineSelf("mcp.mine.grants", ctx)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	if err := json.Unmarshal(normalizeParams(params), &struct{}{}); err != nil {
		return nil, fmt.Sprintf("refused: mcp.mine.grants takes no params (%v) — the view IS the caller", err), nil
	}
	all, rerr := admin.st.McpRows(ctx)
	if rerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.grants: the rows could not be read (%w)", rerr)
	}
	// The SAME per-agent Registry view the person's mcp.grants answers and
	// T5's before/after carry (one view type, two doors) — the registry is
	// the one judge, and MG4's disabled-drop and MC5's grants-nothing are
	// already inside it.
	views, gerr := admin.grantViewsFor(ctx, all, []string{self})
	if gerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.grants: %w", gerr)
	}
	return protocol.MineGrantsResult{Agent: self, Grants: views[self]}, "", nil
}

// --- mcp.mine.status ------------------------------------------------------------

func mineStatusRead(ctx context.Context, admin *mcpAdmin, cache *mcpProbeCache, params json.RawMessage) (any, string, error) {
	self, refusal, err := mineSelf("mcp.mine.status", ctx)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	if err := json.Unmarshal(normalizeParams(params), &struct{}{}); err != nil {
		return nil, fmt.Sprintf("refused: mcp.mine.status takes no params (%v) — it answers from the cache for the caller alone", err), nil
	}
	all, rerr := admin.st.McpRows(ctx)
	if rerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.status: the rows could not be read (%w)", rerr)
	}
	// CACHE ONLY: snapshot reads the answers the person's probe door
	// produced; nothing here calls checkOrJoin, spawns a process, or opens a
	// connection (MG5's discipline over the tool door — proven by a spy
	// count in cmd/aeon's TestMcpStatusToolDoesNotProbe).
	snap := cache.snapshot()
	res := protocol.MineStatusResult{Servers: []protocol.MineStatus{}}
	for _, s := range mineVisibleServers(all, self) {
		sum, probed := snap[s.Name]
		if !probed {
			res.Servers = append(res.Servers, protocol.MineStatus{Server: s.Name, State: "unchecked"})
			continue
		}
		st := "down"
		if sum.OK {
			st = "ok"
		}
		res.Servers = append(res.Servers, protocol.MineStatus{Server: s.Name, State: st, Error: sum.Error, CheckedAt: sum.CheckedAt})
	}
	return res, "", nil
}

// --- the two proposes: visibility and shape, never the write ---------------------

// mineNoSuchServer is the ONE sentence for a name outside the agent's view —
// issued identically whether the name exists elsewhere (a peer's private, a
// public the agent declares nothing from) or nowhere at all. Existence in
// someone else's world is not this door's to confirm.
func mineNoSuchServer(name string) string {
	return fmt.Sprintf("refused: no such server %q — mcp_propose_remove_server removes a server THIS agent can see; see the view with mcp_servers", name)
}

// mineNoSuchGrant is the same discipline for the declared pair.
func mineNoSuchGrant(qualified string) string {
	return fmt.Sprintf("refused: no such grant %q — mcp_propose_revoke_grant revokes a pair THIS agent currently holds; see the grants with mcp_grants", qualified)
}

func mineProposeRemove(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	self, refusal, err := mineSelf("mcp.mine.proposeRemove", ctx)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	var p protocol.MineProposeRemoveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: mcp.mine.proposeRemove params are malformed (%v)", err), nil
	}
	all, rerr := admin.st.McpRows(ctx)
	if rerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.proposeRemove: the rows could not be read (%w)", rerr)
	}
	visible := false
	for _, s := range mineVisibleServers(all, self) {
		if s.Name == p.Server {
			visible = true
			break
		}
	}
	if !visible {
		return nil, mineNoSuchServer(p.Server), nil
	}
	// The proposal MEANS its revokes (T6's cascade rule): holders are
	// counted across the whole config, and the payload carries the cascade
	// the commit will audit one row at a time.
	holders := 0
	for _, d := range all.Declares {
		if server, _, ok := strings.Cut(d.Qualified, "."); ok && server == p.Server {
			holders++
		}
	}
	payload, merr := json.Marshal(mcpServerRemovePayload{
		Name:            p.Server,
		CascadeDeclares: holders > 0,
		Reason:          p.Reason,
	})
	if merr != nil {
		return nil, "", fmt.Errorf("mcp.mine.proposeRemove: the payload will not encode: %w", merr)
	}
	if verr := validateMcpServerRemoveProposal(payload); verr != nil {
		return nil, fmt.Sprintf("refused: %v", verr), nil
	}
	return protocol.MineProposeReady{Kind: string(desk.KindMCPServerRem), Payload: payload}, "", nil
}

func mineProposeRevoke(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	self, refusal, err := mineSelf("mcp.mine.proposeRevoke", ctx)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	var p protocol.MineProposeRevokeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: mcp.mine.proposeRevoke params are malformed (%v)", err), nil
	}
	if strings.Contains(p.Tool, ".") || strings.Contains(p.Server, ".") {
		return nil, "refused: mcp.mine.proposeRevoke takes the pair SPLIT — server and tool separately; a dot smuggled into either half is not a pair", nil
	}
	qualified := p.Server + "." + p.Tool
	all, rerr := admin.st.McpRows(ctx)
	if rerr != nil {
		return nil, "", fmt.Errorf("mcp.mine.proposeRevoke: the rows could not be read (%w)", rerr)
	}
	held := false
	for _, d := range all.Declares {
		if d.AgentSlug == self && d.Qualified == qualified {
			held = true
			break
		}
	}
	if !held {
		// Identical whether another agent holds it or nobody does — no
		// oracle for whose grants exist (MC2).
		return nil, mineNoSuchGrant(qualified), nil
	}
	// Agent is the STAMP, not the input: the payload can only ever carry
	// self, which is the whole of "no input here can name a different agent".
	payload, merr := json.Marshal(mcpGrantRevokePayload{
		Agent:     self,
		Qualified: qualified,
		Reason:    p.Reason,
	})
	if merr != nil {
		return nil, "", fmt.Errorf("mcp.mine.proposeRevoke: the payload will not encode: %w", merr)
	}
	if verr := validateMCPGrantRevokeProposal(payload); verr != nil {
		return nil, fmt.Sprintf("refused: %v", verr), nil
	}
	return protocol.MineProposeReady{Kind: string(desk.KindMCPGrantRevoke), Payload: payload}, "", nil
}
