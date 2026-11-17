# Brief — S38 Organization layers (records, contracts, and the rename)

**Implements:** [`organization_layers_spec.md`](../organization_layers_spec.md) (Oct 27 2026), first slice.
Rewritten 2026-10-27 after the author's rulings in §0 (they replaced the first draft's "`submit` only on
`emits:` lanes" and "render the file from the row" — both were backwards; see §0). `design.md` and `plan.md` sit
beside this brief; **the plan opens with an investigation (T0)**.
**Stage:** 5 · Fleet. **Depends on:** S4 store, S5/S8 step and blueprint walk, S11 tools/policy, S13 handlers,
S17 plan loader, S31 chat-first loop — **all built code**. **Repo:** `aeon/` (P24) · Go 1.26 · `cgo` (B3)

---

## 0. Rulings this slice starts from (the author's, 2026-10-27)

1. **Loop stays loop.** The spec's Run is the loop. No rename; the spec's open row is closed.
2. **Nothing has shipped, so there is no legacy.** The spec's Project/Component split is done **properly and
   now**: today's `project` becomes **`component`**, a new **`project`** (the group) goes above it. The app reads
   the old name and **the app changes with it** — no compatibility layer, no dual names, no lazy backfill.
3. **Tools are capabilities; blueprints are contracts.** Aeon is usable with **no blueprint at all** — *"write me
   a hello world in C++"* in the chat, and that is it. Creating a task, a goal or a brief is something an **agent
   can do because it holds the tool**, in any chat, under any blueprint or none. A blueprint adds only a
   **contract** on top: *"this step must have produced X"*. A blueprint with no project manager can still create
   tasks, because the tool is the agent's, not the blueprint's.
4. **Everything starts from a chat — manual or automated.** A loop is a conversation. *Brainstorm*: we talk, the
   idea gets defined, then we say *"ok, create the goal"*; the agent runs the blueprint (writes the files, creates the
   goal, attaches them) **inside that same conversation**, and afterwards we can keep talking and edit the goal
   there — all the data stays in one place. *Dev-loops*: we start by writing something, but the agent
   **immediately requires something** (an approved goal or task: *"implement goal X"*). *Automation* is the same door
   with a pre-written first message: when a goal is approved, an event starts the dev-loop blueprint and the loop
   opens with an automated kickoff text. **Manual and automated are one path** — a chat message starts a run.
   **Same principle for tasks**: planners write the plan file *and* create the task rows.
5. **The agent does both — the file and the data entry.** The long-form document stays a document (a
   `loop_document`, already a database row — DB10); the structured facts (title, scope, acceptance criteria,
   components, dependencies) become columns; an **attachment** links the two. *Everything is database; nothing
   is rendered from anything.* (This replaces the first draft's render-from-row design and removes its
   two-truths risk.)
6. **A brief is the goal before we start it.** Goal → approved → loop → an early agent stage writes the Brief →
   `brief_review` → implementation.
7. **Every management action is an agent tool, grantable per agent.** What the old loop-state MCP gave agents
   (`loop_*`, `task_*`) and what the new layers need (project, component, goal, epic, task, brief — including
   *create a component*) are **one catalog**, projected as socket methods **and** as tools from a single definition
   (P25: one action table). Which agent may call which is **handler data plus S11 rows**, chosen when the
   blueprint is built. The catalog has exactly **one carve-out, stated up front so it is a decision and not a
   discovery**: *review decisions* (`goal.decide`, `brief.decide`, task acceptance) and *policy / MCP-config
   authoring* (PL15) are **not tools** — the spec says rejection is always human, and P34 keeps guard authoring
   behind a human door. Everything else is grantable. (You can overrule the carve-out; it is OL2's one line.)
8. **A blueprint may declare a prerequisite.** `dev-loop` *requires* an approved goal or task; a loop of that
   blueprint cannot proceed without one, and the agent says so in the chat instead of failing silently.
9. **Two kinds of task, one table, one board.** A **loop task** is what the old project manager created to run the
   feature: *the glue that holds a run's state* — one run (the feature loop), several tasks, some worked by different
   agents, each climbing the **ladder** the blueprint defines (implement → verify → security → PR → review → merge).
   It is controlled by its agent and the engine, needs **no approval**, and is **read-only to the user**: visible,
   never managed. A **manual task** is made by a person (`bug`, `feature`, …), says what needs doing, has an approval
   state, and may start a simpler blueprint — by hand or by automation. Both carry an **assignee** and a **`manual`**
   flag, both are rows the app can read — a kanban board over the database shows them together or filtered.
10. **Automation and manual work together.** Triggers (spec §Triggers, plus *internal events* such as goal-approved)
    are a **later slice** (S40) that this slice is shaped for; this slice builds the doors they will knock on.

## 1. What this is

**One sentence:** one **management catalog** — project, component, goal, epic, brief, task, attachment, plus the
loop reads the old MCP gave — is exposed both as socket methods and as first-party **agent tools**, grantable per
agent (and so usable in a chat with no blueprint); the spec's **Goal, Epic, Brief, Review** become tables with the spec's invariants; a blueprint lane may
declare `emits:` — a **contract** the lane cannot complete without — and a blueprint may declare `output:` so
the app can show what it yields; and `project`→`component` plus a new `project` tier lands the spec's hierarchy.

| In this slice | Not in this slice (named so nobody smuggles them in) |
| --- | --- |
| rename `project`→`component`; new `project` tier; app + wire follow | `spawn` / `wait` stages, run dependencies, DAG (spec §Wait) |
| the management catalog as tools **and** methods, from one definition; grantable per handler, required by no blueprint | triggers — webhook, cron **and internal-event** (S40); the kickoff-message mechanics (S39) |
| `requires:` on blueprints (an approved goal/task prerequisite); tasks as independent typed work items | the task-type → blueprint mapping and acceptance automation (S40) |
| `output:` on blueprints, `emits:` on lanes (a contract, never a grant) | **approval policies / auto-approve** — manual only; name collides with S10 (OL12) |
| tables: `goal`, `goal_component`, `epic`, `brief`, `review`, `attachment` | config inheritance, `ParamDef` (spec §Config inheritance) |
| the conversation step (chat as a step), `brainstorm` + `goal-to-brief` blueprints | project-scope loops with no component (brainstorm uses a component loop for now — OL6) |
| `loop.goal_id`, derived `execution_state` and cost floor; human `*.decide` | any *new* UI page beyond what the rename forces |

## 2. What exists today, measured — so nobody re-derives it at seeding

- **A blueprint has lanes and steps and no output type.** `blueprint.Blueprint` is
  `{ID, SchemaVersion, Version, Lanes, Questions, OnAbort}`; a step's only statement about its product is
  `Produces Paths` — file paths (`internal/blueprint/types.go`).
- **`produces` is a file-existence check.** `missingArtifact(st.Root, step.Produces)` at
  `internal/loop/advance.go:283` and `continue.go:70`; the prompt is told the paths (`advance.go:460`).
- **Tasks have a table and no writer.** `store.CreateTask` exists with a real state machine
  (`internal/store/tasks.go`, `machine.go`); `plan.LoadDir` reads a `tasks/` directory and validates
  `depends_on` (`internal/plan/derive.go`). grep finds **no non-test caller of `CreateTask`**. *(Grep-measured;
  T0 confirms by replay.)*
- **The legacy agents did exactly the thing this slice replaces.** `opencode/agents/project-planner-loop.md`
  writes `plan.md` and `tasks/{taskid}.md` and reads loop state through MCP tools (`loop_get`, `loop_readFile`,
  `loop_readTask`); `brief-author.md` writes `docs/brief/NN-slug.md`. The ported Aeon handlers
  (`internal/handler/bodies/project-planner.md`: `tools: [read, write, edit, glob, grep, todo]`) kept the files
  and lost the MCP door — so today **nothing writes the entry**. The management catalog's tools are that door, internal.
  The legacy rule *"the planner never mutates loop state — the orchestrator owns it"* survives (OL3): record
  tools **create and describe**; they never move a task's status or decide a review.
- **Documents are rows.** `loop_document`, append-only by `rev`, `WriteDocument` (`internal/store/documents.go:257`);
  the store writes no files (DB10). An attachment can pin `(loop, name, rev)`.
- **The shipped `project` is one repo** — `root`, `remote_key`, `repo_key`, worktree roots in `project_root`,
  icon, owner, tags (`internal/store/projects.go`, migration 0001); every loop/task/step/document is scoped by
  `(loop_slug, project_root)` (DB4, `internal/store/scope.go`). That is the spec's **Component** in everything but
  name. Socket: `project.create|update|list`; the app has a Projects page and project forms.
- **Tools are a closed, pinned set, and registered is not granted.** Ten names pinned by `TestRegistryExactNames`;
  `DefaultGrant()` is `read, list, write`; a handler's `tools:` is an allow-list that is *"a declaration and never a
  grant"*, and S11's `ToolAccess` rows decide per exact tool name (AGENTS §4). Per-entity tools therefore get
  per-entity policy for free; one catch-all `record` tool could not (OL3).
- **A loop already has a chat** (P16: *"creating the loop opens the conversation"*; S31: the chat-first loop
  screen). `LoopStartParams` takes `Blueprint` and `WorkspaceRoot`; `loop.create` exists. How a chat with no
  blueprint maps onto this today is **T0-Q1**, not assumed.
- **Gates resolve by question and answer** (`approve | revise | abort`, `on_reject` with a counter).
- **Cost is stored per step** `known | partial | unknown` (DB11); P32: an aggregate over unknowns is a floor.
- **Migrations stand at 34** (`0034_trigger_gap`; S18 took 0027–0030, S19 0031, S20 0032–0034, all merged); S38 takes the next two (**0035** rename, **0036** records) — claim the next free numbers at seeding. A shipped migration is immutable (§3 of
  AGENTS), so the rename is a new numbered entry.

## Acceptance criteria — the arbiters

1. **No blueprint, still works.** *(Amended 2026-10-27: measured — every loop pins a blueprint today, so "no blueprint" means "the default chat blueprint", which S39 defines (a dedicated `chat` agent and built-in `chat` blueprint, added in S39 T6). S38 proves the grant gate inside a blueprint loop; the plain-chat half is closed by S39.)* In a plain chat loop with no blueprint, an agent granted `task_create` creates a
   task and an agent granted nothing cannot; a *hello-world* chat run touches no new code path and is
   byte-identical to today (tool specs offered, rows written).
2. **The brainstorm walk.** On the replay lane, a chat with the brainstorm agent: several turns of conversation with
   no run; then the user says *create the goal*; the blueprint runs (the agent `write`s `proposal.md` and
   `brief.md`, calls `goal_create`, `attach`es both); a `goal` row exists in `in_review` with two attachments pinned
   at the written revs; **the conversation is still open** and a following `goal_update` in the same thread bumps the
   version and returns it to `in_review`.
3. **The contract cannot be satisfied by a file alone.** A lane declaring `emits: goal` whose agent writes the
   files but never calls `goal_create` does **not** complete; the refusal names the missing *record*, not the
   missing file. A blueprint whose lane `emits:` something its agent's handler cannot call is `invalid` at load, or
   `unverifiable` when the handler is absent (P31) — never a surprise mid-run.
4. **Tasks get a writer, and cycles are unwritable.** `task_create` accepts `depends_on` only on tasks that already
   exist in this loop, so a cycle or self-dependency cannot be constructed; a duplicate id is refused with `plan`'s own
   `Refusal.Sentence()`. A loop task is `manual = false`, owned by its loop, assigned to the agent the blueprint routes
   it to; the ladder's `sets:` moves its status through `machine.go` exactly as today.
5. **A tool creates; it never decides.** No catalog tool changes `task.status` or `*.review_state` to a decided
   value; no registered tool and no handler reaches `goal.decide` / `brief.decide`; a grep-pinned test fails the
   build if one appears. Every Goal passes `in_review`; `approved` is written only with a `review` row whose actor
   is a user. `by_kind` of `agent` or `policy` is refused by the table's CHECK until their slice widens it.
6. **Two kinds of task, one board.** A loop task (`manual = 0`) is refused by the socket's `task.update`/`task.set_status` with a
   sentence naming its owning loop; a manual task is editable and has an approval state a human sets. A manual task exists with
   no loop, links to loops started on it, and its loop's tasks are its children. `task.list` filters by component, project,
   `manual`, assignee, status, type, goal and loop, carries a floor-flagged cost, and `task.changed` events keep a kanban live.
   `manual` is derived from origin at creation and unsettable afterwards. The `dev-loop` replay shows each ladder rung's
   assignee and status on the rows the board reads.
7. **The rename is complete.** `project`→`component` in the store, socket, CLI and app, with **zero** remaining
   references to the old meaning (grep-pinned list in the task notes); the new `project` tier exists with one
   implicit project per component, created by the migration and by `component.create`; the old-store upgrade test
   passes; `PROTOCOL_VERSION` is bumped and the generated contract regenerates (`make check-protogen`).
8. **Execution state is derived, never stored.** `goal.show` computes `not_started | running | blocked | done |
   failed` from its loops; no column holds it; a `partial|unknown` step cost makes the rollup a **floor** (P32).
9. **Existing blueprints load unchanged** (`output:`/`emits:` optional); with no catalog tool granted and no
   `emits:`, step runtime behavior and offered tool specs are byte-identical.
10. **One definition, two doors; the carve-out holds.** Each catalog action is defined once and appears as a socket
   method and as a tool; a test fails if either projection lacks a counterpart. **No decide action and no
   policy/MCP-config authoring appears as a tool** (grep-pinned). Grant `task_create` to one agent and
   `component_create` to another: each can call only its own, decided by S11 rows on the exact name.
11. **A prerequisite is enforced and spoken.** Starting `dev-loop` with no approved goal/task is refused with a
    sentence naming what is required; started from the chat it is the agent that says so and asks.
12. **Chat and steering are untouched** (OL15). Every `step.steer` / `step.steers` / `step.steer.cancel` conformance fixture and the S31 steering tests pass byte-identical;
    a catalog tool call dropped by a steer writes no row; the engine's own push-back rides the existing steer queue. **One S31 decision is reversed on
    purpose and only later** — the *finished loop hides the composer* rule — and it is S39's, not S38's.
13. **Deletion property.** The record layer is additive tables + the catalog + three optional YAML keys behind one
   package (`internal/records`); removing it returns blueprints to `produces:`-only. (The rename is not part of
   the deletion property — it is the foundation.)

## What this is not

- **No rendering, no sync-back.** Nothing generates a document from a row. If an agent edits `brief.md` after
  creating the brief entry, the entry's attachment still points at the old rev until the agent re-attaches; the
  UI says so (three states, P31: *attached rev current · superseded · missing*).
- **No second grant mechanism.** A blueprint's `emits:` never makes a tool available; only the handler's `tools:`
  list and S11's rows do. `emits:` is checked *against* them at load.
- **No approval policy engine.** Manual decisions only; `Review.By.Kind` admits `user`.
- **No new parser.** No templates; the document is the author's prose.

## Risks, named where they live

- **R1 — the rename's blast radius.** It is mechanical and wide: store, `Scope`, every query, socket, CLI, UI.
  T0-Q2 measures it; if it exceeds a threshold written in the investigation, the plan splits at the phase
  boundary (rename = S38a, records = S38b) rather than letting the crux wait behind a refactor.
- **R6 — manual tasks need an identity of their own.** Loop tasks are keyed `(component_root, loop_slug, taskid)`
  (DB4) and that key stays exactly as it is — **the ladder, the status machine, `iterate`, counters and escalation
  are untouched** for them. A manual task has no loop at creation, so `task` gains a surrogate `id` and a partial
  UNIQUE on the loop key where it exists (a rebuild in 0016's pattern; nothing shipped, so no data to carry). T0-Q6
  measures every reader of the old key and confirms it. Cost: one table rebuild and a read-side that no longer assumes a
  loop. Much smaller than first feared, because loop tasks are *already* the author's model.
- **R2 — the pinned tool set** grows by the catalog's size (T0-Q9 fixes the number; roughly two dozen). `TestRegistryExactNames` exists so a tool cannot
  appear unnoticed; T4 amends it with a dated note (S11's `write_file`→`write` precedent) and nothing enters
  `DefaultGrant()`.
- **R3 — a contract an agent cannot read.** If `emits:` only fails at the end, the agent learns too late. OL5
  appends a *"this step must also create: …"* block to the step prompt (the same place `advance.go:460` names
  `produces`), so the agent is told what the blueprint needs while it can still do it.
- **R4 — the conversation step's exit** (OL6): a chat has no natural end, and a contract that is satisfied is not
  the same as a user who is finished. T0-Q4 reads S31's machinery before the exit rule is final.
- **R5 — brief ownership.** The spec says one Brief per (Goal, Component), *owned by the Run*; a retried loop
  would orphan or duplicate it. OL8: owned by the loop, **versioned** (`brief_create` again is `Version+1`,
  back to `in_review`).

---

**Amended-unchanged 2026-11-04 (S60)** — the merge-queue rows key on the
component root exactly as this brief draws it; S60 changed no org-layer surface.
See `S60-merge-queue/`.
