# Plan — S40 Automation

## What is settled before planning

`brief.md` §0 (the author's rulings, including the §0.1 interpretation) and `design.md` AU1–AU16: triggers are human-authored standing
authority (AU1); "run whatever" = any blueprint, still unattended (AU2); closed event set for goals and manual tasks (AU3); a
transactional outbox (AU4); a depth guard (AU5); a closed match grammar (AU6); two event tables beside S20's, reusing its provenance columns (AU7); one dispatcher with
a cursor and a claim-before-start (AU8); `StartFromEvent` beside S20's `StartFromTrigger` (AU9); the kickoff precedence (AU10); honest limits (AU11); five outcomes
(AU12); a reconcile table (AU13). **Not settled, by design:** the exact names and transaction shapes of S38's store functions, S39's
`source`/kickoff plumbing, the peer-actor helper, the migration number, the `completed` status constants — **T0 reads the real
code and writes them down; no implementation task invents them.**

## How we will do it

**Investigation first (T0), then strictly sequential** (the standing no-parallel-tasks direction of S34–S39). T0 produces **no
code**: it writes `investigation.md` here, each question closed by a quoted command. Implementation tasks T1–T9 are
**mechanical**: each names its files, signatures, SQL, test names and a definition of done, and each begins with a one-line
prerequisite check. Every task lands with its suites green and the measured numbers in its Notes the day they are measured
(AGENTS §4a).

**Prerequisites from other slices (must be built, not planned):** S38 T6 (goal/entity store functions incl. `DecideGoal`), S38 T7
(task store with `manual`, `type`, `approval_state`), S38 T15 (socket surface and the actor stamp), S39 T8 (kickoff renderer and
loop-run start, `chat_message.source`), S10 (types only; `TriggerAccess`/`CostCap` stay stubs). **S20 and S19 are built and reused** (loop provenance columns, `StartFromTrigger`'s shape, `trigger.Outcome`/`StillGoing`, `allow_outward`); S35 is not needed.
*(Task ids are the S38/S39 coordinator's; if a number moved, T0-Q1/Q2 records the real one.)*

## T0 — Investigation (no code)

Ten questions (`tasks/T0.md`): **Q1** S38's real goal/task store functions and whether each runs in one transaction; **Q2** S39's
`source`, kickoff renderer, `LoopStartParams` ids and where started-by lives; **Q3** the next migration number, post-S38 column names,
tests that pin the migration count; **Q4** the `StartBlueprintLoop` closure's test coverage and the runner's idempotence; **Q5** where
the dispatcher goroutine starts and joins; **Q6** the human-actor stamp and the narrow surface; **Q7** SQLite facts (a `BEFORE
INSERT` trigger reading `task`, the `event_trigger` table name, `ALTER … ADD COLUMN`) measured on a populated copy; **Q8** the
`EventTriggerSpend` query measured on real rows; **Q9** the bench-page and UI-test plumbing; **Q10** S20 as built (migrations, `StartFromTrigger`, `trigger.Outcome`/`StillGoing`, narrow `trigger`) — the rebase re-verified.
**Exit:** `investigation.md` with ten dated verdicts; `design.md` edited where an answer moves a rule; plan amendments appended.
**Depends: S38 T7, S38 T15, S39 T8 built.**

## T1 — Migration: outbox, triggers, fires, loop started-by (AU3, AU4, AU5, AU7)

`00NN_automation`: `automation_event`, `automation_cursor`, `event_trigger`, `event_trigger_fire`, the manual-only DB trigger, three `loop` columns
(and `chat_message.source_ref` if S39 did not add it). Upgrade test first; migration-count tests bumped; schema-drift test green.
**Depends: T0.**

## T2 — Outbox writes inside S38's store functions (AU3, AU4, AU5)

`insertAutomationEvent` helper (tx-only, depth derived); one call added to `DecideGoal`, manual `CreateTask`, manual-task approval,
manual-task completion; reads/cursor functions. Tests: rollback leaves no outbox row; loop tasks never emit. **Depends: T1.**

## T3 — The trigger store and the match grammar (AU1, AU6, AU7)

`store.Match` (parse/validate/canonical/`Matches`), `store.Actor`, trigger CRUD with the human-only lock, fire-record functions
(`ClaimEventFire`, `DecideEventFire`, `RecordEventFire`, `ListEventFires`, `OpenEventFires`). Pure grammar tests plus store tests. **Depends: T2** (it uses `AutomationEvent`
and `AutomationHead`; T3 touches different files from T2).

## T4 — `StartFromEvent`: S20's door extended for events (AU2, AU9, AU10)

One narrow extraction of the closure body into `startLoopWith` (byte-parity test; S20's `StartFromTrigger` untouched and re-tested); `NewLoop` gains
`AutomationDepth` and `FirstMessage` (`StartedBy`/`TriggerID` already exist from S20); `CreatePinnedLoop` writes provenance, depth and the kickoff in one tx
*before* the runner starts; `StartFromEvent` with `Attended = &unattended` and no outward lever. **Depends: T1; S39 T8.**

## T5 — The dispatcher (AU5, AU8, AU11, AU12)

`internal/automation`: `Dispatcher`, `Drain`, `fire`; `EventTriggerSpend` and run-count queries; depth guard, overlap, limits, target
resolution, start, outcomes. Fake `Starter` and injected clock; **no sleeps**. **Depends: T2, T3, T4.**

## T6 — Crash recovery and daemon wiring (AU13, AU8)

`Reconcile` (the table in AU13) with a kill-between-steps test per row; `Run`; the goroutine started in `serve.go` after
`StartLoops` and joined at shutdown. **Depends: T5.**

## T7 — The read surface (AU12, AU14)

`automation.trigger.list`, `automation.trigger.show`, `automation.trigger.fires` through a `EventTriggerReader` seam; protocol types, three fixtures, `paramsTypeFor`/`resultTypeFor`
cases, `make protogen` and `check-protogen`; the daemon reader. No write method yet. **Depends: T3, T6; S38 T15.**

## T8 — The write surface and the human-only pins (AU1, AU6, AU14)

`automation.trigger.create/update/enable/disable` behind the actor check (first step of every handler); `loop.CheckBlueprint` extracted from `loop.Start`
(pure extraction); validation of match, blueprint, scope and kickoff; five fixtures; the three grep-pinned tests that no tool, handler body
or narrow-surface action reaches a trigger write. **Depends: T7; S38 T15; S39 T8.**

## T9 — The bench page (AU14, AU15)

`AutomationPage` (trigger table, enable/disable with reason, fire log with five outcomes, a JSON create box), `BENCH_PAGES` row,
hand-written types in `lib/protocol.ts`, a vitest file. **Depends: T8.**

## T10 — Close-out: the record

Zero-regression proof (D11); the deletion drill; measured test counts into Notes; `DEMO.md §S40` with a walked Findings table (goal
approved → `dev-loop` unattended → kickoff message → first outward step denied; a manual `bug` task → its blueprint; limits; a
crash-and-reconcile; depth; human-only); `STATUS.md`/`ROADMAP.md`/`PLAN.md`/`TASKS.md` rows; the `AU` prefix row in `ARCHITECTURE.md` §11;
S20's code and docs verified untouched; S41 sketched as a roadmap candidate. **Depends: T1–T9.**

## Files and serialization points

| File | Tasks | Notes |
| --- | --- | --- |
| `internal/store/schema.go` | T1 | one migration entry + `const migration00NN` |
| `internal/store/automation.go` | T2, T5 | T2 creates (outbox); T5 appends cost/count queries — **T2 first** |
| `internal/store/triggers.go`, `trigger_match.go` | T3 | new |
| `internal/store/{goals,tasks}.go` (S38) | T2 | one helper call each; VERIFY real file names |
| `internal/store/loops.go`, `blueprint.go` | T4 | `NewLoop` fields; `CreatePinnedLoop` INSERT |
| `internal/daemon/start_loop.go`, `event_fire.go` | T4 | new; the closure in `blueprint.go` shrinks; `trigger_fire.go` (S20) **not edited** |
| `internal/automation/*.go` | T4 (starter iface), T5, T6 | `dispatcher.go` serialization point: **T5 → T6** |
| `cmd/aeon/serve.go` | T6 | one goroutine, one join |
| `internal/protocol/{triggers.go,method.go,conformance_test.go}` + fixtures | T7, T8 | `triggers.go` serialization point: **T7 → T8** |
| `internal/loop/pin.go`, `internal/daemon/triggers.go` | T8 (pin.go), T7 → T8 (daemon/triggers.go) | pin.go: pure extraction |
| `ui/src/features/test-bench/pages/AutomationPage.tsx`, `ui/src/app/routes.ts`, `ui/src/lib/protocol.ts`, `ui/test/automation.test.tsx` | T9 | |

## Sequencing summary

```
T0 ► T1 ► T2 ► T3 ► T4 ► T5 ► T6 ► T7 ► T8 ► T9 ► T10
```

Linear by ruling and by nature: investigate; give the events and triggers their tables; make domain writes emit atomically; give the
triggers a store; give the daemon one start path; consume the outbox; survive a crash; open the read door, then the human-only write door; show it; write it down.
**Three** new tables plus a cursor, **one** new package (`internal/automation`), **one** goroutine, **seven** socket methods (three reads, four human writes),
**zero** agent tools, **zero** change to the unattended rule, **zero** change for a daemon with no triggers.

**Amendments, 2026-10-28 (T0).** From `investigation.md`: (1) T1 additionally **rebuilds
`loop_run`'s CHECK** to admit `started_by_kind='event'` (measured rejection today) and T4 extends
the two Go whitelists (`store/runs.go:85`, `loop/run.go:41`) — "zero change" still describes the
running daemon, but the table is touched; (2) T2's manual-completion site is **`SetManualTaskStatus`
(`tasks_manual.go:274`), not `SetTaskStatus`**, and that function has **no transaction** — T2 wraps
it before adding the emit (T0-Q1, in bold); (3) the extraction closure is `blueprint.go:315–394`;
(4) the migration is **`0039_automation`**, the eight count-pin sites are named in Q3, and T1 also
adds `chat_message.source_ref` (+ `NewChatMessage.SourceRef`). Design AU7/AU9 carry the dated
edits; task VERIFY lines updated and listed in the investigation.
