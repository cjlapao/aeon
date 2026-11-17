# Design — S47 Tools catalog

Written 2026-10-30. Rulings **TC1–TC13**. TC2 and TC8 are flagged **product/scope calls**; the rest are documented technical calls checked against the code
(brief §1; T0 re-verifies). Do not re-open a ruling without recording the reason in that task's Notes.

## Verified facts (2026-10-30; T0 re-checks)

1. `tools.Registry()` (`internal/tools/toolset.go:116`) builds `map[string]Tool` each call (a function, deliberately — "a variable could be appended to from anywhere"). `Tool{Spec model.ToolSpec, Class RerunClass, Run func}` (`read.go:97`); `RerunClass ∈ safe|detectable|unsafe`, empty invalid (`class.go`). Conditional tools `search`/`graph` are added only while their resolver/service seam is wired (V13: absent, never present-and-empty); `write_file` is a hidden alias of `write` (`aliasWriteFile`); `ToolNames()` filters it; `CanonicalToolName` maps it. `TestRegistryExactNames` (`toolset_test.go:236`) pins the list and each tool-adding slice bumps it deliberately.
2. `DefaultGrant()` = `read, list, write`; `runtime.DefaultGrantNames` is its twin (a test asserts they agree). A step's toolset is `BuildToolset(granted, tc)` (`toolset.go:244`, called at `daemon/step.go:419`); an unknown grant is a construction **panic**, a missing tool at call time is an error naming what *is* available (O6).
3. Policy decides **per call**: `tool_access` rows, `policy.Evaluate(ps, Request{Type: ToolAccess, Agent, MCPServer, Tool}, at)`; deny over ask over allow, default deny (PL4/PL5); the matcher canonicalises names (`policy.SetToolNameCanonicalizer`).
4. `tools.grants {agent}` (S34 T2; `cmd/aeon/agent.go:560 toolGrantsHandleFor`) returns **only** MCP `GrantView`s for one agent and reads the MCP config file directly (S46 T3 moves that to rows). Core tools appear as "judged by the step's S11 policy gate".
5. The records catalog (`internal/records`, 30 specs today) is pure data: `Spec{Name, Method, Family, Description, Schema, Class, Mutates}` — already carries family, description, schema, class, mutates and a **socket twin**.
6. Core tools carry no capability data. There is no field saying `bash` spawns, `webfetch` egresses, `write` writes the workspace or `question` asks a person.
7. Protocol surface: ~160 `Method…` constants, ~154 explicit `register` calls plus the S38 `registerRecords` projection; `Method{Name, RequiresInitialize, Debug, Handle}`; `PublicMethods()` = not `Debug`; `narrow.Actions()` (9 entries, each with a mandatory `Cannot`); `protocol.AgentServiceMethods()` (7, `ReadOnly`/`Cannot`/`Emits`). No structural read/write marker exists (S35 T6's own comment).
8. `aeon handlers list` prints names only; there is no `aeon tools`.
9. After the sibling slices: S43-extra adds `agent.tools` (the allow-list as a column) and a read-only `seed_status` tool; S45 adds seven policy tools; S46 adds five MCP own-view/propose tools and `mcp_*` tables. All are catalog specs and appear here automatically; the pinned registry test counts them.

## TC1 — A catalog is read-only; disabling is policy

No create/update/delete/enable flag exists for tools. "Turn this tool off for that agent" is a `tool_access` **deny** row authored through S45 — one mechanism, one audit. The catalog **shows** that row (TC5) and **links** to the policy section; it never writes one. A guard test asserts no `tools.*` or `surface.*` method is a write and none exists on any non-read door (canaries).

## TC2 — The catalog entry is a record; the executor and schema are code (drift-checked) *(product call)*

CONFIGURATION-ORDER rule 6 asks for every record in the database. A tool has two parts with different natures: its **catalog entry** (name, family, description, class, capabilities, socket twin, origin, version) is something a person lists, reads and references → a **seeded record** (`tool` table, `tools.core` set, class inert); its **executor** (`Run`) and **input schema** are shape — editing either without the other breaks the tool → they stay **code**. The row stores a read-only copy of the schema and its SHA-256 so the catalog can show it and detect drift. **The model-facing spec the runtime offers still comes from `Registry()`** (code) — a deliberate, named exception to "dispatch reads the row": a stored schema that could disagree with the executor would be a second truth for the one thing that must not have two. A drift check (TC11) makes any disagreement visible; nothing auto-repairs silently. *Overrule means serving descriptions from the row too (an operator-tunable prompt surface) — a later slice with its own edit/validation story; the seed row already carries the field.*

## TC3 — Capabilities are declared in code, completeness-tested

New `internal/tools/meta.go`: a closed vocabulary — `workspace_read`, `workspace_write`, `spawn_process`, `network_egress`, `asks_human`, `org_records_read`, `org_records_write`, `starts_loop`, `index_read`, `graph_read` (T0 may add one, never an unreviewed free string) — and `Meta{Family string, Capabilities []Capability, Governed []string (policy types that decide it: tool_access always; command_access for bash; …), Notes string}` per tool name. **A registry name with no `Meta` fails a test** (and so does a `Meta` for a name that does not exist), so a new tool cannot ship without saying what it can do. Records-catalog tools derive `org_records_*` from `Spec.Mutates` (asserted consistent). Reading `Meta` changes no behaviour of any tool.

## TC4 — MCP tools are read from S46's tables, never duplicated

`tool` rows exist only for **system** tools (core, records/task, `run_start`, `search`, `graph`). MCP tools come from the assembled MCP config (S46 rows via the cmd/aeon seam — `internal/mcp` stays importable only by `cmd/aeon`, MC11) and appear in `tools.list` with `source:"mcp"`, `server`, qualified name, tier, `rerun`/`effect` classes and an **UNCLASSIFIED** flag (MC6). One truth per tool.

## TC5 — "To whom" is one pure function, tested against the gate

`internal/toolcatalog`: `Effective(tool, agent, Inputs) Entry` with `State ∈ not_granted | granted_allowed | granted_ask | granted_denied | granted_no_rule` and `Reason`. Inputs: the agent's **grant sources** (default grant; the agent row's `tools` allow-list; MCP `Core` narrowing and `Declares` through the seam) and the **policy set** evaluated with the *same* `policy.Evaluate` for `Request{Type: ToolAccess, Agent, Tool}` at an injected instant. `Reason` is the verdict sentence **verbatim** (D9, one formatter) or the MC10 "no allow" sentence. A tiny shared helper in `internal/tools` (`GrantedSet(granted []string) (names []string, err error)`, extracted from `BuildToolset`'s own logic and **called by it**) is the single definition of "what a grant list yields", so the catalog and the builder cannot disagree. The differential test (acceptance 3) walks every (tool, fixture agent) pair and compares to `policy.Evaluate` and to the toolset a real `BuildToolset` constructs. Agents that are not dispatchable (S43-extra SD7 custom agents) are listed with `dispatchable:false` and no grant state.

## TC6 — The surface is a closed, completeness-tested classification

New `internal/protocol/surface.go`: `var surface = map[MethodName]Surface{…}` with `Surface{Kind: read|write|control|diagnostic, Summary string (one line, required), HumanDoorOnly bool}`. **Every registered method has exactly one entry; a test fails on an unclassified method or an entry for a method that does not exist.** The **door flags are computed from the real structures, never typed into the table**: person socket (registered), `public` (`!Debug`), narrow (`narrow.Actions()` socket methods), agent services (`AgentServiceMethods()`), tool twin (records catalog `Method` → tool name). `HumanDoorOnly` is a declared assertion that a differential test proves equals "registered, absent from narrow, agent services, tool twins" (so S35/S45/S46's guards and this table cannot silently disagree). A differential test also checks the table's `Kind` against known facts (`Spec.Mutates`, `AgentServiceMethod.ReadOnly`, narrow `Kind`). The initial table is seeded from T0's inventory (the const comments in `method.go` are the summaries' source). The table lives in `internal/protocol` as data and adds **no** import.

## TC7 — The `tools.core` seed set (inert)

Records = one per system tool, `Content` = canonical JSON of `{name, family, description, schema, schema_hash, rerun_class, capabilities, governed, twin, aliases, conditional}`; `Key` = canonical tool name; `Version` per record guarded by a golden (`testdata/tools.versions.golden`, "content changed but version did not — bump it") exactly like S43-extra T4. Class **inert**: a catalog entry permits nothing. Applier `GuardAffecting()=false`; `Operator() = {Edit:false, Remove:false}` (a catalog row is not an operator's to change). Conditional tools (`search`, `graph`) are seeded always and flagged `conditional`; `available` is computed live from `Registry()`. The applier writes only the `tool` table.

## TC8 — The daemon's own surface is in scope (read-only) *(scope call)*

"What the app exposes" includes the API, not just the model-facing tools: `surface.list`/`surface.get` list every method with kind, summary and doors (TC6). It is **person-only**: not on the narrow surface (an agent enumerating the daemon's methods is reconnaissance with no use, N8), not an agent tool. *Overrule means dropping T6 and the Surface panel; nothing else depends on it.*

## TC9 — Agents see their own tools only

`tools_mine` and `tool_show` (catalog specs, twins `tools.mine.list/show`) return the caller's own granted tools with details and states; they refuse non-agent sessions (the S46 MG8 pattern) and never reveal another agent's private MCP tools or servers — success **and** error text, "unknown tool" hints drawn from the caller's view only. There is no write tool (TC1). Registered, **not** granted by default; counted bump.

## TC10 — Methods

All `RequiresInitialize`, none `Debug`, none on the narrow surface, conditional on the service (V13):
`tools.list {family?, source?, capability?, available?, q?}` → `{tools: ToolEntry[]}`; `tools.get {name}` → entry + the **per-agent table** + the policy rows that touch it (`policy.list`-shaped views with `Describe`) + for MCP the server link; `tools.byAgent {agent}` → that agent's whole tool view (core, records, MCP) with states and reasons; `tools.status {}` → drift report and seed state; `surface.list {kind?, door?, q?}`; `surface.get {method}`. Existing `tools.grants` is **unchanged**. `ToolEntry{name, aliases[], family, source system|mcp, description, schema, schemaHash, rerunClass, capabilities[], governed[], twin, conditional, available, server?, tier?, effect?, classified, version, origin, rowCurrent}`.

## TC11 — Drift is reported, never silently repaired

`tools.status` compares code (`Registry()`+`Meta`+records catalog, with schema hashes) to rows: `current`, `missing_row` (a tool in code with no row — e.g. a new tool before the seed ran), `retired` (a row whose tool is gone), `differs` (names the fields). Inert sets apply at start so a healthy daemon reports all `current`; a `differs` is a **bug signal** (a developer changed a tool without bumping the record version — the golden test catches it at `go test` time) and is shown, not auto-fixed.

## TC12 — The UI: Settings → Tools and → Surface

Descriptor-driven. **Tools:** a list with details and filters (family, source, capability, availability), capability pills (text + colour), the drift banner, a detail drawer (description, schema read-only, class, capabilities, governed-by policy types, socket twin, the policy rows that touch it each linking into the S45 section, MCP server link into S46) and the **per-agent matrix** (agents × state pills with the reason verbatim). **By agent:** pick an agent (`AgentPicker`) ⇒ that agent's whole tool view. **Surface:** a method list with kind, summary and door chips (person / narrow / agent services / tool), `human door only` badge, filters. Real Settings screen, not the bench.

## TC13 — What does not move

Tool execution, the registry's contents and `TestRegistryExactNames` (sibling slices bump it), `DefaultGrant`, the policy gate, MCP semantics (S12), S34's `tools.grants` shape, the narrow surface's action list, handler allow-lists (agent content), and every S43/S43-extra/S44/S45/S46 file.

## Open items left to T0 — mechanical

1. Re-verify facts 1–9; the exact current registry name list (honest names, conditionals, alias) and its count after whichever sibling slices have merged.
2. Where a step's `granted` list originates (`daemon/step.go` caller chain: handler allow-list, blueprint lane, `DefaultGrantNames` fallback) and which of those the catalog must model.
3. The full method inventory (every `Method…` const and `register` call, plus records twins) with the const comments for TC6's summaries; the differential facts available (records `Mutates`, agent-service `ReadOnly`, narrow `Kind`).
4. The capability vocabulary check: read each core tool's implementation and propose its `Meta` (T1 builds from this).
5. How to extract `GrantedSet` from `BuildToolset` without changing behaviour (characterize first).
6. Neighbour state (S43-extra engine/seams, S45 `Describe` and policy reads, S46 rows/seams, S44), the error-code block, handshake/conformance/fixture/protogen touch points.
7. Whether the S46 scoped MCP seam exposes the per-agent grant view the catalog needs, or T4 must add a read entry to it (in S47's own files).
