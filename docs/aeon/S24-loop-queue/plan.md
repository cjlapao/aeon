# Plan — S24 Loop Queue

**Brief:** [`brief.md`](brief.md). **Design:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md).

## What is settled

Everything in `design.md`'s §3 (CG1–CG9). In particular: the queue item status vocabulary is seven values,
closed; a `blocked` terminal status always halts advancement regardless of `queueAdvance`; the first item in
an otherwise-idle queue always needs an explicit call (no reactive path exists for it); a queue item's
materialization payload is `LoopStartParams` verbatim; authoring the queue is both CLI- and socket-writable
(CG9, a deliberate departure from PL15).

## How we will do it

**Schema and the core package first, the reactor second, surfaces last.** A reactor with nothing to react
to (no items, no status transitions to validate against) is unbuildable before the store exists under it;
a CLI or socket surface with no `internal/queue` to call is the same problem one layer up. Sizing can be
built in parallel with the reactor once the core package exists — neither depends on the other, they only
both depend on `internal/queue`.

**Before writing the reactor (T3), check whether the daemon already has a generic "subscribe to
`loop.finished`/`loop.blocked`" seam**, or whether every existing consumer of those events reads them
inline at the point they're emitted. If no generic subscription exists, T3 adds the minimal one this slice
needs rather than a general event-bus — building a pub/sub layer nobody asked for is exactly the kind of
unrequested abstraction this project's house style refuses.

## Task sequence

| Task | What | Depends on |
| --- | --- | --- |
| **T1** | Migration `0026`: `project.queue_advance`, `project.sizing_mode` columns (plain `ALTER ADD COLUMN` with inline `CHECK`, CG7) and the new `queue_item` table (CG8). Migration count assertion updated to 26. | — |
| **T2** | `internal/queue`: `QueueItem`, `Status`, the seven-value transition table (CG1) with its exhaustiveness test, and store CRUD — `Append`, `List`, `Reorder`, `Remove` (→ `cancelled`, never a hard delete), `NextReady`. | T1 |
| **T3** | The reactor: subscribes to `loop.finished`/`loop.blocked`, applies CG2 (any `blocked` halts, always) and CG4 (cold-start needs an explicit call, never reactive), calls the existing `loop.start` on the next `ready` item. | T2 |
| **T4** | `queueAdvance: ask` wiring: a `gate`-kind question attached to the just-finished loop's slug (§2's FK finding), answered → materializes next item; unanswered → queue stays halted. | T3 |
| **T5** | Sizing: `sizingMode: skill` dispatches a step/loop carrying one new native tool, `report_size` (S11-shaped — `internal/tools`, one-shot, known schema, D9), writing `size_min`/`size_max`/`size_basis` onto the item and transitioning `sizing → ready`. A failed sizing run reverts the item to `pending` (design.md's transition table). | T2 |
| **T6** | `aeon queue` CLI: `append · list · reorder · remove · set-size · settings` (get/set `queueAdvance`/`sizingMode`, and the per-item `attended` override at append time, D5). | T2 |
| **T7** | `queue.*` socket methods mirroring T6's verbs (CG9 — same `internal/queue` calls underneath, a transport choice only). | T2, T6 |
| **T8** | Test-bench UI: `"queue"` added to `BENCH_TABS`, new `QueuePage.tsx` (modeled on `PolicyPage.tsx`/`AskPage.tsx`) — list with position/status, append form, settings toggles, size shown as a range with basis (D8), never a bare number. | T7 |
| **T9** | Demo: append this slice's section to the cumulative `docs/aeon/DEMO.md` (§4b), walked once T1–T8 are built, with a §Findings table. | T1–T8 |

## Files this plan expects to touch or create

| File | Task |
| --- | --- |
| `internal/store/schema.go`, `schema_test.go` | T1 |
| `internal/queue/*.go` (new package) | T2 |
| `internal/daemon/queue_reactor.go` (or similar — T3 names the real file) | T3 |
| `internal/daemon/` question-raising path (extends existing S6 machinery) | T4 |
| `internal/tools/report_size.go`, `internal/handler/` (a sizing handler body) | T5 |
| `cmd/aeon/queue.go` | T6 |
| `internal/protocol/queue.go`, daemon method registration | T7 |
| `ui/src/app/routes.ts`, `ui/src/features/test-bench/pages/QueuePage.tsx` | T8 |
| `docs/aeon/DEMO.md` (§S24 appended) | T9 |

## Ordering rationale

T1→T2 is a hard dependency (nothing to store without the schema). T3 and T5 both depend only on T2 and are
independent of each other — a second engineer could pick up T5 while T3 is in progress with zero file
collision (disjoint packages: `internal/daemon/queue_reactor.go` vs `internal/tools/report_size.go` +
`internal/handler/`). T4 depends on T3 because it extends the reactor's halted-for-a-question path. T6
depends only on T2, same as T3/T5 — a third engineer could start it immediately alongside either. T7 depends
on T6 existing first so the socket methods have a proven implementation to mirror rather than inventing the
CLI and the socket shape simultaneously. T8 depends on T7 (the UI reads through the socket, never the CLI).
T9 is last by construction (§4b: the demo is walked once there is something to walk).

## What this slice does not do

Everything in `brief.md` §6 (N1–N6) stands unchanged. Nothing in this plan builds a cross-project scheduler,
a Gantt renderer, a merge mechanism, an MCP sizing path, a relaxed attendedness default, or a second policy
mechanism.
