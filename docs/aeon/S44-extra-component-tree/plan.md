# Plan — S44-extra Component tree

## What is settled before planning

`brief.md` (the operator's decision and the measured model) and `design.md` CT1–CT17. Short version: `component`
becomes the only org record, with `parent_id` (two levels: a root and its children) and `archived_at`; a root with
children *is* a project, derived and never stored; `project`, `implicit` and `project_telemetry` are removed by two
lossless migrations that keep every id; tree operations (promote, attach, detach, archive, restore) are socket
methods first, agent tools and a new `aeon component` CLI second; five operational settings inherit from the root
when a child leaves them unset; the app is adapted only to keep compiling.

## How we will do it

Characterize first (T1 — goldens and a census fixture on the **untouched** code), then the two migrations (T2, T3)
proven against the census, then the store (T4) so the code speaks the new schema, then the doors in parallel —
protocol/daemon (T5), agent tools (T6), automation/triggers (T7) — then the CLI (T8) and the UI compatibility pass
(T9), then the S44 re-check (T10) and close-out (T11). The migrations and the store land together on one branch: the
tree is never in a state where the schema and the store disagree (T2–T4 are one merge unit; T1 goldens gate it).

## Task sequence

### T0 — Re-verify, census, inventories, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens and the census fixture ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — Migration A: the component tree ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.**

### T3 — Migration B: re-key epics, goals, automation; drop `project` ([`tasks/T3.md`](tasks/T3.md))
**Depends: T2.**

### T4 — The store speaks the tree: operations, effective settings, telemetry ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.** T2–T4 merge together.

### T5 — The socket surface: `component.*`, removals, renamed wire fields ([`tasks/T5.md`](tasks/T5.md))
**Depends: T4.** Parallel-safe with T6, T7.

### T6 — Agent tools: the catalog follows the model ([`tasks/T6.md`](tasks/T6.md))
**Depends: T4.** Parallel-safe with T5, T7 (rebases on T5's registrations for the twins).

### T7 — Automation and triggers: `root` replaces `project` ([`tasks/T7.md`](tasks/T7.md))
**Depends: T4.** Parallel-safe with T5, T6.

### T8 — The CLI: `aeon component`, removal of `aeon project`, renamed flags ([`tasks/T8.md`](tasks/T8.md))
**Depends: T5, T7.**

### T9 — The app: compatibility pass on the new contract ([`tasks/T9.md`](tasks/T9.md))
**Depends: T5, T7** (needs `make protogen` on the final wire).

### T10 — S44 re-check after it merges ([`tasks/T10.md`](tasks/T10.md))
**Depends: S44 merged, T4.**

### T11 — Close-out ([`tasks/T11.md`](tasks/T11.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 → T2 → T3 → T4 ─┬─ T5 ─┬─ T8 ─┐
                        ├─ T6  │      ├─ T11
                        └─ T7 ─┴─ T9 ─┤
              S44 merged ──── T10 ────┘
```

## Risks

| Risk | Mitigation |
| --- | --- |
| A rebuild of `component` or `goal` silently cascades and deletes children (the 0035 measured hazard) | park + restore with explicit column lists; copy guards; `pragma_foreign_key_check` guard; the census fixture compares every child table row by row (T2/T3) |
| An id changes and a stored reference dangles (triggers' match JSON, kickoff templates, events) | ids never change by design (CT5); JSON rewrite tested against every stored shape T0 finds |
| A setting changes meaning for an existing component | migration copies values verbatim; inheritance only from an explicit clear; goldens byte-identical (T1) |
| A reader bypasses the effective-settings resolver | a grep test: no code reads the five columns outside the resolver (T4) |
| The UI breaks on removed methods | T9 keeps it compiling and green; the redesign is deliberately not here |
| S44 lands a `component` column mid-slice | T2 reads the current DDL at start; T10 re-checks after S44 merges |
