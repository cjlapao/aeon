# Investigation — S38 T0 (no code; every answer from a run)

Run 2026-10-27 on HEAD `77a4c675` (S20 BUILT; 35/35 packages, 2539 PASS measured at that commit).
Commands are quoted as run (trimmed output `…`); each closes **settled**, **changed the design** (the OL rule
edited, dated), or **still open and why**. Store probes used throwaway `XDG_STATE_HOME=/tmp/aeon-s38` and
`LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s38/aeon.sock` (a socket directly under `/tmp` fails the daemon's
endpoint-directory chmod — first probe died on `set mode on endpoint directory /tmp: operation not
permitted`, which is the guard working).

## Q1 — the blueprint-less chat today (rows, agent, tools) — **settled**

```
$ aeon loop create --slug q1-simple --mode simple   --project /tmp/aeon-s38/ws --goal "no blueprint at all"
q1-simple   /tmp/aeon-s38/ws   created · simple · preflight
$ aeon loop create --slug q1-standard --mode standard --project /tmp/aeon-s38/ws --goal "standard"
q1-standard /tmp/aeon-s38/ws   created · standard · preflight

-- from the store:
loop:           ('q1-simple', 'active', '')   ('q1-standard', 'active', '')
project: 0 rows | loop_blueprint: 0 rows | chat_message: 0 rows
```

There is **no blueprint-less row-kind** — `loop create` takes `-mode simple|standard|poc` (default
`standard`, daemon/blueprint.go:291-295), no `-blueprint` flag; the loop row is the chat's only container, and
`loop_blueprint` (columns `loop_id, blueprint_id, blueprint_version, content, runtime_json, updated_at`) is
pinned only at start (`store/blueprint.go:157`). **A chat is a loop**; "no blueprint" is a mode, not a row.
Confirmed the brief's amendment: S39's dedicated `chat` agent/blueprint closes the plain-chat half; S38 proves
the grant gate inside a blueprint loop. **By-product finding:** `aeon project …` **does not exist** —
`unknown command "project"` — the `project.create|update|list` methods have no CLI verb at all. T4 (which
touches the CLI anyway) adds `component`/`project` verbs; this is S20's producer-gap lesson (R-22) applied
before it bites.

## Q2 — the rename's blast radius and the split threshold — **settled: do NOT split**

The written threshold, decided **before** measuring: split Phase A into its own slice (S38a) if the rename
reaches **more than 1,600 total references (Go + UI, all files)** or the **column-rename half alone passes
1,000**.

```
$ grep -rwni project internal/{store,protocol,daemon} cmd/aeon internal/{terminal,loop} --include='*.go'
store 130 lines/20 files · protocol 58/17 · daemon 33/11 · cmd 61/10 · terminal 11/3 · loop 7/3   → 300 lines / 64 files
$ grep -rwni project ui/src --include='*.ts*'
123 lines / 28 files
$ grep -rn "project_root\|ProjectRoot" internal cmd --include='*.go'
623 lines / 109 files        (+ ui camel/snake: 109)
```

Tier+wire+UI ≈ **423**, column ≈ **732**, combined ≈ **1,155** — below both thresholds. The plan's
`≈850 references (store 267, protocol 200, …)` table is **stale** (measured 2026-10-27: `grep -rni project`
counts 1,467 lines across the same packages, tests and comments included — the shapes differ; the commands
above are the canonical ones). Verdict: Phase A stays in this slice as **one merge-commit (T1–T4)** — the
tree does not build between T1 and T3 (plan's own line), and per-commit green is the house rule.

## Q3 — the field map, from real documents — **settled** (OL7's lists finalized in design)

From five real briefs (S16/S18/S19/S20/S38), the real S20 `plan.md`, and real `tasks/T5.md`:
headings `## What this is`, `## Why it is N tasks`, `## Definition of Done`, `## The seams`,
`## Locked decisions`, `## Acceptance criteria`, `## What this is not`, `## Risks` map to
**`brief`**: `scope` ← the goal/problem prose (**attached document, not a column**), `out_of_scope` ←
"What this is not" (prose stays attached; column holds the one-line summary), `acceptance` (JSON array) ←
"Definition of Done"/"Acceptance criteria" **only when they are enumerable check-list items** — prose ones
stay in the document; `constraints` (JSON) ← "Locked decisions"; `test_plan` ← the Validation section.
**Nothing renders**: the five headings above are prose a column could not hold — they live in the attached
`loop_document`, which is exactly OL4's division (structured facts as columns; long-form stays the author's
prose). From `tasks/T5.md` (`Why this exists / Scope / Validation` + `**Prefix:** OL… **Rules:** …` header):
**`task`**: title ← the task's one-line outcome, `type` (`feature` here; `bug|chore|…` for manual), body ←
attached `tasks/{taskid}.md`, status ← the machine, `assignee` ← the routed agent, `parent_task_id` ← none
at loop level, `manual=0`. The `Rules:`/`Contract:` header lines are *file prose*, not columns — a column
per heading would make the document's conventions database-schema, which §0.5 forbids.

## Q4 — can a loop converse before/during/after a run? — **changed the design (OL6, OL15)**

```
internal/loop/decide.go:40     if st.Status == "done" || st.Status == "aborted" {
internal/loop/continue.go:40   if st.Status == "done" || st.Status == "aborted" {
```

**`done`/`aborted` are terminal on both answer paths** — today a finished loop cannot accept a message
(S31's UI already hides the composer on finished). But criterion 2 walks *"the conversation is still open"*
after the brainstorm run. The minimal engine delta this slice takes (OL6's "the minimal change is
recorded"): a loop whose **blueprint completed** (not aborted) parks at **`waiting` (on conversation)** —
reusing S6's loop-status `waiting` (the vocabulary has no `done-but-chatting`; adding a seventh status
would touch every consumer) — and `loop.start`/steer resume it on the next message. `done` remains the
**aborted-and-closed** terminal plus every non-conversation blueprint's completion, unchanged. **S39 owns
the composer UI** (OL15's flagged reversal); S38 keeps the composer exactly as S31 built it.

Steer authorship: `step_steer` columns are `[id, step_id, text, route, reasoning_effort, state, created_at,
delivered_at, delivered_iteration]` — **no author**. The engine's `missingRecord` nudge (OL5/OL6) would be
attributed to the person. OL15 suggested adding the column "in 0027/0028" — **invalid**: those migrations
shipped with S18 and are immutable (AGENTS §3). **Changed the design (dated 2026-10-27):** `source TEXT NOT
NULL DEFAULT 'person'` rides **0036**; the transcript prints *"the blueprint asked"* for
`source='engine'`; `step.steer` still writes only `'person'` (LW fixtures byte-identical — the default keeps
every existing row and test untouched).

## Q5 — handler `tools:` → the built toolset — **settled**

Path: `blueprint step.Agent` → handler row (`internal/runtime/handler.go`) → **`internal/runtime/compile.go:223`
`for _, want := range h.Tools`** (the allow-list *declaration*) → the granted names hand to
**`tools.BuildToolset(granted, ToolContext)`** (`internal/tools/toolset.go:216`, panics without a workspace
root — its own comment) → dispatch passes through `policy_gate.go:308`'s `PolicyGateRequest{Agent, Tool:
use.Name, …}` (S11 rows decide per exact name). Registered ≠ granted confirmed at both doors.

`TestRegistryExactNames` amendment text (dated, to land with T9): *"2026-10-27 (S38 T9): the management
catalog (OL3) joins the ten original tools — the catalog's names enter the registry pinned, never
`DefaultGrant()` (which stays `read, list, write`, toolset.go:207); precedent: S11's `write_file`→`write`
amendment. The pin exists so a tool cannot appear unnoticed; it grew **from ten to 43** (10 + 33, Q9)."*

## Q6 — every reader of `(project_root, loop_slug, taskid)`; surrogate id vs `work_item` — **settled: surrogate `id`**

Readers, complete: `store/tasks.go` — `GetTask` (:143), `ListTasks` (:158), `SetTaskStatus` (:199),
`BumpTaskCounter` (:257), `CreateTask` (:56) — **all** take `Scope` + `taskid`; the engine
(`loop/advance.go`, `counters.go`, `escalate.go`, `execute.go`, `runtime/handler.go`) reaches tasks **only
through those functions**, and `step.taskid` keys by name within the loop scope. `memory` rows carry
`taskid` loosely (`store/events.go:260`). Nothing raw-keys the triple outside the store. So the surrogate
`id` + `UNIQUE(...) WHERE loop_slug IS NOT NULL` (OL14) absorbs the loop path untouched — **`work_item` is
not needed**; loop-task functions keep their `Scope`-keyed signatures (the ladder/machine/counters/escalate
do not move at all), and manual tasks are reached by `id`.

## Q7 — DDL dry-run on a populated store — **settled: do the NOT NULL rebuild now, in 0035**

`aeon serve` store (loop rows) + rows inserted exactly as `project.create` writes them, then OL1's DDL
executed **as written**:

```
implicit project: [('p1', 'q1repo (implicit)', 1)]   component link: [('p1', 'p1')]
component_root intact: [('/tmp/aeon-s38/ws', 'p1')]   ← FK survived the column rename
integrity: ok | foreign_key_check: []
```

Then the 0016-pattern rebuild (`CREATE component_new … project_id TEXT … NOT NULL; INSERT SELECT; DROP;
RENAME`) ran clean: `project_id` notnull flag = 1, integrity ok, fk empty. **Verdict:** OL1's recommended
option taken — `project_id NOT NULL` lands **inside 0035**, no follow-up rebuild, no NULL-means-implicit.

> **Addendum, 2026-10-27 (T1, the real migration).** The dry-run above proved the DDL **executes** —
> it did not prove the rows **survive**, and the difference turned out to be everything. In 0035 as
> built, `DROP TABLE component` fired 0005's `ON DELETE CASCADE`
> (`component_root.project_id … REFERENCES project (id) ON DELETE CASCADE`, `store/schema.go:729`) and
> **silently emptied `component_root`** — while `PRAGMA integrity_check` and `foreign_key_check` both
> read clean on the wreckage, because an empty table satisfies every constraint. The dry-run had not
> caught it because its check list ran the pragmas, not the row counts. The shipped 0035 snapshots
> first (`CREATE TABLE component_root_backup AS SELECT * FROM component_root;` before the DROP,
> `INSERT … SELECT` back and DROP after the RENAME), and `TestMigration0035UpgradesAPopulatedStore`
> counts the rows — 2 components list after the upgrade, and `ListComponents` is the reader that would
> have returned zero. **The lesson, general:** an integrity dry-run of a rebuild proves the schema
> compiles; row preservation is a different property and needs a COUNT on both sides.

## Q8 — where a gate answer resolves; can `DecideBrief` join the transaction? — **settled: yes**

`store/blueprint.go:83 AnswerGate(loopID, questionID, answer, status, runtimeJSON)` opens
`s.w.Begin()`, re-reads the question (gate-kind guard, already-answered guard, options-membership guard,
loop-ownership guard) and updates inside one tx. `DecideBrief` joins **inside that same tx** with the
claimed-FROM pattern (`UPDATE brief SET review_state=? WHERE … AND review_state='in_review'`, rows-affected
check, `review` row insert beside it) — S35 TD1's shape. The runtime's edge-follow (`on_reject` counter,
`loop/escalate.go:70`) reads the row **after** commit, so adding the decision cannot race the lane.

## Q9 — legacy MCP → the catalog — **settled: 33 tools**

`packages/loop-state-mcp/src/tools/*.ts` (archive/loop/misc/now/recovery/tasks.ts) names exactly **25** tools:
`loop_get, loop_list, loop_files, loop_readFile, loop_audit, loop_timings, loop_readTask, loop_writeFile,
loop_appendFile, loop_init, loop_finish, loop_archive, loop_delete, loop_prune, loop_materialize, loop_recover,
loop_setPhase, loop_setPhaseStatus, loop_setAwaitingUser, loop_clearBlock, loop_bumpCounter, loop_setField,
loop_render, task_setStatus, task_complete, task_bumpCounter`.

Mapping: **in the catalog** — loop reads 6 (`loop_list/show/files/read_file/audit/timings`), task 6
(`task_list/show/create/update/set_status/bump_counter`; `task_complete` = `set_status(done)` through
`machine.go`, `loop_readTask` → `task_show`), attach 2, project 4, component 4, goal 5, epic 4, brief 2 =
**33**. **Deliberately not tools** (P29: the daemon owns the walk — OL3's `loop_setPhase`-does-not-exist
rule): `loop_init/finish/archive/delete/prune/materialize/recover/setPhase/setPhaseStatus/
setAwaitingUser/clearBlock/setField/bumpCounter` and **`loop_render`** — brief §0.5 killed rendering outright.
`loop_writeFile/appendFile` are the existing `write` document path, not new tools. R2's "about two dozen" →
**33**; the registry pin grows 10 → **43**.

## Q10 — spec gaps for the author — **settled: five, handed up**

The spec's own Status matrix already names most (allowlist management, event-filter grammar, param catalog,
worktree retention during gate-wait, off-main-path transitions). S38's runs add: (1) the spec says a Brief is
"owned by the Run" **and** unique per (Goal, Component) — a retried loop can satisfy at most one reading
until OL8's versioning; the spec's uniqueness row should say `(goal_id, goal_version, component_root)`;
(2) the spec's Review `by.kind` includes `agent`/`policy` but its own invariant says rejection is always
human — the CHECK ships `user`-only (widen only when an owner appears); (3) `execution_state`'s `blocked`
is defined over *dependencies* which no slice has built — S38 writes and tests it on synthetic input and
says so in code (OL11); (4) the spec never says what a *completed* conversation-loop's status means —
Q4's parking rule is this slice's answer, the spec should adopt or replace it; (5) the spec's Goal.source
has `brainstorm`/`manual` but no actor when an **automation** creates a goal (S40 will need it — name it
now to avoid a CHECK rebuild).

---

**Design edits this run made** (dated in design.md): OL1 — column rename stays (Q2), NOT NULL now (Q7);
OL6 — the `waiting`-park delta, minimal, S39 keeps the UI; OL7 — column lists final (Q3); OL10 — method
names from the 33-tool catalog (Q9); OL14 — surrogate id, `work_item` retired (Q6); OL15 — steer `source`
column rides **0036**, not 0027/0028 (Q4). **Plan amendments** (in plan.md's Reality alignment): stale
Q2 numbers corrected; T1–T4 one-commit ruling; `aeon project` CLI gap assigned to T4; QA-suite path
`docs/test_suites/aeon-org-layers.md`; the T0-by-product no-CLI-verb finding.

---

## Close-out cross-check (2026-10-27, T17)

The ten questions above held through the whole build — every Q's measured
answer is still what the tree says (the rename, the vocabulary greps, the
33-tool catalog that grew to 29 projected names + 4 skips, the surrogate id,
the 0036-carried `source` column). What the slice's closing runs **added**
to the record, all quoted in `S38-org-layers/DEMO.md` (F1–F8): there is no
CLI door that registers a component (Q-none asked — the walk tripped over
it); the machine's vocabularies (five types, fourteen statuses, **no
`done`**) corrected the walk's own assumptions; and the deletion drill
proved the catalog layer is **structurally woven**, not bolted on —
startup-panic coupling through the handler bodies' grants, seam types
carried inside the projection file, and a hand-written re-registration that
drifted the moment it was written. The spec-margin list (Q10) gained items
6–8 at close-out. `investigation.md` is closed with the slice; nothing here
contradicts what T0 recorded.
