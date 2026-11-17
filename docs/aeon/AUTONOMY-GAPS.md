# Autonomy gaps: what still stands between Aeon and running a project on its own

Written 2026-10-30, after S43–S54 were planned. This is a **parking document**: it records the gap analysis so it is not
lost while other work goes first. It is not a slice, not a design and not a commitment to order. Every row here becomes
a slice (or a task inside an existing slice) only when it is picked up, and at that point it gets its own T0
investigation (MANAGEMENT-PATTERNS P1) — the facts below were true on the date above and must be re-walked then.

## 1. The question

> From everything built so far and everything planned in S43–S54, what is missing — in the app and the daemon — for a
> fully managed, potentially fully autonomous agentic development of an app?

## 1a. Status: every gap is now planned (2026-10-30)

Each gap below has a slice with a brief, design, plan and tasks. Nothing is built.

| Gap | Slice | Rules / tasks |
| --- | --- | --- |
| §4 S27 Continuous | [S27-continuous](S27-continuous/) | CN1–CN26 · T0–T12 |
| G1 Loop control, caps; G8 watchdog | [S55-loop-control](S55-loop-control/) | LC1–LC24 · T0–T12 |
| G2 Autonomy policy | [S56-autonomy-policy](S56-autonomy-policy/) | AL1–AL17 · T0–T11 |
| G3 Verification gates; G8 failures to triage | [S57-verification-gates](S57-verification-gates/) | VG1–VG24 · T0–T12 |
| G7 Supervision | [S58-supervision](S58-supervision/) | SV1–SV18 · T0–T10 |
| G5 Execution environment | [S59-execution-environment](S59-execution-environment/) | EV1–EV22 · T0–T12 |
| G4 Integration | [S60-merge-queue](S60-merge-queue/) | MQ1–MQ18 · T0–T12 |
| G6 Goal intake | [S61-goal-intake](S61-goal-intake/) | GI1–GI20 · T0–T12 |
| G10 Backup and restore | [S62-backup-restore](S62-backup-restore/) | BK1–BK18 · T0–T11 |
| G9 Outcome learning | [S63-outcome-learning](S63-outcome-learning/) | RT1–RT16 · T0–T9 |

Not planned: G11 (release) and G12 (teams, other forges), both post-alpha. S32 T8/T9, S26's acceptance gaps and the S41 number conflict are
prerequisites or owner decisions, not slices (see §9).

## 1b. Corrections found while planning

The first version of this document was written from a quick grep. The slice investigations read the code and changed these statements:

| Original claim | What the code says | Where it lands |
| --- | --- | --- |
| G1: no hard caps | Per-step caps exist (`MaxIterations` 12, `DefaultDeadline` 30 min, bash 600 s), automation has per-fire caps, S17 `LoopCeiling` 10. Missing: a cap enforced *while a loop runs*. The S10 `cost_cap` policy type is an inert stub (`internal/policy/spec_stubs.go:93-96,245`); `epic.budget_usd` is stored, never read. | S55 |
| G1: no stop | `step.stop` exists and already parks the loop `waiting` with reason `stopped`; only loop-level cancel is missing. `AnswerEscalation` has no caller. Daemon shutdown does not cancel loop runners (they use `context.Background()`). | S55 |
| G3: forge cannot read CI checks | A `Check` type and optional `StatusLister` exist and `mergeGate` already requires an approving review, but neither GitHub nor the Fake implements `ListStatuses`, so CI is never read. `forge.postReview` cannot run from a blueprint (required args never filled), so unattended merge cannot get a review today. Nothing records a merge. **Delivered 2026-11-03 by S57** — `ReadChecks`/`ListStatuses` on GitHub and the Fake (T2), the merge-rule floor and the `merge_event` record (T5), gates + DoD + the post-merge watch (T3–T7). | S57 |
| G4: nothing orders landings | Nothing lands a feature branch on the default branch at all (`dev-loop` ends at the feature branch; `forge.merge` refuses any base but `feature/<loop>`). `default_branch`/`branch_prefix` are read by nothing. **Delivered 2026-11-04 by S60** — the per-repo queue (`mergequeue.*`, one landing slot per base branch held by CAS, positions honoured), branch settings finally read (pinned per loop at start, T4), land as a loop lane behind S57's floor and S56's grant. | S60 |
| G4: rebase as a step | A rebase of a pushed branch needs a force-push, which is banned (`TestNoForcePushAnywhere`). Replaced by merge-forward onto a new `integrate/…` branch. **Delivered 2026-11-04 by S60** as shipped law, not a fallback: catch-up MERGES onto `integrate/mq-<item>` refs (never a rebase, never a force-push — `TestNoForcePushAnywhere` re-run unedited at T12; stale refs listed, never deleted, N9). | S60 |
| G5: commands run on the host | Worse: bash and daemon-side git inherit the daemon's whole environment (`cmd.Env = nil`, `internal/tools/spawn.go`), so provider keys and the forge token are visible to model-run commands. | S59 |
| G5 / S32 | S32 parallel units create a worktree but the step runs in the shared root (`WorkspaceRoot = st.Root`; the worktree path is used only for removal). A correctness finding for S32's owner; S59 T0 and S60 T0 each prove it. **S60 T5 shipped the missing seam additively (2026-11-04)** — the `WorkDir` override honoured by `runOneUnit`, gated on the git seam — the route S60's brief §5 declared; S32's close-out now adopts it as a free closure. | S32 owner |
| G7: no notifications | **Built as S58 (2026-11-04, 11/11)**: a live inbox (`aeon inbox`/`inbox.*` — the S44 registry + questions/blocked/stopped reporting, budget/stuck/gates/scheduler labelled not-wired) with ack/snooze; outbound **channels** as human-door authority records (closed envelope, HMAC, SSRF-guarded, rate ceiling + hourly summary, digest at start); a **wider classified event set** (`notify.events` seed, classes auditable); **background presence** — tray, close-to-tray, `aeon://` deep links (written; the Rust shell is unbuilt and unverified on the Linux dev box — hand-off, S58 T7/T10). Remains: **remote answering** (needs the S41/S27 remote human door — the narrow surface still cannot answer, N1 untouched), **SMTP/email channels** (webhook only; a second `kind` is a design addition), **autostart** (launch-at-login is OS work, not built), and the OS-notification walk on real hardware. | S58 |
| G10: restore is a daemon method; backups run as an automation | Restore is CLI-only and daemon-down; scheduled backups are a daemon task, not a trigger (S40 AU1). Backups hold `credentials_included:false` but are `confidential:true` (transcripts can hold secrets). | S62 |
| G9: scorecards from history alone | **Partial-built as S63 (2026-11-05, 9/10)**: the recorder writes its own facts at the terminal event (history never had rework counts — S63 records them), the milestone writer files template learnings under `observed`, scorecards and the learned digest are derived reads, and RT3 (a score changes nothing) is guarded. Remains: T8's model retrospective, blocked on the `retrospective` blueprint and `retrospective-author` seeds, which belong to S48/S55/S44/S52. | S63 |
| G6: `aeon init` exists | Documented and implemented in `internal/presets`, but no verb is wired in `cmd/aeon/main.go`. Handed to S23's owner, not S61. | S23 owner |
| S27 | `roadmap_dependency` has no table; P28's global cap has no code; roadmap items have no blueprint or goal; S32's `max_parallel_agents` is intra-loop, not a loop cap. | S27 |

## 2. Verdict

After S54, Aeon is **manageable**: every governing record is a row, seeded where code-defined, editable through the
daemon first and the CLI second, visible to agents through list-with-details tools, and audited. That is the product
base the configuration order set out to build.

It is **not yet autonomous**. VISION.md §7 says it plainly — *"Right now Aeon runs a loop. With Stage 5 it runs a
project."* — and the piece that runs a project (S27 Continuous) is still brief-only. Beyond S27, there is a layer no
slice covers at all: the controls that make it **safe to leave running** (stop, caps, an autonomy level), the gates that
make its output **trustworthy without a person reading every diff** (verification, merge-on-green), and the plumbing
that lets a person **supervise from a distance** (notifications, an inbox, remote access).

The minimum path to autonomous delivery is short: finish S26 and S32, then **loop control & budgets → autonomy policy →
verification gates → S27**. Without the first three, S27 is either unsafe (no kill switch, no ceiling) or permanently
blocked by G14 (unattended loops may not act outward).

## 3. What is already true (the base the gaps sit on)

Checked against the tree on 2026-10-30; file references are where the fact was read.

| Capability | State | Where |
| --- | --- | --- |
| Suspend / resume from rows, crash recovery | Built (S6, S7) | `internal/loop` |
| Blueprints as data, pinned per loop | Built (S8); managed as records in S48 (planned) | `internal/blueprint` |
| Policy, permissions, deny-wins | Built (S10, S11); managed in S45 (planned) | `internal/policy` |
| Agents, A2A, plugins | Built (S13–S15, S21, S43) | `internal/agentsvc`, `internal/agentmgr` |
| Git worktrees per loop | Built | `internal/git/worktree.go`, `internal/loop/reconcile_worktree.go` |
| Forge: issues, PRs, reviews, **merge** | Built (S16) — `OpenPR`, `ReadPR`, `ListReviews`, `PostReview`, `MergePR` | `internal/forge/forge.go`, `github.go` |
| Triggers (clock, webhook, narrow surface) | Built (S19, S20); managed in S49 (planned) | `internal/trigger`, `internal/narrow` |
| Unattended-by-construction (G14) | Built: a triggered loop is pinned `attended=0`; unattended outward/irreversible effects are **blocked at once** | `internal/loop/execute.go:13-14,90` |
| Per-loop outward opt-in | Built (S19 T6): `allowOutward` is declared at `loop.start`, per loop, default deny; it **does not** unlock `irreversible` (`forge.merge` stays blocked unattended) | migration 0031, `internal/narrow/trigger.go` |
| Queue, history, roadmap, memory, triage, code graph | Built: S24, S25, S28, S29, S30 (with stated gaps); S26 built with acceptance gaps | `internal/queue`, `history`, `roadmap`, `learnings`, `triage`, `graph` |
| Parallel agents | Partial (S32 7/9) | `internal/loop/advance_parallel.go` |
| Telemetry | Built (S33) | — |
| Epic budget field | **Stored only**: `BudgetUSD` is written and read by `internal/store/epics.go` and passed through `internal/daemon/records.go:263-283`; nothing reads it to stop work | `internal/store/epics.go:22` |
| Project records | `project.create/update/list/show` exist today; **S44-extra (planned, next) folds projects into a two-level `component` tree** — read "project" in this document as "root component" once it lands | `internal/protocol/method.go:183-186`; [`S44-extra-component-tree/`](S44-extra-component-tree/) |

## 4. Gaps inside slices that are already specced (finish these)

| Slice | Gap | Why it matters for autonomy |
| --- | --- | --- |
| **S27 Continuous** | Brief only, 0 tasks. Its own brief names two blockers: **no loop-level stop/cancel exists** (`S27-continuous/brief.md:80`, Q5 "blocking") and `roadmap_item` has no `attended` column. Q6 rules the scheduler's controls socket-first (P34). | This *is* "runs a project". Everything else in this document either feeds it or makes it safe. |
| S26 Roadmap | Built with acceptance gaps stated in SLICES.md. | S27 schedules from S26's "ready" (D4). |
| S32 Parallel agents | 7/9: T8 partial, T9 not built. | Autonomous throughput is parallel; the gate/failure/cap-drain path (T7) is built, close-out is not. |
| S28 Memory | Recorded gaps: board resume wiring, SkillAccess grant. | Learning across loops (§5 G9) rides this. |
| S30 Code graph | Recorded gaps: loop-write automation, native click-through. | Agents need a fresh graph after their own writes. |
| S31 | Partial 10/11: T11 fixture migration unexecuted. | Hygiene; not on the autonomy path. |
| S23 First run | 8/9: T9 human timed measurement not met. | Onboarding; ties to §5 G6. |
| S41 | **Conflicting ID**: `S41-workspaces` (remote workspace listener, design only) vs. older ROADMAP use of S41 for approval/spawn. Neither built. | Remote supervision (§5 G7) and approval policy (§5 G2) both claim this number — resolve before either is planned. |

## 5. Gaps no slice covers

Each gap lists what is missing, the evidence, and the shape a slice would take. Proposed slice names (S55+) are
**placeholders**, not reserved numbers.

### G1 — Loop control and hard caps (proposed S55) · *blocks S27*

**Missing.** A way to stop a loop: there is no `loop.cancel`/`loop.stop` socket method (grep of `internal/protocol` and
`cmd/aeon`, 2026-10-30; S27 brief confirms). No hard ceiling that halts a loop or a project: spend (USD), tokens,
wall-clock, step count, retries. The epic `BudgetUSD` is a stored number nothing enforces.

**Shape.** `loop.stop` (graceful: finish or abandon the current step, record why) and a human-door `loop.kill`;
cancellation is a recorded state, never a process kill (the "nothing spawns" guarantee holds — a stop is a row, the
next advance sees it). Caps as **records** (rule 6): per-loop defaults, per-project and per-epic budgets, read at the
step boundary beside attendedness, failing toward stop. A cap hit suspends with a question rather than failing, so a
person can raise it. Cost "unknown" (P31) must count as a cap risk, not as zero. Agent tools: read caps and spend
(list-with-details); no tool raises a cap.

**Status (S55, built 2026-11-02).** Closed, daemon-side: `loop.stop`/`resume`/`cancel` as socket doors, CLI verbs and
read twins + the inert `loop_request_stop`; cancellation a recorded state through the engine's one writer (run
`cancelled`, no `on_abort` lane); budgets as records in the four scopes, enforced at the pre-dispatch boundary, the
top of the run and before a step row, the cap-hit `blocked` naming budget/scope/dimension/floor/cap with
`loop.raise` the one human door (unattended: no question row — G14 holds); unknown cost a floor, never zero (P31);
`epic.budget_usd` enforced by the adopted budget row with the column unread (grep-pinned); the graceful drain
(steps left at their checkpoint, no orphans). Shaped differently from the 2026-10-30 sketch, on purpose: the cap hit
does not suspend with a question — it parks `blocked`, because an unattended loop has no question to receive (G14),
and the "kill" is `cancel` through the same one-writer path as a stop. What remains: the UI hooks (S27 — its hard
prerequisite is now met), the autonomy grants over control acts (S56), the delivery surface for escalations (S58).

### G2 — Autonomy level per project (proposed S56) · *blocks S27 doing anything outward*

**Built — S56, 2026-11-02 (12/12; `SLICES.md` S56 row).** What closed: human-authored per-root-component grant/deny
records with closed conditions, expiry and an append-only change history; `forge.merge` as its own grant, never implied
by `outward`, forced merge-rule condition + mandatory expiry (AG1 answered: supported, default off, separately granted,
only when S57's rule passes); pinned ceiling + live floor (a widening never reaches a running loop, a revoke/veto/expiry
is honoured at the next outward action); append-only use log with grant id and revision; socket human door + read-only
agent tools with two restrictive propose twins (no `allow` is agent-creatable, store CHECK included) + the `aeon
autonomy` CLI; `autonomy.effective`/`autonomy.pinned` legibility. What remains for the gap to be fully closed: **S55's
`budget.Exhausted` seam (R1) and S57's `verify.MergeRule` seam (R2) are still nil in production** — while they are
unwired the conditions answer cannot-say and a merge grant is inert (grantable, pinnable, auditable, never runnable);
S55's `loop.stop` (R3) is what makes a revoke's `affectedLoops` actionable. The S41 ID conflict named below was mooted
when this slice took S56 — both sides of the conflict are unbuilt and §4's row stands as written.

**Missing (pre-build, for the record).** Today autonomy is a per-loop declaration at start (`allowOutward`), and
`irreversible` (merge) is never allowed unattended. A continuously scheduled project has nobody at start time to declare
anything, so S27 loops would all be unattended-no-outward: they could build and commit locally, never push, open a PR or
merge.

**Shape.** A human-authored, per-project **autonomy policy** record: which effect classes an unattended loop on this
project may perform (push / open PR / comment / merge), under which conditions (G3's gates green, caps not hit, item
labelled), with expiry. Socket human door only (the S45 PM1 shape: absent from narrow, agent services, tool registry,
records catalog except a propose-only restrictive twin). Deny still wins; the S19 T6 guard that `allowOutward` never
touches `irreversible` stays true — merge needs its own explicit grant, not a widened flag. Every effect taken under
the policy is audited with the policy revision that allowed it. Resolve the S41 ID conflict first (§4).

### G3 — Verification gates and merge-on-green (proposed S57) · *blocks trustworthy unattended merge*

> **Delivered 2026-11-03 by S57 — built 13/13, per-task merges to main; `SLICES.md` carries the row, `tasks/T12.md` the
> evidence.** The two paragraphs below are the gap as proposed, kept as written, with two facts corrected by S57 T0's re-walk
> (the inventory table above already carries the corrected pair): the `Check` type and the optional `StatusLister` **did**
> exist in `forge.go` — what was missing was any implementation reading CI on GitHub or the Fake — and `forge.postReview`
> was unrunnable from a blueprint (required args never filled), so unattended merge could not obtain a review. What shipped:
> `ReadChecks` + `ListStatuses` on both implementations (closed four-state vocabulary, T2); `verify.run` gates whose
> pass/fail/unverified routes the loop (T3–T6); the per-component merge-rule floor over live facts, which is what S56's merge
> grant waits on (T5); the `merge_event` record, one per `forge.merge` (T5); and the post-merge watch that files the deduped
> triage bug — G8's filing half. The proposed automatic revert PR was deliberately **not** built: red files a bug, and
> reverting stays a decision a person or a loop makes.

**Missing.** `internal/forge` can open, read, review and merge a PR but **cannot read CI check status** (no checks /
statuses method in `forge.go`). There is no blueprint-level "run the tests/build/lint in the worktree and gate on the
result" step as a first-class, declared gate. No post-merge health check of the main branch, no automatic revert.

**Shape.** (a) A local verification step kind: declared commands from the project record, run in the loop's worktree
under bash policy, output and exit code recorded as evidence. (b) `forge.ReadChecks(pr)` with an honest "unknown" state.
(c) A merge rule on the project: required local gates + required CI checks + required review verdicts (Aeon's own
review agent counts only if the policy says so). (d) Post-merge watch: main's checks red ⇒ open a revert PR (outward,
so under G2) and file a triage item (S29). (e) Definition of done per roadmap item: acceptance criteria that become
gate inputs, not prose.

### G4 — Integration of parallel work (proposed S60)

**Missing.** S32 fans out branches; nothing serialises their landing. No merge queue, no rebase-on-main, no conflict
resolution path, no "my branch is stale" detection.

**Shape.** A per-repo merge queue (rows, ordered, one landing at a time); rebase as a recorded step; a conflict becomes
a suspended question or a dedicated resolution loop on a named blueprint; re-run G3's gates after every rebase.

**Built 2026-11-04 (S60, 13/13).** One planned shape was corrected at design time (MQ6) and shipped so: **not** "rebase
as a recorded step" — a rebase of a pushed branch needs the banned force-push — but **merge-forward**: catch-up cuts an
`integrate/mq-<item>` ref at the base tip and *merges* the branch; verification re-runs bound to that integration sha,
and the conflict becomes an attended question (paths and counts only, SEC-033) or the resolution loop, as shaped.
Queue + slot, settings adoption, the human door, the inert agent proposal, and the walked close-out: [S60](S60-merge-queue/).

### G5 — Execution environment for the app being built (proposed S59)

**Missing.** Today commands run on the host, governed by the bash policy corpus. Building and testing a real app also
needs: isolated runtimes (containers or VMs — Parallels Desktop VMs are a natural fit, with snapshot/restore per loop),
service dependencies (a database, a queue), the **app's own** secrets injected into the environment without ever being
visible to the model or written to a transcript, and preview deployments.

**Shape.** An environment record per project (image/VM template, services, ports, secret *references*), provisioned per
loop or per worktree, torn down by the daemon (rows, not processes it forgets). Secrets resolved at the edge from the
existing secrets store, never in params, logs, audit summaries or tool results (org rule; C17). Network egress policy
per environment.

**Amended 2026-11-04 — S59 built the container half of this shape; the VM half and preview deployments are still missing.**
What exists: an environment *record* per component (image, cpu/mem caps, user, network mode, egress policy, pull policy,
provision mode) authored through 13 `environment.*` human-door methods, secret **references** — never values — through 7
`secretref.*` methods (`sourceKind` + `source` + `injectAs`; `secretref.check` answers two booleans and reads the value only
to decide resolvability), provisioning per loop through a ledger the daemon owns (`provisioning → ready → tearing_down →
removed`, reconciled by label so a restart cannot orphan an engine object), egress policy enforced by the engine
(`networkMode=none` measured: `curl: (6) Could not resolve host`), and every spawn under a scrubbed environment — the
daemon's own env never reaches a step. What is still **missing** exactly as written above: VMs/Parallels snapshot-restore
(the backend is docker CLI only, and `container` is the only non-`host` backend that provisions), service dependencies
that reach `ready` (services are creatable but W2-5's handle bug means none has been healthy through the door), and
**preview deployments**, which S59's spec excludes outright. The panel exists as a Settings tab tested by vitest but has
never been walked against a live daemon, and the idle-TTL sweep has never been observed running. See
[`S59-execution-environment/tasks/T12.md`](S59-execution-environment/tasks/T12.md).

### G6 — Goal intake: from "build app X" to a project (proposed S61)

**Missing.** S26 ideation is a chat with tools; nothing turns a goal into a project. `project.create` exists, but there
is no charter, no acceptance criteria as data, no repository bootstrap (create repo, scaffold, CI, branch protection),
no first roadmap generated from the charter.

**Shape.** A charter record (goal, constraints, non-goals, acceptance criteria, stack, budget, autonomy level) authored
by a person with agent help; an intake blueprint that proposes repo scaffold + first roadmap as **proposals** a person
commits (S35/S45 desk); acceptance criteria flow into G3's definition of done. App: a new-project wizard on top.

### G7 — Supervision from a distance (built as S58, 2026-11-04)

**Built.** The shape below is what shipped, with two honest tails: the desktop shell (tray / close-to-tray /
deep link / OS notification on window-hidden) is written but **never compiled on this machine** (no Rust
toolchain — S58 T7 and T10 record it as the standing hand-off), and remote answering stayed unbuilt exactly as
the paragraph below predicted it must (it needs the remote human door, not the narrow surface; email/SMTP and
autostart likewise remain).

**Missing.** A loop that suspends with a question waits silently until someone opens the app. No notifications (ask,
blocked, cap hit, gate failed, merged), no unified inbox of asks across projects, no digest, no remote access (S41's
remote listener is design-only).

**Shape.** Notification channels as records (desktop first; email/chat later), subscription rules per project and
event class, an inbox view over pending questions + blocked loops + failed gates. Answering remains a person-only act
(the S19 T5 rule: the narrow surface reads questions, never answers them) — remote answering needs the remote human
door, not the narrow surface.

### G8 — Failure handling and escalation

**Missing.** No declared retry policy per step kind, no stuck-loop detection (no progress for N steps or T time), no
escalation ladder (retry → different agent → ask a person → abandon), no automatic triage of failures.

**Shape.** Retry/escalation as blueprint-level records; a daemon watchdog that turns "no progress" into a suspended
question; failed gates and post-merge reds filed into S29 with evidence attached (triage validates, roadmap schedules —
the existing S29 → S26 hand-off).

**Status (S55, built 2026-11-02; the rest stays open).** Closed, daemon-side: the watchdog — rules as records
(`watchdog_rule` + append-only `watchdog_event`), the seeded inert five, the running-loops-only service (a loop
legitimately waiting on a person is untouched by construction), LC17 attendedness (attended ⇒ `stop`, unattended ⇒
`block stuck`, once per episode), the four no-progress signals, and the six doors. The escalation question itself was
already the S55-wired `loop.answer` door (LC6). What remains, still nobody's: per-step-kind retry policies as
blueprint records; failed gates and post-merge reds filed into S29 (S57's job, with the S29 → S26 hand-off); the
delivery of `notify`-arm and escalation events to a person (S58).

### G9 — Learning from outcomes

**Missing.** Memory (S28) is written deliberately; nothing writes a retrospective after a loop or an item lands. No
per-agent or per-blueprint quality signal (gate pass rate, rework rate, cost per item) derived from history.

**Shape.** A retrospective step at item close writing to S28 with provenance; scorecards computed from S25/S53 history
and S33 telemetry, shown in the app and readable by agents (list-with-details); used by a person to choose agents and
blueprints, never self-applied.

**Partial-built 2026-11-05 (S63, 9/10)** — with the deviations recorded at close-out. The retrospective step is **not**
a step at item close: the recorder is a daemon reactor writing one outcome row per terminal event (`loop.finished`
and friends), because a step could only learn about its own loop and history carries no rework, gate or per-agent
facts (T0's finding). The learnings are the shape the design settled: a milestone writer (`n = 1, 3, 10, 30 …`)
filing S28 learnings with `observed` provenance from **one template** — enum-and-number bodies, no model prose, no
loop free text anywhere in the fact plane (walked with a hostile goal planted). Scorecards are computed from S63's
own fact tables (`retro_outcome`/`retro_agent`, append-only by trigger) **composed with** S25/S53 history and S33
telemetry where those carry the fact, and every state word — `too-few`, `cannot say`, coverage-first — is the
daemon's answer, never a client-side recomputation; the same services answer the person's socket doors and the
agent's two tools (`scorecard_mine`, `digest_learned` — subject from the step, no parameter). "Never self-applied"
is RT3 and it is structural, not aspirational: the scorecard package is unreachable from dispatch/engine imports
(`TestScorecardNeverReachedByDispatch`, scratch-import canary walked), zero settings or grants derive from a score,
and the sentence — "A score is a measurement of what happened…" — exists once on the wire and is quoted, never
restated. **Remains (T8, unbuilt by choice):** the model retrospective — a `retrospective` blueprint and a
`retrospective-author` agent, neither of which exists in any seed S63 may own (S48/S55/S44/S52 own the seeds) —
so `retro.status` answers `not enabled` and the digest shows only facts. Evidence: `aeon/scripts/qa-s63-outcome.sh`
rc=0 (2026-11-05), [S63 brief §7](S63-outcome-learning/brief.md).

### G10 — Database backup and restore (proposed S62) · *raised by rule 6*

**Missing.** CONFIGURATION-ORDER rule 6 makes the database the single source of truth for every record (agents,
policies, blueprints, MCP servers, automations). Losing it loses the whole configuration and history. No export,
scheduled backup, restore or integrity check is planned.

**Shape.** `store.backup` / `store.restore` human-door methods (online SQLite backup API), scheduled backups as an
automation, a verified restore drill in the close-out pattern (P9), export of managed records to a reviewable format.

**Built 2026-11-03 (S62, 12/12)** — with two deliberate deviations from the shape, both recorded at close-out:
the human-door methods are `backup.*` (`status · list · run · verify · prune`, beside `export.run` / `export.sets`),
not `store.backup` / `store.restore` — and **restore is not a method at all**: it is a CLI direct-door command
(`aeon backup restore FILE [--yes]`), deliberately kept off every socket method, tool and catalog spec. The
scheduled backup is a **daemon maintenance task, not an automation** (the ticker wakes at start then hourly, and
the interval is the due threshold, not the cadence — a week of downtime is one catch-up). The pre-migration copy
is gated in `store.Open` itself, so no door can bypass it, and the verified restore drill ran at close-out (T11:
the round trip plus all five refusals, with the replaced set kept). The export is a deterministic `aeon export`
whose sets report *unavailable*, never empty. See the S62 brief §6 and T11's Notes.

### G11 — Release (optional for the alpha)

Tagging, changelog from history, release notes, deploy pipelines. All outward/irreversible — only ever under G2.

### G12 — Teams and other forges (post-alpha)

`internal/forge` has one real implementation (GitHub). Multi-user roles, per-person attribution beyond the local peer,
and shared daemons are outside the current single-operator model; record, do not plan yet.

## 6. App (UI) gaps

| Gap | Depends on |
| --- | --- |
| Project "mission control": running / blocked / waiting-on-you / spend vs cap / next ready item | G1, G7, S27 |
| New-project wizard from a goal | G6 |
| Unified inbox of questions, blocked loops and failed gates | G7 |
| Autonomy controls per project, with the daemon's own confirmation statement (P7 `useConfirm`) | G2 |
| Cost and cap dashboard over S33 telemetry | G1 |
| Gate evidence view on an item / PR (local gates, CI checks, reviews) | G3 |

## 7. Recommended order

```text
S44 (in flight) -> S44-extra -> S43-extra -> S45 ... S54       configuration as managed records
S62 backup & restore                (small; only needs the store: pull it forward, any time)
S55 loop control, budgets, watchdog (safe to leave running)
S57 verification gates              (trustworthy without reading every diff)
S59 execution environment           (host-scrubbed first: closes the env-inheritance hole; independent of S55/S57)
S56 autonomy policy                 (allowed to act outward, by a person's rule; needs S55 + S57 seams)
S60 merge queue                     (needs S32 closed, S57, S56)
S27 continuous                      (runs a project; needs S24, S26, S32, S55, S56, S57)
S58 supervision / inbox             (can start after S55; its attention sources fill in as S55/S57/S27 land)
S61 goal intake                     (needs S45, S48, S44-extra; stage forge.createRepo last)
S63 outcome learning                (last: reads what the others record)
```

Prerequisites that are not slices: close S32 (T8/T9), resolve S26's acceptance gaps, fix S32's worktree-workspace finding, decide the S41 number conflict.
The S59 environment scrub is a **security fix**, not an autonomy feature, and could be built right after S44-extra.

## 8. Open questions: how the slices answered them

| # | Question | Answer in the plans (a default; the owner can change it) |
| --- | --- | --- |
| AG1 | Is merge ever allowed unattended? | Supported, default off. A separate explicit `merge` grant (S56), expiry at most 90 days, honoured only when S57's merge rule passes; never implied by an outward grant. |
| AG2 | Does a cap hit suspend with a question or fail? | The loop parks `blocked` with reason `cap-hit`, resumable only by the human-door `loop.raise` (S55). Unattended loops have no questions (G14). |
| AG3 | Execution environment backend? | `host-scrubbed` first (explicit env allowlist, secrets by reference, redaction); `container` staged behind the same record; VM later (S59). |
| AG4 | Does Aeon's own review count for merge? | Only if the per-component merge rule says so (S57). |
| AG5 | Which S41 wins the number? | Not decided by any slice; still an owner decision (§9). |
| AG6 | Pull backup forward? | Yes: S62 is small and independent, build it early. |

## 9. Items for an owner, not slices

1. **S41 ID conflict** (remote workspaces vs approval/spawn) and the remote human door that remote answering (S58) waits for.
2. **S32** T8/T9, and the finding that parallel units run in the shared root rather than their worktree.
3. **S26** acceptance gaps.
4. **`aeon init`** is implemented but not wired into the CLI (S23's owner).
5. **Reading `internal/secrets`**: the planning sandbox could not read it; S58 T0, S59 T0 and S62 T0 each need an owner-approved read of its exported API.
6. **S56 workspace-scope grant**: S61 needs a grant for `forge.createRepo` that has no root component yet, but S56 grants are per root component. One of them has to give.
