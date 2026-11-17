# Brief — S44-extra Component tree: one record, a root and its children

Written 2026-10-30. Prefix **CT**. Operator decision, same day: *"I want the better solution … we can remove tables and
we should refactor the code to fit this … the tree should have a depth … root and its children … make it our next
priority so we can start working in the UI once it's done, and then we move to S45."*

## 1. Why

S38 (org layers) built two records: `project` (the **group**: name, descriptions, emoji-or-image icon, an `implicit`
flag) and `component` (the **repository**: slug, roots, remote, branch settings, queue/sizing/parallel settings), with
`component.project_id NOT NULL`. A brand-new component gets an **implicit** project of its own. In practice:

- Every standalone repository is a "group of one". The app folds the group layer only when there is **exactly one**
  implicit project in total (`ui/src/features/projects/layers.ts:43`); with five standalone components the person sees
  five groups of one.
- Two forms, two record kinds, two telemetry tiers (`component_telemetry`, `project_telemetry`), two families of
  methods and agent tools for what a person thinks of as one thing.
- Moving a component between groups leaves the old implicit group behind as a row (no delete exists anywhere).

The operator's model: **one record, `component`, with an optional parent — at most two levels.** A **root** is a
component with no parent. A root with at least one child **is a project** (shown as a folder, marked as a project). A
root with no children is a plain component. Internally every node is the same record, configured the same way.
Turning a component into a project is **promotion**: create a new root and make the existing component its first
child. No data moves; every id stays.

## 2. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| `project` table (group) created by legacy migration 0035, with `implicit`, `icon_emoji`/`icon_image` | `internal/store/schema.go:1690-1702` |
| `component.project_id TEXT NOT NULL REFERENCES project(id)` (0035 rebuild, children parked by backup copy only — **no copy guard, no fk guard**) | `schema.go:1713-1752` |
| Later `component` columns: `queue_advance`, `sizing_mode` (0041), `max_parallel_agents` (S32 file) | `schema.go:2218-2220`, `migrations/20261030071034_s32_parallel_agents.sql:2` |
| Tables with an FK to `project`: `component`, `epic`, `goal`, `project_telemetry` (CASCADE) | `schema.go:1729,1775,1788`; `migrations/20261030084236_telemetry.sql:82` |
| Tables with an FK to `component`: `component_root` (CASCADE), `goal_component`, `task` (nullable), `component_telemetry` (CASCADE) | `schema.go:765-773,1808,1903`; telemetry file `:66` |
| Plain-text project/component ids: `automation_event`, `event_trigger` (and the stored match JSON keys `project`/`component`) | `schema.go:2050-2051,2074-2075`; `store/trigger_match.go:29` |
| Implicit projects are written by `CreateComponent` and the dead `UpsertComponent` (no production caller) | `store/components.go:98-105`; `store/loops.go:451-497` |
| Socket: `project.create/update/list/show`, `component.create/update/list`, catalog-projected `component.show`, `telemetry.component/project` | `protocol/method.go:177-186,461-462,988-994,1129-1130` |
| Agent tools: `project_list/show/create/update`, `component_list/show/create/update`; `project_id` on `epic_*`, `goal_list`, `loop_list` | `records/catalog.go:17-61,177` |
| CLI: `aeon project create/update/list/show` (socket clients); **no `aeon component` verb exists** (error strings already name one) | `cmd/aeon/project_verbs.go` |
| Telemetry rollups bump component + project + workspace tiers; moves transfer project rollups | `store/telemetry.go:255,360,394,461,952` |
| UI callers of `project.*`: `ProjectsScreen.tsx`, `layers.ts`, bench `TelemetryPage.tsx` | `ui/src/features/...` |

Full inventories (store, protocol, daemon, CLI, UI, tests, migration-count assertions) are in `tasks/T0.md`.

## 3. What this slice delivers

1. **One record.** `component` gains `parent_id` (self-reference, nullable) and `archived_at`; the `project` table,
   the `implicit` flag and `project_telemetry` are **removed**. Depth is exactly two, enforced by the store and by
   schema triggers.
2. **Lossless migration.** Every existing project and component keeps its id. Implicit groups of one collapse into
   their component; every other group becomes a root component **with the same id**; every reference (epics, goals,
   automation events, triggers and their stored match JSON, telemetry) is re-pointed or kept by construction.
3. **Tree operations, daemon first:** create (optionally under a parent), update, list, show, **promote**, **attach**,
   **detach**, **archive**, **restore** — socket methods, agent tools (rule 2), and a new `aeon component` CLI as
   their client (rule 1). `project.*`, `telemetry.project` and `aeon project` are removed.
4. **Settings inherit down one level.** The operational settings (queue advance, sizing mode, parallel-agent cap,
   default branch, branch prefix) may be left unset on a child and resolve from its root; every read shows the
   effective value **and where it came from**. Existing values never change meaning.
5. **One telemetry tier per node.** A root's row is the total of itself and its children; the workspace tier is
   unchanged.
6. **The app keeps working, unchanged in design.** The UI is adapted only enough to compile and pass its tests on the
   new contract; the real tree UI is the next piece of work, built on this slice.
7. **The planned slices are aligned.** S49, S50–S54 (and S47's counts) were amended on 2026-10-30 to speak the new
   model (see `design.md` CT15). S43 (built) and S44 (in flight) are **not edited**; T10 re-checks S44 after it merges.

## 4. Out of scope

- The tree UI (folders, project marker, promotion flow) — next, on top of this slice.
- More than two levels. A child cannot have children; a root cannot become a child while it has children.
- The learnings `project` scope (it is keyed by a **root path**, not a project id — `internal/learnings/types.go:31-33`);
  S52 owns its naming.
- The unused `loop.scope` column (`CHECK IN ('component','project')`, no Go writer) — left as is, documented.
- Per-person or per-team access to a subtree.

## 5. Acceptance

1. A fresh store and an upgraded store produce the same schema; no `project` or `project_telemetry` table, no
   `implicit` column, no `project_id` column anywhere except the documented `loop.scope` value domain.
2. An upgraded store keeps **every** id, row and rollup: census counts before = after, per the mapping in CT5;
   `pragma_foreign_key_check` empty; copy guards and fk guards in the migrations.
3. Depth two holds against every door: the store refuses, the schema triggers refuse when the store is bypassed.
4. Promote/attach/detach/archive/restore are socket methods, agent tools and CLI verbs, each stamping its actor exactly
   as component writes do today (T0 records whether that includes an audit row — this slice adds none of its own),
   each covered by a parity test.
5. Effective settings: unchanged behaviour for every existing component (characterization goldens byte-identical
   where no difference is intended); children inherit when unset; detach materializes inherited values.
6. `make protogen && make check-protogen && make guard` green; UI `vitest` + lint green on the new contract.
7. Every planned-slice doc that named the old model is amended, and none of S43/S44 is touched.

## 6. Status

Planned. **Next in the build order** (after S44, which is in flight): S44-extra → S43-extra → S45. S43-extra and this
slice touch disjoint code and may run in parallel if wanted.
