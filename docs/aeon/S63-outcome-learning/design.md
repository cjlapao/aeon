# Design — S63 Outcome learning

Rules **RT1–RT16**, cited from `plan.md` and the task files. Shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md); a deviation is recorded with its reason. T0 re-walks every
fact below.

## Verified facts (read from the tree 2026-10-30)

| # | Fact | Where |
| --- | --- | --- |
| F1 | `publishLoop` invokes three reactors **only** for `loop.finished` and `loop.blocked`, each as a goroutine that never blocks the emit path (V18) | `daemon/blueprint.go:602-616`; `daemon/queue_reactor.go:40-46` |
| F2 | `loop.finished` carries `{loopId, status}` with status `done`/`aborted`; a rest-idle loop emits `loop.run.finished` instead; `loop.blocked` carries a `reason` string kind | `loop/rest.go:94-103`; `daemon/queue_reactor.go:40-46,67` |
| F3 | `blocked` is **not** terminal: it can be retried (`loop.retry_handler_block`) or answered, and the loop can later finish | loop-control investigation; `loop/rest.go:24-37` |
| F4 | The one automatic learning writer is the triage feed; it uses `learnings.New(store).Append` — "S28's own write path is the law" | `daemon/triage_learning_feed.go:32-59` |
| F5 | `Append` validates scope, key, body, provenance; `relayed` is refused; `observed_count` only for `observed`, ≥ 0; a learning and its tags land in one transaction | `learnings/store.go:44-85`; `store/learnings.go:100-142` |
| F6 | M3: observed strength is **set once at write time** — no bump API exists | S28 `design.md` M3; `store/learnings.go` (no update function) |
| F7 | `project` scope_key is a repository root **path** (`component_root`), not a record id | `learnings/types.go:31-33` |
| F8 | Loop-level counters live in `RuntimeState.Counters` (`total_failed_gates`, `qa_rounds`, `splits_done`); task counters are columns on the task row | `loop/counters.go:55-62`; `schema.go:1887-1889` |
| F9 | Step rows carry the dispatched agent, outcome, cost state and floor (`encodeCost` stores a partial floor, never a bare total) | `schema.go:876-898`; `store/steps.go:402-439` |
| F10 | History merges live and archived loops with a floored `Rollup`; it has no counters, no per-agent facts, and is a read projection | `history/query.go:43-67,102+`; `history/rollup.go:12-35` |
| F11 | `RunCost{USDAtLeast, Partial, Unknown, Steps, Exact}` per loop/run is a GROUP BY, never a stored total | `store/runs.go:280-307` |
| F12 | Handoff `verification` is `pass|fail|unverified`; off-vocabulary reads as inert `""` | `handler/handoff.go:96-123` |
| F13 | Agent identity for tools is stamped by the records service (`Identify`), never taken from params | `daemon/records.go:89-108` |
| F14 | `internal/loop` and `internal/runtime` reference no learnings code (grep) | investigation 2026-10-30 |
| F15 | Triage validation loops are slugged `triage-validate-<id>` and already feed a learning on finish | `daemon/triage_dispatch.go:~265-274` |
| F16 | A daemon-started loop (queue advance) goes through the one `loop.start` door: `d.Table.StartBlueprintLoop` | `daemon/queue_reactor.go` (`advanceNext`) |

## RT1 — Facts first, summaries second

Everything in this slice stands on **recorded facts**, not on re-reading logs later. Two append-only tables:

- `retro_outcome` — one row per terminal event of a loop.
- `retro_agent` — one row per (outcome, agent) for agents that ran a step in that loop.

Both are written by one daemon reactor (RT2). Scorecards (RT7) are computed from them (plus history/telemetry for
what those already own); learnings (RT4) are rendered from them. A fact is **never edited or deleted** (trigger, P3);
a correction is a new row (`supersedes` not needed — outcomes are events). No FK from a fact to `loop` (history
outlives the loop, P3); the join key is `(component_root, loop_slug)`.

## RT2 — The recorder: a reactor at the existing emission point

`onRetroTerminal(kind, componentRoot, loopSlug, fields)` is called from `publishLoop` beside the queue, roadmap and
triage reactors (F1) — **one added call, no new subscription layer** — and, like them, only dispatches a goroutine.
It skips (records nothing) for: loops whose slug starts `triage-validate-` (F15), loops started by the retrospective
itself (RT5; tagged by blueprint id `retrospective`), and `loop.run.finished` (not in scope, brief §4).

On `loop.finished`/`loop.blocked` it reads, **in one read transaction where possible**:

| Recorded field | Source |
| --- | --- |
| `outcome_kind` | `finished/done`, `finished/aborted`, or `blocked/<reason-kind>` (reason kind = the reason string's leading token; unknown kinds stored verbatim as `other:<kind>` capped at 40 chars, enums/ids only) |
| `blueprint_id`, `blueprint_version` | the loop's pin |
| `wall_ms` | `ended_at − started_at` of the run (`loop_run`), else null |
| `cost_usd_floor`, `cost_partial_steps`, `cost_unknown_steps`, `cost_steps` | `LoopCost` (F11) — the floor, never a total |
| `tasks_done`, `tasks_failed`, `retries`, `review_rounds`, `security_rounds` | `ListTasks` summed (F8) |
| `qa_rounds`, `total_failed_gates`, `splits_done`, `escalations` | `RuntimeState.Counters` / escalation questions raised |
| `unverified_steps`, `verified_pass`, `verified_fail` | step handoff verdicts (F12) |
| `goal_id`/`goal_version`, `started_by_kind` | the loop row |
| `attended` | the pin (null stays null) |

`retro_agent` per agent slug (a name before S43 migrates — the slug is read via the agent row; unresolved stays the
literal name with `agent_slug_known = 0`): `steps`, `steps_done`, `steps_rejected`, `steps_blocked`, `verify_pass`,
`verify_fail`, `verify_unverified`, `cost_usd_floor`, `cost_partial_steps`, `cost_unknown_steps`, `agent_ms`.

A field that cannot be read is **null with `*_state = 'not-recorded'`**, never zero (P31). A read failure writes the row
with what could be read and `complete = 0` and logs; the reactor never panics and never blocks (V18).

Idempotency: unique key `(component_root, loop_slug, terminal_key)` where `terminal_key` is the event's sequence
(`seq` from the event append; T0 confirms it is available to `publishLoop`, else `loop.status`+`updated_at`). A replay
inserts nothing (`INSERT … ON CONFLICT DO NOTHING`).

## RT3 — The locked rule: nothing self-applies

> **Scorecards, retrospectives, outcome facts and learnings never change dispatch, routing, grants, budgets,
> policy, queue order, roadmap priority or blueprint selection. They are read by people (and, by a person's explicit
> grant, by agents) and decided on by people.**

Enforced three ways, each with a canary:

1. **Import guard** (`TestScorecardNeverReachedByDispatch`): a walk over non-test Go files asserts
   `internal/scorecard` and `internal/retro` (read side) are imported **only** by `internal/protocol` (via a local
   interface), `internal/daemon/scorecard_reads.go`, `internal/daemon/digest_reads.go`, `internal/records` (data, no
   import), `cmd/aeon/scorecard*.go`, and tests. It lists the forbidden importers by name — `internal/loop`,
   `internal/runtime`, `internal/policy`, `internal/tools`, `internal/handler`, `internal/model`,
   `internal/automation`, `internal/queue`, `internal/roadmap`, `internal/triage`, `internal/trigger`,
   and the dispatch files `daemon/blueprint.go` (`resolveDispatchAgent`), `daemon/roadmap_dispatch.go`,
   `daemon/triage_dispatch.go`, `daemon/queue_reactor.go`. Walk asserts ≥ 50 files seen.
2. **Write-side narrowness**: the recorder (`internal/retro` write side) imports `store` and `learnings` only; it has
   no import of any package that can start a loop, change a grant or edit a setting.
3. **Method classification** (S47): every `scorecard.*`, `digest.*`, `retro.*` method is `read`; a test fails if any is
   registered with `Mutates`. The only writes in this slice are the recorder's rows and the learning (RT4).

A sentence goes into every scorecard/digest result's `statement`: *"A score is a measurement of what happened. It does
not change what any agent, blueprint or project is allowed or asked to do."*

## RT4 — The retrospective learning: milestones, template, no free text

Why milestones: F6 — observed strength is set once at write time, so an "N-th time" count cannot be bumped; writing one
learning per loop would flood retrieval. Both are respected by writing at **tally milestones**.

After a `retro_outcome` row is inserted, the writer computes the tally
`n = COUNT(*) FROM retro_outcome WHERE component_root = ? AND outcome_kind = ?` — a **derived** count, no stored tally
table. If `n ∈ Milestones` (`1, 3, 10, 30, 100, 300, …` — ×3.33 steps, a named constant) and the outcome is **notable**
(RT4a), it writes one learning through `learnings.New(store).Append` — the one write path (F4/F5):

- `Scope = project`, `ScopeKey = component_root` (the repository the loop ran in — the S28/S29 precedent F7);
- `Provenance = observed`, `ObservedCount = &n` (true tally at write time; never changed afterwards);
- `SourceLoopSlug` = the loop that hit the milestone;
- tags (S28 M5 grammar): `retro`, `retro:outcome:<kind>`, `retro:blueprint:<id>`;
- `Body` = the **template** for the kind rendered from the closed placeholder set `{count, outcome_kind, blueprint,
  rounds_review, rounds_security, retries, gates_failed, cost_floor, cost_state, wall, escalations, window}` — every
  value an enum, an integer or a formatted floor. **No loop goal, task title, brief text, reason detail, agent output
  or model output ever reaches a body (SEC-033 / P3).** `Detail` strings of blocked reasons are not used, only kinds.

**RT4a — what is notable.** `done` with zero rework is **not** notable (the scorecard counts it; a learning each time
would be noise). Notable: any `blocked/*` kind; `aborted`; `done` with `review_rounds + security_rounds + retries +
qa_rounds > 0`; any escalation; any unverified step; (S57) a gate failure, a revert. The set is a function over the
recorded fields — one place, tested.

Idempotency: `retro_learning(outcome_id PRIMARY KEY, learning_id, written_at)` (append-only) links the outcome that
triggered a write to its learning. A startup reconcile (`retro.Reconcile`, run after the recorder is wired, before
reactors fire) finds outcome rows whose tally is a milestone, notable, and have no `retro_learning` row, and writes
them — so a crash between the fact and the learning loses nothing and duplicates nothing.

A refused `Append` (`*learnings.Refusal`) is logged and retried by the next reconcile; it is never fatal (the triage
feed's precedent). Retrieval sees these learnings like any other (S28 query path) — which is the point: the next loop
on that component can be *told* "review rounds on this repository have averaged 2 over the last 10 loops".

## RT5 — The optional model retrospective (off by default, capped, `assumed` only)

A richer, model-written retrospective is allowed **only** as an opt-in:

- setting `retro.model.enabled` (S44 declaration; default `false`; human door);
- on a **notable** outcome with the setting on, the reactor starts one loop of the seeded `retrospective` blueprint
  (S48 cross-request) through the one `loop.start` door (F16), `attended = false`, `allowOutward = false`
  (G14: unattended, no outward effect possible), with a budget cap from S55 (`retro.model.maxUsd`, default `0.25`, a
  setting), rate-limited per component (`retro.model.maxPerDay`, default `5`);
- agent `retrospective-author` (S43-extra seed set) has a **minimal grant**: `read`, `learning_add` (S52 LM7) — no
  bash, no write, no outward tool; its handler body states it summarises only the facts it is given;
- its input is the **fact record** (RT2 fields as JSON), never the transcript; its output is one learning, filed by
  S52's `learning_add` with provenance forced to `assumed` (an agent cannot claim `observed` — S52 LM4) and scope
  `project`/`component_root`;
- **no recursion**: a loop whose pinned blueprint id is `retrospective` is skipped by the recorder (RT2) and never
  triggers another;
- while S55 is unbuilt the setting cannot be turned on (the declaration refuses: "needs a budget cap (S55)") — a
  retrospective without a cap would be an uncapped model spend.

Everything else in the slice works with the model path absent.

## RT6 — Failure honesty

The recorder is **best effort and says so**: `complete = 0` rows and a counter `retro_dropped_total` exposed in
`retro.status` (count of events the reactor skipped on error). `retro.status` also reports the coverage start
(`MIN(recorded_at)`) so every scorecard can say "facts recorded since <date>; earlier loops are not included"
(RT9). Reconcile (RT4) covers the learning half; the fact half cannot be replayed from events, so a lost row is a
**named gap**, not a silent one — except via the optional backfill (RT13).

## RT7 — Scorecards are derived reads

`internal/scorecard` is a **pure read package** (no writes, no cache — N8). It takes the store, a `Subject`
(`agent slug | blueprint id | component id + includeChildren`), a window, and returns a `Card`:

| Metric (each `{value, n, state, note?}`) | Computed from |
| --- | --- |
| `loops` (n), `finishedDone`, `aborted`, `blocked` and their **rates** | latest `retro_outcome` per loop; `blocked` counts loops that **ever** blocked |
| `firstPassRate` | loops that finished `done` with zero rework / loops finished `done` |
| `reworkPerItem` | mean of `(review_rounds + security_rounds + retries + qa_rounds)` per **finished** loop; also `reviewRounds`, `securityRounds` separately |
| `gatePassRate` | first-pass gate results / gate results — **S57**; `cannot-say` until `gate_run` exists |
| `costPerCompletedItem` | floor: `SUM(cost_usd_floor)` over finished-done loops / count, with `unknownSteps`, `partialSteps` and a rendered "at least $X (K of N steps unknown)"; `cannot-say` if no finished loop has any known cost |
| `wallPerItem` | median and p90 of `wall_ms` over finished loops (median, never a mean of skew) |
| `escalations` | count and per-loop rate |
| `unverifiedRate` | unverified steps / steps |
| `mergedThenRevertedRate` | **S57**; `cannot-say` until it lands |
| **per agent only:** `stepsRun`, `stepRejectRate`, `verifyFailRate`, `verifyUnverifiedRate`, `costPerStep` (floor) | `retro_agent` |
| **per blueprint only:** `completionByVersion` (rates split by `blueprint_version`) | `retro_outcome` |

State vocabulary (closed, in `scorecard.vocabulary`): `ok`, `too-few` (n < `MinSample`), `cannot-say` (no
recorded data or a prerequisite slice absent), `partial` (a floor or incomplete rows, with the count). A value is
present **only** when state is `ok` or `partial`; `too-few` and `cannot-say` carry `value: null`. **No metric is ever
0 when the truth is "unknown".** Rates are shown with their `n`. `MinSample` is a named constant (`5`), surfaced in
the vocabulary, later adopted as an S44 setting (T5).

A subject with no facts returns an empty card with `coverage` and a sentence, not an error and not zeros.

## RT8 — Windows, comparison and tree roll-up

- Windows: `since`/`until` RFC3339 (default last 30 days) and a `last N loops` cap; the result echoes the effective
  window. Comparison is **two reads**, not a stored delta (`scorecard.compare {subject, windowA, windowB}` returns two
  cards side by side with the same `n` rules) — a person sees "before the blueprint change vs after" without anything
  being computed in the background.
- Component cards: a **root** component's card rolls up its children (S44-extra tree, depth two) by a
  `component_root IN (…)` over the tree's repository roots; `includeChildren=false` gives the node alone; a
  `breakdown` lists each child's loops/done/blocked counts only (no per-child score ranking — ranking invites gaming;
  RT11).
- Blueprint and agent cards are workspace-wide by default with an optional `rootId` filter.

## RT9 — Coverage honesty

Every card carries `coverage{recordedSince, loopsInHistory, loopsRecorded, loopsNotRecorded}` — `loopsInHistory` from
the history projection (S25), `loopsRecorded` distinct loops in `retro_outcome` — so "we only have facts for 12 of
47 loops" is on the card. Archived loops predating the recorder appear in history but not in `retro_*`: the card says so
and, where the optional backfill (RT13) ran, marks those rows `source: 'backfill'` with the fields that backfill
cannot know shown `not-recorded`.

## RT10 — The socket surface (P34; daemon first)

All `RequiresInitialize`, none `Debug`, **all reads** (RT3.3):

| Method | Params | Result |
| --- | --- | --- |
| `scorecard.agent` | `{agent (slug), since?, until?, last?, rootId?}` | `{card, statement}` |
| `scorecard.blueprint` | `{blueprint, since?, until?, last?, rootId?}` | `{card, statement}` |
| `scorecard.component` | `{id, includeChildren?, since?, until?, last?}` | `{card, breakdown, statement}` |
| `scorecard.compare` | `{kind, subject, windowA, windowB}` | `{a, b, statement}` |
| `scorecard.vocabulary` | `{}` | metrics (with descriptions), states, `minSample`, windows, milestones |
| `digest.learned` | `{id, since?, until?}` | RT12 |
| `retro.list` | `{id?, agent?, blueprint?, outcome?, since?, limit?, cursor?}` | outcome rows with `Describe` sentences |
| `retro.show` | `{outcomeId}` | the row, its agent rows, its learning link |
| `retro.status` | `{}` | recorder health: coverage start, dropped count, last write, reconcile state, model-retrospective setting state |

`Describe` renders each row as a sentence from ids and enums ("loop `x` on `y` finished done after 1 review round, cost
at least $0.42 (1 of 6 steps unknown)"). Errors: one new code block (T0 records the next free block, ≥ 2 codes):
`ErrScorecardRefused` (unknown subject, bad window) and `ErrRetroUnavailable`.

## RT11 — Agent tools (P6) and who sees what

One definition, two doors (S38 catalog), **registered, not granted**, reads only. Specs (family `scorecard`):

| Tool | Twin | Scope |
| --- | --- | --- |
| `scorecard_mine` | `scorecard.agent` (agent identity **stamped**, params carry none) | the calling agent's **own** card |
| `scorecard_component` | `scorecard.component` | the calling loop's **own root component** aggregate, **without** per-agent rows |
| `digest_learned` | `digest.learned` | the calling loop's own root component |

Decisions (recommended defaults, Open decisions OD2/OD3): an agent **never** reads another agent's card or the
per-agent breakdown (the S52 LM7 scoping rule: never another agent's agent-scoped memory); the component tool has no
`breakdown`; no tool lists agents ranked. Handler bodies are **not** changed to mention scorecards and no handler lists
the tools by default — an operator grants them knowingly, because an agent reading its own score is a behaviour
change the person should choose (RT3). The tool descriptions state the measurement-not-instruction sentence.
`TestRegistryExactNames` bumped **counted and dated**; default grant unchanged; no tool whose twin is a write exists.

## RT12 — The "what did we learn" digest (content spec for S58)

`digest.learned {id, since?, until?}` → a fixed-section structure S58 renders; **sections are data, order is fixed**:

1. `window`, `statement`, `coverage` (RT9).
2. `outcomes`: loops by outcome kind (counts and rates, with `n` states), compared with the previous equal window
   (two reads).
3. `rework`: review/security/retry/QA rounds, the three most rework-heavy **blueprints** (by id, not by item text).
4. `blocked`: top blocked kinds with counts (kinds only; S55 `cap-hit`/`stuck`, S57 gate kinds appear when they exist).
5. `cost`: floor, unknown count, per-completed-item floor, the largest loop **by id** (a link, never its goal text).
6. `learned`: learnings added in the window for this component's repositories, listed by id, scope, provenance (the
   four words), observed count and **their stored bodies verbatim** (they are already-validated learnings; the retro
   bodies are templates) — grouped `told`/`observed`/`assumed`/`relayed` in honesty order; retracted ones (S52)
   excluded.
7. `attention`: items needing a person — loops blocked right now (kind + age), pending questions count (from the
   existing awaiting read), proposals pending (S45 desk) — **links and counts**, and an explicit "nothing needs you"
   when empty.

The digest **performs no delivery and schedules nothing** (S58 owns the channel and schedule). Empty windows return the
sections with `state: 'cannot-say'`/zero loops and a sentence, not an error.

## RT13 — Optional backfill

`retro.backfill` (human door, dry-run default, `--yes` to write): derives `retro_outcome` rows for loops that
finished before the recorder, from **history only** (status, started/ended, floored cost, blocked kind from the stored
reason) with `source='backfill'`, `complete=0`, and counters/gates/agents `not-recorded`. It records nothing for loops
already recorded and never writes learnings. A preview shows counts by outcome first. Archived loops are included
only where the archive header carries the fields (T0 measures which). Backfill rows are excluded from rates that need
fields they lack (state `partial` with the reason), so they can never inflate a first-pass rate.

## RT14 — UI specification (the screens are built after this slice)

Specified, not built (the app work follows S44-extra/S45's components):
- **Projects → a root component → "Outcomes" tab**: the component card (RT7) with each metric's `n`, state pill
  (`too few` / `cannot say` / `partial`) and the floor rendering; window picker; compare-to-previous toggle.
- **Settings → Agents → one agent → "Track record"**: the agent card; a banner with the RT3 sentence; no ranking
  view, no "best agent" widget (RT11).
- **Settings → Blueprints → one blueprint → "Track record"**, split by version.
- **Digest view**: sections of RT12 in order; each link goes to the loop/learning/question; the attention section first
  when non-empty.
- Rules (P7): forms and labels from `scorecard.vocabulary`; states shown verbatim, never colour-only; no number
  rendered without its `n`; offline shows last data; en/pt parity; no `localStorage` for domain data; an rpc-guard
  test. These become a UI task in whichever slice builds the screens.

## RT15 — The evaluation boundary (parked)

This slice **does not** build model evals. Production outcomes are confounded (different tasks, repos, blueprints), so a
scorecard difference is a prompt for a person to look, not proof a model or agent is better. An eval would need:
a frozen set of tasks with acceptance checks (S57's definition of done), a way to replay them in an isolated
environment (S59), the same blueprint pinned, N repetitions per candidate (token cost, S55 budget), a rubric or
check script (not a model judging itself), and storage of runs apart from production facts. That is a future slice; this
one only guarantees the facts it records could seed one (loop ↔ blueprint version ↔ agent ↔ outcome, all by id).

## RT16 — Records, seed, guards and what does not move

- **Tables (migration, T1):** `retro_outcome`, `retro_agent`, `retro_learning` (append-only **by trigger**; no UPDATE or
  DELETE accessor + a package grep test), and `retro_template(kind PRIMARY KEY, version, template, origin, created_at,
  updated_at)` for the body templates.
- **Seed set (P8, rule 6, T3):** `retro.templates` — inert class (templates change no authority; they only shape
  learning text), `Version` guarded by a golden, `origin='system'`; operator rights stated `{Edit: no, Remove: no}`
  in v1 (OD5); restore explicit; startup applies it automatically (inert). Template placeholders are validated against
  the closed set at seed time and at render time; a template naming any other placeholder is refused.
- **Audit:** none beyond the append-only facts (they *are* the audit of outcomes); the learning carries its own
  `source_loop_slug`. Summaries elsewhere are fixed templates from ids (SEC-033).
- **Does not move:** S28's learnings code and write path (this slice only calls `Append`), the triage feed, history,
  telemetry, the counters table, any reactor's behaviour (the recorder is an added call), blueprints and handler bodies.
  Triage validation learnings stay triage's.
- **Security notes:** no secret anywhere; bodies cannot carry loop text; nothing here is added to `narrow.Actions()` or
  agent services (a sibling guard asserts the nine read methods stay off the narrow surface — records_guard pattern);
  no read leaks another agent's data (RT11).

## Open decisions (recommended default used in the tasks)

| # | Question | Default |
| --- | --- | --- |
| OD1 | Learning scope key for a retro: the loop's repository path (`component_root`) or the root component's own path? | **The loop's own `component_root`** (the S28/S29 precedent F7); tree roll-up is a scorecard concern, and a root with no repository has no path |
| OD2 | May an agent read its own card? | **Yes, tool exists but is granted to no one by default**; an operator grants it knowingly |
| OD3 | May an agent read component aggregates? | **Yes**, own root, no per-agent breakdown |
| OD4 | Unit of "item": a loop, or a roadmap/queue item? | **A loop in v1**; S27's attempt record (cross-request) lets a later task redefine "item" without a schema change here |
| OD5 | Are retro templates operator-editable? | **No in v1** (seeded, restorable); editing is a later task once the placeholder set has been lived with |
| OD6 | Minimum sample `MinSample` | **5**, a named constant surfaced in the vocabulary, adopted as an S44 setting later |
| OD7 | Milestone set | **1, 3, 10, 30, 100, 300, …** (named constant) |
| OD8 | Record `loop.run.finished` (idle/conversation runs)? | **No** in v1 — not an item of work |
| OD9 | Model retrospective on by default? | **No**; cannot even be enabled before S55 gives it a cap |
| OD10 | Backfill included in v1? | **Yes** (T2), dry-run default, never writes learnings |
