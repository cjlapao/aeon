# Plan — S27 Continuous work

## What is settled before planning

`brief.md` (the problem, the two collisions, §5's prerequisites, §8's "must not become") and `design.md` CN1–CN26. Short version: a
**pure selection core** (`internal/continuous`) picks **at most one candidate per repository per tick** — the queue head first, else a `ready`
roadmap item into a drained repository — and the daemon starts it through the **one existing start door** (`startLoopWith`) as an **unattended** loop
whose outward/merge permissions are **derived from S56 grants and pinned**; admission is by **agent slots** (queue visibly, never refuse, never
preempt); a repository is **occupied** while any loop on it is `running|waiting|blocked`, so a blocked item holds its repository and is **never
restarted**; every start is claimed before it happens (`scheduler_start`), every decision is an append-only audited row, a dry run is the same
function, and the controls are human-door socket methods with read-only agent tools. Dependencies between roadmap items become real
(`roadmap_dependency`) because "ready" is hollow without them.

## Prerequisites (T0 verifies each; a missing one stops the dependent task, not the slice)

| Needs | For | If absent |
| --- | --- | --- |
| S44-extra T4 (`EffectiveForRoot`, `rootId`) | T5, T8, T10 | T4's core takes a `weight` input; T5 wiring waits |
| S44 T3 (config store) | T8 (settings) | T8 declares the four keys against the registry when it lands; the core reads defaults through a `Settings` seam |
| S55 LC (`loop.stop`, budgets, `cap-hit`) | T6, `stopAll` in T7 | `scheduler.stopAll` is **not registered** (V13); `Budgets` seam nil in tests |
| S56 AL (`Grants.Resolve`) | T5's pin, T6 | no scheduler start with outward permission; T5 pins **false/false** until S56 (tested) |
| S57 VG (`gate-failed` kinds) | display only | `needs-you` shows whatever kind string arrives |
| S26 acceptance gaps, S32 T8/T9 | T11/T12 demo | named in T0's blockers; the demo records "not run — reason" |

## How we will do it

Characterize first (T1): the S24 reactor, the two materialization branches and the start door are pinned **before** anything moves. Then the
**data** (T2) and the **dependency graph** (T3) — both independent of the scheduler and useful alone. Then the **pure core** (T4) with its property
tests, because everything else is a seam around it. T5 joins it to the daemon and moves the reactors from "start" to "kick" **for enabled repositories
only**; T6 wires the cross-slice seams once S55/S56 exist; T7 is the guard-sensitive socket surface; T8–T10 are the other doors (tools, CLI, UI); T11
proves restart, crash and runaway behaviour end to end; T12 closes out.

## Task sequence

### T0 — Re-verify, prerequisite census, inventories, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens: reactors, materialization, the start door ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — Migrations and the store: scheduler tables, dependencies, autonomy cap, `scheduler` provenance ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.**

### T3 — Roadmap dependencies: the graph, `IsReady`, the doors ([`tasks/T3.md`](tasks/T3.md))
**Depends: T2.** Parallel-safe with T4.

### T4 — `internal/continuous`: the pure core, the preview, the property tests ([`tasks/T4.md`](tasks/T4.md))
**Depends: T2** (types only). Parallel-safe with T3.

### T5 — The daemon adapter: reader, starter, claim, reconcile, reactor kicks ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4.**

### T6 — The cross-slice seams: grants, budgets, stop, attention ([`tasks/T6.md`](tasks/T6.md))
**Depends: T5 and S55/S56 merged** (S57/S58 optional).

### T7 — The socket surface and the human-door guard amendment ([`tasks/T7.md`](tasks/T7.md))
**Depends: T5.** Parallel-safe with T6 (the `stopAll` method waits for S55).

### T8 — Agent tools and the S44 settings ([`tasks/T8.md`](tasks/T8.md))
**Depends: T7, S44 merged.**

### T9 — The CLI client: `aeon scheduler`, `aeon roadmap depend` ([`tasks/T9.md`](tasks/T9.md))
**Depends: T7, T3.** Parallel-safe with T8, T10.

### T10 — The "Continuous work" panel in Settings ([`tasks/T10.md`](tasks/T10.md))
**Depends: T7** (and S44-extra's compatibility pass).

### T11 — End to end: restart, crash, runaway, fairness ([`tasks/T11.md`](tasks/T11.md))
**Depends: T5, T6, T7.**

### T12 — Close-out: acceptance re-run, drills, the demo walk, the record ([`tasks/T12.md`](tasks/T12.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 → T2 ─┬─ T3 ─┐
              └─ T4 ─┴─ T5 ─┬─ T6 ─┐
                            ├─ T7 ─┼─ T8 ──┐
                            │      ├─ T9 ──┼─ T11 → T12
                            │      └─ T10 ─┘
S55,S56 merged ────────────────┘ (T6)
```

## Risks

| Risk | Mitigation |
| --- | --- |
| The scheduler starts something unattended it should not | `attended` is a hard `false`; outward/merge only from S56 and only narrowed by the item; T5 pins false/false until S56 exists; a call-site test proves `startLoopWith` is the only start |
| Two starters per repository (S24 reactor + scheduler) double-start | CN19: the reactor kicks instead of starting for enabled repositories; `scheduler_start`'s PK is the claim; deterministic slugs; T1's goldens prove the disabled path byte-identical |
| A blocked item is restarted | no start path for non-`ready` items; `refused` claims are never revisited; a test enumerates start call sites |
| A runaway chain of starts | `maxStartsPerRootPerHour` + S55 budgets; T11's runaway test with a fake instantly-finishing blueprint |
| Authority spreads when the tree changes | rows are per repository; `enableTree` expands once; a test attaches a child to an enabled root and asserts it is **not** enabled |
| Preview lies | preview **is** `Plan`; `TestPreviewIsTheTick` |
| Migration rebuild of `loop_run` loses rows | copy guard, index recreated by name, census row counts before/after; reads current DDL first |
| S55/S56 slip | seams are interfaces; the scheduler is shippable as "enabled, builds only" only if S56's absence pins false/false — and that is stated in the close-out as **Partial** rather than Built |
