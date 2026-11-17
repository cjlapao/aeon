# Design — S55 Loop control, caps & watchdog

Rules are **LC1–LC24**, cited from `plan.md` and the task files. Task shapes follow [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md); a deviation is recorded here with its reason. "Root component" is the S44-extra tree root (`rootId`).

## Verified facts (read 2026-10-30; T0 re-walks every row)

| Fact | Where |
| --- | --- |
| Loop `status` is `TEXT NOT NULL`, **no CHECK**; vocabulary enforced in Go only: `running`, `waiting`, `blocked`, `done`, `aborted`, `idle` | `store/schema.go:775-807`; writers `loop/advance.go:135,144,180`, `loop/rest.go:24-37` |
| Terminal statuses `done`/`aborted`; `endStatus(bp, aborted)` is **the one writer** of a walk's terminal (a DoD grep forbids a second `Status = "done"`) | `loop/rest.go:29-37`, `advance.go:~108-125` |
| Run state is a separate vocabulary `running|done|failed|cancelled` (CHECK); `runStateFor` maps loop `aborted` → `cancelled`; `EndRun` is called from `emitIfOver` | `store/schema.go:1979,2117`; `loop/rest.go:44-52,61-80` |
| Step stop exists: `step.stop`/`step.continue`; per-step cancel registry with a `stopped` truth bit; a stopped step is written `suspended` + `pause_reason='user'`; the loop's `Model` callback maps it to Kind `stopped`; the engine parks the loop `waiting`, `LastReason="stopped"`, **QuestionID 0**, emits `loop.waiting{reason:"stopped", stepId}` | `protocol/step_stop.go:67,112`; `daemon/stepcancel.go`; `daemon/blueprint.go:~258-266`; `loop/advance.go:285-302` |
| `step.pause_reason` is a plain `TEXT` (migration 0020, no CHECK) | `store/schema.go:1296` |
| The runtime returns **`failed`** (not `blocked`) when its context is cancelled; MaxIterations/DeadlineAt are checked **before each model call** and return `OutcomeBlocked` | `runtime/loop.go:215-232` |
| A cancelled step context is indistinguishable from a stop *except* via the registry's `stopped` bit; connection drop and daemon shutdown produce the same generic cancellation | `daemon/stepcancel.go` header |
| Loop runners are started with `context.Background()`; only `StartLoops(recoveryCtx)` passes a cancellable ctx; at SIGTERM only `cancelRecovery()` fires, then `d.Shutdown()` (terminals only) | `daemon/start_loop.go:122`, `runs.go:202`, `blueprint.go:287,308,375,1009`; `cmd/aeon/serve.go:355-374`, `daemon/terminal.go:50-57` |
| `Runner` holds `active map[string]struct{}` (no per-loop cancel); `Run` checks `ctx.Err()` at the top of each iteration | `loop/runner.go:15-76` |
| `bash` children run under `Setpgid`, killed as a group on timeout/ctx-cancel (SIGTERM, 2 s grace, SIGKILL); `cmd.Env = nil` inherits the daemon environment | `tools/spawn.go:128-160,236-243` |
| `AnswerEscalation` exists, serves only a gate question with options `[continue abort]`, no PendingAction; **no caller** outside `dev-loop_test.go:172`; `AnswerBlueprintLoop` routes by state to `AnswerActionAsk` or `AnswerGate` | `loop/escalate.go:317-361`; `daemon/blueprint.go:332-380` |
| Existing limits: `MaxIterations=12`, `DefaultDeadline=30m`, `LoopCeiling=10`, reject caps 2, `BashTimeout=600s`; automation `max_runs_per_hour`, `max_cost_per_day_usd` (fire-time only), `MaxAutomationDepth=3` | `runtime/limits.go:41,55`; `loop/counters.go`; `automation/dispatcher.go:129-180`; `store/automation.go:259` |
| Cost types `known|partial|unknown`; per step `cost_usd/cost_state/cost_detail`; per turn `step_turn` (tokens nullable); rollups `component_telemetry`/`workspace_telemetry`; per-loop floor `LoopCost` (`USDAtLeast`, `Partial`, `Unknown`, `Exact`); `EventTriggerSpend` never adds an unknown as zero | `model/types.go:274-300`; `store/steps.go:335,402-439`; `migrations/20261030084236_telemetry.sql`; `store/runs.go:280-307`; `store/automation.go:261-292` |
| `epic.budget_usd` is stored, returned, settable via `epic_create/update`, read by nothing | `store/epics.go:22,36,51,70,108`; `daemon/records.go:263-283`; `records/catalog.go:53,56` |
| `cost_cap` policy type: `CostCapSpec{Token, Max}` ("the presence of the row IS the cap"), registered with a **stub** evaluator that answers could-not-determine; `req.Cost` is a token name | `policy/spec_stubs.go:83-96,245`; `policy/evaluate.go:44` |
| Attendedness pinned once at start (`loop.attended` nullable, `loop.allow_outward`); unattended = no question rows; an unattended `then: escalate` collapses to `blocked` | `store/schema.go:709,1516-1518`; `loop/execute.go:68-93`; `loop/escalate.go:105-137` |
| `BlockedReason{Kind, Detail, Action, Slice}`; block kinds seen: `model-error`, `exhaustion`, `step-<outcome>`, `action-denied`, `runner-error`, … | `loop/action.go:21-26`; `loop/advance.go:248,403` |
| `CheckHandlerRetry` is the model for "one eligibility function, shared by `loop.show` and the op"; it refuses to replay a step that holds a receipt | `loop/retry.go:28-60` |
| Notify hints are a closed set of three kinds (`loop.finished|blocked|waiting`) attached at delivery | `daemon/lifecycle.go:155-197` |
| Loop status pins in tests: `statuses := [running waiting blocked done aborted idle]`; `StillGoing` = running\|waiting\|blocked | `automation/dispatcher_test.go:470`; `trigger/overlap.go:41-47` |
| The narrow surface is an enumerated allow-list; "operational control" is permanently absent (poisoned-table enumeration tests) | `narrow/actions.go`; S19 T1/T5 |
| Not yet checked (T0): `ArchiveLoop` status guard; `tools/question.go`; S20 schedule-side limits; queue (S24) limits; `commandMaxLen`; whether `step.updated`-style last-activity timestamps exist for the watchdog; the current migration count and latest migration | — |

## LC1 — No new loop status; three new *reasons*

The vocabulary is pinned by tests and by `StillGoing` (SQL IN-list). A new status would touch every reader and every trigger-overlap rule for no behavioural gain. This slice adds **no** status and **no** CHECK:

| Situation | Loop status | Marker |
| --- | --- | --- |
| Person stopped it | `waiting` | `LastReason="stopped"`, `QuestionID=0` (today's S31 D6 shape) |
| Person cancelled it | `aborted` (terminal) | `Flags["aborted"]`; run state `cancelled` |
| A budget was hit | `blocked` | `BlockedReason{Kind:"cap-hit"}` |
| Watchdog: stuck, attended | `waiting` | `LastReason="stopped"` + `stopReason` in the control event (a person resumes) |
| Watchdog: stuck, unattended | `blocked` | `BlockedReason{Kind:"stuck"}` |

`TestStillGoingAgreesBetweenSQLAndTriggerPackage` and the loop/loop_run CHECK tests stay byte-unchanged (a sibling test asserts the vocabulary is still exactly the six).

## LC2 — `loop.stop`: graceful, durable, exactly-once

`loop.stop {id, reason?}` (human door). A person-visible outcome in every case; never an error for "nothing was running":

1. Resolve the loop's running steps: every `StepStates` entry with `Status=="running"` and a `StepID` (parallel loops have several — S32 `Positions`), plus `LastStepID` when `StepInFlight`.
2. Call the **existing** per-step stop (`stepCancels.stop(id)`) for each **first** — it records the deliberate-stop bit, so the step parks `suspended`/`pause_reason='user'` and the engine maps it to Kind `stopped`. Cancelling the runner context first would race the step into `failed` (the runtime returns `failed` on a cancelled context, `runtime/loop.go:230-236`), so the order is fixed: **steps, then runner**.
3. With no step running (the runner is between actions, or the loop is `running` but idle between lanes), the runner is interrupted with a **typed cause** (`context.WithCancelCause`, cause `loop.ErrStopRequested`); the runner goroutine, on seeing that cause at the top of `Run`, calls `store.ParkLoopStopped(id)`, a **compare-and-set** (`status='running'` → `waiting`, `last_reason='stopped'`) in one transaction, then emits `loop.waiting{reason:"stopped"}`. A transition that lost the race (the loop moved to `waiting`/`blocked`/`done` meanwhile) is a no-op reported as `{stopped:false, status:<current>}`.
4. A loop already `waiting` at a gate/permission/step question, `blocked`, `idle` or terminal: `stopped:false` with the status and a pointer ("loop X is waiting on a question — answer it, or cancel the loop"). Stop is for *running* work.

The result is `LoopStopResult{loopId, stopped, status, parkedSteps:[stepId], note}`. Audit: one `loop_control_event` row per **changing** stop (LC10).

## LC3 — `loop.resume`

`loop.resume {id, message?}` (human door). Eligible only when `status=='waiting'`, `LastReason=='stopped'`, `QuestionID==0`:

- with a parked step (`suspended`/`pause_reason='user'`): delegates to the **step.continue** path (`CompleteSuspendedStep`/`OnStepContinued`, `loop/continue.go:42+`) — cold resume from the checkpoint; the optional `message` is the S31 stop-and-inject text, delivered exactly once;
- with no parked step (a between-steps stop): CAS `waiting`→`running` and `Runner.Start`.

It also resumes a watchdog `stuck` **block** (`BlockedReason.Kind=="stuck"`): the person has looked; CAS `blocked`→`running`. It does **not** resume `cap-hit` (that is `loop.raise`, LC9) and refuses every other block with that block's own remedy. Eligibility is **one function** (`loop.CheckResume`, the `CheckHandlerRetry` shape) used by `loop.show` and the op — the button and the daemon cannot disagree.

## LC4 — `loop.cancel`: terminal, cheap, certain

`loop.cancel {id, reason}` (human door; `reason` 1–512 bytes, stored as user text in its own column, never in an event summary). From `running`, `waiting`, `blocked` or `idle` (an idle loop's gentler door stays `loop.close`).

1. If running: stop steps then interrupt the runner (LC2 steps 2–3), waiting for the runner to exit (≤ 10 s, a named constant; on timeout the cancel still commits and the runner's next advance sees a terminal loop and exits).
2. Set `Flags["aborted"]=true`, clear the position, and let **`endStatus(bp, true)`** produce `aborted` through the existing `emitIfOver` path (so the **one-writer** rule and `loop_run` `cancelled` hold; no code writes the literal a second time).
3. **The blueprint's `on_abort` lane does not run.** Decision (Open decisions OD1): `on_abort` lanes are model/code work that can spend money and take outward actions; a cancel must be certain and free. The cancel records "on_abort skipped by cancel" in the control event. A person who wants the abort lane answers the gate with `abort` instead (unchanged).
4. A parked/suspended step row of the cancelled loop stays `suspended` (history honest: stopped, never continued). Parallel worktrees are settled by the existing `settleWorktree`/`ReconcileWorktrees` (T0 confirms the cancel path calls the same settle).
5. `loop.cancelled`-equivalent: the existing `loop.finished{status:"aborted"}` event fires; no new event kind is needed for the end.

## LC5 — Authority: stop, cancel, resume, raise are a **human door**; an agent may only *request*

All four (and `loop.control.dismiss`, `budget.set|remove`, `watchdog.set|…`) are socket methods on the person's 0600 + peer-credential surface (P4 shape). They are **absent** from `narrow.Actions()`/`BoundaryStatement()` (S19's "operational control is permanent-absent" is untouched), agent services, the tool registry and the records catalog — except the named twins in LC15. Actor = `table.ActorFor` (kernel peer), never params. The system actors `{system,watchdog}` and `{system,budget}` exist only inside the daemon and are stamped by the daemon's own code paths, never reachable from a request.

**Why an agent cannot stop or cancel (OD2).** Stop is restrictive, but the S35/S45 rule "restrictive-only for agents" protects *configuration*; a stop is *operational*: an agent (or a third-party plugin agent, S15) able to halt other loops is a denial-of-service lever across every project, and a cancel destroys progress irreversibly. The agent's honest need — "I am looping uselessly / I need a person" — is already served by suspending with a question. What an agent *can* do is **request** that a person stop a loop (LC15): an inert row and an event, nothing halts until a person acts.

## LC6 — The escalation door

`AnswerBlueprintLoop` gains one arm (`daemon/blueprint.go:332-380`), tried after the queue arm and before `AnswerGate`: `status=='waiting'`, no `PendingAction`, `QuestionID!=0`, question kind `gate`, options exactly `[continue abort]` → `loop.AnswerEscalation`. **No new method**: `loop.answer` keeps its params; the state decides the door (the comment at `blueprint.go:332-339` already states that contract). A test walks the S17 exhaustion path attended end to end over the socket. `AnswerEscalation`'s own body is unchanged.

## LC7 — Budgets are records, not policy

`budget` is a table, not a `cost_cap` policy row. Decision (OD3), against `policy/spec_stubs.go:83-96,245`:

- The stub's shape is "a cap on one *named token*; the row's presence is the cap". A budget is a **running aggregate** (spend so far across many loops and days) compared with several dimensions at once; the policy engine decides a *request* at a call site (`req.Cost` token) and carries no spend state.
- Scopes `epic` and `loop` are not policy target kinds; adding them widens the closed target vocabulary S45 manages.
- A cap **raise** is a loosening; in the policy model that is remove-and-add (two audit rows, a window with no cap). A budget raise is one audited update.
- Coupling the autonomy path to S45's policy desk would delay it for no gain.

`cost_cap` stays registered and reserved, **unchanged**; this slice's T-last annotates the S10 design with one line pointing at `budget`. (If S17-era intent ever returns, the policy type can be reimplemented as a reader of `budget`.)

## LC8 — The `budget` record

| Column | Type | Meaning |
| --- | --- | --- |
| `id` | TEXT PK (`bud-<id>`) | |
| `scope` | TEXT CHECK `workspace|component|epic|loop` | |
| `subject_id` | TEXT NOT NULL | `'workspace'` / a component id (a root's cap covers its **subtree total**, a child's only itself — the S44-extra CT7 rollup meaning) / an epic id / a loop id |
| `window` | TEXT CHECK `lifetime|day` | `day` = rolling 24 h |
| `usd_cap`, `tokens_cap`, `wall_ms_cap`, `steps_cap` | REAL / INTEGER, NULL = no cap on that dimension | CHECK at least one non-NULL; CHECK ≥ 0 (zero is a real cap: nothing may be spent) |
| `unknown_policy` | TEXT CHECK `proceed|stop` DEFAULT `proceed` | LC11 |
| `warn_pct` | INTEGER NULL CHECK 1–99 | emits a `budget.warn` event once when spend crosses it |
| `enabled` | INTEGER DEFAULT 1 | |
| `origin` | TEXT CHECK `user|migrated` | `migrated` for rows adopted from `epic.budget_usd` |
| `created_at`, `updated_at` | TEXT | |

Unique `(scope, subject_id, window)` — one budget per scope/subject/window; tightening several dimensions is one row. `budget_event` (append-only by trigger, P3): `created|changed|removed|raised|hit|warned`, actor, loop id (for `raised|hit`), old/new values as fixed numeric columns, no user text.

## LC9 — Enforcement: the step boundary **and** before each model call

Spend is read through one function (`internal/budget.Spend`) from existing rows:

| Dimension | Source | Notes |
| --- | --- | --- |
| USD (lifetime, component/root) | `component_telemetry` row (O(1)) | already exact/partial/unknown aware (S33) |
| USD (workspace) | `workspace_telemetry` | |
| USD (day window, epic, loop) | `step` aggregate (the `EventTriggerSpend` / `costQuery` pattern); epic = loops with `loop.goal_id → goal.component_id`'s epic (T0 verifies the link `loop.goal_id → goal.epic_id`) | index check in T0 |
| tokens | `step_turn` token columns (nullable → *unknown tokens*, LC11) | |
| wall | `step_turn.wall_ms` sum | |
| steps | `COUNT(step)` | |

**E1 — boundary:** in `AdvanceLoop`, before `dep.Model` is called for a model step (and before a `step.continue`/`loop.resume` re-dispatch), `Deps.Budget` (a func seam, nil = unenforced, like `Deps.Records`) evaluates every applicable budget (the loop's own, its epic, its component, its root component, the workspace). A breach parks the loop `blocked`/`cap-hit` **without starting the step** (position unchanged, nothing in flight).
**E2 — in-step:** `runtime`'s bound check at the top of each iteration (`runtime/loop.go:223-232`) gains a third bound through a `StepRequest.Budget` seam. A breach returns the step as **suspended** with `pause_reason='cap'` (a new *value* in the plain-TEXT column — no migration), *not* `blocked`: the step keeps its checkpoint, so `loop.raise` continues the **same** step with no lost work (`blocked` here would make the position replay-refused by receipt discipline, `retry.go:58-60`). The engine maps `pause_reason='cap'` to Kind `capped` and parks the loop `blocked`/`cap-hit` carrying the `stepId`.
**E3 — start:** `loop.start` refuses when an applicable budget is already exhausted ("budget B on component C is exhausted — raise it or start later"). S27 uses the same check.
**Honest bound.** Checks run *before* a call; one call can overshoot. The worst case is bounded by `Route.MaxOutputTokens × output price` plus input; `budget.show` reports "worst-case overshoot per call" from the route when priced, and says "not bounded in USD" when the route is unpriced. The slice never claims a precise ceiling.

## LC10 — Cap-hit is `blocked`, never a question (G14)

Unattended loops have no question rows (`loop/execute.go:68-93`, `escalate.go:137`). One path for both modes avoids a state only attended loops can reach: a cap hit **blocks** with `BlockedReason{Kind:"cap-hit", Detail:<budget id, scope, subject, dimension, spend floor, cap, window>}` and emits the existing `loop.blocked` (so every consumer, notify hint and the S58 inbox already see it) plus one `budget_event{hit}`. It is **not** `loop.answer`-able and **not** `loop.retry_handler_block`-able; the only door is `loop.raise`.

## LC11 — Cost honesty (P31)

Per budget `unknown_policy`:

- `proceed` (default; AU11's rule — `automation/dispatcher.go:172-178`): the cap is compared with the **floor** (`USDAtLeast`); unknown/partial steps do not stop the loop, but the evaluation result carries `costNote: "cost-unknown (N steps)"` and `budget.spend` shows `exact:false`. An unknown is **never added as zero** and **never reported as under-cap with a clean bill**.
- `stop`: any applicable step with unknown cost in the budget's window makes the USD dimension **unprovable** ⇒ treated as a breach (reason `cost-unproven`). For operators who want a hard dollar guarantee; it halts unpriced routes.
Tokens, steps and wall are exact numbers independent of price: the recommendation printed beside every unpriced route is "set a tokens cap" (the `budget.show` note). Unknown *tokens* (nullable turn columns) follow the same two policies on the tokens dimension.

## LC12 — `loop.raise` (human door)

`loop.raise {id, budgetId?, usdCap?, tokensCap?, wallMsCap?, stepsCap?, reason}`: for a `blocked`/`cap-hit` loop, raise the breached dimension(s) on the budget that was hit (default `budgetId` = the one in the block reason) and resume. Refusals (each a sentence): loop not `blocked`/`cap-hit`; new cap **not above the current spend floor** ("cap X is below spend Y — the loop would re-block immediately"); `budgetId` not applicable to this loop. One transaction: update the budget row, append `budget_event{raised}`, then resume (E1-hit: CAS `blocked`→`running` + `Runner.Start`; E2-hit: the step-continue path). Only the breached budget is touched; other applicable budgets are re-evaluated on resume and may re-block — the sentence says which.

## LC13 — Epic budget adopted, single truth

Migration backfills one `budget` row (`scope='epic'`, `usd_cap=budget_usd`, `window='lifetime'`, `origin='migrated'`) per epic with a non-NULL `budget_usd`. After it, `epic.budget_usd` is **no longer written or read**: `epic_create/update`'s `budget_usd` writes the budget row (the records path, `daemon/records.go:263-283`), and the epic view's `budgetUsd` is derived from it. The column stays (dropping it is a rebuild S55 does not need; a comment marks it dead). A grep test asserts no production read of the column outside the migration helper. Automation `max_cost_per_day_usd` is **unchanged** (fire-time admission control, AU11); the two compose (a fire can be admitted and its loop later capped).

## LC14 — Reads: spend vs cap, spend over time

- `budget.list {scope?, subjectId?, enabled?}` → rows with **effective status** (`ok|warn|hit`) and spend vs cap per dimension (floor + exact flag), who changed it last.
- `budget.show {id}` → the row, spend breakdown, applicable loops, worst-case-overshoot note, history from `budget_event`.
- `budget.effective {loopId | rootId | componentId}` → every budget that would apply (S27 CN calls this before starting work).
- `spend.report {groupBy: day|trigger|component|epic|route, since, until, rootId?}` → buckets `{key, usdAtLeast, exact, partial, unknown, tokens?, steps}`; `groupBy:trigger` exposes `EventTriggerSpend`'s logic to the socket. Reads only; derived, nothing stored.

## LC15 — Agent tools (rule 2; P6)

Specs in `internal/records/catalog.go` (families `loopcontrol`, `budget`, `watchdog`), each with a socket twin:

| Tool | Twin | Class | Mutates |
| --- | --- | --- | --- |
| `loop_control_state` | `loop.control` (read: eligibility of stop/resume/cancel/raise, open stop requests, caps hit) | safe | no |
| `budget_list`, `budget_show`, `budget_spend` (= `spend.report`), `budget_effective` | `budget.*` reads | safe | no |
| `watchdog_list` | `watchdog.list` | safe | no |
| **`loop_request_stop`** | `loop.requestStop {loopId, reason}` | detectable | **yes** — inserts an inert `loop_control_request` |

`loop_request_stop`: schema `additionalProperties:false`, **no** identity/status fields; the daemon stamps the agent identity (slug/handler/loop). Reason 1–512 bytes. **Per-loop open-request limit 5**, per-agent 20 (named constants, counted refusals). It only records a request and emits `loop.stop.requested`; nothing halts. A person sees it (via `loop.control` / S58 inbox) and acts or `loop.control.dismiss`es. No tool stops, cancels, resumes, raises, or sets a budget. Registered, **not granted**; `TestRegistryExactNames` bumped counted and dated; descriptions ≥ 40 chars.

## LC16 — The eligibility block on `loop.show`

`LoopView` gains `control: {stop, resume, cancel, raise: {eligible bool, refusal?{kind, detail}}, openRequests, capHit?{budgetId, scope, dimension, spendFloor, cap}}`. Computed by the **same functions** the ops call (`loop.CheckStop|CheckResume|CheckCancel|CheckRaise`). The UI renders the daemon's answer, never its own guess (`CheckHandlerRetry` precedent).

## LC17 — The watchdog

A daemon service (`internal/daemon/watchdog.go`) evaluates `watchdog_rule` records every `interval` (a setting-free constant 30 s, jittered) over **running loops only** (a loop `waiting` on a person or `blocked` is *not* stuck — never touched). Signals (closed vocabulary, served by `watchdog.vocabulary`):

| Signal | Meaning |
| --- | --- |
| `no-activity` | a `running` loop with no event, step checkpoint or step row update for `thresholdMs` |
| `same-position` | the loop has re-dispatched the same position ≥ `threshold` times (retries/redo that the counters did not cap) |
| `repeat-block-reason` | the same `LastReason` text hash observed ≥ `threshold` consecutive times |
| `step-over-deadline` | a step still `running` past its manifest `DeadlineAt` + grace (the runtime should have stopped it) |

Actions (closed): `notify` (an event `watchdog.fired`; delivery is S58's), `stop` (parks `waiting`/`stopped` through LC2's CAS with actor `{system,watchdog}`), `block` (CAS `running`→`blocked`, `Kind:"stuck"`). **Ladder** = several rules on one signal with rising thresholds (`notify` at 15 min, then `stop`/`block` at 60). The effective action for a `stop`/`block` rule is chosen by attendedness at fire time: attended ⇒ `stop` (a person is present to resume), unattended ⇒ `block` (G14: no question). Idempotent: one `loop_control_event{kind:watchdog}` per (loop, rule, episode); a loop that makes progress resets its episode. **No automatic retry or re-dispatch exists** (OD8; S27 brief §8).

## LC18 — `watchdog_rule` records and the seeded default set

Table `watchdog_rule(id, scope workspace|component, subject_id, signal, threshold INTEGER, action notify|stop|block, enabled, origin system|user, …)`. Declared set **`watchdog.default`** (P8) via the S43-extra engine: class **inert** (it only ever *restricts* running work; it widens nothing), applied at daemon start; defaults `no-activity 15 min → notify`, `no-activity 60 min → stop|block`, `same-position 6 → stop|block`, `step-over-deadline → block`. Operator rights `{Edit, Remove, Disable}`; a removed/diverged system row is never silently re-seeded (rule 6). Writes: `watchdog.set|remove|setEnabled` on the human door (P4). Because *disabling* a rule loosens a safeguard, `watchdog.setEnabled{false}` and `remove` of a system rule require `--yes` (seatbelt) and audit.

## LC19 — Daemon drain (shutdown)

Runners derive from a **daemon-lifetime context with cause** (`context.WithCancelCause`, cause `ErrDaemonShutdown`); `Runner.Start(ctx,…)` keeps a per-loop cancel (`active map[string]*runnerEntry`). At SIGTERM (`serve.go`): (1) `Runner.Drain()` — no new `Start`, no new advance; (2) cancel the loop context with the shutdown cause; (3) steps observe cancellation through the unchanged `ctx.Err()` check and bash process groups are terminated by `spawn.go`'s existing ctx handling (SIGTERM, 2 s, SIGKILL); (4) **a step cancelled with cause `ErrDaemonShutdown` is NOT committed as `failed`**: `handleStep`/`RunStepForLoop` consult `context.Cause(ctx)` (the registry-bit pattern, extended) and leave the row `running` at its last checkpoint for the existing recovery walk (`daemon/recover.go:63-125`, receipts case A/B, interrupted-cost floor); (5) the existing `shutdownGrace` (30 s) bounds the wait. If T0/T3 finds the commit path cannot distinguish the cause without a wider change, the fallback (OD7) is: **no behaviour change** plus process-group termination only, with the orphan risk documented — the slice never *worsens* recovery.

## LC20 — Events, notify hints, vocabulary (for S58)

New event kinds (closed list in `protocol/events.go`): `loop.stop.requested`, `loop.control.dismissed`, `budget.warn`, `budget.hit`, `watchdog.fired`. Cap-hit/stuck also ride the existing `loop.blocked` with `reason.kind`; a stop rides `loop.waiting{reason:"stopped"}`. `notifyHintFor` gains `loop.stop.requested` (warning), `budget.hit` (warning), `watchdog.fired` (warning) — the hint is **delivery-time only** like today's three. Cross-slice request to S58 SV: surface these kinds and `blocked`/`cap-hit` in the inbox.

## LC21 — Migrations (timestamped, never `schema.go`)

Three files, each one transaction, each with copy/`pragma_foreign_key_check` guards where a table is rebuilt (none is rebuilt here — all new tables), `date +%Y%m%d%H%M%S`, ordered after whatever is merged when T2 starts:

1. `…_loop_control.sql` — `loop_control_event` (append-only triggers), `loop_control_request`.
2. `…_budget.sql` — `budget`, `budget_event` (append-only triggers), epic backfill (LC13).
3. `…_watchdog.sql` — `watchdog_rule`.

Migration-count assertions bumped by **3**, T0 lists every literal (`policy_test.go:306-332`, `schema_test.go:908-922`, `loop/counters_test.go:293-310`, ledger checks, the `[:N]` vintage slices stay). The loop table is **not** altered.

## LC22 — The CLI (P5; rule 1)

`aeon loop stop ID [--reason T]`, `resume ID [--message T]`, `cancel ID --reason T --yes`, `raise ID [--budget B] [--usd N] [--tokens N] [--wall DUR] [--steps N] --reason T --yes`, `aeon loop requests [ID]`, `aeon loop dismiss REQ`; `aeon budget list|show|effective|set|remove|spend`, `aeon watchdog list|show|vocabulary|set|enable|disable|remove`. Each is a socket client; `--json` is the wire result; the CLI re-validates nothing; `--yes` is a seatbelt (required for cancel, raise, budget remove, watchdog disable/remove). **No offline fallback** (recorded deviation from P5): these verbs act on a *running* daemon; with none, the fixed sentence "no daemon is running — there is nothing to stop" / "…budgets are applied by a running daemon; `aeon budget set` needs one" and a non-zero exit. Existing `loop` verbs keep flags and output (goldens).

## LC23 — Security notes

No secret is involved. All writes stamp the peer actor; summaries are fixed templates from ids and enums (SEC-033); `reason` text is stored in its own bounded column. Stop/cancel/raise are rate-trivial but audited. The narrow surface remains unable to stop anything: a CI job that triggers loops (S19) cannot cancel them by design — that is a person's act.

## LC24 — What deliberately does not change

`step.stop`/`step.continue` (reused, not altered); `loop.close`; the blueprint `abort` gate answer and `OnAbort` lanes; reject counters and `LoopCeiling`; `MaxIterations`/`DefaultDeadline`; automation limits (AU11); `allowOutward`/attendedness; the policy `cost_cap` stub; loop status vocabulary and its pin tests; the model client's own retry/backoff.

## Open decisions (default chosen; reversible before build)

| # | Question | Default | Why |
| --- | --- | --- | --- |
| OD1 | Does `loop.cancel` run the blueprint's `on_abort` lane? | **No** | a cancel must be certain and free; the abort lane stays reachable by answering a gate `abort` |
| OD2 | May an agent stop or cancel a loop? | **No — request only** | operational DoS lever / irreversible; LC5 |
| OD3 | `cost_cap` policy or a `budget` table? | **Table** | LC7 |
| OD4 | Default `unknown_policy` | **proceed** (+ steps/tokens/wall exact caps) | consistent with AU11; `stop` available per budget |
| OD5 | Cap-hit when attended: question or block? | **Block, both modes** | one path; G14 |
| OD6 | In-step cap hit: fail, block, or suspend? | **Suspend (`pause_reason='cap'`)** | resumable with no lost work; `blocked` would be replay-refused |
| OD7 | Shutdown: cancel-with-cause and leave steps `running`, or no change? | **Cancel-with-cause**; fallback = process-group kill only | LC19 |
| OD8 | Automatic retry of a blocked/stuck loop | **None** | S27 brief §8 refuses timer restarts |
| OD9 | Budget windows | **`lifetime` and rolling `day`** | rollups are lifetime; calendar months invite timezone/DST ambiguity |
| OD10 | May agents lower/propose budgets? | **Reads only** | needs S45's desk to do it safely; revisit after S45 |
| OD11 | Parent/child budget interplay | **Independent; all applicable checked; tightest breach reported** | no inheritance arithmetic to get wrong |
| OD12 | Stuck action when attended/unattended | **stop / block** | G14 |
| OD13 | `loop.stop` on a loop waiting at a gate | **Refused with a pointer** | nothing is running; cancel exists |
| OD14 | Default watchdog thresholds | see LC18 | tunable; seeded, operator-editable |
| OD15 | Cancel's suspended step row | **Left `suspended`** | honest history |

## Cross-slice requests

- **S27 CN:** call `budget.effective` and refuse to schedule a root with *no* applicable budget unless the operator opted out; never treat a `cap-hit`/`stuck` block as restartable by a timer; honour `loop.start`'s exhausted-budget refusal as "not ready".
- **S56 AL:** `loop.raise` is a budget act, **not** an autonomy grant; keep the two audits separate. The pinned `allow_outward` stays as is.
- **S58 SV:** deliver the LC20 events and the `blocked`/`cap-hit`/`stuck` states; the inbox lists open `loop_control_request` rows.
- **S57 VG:** a verification step that exhausts its budget parks `cap-hit` like any model step; no special case.
- **S62 BK:** include `budget`, `budget_event`, `watchdog_rule`, `loop_control_*` in the backup set (they are ordinary tables).
- **S63 RT:** `budget_event{hit|raised}` and `loop_control_event` are inputs to scorecards.
- **S44-extra:** none (consumes `rootId` and the effective-settings resolver only).
