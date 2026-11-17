# Design — S50 Roadmap management

Rulings **RM1–RM12** (documented technical calls checked against the code; T0 re-verifies). Task shapes cite [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md).

## RM1 — Edit is a narrow, named set of fields

`roadmap.update {id, title?, epic?, briefPath?, planPath?, clearEpic?}` (strict params). Title 1–200 bytes (no control characters), epic ≤ 64 or cleared, paths must be **absolute under the item's `component_root`** (the S26 rule for artifact paths — T0 records it) and are editable **only while status ∈ {idea, ideating, brief-agreed, planned, ready}** (after `running` the paths are history: refused naming the status). Status, priority, estimate and provenance are changed **only** by their own verbs (the machine and honesty rules stay where they are). `done`/`cancelled` items accept title/epic edits (a typo fix) but not paths. Unchanged ⇒ `changed:false`, no audit.

## RM2 — The audit: `roadmap_change` (P3)

`roadmap_change(id, item_id, op ∈ ('add','update','cancel','estimate','priority','ideate','plan','materialize','transition'), summary, actor_kind ∈ ('user','cli','system'), actor_id, occurred_at)`, append-only by trigger, no FK. The reactor-driven transitions (`brief-agreed`, `done`, …) are written with actor `system:reactor` so a person can see *what moved on its own*. Fixed-template summaries (`"moved item %d idea → ideating"`, `"edited item %d (fields: title, epic)"`), no user text.

## RM3 — The hidden blueprint choices become records and settings

- **Records (rule 6):** `planHostBlueprintYAML` (an embedded YAML string in `internal/daemon/roadmap_dispatch.go`) is moved **byte for byte** into `internal/blueprint/blueprints/roadmap-plan.yaml` so S48's `blueprints.standard` set seeds it as a system blueprint; the dispatch reads it by name through S48's source. (T0 confirms the string is a valid standalone blueprint; if it is a template completed at runtime, the template stays code and only its static part moves — recorded.)
- **Settings (S44):** `roadmap.ideationBlueprint` (string, default `brainstorm`, grammar = the blueprint name grammar), `roadmap.planBlueprint` (default `roadmap-plan`), declared in `internal/config/settings_decl_roadmap.go`; read through `config.Reader` **at each dispatch** (never at construction). S44's registry has no dynamic validator, so **existence + validity are checked at use** by the dispatch through S48 (`Analyze` clean): a missing/invalid choice **refuses the dispatch with a named sentence** (never a silent fallback to the default), and `roadmap.health` reports it before anyone presses the button. The settings are not `Sensitive` (they pick which of the operator's own blueprints to run; policy still gates every call).
- Defaults reproduce today's behaviour exactly (acceptance 1).

## RM4 — Dispatch stays a person's act

`ideate`, `plan` and `materialize` keep their human-door semantics (they start loops). They gain the **actor** (peer-stamped), the audit, and in the app a confirmation with the daemon's authority statement. No tool dispatches (R6); the agent-callable start remains S39's ask-gated `run_start`.

## RM5 — One formatter for honesty states

`roadmap.Describe(item)` renders status, three-state priority (`known (P1)` / `assumed (P2)` / `unknown`), estimate range **with its basis** (never a bare number — D8), readiness and blocker, loop link and status — fixed templates; used by list/show/board/CLI/UI/tools. The three states are never collapsed to a badge.

## RM6 — Reads: board with details, history, health

`roadmap.board {componentRoot?, status?, epic?}` (the list with `Describe`, readiness `IsReady` and the blocker sentence, the loop's status, grouped counts) — `roadmap.list/show` stay as built (compat). `roadmap.history {id, limit?}` (the audit). `roadmap.health {}` (RM3's two settings: `{setting, name, state ok|missing|invalid, report counts}`). `roadmap.vocabulary {}` (statuses with their legal next states from `AllowedTransitions`, priority/estimate states, provenance, path rules) so forms render from the daemon.

## RM7 — Methods and doors

Writes (**human door**, P4): `roadmap.update` (new), and the existing `add/cancel/estimate/priority/ideate/plan/materialize` gain actor + audit (params and results otherwise unchanged; `S26` fixtures byte-identical). Reads as RM6. All `RequiresInitialize`, none `Debug`, off the narrow surface, classified in S47's table.

## RM8 — Agents read and propose an idea

Specs (family `roadmap`): `roadmap_list`, `roadmap_show` (safe), `roadmap_propose_idea` (**Mutates**): an **inert** proposal of new kind `roadmap_idea` `{componentRoot, title, epic?, note}`; `desk.Validate` = the same validators `roadmap.add` uses (component exists, title rules); `Apply` = `roadmap.add` with provenance **`authored`** and the **committing person** as actor. No tool updates, cancels, estimates, prioritizes or dispatches. P6 rules (schemas identity-free, registered-not-granted, counted bump +3).

## RM9 — CLI (P5)

`aeon roadmap` keeps its nine verbs unchanged (goldens) and gains `update`, `history`, `board`, `health`, `vocabulary`; `ideate`/`plan`/`materialize` need `--yes` (they start agents; the daemon's statement is printed first). Offline fallback via the same service.

## RM10 — UI (P7)

Extend `ui/src/features/roadmap/RoadmapPage.tsx` (a real route): item drawer with edit (fields per RM1, paths only when allowed — the daemon's vocabulary says when), history tab, the **Dispatch settings** card (two blueprint pickers from S48, health dots, writing `settings.set`), dispatch buttons with the authority confirm, board filters daemon-side, propose-idea desk via S45's proposals tab. The Gantt and its renderer are untouched; the bench page stays.

## RM11 — Invariants that must not move

The closed lifecycle (no edge added; `running → cancelled` stays refused), D4 readiness, D8 estimate-with-basis, queue-first materialization (R4), the reactor and `publishLoop` joins, the 11 measured edges of the door, S26's fixtures.

## RM12 — What does not move

The Gantt/renderer, S24's queue, S38's epics/tasks, S44's registry (only a declaration file is added), S48's engine, and every sibling file beyond: the plan-host YAML move (T2), the dispatch reading settings (T3), actor+audit on existing writes (T2).

## Open items for T0

Re-verify facts; the full writer inventory (door, reactor, dispatch, store); `planHostBlueprintYAML` standalone-ness; the artifact-path rule and where it is enforced; `IsReady` and blocker text; materialize's blueprint choice and the queue-first path; S44/S48/S43-extra/S45/S47 state; the `epic` field relation to S38; proposal-kind DDL; error block; baseline; CLI parity (expected 9/9 + gaps).
