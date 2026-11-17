package protocol

// S34 — the trust bench's MCP subject: config truth (T3, mcp.status) and the
// operator-triggered probe (T4, mcp.check). A different subject from
// agent_roster.go's roster/grants (brief §3's "one handler file per
// subject"), and framed through the ONE forwarding shape every entry-point
// seam in this package uses — this file contains no MCP logic of its own,
// not a config parse, not a dial. internal/mcp is never imported here
// (internal/mcp/absent_test.go's banned list names this package
// explicitly); Table.MCP is a plain func, same deletability discipline as
// Agents/Plugins/AgentRoster/ToolGrants.

import (
	"context"
	"encoding/json"
)

// mcpCall builds the ONE handler both mcp.status and mcp.check share,
// dispatched by the method name bound at REGISTRATION time (never trusted
// from params) — the same pattern agentServicesCall uses for its seven
// methods.
func (t *Table) mcpCall(name MethodName) func(context.Context, *Session, json.RawMessage) (any, error) {
	return func(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
		if t.MCP == nil {
			return nil, ErrNotWired.WithWhat("MCP trust reads are not wired — the daemon started without the entry point's MCP seam (cmd/aeon did not wire Table.MCP; see docs/aeon/S34-trust-bench/tasks/T3.md and T4.md); `aeon serve` wires it, a hand-built Table does not")
		}
		result, refusal, err := t.MCP(ctx, string(name), params)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, ErrAgentRefused.WithWhat("%s", refusal)
		}
		return result, nil
	}
}
