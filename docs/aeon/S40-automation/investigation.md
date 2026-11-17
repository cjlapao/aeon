# Investigation — S40 Automation

Ran 2026-10-28 (Europe/London) on `main` at `a45c9413` (S39 T13), `aeon/` tree, Go toolchain with
the standard preamble (`GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1
CGO_CFLAGS="-I$PWD/../.tmp/cgo-shim" XDG_STATE_HOME=$(mktemp -d)`). No code changed.
Prerequisites verified with the commands the questions themselves run: **S38 T6/T7** (Q1 found
`DecideGoal`/`CreateManualTask`/`DecideTask`/`SetManualTaskStatus`/`manual`), **S38 T15** (Q6 found
the socket actor stamp), **S39 T8** (Q2 found `source`, `RenderKickoff`, the ids) — all BUILT.

A word on method, because it caught one of its own mistakes: every SQL fact here is measured on a
**copy** of a freshly migrated throwaway store (the machine's `~/.local/state/aeon/state.db` was
inspected and found **drifted** — 37 ledger rows, no `task.manual`, no `loop_run`: a development
artefact, left alone, recorded). One early probe appeared to show the CHECK already allowing
`event`; that was **my own earlier rebuild leaking into the copy** — the clean re-measure says
otherwise (Q7). Contaminated probes were discarded and re-run, and are marked where they taught
something.

## Q1 — S38's goal and task store functions

```sh
grep -rn "func (s \*Store) \(DecideGoal\|CreateTask\|SetTaskStatus\|DecideTask\|CreateGoal\)" internal/store
grep -n "w.Begin()\|tx.Commit()" internal/store/goals.go internal/store/tasks.go internal/store/tasks_manual.go
sed -n '274,300p' internal/store/tasks_manual.go     # SetManualTaskStatus
sed -n '302,340p' internal/store/tasks.go            # SetTaskStatus
```

| function | file:line | opens tx? | commit at | emit site (insert `insertAutomationEvent` before) | event |
| --- | --- | --- | --- | --- | --- |
| `DecideGoal(id string, version int, decision string, by Actor, reason string) (DecideResult, *Refusal, error)` | `internal/store/goals.go:417` | yes (`:421`) | `:455` | before `:455`, after `reviewInsert(...)` (`:452`) | `goal.approved` when `decision == "approved"` (the write itself is `UPDATE goal SET review_state=? ... AND review_state='in_review'` at `:427`) |
| `CreateManualTask(in NewManualTask) (Task, *Refusal, error)` | `internal/store/tasks_manual.go:62` | yes (`:84`) | `:147` | before `:147` | `task.created` |
| `DecideTask(id string, decision string, by Actor, reason string) (DecideResult, *Refusal, error)` | `internal/store/tasks_manual.go:301` | yes (`:325`) | `:355` (claimed-FROM `approval_state='pending'`, `:331`) | before `:355` | `task.updated` when `decision == "approved"` |
| `SetManualTaskStatus(id string, to TaskStatus) (SetTaskStatusResult, *Refusal, error)` | `internal/store/tasks_manual.go:274` | **no** | — | wraps its own `UPDATE task SET status=?, updated_at=? WHERE id=? AND manual=1` (`:287`) | `task.updated` when `to == StatusComplete` |

**In bold, as the question demands:** **`SetManualTaskStatus` writes without opening a
transaction** — a single `s.w.Exec` UPDATE, read-back after. T2 must wrap it (Begin → the UPDATE →
`insertAutomationEvent(tx, …)` → Commit) before adding the emit. `SetTaskStatus` (the ENGINE path,
`tasks.go:302`) writes **two** statements without a tx (task + loop `updated_at` touch) — it is NOT
an emit site (engine tasks never emit, AU3/Q7's DB trigger says so twice), recorded anyway because
the question asked for the tx facts.

The `completed` constants: `internal/store/machine.go:38` `StatusComplete TaskStatus = "complete"`
— there is **no `"completed"` status**; the vocabulary is `pending|issue-open|in-progress|verified|
secure|pr-open|in-review|changes-requested|merged|complete|failed|blocked` (`machine.go:29–40`),
shared by manual tasks via `ValidateTransition(…, ModeStandard)`. Trigger `on_event` strings stay
`created | approved | completed` (AU3) and map to those writes: `created`→`CreateManualTask`,
`approved`→`DecideTask("approved")`, `completed`→`SetManualTaskStatus(StatusComplete)`.

**Verdict: settled.** Deliverable: the table above.

## Q2 — S39's kickoff, `source`, and started-by

```sh
grep -rn "source" internal/store/chat.go | head -12
grep -rn "RenderKickoff\|KickoffData" internal/blueprint/*.go
grep -n "GoalID\|TaskID" internal/protocol/blueprint_loop.go
grep -n "started_by_kind" internal/store/schema.go internal/store/runs.go
```

- `chat_message` **has `source`** (`chat.go:514–533`: NULLABLE; `""` defaults to `user` for a human
  author only; `ValidSource` = `user|template|automation|blueprint`; `nullable()` on insert).
  **It has NO ref column** — the SELECT list is `id, loop_slug, component_root, seq, author, kind,
  body, question_id, created_at, source`. T1 adds `chat_message.source_ref` (AU9's
  "`source_ref` is added by T1 if absent" — it is absent). `store.NewChatMessage`
  (`chat.go:137`) carries `Author, Kind, Body, Source` — **`SourceRef` is added by T1/T4**.
- Renderer, exact: `internal/blueprint/kickoff.go:35`
  `func RenderKickoff(src string, d KickoffData) (string, error)`, `KickoffData{Goal, Task,
  Component, Project string; Params map[string]string}` (`:13–19`). AU10's extra `Params` keys
  (`event`, `trigger`, `depth`, `untrusted`) ride the existing map — no type change.
- `LoopStartParams` carry `GoalID` (`blueprint_loop.go:51`) and `TaskID` (`:55`) ✓.
- `loop.started_by`/`loop.trigger_id` exist (0033; `loops.go:65–66` `NewLoop.StartedBy/TriggerID`,
  written in the pin tx) and **`SetLoopProvenance` (`loops.go:168`) takes free text** — the value
  `event` passes the store untouched (only `""` refuses). The trigger package's `Provenance` enum
  (schedule/webhook, `trigger_fire.go:101–103`) never sees it: S40 writes via `NewLoop`, not the
  trigger package.
- **Does `loop_run` also record started-by? YES — and it is the one thing that must change.**
  `schema.go:1945`: `CHECK (started_by_kind IN ('user','agent','trigger'))`, re-checked in Go at
  `store/runs.go:85` (`case "user","agent","trigger":`) and documented at `loop/run.go:41`. Measured
  (Q7): inserting `event` **fails** the CHECK. So T1 must rebuild `loop_run`'s CHECK to admit
  `'event'`, and T4 must extend the Go whitelist (`runs.go:85`, comment `loop/run.go:41`) — the
  design said "adds only `loop.automation_depth`"; that line is now edited dated
  (2026-10-28, T0-Q2), and T1/T4's VERIFY lines updated.

**Verdict: changed the design** (AU7: `loop_run` carries started-by, so the value `event` must be
added to its CHECK + both Go whitelists — not "one column only"). Everything else settled.

## Q3 — Migration number, names after S38, count pins

```sh
grep -n '{38, "0038' internal/store/schema.go          # the last entry
aeon db info                                            # fresh store: "schema 38 migration(s) applied, latest 0038_message_source"
grep -rn "len(Migrations())\|!= 38\|want 38" internal/store/*_test.go internal/loop/counters_test.go
sqlite3 copy.db "PRAGMA table_info(task); PRAGMA table_info(loop);"
```

- **Next free number: 39 → `0039_automation`** (fresh store measures `38 migration(s) applied,
  latest 0038_message_source`).
- **Count/index pins (the eight numeric sites, three files — the same eight S39 T1 moved):**
  `store/schema_test.go:909` (`len(all) != 38`) and `:922` (`want 38` message) inside
  `TestTheRebuildBeginsOnZeroPreExistingPolicyRows` (`:894`); `store/policy_test.go:458` inside
  `TestPolicyAcceptanceIsRecordedPerTarget` (`:406`) and `:1318` inside
  `TestPolicyEventMethodsReportAFailedStatement` (`:1275`); `loop/counters_test.go:280`
  (`len(m) != 38`) and `:284` (`last.ID != 38 || … Contains(last.Name, "message_source")`) inside
  `TestSchemaFreshEqualsMigrated` — the last pair also moves the **name** to
  `0039_automation`. `policy_test.go:306/325` is a before/after delta and needs no bump.
- `loop`'s scope column today: **`component_root`** (measured `table_info`; `project_key` is
  nullable, S38's).
- `task`: rebuilt by 0036 onto a **surrogate `id`** (TEXT — measured inserts need no other key),
  **`manual`** column present, plus `type` (CHECK `bug|feature|chore|refactor|investigation` or
  NULL), `approval_state`, `component_id`, `goal_id`, `parent_task_id`, `assignee_kind/assignee`.
  Loop tasks keep `(component_root, loop_slug, taskid)` through a PARTIAL unique index.
- The goal table is `goal`; the decision column is **`review_state`** (values include
  `draft|in_review|approved|rejected`), and `goal.source_kind` already admits
  **`'event'`** (CHECK `manual|brainstorm|webhook|cron|event`, `schema.go:1759`) — an automation
  can stamp `source_kind='event'` with no migration.

**Verdict: settled.** Deliverable: the names block above.

## Q4 — The `StartBlueprintLoop` closure and the runner's idempotence

```sh
grep -rn "StartBlueprintLoop" internal --include=*.go | grep -v _test
sed -n '100,135p' internal/daemon/trigger_fire.go    # StartFromTrigger
grep -rn "func (s \*Store) SetLoopProvenance" -A 12 internal/store/loops.go
```

- The closure is **`internal/daemon/blueprint.go:315–394`** (`d.Table.StartBlueprintLoop = func(ctx,
  p protocol.LoopStartParams) (any, error)`; it trims/validates the workspace, defaults the slug
  `loop-<nanos36>`, creates + pins the loop, and calls `d.loopRunner.Start`). **The design's
  "blueprint.go:281-322" is wrong** — the real range is 315–394; T4's extraction is from there
  (design edited dated 2026-10-28, T0-Q4).
- Test coverage: real call-sites exist to exercise the extraction —
  `daemon/blueprint_workspace_test.go:20,76` and `daemon/blueprint_start_org_test.go:51,85,107,198`
  (six calls). They stay green through T4's byte-parity (the closure keeps returning
  `startLoopWith`'s result map).
- `StartFromTrigger` (`trigger_fire.go:100`) is the shape AU9 mirrors: it rejects provenances other
  than `trigger|webhook` (`:101–103`), sets `attended := false` locally (`:108`), calls the closure,
  type-asserts `res.(map[string]any)` for `m["id"]` (a missing id is an error), then stamps
  provenance **after** with `SetLoopProvenance` — loudly logging, never orphaning, a failed stamp.
  The design's three reasons S40 cannot reuse it are each confirmed by reading: `trigger.Fire` has
  no kickoff/goal-id/task-id/depth fields, the provenance guard refuses `event`, and the
  after-the-start stamp is too late for a first message.
- Idempotence for the dispatcher is **not** in this path — it is the fire row:
  `UNIQUE (trigger_id, outbox_id)` + claim-before-start (AU8), so a re-delivered outbox row meets
  an existing claimed row and cannot start twice. Nothing in the start path needs a guard.

**Verdict: settled** (with the closure-range correction listed as a design edit).

## Q5 — Where the dispatcher goroutine starts and joins

```sh
grep -rn "trigger.NewScheduler" internal/daemon/*.go | grep -v _test
grep -rn "go d.triggers.Run(ctx)" internal/daemon/blueprint.go
grep -n "func (sc \*Scheduler) Run" -A 12 internal/trigger/clock.go
```

The scheduler is **constructed** at `internal/daemon/daemon.go:339` (`trigger.NewScheduler(
triggerRows{opts.Store}, d, trigger.SystemClock{})`, gates wired right after — `sc.Gate.Overlap`,
`sc.Gate.Access`), and **started** in the daemon's run path, `internal/daemon/blueprint.go:986–996`:
`Reconcile` first (a failure speaks and the tick runs anyway), then `go d.triggers.Run(ctx)`
(`:995`). `Run` (`trigger/clock.go:240`) is a `time.Ticker` loop honouring `ctx` — it **joins by
ctx cancellation**, no channel. The dispatcher follows the same shape: constructed beside `:339`
when `opts.Store != nil`, `go d.automation.Run(ctx)` beside `:995` after the trigger reconcile.
(The daemon also recovers pinned loops right after, `PinnedLoopsByStatus("running")` — the event
reconcile (AU13) goes before the ticker starts, mirroring `Reconcile`.)

**Verdict: settled.**

## Q6 — The human-actor stamp and the narrow surface

```sh
grep -rn 'Actor{Kind: "user"' internal/daemon/*.go | grep -v _test
grep -n "func Actions" internal/narrow/actions.go
```

- The peer stamp for socket doors is `internal/daemon/records_socket.go:38,42`:
  `store.Actor{Kind: "user", ID: strconv.Itoa(r.uid(sess))}` — the S38 T15 actor the decides ride.
  S40's four human writes stamp the same way (a `RecordsService.Do`-style path passes `by Actor`
  into the store; identity is stamped, never parsed from input). `blueprint.go:1095–1097` shows the
  loop-side variant (`ID: u.Username`, fallback `"socket-peer"`).
- The narrow table is `internal/narrow/actions.go:108` (`var actions = []Action`), read by
  `Actions()` (`:208`); each action carries `SocketMethod` (`:61`). **No `trigger.*` and no
  `automation.*` action exists** — S20 kept its methods off the narrow surface, and S40 keeps its
  seven there too; the AU14 human-only pins have a fresh precedent to copy from S39's
  `s38_invariants_test.go` (which asserts absence from `narrow.Actions()`).

**Verdict: settled.**

## Q7 — SQLite facts, measured on a populated copy

Method: fresh store via `aeon db info` on `XDG_STATE_HOME=$(mktemp -d)` ("schema 38 migration(s)
applied"), **copied** to `/tmp/aeon-q7/copy.db`, rows inserted by SQL (loop `lp1`, `task te1
manual=0 / mt1 manual=1`, steps with each cost state).

```sql
CREATE TRIGGER automation_event_manual_only BEFORE INSERT ON automation_event
WHEN NEW.subject_kind = 'task'
 AND COALESCE((SELECT manual FROM task WHERE CAST(id AS TEXT) = NEW.subject_id), 0) <> 1
BEGIN SELECT RAISE(ABORT, 'automation events are for manual tasks only'); END;
```

```
ghost: Error: stepping, automation events are for manual tasks only (19)
te1:   Error: stepping, automation events are for manual tasks only (19)   (manual=0)
mt1:   ok
kept=1
```

- **The design's trigger SQL (design.md:73–76) is verified verbatim and is ghost-safe** —
  `COALESCE` **outside** the subquery makes a missing row abort. The pitfall, measured the hard
  way: moving `COALESCE` **inside** (`SELECT COALESCE(manual,0) FROM task WHERE …`) makes a ghost
  row evaluate `NULL <> 1` → WHEN false → the event **passes** (first probe kept `kept=2`). The
  migration copies design.md's text exactly; do not "tidy" it.
- The `event_trigger` / `event_trigger_fire` table names are legal (created; `trigger` alone is the
  reserved word — AU7's warning stands).
- `ALTER TABLE loop ADD COLUMN automation_depth INTEGER NOT NULL DEFAULT 0` — applies ✓.
- `loop_run`'s CHECK rejects `event` (measured): `Error: stepping, CHECK constraint failed:
  started_by_kind IN ('user','agent','trigger')`. **The rebuild was measured end-to-end**: `.schema
  loop_run` → sed the CHECK to include `'event'` → `PRAGMA foreign_keys=OFF` → create
  `loop_run_new`, `INSERT INTO loop_run_new SELECT *`, `DROP`, `RENAME` → insert `event` →
  `event kept=1`. Two measured traps for T1: the `.schema` dump also emits the **CREATE INDEX**
  statements (re-running them hits `index idx_loop_run_state already exists` — hand-write the
  rebuild and re-create indexes by name afterwards), and the earlier **contaminated probe** — an
  abandoned rebuild renamed my hand-written table into place and briefly *proved* the wrong thing;
  a rebuild must be proven on a fresh copy, never on one that already had a probe run.

**Verdict: settled** (the CHECK rebuild itself is a changed-design item — recorded under Q2/AU7).

## Q8 — `EventTriggerSpend` on real rows

The rows: one loop with `started_by='event', trigger_id='tr1', started_at='2026-10-28T06:00:00Z'`
and three steps — `(known, 0.5)`, `(partial, 0.2)`, `(unknown, NULL)`. The house query (pattern from
`store/runs.go:293` `costQuery`, join on `(slug, component_root)`):

```sql
SELECT COALESCE(SUM(s.cost_usd),0),
       COALESCE(SUM(CASE WHEN s.cost_state='partial' THEN 1 ELSE 0 END),0),
       COALESCE(SUM(CASE WHEN s.cost_state='unknown' THEN 1 ELSE 0 END),0),
       COUNT(*)
FROM step s JOIN loop l ON l.slug=s.loop_slug AND l.component_root=s.component_root
WHERE l.started_by='event' AND l.trigger_id='tr1' AND l.started_at >= '2026-10-27T07:00:00Z';
```

```
0.7|1|1|3
```

`USD=0.7` (a floor — the partial step's 0.2 is included, matching AU11 "sums `cost_usd` over steps
whose `cost_state` is `known` or `partial`"), `Floor=true`, `Unknown=true`, `Steps=3`. The CHECK
vocabulary is **`cost_state IN ('known','partial','unknown')`** — `known`, not `exact` (measured
rejection: `CHECK constraint failed: cost_state IN ('known','partial','unknown')`). A `running`
step with no cost yet has `cost_state='unknown'` by the insert default (`steps.go:178`) — the NULL
row of AU11 is covered by the `unknown` arm.

**Verdict: settled.**

## Q9 — Bench-page and UI-test plumbing

```sh
git show HEAD:./src/app/routes.ts | grep -n bench
grep -rn "StatusPage" src/features/test-bench/TestBench.tsx
```

- The bench route **already exists in the committed routes**: `{ id: "bench", path: "/bench/:page?",
  title: "Test bench", … }` (`routes.ts:35`, `RouteId` includes `"bench"`). **T9 needs no edit to
  the foreign-dirty `routes.ts` or `router.tsx`** — a new page registers itself in the pages array
  of `src/features/test-bench/TestBench.tsx` (a clean file; entries look like
  `{ id: "status", label: "Status", path: "/bench" }` at `:39`), importing from `./pages/`.
- `ui/src/lib/protocol.ts` is clean (not in the foreign-dirty set) — the socket-method types go
  there. Test file: `aeon/ui/test/automation.test.tsx`, following the S39 precedent
  (`test/chat-runs.test.tsx`); the older `bench-*.test.tsx` naming also exists — either fits the
  house, plan's name is used.
- Pages to imitate: `pages/PolicyPage.tsx` (list + form, closest shape).

**Verdict: settled.**

## Q10 — S20 as built (the rebase re-verified)

```sh
grep -n '{32, "00\|{34, "00' internal/store/schema.go
grep -rn "type Outcome\|Outcome = \"" internal/trigger/record.go
grep -n "func StillGoing" internal/trigger/overlap.go
grep -rn "MethodTrigger" internal/protocol/*.go | grep -o 'MethodTrigger[A-Za-z]*' | sort -u
```

- Migrations **0032–0034** confirmed: `0032_trigger_schedule_and_fire`, `0033_trigger_fire_inputs`
  (`loop.started_by`/`trigger_id`, `trigger_schedule.workspace_root/goal`), `0034_trigger_gap`.
  **AU7 rebase confirmed.**
- `trigger.Outcome` (`record.go:21`) is a **closed five**: `fired | did_not_fire | could_not_say |
  skipped | refused` (`:25–40`) — AU12's five outcomes map onto it cleanly (S40's fire row uses
  `started|refused|limited|skipped|could-not-say`, its own CHECK — the vocabularies are parallel,
  not shared strings; no collision, nothing moves). `StillGoing(status)` exists at
  `trigger/overlap.go:41` — reused for the overlap gate as designed.
- S20's socket methods: `trigger.add|list|pause|resume|fire-now|history`
  (`MethodTriggerAdd|List|Pause|Resume|FireNow|History`). **`MethodAutomation*` / any
  `automation.` string: zero hits repo-wide** — the seven S40 names are free, and the narrow
  surface holds no `trigger` action (Q6), so nothing there calls `StartBlueprintLoop`: the only
  non-socket caller is the scheduler path `Tick → StartFromTrigger → d.Table.StartBlueprintLoop`,
  which keeps working through T4's extraction (the closure delegates to `startLoopWith`).
  **AU9 rebase confirmed.**

**Verdict: settled — both AU7 and AU9 rebase CONFIRMED, no change.**

## Design edits made by this investigation

1. **AU7** (2026-10-28, T0-Q2): "Only one new `loop` column" stands for `loop`, but the value
   `event` also has to enter `loop_run`'s `started_by_kind` CHECK (rebuild, measured Q7) and the
   two Go whitelists (`store/runs.go:85`, `loop/run.go:41` comment). Edited dated in `design.md`.
2. **AU9** (2026-10-28, T0-Q4): the extraction range is `blueprint.go:315–394`, not `281–322`.
   Edited dated in `design.md`.

## VERIFY-line updates to task files (differences found, listed)

- `tasks/T1.md`: the `loop_run` CHECK rebuild is **added** to its scope (with Q7's two traps);
  migration number recorded as `0039_automation`; count-pin sites listed (eight, Q3).
- `tasks/T4.md`: closure range corrected to `blueprint.go:315–394`; Go whitelist extension named.
- `tasks/T2.md`: `SetManualTaskStatus` marked **needs a tx wrap first** (Q1, bold).
- Others verified as written (Q7's trigger SQL verbatim; AU11's cost vocabulary `known` — T5's
  query text stands).
