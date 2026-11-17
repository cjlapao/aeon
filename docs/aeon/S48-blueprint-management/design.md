# Design — S48 Blueprint management

Written 2026-10-30. Rulings **BP1–BP15**. BP6 is a flagged **product call**; the rest are documented technical calls checked against the code (brief §1; T0 re-verifies). Do not re-open a ruling without
recording the reason in that task's Notes.

## Verified facts (2026-10-30; T0 re-checks)

1. **Files.** Blueprints live at `<store dir>/blueprints/<name>.yaml` (`blueprint.NamedPath`); names match `^[a-z0-9][a-z0-9_-]*$`; symlinks refused. Standard names are `standardNames` (11) backed by `//go:embed blueprints/*.yaml`; `Resolve` materializes a standard one on first use and runs `upgradePristineStandard` (SHA-256 match against `pristineUpgrades` — `two-step`: 2 hashes, `dev-loop`: 1). `Names()` returns standard ∪ custom. (`internal/blueprint/standard.go`.)
2. **Socket** (`internal/protocol/blueprint_loop.go:308-360`, seams `Table.ListBlueprints/BlueprintFields/ShowBlueprint/ValidateBlueprint/SaveBlueprint`, wired in `internal/daemon/blueprint.go:452-520`): `list → {names}`, `fields → {fields}`, `show → {name, path, content}`, `validate → Report`, `save` (≤ 1 MiB; `Analyze(Mode, Agents: handler.Names(), Resolvers, Actions, AgentTools)`; `report.OK(false)`; `report.Blueprint == name`; temp file + rename, or `os.Link` for `createOnly`) — **no delete/duplicate/history/usage/diff**; `save` has no actor.
3. **CLI** (`cmd/aeon/blueprint.go`): `fields`, `list` (**`StandardNames()` only**), `path` (a filesystem path), `validate`. **UI** duplicates client-side (`duplicateBlueprint.ts`: parse YAML, set `id`, `version: 1`).
4. **Pins.** A started loop pins the blueprint **content** in `loop_blueprint` (`blueprint_id`, `version`, `content`, runtime JSON) — immutable; later edits never affect it.
5. **References by name:** `trigger_schedule.blueprint`, `event_trigger.blueprint`, `queue_item.blueprint`, `loop_run` history (`schema.go` lines 1560/1975/2077/2113/2228 — T0 lists the exact tables and which hold *live* references); inside a blueprint: `Step.Agent`, `Blueprint.Converse`, `Step.Skill` (`callee/skill-id`), `Step.Action`, `Step.Resolver`-style tags.
6. **The engine's schema is large and stable** (`types.go`: lanes, ladders, `When`, `Parallel`, `ParallelGroup`, `After`, `Converse`, `Kickoff`, `Output`, `Requires`, `Emits`, `Decides`); `Analyze` returns three-state diagnostics; `OK(strict)`.
7. **AGENTS.md** states "the store is the source of truth; files are a disposable mirror". Blueprints are the exception this slice removes.
8. After siblings: S43-extra changes `handler.Names()` to read agent rows; S43 made lanes name agent **slugs** (T0 confirms what `Step.Agent` holds and what S43's agent-delete check does about blueprints); S45 supplies the generic `proposal.commit` and `internal/desk`; S47 supplies `toolcatalog` and surface classification (the new write methods must be classified there).

## BP1 — Four tables; the YAML text is the data

| Table | Columns |
| --- | --- |
| `blueprint` | `name` PK (grammar CHECK net), `version INTEGER` (the YAML `version`), `content TEXT` (≤ 1 MiB, the source of truth, **as written** — comments and formatting preserved), `content_hash` (SHA-256 hex), `origin ∈ ('system','user')`, `created_at`, `updated_at`, `updated_by` |
| `blueprint_revision` | `id` PK AUTOINCREMENT, `name`, `rev INTEGER` (1.. per name, **never reset**, unique `(name, rev)`), `version`, `content`, `content_hash`, `op ∈ ('create','save','duplicate','revert','seed','update','restore','adopt','import')`, `note` (≤ 256), `actor_kind ∈ ('user','cli','system')`, `actor_id`, `occurred_at` — **append-only by trigger** |
| `blueprint_ref` | `(name, kind ∈ ('agent','converse','skill','action','resolver'), target)` — **derived** from content on every write (BP11), rebuildable |
| `blueprint_import` | `id`, `path`, `sha256`, `result ∈ ('imported','refused')`, `name`, `detail ≤ 1024`, `imported_at` — the file-adoption ledger (BP3) |

`content` is stored **verbatim**, never re-serialized: authors' comments and layout matter, and the engine parses on use. The hash is of those bytes. `blueprint.name == the YAML id` is enforced by the writer (the existing `save` rule). No FK from
`blueprint_revision` to `blueprint` (history outlives deletion); no FK from the `trigger_schedule`/`event_trigger`/`queue_item` name columns (they stay text; the delete check is the application-layer reference accessor, BP10 — S43 AM4's shape).

## BP2 — Reads go through a `blueprint.Source` seam; no fallback

`internal/blueprint` gains `Source` (`Get(name) (raw []byte, found bool, err)`, `Names()`, `Standard()`), with `embeddedSource` (tests and the offline validate path) and a store-backed source supplied at serve through
`daemon`. Production **never** falls back to files or to the embedded copy (two truths). `Resolve`/`NamedPath`/`Names` file semantics survive only inside the adoption importer; their callers (T0's inventory — daemon list/show/validate/save/start, `cmd/aeon` verbs, trigger/queue validation at fire time) move to the seam, characterized first.

## BP3 — Adoption of existing files: once per file, per-file outcome, files untouched

At the first start after the migration the importer walks the state `blueprints/` directory (no symlinks, regular `*.yaml`, name grammar) and, for each file not already in `blueprint_import` by hash: read → `Analyze` (`OK(false)`, the criterion `save` uses) → classify:
**(a)** standard name and `sha256` equals the current embedded bytes or any `pristineUpgrades` hash ⇒ `origin='system'`, **pristine** (the seed then brings it current by the normal versioned update); **(b)** standard name, other content ⇒ `origin='system'` with **the user's content kept** (an *edited shipped blueprint*, BP6) and the seed will report it `conflict/diverged`, never overwrite; **(c)** non-standard name ⇒ `origin='user'`; **(d)** invalid ⇒ **not imported**, one `blueprint_import` row (`refused`, the diagnostics) — never silently dropped, listed by `doctor`. Each import is one transaction (`blueprint` + first `blueprint_revision` `op='adopt'` + `blueprint_ref` + ledger). Files are **never modified, renamed or deleted**; a later edit to a file is ignored and `doctor` reports drift. Importing the operator's own files is carrying their decisions forward (the S46 MG3 argument), so it is a migration, not a seed.

## BP4 — Blueprint content is canonical text; validation is the existing `Analyze`

Every write validates with the **unchanged** `blueprint.Analyze` (same options as today's `save`: `Mode`, agents from the live agent source, resolvers, actions, agent tools) and refuses on `!report.OK(false)` with the report verbatim; there is no SQL-only path and no second validator. The agent-name source for `ValidationOptions.Agents` is whatever S43/S43-extra settled (T0 records).

## BP5 — The `blueprints.standard` seed set (inert)

Records = the shipped blueprints (key = name, `Content` = embedded YAML bytes as a JSON string, per-record `Version` guarded by a golden `testdata/blueprints.versions.golden` exactly like S43-extra T4). Class **inert** (a blueprint permits nothing — policy still gates every call). Applier: `Upsert` writes `blueprint` (`origin='system'`) + a `blueprint_revision` (`op='seed'`/`'update'`/`'restore'`) + `blueprint_ref` in the engine's transaction; `Fingerprint` = content hash; `Operator() = {Edit:true, Remove:false}`; `GuardAffecting()=false`. The hand-rolled `pristineUpgrades` table is **retired** — the engine's rule (unmodified + higher record version ⇒ update in place; modified ⇒ `diverged`, skipped; `restore` explicit) is exactly its intent — with a test proving every hash it recognised is still recognised as pristine at adoption (BP3 a). **T0 resolves the `two-step` discrepancy** (listed in `standardNames`, no embedded file): either an embedded file exists under another listing, or `two-step` is a legacy name — the seed set carries exactly the embedded files and the decision is recorded.

## BP6 — A shipped blueprint is editable; it then reads `diverged` *(product call)*

Today a standard blueprint is a materialized, user-editable file. Keeping that: an operator **may edit** a system blueprint (it stays `origin='system'`, the ledger says `diverged`, `blueprint.show` carries `divergedFromShipped:true` and the diff against the shipped record is one call away), **may duplicate** it into a user blueprint, **may restore** the shipped version (`seed.restore`, explicit), and **may not delete** it (shipped blueprints are referenced by shipped things — queue, roadmap handoffs). *Overrule means system blueprints are read-only and you edit by duplicating (the S43 agent rule) — safer for upgrades, but a behaviour change for existing installs, so not recommended without a deprecation.*

## BP7 — Revisions: complete, append-only, never rewritten

Every successful write (`create`, `save`, `duplicate`, `revert`, seed `update`/`restore`, `adopt`) appends **one** revision with the actor; an identical-content save is `changed:false` with **no** revision; a refusal writes none. `revert {name, rev}` appends a **new** revision whose content is the old one (history is never rewritten); revision numbers per name never reset, even across delete-and-recreate. `blueprint.diff {name, a, b}` computes a unified text diff daemon-side between two revisions, a revision and `current`, or `shipped` (the system record's content) — one implementation, no client-side diffing.

## BP8 — Lint: advisory, dated, never gating

`blueprint.lint` returns `advisories[]` **separate from** `Report` and **never** affecting `Report.OK` or a save: for each model step with an `agent`, for each tool the step's `emits` implies, the agent's state via S47's `toolcatalog.Effective` — `granted_denied`, `granted_no_rule`, `not_granted` become an advisory naming agent, tool, and the **verdict sentence verbatim**; steps naming a non-dispatchable (custom) agent (S43-extra SD7) advise that it will refuse at dispatch. Each advisory carries `evaluatedAt`; the policy can change after the lint, so the label says "as of". The function lives in `internal/blueprint` taking a small interface (`ToolState(agent, tool) (State, Reason, ok)`) so the package imports neither policy nor tools; the daemon supplies the adapter over `toolcatalog`.

## BP9 — Agents read and propose; they never save

Catalog specs (family `blueprint`, one definition with socket twins): `blueprint_list`, `blueprint_show`, `blueprint_validate`, `blueprint_lint` (reads — blueprints are not secret between agents; there is no MC2 analogue here) and `blueprint_propose` (**Mutates**, class detectable): creates an **inert** proposal of a **new kind `blueprint_save`** (`{name, content, createOnly?, note}`) through the same create path as `proposal.create`; `desk.Validate` = `Analyze` + `id==name` + size (create-time refusal, S35 TD3); the commit is S45's generic `proposal.commit` (digest-bound, effect-then-flip), with the **committing person** as the actor. The enum widening is a table rebuild (S37/S46 precedent; T9 reads the **current** DDL). No tool saves, duplicates, deletes or reverts; schemas carry no identity/status fields; a per-agent open-proposal limit (S45's named constant); registered-not-granted; counted bump.

## BP10 — Delete is refused while referenced, and for system blueprints

`Store.BlueprintReferences(name)` returns live references: `trigger_schedule` rows, `event_trigger` rows, `queue_item` rows in a **non-terminal** status (T0 lists the statuses), and any other live reference T0 finds (roadmap/triage hand-offs). `blueprint.delete` refuses with every reference named; for a system blueprint it refuses by name ("a shipped blueprint cannot be deleted; restore or duplicate it"). Loops already started from it are **reported as unaffected** (they carry pinned content), not a reason to refuse. Delete removes `blueprint` and `blueprint_ref`; **revisions remain**. Re-creating the name continues its revision numbering.

## BP11 — References are derived, so "who uses X" is a query

`blueprint.References(*Blueprint) []Ref` (pure, in `internal/blueprint`) extracts agents (`Step.Agent`), `Converse`, skill targets (`callee`), actions, resolver tags; the writer rebuilds `blueprint_ref` for the name in the **same transaction** as the content write; a consistency test re-extracts for every row and compares. This is what lets S43's agent-delete refusal (AM4's blueprint half) and S47/S49 pickers ask SQL instead of parsing text; **S48 T6 adapts S43's merged delete refusal** to consult it (one added check, listed in Notes; S43's tests unedited).

## BP12 — Methods (all `RequiresInitialize`, none `Debug`; conditional on the seam)

Reads: `blueprint.list {origin?, q?}` → **additive**: keeps `names` (compat) and adds `blueprints: BlueprintSummary[]` (`name, origin, version, revision, contentHash, divergedFromShipped, updatedAt, updatedBy, usedByCount`); `blueprint.show {name}` → keeps `name`, `content`, keeps a `path` field **empty** (documented; T0 greps consumers) and adds the summary plus `report` (the Report) and `references`; `blueprint.usage {name}` → `{triggers, schedules, queueItems, loopsStarted (count), agents, actions, skills}`; `blueprint.history {name, limit?}`; `blueprint.revision {name, rev}`; `blueprint.diff {name, a, b}`; `blueprint.lint {name?|content?}`; `blueprint.fields`/`blueprint.validate` unchanged.
Writes (**human door**): `blueprint.save {name, content, createOnly?, mode?, note?}` (existing method, rewritten onto rows; adds the actor from `table.ActorFor`, `changed`, `revision`), `blueprint.duplicate {from, name, note?}` (server-side; rewrites `id` and sets `version: 1` **textually** preserving comments — the one place text is edited, by a tested function replacing the two top-level scalars), `blueprint.delete {name}`, `blueprint.revert {name, rev, note?}`. `shipped`-restore is `seed.restore {set:"blueprints.standard", key:name}` (S43-extra's door), not a new method.

## BP13 — The CLI is a client; `path` retires; `edit` and `export` replace it

Parity table in T0; `list` now matches the socket (custom names included — the one intentional change). `aeon blueprint path` prints a *file* that no longer exists as truth: it becomes a refusal naming `export`/`edit`. New verbs: `show`, `save --file F [--create-only] [--note]` (the CLI reads the file and sends content; the daemon never reads arbitrary paths), `edit NAME` (export to a temp file → `$EDITOR` → `blueprint.save`; invalid ⇒ the temp file is kept and its path printed), `duplicate`, `delete`, `history`, `diff`, `revert`, `usage`, `lint`, `export [--out DIR]` (**a disposable mirror**, never read back). Offline fallback through the same code (S45 PM4 pattern).

## BP14 — UI: extend the existing Blueprints screen; it is a real route, not the bench

`ui/src/features/blueprints/` (`BlueprintsScreen`, `BlueprintEditor`, `BlueprintFlow`, `duplicateBlueprint.ts`) is a real route today. S48 extends it: list with origin/version/diverged badges and usage counts, the editor saving through `blueprint.save` (with the actor-visible revision), lint advisories and the validation report side by side, a revisions tab with diff and revert, duplicate and delete via the daemon (the client-side `duplicateBlueprint.ts` is **retired** for the daemon call), usage with links to the trigger/queue (S49) and agent (S43) views. Daemon-driven; nothing hardcoded.

## BP15 — What does not move

The engine and schema (S8/S17/S32/S38/S39), `Analyze`'s verdicts for existing inputs, `loop_blueprint` pins and every started loop, `loop.start`'s semantics, the policy gate, MCP, the tools catalog, the narrow surface, and every S43/S44/S45/S46/S47 file (S48's one adaptation, S43's agent-delete refusal, is its own task and is listed).

## Open items left to T0 — mechanical

1. Re-verify facts 1–8; resolve the `two-step` embedded-file discrepancy; list every embedded and standard name.
2. The **reader inventory**: every caller of `blueprint.Resolve/NamedPath/Names/StandardNames` and every `blueprint.Analyze` caller (daemon, CLI, trigger/queue/automation validation at fire), daemon vs CLI/offline context.
3. The **reference inventory**: every table/column naming a blueprint and which hold *live* references; the `queue_item` and trigger status vocabularies (terminal vs live).
4. What `Step.Agent` holds after S43 (slug vs name) and what S43's agent-delete refusal currently does about blueprints.
5. Consumers of `blueprint.show`'s `path` field (UI, bench, CLI, tests) and of `blueprint.list`'s `names`.
6. The CLI parity table (every verb/flag → socket method or gap).
7. The proposal-kind CHECK's current DDL (after S46's `mcp_grant_revoke`), the rebuild precedent and the migration-count assertions.
8. Neighbour state (S43-extra engine, S45 desk/commit/UI, S47 `toolcatalog`/surface), the error-code block, handshake/conformance/fixtures/protogen touch points, trigger-body support in `.sql` migrations (S44 T0 3.1), and a baseline of the affected packages.
