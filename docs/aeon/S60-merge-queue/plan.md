# Plan — S60 Merge queue

## What is settled before planning

`brief.md` (the measured gap: nothing lands a feature branch on the default branch, and nothing orders landings) and `design.md` MQ1–MQ18.
Short version: a per-repository queue keyed by `component_root`, **one active landing per base branch enforced by a partial unique index**,
append-only transition history; the **work is done by loops** (`land-feature`, `resolve-conflict`) driven by a reactor, so G14, effect classes,
S55 stop, cost and recovery apply unchanged; catch-up is **merge-forward onto a new `integrate/…` ref in its own worktree** (no rebase, no
force-push, the person's checkout untouched); six new closed-registry actions (`queue.enqueue`, `git.integrate`, `git.concludeIntegration`,
`git.publishIntegration`, `forge.openLandingPR`, `forge.land`); conflicts record **paths only** and exit through `ask | resolve | block`;
green gates are bound to the SHA they verified; `default_branch`/`branch_prefix` are finally read, pinned per loop; mode `off` by default,
merge needs S56's grant. Open decisions OD1–OD13 carry recommended defaults.

## How we will do it

Investigate and characterize first (T0, T1 — branch names, base behaviour, the `forge.merge` guards, the S32 worktree path, all **on the untouched
code**). Then the storage (T2), the pure state machine (T3), the branch-naming adoption (T4), the one missing seam and the git half (T5), the forge
half (T6), the driver and landing loops (T7). Then the doors — socket methods and the human-door guard (T8), agent tools (T9), the CLI (T10),
a bench-level view (T11) — and the close-out (T12). T4 and T5 touch `internal/action` and `loop` and must not interleave with S32's own
close-out edits: **T5 starts only after S32 is closed** (or after T0 records that its gap was closed another way).

## Task sequence

### T0 — Re-verify, inventory, parity table, S32 state, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens: branch names, base, merge guards, worktree path ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — Migrations and the store: queue, history, config, widened ledger ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** T2 and T3 may proceed in parallel.

### T3 — `internal/mergequeue`: the pure state machine, `ValidateEnqueue`, the store adapter ([`tasks/T3.md`](tasks/T3.md))
**Depends: T0.** Parallel-safe with T2; their join is the adapter (T3 step 5 needs T2).

### T4 — Branch naming and base resolution: settings used and pinned per loop ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1, S44-extra T4.**

### T5 — The step-workspace seam, integration worktrees on the S32 ledger, and the git actions ([`tasks/T5.md`](tasks/T5.md))
**Depends: T2, T4, S32 closed.**

### T6 — The forge actions, `forge.land`, the merge-rule seam and the grant seam ([`tasks/T6.md`](tasks/T6.md))
**Depends: T5, S56 contract, S57 contract (`ListStatuses`, `MergeRule`).** The seams exist with fail-closed defaults if the owners have not merged.

### T7 — The driver, the landing and resolution loops, conflict handling, recovery ([`tasks/T7.md`](tasks/T7.md))
**Depends: T3, T5, T6, S48 seed engine.**

### T8 — Daemon doors: methods, errors, the human-door guard, conformance ([`tasks/T8.md`](tasks/T8.md))
**Depends: T3, T7.** Parallel-safe with T9 after its guard sub-step lands first.

### T9 — Agent tools: reads, own-item dequeue, propose-only enqueue ([`tasks/T9.md`](tasks/T9.md))
**Depends: T8, S45 desk.**

### T10 — The CLI: `aeon mergequeue` as a client ([`tasks/T10.md`](tasks/T10.md))
**Depends: T8.** Parallel-safe with T9, T11.

### T11 — A bench-level view and the descriptor-driven UI contract ([`tasks/T11.md`](tasks/T11.md))
**Depends: T8, S45 T9 (shared components).**

### T12 — Close-out ([`tasks/T12.md`](tasks/T12.md))
**Depends: everything.**

## Dependency graph

```text
T0 ─┬─ T1 ─┬─ T2 ─────────────┐
    │      ├─ T4 ─┐           │
    │      │      ├─ T5 ─ T6 ─┼─ T7 ─ T8 ─┬─ T9 ──┐
    └─ T3 ┴──────┘            │           ├─ T10 ─┼─ T12
                              │           └─ T11 ─┘
 S32 closed ──────────────────┘ (gates T5)     S56/S57/S48 contracts ──► T6, T7
```

## Risks

| Risk | Mitigation |
| --- | --- |
| Widening `loop_worktree.owner_kind` is a table rebuild on a table with a CASCADE FK parent | park/copy-guard/fk-guard pattern, census of live rows, drill on an upgraded store (T2) |
| `WorkDir` changes where steps run for **every** loop | empty ⇒ today's behaviour; characterization goldens (T1) run through both; the seam has one writer (T5) |
| The 6-action registry bump trips pinned counts | counted, dated, data-only edit; the closed-registry and "no blueprint adds an action" tests pass unedited (T5/T6) |
| A conflict leaks file contents into the database | the only conflict datum is a path list; a test plants a secret-looking hunk and greps the DB and event log for it (T7) |
| Landing with a stale verification | `verified_sha` bound to the PR head; `forge.land` refuses mismatches (T6) |
| S56/S57 not merged when T6/T7 land | seams fail **closed**: no grant ⇒ denied; no merge rule ⇒ the existing `mergeGate` only; recorded in Notes and re-run at close-out |
| `AddWorktree` reuse shortcut hides a missing directory | new `AddIntegrationWorktree` verifies and rebuilds (T5) |
| Branch prefix edit orphans branches of a running loop | names pinned at `loop.start`; test edits the prefix mid-loop (T4) |
| The driver and the person race on the same item | every transition is a guarded `UPDATE … WHERE status = ?` inside one transaction with the event row; a lost race is a refusal naming the current state (T3) |
