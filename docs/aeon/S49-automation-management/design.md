# Design — S49 Automation management

Written 2026-10-30. Rulings **AN1–AN15**. AN2 is an operator-visible **exception to rule 6**; AN3 closes a real authority gap; the rest are documented technical calls, checked against the code (brief §1; T0 re-verifies).
Do not re-open a ruling without recording the reason in that task's Notes. S20's TR1–TR12 and S40's AU1–AU16 stand unless a ruling below says it *extends* one.

## Verified facts (2026-10-30; T0 re-checks)

1. **Two families, two surfaces.** S20: `trigger_schedule` (migration 0032, +`workspace_root`,`goal` in 0033) and `trigger_fire`; `trigger.list/pause/resume/fireNow/history/add` (`method.go:122-134`; handlers `internal/protocol/trigger.go`); CLI `aeon trigger` (`list|pause|resume|fire-now|add|history`, `cmd/aeon/trigger.go:37-49`; `add` flags: id, kind schedule|webhook, blueprint, cron, timezone, workspace, goal, window-start/end/days, secret-env). S40: `event_trigger`, `event_trigger_fire`, `automation_event` (outbox), `automation_cursor` (0039); `automation.trigger.list/show/fires/create/update/enable/disable` (`method.go:196-202`); **no CLI verb references them** (grep of `cmd/`).
2. **AU1's three locks cover S40 only.** (1) no agent tool; (2) socket refuses an actor of kind `agent`; (3) `event_trigger.created_by_kind CHECK (created_by_kind = 'user')`. `handleTriggerAdd` takes `ctx, _ *Session, raw` — **no actor** — and `trigger_schedule` has **no creator column**; schedule edits write no audit.
3. **Authority semantics** (S40 AU2, S20 TR2): every event/schedule start is **unattended** with `allow_outward = false`, declared at start and never inferred (`StartFromEvent`/`StartFromTrigger`). `loop.started_by` ∈ `'trigger','webhook','event'` and `loop.trigger_id` record provenance (0033, 0039).
4. **Never deleted** (AU1): disable with a reason; an archive was named a follow-up (AU16). Disabled ⇒ matching events are recorded `skipped('disabled')` and **not replayed** on re-enable (AU15, `from_outbox_id`).
5. **Closed vocabularies:** events (4), match keys (6) with applicability rules, outcomes (`started|refused|limited|skipped|could-not-say`, AU12, mapped from S20's `trigger.Outcome`), limits (`max_runs_per_hour` 1–1000, `max_cost_per_day_usd`), five-field cron + explicit IANA zone (TR6) + optional working-hours window (TR8).
6. **A trigger names a blueprint and always follows latest** (AU7); creation validates it resolvable and `Validate`-clean (AU14). Today that resolution is `blueprint.Resolve` (files) — S48 moves it to rows. Nothing reports a blueprint that has *since* become invalid or been deleted.
7. **Webhook secrets** are env var **names** (C17); TR9–TR11 (HMAC, untrusted payload, enumerated narrow-surface projection) are S19/S20's.
8. **The UI** is `AutomationPage.tsx` in the bench (a JSON textarea posting `automation.trigger.create`; no schedule UI).
9. **After siblings:** S48 supplies blueprint rows/`Analyze`/`BlueprintReferences` (which already names schedules, event triggers and non-terminal queue items); S45 supplies `internal/desk`, the generic `proposal.commit` and UI components; S47's surface table must classify the new methods.

## AN1 — One Automation view; the old methods stay

A unified key `{source: "schedule"|"webhook"|"event", id}` (ids are TEXT in both tables). New read methods answer over both families; `trigger.*` and `automation.trigger.*` keep their names, shapes and semantics (compat, bench, CLI). No table is merged or rebuilt (AU7: "neither table is rebuilt for it"). The unified **fire log** is a SQL **view** over both fire tables through AU12's outcome mapping (AN5).

## AN2 — Triggers are never seeded (an exception to rule 6) *(operator-visible)*

Rule 6 says system records are seeded. A trigger is **standing authority to start agents with no one present**, and S40 AU1 lock 3 makes `created_by_kind = 'user'` a database CHECK: the product may not author one. A seed is the product authoring one. So **triggers are records in the database (they already are) and are exempt from seeding**: no `automation.*` seed set is declared, `origin` is not added, and a guard test asserts no seed applier writes to `trigger_schedule` or `event_trigger`. *Overruling means first amending AU1 lock 3 (a CHECK rebuild plus a decision about whose authority a shipped trigger carries) — a larger, riskier change than a management slice; not recommended.* CONFIGURATION-ORDER gets a one-line note.

## AN3 — Schedules and webhooks get the same authority locks as event triggers *(closes a gap)*

Extend AU1 to the S20 family, three locks, each tested: **(1) no agent tool** for any trigger write (S40's guard, extended to the schedule methods); **(2) socket** — `trigger.add/update/pause/resume/fireNow` take the stamped actor (`table.ActorFor`), refuse an actor of kind `agent` and an unresolvable peer, and accept **no** actor/creator/origin param; **(3) store** — `trigger_schedule` gains `created_by_kind TEXT NOT NULL DEFAULT 'user' CHECK (created_by_kind = 'user')` and `created_by_id TEXT NOT NULL DEFAULT ''` (empty = "predates S49's provenance", **never** a fabricated id) by `ALTER`; the Go write function takes a `store.Actor` and refuses a non-`user` kind before any SQL. The CLI's `{user,cli}` actor is the existing human door for the offline fallback. *This changes `trigger.add` from open-to-any-peer to person-only; no agent or script on the narrow surface could ever reach it, so the observable change is for a sandboxed process holding the 0600 socket.* `pause`/`disable` **narrow** authority but stay human writes in this slice (an agent's route to narrowing is the proposal, AN9) — one rule, one audit.

## AN4 — `automation_change`: every authority change, audited once, append-only

New table `automation_change(id, source, trigger_id, op ∈ ('create','update','enable','disable','pause','resume','fire_now','archive','unarchive'), summary, actor_kind, actor_id, occurred_at)`, append-only by trigger (the `setting_event`/`policy_change`/`mcp_change` precedent), no FK (history outlives archive). **Every** write on both families — including the pre-existing ones — writes **one** row in the same transaction as the write (T4 threads a tx-variant through the existing store functions); none for reads, refusals, dry-runs or no-ops. `summary` is a fixed template from ids and enums only (`"created event trigger %s (event %s → blueprint %s, ≤ %d runs/h)"`), never raw user text (SEC-033). `fire_now` records *who pressed it* and the resulting fire id; the existing `trigger_fire` row remains the record of what happened.

## AN5 — The unified fire log is a view

`CREATE VIEW automation_fire_view AS` the UNION of `trigger_fire` (source `schedule`/`webhook` by the trigger's kind) and `event_trigger_fire` (source `event`) with columns `source, trigger_id, at, outcome, reason, loop_id, loop_slug, subject_kind, subject_id, depth, cost_note`, outcomes mapped by AU12's table (T0 records the exact S20→AU12 mapping and the `could-not-say` range semantics). A view has no write path and no new truth; a differential test compares it to both tables.

## AN6 — Archive, not delete

`ALTER TABLE trigger_schedule ADD COLUMN archived_at TEXT, ADD archived_reason TEXT NOT NULL DEFAULT ''` and the same on `event_trigger`. `archive {source,id,reason}`: reason required; sets the engine-visible off state (`paused` for schedules, `enabled=0` + `disabled_reason` for events) **and** `archived_at`, so **the clock and dispatcher need no change** (they already skip paused/disabled — T0 confirms exactly which columns each reads and the test asserts an archived trigger never fires). Default lists exclude archived (`includeArchived`); archived rows keep their fire history and audit. `unarchive` clears `archived_at` and leaves the trigger **disabled/paused** — re-enabling is a separate, deliberate authority act (AN3). Archiving a trigger an in-flight loop was started by does not touch that loop.

## AN7 — Health: what would happen if it fired right now (a read, never a gate)

`automation.health` computes, per automation and at an injected instant: **blueprint** `ok | missing | invalid | unverifiable` (S48 source + the same `Analyze` call `loop.start` uses; the `Report` verdict counts), **webhook secret** `{envName, set: bool}` (presence of the named variable in the daemon's environment — **never the value**), **workspace** exists/not (the loop's own workspace rule is unchanged), **scope** component/project exist (S38 read), and `canFire` = all of the above ok **and** enabled/unarchived. It **changes nothing** about whether a trigger fires — the engines keep their own run-time checks and outcomes (`refused` with their reason); health is the *advance warning* the author never had. Pure adapters over S48/S38 reads; no cache.

## AN8 — Previews are pure and read-only

`automation.preview {schedule spec | id, n≤20, from?}` → the next *n* fire instants computed by **the scheduler's own next-fire function** (wrapped by a thin pure function in `internal/trigger` if none is exported; no behaviour change) — zone, DST gap ("skipped") and overlap ("fires once", TR6) and the working-hours window included, each instant labelled with any rule that moved it. `automation.test {match, sample}` → the **real matcher** (AU6) applied to a hand-written sample event, with a per-key result ("`goal_type`: sample `bug` ∈ [feature, bug] ✓"). `automation.dryrun {id, sinceOutboxId?, limit≤200}` → the outbox events since a point that **would match** this trigger now (read-only scan of `automation_event`), **never** claiming they fired and never inserting a claim. All three write nothing.

## AN9 — Agents read, and may propose only to *narrow*

Catalog specs (family `automation`): reads `automation_list`, `automation_show`, `automation_fires`, `automation_health` (triggers are not private between agents; there is no MC2 analogue) and **`automation_propose_disable`** (class detectable, **Mutates**): an inert proposal of a **new kind `automation_disable`** `{source, id, reason}`; `Validate` = exists, enabled, not archived; `Apply` = the T4 disable/pause path with the **committing person** as actor. **No tool creates, enables, updates, archives or fires**, and **no proposal kind widens**: an agent cannot even propose new authority (the stricter reading of AU1, as for MCP). The kind addition is a table rebuild (S37/S46/S48 precedent; T6 reads the **current** DDL). Schemas carry no identity/status fields; per-agent open-proposal limit; registered-not-granted; counted bump.

## AN10 — Methods (all `RequiresInitialize`, none `Debug`, conditional on the seam, off the narrow surface)

Reads: `automation.list {source?, enabled?, archived?, blueprint?, rootId?, componentId?, q?}` → `{automations: AutomationView[], statement?}`; `automation.show {source,id}` (view + canonical match/schedule + description + health summary + last 5 fires); `automation.fires {source?, id?, outcome?, limit?, beforeId?}` (the view; counts per outcome); `automation.runs {source,id,limit?}` (loops started: id, slug, status, startedAt, from `loop.trigger_id`); `automation.health {source?, id?}`; `automation.vocabulary {}`; `automation.preview`, `automation.test`, `automation.dryrun`; `automation.changes {source?, id?, limit?}`.
Writes (**human door**): `trigger.update {id, …}` (new, schedules/webhooks; every add field except `id`/`kind`; secret env name only), `automation.archive {source,id,reason}`, `automation.unarchive {source,id}`; the existing writes `trigger.add/pause/resume/fireNow` and `automation.trigger.create/update/enable/disable` keep their names and shapes but gain the AN3 locks and the AN4 audit.
`AutomationView{source, id, name, description (AN11), enabled, paused, archived, blueprint, blueprintState?, scope{rootId, componentId}, schedule?{expr, zone, window?, nextFire?}, webhook?{secretEnvName}, event?{event, match}, limits?{maxRunsPerHour, maxCostPerDayUsd}, lastFire?{at, outcome, reason}, createdBy, createdAt, updatedAt}`.

## AN11 — One description formatter

`automation.Describe(view) string` builds the one-sentence description from canonical rows with fixed templates and `%q`-quoted values (schedule: expression rendered in words for the common cases, else the raw expression; zone; window; blueprint; workspace; goal. webhook: path/secret env **name**. event: event, match keys in AU6's canonical order, scope, limits). Used by list/show/CLI/UI/tools — never assembled client-side.

## AN12 — The CLI: `aeon automation`, and `aeon trigger` kept

`aeon automation list|show|fires|runs|health|changes|vocabulary|preview|test|dryrun` (reads) and `add-schedule|add-webhook|add-event|update|enable|disable|pause|resume|fire-now|archive|unarchive` (writes; `enable`, `fire-now`, `unarchive` and `add-*` with `--enabled` need `--yes` — a seatbelt for a typo, not a security control). `aeon trigger …` is **unchanged** in flags and output (goldens) but its verbs are now clients of the same methods. Offline fallback through the same code (S45 PM4 pattern); webhook secrets by `--secret-env NAME` only (C17: no value flag exists).

## AN13 — The UI: Settings → Automation, with the daemon's statement of authority

One list over both families; forms generated from `automation.vocabulary`; a **blueprint picker** (S48 list with the health dot), **scope pickers** (`component.list`/`component.show` — the S44-extra tree), a cron field with **live next-fires** (`automation.preview`), a timezone select from the vocabulary, an event-trigger **match builder** (event ⇒ only its applicable keys, task types from the vocabulary) with a **sample test** (`automation.test`) and a "would have matched" count (`automation.dryrun`). **Enable/fire-now/unarchive/add-enabled confirm with the daemon's own authority statement** (`vocabulary.authorityStatement`: "this starts agents with no one present; outward actions are denied (G14); it will run <description>") plus the health result — the UI invents no text. Fire log with the five outcomes as distinct pills and reasons verbatim; archive with a reason; audit tab. Real Settings screen; the bench page stays.

## AN14 — The invariants this slice must not move

Attendedness and outward: every start stays `attended=0, allow_outward=0` (a test re-runs S40 T4's pinned-row assertion and the grep that the only `Attended:`/`AllowOutward:` assignments are `&unattended`/`false`); AU3's closed events and the manual-only DB trigger; the outbox transaction rule; AU5's cascade guard; AU8's claim-before-start and durable cursor; AU15's no-replay; S20's TR3–TR8; the dispatcher and clock code.

## AN15 — What does not move

The engines, the webhook receiver and narrow surface, event/limit/outcome vocabularies, the blueprint engine (S48), policy/MCP/tools (S45–S47), `trigger.*`/`automation.trigger.*` wire shapes (additive only), and every sibling-slice file beyond the adaptations T4 lists (the existing write paths gaining actor, lock and audit).

## Open items left to T0 — mechanical

1. Re-verify facts 1–9; the AU1 guard test names/locations and a hash of each (T5 adds siblings).
2. **Write inventory**: every function that mutates `trigger_schedule`, `event_trigger` or their engines' state (store, `internal/trigger` registrar, `internal/daemon` seams, handlers), with file:line, to thread the actor, the locks and the audit tx (T4).
3. **State columns**: exactly which columns the clock reads (paused/enabled/…) and the dispatcher reads (`enabled`), to make archive engine-invisible (AN6); `trigger_schedule` full DDL.
4. **Outcome vocabularies** of both fire tables and AU12's mapping (AN5), incl. `could-not-say` ranges.
5. The scheduler's **next-fire function** and the matcher's entry points (AN8): exported? signature? DST helpers.
6. **CLI parity table**: every `aeon trigger` verb/flag and every socket method of both families → twin or gap (event triggers: all gaps).
7. **S48/S38 state**: blueprint source/`Analyze` entry points and `BlueprintReferences`; S44-extra's `component.list`/`component.show` for the pickers; S45 desk/commit/UI; S47 surface table; current `proposal.kind` DDL.
8. Error-code block, handshake/conformance/fixtures/protogen touch points, `.sql` trigger-body support, a baseline of affected packages.
