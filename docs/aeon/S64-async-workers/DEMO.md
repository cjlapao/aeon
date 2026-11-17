# S64 DEMO — the ten-minute verb becomes a handle

Walked 2026-10-30 (evidence in `tasks/T6.md`); reproducible from a
checkout with `aeon` built `-tags sqlite_fts5`. Point the runtime and
state dirs at a throwaway location (`export XDG_RUNTIME_DIR=$T/xdg-run
XDG_STATE_HOME=$T/xdg-state; mkdir -p $XDG_RUNTIME_DIR`), start
`aeon serve`, and keep a second shell with the same exports.

1. **The slow verb answers fast.** On the socket (socat or any JSON-RPC
   client): `initialize`, then
   `{"method":"graph.build","params":{"componentRoot":"<a big repo>"}}`.
   Answer in milliseconds: `{"workerId":"aw_…","state":"queued","started":true}`.
   No loading state anywhere can now be blamed on a parse.
2. **The ledger answers the page-mount question.**
   `asyncworkers.list {"workerType":"graph.build"}` (or `{"states":["interrupted"]}`,
   `{"componentRoot":"…"}`) returns rows — type, id, state, metadata,
   progress, the three timestamps, and the finished build's full
   `BuildReport` inside `result`. This is exactly what the app calls on
   mount: reopen mid-build, the work is on the page.
3. **Follow and get your report anyway.** `aeon graph build --root <dir>`
   starts the same worker, prints progress lines as they change and the
   BuildReport at the end — the old CLI UX, composed from handle + poll,
   no blocking socket verb.
4. **Kill the daemon mid-build.** `kill -9` the serve process during a
   big build, start it again: the log says
   `async-worker recovery: N worker(s) marked interrupted` and the row
   reads `state:"interrupted"`, `error:"daemon stopped before the worker
   finished"` — never `failed`, and nothing re-fires itself. Retrying is
   a button on the owning page, not an ambush.
5. **Cancel is the ledger's only write.** `asyncworkers.cancel
   {"workerId":"aw_…"}` answers the current row with
   `cancelRequested:true`; the row then lands `cancelled` /
   `error:"cancelled"`. A second cancel answers "already ended cancelled"
   with its reason. `asyncworkers.start` does not exist — `methods` in a
   fresh `initialize` shows the family carries list/get/cancel only.
6. **The app half (vitest lane, no display needed here):** the ledger
   page renders the six states apart, shows `progress unknown` unless a
   runner reported it, offers cancel on live rows only, and has no start
   affordance — asserted button-by-button in `bench-async-workers.test.tsx`.
   In the real app the GraphPage banner appears the moment the ledger
   holds a live row for the page's root, and the status line refreshes
   itself when the build lands.

**Expectations that pin the honesty:** `interrupted` and `failed` are
different rows in the DOM and the database; absent progress never
renders as 0%; a second `graph.build` for a flying root answers
`started:false` naming the live row instead of starting a second parse.
