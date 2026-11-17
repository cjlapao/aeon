# Sketch — S27 Continuous work (Stage 5 · The fleet)

**Status:** sketch, not a design. Deliberately shallower than S1–S23 — **this is the largest change to the daemon's model in
the whole plan**, and a full brief for a scheduler that has never run would be fiction.
**Date:** 2026-09-07 · **Depends on:** S8, S11, S16, S24, S26.

**NARROWED 2026-10-27 (S20 T8).** This sketch was written before the clock existed, and it claimed more than
S27 owns. **The clock, the firing, and the unattendedness reasoning are S20's — BUILT, 2026-10-27**:
`internal/trigger/` ships the injectable tick, one-catch-up reconcile, DST honesty, the working-hours window
(TR8), overlap skip-and-record (TR5), the five fire answers (TR4), `TriggerAccess` governance (TR9), the webhook
mounting (TR11), and the operational `trigger list|pause|resume|fire-now` (TR12). A clock-started loop is
unattended by construction and **S20 proved it, not argued it** — `Fire` carries no field that could speak
attendedness, and the loop row says so. **Do not re-derive any of that here.** What is genuinely S27's survives
below and is only this: **work selection** — which roadmap item is *ready*, who queues it, what happens at the
caps, and what blocked items do. The scheduler S27 adds is, mechanically, a caller of what S20 already built;
the roadmap-dependency stands. See [`../S20-triggers/`](../S20-triggers/) — STATUS's S20 row is the receipt.

---

## What it is

Agents stop being one-shot. **A worker picks up a ready roadmap item, runs it, and when it finishes checks whether there is
another.**

That turns Aeon from a thing you start into a thing that is running.

**It is S24's queue, generalized past one project.** S24 gives a single project an ordered, self-sustaining backlog with a
human gate the project can turn on or off; this sketch is the same advance-on-done idea with the scope widened from "the
next item in one project's queue" to "whatever item anywhere is ready," picked up by a scheduler rather than materialized
by a person adding to a list.

## Why it is the largest change

Everything in the alpha assumes a loop is **started deliberately** — by a person at a CLI, or from the app. The first
machine-caller was **S20's trigger (built 2026-10-27)**: work starting when nobody asked at that moment, unattended and
recorded. S27 extends that line rather than inventing it — its caller starts work **because a loop finished**, not because
a clock ticked or a signature arrived. The collision below was therefore already met and answered once; S27 inherits the
answer (above) and carries only what is genuinely new here: **choosing what to run.**

**It is additive in mechanism** — a scheduler is another caller of `loop.start`, and S8's runner already starts a goroutine
per running loop. **It is not additive in consequence.**

## The two collisions, and their answers

**G14 — attendedness is declared at start and pinned.** Who declares it for a loop nobody started?

**Answered and BUILT — S11 recorded it, S20 made it physical (2026-10-27).** An auto-started loop is **unattended by
construction**: the trigger's `Fire` has no field that could speak attendedness, the daemon authors `attended=false`
beside the pin, and a fired loop is tested unattended end-to-end (`TestClockStartedLoopIsUnattended`, and the same
over the socket for fire-now — a person watching is not a party the loop runs for). So S16's effect classes deny
`outward` and `irreversible`, and **an auto-started loop cannot open or merge a pull request.** S27 inherits this;
it does not re-answer it. What remains S27's own: the opt-in stays on the roadmap item, where a human was actually
thinking about what the work may do.

**Which is the correct default for work that began without anyone asking**, and it moves the opt-in to where a human *was*
deciding: **the roadmap item.** That is a better place for the decision than the moment of starting, because it is the
moment someone was actually thinking about what this work is allowed to do.

**And the second collision is a resource one.** P28 sets a per-loop memory budget and a global cap; P29 says the daemon owns
every loop. A scheduler that starts work whenever an item is ready will find the cap, and **what it does then is a design
decision, not an error case**: queue, refuse, or preempt. Queuing is almost certainly right, and it should be visible in the
roadmap rather than silent.

## What "ready" means

S26's definition, consumed here. Roughly: a brief is agreed, dependencies are complete, and priority is not `unknown`.

**That last clause matters.** Bug triage's `could-not-tell` produces items with unknown priority, and a scheduler that treats
unknown as low **would silently never run them** — which is the automated version of the failure triage exists to prevent.

## What it must not become

**Not autonomous outward action.** The unattended default is the guard, and it must not acquire an override that lives
anywhere but a roadmap item someone wrote.

**Not invisible.** A scheduler that starts work should be as legible as a person starting it: the same events, the same app,
the same history entry — and S25 renders it identically, because it *is* identical apart from who asked.

**And not a way around the caps.** S17's two-level counters and loop ceiling exist because a loop that cannot converge should
stop. A scheduler that restarts a blocked item on a timer has removed them without touching them.

## Open questions for whoever specs it

- How many loops run at once, and is that a setting or derived from P28's budget?
- What happens when a scheduled loop **blocks** — does the item go back to the roadmap, and in what state?
- Does the scheduler respect a working-hours window? *"Agents worked all weekend"* is a feature to some people and a
  surprise bill to others.
- How is it stopped? **`aeon loop stop` is human authority (R6)**, and a scheduler needs an off switch that is not
  "uninstall". That switch is operational control, so it lives in the CLI and the app and **never** on the narrow surface.
  **S20 established the pattern** — `aeon trigger pause|resume` (and the `trigger.pause`/`trigger.resume` socket
  methods the app calls) are operational-only, forbidden-token-pinned off the narrow surface; S27's scheduler needs
  its own switch shaped the same way. Whether S20's pause is *enough* (does pausing the trigger stop the continuous
  worker?) is an open question this slice must answer, not assume.
- Does the continuous worker respect a working-hours window? **Solved for schedules** (S20 TR8: the window is the
  schedule's own contract, pair-or-absent, refused-with-the-sentence). Open here only for the *advance-on-done*
  start — a worker that starts the next item because the last one finished has no clock instant to test; does the
  window still gate it, and what does it "did not fire" when the answer came from a finish instead of a tick?
