# Brief — S47 Tools catalog (what the app exposes, and to whom)

Written 2026-10-30. **Stage:** 5 · Fleet — the read-only counterpart to S43/S45/S46 in [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md).
**Depends on:** S11/S13 (tool registry, handler allow-lists), S38 (records catalog), S43 (agent rows), **S43-extra T3** (seed engine, for T3),
**S45** (policy reads/`Describe`, the guard pattern), **S46** (MCP rows and the scoped/unscoped read seams — for the MCP half; T0 re-checks what has
merged). S44 is not a dependency.

## 0. The standing rules this slice carries (CONFIGURATION-ORDER, 2026-10-30)

1. **Daemon first, CLI second** — here the gap is total: nothing on the socket or CLI lists the tool registry.
2. **Every management operation gets a tool.** This slice has **no write operations** (the operator's words: "not a CRUD but a list with details"), so
   its agent tools are own-view reads only; there is nothing to propose.
3. **A list with details** — this slice *is* that rule, applied to the tools themselves and to the daemon's own surface.
4. **Every record is a row; system ones are seeded.** A tool's **catalog entry** (identity, family, description, class, capabilities, twin) is a seeded record;
   its **executor and input schema are code** (shape — they cannot be edited without breaking the tool), shown read-only and drift-checked against the row.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Fact | Evidence |
| --- | --- |
| **The registry is a function in code.** `tools.Registry()` returns `name → Tool{Spec model.ToolSpec, Class RerunClass, Run}`: the core ten (`bash, edit, glob, grep, list, question, read, todo, webfetch, write`), S38's management catalog (23 `internal/records` specs) + the task family (6) + `run_start`, conditional `search`/`graph` (absent unless their seam is wired, V13), and a hidden alias `write_file`. Pinned name-for-name by `TestRegistryExactNames`, and **each slice that adds tools bumps it** (S45 +7, S43-extra +1, S46 +5, planned). | `internal/tools/toolset.go:116`, `toolset_test.go:236` |
| **Nothing exposes it.** There is **no socket method and no CLI verb that lists the registry**. `aeon handlers list` prints handler *names*. S34's `tools.grants {agent}` returns **only** the MCP `GrantView` for one agent (core tools read "judged by the step's S11 policy gate") and reads the MCP config file directly. | `cmd/aeon/handlers.go:42`, `cmd/aeon/agent.go:560`, `internal/protocol/agent_roster.go:46` |
| **A tool carries no capability facts.** `Tool` has a re-run class (`safe/detectable/unsafe`) but nothing saying it **writes the workspace, spawns a process, reaches the network, asks a person or writes the store**; the records catalog has `Mutates`, the core ten have nothing. | `internal/tools/read.go:97`, `internal/records/spec.go` |
| **"Who may call it" is spread over four mechanisms.** (1) `DefaultGrant()` = `read, list, write`; (2) a handler's allow-list (`Definition.Tools` — in the DB `agent.tools` after S43-extra); (3) MCP: `Config.Agents.Core` narrowing and `Declares`; (4) S10's `tool_access` policy rows decide **per call** (deny over ask over allow, default deny). "Registered is not granted." | `toolset.go:235`, `handler/handler.go`, `mcp/config.go`, `policy/spec_toolaccess.go`, `daemon/step.go:411` |
| **The daemon's own surface is unclassified.** ~160 protocol methods (`Method{Name, RequiresInitialize, Debug, Handle}`); `PublicMethods()` is "not Debug"; the narrow surface is an 9-entry allow-list (`internal/narrow/actions.go`); agent services are 7 methods; S38 projects 30 catalog specs as twins. There is **no structural read/write marker** on a method (S35's own T6 comment says so) and no one list that says which door reaches what. | `internal/protocol/method.go`, `narrow/actions.go`, `agent.go:51` |
| **UI:** nothing for tools outside the bench. | — |

## 2. What this slice is

1. **A tool catalog you can read**: every tool that exists (core, records/task, `run_start`, conditional ones, **and MCP-provided tools** from S46's rows), each with description,
   family, input schema (read-only), re-run class, **declared capabilities**, its socket twin, where it came from (`system` / `mcp`), and whether its catalog row is current.
2. **"To whom"**: for any tool, who can call it and why not — default grant, handler allow-lists, MCP declarations, and the **policy verdict** per agent computed by
   the same evaluator the gate uses; and the inverse, **one agent's whole tool view**. A matrix, not a guess.
3. **What the daemon exposes**: a classified, complete listing of every socket method with its **door membership** (person socket, narrow HTTP/MCP-stdio surface,
   agent services, tool twin) and whether it **writes** — backed by a **closed classification table with a completeness test**, so a new method cannot ship unclassified.
4. **Seeded catalog records**: the `tools.core` seed set (class **inert**) puts the system tools' catalog entries in the database with `origin='system'`; a drift check
   reports a tool in code with no row (or the reverse). Nothing about *execution* moves to the database.
5. **Own-view agent tools** (`tools_mine`, `tool_show`) and an `aeon tools` / `aeon surface` CLI, **both clients of the socket methods**.
6. **Settings → Tools** and **Settings → Surface** panels, with links into S45 (policy for this tool) and S46 (the MCP server).

## 3. Out of scope

- **Editing anything.** No create/update/delete of tools, no per-tool enable flag (disabling is a policy `deny` row — S45), no editing a description or schema.
- Changing how tools execute, the registry's contents, `DefaultGrant`, the gate, or any grant mechanism.
- Authoring grants (handler tool lists are agent content — S43/S43-extra; MCP grants — S46; policy — S45). This slice only **shows** them and links.
- The MCP **client**; probing servers (S46/S34 own it).
- Changing S43, S43-extra, S45, S46 or S44 (the `TestRegistryExactNames` bumps they make are theirs).

## 4. Open questions — decided in `design.md`, flagged where the operator may overrule

1. Does the DB become the source of truth for tool *descriptions/schemas*? — TC2 *(product call)*: no; catalog entry is a record, executor and schema are code, drift-checked.
2. Is the daemon's method surface in scope? — TC8 *(scope call)*: yes, as a classified read-only listing.
3. May an agent see tools it is not granted? — TC9 (no: own view only; MCP private tools are invisible to peers, MC2).
4. Do MCP tools get rows in this catalog? — TC4 (no: they are *read* from S46's tables; one truth).

## 5. Acceptance criteria

1. **Complete:** `tools.list` returns exactly the union of `Registry()` honest names, the wired conditional tools, and the enabled MCP tools; the hidden alias is not listed (it is shown as an alias on `write`).
2. **Every tool declares capabilities:** a registry name with no capability entry fails a test; a new tool cannot ship undeclared.
3. **"To whom" equals the gate:** for fixture agents and tools, the catalog's per-agent verdict **equals** `policy.Evaluate`'s for the same `tool_access` question and the grant state equals what `BuildToolset` would build; a differential test walks all (tool, agent) pairs.
4. **MC2 holds:** an agent's own-view output (success and error text) never reveals another agent's private MCP tools or servers.
5. **Seeded and drift-checked:** `tools.core` (inert) applies at start; every system tool has a row with `origin='system'`; `tools.status` reports a code tool without a row, a row without code (`retired`), and a row whose description/class/capabilities/schema hash differs from code; none is auto-repaired silently.
6. **Surface complete and honest:** every registered socket method has exactly one classification entry; a test fails on an unclassified method; `surface.list` door flags equal the real structures (`narrow.Actions()`, `AgentServiceMethods()`, records twins, `PublicMethods()`), proven by a differential test.
7. **Read-only:** no socket method, tool or CLI verb of this slice mutates anything except the seed apply (which is S43-extra's door); guard tests with canaries.
8. **Daemon first:** every `aeon tools`/`aeon surface` verb has a socket method; a parity guard fails otherwise.
9. **Visible:** the Settings panels list, filter, inspect, show the per-agent matrix and the surface matrix, and link into policy/MCP — through registered socket methods only (P34 grep as a test).
10. **No regression:** S11/S13/S34/S38 suites unchanged; `tools.grants` keeps its shape.

## 6. Handoff status (2026-10-30, planning session)

**Done:** this brief, `design.md` (TC1–TC13 + verified facts), `plan.md`, `tasks/T0.md`–`T11.md`.
**Not started:** all implementation. Run T0 first (no code). **Blocked on:** S43-extra T3 for T3; S45 (policy reads) for T4's policy half; S46 T3 (rows) for the MCP half of T4/T5; S45 T9 for the shared UI components in T10. T1, T2, T6 can start after T0.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).
> Specific to this slice: S44-extra changes the records catalog (−4 `project_*`, +5 `component_*` tree tools) and the socket (−5 project/telemetry methods, +6 `component.*`). Every count in this brief is re-measured by T0 after S44-extra merges.

> **Closed 2026-11-02 by T11 (the record):** T0–T11 all built; every one of the ten acceptance criteria re-run in the close-out against a
> real binary and a live daemon, the one migration drilled six ways on real stores, and the walk recorded in
> [`../DEMO.md`](../DEMO.md) §S47 with its transcripts and its named jsdom witnesses (no Tauri libraries on this box, so the browser half is
> stated as not run rather than implied). Two things the close-out found and fixed rather than admired: **`tools.byAgent`/`tools_mine`
> answered every granted tool twice** (146 rows for 73 tools — the universe was a raw union and `ForAgent` keeps duplicates; two tests now
> pin one row per tool, with teeth proven), and **the versions golden had silently gone stale** for the three tools that landed after T3
> (`tool_show`, `tools_mine`, `backup_status`) — `T7`'s gap, named in its Notes. What the next slice owes this one: nothing is writeable here
> by design, so if a person ever needs to *author* a catalog entry it is a new slice with its own authority discussion, not a flag on these
> nine doors.
