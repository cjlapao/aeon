package main

// agent_transition_services.go — S35 T4: `agent.enable`/`agent.disable`/
// `agent.revoke` on the socket. Reality finding (confirmed this session,
// corrects brief §3/design.md TD5's "calls TransitionPluginAgent verbatim"):
// the CLI verbs do not call TransitionPluginAgent directly either — enable
// re-runs plugin.Rehydrate before moveVerb (a package that grew a request
// since import is caught before a verb trusts it), and revoke goes through
// plugin.Revoke, not the bare store transition. This seam calls the SAME
// functions the CLI calls (plugin.Enable/Disable/Revoke, agent.go's own
// moveVerb/cmdAgentRevoke bodies), so the Rehydrate re-check and whatever
// Revoke records beyond the bare transition travel through this door too —
// never TransitionPluginAgent one layer further down.
//
// Bigger reality finding, beyond what this task's own text flagged:
// agent.go's package header says plainly "NO MANAGEMENT ON THE NARROW
// SURFACE... never a socket method (R6)". That line predates this slice's
// own ruling (brief §3: enable/disable/revoke are move-state writes within
// already-bounded authority, not widen-authority writes — unlike import,
// which stays CLI/app-only and reachable from nowhere else). Updated that
// comment in the same commit as this file, narrowing R6 rather than
// contradicting it silently: import is still refused everywhere but here
// and the app; enable/disable/revoke are not.
//
// internal/plugin is banned from internal/protocol (absent_test.go's own
// stated invariant: the import is allowed in exactly {cmd/aeon,
// internal/plugin}), so this is a func seam — Table.AgentTransition,
// dispatched by method name, the same one-seam-many-methods shape
// Table.MCP already uses (S34 T3/T4) — not a ProposalStore-style local
// interface.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func wireAgentTransitionServices(st *store.Store, opts *daemon.Options) error {
	opts.AgentTransition = agentTransitionHandleFor(st)
	return nil
}

type agentTransitionResult struct {
	Agent string `json:"agent"`
	State string `json:"state"`
}

// agentTransitionHandleFor answers Table.AgentTransition, dispatched by
// method name — mcpHandleFor's own shape (mcp_services.go), checking
// nothing beyond the dispatch itself.
func agentTransitionHandleFor(st *store.Store) func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	return func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		switch protocol.MethodName(method) {
		case protocol.MethodAgentEnable:
			return agentEnable(st, params)
		case protocol.MethodAgentDisable:
			return agentDisable(st, params)
		case protocol.MethodAgentRevoke:
			return agentRevoke(st, params)
		default:
			return nil, fmt.Sprintf("refused: %q is not a method this seam answers", method), nil
		}
	}
}

type agentNameParams struct {
	Name string `json:"name"`
}

// agentEnable is cmdAgentMove's enable branch, JSON in place of flags: read
// the record, re-run Rehydrate on the re-read package (the re-check this
// slice's own investigation found and insisted travel through every door),
// then plugin.Enable. The returned *Agent can be nil even on success
// (plugin.move's own documented case — stopping/moving a package this
// process never held in memory), so the response always reads the state
// back from the store, exactly as cmdAgentMove does, never the returned
// value.
func agentEnable(st *store.Store, params json.RawMessage) (any, string, error) {
	var p agentNameParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: agent.enable params: %v", err), nil
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, "refused: agent.enable needs a non-empty name", nil
	}
	rec, found, err := st.PluginAgentState(name)
	if err != nil {
		return nil, "", fmt.Errorf("read the record for %s: %w", name, err)
	}
	if !found {
		return nil, fmt.Sprintf("refused: %s has no record — is it one of the first-party agents (they have no plugin state), or simply never imported? (agent.list)", name), nil
	}
	boundary := policy.NewStore(policyRepo(st))
	existing, rj := boundary.List(context.Background(), time.Now().UTC())
	if rj != nil {
		return nil, fmt.Sprintf("refused: the policy set could not be read (%s) — the Rehydrate check judges against the set in force, and inventing one would judge against nothing", rj.Reason), nil
	}
	if _, prj := plugin.Rehydrate(rec.Source, existing, time.Now().UTC()); prj != nil {
		return nil, prj.Error(), nil
	}
	if _, prj := plugin.Enable(name); prj != nil {
		return nil, prj.Error(), nil
	}
	return agentTransitionReadBack(st, name)
}

// agentDisable is cmdAgentMove's disable branch — no Rehydrate re-check,
// disabling never re-trusts a package, it only stops one.
func agentDisable(st *store.Store, params json.RawMessage) (any, string, error) {
	var p agentNameParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: agent.disable params: %v", err), nil
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, "refused: agent.disable needs a non-empty name", nil
	}
	if _, prj := plugin.Disable(name); prj != nil {
		return nil, prj.Error(), nil
	}
	return agentTransitionReadBack(st, name)
}

type agentRevokeParams struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// agentRevoke is cmdAgentRevoke's body — the reason is refused early with a
// clear message (UX), but TD5's real backstop is the store's own CHECK
// (migration 0023's reasonless-revoke constraint, unchanged): this early
// refusal is correct UX, never the only guard.
func agentRevoke(st *store.Store, params json.RawMessage) (any, string, error) {
	var p agentRevokeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: agent.revoke params: %v", err), nil
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, "refused: agent.revoke needs a non-empty name", nil
	}
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		return nil, "refused: revoke without a reason — the reason is REQUIRED and survives (AP4): who will read it? the operator who wonders next month why this agent stopped. Name the vendor, the audit, the incident", nil
	}
	if _, prj := plugin.Revoke(name, reason); prj != nil {
		return nil, prj.Error(), nil
	}
	return agentTransitionReadBack(st, name)
}

func agentTransitionReadBack(st *store.Store, name string) (any, string, error) {
	rec, found, err := st.PluginAgentState(name)
	if err != nil {
		return nil, "", fmt.Errorf("read %s's state back: %w", name, err)
	}
	state := "unknown"
	if found {
		state = rec.State
	}
	return agentTransitionResult{Agent: name, State: state}, "", nil
}
