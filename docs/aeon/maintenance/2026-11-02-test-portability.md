# Test portability and runtime — 2026-11-02

The operator approved consistent FTS5 settings, prerequisite checks, separate fast/full test lanes,
and measured performance fixes on 2026-11-02.

## Design and plan

1. Keep the CGO SQLite driver and real FTS5 schema. Align editor, Makefile, debugger and nested builds.
2. Check the C compiler and required SQLite features before the Go suite. Missing prerequisites must
   produce an actionable failure, not skips or a different database implementation.
3. Use Go's short mode to omit explicitly expensive build/daemon checks. The full lane remains the
   default and retains all integration checks. Document local socket and native toolchain requirements.
4. Migrate an empty database once per test package and clone its closed, checkpointed bytes for
   ordinary row tests. Each test retains an independent on-disk database. Migration, upgrade and
   durability tests still open and migrate their own databases.
5. Fix observed fixture portability and cleanup defects, then measure isolated warm runs and retain
   explicit evidence of unrelated failures and unverified platforms.

The brainstorming skill's writing-plans dependency is absent from this environment. This record
contains the approved design and ordered implementation plan instead.

## Notes

Initial reproduction: `go test ./internal/loop -run '^TestFanOutTerminalRemovesWorktree$' -count=1`
failed with `no such module: fts5`; adding `-tags sqlite_fts5` passed (1.044s package time).
The initial full runs overlapped; their timings are diagnostic, not performance comparisons.
The sandbox blocks TCP/Unix sockets; full validation requires a runner permitting local listeners.

## Outcome — 2026-11-03

Plan items 1–3 and 5 are done on a listener-permitting runner; item 4 (per-package migrated-DB
cloning) is open debt. What shipped:

- **Tags aligned** (`sqlite_fts5` literal in `aeon/Makefile`, editor settings, debugger configs, the
  TestMain exec'd build); `check-test-prereqs` fails with an actionable message on a missing C
  compiler or SQLite feature; lanes: `test-unit` (short, `./internal/... ./tools/...` — skips the
  CLI integration lane), `test-go` (full), `test-profile` (uncached `-json` timings), `test-noretrieval`.
- **Go fixture portability**: the S62 backup CLI tests used `t.TempDir()` for the endpoint parent; on
  a box whose `TMPDIR` is long the socket path crosses the 108-byte unix cap and the dial dies with
  `invalid argument` — a fault `selectDoor` correctly refuses, which read as the product failing.
  Four tests moved to the file's own `bkShortStateDir`. (`cmd/aeon/backup_cli_test.go`.)
- **Go event-timing race**: `Manager.finish` writes the terminal row *then* publishes the finished
  event (the durable order — the app must not be told `finished` before the row says so), while
  `TestGraphBuildStartsThroughTheSingleManager` polled the row and read the event log immediately;
  under load it caught `[asyncworker.started]` alone. `waitWorkerEventKinds` gives the log read the
  same settling budget `waitWorkerTerminal` already gives the row; assertions unchanged.
  (`internal/daemon/asyncworkers_test.go`, call sites also in `reindex_async_test.go`.)
- **UI**: the fake daemon's `{}` fallback for unconfigured `rpc_call` methods is a test-fabricated
  shape — per-test fixtures (`backupStatus()`, `"seed.status"` lists) replaced defensive guesses where
  the app legitimately trusts the contract; genuine app bug fixed in `toolsModel.ts` (`?.` on
  `vocab.capabilities`, the `?? []` after `.map` was dead); stale layout assertions rewritten to the
  S41 rail + kit Combobox contracts (options mount only while the popup is open; a doomed-start test
  needs an explicit negotiated `protocolVersion`, the fake's default 7 > `MIN_COMPATIBLE_APP_PROTOCOL_VERSION`
  2 ⇒ read-only); `testTimeout/hookTimeout: 20000` and `asyncUtilTimeout: 4000` widened as *budgets*
  for the 1,100-test jsdom load (tests that pass alone at 300 ms flake at 5 s under contention).
  `test/product.test.tsx` (1,287 lines, SIZE1) was split in the same change into seven subject files
  (`product-{shell,start,ladder,thread,components,files,recovery}.test.tsx`, max 305 lines, measured).

### Validation — 2026-11-03, dev box (native gcc, unix sockets permitted, `TMPDIR` long enough to
have exposed the 108-byte cap)

- `go test -tags sqlite_fts5 ./...` — **EXIT 0, all 74 test packages fresh-run, zero FAIL** (~8.5 min
  wall; `cmd/aeon` 506 s is the serial real-binary critical path, every other package overlaps it).
- Targeted re-verification: `./cmd/aeon -run 'TestBackupDirectDoorLine|TestDirectDoorStatus…|TestSelectDoor…'
  -count=2` ok; `./internal/daemon -run 'TestGraphBuildStarts…|TestRetrievalReindexSocketNeverAutoFires'
  -count=3` ok. `go vet` and `make guard` clean.
- `npx vitest run` (aeon/ui) — **113 files, 1124 passed, 1 skipped** (the skip is the pre-existing
  `agent-chat-adapters.test.ts` `it.skip`), 3× green consecutively, ~30–33 s.
- Cold-suite timing profile (from a `-json -count=1` run): package totals sum ~532 s against 501 s
  wall — the wall is `cmd/aeon`'s 497 s serial lane, which builds the binary once and execs it
  deliberately (`cli_integration_test.go` states the argument). The fast lane is `make test-unit`;
  unchanged packages otherwise ride Go's test cache.

### Not run / open (stated per §4a)

- `make test-race` and the Rust (`rust-test`) lanes — not run here (Tauri system libraries absent;
  `make -C ui check-deps`). Plan item 4 (migrate-once + clone, worth ~114 s in `internal/store` alone)
  unimplemented. CI lanes stay parked (§7) and were not exercised.
- Home-heading i18n divergence is a product wording decision, left alone: en "What should we build?"
  vs pt still "O que o Aeon deve orquestrar?".
