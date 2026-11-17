# Plan — S43-extra Seed process

## What is settled before planning

`brief.md` (the operator's rule, the measured gap, acceptance) and `design.md` SD1–SD13. Short version: one engine
(`internal/seed`) applies code-declared, versioned seed sets to the database with a ledger and an append-only audit;
**inert** sets apply at daemon start, **guard** sets only by an explicit human-door action (D4/PL1 preserved);
first-party agents become complete rows and **dispatch reads the row**; the operations are daemon-first
(`seed.status/plan/apply/restore`), with a read-only agent tool, a CLI client and a Settings panel; every later
management slice declares its own set against this engine.

## How we will do it

Pure core first (T1), storage beside it in parallel (T2), then the join (T3). Then the one risky domain change — moving
first-party dispatch from code to rows (T4 data, T5 dispatch) — done **characterization-first** with a lossless
round-trip proof. Then the doors (T6 daemon, T7 CLI, T8 UI), then close-out. The slice ships **one real set**
(`agents.firstparty`); the engine's generality is proven by a fake second set in tests, and the real second set is S45's.

## Task sequence

### T0 — Re-verify, inventory every consumer of the embedded bodies, settle the mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — `internal/seed`: sets, records, registry, canonical hashing, the pure plan ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.** Parallel-safe with T2.

### T2 — The ledger and audit tables, and the store's SQL half ([`tasks/T2.md`](tasks/T2.md))
**Depends: T0.** Parallel-safe with T1.

### T3 — The engine: apply, plan, status, restore, classes, divergence, retirement ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2.**

### T4 — The agents set: schema extension, applier, lossless source, adoption of S43's stubs ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.**

### T5 — Dispatch reads the row ([`tasks/T5.md`](tasks/T5.md))
Characterize, add `handler.Source`/`FromRow`, switch production to the store, refuse custom agents, order startup.
**Depends: T4.**

### T6 — The daemon doors: startup apply, `seed.*` methods, `seed_status` tool, guards ([`tasks/T6.md`](tasks/T6.md))
**Depends: T3, T5.**

### T7 — The CLI client, `aeon init` integration and `doctor` ([`tasks/T7.md`](tasks/T7.md))
**Depends: T6.** Parallel-safe with T8.

### T8 — The "System records" panel in Settings ([`tasks/T8.md`](tasks/T8.md))
**Depends: T6.** Parallel-safe with T7.

### T9 — Close-out ([`tasks/T9.md`](tasks/T9.md))
**Depends: T0–T8.**

## Sequencing summary

```
T0 ─▶ T1 (core) ──┐
 └──▶ T2 (SQL) ───┴─▶ T3 (engine) ─▶ T4 (agents set) ─▶ T5 (dispatch from rows) ─▶ T6 (daemon doors) ─┬─▶ T7 (CLI, init)
                                                                                                        └─▶ T8 (UI) ──▶ T9
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 2 (`seed_ledger`+`seed_event`; `agent` dispatch columns + `origin`) |
| Production seed sets | **1** (`agents.firstparty`, class `inert`) |
| First-party agent rows that are stubs after T4 | **0** (of 33 bodies / the registered set — T0 records the exact number) |
| Embedded-body reads on the dispatch path after T5 | **0** (tests only) |
| New socket methods | 4 (`seed.status/plan/apply/restore`) |
| New agent tools | 1 (`seed_status`, read-only) |
| S43 tests edited | **0** |

## Cross-slice coupling

- **S45 (policy)** is refactored in its own docs to declare `policy.corpus` (guard) against this engine and to add
  `origin` to `policy`; it **depends on S43-extra T3** (the engine) and not on T4–T8.
- **S46–S49** declare their sets the same way; none starts before T3 lands.
- **S44** is independent (settings declarations are shape, not records, SD1).
