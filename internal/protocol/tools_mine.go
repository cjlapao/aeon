package protocol

// ════════════════════════════════════════════════════════════════════════════
// The agent's own tool view (S47 T7, TC9/MC2): `tools.mine.list` and
// `tools.mine.show`.
//
// These two are the SAME catalog action the model calls as the `tools_mine` and
// `tool_show` tools, reached the person's way — one definition, two doors (the
// `loop.get`/`brief.show` precedent: `registerRecords` projects each spec's
// `Method`, so the socket name and the tool name cannot drift, and
// `TestRecordsParityToolsFamily` walks both directions). What lives here is the
// WIRE SHAPE and the two pointer sentences; the answer is built by the daemon's
// catalog service, the one layer that has read the registry, the rows and the
// policy set, so the person's `tools.byAgent` and the agent's `tools_mine` judge
// through the same `toolcatalog.ForAgent` and cannot disagree about a grant.
//
// Three rules the shape itself enforces:
//
//   - NO `agent`/`actor` field anywhere, in params or in a row. "Self" is the
//     identity stamped on the step (OL3); the schema is `additionalProperties:
//     false` with two keys total (`{}` and `{name}`), so a param naming another
//     agent is a parse refusal before any handler runs, not a filter.
//   - A tool the agent does not hold is ABSENT from the list — never a row that
//     says "not granted" — and one it asks to see by name gets the identical
//     refusal a name that never existed gets, with hints drawn from its OWN set
//     only. Two doors that answer differently for those two cases are an
//     existence oracle for a peer's private MCP tool (MC2).
//   - The rows carry no agent column: naming the subject in a row would invite a
//     client to ask whose row this is, and there is exactly one answer, from the
//     connection.
// ════════════════════════════════════════════════════════════════════════════

import (
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// The two socket names. They are the catalog specs' `Method` fields, spelled
// once here as constants so the surface table, the guards and the fixtures all
// read one source; `registerRecords` owns the registration.
// (The two `MethodName` constants live in method.go's S47 T7 block, beside every
// other name on the table — protogen enumerates there, and one file holding the
// names is what keeps a grep for a method name down to one hit.)

// The two pointer sentences a PERSON gets from these doors (rule 6). A person
// holding the endpoint could otherwise use the agent-scoped doors as an oracle —
// ask what a given agent holds, one agent at a time — which is exactly why the
// person's unscoped reads stay a separate path (S46 T8's pattern, carried). The
// sentences name the door that DOES answer, because "not this door" without a
// next move is a dead end with a code on it.
const (
	ToolsMinePersonRefusal = "this door is agent-scoped; a person uses tools.byAgent"
	ToolShowPersonRefusal  = "this door is agent-scoped; a person uses tools.get"
)

// ToolsMineParams is `tools.mine.list`'s params: NOTHING. The type exists so the
// generated contract says in TypeScript what the catalog schema says in JSON —
// `additionalProperties:false` with zero properties — and so a param naming an
// agent is refused by the strict decode rather than ignored. There is no field in
// which to try (the OL3 rule, stated as a type).
type ToolsMineParams struct{}

// ToolShowParams is `tools.mine.show`'s params: one name, and only one name. No
// `agent`, no `scope`, no "include ungranted" — those three are exactly the
// questions this door is not allowed to answer, and a type with nowhere to put
// them is the cheapest place to say so.
type ToolShowParams struct {
	Name string `json:"name"`
}

// ToolsMineEntry is one row of the agent's own list: the catalog's own words
// about the tool — the same `ToolEntry` fields a person sees in `tools.list`, so
// a tool is described identically on both doors — beside THIS agent's effective
// state and the policy's verbatim reason sentence (the engine's own formatter,
// never a paraphrase: a second renderer of a decision is a second authority over
// what the decision means).
//
// There is no `Agent` field. The caller IS the subject, and a row that named one
// would be an invitation to ask about someone else.
type ToolsMineEntry struct {
	Tool    ToolEntry         `json:"tool"`
	State   toolcatalog.State `json:"state"`
	Reason  string            `json:"reason,omitempty"`
	Granted []string          `json:"granted_by,omitempty"`
}

// ToolsMineResult is `tools.mine.list`'s answer.
//
// `Tools` is the agent's GRANTED set and nothing else: every row is one of
// `granted_allowed`, `granted_ask`, `granted_denied` or `granted_no_rule`, and a
// tool the agent holds no grant for is absent. An empty list is therefore a real
// answer about an agent that holds nothing — which is why `Held` is stated
// separately from `Exists`: a step whose agent has no row at all is refused
// before this answer, so an empty list never means "no such agent".
type ToolsMineResult struct {
	Tools []ToolsMineEntry `json:"tools"`
	// Held counts the rows, so a client that renders a badge does not count the
	// array and the answer cannot disagree with itself.
	Held int `json:"held"`
	// Dispatchable is the SD7 fact stated once at the top: a custom agent's row
	// names tools and no step of its own can run. The list stays honest about
	// what the row says; this is what stops a panel calling it usable.
	Dispatchable bool `json:"dispatchable"`
	// PolicySeen counts the `tool_access` rows that could speak about this
	// agent. It is a count and not a list because the rows ride on each entry
	// they touch; it exists so "no rules apply to you" is a claim the answer can
	// be checked against rather than inferred from empty reason fields.
	PolicySeen int `json:"policy_seen"`
}

// ToolShowResult is `tools.mine.show`'s answer: one tool in full, from the
// agent's own point of view.
//
// `Policies` holds only the rows that could change whether THIS agent may call
// it — an `agent` target equal to self, an `all-agents` row, or a lane-type row
// matching the agent's type — and never another agent's rows, which would report
// another agent's restrictions. `Schema` is the code's schema, the one the model
// is already offered for a tool it holds (rule 7: no second schema to drift).
type ToolShowResult struct {
	Tool     ToolEntry         `json:"tool"`
	State    toolcatalog.State `json:"state"`
	Reason   string            `json:"reason,omitempty"`
	Granted  []string          `json:"granted_by,omitempty"`
	Policies []PolicyRef       `json:"policies"`
	// MCP is present for a qualified `server.tool` name only. A name that is not
	// in this agent's view never reaches here at all, so the presence of this
	// field is not an oracle: an invisible server and an absent one are refused
	// identically before it is filled.
	MCP *ToolMCPLink `json:"mcp,omitempty"`
}
