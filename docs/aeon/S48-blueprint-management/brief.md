# Brief — S48 Blueprint management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 2 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): the first consumer of agents (S43), policy (S45), MCP (S46) and the tools catalog (S47).
**Depends on:** S8/S17 (blueprint engine), S32/S38/S39 (lane features), S43 (agent slugs), **S43-extra T3** (seed engine), **S45** (`internal/desk`, the proposal commit, the UI components),
**S47** (`toolcatalog`, for the lint advisories). T0 re-checks what has merged. S44 is not a dependency.

## 0. The standing rules this slice carries (CONFIGURATION-ORDER, 2026-10-30)

1. **Daemon first, CLI second.** The socket already has `blueprint.list/fields/show/validate/save`; the CLI has less (`fields`, `list` of *standard names only*, `path`, `validate`).
2. **Every management operation gets an agent-visible tool.** Reads and validation are open to agents; authoring is **propose-only** (an inert proposal a person commits).
3. **A list with details** — what blueprints exist, whether each is shipped or yours, its version, what uses it and what it uses.
4. **Every record is a row — system ones included, seeded.** Blueprints move **out of files in the state directory and into the database**; the 11 standard blueprints become the `blueprints.standard` seed set.
   AGENTS.md already says "the store is the source of truth; files are a disposable mirror" — blueprints are today the exception.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Fact | Evidence |
| --- | --- |
| **Blueprints are files.** `<store dir>/blueprints/<name>.yaml`. Standard blueprints are `//go:embed blueprints/*.yaml` and **materialized to a file on first use** (`blueprint.Resolve`); custom ones are any other `*.yaml` in the directory. | `internal/blueprint/standard.go` |
| **A hash table stands in for versioning.** A pristine older copy of `two-step` / `dev-loop` is replaced with the new embedded bytes when its SHA-256 matches a known shipped revision; any other content is "a person's edit and remains theirs". | `standard.go` `pristineUpgrades` |
| **`standardNames` lists 11 names; 10 `*.yaml` files are embedded** (`two-step` has no embedded file in the directory listing). | `standard.go:24` vs `ls internal/blueprint/blueprints` — **T0 must resolve this** |
| **Socket:** `blueprint.list` (**names only**), `blueprint.fields` (the schema vocabulary), `blueprint.show` (`{name, path, content}` — a *file path*), `blueprint.validate`, `blueprint.save` (validate with `Analyze`, require `id == name`, atomic temp+rename or hardlink for `createOnly`). **No delete, duplicate, rename, revision history, usage or diff.** | `internal/protocol/blueprint_loop.go:308-360`, `internal/daemon/blueprint.go:452-520` |
| **CLI:** `aeon blueprint fields|list|path|validate`. `list` prints `StandardNames()` — **not** the custom blueprints the socket's list includes; `path` prints a *filesystem* path; there is no `show`, `save`, `duplicate`, `delete`. | `cmd/aeon/blueprint.go:22-48` |
| **The UI duplicates client-side** by re-parsing YAML and rewriting `id`/`version` (`duplicateBlueprint.ts`); the editor is `ui/src/features/blueprints/` (442 lines); the bench has its own page. | `ui/src/features/blueprints/` |
| **A blueprint is referenced by name** from `trigger_schedule.blueprint`, `event_trigger.blueprint`, `queue_item.blueprint`, loop-run history, and is **pinned by content** into `loop_blueprint` (id, version, content) when a loop starts. Its lanes name agents as strings (`Step.Agent`, `Blueprint.Converse`). | `internal/store/schema.go` (tables above), `internal/blueprint/types.go:85` |
| **Validation** is `blueprint.Analyze(raw, ValidationOptions{Mode, Agents: handler.Names(), Resolvers, Actions, AgentTools})` → a `Report` with three-state diagnostics (`valid/invalid/unverifiable`). There is **no author-time check against policy**. | `internal/blueprint/analyze.go`, `report.go` |

## 2. What this slice is

1. **Blueprints as records**: `blueprint` (current content, version, origin `system|user`), an append-only `blueprint_revision` history (every save is a revision, with the peer actor), and a derived `blueprint_ref`
   table (which agents/actions/skills/resolvers a blueprint uses) rebuilt from content on every write.
2. **Every reader switches from files to rows**, behaviour-identical (characterized first): `loop.start`, triggers, queue, the CLI, validation.
3. **Adoption of existing files**: pristine standard copies, **edited** standard copies (kept, shown as diverged from shipped — never overwritten) and custom files are imported per file, validated, files never touched.
4. **The `blueprints.standard` seed set** (inert): the shipped blueprints are records; the hash-table upgrade becomes the engine's versioned update / diverged / restore semantics.
5. **Management on the socket** (daemon first): richer `list/show`, `usage`, `history/revision/diff`, `lint` (policy and tool advisories), `duplicate`, `delete` (refused while referenced, naming the references), `revert`, and the existing `save`
   rewritten onto rows.
6. **Agent tools**: `blueprint_list/show/validate/lint` (reads) and `blueprint_propose` (an inert proposal of a new proposal kind `blueprint_save`, committed by a person).
7. **The CLI as a client**: parity, `show`, `edit` (temp file → socket save), `duplicate`, `delete`, `history`, `export` (a disposable mirror), offline fallback through the same code.
8. **Settings → Blueprints**: list with details and usage, the editor over the socket with the lint and diff views, revisions with revert, duplicate, delete.

## 3. Out of scope

- **The blueprint engine and its schema** (S8/S17/S32/S38/S39): lanes, ladders, conditions, parallelism, `after:`/`converse`. No new blueprint feature, no change to `Analyze`'s verdicts for existing inputs.
- **Authoring policy, MCP or agents** (S45/S46/S43) — the lint *reads* them and links.
- **Automation** (S49): triggers/schedules own their blueprint references; S48 provides the reference read and refuses a delete that would strand them.
- **Changing pinned loops**: `loop_blueprint` content is immutable; deleting or editing a blueprint never touches a running or finished loop.
- **Changing S43/S44/S45/S46/S47** (S48 adapts S43's agent-delete refusal inside its own task, listed).

## 4. Open questions — decided in `design.md`, flagged where the operator may overrule

1. May an operator edit a *shipped* blueprint? — BP6 *(product call)*: yes (as today), it then reads `diverged`; delete is refused; restore returns the shipped version.
2. Are existing files imported automatically? — BP3 (once, per file, never modified).
3. Does an agent author blueprints? — BP9 (propose only).
4. Is the policy lint blocking? — BP8 (advisory, time-dated; never fails a save).

## 5. Acceptance criteria

1. **The database is the only source of truth**: with the state-dir `blueprints/` directory renamed after adoption, `loop.start`, triggers, queue and the CLI behave identically; no runtime read of a blueprint file remains outside the importer.
2. **Behaviour-identical**: for every standard and fixture blueprint, `Analyze` reports, `loop.start` pins, `blueprint.show` content and `aeon blueprint list/validate` output are byte-identical from rows as from files (the CLI `list` now also shows custom names — the one *intentional* change, listed).
3. **Adoption is safe and lossless**: a pristine standard copy → system row; an **edited** standard copy → system row with the user's content, `diverged`, **never overwritten by a later seed**; a custom file → user row; an invalid file → reported per file with its diagnostics, **never silently dropped**; files are never modified or deleted.
4. **Revisions are complete**: every successful write creates exactly one append-only revision naming the actor; `revert` creates a *new* revision (history is never rewritten); no revision for a refused or no-op write.
5. **References are derived and consistent**: `blueprint_ref` equals the references re-extracted from content for every blueprint (a consistency test); `usage` lists referencing triggers/schedules/queue items and agents used.
6. **Delete is safe**: refused while a trigger, schedule or open queue item references the blueprint, and for system blueprints; the refusal names every reference; a deleted user blueprint leaves its revisions.
7. **Agent deletion** (S43 AM4) now also names referencing blueprints — via `blueprint_ref`, not text search.
8. **Seeds**: `blueprints.standard` (inert) applies at start; versions guarded by a golden; an edited shipped blueprint is `diverged` and skipped; `restore` is explicit; the pristine-hash table is retired with a test proving every previously-recognised pristine hash is still upgraded correctly.
9. **Lint is advisory and dated**: advisories name the agent, the tool and the policy verdict sentence verbatim, carry the evaluation instant, and **never** change `Report.OK`.
10. **Agent tools**: reads open; `blueprint_propose` creates an inert proposal that changes nothing until a person commits via S45's generic commit; no tool saves, deletes or reverts.
11. **Daemon first**: every `aeon blueprint` verb has a socket method (parity guard); the writes are absent from the narrow surface, agent services and the tool registry (guards with canaries).
12. **UI**: list/inspect/edit/diff/revert/duplicate/delete through registered socket methods only (P34 grep as a test).
13. **No regression**: S8/S17/S32/S38/S39/S40 suites unchanged; pinned loops unaffected.

## 6. Handoff status (2026-10-30, planning session)

**Done:** this brief, `design.md` (BP1–BP15 + verified facts), `plan.md`, `tasks/T0.md`–`T13.md`.
**Not started:** all implementation. Run T0 first (no code). **Blocked on:** S43-extra T3 for T5; S45 (desk/commit) for T9; S47 (`toolcatalog`) for the lint's policy advisories (T7); S45 T9 for shared UI components (T12).

> **Closed 2026-11-03 by T13 (the record):** T0–T13 all built. All thirteen acceptance criteria were re-run
> in the close-out with their named tests (99 named PASSes, 0 FAIL) and walked again against a real binary,
> a live daemon and a second binary built from the commit *before* S48's migration, so the files-era upgrade
> was drilled on a genuinely old store rather than an approximation; the two migrations were drilled six
> ways and the walk is recorded in [`../DEMO.md`](../DEMO.md) §S48 with its transcripts, the browser half
> stated as not run rather than implied (no Tauri libraries on this box). Three things the close-out found
> rather than admired, each named in T13's Notes: **criterion 7 had no test driving its branch** — the
> table's `TestAgentDeleteRefusedNamingReferencingBlueprints` did not exist and T6's Notes said it did, so
> the test is written here, in S48's own file, with the refusals and the re-point-then-delete cycle proved
> against a real store; **`seed restore` cannot see divergence in the session that caused it** —
> `PlanRestore` reads the persisted ledger state, so "restore refused — the record is up to date" answers
> beside a `seed status` that says the opposite until the next apply pass classifies it (S47's plan, quoted
> and left unedited); and **the set's ledger can report one version behind what is already live**, a
> cosmetic `1 behind` line over rows whose bytes already match the shipped record. Two gaps this box cannot
> close are stated rather than hid: the browser half (D17, the adoption note, the desk's badge and diff
> preview) and the full `./internal/daemon/` package, which did not finish under the box's load even at a
> 45-minute test budget — the slice's own 28 daemon tests do, in 29 seconds. What the next slice owes this
> one: a blueprint file is now an **export**, nothing more — `blueprint export` writes one, `blueprint edit`
> round-trips one, and no reader in the daemon opens a blueprint file again; authoring still has no door of
> its own, and that is a future slice's authority discussion, not a flag on these doors.
