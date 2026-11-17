package mcp

// address.go: an MCP tool's S74 policy address, `mcp.<server>.<tool>.<access>`.
//
// A tool is a read only when it is classified, its effect is local and it is
// safe to re-run: the server's readOnlyHint raises the declared classes
// upward only (classify.go), so a server that does not say "read-only" never
// lands here. Anything undeclared is a write — absent is not safe (P31).

import "github.com/cjlapao/aeon/internal/tools"

// ToolAddress is the policy address of one MCP tool as an agent's view reports
// it. A core tool has no MCP address and gets "".
func ToolAddress(t Tool) string {
	if t.Tier == TierCore || t.Server == "" {
		return ""
	}
	access := tools.AccessWrite
	if t.Effect == EffectClassLocal && t.Rerun == tools.RerunSafe {
		access = tools.AccessRead
	}
	return tools.MCPAddress(t.Server, t.Name, access)
}

// addressFor finds the qualified tool in the agent's view and returns its
// address, or "" when the agent's view does not hold it.
func (r *Registry) addressFor(agent, qualified string) string {
	for _, t := range r.ToolsFor(agent) {
		if t.Qualified == qualified {
			return ToolAddress(t)
		}
	}
	return ""
}

// QualifiedAddress is the policy address of the configured tool named
// `server.tool`, or "" when the configuration holds no such tool. The step's
// gate asks with only a tool name; this is how an address pattern such as
// `mcp.github.*.*` gets something to match, the same address the grant view
// reports (ToolAddress), derived from the same classes.
func QualifiedAddress(cfg Config, qualified string) string {
	server, tool, ok := splitQualified(qualified)
	if !ok {
		return ""
	}
	find := func(s ServerConfig, tier Tier) string {
		if s.Name != server {
			return ""
		}
		tc, found := findTool(s, tool)
		if !found || !classified(tc) {
			return ""
		}
		return ToolAddress(externalTool(tier, s.Name, tc))
	}
	for _, s := range cfg.Public {
		if a := find(s, TierPublic); a != "" {
			return a
		}
	}
	for _, ac := range cfg.Agents {
		for _, s := range ac.Private {
			if a := find(s, TierPrivate); a != "" {
				return a
			}
		}
	}
	return ""
}
