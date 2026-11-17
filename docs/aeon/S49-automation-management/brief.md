# Brief — S49 Automation management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 2 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md), after blueprints because a trigger *fires a named blueprint*.
**Depends on:** S20 (schedules and webhooks), S40 (event triggers, outbox, dispatcher), S48 (blueprint rows, `BlueprintReferences`, validation), S45 (the guard pattern, desk/commit, UI components), S47 (surface classification),
S43 (agents), S38 + S44-extra (the component tree). T0 re-checks what has merged. S44/S46 are not dependencies.

## 0. The standing rules this slice carries (CONFIGURATION-ORDER, 2026-10-30) — and the one it makes an explicit exception to

1. **Daemon first, CLI second.** Event triggers have **no CLI at all**; schedules have a partial one.
2. **Every management operation gets an agent-visible tool; where it widens a guard it is propose-only and restrictive-only.** A trigger is *standing authority to start agents unattended* — S40 AU1 says only a person writes one — so
   agents get **reads and a propose-to-disable**; they cannot create, enable or change authority, **not even by proposal**.
3. **A list with details**: what runs when, what it starts, whether its blueprint still works, what happened the last N times, what it would do next.
4. **Every record is a row.** Triggers already are. **The seed half of rule 6 does not apply here, deliberately:** S40 AU1 lock 3 is `event_trigger.created_by_kind CHECK (= 'user')` — *no non-person may author a trigger*, which a seed would be.
   So **no trigger is ever seeded** and no seed set is declared (design AN2, a flagged exception). Overruling means amending AU1 lock 3 first.

## 1. What exists today, measured (2026-10-30, `aeon/` at S43's merge)

| Fact | Evidence |
| --- | --- |
| **Two disjoint families.** S20: `trigger_schedule` (`kind` schedule/webhook, cron, **zone**, workspace, goal, window, secret env **name**) + `trigger_fire`; socket `trigger.list/pause/resume/fireNow/history/add`; CLI `aeon trigger list|pause|resume|fire-now|add|history`. S40: `event_trigger` + `event_trigger_fire` + the `automation_event` outbox; socket `automation.trigger.list/show/fires/create/update/enable/disable`; **no CLI verb**. | `method.go:122-134,196-202`, `cmd/aeon/trigger.go:37-49`, `store/schema.go` 0032/0033/0039 |
| **Unequal authority guarding.** S40 enforces "only a person writes one" three ways (no agent tool; the socket refuses an actor of kind `agent`; `event_trigger.created_by_kind CHECK = 'user'`). S20's `trigger.add` handler takes **no session and no actor** (`handleTriggerAdd`, `internal/protocol/trigger.go:191`), `trigger_schedule` has **no creator column**, and schedule edits have no audit. | the three cited files |
| **No update for schedules, no delete/archive for either.** AU1: "a trigger is never deleted — disabled, with a reason" (an archive was named a follow-up). | `S40-automation/design.md` AU1, AU16 |
| **A trigger names a blueprint and "always follows latest".** If the blueprint is later edited into an invalid state, or deleted, the next fire **refuses** at run time; nothing tells the author beforehand. | AU7 (`blueprint TEXT NOT NULL`), AU9 |
| **Webhook secrets:** the row stores an **env var name** only (C17); the HMAC secret value never touches the store. | `cmd/aeon/trigger.go:99` |
| **Event match grammar** is closed (AU6): keys `event, root, component, goal_type, task_type, source_kind` (`project` renamed `root` by S44-extra CT11); four events (`goal.approved`, `task.created`, `task.approved`, `task.completed`); applicability validated at write. | `S40-automation/design.md` AU3, AU6 |
| **The UI** is a bench page (`AutomationPage.tsx`) with a **JSON textarea** posting `automation.trigger.create` ("no designed form — a follow-up", AU14/AU16). | `test-bench/pages/AutomationPage.tsx` |
| **Nothing answers** "when will this next fire?", "would this event have matched?", "does its blueprint still validate?", "what did it start?". | — |

## 2. What this slice is

1. **One Automation view over both families** (`source: schedule | webhook | event`): list with details, show, a **unified fire log** (a SQL view — the "later unification" AU7 named), the loops each started, and a **health** read (blueprint present and valid *now*, webhook secret env set, workspace exists).
2. **Close the authority gap on schedules**: the S40 locks extended to every write on both families — actor stamped from the peer, an `agent` actor refused, a **creator and an append-only audit of every change** (`automation_change`).
3. **The missing management operations**: `trigger.update` (schedules/webhooks), **archive/unarchive** for both families (terminal-ish, reversible, audit-preserving — AU1's "never deleted" honoured), and read-only **previews**: the next N fire instants (DST/window/zone rules from the existing clock) and an event **match test** (does this sample event match; which recent outbox events *would have*).
4. **Descriptor-driven forms** from a daemon `automation.vocabulary` (events, match keys and their applicability, task types, limits, windows, timezones), one formatter for every trigger's **description sentence**.
5. **Agent tools**: reads and a **propose-to-disable** (new proposal kind `automation_disable`); nothing that creates, enables, updates or archives.
6. **The CLI as a client**: `aeon automation …` (new, covers both families and the missing event-trigger verbs), `aeon trigger …` kept; offline fallback through the same code.
7. **Settings → Automation**: unified list, forms with blueprint/scope pickers (S48/S38), next-fires and match-test previews, fire log with the five outcomes, health, archive, audit.

## 3. Out of scope

- **The engines**: the clock, DST/overlap/reconcile logic (S20), the dispatcher, cascade guard, limits, kickoff (S40). No new event, match key, outcome or limit type. No change to attendedness (`unattended`, `allow_outward=false` — AU2/TR2 stay exactly as built).
- **Seeding triggers** (AN2) and **authoring blueprints/policy/agents** (S48/S45/S43) — this slice links to them.
- **Deleting a trigger** (archive only) and **replaying missed events** (AU15).
- **The webhook receiver** and its signature check (S19/S20/TR9–TR11), the narrow surface's enumerated trigger actions.
- Changing S43–S48 beyond the listed adaptations.

## 4. Open questions — decided in `design.md`, flagged where the operator may overrule

1. Are triggers ever seeded? — AN2 *(exception to rule 6; overrule ⇒ amend AU1 lock 3)*.
2. Do schedules get the same human-only authoring as event triggers? — AN3 *(closes a real gap; changes `trigger.add`'s open door)*.
3. Can agents propose anything? — AN9 (only to disable).
4. Archive vs delete — AN6 (archive; AU1 honoured).

## 5. Acceptance criteria

1. **Unified and complete**: `automation.list` returns every schedule, webhook and event trigger (archived excluded by default) with source, state, blueprint, scope, next fire (schedules), last outcome, health; `automation.fires` is one log over both fire tables via a view.
2. **Authority is human-only on every write of both families**: `trigger.add/update/pause/resume/fireNow`, `automation.trigger.create/update/enable/disable`, `automation.archive/unarchive` refuse an `agent` actor and an unresolvable peer, accept params carrying no actor/creator, and record the creator; a test walks every write method against the same three locks (extended to schedules).
3. **Every change is audited once**: each write (including the pre-existing ones) appends exactly one `automation_change` row naming the peer, before/after summary (fixed-template, ids only); none for reads, refusals or no-ops; append-only.
4. **Never deleted**: no delete exists; archive preserves rows and fire history; an archived trigger **never fires** and is excluded by default; unarchive restores it **disabled**.
5. **Health is honest**: for each automation, blueprint `ok | missing | invalid` (with the `Report` verdict) is computed from S48 rows now; a webhook's secret env presence is `set | unset` (never a value); a missing workspace is reported; none of these changes whether the trigger fires (a read).
6. **Previews are pure and read-only**: next-N fire instants equal what the clock would fire (differential test against the real scheduler function incl. DST gap/overlap and windows); the match test equals the real matcher; neither writes.
7. **Schedule update** validates exactly like add (cron, zone, window, blueprint resolvable+valid via S48), preserves identity and history, and bumps `updated_at`; a webhook secret is changed by changing its env **name**, never by sending a value.
8. **Agent tools**: reads open; `automation_propose_disable` creates an inert `automation_disable` proposal committed by a person via S45; no agent tool or proposal can create, enable, update or archive; absent from the narrow surface (guards with canaries).
9. **Daemon first**: every `aeon automation`/`aeon trigger` verb has a socket method (parity guard); the writes are absent from the narrow surface, agent services and tool registry.
10. **UI**: unified list/forms/previews/health/fire log/archive/audit through registered socket methods only (P34 grep as a test).
11. **No regression**: S20/S40 suites unchanged except the listed adaptations; a started loop's attendedness is unchanged (`attended=0`, `allow_outward=0` for event/schedule starts).

## 6. Handoff status (2026-10-30, planning session)

**Done:** this brief, `design.md` (AN1–AN15 + verified facts), `plan.md`, `tasks/T0.md`–`T10.md`.
**Not started:** all implementation. Run T0 first (no code). **Blocked on:** S48 (blueprint rows and validation) for the health read and blueprint picker; S45 (desk/commit/UI) for T6/T9; S47 for surface classification (T5).

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).

**Built 2026-11-04 (T0–T10; close-out merged, main tip `f66c1dcf`).** All eleven tasks built in this slice's
worktree and pushed to main; the implementation lives in `internal/automationview`, `internal/trigger`,
`internal/automation`, `internal/store` (`automation_mgmt.go` + three migrations), `internal/protocol`
(`automation_read.go`/`automation_advice.go`), `internal/daemon` (`automation_read.go`/`automation.go`/
`automation_health.go`), `cmd/aeon` (`automation.go` verbs + `automation_guard_test.go`), the catalog and
registry, and `ui/src/features/settings/automation/` (13 sources + guard) — per-task locations in each
task's Notes. Accepted against the eleven criteria (T10 Notes, dated 2026-11-04, each row naming its
tests); the demo walked on a real daemon over a throwaway store (DEMO §S49, spill incident told honestly);
the migration drills ran including the live append-only refusals. **Validation gaps kept open here, not
smoothed:** no desktop click-through or D15 (this host has no Tauri system libraries — the UI is witnessed
by its 46 vitest tests + the 24-door guard inside the 1332-pass suite); the full `internal/daemon` suite
did not finish under sibling test load (targeted automation/trigger/proposal/supervision sets green);
`-race` is unrunnable on four packages at process startup (TSAN `errno 12`, host mmap; automation's own
five packages race-green). Engines, attendedness and outward rules re-pinned untouched; rule 6's named
exception (never seed a trigger) shipped in `aeon/AGENTS.md` §4 beside the other door rows.

