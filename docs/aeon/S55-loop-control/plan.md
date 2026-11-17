# Plan — S55 Loop control, caps & watchdog

## What is settled before planning

`brief.md` and `design.md` LC1–LC24. Short version: no new loop status — stop parks `waiting`/`stopped`, cancel ends `aborted`/`cancelled` through the one `endStatus` writer, a cap hit parks `blocked`/`cap-hit`; stop, resume, cancel and raise are human-door socket methods (an agent may only record a *request*); budgets are a dedicated table (not the `cost_cap` policy stub) enforced at the step boundary and before each model call with explicit cost-honesty rules; the exhaustion escalation door is wired into `loop.answer`; a watchdog evaluates seeded `watchdog_rule` records over running loops; the daemon drains with a cancel cause that leaves steps recoverable.

## How we will do it

Characterize first (T1: the status vocabulary, the stopped path, the escalation path, cost reads, shutdown behaviour, the epic budget), then storage (T2), then the control core in `internal/loop`/`daemon` (T3), then the shutdown drain (T4, isolated because it is the riskiest change), then the doors and guards (T5). Budgets are a second track (T6 store → T7 engine and enforcement → T8 doors and epic adoption) that reuses T3's parking machinery. The watchdog (T9) needs T3 and the S43-extra seed engine. Agent tools (T10) and the CLI (T11) come after the socket surface; close-out is T12. (13 tasks: T0–T12.)

## Task sequence

### T0 — Re-verify, inventories, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing** (and S44-extra merged, S43-extra T3 for T9).

### T1 — Characterization goldens ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — Storage: `loop_control_event`, `loop_control_request`, store functions ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.**

### T3 — The control core: runner interrupt, stop, resume, cancel, escalation door ([`tasks/T3.md`](tasks/T3.md))
**Depends: T2.**

### T4 — The daemon drain: shutdown that leaves steps recoverable ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.** Parallel-safe with T5–T8 after T3 merges.

### T5 — Socket doors and the P4 guard amendment for loop control ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3.**

### T6 — Budget storage: `budget`, `budget_event`, spend queries, epic backfill ([`tasks/T6.md`](tasks/T6.md))
**Depends: T1.** Parallel-safe with T2/T3.

### T7 — Budget engine and enforcement: boundary, in-step, start, cap-hit, raise ([`tasks/T7.md`](tasks/T7.md))
**Depends: T3, T6.**

### T8 — Budget doors: `budget.*`, `spend.report`, `loop.raise`, epic adoption, guards ([`tasks/T8.md`](tasks/T8.md))
**Depends: T5, T7.**

### T9 — The watchdog: rules, seed set, service, actions ([`tasks/T9.md`](tasks/T9.md))
**Depends: T3, T5; S43-extra T3.**

### T10 — Agent tools: reads and `loop_request_stop` ([`tasks/T10.md`](tasks/T10.md))
**Depends: T5, T8, T9.**

### T11 — The CLI: `loop stop|resume|cancel|raise|requests|dismiss`, `budget`, `watchdog` ([`tasks/T11.md`](tasks/T11.md))
**Depends: T5, T8, T9.** Parallel-safe with T10.

### T12 — Close-out ([`tasks/T12.md`](tasks/T12.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 → T3 ─┬─ T4 ─────────────────────┐
         │           ├─ T5 ─┬─ T8 ─┬─ T10 ───────┤
         │           │      │      ├─ T11 ───────┼─ T12
         │           └─ T9 ─┼──────┘             │
         └─ T6 → T7 ────────┘   (T7 needs T3, T6)
```

## Risks

| Risk | Mitigation |
| --- | --- |
| Cancelling the runner context before a step is stopped turns a user stop into a `failed` step (the runtime returns `failed` on cancellation, `runtime/loop.go:230-236`) | fixed order **steps then runner** (LC2); a race test with a step finishing during the stop |
| A new loop status breaks `StillGoing`/trigger overlap | none added (LC1); a sibling test pins the vocabulary |
| A cap overshoots by one model call | checked before every call; the worst case is documented and shown in `budget.show` (LC9); never claimed as exact |
| An unknown cost silently reads as under-cap | LC11; unknown is never summed as zero; tests per policy |
| Shutdown drain commits a `failed` step or leaves an orphan bash | cause-aware commit (LC19); a kill-mid-step walk; documented fallback |
| An agent obtains a stop/cancel/raise lever | P4 guards + canaries; the only agent write is an inert request (LC15) |
| Double truth for epic budgets | the column becomes dead; a grep test forbids production reads (LC13) |
| Watchdog stops a loop legitimately waiting on a person | evaluates `running` loops only; a test with a loop waiting for days |
| S27 starts work before budgets exist | cross-slice request to S27: refuse a root with no applicable budget |
