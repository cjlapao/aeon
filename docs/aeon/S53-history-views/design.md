# Design — S53 History views

Rulings **HV1–HV7**; shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md). T0 re-verifies.

## HV1 — Coverage is a first-class read
`history.coverage {projectRoot?, componentRoot?}` → `{scope, live{loops, oldest, newest}, archived{loops, oldest, newest}, undecodable{count, ids[≤20]}, floor{statement}, notShown[{reason, count}]}`. It is computed by the **same query code** (`internal/history`) with counting/early-out variants (no second merge implementation) and reports `undecodable` archives (a blob that fails to decode is "cannot say", never silently absent — P31's third state). The `floor.statement` is S25's own P32 sentence, verbatim.

## HV2 — One formatter, one vocabulary
`history.Describe(row)` renders a row (lane label, outcome word from S25's closed set, duration, cost with its honesty state per S33's three-state rule where the rollup carries one, links `{kind: loop|task|goal|roadmap, id}` as **data**). `history.vocabulary {}` serves lanes, outcome words, filter keys — derived from the package's constants.

## HV3 — Reads only; export stays person-only
No table, no write. `history.export` is a socket read that returns/produces a bundle; it is **not** exposed on the narrow surface or as an agent tool (a guard asserts it; an agent has `history_list/show` only). CLI `export` writes a file client-side from the daemon's payload.

## HV4 — Agent tools (P6, reads only)
Specs (family `history`): `history_list`, `history_show` with twins `history.list/show` (existing). Schemas filter-only (no identity fields). Counted bump **+2**, not granted by default. No proposal kind (nothing to propose).

## HV5 — Methods
`history.coverage`, `history.vocabulary` (new reads, `RequiresInitialize`, not `Debug`, conditional, off the narrow surface, classified `read` in S47's table); `history.list/show/export` unchanged (compat; additive `describe` field on rows).

## HV6 — CLI (P5)
`aeon history list|show|export` unchanged (goldens) plus `coverage [--root --component] [--json]` and `vocabulary`; offline fallback via the same code (the history store opens read-only).

## HV7 — UI (P7)
The project History view becomes a real route under Projects (the bench tab stays): scope pickers (`component.list`/`component.show`, S44-extra), lane switch from `vocabulary`, a **coverage banner** from `history.coverage` (undecodable shown as a warning with the ids), rows via `Describe` with links, drill-down, export (person; the daemon's payload saved by the app shell). No write control exists.
