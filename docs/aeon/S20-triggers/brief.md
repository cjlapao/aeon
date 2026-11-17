# Brief — S20 Triggers

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **P10, P25, P29, P31 and R6 are binding.**
**Stage:** 4 · Space shuttle, third of six. Depends on **S19**, **S10**. **S27 extends it.**
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 1. What this is

**A clock.** *"At 03:00, start loop X from blueprint Y."*

And a webhook, which is the same thing with a different cause.

## 2. Why it is eight tasks and not twenty

**Almost all of it was designed for other reasons.**

| Already exists | Where |
| --- | --- |
| The `trigger` action kind | S19's table |
| *"A webhook is a `trigger`, not a third surface"* | S19's design |
| An unattended loop cannot act outwardly | S11 + G14 |
| *"There is no inference path"* for attendedness | S11 |
| A cost cap | S10's `CostCap` |
| A cron library | `robfig/cron/v3`, already in the tree |

**The genuinely new work is four scheduling questions and an HMAC check.**

## 3. The safety story is inherited, not built

Nobody is at the keyboard at 03:00. **G14 pins attendedness at loop start and forbids inferring it.**

So a clock-started loop is **unattended by construction** — `outward` and `irreversible` actions are
denied, and **it cannot open or merge a pull request.** A 3am job that reaches one **suspends** (S6), and
somebody finds a question in the morning.

**Do not add an override.** If a scheduled loop needs to act outwardly, that is a decision somebody makes
at the item, not a flag on the schedule — and that decision belongs to S27, where a human was actually
thinking about what the work is allowed to do.

## 4. Definition of Done

| | |
| --- | --- |
| **D1** | **A clock-started loop is unattended**, asserted on the loop row — and its first `outward` action **suspends** |
| **D2** | **A daily job missed for a week fires exactly once** on startup |
| **D3** | **A missed fire records `could-not-say`**, distinguishable from both *fired* and *did-not-fire* |
| **D4** | **An overlapping fire is skipped and the skip is recorded** |
| **D5** | **A wall-clock time that does not exist is skipped** (spring forward) |
| **D6** | **A wall-clock time that occurs twice fires once** (autumn back) |
| **D7** | A fire outside the working-hours window does not start, and says why |
| **D8** | A runaway is stopped by **`CostCap`**, not by a scheduler limit |
| **D9** | **An unsigned or mis-signed webhook is refused and no loop starts** |
| **D10** | **A verified webhook still evaluates `TriggerAccess`** — authentication is not authorization |
| **D11** | **A webhook payload is labelled untrusted** in the assembled prompt |
| **D12** | **Pause is not reachable from the narrow surface**, asserted by grep (R6) |
| **D13** | **No second listener** — the webhook is S19's mounting, asserted by grep |
| **D14** | **Every test runs without sleeping** — the clock is injectable |

**D1 is the one that must not regress** and it is mostly inherited — which means it is the one a careless
implementation breaks by accident, by passing an attendedness hint along with the fire.

**D3, D5 and D6 are the three nobody would write unprompted.** D3 because an absent record looks like a
non-event; D5 and D6 because they happen twice a year in one timezone at a time.

## 5. The seams

**Downward — S19's action table.** The webhook is a `trigger` mounting on the existing surface (TR11). **If
this slice opens a listener, P25 is being violated.**

**Downward — S10.** `TriggerAccess` governs whether a kind of trigger may fire at all; `CostCap` stops a
runaway. **No second cap, no second gate.**

**Downward — S6.** An unattended loop meeting an outward action suspends. Inherited, not built.

**Upward — S27.** It adds work *selection* on top of this clock. **This slice must not pre-empt it** — no
queueing, no readiness, no roadmap.

## 6. Locked decisions

- **TR1** — **a schedule fires a named blueprint.** It never selects work.
- **TR2** — **unattended, pinned at start.** No override, no hint, no exception.
- **TR3** — **at most one catch-up.** Never a burst.
- **TR6** — **an explicit IANA timezone.** Not an offset, not the host's local zone.
- **TR9** — **a valid signature proves who sent it, not what they may cause.**
- **TR11** — **no listener of its own.**
- **TR12** — **governance is policy; pause is operational, reachable from CLI and app alike, never the
  narrow surface.** **Fixed 2026-10-26 against P34** (`ARCHITECTURE.md`): checked against the real code —
  **S20 is not built** (`docs/aeon/STATUS.md` lists it `specced`, Stage 4 is 0/53; no `internal/trigger`
  package exists), so this is an ordinary design-time fix, not a retrofit. T8's own table already said
  pause/resume live in "CLI and app," but T8's scope built only `aeon trigger` CLI verbs with no socket
  method for the app to actually call — fixed in `tasks/T8.md`: `trigger.list`/`trigger.pause`/
  `trigger.resume`/`trigger.fireNow` are now registered socket methods calling the identical functions the
  CLI verbs call. Pausing an already-configured trigger doesn't widen authority (same class as S35's
  `agent.enable/disable`), so these are plain socket writes, not a staged proposal — and R6/N14's narrow-
  surface exclusion is unchanged either way (the app and the narrow surface are different doors).

## 7. Out of scope

- **N1 — No work selection.** S27, and it needs S26's roadmap.
- **N2 — No queueing.** An overlapping fire is skipped, not queued (TR5).
- **N3 — No parallelism.** S5 is serial; P29 says the daemon owns every loop.
- **N4 — No webhook listener.** S19's mounting.
- **N5 — No retry of a fire.** A fire that started a loop has done its job. **A scheduler that restarts a
  blocked loop on a timer has removed S17's caps without touching them.**
- **N6 — No `framework/jobs`.** It hard-depends on `redis`; see §8.
- **N7 — No outward-action override for scheduled loops.** §3.

## 8. The framework finding, recorded so it is not rediscovered

**`framework/jobs` is the obvious home for a schedule and cannot be used.** It declares
`Deps("config", "logging", "redis")` and is `asynq`-backed.

**Requiring Redis to schedule a 3am job breaks the one-install promise** (P10).

**`robfig/cron/v3` is already in the dependency tree via that same service** — so the library is free and
the service is unusable. **Use the library. Nothing to request upstream.**

## 9. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Full cron syntax or a subset? | T1 |
| Q2 | **Is a *suspended* loop "still going" for overlap purposes?** | T3 |
| Q3 | Does a paused schedule record could-not-say? | T4 |
| Q4 | Where does a webhook payload enter the loop? | T6 |

**Q2 is the one with a real trap.** A loop suspended on a question is **not running and not finished.** If
the next fire treats it as finished, **a second loop starts while a human still owes an answer to the
first** — two loops on one blueprint, one of them invisible to the person who thought they were mid-task.

**The answer is probably "treat suspended as still going"**, and it must be stated either way.

## 10. Risks

- **R1 — An attendedness hint leaks through.** The fire path is new code calling `loop.start`, and passing
  "this is a scheduled run, treat it as attended" is a one-line convenience that destroys G14. **D1 is the
  guard.**
- **R2 — A burst on startup.** The naive reconcile loops over missed instants. **D2 is the guard**, and the
  symptom in production is forty loops at once.
- **R3 — The DST cases are never written.** They are twice-a-year, timezone-specific, and easy to defer.
  **D5 and D6 are the guard, and they must use a real IANA zone**, not a fixed offset.
- **R4 — A listener appears.** A webhook "obviously" needs a port. **D13 is the guard**, and S19 already has
  the mounting.
