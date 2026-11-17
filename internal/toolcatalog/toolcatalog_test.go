package toolcatalog_test

// toolcatalog_test.go — the differential tests: the catalog, the policy
// evaluator, and the tool builder, made to answer the same questions
// independently and compared. A catalog that merely *looks* consistent with the
// gate is the thing S47 refuses to ship (TC5).

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
)

// TestEffectiveEqualsTheGateForEveryPair is the headline. For every (registry
// tool × fixture agent) the catalog's state must equal what `policy.Evaluate`
// decides for the identical Request — computed here a second time, from the raw
// fixture lists and a fresh `Evaluate` call, and NOT by calling back into the
// package under test. The independent half is the point: a test that reused
// `Effective`'s own helpers would pass while both were wrong.
func TestEffectiveEqualsTheGateForEveryPair(t *testing.T) {
	in := FixtureInputs()
	agents := FixtureAgents()
	toolNames := tools.ToolNames()
	if len(toolNames) == 0 {
		t.Fatal("the registry is empty — the differential would prove nothing")
	}
	grantedPairs, decidedPairs := 0, 0
	for agent := range agents {
		for _, tool := range toolNames {
			got := toolcatalog.Effective(tool, agent, in)

			// The independent recomputation of "is it held": the default grant
			// plus the agent's own list, by name, exactly as the row says it.
			held := false
			for _, n := range in.DefaultGrant {
				if n == tool {
					held = true
				}
			}
			for _, n := range agents[agent].Granted {
				if n == tool {
					held = true
				}
			}
			// The independent verdict: the same Request the gate builds, the same
			// set, the same instant.
			v := policy.Evaluate(in.Policies, policy.Request{
				Type:  policy.ToolAccess,
				Agent: agent,
				Tool:  tool,
			}, in.At)

			var want toolcatalog.State
			switch {
			case !held || !agents[agent].Dispatchable:
				want = toolcatalog.StateNotGranted
			case v.Allowed:
				want = toolcatalog.StateGrantedAllowed
			case v.Asked:
				want = toolcatalog.StateGrantedAsk
			case v.DefaultDeny, v.Unimplemented:
				want = toolcatalog.StateGrantedNoRule
			default:
				want = toolcatalog.StateGrantedDenied
			}

			if got.State != want {
				t.Errorf("Effective(%s,%s) = %s, the gate says %s (verdict allowed=%v asked=%v default-deny=%v, held=%v, dispatchable=%v)",
					tool, agent, got.State, want, v.Allowed, v.Asked, v.DefaultDeny, held, agents[agent].Dispatchable)
			}
			if want == toolcatalog.StateNotGranted {
				if got.Reason != "" {
					t.Errorf("Effective(%s,%s) invented a reason for a tool the agent does not hold: %q", tool, agent, got.Reason)
				}
				continue
			}
			grantedPairs++
			if got.Reason != v.Sentence() {
				t.Errorf("Effective(%s,%s) reason is not the verdict sentence verbatim:\n got: %q\nwant: %q",
					tool, agent, got.Reason, v.Sentence())
			}
			if got.Reason != "" {
				decidedPairs++
			}
			// A granted pair must name at least one source: "granted" with nobody
			// having granted it is a state with no provenance.
			if len(got.GrantedBy) == 0 {
				t.Errorf("Effective(%s,%s) = %s with no GrantedBy", tool, agent, got.State)
			}
			for _, s := range got.GrantedBy {
				if s != toolcatalog.GrantedDefault && s != toolcatalog.GrantedHandler {
					t.Errorf("Effective(%s,%s) names source %q for a system tool — only the MCP half may say mcp-declare",
						tool, agent, s)
				}
			}
			if got.Dispatchable != agents[agent].Dispatchable {
				t.Errorf("Effective(%s,%s) reports dispatchable=%v, the agent's row says %v", tool, agent, got.Dispatchable, agents[agent].Dispatchable)
			}
		}
	}
	if grantedPairs == 0 || decidedPairs == 0 {
		t.Fatalf("the fixture exercised no granted (%d) or no decided (%d) pair", grantedPairs, decidedPairs)
	}
	t.Logf("%d granted pairs compared with the evaluator, %d of them carrying a verdict", grantedPairs, decidedPairs)
}

// TestGrantedSetEqualsWhatBuildToolsetBuilds is TC5's other half: the catalog and
// the builder share the grant function, so the names they agree on are the names
// `BuildToolset` itself records as held — including duplicates, alias spellings
// and order, and the identical refusal for an unknown name.
func TestGrantedSetEqualsWhatBuildToolsetBuilds(t *testing.T) {
	root := t.TempDir()
	agents := FixtureAgents()
	for agent, a := range agents {
		list := append(append([]string(nil), FixtureDefaultGrant()...), a.Granted...)
		names, err := tools.GrantedSet(list)
		if err != nil {
			t.Fatalf("%s: GrantedSet refused the fixture's own list: %v", agent, err)
		}
		set, err := tools.BuildToolset(list, tools.ToolContext{WorkspaceRoot: root})
		if err != nil {
			t.Fatalf("%s: BuildToolset refused the fixture's own list: %v", agent, err)
		}
		if strings.Join(names, ",") != strings.Join(set.Granted(), ",") {
			t.Errorf("%s: GrantedSet says %v, the toolset holds %v — the catalog and the builder have drifted",
				agent, names, set.Granted())
		}
		for _, n := range names {
			if !set.Has(n) {
				t.Errorf("%s: %s is granted by the shared function and the builder cannot call it", agent, n)
			}
		}
	}
	// And the refusal is one sentence in two doors, not two sentences.
	_, errA := tools.GrantedSet([]string{"read", "no_such_tool_xyz"})
	_, errB := tools.BuildToolset([]string{"read", "no_such_tool_xyz"}, tools.ToolContext{WorkspaceRoot: root})
	if errA == nil || errB == nil {
		t.Fatal("an unknown grant name was accepted")
	}
	if errA.Error() != errB.Error() {
		t.Errorf("the catalog's refusal differs from the builder's:\n%q\n%q", errA, errB)
	}
	if !strings.Contains(errA.Error(), "no_such_tool_xyz") || strings.Contains(errA.Error(), "nope") {
		t.Errorf("the refusal does not name exactly what it refused: %v", errA)
	}
}

// TestReasonIsTheVerdictSentenceVerbatim is the anti-second-formatter guard
// (PL): the catalog quotes the engine, byte for byte, on every pair where the
// engine spoke.
func TestReasonIsTheVerdictSentenceVerbatim(t *testing.T) {
	in := FixtureInputs()
	cases := []struct{ tool, agent string }{
		{"read", "atlas"},   // the allow that holds
		{"bash", "atlas"},   // the deny that outranks it
		{"grep", "atlas"},   // the ask
		{"read", "sentry"},  // held by the default, no rule at all
		{"list", "atlas"},   // default grant, no rule
		{"todo", "nightly"}, // the all-agents allow
	}
	sawDefaultDeny, sawDeny, sawAsk, sawAllow := 0, 0, 0, 0
	for _, c := range cases {
		e := toolcatalog.Effective(c.tool, c.agent, in)
		if e.State == toolcatalog.StateNotGranted {
			t.Fatalf("%s/%s is not granted in the fixture — the case list no longer matches it", c.tool, c.agent)
		}
		v := policy.Evaluate(in.Policies, policy.Request{Type: policy.ToolAccess, Agent: c.agent, Tool: c.tool}, in.At)
		if e.Reason != v.Sentence() {
			t.Errorf("%s/%s: reason is not verbatim:\n got: %q\nwant: %q", c.tool, c.agent, e.Reason, v.Sentence())
		}
		if !strings.HasPrefix(e.Reason, "allowed: ") && !strings.HasPrefix(e.Reason, "refused: ") &&
			!strings.HasPrefix(e.Reason, "asked: ") {
			t.Errorf("%s/%s: %q is not a sentence the engine's own formatter produces", c.tool, c.agent, e.Reason)
		}
		switch e.State {
		case toolcatalog.StateGrantedNoRule:
			sawDefaultDeny++
		case toolcatalog.StateGrantedDenied:
			sawDeny++
			if v.OverriddenAllow == nil {
				t.Errorf("%s/%s: a deny that outranked no allow — the case does not exercise D1's visible half", c.tool, c.agent)
			}
		case toolcatalog.StateGrantedAsk:
			sawAsk++
		case toolcatalog.StateGrantedAllowed:
			sawAllow++
		}
	}
	if sawDefaultDeny == 0 || sawDeny == 0 || sawAsk == 0 || sawAllow == 0 {
		t.Errorf("the cases covered no_rule=%d denied=%d ask=%d allowed=%d — one shape is untested",
			sawDefaultDeny, sawDeny, sawAsk, sawAllow)
	}
}

// TestNotGrantedHasNoVerdict: an agent that does not hold a tool gets
// `not_granted` and NO sentence. A fabricated reason would read as the engine
// having decided something it was never asked about — the difference between a
// catalog and a rumor.
func TestNotGrantedHasNoVerdict(t *testing.T) {
	in := FixtureInputs()
	// `attach` is in the registry and in nobody's list; `no-such-agent` is nobody.
	e := toolcatalog.Effective("attach", "sentry", in)
	if e.State != toolcatalog.StateNotGranted || e.Reason != "" || len(e.GrantedBy) != 0 {
		t.Errorf("a tool nobody holds came back %+v", e)
	}
	for _, agent := range []string{"no-such-agent", ""} {
		e := toolcatalog.Effective("read", agent, in)
		if e.State != toolcatalog.StateNotGranted || e.Reason != "" {
			t.Errorf("agent %q: %+v — an answer was invented about an agent that does not exist", agent, e)
		}
	}
	// A rule about a tool the agent does not hold must not leak its verdict in.
	if !contains(in.Policies, func(p policy.Policy) bool { return p.ID == "deny-bash-all" }) {
		t.Fatal("the fixture lost its all-agents deny")
	}
	e = toolcatalog.Effective("bash", "sentry", in) // sentry holds no bash
	if e.State != toolcatalog.StateNotGranted || e.Reason != "" {
		t.Errorf("the all-agents bash deny leaked a verdict to an agent that does not hold bash: %+v", e)
	}
	// And the same tool for an agent that DOES hold it gets the sentence.
	e = toolcatalog.Effective("bash", "atlas", in)
	if e.State != toolcatalog.StateGrantedDenied || e.Reason == "" {
		t.Fatalf("atlas's bash: %+v", e)
	}
}

// TestAliasAndCanonicalNamesAnswerAlike is SEC-051's name-blind-deny case seen
// from the catalog: a row written `write_file` and a question asked as `write`
// are the same tool, and the panel must not say one and refuse the other.
func TestAliasAndCanonicalNamesAnswerAlike(t *testing.T) {
	in := FixtureInputs()
	// reviewer holds both spellings; the fixture has an allow row written
	// `write_file`. Every spelling must reach it.
	asked := []string{"write", "write_file"}
	var reasons []string
	for _, name := range asked {
		e := toolcatalog.Effective(name, "reviewer", in)
		if e.State != toolcatalog.StateGrantedAllowed {
			t.Errorf("%s for reviewer: %s (%q) — the alias-written allow should decide both spellings", name, e.State, e.Reason)
		}
		reasons = append(reasons, e.Reason)
	}
	if reasons[0] != reasons[1] {
		t.Errorf("the two spellings got two sentences:\n%q\n%q", reasons[0], reasons[1])
	}
	// The hidden alias still is not a listing: the catalog answers about it when
	// asked, and `tools.ToolNames()` still does not show it.
	for _, n := range tools.ToolNames() {
		if n == "write_file" {
			t.Error("ToolNames() leaked the hidden alias into the differential's tool list")
		}
	}
	// PoliciesTouching must find the alias-written row for either spelling — the
	// "a row hiding behind a name" case.
	both := toolcatalog.PoliciesTouching("write", in.Policies)
	alias := toolcatalog.PoliciesTouching("write_file", in.Policies)
	if strings.Join(idsOf(both), ",") != strings.Join(idsOf(alias), ",") {
		t.Errorf("PoliciesTouching disagrees between spellings: %v vs %v", idsOf(both), idsOf(alias))
	}
	if len(both) == 0 {
		t.Error("PoliciesTouching found no row for write, though the fixture has an allow written write_file")
	}
}

// TestCustomAgentIsListedNotDispatchable is SD7's honesty: a custom agent's row
// may name tools, and the catalog must still say the app cannot run it — while
// keeping the sources visible, because the row really does name them.
func TestCustomAgentIsListedNotDispatchable(t *testing.T) {
	in := FixtureInputs()
	for _, tool := range []string{"read", "bash"} { // cursitor's row names both
		e := toolcatalog.Effective(tool, "cursitor", in)
		if e.Dispatchable {
			t.Errorf("%s for a custom agent reports dispatchable", tool)
		}
		if e.State != toolcatalog.StateNotGranted {
			t.Errorf("%s for a custom agent reads %s — a capability the app cannot exercise is being advertised", tool, e.State)
		}
		if e.Reason != "" {
			t.Errorf("%s: a custom agent got a verdict it cannot receive: %q", tool, e.Reason)
		}
		if len(e.GrantedBy) == 0 {
			t.Errorf("%s: the sources vanished — the agent's row really does name this tool", tool)
		}
	}
	// The MCP half, same rule: a custom agent's declared MCP tool is not
	// dispatchable and must not read as available.
	f := MCPFixtureSet()
	f.Held["cursitor"] = []string{"gh.list_issues"}
	f.States["cursitor|gh.list_issues"] = MCPStateFixture{State: toolcatalog.MCPAvailable, Reason: "allowed: everything"}
	in.MCP = f
	e := toolcatalog.Effective("gh.list_issues", "cursitor", in)
	if e.Dispatchable || e.State != toolcatalog.StateNotGranted {
		t.Errorf("a custom agent's MCP tool reads %s (dispatchable=%v)", e.State, e.Dispatchable)
	}
}

// TestMCPStatesMapOneToOne is the S12 half: four states in, four states out,
// nothing renamed, nothing dropped, and the reason verbatim from the view.
func TestMCPStatesMapOneToOne(t *testing.T) {
	in := FixtureInputs()
	want := map[string]struct {
		state  toolcatalog.State
		reason string
	}{
		"gh.list_issues":  {toolcatalog.StateGrantedAllowed, mcpReason("gh.list_issues", "atlas")},
		"gh.create_issue": {toolcatalog.StateGrantedDenied, mcpReason("gh.create_issue", "atlas")},
		"gh.search_code":  {toolcatalog.StateGrantedAsk, mcpReason("gh.search_code", "atlas")},
		"gh.silent_tool":  {toolcatalog.StateGrantedNoRule, mcpReason("gh.silent_tool", "atlas")},
	}
	for qualified, w := range want {
		e := toolcatalog.Effective(qualified, "atlas", in)
		if e.State != w.state {
			t.Errorf("%s: state %s, want %s", qualified, e.State, w.state)
		}
		if e.Reason != w.reason {
			t.Errorf("%s: reason %q, want the view's own %q", qualified, e.Reason, w.reason)
		}
		if !e.Dispatchable {
			t.Errorf("%s: a system agent's MCP tool reports not dispatchable", qualified)
		}
	}
	// The declare source shows up exactly where a declare row exists, and nowhere
	// else — MC5: declaring is a source, not a verdict.
	for _, q := range []string{"gh.list_issues", "gh.create_issue", "gh.search_code"} {
		e := toolcatalog.Effective(q, "atlas", in)
		if len(e.GrantedBy) != 1 || e.GrantedBy[0] != toolcatalog.GrantedMCPDeclare {
			t.Errorf("%s: GrantedBy %v, want [mcp-declare]", q, e.GrantedBy)
		}
	}
	if e := toolcatalog.Effective("gh.silent_tool", "atlas", in); len(e.GrantedBy) != 0 {
		t.Errorf("an undeclared held tool claims sources: %v", e.GrantedBy)
	}
	// A name the view does not have for this agent is not held (MC2), and no
	// policy set can talk it into being available.
	hard := FixtureInputs()
	hard.Policies = append(FixturePolicies(), rowFor(t, "allow-everything", "gh.private_tool", policy.Allow, policy.AllAgents, ""))
	if e := toolcatalog.Effective("gh.private_tool", "atlas", hard); e.State != toolcatalog.StateNotGranted {
		t.Errorf("a tool outside the agent's MCP view read %s — MC2 says a peer's private is invisible, even in an answer", e.State)
	}
	// With no MCP view at all (an unwired seam), an MCP name is not_granted and
	// says nothing — never a refusal (V13).
	noMCP := FixtureInputs()
	noMCP.MCP = nil
	if e := toolcatalog.Effective("gh.list_issues", "atlas", noMCP); e.State != toolcatalog.StateNotGranted || e.Reason != "" {
		t.Errorf("with no MCP view: %+v", e)
	}
	// A fifth state is not silently an allow.
	five := FixtureInputs()
	five.MCP = fiveStateView{}
	if e := toolcatalog.Effective("gh.list_issues", "atlas", five); e.State == toolcatalog.StateGrantedAllowed {
		t.Error("an unknown MCP state was read as available")
	}
}

// fiveStateView is the fake that invents a state S12 does not have.
type fiveStateView struct{}

func (fiveStateView) ToolsFor(string) []string { return []string{"gh.list_issues"} }
func (fiveStateView) State(string, string) (toolcatalog.MCPState, string, bool) {
	return toolcatalog.MCPState("available-ish"), "a state nobody declared", true
}
func (fiveStateView) Declares(string, string) bool { return true }

// TestPoliciesTouchingFindsAllMatchingRowsIncludingAllAgents is the panel's
// question — "what rules speak about this tool?" — held to the evaluator's own
// answer, so a row cannot hide behind `all-agents` or an alias.
func TestPoliciesTouchingFindsAllMatchingRowsIncludingAllAgents(t *testing.T) {
	ps := FixturePolicies()
	got := idsOf(toolcatalog.PoliciesTouching("read", ps))
	// allow-read-atlas (agent), expired-deny-read-atlas (agent, lapsed — the
	// panel lists it and says it lapsed), deny-read-review (lane-type).
	for _, want := range []string{"allow-read-atlas", "expired-deny-read-atlas", "deny-read-review"} {
		if !contains(got, func(s string) bool { return s == want }) {
			t.Errorf("PoliciesTouching(read) missed %s; got %v", want, got)
		}
	}
	for _, no := range []string{"deny-bash-all", "ask-grep-all", "allow-todo-all", "allow-write-file-reviewer"} {
		if contains(got, func(s string) bool { return s == no }) {
			t.Errorf("PoliciesTouching(read) named %s, which names a different tool", no)
		}
	}
	// The all-agents deny on bash is found for bash, next to the narrow allow it
	// outranks — the panel that shows one and not the other is the panel that
	// tells someone to delete a rule that would then let the call through.
	if got := idsOf(toolcatalog.PoliciesTouching("bash", ps)); strings.Join(got, ",") != "allow-bash-atlas,deny-bash-all" {
		t.Errorf("PoliciesTouching(bash) = %v, want [allow-bash-atlas deny-bash-all]", got)
	}
	// A loop-targeted and an mcp_server-targeted row are excluded: this is the
	// per-agent view.
	withOthers := append(append([]policy.Policy(nil), ps...),
		rowFor(t, "loop-deny-read", "read", policy.Deny, policy.LoopKind, "l1"),
		rowFor(t, "server-deny-read", "read", policy.Deny, policy.MCPServerKind, "gh"))
	for _, id := range idsOf(toolcatalog.PoliciesTouching("read", withOthers)) {
		if id == "loop-deny-read" || id == "server-deny-read" {
			t.Errorf("PoliciesTouching named a %s-targeted row in the per-agent view", id)
		}
	}
	// A row whose spec is unreadable names no tool and is not a candidate.
	broken := rowFor(t, "broken", "read", policy.Allow, policy.AllAgents, "")
	broken.Spec = []byte(`{"toolName":"","state":"allow"}`)
	if contains(idsOf(toolcatalog.PoliciesTouching("read", append(ps, broken))), func(s string) bool { return s == "broken" }) {
		t.Error("a row with an empty toolName was listed as speaking about read")
	}
}

// TestPoliciesTouchingAgreesWithTheEvaluator decides each row on its own with the
// evaluator and requires the same yes/no from the candidate list — the rule is
// reproduced, so it is tested rather than trusted.
func TestPoliciesTouchingAgreesWithTheEvaluator(t *testing.T) {
	ps := FixturePolicies()
	for _, tool := range []string{"read", "bash", "grep", "todo", "write", "attach"} {
		candidates := map[string]bool{}
		for _, p := range toolcatalog.PoliciesTouching(tool, ps) {
			candidates[p.ID] = true
		}
		for _, p := range ps {
			// The oracle asks the question the row is written to answer: an
			// agent-targeted row is decided about ITS agent (a candidate list for a
			// tool spans agents — that is what the panel shows), while a
			// broad-targeted row is decided about any of them.
			probe := "atlas"
			if p.Target.Kind == policy.AgentKind && p.Target.ID != "" {
				probe = p.Target.ID
			}
			v := policy.Evaluate([]policy.Policy{p}, policy.Request{Type: policy.ToolAccess, Agent: probe, Tool: tool}, At)
			matched := !(v.DefaultDeny || v.Unimplemented)
			if matched && !candidates[p.ID] {
				t.Errorf("%s: the evaluator decides with %s alone, but PoliciesTouching(%s) left it out", tool, p.ID, tool)
			}
			if candidates[p.ID] && p.Target.Kind == policy.LaneTypeKind && !matched {
				continue // A lane row is a candidate for the panel and matches nothing without a lane.
			}
			if candidates[p.ID] && !matched && p.NotAfter != nil {
				continue // A lapsed row is listed and labelled; the engine has stopped seeing it.
			}
			if candidates[p.ID] && !matched {
				t.Errorf("%s: PoliciesTouching listed %s, which the evaluator lets match nothing for %q", tool, p.ID, probe)
			}
		}
	}
}

// TestForAgentAndForToolAreTheSameEntriesFromTwoDirections is what T5's two doors
// rely on: `tools.byAgent` and `tools.list`'s matrix read the same cells.
func TestForAgentAndForToolAreTheSameEntriesFromTwoDirections(t *testing.T) {
	in := FixtureInputs()
	agents := []string{"atlas", "sentry", "reviewer", "cursitor"}
	toolNames := []string{"read", "bash", "grep", "todo", "list"}
	byAgent := toolcatalog.ForAgent("atlas", toolNames, in)
	if len(byAgent) != len(toolNames) {
		t.Fatalf("ForAgent returned %d entries for %d tools", len(byAgent), len(toolNames))
	}
	for i, e := range byAgent {
		if e.Tool != toolNames[i] || e.Agent != "atlas" {
			t.Errorf("ForAgent reordered or mislabeled: %+v", e)
		}
	}
	// The same cell, reached the other way, is the same entry.
	for _, tool := range toolNames {
		col := toolcatalog.ForTool(tool, agents, in)
		for _, e := range col {
			if e.Tool != tool || e.Agent == "" {
				t.Errorf("ForTool returned a cell that does not name itself: %+v", e)
			}
			direct := toolcatalog.Effective(tool, e.Agent, in)
			if !reflect.DeepEqual(e, direct) {
				t.Errorf("ForTool(%s,%s) = %+v, Effective = %+v — two answers to one cell", tool, e.Agent, e, direct)
			}
		}
	}
	// Sorting is stable and total, so two surfaces cannot show different orders.
	sorted := toolcatalog.SortedForRendering(byAgent)
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].Tool > sorted[i].Tool {
			t.Errorf("SortedForRendering is not ordered: %s before %s", sorted[i-1].Tool, sorted[i].Tool)
		}
	}
	if strings.Join(toolNames, ",") == strings.Join(namesOf(sorted), ",") {
		// A coincidence is allowed; a surprise is not. Log it so a reader knows the
		// sort actually had work to do or did not.
		t.Log("the fixture order was already sorted")
	}
}

// contains is the tiny helper both this file and the fixtures use.
func contains[T any](xs []T, pred func(T) bool) bool {
	for _, x := range xs {
		if pred(x) {
			return true
		}
	}
	return false
}

func namesOf(es []toolcatalog.Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Tool)
	}
	return out
}
