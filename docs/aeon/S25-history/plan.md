# Plan — S25 Project History

**Brief:** [`brief.md`](brief.md). **Design:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md).

## What is settled

Everything in `design.md`'s §3 (H1–H6, Q1–Q3). In particular: no new table, no new column, ever (D5); the
view merges live `loop` rows and `loop_archive` rows into one time-ordered sequence (D7); a `blocked` loop
renders its stored reason verbatim (D4/H4); every aggregate carries P32's three-state floor (D3/H6); export
and the view share one query, proven by a diff test (H3).

## How we will do it

**The read engine first, every surface after.** `internal/history`'s merge-and-group query is what every
other task calls — the CLI, the socket methods and the UI all sit on top of it, never reimplementing the
merge. Drill-down (T2) can be built alongside the CLI/socket/UI tasks once T1 exists; none of T3–T5 is
blocked on it, since a view that lists loops without yet being able to open one is still useful to build
against.

**No index is added speculatively.** T1 ships against the existing indexes; a follow-up index is added only
if a measured query plan over a realistically sized fixture shows it is needed (`design.md`'s Q1 resolution).

## Task sequence

| Task | What | Depends on |
| --- | --- | --- |
| **T1** | `internal/history`: the merge query (live `loop` + `loop_archive`, time-ordered), the four lane groupings (H1), P32's floored rollup (H6). | — |
| **T2** | Drill-down: live `step_message` reads (unchanged path, H2) and archived `loop_archive_file` reads (header-first, content on demand — Q3). | T1 |
| **T3** | `aeon history` CLI: `list`, `show`, `export` (H3 — NDJSON, parity-tested against the view's own query). | T1 |
| **T4** | `history.*` socket methods, read-only, mirroring T3's verbs — the same `internal/history` calls underneath. | T1 |
| **T5** | App surface: a project-scoped History view reusing S9's existing Gantt/lane component, rendering the four groupings and the floored rollups. | T4 |
| **T6** | Demo: append this slice's section to the cumulative `docs/aeon/DEMO.md`, walked, with a §Findings table. | T1–T5 |

## Files this plan expects to touch or create

| File | Task |
| --- | --- |
| `internal/history/*.go` (new package) | T1, T2 |
| `cmd/aeon/history.go` | T3 |
| `internal/protocol/history.go`, daemon method registration | T4 |
| `ui/src/features/*/HistoryPage.tsx` (or equivalent — T5 names the real path; no reserved nav slot exists yet, unlike S31's Telemetry/Agents tabs) | T5 |
| `docs/aeon/DEMO.md` (§S25 appended) | T6 |

## Ordering rationale

T1 is the one hard dependency everything else shares. T2–T4 are otherwise independent of each other once T1
exists — three engineers could take them in parallel with zero file collision (`internal/history` vs.
`cmd/aeon/history.go` vs. `internal/protocol/history.go`). T5 depends on T4 because the app reads through the
socket, never the CLI, matching every other test-bench/app surface in this project. T6 is last by
construction.

## What this slice does not do

Everything in `brief.md` §6 (N1–N5) stands. In particular: no per-turn telemetry (that is S33, a separate
slice by design), no forward-looking Gantt (S26), no cross-project view, no new store writes.
