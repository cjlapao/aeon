# Plan — S49 Automation management

## What is settled before planning

`brief.md` and `design.md` AN1–AN15. Short version: one Automation view over schedules, webhooks and event triggers; the S40 human-only authority locks extended to the schedule family (closing a real gap — `trigger.add` takes no actor and
`trigger_schedule` has no creator); every authority change audited once in an append-only `automation_change`; schedule update and archive (never delete); a unified fire-log view; honest health (blueprint/secret/workspace) and pure previews (next fires, match
test, would-have-matched); one description formatter; agents read and may propose only to *disable*; `aeon automation` as a client; a Settings → Automation that states the daemon's authority text. **Triggers are never seeded** (an exception to rule 6, AN2).

## How we will do it

Foundation first: the migration, view and store accessors (T1) and the pure helpers (T2). Then the **write side** (T4) before the reads that depend on its audit, but the unified reads (T3) can proceed in parallel since they need only T1/T2.
Then the guards (T5) **before any client**, then the proposal kind (T6), tools (T7), CLI (T8), UI (T9) — clients in parallel — and close-out (T10).

## Task sequence

### T0 — Re-verify, write/state inventories, parity table, mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**
### T1 — Provenance, archive, audit, and the unified fire view: migration and store ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.** Parallel-safe with T2.
### T2 — Pure helpers: `Describe`, vocabulary, next-fires wrapper, match tester, health composer ([`tasks/T2.md`](tasks/T2.md))
**Depends: T0.** Parallel-safe with T1.
### T3 — The unified reads: list, show, fires, runs, health, vocabulary, previews, changes ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2** (health needs S48; the rest does not).
### T4 — The write side: authority locks for both families, audit, `trigger.update`, archive ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1, T2.** (Adapts the S20/S40 write paths — S49's own change, listed.)
### T5 — The guards: human-door writes on both families, never-delete, never-seed — with canaries ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4.** Gates T6–T9.
### T6 — The `automation_disable` proposal kind ([`tasks/T6.md`](tasks/T6.md))
**Depends: T4, T5 and S45 T1/T5.**
### T7 — Agent tools: four reads and one propose-to-disable ([`tasks/T7.md`](tasks/T7.md))
**Depends: T3, T5, T6.**
### T8 — The CLI as a client: `aeon automation` ([`tasks/T8.md`](tasks/T8.md))
**Depends: T3, T4, T5.** Parallel-safe with T7, T9.
### T9 — Settings → Automation ([`tasks/T9.md`](tasks/T9.md))
**Depends: T3, T4, T5 and S45 T9, S48 T12-level pickers.** Parallel-safe with T7, T8.
### T10 — Close-out ([`tasks/T10.md`](tasks/T10.md))
**Depends: T0–T9.**

## Sequencing summary

```
T0 ─▶ T1 (migration, view, store) ─┬─▶ T3 (unified reads) ─────────┐
 └──▶ T2 (pure helpers) ───────────┴─▶ T4 (write side; adapts S20/S40) ─┴─▶ T5 (guards) ─┬─▶ T6 (proposal kind; S45 T1/T5) ─▶ T7 (tools) ─┐
                                                                                         ├─▶ T8 (CLI) ───────────────────────────────────┼─▶ T10
                                                                                         └─▶ T9 (UI; S45 T9) ────────────────────────────┘
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 2 (`automation_management`: columns + audit + view; `proposal` kind rebuild for `automation_disable`) |
| New tables | 1 (`automation_change`) + 1 view (`automation_fire_view`) + 4 `ALTER` columns on each family |
| New socket methods | 12 (`automation.list/show/fires/runs/health/vocabulary/preview/test/dryrun/changes` reads; `trigger.update`, `automation.archive/unarchive` writes) |
| New proposal kinds | 1 (`automation_disable`) |
| New agent tools | 5 (4 reads, 1 propose-to-disable) — granted to **0** by default |
| Seed sets / seeded triggers | **0** (AN2) |
| Write methods that accept an actor/creator in params | **0** |
| Write methods that accept an `agent` actor | **0** (all 11: `trigger.add/update/pause/resume/fireNow`, `automation.trigger.create/update/enable/disable`, `automation.archive/unarchive`) |
| Delete paths for triggers | **0** |
| Changes to attendedness/outward at start | **0** |

## Cross-slice coupling

S48 (blueprint rows/health/picker) · S45 (desk/commit/UI) · S47 (surface classification of the new methods) · S38 (scope pickers) · S43 (agents in lanes are S48's concern, not S49's). S20/S40 code is **adapted only** where T4 lists (actor, locks, audit on existing writes).
