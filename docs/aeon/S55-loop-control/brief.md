# Brief — S55 Loop control, caps & watchdog

Written 2026-10-30. Prefix **LC**. Origin: [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G1** (loop stop/cancel, hard caps) and the watchdog half of **G8**; the first slice of the autonomy path (S55 → S56 AL → S57 VG → S27 CN). Task shapes are cited from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11). Org model: the S44-extra component tree (`rootId` = a root component).

## 1. Why

A loop that runs without a person watching must be **stoppable** and **bounded**. Today it is neither in the way an operator needs:

- **No loop-level stop, cancel, resume or raise.** The loop methods are `loop.create|list|write|read|start|show|files|file.read|answer|close|archive|message|run|run.options|retry_handler_block|transcript` (`internal/protocol/method.go:135-170,224`); `loop.close` only claims an *idle* loop (`store/runs.go:186-196`). `step.stop`/`step.continue` exist (S31 D6, `protocol/step_stop.go:67,112`) and already park a loop `waiting` with `LastReason: "stopped"` (`loop/advance.go:285-302`), but they are step-addressed, have **no CLI verb** (`cmd/aeon` has none), and reach only a step that is running *right now*. A loop between steps, blocked, or `waiting` cannot be cancelled by anyone.
- **No spend, token or wall-clock ceiling across a loop, component or epic.** What exists is per-step and per-fire: `MaxIterations = 12` and `DefaultDeadline = 30 min` per step (`runtime/limits.go:41,55`), `BashTimeout = 600 s`, the blueprint reject counters and `LoopCeiling = 10` (`loop/counters.go`), and the automation trigger's `max_runs_per_hour` / `max_cost_per_day_usd` checked **only at fire time** (`automation/dispatcher.go:141-180`). Nothing enforces cost once a loop is running. `epic.budget_usd` is stored and returned but read by nothing (`store/epics.go:22,36,51`). The S10 policy type `cost_cap` is a stub whose evaluator answers "could not determine" (`policy/spec_stubs.go:93-96,245`).
- **An escalation question that nobody can answer.** `loop.AnswerEscalation` (`loop/escalate.go:317-361`) exists with no socket or daemon caller; `AnswerBlueprintLoop` routes only to `AnswerActionAsk` or `AnswerGate` (`daemon/blueprint.go:332-380`). An attended loop that exhausts a reject cap asks `continue|abort` and strands.
- **A daemon shutdown leaves loop work un-cancelled.** Every loop runner is started with `context.Background()` (`daemon/start_loop.go:122`, `runs.go:202`, `blueprint.go:287,308,375`); at SIGTERM only the *recovery* context is cancelled (`cmd/aeon/serve.go:355-374`), so steps and their bash process groups are simply abandoned when the process exits, to be recovered at next start.
- **Nothing notices a loop that has stopped making progress.**

S27 (continuous work) starts loops nobody asked for at that moment. It cannot ship until a person can stop what it started and a ceiling holds while it runs (S27 brief §5, Q5: *"nothing in this brief can finish without an answer"*).

## 2. What this slice delivers

1. **Stop, resume, cancel, raise** — daemon-first socket methods on the human door, a CLI client, an eligibility block on `loop.show` that the app renders as given. **No new loop status**: stop parks `waiting`/`stopped` (existing), cancel ends `aborted` with run state `cancelled` (existing), a cap hit parks `blocked` with reason kind `cap-hit` (new *reason kind*, existing status).
2. **The escalation door wired** — `loop.answer` routes an exhaustion-escalation question to `AnswerEscalation`.
3. **Budgets as records** — a `budget` table (scope `workspace|component|epic|loop`; USD, tokens, wall-clock, steps; window `lifetime|day`), enforced at the step boundary **and** before each model call, honouring cost state known/partial/unknown. `epic.budget_usd` adopted. Reads: spend vs cap per scope, spend over time and per trigger.
4. **Cap-hit semantics** that respect G14: an unattended loop has no questions, so a cap hit is `blocked`, resumable only by a person through `loop.raise` (raise the cap, resume the work).
5. **A watchdog** — records (`watchdog_rule`, a seeded default set) that turn "running but not progressing" into a notification, a stop (attended) or a block (unattended).
6. **A graceful daemon drain** — runners get a cancellable context; at shutdown steps are left at their last checkpoint and bash process groups are terminated, never orphaned.
7. **Agent tools (rule 2):** list-with-details reads of loop control state, budgets, spend and watchdog rules, plus **one** write, `loop_request_stop`, which records a *request* a person acts on. No agent tool stops, cancels, resumes, raises or edits a cap.

## 3. Out of scope

- The scheduler (S27 CN), autonomy grants (S56 AL), verification gates (S57 VG), notification delivery (S58 SV), execution environments (S59 EV).
- **Automatic retry of a blocked loop.** S27's brief §8 explicitly refuses "restart a blocked item on a timer"; this slice ships detection and escalation, never a timer-driven re-dispatch.
- Calendar-month budget windows; per-person budgets; price lookup (route prices stay as S3/S18 define them).
- The UI (a hook is specified; the screens come after).

## 4. Acceptance (re-run fresh at close-out)

1. A person can stop a running loop (mid-step and between steps), resume it, and cancel it from any non-terminal state; each is a socket method, a CLI verb and (read-only) an agent tool twin; **no existing loop-status test changes**.
2. A step is never recorded as *failed* because the person stopped it or the daemon shut down.
3. A cap on any scope halts a loop **before** the next model call that would breach it, and the loop's `blocked`/`cap-hit` reason names the budget, scope, dimension, spend floor and cap; `loop.raise` raises and resumes with no lost work.
4. An unknown-cost step never reads as zero (P31); the rule for it is stated per budget and tested.
5. `epic.budget_usd` is enforced; `max_cost_per_day_usd` is unchanged; no two stored truths.
6. The exhaustion escalation question is answerable over the socket.
7. The watchdog stops/blocks a loop with no progress per its rules and never touches a loop that is legitimately waiting on a person.
8. Daemon SIGTERM mid-step leaves no orphan process and the loop recovers at next start.
9. Guards (P4) hold: every authority-changing method is absent from the narrow surface, agent services, tool registry and records catalog (except the named read twins and `loop_request_stop`); canaries fail when fed violations.
10. `make protogen && make check-protogen && make guard` green; migration drills pass.

## 5. Dependencies

**S44-extra** (the tree: `rootId`, effective settings), **S43-extra** (the seed engine, for the watchdog set), S31/S39 (the stop machinery and run states), S33 (telemetry rollups). Not S45. Feeds **S27** (hard prerequisite), **S56**, **S58**.

## 6. Status

Built 2026-11-02 (branch `s55-loop-control`, 13/13 tasks). Loop stop/resume/cancel as socket doors + CLI verbs + agent tool twins; budgets (`budget` records, the four scopes, the P31 floor discipline, epic adoption with the column dead and grep-pinned) enforced at the pre-dispatch boundary, the top of the run, and before a step row, with `loop.raise` the one human door off a cap-hit; the watchdog (rules as records, the seeded inert five, running-loops-only, LC17 attendedness, once-per-episode) with its six doors; the graceful drain (LC19) and the escalation door wired (LC6). Close-out (T12): acceptance re-run fresh 10/10, the migration drills, the real-daemon walk (transcript in T12 Notes), the full suite with every red accounted. What remains for the slices this feeds: S27's UI over the control plane (the hard prerequisite is now met), S56's autonomy grants over control acts, S58's delivery surface for `notify` and escalations.
