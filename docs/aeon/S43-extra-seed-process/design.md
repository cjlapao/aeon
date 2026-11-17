# Design — S43-extra Seed process

Written 2026-10-30. Rulings **SD1–SD13**. SD1 is the operator's rule (2026-10-30); SD5 is a flagged **product call**;
the rest are documented technical calls checked against the code (brief §1 lists the facts and their locations; T0
re-verifies them). Do not re-open a ruling without recording the reason in that task's Notes.

## SD1 — Every managed record is a row; code is the seed source, never the source of truth

A *record* is anything a person could want to list, read, copy, export or reference by identity: agents, policies,
MCP server definitions, blueprints, tool metadata, automation definitions, presets. At runtime the daemon reads the
row and **only** the row. A record defined in code is a **seed record**: an embedded, versioned definition that the seed
process writes into the database with `origin='system'`. A *definition of shape* — the closed policy vocabulary, S44's
setting declarations, protocol methods, tool executors — is not a record and stays code; the test is "could a person
reasonably want to list/read/copy it?" (S44 keys: no. A system agent's prompt: yes.)

## SD2 — A seed set is declarative, declared per area, in its own file

```go
type Class string  // "inert" | "guard"  (SD5)
type Set struct {
    ID      string         // "agents.firstparty", "policy.corpus", "mcp.servers", "blueprints.standard" …
    Class   Class
    Version int            // the set's release number; bumps when ANY record's content changes
    Records func() []Record
}
type Record struct {
    Key     string         // the stable identity inside the set — the domain's own key (an agent slug, a policy id)
    Version int            // the record's own monotonic version
    Content []byte         // canonical JSON the domain's Applier understands
}
```

A set is registered by an `init()` in `internal/seed/decl_<area>.go` (one file per area, so parallel worktrees add files,
not edits to one list — the S44 CS3 / migrations-README collision argument). `mustRegister` panics on a duplicate set
ID, a record with an empty or duplicate key, a non-positive version, or content that fails the domain's own
validator (the check the seed would hit at apply time, moved to `go test` time — the `bodyEntries` init-panic stance,
S13, but at test time instead of at every process start). The **embedded source** (markdown bodies, YAML, JSON corpus)
is parsed by each domain's `Source` function into `[]Record`; the engine never parses domain formats.

## SD3 — The ledger and the audit: two tables, one transaction per record

`seed_ledger (set_id, record_key, applied_version, applied_hash, state, applied_at, applied_by, PRIMARY KEY (set_id,
record_key))`, `state ∈ ('applied','removed','diverged','retired','failed')`; `seed_event` — append-only by trigger (the
S44 `setting_event` / S45 `policy_change` precedent): `id, set_id, record_key, op ∈ ('apply','update','skip',
'restore','retire','adopt'), from_version, to_version, actor_kind, actor_id, occurred_at`. **A record's write and its
ledger row commit in one transaction** (the domain `Applier` receives the open `*sql.Tx`), so there is never a row
without a ledger entry or the reverse; a crash leaves the previous state intact. The hash is SHA-256 of the canonical
`Content`; it is how `status` knows a row still equals what was applied.

## SD4 — Origin on every seedable table; "internal" keeps meaning what each domain already says

Each domain table gains `origin TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user','import'))` (agents
already have `internal` and `signed`; `origin` is added alongside and `internal ⇔ origin='system'` is asserted by a
test — S43's `internal` flag and its edit/delete refusals stay exactly as built). What a `system` row refuses is
**the domain's rule, not the engine's**: agents — edit and delete refused, duplicate allowed (S43 AM1); policy — see
S45; MCP/blueprints — their slices. The engine only guarantees provenance, versioning and non-clobbering.

## SD5 — Two classes: inert sets apply unasked; guard sets only by a human *(product call)*

*Overrule means guard sets also auto-apply — which contradicts D4 and PL1, so it needs its own amendment.*
- **inert** (`agents.firstparty`, `blueprints.standard`, tool metadata, MCP server *definitions* shipped
  **disabled**): applied automatically **at daemon start, after migrations and before the daemon accepts loops**, and by
  `aeon init`. Nothing here changes what any loop is permitted to do.
- **guard** (`policy.corpus`, MCP grants and enabled servers, anything that adds an `allow`, enables a server or widens a
  grant): **never automatic.** Applied only by `seed.apply` over the socket human door (peer-stamped) or the CLI client
  of it, after `seed.plan` showed the diff. `aeon init` (S23) is an explicit operator verb and *offers* the guard
  plan, applying it only on a confirmation it prints. A **daemon-start path that could reach a guard set is a test
  failure** (`TestStartupSeedNeverReachesAGuardSet`, with a canary), extending D4's existing guard rather than
  replacing it.
A set declares its class; the engine **refuses to register a set whose Applier is flagged guard-affecting as inert**
(each `Applier` carries `GuardAffecting() bool`; `mustRegister` cross-checks).

## SD6 — Never clobber an operator's choice

Each domain declares what an operator may do to a system row (`Applier.Operator() OperatorRights`: `Edit`, `Remove`).
When a system row has been **removed** or **changed** by an operator, the ledger moves to `removed` / `diverged` (detected
by the engine comparing the live row's content hash to `applied_hash` through `Applier.Fingerprint`). `seed.apply` then
**skips it and reports it**; only an explicit `seed.restore {set, key}` (human door, audited) re-applies it. A record
whose version in code is higher than `applied_version` *and* whose row is unmodified is updated in place
(`op=update`); a modified row is reported `diverged` with both versions, never overwritten. A record that disappears from
a later release is `retired` (flagged, row kept, never auto-deleted).

## SD7 — DB-driven dispatch covers internal agents only; a custom agent is not dispatchable here

First-party dispatch moves from the init-time `bodyEntries` map to the **`agent` row** (SD8). A user-created or imported
agent (`origin='user'|'import'`) has no validated tool allow-list or handoff schema — the loader's guards (O1: tools ⊆
registry; `BuildSchema` for handoff; the mechanical `Verify` findings) have never run on it. This slice does **not**
make such a row runnable: dispatch by a non-system slug refuses with a named sentence ("agent `x` is a custom agent and
is not dispatchable yet"). Making custom agents runnable means running the same guards at write time and is a later
slice's decision — flagged, tested (`TestCustomAgentIsNotDispatchable`), not assumed.

## SD8 — Switching dispatch to rows without breaking S13

`internal/handler` keeps its **parser, validators, `Compile`, `Verify` and `BuildSchema`** (the guards that make a body
safe) unchanged; what changes is the **source** of a `Handler`. Steps, each characterized first: (1) extend `agent`
with the columns dispatch needs — `tools TEXT` (JSON array), `temperature REAL`, `handoff TEXT` (JSON object),
`policy_agent TEXT`, `body_hash TEXT` — by a new migration file; (2) the `agents.firstparty` Source parses the embedded
bodies with the **existing** `parse` + `Verify` + `BuildSchema` (so a bad body still fails at `go test`) and emits
records whose `Content` carries exactly the fields `Handler{Name, SystemPrompt, Tools, Handoff, PolicyAgent}` needs;
(3) a `handler.FromRow(store.Agent) (Handler, *Reject)` re-runs the same validators on the row's content (defence in
depth: a hand-edited internal row cannot smuggle a tool the registry lacks); (4) `handler.Lookup/Build/Names/…` read
through a `Source` interface whose production implementation is the store, with **the embedded map kept only as the
seed source and as the `Source` used by tests that build no database**; (5) the **lossless round-trip test** (acceptance
3) walks all 33 bodies. Lookup keeps its by-name signature for now (S43's slug-keyed dispatch is whatever S43 shipped —
T0 reads it); the key mapping lives in one function. Ordering guarantee: the daemon seeds inert sets **before** it
accepts a loop, so dispatch never meets an empty `agent` table; if the table lacks a first-party row at dispatch, the
loop refuses with a sentence naming the missing agent and `aeon seed apply`, never falling back to the embedded copy
(a silent fallback would reintroduce two sources of truth).

## SD9 — Adopting S43's stub rows

Existing databases have first-party `agent` rows that are stubs (`version '1'`, empty content). The agents applier
**adopts** them: same slug, `origin` backfilled to `system`, content filled, `version` raised to the record's version,
ledger entry created with `op='adopt'`. Adoption never changes `slug`, `id`, `created_at`, or any non-content column, and
never touches a row whose `internal` is 0 (a user agent that happens to share a slug is a **conflict**: reported, never
overwritten — S43's slug-collision rule applies).

## SD10 — The operations (P34: daemon first)

| Method | Params | Result | Door |
| --- | --- | --- | --- |
| `seed.status` | `{ set? }` | per set: `{id, class, availableVersion, appliedVersion, records:[{key, state, availableVersion, appliedVersion}]}` | read |
| `seed.plan` | `{ set, keys? }` | the diff `apply` would make, per record, with the reason for every skip | read (no write) |
| `seed.apply` | `{ set, keys? }` | per record `{key, op, fromVersion, toVersion}` | **human door** (writes) |
| `seed.restore` | `{ set, key }` | the record re-applied | **human door** |

All `RequiresInitialize`, none `Debug`. `seed.status`/`seed.plan` are reads; `apply`/`restore` stamp `table.ActorFor`
and refuse an unresolvable peer. Startup auto-apply of inert sets uses actor `system:startup` and is the **only** apply
path without a peer. A read-only catalog tool `seed_status` (records catalog, `Mutates:false`) lets an agent see what is
out of date; **no tool applies or restores**. The write methods are absent from the narrow surface, agent services and the
tool registry (guards with canaries, as S44 CS9 / S45 PM10).

## SD11 — The UI: a "System records" panel in Settings

Descriptor-driven from `seed.status`: each set with class badge (`inert` / `guard`), available vs applied version, record
counts by state, a "plan" action (shows the diff), "apply" (inert: one click; **guard: confirmation naming each record
class and what it permits**), per-record "restore" for `removed`/`diverged`. Empty/up-to-date and failed states are
distinct (never a blank panel). It is a panel on the real Settings screen, not a bench page.

## SD12 — What every later slice declares

A management slice adds: (1) an `internal/seed/decl_<area>.go` file with its `Set`, (2) its domain `Applier`
(`Upsert(tx, Record)`, `Fingerprint(tx, key)`, `GuardAffecting()`, `Operator()`), (3) the `origin` column on its table,
(4) its Settings list's "system" badge and refusals. **No slice invents another way to ship a default.** The S45 refactor
(`policy.corpus`, guard) is the first worked example.

## SD13 — What does not move

S43's card format, signature verification and import; `plugin_agent`'s lifecycle machine; the handler *validators*,
`Compile`, `Verify` and `BuildSchema`; the policy gate; `internal/blueprint`'s file-based standard blueprints and the MCP
config file (S48/S46 move them, each declaring its set); the closed policy vocabulary; every S44 setting declaration.

## Open items left to T0 — mechanical

1. Re-verify the brief's facts; list **every** consumer of `handler.*` and `bodyEntries` with file:line (the ~9 sites above
   are the planning session's grep; T0's list is authoritative).
2. What key S43's dispatch actually uses (slug or name) and what S43's T3 did to `handler/registry.go`.
3. How `aeon serve` starts (where "after migrations, before accepting loops" is) and how `aeon init` runs.
4. Whether the migration parser carries a trigger body (S44 T0 item 3.1's answer).
5. The real next free error codes and the `protogen`/handshake touch points (as S44 T5).
6. Whether any first-party handler body fails to round-trip through `agent` columns (e.g. a front-matter field with no
   column) — the T4 migration's column list is whatever T0 proves is needed, not this file's guess.
7. What `daemon.Options` field carries the seed service and where daemon start calls inert apply.
