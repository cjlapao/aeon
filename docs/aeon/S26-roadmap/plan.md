# Plan — S26 Roadmap

**Brief:** [`brief.md`](brief.md). **Design:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md).

## What is settled

Everything in `design.md`'s §2 (R1–R4). In particular: ideation is an ordinary loop ending at an existing
`produces:` check (R3), never a new blueprint shape; an item's status is its own eight-value field, never
read off the artifact it points to (R1); `epic` is a tag, not a row (R2); materializing a `ready` item
prefers S24's queue when present and falls back to a direct `loop.start` otherwise, never a second
mechanism either way (R4).

## How we will do it

**The core package first, the two dispatch paths in parallel, materialization and the surfaces last.**
Ideation-dispatch (idea → brief-agreed) and planning-dispatch (brief-agreed → planned) are structurally
identical in shape (dispatch an existing handler, watch for its declared artifact, transition status) and
are combined into one task rather than split, since splitting them would mean two tasks sharing almost all
of their test scaffolding for no real parallelism gain — they are sequential by nature (planning cannot
start before a brief exists), so nothing is lost by building them together.

**CLI and socket are combined for the same reason S24 kept them separate does not apply here**: this slice's
surface is smaller, and the socket methods mirror the CLI 1:1 with no independent validation logic of their
own to parallelize against.

## Task sequence

| Task | What | Depends on |
| --- | --- | --- |
| **T1** | Migration: `roadmap_item` (R1's eight-value status, R2's `epic` tag, D2's priority split, D3's estimate range, D5's provenance). | — |
| **T2** | `internal/roadmap`: types, the eight-value transition table (exhaustive, closed), store CRUD. | T1 |
| **T3** | Ideation dispatch (idea → ideating → brief-agreed, R3's `produces:` check) and planning dispatch (brief-agreed → planned, S13's `project-planner`), both reusing existing dispatch machinery (the same pattern S24's T5 used for sizing). | T2 |
| **T4** | Materialization (ready → running, R4's queue-or-direct-start fallback). | T2 |
| **T5** | `aeon roadmap` CLI and `roadmap.*` socket methods, combined (append/list/show/settings, mirroring the CLI 1:1). | T2 |
| **T6** | App surface: the forward Gantt, reusing S25's lane component unchanged, pointed at items instead of loops. | T5 |
| **T7** | Demo: append this slice's section to the cumulative `docs/aeon/DEMO.md`, walked, with a §Findings table. | T1–T6 |

## Files this plan expects to touch or create

| File | Task |
| --- | --- |
| `internal/store/schema.go`, `schema_test.go` | T1 |
| `internal/roadmap/*.go` (new package) | T2 |
| `internal/daemon/roadmap_dispatch.go` (or similar — T3 names the real file) | T3 |
| `internal/daemon/roadmap_materialize.go` | T4 |
| `cmd/aeon/roadmap.go`, `internal/protocol/roadmap.go` | T5 |
| `ui/src/features/roadmap/RoadmapPage.tsx` | T6 |
| `docs/aeon/DEMO.md` (§S26 appended) | T7 |

## Ordering rationale

T1 → T2 is the one hard dependency. T3 and T4 are both independent of each other once T2 exists — two
engineers could take them in parallel with zero file collision. T5 depends on T2 only (it can be built
alongside T3/T4, not after them — the CLI/socket surface does not need materialization or dispatch to exist
to list/append/show items). T6 depends on T5 (the app reads through the socket, matching every other
surface in this project). T7 is last by construction.

## What this slice does not do

Everything in `brief.md` §6 (N1–N5) stands. No new blueprint shape, no cross-project roadmap, no scheduler,
no second ordering/record mechanism, no plan content of its own.
