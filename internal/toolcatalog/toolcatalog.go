// Package toolcatalog answers one question in one place: **can agent A call tool
// T, and if not, why** (S47, TC5).
//
// Everything the catalog shows about grants comes through here — `tools.list`,
// `tools.get`, `tools.byAgent`, `tools.status` (T5), the agent's own-view tools
// (T7), the CLI (T8) and the two Settings panels (T10). Before this package the
// same question had three partial answers in three places: `BuildToolset` knew
// what a grant list builds, `policy.Evaluate` knew what a policy set permits, and
// the MCP registry's `GrantView` knew both for MCP tools with the effect-ask
// folded in. Three answers is three chances to disagree, and the disagreement a
// person would notice is the bad kind: the catalog says a tool is available, the
// step cannot call it.
//
// So this package holds no state and touches nothing. It takes the grant list,
// the policy set and the clock as parameters — `Inputs` — and returns an `Entry`.
// There is no store handle to reach for, no `time.Now` to make a test depend on
// the hour, and no `internal/mcp` import: the MCP half arrives as the `MCPView`
// seam, implemented by the one package allowed to import `internal/mcp`
// (`cmd/aeon`, MC11). `TestEffectiveIsPureAndClockInjected` reads the import list
// to keep that true rather than merely meaning it.
//
// The three laws this exists to keep:
//
//   - **One evaluator.** The verdict comes from `policy.Evaluate` with the same
//     `Request` the step's gate builds, and the reason is `Verdict.Sentence()`
//     **verbatim** — never reworded, never re-formatted (PL: one formatter, one
//     truth). `TestReasonIsTheVerdictSentenceVerbatim` compares bytes.
//   - **One grant definition.** "Is it in the granted set" asks
//     `tools.GrantedSet`, the function `BuildToolset` itself calls (T4's
//     extraction), so the catalog and the builder cannot diverge.
//   - **No invented answer.** A tool the agent does not hold gets `not_granted`
//     and an empty reason: no policy sentence is fabricated for a question the
//     engine was never asked (`TestNotGrantedHasNoVerdict`).
package toolcatalog

import (
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// State is the catalog's answer about one (tool, agent) pair. The five values are
// the whole vocabulary — TC5 refuses a sixth, and `TestNoStateOutsideTheFive`
// pins it — because a person reads these words in a panel and a door refuses or
// allows on them.
//
// The split between `granted_no_rule` and `granted_denied` is not decoration:
// one means "nothing spoke, and silence permits nothing" (PL5) and the other
// means "a rule refused". A panel that showed both as "denied" would send someone
// to delete a rule that does not exist, and a panel that showed both as "no rule"
// would send them to add one that would be outranked anyway.
type State string

const (
	// StateNotGranted: the agent does not hold the tool at all. No verdict was
	// sought, so none is shown.
	StateNotGranted State = "not_granted"
	// StateGrantedAllowed: held, and an allow rule decided it.
	StateGrantedAllowed State = "granted_allowed"
	// StateGrantedAsk: held, and an ask rule outranks the allow — a human decides
	// at the moment of the call (S11), which is not a permission.
	StateGrantedAsk State = "granted_ask"
	// StateGrantedDenied: held, and a deny rule refused it.
	StateGrantedDenied State = "granted_denied"
	// StateGrantedNoRule: held, and no rule matched. PL5: an empty match set
	// denies; it does not permit by omission.
	StateGrantedNoRule State = "granted_no_rule"
)

// Valid reports whether s is one of the five. Exported so T5's door can refuse a
// value that arrived from a fixture or a stale client rather than render it.
func (s State) Valid() bool {
	switch s {
	case StateNotGranted, StateGrantedAllowed, StateGrantedAsk, StateGrantedDenied, StateGrantedNoRule:
		return true
	}
	return false
}

// States is the vocabulary in the order a panel lists it.
func States() []State {
	return []State{StateGrantedAllowed, StateGrantedAsk, StateGrantedDenied, StateGrantedNoRule, StateNotGranted}
}

// The sources that can put a name in a granted set. Closed, like the states:
// T0's grant-chain inventory found exactly these four lists feeding a step's
// tools (the default grant, the agent row's allow-list — the handler's `Tools`
// field read from `agent.tools` — and the MCP declare rows), and blueprint lanes
// are not one of them.
const (
	GrantedDefault    = "default"
	GrantedHandler    = "handler"
	GrantedMCPDeclare = "mcp-declare"
)

// Entry is the answer for one (tool, agent) pair.
//
// `Reason` is the verdict's own sentence, byte-for-byte, or the empty string when
// no verdict was sought — which is the case `TestNotGrantedHasNoVerdict` guards,
// because a fabricated reason is worse than none: it reads like the engine
// decided something it was never asked.
//
// `Dispatchable` is the step's half of the truth (S43-extra SD7): a custom agent
// may be listed with tools named on its row, and it still cannot be dispatched,
// so a panel that said "granted" about such an agent would be showing a
// capability the app cannot exercise.
type Entry struct {
	Tool         string   `json:"tool"`
	Agent        string   `json:"agent"`
	State        State    `json:"state"`
	Reason       string   `json:"reason,omitempty"`
	GrantedBy    []string `json:"granted_by,omitempty"`
	Dispatchable bool     `json:"dispatchable"`
}

// MCPState is S12's authority view of one MCP tool, spelled here rather than
// imported: `internal/mcp` is importable only by `cmd/aeon` (MC11), and a catalog
// that pulled it in would make that package undeletable — the very seam S46 built
// to keep the MCP layer removable.
type MCPState string

const (
	MCPAvailable   MCPState = "available"
	MCPAsk         MCPState = "ask"
	MCPUnavailable MCPState = "unavailable"
	MCPRefused     MCPState = "refused"
)

// MCPView is the seam through which the MCP half of the answer arrives, supplied
// by `cmd/aeon` (the only package that may reach `internal/mcp`) over S46's scoped
// reads and `GrantView`. The four states map 1:1 onto this package's verdict
// states — `TestMCPStatesMapOneToOne` pins all four and refuses a fifth — and the
// reason travels verbatim from `GrantView.Reason`, which is itself a verdict
// sentence or the effect-ask sentence.
type MCPView interface {
	// ToolsFor lists the qualified names (`server.tool`) this agent may see: the
	// servers it declared against, narrowed by the core narrowing. A name absent
	// here is not held, whatever else anyone says about it (MC2: a peer's private
	// is invisible, even in an error).
	ToolsFor(agent string) []string
	// State answers the authority question for one qualified name. ok is false
	// when the view has nothing to say (the name is not this agent's), which the
	// catalog reads as not-held rather than as a refusal.
	State(agent, qualified string) (state MCPState, reason string, ok bool)
	// Declares reports a `mcp_declare` row behind this agent for that name — the
	// `mcp-declare` entry in `GrantedBy`. Declaring grants nothing by itself (MC5);
	// it is a source, not a verdict.
	Declares(agent, qualified string) bool
}

// Inputs is everything the answer is computed from, all of it supplied.
//
// `At` is the clock the caller holds, because a policy with a `NotAfter` is
// decided against a moment and a function that read the clock itself would make
// every fixture expire on some night nobody tested. nil `MCP` means "this build
// has no MCP view" — an unwired seam is an absent answer, never a refusal (V13).
type Inputs struct {
	// DefaultGrant is the set a step gets when nobody says otherwise — the list
	// `tools.DefaultGrant()` hands out, passed in rather than called so a test can
	// ask the catalog about a world with a different default.
	DefaultGrant []string

	// AgentGrants reads the agent's side: its row's tool allow-list, whether it is
	// dispatchable, and whether the agent exists at all. A custom agent reports
	// dispatchable=false (S43-extra SD7). ok=false means no such agent: every
	// entry then reads not_granted, and nothing is invented about a name nobody
	// stored.
	AgentGrants func(agent string) (granted []string, dispatchable bool, ok bool)

	// MCP is the S12 seam described above; nil is legal and means absent.
	MCP MCPView

	// Policies is the set the caller's gate would decide with — the SAME rows the
	// step's frozen set holds, not a re-read guess. This package never selects,
	// sorts or trims it; `policy.Evaluate` does, and it is the only evaluator.
	Policies []policy.Policy

	// At is the instant expiry is judged at — the moment the caller's gate would
	// have used. It is a value and not a call: this package has no clock, so a
	// fixture that wants a rule to be lapsed says so with a date instead of
	// waiting for one.
	At time.Time
}

// Effective answers the question for one (tool, agent) pair.
//
// The order is the order of the reasoning, and it matters: the grant question is
// settled first, because asking the policy engine about a tool an agent does not
// hold produces a default-deny that reads like a decision. Then the verdict, from
// the evaluator itself.
func Effective(tool, agent string, in Inputs) Entry {
	e := Entry{Tool: tool, Agent: agent}

	if IsMCPToolName(tool) {
		return effectiveMCP(tool, agent, in, e)
	}

	granted, dispatchable, known := in.grantsFor(agent)
	e.Dispatchable = dispatchable
	if !known || tool == "" {
		// Nothing is claimed about an agent that is not in the table, and about an
		// empty tool name — a fabricated verdict for either would be a sentence the
		// engine never said.
		e.State = StateNotGranted
		return e
	}

	sources := sourcesFor(tool, in.DefaultGrant, granted)
	if len(sources) == 0 {
		e.State = StateNotGranted
		return e
	}
	e.GrantedBy = sources

	// The same Request the step's gate builds — same type, same agent, same tool —
	// so the answer here is the answer the gate would give, from the same
	// evaluator (T4's differential test is what makes that more than a claim).
	v := policy.Evaluate(in.Policies, toolRequest(agent, tool), in.At)
	e.State, e.Reason = stateOf(v)
	if !e.Dispatchable {
		// SD7: a name on a custom agent's row is not a capability the app can
		// exercise, so the pair is reported as not held and the sources are left
		// visible — the list is a fact about the row, `not_granted` is the fact
		// about the step. The reason goes with the state: an agent that can never
		// be dispatched was never a step the engine could be asked about, and a
		// sentence left behind would read as a decision about a call that cannot
		// exist.
		e.State = StateNotGranted
		e.Reason = ""
	}
	return e
}

// ForAgent is one agent's whole view: the tools asked about, in the order asked,
// each one answered independently. A caller that wants "everything" passes
// `tools.ToolNames()` plus the agent's MCP names — this function does not decide
// what the listing is, only what each row means.
func ForAgent(agent string, tools []string, in Inputs) []Entry {
	out := make([]Entry, 0, len(tools))
	for _, t := range tools {
		out = append(out, Effective(t, agent, in))
	}
	return out
}

// ForTool is the matrix column: which agents hold and may use this tool. Agents
// come back in the order given (a caller with an opinion about ordering has one;
// the catalog has none) and duplicates are kept, because the caller counted them.
func ForTool(tool string, agents []string, in Inputs) []Entry {
	out := make([]Entry, 0, len(agents))
	for _, a := range agents {
		out = append(out, Effective(tool, a, in))
	}
	return out
}

// PoliciesTouching returns the `tool_access` rows that can speak about this tool:
// the ones whose spec `toolName` canonicalises to the tool's canonical name, and
// whose target is a kind that can govern an agent at all — `all-agents` or
// `lane-type` or an agent. It is what "show me the rules behind this tool" needs,
// and the reason the canonical form is compared rather than the string: a row
// written `write_file` governs `write` (SEC-051/SEC-059), and a listing that
// missed it would tell a person nothing refuses their tool while the gate refuses
// every call.
//
// What it deliberately does NOT do is decide. The evaluator decides; this selects
// candidate rows for a panel, and `TestPoliciesTouchingFindsAllMatchingRowsIncludingAllAgents`
// holds it to the evaluator's own answer row by row. Rows whose target is a loop
// or an MCP server are excluded: this is the per-agent view, and an
// `mcp_server`-targeted row belongs to the MCP half, where `MCPView` renders it.
func PoliciesTouching(tool string, ps []policy.Policy) []policy.Policy {
	if tool == "" {
		return nil
	}
	want := tools.CanonicalToolName(tool)
	out := make([]policy.Policy, 0, len(ps))
	for _, p := range ps {
		if p.Type != policy.ToolAccess {
			continue
		}
		if !governsAnAgent(p.Target) {
			continue
		}
		name, ok := toolAccessToolName(p.Spec)
		if !ok {
			continue // A row whose spec cannot be read names no tool; it is inert, not a match.
		}
		if tools.CanonicalToolName(name) != want {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SortedForRendering orders entries the way both panels and the CLI list them —
// tool name, then agent — so two surfaces built from the same entries do not
// disagree about position. It is here rather than in each caller because the
// comparison is the same one everywhere and a drift in sort order reads as a
// different answer.
func SortedForRendering(entries []Entry) []Entry {
	out := append([]Entry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

// IsMCPToolName is the one definition of "this name is an MCP tool's": a
// qualified `server.tool`, which the system registry can never spell (its names
// have no dot — `TestMCPToolNamesAreTheQualifiedOnes` pins that the two name
// spaces do not overlap, so no name is ever judged twice).
func IsMCPToolName(name string) bool { return strings.Contains(name, ".") }
