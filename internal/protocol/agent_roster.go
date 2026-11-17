package protocol

// S34 T1 — the roster, as identity. A different subject from agent.go's S14
// agentsvc forwarding (that file's own header: "NO agent logic" beyond
// framing) and from agent.plugins' import ledger (MethodAgentPlugins's own
// doc comment in method.go): this one answers who EVERY agent is —
// first-party bodies and imported plugins alike — never what they may do
// (that is tools.grants, T2) and never the four plugin state verbs (R6:
// those stay CLI/app-only, unreachable from this socket).

import (
	"context"
	"encoding/json"
)

// The wire shape (name/provenance/state/source/importedAt, same REAL
// vocabulary documented in docs/aeon/S34-trust-bench/tasks/T1.md's Notes)
// lives in cmd/aeon/agent.go's agentRosterEntry/agentListResult — the same
// split agent.plugins already uses (pluginsWire/pluginsResult are cmd/aeon's
// alone; protocol/agent.go's agentPluginsCall knows no field names, only
// `any`). This file follows that precedent rather than design.md's original
// sketch of the split, so the wire shape has exactly one home, not two.

// handleAgentList answers `agent.list`. Like agentPluginsCall it checks
// NOTHING about agents itself — not even its own params: the "takes no
// params" refusal is cmd/aeon's agentRosterHandleFor's job (copied verbatim
// from pluginsHandleFor's own block — the one place that convention
// actually lives today, agentPluginsCall included), so there is exactly one
// place that wording can drift, not two. This file only frames the call,
// through a plain injected func so `rm -r internal/plugin` still leaves this
// package building (AS11's deletability, same discipline Plugins follows).
func (t *Table) handleAgentList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.AgentRoster == nil {
		return nil, ErrNotWired.WithWhat("the agent roster is not wired — the daemon started without the entry point's roster seam (cmd/aeon did not wire Table.AgentRoster); `aeon serve` wires it, a hand-built Table does not")
	}
	result, refusal, err := t.AgentRoster(ctx, params)
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		return nil, ErrAgentRefused.WithWhat("%s", refusal)
	}
	return result, nil
}

// handleToolsGrants answers `tools.grants`. Same framing discipline as
// handleAgentList: params decoding (the one real field, `agent`) and the
// unknown-agent check both live in cmd/aeon's toolGrantsHandleFor — this
// file holds no grant logic, only the seam.
func (t *Table) handleToolsGrants(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.ToolGrants == nil {
		return nil, ErrNotWired.WithWhat("tool grants are not wired — the daemon started without the entry point's grants seam (cmd/aeon did not wire Table.ToolGrants); `aeon serve` wires it, a hand-built Table does not")
	}
	result, refusal, err := t.ToolGrants(ctx, params)
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		return nil, ErrAgentRefused.WithWhat("%s", refusal)
	}
	return result, nil
}
