# Brief — S46 MCP management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 1 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md), beside S45.
**Depends on:** S12 (MCP client), S34/S36 (trust reads, dispatch), S35 (proposals), S43 (agent slugs), **S43-extra** (the
seed engine, for T10), **S45** (`internal/desk`, the generic `proposal.commit`, the PL15/TD7 guard pattern, the
`AgentPicker`; T0 re-checks what has merged). S44 is not a dependency.

## 0. The standing rules this slice carries (CONFIGURATION-ORDER, 2026-10-30)

1. **Daemon first, CLI second.** Every `aeon mcp` capability is a socket method; the CLI is a client.
2. **Every management operation gets an agent-visible tool**; where it widens a guard the tool is propose-only and
   restrictive-only. Here a **second constraint** applies that policy did not have: *private is invisible and uncallable
   to peers* (MC2) — an agent's read tools return **only its own view**.
3. **A list with details**, not a CRUD form: what servers exist, who they are for, what each tool is classified as, what is
   actually granted and why not.
4. **Every record is a row — system ones included — and seeded.** The MCP configuration **moves from a JSON file into the
   database** (`aeon.mcp.json` / `$AEON_MCP_CONFIG` → tables), and any server definition the product ships becomes a seed
   record. Code (and the legacy file) are sources, never the source of truth.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Capability | Today | Gap |
| --- | --- | --- |
| **Where config lives** | a **JSON file** (`paths.MCPConfigFile()`: `$AEON_MCP_CONFIG` or the XDG default), strict (unknown keys refused), shape `Config{Public []ServerConfig, Agents map[agent]AgentConfig{Core, Private, Declares}}` (`internal/mcp/config.go:86`) | not a record; not in the DB |
| **The only writer** | `mcp.SaveConfig(path, cfg)` (validate + atomic temp+rename, `saveMu`) — called only by the proposal-commit effect in `cmd/aeon/proposal.go`; **no `aeon mcp add/remove` verbs exist by S35 TD4** ("config authoring exists only as proposal + commit") | no direct authoring; no app path |
| **The CLI** | `aeon mcp list [--agent] [--include-refused]`, `call`, `serve` (`serve` mounts the narrow surface — aeon *as* an MCP server, unrelated to this slice) | `list`/`call` have no socket twin beyond S34's two reads |
| **The socket** | `mcp.status`, `mcp.check` (S34): config truth and an operator-triggered probe, via the one-func seam `Table.MCP` because **`internal/mcp` may be imported only by `cmd/aeon`** (MC11: the whole layer is deletable; `internal/mcp/absent_test.go` pins it) | no list-with-details, no per-agent grant view for the app (S34's `tools.grants` is a bench read), no writes |
| **Agent identity** | `Config.Agents` and `ToolsFor(agent)` key agents by the string dispatch passes (a **name** today; no slug anywhere in `internal/mcp`) | not on S43's slug; no picker |
| **Proposals** | kinds `mcp_server_add`, `mcp_server_remove` exist (S35); effects live in `cmd/aeon` (S45 T1 moves them behind `internal/desk`) | no grant-narrowing kind |
| **Shipped definitions** | none: level-0 is "no MCP servers configured. tools available: the core nine only" (MC11) | nothing to seed *today* — the mechanism must exist (rule 4) |
| **UI** | bench `McpPage.tsx` (297 lines, reads only) | no Settings surface |

## 2. What this slice is

1. **MCP configuration as records**: `mcp_server`, `mcp_server_tool`, `mcp_agent`, `mcp_declare` tables; the existing
   `Config.Validate` stays **the** validator (run on the candidate config before every write); a pure `Assemble` turns rows
   into the `mcp.Config` the unchanged registry consumes. The legacy file is adopted **once**, all-or-nothing, never silently
   dropped.
2. **Daemon-first management** (human-door socket methods, off the narrow surface): list/inspect servers, inspect any
   agent's grants with the S12 reasons, add/update/remove servers, enable/disable, declare/revoke grants, narrow an agent's
   core set, import the legacy file, read the audit.
3. **Agent tools**: own-view reads (`mcp_servers`, `mcp_grants`, `mcp_status`) and **restrictive-only proposals**
   (`mcp_propose_remove_server`, `mcp_propose_revoke_grant`), granted to no one by default.
4. **The CLI as a client** with an offline fallback through the same code.
5. **Seed sets** (`mcp.servers` inert/disabled, `mcp.grants` guard) declared against the S43-extra engine — shipping **zero**
   records now, proven with fakes.
6. **A Settings → MCP section**: servers with live status and a probe, a grants matrix with S12's reasons, forms rendered
   from the daemon's vocabulary, the MCP proposals, the audit.

## 3. Out of scope

- S12 semantics: tiers, `ToolsFor`/`Availability`/`Refused`, the call gate, MC1–MC12. S34's probe and S36's dispatch.
- The MCP **client** (stdio/http transports) and `aeon mcp serve` (the narrow surface).
- Authoring **policy** rows (S45) — the grants UI links to it; it never writes policy itself.
- Storing a credential: env carries **names**, never values (C17), here as before.
- Shipping any real MCP server definition (a later change declares its record).
- Changing S43, S45 or S44.

## 4. Open questions — decided in `design.md`, flagged where the operator may overrule

1. Is direct human-door authoring allowed for MCP (S35 TD4 said proposals only)? — MG6 *(extends the 2026-10-30 PL15/TD7 ruling)*.
2. Does the legacy file import itself automatically? — MG3 *(product call)*.
3. May an agent see MCP config? — MG8 (only its own view; MC2).
4. What can agents propose? — MG9 (narrowing only).

## 5. Acceptance criteria

1. **The DB is the only source of truth**: with `$AEON_MCP_CONFIG` pointed at a different file after adoption, behaviour is
   unchanged; no runtime read of the file remains outside the importer.
2. **Behaviour-identical**: for the S12/S34/S36 fixture configs, `ToolsFor`, `Availability`, `Refused`, `GrantView`,
   `mcp.status`, `mcp.check` and `aeon mcp list` produce byte-identical output from rows as from the file.
3. **One validator**: every write — socket, CLI, proposal commit, import, seed — passes `Config.Validate` on the whole candidate
   and is refused with its sentence verbatim; there is no SQL-only path that skips it.
4. **Live**: a write is visible to the next step offer and the next `mcp.status` with no restart.
5. **Adoption is safe**: an existing valid file is imported once (idempotent by hash), audited, mapped to agent slugs, and
   **never silently dropped** — an invalid file or an unresolvable agent refuses the whole import, naming it, and leaves level-0.
6. **MC2 holds on every new read**: an agent's tool output and error text never reveal another agent's private servers.
7. **Credentials never stored**: `env` holds names only; no read, list, audit row, log or UI shows a value.
8. **Daemon first**: every `aeon mcp` verb has a socket method; a parity guard fails otherwise.
9. **Human door only**: the write methods are absent from the narrow surface, agent services, tool registry and records
   catalog (guards with canaries); agent tools never widen — they cannot add a server, declare a grant, enable, or commit.
10. **Agent deletion names MCP references**: deleting an agent still referenced by MCP rows is refused, naming them.
11. **Audit**: each write leaves one append-only `mcp_change` row naming the peer actor; none for reads, refusals or no-ops.
12. **Seeding**: `mcp.servers` (inert, disabled-only) and `mcp.grants` (guard) are declared; production ships zero records; a
    fake guard record is never applied at start; a removed system server is not resurrected.
13. **UI**: lists, inspects, probes, edits and grants through registered socket methods only (P34 grep as a test).
14. **No regression** in S12/S34/S35/S36 suites except amended guards, each listed.

## 6. Handoff status (2026-10-30, planning session)

**Done:** this brief, `design.md` (MG1–MG15 + verified facts), `plan.md`, `tasks/T0.md`–`T12.md`.
**Not started:** all implementation. Run T0 first (no code). **Blocked on:** S45 T1/T5 (the shared `desk` registry and generic
commit) for T6; S45 T9 (the `AgentPicker`) for T11; S43-extra T3 (the engine) for T10; everything else can start after T0.

**Amended 2026-11-02 (T12 close-out):** all thirteen tasks (T0–T12) built and merged to main; the slice is **Built (13/13)**. The fourteen criteria were re-run in T12 with dated evidence (32 named tests, five drills incl. the new upgrade-from-vintage twin, the real-binary demo walk in `../DEMO.md` §S46); the only browser-lane gaps are stated there (D15 not run — no Tauri libs on the walk box; UI steps carry their jsdom witnesses). The five acceptance-grade rulings above stand as written; none was softened.
