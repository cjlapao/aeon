package toolcatalog

// The internals of one answer: the grant half, the verdict half, and the MCP
// half, each reached the way its owner reaches it — `tools.GrantedSet` for the
// names, `policy.Evaluate` for the decision, `MCPView` for what S12's authority
// says. Nothing here decides anything on its own; that is the whole design.

import (
	"encoding/json"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// grantsFor resolves the two lists that make up what an agent holds, through the
// one function the builder uses.
//
// The union is default ∪ agent allow-list, in that order: the default grant first
// because it is the floor every step gets, the row's names after, and duplicates
// kept by `GrantedSet` (whose output `Granted()` reports verbatim — deduping here
// would make the catalog's list disagree with the builder's, which is TC5's exact
// prohibition). An unknown grant name is a real error and is handled honestly:
// the names that do exist still answer, and the invalid entry simply grants
// nothing, because a catalog that refused to answer would leave a panel with
// nothing to show while the builder — with the same list — refuses to build the
// step at all. That asymmetry is the catalog's one place where it is allowed to
// differ from the builder, and it is a difference in what it reports, never in
// what it permits.
func (in Inputs) grantsFor(agent string) (granted []string, dispatchable bool, known bool) {
	if in.AgentGrants == nil {
		return nil, false, false
	}
	names, dispatchable, ok := in.AgentGrants(agent)
	if !ok {
		return nil, false, false
	}
	combined := append(append([]string(nil), in.DefaultGrant...), names...)
	valid, err := tools.GrantedSet(combined)
	if err != nil {
		// The list names something the registry does not have. Keep the valid
		// half (see the doc above) and let the doors report the refusal, which is
		// `GrantedSet`'s own sentence — the same one the builder gives.
		valid = dropUnknown(combined)
	}
	return valid, dispatchable, true
}

// dropUnknown keeps the names the registry knows, in order, when a grant list
// contains a name it does not. Only reachable from a malformed agent row; the
// refusal text still comes from `GrantedSet`, never rewritten here.
func dropUnknown(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if tools.IsGrantedTool(n) {
			out = append(out, n)
		}
	}
	return out
}

// sourcesFor answers "who put this name here", which is the difference between a
// grant a person can see on the agent's row and one that arrives because every
// step gets it. A name in both lists names both sources — one list winning over
// the other would be a guess about who to thank, and the panel is where somebody
// goes to undo it.
func sourcesFor(tool string, defGrant, agentGrant []string) []string {
	var out []string
	if containsName(defGrant, tool) {
		out = append(out, GrantedDefault)
	}
	if containsName(agentGrant, tool) {
		out = append(out, GrantedHandler)
	}
	return out
}

// containsName is a name test, not a canonical-name test: `GrantedSet`'s output
// carries the spellings the author granted, and `Effective` is asked about a
// spelling too. The canonical folding happens where it must — in the policy
// matcher, which canonicalises both sides (SEC-059), and in `PoliciesTouching`.
// Folding here would let a grant spelled `write_file` answer a question about
// `write` while `Granted()` — the builder's answer — said otherwise.
func containsName(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// toolRequest builds the question exactly as the step's gate builds it. The
// fields left zero are left zero on purpose: `Loop` and `LaneType` belong to a
// step and not to a catalog row, and a policy targeted at a lane-type cannot be
// answered here any more than a loop-targeted one can — which is why the panel
// says what it is showing and `PoliciesTouching` says what it excluded.
func toolRequest(agent, tool string) policy.Request {
	return policy.Request{
		Type:  policy.ToolAccess,
		Agent: agent,
		Tool:  tool,
	}
}

// stateOf maps the engine's verdict onto the catalog's five states. It is the
// only place a verdict becomes a state, and it rewords nothing: `Reason` is
// `Verdict.Sentence()` byte-for-byte, because the one formatter is the truth and a
// second renderer is a second truth (PL).
//
// The mapping is total over the engine's shapes, in this order:
//
//   - `Unimplemented` (D10) → `granted_no_rule`. Nothing refused, nothing
//     permitted, the engine could not decide. Reading it as a denial would blame a
//     rule for a gap in the binary; reading it as an allow would permit. Its
//     sentence says "has no evaluator yet", which is what a person needs to see.
//     `tool_access` has a real evaluator, so this path exists for the day one is
//     unregistered, not for today.
//   - `Allowed` → `granted_allowed`.
//   - `Asked` → `granted_ask` (an ask outranks an allow and is not a permission —
//     S11; the sentence names the allow it looked past).
//   - `DefaultDeny` → `granted_no_rule` (PL5: silence permits nothing).
//   - anything else → `granted_denied`. The engine returns no fifth shape, so this
//     branch is the deny a matched deny rule produces, and the only way to reach
//     it with something else is a future engine shape, which would then be a
//     refusal and reads correctly as denied.
func stateOf(v policy.Verdict) (State, string) {
	switch {
	case v.Unimplemented:
		return StateGrantedNoRule, v.Sentence()
	case v.Allowed:
		return StateGrantedAllowed, v.Sentence()
	case v.Asked:
		return StateGrantedAsk, v.Sentence()
	case v.DefaultDeny:
		return StateGrantedNoRule, v.Sentence()
	default:
		return StateGrantedDenied, v.Sentence()
	}
}

// effectiveMCP is the MCP half: the four S12 states mapped 1:1, with no policy
// evaluation of its own.
//
// That is not laziness, it is the design S12 and S46 built: `GrantView` already
// folds the policy set AND the effect-ask for a declared MCP tool, and a second
// `Evaluate` here would produce a second answer to one question — the failure MC10
// and TC5 both exist to prevent. The reason arrives verbatim from the view for the
// same reason.
func effectiveMCP(tool, agent string, in Inputs, e Entry) Entry {
	if in.MCP == nil {
		// An unwired seam is an absent answer, never a refusal (V13). "Not held"
		// is what the catalog can honestly say when the MCP layer is not in the
		// build.
		e.State = StateNotGranted
		return e
	}
	_, dispatchable, known := in.grantsFor(agent)
	e.Dispatchable = known && dispatchable

	held := false
	for _, q := range in.MCP.ToolsFor(agent) {
		if q == tool {
			held = true
			break
		}
	}
	if !held {
		e.State = StateNotGranted
		return e
	}
	if in.MCP.Declares(agent, tool) {
		e.GrantedBy = append(e.GrantedBy, GrantedMCPDeclare)
	}
	state, reason, ok := in.MCP.State(agent, tool)
	if !ok {
		e.State = StateNotGranted
		return e
	}
	if !e.Dispatchable {
		// SD7 again, on the MCP side: a custom agent's declared MCP tool is still
		// a tool no step of its own can call. The reason is dropped with the state,
		// for the same reason `Effective` drops it: an agent that cannot be
		// dispatched was never a step the engine could be asked about.
		e.State = StateNotGranted
		e.Reason = ""
		return e
	}
	switch state {
	case MCPAvailable:
		e.State = StateGrantedAllowed
	case MCPAsk:
		e.State = StateGrantedAsk
	case MCPUnavailable:
		e.State = StateGrantedNoRule
	case MCPRefused:
		e.State = StateGrantedDenied
	default:
		// A state outside the four is not silently an allow. It is also not
		// silently a denial — it is a view this package cannot interpret, and the
		// honest rendering of that is "no rule spoke", with the text a person
		// needs to go and look. `TestMCPStatesMapOneToOne` fails any view that
		// invents a fifth.
		e.State = StateGrantedNoRule
	}
	e.Reason = reason
	return e
}

// governsAnAgent is the target half of `PoliciesTouching`: the kinds that can
// speak about an agent's tool call. Loop and MCP-server targets are excluded —
// the first belongs to a step this catalog is not looking at, the second to the
// MCP view — and an unknown kind can govern nothing.
//
// The comparison is by kind and not by re-running the evaluator's filter, and
// that is the one place this package reproduces a rule instead of calling the
// owner of it. It is tested rather than trusted:
// `TestPoliciesTouchingAgreesWithTheEvaluator` decides each candidate row on its
// own with `policy.Evaluate` and requires the same yes/no.
func governsAnAgent(t policy.Target) bool {
	switch t.Kind {
	case policy.AllAgents, policy.AgentKind, policy.LaneTypeKind:
		return true
	}
	return false
}

// toolAccessToolName reads the spec's `toolName` without claiming to validate the
// row — `policy` owns validation, and a stored row has already passed it. What it
// does refuse is a spec it cannot read and an empty name: both name no tool, and a
// candidate list that included them would tell a person a rule exists behind a
// tool when the evaluator would let it match nothing.
func toolAccessToolName(spec []byte) (string, bool) {
	if len(spec) == 0 {
		return "", false
	}
	var s struct {
		ToolName string `json:"toolName"`
	}
	if err := json.Unmarshal(spec, &s); err != nil || s.ToolName == "" {
		return "", false
	}
	return s.ToolName, true
}
