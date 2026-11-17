# Brief — S62 Backup, export & restore

Written 2026-10-30. Prefix **BK**. Closes gap **G10** of [`../AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md). Task shapes follow
[`../MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded in `design.md`.

**This slice is small and independent.** It needs only the store. It does **not** depend on S55–S61, S27, S45–S54 or
S43-extra (their tables are just more data in the file). Its settings (`backup.*`) degrade to environment variables if
S44 has not merged. **It can and should be built early** — before the autonomy slices — because every one of them
raises the cost of losing the database.

## 1. Why

[`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md) rule 6 makes the database the single source of truth for every
record: agents, policies, blueprints, MCP definitions, automation, settings, goals, roadmap, memory, history, telemetry.
Code is only a seed source. **Losing `state.db` now loses everything, and the product ships no way to prevent or undo
that.** Measured 2026-10-30:

- There is **no** backup, `VACUUM INTO`, SQLite backup API, `integrity_check` or `wal_checkpoint` anywhere in `internal/`
  or `cmd/` (grep), and no restore.
- Migrations are **forward-only** (`store/migrations/README.md`; `AGENTS.md` §3) and run **on every open**
  (`store/migrate.go:59`, called from `store.Open`) — by the daemon (`cmd/aeon/serve.go:177`) **and by every CLI verb that
  opens the store** (`cmd/aeon/store_verbs.go:73-82`). Nothing copies the file first. A migration that corrupts or a
  binary upgrade that goes wrong has no undo.
- `aeon db upgrade` refuses to apply (`store_verbs.go:199-216`), and `--dry-run` reports "no pending" **after** opening
  has already applied them — so there is no safe moment to take a copy by hand, either.
- `doctor` checks the socket and the narrow HTTP surface only (`cmd/aeon/doctor.go`); it says nothing about the database
  or its safety.
- Other "exports" are not backups: `history.export` (NDJSON of one query), `aeon agents export` (one card), the cold
  archive (`loop_archive`, which *moves* loops out of the live tables).
- The file is not trivially copyable: WAL mode (`store/open.go` `composeDSN`), committed data sits in `state.db-wal`
  until a checkpoint (`store.go:487-545` comment: a store with thousands of events read **4 KiB** because the rest was in
  the log), and the vector tables (sqlite-vec `vec0`) and FTS5 index live in the same file.

## 2. What this slice delivers

1. **Online backup** of a live store into one portable file, `<name>-<UTC timestamp>-v<schema>.aeonbak` — a snapshot via
   SQLite's online backup API (consistent under concurrent writes, WAL- and vector-safe), plus a manifest (schema, row
   counts, SHA-256, binary version). Mode 0600 in a 0700 directory. **Never leaves the machine** (no outbound, ever).
2. **Automatic backup before every migration** that has pending work on a non-empty store, taken inside `store.Open`
   so it covers the daemon **and** every CLI verb. If it cannot be taken, the open **refuses to migrate** (forward-only
   migrations make the copy the only undo) unless an operator says `--no-backup`.
3. **Verification:** checksum, `integrity_check`, `foreign_key_check`, schema comparison, and a drill that opens a temp
   copy and migrates it to current. A backup that has not been verified is shown as **unverified**, never as good.
4. **Restore** — a **human, daemon-down, CLI-only** operation: it proves no daemon and no other process holds the file,
   verifies the backup, stages and atomically swaps, and **keeps the replaced database** (with its WAL/SHM as a set).
   It is deliberately **not** a socket method.
5. **A schedule** — a daemon maintenance task (not a trigger; see design BK7) with an append-only `backup_run` record,
   bounded retention (latest N + daily + weekly), and the only delete this slice adds, fenced inside the backup dir.
6. **Export of managed records** as a reviewable text tree (`aeon export`) for diffs in git — read-only, no secrets,
   deterministic. Import is **out of scope**; restore is whole-database only.
7. **Surfaces, daemon first:** `backup.run|list|verify|status|prune` and `export.run|sets` socket methods, read-only agent
   tools (`backup_status`, `export_sets`), CLI `aeon backup …` and `aeon export` as clients with an **offline direct
   door**, `aeon doctor` database + backup-freshness lines, and a Settings → Backups panel.

## 3. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| State dir: `$AEON_STATE_DIR` (absolute) → `$XDG_STATE_HOME/aeon` → `~/.local/state/aeon`; Windows `%LocalAppData%\aeon`; file `state.db`; `cursor.key` beside it | `internal/paths/paths.go` (`StateDirEnv`, `StoreDirFor`, `StoreFile`, `CursorSigningKeyFile`) |
| Driver `mattn/go-sqlite3 v1.14.52`, CGO, sqlite-vec statically linked, driver name `sqlite3` | `go.mod:9`; `internal/database/driver.go:113`; `store/open.go` header |
| Pragmas: WAL, foreign_keys=1, busy_timeout=5000, synchronous=NORMAL; writer `_txlock=immediate`; writer pool **1**, reader pool default **4** | `store/open.go` `composeDSN`, `DefaultReadPoolSize`, `writerPoolSize` |
| Two production callers of `store.Open`: the daemon and `openStoreDirectly` | `cmd/aeon/serve.go:177`; `cmd/aeon/store_verbs.go:75` |
| `Open` pings, wraps GORM, then `s.Migrate()` unless `SkipMigrations` (documented as test-only: "no CLI path sets it") | `store/open.go` (`Open`, `Options.SkipMigrations`) |
| Migrations run through the framework's runner with a `_migrations` ledger; `Pending()`/`AppliedMigrations()` read it | `store/migrate.go:59,80,111` |
| `store.Info`: Path, Bytes, WALBytes, JournalMode, Applied/Pending, Tables, vec versions, pool sizes | `store/store.go:487-545` |
| `store.SchemaFingerprint()` — the normalised schema shape, comparable | `store/fingerprint.go:93` |
| Vector tables are created **on demand** (`learning_vec`, `bug_vec`, retrieval vec0); FTS5 `retrieval_chunk_fts` + sync triggers | `store/schema.go:2184`; migrations notes in `0040`, `0044`, `20261030061725_learning.sql` |
| Secrets live **outside** the db: route keys via `internal/secrets` (`SaveRouteKey`/`ReadRouteKey`), `cursor.key` 0600; trigger webhooks name an env var (`secret_env`), MCP stdio env holds names — both state "a credential in a row would be a credential in every backup" | `daemon/memory.go:680-735`; `trigger/schedule.go:91`; `mcp/config.go:130` |
| Liveness of a daemon: `endpoint.Claim()` dials the endpoint and **reclaims a stale socket** (it mutates); `endpoint.Path()` resolves it | `internal/endpoint/claim.go:55,95` |
| Daemon shutdown: SIGTERM → `shutdownGrace` 30 s → `d.Shutdown()`; loop runners use `context.Background()` | `cmd/aeon/serve.go:29,355-374` |
| `db` verbs: `info [--vectors] [--fingerprint]`, `upgrade --dry-run`, `query awaiting`; doctor has no database check | `cmd/aeon/store_verbs.go:84-216`; `cmd/aeon/doctor.go` |
| The notification hint set is closed to three loop kinds | `daemon/lifecycle.go:187-197` |

## 4. Out of scope

- Importing records from an export (restore is the whole database).
- Backing up credential stores (`internal/secrets` files, `cursor.key`, environment). Named, not done: an operator copies
  their key files themselves; a restored database on a machine without them shows `hasKey:false` routes honestly.
- Off-machine copies or cloud storage — Aeon has **no outbound path for this and will not add one** (`nettest`: "no
  telemetry" is permanent). An operator may copy the 0600 file wherever they choose.
- Point-in-time recovery, WAL shipping, replication.
- Encryption at rest of the backup file (file permissions are the control, P26; recorded as a deferred decision OD9).
- Backing up the repositories/worktrees (they are git's), the MCP config file before S46, blueprint files before S48
  (**they are files, not rows, today** — T0 records exactly which state-dir files the backup does *not* cover until
  S46/S48 move them in).

## 5. Acceptance

1. A backup taken **while writers run** passes `integrity_check`/`foreign_key_check`, equals one committed snapshot (a
   transactional invariant holds), contains vector and FTS5 data, and is a **single file** (no `-wal`/`-shm` beside it).
2. `kill -9` during a backup leaves **no** file with a final name; a restart sweeps the partial.
3. A migration pending on a non-empty store **cannot** run without a verified-copy-on-disk or an explicit `--no-backup`,
   through the daemon and through a CLI verb alike; a fresh/empty store takes no backup.
4. Restore refuses: under a live daemon; with another process holding the file; a corrupt checksum; a newer-schema
   backup; a missing `--yes`. On success the previous database, WAL and SHM are kept together, and a restore→start→query
   round trip returns the pre-backup data.
5. Prune can unlink **only** files matching the backup naming, regular, non-symlink, inside the backup directory
   (canary-proven); nothing else in the slice calls an unlink.
6. `backup.*`/`export.*` are absent from the narrow surface and the agent services; the agent registry gains exactly two
   read-only tools; restore is on **no** socket/tool/catalog surface (guard + canary).
7. `aeon export` is deterministic (byte-identical twice), contains no credential value (planted-secret test), and
   reports unbuilt record sets as *unavailable*, never as empty.
8. `aeon doctor` states database facts and backup age honestly ("never backed up" is a sentence, not a blank).
9. `make protogen && make check-protogen && make guard` green; the migration count assertions are bumped by exactly one.

## 6. Status

**Built 2026-11-03 (12/12)** — worktree `s62-backup-restore-resume`, per-task merges to main (T0–T8 on 2026-11-02,
T9/T10/T11 on 2026-11-03). All nine acceptance criteria of §5 proven — the evidence table is in T11's Notes, the
drills (backup under writes, six `kill -9`s, the restore round trip with its five refusals), the migration and
pre-migration drills, and the walked demo (including the scheduled catch-up fire) are recorded there with
commands, dates and results. The S44 adoption landed at T10 (the ten `backup.*` settings; durations TypeString,
the four keeps Min 1, none Sensitive, env → row → default, the pre-migration hook pinned on `NewEnvSettings`).
Two reds S62 had left on main were found by T11's acceptance re-run and fixed with the record: T8's missing
`export_sets` toolseed golden line, and the agentsvc registry pin (68→72, four dated landings). **Hand-off:**
S58 (supervision) consumes `backup.finished` · `backup.failed` · `backup.stale` · `backup.pruned`; the five
`backup.*` method names (plus `export.run` · `export.sets` for the tool-catalog readers) are the registered set —
`backup.restore` is deliberately on no surface (CLI direct door only). Known design fact for the next reader:
the backup ticker wakes at start then hourly, and `AEON_BACKUP_INTERVAL` is the due threshold, not the cadence.
