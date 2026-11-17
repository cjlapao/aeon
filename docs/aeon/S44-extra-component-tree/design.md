# Design — S44-extra Component tree

Rules are **CT1–CT17**, cited from `plan.md` and the task files. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded here with its reason.

## CT1 — One record, two levels

`component` is the only org record. New columns:

| Column | Type | Meaning |
| --- | --- | --- |
| `parent_id` | `TEXT NULL REFERENCES component(id)` (no ON DELETE action) | `NULL` ⇒ **root**; set ⇒ **child** of that root |
| `archived_at` | `TEXT NULL` | set ⇒ archived (CT9) |

Words used everywhere (code comments, sentences, docs):

- **root** — `parent_id IS NULL`.
- **child** — `parent_id IS NOT NULL`.
- **project** — a root with at least one non-archived child. **Derived, never stored**: views carry
  `kind: "project" | "component"` and `childCount`, computed by the read. A stored kind would be a second truth.
- **tree** — a root and its children.

Removed: table `project`, column `implicit`, column `component.project_id`, table `project_telemetry`, store file
`projects.go`, daemon file `daemon/projects.go`, protocol file `protocol/projects.go`, CLI file `project_verbs.go`,
the dead `store.UpsertComponent` (no production caller, `loops.go:451`), `store.CountComponentsInProject`.

## CT2 — Depth exactly two, enforced twice

The store refuses with a sentence (the door a person sees); the schema refuses when the store is bypassed (the
backstop a test proves). Rules:

1. A parent must exist, must be a root, must not be archived.
2. A node is never its own parent.
3. A node that has children cannot be given a parent.
4. A child cannot be given children (follows from 1).

Schema: `BEFORE INSERT` and `BEFORE UPDATE OF parent_id` triggers on `component` with `RAISE(ABORT, 'component tree:
…')` — the first triggers ever on this table; the trigger names start `component_tree_`. CHECK cannot reference
other rows, hence triggers. Cycles are impossible: depth two plus rule 2.

## CT3 — Roots are optional; "a project has at least one component"

`component_root` (the repository paths, PK the path) is unchanged in shape. What changes: a node may have **zero**
roots. Today every component has exactly one primary root and every read inner-joins it
(`store/components.go:235,255`) — those reads become `LEFT JOIN`s and the view's `root` becomes optional.

The invariant **"every node has a primary root, or is a root with at least one child"** is the operator's "a project
needs at least a component". It cannot be a cheap schema rule, so:

- the store's write paths keep it (promote creates parent and adopts the child in one transaction; detach and archive
  refuse to leave a repo-less root with no children — "this project has no repository of its own; detaching its last
  component would leave an empty project. Attach another component first, or archive the project");
- legacy rows that violate it (a migrated group with no components) are **kept** and reported: the view carries
  `empty: true`, and `aeon doctor` prints one line per empty project (CT5).

A child **must** have a primary root (it is a repository). A root may have its own root (a monorepo root whose
children are subpaths) or none (a pure project).

## CT4 — Tree operations (store, then every door)

| Operation | Effect | Refusals (each a sentence + a test) |
| --- | --- | --- |
| `create {…, parentId?}` | new node; with `parentId` it is a child. A child requires `root`; a root without `parentId` requires `root` too (a new **pure** project is only made by `promote`) | parent missing/archived/a child; root already registered (existing rule) |
| `update {id, …}` | metadata and settings; **cannot change `parent_id`** (one way to do each thing) | unknown id; archived (only `restore` touches an archived node) |
| `promote {id, name, slug?, shortDescription?, description?, icon?}` | one transaction: create a repo-less root, set `id`'s parent to it. `id` keeps its id, roots, loops, rollups | `id` is a child; `id` has children; `id` archived |
| `attach {id, parentId}` | a root with no children becomes a child of `parentId` | `id` has children; `id` is already a child (detach first); `id` has no primary root; parent rules (CT2) |
| `detach {id}` | a child becomes a root; inherited settings are **materialized** (CT6) so behaviour does not change | `id` is a root; would leave a repo-less parent with no children (CT3) |
| `archive {id}` | sets `archived_at`; hidden from default lists; `loop.start`/queue advance on its roots refused ("component X is archived — restore it to start work") | `id` has non-archived children (archive or detach them first); would leave a repo-less parent empty |
| `restore {id}` | clears `archived_at` | parent archived (restore the parent first) |
| `list {parentId?, rootsOnly?, includeArchived?}` | flat list with `parentId`, `kind`, `childCount`, `empty`, effective settings | — |
| `show {id}` | node + children (non-archived unless asked) + effective settings with sources + roots | unknown id |

No delete exists before or after this slice (measured: no `DELETE FROM component|project` anywhere).

Every write updates `updated_at` and goes through the store's one write path per operation; the records path stamps
the actor as today (S38). Telemetry rollups move with attach/detach/promote (CT7).

## CT5 — The migration mapping (ids stable, nothing lost)

Two timestamped migrations (`date +%Y%m%d%H%M%S`), each one transaction, each consistent on its own:

**Migration A — `…_component_tree.sql`**

1. `zz_ct_project_map(project_id PK, component_id, disposition)` is filled from `project` + `component`:
   - implicit **and exactly one** component ⇒ `collapse`: maps to that component's id;
   - implicit with **zero** components **and no reference anywhere** (epic, goal, automation_event, event_trigger
     column or stored match JSON, project_telemetry with any non-zero counter) ⇒ `drop`;
   - everything else (explicit; implicit with ≥ 2 components; implicit with 0 components but referenced) ⇒ `keep`:
     maps to **itself** — the project's id becomes a component id.
2. `component_new` is built with `parent_id`, `archived_at`, nullable inherited settings (CT6), without `project_id`.
   Rows: every existing component (`parent_id` = the map's target when the disposition is `keep`, else `NULL`;
   settings copied verbatim — never nulled, so behaviour is byte-identical), plus one row per `keep` project: same id,
   `name`, `short_description`, `description`, `icon` = `icon_emoji` or `icon_image` (CHECK guaranteed never both;
   the component icon accepts both shapes — `daemon/components.go:17`), `slug` = the slugified name (T0 measures
   collisions; slug is not unique today), repository columns `NULL`, root-level settings at their column defaults,
   `created_at`/`updated_at` preserved.
3. Children of `component` are **parked and restored** (the 0018/0019 pattern; the 0035 lesson — `DROP TABLE` on an FK
   parent deletes first and cascades, and `PRAGMA foreign_keys=OFF` is a no-op inside the migration transaction):
   `component_root` (CASCADE), `component_telemetry` (CASCADE), `goal_component`, `task` (nullable FK, NO ACTION).
   T0 decides per child (park vs. alternative) with measured row counts; the default is park.
4. `component_telemetry` gains one row per `keep` project, copied from its `project_telemetry` row (the project row
   already is the total of its components — CT7's new meaning, so it is a copy, not a sum).
5. Guards: a copy guard (`CHECK (moved = total)`) for every copied table, a final `pragma_foreign_key_check()` guard,
   and the depth triggers created **after** the data is in (the data is depth-two by construction; a test proves it).

**Migration B — `…_project_rekey.sql`**

1. `epic` and `goal` are rebuilt: `project_id` ⇒ `component_id TEXT NOT NULL REFERENCES component(id)`, values mapped
   through `zz_ct_project_map`. `goal`'s children (`goal_component` CASCADE, and any FK T0 finds — `brief`?) are
   parked. A trigger refuses an epic/goal on a child (CT10).
2. `automation_event.project_id` and `event_trigger.project_id` are renamed `root_id` (`ALTER TABLE … RENAME COLUMN`,
   no rebuild) and their values mapped. Stored trigger match JSON: the key `project` is rewritten to `root` and its
   values mapped (SQLite JSON functions; T0 counts rows and writes the exact statement; CT11).
3. `project_telemetry` and `project` are dropped (in that order), then `zz_ct_project_map`.
4. Guards as A.

**Census and drills (T1, T11):** a fixture store with every shape — explicit project with 0/1/3 components, implicit
with 0 (referenced and unreferenced)/1/2 components (the ≥ 2 case is reachable by today's move), epics/goals on
each, triggers with `project` in their match, automation events, telemetry on every tier — upgraded and compared
row by row to the expected mapping. Fresh vs. upgraded schema identical (`TestAFreshSchemaAndAnUpgradedSchemaAreIdentical`
extended). The literal migration-count assertions are bumped by exactly two (T0 lists them).

## CT6 — Inherited settings (one level)

Columns that inherit: `queue_advance`, `sizing_mode`, `max_parallel_agents`, `default_branch`, `branch_prefix`.

- On a **root** they are NOT NULL in effect (a trigger refuses `NULL` on a root; the column defaults hold for new
  roots).
- On a **child** `NULL` means *inherit from the root*; a value means *override*. Existing CHECKs become
  `x IS NULL OR x IN (…)`.
- One resolver, `store.EffectiveComponent(id) (EffectiveComponent, error)`, returns each value with its **source**
  (`own | root | default`). Every reader goes through it: `ComponentQueueSettings` (`store/queue.go:310`),
  `componentCap` (`loop/advance_parallel.go:40`), branch readers (T0 lists them). A test greps that no other code
  reads those columns.
- Migration copies every existing value as is, so nothing inherits until a person clears a child's value.
- `detach` materializes each inherited value onto the child before clearing `parent_id`; `attach` keeps the node's own
  values (they become overrides). Both are tests.
- Non-inherited fields: name, slug, descriptions, icon, owner, tags, remote/repo keys.

`update` gains an explicit `inherit: ["queueAdvance", …]` list to clear an override (absent never means unset — the
S38 keep-what-you-were rule; `projects.go:41-43` precedent).

## CT7 — Telemetry: one table, subtree totals

`component_telemetry` keeps its columns. Meaning: **a node's row is the total of its own loops and its children's.**
For a child or a standalone root that is just its own; for a project it is what `project_telemetry` held.

- Bump: the node, its parent when it has one, and the workspace — each **once** (a root with its own repository and
  children is bumped once per event, not twice). Rewrites `bumpTelemetryRollups` and `resolveTelemetryScope*`.
- `attach`/`detach`/`promote` transfer the node's row to/from the parent in the same transaction (replaces
  `transferProjectRollups`, keeps its exactness tests: `TestMoveTransfersRollupExactly` and siblings, re-pointed).
- `cost_state` floor is recomputed on the parent after any transfer (replaces `recomputeProjectCostState`).
- `telemetry.project` is removed; `telemetry.component` on a project returns the subtree total and `childCount`.
  "Own only" for a project with its own repository = its row minus its children's rows (documented, not stored).

## CT8 — The socket surface (P34; daemon first)

Removed: `project.create`, `project.update`, `project.list`, `project.show`, `telemetry.project` (constants, handlers,
`Table.Projects`, fixtures, handshake entries, `contract.ts` entries via protogen).

`component.*` (all `RequiresInitialize`, none `Debug`; human socket **and** catalog twins, CT9):

| Method | Params | Result |
| --- | --- | --- |
| `component.create` | existing minus `projectId`, plus `parentId?` | `ComponentView` |
| `component.update` | existing minus `projectId`, plus `inherit?: string[]`; inheritable fields optional | `ComponentView` |
| `component.list` | `{parentId?, rootsOnly?, includeArchived?}` | `{components: ComponentView[]}` |
| `component.show` | `{id}` (becomes an explicit constant, not only catalog-projected) | `{component, children, effective}` |
| `component.promote` | `{id, name, slug?, shortDescription?, description?, icon?}` | `{project: ComponentView, child: ComponentView}` |
| `component.attach` | `{id, parentId}` | `ComponentView` |
| `component.detach` | `{id}` | `ComponentView` |
| `component.archive` / `component.restore` | `{id}` | `ComponentView` |

`ComponentView` = existing fields minus `projectId`, plus `parentId?`, `kind`, `childCount`, `empty`, `archivedAt?`,
`root?` (now optional), and `effective: {queueAdvance, sizingMode, maxParallelAgents, defaultBranch, branchPrefix}`
each `{value, source}`.

One new error code for tree refusals (`ErrComponentTreeRefused`, the reason in `what`, a three-part `data`, one
conformance fixture per refusal family) from the next free block T0 records. Other consumers' wire names change with
the model: `goal.show` `projectId` ⇒ `rootId`; trigger views/params `projectId` ⇒ `rootId`; task list `project` ⇒ `root`.
`ComponentRootKnown`'s stale comment ("through `project.create`", `method.go:583-589`) is corrected.

Rule for removing methods: the app and daemon ship together and the contract is generated; removal is allowed and
listed in the handshake test's dated comment (T0 confirms no rule forbids it; if one does, the slice stops and asks).

## CT9 — Agent tools (rule 2; one definition, two doors — P6)

These writes do not widen any guard, so they are **direct** tools (as `component_create` is today), not proposals.

- Removed specs: `project_list`, `project_show`, `project_create`, `project_update`.
- Changed: `component_list` (`project_id` filter ⇒ `parent_id`, `roots_only`), `component_show` (children + effective
  settings), `component_create` (`project_id` ⇒ `parent_id`; the "empty makes an implicit group" text goes),
  `component_update` (inheritable fields + `inherit`).
- New: `component_promote`, `component_attach`, `component_detach`, `component_archive`, `component_restore`
  (`Mutates: true`, class `detectable`).
- `epic_list`/`epic_create`/`goal_list`/`loop_list`: `project_id` ⇒ `root_id` (a root component id).
  `catalog_test.go:48-56`'s project_id rule becomes the same rule over `root_id`.
- `TestRegistryExactNames` (both want lists, `toolset_test.go:155,243`) bumped **deliberately and counted** (−4 +5;
  compute from T0's real count), `testdata/rerun_classes.json` updated. Registered, not granted (default grant
  unchanged). Handler bodies (`chat.md`, `proposal-author.md`) keep `component_list` — unchanged name.
- Records identity: `Identity{ComponentID, ProjectID}` ⇒ `Identity{ComponentID, RootID}` (`daemon/records.go:89-108`);
  `ownSubject` checks a goal/epic against the agent's **root**.

## CT10 — Epics, goals, tasks: attached to roots

- `epic.component_id` and `goal.component_id` must name a **root** (store refusal + trigger). A goal's
  `goal_component` members must be the root itself (standalone) or its children ("a component of THIS project",
  `catalog.go:73-80`, kept with the new meaning).
- `ListTaskRows` project filter (`tasks_board.go:60`) ⇒ `root` filter: `component_id = ? OR component_id IN (SELECT id
  FROM component WHERE parent_id = ?)`.
- `componentProject` (`store/automation.go:160`) ⇒ `componentRoot(tx, id)`: the parent id, or the id itself for a root.
- Kickoff data keeps the template field name `Project` (stored kickoff templates may name `{{.Project}}`): its value is
  the **root's name** (`daemon/runs.go:162-165`). A comment says why the name stays.

## CT11 — Automation and triggers

- Events carry `root_id` and `component_id`; the closed match grammar (S40 AU6) key `project` becomes **`root`**
  (`store/trigger_match.go:29,172-173,203-204`), migration B rewrites stored match JSON (CT5 B2). A trigger created
  with the old key after the slice is refused with a sentence naming `root`.
- `daemon/triggers.go:145 checkScope(projectID, componentID)` ⇒ `checkScope(rootID, componentID)`: the component, when
  given, must be in that root's tree.
- `automation/dispatcher.go:185-187` and `recover.go:76-78` follow the rename.

## CT12 — What deliberately does not change

- `component_root` paths, the `--component` path flag across the CLI (it is a **root path**, not an id).
- Every `ComponentRoot` parameter on loop, queue, roadmap, triage, graph, retrieval, terminal, git, history — they
  name repository paths and are untouched.
- `loop.scope` (unused, no writer) — left; its comment updated to say "project" now means "the root of a tree".
- Learnings `scope='project'` keyed by root path — unchanged (S52).
- The workspace telemetry tier.

## CT13 — The CLI (P5; rule 1)

New `aeon component` (a client of the socket methods; the CLI has had no component verb at all — a rule-1 gap this
slice closes): `list [--parent ID] [--roots] [--archived] [--json]`, `show ID [--json]`, `create --name --root [--parent
ID] …`, `update ID …  [--inherit FIELD,…]`, `promote ID --name N [--slug S] …`, `attach ID --to PARENT`, `detach ID`,
`archive ID`, `restore ID`. `aeon project` is removed: the verb prints one fixed line, *"`aeon project` was replaced
by `aeon component` (component tree, S44-extra) — see `aeon component --help`"*, and exits non-zero. `aeon telemetry
project` likewise. Flags elsewhere: `goal list|create --project` ⇒ `--root`, `task list --project` ⇒ `--root`;
the old flag is refused with a sentence naming the new one. No offline fallback is added (none of these verbs has one
today; P5's direct door is not required here — recorded deviation).

## CT14 — The app: compatibility only

The UI is adapted **only** to compile and pass its tests on the new contract, with no new design:
`ProjectsScreen.tsx` and `layers.ts` read `component.list`/`component.show` (a root with children renders where a
group rendered; the "fold implicit" rule becomes "show roots"); bench `TelemetryPage.tsx` drops the project tier and
the move control becomes attach/detach; `ParallelAgentsPage.tsx` reads `effective.maxParallelAgents`;
`AutomationPage.tsx` and `lib/protocol.ts` follow `rootId`. Tests follow. The real tree UI is the next slice.

## CT15 — Planned-slice alignment (done 2026-10-30, with this plan)

Edited in place (none being built): **S47** (catalog spec counts), **S49** (automation scope `projectId` ⇒ `rootId`,
grammar key `project` ⇒ `root`, CLI `--project` ⇒ `--root`, pickers read `component.list`), **S50/S51** (board
`--component` accepts a root and means its tree), **S52** (the agent's "projects" are the roots it has run in; the
`project` learnings scope stays a root path), **S53** (`coverage {project?}` ⇒ `{root?}`, CLI `--project` ⇒ `--root`,
pickers from `component.list`), **S54** (`telemetry.project` removed). Not edited: **S43** (built), **S44** (in
flight — T10 re-checks it after merge), S43-extra, S45, S46, S48 (no reference to the old model).

## CT16 — Order and coexistence

S44 is in flight and may add migrations; this slice's migrations are timestamped after whatever is merged when T2
starts, and its rebuild reads the **current** `component` DDL (P6 rule). S43-extra touches `agent`/seed tables only —
disjoint; either order. S45+ build on this slice's model.

## CT17 — Security notes

No new outward surface: nothing here is added to `narrow.Actions()` or agent services (a sibling guard asserts the
nine new/changed `component.*` methods stay off the narrow surface — `records_guard_test.go` pattern). Archive is not
delete. No secret is involved. Audit continues through the existing records path; summaries are fixed templates from
ids (SEC-033).
