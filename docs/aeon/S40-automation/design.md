# Design — S40 Automation

Fills in `brief.md`. Rules are **AU1–AU16** (prefix `AU`, unused in `ARCHITECTURE.md` §11; T10 reserves it). They are cited from
`plan.md` and `tasks/`. Where a rule names an S38/S39 function, the name is that slice's *planned* one: the task that uses it begins
with a `VERIFY: grep` and records the real name in its Notes.

## AU1 — A trigger is standing authority, and only a person writes one

A trigger is a durable instruction to start agents without anyone present. Therefore:

- **Authoring is a human-only door**, in the family of `*.decide` (S38 OL10) and policy/MCP-config authoring (PL15, S35): socket writes
  by a human peer. S38 OL3's carve-out already excludes it from the agent catalog; S40 makes it a test.
- Three independent locks, each tested: (1) **no agent tool** — no name in `tools.Registry()` and no source under `internal/tools/`,
  `internal/records/` (S38's catalog) or the narrow surface references a trigger write; (2) **socket** — `automation.trigger.create/update/enable/
  disable` refuse an actor of kind `agent`; (3) **store** — `event_trigger.created_by_kind` has `CHECK (created_by_kind = 'user')` and the
  Go function takes a `store.Actor` and refuses a non-`user` kind before any SQL.

> Extended 2026-10-30 by S27 CN2: the scheduler's rows are authority records under the same three locks. See S27-continuous/design.md.
- Reads (`automation.trigger.list/show/fires`) are open to any initialised client. The fire log is audit, not authority.
- **A trigger is never deleted** in this slice — disabled, with a reason. Deleting would orphan `event_trigger_fire` audit rows. (A
  follow-up may add archive.) `enable`/`disable` are human writes too: turning a standing authority back on is authoring.

## AU2 — "Run whatever" means any blueprint; it does not lift G14 or S19's outward lever (brief §0.1)

`StartFromEvent` (AU9) starts every loop with `Attended = &unattended` (a local `unattended := false`, the same line S20's
`StartFromTrigger` has) and `AllowOutward = false`, both declared at start and never inferred (G14, S20 TR2, S19 T6). The started loop
therefore: denies `ask` at once, has no outward opt-in (S19: `loop.allow_outward = 0`), and **stays unattended when a person chats into it**
(S39 CR7). **No code path in S40 sets `attended` or `allow_outward` to anything else.** A test (T4) asserts the pinned loop row has
`attended = 0` and `allow_outward = 0`; a grep test asserts the only `Attended:` and `AllowOutward:` assignments in `StartFromEvent`'s file are
`&unattended` and `false`. The author's stronger reading is a one-line change at that call site plus a human-written column (brief §0.1);
it is recorded, not built. Amended 2026-10-30 by S56: schedule, webhook, event and scheduler starts may be pinned from a person-authored per-root autonomy policy (`loop_autonomy_pin`); `loop.allow_outward` stays 0 for them; the narrow surface and the three AU1 locks are unchanged. See S56-autonomy-policy/design.md AL7/AL8.

## AU3 — A closed event set, for goals and manual tasks only

| Event | Emitted by (S38 store function, planned name) | Subject | Facts copied to the outbox row |
| --- | --- | --- | --- |
| `goal.approved` | `DecideGoal` when it moves `in_review → approved` (the claimed-FROM `UPDATE` changed 1 row) | goal | `subject_version` = approved version, `goal_type`, `source_kind`, `project_id`, `component_id` (only when the goal has **exactly one** `goal_component`; else `''`) |
| `task.created` | `CreateTask` when `manual = 1` | task | `task_type`, `component_id`, `project_id` (via component), `goal_id` ignored |
| `task.approved` | the manual-task approval function (`DecideTask`/`ApproveTask`, S38 T7) on `pending → approved` | task | same as above |
| `task.completed` | `SetTaskStatus` when `manual = 1` and the new status is a **completed** one (T2 picks the constants from `machine.go`; VERIFY) | task | same as above |

- **Loop tasks never emit** (`manual = 0`). Three locks: the store functions on the loop-task path do not call the outbox helper; the
  helper itself refuses (`ErrNotAutomatable`) a task event whose row is not `manual = 1`; and a DB `BEFORE INSERT` trigger aborts such
  an insert. Together they make *loops-of-loops through the board* unrepresentable.
- No event for `rejected`, `parked`, edits, loops, runs or steps. Adding one is an **additive** change: a `CHECK` rebuild plus an emit site
  plus a match-grammar line (AU16).
- `task.created` and `task.approved` for the same task are **two events**, and a trigger names exactly one in `match.event`. A task
  created pre-approved (a person creating it as approved) emits both, in order.

## AU4 — The outbox: same transaction, or no event

```sql
CREATE TABLE automation_event (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  event             TEXT NOT NULL CHECK (event IN ('goal.approved','task.created','task.approved','task.completed')),
  subject_kind      TEXT NOT NULL CHECK (subject_kind IN ('goal','task')),
  subject_id        TEXT NOT NULL,
  subject_version   INTEGER NOT NULL DEFAULT 0,          -- goal: the approved version; task: 0
  project_id        TEXT NOT NULL DEFAULT '',            -- '' = not applicable / not known (never NULL, never "any")
  component_id      TEXT NOT NULL DEFAULT '',
  goal_type         TEXT NOT NULL DEFAULT '',
  task_type         TEXT NOT NULL DEFAULT '',
  source_kind       TEXT NOT NULL DEFAULT '',
  depth             INTEGER NOT NULL DEFAULT 0 CHECK (depth >= 0),
  caused_by_trigger TEXT NOT NULL DEFAULT '',
  created_at        TEXT NOT NULL,
  CHECK ((event LIKE 'goal.%' AND subject_kind = 'goal') OR (event LIKE 'task.%' AND subject_kind = 'task'))
);
CREATE INDEX automation_event_subject ON automation_event (subject_kind, subject_id);
CREATE TABLE automation_cursor (
  name       TEXT PRIMARY KEY,                           -- 'dispatcher'
  last_id    INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
CREATE TRIGGER automation_event_manual_only BEFORE INSERT ON automation_event
WHEN NEW.subject_kind = 'task'
 AND COALESCE((SELECT manual FROM task WHERE CAST(id AS TEXT) = NEW.subject_id), 0) <> 1
BEGIN SELECT RAISE(ABORT, 'automation events are for manual tasks only'); END;
```

- **The helper** `insertAutomationEvent(tx *sql.Tx, now string, in AutomationEventIn) error` is the **only** writer of the table
  (grep-pinned in `internal/store`). It takes the caller's `*sql.Tx`, so a rollback of the domain write removes the event; it never
  opens its own transaction and never calls `Daemon.publish`.
- `AutomationEventIn` carries `OriginLoopID string` — the loop whose agent caused this write, `""` for a person. The helper derives
  `depth` and `caused_by_trigger` **inside the tx** from `loop.automation_depth` / `loop.started_by` / `loop.trigger_id` (AU5). A caller cannot pass a
  depth; identity is stamped, not argued (OL3).
- Domain `task.changed` / `goal.*` notifications to the UI stay on the existing `publish` path, unchanged. The outbox is for
  automation only.
- The `task`/`goal` column names in the trigger and helper follow S38 OL7/OL14 (`task.id`, `task.manual`); **VERIFY** before T1.

## AU5 — The cascade guard: a depth counter

- `loop.automation_depth INTEGER NOT NULL DEFAULT 0`. A loop started by a person: 0. A loop started by an **event** trigger on an event of depth
  *d*: **d + 1**. Loops started by S20 (`'trigger'`, `'webhook'`) keep the default 0.
- An outbox event caused by a write inside loop *L* (an agent's `task_create manual:true`, `goal_*`) has `depth = L.automation_depth`
  and `caused_by_trigger = L.trigger_id` when `L.started_by = 'event'` (a schedule/webhook-started loop, `'trigger'`/`'webhook'`, is depth 0: only event triggers can cascade, S20 fires cannot loop back). A person's write: depth 0.
- **`MaxAutomationDepth = 3`.** The dispatcher refuses a fire when `event.depth > 3`: `refused('depth-exceeded: event depth 4 > 3,
  caused by trigger <id>')`, recorded like any other. So a chain of at most four trigger-started loops can exist; the fifth link is a
  `event_trigger_fire` row an operator can read.
- The refused row is written for **each matching trigger**. An over-deep event matching no trigger writes nothing (the outbox row
  itself is the record).

## AU6 — The match grammar (closes the spec's open row)

```json
{ "event": "goal.approved", "component": "c_1", "goal_type": ["feature","bug"], "source_kind": "manual" }
```

- **Keys, and only these:** `event` (required, one of AU3's four), `project`, `component`, `goal_type`, `task_type`, `source_kind`.
  A value is a string (equality) or a list of strings (in). A key that is absent matches anything. **No** regex, glob, JSONPath,
  numeric or boolean forms, nested objects, `not`, or `or` — an operator who needs *or* writes two triggers.
- **Applicability is validated at write time:** `goal_type` and `source_kind` only with `goal.*`; `task_type` only with `task.*`
  (and each task type must be in S38's type list: `bug|feature|chore|refactor|investigation`; goal types are free text). A key that
  cannot match its event is refused, not silently inert.
- **'' never matches a present key.** An event whose `component_id` is `''` (a multi-component goal) matches a trigger with no
  `component` key and does **not** match one that names a component.
- Limits: ≤ 16 values per key, each 1–128 chars, no empty strings.
- **Canonical form** is stored: keys fixed order, every value a sorted, de-duplicated list (a single string is stored as a
  one-element list). Two triggers differing only in spelling are the same JSON.
- Unknown key → refused with *`match: unknown key "labels" (allowed: event, project, component, goal_type, task_type, source_kind)`*.
- **Task type → blueprint needs no table.** `{event: task.approved, task_type: bug}` + `blueprint: fix-bug` *is* the mapping; a
  second trigger `task_type: feature` → `dev-loop` is the other row. A manual task of a type no trigger names simply starts nothing.
- A trigger's own `project_id` / `component_id` columns are the **scope** (and, for `component_id`, the target workspace, AU7). They
  are enforced *in addition to* `match.project`/`match.component`; a trigger scoped to component C only ever sees events of C.

## AU7 — The tables: beside S20's, reusing its provenance columns

S20 is built (migrations 0032–0034). Its `trigger_schedule` is keyed by a schedule (`kind`, `zone NOT NULL`, `expr`, `workspace_root`, `goal`) and its
`trigger_fire` by `(schedule_id → trigger_schedule ON DELETE CASCADE, instant)` with `outcome NOT NULL`. An event trigger has a match, a kickoff
and limits and no zone; an event fire is keyed by an **outbox row** and needs a *claimed-undecided* state. Extending either table would be a
`CHECK` rebuild plus a dummy schedule row per event trigger, so **S40 adds two tables beside them** and reuses the rest of S20:

- `loop.started_by` (new value `'event'`; S20 writes `'trigger'`/`'webhook'`) and `loop.trigger_id` (the `event_trigger.id`) — migration 0033's
  columns, **not** new ones. Written in the pin transaction through `store.NewLoop.StartedBy/TriggerID`, which already exist.
- `trigger.StillGoing(status)` for overlap, and `trigger.Outcome` as the vocabulary S40's outcomes map onto (AU12).
- Only **one** new `loop` column: `automation_depth` (S20 has no cascade concept).
- **2026-10-28 (T0-Q2), changed by measurement:** `loop_run` also records started-by —
  `CHECK (started_by_kind IN ('user','agent','trigger'))` (`store/schema.go:1945`), re-checked in Go
  (`store/runs.go:85`, comment at `loop/run.go:41`). Measured: `event` fails the CHECK. So the value
  `event` enters that CHECK too (the rebuild was measured end-to-end in `investigation.md` Q7, traps
  included) and both Go whitelists gain the fourth word. `loop` itself still gains only
  `automation_depth`.

```sql
CREATE TABLE event_trigger (
  id                   TEXT PRIMARY KEY,
  name                 TEXT NOT NULL UNIQUE CHECK (name <> ''),
  source               TEXT NOT NULL DEFAULT 'event' CHECK (source = 'event'), -- constant: S20's sources live in trigger_schedule
  project_id           TEXT NOT NULL DEFAULT '',                          -- scope; '' = any (validated in Go, no FK to S38's tables)
  component_id         TEXT NOT NULL DEFAULT '',                          -- scope AND target workspace; '' = use the event's
  match_json           TEXT NOT NULL CHECK (json_valid(match_json)),      -- canonical Match (AU6)
  blueprint            TEXT NOT NULL CHECK (blueprint <> ''),             -- name resolvable by blueprint.Resolve; always follows latest
  kickoff              TEXT NOT NULL DEFAULT '',                          -- '' = the blueprint's own kickoff, else S39 CR6 default
  max_runs_per_hour    INTEGER NOT NULL CHECK (max_runs_per_hour BETWEEN 1 AND 1000),
  max_cost_per_day_usd REAL CHECK (max_cost_per_day_usd IS NULL OR max_cost_per_day_usd >= 0),
  enabled              INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  disabled_reason      TEXT NOT NULL DEFAULT '',
  from_outbox_id       INTEGER NOT NULL,                                  -- MAX(automation_event.id) at creation: no backlog replay
  created_by_kind      TEXT NOT NULL CHECK (created_by_kind = 'user'),    -- AU1 lock 3
  created_by_id        TEXT NOT NULL,
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);
CREATE TABLE event_trigger_fire (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_id   TEXT    NOT NULL,                                         -- no FK: audit outlives edits; triggers are never deleted
  outbox_id    INTEGER NOT NULL,
  source       TEXT    NOT NULL DEFAULT 'event' CHECK (source = 'event'),
  subject_kind TEXT    NOT NULL,
  subject_id   TEXT    NOT NULL,
  depth        INTEGER NOT NULL,
  outcome      TEXT CHECK (outcome IS NULL OR outcome IN ('started','refused','limited','skipped','could-not-say')),
  reason       TEXT    NOT NULL DEFAULT '',
  loop_id      TEXT,
  loop_slug    TEXT,
  cost_note    TEXT    NOT NULL DEFAULT '' CHECK (cost_note IN ('','cost-unknown','cost-floor')),
  claimed_at   TEXT    NOT NULL,
  decided_at   TEXT,
  UNIQUE (trigger_id, outbox_id)
);
CREATE INDEX event_trigger_fire_by_trigger ON event_trigger_fire (trigger_id, id DESC);
ALTER TABLE loop ADD COLUMN automation_depth INTEGER NOT NULL DEFAULT 0;  -- started_by / trigger_id already exist (S20, 0033)
```

- `outcome IS NULL` **is** the "claimed, undecided" state — a claimed row is never a sixth outcome. Only `ClaimEventFire` inserts NULL;
  only `DecideEventFire` (claimed-FROM `WHERE outcome IS NULL`) sets one; a second decision changes zero rows and returns the first.
- The names `event_trigger`/`event_trigger_fire` are deliberately **not** `trigger`/`trigger_fire`: the latter is S20's and `trigger` is an SQLite keyword. `match_json`, never `match`.
- **No FK to S38's tables on purpose**: S38 renames and rebuilds are in flight and a CASCADE here would delete audit. Scope ids are
  validated in the store function against S38's read functions (`VERIFY` names).
- **Later unification (not S40):** when a combined fire log is wanted, a view `UNION`s `trigger_fire` and `event_trigger_fire` through the AU12 mapping; neither table is rebuilt for it.

## AU8 — The dispatcher: one goroutine, a durable cursor, a claim before every start

Package `aeon/internal/automation` (new). The daemon owns the goroutine; the package owns the algorithm.

```go
type Dispatcher struct {
    Store       *store.Store
    Starter     Starter                       // daemon adapter: (*Daemon).StartFromEvent (AU9)
    ResolveRoot func(componentID string) (string, error) // S38 component read (VERIFY name)
    Clock       func() time.Time              // injected; tests never sleep
    Log         interface{ Infof(string, ...any) }
    PollEvery   time.Duration                 // default 1s; used only by Run
    BatchSize   int                           // default 100
}
func (d *Dispatcher) Run(ctx context.Context) error                 // Reconcile (AU13) once, then Drain on each tick until ctx ends
func (d *Dispatcher) Drain(ctx context.Context) (events int, err error) // one pass; the test entry point
func (d *Dispatcher) Reconcile(ctx context.Context) (Summary, error)    // boot-time recovery
```

**`Drain`**, in order:

1. `cursor := Store.AutomationCursor("dispatcher")`.
2. `evs := Store.ReadAutomationEvents(cursor, BatchSize)`, ascending id. Empty → return.
3. For each `ev`: `triggers := Store.ListEventTriggersFor(ev)` — every trigger with `source='event'`, `from_outbox_id < ev.id`, and
   `Match.Matches(ev)` and scope-compatible, **including disabled ones** (they get a `skipped('disabled')` row — "did not fire, and
   says why"), ordered by trigger id.
4. For each trigger run **`fire(ctx, trig, ev)`** (below). A returned error means *the store is failing*: stop the pass **without**
   advancing the cursor and return the error (logged, retried next tick). Per-trigger business outcomes are never errors.
5. After every trigger of `ev` is decided (or already claimed), `AdvanceAutomationCursor("dispatcher", ev.id)` — monotonic, never
   moves backward.

**`fire(ctx, trig, ev)`:**

1. `!trig.Enabled` → `RecordEventFire(claim+decide in one INSERT)` = `skipped('disabled')`; return.
2. **Claim:** `fire, fresh, err := Store.ClaimEventFire(...)` — `INSERT ... outcome NULL` against `UNIQUE(trigger_id, outbox_id)`. `fresh == false`
   (already claimed by an earlier pass or before a crash) → return; recovery owns open claims (AU13).
3. **Depth:** `ev.Depth > MaxAutomationDepth` → decide `refused('depth-exceeded: …')`; return.
4. **Overlap:** an earlier fire of this trigger on the same `(subject_kind, subject_id)` with `outcome='started'` whose loop is `trigger.StillGoing(status)` (running/waiting/blocked — S20's own rule, imported, not copied) → `skipped('overlap: loop <slug> is still <status>')`. (A goal re-approved after an edit while its first
   loop is still running does not start a second one; once that loop ends, it does.)
5. **Limits (AU11):** runs/hour, then cost/day → `limited('max-runs-per-hour: 5 started since <t>')` /
   `limited('max-cost-per-day: $x.xx known (floor) ≥ $y')`. The cost note (`cost-unknown`/`cost-floor`) is stored on whichever row is
   decided.
6. **Target:** `root := first non-empty of trig.ComponentID, ev.ComponentID` → `ResolveRoot`. Neither → `refused('no-target-component: the
   goal spans several components; set component on the trigger')`. Resolve error → `refused('workspace-invalid: …')`.
7. **Start:** `Starter.Start(ctx, StartRequest{...})` (AU9) with `Slug = AutoSlug(trig.ID, ev.ID)` and the depth `ev.Depth + 1`.
   - `*Refusal` (blueprint refused, S38 `requires:` unmet, workspace invalid, subject missing) → `refused(<reason>: <sentence verbatim>)`.
   - any other error → `could-not-say('start-error: …')` — a `Commit` error is ambiguous by nature; AU13 explains.
   - success → `DecideEventFire(started, loop_id, loop_slug)`.
8. Publish a UI notification (`trigger.fired`) is **not** part of this slice; the page reads `automation.trigger.fires` (AU14).

## AU9 — The start door: `StartFromEvent`, beside S20's `StartFromTrigger`

**What exists (S20, built):** `(d *Daemon) StartFromTrigger(ctx, trigger.Fire)` (`daemon/trigger_fire.go`) — the one door a clock/webhook fire
walks: it calls `d.Table.StartBlueprintLoop` with `Attended: &attended(false)`, a deterministic slug, then writes provenance **after** the
start via `Store.SetLoopProvenance`. **S40 reuses that door's shape and does not invent a second kind of start.** It cannot reuse the function
itself, for three measured reasons: its `Fire` type has no kickoff, no goal/task ids and no depth (and S20 grep-pins that `Fire` carries
no hints); it accepts only provenance `trigger`/`webhook`; and stamping provenance *after* `StartBlueprintLoop` returns is too late for the
kickoff — the closure has already called `d.loopRunner.Start`, so a first step could run **before** its first message exists.

So S40 adds, in a new file `daemon/event_fire.go`, a sibling `StartFromEvent(ctx, automation.EventStartRequest) (loopID string, err error)`
and makes **one narrow extraction** from the closure (`daemon/blueprint.go:315–394` — **corrected
2026-10-28 (T0-Q4)**: the file read as built puts the closure at `d.Table.StartBlueprintLoop = func…`
starting :315 and closing :394, not the 281–322 this line first said): its body moves, unchanged, into

```go
// internal/daemon/start_loop.go
type startExtras struct {          // unexported: no socket client can reach it (S20's "no parameter to smuggle" rule)
    StartedBy, TriggerID string     // written through store.NewLoop (fields exist, S20 R-3)
    Depth                int        // store.NewLoop.AutomationDepth (new)
    FirstMessage         *store.NewChatMessage
}
func (d *Daemon) startLoopWith(ctx context.Context, p protocol.LoopStartParams, x startExtras) (map[string]any, error)
```

The closure becomes `res, err := d.startLoopWith(ctx, p, startExtras{}); …` (same result map, errors and events — T4's byte-parity test),
and `StartFromTrigger` is **not edited** (S20 keeps working through the closure). `StartFromEvent` calls `startLoopWith` with
`startExtras{StartedBy: "event", TriggerID: req.TriggerID, Depth: req.Depth, FirstMessage: …}`, so provenance, depth and the first message are written
**in `CreatePinnedLoop`'s one transaction**, before the runner starts. `SetLoopProvenance` is not used.

- `StartFromEvent` mirrors `StartFromTrigger`'s guards: `attended := false` (local) → `Attended: &attended`; `AllowOutward` left `false`; slug
  `automation.AutoSlug(triggerID, outboxID)`; workspace validated by `validateLoopWorkspace`; a missing loop id in the result is an error.
- **Refusals** (`loop.PinRefusal`, S38's `requires:` refusal, workspace and kickoff errors) map to `automation.Refusal{Reason, Sentence}`;
  anything else is a plain error (→ `could-not-say`).
- **The first message** is `NewLoop.FirstMessage`, appended by `CreatePinnedLoop` through `appendThread(tx, …)` (`store/chat.go`) with
  `source = 'automation'`, `source_ref = <trigger id>` (S39 T8's `source`; `source_ref` is added by T1 if absent). A loop cannot exist
  without its kickoff, which deletes a row of the recovery table.

## AU10 — The kickoff

- Template precedence (S39 CR6): **trigger `kickoff`** → the blueprint's `kickoff:` default → the goal/task body (S39's fallback).
- Data: S39's `{Goal, Task, Component, Project, Params}` plus `Params["event"]`, `Params["trigger"]` (the name), `Params["depth"]`,
  and `Params["untrusted"] = "true"` when the subject goal's `Source.Untrusted` (S38 OL7) is true. The renderer (S39 T8) owns
  escaping and the data label; S40 only supplies the flag. **Needs human review** (brief §5).
- The rendered text becomes the loop's first chat message with `source = automation`; the thread shows *"started by trigger
  `<name>`"*. The model sees the same text it would see typed — the engine cannot tell, and does not need to (CR6).
- A template error at fire time (bad field) → `refused('kickoff-invalid: …')`; templates are also parsed at `automation.trigger.create` so the
  common mistake never reaches a fire.

## AU11 — Limits, and cost honesty

| Limit | Rule | Computed from |
| --- | --- | --- |
| `max_runs_per_hour` (required, 1–1000) | **hard**: if `count ≥ max` → `limited` | fires of this trigger with `outcome='started'` **or** open (`NULL`) and `claimed_at ≥ now − 1h`, excluding the fire being decided |
| `max_cost_per_day_usd` (optional) | see below | `EventTriggerSpend`: steps of loops this trigger started with `loop.started_at ≥ now − 24h` |

`EventTriggerSpend` returns `{USD float64, Floor bool, Unknown bool}` from one query: `USD` sums `cost_usd` over steps whose `cost_state` is
`known` or `partial`; `Floor` = any `partial`; `Unknown` = any step whose `cost_state` is `unknown` (or NULL — a running step with no
cost yet). **Unknown is never added as zero.** Decision table:

| `USD ≥ max`? | `Unknown`? | Result | `cost_note` |
| --- | --- | --- | --- |
| yes | any | **limited** `max-cost-per-day` — a floor that already reaches the cap proves it | `cost-floor` if `Floor` else `''` |
| no | no | proceed | `cost-floor` if `Floor` else `''` |
| no | **yes** | proceed on **runs/hour only**; the cap cannot be checked | **`cost-unknown`** |

The fire row says `cost-unknown` so an operator sees that the cost cap did not hold that time. Cost is **never estimated**.
`CostCap` as an S10 policy (S17's evaluator) stays the right place for a *per-step* token budget; this limit is the trigger's own
spend ceiling, the spec's `TriggerLimits`, and does not compete with it (S20 TR7's one-place rule is honoured by S17 owning enforcement
*inside* a loop, S40 owning the *rate of starting* loops).

## AU12 — The five outcomes and their reasons

| Outcome | Meaning | Reasons written (closed list; text after `:` is free) |
| --- | --- | --- |
| `started` | a loop exists | — (`loop_id`, `loop_slug` set) |
| `refused` | a rule said no; nothing started | `depth-exceeded`, `requires-unmet`, `blueprint-refused`, `no-target-component`, `workspace-invalid`, `subject-missing`, `kickoff-invalid` |
| `limited` | a limit held | `max-runs-per-hour`, `max-cost-per-day` |
| `skipped` | nothing to do, by design | `disabled`, `overlap` |
| `could-not-say` | we cannot prove whether it started | `start-error`, `trigger-edited-since-claim` |

**Mapping onto S20's vocabulary (`trigger.Outcome`, `internal/trigger/record.go`).** The two tables stay separate but the words must agree
(P31 / TR4: *fired · did not fire · could not say*). `automation.S20Outcome(o string) (trigger.Outcome, bool)` is total over the five and tested:

| S40 outcome | S20 `trigger.Outcome` | Why |
| --- | --- | --- |
| `started` | `OutcomeFired` | a loop exists |
| `limited` | `OutcomeDidNotFire` | S20's "did not fire, and says why" (window/paused); a limit is the same kind of reason |
| `skipped` (`overlap`) | `OutcomeSkipped` | S20 TR5 verbatim |
| `skipped` (`disabled`) | `OutcomeDidNotFire` | S20: paused = did not fire; the code stays `skipped` here because the author's list names it |
| `refused` | `OutcomeRefused` | a rule said no |
| `could-not-say` | `OutcomeCouldNotSay` | TR4's third answer |

If the author prefers one vocabulary today, it is a rename of the five strings in one const block and the `CHECK` of `event_trigger_fire` (a
new table, so no rebuild risk) — recorded, not done.

A test asserts every reason string used in code is in the table, and `automation.trigger.fires` returns the outcome counts so the UI shows all five
(zero is shown as zero, not hidden).

## AU13 — Crash recovery: claim, then start; reconcile at boot

The only crash windows are *after the claim, before the loop commit*, and *after the loop commit, before the fire is decided*. The
slug `auto-<trigger id>-<outbox id>` is deterministic and `loop` is `UNIQUE(project_root, slug)`, so a second loop for one fire cannot
exist even if reconcile were buggy.

`Reconcile` runs **once, before the first `Drain`**, over `OpenEventFires()` (outcome NULL):

| # | Found | Action |
| --- | --- | --- |
| 1 | a loop with the fire's slug exists in the resolved workspace | `started(loop_id)` — complete the record. (The loop's runner is already restarted by `Daemon.StartLoops`; a loop row is written together with its kickoff, AU9.) |
| 2 | no such loop, trigger now **disabled** | `skipped('disabled')` |
| 3 | no such loop, trigger `updated_at > fire.claimed_at` (edited since the claim) | `could-not-say('trigger-edited-since-claim')` — the definition that justified the claim is gone |
| 4 | no such loop, trigger unchanged | **re-run `fire` from step 3** (depth) with the existing claim — safe because the start is idempotent by slug; its outcome is recorded normally |
| 5 | the fire's outbox row or trigger row is missing | `refused('subject-missing: …')` with both ids in the reason (nothing can be started or proven) |
| 6 | `Starter.Start` returned a non-refusal error during a normal fire | `could-not-say('start-error: …')`, **not** retried: a `Commit` that errored may have committed; an operator reads the loop list and re-fires by editing or re-approving |

A claim is never deleted and never re-claimed. After `Reconcile`, `Drain` resumes from the **cursor**, which may be behind some
events whose fires are already claimed: step 2 of `fire` (`fresh == false`) makes the replay a no-op.
**P31:** the third answer exists so that an event the daemon was down across is distinguishable from one that never fired.

## AU14 — The surface (P34: API first)

| Method | Kind | Params → Result |
| --- | --- | --- |
| `automation.trigger.list` | read | `{componentId?, projectId?, enabled?}` → `{triggers: EventTriggerView[]}` (each with `lastFire?`) |
| `automation.trigger.show` | read | `{id}` → `EventTriggerView` (+ canonical `match`) |
| `automation.trigger.fires` | read | `{triggerId?, outcome?, limit?, beforeId?}` → `{fires: EventFireView[], counts: {started,refused,limited,skipped,could-not-say}}` |
| `automation.trigger.create` | **human write** | `{name, projectId?, componentId?, match, blueprint, kickoff?, maxRunsPerHour, maxCostPerDayUsd?}` → `EventTriggerView` |
| `automation.trigger.update` | **human write** | `{id, …any of the create fields, clearMaxCost?}` → `EventTriggerView`; bumps `updated_at` |
| `automation.trigger.enable` / `automation.trigger.disable` | **human write** | `{id}` / `{id, reason}` → `EventTriggerView` (disable requires a reason sentence) |

- Writes derive the actor from the peer (S38 T15's stamp); an actor of kind `agent` gets `ErrBadParams`-class refusal
  *"triggers are written by a person; this actor is an agent"*. The actor id is stored in `created_by_id` (create only; update does not
  change authorship).
- `create` validates: match grammar (AU6), blueprint resolvable (`blueprint.Resolve`) and **valid** (`Validate` with production
  registries — same call as `loop.Start`; its diagnostics are the refusal), scope ids exist, limits in range, kickoff template parses
  against the S39 data shape.
- Additive protocol only (new methods): `PROTOCOL_VERSION` does **not** bump. Every method has a conformance fixture; `make protogen`
  regenerates `ui/src/proto/contract.ts`; the app's hand-written types live in `ui/src/lib/protocol.ts`.
- **The page** is a bench page (S34/S35 precedent): trigger table (name, event, blueprint, enabled, last fire outcome), enable/disable
  buttons (disable asks for a reason), and a fire log with the five outcomes as distinct pills and their reasons verbatim. A plain **JSON textarea** posts `automation.trigger.create` and shows
  the daemon's refusal sentence verbatim (no designed form — a follow-up); there is no delete, approve, run-now or retry control.

## AU15 — Disabling is exact

`disabled` means: matching events are recorded as `skipped('disabled')` (not replayed when the trigger is enabled again — the
cursor has passed them and a replay would start agents on stale news). `enable` sets `enabled = 1`; events that occurred while
disabled stay `skipped`. A trigger created later never sees the backlog (`from_outbox_id`).

## AU16 — What stays out, named so it is not lost

**S41:** `ApprovalPolicy`/auto-approval (the `review.by_kind` widening), `spawn`/`wait`, config inheritance (`ParamDef`), project-scope
loops. **S20 (built):** its schedule/webhook sources, `DedupeKey`, `SecretRef` and input mapping stay S20's; the `TriggerAccess` evaluator (S20 T7, `Gate.Access`) is not wired yet — S40's fire sequence leaves a slot between *limits* and *target* for it; T5 leaves a `// S20 T7: TriggerAccess evaluates here` comment, no code.
**Later:** outbox trimming (consumed rows older than N days), a trigger editor in the app, `trigger.fired` UI events, per-trigger
attendedness (brief §0.1), more events (`goal.rejected`, `task.rejected`), trigger archive.

## Test map

1. rolled-back `DecideGoal` / `CreateTask` / approval / completion leave **no** `automation_event` and no domain row (D1)
2. a loop task create/approve/complete writes no outbox row; the DB trigger aborts a hand-forged insert; the Go helper refuses (D2)
3. closed set: DDL `CHECK` ≡ Go `AutomationEvents()`; an unknown event cannot be inserted (D1, D9)
4. match grammar: accepts the five keys; refuses unknown keys, wrong-event keys, empty values, regex-looking values taken literally; canonical form stable (D9)
5. trigger store: create/update/enable/disable; `created_by_kind` check; agent actor refused before SQL; `updated_at` bump (D3)
6. human-only: no `trigger*` tool name; no source under tools/records/narrow references a trigger write; agent actor refused on all four socket writes (D3)
7. `startLoopWith`/`StartFromEvent`: socket closure byte-identical result; event start → `attended = 0`, `allow_outward = 0`, `started_by = 'event'`, `trigger_id`, depth, first message `source = automation` with trigger id, all in one tx (D4, D11)
8. dispatcher: goal.approved starts one loop; same event twice → still one (UNIQUE); disabled → `skipped(disabled)`; no backlog for a new trigger (D5, D6)
9. depth: a four-deep chain stops at depth 4 with `refused(depth-exceeded)` (D8)
10. limits: runs/hour hard; cost known / partial-floor / unknown cases and `cost_note` (D7)
11. overlap: second fire while first loop runs → `skipped(overlap)`; after `done` → starts (D6)
12. recovery: crash after claim (no loop) → re-run; crash after loop commit → `started`; edited trigger → `could-not-say`; disabled → `skipped` (D5)
13. surface: seven fixtures round-trip; `check-protogen` clean; page renders five outcomes (D10)
14. zero-trigger daemon: suites unchanged; socket `loop.start` shape unchanged (D11)
