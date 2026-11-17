package protocol

// ════════════════════════════════════════════════════════════════════════════
// The person's MCP management reads (S46 T3, MG5-reads/MG8/MG11/MG12/MG13):
// `mcp.servers`, `mcp.server`, `mcp.grants`, `mcp.vocabulary`, `mcp.changes`.
//
// # What this file is
//
// Five READ-ONLY methods over the one `Table.MCPAdmin` func seam — the same
// one-seam-many-methods shape Table.MCP carries for mcp.status/mcp.check
// (S34) and Agents carries for the seven agent verbs. The seam holds all of
// it: the row reads, the probe-cache join, the near-names sentences, the
// vocabulary taken from the validator's own constants. THIS file holds no
// MCP logic and imports no `internal/mcp` — absent_test.go's banned list
// names this package explicitly (MC11), so the wire types below are plain
// Go types with json tags mirroring the wire shape, and deleting
// internal/mcp plus cmd/aeon's seam files leaves this package building with
// the five doors inert-when-unwired (V13: a nil seam leaves them ABSENT —
// RegisterMCPAdmin, the RegisterSeed/RegisterSettings rule).
//
// # MG8 — these are the PERSON's unscoped reads, never an agent's
//
// The management view is the human door's: the person at the 0600 endpoint
// sees every server, private ones included, because "the human sees
// everything" is what MG8 says the person is. The AGENT-SCOPED reads
// (`mcp_servers`, `mcp_grants`, `mcp_status`) are T8's tools and a DIFFERENT
// code path — they answer through Registry.ToolsFor(self)/GrantView(self)
// with the identity the daemon stamps, so an agent can never name another
// agent here. That separation is why no param below carries an agent
// identity for SELF-lookups: these reads take FILTERS a person chooses, and
// a test (T3's MC2 note) pins that no tool handler calls through these
// five names.
//
// # MG13 — names only, everywhere, including in the shape
//
// The transport summary carries `envNames` — the variable NAMES — and has
// no field where a VALUE could travel. That is the §4c rule made
// structural (a different type, not a redaction): a credential cannot leak
// through a shape that has nowhere to put one.
//
// # MG11 — the probe is a fact read, never a fact caused
//
// ServerView.status joins from the SAME probe cache `mcp.status` reads;
// nothing here dials a server, spawns a process, or refreshes anything.
// No probe row means the distinct word "unprobed" (MCPServerStatusUnprobed)
// — three-state honesty (P31): reachable, unreachable, and never-yet-asked
// are three answers, and collapsing the third is how "nobody checked" gets
// believed as "it works".
//
// # The sentences belong to the seam; this file adds no interpretation
//
// The handlers forward (mcpCall's discipline): a typed *Error returned by
// the seam in `err` travels UNTOUCHED (ErrMcpUnknown's near-names sentence
// is the seam's — only the seam knows which names exist, and a paraphrase
// here would be a second authority over the fact), a non-empty refusal
// string becomes ErrMcpRefused with the seam's words verbatim in `what`,
// and a plain error is infrastructure and passes through as Dispatch's
// internal error (DB19's discrimination). Params never carry an actor or
// any identity key — rejectIdentityKeys refuses the bag that names one, the
// package's own stand-in for strict decoding (unknown-but-innocent fields
// stay ignored: T3's additive-evolution rule, pinned by
// TestNothingCallsDisallowUnknownFields).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
)

// MCPServerStatusUnprobed is the distinct third state of ServerView.Status:
// the probe cache holds NO row for this server — nobody has probed it (the
// status was never caused), which is a different fact from a probe that
// answered reachable or unreachable (MG11, P31). The probed states are the
// seam's words, joined from the same cache `mcp.status` reads; only the
// absence-state is pinned here, because the protocol owns the claim "this
// door never probes" and pins the word that proves it.
const MCPServerStatusUnprobed = "unprobed"

// MCPChangesNoChangesStatement is the EMPTY TABLE's sentence for
// mcp.changes — pinned byte for byte where the wire shape is defined (the
// PolicyChangesEmptyStatement discipline; T5/T8's CLI twin prints the
// identical line and a test pins the two surfaces). It rides ONLY the
// provably-empty audit: a filtered-empty read prints an empty list and NO
// statement, because "my filter matched nothing" is the filter's answer and
// the empty-table sentence is the table's — printing it beside a full audit
// would claim no configuration was ever changed (MG11's honesty, carried
// from the authoring audit onto this one).
const MCPChangesNoChangesStatement = "no MCP configuration changes recorded."

// --- the wire types -----------------------------------------------------------

// MCPTransportSummary is a server's transport as the management view
// renders it: enough to identify and audit the mount, never enough to
// carry a credential. EnvNames are variable NAMES only (MG13/C17 — the
// values live in the daemon's environment and the shape has no field for
// one, so leaking is not a forgotten call site but a compile failure).
type MCPTransportSummary struct {
	Type    string   `json:"type"` // "stdio" | "http" — the validator's own set
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	// EnvNames is the stdio mount's environment variable names — names,
	// never values (MG13). A value-shaped string could only arrive from a
	// seam that broke its contract; the rows themselves cannot hold one
	// (the migration's grammar refuses it).
	EnvNames []string `json:"envNames,omitempty"`
	// EnvFrom is which stored variable each env name is bound to — variable NAMES,
	// never values. A name missing from it is an unbound legacy one.
	EnvFrom map[string]string `json:"envFrom,omitempty"`
}

// MCPToolView is one declared tool with its classes and whether the
// classification is COMPLETE (MG5: `classified` distinguishes a declared
// class from a complete classification — an unclassified tool exists but is
// refused at call time, MC6, and the management view must show that
// difference rather than a green row).
type MCPToolView struct {
	Name       string `json:"name"`
	Rerun      string `json:"rerun,omitempty"`  // "", "safe" | "detectable" | "unsafe"
	Effect     string `json:"effect,omitempty"` // "", "local" | "outward" | "irreversible"
	Classified bool   `json:"classified"`
}

// MCPServerView is one server as `mcp.servers` and `mcp.server` render it —
// the row plus the two joined facts (declaredBy's count and the probe
// cache's status), assembled by the seam and typed (not derived) here.
type MCPServerView struct {
	Name string `json:"name"`
	// Title is the friendly name the app shows and Description what the server is
	// for; Name stays the slug every reference (tool addresses, grants) uses. Both
	// are display text and absent when none was given.
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Tier is "public" | "private" — derived, never stored, exactly as
	// mcp.status's server wire row spells it (S34 T3's shape carried).
	Tier string `json:"tier"`
	// Agent is the owning slug for a private server; absent for a public
	// one (the same fact the tier already states, so absence is honest —
	// an empty string would name nobody with a value).
	Agent     string              `json:"agent,omitempty"`
	Transport MCPTransportSummary `json:"transport"`
	// Tools is [] when the server declares none, never null — the
	// settings-list contract carried (a client that must nil-check an
	// empty list is a client that renders "unknown" as an error).
	Tools      []MCPToolView `json:"tools"`
	Enabled    bool          `json:"enabled"`
	Origin     string        `json:"origin"` // "system" | "user" | "import" — the row's provenance
	DeclaredBy int           `json:"declaredBy"`
	// Shared is true when every agent declares this public server's tools.
	Shared bool `json:"shared,omitempty"`
	// Status joins from the SAME probe cache `mcp.status` reads; the
	// distinct absence-state word is MCPServerStatusUnprobed. A read here
	// never probes (MG11) — absence of the field means the seam had no
	// probe view to join at all, which is the seam's honesty, not this
	// door's.
	Status    string `json:"status,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

// MCPServersParams are `mcp.servers`' filters. Every filter is a POINTER so
// the three readings stay distinct: absent (no opinion), present-and-empty
// (a filter that matches nothing — refused by the seam as nonsense if that
// is what it would answer), present (a real filter). These are the
// PERSON's filters over the whole table (MG8: the management view is
// unscoped); they name shape and provenance, never a caller identity —
// rejectIdentityKeys refuses any bag that carries one.
type MCPServersParams struct {
	Tier    *string `json:"tier,omitempty"`    // "public" | "private"
	Agent   *string `json:"agent,omitempty"`   // owning slug — "what exists for/under this agent"
	Enabled *bool   `json:"enabled,omitempty"` // the enabled/disabled split, filterable
	Origin  *string `json:"origin,omitempty"`  // "system" | "user" | "import"
}

// MCPServersResult is `mcp.servers`' answer: the servers matching the
// filters, each with its details (the list that was a CLI table becomes a
// row-complete read; MG5's "list WITH details"). [] when nothing matches —
// never null.
type MCPServersResult struct {
	Servers []MCPServerView `json:"servers"`
}

// MCPServerParams are `mcp.server`'s params: the one server to read in
// full. Name is required (an empty name is refused by the seam as
// nonsense — a door that guessed the nearest name would answer for a
// server nobody asked about).
type MCPServerParams struct {
	Name string `json:"name"`
}

// MCPServerResult is `mcp.server`'s answer: the full view, the agent SLUGS
// whose declares name one of its tools (the count's expansion — MG5's
// "everything"), and the server's last 20 audit rows (newest first; the
// same rows mcp.changes answers filtered to this server). An unknown name
// REFUSES with the typed ErrMcpUnknown naming the nearest names — the
// seam's sentence, because only the seam knows the names (never a bare
// not-found, never an empty success).
type MCPServerResult struct {
	Server     MCPServerView   `json:"server"`
	DeclaredBy []string        `json:"declaredBy"`
	Changes    []MCPChangeView `json:"changes"`
}

// MCPGrantView is one row of the authority view — S12's GrantView VERBATIM
// (MG7: the grants view IS that view, a second derivation is the bug this
// slice exists to prevent), plus the withheld rows' missing classes as the
// optional fifth member. MissingClasses is a POINTER for the Refusal
// distinction (registry.go's own comment): nil is "this row's refusal is
// not about classification", an empty slice would be "nothing is missing"
// — two different facts a plain slice cannot tell apart.
type MCPGrantView struct {
	Qualified      string    `json:"qualified"`
	Tier           string    `json:"tier"`
	State          string    `json:"state"` // "available" | "unavailable" | "refused" | "ask"
	Reason         string    `json:"reason,omitempty"`
	MissingClasses *[]string `json:"missingClasses,omitempty"`
}

// MCPGrantsParams are `mcp.grants`' params: the ONE agent whose view to
// read. Required (MG8's discipline: the view is per-agent, and a request
// naming nobody would be asking for the whole roster, which is the person's
// door for — `mcp.servers` spans everything already). An unknown agent
// REFUSES with the typed ErrMcpUnknown naming the known agents — the seam's
// sentence, the same shape tools.grants' door refused with, now typed.
type MCPGrantsParams struct {
	Agent string `json:"agent"`
}

// MCPGrantsResult is `mcp.grants`' answer for one agent: the S12
// GrantView list plus the two facts that explain it — the core NARROWING
// and the declarations (MG5: the grants list is decided by all three, and
// showing it without them is the UI silently combining two facts, MG7's
// forbidden shape).
type MCPGrantsResult struct {
	Agent string `json:"agent"`
	// Core is the core-narrowing list: NULL is "no narrowing — the whole
	// core set" and [] is "core narrowed to nothing" — two distinct facts
	// (the mcp_agent.core column's own NULL-vs-'[]' discipline, carried to
	// the wire; the settings' *string rule's lesson: the null must be
	// honest). This is a plain slice because Go's nil slice marshals to
	// JSON null and an empty one to [] — the seam owns which it fills.
	Core []string `json:"core"`
	// Declares is every qualified name this agent declared (MC5: declaring
	// is the grant; usability still needs S10's allow — MG7's two facts,
	// shown separately, never combined). [] when the agent declares
	// nothing, never null.
	Declares []string       `json:"declares"`
	Grants   []MCPGrantView `json:"grants"`
}

// MCPGrammar is the name grammars as REGEXP STRINGS — taken by the seam
// from the validator's own variables (the exported
// mcp.ServerNameRE/ToolNameRE/EnvNameRE spellings). Typed here as strings
// on purpose: a client that compiled these into regexes tests the SAME
// rules the daemon validates with (policy.vocabulary's rule carried: a UI
// that guesses a grammar drifts from it; served the grammar, an offered
// value is an accepted value).
type MCPGrammar struct {
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	EnvName string `json:"envName"`
}

// MCPVocabularyResult is `mcp.vocabulary`'s answer: the closed sets —
// transports, re-run classes, effect classes — and the name grammars, all
// taken by the seam from the code that ENFORCES them (mcp's constants and
// internal/tools' re-run classes), never restated in this package or any
// client. No params: the answer is a function of the compiled code, and
// refusing it with ErrNotWired-shaped excuses would claim the daemon
// cannot state facts it ships in its own binary (policy.vocabulary's
// argument, carried).
type MCPVocabularyResult struct {
	Transports []string   `json:"transports"` // "stdio" | "http"
	Rerun      []string   `json:"rerun"`      // "safe" | "detectable" | "unsafe" (internal/tools', W16)
	Effect     []string   `json:"effect"`     // "local" | "outward" | "irreversible"
	Grammars   MCPGrammar `json:"grammars"`
}

// MCPChangeView is one `mcp_change` audit row on the wire — the columns of
// the table, field for field (the ProposalRow/PolicyChangeView mirror
// convention). server, agent and qualified are absent where the table
// holds NULL: the op decides which facts exist (a core_set names the agent
// and no server; a server_add names the server and no qualified tool).
// summary is the fixed-template sentence the writer built from names and
// ids ONLY (MG13: no value can be in the audit because no value can enter
// the template), printed as it is — this file assembles nothing.
type MCPChangeView struct {
	ID         int64  `json:"id"`
	Op         string `json:"op"`
	Server     string `json:"server,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Qualified  string `json:"qualified,omitempty"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

// MCPChangesParams are `mcp.changes`' filters: the server, the agent, and
// the depth. Pointers again for the absent-vs-empty distinction; limit
// passes through to the seam, which owns the clamp and the default (one
// definition, two doors — policy.changes' rule).
type MCPChangesParams struct {
	Server *string `json:"server,omitempty"`
	Agent  *string `json:"agent,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
}

// MCPChangesResult is `mcp.changes`' answer: the newest-first rows, and
// the EMPTY TABLE's sentence when (only when) the audit holds no rows at
// all — MCPChangesNoChangesStatement. Filtered-empty prints the empty list
// and NO statement (the header's three states, policy.changes' discipline
// carried: the filter's emptiness is the filter's answer).
type MCPChangesResult struct {
	Changes   []MCPChangeView `json:"changes"`
	Statement string          `json:"statement,omitempty"`
}

// --- registration -------------------------------------------------------------

// RegisterMCPAdmin registers the five management reads when — and only
// when — Table.MCPAdmin is wired: the RegisterSettings/RegisterSeed pattern
// (V13: a nil seam is the configuration speaking, and the methods are
// ABSENT, not registered-and-refusing — a daemon without the seam answers
// ErrUnknownMethod, the same absence a client can tell apart from a door
// that judged and declined). RequiresInitialize true, Debug false (product
// surface: what keeps these off the narrow projection is the allow-list
// that never names them, never an accident). Returns whether the doors are
// in the table, so the daemon's wiring is asserted, not trusted.
func (t *Table) RegisterMCPAdmin() bool {
	if t.MCPAdmin == nil {
		return false
	}
	t.register(&Method{Name: MethodMCPServers, RequiresInitialize: true, Handle: t.handleMCPServers})
	t.register(&Method{Name: MethodMCPServer, RequiresInitialize: true, Handle: t.handleMCPServer})
	t.register(&Method{Name: MethodMCPGrants, RequiresInitialize: true, Handle: t.handleMCPGrants})
	t.register(&Method{Name: MethodMCPVocabulary, RequiresInitialize: true, Handle: t.handleMCPVocabulary})
	t.register(&Method{Name: MethodMCPChanges, RequiresInitialize: true, Handle: t.handleMCPChanges})
	t.register(&Method{Name: MethodMCPProcesses, RequiresInitialize: true, Handle: t.handleMCPProcesses})
	t.register(&Method{Name: MethodMCPProcessLogs, RequiresInitialize: true, Handle: t.handleMCPProcessLogs})
	return true
}

// --- the one forwarding shape --------------------------------------------------

// mcpAdminCall is mcpCall's forwarding shape carried onto the five reads:
// dispatch by the method name BOUND AT REGISTRATION (never trusted from
// params), typed pass-through for the seam's own *Error, refusal strings
// verbatim in ErrMcpRefused's `what`, plain errors through as
// infrastructure. The nil-seam branch is belt-only — registration is
// conditional and a nil seam never registers these names — but the seam
// can be ASSIGNED after construction, so a table mutated underneath a
// registered method must still answer with the wiring's own name rather
// than dereferencing nil (ErrNotWired, not a panic).
func (t *Table) mcpAdminCall(ctx context.Context, name MethodName, params json.RawMessage) (any, error) {
	if t.MCPAdmin == nil {
		return nil, ErrNotWired.WithWhat("the MCP management reads are not wired — the daemon started without the entry point's MCPAdmin seam (cmd/aeon wires Table.MCPAdmin; a hand-built Table does not); `aeon serve` wires it, and until it does these doors are absent rather than refusing")
	}
	result, refusal, err := t.MCPAdmin(ctx, string(name), params)
	if err != nil {
		return nil, err // a *Error travels untouched (ErrMcpUnknown's near-names sentence is the seam's); a plain error is infrastructure
	}
	if refusal != "" {
		return nil, ErrMcpRefused.WithWhat("%s", refusal)
	}
	return result, nil
}

// --- the five handlers ----------------------------------------------------------

func (t *Table) handleMCPServers(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPServers), params); err != nil {
		return nil, err
	}
	var p MCPServersParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPServers, params)
}

func (t *Table) handleMCPServer(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPServer), params); err != nil {
		return nil, err
	}
	var p MCPServerParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPServer, params)
}

func (t *Table) handleMCPGrants(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPGrants), params); err != nil {
		return nil, err
	}
	var p MCPGrantsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPGrants, params)
}

func (t *Table) handleMCPVocabulary(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPVocabulary), params); err != nil {
		return nil, err
	}
	// No params type exists because there are no params — decoding into an
	// empty struct still refuses a params member that is not an object
	// (`[1]`, `"x"`), which is the params' own shape fault, -32602.
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPVocabulary, params)
}

func (t *Table) handleMCPChanges(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPChanges), params); err != nil {
		return nil, err
	}
	var p MCPChangesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPChanges, params)
}

// ════════════════════════════════════════════════════════════════════════════
// mcp.importFile — S46 T4's HUMAN door (MG3, MG2, MG13): deliberately adopt
// a legacy mcp.json into the tables.
//
// The startup half of MG3 is automatic and silent-to-a-log-line (cmd/aeon's
// adoptLegacy — it runs once, all-or-nothing, and the file is never touched
// again); this method is the DOOR the person chooses to walk through, and it
// shares the importer function with the startup path (one implementation,
// two callers — Q1's rule carried from S34). Two modes: "dry-run" answers
// the diff and writes NOTHING (not even a ledger row — a plan is not a
// fact); "replace" replaces ALL mcp_* configuration rows in one transaction
// (never a merge, MG3: the tables are the truth and an import overwrites
// them atomically or not at all) with the audit and ledger rows the write
// doors owe.
//
// The actor is the peer credential, never a param — rejectIdentityKeys
// refuses an actor key as a shape error (T5's rule, exercised here first
// because this is the first MCP WRITE door), and this handler is the one
// place the session's identity enters: Table.ActorFor stamps it into the
// context (WithMCPActor below) and the seam reads it back (MCPActor). A
// connection that resolves to no peer (uid 0 — the transport never
// recorded a peer) is refused with ErrMcpNoActor rather than stamped with a
// placeholder: an unattributable configuration write is the audit row that
// cannot name who widened what agents may do.
// ════════════════════════════════════════════════════════════════════════════

// McpImportFileParams is the door's whole input. Path defaults to the
// legacy path (resolved by the seam — this package names no filesystem);
// when given it must be absolute and a regular file, which the SEAM checks
// (the filesystem is the entry point's, not the protocol's). Mode has no
// default: "dry-run" and "replace" are different acts, and guessing which
// one a human meant is how a plan becomes a mutation.
type McpImportFileParams struct {
	Path *string `json:"path,omitempty"`
	Mode string  `json:"mode"`
}

// McpImportDiff is the dry-run answer — sets, not deltas: what the file
// would ADD and what it would REMOVE against the rows as they stand. Every
// member is a NAME (server, server.tool, key-qualified declaration);
// EnvNames carries env variable NAMES and no field here can hold a value
// (MG13 made structural again — the same shape rule as TransportSummary).
type McpImportDiff struct {
	ServersAdded    []string `json:"serversAdded"`
	ServersRemoved  []string `json:"serversRemoved"`
	ToolsAdded      []string `json:"toolsAdded"`
	ToolsRemoved    []string `json:"toolsRemoved"`
	DeclaresAdded   []string `json:"declaresAdded"`
	DeclaresRemoved []string `json:"declaresRemoved"`
	EnvNames        []string `json:"envNames"`
}

// McpAffectedGrants is one agent's before/after around a replace — the
// grants fact the human is about to change, shown for EVERY agent the
// replace touches (MG2's visibility rule for writes).
type McpAffectedGrants struct {
	Agent  string   `json:"agent"`
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// McpImportFileResult is the one result shape both modes answer. Outcome
// carries the importer's word (no-file, already-imported, skipped, refused,
// imported, replaced, dry-run); Detail is the validator's own sentence on a
// refusal (the seam adds no interpretation — MG11's discipline carried to
// imports), and on success the counts state what landed. Diff travels only
// on dry-run; Affected only on replace.
type McpImportFileResult struct {
	Mode     string              `json:"mode"`
	Outcome  string              `json:"outcome"`
	Path     string              `json:"path"`
	Servers  int                 `json:"servers"`
	Tools    int                 `json:"tools"`
	Declares int                 `json:"declares"`
	Detail   string              `json:"detail,omitempty"`
	Diff     *McpImportDiff      `json:"diff,omitempty"`
	Affected []McpAffectedGrants `json:"affected,omitempty"`
}

// --- the actor channel (the seam reads what the handler stamped) ----------

type mcpActorCtxKey struct{}

// WithMCPActor stamps the person behind an MCP write door into the context.
// It is exported for the dispatch layer (and the CLI's direct door, which
// stamps the kernel's uid the same honest way); handlers, not params, are
// the only source of identity.
func WithMCPActor(ctx context.Context, kind, id string) context.Context {
	return context.WithValue(ctx, mcpActorCtxKey{}, Actor{Kind: kind, ID: id})
}

// MCPActor reads the stamp back. ok=false means NOBODY pressed this door
// from the protocol's point of view, and a write seam must refuse.
func MCPActor(ctx context.Context) (kind, id string, ok bool) {
	a, ok := ctx.Value(mcpActorCtxKey{}).(Actor)
	if !ok || a.Kind == "" || a.ID == "" {
		return "", "", false
	}
	return a.Kind, a.ID, true
}

// --- registration + handler --------------------------------------------------

// RegisterMCPImportFile registers the human import door. It is a SEPARATE
// conditional from RegisterMCPAdmin's five reads on purpose: the reads are
// the person's passive view (unscoped, no actor, MG8) and register from a
// store handle alone, while the write door additionally requires the
// runtime to be able to name a peer (ActorFor). A daemon that wired the
// seam but not the actor stamp still gets the five reads and answers this
// door absent — absent, never a refusing half-wired write (V13).
func (t *Table) RegisterMCPImportFile() bool {
	if t.MCPAdmin == nil || t.ActorFor == nil {
		return false
	}
	t.register(&Method{Name: MethodMCPImportFile, RequiresInitialize: true, Handle: t.handleMCPImportFile})
	return true
}

func (t *Table) handleMCPImportFile(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPImportFile), params); err != nil {
		return nil, err
	}
	var p McpImportFileParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The stamp runs HERE, at the dispatch edge, where the session still
	// exists — the seam below sees a context, never a Session.
	actor := t.ActorFor(ctx, sess)
	if actor.Kind == "" || actor.ID == "" || actor.ID == "0" {
		return nil, ErrMcpNoActor.WithWhat("the MCP import needs a person, and this connection resolved to no peer identity (uid 0) — the actor is the kernel's peer credential, never a param, and an unresolvable peer is refused rather than stamped \"user 0\"")
	}
	return t.mcpAdminCall(WithMCPActor(ctx, actor.Kind, actor.ID), MethodMCPImportFile, params)
}

// --- S46 T5: the human write doors ----------------------------------------------------

// mcpIdentityKeys are the param keys an MCP write door refuses as a shape
// error: the actor, the origin, the id and every timestamp are stamped by
// the daemon from the connection and the transaction — a caller that names
// any of them is forging the fact the audit exists to keep. The list is
// MCP's OWN (the policy-authoring list stays S45's — `id` means proposal.id
// there, and the two lists must not grow each other).
var mcpIdentityKeys = []string{
	"actor", "actorKind", "actor_kind", "actorId", "actor_id",
	"origin", "id", "status", "createdBy", "created_by", "acceptedBy", "accepted_by",
	"createdAt", "created_at", "updatedAt", "updated_at", "occurredAt", "occurred_at",
}

// rejectMCPIdentityKeys mirrors rejectIdentityKeys exactly (ErrBadParams,
// the same sentence shape) over the MCP list. The protocol package's own
// law — no DisallowUnknownFields — stands: the refused set is NAMED, and
// the named set is exactly the identity (MG5's params rule).
func rejectMCPIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope with its own error, in its own words
	}
	for _, key := range mcpIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor, the origin and the timestamps are stamped by the daemon, never named by the caller (MG5)",
				method, key)}
		}
	}
	return nil
}

// MCPTransportInput and MCPToolInput are the params' halves of a server —
// the same shape the validator parses from the legacy file (one grammar,
// MG13: Env holds NAMES, the type makes a value impossible to express).
type MCPTransportInput struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	URL     string   `json:"url,omitempty"`
	// EnvFrom binds an env NAME to a stored variable of the variable manager.
	// Every env name needs exactly one of EnvFrom or EnvValues: an environment
	// variable cannot be empty.
	EnvFrom map[string]string `json:"envFrom,omitempty"`
	// EnvValues is a value typed for an env name. It is WRITE-ONLY: the door stores
	// it as a hidden secret in the variable manager under an MCP_<SERVER>_<NAME>
	// name and keeps only the binding — it is never stored here, never echoed,
	// never in an audit row, and a proposal payload cannot carry one.
	EnvValues map[string]string `json:"envValues,omitempty"`
}

type MCPToolInput struct {
	Name   string `json:"name"`
	Rerun  string `json:"rerun,omitempty"`
	Effect string `json:"effect,omitempty"`
}

// MCPServerInspectParams is the candidate transport an inspect runs. The same
// shape an add carries: a command and its argument list, NAMES of environment
// variables (values come from the daemon's environment, never the wire), or a
// URL. No name, tier or tools — nothing is being added.
type MCPServerInspectParams struct {
	Transport MCPTransportInput `json:"transport"`
	// UseVariables hands the command the VALUES of the env names it declares, taken
	// from the variable manager. Off by default: an inspect runs a command a person
	// just typed, so a stored secret must be asked for by name and on purpose — the
	// manager never shows a secret back, and this is the one road that would.
	UseVariables bool `json:"useVariables,omitempty"`
}

// MCPInspectToolAnnotations is the server's own report about a tool — hints,
// never a classification (the registry's rule: only OUR configuration
// classifies a tool).
type MCPInspectToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// MCPInspectTool is one tool as the live server described itself: name,
// description and the input schema it accepts.
type MCPInspectTool struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	InputSchema json.RawMessage            `json:"inputSchema,omitempty"`
	Annotations *MCPInspectToolAnnotations `json:"annotations,omitempty"`
}

// MCPServerInspectResult is what `mcp.server.inspect` answers: who the server
// says it is, the protocol revision it speaks, how long the run took, and every
// tool it listed.
type MCPServerInspectResult struct {
	ServerName      string           `json:"serverName"`
	ServerVersion   string           `json:"serverVersion"`
	ProtocolVersion string           `json:"protocolVersion"`
	Ms              int64            `json:"ms"`
	Tools           []MCPInspectTool `json:"tools"`
}

type MCPServerAddParams struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Tier        string            `json:"tier"`
	Agent       string            `json:"agent,omitempty"` // slug; required for private, forbidden for public
	Transport   MCPTransportInput `json:"transport"`
	Tools       []MCPToolInput    `json:"tools,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"` // absent = true (MC3: presence = enabled)
}

type MCPServerUpdateParams struct {
	Name string `json:"name"`
	// Title and Description replace the display text when present (pointers: absent
	// keeps it, present and empty clears it).
	Title       *string            `json:"title,omitempty"`
	Description *string            `json:"description,omitempty"`
	Transport   *MCPTransportInput `json:"transport,omitempty"` // whole-field replacement of the provided sections
	Tools       *[]MCPToolInput    `json:"tools,omitempty"`     // pointer: absent keeps tools, present replaces them whole
}

type MCPServerRemoveParams struct {
	Name            string `json:"name"`
	CascadeDeclares bool   `json:"cascadeDeclares,omitempty"`
}

type MCPServerSetEnabledParams struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// MCPServerShareParams share a public server with every agent (Shared true) or
// stop sharing it. Sharing declares the server's classified tools for all
// agents, now and later; it grants nothing — a policy allow still decides.
type MCPServerShareParams struct {
	Name   string `json:"name"`
	Shared bool   `json:"shared"`
}

type MCPGrantDeclareParams struct {
	Agent     string `json:"agent"`
	Qualified string `json:"qualified"`
}

type MCPGrantRevokeParams struct {
	Agent     string `json:"agent"`
	Qualified string `json:"qualified"`
}

type MCPCoreSetParams struct {
	Agent string    `json:"agent"`
	Core  *[]string `json:"core"` // null = the WHOLE core set; [] = none — two different facts (MC)
}

// MCPAgentGrants is the before/after a write produced for one agent — the
// answer that lets the person see what the write GRANTED without trusting
// the write's own summary.
type MCPAgentGrants struct {
	Agent  string         `json:"agent"`
	Before []MCPGrantView `json:"before"`
	After  []MCPGrantView `json:"after"`
}

// MCPWriteResult is the shape every write door answers (MG5): the updated
// server view when the write is about a server, the before/after grants for
// every affected agent, changed (false for an idempotent no-op — and a
// no-op writes NO audit row), warnings in fixed wording (MC6 unclassified,
// MC10 the declare-is-not-a-permission sentence), and the cascade's revoke
// list when a removal took declarations with it.
type MCPWriteResult struct {
	Server   *MCPServerView   `json:"server,omitempty"`
	Grants   []MCPAgentGrants `json:"grants"`
	Changed  bool             `json:"changed"`
	Warnings []string         `json:"warnings,omitempty"`
	Revoked  []string         `json:"revoked,omitempty"`
}

// RegisterMCPWrites binds the eight human doors (the seven writes and inspect). Conditional on the
// seam (V13 — a nil seam leaves them ABSENT, never refusing); the actor
// stamp is checked at the dispatch edge (an unresolvable peer is refused
// per call — the door stands only for a person at the endpoint).
func (t *Table) RegisterMCPWrites() bool {
	if t.MCPAdmin == nil {
		return false
	}
	for _, m := range []struct {
		name MethodName
		fn   func(context.Context, *Session, json.RawMessage) (any, error)
	}{
		{MethodMCPServerAdd, t.handleMCPServerAdd},
		{MethodMCPServerUpdate, t.handleMCPServerUpdate},
		{MethodMCPServerRemove, t.handleMCPServerRemove},
		{MethodMCPServerSetEnabled, t.handleMCPServerSetEnabled},
		{MethodMCPServerShare, t.handleMCPServerShare},
		{MethodMCPServerInspect, t.handleMCPServerInspect},
		{MethodMCPProcessStart, t.mcpProcessControl(MethodMCPProcessStart)},
		{MethodMCPProcessStop, t.mcpProcessControl(MethodMCPProcessStop)},
		{MethodMCPProcessRestart, t.mcpProcessControl(MethodMCPProcessRestart)},
		{MethodMCPGrantDeclare, t.handleMCPGrantDeclare},
		{MethodMCPGrantRevoke, t.handleMCPGrantRevoke},
		{MethodMCPCoreSet, t.handleMCPCoreSet},
	} {
		name, handler := m.name, m.fn
		t.register(&Method{Name: name, RequiresInitialize: true, Handle: handler})
	}
	return true
}

// mcpWriteEdge is the shared front of the seven handlers: the identity-key
// refusal, the params decode, and the PEER STAMP — where the Session still
// exists. The actor never arrives from params (the refusal above is exactly
// that), and an unresolvable peer (uid 0, or no ActorFor wired at all) is
// refused here so the seam below never runs for a write nobody owns.
func (t *Table) mcpWriteEdge(ctx context.Context, sess *Session, name MethodName, params json.RawMessage, into any) (context.Context, error) {
	if err := rejectMCPIdentityKeys(string(name), params); err != nil {
		return nil, err
	}
	if err := decodeParams(params, into); err != nil {
		return nil, err
	}
	if t.ActorFor == nil {
		return nil, ErrMcpNoActor.WithWhat("the MCP write needs a person, and this runtime wired no actor stamp at all — the door is registered but unattributable, which is refused, not half-open")
	}
	actor := t.ActorFor(ctx, sess)
	if actor.Kind == "" || actor.ID == "" || actor.ID == "0" {
		return nil, ErrMcpNoActor.WithWhat("the MCP write needs a person, and this connection resolved to no peer identity (uid 0) — the actor is the kernel's peer credential, never a param, and an unresolvable peer is refused rather than stamped \"user 0\"")
	}
	return WithMCPActor(ctx, actor.Kind, actor.ID), nil
}

func (t *Table) handleMCPServerAdd(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerAddParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerAdd, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerAdd, params)
}

// handleMCPServerInspect forwards a candidate transport to the seam, which runs
// it. It is a human door like the writes beside it — it starts a process on the
// person's behalf, so the peer stamp is required before anything runs.
func (t *Table) handleMCPServerInspect(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerInspectParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerInspect, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerInspect, params)
}

func (t *Table) handleMCPServerUpdate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerUpdateParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerUpdate, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerUpdate, params)
}

func (t *Table) handleMCPServerRemove(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerRemoveParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerRemove, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerRemove, params)
}

func (t *Table) handleMCPServerSetEnabled(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerSetEnabledParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerSetEnabled, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerSetEnabled, params)
}

func (t *Table) handleMCPServerShare(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPServerShareParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPServerShare, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPServerShare, params)
}

func (t *Table) handleMCPGrantDeclare(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPGrantDeclareParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPGrantDeclare, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPGrantDeclare, params)
}

func (t *Table) handleMCPGrantRevoke(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPGrantRevokeParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPGrantRevoke, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPGrantRevoke, params)
}

func (t *Table) handleMCPCoreSet(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p MCPCoreSetParams
	c, err := t.mcpWriteEdge(ctx, sess, MethodMCPCoreSet, params, &p)
	if err != nil {
		return nil, err
	}
	return t.mcpAdminCall(c, MethodMCPCoreSet, params)
}
