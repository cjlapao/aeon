# Design — S38 Organization layers

Fills in `brief.md`. Rules are **OL1–OL15** (prefix `OL`, to be reserved in `ARCHITECTURE.md` §11) and are cited
from `plan.md`. ~~**OL1, OL6 and OL7's column lists are provisional until T0 closes**~~ — **T0 closed them
2026-10-27** ([investigation.md](investigation.md)): OL1 stays a full rename (Q2 under threshold), OL6 takes
the `waiting`-park delta, OL7's lists are final (Q3), OL10's names are the 33-tool catalog (Q9), OL14 is the
surrogate `id` (Q6), and OL15's steer column rides **0036** — each edit dated below.

## OL1 — The rename, done now (final 2026-10-27, T0-Q2/Q7: column rename stays; NOT NULL lands inside 0035)

Migration **`0035_org_layers`** *(next free number as of 2026-10-27; claim at seeding)*, one transaction, in this order:

```sql
ALTER TABLE project      RENAME TO component;        -- SQLite rewrites FKs/indexes that reference it
ALTER TABLE project_root RENAME TO component_root;
ALTER TABLE component_root RENAME COLUMN project_id TO component_id;
CREATE TABLE project (                                -- the NEW tier, the spec's Project
  id TEXT PRIMARY KEY, name TEXT NOT NULL, short_description TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '', icon_emoji TEXT, icon_image TEXT,
  implicit INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  CHECK (icon_emoji IS NULL OR icon_image IS NULL)    -- spec: an Icon is one or the other
);
ALTER TABLE component ADD COLUMN project_id TEXT REFERENCES project (id);
-- one implicit project per existing component, named after it; then project_id is set
```

- **No lazy backfill, no NULL-means-implicit.** Because nothing shipped, the migration creates the implicit
  project for every existing component and `component.create` does the same; `project_id` is therefore never NULL
  after the migration. ~~(a follow-up `NOT NULL` is a rebuild — T0-Q7 decides…)~~ **T0-Q7 ran the DDL and the
  0016-pattern rebuild on a populated store copy (integrity ok, FK empty): `project_id NOT NULL` lands inside
  0035. Decided 2026-10-27 by execution, not preference.**
- `component` also gains the spec's presentation fields it lacks (`short_description`, `description`) and keeps
  `icon`; a component carries its repo (`root`, `default_branch`, `branch_prefix`), as the spec's `RepoRef`.
- **The scope key.** `store.Scope{LoopSlug, ProjectRoot}` and the `project_root` column on `loop`, `task`, `step`,
  `loop_document`, … are DB4's identity. They name the component's root. **Renamed to `component_root` /
  `ComponentRoot` in the same migration — settled 2026-10-27 (T0-Q2)**: measured under the written split
  thresholds (tier+wire+UI ≈ 423 refs; column half ≈ 732; total ≈ 1,155 against gates of 1,600 total / 1,000
  column — [investigation Q2](investigation.md)). Mechanical because `Scope` is one definition and `scope.go`
  already greps for bare predicates (I4).
- Wire: `project.create|update|list` → `component.*`; **new** `project.*` methods for the group tier;
  `PROTOCOL_VERSION` bumps; `make protogen` regenerates `ui/src/proto/contract.ts`. The Projects page becomes a
  Projects page (the group, hidden when implicit and single-child — the spec's own UI rule) over a Components
  list; the UI hides a layer that has one child, **the data never collapses**.

## OL2 — Capabilities and contracts (the crux)

Two independent things, kept independent on purpose:

| | Where it lives | Says | Can it grant? |
| --- | --- | --- | --- |
| **Capability** | a handler's `tools:` list + S11 `ToolAccess` rows | *this agent can call `task_create`* | **yes** — the only grant |
| **Contract** | a blueprint lane's `emits:` | *this step is not done until `task` exists* | **no** — it is checked against the capability, never creates one |

Consequences, each a test:
- A chat agent with no blueprint and `task_create` in its handler list creates tasks. Nothing in the blueprint
  layer is consulted. (criterion 1)
- A blueprint without a project-manager lane can still have tasks, because any agent in it may hold the tool.
- A lane may declare `emits:` only if its agent's handler can call the tool; the validator checks it at load
  (`invalid` when the handler exists and lacks it, `unverifiable` when the handler is absent — P31 again).
- The default grant is unchanged (`read, list, write`); which agents hold which catalog tools is **handler data**,
  seeded for `brief-author`, `project-planner`, the new brainstorm agent, and — as a decision, not a default —
  not for the general chat agent until the operator grants it.

## OL3 — The management catalog: one definition, two doors

The old loop-state MCP gave agents `loop_*` and `task_*` tools (measured in `opencode/agents/*.md`: `loop_get` ×210,
`loop_setPhaseStatus`, `task_complete`, `loop_setField`, `loop_readFile`, `loop_writeFile`, …); the new layers add
project, component, goal, epic, brief, attachment. This slice makes them **one catalog**: each action is defined once
(name, params, validator, store call, the actor it stamps) and is projected as **(a) a socket method** and **(b) a
first-party agent tool** — P25's *one action table*. A test fails if a projection lacks its counterpart.

| Family | Actions (indicative; T0-Q9 fixes the list from the legacy MCP and the entity tables) |
| --- | --- |
| project | `project_list/show/create/update` |
| component | `component_list/show/create/update` (create = register a repo, as the old `project.create`) |
| goal | `goal_list/show/create/update` (update = material edit → `Version+1` → `in_review`), `goal_submit_review` |
| epic | `epic_list/show/create/update` |
| brief | `brief_show/create` (create again = `Version+1`) |
| task | `task_list/show/create/update/set_status`, `task_bump_counter` (legacy `task_*`; status writes go through `machine.go`) |
| attachment | `attach`, `attachments_list` |
| loop (reads + document I/O) | `loop_list/get/files/read_file/audit/timings` (legacy reads), and loop documents via the existing `read`/`write` |

- **Grantable per agent, in any blueprint or none.** Which agent holds which is **handler data** (`tools:`) plus S11
  `ToolAccess` rows on the **exact tool name** — so `task_create` can go to a chat agent and `component_create` to a
  different one. Nothing is in `DefaultGrant()`; the registry pin (`TestRegistryExactNames`) grows with a dated
  amendment, the catalog's size fixed by T0-Q9.
- **The one carve-out (decided up front, overrulable here):** *review decisions* (`goal.decide`, `brief.decide`, manual-task
  approval), *trigger authoring* (S40: standing authority to run agents unattended) and *policy/MCP-config authoring* (PL15, S35) are **not in the catalog**. They stay human doors: the
  spec says rejection is always human and an agent deciding its own proposal is the failure the spec's invariant
  exists to prevent. The tool side carries a grep-pinned test; the socket side has `*.decide`.
- **Identity is stamped by the handler, never by tool input** — loop, component, step, agent, `created_by` from the
  step context (the `PolicyGateRequest{Tool: use.Name}` posture, `loop.go:619`). An agent cannot claim another
  loop's component or authorship; a `components[]` entry outside the loop's project is refused with a sentence.
- **Goals are always proposals**: `goal_create` leaves `in_review`; `Source.Kind` derives from the step context
  (inside a brainstorm: `brainstorm`; in a chat: `manual`), `Untrusted` from kind, never from arguments.
- **`task_create` can't build a cycle**: `depends_on` may name only tasks that already exist; duplicate id is refused
  with `plan`'s own `Refusal.Sentence()`.
- **Status writes keep the machine.** `task_set_status` goes through `ValidateTransition`; the legacy rule *"the
  planner never mutates loop state"* becomes a **grant** question (the planner's handler simply isn't given it), not a
  code fork. The loop's own phase/lane position stays the engine's (P29): no `loop_setPhase` tool exists, because the
  daemon owns the walk.
- Re-run class (P27): writes are `detectable` — keyed so a crash between row and receipt is recognised on resume.

## OL4 — Files stay files; the entry links them

An `attachment` is `(subject_kind, subject_id, loop_slug, component_root, document, rev)`. It pins a revision of
a `loop_document`; it never copies bytes and nothing is rendered. Reads return the pinned rev's content and a
three-state currency (P31): **current** (head rev == pinned), **superseded** (a newer rev exists — the agent or a
human edited after attaching), **missing** (the document was archived). `goal_create`/`brief_create`'s optional
`document:` is shorthand for create-then-attach. Because documents are append-only by rev, "what exactly was
approved" is answerable: a `review` row records the subject version, and the attachments of that version carry
their revs.

## OL5 — Blueprint keys: `output:`, `emits:` and `requires:` (OL9)

```yaml
id: brainstorm
output: [goal, proposal.md, brief.md]            # declared deliverables — for discovery and the UI
lanes:
  - id: discuss
    step:
      kind: model-step                           # runs when the user says "create the goal" (OL6)
      agent: brainstormer
      emits:
        - { type: goal, attach: [proposal.md, brief.md] }
```

- `output:` is **documentation the app can show** (what does this blueprint yield?) and a load-time cross-check
  (every named type is emitted by some lane, every named file is `produces`d or `attach`ed by one) — the same
  *declared-but-unreachable* diagnostic the validator already gives questions. Default empty = today.
- `emits:` is the **contract**: a list of `{type, min?, attach?}`. The lane is satisfied when this step created
  `≥ min` (default 1) records of `type` and every `attach` path is attached to one of them. Checked beside
  `missingArtifact` (`advance.go:283`, `continue.go:70`) as `missingRecord`; the refusal names the record and the
  call to make (*"lane `discuss` needs a `goal` — call `goal_create`, then `attach` proposal.md and brief.md"*).
- **The prompt is told** — the same place `advance.go:460` names `produces` gains a *"this step must also
  create"* block, so the agent learns the contract while it can still act (R3).
- A gate step may carry **`decides: goal|brief`**: its answer (`approve`/`abort`) calls `DecideGoal`/`DecideBrief` in the answer path (`revise` changes no state; the lane re-emits `Version+1`). A fourth optional key, additive (T11, T14).
- `emits:` allowed only on `model-step`; type must be one of the catalog's entity names; validated against the
  agent's handler (OL2). `schemaVersion` stays 1 — two optional keys are additive (P15).
- Task contract example, same shape: `dev-loop`'s `plan` lane gains `emits: [{type: task, min: 1}]` beside
  `produces: [plan.md, tasks/]`, and the planner handler gains `task_create`. The `tasks/{taskid}.md` detail files
  stay (the legacy planner's whole point is their depth); the rows are the structured index over them
  (`attach` each task to its detail file).

## OL6 — Conversation first: the run happens inside the chat (final 2026-10-27, T0-Q4: `done` IS terminal today — the slice takes the minimal `waiting`-park delta)

The loop is a conversation (P16, S31). A blueprint is **invoked within it**, not the container of it:

1. We talk — brainstorming, refining the idea — with an agent that may hold *no* management tools yet.
2. We say *"ok, create the goal"*. The blueprint's lanes then run (the agent writes `proposal.md`/`brief.md`, calls
   `goal_create`, calls `attach`); the lane's `emits:` contract (OL5) says what must exist before it completes.
3. The run ends; **the conversation does not**. We keep talking and can `goal_update` in the same thread; the
   goal's data, its attachments and the chat stay together because they hang off one loop.

Kickoff has two spellings and **one door**: a person types the message, or an automation inserts a pre-written one
(S39/S40). A loop *starts* from a chat message either way. T0-Q4 answered (2026-10-27, quoted in [investigation.md](investigation.md)): `decide.go:40` and
`continue.go:40` both refuse on `done|aborted` — **a finished loop cannot accept a message**. The minimal
change, taken: a loop whose **blueprint completed** (never on abort) **parks at `waiting`** — S6's existing
loop status, reused rather than inventing a seventh status every consumer would have to learn — and a
message resumes it through the existing start/steer path. Non-conversation blueprints still `done` exactly
as today. **Chat-after-run is the slice's most important engine delta, and this sentence is its ruling.** A `kind: conversation` step is *not* introduced unless T0 shows a model-step cannot carry the
conversation. Where the contract is missing at the end of a run, the runtime **steers** (S31's durable `step_steer`,
D3/D4) with the `missingRecord` sentence instead of failing the step.

Brainstorm still runs in a **component** loop in this slice; the spec's project-scope run (read all components, no
worktree) is a later slice, and the goal names its components explicitly so nothing here changes when it arrives.

## OL7 — Entities (final 2026-10-27, T0-Q3: the field map was walked through five real briefs, a real plan and real task files)

All in `0036_records` *(next free after the rename; claim at seeding)*; **no status column** where the spec says derived.

| Table | Keys / columns that matter | Spec rule it holds |
| --- | --- | --- |
| `epic` | `id, project_id, title, objective, owner, target_date, budget_usd` | status derived from goals |
| `goal` | `id, project_id, epic_id NULL, version, title, body_document NULL, type, source_kind, source_ref, source_loop NULL, review_state CHECK(draft,in_review,approved,rejected,parked), created_by_kind, created_by_id` | two axes: stored review, derived execution |
| `goal_component` | `goal_id, goal_version, component_id, rationale, estimate_usd NULL, confidence NULL, basis` | NULL = unknown (P31); rollup = lowest confidence, summed known cost with a floor flag |
| `brief` | `id, loop_slug, component_root, goal_id NULL, goal_version NULL, version, scope, out_of_scope, acceptance (JSON), constraints (JSON), test_plan, review_state`, `UNIQUE(component_root, loop_slug, version)` | versioned, loop-owned (R5) |
| `review` | `id, subject_kind, subject_id, subject_version, decision, by_kind CHECK(by_kind='user'), by_id, reason, decided_at` | append-only; widening to `policy`/`agent` is a rebuild in 0016's pattern, chosen on purpose |
| `attachment` | OL4 | files linked, never copied |
| `task` (+cols) | OL14 — `id, manual, assignee_kind/assignee, component_id, goal_id, parent_task_id, type, approval_state`, plus `task_loop` | loop tasks unchanged and read-only; manual tasks independent; one board |
| `loop` (+cols) | `goal_id NULL, goal_version NULL, scope DEFAULT 'component'` via `ALTER TABLE` | link, not containment |

- `Body` in the spec's Goal and Brief becomes the **attached document** (OL4), which resolves the spec's open row
  *"Output templates for Goal and Brief"*: there is no template; the structured fields are columns and the prose is
  the author's file.
- **A material edit** (scope, components, estimates, attached body rev) is `Version+1` and `review_state →
  in_review`, via one store function. `Source.Untrusted` is a Go function of `source_kind`.
- `goal.review_state = 'approved'` is written only by `store.DecideGoal`, which inserts the `review` row in the
  same transaction behind a `WHERE review_state = 'in_review'` claimed-FROM guard (S35 TD1's pattern, reused): a
  second decision changes zero rows and returns who decided and when.

## OL8 — The brief gate and review

The `brief-gate` lane stays an ordinary S8 gate. Its resolution also calls `store.DecideBrief` in the gate's own
transaction: `approve → approved`, `revise → in_review` (the existing `on_reject goto` returns to the brief lane,
which calls `brief_create` again → `Version+1`), `abort → rejected`. The decision is recorded with the
peer-credential user. **Quick loops and plain chats get no brief row** — the spec's synthesized brief with
policy-approve needs the approval-policy actor, not built here; *absent* is *absent*, never *approved* (P31).

## OL9 — Prerequisites: `requires:` (and starting from a goal)

A blueprint may declare what a loop of it needs before it can proceed:

```yaml
id: dev-loop
requires: [{ any_of: [{type: goal, state: approved}, {type: task, state: approved}] }]
```

- Checked at **start** (`loop.start` takes `GoalID` and/or `TaskID`; the daemon pins `goal_version`) and refused with a
  sentence naming the requirement; a chat that starts a `requires:` blueprint without one gets the **agent** saying
  so and asking *"which goal or task?"* — same check, spoken through the conversation (S39 owns the dialogue).
- `state` is a review state for a goal (`approved`) and the task's **approval** state (OL14). Unknown types or states
  are a load diagnostic. Default none = today (quick loops, plain chats).
- The prompt is seeded from the goal/task body, as the `goal` string is today. One loop per (goal, component) stays a
  convention, not a constraint (spawn is later).

## OL10 — The daemon surface (P34: API first; names final 2026-10-27 — the 33-action catalog, investigation Q9)

The catalog's methods (OL3) plus the human-only `goal.decide`, `brief.decide` and task acceptance, after the rename. The record **tools** and the **socket writes** are two
doors into the same store functions — the tool stamps the agent's identity, the socket the peer's. `*.decide`
records the peer-credential user and refuses `agent` as an actor kind. A grep-pinned test asserts no registered tool
and no handler reaches a `*.decide` (S35's `TestNoPolicyAuthoringReachesTheNarrowSurface` shape). CLI
(`aeon goal …`) comes last, as P34 orders.

## OL11 — Derived, not stored

`execution_state(goal)`: no loops → `not_started`; any running → `running`; all remaining waiting on a *dependency* →
`blocked` (written and tested on synthetic input; unreachable until the wait slice — said so in the code); all done →
`done`; any failed and none running → `failed`. Cost/time are `GROUP BY` over `step`; a `partial|unknown` step makes
the figure a **floor** (P32). A test asserts no stored column or trigger named `execution_state` or `*_total` exists
on the new tables.

## OL12 — Naming collisions, ruled once

| Spec word | Collides with | Ruling |
| --- | --- | --- |
| Run | the loop | **Loop** (brief §0.1) |
| Project (spec) | the shipped `project` | shipped one is renamed **Component** (OL1); `project` now means the group |
| Policy (approval gates) | S10 `policy` (tool access) | neither built here; the later slice's name is **ApprovalPolicy**, table `approval_policy` |
| `Brief.Scope` / `RunScope` | each other, in the spec | `brief.scope` (prose) vs `loop.scope` (component\|project); Go `LoopScope` |
| Task (spec) | `agent_task` (S14) | the spec's Task is the existing `task`; `agent_task` unrelated |

## OL13 — Where the rest lands (named, not designed; sketches exist)

**S39 · chat runs** — [`../S39-chat-runs/brief.md`](../S39-chat-runs/brief.md): chat-after-run, the *"create the goal"*
trigger phrase becoming a first-class *run this blueprint* action, the agent that asks for a missing prerequisite.
**S40 · automation** — [`../S40-automation/brief.md`](../S40-automation/brief.md): internal-event triggers
(goal approved → start `dev-loop` with a kickoff message), task type → blueprint, webhooks/cron, ApprovalPolicy.
Later: **spawn/wait** and run dependencies, **config inheritance** (`ParamDef`), **project-scope loops**.

## OL14 — Two kinds of task, one table (final 2026-10-27, T0-Q6: surrogate `id`; the `work_item` fallback is retired)

**Loop tasks** (`manual = 0`) are the glue that holds a run's state. The feature loop is **one run**; its planner creates
N tasks; the blueprint's per-task **ladder** (`dev-loop`'s `build` lane: issue → implement → verify → security → PR → review →
merge → close) is what moves each task up the status ladder via `sets:`, with evidence required at the rungs the machine
names. Different rungs run different agents (`by-tech-tag` engineer, `verifier-engineer`, `security-engineer`,
`code-reviewer`) — so a task has an **assignee that changes as it climbs**. Nothing about this is new: it is the existing
`task` row, `machine.go` and `iterate`. S38 only makes it **visible and typed**.

- **Controlled by the agent and the engine; read-only to people.** No approval (`approval_state = 'none'`, a CHECK when
  `manual = 0`), no user edits: the socket's `task.update`/`task.set_status` refuse a loop task with a sentence naming the owning
  loop. The app shows them — that is the point — it never manages them.
- **Manual tasks** (`manual = 1`) are made by a person (or an agent *acting for one*, through the granted `task_create` with
  `manual: true` — a tool-stamped fact, not an argument): `type` (`bug | feature | chore | refactor | investigation`), a body (an attached
  document, OL4), `approval_state` (`pending | approved | rejected`, set by a human — the carve-out), an `assignee`
  (a person or an agent), and an optional automation trigger (S40). **No loop at creation.** A blueprint run started on it
  (`loop.start {TaskID}`, `requires:` in OL9) **links** a loop to it; that loop may then create loop tasks as **children**
  (`parent_task_id`) — the manual task is the *what*, the feature loop's tasks are the *how*.

Shape (additive over the existing columns):

| Column | Meaning |
| --- | --- |
| `id` | surrogate primary key, global; the legacy `(component_root, loop_slug, taskid)` becomes a **partial UNIQUE where `loop_slug IS NOT NULL`** |
| `manual` | 0 = loop-managed, 1 = hand-made. **Derived from origin at creation, never settable later** |
| `assignee_kind`, `assignee` | `agent` + the handler name, or `user` + the peer-credential id; replaces the narrower `engineer` text (kept as the first assignee) |
| `component_id`, `goal_id NULL`, `parent_task_id NULL` | where it lives, what it serves, what it decomposes |
| `type`, `approval_state`, `created_by_kind/id` | typed work item; acceptance (manual only); provenance |
| `task_loop(task_id, loop_slug, component_root, role)` | which loops work it (`role`: `owner` for a loop task's own loop, `worker` for a manual task a loop was started on) |

- **Board reads.** `task.list` filters by project, component, `manual`, assignee, status, type, goal, loop, parent;
  every row carries status, assignee, type, loop link, parent, and **cost-so-far as a floor-flagged group-by over `step`**
  (`step.taskid` already exists; P32 holds). `task.changed` events (additive) keep a kanban live without polling. **Columns are the
  UI's choice**: the fourteen ladder statuses collapse to a handful of board columns in the app, never in the data. A task's
  status stays the machine's; the board never writes it.
- **Approval is a human door** (the carve-out): a manual task is approved by a person, and **what approval triggers is S40** —
  the author's ruling: *an automation was set by a human, so it runs whatever it declares*; the trigger names its event
  (`created | approved | completed`), there is no global rule to decide.
- **Agent scope.** A granted `task_create` / `task_update` / `task_set_status` acts only on tasks the agent's loop owns, or on a
  manual task its loop was started on; any other id is refused (identity stamped, not argued — OL3).
- ~~*What would change it:* … the fallback is a separate `work_item` table …~~ **T0-Q6 (2026-10-27): every reader of the
  loop-scoped triple goes through five `store/tasks.go` functions taking `Scope`; the engine never raw-keys it
  (advance/counters/escalate/execute/handler all via the store; `step.taskid` names within scope). The surrogate
  id absorbs it; `work_item` is retired — one table, one board, as §0.9 asked.**

## OL15 — Chat and steering: what S38 must not break (measured against S31, built 2026-10-24)

S31 built the steering contract and S38 sits on top of it unchanged. Measured in `internal/protocol/steer.go`, `internal/store/steers.go`
and the loop-chat design (`docs/superpowers/specs/2026-10-24-loop-chat-design.md`):

| Invariant (S31) | Source | What S38 does about it |
| --- | --- | --- |
| Any running agent step can be steered with a message; it is **queued durably** and injected at the **next turn boundary**, never mid-stream | `step.steer`, D3/D4, LW1 | untouched. Catalog tools are ordinary tools: a `goal_create` call the model had queued when a steer arrived is **not run** (LW2/LW3's synthetic *"not run: the person steered"* result), so **no row is written**; a call that did run is `detectable` (OL3) and survives a crash |
| A steer is keyed by `stepId`, **never by loop** (P33-ready: several steps may run at once) | D12 | S38 adds no loop-keyed message path. A task row links to its step through `step.taskid`, so a board can offer *"jump to the agent working this task"* — navigation, not a second steering door |
| Loop view: **To:** lists the *running* agents; with several running the person chooses one; agent view implies the target | S31 design, *Message box* | untouched; there is **no broadcast** and S38 does not add one |
| Provider/model and effort changes ride a steer and apply from the next turn | D5 | untouched |
| Stop, then `step.continue {message}` | D6 | untouched |
| `step.steer` on a step that is **not running** is refused naming the remedy (*use answer / step.continue*) | LW4 | untouched, and it is why the next row exists |
| **The runtime's own nudges use the same queue.** The `missingRecord` push-back (OL5/OL6) is a steer from the engine, not a new injection path | S31 D4 | one queue, one delivery transaction; T0-Q4 checked (2026-10-27): `step_steer` carries **no author** — and the suggested "in 0027/0028" is invalid (those shipped with S18; migrations are immutable, AGENTS §3). The minimal `source TEXT NOT NULL DEFAULT 'person'` column rides **0036**; `step.steer` still writes only `'person'`, so LW fixtures stay byte-identical and the engine's nudge prints *"the blueprint asked"* |

**One decided S31 behavior this design reverses, on the author's ruling — flagged, not slipped in:** S31's screen spec says *"Finished loop:
the message box is hidden"* (variant 5; built as T10's UI). The conversation-first model (OL6) needs the opposite: a run ends,
**the conversation continues**, and the goal is edited in the same thread. So the composer after a run is no longer hidden when the loop
has a conversation agent. This is **S39's** engine and UI delta, recorded here so the reversal is a visible decision and not a regression
someone later "fixes" by hiding the box again. Until S39 lands, S38 keeps S31's behavior exactly.

## Test map

1. no-blueprint chat: granted agent creates a task; ungranted cannot; hello-world run byte-identical (criterion 1)
2. brainstorm replay: files + `goal_create` + two `attach`es; goal `in_review`; attachments at the written revs (2)
3. contract: files without `goal_create` → lane refuses naming the record; steer continues the conversation (3)
4. validator: `emits:` against a handler lacking the tool → invalid; absent handler → unverifiable (3)
5. `task_create`: dep on a missing task refused; duplicate id refused with `plan`'s sentence; no cycle constructible (4)
6. create-never-decide: grep + behaviour — no tool/handler reaches `*.decide` or writes a decided state; `by_kind` CHECK (5)
7. rename: old-store upgrade test; grep list of old-meaning references is empty; protogen clean (6)
8. derived state and floor flag; no stored `execution_state` (7)
9. compatibility: shipped blueprints load; tool specs byte-identical with no grants/`emits` (8)
10. identity: a tool call naming another loop's component or goal is refused; stamped identity ignores input
11. attachment currency: current / superseded / missing rendered distinctly
12. double decide: second `goal.decide` changes zero rows and names who/when
13. chat invariants: `step.steer`/`step.steers`/`step.steer.cancel` conformance fixtures byte-identical; a steered-away `goal_create` writes no row; the engine's `missingRecord` nudge is delivered through the existing steer queue and labelled as the blueprint's
