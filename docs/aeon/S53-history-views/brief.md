# Brief — S53 History views

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 3 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): "lowest urgency — a read surface today and stays useful as one; 'management' here mostly means promoting the existing view out of the bench".
Shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1, P4, P5, P6, P7, P9–P11**). **Depends on:** S25 (history), S38 + S44-extra (the component tree), S45 (UI components), S47 (surface). T0 re-checks.

## 0. Rules carried — and the ones that do not apply

Daemon first (parity already holds: 3 verbs both ways); a list with details; every management operation gets a tool — **there is no write operation**, so the tools are reads. **Rule 6 does not apply**: history is a *derived* merge (a query over live loops **plus archived ones**), not a set of records; nothing is seeded and no table is added.

## 1. What exists today, measured (2026-10-30)

| Fact | Evidence |
| --- | --- |
| **Built.** Socket `history.list`, `history.show`, `history.export`; CLI `aeon history list|show|export`; `internal/history` (merge query, four lanes, rollup, drill-down, the P32 floor); a project History view in a **bench tab**. | `method.go:264-266`, `cmd/aeon/history.go:47-51`, `internal/history/*` |
| **The archival finding (S25 §2):** `ArchiveLoop` deletes the live `loop` rows; a naive cross-loop query silently under-reports a project's past, so the merge includes `loop_archive` headers in one time-ordered sequence. | `S25-history/design.md` §2 |
| **Lanes:** by loop (default), by phase, by task, by …(four); transcript drill-down reads `loop_archive_file` only on request. | `S25 design H1` |
| **No project/component picker integration in the app**, no way to see **what the view is *not* showing** (coverage), no agent tool. | — |

## 2. What this slice is

1. **`history.coverage {root?}`** (a root component; covers its tree) — the honesty read the view lacked: counts of live vs archived loops in scope, the oldest and newest item, the P32 floor's statement, items the query *cannot* show and why (e.g. an archive whose blob failed to decode — three states: shown / not shown / cannot say).
2. **`history.board`-style details**: the existing list rows plus the rollup (cost/time/outcome words) and links to the loop/task/goal/roadmap item, through one formatter; `history.vocabulary` (lanes, outcome words, filters).
3. **Agent tools** `history_list`, `history_show` (reads; **no export** — it writes a file/large payload; person-only).
4. **CLI** unchanged plus `coverage`, `vocabulary`; **Settings/project History screen** (a real route under Projects) with lane switching, scope pickers (S38), coverage banner, drill-down, export (person).

## 3. Out of scope

The merge query, lanes, rollup and floor logic (S25); archiving loops (a loops-domain operation); telemetry (S54); any write or table. No change to S25 beyond additive reads.

## 4. Acceptance criteria

1. **Coverage is exact and honest**: for fixture stores (live only; live + archived; an undecodable archive) `coverage` reports shown/not-shown/cannot-say counts equal to the real query's results (differential), and the P32 floor statement verbatim.
2. **Details through one formatter**; lanes and outcome words from `vocabulary`, never client-typed.
3. **Read-only everywhere**: no method, tool, verb or UI control in this slice writes; `history.export` stays a person's door (absent from the narrow surface/agent tools — guards with canaries).
4. **Agent tools** `history_list/show` equal the socket reads field for field; no tool exports.
5. **Parity**: every `aeon history` verb has a socket method both ways; **UI** through registered methods only (P34 grep); S25 suites/goldens unchanged.

## 5. Handoff

Done: brief, design, plan, tasks T0–T5. Blocked on: S45 T9 (UI components), S38 (pickers). T0 first.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).
