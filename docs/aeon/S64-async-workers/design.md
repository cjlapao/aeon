# S64 design — the ledger, the manager, the doors

## The ledger (T1)

`async_worker` (migration `20261030175511_async_workers.sql`, the 51st
file-form migration): `worker_type`, `dedupe_key`, `component_root`,
`worker_id` (`aw_` + 32 hex, `store.NewWorkerID()`), `state`
(`queued|running|finished|failed|cancelled|interrupted`), `metadata`
(JSON KV, optional), `progress` (real, **absent means unknown — not 0**),
`progress_note`, `error`, `cancel_requested`, `result` (JSON, the runner's
answer), `queued_at/started_at/finished_at` (RFC3339 UTC).

A partial unique index `async_worker_flight ON (worker_type, dedupe_key)
WHERE state IN ('queued','running')` makes *one live worker per unit of
work* the database's truth, not the manager's memory: a second Start for
the same (type, key) reads the existing row back and answers "already
flying" (`started:false`) instead of racing. `async_worker_query` serves
the page-mount question — `(worker_type, component_root, state)`.

The store surface (8 methods, all named in `store.go`'s table):
Insert/Get/List/Start/Progress/Finish/RequestCancel/
MarkAsyncWorkersInterrupted. Reads go on `s.r`, writes on `s.w` — the
pin `TestReadsUseTheReaderPoolAndWritesTheWriter` caught three reads on
the writer during this slice and the fix is the test working, not being
exempted.

## The manager (T2)

`internal/asyncworkers` is a plain package (not a framework service).
`Manager.Register(workerType, Runner)` maps type → Go function
(`func(ctx, Job) ([]byte, error)` — the JSON result is what lands in
`result`); `Start(workerType, componentRoot, dedupeKey, metadata)`
inserts-or-reads-live through the flight index, launches the goroutine
when it won the insert, and never blocks the caller. `Recover` sweeps
live rows to `interrupted` at startup; `Shutdown(ctx)` stops accepting,
cancels, waits `shutdownGrace` (5s, a var for tests) for goroutines to
write their own terminal state, then sweeps the rest.

Every terminal write is **guarded** (write only while still live), so
whichever of {runner goroutine, cancel, shutdown sweep} reaches the row
first wins and the events published match the row that survived — a late
runner onto an interrupted row is silent by design; the row already told
the truth. Classification: deadline → `failed "timed out after %s"`;
context cancel → `cancelled`; error → `failed err.Error()`; panic →
`failed "panic: %v"` (the panic never escapes the goroutine). Default
timeout 30m, `MaxConcurrent` 2, progress throttled to 1/s (last value
always lands). Events go through the injected `Publish` — the daemon
hands it `d.publish`, so every kind rides the EventLog (durable) and the
subscription rails exactly like every other kind; tests and the storeless
daemon can pass nil.

## The doors (T3)

`protocol.AsyncWorkers` is the seam (`ListViews/GetView/Cancel/Types/
StartView`); `Table.AsyncWorkers` nil ⇒ the family is **absent** (V13 —
refusals that name the missing seam, no half-doors) and `graph.build`
refuses naming the manager. `RegisterAsyncWorkers()` registers list/get/
cancel only. `graph.build` no longer builds inline: it decodes, resolves
the absolute root and `StartView("graph.build", abs, abs, nil)`,
answering `GraphBuildAccepted{workerId, state, started}` in milliseconds.
Cancel answers with the **post-cancel read-back** (the current row), and
a terminal worker answers "already ended" with its reason rather than
pretending to cancel.

## Events (T3)

Six kinds, `asyncworker.started|progress|finished|failed|cancelled|
interrupted`, aliased in `protocol/events.go` to the manager's consts so
there is one spelling. Each has a conformance fixture, so all six are in
the generated contract's `knownEventKinds` — without that the UI's
`onEvent` gate drops them (that gate is why fixtures are not optional).
Payload is the row View: rows are truth, events are paint — a client
that misses events still reads the truth at mount.

## The app (T5)

`ui/src/lib/asyncworkers.ts`: typed wrappers (rpcTyped over the
generated contract) + `useAsyncWorkers(connected, filter)` — the mount
read (the reopen-mid-build answer) plus an `onEvent` listener that
merges payload-Views by `workerId` while connected. `AsyncWorkersPage`
(bench, order 4400, needsConnection): six states as six renderings,
progress unknown unless reported, cancel as the only write, **no start
affordance** — the page says so and the test walks the buttons.
GraphPage: build now renders the handle, a live-row banner (state,
progress note, %, cancel) reads the ledger for the page's root, and the
status line re-reads itself when the last live row lands.

## Wiring (T4)

One `*asyncworkers.Manager` value behind every door (P34, pinned by
`reflect.Pointer` comparison): the daemon builds it after the graph
service, registers `graph.build` (runner = `graphSvc.Build` → JSON),
hands it `d.publish`, exposes `Workers()`, and `Table.AsyncWorkers` is
the same pointer. `serve` calls `Recover` in the recovery goroutine;
`Shutdown` drains workers before terminal cancel. The notifyGraph hook
starts a `graph.build` worker for unbuilt roots (direct Build remains
the storeless fallback). `aeon graph build` follows the worker to a
terminal state (1s polls, 35-min budget) and prints the BuildReport
from the row's `result` — same terminal UX, none of the blocking socket
call.
