# Brief — S43-extra Seed process (every record in the database, system ones seeded from code)

Written 2026-10-30. **Stage:** 5 · Fleet — the foundation under Tier 1+ of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md).
**Depends on:** S43 (merged — agent rows, slugs), S4 (store). **Blocks:** S45 and every later management slice
(each declares its own seed set against this engine). It is called "S43-extra" because it finishes what S43's AM1
promised and did not fully deliver (§1), and because S43 is built and not edited from here — the fix is this slice's
own work, not a change to S43's tasks.

## 0. The rule this slice makes true (operator, 2026-10-30)

> **Every record the product manages lives in the database — including the system ones that are defined in code.**
> A system agent, a shipped policy, a standard blueprint, an MCP server definition, a tool's metadata: each is a row,
> put there by a **seed process**, and at runtime the daemon reads **only** the row. Code is the *source of the seed*,
> never the *source of truth*.

Why it matters: a product is something a person can see, list, inspect, version and reason about. While a system
agent is a markdown file compiled into the binary, the app cannot show its prompt, a policy cannot name it
consistently, an export cannot carry it, and "duplicate the system one to change it" (S43's own promise) has nothing
real to copy. One storage model, with a provenance flag, removes a whole class of "this lives in code, that lives in
a file, the other lives in a table" special cases.

What is **not** a record (stays code): the *shape* of things — the closed policy vocabulary (PL3), S44's setting
declarations, protocol methods, tool *executors*. Rule of thumb: if a person could reasonably want to **list, read or
copy it**, it is a record; if it is a type definition, it is code.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Fact | Evidence |
| --- | --- |
| **S43 made agents rows, but first-party agents are stubs.** `Store.MigrateFirstPartyAgents` (`internal/store/agent.go:336`) creates an `agent` row from each `plugin_agent` *lifecycle* row: `Slug, Name, Version:"1", Internal:true` — **no description, prompt, skills, functionality, tools, temperature or handoff**. It runs only when someone calls `agent.migrate` (`cmd/aeon/agent_management.go:164`). | `agentmgr.go:264`, `store/agent.go:329-364` |
| **The real first-party content is still code.** 33 markdown bodies with YAML front matter (`name, description, temperature, tools allow-list, handoff, policy-agent, body`) are `//go:embed all:bodies` (`internal/handler/registry.go:21`), parsed at init into a process map `bodyEntries` that **panics** on an invalid body; dispatch reads that map by *name* (`handler.Lookup/Build/Names/LoadDefinition`, ~9 call sites: `daemon/policy_gate.go:210`, `daemon/handlers_ops.go:30`, `daemon/blueprint.go:487`, `learnings/scope.go:39`, `plugin/import.go:304`, `agentsvc/cards.go:103`, `cmd/aeon/{handlers,agent_services,blueprint}.go`). | `internal/handler/registry.go` |
| **The `agent` table cannot hold what dispatch needs.** Columns: `slug, name, description, functionality, metadata, prompt, skills, version, internal, avatar_*, signature, signer_certificate, signed`. No `tools`, `temperature`, `handoff`, `policy_agent`. | `migrations/20261030130000_agent.sql` |
| **Policy** (S10/S11) is in the DB, but the shipped corpus (38 agents / 367 rules) is applied only by the explicit verb `aeon policy seed-corpus`; **D4 deliberately forbids an automatic seeder** and a guard (`authoredRecordCall`) enforces it. | `cmd/aeon/policy.go`, `policy_test.go:483` |
| **Standard blueprints** are `//go:embed blueprints/*.yaml`, copied to **files** on first use with pristine-hash upgrade rules; a loop pins the content it started with in `loop_blueprint`. | `internal/blueprint/standard.go` |
| **MCP config** is a **file** (`paths.MCPConfigFile()`) with an embedded default. | `internal/paths/paths.go:152`, `internal/mcp/registry.go` |
| **Presets** (`aeon init`) are code (`internal/presets`). | S23 T4 |

So four different "ship defaults in the binary" mechanisms exist (migration backfill, embedded + init-time map, embedded
+ file copy with hashes, an explicit operator verb) and none of them leaves a record of what was applied or lets the app
ask what is out of date.

## 2. What this slice is

1. **One seed engine** (`internal/seed`): *seed sets* declared in code, applied idempotently to the database, versioned,
   hash-checked, with a **ledger** (what was applied, at which version, by whom) and an append-only **audit**.
2. **Two classes** of set, because not every seed is equally safe to apply unasked:
   - **inert** — changes nothing about what is *permitted* (agent definitions, blueprints, tool metadata, MCP server
     *definitions* disabled by default): applied **automatically** at daemon start and by `aeon init`.
   - **guard** — widens or sets what a running loop may do (policy rows, MCP grants/enabled servers): **never
     automatic**; applied only by an explicit human-door action. This preserves D4 and PL1 exactly.
3. **The agents set, done for real**: first-party agents seeded with their full content from the embedded bodies
   (lossless — a round-trip test proves the DB-built handler equals the embedded-built one), the `agent` table extended
   for what dispatch needs, and **dispatch reading the database**, with the embedded bodies demoted to "seed source".
4. **The operations**, daemon first: `seed.status` / `seed.plan` / `seed.apply` / `seed.restore` on the socket (human
   door), a read-only `seed_status` agent tool, an `aeon seed` CLI client, and a "System records" panel in Settings.
5. **The contract for every later slice**: S45 (policy), S46 (MCP), S47 (tools), S48 (blueprints), S49 (automation) each
   *declare their own seed set* in their own file against this engine — no slice invents another way to ship a default.

## 3. Out of scope

- The seed sets for policy, MCP, tools, blueprints, automation — each is its slice's task.
- Moving blueprints or MCP config out of files (S46/S48).
- Dispatching **user-created or imported** agents. DB-driven dispatch in this slice covers **internal (seeded)**
  agents only; a custom row has no validated tool allow-list and is not dispatchable until a later slice says so
  (named, tested, honest — see design SD7).
- Remote/cloud seed sources, signed seed bundles, or seed updates delivered outside a binary release.
- Changing S43's agent card, signature or import behaviour.

## 4. Open questions (decided in `design.md`; flagged where the operator may overrule)

1. Which sets run unasked? — SD5 *(product call)*.
2. Does re-seeding ever overwrite a row an operator changed or removed? — SD6 (never silently).
3. How is dispatch switched from code to rows without breaking S13's guarantees? — SD8.
4. Is a custom agent dispatchable? — SD7 (not in this slice).

## 5. Acceptance criteria

1. **Every first-party agent is a complete row** (description, prompt, tools, temperature, handoff, policy-agent,
   version, `internal`), produced by the seed process — none is a stub.
2. **Dispatch reads only the row.** Deleting `internal/handler/bodies` from the *runtime* path (a build-tagged test
   binary that links the seed source only into the seeder) leaves every first-party loop dispatching identically.
3. **Lossless:** for all 33 bodies, `Compile(handler built from the row)` equals `Compile(handler built from the
   embedded body)`, byte for byte, including tools, temperature, handoff schema and policy-agent.
4. **Idempotent and versioned:** a second apply changes nothing; a higher record version updates the internal row in
   place; a lower version is refused, named; a removed record is retired, never auto-deleted.
5. **Inert sets apply at start; guard sets never do.** A guard set is applied only by an explicit `seed.apply`, and a
   test proves the daemon-start path cannot reach it (the D4 guard, restated and extended).
6. **Never overwrite an operator's choice:** a record the operator removed or changed is reported `diverged`/`removed`
   by `seed.status` and skipped by `seed.apply` until an explicit `seed.restore`.
7. **Atomic and audited:** each record's write and its ledger row commit together; each apply/skip/restore/retire is one
   append-only audit row naming the actor (`system` at start, the peer uid on the socket).
8. **Daemon first:** every `aeon seed` verb has a socket method; `seed_status` is a read-only catalog tool; no seed
   *write* is reachable from the narrow surface, agent services or the tool registry (guards with canaries).
9. **Visible:** `seed.status` and the Settings panel list every set with available vs applied version and per-record state.
10. **S43's existing tests** (agents, cards, signature, import, policy slug migration) pass unchanged.

## 6. Handoff status (2026-10-30, planning session)

**Done:** this brief, `design.md` (SD1–SD13 + verified facts), `plan.md`, `tasks/T0.md`–`T9.md`.
**Not started:** all implementation. Run T0 first (no code: re-verify, inventory every consumer of the embedded
bodies, settle the mechanical items).
