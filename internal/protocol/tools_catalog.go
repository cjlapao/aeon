package protocol

// ════════════════════════════════════════════════════════════════════════════
// The person's tools catalog (S47 T5, TC10/TC11): `tools.list`, `tools.get`,
// `tools.byAgent`, `tools.status`.
//
// # What this file is
//
// Four READ-ONLY methods over the one `Table.ToolsCatalog` seam, the same
// one-seam-many-methods shape `RegisterMCPAdmin` uses for the five management
// reads and `RegisterSeed` uses for the four seed doors. This file holds no
// catalog LOGIC: it decodes params, refuses identity keys, forwards, and wraps
// the list's empty answer in the table's own sentence. Everything that decides
// — which tools exist, what each one is, whether a row still matches the code,
// who may call what and why not — is the seam's, because the seam is the daemon
// adapter that reads the registry, the `tool` rows, the policy set and the MCP
// view. A nil seam leaves the four names ABSENT (V13: RegisterToolsCatalog
// returns false, a client gets JSON-RPC's own -32601, and an absent door is a
// fact about the build while a refusal is a fact about a decision — the two must
// never be confused, which is why the seam's nil branch below is belt-only).
//
// # Why the catalog is a door at all (TC5)
//
// "Can agent A call tool T, and if not, why" is one question with three
// authorities behind it: the registry (does the tool exist, what is its schema),
// the grant chain (default ∪ the agent's row, through the SAME function
// `BuildToolset` calls — `tools.GrantedSet`, T4), and the policy engine
// (`policy.Evaluate`, whose `Verdict.Sentence()` is the one formatter). Any
// client that assembled the answer itself would own a second version of that
// question, and the two would drift exactly when it mattered — while the step
// still refused the call the UI had just shown as allowed. So the join happens
// once, in `internal/toolcatalog` (pure, clock injected), and reaches every
// surface through these doors.
//
// # TC2 — code is the truth about a tool; the row is a projection
//
// `ToolEntry.schema`, `schemaHash`, `description` and the class come from
// `Registry()` and `tools.AllMeta()`, NEVER from the `tool` row; the row supplies
// `version`, `origin` and the drift verdict (`rowCurrent`). Serving the row's
// schema would put the catalog out of step with the executor the model is
// actually handed, and the disagreement would be invisible — which is what
// `tools.status`'s drift report exists to make loud instead (TC11). The row is
// still read, still shown as data, and never trusted as the spec.
//
// # TC1 — there is no write here
//
// Four reads, zero mutations, no audit row: a spy pins it (`TestToolsReadsWrite
// Nothing`, daemon side). `tools.grants` (S34) is untouched — this slice adds a
// catalog beside it, and a test pins the older door's shape so "improving" it is
// a reviewable diff and not a side effect.
//
// # TC9/MC2 — this is the human door, not the agent's
//
// The parameters are FILTERS and a SUBJECT, never a caller identity:
// `rejectIdentityKeys` refuses a bag that names an actor. An agent's own view is
// T7's `tools_mine`/`tool_show`, a separate path through the same pure catalog
// with the identity the daemon stamps — so an agent can never ask this door
// about another agent. `tools.byAgent {agent}` is the PERSON asking about a
// subject, the same way `mcp.grants {agent}` is.
//
// # The sentences belong to the seam
//
// A typed `*Error` from the seam (`ErrToolUnknown`, `ErrToolAgentUnknown`)
// travels UNTOUCHED — only the seam reads the rows and the registry, so only it
// can name the nearest names, and a paraphrase here would be a second authority
// over the fact. A plain error is infrastructure and passes through as
// Dispatch's internal error (DB19's discrimination). The reason strings inside
// an `AgentGrantRow` are `policy.Verdict.Sentence()` verbatim from the seam's
// call into `toolcatalog` — the catalog quotes the engine, and this file adds no
// interpretation of its own.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// --- the closed words on the wire -------------------------------------------

// The two origins a `ToolEntry` can carry. A tool is either something this build
// implements (system) or something an MCP server declares (mcp); the pair is
// closed because a third word would be a tool nobody can run and nobody
// classified.
const (
	ToolsSourceSystem = "system"
	ToolsSourceMCP    = "mcp"
)

// The four drift states `tools.status` reports (TC11), pinned here as the wire
// vocabulary and produced by the seam from `toolseed.DriftState` — the two
// spellings are asserted equal by the daemon's own tests, and a fifth word would
// be a state nobody renders.
//
// The distinction that matters is between the last two: `missing_row` is a tool
// in code with no row (the seed has not run), while `differs` is a row that
// exists and DISAGREES with the code — a bug signal, not a startup state, and
// the reason it is shown rather than repaired is TC11: silently rewriting a row
// would make the disagreement unfindable, which is the only thing worse than
// saying it out loud.
const (
	ToolsDriftCurrent    = "current"
	ToolsDriftMissingRow = "missing_row"
	ToolsDriftRetired    = "retired"
	ToolsDriftDiffers    = "differs"
)

// ToolsListNoMatchStatement is the EMPTY TABLE's sentence for `tools.list` —
// pinned byte for byte where the wire shape is defined (the
// `MCPChangesNoChangesStatement` discipline) and printed ONLY when a filter was
// supplied and matched nothing. An unfiltered empty list prints no statement,
// because that would claim the build contains no tools at all: the filter's
// emptiness is the filter's answer and the table's emptiness is the table's, and
// conflating them is how "my search was wrong" gets read as "nothing exists".
const ToolsListNoMatchStatement = "no tools match this filter."

// --- the wire types ----------------------------------------------------------

// AgentGrantRow is one (tool, agent) cell — the state, the engine's verbatim
// sentence behind it, and where the grant came from. It is the SAME type the
// pure catalog produces, not a copy with the same fields: a wire twin of
// `toolcatalog.Entry` would be a second definition of the state vocabulary, and
// the whole argument for this slice is that there is one vocabulary
// (`internal/toolcatalog`'s five states), asserted against `policy.Evaluate`
// pair by pair (T4's differential). `GrantedBy` names the sources
// ("default"/"handler"/"mcp-declare") and `Dispatchable` is false for a custom
// agent, whose row may name tools the app can never run (SD7).
type AgentGrantRow = toolcatalog.Entry

// ToolEntry is one tool as the catalog renders it (TC10). Field by field, each
// one has one home: the description, schema, hash, class, capabilities, governed
// set, twin, conditional flag and family come from CODE (TC2 — `Registry()` plus
// `tools.AllMeta()`, the same values the model is handed); `version`, `origin`
// and `rowCurrent` come from the seeded `tool` ROW; `server`, `tier`, `effect`
// and `classified` exist only for an MCP tool (a system tool has no server to
// link, and an empty-string server would name one with nothing).
//
// `available` is three-state honesty (P31) about the build rather than about a
// permission: `search` and `graph` exist in code and are absent from
// `Registry()` when their seam is not wired, and an honest catalog lists them
// with `available:false` and the conditional flag set. Collapsing them into
// "does not exist" would tell an operator their own build has no such tool;
// collapsing them into "available" would advertise a tool whose call can only
// fail.
//
// `aliases` is where the hidden `write_file` spelling rides — on `write`, never
// as an entry of its own, because one executor with two rows is the second truth
// TC11 exists to make visible.
type ToolEntry struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Family  string   `json:"family"`
	// Category, Access and Address are S74's tool address, computed (never
	// stored) from the registry and the MCP rows: `<category>.<tool>.<access>`,
	// four segments for an MCP tool (`mcp.<server>.<tool>.<access>`). A policy
	// pattern is matched against Address.
	Category    string `json:"category,omitempty"`
	Access      string `json:"access,omitempty"` // "read" | "write"
	Address     string `json:"address,omitempty"`
	Source      string `json:"source"` // "system" | "mcp" (ToolsSource*)
	Description string `json:"description"`

	// Schema is the code-built input schema and SchemaHash its digest
	// (`tools.SchemaHash` over the canonicalised form). NOT the row's
	// columns (TC2); the row's copy exists so drift is detectable, and
	// `rowCurrent` is where the disagreement shows.
	Schema       json.RawMessage `json:"schema"`
	SchemaHash   string          `json:"schemaHash"`
	RerunClass   string          `json:"rerunClass"` // "safe" | "detectable" | "unsafe"
	Capabilities []string        `json:"capabilities"`
	Governed     []string        `json:"governed"` // the policy types that judge it per call
	Twin         string          `json:"twin,omitempty"`
	Conditional  bool            `json:"conditional"`
	Available    bool            `json:"available"`

	// The MCP half, absent for a system tool. `classified:false` is MC6: a
	// declared tool nobody classified, which the runtime refuses at call time —
	// shown as data, not hidden, because a hidden unclassified tool is a
	// capability the management screen pretends is absent while its server
	// still advertises it.
	Server     string `json:"server,omitempty"`
	Tier       string `json:"tier,omitempty"`
	Effect     string `json:"effect,omitempty"`
	Classified bool   `json:"classified"`

	Version    int    `json:"version"`
	Origin     string `json:"origin"`
	RowCurrent bool   `json:"rowCurrent"`
}

// PolicyRef is one `tool_access` row that speaks about a tool (TC10's
// "the policy rows that touch it"), reduced to what a panel renders and links.
// It is DATA, not navigation: `Links`-style `{kind,id}` is what the UI builds
// routes from, so the daemon never has to know that a policy row is clickable in
// one screen and not another.
//
// `Describe` is `policy.Describe(p)` verbatim — S45's one formatter, the same
// sentence `policy.list` prints and the CLI shows (T0's ruling: S45 is merged,
// so `Describe` is used and a hand-assembled sentence here would be the second
// formatter the policy package exists to prevent). `Expired` is the third state
// a panel must show: a lapsed row is a fact about what was once decided, not a
// current permission, and `toolcatalog.PoliciesTouching` lists it on purpose.
type PolicyRef struct {
	// ID is the row's own id as the policy package spells it — a string and not
	// a number: the engine's identity is opaque, and a client that parsed it as
	// an integer would break the first time a set is imported under a key that
	// is not a number.
	ID       string `json:"id"`
	Action   string `json:"action"` // "allow" | "deny" | "ask"
	Source   string `json:"source"` // operator | agent | mcp_server | import
	Origin   string `json:"origin,omitempty"`
	Target   string `json:"target"` // "all-agents", "agent:atlas", "lane-type:review"
	ToolName string `json:"toolName"`
	Describe string `json:"describe"`
	Expired  bool   `json:"expired"`

	// Link is the navigation as DATA (T5's note 4, S45's discipline): the daemon
	// says which kind of thing this row is and which row it is, and the app builds
	// the route. A client that assembled its own path would bake a URL shape into
	// the one place the shape is allowed to change.
	Link Link `json:"link"`
}

// ToolMCPLink is the MCP half of `tools.get`'s answer for a qualified tool: the
// server it belongs to, the tier that decides who may see it, and the data link
// the UI turns into a navigation into S46's screens. It is absent for a system
// tool (there is no server), and an empty Server is not used to say the same
// thing.
type ToolMCPLink struct {
	Server string `json:"server"`
	Tier   string `json:"tier"`
	Link   Link   `json:"link"`
}

// Link is a navigation fact, not a URL: what kind of thing and which one. The
// UI owns the route table; the daemon owns the identity (S45's PM10 pattern —
// links are data).
type Link struct {
	Kind string `json:"kind"` // "mcp_server" | "policy" | "agent"
	ID   string `json:"id"`
}

// --- params and results ------------------------------------------------------

// ToolsListFilter is `tools.list`'s params: every filter is OPTIONAL and a
// POINTER, so the three readings stay distinct — absent (no opinion), present
// (a real filter), present-and-empty (a filter that can match nothing, which is
// the case that earns `statement`). Filtering is DAEMON-side (the task names it):
// a client that filtered locally would have to fetch the whole catalog on every
// keystroke and would own a copy of the matching rules that the catalog can
// change. `q` is a case-insensitive substring over name and description.
//
// `available` is a filter over the availability fact, not a permission: asking
// for `available:true` drops the unwired conditionals (`search`, `graph` in a
// build without their seams), which is the honest way to render a "what can I
// actually call today" list.
type ToolsListFilter struct {
	Family     *string `json:"family,omitempty"`
	Source     *string `json:"source,omitempty"`     // "system" | "mcp"
	Capability *string `json:"capability,omitempty"` // one of the declared capabilities
	Access     *string `json:"access,omitempty"`     // S74: "read" | "write"
	Available  *bool   `json:"available,omitempty"`
	Q          *string `json:"q,omitempty"`
}

// AnyFilter reports whether the caller supplied at least one filter — the
// difference between an empty list that needs the table's sentence and an empty
// list that does not. It is a method on the params rather than a flag the seam
// could set, because only this layer saw the request.
func (f ToolsListFilter) AnyFilter() bool {
	return f.Family != nil || f.Source != nil || f.Capability != nil || f.Access != nil || f.Available != nil || f.Q != nil
}

// ToolsListResult is `tools.list`'s answer. `Tools` is [] and never null (the
// settings-list contract: a client that must nil-check an empty list renders
// "unknown" as an error).
type ToolsListResult struct {
	Tools     []ToolEntry `json:"tools"`
	Statement string      `json:"statement,omitempty"`
}

// ToolsGetParams are `tools.get`'s params: the one tool to read in full. An
// empty name is the seam's refusal (it knows the names and therefore the near
// ones), never a lookup of the empty string.
type ToolsGetParams struct {
	Name string `json:"name"`
}

// ToolsGetResult is one tool, the whole per-agent table for it, and the rules
// that speak about it. The agents table is every agent that EXISTS, granted or
// not — a table that listed only the granted ones would make the absence of a
// row carry the meaning "denied", which is the exact inversion of the fact
// (TC5's "who can call each and why not"). `Policies` comes from
// `toolcatalog.PoliciesTouching`, which spans `all-agents` rows and
// alias-authored spellings on purpose: the panel that shows the narrow allow and
// hides the broad deny is the panel that tells someone to delete the wrong rule.
type ToolsGetResult struct {
	Tool     ToolEntry       `json:"tool"`
	Agents   []AgentGrantRow `json:"agents"`
	Policies []PolicyRef     `json:"policies"`
	MCP      *ToolMCPLink    `json:"mcp,omitempty"`
}

// ToolsByAgentParams are `tools.byAgent`'s params: the agent to render. The
// subject is a name the person chose, not a caller identity — the caller is the
// peer at the endpoint and is never a param (rejectIdentityKeys).
type ToolsByAgentParams struct {
	Agent string `json:"agent"`
}

// ToolsByAgentResult is one agent's whole tool view: every tool that exists
// (system honest names, the wired conditionals, and the MCP tools this agent may
// see), each with its state and the engine's verbatim reason. `Dispatchable` is
// the SD7 fact, stated once at the top so a panel cannot render a green matrix
// for an agent no step of its own can run: when it is false every row is
// `not_granted`, and the `GrantedBy` lists survive, because the agent's row does
// name those tools and a panel that hid that would contradict the agent screen
// that put them there.
type ToolsByAgentResult struct {
	Agent        string          `json:"agent"`
	Dispatchable bool            `json:"dispatchable"`
	Tools        []AgentGrantRow `json:"tools"`
}

// ToolsSeedState is the `tools.core` seed half of `tools.status`: what the code
// offers, what the ledger says was applied, and the engine's own state word for
// the set. Read from `seed.status` for the one set (TC11's note 5) — the catalog
// does not keep a second ledger of what it seeded.
type ToolsSeedState struct {
	Set              string `json:"set"`
	AvailableVersion int    `json:"availableVersion"`
	AppliedVersion   int    `json:"appliedVersion"`
	State            string `json:"state"`
}

// ToolDriftEntry is one row's drift verdict (TC11): which tool, which state, and
// for `differs` the field names that disagree, in the applier's canonical order.
// Rendered as DATA, never as an error — an inert set that has not been applied
// yet is a healthy daemon, and a `differs` is a developer having changed a tool
// without bumping the record version, which the golden test already catches at
// `go test` time. What matters is that it is SAID.
type ToolDriftEntry struct {
	Name   string   `json:"name"`
	State  string   `json:"state"`
	Fields []string `json:"fields,omitempty"`
}

// ToolsCounts is the shape of the catalog in three numbers, split so a panel can
// say "3 of 71 are not wired in this build" without counting client-side.
// `ConditionalUnwired` counts entries that exist in code, are conditional, and
// are absent from `Registry()` right now — the honest "exists, cannot run today".
type ToolsCounts struct {
	System             int `json:"system"`
	MCP                int `json:"mcp"`
	ConditionalUnwired int `json:"conditionalUnwired"`
}

// ToolsStatusResult is `tools.status`' answer: the seed state, every drift entry
// (empty when the catalog is current — and an empty list here is the healthy
// case, which is why there is no statement field: nothing was filtered, and the
// absence of drift is not a claim that the check did not run), and the counts.
type ToolsStatusResult struct {
	Seed   ToolsSeedState   `json:"seed"`
	Drift  []ToolDriftEntry `json:"drift"`
	Counts ToolsCounts      `json:"counts"`
}

// --- the seam ----------------------------------------------------------------

// ToolsCatalog is the daemon adapter the four reads forward to. It is an
// interface rather than a func seam (the `MCPAdmin` shape) because the catalog doors
// answer four different questions with four different result shapes, and a
// string-dispatched func would put a switch over method names in a package whose
// registration table already dispatches.
//
// The adapter is `internal/daemon`'s (it holds the store, the policy set and the
// daemon's copy of the MCPAdmin func value); this package names no row, no
// column and no `internal/mcp` symbol (MC11), so deleting the MCP code and the
// entry point's seam files still leaves this building, with the doors inert
// when unwired.
//
// Every call reads rows and policies FRESH (N8: no cache). A cached catalog is
// the failure mode this whole slice argues against — a screen showing a grant the
// gate has already stopped honouring, or a rule an operator added a minute ago
// and cannot see. `TestToolsReadsAreLive` pins it: change a row, answer again,
// no restart and no pause.
type ToolsCatalog interface {
	// ToolsList answers `tools.list` with the filters already decoded. The
	// adapter does the matching (daemon-side, TC10) and returns [] when
	// nothing matches; the STATEMENT is this package's, not the seam's,
	// because only this layer saw whether a filter was supplied.
	ToolsList(ctx context.Context, f ToolsListFilter) ([]ToolEntry, error)

	// ToolsGet answers `tools.get`. An unknown name — including an empty one —
	// comes back as the typed ErrToolUnknown through err, with the nearest
	// names already in its `what` (only the seam reads the registry and the
	// rows, so only it can name them).
	ToolsGet(ctx context.Context, name string) (ToolsGetResult, error)

	// ToolsByAgent answers `tools.byAgent` for every tool that exists. An
	// unknown agent comes back as the typed ErrToolAgentUnknown naming the
	// known slugs — never an empty view, which would read as "that agent may
	// call nothing" about an agent that does not exist.
	ToolsByAgent(ctx context.Context, agent string) (ToolsByAgentResult, error)

	// ToolsStatus answers `tools.status`: the `tools.core` seed state from the
	// engine's own status, the drift report, and the counts. It writes nothing,
	// repairs nothing, and never calls the applier (TC11).
	ToolsStatus(ctx context.Context) (ToolsStatusResult, error)

	// ToolsVocabulary answers `tools.vocabulary` (S47 T10, TC3): the closed
	// capability vocabulary with the daemon's own one-line meanings, plus the
	// family, source and re-run-class spellings a filter may be offered. The
	// seam owns the answer because it owns the catalog the words describe — the
	// families it lists must be the families `ToolsList` can return, or a screen
	// built from this door would offer a filter that answers nothing.
	ToolsVocabulary(ctx context.Context) (ToolsVocabularyResult, error)
}

// --- registration -------------------------------------------------------------

// RegisterToolsCatalog registers the catalog reads when — and only when —
// `Table.ToolsCatalog` is wired: the RegisterSettings/RegisterSeed/RegisterMCPAdmin
// rule (V13: a nil seam is the configuration speaking, and the methods are
// ABSENT, not registered-and-refusing). `RequiresInitialize` true, `Debug` false
// — these are product surface for the person at the endpoint, and what keeps
// them off the narrow HTTP/MCP-stdio projection is the allow-list that never
// names them (T5's guard amendment walks that claim rather than trusting it).
// The five are T5's four plus T10's vocabulary door (the operator ruling of
// 2026-11-02 recorded at `MethodToolsVocabulary`).
// Returns whether the doors are in the table, so the daemon's wiring is asserted
// by a test and not trusted by a comment.
func (t *Table) RegisterToolsCatalog() bool {
	if t.ToolsCatalog == nil {
		return false
	}
	t.register(&Method{Name: MethodToolsList, RequiresInitialize: true, Handle: t.handleToolsList})
	t.register(&Method{Name: MethodToolsGet, RequiresInitialize: true, Handle: t.handleToolsGet})
	t.register(&Method{Name: MethodToolsByAgent, RequiresInitialize: true, Handle: t.handleToolsByAgent})
	t.register(&Method{Name: MethodToolsStatus, RequiresInitialize: true, Handle: t.handleToolsStatus})
	t.register(&Method{Name: MethodToolsVocabulary, RequiresInitialize: true, Handle: t.handleToolsVocabulary})
	t.register(&Method{Name: MethodToolsMatchPattern, RequiresInitialize: true, Handle: t.handleToolsMatchPattern})
	return true
}

// --- the one forwarding shape --------------------------------------------------

// toolsCatalogCall is `mcpAdminCall`'s forwarding discipline carried onto the
// four reads: dispatch by the method name BOUND AT REGISTRATION (never trusted
// from params), typed pass-through for the seam's own `*Error` (the nearest-names
// sentence is the seam's fact), and a plain error through as infrastructure.
//
// The nil branch is belt-only — registration is conditional and a nil seam never
// registers these names — but a seam can be ASSIGNED (or unassigned) after
// construction, and a table mutated underneath a registered method must answer
// with the wiring's own name rather than dereference nil (ErrNotWired, not a
// panic: a panic in a handler is the daemon answering nothing to everybody).
func (t *Table) toolsCatalogCall(ctx context.Context, call func(ToolsCatalog) (any, error)) (any, error) {
	if t.ToolsCatalog == nil {
		return nil, ErrNotWired.WithWhat("the tools catalog is not wired — the daemon started without the entry point's catalog seam (cmd/aeon wires Table.ToolsCatalog; a hand-built Table does not); `aeon serve` wires it, and until it does these doors are absent rather than refusing")
	}
	return call(t.ToolsCatalog)
}

// --- the four handlers ----------------------------------------------------------

func (t *Table) handleToolsList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodToolsList), params); err != nil {
		return nil, err
	}
	var f ToolsListFilter
	if err := decodeParams(params, &f); err != nil {
		return nil, err
	}
	raw, err := t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsList(ctx, f)
	})
	if err != nil {
		return nil, err
	}
	tools, ok := raw.([]ToolEntry)
	if !ok {
		return nil, ErrStoreUnavailable.WithWhat("the tools catalog seam returned a value that is not a tool list — the seam and this package disagree about the shape between them, which is a build fault, not a refusal")
	}
	if tools == nil {
		tools = []ToolEntry{}
	}
	return ShapeToolsListResult(f, tools), nil
}

// ShapeToolsListResult is the ONE place a `tools.list` answer is shaped (S47 T8):
// the nil-slice normalisation and the empty table's sentence. It is exported
// because the CLI's DIRECT door must produce the SAME answer offline — a second
// copy of "when does an empty list get a sentence" is exactly the drift the
// parity guard exists to catch, and the two doors would diverge on the one answer
// where they most need to agree (an empty list read three ways).
func ShapeToolsListResult(f ToolsListFilter, tools []ToolEntry) ToolsListResult {
	if tools == nil {
		tools = []ToolEntry{}
	}
	res := ToolsListResult{Tools: tools}
	// The table's sentence ONLY for a filtered-empty answer (the three states of
	// an empty list, policy.changes' discipline carried): unfiltered-empty would
	// claim the build contains no tools.
	if len(tools) == 0 && f.AnyFilter() {
		res.Statement = ToolsListNoMatchStatement
	}
	return res
}

func (t *Table) handleToolsGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodToolsGet), params); err != nil {
		return nil, err
	}
	var p ToolsGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsGet(ctx, p.Name)
	})
}

func (t *Table) handleToolsByAgent(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodToolsByAgent), params); err != nil {
		return nil, err
	}
	var p ToolsByAgentParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsByAgent(ctx, p.Agent)
	})
}

func (t *Table) handleToolsStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodToolsStatus), params); err != nil {
		return nil, err
	}
	// No params type exists because there are no params — decoding into an
	// empty struct still refuses a params member that is not an object
	// (`[1]`, `"x"`), which is the params' own shape fault and takes
	// JSON-RPC's -32602, not an Aeon code (mcp.vocabulary's argument carried:
	// the answer is a function of the compiled build, so a door that guessed
	// what a filter meant would be inventing a fact the code already settles).
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	raw, err := t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsStatus(ctx)
	})
	if err != nil {
		return nil, err
	}
	res, ok := raw.(ToolsStatusResult)
	if !ok {
		return nil, ErrStoreUnavailable.WithWhat("the tools catalog seam returned a value that is not a status result — a build fault between the seam and its doors, not a refusal")
	}
	if res.Drift == nil {
		res.Drift = []ToolDriftEntry{}
	}
	return res, nil
}
