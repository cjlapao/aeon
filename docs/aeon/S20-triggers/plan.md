# Plan — S20 Triggers

> **Source material:** [`design.md`](design.md) · the roadmap's [§4](../ROADMAP.md) · S19's action table
> and N15 · G14's attendedness pin in [`../S11-permissions/design.md`](../S11-permissions/design.md).

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 4 · Space shuttle, third of six. Depends on **S19**, **S10**.

---

## What is settled before planning

**The safety story is inherited.** G14 pins attendedness at loop start and S11 closed the inference path,
so a clock-started loop is unattended by construction and its first outward action suspends. **There is no
safety task in this plan** — only a test that the inheritance still holds.

**The surface is inherited.** S19's table has `trigger`, and already decided a webhook is a mounting rather
than a third surface. **No listener task.**

**The cap is inherited.** `CostCap` is S10's. **No cap task.**

**The library is free and the service is unusable.** `robfig/cron/v3` is already in the tree;
`framework/jobs` hard-depends on `redis`, which breaks P10. **No dependency task, and no upstream request.**

**So this plan is four scheduling questions, an HMAC check, and the wiring.**

## Reality alignment — checked 2026-10-27, before T1

The same audit S18 and S19 ran: every assumption in this plan and the nine task files, against the tree.
Thirteen findings (numbered **R** here, so they do not collide with the walk's F-numbers); none changes a
decision, all change what the tasks *type*.

- **R-1 · `TriggerAccessSpec` ships as `{"trigger","state"}` — not `{kind,state}`.** The stub at
  `policy/spec_stubs.go:107` carries `Trigger string`, and the pinned shape string a refusal quotes says
  `{"trigger": string, "state": "allow"|"deny"}`. T7 evaluates the shipped field; the task file's
  "{kind, state}" and the demo's `'{"kind":"webhook",...}'` are amended at T9, not the schema.
- **R-2 · The current TriggerAccess evaluator is the D10 stub — refusing to decide, not denying.**
  `stubEvaluator` answers Unimplemented (T11's could-not-determine), and the `Stub()` marker drives the
  in-force answer automatically. T7's replacement follows the **CommandAccess precedent** recorded in
  AGENTS §4d: delete the method *and* its registration in the same diff, and the in-force answer moves on
  its own. "Replace S10's refusing stub" (the task file) means precisely that diff.
- **R-3 · No loop provenance column exists.** `step_message.provenance` (0027) is S18's per-turn field, not
  a loop origin. `started-by: trigger` (T2 scope 5, demo §1) needs **migration 0033** — ALTER loop ADD
  `started_by`/`trigger_id`, 0031's NOT NULL DEFAULT discipline (a pre-S20 loop was started by a person,
  and that is the honest reading of absence) — plus a `--started-by` filter on `loop list`, which does not
  exist today either.
- **R-4 · What the stub answers today is "could not determine", and what T7 ships is default-deny.**
  With PL5 (`Evaluate` denies all types on an empty set), a fresh install with no TriggerAccess **allow**
  row fires nothing — schedules included. The task file's note requirement stands and gets louder.
- **R-5 · The webhook route cannot carry the surface's key.** GitHub does not know `AEON_HTTP_KEY`, and a
  body-signed-then-key-401'd delivery proves the wrong thing. The mounting uses the framework's
  `ApiKeySecurityLevelNone` for the **one** webhook delivery route, whose handler authenticates by HMAC
  **before anything else** (missing signature → auth/401, no loop, recorded). Everything else on the
  surface stays apikey level; a test pins that exactly one None-level route exists and names it. N15
  survives intact: without `--http` + key the surface does not exist, so webhooks die with it.
- **R-6 · The webhook is an action, not route-magic — so the signature travels as a param.** P25/N18:
  stdio must deliver identically (`narrow list` diffs stay empty), and stdio has no headers. The HTTP
  mounting accepts `X-Hub-Signature-256` as sugar for the same param. Consequences to update in T6's
  same commit: `testdata/actions.json` +1, `make protogen` contract.ts, `TestReadIsGETTriggerIsPOST`'s
  count (entries + refusals + 1), errors_test's seven-path sentinel enumeration, and the catch-all set
  (`POST /v1/webhooks/{rest:.*}`).
- **R-7 · Demo §3's "advance the host clock seven days" is not walkable** — nobody is setting the host
  clock. The catch-up is walked **genuinely-down for real minutes** (kill across minute boundaries,
  restart, one fire + could-not-say rows); "no test sleeps" binds the suite, not the manual walk.
- **R-8 · Demo §9's CostCap spec `{"scope","limitUSD"}` is not the shipped shape.** `costCapShape` is
  `{"token": string, "max": number}` (spec_stubs.go). The block line's wording is `Verdict.Sentence()`'s,
  not the demo's guess. T9 corrects.
- **R-9 · There is no trigger CLI and no `trigger.*` method.** New `cmd/aeon/trigger.go` + `main.go` case
  + four registered socket methods (T8), and the **forbidden-token scan** in
  `narrow/actions_test.go` must learn `pause`/`resume`/`fire` so the poisoned-table enumeration — not a
  named grep — is what proves D12.
- **R-10 · `robfig/cron/v3` is present as an *indirect* dep and in the module cache**; `framework/jobs`
  is not redis-backed in this tree at all (no redis in go.sum — the design's warning is stale, its
  conclusion right: use the library). Q1 answered in `parseExpr`: five-field + named descriptors +
  `@every` ≥ 1m; seconds and `@reboot` refused (the sub-minute check is two successive fires — one
  instant-gap away from mis-refusing `*/1`).
- **R-11 · The overlap predicate reads store statuses, not view statuses.** `loop.status ∈
  {running,suspended,orphaned,done}`; `waiting`/`blocked` are daemon **view** derivations. Q2's
  "suspended is still going" is asserted over the store column.
- **R-12 · Scheduling tables land in migration 0032** (`trigger_schedule` + `trigger_fire`, one
  migration — a schedule without its fire-record is half a promise), ledger sentence included;
  fresh-equals-migrated and the CHECK-enforcement tests ship with it.
- **R-13 · The fire path is the same seam the narrow trigger uses** — `loop.Pin(..., attended *bool,
  with, route)`: fire passes `attended=&false`, `allowOutward:false`, and takes **no attendedness
  parameter at all** (D1's guard is a signature, not a comment). Provenance rides separate columns
  (R-3), because sharing a field with attendedness is how "who started it" becomes "may it act".
  (T2 refinement: the seam reached is `loop.Start` — the entry point that pins attendedness inside
  `CreatePinnedLoop`'s transaction; "Pin" named the write, and Start is its only door.)
- **R-14 · "suspends at its first outward action" is DENY-AT-ONCE as measured** (T2's live leg,
  `forge.openPR` on a code-action step): the unattended loop parks **`blocked`** carrying the S10
  sentence ("effect class outward resolves to deny — loop is unattended"), **no question row** (S11's
  R4: unattended asks deny with no question), and **no spawn receipt**. The design's word "suspends"
  predates S11's ruling; what stays true is the safety claim — nothing proceeds outwardly and a
  person finds the blocked row in the morning. The task's gate test is named for the reality:
  `TestScheduledFireIsUnattendedAndStopsAtOutwardLive`.
- **R-15 · The fire needs two inputs T1 did not carry** — a schedule starts a loop SOMEWHERE and
  the loop prints a goal. `workspace_root` + `goal` arrive with **migration 0033** (0032 stays
  immutable, four commits old and unpushed — the rule does not get an exception for our own comfort),
  written by the daemon's fire path; the validator refuses a non-absolute or missing directory at
  write, with the loop's own words (S19 T7's typed refusals restated where the typing happens).
- **R-16 · Q2's "suspended" is step-speak; the loop row says `waiting`** (T3's audit, before its first
  commit): S6 suspends the STEP (`suspended` is `step.status`'s CHECK word, the reason lives in
  `question`), and `loop.status` — a CHECK-less column whose set the engine owns via `SavePinnedRuntime`
  — carries `running / waiting / blocked / done / aborted`. A `StillGoing` predicate written against the
  design's literal vocabulary would block NOTHING (no loop row ever reads `suspended`) — the exact
  "test that passes under both readings" the task warned about, arriving as a predicate that passes
  under NEITHER. Q2 as shipped: running/waiting/blocked block; done/aborted and unknowns do not.

- **R-17 · the design's own example sets the walk cap** (T4): the burst case the design types is
  **10,079 instants** (a week of a minutely schedule) — the first `walkCap` (10,000) classified the
  design's OWN example as corrupt data and left it untouched. The cap is 20,000 now (~14 days of
  minutes): it must sit above the largest honest outage or it is not a bound, it is a bug that
  silences schedules. Past it the frontier is wrong, not old — loud, untouched, for a human.
  And the walk is ONE function (`walkInstants`) shared by tick and reconcile, because two walkers
  drift on what "missed" means.

- **R-18 · the two days a year, measured 2026-10-27 before any handling was written** (T5):
  Go's `time.Date` **does not error** on the DST edges and picks opposite directions per zone —
  London's missing `2026-03-29 01:30` resolved FORWARD to 02:30 BST, London's doubled
  `2026-10-25 01:30` resolved to the **GMT (second)** pass, while Santiago's missing
  `2026-09-06 00:30` resolved **BACKWARD** to Sep 5 23:30 and its doubled `2026-04-04 23:30`
  to the **-03 (first)** pass. robfig v3.0.1 returns **both** passes of a doubled wall (the
  naive double-fire) and **silently skips** a missing day. Chile's rules on this box's tzdata:
  spring-forward first Saturday of September at 24:00 (energy-crisis rule kept), autumn back
  first Saturday of April at 24:00 — March/April northern templates die here. And
  `p.AddDate(0,0,1)` normalizes **through the instant** — beside a gap it can land the probe
  back inside the previous day (measured, Santiago); missing-wall probes must ask the calendar
  (`time.Date(y, m, d+1, h, m, s, 0, loc)`), never AddDate.

- **R-19 · the webhook row found three shape bugs its own kind had never needed** (T6, measured
  2026-10-27 while wiring the first real delivery path): (i) a webhook row carries **no zone** —
  zone is the expression's wall-clock contract (TR6) and this kind has no expression, yet both
  the validator and the store's row check demanded one because the first webhook TEST inherited
  `Europe/London` from a schedule helper (the honest shape exists only once its consumer does);
  (ii) the request **body is read exactly once** per request — `paramsFor`'s JSON merge drains
  `r.Body`, so the delivery route that needs the signed bytes as `payload` must not JSON-merge
  at all (the first mount read it twice and the payload arrived empty); (iii) **row filters
  belong to the consumer** — the adapter filtered `kind=schedule AND not paused`, which answers
  the TICK's question; a delivery must see its paused row to answer 409 with a reason (TR12's
  operational off is not a 404), and the consumers already filtered kind/paused themselves.
- **R-20 · governance is a policy table, and it was real the day it shipped** (T7, measured
  2026-10-27): three facts the task files could not know. (i) **A fresh install fires
  NOTHING** — S10's Q1 left the allow side empty and PL5 denies what no allow matched, so
  every tick and every verified webhook answers `refused` until `aeon policy add
  trigger_access …` exists; the daemon test suite itself had to start seeding allows,
  which is governance wired rather than decorative. (ii) **A closed store PANICS through
  the policy repo callbacks** (nil deref, measured by T7's own fail-closed test) — a
  governance hook the ticker calls cannot be allowed to crash the tick goroutine, so the
  daemon's hook recovers into fail-closed refusal ("refusing, not guessing"); the
  store-side root cause is named OPEN, every consumer of those callbacks shares it.
  (iii) **S10's one-formatter guard is alive** — `verdict_test.go:758` refused daemon tests
  that assembled the sentence string and made them print `Verdict.Sentence()`; the
  verified-delivery refusal is byte-exact against the formatter, not a paraphrase.
  The decided-types set moved in both inforce tests together, as their own comment demands.
- **R-21 · the guards needed guards of their own** (T8, measured 2026-10-27): four facts from
  building the operational plane. (i) **The forbidden-token scanner was blind to camelCase** —
  `trigger.fireNow` tokenized as one word, so forbidding "fire" matched nothing; the fix
  (camelCase as a boundary) was measured against the existing scans (all lowercase) before
  landing, and a poison entry now proves the word bites. (ii) **Source-scan tests must strip
  comments** — my own prose about attendedness tripped the CLI-seam scan; a guard that punishes
  explanation selects for files that stop explaining themselves. (iii) **fire-now on a webhook
  row is refused-and-recorded**: the signature is the delivery's entire authentication, and a
  convenience that could start that loop unsigned IS the bypass. (iv) **The `manual` kind is
  policy vocabulary without a row producer** — migration 0032's CHECK admits only schedule and
  webhook, and no migration was invented to flatter a test; `fireAt` already asks governance
  for `string(s.Kind)` so the closed three will mean something when a producer ships.

- **R-21 · the guards needed guards of their own** (T8, measured 2026-10-27): four facts from
  building the operational plane. (i) **The forbidden-token scanner was blind to camelCase** —
  `trigger.fireNow` tokenized as one word, so forbidding "fire" matched nothing; the fix
  (camelCase as a boundary) was measured against the existing scans (all lowercase) before
  landing, and a poison entry now proves the word bites. (ii) **Source-scan tests must strip
  comments** — my own prose about attendedness tripped the CLI-seam scan; a guard that punishes
  explanation selects for files that stop explaining themselves. (iii) **fire-now on a webhook
  row is refused-and-recorded**: the signature is the delivery's entire authentication, and a
  convenience that could start that loop unsigned IS the bypass. (iv) **The `manual` kind is
  policy vocabulary without a row producer** — migration 0032's CHECK admits only schedule and
  webhook, and no migration was invented to flatter a test; `fireAt` already asks governance
  for `string(s.Kind)` so the closed three will mean something when a producer ships.

- **R-22 · the walk is a code-reviewing instrument** (T9, measured 2026-10-27): the slice walked green on
  units and was wrong five ways until a real daemon met real rows. (i) **The producer gap**: reality-alignment
  inventoried the CLI verbs but nobody asked *who writes the row* — `trigger add` never existed, and demos
  "worked" because fire-now happened to seed what Add should have. (ii) **Empty frontiers are zero times**:
  a fresh schedule was silent forever; fixtures that seed their own frontier cannot see a producer that
  doesn't. (iii) **Governance matched KIND, not identity** — and the fixtures mirrored the bug (seeded
  kind-words), the exact failure mode where unit suites are greenest and wrongest; a real policy row meeting
  a real trigger was the only test that mattered. (iv) **fire-now's minute grid was the tick's idempotency
  key** — truncation collided with `UNIQUE(schedule_id, instant)` and hid the running loop from the overlap
  gate's own `<` query. (v) The forbidden-token scanner read camelCase as one token — guards need guards.
  The doctrine this slice leaves for every future T9: **a fixture written from the code under test proves
  agreement, not correctness — only the consumer's place proves that.**

## How we will do it