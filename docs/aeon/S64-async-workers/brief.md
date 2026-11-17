# S64 — Async workers: long-running functions off the blocking paths

Requested 2026-10-30, in the user's words: move long-running functions —
"for example the code graph build, in complex code is very very easy to take like 10
minutes of work" — off blocking paths into async workers, because "if a
user opens my app and is forced to wait 10 minutes in a loading that is
really bad". The requested shape, kept intact:

* "a new service that would be the manager, lets call it AsyncWorkers,
  this would then take a interface and that would be the way it manage" —
  one manager, one interface, everything registers through it;
* other services "have its worker code and register it with the service
  and fire and forget" — workers are Go functions the owning package
  registers, not payloads the socket can name;
* the manager "manage the channels and keep a database table that would
  store for each of these workers the worker type, the worker id, the
  worker state, the metadata, like component id, folder path, this would
  be a kvp, time started, time finished" — the ledger;
* workers "communicate back with status and when finish would the set the
  finish to do", and the manager does "pushing the real time messages to
  the app using the daemon" — events on the existing subscription rail;
* "the app can also query the state of anything, for example go to a page
  the app would ask for this page is there any worker of this type
  running, if yes then we return the worker last details in db";
* "The first example to test this our s30-code-graph service";
* "like next time you open the app if something is still going on we know
  that we need to come back" — the reopen-mid-build promise the whole
  slice exists for.

## Decisions the user confirmed

1. **Crash honesty over auto-restart.** On daemon restart, live rows
   become `interrupted` with a spoken reason; nothing restarts itself.
   A crashed ten-minute parse re-fired invisibly is a loop nobody agreed
   to; the row invites a deliberate retry instead.
2. **The CLI keeps its blocking UX.** `aeon graph build` starts the
   worker, follows it in the terminal (progress lines on change) and
   prints the BuildReport at the end — the socket verb is fire-and-forget;
   the human-facing CLI opts into waiting.

## Non-goals (named, not silent)

* **No generic start door.** A worker is a registered Go function plus
  arguments only the owning service can shape; a socket that named
  workers by string with caller-supplied payloads would be a remote
  code execution door. `asyncworkers.*` carries list/get/cancel and no
  start verb — pinned by test (`TestAsyncWorkersFamilyCarriesNoStart`).
* No priorities, no fairness tuning, no retry policy, no worker pools
  beyond one bounded concurrency cap (default 2).
* No new service registration: the manager is a plain internal package
  the daemon wires (AGENTS.md: framework-service registration is frozen).
* The ledger page cannot start work; starting stays the owning page's
  button (GraphPage's Build for `graph.build`).
