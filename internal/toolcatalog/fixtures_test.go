package toolcatalog_test

// fixtures_test.go — the world the differential tests decide about.
//
// Everything here is a fixture (AGENTS.md §5: no test points at a real store, a
// live provider or a real repository). The policy rows are built the way the
// store stores them — `toolName` AND `state` in the spec, `state` restating the
// row's action, because the evaluator calls a row whose two halves disagree
// *contradictory* and lets it decide nothing, and a fixture that got that wrong
// would silently test the inert path while believing it tested the verdict.
//
// The agents are the shapes the grant chain actually produces: system agents with
// differing allow-lists (one holds a shell, one holds nothing, one holds the
// alias spelling), and one custom agent that is not dispatchable (S43-extra SD7)
// — the case a panel must render honestly rather than flatter.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// At is the clock every fixture is decided at. A fixed instant, not `time.Now`: a
// fixture that expired on some night nobody tested is a fixture that reports a
// green suite and a broken product.
var At = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// lapsed is one hour before At — the instant an expired row stopped existing.
var lapsed = At.Add(-time.Hour)

// row builds one tool_access rule. `state` is written as the action restated, in
// the pinned shape `{"toolName": string, "state": "allow"|"deny"|"ask"}` — the
// evaluator reads both halves and refuses to choose between them.
func row(id, tool string, action policy.Action, kind policy.TargetKind, target string) policy.Policy {
	spec, err := json.Marshal(map[string]string{"toolName": tool, "state": string(action)})
	if err != nil {
		panic(err) // A two-string map cannot fail to marshal; a failure here is a typo in this file.
	}
	return policy.Policy{
		ID:     id,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: kind, ID: target},
		Action: action,
		Spec:   spec,
		Source: policy.Operator,
	}
}

// expiredRow is a row that stopped existing before At: D6 says it is gone, not
// denied, so the answer about its tool must read as though it was never written.
func expiredRow(id, tool string, action policy.Action, kind policy.TargetKind, target string) policy.Policy {
	p := row(id, tool, action, kind, target)
	p.NotAfter = &lapsed
	return p
}

// FixturePolicies is the set the differential test decides with, and every entry
// in it is there to make one specific mistake visible:
//
//   - `allow-read-atlas` — a narrow allow that should win where nothing outranks it.
//   - `deny-bash-all` — a broad deny that outranks any allow (PL4), so a catalog
//     that sorted by specificity instead of folding actions would say "allowed".
//   - `ask-grep-all` — an ask, which is neither yes nor no: on a step it suspends
//     for a human, and the sentence names the allow it looked past.
//   - `deny-read-review` — a lane-type row. `Effective`'s request carries no lane
//     (a catalog row is not scoped to a step's lane), so the engine lets it match
//     nothing — and the catalog must agree, not invent lane knowledge it does not
//     have. `PoliciesTouching` still lists it as a candidate, which is the honest
//     difference between "could speak about this tool" and "decided it".
//   - `expired-deny-read-atlas` — gone, not denied.
//   - `allow-write-file-reviewer` — an alias-authored row, which must decide
//     `write` exactly as it decides `write_file` (SEC-059), the case a name-blind
//     comparison (SEC-051) would silently deny.
//   - `allow-read-sentry-deny-later` and the two `todo` rows — a pair on a tool
//     nobody in the fixture holds, so the not-granted path is exercised with real
//     rows present: the catalog must not consult the engine for a tool the agent
//     does not hold, and so must not print a verdict it never asked for.
func FixturePolicies() []policy.Policy {
	return []policy.Policy{
		row("allow-read-atlas", "read", policy.Allow, policy.AgentKind, "atlas"),
		// A broad deny over a narrow allow: D1's visible half, where the refusal
		// names the allow it looked past. Without the allow under it, a catalog
		// that lost `OverriddenAllow` from the sentence would still look right.
		row("allow-bash-atlas", "bash", policy.Allow, policy.AgentKind, "atlas"),
		row("deny-bash-all", "bash", policy.Deny, policy.AllAgents, ""),
		row("ask-grep-all", "grep", policy.Ask, policy.AllAgents, ""),
		row("deny-read-review", "read", policy.Deny, policy.LaneTypeKind, "review"),
		expiredRow("expired-deny-read-atlas", "read", policy.Deny, policy.AgentKind, "atlas"),
		row("allow-write-file-reviewer", "write_file", policy.Allow, policy.AgentKind, "reviewer"),
		row("allow-todo-all", "todo", policy.Allow, policy.AllAgents, ""),
		expiredRow("expired-allow-todo-all", "todo", policy.Allow, policy.AllAgents, ""),
	}
}

// Agent is one fixture agent: the allow-list its row carries, whether it may be
// dispatched, and the lane it would run in (kept here for the tests that want to
// say *why* a lane-type row is inert, never read by `Effective`).
type Agent struct {
	Granted      []string
	Dispatchable bool
	LaneType     string
	Custom       bool
}

// FixtureAgents is the cast. The names are the honest shape of the tables: a
// system agent with a shell, a system agent with nothing, a system agent holding
// the alias spelling, and a custom agent whose row names tools it can never use.
func FixtureAgents() map[string]Agent {
	return map[string]Agent{
		"atlas":     {Granted: []string{"read", "grep", "bash", "webfetch", "task_list", "loop_get", "seed_status", "attach"}, Dispatchable: true, LaneType: "build"},
		"sentry":    {Granted: nil, Dispatchable: true, LaneType: "review"},
		"reviewer":  {Granted: []string{"read", "write", "write_file", "grep", "task_show", "brief_show"}, Dispatchable: true, LaneType: "review"},
		"cursitor":  {Granted: []string{"read", "bash"}, Dispatchable: false, LaneType: "build", Custom: true},
		"webproxy":  {Granted: []string{"webfetch", "read"}, Dispatchable: true, LaneType: "outward"},
		"nightly":   {Granted: []string{"read", "grep", "todo", "policy_list", "watchdog_list", "component_show"}, Dispatchable: true, LaneType: "build"},
		"ghost":     {Granted: []string{"read"}, Dispatchable: true, LaneType: "build"},
		"nobody-at": {Granted: nil, Dispatchable: true, LaneType: "build"},
	}
}

// FixtureDefaultGrant is the floor: what every step gets when nobody says
// otherwise. Deliberately not `tools.DefaultGrant()` — a fixture that called the
// production default would tie the two together and a change to one would silently
// change the other, which is what this package exists to keep apart.
func FixtureDefaultGrant() []string { return []string{"read", "list"} }

// FixtureInputs assembles the whole world into the one value `Effective` reads.
func FixtureInputs() toolcatalog.Inputs {
	agents := FixtureAgents()
	return toolcatalog.Inputs{
		DefaultGrant: FixtureDefaultGrant(),
		AgentGrants: func(agent string) ([]string, bool, bool) {
			a, ok := agents[agent]
			if !ok {
				return nil, false, false
			}
			return append([]string(nil), a.Granted...), a.Dispatchable, true
		},
		MCP:      MCPFixtureSet(),
		Policies: FixturePolicies(),
		At:       At,
	}
}

// MCPFixture is a stand-in for S12's authority view: the four states it can
// answer with, per agent and per qualified name, with no `internal/mcp` import
// anywhere near this file (MC11).
type MCPFixture struct {
	Held     map[string][]string        // agent → qualified names it may see
	States   map[string]MCPStateFixture // "agent|qualified" → state + reason
	DeclareS map[string]bool            // "agent|qualified" → declared
}

// MCPStateFixture is `MCPState` spelled locally so the fake cannot accidentally
// depend on the production type's zero value reading as a real state.
type MCPStateFixture struct {
	State  toolcatalog.MCPState
	Reason string
}

func (m MCPFixture) ToolsFor(agent string) []string { return m.Held[agent] }

func (m MCPFixture) State(agent, qualified string) (toolcatalog.MCPState, string, bool) {
	if _, ok := m.Held[agent]; !ok {
		return "", "", false // An agent the view knows nothing about has nothing in it (MC2).
	}
	k := agent + "|" + qualified
	s, ok := m.States[k]
	if !ok {
		return "", "", false
	}
	return s.State, s.Reason, true
}

func (m MCPFixture) Declares(agent, qualified string) bool {
	return m.DeclareS[agent+"|"+qualified]
}

// MCPFixtureSet is the MCP half of the cast: one server with three tools in the
// four states, plus one name the agent holds without a state (the view has
// nothing to say, which is not a refusal) and one name it does not hold at all.
func MCPFixtureSet() MCPFixture {
	return MCPFixture{
		Held: map[string][]string{
			"atlas":    {"gh.list_issues", "gh.create_issue", "gh.search_code", "gh.silent_tool"},
			"webproxy": {"gh.list_issues"},
		},
		States: map[string]MCPStateFixture{
			"atlas|gh.list_issues":    {State: toolcatalog.MCPAvailable, Reason: "core: judged by the step's S11 policy gate (S12 does not re-decide core)"},
			"atlas|gh.create_issue":   {State: toolcatalog.MCPRefused, Reason: "refused: agent:atlas · tool_access · gh.create_issue by rule 42"},
			"atlas|gh.search_code":    {State: toolcatalog.MCPAsk, Reason: "asked: agent:atlas · tool_access · gh.search_code by rule 43"},
			"atlas|gh.silent_tool":    {State: toolcatalog.MCPUnavailable, Reason: "no rule names this tool; nothing is permitted by default (PL5)"},
			"webproxy|gh.list_issues": {State: toolcatalog.MCPAvailable, Reason: "allowed: agent:webproxy · tool_access · gh.list_issues by rule 41"},
		},
		DeclareS: map[string]bool{
			"atlas|gh.list_issues":  true,
			"atlas|gh.create_issue": true,
			"atlas|gh.search_code":  true,
		},
	}
}

// rowFor is `row` with the testing.T in the signature, so a fixture that cannot
// be built fails at the call site that asked for it.
func rowFor(t *testing.T, id, tool string, action policy.Action, kind policy.TargetKind, target string) policy.Policy {
	t.Helper()
	return row(id, tool, action, kind, target)
}

// idsOf names a rule set the way a failure message should: by id, in order.
func idsOf(ps []policy.Policy) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// mcpReason is the fixture's own record of what the view said, so the 1:1 test
// compares against the source of truth and not against a copy made in the test.
func mcpReason(qualified, agent string) string {
	return MCPFixtureSet().States[agent+"|"+qualified].Reason
}
