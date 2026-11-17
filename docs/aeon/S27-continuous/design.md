# Design — S27 Continuous work

Written 2026-10-30 against the real code. Rules are **CN1–CN26**, cited from `plan.md` and the task files. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded in the rule that makes it.
**Brief:** [`brief.md`](brief.md) (its Q1–Q6 are answered here — see its appended "Superseded questions" section).
**Sketch:** [`SKETCH.md`](SKETCH.md) (narrowed by S20, 2026-10-27: the clock, the firing and the unattendedness proof are S20's; this slice is
**work selection** only).

Vocabulary: **root component** is the S44-extra tree's top level (`rootId`; a root with children *is* a project, derived). The scheduler's
unit of work is a **repository** — a `component_root` path, which every roadmap, queue, bug, loop and graph row is already keyed by and which
S44-extra leaves untouched (its CT12). Where a rule says "per repository" it means per `component_root`; where it says "tree" it means a
root component and its children.

## 0. Why this is still the largest change, and what makes it small

A scheduler is a new kind of caller — one that starts work when nobody asked at that moment. Mechanically it is **another caller of the one
start door** (`startLoopWith`, `daemon/start_loop.go:44`) that S20's schedules and S40's event triggers already walk; S8's runner already runs a
goroutine per running loop. What is genuinely new is **what to start, whether it may start now, and what happens when it cannot** — and doing all
three **visibly**, because a scheduler that starts work is exactly as legible as a person starting it (brief §8).

This slice is deliberately built **after** three prerequisites that make it safe (`brief.md` §5, extended by `AUTONOMY-GAPS.md`):

| Prerequisite | Provides | Without it |
| --- | --- | --- |
| **S55 LC** loop control and budgets | `loop.stop`, resumable `cap-hit` blocks (`loop.raise`), `budget` rows with an admission read, the watchdog | no off switch, no ceiling (brief Q5; §5's first bullet) |
| **S56 AL** autonomy grants | per-root-component grants of outward/merge, a resolve read, a revision id | an unattended loop can never open a PR (G14) — the scheduler would be a build-only machine |
| **S57 VG** verification gates | `gate-failed` blocked kinds, merge-on-green, post-merge watch | nothing makes an unattended merge trustworthy |
| **S44-extra** component tree | `rootId`, effective settings | the scheduler would be built on the model being replaced |
| **S24 / S26 / S32** | queue, roadmap, `max_parallel_agents` | the inputs |

## 1. Verified facts (re-walked by T0; file:line from 2026-10-30)

| # | Fact | Where |
| --- | --- | --- |
| F1 | `startLoopWith(ctx, LoopStartParams, startExtras)` is the **one** start door; `startExtras{StartedBy, TriggerID, Depth, FirstMessage}` is unexported so no socket client can reach it; event fires hand it `StartedBy:"event"` | `daemon/start_loop.go:35-44`, `daemon/event_fire.go:94` |
| F2 | A start's provenance reaches `loop_run.started_by_kind` only as `event` / `trigger` / `user`; the CHECK is `IN ('user','agent','trigger','event')` and Go admits the same four | `store/blueprint.go:168-178`, `store/runs.go:83-86`, `store/schema.go:2118` |
| F3 | `loop.started_by` / `trigger_id` / `automation_depth` exist (0033, 0039); `history` never reads `started_by`, so a loop's provenance does not change how S25 renders it | `store/loops.go:49-65,184`; grep of `internal/history` (no hit) |
| F4 | Queue statuses are seven (`pending sizing ready running done blocked cancelled`); `NextReadyQueueItem` returns the first `ready` row by `position,id` **only while no row of that repository is `running` or `blocked`** — a queue-level hold that does not see loops a person started | `store/queue.go:225-247`, `store/schema.go:2223-2248` |
| F5 | The queue reactor runs on `loop.finished`/`loop.blocked` from `publishLoop`; `blocked` (any kind) halts and raises a `gate` question on the blocked loop's slug; `done` + `queue_advance=auto` calls `advanceNext`, `ask` raises a question | `daemon/queue_reactor.go:38-112`, `daemon/blueprint.go:598-616` |
| F6 | `advanceNext` claims claimed-FROM (`ready→running`), calls `d.Table.StartBlueprintLoop`, stamps `loop_slug`; a failed start lands the item `blocked` with a question — the machine has no `running→ready` edge | `daemon/queue_reactor.go:115-176` |
| F7 | `queue_item.attended` exists (nullable 0/1, S24 D5/D6) and `advanceNext` passes it verbatim to `loop.start` | `store/schema.go:2233`, `daemon/queue_reactor.go:137-145` |
| F8 | Roadmap statuses are eight; `ready→running` is claimed-FROM in `roadmapMaterialize`; the queue-or-direct branch is "do rows exist for this repository" (**any** row, even `done`) | `roadmap/transitions.go:31-40`, `daemon/roadmap_dispatch.go:316-368` |
| F9 | A roadmap item has **no** blueprint, goal text or workspace: `roadmap.materialize` requires them from the **caller** (`RoadmapMaterializeParams`); the CLI makes them flags | `protocol/roadmap.go:145-153,436-438`, `cmd/aeon/roadmap.go:389-407` |
| F10 | `IsReady` = brief path set ∧ plan path set ∧ priority ≠ unknown; its own comment says *"a dependency GRAPH is nobody's until S27 consumes this"* — there is **no** dependency column or table | `roadmap/transitions.go:98-122`, `store/schema.go:2321-2346` |
| F11 | `priority_state` is `known|assumed|unknown`; `priority_label` is free text with no order | `store/schema.go:2327-2338`, `roadmap/types.go:69-85` |
| F12 | `roadmap_item` has **no** `attended` column (brief §6); `queue_item` does | `store/schema.go:2321` vs `2233` |
| F13 | `trigger.StillGoing` = `running|waiting|blocked`; the automation SQL overlap IN-list agrees (pinned by a test over six statuses) | `trigger/overlap.go:41-47`, `automation/dispatcher_test.go:470` |
| F14 | An event fire claims **before** it starts: `UNIQUE(trigger_id, outbox_id)`, outcome `NULL` = claimed-undecided, so a crash between claim and start cannot re-fire; limits (runs/hour, cost/day, depth) are checked at fire time only | `store/triggers.go:380-500`, `automation/dispatcher.go:95-180` |
| F15 | The window is exported: `Schedule.InWindow(t, loc)`, `LoadZone`, pair-or-absent validation; `trigger` is clock-injected (`Clock`, `TestNoTimeNowOutsideTheSystemClock`) | `trigger/schedule.go:152-231,301`, `trigger/clock.go:14-26,100` |
| F16 | Startup order: `ReconcileLoops` → `Recover` → `StartLoops` in one goroutine; `RunAutomation` starts only after `recoveryDone` | `cmd/aeon/serve.go:304-347`, `daemon/automation.go:23-31` |
| F17 | Loops' step-level caps (`MaxIterations 12`, `DeadlineAt 30m`), the reject counters and `LoopCeiling 10` stop a **single** loop; **nothing** limits how many loops start, per hour or at once | `runtime/limits.go:41,55`, `loop/counters.go` (report: loop-control investigation §2) |
| F18 | P28 ("per-loop memory budget and a global cap") has **no code**; "one running loop per project root" (ARCHITECTURE:63) is not enforced by `loop.start` either — only a *same-loop* double start is refused | `grep` of `internal/` (no `GlobalCap`/`MemoryBudget`); `daemon/blueprint.go:394` |
| F19 | `component.max_parallel_agents` (1..16, default 1) bounds **steps inside one loop** (S32), re-read fresh per advance; it is not a loop-concurrency limit | `loop/advance_parallel.go:39-52`, `migrations/20261030071034_s32_parallel_agents.sql:2` |
| F20 | `publishLoop` is the single emission point that calls the three reactors (`onQueueTerminal`, `onRoadmapTerminal`, `onTriageTerminal`) on terminal kinds, each in a goroutine | `daemon/blueprint.go:567-617` |
| F21 | There is no `loop.stop`/`cancel`/`pause` and no `aeon loop stop`; `step.stop` stops one step; a blocked loop is not answerable by `loop.answer`; `loop.retry_handler_block` handles only missing-handler blocks | `protocol/method.go:135-170,212-213`, `loop/retry.go` (report: §1) |
| F22 | `automation_event`/trigger authoring is human-only by **three locks** (S40 AU1); a trigger is an authority record and is never seeded (CONFIGURATION-ORDER rule 6 exception) | `docs/aeon/S40-automation/`, `CONFIGURATION-ORDER.md` rule 6 |
| F23 | The only existing "limits at fire time" cost read, `EventTriggerSpend`, has no socket method; `telemetry.workspace/component` give spend totals | `store/automation.go:271-292`, `protocol/method.go:460-463` |
| F24 | `notifyHintFor` toasts only `loop.finished`, `loop.blocked`, `loop.waiting`; no daemon push exists outside a connected client | `daemon/lifecycle.go:187-197` |

## 2. Decisions

### CN1 — A new package, a pure core, one start door

`internal/continuous` holds **a pure selection core** (`Plan(snapshot, settings, now) → []Decision`) and the seams the daemon fills
(`Reader`, `Starter`, `Clock`, `Grants`, `Budgets`). No SQL (I2: SQL lives in `internal/store`, `TestNoSQLLivesOutsideTheStore`), no `time.Now`
(one occurrence, the `SystemClock`, counted — the `trigger` precedent, F15), no goroutine of its own beyond the tick loop in `Worker.Run`.
Starting is the daemon's: `daemon/continuous.go` is the only place the package meets the daemon, as `daemon/automation.go` is for automation.
**The scheduler never calls anything but `startLoopWith`** (and the existing queue/roadmap claim transitions) — a call-site test pins it, as
`TestOneMaterializationPathEitherBranch` pins the roadmap's (S26 T4).

### CN2 — The scheduler is an authority record: human door, never seeded, never agent-toggled, default off

"Start work when nobody asked" is an authority exactly like S20/S40's triggers (F22). Therefore:

- **Rows.** `scheduler_state` (a singleton: master `enabled`, `paused`, the actor pair of the last change) and `scheduler_root` (one row per
  repository: `enabled`, the work template of CN5, window, the minimum priority state). Both carry `updated_by_kind/id` (OL7).
- **Default off, twice.** A fresh store has `enabled=0` and **no** `scheduler_root` rows. Nothing runs until a person enables the master **and**
  at least one repository. Enabling a repository needs the master **and** the repository, so there are two deliberate acts.
- **Never seeded.** The seed engine (S43-extra) is not used: `scheduler_*` is exempt exactly as triggers are (CONFIGURATION-ORDER rule 6, S49 AN2).
  A test asserts no seed set declares a `scheduler_*` record.
- **Human door only (P4).** Every write is a socket method on the person's 0600 + peer-credential surface and is **absent** from the narrow surface
  (`narrow.Actions()`, `BoundaryStatement()`), agent services, the tool registry, and the records catalog (reads only — CN20). `internal/narrow`,
  `internal/agentsvc`, `internal/tools`, `internal/records` contain none of the write method names (literal and constant grep). Sibling guards in
  `cmd/aeon` with a positive half first and a `…GuardsHaveTeeth` canary; no existing guard body is edited (T7). S40's AU1 gets a **sibling** assertion
  that the scheduler's rows have no non-person writer.
- **No inheritance of authority.** Enabling a repository enables **that repository only**. A child component attached to a root later is **not**
  enabled by its parent being enabled; `scheduler.enableTree` is a **bulk convenience** that expands, at the moment of the call, into one
  `scheduler_root` row per repository in the tree and records each (CN18). Authority that spreads silently when the tree changes is the failure mode.
- **Enable is confirmed.** Enabling states what it grants, in the daemon's own words (`scheduler.status` carries the statement), and the CLI/UI
  require `--yes`/`useConfirm` (a seatbelt, not a control — P5).

### CN3 — Unattended by construction; autonomy is derived from S56 grants and pinned; an item can only narrow

1. **`attended` is a hard `false`, authored in the daemon** at the scheduler's start call, never read from a row or parameter — the S20 `Fire`
   shape (D1): there is no field a queue row or a roadmap item can use to speak it. `TestSchedulerStartIsUnattended` drives a start end to end and
   reads `loop.attended = 0`.
2. **`allow_outward` and the merge permission come from S56.** At pick time the scheduler calls `Grants.Resolve(rootId, source)` (S56) →
   `{AllowOutward bool, AllowMerge bool, Revision string}`. They are **pinned into the loop at start** through the existing per-loop pin
   (`allow_outward` is migration 0031's column; S56 owns the merge pin column) — never re-evaluated mid-loop, exactly as today's declaration
   (S19 T6). The grant `Revision` is recorded in the decision row (CN16) and in `loop_run`'s provenance id (CN4) so "which rule allowed this?" is one read.
3. **An item may only narrow.** `roadmap_item` gains `autonomy_cap TEXT NOT NULL DEFAULT 'grant' CHECK (… IN ('grant','none','outward'))`:
   `grant` = take what S56 grants; `outward` = at most outward, **no merge**; `none` = nothing outward. Effective = `min(grant, cap)`; the CHECK has
   no value above the grant, so a widening is **unrepresentable**. A queue item's declared `attended` stays as it is (F7); its outward posture is
   the grant narrowed by an equivalent `queue_item.autonomy_cap` (same migration). This is the brief §6 amendment, **inverted by S56**: the opt-in
   used to live on the item; the grant now lives on the root component and the item is the narrowing.
4. **A person-declared `attended` queue item is never started by the scheduler** (F7): the person who wrote `attended=1` meant to be present.
   It is skipped with reason `attended-item` and runs only through the explicit `queue.advance` door. (Without this, the scheduler would either
   break G14 or silently turn an attended declaration into unattended.)
5. **S19's invariant stands:** `allow_outward` never unlocks `irreversible`; merge is its own S56 pin. S27 adds **no** path to widen either.

### CN4 — `started_by_kind = 'scheduler'`: a CHECK rebuild, not a reuse of `trigger` or `event`

Provenance must say who started a loop, and "trigger" (a clock instant) and "event" (an outbox row) would both lie. The CHECK is widened to admit
`'scheduler'` by the **0039 precedent**: a `loop_run` table rebuild (copy verbatim, drop, rename) with a copy guard and the index recreated **by
name** (F2; AGENTS.md §3's 0019 lesson — a rebuild that forgets its index dies on "already exists"). The Go half is `store/runs.go:84` plus the
mapping at `store/blueprint.go:173-178` (`StartedBy == "scheduler"` ⇒ kind `scheduler`). `loop.started_by` = `scheduler`; `loop.trigger_id` = the
**decision id** of the start (CN16) — the existing column names the originating authority record, here a scheduling decision. The migration
**reads the current DDL first** (S44-extra or another slice may have rebuilt `loop_run`; P6's rule).

History parity (F3): S25 renders a scheduler-started loop identically because it reads loop rows, not provenance. A test asserts a scheduler-started
loop's history entry equals a person-started one's except for the provenance fields (and, after S58, the inbox shows the origin).

### CN5 — The work template: per-repository, authored by a person, validated at enable time

A roadmap item has no blueprint, goal text or workspace (F9); the queue row carries them. The scheduler needs a **template** to turn a `ready`
roadmap item into `LoopStartParams`:

| Field (on `scheduler_root`) | Meaning | Rule |
| --- | --- | --- |
| `blueprint` | the blueprint a roadmap item runs | **required to enable**; validated at enable time through the blueprint validator (S48's `blueprint.validate`, or today's `loadStartBlueprint`): it must exist, validate, **end** (not `after: converse` — an idle loop would never free the repository), and declare no required input the scheduler cannot supply |
| `tech` | optional tech tag | passed as `LoopStartParams.Tech` |
| `with_json` | fixed `with` pairs | merged under the item's link pair |
| (derived) goal | the item's `title` | plus the link pair `roadmap_item`, `roadmap_role: materialization` (F8's `roadmapLink`) and the `brief_path`/`plan_path` in `with` so the loop sees them |
| (derived) workspace | the repository's `component_root` | validated by the existing `validateLoopWorkspace` |

Queue items carry their own `blueprint/goal/workspace/tech/with` (CG3); the template does **not** override them. The template is validated again at
pick time (a blueprint removed or broken since enable ⇒ `refused: template-invalid`, item untouched, `needs-you` — CN13).

### CN6 — Candidates, per repository, exactly one

At each tick, for every enabled repository the core yields **at most one candidate** — the next unit of work — chosen as:

1. **Queue first.** The head of the repository's queue (`NextReadyQueueItem`, F4) — a human-ordered list is an explicit instruction and S24's
   ordering is never second-guessed.
2. **Roadmap second, only into a drained repository.** A `ready` roadmap item is a candidate only when the repository has **no live queue rows**
   (`pending|sizing|ready|running|blocked`) and **no occupying loop** (CN8). It is then materialized by the **direct** branch — the scheduler does
   not append to a queue it just proved empty, and does not flood a queue (R4's "rows exist" probe, F8, is replaced for scheduler starts by this
   stricter predicate; `roadmap.materialize` by a person is untouched).
3. Among several `ready` roadmap items: priority state `known` before `assumed` (`unknown` is not `ready`, D4), then the item with **no unfinished
   dependency** (CN7), then oldest `id`. `priority_label` is display-only — it has no order (F11); an open decision (§4, OD3) asks whether to add one.

The core is a function of a snapshot, so **preview equals tick by construction** (CN15).

### CN7 — Dependencies become real: `roadmap_dependency`

`IsReady`'s "dependencies complete" is hollow (F10): the plan *is* the dependency record and no graph exists. An autonomous scheduler that cannot
express "B waits for A" will run B first or serialize everything by accident. This slice adds the smallest honest graph:

- `roadmap_dependency(item_id, depends_on_id, created_at, created_by_kind, created_by_id)`, PK `(item_id, depends_on_id)`, `CHECK (item_id <>
  depends_on_id)`, both FKs to `roadmap_item(id)`; an insert trigger refuses a **cross-repository** edge (same `component_root` only — the graph is
  per repository, matching every other rule) and a **cycle** (the store walks the closure inside the transaction; a trigger backstop checks the
  direct reverse edge).
- **`IsReady` is extended, not replaced:** the three existing conditions plus "every dependency is `done`". A dependency that is `cancelled` makes the
  item **not ready with its own sentence** ("depends on item N, which was cancelled — remove the dependency or cancel this item"): never silently
  ready, never silently stuck. Re-decided at the pick (D4's "now, never cached").
- Edges are authored by a person or an agent (they widen nothing): `roadmap.depend`/`roadmap.undepend` socket methods, an `aeon roadmap depend`
  verb, and `roadmap_depend`/`roadmap_undepend` catalog tools (direct, registered-not-granted). S50 (roadmap management, planned) adopts the audit and
  the board display of edges — a cross-slice request, not a dependency of this one.
- Pre-existing items have no edges: behaviour is unchanged until someone adds one.

### CN8 — Occupancy: a repository is occupied while any loop on it is still going

A repository is **occupied** while any `loop` row with that `component_root` has status `running`, `waiting` or `blocked` (`trigger.StillGoing`,
F13) — including loops **a person started** (the queue's own hold, F4, does not see those). While occupied, the repository has **no candidate**
(reason `repo-occupied`, naming the loop slug and its state). This is what makes the architecture's "one running loop per repository" true for
the scheduler without changing `loop.start` for people (F18 — a person may always start a loop; the scheduler yields).

- **`blocked` holds the repository** until a person acts — cancel the queue item, S55's `loop.raise`/`loop.stop`, or answer. This is brief §8
  made structural: **the scheduler has no code path that restarts a blocked item**, and a test enumerates the scheduler's start call sites to prove it.
- **`waiting` also holds**: the loop owes a person an answer; a second copy duplicates the work (S20 Q2's ruling, carried).
- The occupancy predicate is **one function** shared with S20: `continuous` imports `trigger.StillGoing`; the SQL IN-list is the same list; the
  existing agreement test (`TestStillGoingAgreesBetweenSQLAndTriggerPackage`, F13) gains the scheduler's SQL as a third party.
- A tick holds an **in-process mutex per repository** across occupancy check → claim → start, and re-checks occupancy **after** the claim and before
  the start. A person's `loop.start` landing in that window is not prevented (people are never blocked, CN8) — the next tick sees `repo-occupied`.
  Recorded, not hidden.

### CN9 — Capacity (brief Q1 and Q3): admission by agent slots, deferral is visible, never preemption, never refusal

P28's memory budget has no code (F18) and S32's `max_parallel_agents` is intra-loop (F19). The scheduler's cap is therefore **a count**, stated as
such: **agent slots**.

- A running loop **weighs** its repository's effective `max_parallel_agents` (S44-extra effective settings; ≥ 1). The daemon-wide cap is
  `scheduler.maxAgentSlots` (an S44 setting, integer 1..64, **default 2**). A candidate is admitted only if `used + weight ≤ cap`, where `used`
  sums the weights of **every loop the daemon is running** (`status = 'running'` and `loop.Runner.Active`) — person-started included, because memory
  does not care who started it. `waiting` and `blocked` loops hold no process (suspension is exit — the guarantee) so they weigh **0** here (they still
  occupy the repository, CN8).
- A candidate that does not fit is **deferred** — **queue, never refuse, never preempt** (brief Q1's recorded instinct, now decided): it stays `ready`,
  a `deferred` decision with reason `capacity` names the slots used, the cap and the weight needed. Preemption (stopping a running loop to start
  another) is **out** and would require S55's `loop.stop` as a human act.
- **Admission is fair, not first-come:** repositories are considered in the order of CN11, the first that fits is admitted, and a candidate whose weight
  exceeds the **whole cap** is refused **once** with `weight-exceeds-cap` (naming the setting) rather than starving forever.
- The honest limit is written into the setting's description and the preview: **a count of slots is not a memory measurement**; P28's byte budget stays
  an unbuilt promise, and this slice does not claim it (F18).
- Both caps are live reads of S44 settings (CS7: no cache), so a person lowering the cap takes effect on the next tick; running loops are not touched.

### CN10 — The working-hours window (brief Q4): optional per repository, gates every pick

Default **unrestricted**: the safety control against runaway cost is S55's budgets (and S56's grants), not a clock. A repository may declare a window
(`window_start`, `window_end`, `window_days`, `zone` — pair-or-absent, the TR8 contract, **the same validation function**, F15). The window gates
**every** pick — including the one that follows a finish (the sketch's open question: yes, it still gates; a worker that starts the next item because
the last one finished has no clock instant, so the tick's own `now` is tested). Outside the window the candidate is **deferred** with reason
`window-closed` and the **next open instant** in the sentence; opening is noticed by the tick (no separate timer). Items already running are
never stopped by a window closing.
`continuous.Window` is a thin value that builds a `trigger.Schedule{Window…}` and calls `InWindow` — one definition of a window, parity-tested.

### CN11 — Fairness and starvation

Across repositories, candidates are ordered by: (1) **aged** candidates first (deferred for capacity longer than `scheduler.agingHours`, an S44 setting,
default 24), oldest first — so a low-priority repository is never starved by a stream of higher ones; then (2) the **best candidate priority state**
(`known` before `assumed`); then (3) the repository whose **last scheduler start is oldest** (round-robin); then (4) `component_root` text (a total
order, so two runs of the same snapshot decide identically). Aging is derived from the decision log (the first `capacity` deferral of the subject since
it last started or was absent) — no extra column. Within a repository there is exactly one candidate (CN6), so intra-repository order is CN6's.

### CN12 — Rate backstop: the anti-runaway that is not a budget

S55's budgets bound spend; they do not bound **how often a loop starts**. A fast-failing or instantly finishing item chain, or a bug in selection,
could start loops in a tight cycle. `scheduler.maxStartsPerRootPerHour` (S44 setting, 1..60, **default 6**) caps scheduler starts per repository in a
sliding hour from the **decision log** (`started` rows); exceeding it defers with reason `rate-limit` and the instant the oldest start leaves the hour.
It is the AU11 `max_runs_per_hour` shape (F14), applied to selection.

### CN13 — A blocked unattended loop (brief Q2): the item says "needs you", durably, and the inbox hears it

An unattended loop cannot ask a question (G14: an `ask` denies at once, no row). When a scheduler-started loop reaches `blocked` the repository is held
(CN8) and the item must **show why and be findable**, without a question nobody can answer in time:

- The queue item is `blocked` (S24 CG2, unchanged) and the S24 reactor's gate question on the blocked loop still exists for whoever opens the app (F5) —
  S27 does not remove it.
- For a **roadmap-sourced** item (status stays `running`, D6/F8) the scheduler writes a `needs-you` decision row: `kind` from the loop's
  `BlockedReason.Kind` verbatim (`model-error`, `exhaustion`, `step-blocked`, `action-denied`, `action-failed`, S55's `cap-hit`, S57's `gate-failed`, …),
  the loop slug, the since-instant — **deduplicated** (one per loop block). It is the durable, queryable "needs you" state; `scheduler.attention` lists
  every open one (open = the loop is still `blocked`/`waiting`; **derived**, never a stored flag that can disagree — DB6).
- The scheduler **diagnoses nothing further** (S24 CG2's rule carried: a second taxonomy drifts); the kind is displayed, not interpreted — with one
  exception it must read: a `cap-hit` block is shown with S55's raise instruction (the sentence S55 supplies), because "raise the cap" is a different
  act from "retry".
- **S58 SV** subscribes to `scheduler.attention`'s source (cross-slice request C1): the inbox and notifications list it. Until S58 exists the existing
  `loop.blocked` toast (F24) is the only push; the preview and `aeon scheduler attention` are the pull.

### CN14 — The idempotent claim: `scheduler_start`, claim-before-start, deterministic slugs

The S40 shape (F14) carried exactly. `scheduler_start(subject_kind, subject_id, decision_id, claimed_at, loop_slug, outcome)` with
`PRIMARY KEY (subject_kind, subject_id)` (`subject_kind ∈ queue|roadmap`). The protocol:

1. **Claim:** `INSERT scheduler_start` (outcome `NULL` = claimed-undecided). The PK makes a **second claim of the same item by anyone** fail — this is
   also **the per-item attempt budget**: the scheduler starts an item **at most once, ever**. A person who re-queues creates a new queue item (new id).
2. **Transition:** the existing claimed-FROM `ready→running` (queue `TransitionQueueItem`, roadmap `Transition`). Losing the race ⇒ outcome `lost`, nothing started.
3. **Start:** `startLoopWith` with a **deterministic slug** — `roadmap-item-<id>` (the existing roadmap precedent, F8) and `queue-item-<id>` (new) — and
   the link pair in `with`. A start that landed collides on retry, which is the single-winner refusal.
4. **Stamp:** `SetQueueItemLoop`/`RecordLoop` and `scheduler_start.outcome='started'`, `loop_slug`.

**Recovery (a daemon killed mid-pick).** After `RecoveredStep` reconciliation, before the tick loop starts, `Worker.Reconcile` reads `scheduler_start`
rows with `outcome IS NULL`: a loop with the deterministic slug exists ⇒ stamp and record `started`; none exists ⇒ retry the start **once** (single
winner by slug); a start that returns a refusal ⇒ outcome `refused`, the S24 blocked path (queue) or a `needs-you` (roadmap), **never retried**. A
`refused` row is never revisited — "no restart on a timer" (brief §8) stays true; the one retry is for a crash gap, not a failure.

### CN15 — A dry run that is the real thing: `scheduler.preview`

`Plan` is pure, so `scheduler.preview` runs the **same function** over a fresh snapshot and returns, per repository: the candidate it would start (or
none) and — for **every** `ready` unit and every skipped one — the **first failing reason** from a closed vocabulary:

`master-off · paused · repo-off · repo-occupied · queue-live · attended-item · priority-unknown · not-ready · dependency-open · dependency-cancelled ·
window-closed · capacity · weight-exceeds-cap · rate-limit · budget-exhausted · grant-absent · template-invalid · already-claimed`

A test drives `Plan` and `Worker.Tick` over identical snapshots and asserts the tick's starts equal the preview's "would start" set (`TestPreviewIsTheTick`).
Every reason is a **sentence from ids and enums**, never user text in an audit summary (SEC-033).

### CN16 — Every decision is audited, append-only, deduplicated

`scheduler_decision(id, at, component_root, subject_kind, subject_id, decision, reason, summary, grant_revision, actor_kind, actor_id, loop_slug)`;
`decision ∈ started · deferred · skipped · refused · needs-you · lost · enabled · disabled · paused · resumed · settings`. **Append-only by trigger**
(UPDATE/DELETE abort — the `component_tree_`/S43-extra precedent); `summary` is a **fixed template** over ids and enums (P3, SEC-033). Writes happen in
the **same transaction** as the state change they record (a start's decision, claim and outcome commit together or not at all). **Dedup:** a `deferred`
or `skipped` decision is written only when the (subject, reason) **differs** from the last row for that subject — a repository blocked for a day writes one
row, not 5,760 — and the dedupe is a store function, not a `UNIQUE`. The `actor` of a control write is the kernel peer (`table.ActorFor`), never params;
the scheduler's own decisions carry `actor_kind='scheduler'` (a closed value added by the migration's CHECK). History **outlives** its subjects (no FK).

### CN17 — Starting and restarting the scheduler; the tick

`Worker.Run(ctx)` ticks every `scheduler.tickSeconds` (S44, 5..300, default 15) and **also** on a `Kick` (CN19). `daemon.RunContinuous(recoveryCtx)`
starts only after `recoveryDone` (F16), exactly as `RunAutomation` does, and returns immediately when the store or runner is absent. One tick =
snapshot → `Plan` → apply decisions in order, **one start per repository per tick**, never concurrent starts into one repository. `--no-recover`
disables the scheduler too (a daemon that did not recover must not start new work). The clock is injected; tests drive `Tick(at)` directly.

### CN18 — Controls are socket-first (brief Q6; P34)

Daemon first, CLI second, agents read-only. Socket methods (all `RequiresInitialize`, none `Debug`):

| Method | Kind | What |
| --- | --- | --- |
| `scheduler.status` | read | master/paused, per-repository state and template/window, effective settings with sources, the confirmation statement, counts of open attention items |
| `scheduler.preview {componentRoot?, rootId?}` | read | CN15 |
| `scheduler.decisions {componentRoot?, subject?, decision?, reason?, since?, limit?}` | read | the audit, newest first |
| `scheduler.attention {componentRoot?, rootId?}` | read | CN13 |
| `scheduler.enable` / `scheduler.disable` | human door | the master |
| `scheduler.pause {reason}` / `scheduler.resume` | human door | **stop picking** (running loops continue); `pause` requires a reason (stored, templated into the audit) |
| `scheduler.root.set {componentRoot, enabled, blueprint, tech?, with?, window?, minPriority?}` | human door | create/update a repository's row; validates CN5 |
| `scheduler.root.disable {componentRoot}` | human door | |
| `scheduler.enableTree {rootId, blueprint, …}` | human door | CN2's bulk expansion; refuses a repository with no valid template |
| `scheduler.stopAll {reason}` | human door | `pause` **plus** S55's `loop.stop` on every scheduler-started loop (a hard dependency on S55); the one "uninstall-free off switch" the sketch asked for |

`pause` is the operational off the sketch required ("an off switch that is not uninstall"), shaped like S20's `trigger.pause` (operational-only,
forbidden-token-pinned off the narrow surface — and S20's pause does **not** stop the continuous worker, which is answered: they are separate
switches, both visible in `scheduler.status`). **Whether S20's pause is enough: no** — it stops clocks; the scheduler starts work by finishes.
S27 handles the `loop.stop`-based `stopAll` only if S55 is built; T0 records it, and if S55's `loop.stop` is absent the method is **not registered**
(V13: absent, not refusing) rather than a half-door.

### CN19 — The reactors wake the scheduler; they stop starting for enabled repositories

When the scheduler is enabled **for a repository** (master on, not paused, repo row enabled) the S24 reactor's auto-advance must not also start the next
item — two starters per repository is two claim paths. Therefore:

- `handleQueueTerminal`'s `done` branch and `blocked` branch are **unchanged** in their status writes and question (F5). The `advanceNext` call (and
  the `ask` question) is replaced, **for enabled repositories only**, by `Worker.Kick(componentRoot)` — a non-blocking send that makes the next tick run
  now. For disabled repositories (and when the master is off) **the code path is byte-identical** (T1's goldens prove it).
- `queue_advance=ask` on an enabled repository: the question S24 would have raised is **not raised** (the scheduler is the decision-maker the person
  chose); T0 asks whether that is acceptable (OD5, default yes) because it removes a question a person might have wanted.
- `roadmap.materialize` by a person and `queue.advance` by a person stay exactly as they are — the **explicit doors** — and are not gated by the cap
  (a person's act), but their loops count toward capacity (CN9) and occupancy (CN8).
- The roadmap reactor's `done`/`blocked` handling is unchanged; a `Kick` is added beside it.

### CN20 — Agent tools: reads only (standing rule 2, with the scheduler as the stated exception)

Records catalog family `scheduler`, one definition with its socket twin (S38 "two doors"): `scheduler_status`, `scheduler_preview`, `scheduler_decisions`,
`scheduler_attention` — `safe`, `Mutates:false`, **registered, not granted**, counted `TestRegistryExactNames` bump (T8). **No write tool exists** —
not `enable`, not `pause`, not even a propose twin — because the scheduler is the thing that decides what runs unattended and nothing an agent
produces may move it. Agents may *see* what will run and why not (a planner choosing priorities benefits); that is all. An open decision (OD6) asks
whether a restrictive `scheduler_propose_pause` (a new `proposal.kind` table rebuild) is wanted; default **no**.
`roadmap_depend`/`roadmap_undepend` (CN7) are the only new mutating tools of the slice and widen nothing.

### CN21 — Settings (S44), descriptor-driven

Three daemon-wide settings declared in `internal/config/settings_decl_scheduler.go` against S44's closed registry (CS3): `scheduler.maxAgentSlots`
(int 1..64, default 2), `scheduler.tickSeconds` (int 5..300, default 15), `scheduler.maxStartsPerRootPerHour` (int 1..60, default 6),
`scheduler.agingHours` (int 1..720, default 24). They are **not** authority (they only tighten or loosen *capacity*); the **master switch and the
per-repository rows are** (CN2) and so are **not** S44 settings (S44's door is human-only but generic; authority records get their own audited door).
Per-repository values are columns of `scheduler_root`, not settings (S44 CS6: global scope only).

### CN22 — The UI (descriptor-driven, P7) — specified here, built in T10

A "Continuous work" panel on the real Settings screen: a **status header** (master, paused, slots used/cap, next tick), a **repositories list with
details** (template blueprint, window, minimum priority, last start, state with the reason), a **preview** ("what would run next and why not" — the
CN15 reasons, one line per ready unit), a **decisions log** (filters from `scheduler.decisions`), an **attention list** (CN13). Forms render from a
daemon descriptor in `scheduler.status` (blueprint choices from `blueprint.list`, window grammar, priority states); no vocabulary typed in the UI;
refusals verbatim with the draft kept; enable/`stopAll` confirm with the daemon's statement; offline disables writes. The real **tree UI** belongs to
the S44-extra follow-up; this panel scopes by repository and groups by root component using `component.list`.

### CN23 — Recovery and restart interplay (brief §3)

Daemon restart with the scheduler on: `ReconcileLoops` → `Recover` → `StartLoops` (running loops restart) → `Worker.Reconcile` (CN14) → `Run`. The
tick re-reads occupancy from `loop` rows, so a recovered `running` loop correctly occupies its repository and weighs its slots; a **killed** scheduler
mid-tick loses at most one in-flight pick, recovered by CN14. `scheduler_state.paused` and every row are durable (rows, not memory — P2).

### CN24 — Interplay with S55/S56/S57/S58 (the seams)

Declared as **local interfaces** in `internal/continuous` (P4's func-seam pattern, as `Table.MCP`), each with **nil = "feature absent"** semantics
in the **core** but a **construction error in production wiring** when the hard dependency is expected to be built (T6): `Budgets.Admit(repo, epic?)`
(S55), `Grants.Resolve(rootId, source)` (S56), `Stopper.StopLoop(slug, reason)` (S55), `Attention` source mapping for S57's kinds (read-only, by kind
string). S27 **must not** import those packages' internals; the seams are the contract, written as **Cross-slice requests** (§5).

### CN25 — What S27 will not do (carried and sharpened)

No autonomous outward action beyond an S56 grant; no invisible start (every start has a decision row, an event, a history entry); no restart of a blocked
item, ever; no preemption; no second start path; no seeded scheduler record; no agent write tool; no memory claim; no ordering of `priority_label`
text; no change to `loop.start` for people.

### CN26 — Testing seams

An injected `Clock` and `Tick(at)`; a fake `Starter`, `Grants`, `Budgets`; a `Reader` over a real temp store for the daemon tests; the pure core is
table-driven with **property tests** (unknown priority never starts; a `blocked`/`waiting` loop's repository never has a candidate; at most one start per
repository per tick; preview ⊇ start; deterministic for a given snapshot; the window obeys `trigger.InWindow`'s truth table).

## 3. Answers to the brief's questions

| Q | Answer | Rule |
| --- | --- | --- |
| Q1 — P28 collision: queue, refuse or preempt | **Queue**, visibly (`deferred`/`capacity` with the numbers); never refuse (except a candidate heavier than the whole cap, once), never preempt; the cap is a count of agent slots, not memory | CN9, CN15 |
| Q2 — what the item shows when its unattended loop blocks | the item/loop stay as S24/S26 leave them; a durable, deduplicated `needs-you` decision with the verbatim `BlockedReason.Kind` and a derived `scheduler.attention` list that S58 surfaces; the repository stays held | CN8, CN13 |
| Q3 — how many loops at once | a setting (`scheduler.maxAgentSlots`, default 2) weighted by each repository's `max_parallel_agents`; one scheduler loop per repository | CN8, CN9, CN21 |
| Q4 — working hours | optional per repository, default unrestricted, gates every pick including finish-driven ones; budgets are the safety control | CN10 |
| Q5 — where stop/cancel lives | **S55 LC** (hard dependency); the scheduler's `stopAll` calls it; absent ⇒ the method is not registered | CN18, CN24 |
| Q6 — socket-first controls | `scheduler.*` methods (CN18); CLI a client; agents read-only; descriptor-driven panel | CN18, CN20, CN22 |

## 4. Open decisions (recommended default, and why)

| # | Question | Default | Why |
| --- | --- | --- | --- |
| OD1 | Does `roadmap_dependency` belong to S27 or wait for S50? | **S27 (T3)** | the scheduler's ordering is wrong without edges (F10); S50 only needs to render and audit them |
| OD2 | Default `scheduler.maxAgentSlots` | **2** | one slot is serial and wastes the point; four is a guess about memory nobody measured (F18) |
| OD3 | Order `priority_label` (add a rank)? | **No — state only** (`known` > `assumed`, then age) | free text has no order (F11); inventing one would be a second authority. A person who wants strict ordering uses the queue |
| OD4 | Do `assumed`-priority items run? | **Yes, per-repo `minPriority` defaults to `assumed`** (D4 already admits them to `ready`); a person can raise it to `known` | the decision belongs to the repository's owner and is visible in the template |
| OD5 | When the scheduler is on, drop the `queue_advance=ask` question? | **Yes** | two deciders per repository is the failure; a person who wants to be asked leaves the repository disabled |
| OD6 | A restrictive agent `scheduler_propose_pause`? | **No** | needs a `proposal.kind` table rebuild for little; S55's budgets already stop runaway spend |
| OD7 | `weight-exceeds-cap` refuse-once vs defer-forever | **refuse once** with the setting named | a candidate that can never fit must not look "queued" for ever |
| OD8 | Should the rate backstop count person-started loops? | **No — scheduler starts only** | it bounds the scheduler; a person's pace is theirs |
| OD9 | `enableTree` — expand once or follow the tree? | **expand once** (CN2) | following the tree is authority that spreads silently |

## 5. Cross-slice requests (written here, owned elsewhere)

| # | To | Request |
| --- | --- | --- |
| C1 | **S58** | subscribe to `scheduler.attention` (CN13) and `scheduler.decision` events for the inbox/notifications; treat `needs-you` as an inbox class |
| C2 | **S55** | expose `Budgets.Admit(componentRoot, epicID?) → (ok bool, reason string)`; expose `loop.stop` as a human-door method **and** a callable seam; make `cap-hit` a `BlockedReason.Kind`; honour a per-loop budget pinned at start by the daemon (not via `LoopStartParams`) |
| C3 | **S56** | expose `Grants.Resolve(rootId, source string) → {AllowOutward, AllowMerge, Revision}` with `source` including `scheduler`; make merge a pin on the loop as `allow_outward` is today |
| C4 | **S57** | publish `gate-failed` (and the post-merge-red kind) as `BlockedReason.Kind` strings so `needs-you` shows them verbatim; no S27 code reads their meaning |
| C5 | **S44-extra** | the effective-settings read for `max_parallel_agents` by repository (`EffectiveForRoot`, its T4) |
| C6 | **S50** | render `roadmap_dependency` edges on the board and add them to its change audit; adopt `roadmap.depend` as an edit |
| C7 | **S63** | the `scheduler_start` outcome and `loop.finished` are the retrospective trigger; S27 emits nothing special |
| C8 | **S45** | if built before T10, reuse its `AgentPicker`/desk components; S27 does not depend on S45's desk (no proposal kind is added) |
