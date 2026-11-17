# Design — S20 Triggers (Stage 4 · Space shuttle)

**Date:** 2026-10-14
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, third of six
**Depends on:** S19 (the `trigger` kind and the narrow surface), S10 (`TriggerAccess`, `CostCap`). **S27 extends it.**
**Inherits:** S19's `trigger` action kind and N15; G14's attendedness pin.

> Decision ids are prefixed **TR**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

Everything in Aeon so far assumes a loop is **started deliberately** — by a person at a CLI, from the app,
or through S19's `trigger`.

This slice adds the one caller that is none of those: **a clock.**

### It adds a clock to a model that already exists

**S19's action table already has `Kind = read | trigger`**, and already decided *"a webhook is a
`trigger`, not a third surface."* **S11's design already anticipates** *"a scheduler picking up a ready
roadmap item, and a webhook"* — and already closes the inference path that would let either declare itself
attended.

**So this slice does not invent triggers.** It is ~8 tasks rather than ~20 because most of it was designed
for other reasons, years of slices ago.

### Two different things, and only one of them is here

| | What it is | Needs |
| --- | --- | --- |
| **S20 — this slice** | **A clock.** *"At 03:00, start loop X from blueprint Y"* | the daemon, a blueprint, the `trigger` kind |
| **S27 — Stage 5** | **Work selection.** A worker picks whatever roadmap item is *ready*, runs it, picks another | S20 **and** S26's roadmap, "ready", queueing, caps |

**The existing Stage 5 sketch conflates them**, and separating them is what lets time-based triggers land
here **without dragging the roadmap in.**

> **A schedule fires a named blueprint. It does not choose its own work.**

### The hard part was solved two stages earlier

Nobody is at the keyboard at 03:00. **G14 pins attendedness at loop start and forbids inferring it**, and
S11 ensures there is no inference path.

So a clock-started loop is **unattended by construction**: its `outward` and `irreversible` actions are
denied, and **it cannot open or merge a pull request.** A 3am job that reaches one **does not silently
proceed** — it suspends (S6), and somebody finds a question in the morning.

**That is why this slice is cheap.** The dangerous property of unattended work was handled before there was
any unattended work.

### What is genuinely new

Four scheduling questions, and they are new because nothing in the plan has needed a clock before.

---

## Goal

**A loop can start because the time arrived or because something outside asked — and both are as legible as
a person starting it.**

Demonstrable: schedule a loop for a minute from now, watch it fire unattended and suspend at the first
outward action; stop the daemon through a fire and watch exactly one catch-up; deliver a signed webhook and
watch its payload labelled untrusted.

---

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| TR1 | **A schedule fires a *named blueprint*. It never selects work** | Work selection needs a roadmap, "ready", queueing and caps — all of which are S27's. Keeping them out is what makes this slice eight tasks |
| TR2 | **A clock-started loop is unattended, and the attendedness is pinned at start as any other loop's is** (G14) | No new mechanism and no exception. The inference path S11 closed is the one that would otherwise let a scheduler declare itself attended |
| TR3 | **Missed fires reconcile on startup to at most one catch-up, never a burst.** A daily job missed for a week runs **once** | A burst is a thundering herd of loops that each think they are today's. And the skipped fires are **recorded, not dropped** — see TR4 |
| TR4 | ***"Did this fire?"* has three answers**: fired · did not fire · **we were down and cannot say** | **P31's ninth instance.** The third is the one that matters, and the one that disappears if a missed fire is simply absent from the log |
| TR5 | **An overlapping fire is skipped, and the skip is recorded** | S5 is serial, so a loop overrunning its period is a **signal** — not something to parallelise. Recording it is what makes the signal visible |
| TR6 | **A schedule stores an explicit IANA timezone.** A wall-clock time that does not exist is **skipped**; one that occurs twice fires **once** | Cheap to decide now; otherwise it is a bug report nobody can reproduce more than twice a year, in a component nobody was watching |
| TR7 | **Cost runaway is a `CostCap` policy** (S10), not a scheduler feature | The policy work pays for itself the first time it is reused. A second cap mechanism in the scheduler would be a second place to look and a second thing to get wrong |
| TR8 | **A schedule may carry a working-hours window.** Absent one, it runs whenever it fires | *"The agents worked all weekend"* is a feature to some people and a surprise bill to others — and that is a per-deployment judgement, not a default |
| TR9 | **A webhook is signature-verified (HMAC), and a valid signature proves who sent it — never what they may cause** | The first half is table stakes. The second is the half that gets forgotten: authentication is not authorization, and a verified sender still gets `TriggerAccess` evaluated |
| TR10 | **A webhook payload is untrusted input**, carrying W23's label into every prompt it reaches | W23, MC7, AS12 and now this — the fourth surface. A payload is a stranger's text that arrives without any agent involved at all |
| TR11 | **The webhook endpoint is another enumerated projection on the narrow surface** (P25), off by default, explicit address, key required (N15) | One protocol, N projections. A webhook listener of its own would be a fourth surface to secure and keep narrow — and **this is why S20 lands after S19** |
| TR12 | **Two off switches, and they are not interchangeable:** `TriggerAccess` policy is **governance**; pause/disable is **operational control** (R6) — CLI and app only, never the narrow surface | A scheduler needs an off switch that is not "uninstall". And operational control on the narrow surface would hand a caller the means to bypass the guards written to constrain it. Putting the governance half in config instead of policy would reopen exactly what policy replaced |

---

## Architecture

```
aeon/internal/trigger/
  schedule.go    the schedule: blueprint, cron expression, IANA zone, window (TR1, TR6, TR8)
  clock.go       the tick; firing; overlap (TR5)
  reconcile.go   startup catch-up, bounded to one (TR3, TR4)
  webhook.go     HMAC verification; the payload as untrusted (TR9, TR10)
  record.go      fired / skipped / could-not-say (TR4)
  control.go     pause and resume, CLI AND socket (TR12, P34 — corrected 2026-10-26: this file previously
                 said CLI-only, which contradicted this same design's own TR12 row above)
```

**The cron library is `robfig/cron/v3`, already in the dependency tree** — see the note below, because the
obvious alternative is a trap.

### Why not the framework's `jobs` service

**It is the obvious home for a schedule and it cannot be used.** `framework/jobs` declares
`Deps("config", "logging", "redis")` and is `asynq`-backed.

**Requiring Redis to schedule a 3am job breaks the one-install promise** (P10) — *"install Redis"* is not a
step a single-user local daemon gets to have.

`robfig/cron/v3` is already in the tree **via that same service**, so the library is free and the service is
unusable. **Use the library.** Nothing to request upstream — and this is written down because the next
person will also notice `jobs` and wonder.

### A fire, step by step

```
tick → is it in the window? (TR8)
     → is a previous run still going? ─── yes ──▶ SKIP, recorded (TR5)
     → TriggerAccess allows? ─────────── no ───▶ refused, recorded (TR12)
     → start loop, attendedness PINNED UNATTENDED (TR2, G14)
     → first outward action ──────────────────▶ SUSPENDS (S6). A question waits.
```

**The last line is the whole safety story**, and it is inherited rather than built.

### Three answers, not two

```
fired               the loop started, here is its id
did not fire        the window excluded it, or it was paused
COULD NOT SAY       the daemon was down across this instant
```

**The third exists because the daemon is not always up**, and a missed fire that is simply absent from the
log is indistinguishable from one that never should have happened. **P31's ninth instance.**

---

## The demo

Schedule, fire, suspend. Stop the daemon through a fire. Deliver a webhook. In full:
[`DEMO.md`](DEMO.md).

---

## Testing

| Property | How |
| --- | --- |
| **A clock-started loop is unattended** | The loop row's attendedness, asserted — and an `outward` action suspends rather than proceeding |
| **A missed week fires once** | Advance the clock a week with the daemon down; start; **exactly one** loop starts |
| **A missed fire is recorded as could-not-say** | Distinguishable from both fired and did-not-fire |
| **An overlapping fire is skipped and recorded** | With a previous run still going |
| **A non-existent wall-clock time is skipped** | A spring-forward schedule at 02:30 in a zone that skips it |
| **A doubled wall-clock time fires once** | An autumn-back schedule at 01:30 |
| The window excludes | A fire outside the window does not start, and says why |
| `CostCap` stops a runaway | An hourly loop against a cap; the cap fires, not a scheduler limit |
| **An unsigned or mis-signed webhook is refused** | And no loop starts |
| **A verified webhook still evaluates `TriggerAccess`** | Authentication is not authorization |
| **A webhook payload is labelled untrusted** | On the assembled prompt |
| **Pause is not on the narrow surface** | Grep over S19's action table |
| **No second listener** | Grep: the webhook endpoint is S19's mounting, not a new one |
| The clock is injectable | Every test above runs without sleeping |

---

## Out of scope

- **No work selection.** A schedule fires a named blueprint (TR1). Picking a *ready* item is **S27**, and
  it needs S26's roadmap.
- **No queueing.** An overlapping fire is skipped (TR5), not queued. Queueing is a decision about what to
  do when work piles up, and that belongs with S27's caps.
- **No parallelism.** S5 is serial and P29 says the daemon owns every loop. A scheduler that starts work
  concurrently is a different slice with a different resource model.
- **No webhook listener of its own.** S19's mounting (TR11).
- **No retry of a failed fire.** A fire that starts a loop has done its job; what the loop does is the
  loop's. A scheduler that restarts a blocked loop on a timer has removed S17's caps without touching them.
- **No `jobs` service.** See above.

---

## Amendments to earlier slices

| Slice | Change | State |
| --- | --- | --- |
| **S19** | The webhook endpoint is added to the existing action table as a `trigger` mounting. **No new surface** | **this slice's T6** |
| **S10** | `TriggerAccess` gains its evaluator — the schema and refusing stub landed in S10's T3 | **this slice's T7** |
| **S27** | The sketch conflates the clock with work selection. **Narrow it to selection only**, and record that the clock is S20's | **this slice's T8** |
| **S23** | `doctor` reports schedules, their last fire, and anything in the could-not-say state | open — S23's T8 |

---

## Open questions

| # | Question | Where the answer comes from |
| --- | --- | --- |
| Q1 | Full cron syntax, or a restricted subset? | T1. **A subset is defensible** — `@daily`, `@hourly` and a five-field expression cover the stated need, and every syntax feature is a thing to validate and explain. Decide with the demo in front of you |
| Q2 | What is "a previous run still going" for a **suspended** loop? | T3. **A loop suspended on a question is not running, and it is not finished.** Firing again would start a second loop while a human owes an answer to the first — **so the answer is probably "treat suspended as still going"**, and whichever way it goes it must be stated |
| Q3 | Does a paused schedule still record could-not-say while the daemon is down? | T4. **No** — paused means it would not have fired, so there is nothing to be uncertain about. State it, because the implementation falls out differently |
| Q4 | Does a webhook fire carry its payload into the loop, and if so where? | T6. It must be reachable (that is the point of a webhook) and it must be **labelled** (TR10). The blueprint's inputs are the obvious place; **the manifest is where the label lives** |
