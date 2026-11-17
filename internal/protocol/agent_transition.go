package protocol

// agent_transition.go — S35 T4: agent.enable/disable/revoke forward to
// Table.AgentTransition, the same one-seam-many-methods shape mcpCall
// already uses (mcp_trust.go) — internal/plugin is banned from this
// package, so the real logic (plugin.Enable/Disable/Revoke, called the way
// agent.go's own moveVerb/cmdAgentRevoke call them) lives in cmd/aeon.

import (
	"context"
	"encoding/json"
)

func (t *Table) agentTransitionCall(name MethodName) func(context.Context, *Session, json.RawMessage) (any, error) {
	return func(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
		if t.AgentTransition == nil {
			return nil, ErrNotWired.WithWhat("agent transitions are not wired — the daemon started without the entry point's seam (cmd/aeon did not wire Table.AgentTransition; see docs/aeon/S35-trust-desk/tasks/T4.md); `aeon serve` wires it, a hand-built Table does not")
		}
		result, refusal, err := t.AgentTransition(ctx, string(name), params)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, ErrAgentRefused.WithWhat("%s", refusal)
		}
		return result, nil
	}
}
