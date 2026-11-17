# Brief — S50 Roadmap management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 3 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): "already the most advanced of this group — it has real bench CRUD".
Task shapes are cited from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P11**). **Depends on:** S26 (roadmap), S24 (queue), S44 (settings), S45 (desk/commit, UI), S47 (surface classification), S48 (blueprint rows, for the pickers),
S43-extra (the seed engine, for the one embedded blueprint). T0 re-checks what has merged.

## 0. The standing rules this slice carries

1. **Daemon first.** The socket already has all nine `roadmap.*` verbs and the CLI mirrors them one-for-one; the gap is *management*, not parity.
2. **Every management operation gets a tool.** Reads open; guard-widening writes propose-only. Dispatching a roadmap item **starts loops** (`ideate`, `plan`, `materialize`), which stays a person's act (R6; the agent-callable start is the S39 `run_start` tool, ask-gated).
3. **A list with details** and **4. every record a row, system ones seeded** — roadmap items are already rows and are never shipped; but the roadmap's dispatch has **code-defined records hiding in it** (§1), which rule 6 says must be records.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Fact | Evidence |
| --- | --- |
| **Built and daemon-first.** Nine socket verbs `roadmap.add/list/show/cancel/estimate/priority/ideate/plan/materialize`, the CLI `aeon roadmap` with the same nine, a real app screen (`ui/src/features/roadmap/RoadmapPage.tsx`, a Gantt) **and** a bench page with CRUD over the same doors. Migration 0043; the `internal/roadmap` door (11 measured edges); a dispatch + reactor joined at `publishLoop`. | `method.go` (roadmap consts), `cmd/aeon/roadmap.go:35-51`, `S26-roadmap/` |
| **The lifecycle machine** is closed: `idea → ideating → brief-agreed → planned → ready → running → done`, `{idea…ready} → cancelled`; `running` has **no** edge to `cancelled`; `done`/`cancelled` are terminal; self-transition idempotent. | `internal/roadmap/transitions.go:7-45` |
| **Item shape:** `component_root`, `title`, free-text `epic`, `status`, three-state priority (`known|assumed|unknown` + label), estimate range with a mandatory basis, `provenance` (`authored|triggered|triaged`), `brief_path`, `plan_path`, `loop_slug`. | `store/schema.go:2321` |
| **No edit.** There is no way to change a title, an epic, or a brief/plan path after `add`; only `cancel`, `estimate`, `priority`. | the nine verbs |
| **A code-defined blueprint choice hides in dispatch.** Ideation starts the standard **`brainstorm`** blueprint (const `roadmapIdeationBlueprint`) and planning runs an **embedded YAML string** (`planHostBlueprintYAML`) that is not a blueprint record at all. Neither is configurable or visible in the app. | `internal/daemon/roadmap_dispatch.go:67,217` |
| **`epic` is free text** on a roadmap item while S38 made epics a real entity (`epic` rows). | `schema.go:2321` vs S38 |
| **No agent tools** for the roadmap in the tool registry or records catalog. | `internal/tools`, `internal/records` |
| **No audit** of who changed an item (provenance is only the creation source). | schema |

## 2. What this slice is

1. **Complete the management surface**: `roadmap.update` (title, epic, brief/plan path — the fields that were never editable), a per-item append-only change log (`roadmap.history`), a **board read** with details (readiness `IsReady` and its blocker sentence, the loop it started and that loop's status, estimate/priority honesty states rendered by one formatter), and a `roadmap.vocabulary` read. **Terminal states stay terminal** (R1): there is no reopen.
2. **Make the hidden choices records and settings**: the embedded plan host YAML becomes a **standard blueprint record** (`roadmap-plan`, in S48's `blueprints.standard` set), and the two blueprint choices become S44 **settings** (`roadmap.ideationBlueprint`, `roadmap.planBlueprint`) with a blueprint picker in the UI and a **health** read (does the named blueprint exist and validate *now*).
3. **Audit** every roadmap write in an append-only `roadmap_change` (P3).
4. **Agent tools**: reads (`roadmap_list`, `roadmap_show`) and a propose-only `roadmap_propose_idea` (new proposal kind `roadmap_idea`; a person commits; provenance `authored`).
5. **The real app screen** gains the management panels (edit, history, settings, dispatch with the daemon's authority statement), and the **CLI** gains the missing verbs (P5).

## 3. Out of scope

- The lifecycle machine, readiness rule (D4), reactor, queue-first materialization, the Gantt rendering (S26). No new status, edge or provenance value.
- Epics as entities (S38) — `epic` stays text here; linking it to S38 epics is named for a later slice.
- Starting loops from an agent (S39 `run_start`), the queue (S24) and automation (S49).
- Changing S26/S24/S38/S44/S48 beyond the listed adaptations.

## 4. Open questions — decided in `design.md`

1. Are the dispatch blueprints settings or hardcoded? — RM3 (settings; defaults unchanged). 2. Can an item be edited after it starts running? — RM4 (title/epic yes; paths only before `running`). 3. Can an agent add an item? — RM8 (propose only).

## 5. Acceptance criteria

1. **Behaviour-identical by default**: with default settings, `ideate`/`plan`/`materialize` start exactly the loops they start today (characterized goldens), including the plan host now read from a blueprint **record** whose bytes equal the old embedded string.
2. **Edit is safe**: `roadmap.update` changes only title/epic/brief_path/plan_path, refuses unknown fields and illegal paths, preserves status/priority/estimate/provenance/loop link, is refused for `done`/`cancelled` fields that would rewrite history (paths), and audits once.
3. **Audit complete**: every roadmap write (add, update, cancel, estimate, priority, ideate, plan, materialize, reactor-driven transitions) has one `roadmap_change` row naming the actor (`system:reactor` for reactor transitions); none for reads/refusals/no-ops; append-only.
4. **Settings honest**: the two blueprint settings validate (exists + `Analyze`-clean) at **use** with a named refusal and fall back to nothing silently; the UI picker writes via `settings.set`; `roadmap.health` reports `ok|missing|invalid` per setting.
5. **Details visible**: list/board reads carry readiness, blocker reason, loop status, estimate/priority honesty states via the one formatter; the three-state priority/estimate never collapse.
6. **Agent tools**: reads open; `roadmap_propose_idea` is an inert proposal; no tool starts, plans, materializes, cancels or edits (guards with canaries).
7. **Daemon first**: every `aeon roadmap` verb has a socket method (parity guard both ways); writes absent from the narrow surface/agent services/tool registry.
8. **UI**: edit/history/settings/dispatch through registered socket methods only (P34 grep); dispatch confirms with the daemon's authority statement.
9. **No regression**: S26/S24/S38 suites unchanged except listed adaptations; no status edge added.

## 6. Handoff status

**Done:** this brief, `design.md` (RM1–RM12), `plan.md`, `tasks/T0.md`–`T7.md`. **Blocked on:** S44 (settings) for T3, S48 + S43-extra for the plan-host record (T2), S45 for T4/T6. T0 first.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).
> Specific to this slice: roadmap rows stay keyed by `component_root` (a repository path — unchanged). A board filter that takes a component may name a **root**, and then means the root's whole tree.
