# Plan — S48 Blueprint management

## What is settled before planning

`brief.md` and `design.md` BP1–BP15. Short version: blueprints become rows (`blueprint`, append-only `blueprint_revision`, derived `blueprint_ref`, an import ledger) with YAML stored **verbatim**; every reader moves from files to a
`blueprint.Source` seam, characterized first; existing files are adopted per file and never touched; the 11 shipped blueprints become the inert `blueprints.standard` seed set (editable, then `diverged`; restore explicit; delete refused);
daemon-first management (usage, history, diff, lint, duplicate, delete, revert, save rewritten); agent reads plus a propose-only `blueprint_save` proposal kind; the CLI as a client with `edit`/`export`; the existing Blueprints screen extended.

## How we will do it

Tables and the pure helpers first, in parallel (T1, T2). Then the **source switch** (T3), characterization-first — the riskiest step. Then adoption (T4), the seed set (T5), the writes (T6), the reads (T7). Guards before any client (T8). Then the proposal
kind (T9), agent tools (T10), CLI (T11), UI (T12) — the clients in parallel — and close-out (T13).

## Task sequence

### T0 — Re-verify, inventories, parity table, mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**
### T1 — The tables, revisions, derived refs, import ledger, and the store's SQL half ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.** Parallel-safe with T2.
### T2 — `internal/blueprint`: references, text duplicate, diff, lint, and the `Source` seam types ([`tasks/T2.md`](tasks/T2.md))
**Depends: T0.** Parallel-safe with T1.
### T3 — The source switch: every reader from files to rows, byte-identically ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2.**
### T4 — Adopting existing files ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.**
### T5 — The `blueprints.standard` seed set, and retiring the hash table ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4, S43-extra T3.**
### T6 — Writes: save, duplicate, delete, revert — revisions, refs, and agent-delete protection ([`tasks/T6.md`](tasks/T6.md))
**Depends: T3.** (Adapts S43's merged agent-delete refusal — S48's own change.)
### T7 — Reads: details, usage, history, revision, diff, lint ([`tasks/T7.md`](tasks/T7.md))
**Depends: T3** (lint's policy half needs S47 `toolcatalog`).
### T8 — The guards: human-door writes, single write path, no file reads, with canaries ([`tasks/T8.md`](tasks/T8.md))
**Depends: T6, T7.** Gates T9–T12.
### T9 — The `blueprint_save` proposal kind ([`tasks/T9.md`](tasks/T9.md))
**Depends: T6, T8 and S45 T1/T5.**
### T10 — Agent tools ([`tasks/T10.md`](tasks/T10.md))
**Depends: T7, T8, T9.**
### T11 — The CLI as a client ([`tasks/T11.md`](tasks/T11.md))
**Depends: T6, T7, T8.** Parallel-safe with T10, T12.
### T12 — Settings → Blueprints ([`tasks/T12.md`](tasks/T12.md))
**Depends: T6, T7, T8 and S45 T9.** Parallel-safe with T10, T11.
### T13 — Close-out ([`tasks/T13.md`](tasks/T13.md))
**Depends: T0–T12.**

## Sequencing summary

```
T0 ─▶ T1 (tables) ──┐
 └──▶ T2 (pure)  ───┴─▶ T3 (source switch) ─┬─▶ T4 (adopt files) ─▶ T5 (seed set; needs S43-extra T3)
                                            ├─▶ T6 (writes) ─┐
                                            └─▶ T7 (reads) ──┴─▶ T8 (guards) ─┬─▶ T9 (proposal kind; S45 T1/T5) ─▶ T10 (tools) ─┐
                                                                                ├─▶ T11 (CLI) ───────────────────────────────────┼─▶ T13
                                                                                └─▶ T12 (UI; S45 T9) ────────────────────────────┘
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 2 (`blueprint` tables; `proposal` kind rebuild for `blueprint_save`) — T0 confirms |
| New tables | 4 (`blueprint`, `blueprint_revision`, `blueprint_ref`, `blueprint_import`) |
| New production seed sets | 1 (`blueprints.standard`, **inert**, = the embedded files) |
| New socket methods | 8 (`blueprint.usage/history/revision/diff/lint` reads; `blueprint.duplicate/delete/revert` writes) + the rewritten existing `save`, additive `list`/`show` |
| New proposal kinds | 1 (`blueprint_save`) |
| New agent tools | 5 (4 reads, 1 propose) — granted to **0** by default |
| Runtime reads of a blueprint *file* after T3 | **0** (importer only) |
| Files the importer modifies/deletes/renames | **0** |
| Hand-rolled pristine-hash upgrade tables after T5 | **0** (all previously recognised hashes proven pristine) |
| S43/S44/S45/S46/S47 files edited | **0** (S43's agent-delete refusal adaptation in T6, listed) |

## Cross-slice coupling

- **S45**: desk/commit (T9), UI components (T12). **S43-extra**: the engine (T5). **S47**: `toolcatalog` (T7 lint) and the surface classification (T8 classifies the new methods there). **S43**: agent slugs; T6 adapts its delete refusal. **S49** (automation) will consume `usage`/`BlueprintReferences` and a blueprint picker; S48 does not build them.
