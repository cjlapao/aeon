# Design — S46 MCP management

Written 2026-10-30. Rulings **MG1–MG15**. MG6 extends an operator ruling (S45 PM1); MG3 and MG10 are flagged **product calls**;
the rest are documented technical calls, checked against the code (brief §1; T0 re-verifies). Do not re-open a ruling without
recording the reason in that task's Notes.

## Verified facts (2026-10-30; T0 re-checks)

1. **Config is a strict JSON file.** `internal/mcp/config.go:86` `Config{Public []ServerConfig, Agents map[string]AgentConfig}`;
   `AgentConfig{Core []string (nil = whole core set), Private []ServerConfig, Declares []string}`; `ServerConfig{Name,
   Transport{Type stdio|http, Command, Args, Env []string (NAMES only, C17), URL}, Tools []ToolConfig{Name, Rerun, Effect}}`.
   Unknown keys refused; the same server name in two places is a **load error**; `Validate()` (`:310`) checks name/tool/env
   grammars, transports, core grants (`validateCoreGrant`), declares (`validateDeclares`), effect/rerun vocabularies.
2. **One writer.** `SaveConfig(path, cfg)` (`:274`): validate, atomic temp+rename, process-wide `saveMu`. Its only caller is the
   proposal-commit effect (`cmd/aeon/proposal.go`, `mcp_server_add/remove`). `WithServer`/`WithoutServer` build candidates.
3. **The registry is the security object.** `mcp.New(cfg)`; `ToolsFor(agent)` is the only way to obtain tools — there is no
   "all tools" list to filter (MC2, D3); `Availability`, `Refused`, `GrantView(agent, policies, at)` (`grants.go:57`: states
   `available|unavailable|refused|ask`, reasons from S10's sentences / MC10 / effect-ask).
4. **MC11 deletability.** `internal/mcp` is imported only by `cmd/aeon` (and its own tests); `internal/mcp/absent_test.go` walks
   the module and pins it. The daemon and protocol reach MCP through one-func seams (`Table.MCP`, `daemon.MCPCall/MCPWithheld/
   MCPBundle`, `mcp_seam.go`) supplied by `cmd/aeon serve`.
5. **Socket today:** `mcp.status`, `mcp.check` (`method.go:366-372`, registered `:1080`) via `Table.MCP`; S34's per-agent grants read
   `tools.grants` via `Table.ToolGrants`. **CLI today:** `aeon mcp list/call/serve` (`cmd/aeon/mcp.go:59-65`).
6. **Agents are keyed by whatever string dispatch passes** (a name today) — `grep slug internal/mcp` finds nothing; S43 did not touch
   MCP (CONFIGURATION-ORDER already noted this). The `agent` table has `slug` (unique index `idx_agent_slug`).
7. **Proposals:** `proposal.kind` CHECK is closed (S35 TD1; S37 widened it by a table rebuild, `agent_import`); kinds
   `mcp_server_add`, `mcp_server_remove` exist. S45 T1 moves their effects into `internal/desk`; S45 T5 adds the generic
   `proposal.commit`.
8. **No shipped definitions**: level-0 prints "no MCP servers configured. tools available: the core nine only (S11)"
   (`cmd/aeon/mcp.go` `mcpAbsentLines`, pinned by the demo walk).
9. **Bench UI:** `McpPage.tsx` (297 lines) reads only.

## MG1 — The configuration becomes four tables; `Config.Validate` stays the validator

Rows are the **data**; the existing `mcp.Config` and its `Validate` remain the **meaning**. Tables (one migration file):

| Table | Columns (all `NOT NULL` unless noted) |
| --- | --- |
| `mcp_server` | `id INTEGER PK`, `name` (grammar CHECK net), `tier ∈ ('public','private')`, `agent_slug` (NULL for public, set for private; FK → `agent(slug)` `ON DELETE RESTRICT`), `transport_type ∈ ('stdio','http')`, `command`, `args` (JSON array), `env` (JSON array of NAMES), `url`, `enabled ∈ (0,1)`, `origin ∈ ('system','user','import')`, `created_at`, `updated_at` — `UNIQUE(name)` (a name is global: one namespace, MC8) and `CHECK ((tier='public') = (agent_slug IS NULL))` |
| `mcp_server_tool` | `server_id` (FK, `ON DELETE CASCADE`), `name`, `rerun`, `effect` (empty = UNCLASSIFIED, MC6 — stored as `''`, not defaulted), `PRIMARY KEY (server_id, name)` |
| `mcp_agent` | `agent_slug` (PK, FK → `agent(slug)` RESTRICT), `core TEXT` (JSON array, **NULL = the whole core set**, a distinct fact from `'[]'` = none) |
| `mcp_declare` | `agent_slug` (FK), `qualified` (`server.tool`), `PRIMARY KEY (agent_slug, qualified)` — a declaration **is** the grant (MC5) |

`mcp_server.name` UNIQUE and the tier/owner CHECK make "same name in two places" structurally impossible in rows (the file's
load error becomes a constraint), and `Config.Validate` still runs on the assembled candidate for everything SQL cannot say
(grammars, declares ⊆ existing public tools, effect/rerun vocabularies). **The database never decides validity; the
validator does** — DB CHECKs are the net beneath it (the PL3/`policy` division).

## MG2 — Agents are slugs in rows; the registry still sees what dispatch passes

Rows reference `agent.slug` (FK RESTRICT). `internal/mcp` is **unchanged** and still takes a string key. The assembly step
(MG4) maps `agent_slug → dispatch key` using the agent table and whatever key dispatch passes today (T0 records it: name or
slug). One mapping function, one place. Consequence of the FK: deleting an agent that MCP rows reference fails at the
database; **S46 T5 makes that refusal named** (S43 AM4's check gains MCP references) — adapting S43's merged delete path is S46's
own task, not a request to S43.

## MG3 — Adopting the legacy file: once, automatic, all-or-nothing, never silent *(product call)*

*Overrule means an explicit-only import (the operator must run `aeon mcp import-config`); until they do, MCP is level-0 —
which silently disables servers that worked yesterday and is why the default is automatic.* At the first start after the
migration, **if** the legacy file exists **and** the MCP tables are empty **and** no `mcp_import` row exists for its hash:
parse + `Validate` the file; map every agent key to a slug (an unresolvable name **refuses the whole import**, S43 T2's
all-or-nothing rule); write rows (`origin='import'`) and one `mcp_import` ledger row (path, SHA-256, imported_at, result,
counts) **and** `mcp_change` rows in one transaction. An **invalid** file or unresolved agent: nothing is written, the
daemon starts at level-0, and `doctor` plus a start log line name the file, the line/field and the fix — **never a silent
empty config**. The file is **never modified or deleted** (a filesystem mutation outside the database; an operator may keep it
as a backup). After adoption the file is **ignored at runtime**; a later hand-edit is reported by `doctor` ("legacy file differs
from what was imported; the database is authoritative") and re-importable only through the explicit human-door
`mcp.importFile` (replace-all, with a before/after diff, never a merge). Importing the operator's *own* previously authored
config is carrying their decisions forward, not seeding a new widening — which is why it is a migration and not a guard seed.

## MG4 — Assembly lives in `internal/mcp`, pure; rows are read in `cmd/aeon` (MC11 intact)

`mcp.Assemble(rows Rows) (Config, error)` and `mcp.Disassemble(Config) Rows` are **pure functions over plain, mcp-local row
structs** (no `internal/store` import — `internal/mcp` keeps its imports). `cmd/aeon` (the seam file `mcp_admin.go`, the only
importer of `internal/mcp`) reads rows from the store, calls `Assemble`, then `Validate`, and hands the registry the same
`Config` it always got. A round-trip law is tested: `Assemble(Disassemble(c)) == c` for every fixture config. **Only enabled
servers are assembled** into the config the registry sees (a disabled server exists as a row but not in the offer); the
**candidate** validated on every write contains disabled servers too, so enabling later cannot be the first time a definition
is checked. The daemon reaches all of it through the existing `Table.MCP`-style func seam (a new `MCPAdmin` seam for the
management methods); **`internal/daemon` and `internal/protocol` still import no `internal/mcp`**, and deleting `internal/mcp`
plus `cmd/aeon/mcp*.go` still leaves the rest building (the tables simply go unused). The deletability test is extended, not
relaxed.

## MG5 — The methods (all `RequiresInitialize`, none `Debug`)

Reads (open): `mcp.servers {tier?, agent?, enabled?, origin?}` → list **with details** (name, tier, owner slug, transport
summary with **env names only**, tools with `rerun`/`effect` and a `classified` flag, `enabled`, `origin`, declared-by count,
live reachability joined from the same probe cache `mcp.status` uses, `updatedAt`); `mcp.server {name}` → everything;
`mcp.grants {agent}` → the S12 `GrantView` list **plus** core narrowing and declarations, for **any** agent (the person sees all;
agents do not — MG8); `mcp.vocabulary {}` → transports, `rerun`/`effect` vocabularies (from `internal/tools`/`mcp` constants, not
restated), name/tool/env grammars; `mcp.changes {server?, agent?, limit?}` → the audit. Existing `mcp.status` and `mcp.check`
are unchanged in shape.
Writes (**human door**): `mcp.server.add`, `mcp.server.update` (transport, tools/classes), `mcp.server.remove`,
`mcp.server.setEnabled`, `mcp.grant.declare`, `mcp.grant.revoke`, `mcp.core.set`, `mcp.importFile`. Each: strict params (no actor,
no origin), actor from `table.ActorFor` (unresolvable ⇒ refuse), **candidate-config validation** (MG1) with the validator's
sentence verbatim, one transaction for rows + `mcp_change`, result = the updated view plus the **before/after grants view**
for every agent the change touches.

## MG6 — Direct human-door authoring is allowed; the narrow/agent surfaces stay closed (extends S45 PM1)

S35 TD4: "`aeon mcp add/remove` do not exist as bypass verbs; config authoring exists only as proposal + commit." The operator
ruled (2026-10-30, for policy) that authoring and decision move to the socket as a human door off the narrow surface; the same
reasoning applies — the danger was a *non-person caller* gaining the means to widen what tools exist or are granted, and those
callers reach the daemon only through surfaces that never name these methods. **S46 amends TD4 the way S45 amended PL15/TD7:**
sibling guard functions with canaries (T7), `AGENTS.md` and the S35 annotation updated, originals pinned by hash. The proposal
path **stays** (agents propose narrowing; a person commits). *Flagged: this extends a ruling given for policy to MCP; confirm.*

## MG7 — "Granted" is two facts; the UI shows both and never combines them silently

`declares` is the grant (MC5) but a tool is usable only if S10's policy also allows it (MC10: no allow ⇒ `unavailable`). The
grants view is S12's `GrantView` verbatim (states and reasons). The UI's "grant" flow is **two explicit steps**: `mcp.grant.declare`
(S46) and, offered afterwards with its own confirmation, a `tool_access` allow via S45's `policy.add` — never one hidden
cross-domain transaction. An agent's tool never performs either.

## MG8 — MC2: agents see only their own view

The read tools (`mcp_servers`, `mcp_grants`, `mcp_status`) are **agent-scoped by construction**: they call `Registry.ToolsFor(self)`
/ `GrantView(self)` where `self` is the identity the daemon stamps (OL3 — an agent cannot name another). Output, error text and
audit never include another agent's private server names, tools, existence or counts (a test plants a private server for B and
asserts A's every output byte-excludes it). `mcp.check` (the probe) is **not** a tool — probing spawns processes and is an
operator act (S34 TB8). The **person's** socket reads are unscoped (the human sees everything; that is the management view).

## MG9 — Agent write tools: narrowing only, proposals only

`mcp_propose_remove_server` (existing kind `mcp_server_remove`) and `mcp_propose_revoke_grant` (**new kind `mcp_grant_revoke`**,
T6) create inert proposal rows; both can only *reduce* what exists. There is **no** tool that adds a server, declares a grant,
enables, imports, or commits — an agent cannot even *propose* widening (policy's rule is the same: restrictive-only). The new
proposal kind is the "design revision" S35 TD1 requires for the closed enum; it is made here, with the S37-style table rebuild,
counted in the migration drill. All tools: ≥ 40-char descriptions, strict schemas with **no** identity/status/source fields,
registered-not-granted, counted registry bump.

## MG10 — Seed sets: declared now, shipping nothing *(product call)*

Per CONFIGURATION-ORDER rule 6, shipped MCP definitions are records. None ship today (fact 8), so S46 **declares** two sets
against the S43-extra engine and ships **zero records** (the empty production set is a tested, honest state):
`mcp.servers` (class **inert**: server *definitions* with `enabled=false` and no grants) and `mcp.grants` (class **guard**: any
`enabled=true` flip or `declares` row). The engine's `GuardAffecting()` cross-check forces the split. System rows: an operator may
**remove** one (not resurrected, `restore` explicit) and may **duplicate** it into a user row; **edit** of a system server is
refused (the policy rule, S45 PM16). *Overrule means no sets are declared until something ships — then the first shipped default
would have to invent the mechanism, which is exactly what rule 6 forbids.*

## MG11 — Status and probe are unchanged in meaning

`mcp.status`/`mcp.check` keep their wire shapes and semantics (S34). Only their **source** moves: the seam assembles the config from
rows. Parity is tested field-for-field against the file-based fixtures (acceptance 2).

## MG12 — Writes are live

N8: no cache. The step-offer path (S36) builds its registry from the assembled config per offer (T0 confirms where
`mcp.New` is called today); a committed write must be visible to the next offer and the next `mcp.status` with no restart. If T0
finds the registry is built once at boot, T3 changes **when** it is built (per offer or on a row-version bump), characterized first.

## MG13 — Credentials: names only, everywhere

`env` is a JSON array of variable **names** matching `envNameRE`; a value-shaped string (`=`, whitespace) is refused by the grammar.
The UI form says values come from the daemon's environment; the probe reports **set/unset per name**, never a value. A grep test
asserts no `mcp.*` method, audit summary, log line or UI string path can carry a value (C17), and `mcp_change` summaries are
fixed templates built from names/ids only.

## MG14 — The UI: Settings → MCP

Descriptor-driven from `mcp.vocabulary`. Tabs: **Servers** (list with details: tier pill, owner via `AgentPicker`, transport summary,
tools with rerun/effect pills and an **unclassified** warning, `enabled` switch, `origin` badge, live status dot, **Probe** button
= `mcp.check`), **Grants** (an agent × tool matrix from `mcp.grants`, each cell the S12 state with its reason text; the two-step grant
flow of MG7), **Proposals** (S35 desk filtered to MCP kinds, commit/reject via S45's generic methods with the before/after confirm),
**Audit**, **Import** (legacy file status, `mcp.importFile` with a diff preview). Real Settings screen, not the bench; the bench
`McpPage` stays untouched.

## MG15 — What does not move

S12's registry semantics and MC1–MC12; the MCP client and transports; S34's probe/single-flight; S36's dispatch seam shapes; `aeon mcp
call` and `aeon mcp serve`; the narrow surface's action list; the policy vocabulary and gate; S43's, S44's and S45's code and docs
(S46 *adapts* S43's agent-delete refusal in T5, as its own task).

## Open items left to T0 — mechanical

1. Re-verify facts 1–9; list every reader of the config (`loadMCPConfig`, `mcp_seam.go`, `mcp_services.go`, `proposal.go`) with file:line.
2. What key dispatch passes to `ToolsFor` today (name or slug) after S43/S43-extra (MG2's mapping).
3. Where `mcp.New` is called — per offer or once (MG12).
4. The parity table: every `aeon mcp` verb and flag → socket twin or gap; what `mcp.status`/`mcp.check`/`tools.grants` return today.
5. How the probe reports env presence (MG13) — does any path print a value?
6. The proposal-kind CHECK rebuild precedent (S37 T1) and its migration-count assertions (T6).
7. S45/S43-extra state (merged? seams?), the error-code block, handshake/conformance/protogen touch points.
8. Whether `ALTER`/FK to `agent(slug)` is acceptable given the `agent` table's constraints (`idx_agent_slug` is a unique index, not a PK) — SQLite requires a UNIQUE/PK parent key; confirm.
