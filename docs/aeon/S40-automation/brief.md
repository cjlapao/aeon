# Brief — S40 Automation

**Implements:** the sixth ruling of [`../S38-org-layers/brief.md`](../S38-org-layers/brief.md) §0 (*automation and manual work together*) and
S38 OL14/OL13. Written 2026-10-27 as **S39's successor**: S38 gives agents the management tools and records, S39 gives a loop a
conversation and a kickoff message with a `source`; this slice lets **an event start the loop**.
`design.md`, `plan.md` and `tasks/T0–T10.md` sit beside this brief. **The plan opens with an investigation (T0).**
**Stage:** 5 · Fleet. **Depends on:** **S38** (T6 entity store functions, T7 task store, T15 socket surface), **S39** (T8 kickoff
renderer and loop-run start), **S10** (`TriggerAccess`, `CostCap` types), S11 (G14 attendedness, the gate), S5/S8 (step, blueprint
walk). **S20 and S19 are built** (2026-10-27): S40 reuses S20's loop-start door, provenance columns and fire vocabulary, and S19's per-loop `allow_outward`; it does **not** depend on S35. **Prefix:** `AU`. **Repo:** `aeon/` (P24) · Go 1.26 · `cgo` (B3)

---

## 0. The rulings this slice encodes (the author, 2026-10-27)

1. *"If there is automation then this was set by a human; we allow it to run whatever the automation — normally a blueprint of
   some sort — does."* A trigger is **standing authority**, written by a person.
2. *"On a goal, once approved we start the dev-loop blueprint, so that agent will get an automated text in a way to kick in the
   conversation."* An event starts a loop whose first message is pre-written.
3. **Manual and automated work together.** A manual task with an automation trigger starts the agent. The **trigger names its own
   event** (`created | approved | completed`); there is no global rule about when a hand-made task starts.
4. **Authoring a trigger is a human-only door.** It is not in S38's agent catalog (OL3's carve-out), and a grep-pinned test says so.
5. Internal events are first-party but still **bounded**: rate and cost limits, a cascade guard, and an honest log of every decision.

### 0.1 The one interpretation the author can overrule — "run whatever" versus G14 and S19's outward lever

S20 TR2 / S11 G14 say a loop started by a clock or an event is **unattended by construction**; S20 built it that way
(`daemon/trigger_fire.go` `StartFromTrigger`: `attended := false`, "authored by the start path, never supplied by the fire"). S19 T6 added
the **only** outward lever: `LoopStartParams.AllowOutward` → `loop.allow_outward` — a per-loop opt-in, declared by whoever starts the run, that
"lifts OUTWARD for an unattended loop and nothing else" (irreversible stays S16's ask). S20's fire path deliberately gives a schedule no way
to set it. Ruling 1 could be read as lifting both. **This slice does not read it that way, and adds no override:**

- **"Whatever" means any blueprint may be the target.** No allow-list of blueprints, no class of "safe" blueprints. A human wrote the
  trigger, so the daemon starts what it names.
- **It does not mean any outward action may run.** The started loop declares `attended = 0` and `allow_outward = 0`, exactly as S20's fires
  do. So "goal approved → `dev-loop`" runs plan, build and verify, and the first outward step (opening a PR, pushing) is **denied and
  recorded**, not performed. An operator who wants the PR opened has two existing human doors and no S40 code: an S11 `allow` row for the
  action, or (below) the per-trigger opt-in.
- **If the author means the stronger reading**, the change is one line at one call site plus one column: `EventStartRequest.AllowOutward`
  (always `false` in S40) is set from a new `event_trigger.allow_outward` column that a human writes at authoring, and `StartFromEvent`
  passes it as `AllowOutward:` to the start. It is **not built** because S19's design says a standing grant is a grant whose theft is
  unbounded, S20 refuses the same thing for schedules, and S39 CR7 says an event-started loop stays unattended even when a person chats into
  it. Recorded here so the choice is visible; irreversible actions stay S16's ask either way.

## 1. What this is

**One sentence:** a `trigger` row says *when event E happens to a goal or a manual task matching M, start blueprint B with
kickoff text K, within these limits*; domain writes drop a durable **outbox** row in their own transaction, a daemon goroutine
consumes the outbox by cursor, claims each (trigger, event) pair exactly once, starts the loop unattended through the same
function the socket uses, and records one of five outcomes.

| In this slice | Not in this slice |
| --- | --- |
| `automation_event` outbox written in the **same transaction** as the goal/task change | schedule and webhook triggers (**S20**, built: `trigger_schedule`) |
| closed event set `goal.approved · task.created · task.approved · task.completed` | events for loop tasks, loops, runs, or anything else |
| `event_trigger` and `event_trigger_fire` tables, **beside** S20's `trigger_schedule`/`trigger_fire` (§2 says why not extend) | `ApprovalPolicy` / auto-approval (**S41**) |
| dispatcher with durable cursor, dedupe claim, depth guard, limits, crash recovery | `spawn`/`wait`, config inheritance, project-scope loops (**S41+**) |
| `StartFromEvent` beside S20's `StartFromTrigger`; the closure body extracted narrowly so provenance and the kickoff land in the pin transaction | an agent tool to author triggers (never) |
| `automation.trigger.list/show/fires` and human-only `automation.trigger.create/update/enable/disable` | JSONPath input mapping, `DedupeKey`, `SecretRef` (webhook concerns) |
| a minimal bench page: list, enable/disable, fire log | a kanban, a trigger editor with previews |

## 2. What exists today, measured — so nobody re-derives it at seeding

- **Events are at-most-once with respect to domain writes.** `Daemon.publish` (`daemon/lifecycle.go:145`) appends an `event` row in its
  own statement **after** the domain commit and only logs a failure. A crash between the two loses the event. Automation cannot
  ride it: it needs a **transactional outbox**. The `event` table is append-only with no consumer cursor.
- **S20 is built and already starts loops from the daemon.** `(d *Daemon) StartFromTrigger(ctx, trigger.Fire)` (`daemon/trigger_fire.go`)
  walks `d.Table.StartBlueprintLoop` (the closure at `daemon/blueprint.go:281-322`, which calls `loop.Start` and `d.loopRunner.Start`) with
  `attended := false`, a deterministic slug (`Fire.Slug()` = `<trigger id>-<instant>`), then stamps provenance **after** the start with
  `Store.SetLoopProvenance(ctx, id, "trigger"|"webhook", triggerID)`. `loop.started_by` and `loop.trigger_id` exist
  (migration 0033, `NOT NULL DEFAULT ''`) and `store.NewLoop` already carries `StartedBy`/`TriggerID`, written by `CreatePinnedLoop`'s INSERT.
  There is **no** automation depth, no kickoff message and no event source.
- **S20's tables do not fit events.** `trigger_schedule` (0032/0033) is keyed by a schedule: `kind IN ('schedule','webhook')`, `zone NOT NULL`,
  `blueprint`, `workspace_root`, `goal`. `trigger_fire` is `(schedule_id → trigger_schedule ON DELETE CASCADE, instant, outcome NOT NULL
  ∈ fired|did_not_fire|could_not_say|skipped|refused, reason, loop_id, at)`, `UNIQUE(schedule_id, instant)`. An event fire is keyed by an
  outbox row, needs a *claimed-undecided* state (`outcome` is NOT NULL there), has no instant, and an event trigger has a match, a kickoff and
  limits and no zone. Extending would mean a `CHECK` rebuild of both tables plus a dummy schedule row per event trigger. **S40 adds
  `event_trigger` and `event_trigger_fire` beside them** and shares what is shareable: the loop provenance columns, `trigger.Outcome`'s vocabulary
  (mapped, AU12) and `trigger.StillGoing` (the overlap rule).
- **The S20 scheduler is not wired into `serve.go` yet** (no `trigger` reference there) and `Gate.Access` (TriggerAccess) is unwired; the S40
  dispatcher is an independent goroutine that does not need either.
- **S19's outward lever exists** (`loop.allow_outward`, migration 0031) — see §0.1.
- **Attendedness is a tri-state declared at start** (`loop.attended`, NULL/0/1; `loop/pin.go:113`, `store/loops.go:191`). The CLI sends
  `true`; an event-started loop must send explicit `&false`.
- **Slug is the idempotency handle.** `loop` has `UNIQUE(project_root, slug)`; a deterministic slug makes a second start of the same
  fire impossible, which is what crash recovery leans on.
- **Cost exists per step and has no aggregate.** `step.cost_usd` / `cost_state ('known'|'partial'|'unknown')` are written only by
  `Store.SetStepCost` (`store/steps.go:326`). `partial` is a floor; `unknown` must never be summed as zero (P32).
- **The daemon already hosts background goroutines with a ctx and a join.** `serve.go:187-203` (recovery) and `:206-228`
  (shutdown) are the pattern the dispatcher copies.
- **Today `CreateTask` has no non-test caller and goal/task store functions are S38's.** S40 edits them (T2); until S38 T6/T7 land
  there is nothing to edit. T0 records their real names.
- **The socket has no peer identity on `Session`** (`protocol/session.go:23`); `Daemon.peer(connID)` knows uid/pid. S38 T15 introduces
  the actor stamp that `*.decide` uses; S40 reuses it. T0-Q6 records the exact helper.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **The outbox is transactional.** Every goal/manual-task change in the closed event set inserts its `automation_event` row in the **same transaction**; a test forces a rollback and finds **no** outbox row and no domain row |
| **D2** | **Only goals and manual tasks emit.** A loop task (`manual = 0`) never produces an outbox row (Go guard **and** a DB trigger); this is what prevents loops-of-loops |
| **D3** | **Authoring is human-only.** `automation.trigger.create/update/enable/disable` refuse an actor of kind `agent`; the store function refuses `created_by_kind != 'user'`; **no agent tool, handler or narrow-surface action reaches them** (grep-pinned, S35's shape) |
| **D4** | **A matching trigger starts a loop** — any blueprint — **unattended** (`attended = 0`), with `started_by = 'event'`, `trigger_id`, `automation_depth` recorded, and a first chat message of `source = automation` naming the trigger; the trigger's `kickoff` overrides the blueprint default (S39 CR6) |
| **D5** | **Exactly once per (trigger, event).** `event_trigger_fire` is `UNIQUE(trigger_id, outbox_id)` and is **claimed before** the start; a daemon killed between claim and start is reconciled at boot to `started` or `could-not-say`, never to a second loop |
| **D6** | **Five outcomes, none silent.** Every matching (trigger, event) ends as `started`, `refused(reason)`, `limited(reason)`, `skipped(reason)` or `could-not-say(reason)` — and the fire log shows them with their reasons (P31, S20 TR4) |
| **D7** | **Limits are real.** `max_runs_per_hour` is a hard bound; `max_cost_per_day_usd` is computed from step costs with P32 honesty (partial = floor; unknown never counted as zero — when cost is unknown the cost limit does not pretend, runs/hour still bounds, and the fire says `cost-unknown`) |
| **D8** | **Cascades are bounded.** Events carry a depth; a run started by a trigger carries depth+1 and its events inherit it; depth > 3 is refused and recorded, never silently dropped |
| **D9** | **The grammar is closed.** `match` accepts only `event` (required) and `project`, `component`, `goal_type`, `task_type`, `source_kind` — equality or in-list — and refuses everything else with a sentence. Task type → blueprint needs **no mapping table**: it is a trigger with `match.task_type` |
| **D10** | **The surface is honest.** Socket reads and human-peer writes with conformance fixtures and regenerated protocol types; a minimal bench page lists triggers, flips enable/disable, and shows the fire log with the five outcomes |
| **D11** | **Nothing else moves.** With no trigger defined: S38/S39 suites pass unchanged, shipped blueprints load unchanged, `loop.start` over the socket behaves byte-for-byte as before (same result shape, `started_by = user`), the closed tool list is unchanged |
| **D12** | **S20 is reused, not duplicated.** Event fires reuse `loop.started_by/trigger_id` (value `event`), S20's `StartFromTrigger` pattern (`StartFromEvent` beside it), `trigger.StillGoing`, and an outcome vocabulary mapped onto `trigger.Outcome`; the two new tables sit beside `trigger_schedule`/`trigger_fire`, and S20's documents are **not** edited |

## 4. What this is not

- **No trigger authoring by an agent, ever.** A trigger is standing authority to run agents unattended; an agent creating one is the
  failure S35 and PL15 exist to prevent. There is no `trigger_*` tool and a test says so.
- **No outward-action override** (§0.1). The slice neither adds nor needs one.
- **No second event bus.** The existing `event` stream stays the UI's notification channel. The outbox is a separate, narrow,
  transactional table the dispatcher alone consumes.
- **No auto-approval.** An event may start a loop on an **already approved** goal/task; nothing here approves anything. `goal.approved`
  is a human's decision recorded by S38's `DecideGoal`; S41 owns policy-approved decisions.
- **No retry loop.** A failed start is `refused` or `could-not-say` and stays so; an operator re-fires by editing or re-approving.
- **No new refusal vocabulary for blueprints.** `PinRefusal` and S38's `requires:` sentences are carried verbatim into the fire log.

## 5. Needs human review before it ships (externally risky)

- **§0.1** — the unattended interpretation of "run whatever". Author to confirm.
- **Kickoff text** is rendered from a goal/task body. A goal whose `Source` is untrusted (S38 OL7) can reach a kickoff only after a
  human approved it, but its text is still **data**: S39's renderer owns the label; T4/T5 pass `Params.untrusted` through and a test
  asserts it. A reviewer should read the rendered kickoff of the demo once.
- **Limits' defaults.** `max_runs_per_hour` has no default (required, 1–1000); a trigger without a ceiling is not creatable.

## 6. Risks, named where they live

- **R1 — S38's functions are not built yet.** T2 edits `DecideGoal`, `CreateTask`, task approval and the completion transition. Their
  names and signatures below are S38's *planned* ones; T0-Q1 reads the real code and T2 begins with a grep. If a function has no
  transaction to join (e.g. it writes in two statements), T2 first wraps it — flagged to the S38 owner, not worked around.
- **R2 — event storms and cascades.** A manual task created by a loop that was started by a trigger that fired on a task created by a
  loop… The depth counter (AU5) is the bound; `max_runs_per_hour` is the second. T5's test walks a four-deep chain.
- **R3 — cost honesty.** A cost cap computed from unknown cost is a lie in either direction. AU11 states the rule and T5 tests the
  three cases (known / partial / unknown).
- **R4 — the crash window between claim and start.** Closed by slug determinism and the reconcile table (AU13, T6).
- **R5 — "human" is a peer-credential, not a login.** `Session` carries no identity today. D3 is only as strong as S38 T15's actor
  stamp; T0-Q6 and T8 pin the dependency and a test stubs an `agent` actor.
- **R6 — migration numbering.** The ledger runs to 0034 today (S20's 0032–0034, S19's 0031); S38 takes two and S39 one before S40. S40 uses **the next free number at seeding** (`00NN_automation`; 0035+ as of 2026-10-27) and never hard-codes it.
- **R7 — outbox growth.** Rows are small and append-only; trimming consumed rows is a follow-up (AU16), with a note in the demo.

---

> **Footnotes to revisit when S20 completes (author, 2026-10-27 — S20 is still in flight, so these are left as notes, not blockers):**
> 1. S20's scheduler is not yet wired into `serve.go`, and its T7 (TriggerAccess), T8 (`aeon trigger` control CLI) and T9 (demo) are open. T6 here wires the event dispatcher beside the scheduler; re-read how S20 ends up wiring it before seeding T6.
> 2. This slice describes trigger authoring as a **direct write by a human peer** on the socket, not a staged proposal (S35's closed `kind` enum is untouched). The wording that places it in the "family of PL15 / S35" should be justified or softened once S20's T7/T8 settle where trigger authority lives.
