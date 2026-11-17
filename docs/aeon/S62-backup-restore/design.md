# Design — S62 Backup, export & restore

Rules are **BK1–BK18**, cited from `plan.md` and the task files. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded in the rule that makes it.
"**VERIFY**" means T0 re-walks the fact before any code depends on it.

## Verified facts

| # | Fact | Where |
| --- | --- | --- |
| F1 | `store.Open` is the only function that opens a database; it resolves config, opens a writer (pool 1) and a reader pool, pings, wraps GORM, then **calls `s.Migrate()` unless `Options.SkipMigrations`** | `store/open.go` (`Open`, `Options`, `writerPoolSize`) |
| F2 | Only two production callers of `store.Open`: the daemon and the CLI's `openStoreDirectly` | `cmd/aeon/serve.go:177`; `cmd/aeon/store_verbs.go:75` |
| F3 | `Migrate()` = framework `MigrationService.RunAll`; one transaction per migration; ledger `_migrations`; a failure rolls that migration back and stops the run; `Pending()` and `AppliedMigrations()` read the ledger | `store/migrate.go:59,80,111` |
| F4 | A fresh file has no `_migrations` yet; `AppliedMigrations()` errors until the runner creates it — **VERIFY** the exact behaviour for the "empty store" test | `store/migrate.go:111-125` |
| F5 | Pragmas are set in the DSN: `_journal_mode=WAL`, `_foreign_keys=1`, `_busy_timeout`, `_synchronous=NORMAL`, writer `_txlock=immediate`; `DATABASE_SQLITE_DSN` (raw DSN) is honoured with missing pragmas appended | `store/open.go` `composeDSN`, `withRawDSN`, `addMissingPragmas` |
| F6 | `store.Info` reports `Bytes` and `WALBytes` **separately** because committed data lives in `state.db-wal` until a checkpoint | `store/store.go:487-545` |
| F7 | Driver is `mattn/go-sqlite3` registered as `"sqlite3"`; `RegisterExtensions()` registers sqlite-vec via `sqlite3_auto_extension` for connections opened afterwards; FTS5 needs the `sqlite_fts5` build tag | `internal/database/driver.go:113`; `store/open.go` (`database.RegisterExtensions()`); `store/schema.go:2184` |
| F8 | vec0 tables are created on demand and named from data; FTS5 shadow tables and sync triggers are ordinary schema | `store/schema.go` 0040/0044 notes; `20261030061725_learning.sql` |
| F9 | `store.SchemaFingerprint()` returns a sorted, normalised, diffable description of a database's shape | `store/fingerprint.go:42-93` |
| F10 | I2: SQL lives only in `internal/store` (grep test `TestNoSQLLivesOutsideTheStore`) — so the snapshot, the integrity reads and the `backup_run` accessors are store code; file handling, naming, tar, retention are **not** | `store/store.go` header; `store_test.go` |
| F11 | `endpoint.Claim()` dials the endpoint, refuses if something answers, and **reclaims (removes) a stale socket**; `endpoint.Path()` resolves the path; Windows uses a named pipe with no stat | `internal/endpoint/claim.go:42-110` |
| F12 | SIGTERM: `cancelRecovery()`, wait up to `shutdownGrace` = 30 s, `d.Shutdown()`, bootstrap shutdown; loop goroutines use `context.Background()` | `cmd/aeon/serve.go:29,355-374`; `daemon/blueprint.go` |
| F13 | Secrets are outside the db: `RouteStore.Save` writes the key through `internal/secrets` **first** and clears `route.APIKey` before the row; a test reads raw DB bytes to prove it; `route` has no key column | `daemon/memory.go:680-735`; `store/schema.go:1180-1190` |
| F14 | Webhook HMAC secrets and MCP stdio env are stored as **env var names**, never values — "a credential in a row would be a credential in every backup" | `trigger/schedule.go:91`; `mcp/config.go:130`; `trigger/webhook.go:33-40` |
| F15 | `loop.Runner`, recovery and the trigger clock run at startup; `d.RunAutomation`/`StartLoops` start after recovery — precedents for daemon maintenance tasks that are **not** triggers | `cmd/aeon/serve.go:~308-345`; `daemon/blueprint.go:985-1012` |
| F16 | `notifyHintFor` is a closed set of three loop kinds; `PublishNotify`/`publishWithNotify` attach a hint at delivery time | `daemon/lifecycle.go:155-197` |
| F17 | Narrow surface = enumerated allow-list; agent services = 7 methods; `records.Catalog()` projects specs as socket twins; `TestRegistryExactNames` is counted | `internal/narrow/actions.go`; `protocol/records.go`; `tools/toolset_test.go:155,243` |
| F18 | `doctor` takes no arguments and probes the socket and the narrow HTTP surface; `internal/doctor.Report` renders routes/retrieval/mountings/credentials/forge/bash-policy with no database facts | `cmd/aeon/doctor.go`; `internal/doctor/doctor.go` |
| F19 | `aeon db upgrade` without `--dry-run` refuses by design; with it, opening has already applied migrations and the output says so | `cmd/aeon/store_verbs.go:199-230` |
| F20 | `internal/secrets` is not readable in the planning sandbox — only its call sites are known; T0 lists its exported API and the key-file location by an owner-approved read | `cmd/aeon/serve.go:136`; `daemon/memory.go:688,706,727` |

## BK1 — A small slice that depends on the store only

Everything the slice needs is already in the tree: the store, the paths package, the endpoint probe, the socket and the
CLI. S44's settings are **adopted** by T10 and are not needed before it (BK11). S43-extra's seed ledger and audit tables,
S45–S54's tables, S44-extra's component tree: all of it is rows in the file; none of it is read by the backup path. The
export (BK14) is the only part that reads domain tables, and it does so through a registry whose sets report
*unavailable* when their table is absent — so it is correct whatever has merged.

## BK2 — What a backup contains, and what it does not (stated in the manifest, `doctor` and the UI)

**Contains:** every row of every table in `state.db` — records, history, transcripts (`chat_message`, `step_message`, tool
results), indexed repository text (`retrieval_chunk` and its FTS5/vec data), learnings, telemetry, the proposals and audit
tables. **A backup is therefore confidential**: transcripts and indexed source can contain anything a model read or a tool
printed, including a secret a person pasted. File mode 0600, directory 0700, never uploaded or sent by Aeon.

**Does not contain, ever, in this slice:**

- route API keys (kept by `internal/secrets`, F13, outside the db),
- `cursor.key` (beside the db, 0600),
- the forge token, narrow HTTP key, webhook secrets and MCP env **values** (environment only — only their *names* are in
  rows, F14),
- the MCP config file and blueprint files **until S46/S48 move them into the db** (T0 lists every state-dir file not in
  the db; the manifest field `outside_db` names them so a restore does not pretend to cover them),
- repositories and worktrees (git's).

The manifest says it in fields, not prose: `credentials_included: false`, `confidential: true`, `outside_db: [...]`. A
naive flag named `contains_secrets: false` would be a *false reassurance* (the transcripts may contain secrets), so that
name is **not** used. A restore onto a machine without the key files yields routes with `hasKey:false` (the existing
honest state, F13); a new `cursor.key` is generated and only invalidates pagination cursors.

## BK3 — The snapshot mechanism: SQLite's online backup API

**Decision:** copy the live database with SQLite's **online backup API** through the driver (`sqlite3.SQLiteConn.Backup`,
reached with `(*sql.Conn).Raw` on a dedicated connection from the **reader** pool, destination a second connection on a
temp file), in a single `step(-1)`.

Why, against the alternatives (each rejected with its reason):

| Option | Verdict |
| --- | --- |
| Copy `state.db` (+ `-wal`) with `cp` | **Rejected.** Without a checkpoint and a lock the pair is not consistent; F6 shows most data can be in the WAL; a copy mid-checkpoint is a corrupt file |
| `VACUUM INTO` | Consistent and compact, but it **recreates** schema objects: virtual tables (`vec0`, FTS5) are rebuilt through their modules and the shadow tables are re-inserted row by row — slow on large embedding tables and dependent on the module loading at read time. **VERIFY**-able, but it is the fallback, not the default |
| **Online backup API, `step(-1)`** | **Chosen.** Page-level copy: vec0 and FTS5 shadow tables, triggers, indexes and `sqlite_sequence` arrive **verbatim**; takes one read transaction on the source, which in WAL **does not block the single writer**; consistent to the instant it starts |
| `PRAGMA wal_checkpoint(TRUNCATE)` then copy | Rejected: blocks writers, still not atomic against a new write between checkpoint and copy |

Consequences, each a test (T2):

1. A single read snapshot: a writer hammering paired inserts (a transactional invariant — e.g. every `A` row has its `B`
   row, a running sum equals a counter) during the backup yields a copy where the invariant holds.
2. The copy's journal mode is normalised to `DELETE` on the **destination** connection before close, so the artefact is
   **one file** with no `-wal`/`-shm` (a restore must never inherit a stale WAL — BK9).
3. The long read transaction holds back WAL checkpointing for its duration; the cost is bounded by the db size and is
   documented in `doctor` ("WAL growing during a backup"). The task never runs the backup on the **writer** connection
   (DB15 / O6: reads must not queue behind writes).
4. `vec_version()` and an FTS5 `MATCH` work against the copy (acceptance 1); sqlite-vec registers through
   `sqlite3_auto_extension` for **every** connection opened after `RegisterExtensions()` (F7), so the destination
   connection is opened by the same code path.
5. The backup runs under a context; cancelling it removes the partial and releases the read transaction.

If T0 finds the driver's `Backup` unavailable under the build tags (it is part of the default cgo build; VERIFY on all six
targets), `VACUUM INTO` is the documented fallback and the same verification applies.

## BK4 — The `.aeonbak` container, the manifest, and atomic publication

A backup is **one file**, a plain **tar** (stdlib `archive/tar`, no compression by default — SQLite pages compress poorly
and speed matters; an optional gzip is OD7) with exactly two members, in this order:

1. `manifest.json` — written **last in the flow but first in the archive** (the archive is assembled after the db copy is
   verified, so it can hold the digest).
2. `state.db` — the verified snapshot.

Manifest (a closed shape; unknown fields rejected on read):

```json
{
  "format": 1,
  "name": "aeon-state",
  "created_at": "2026-10-30T12:00:00.000Z",
  "reason": "manual | scheduled | pre-migration",
  "aeon_version": "…",
  "applied_migrations": 51,
  "last_migration": "20261030130000_agent",
  "schema_fingerprint_sha256": "…",
  "tables": { "step": 18234, "event": 90112 },
  "db_bytes": 104857600,
  "db_sha256": "…",
  "journal_mode_in_file": "delete",
  "credentials_included": false,
  "confidential": true,
  "outside_db": ["mcp config file", "blueprint files"],
  "verified": "verified | unverified"
}
```

- **File name** `<name>-<UTC yyyymmddThhmmssZ>-v<applied_migrations>.aeonbak`, `<name>` = `aeon-state` (a setting-free
  constant; no user text reaches a filename). The name regex is a package constant used by `list`, `prune` and the
  guards (BK8).
- **Row counts** are taken from the **copy**, never the live db (no scan load on a store that is working).
- **Atomic publish:** build the db copy in `<dir>/.tmp-<id>/state.db` (0700 dir, 0600 file) → verify it → write the tar to
  `<name>.aeonbak.partial` (0600) → `fsync` file → `rename` to the final name → `fsync` the directory → remove the temp
  dir. A crash at any point leaves only `.tmp-*`/`.partial` names, which `list` ignores and a start-up sweep removes
  (BK8). **No file with a final name is ever incomplete** (acceptance 2).
- **Single-flight:** `<dir>/.backup.lock` created `O_EXCL` with pid and start time; a second run (daemon task, CLI direct
  door, pre-migration hook) refuses with a sentence naming the holder; a lock older than 6 h or whose pid is not alive is
  reclaimed and the reclaim is recorded (OD6).
- **Free space:** refuse when `free < 1.2 × (db + wal)` where the platform can say; where it cannot (Windows build,
  unusual fs) the result is the third state **"cannot say"**, recorded in the run, never a silent pass (BK17).

## BK5 — Verification, in three honest levels

| Level | What | When | Result shown |
| --- | --- | --- | --- |
| **Written** | archive readable, manifest parses, member sizes match, `db_sha256` matches | every run, always | `unverified` until level 2 |
| **Verified** | extract to a temp dir; `PRAGMA integrity_check` = `ok`; `PRAGMA foreign_key_check` empty; `vec_version()` works; schema fingerprint equals the manifest's; every table's row count equals the manifest's | after every run (setting `backup.verifyAfter`, default on) and on `backup verify` | `verified` |
| **Drill** | the temp copy is **opened by `store.Open`** and **migrated to current** (a temp dir, never the live state dir), then `integrity_check` again; proves the backup is restorable *by this binary* | `backup verify --drill` and in T11 | `verified+drill` with the migrations it applied |

`verified` is a fact **about a point in time** (the manifest says `verified: unverified` at write time, since the tar is
immutable once published; the verdict lives in `backup_run`, BK10). `backup.list` joins file ↔ latest verify row. A backup
with no verify row is shown as **unverified** and a retention policy never counts an unverified backup toward "keep the
newest good one" (BK8).

A **newer-than-this-binary** backup (the manifest's applied set contains a migration name unknown to `Migrations()`) is
*readable and verifiable at level 1* but **not** level 2/3 and never restorable (BK9) — the sentence names the unknown
migration and says to use a newer `aeon`.

## BK6 — The automatic backup before a migration

Placed **inside `store.Open`**, between the successful ping/GORM wrap and `s.Migrate()` (F1), so it covers every caller
(F2) and no verb can migrate around it.

```text
Open → ping → GORM wrap → [pre-migrate hook] → Migrate
                            │ pending == 0                    → nothing
                            │ store empty (no ledger rows)    → nothing (a fresh store has nothing to lose)
                            │ raw in-memory/DSN without a file → nothing (named in the run reason)
                            └ otherwise                       → backup (reason=pre-migration) → verified level 1+2
                                 ok      → Migrate
                                 failed  → REFUSE: Open returns an error naming why, where, and `--no-backup`
```

- **Mechanism:** `Options.PreMigrate func(ctx, *Store, []Migration) error`, wired by the **two callers** (the daemon in
  `serve.go`, `openStoreDirectly` in `store_verbs.go`) with the same constructor from `internal/backup`. `store` stays free
  of file/tar logic (F10): the hook receives the store and calls `s.SnapshotTo(...)` (BK3) through the package. A caller
  that passes no hook (tests, `SkipMigrations`) migrates as today; a **guard test** asserts both production callers pass
  it (a source walk over `cmd/aeon`, with a canary).
- **"Empty" is measured, not assumed:** the ledger has at least one applied row (F3/F4; T0 pins the exact check, which must
  not itself create the ledger).
- **Refuse by default.** Migrations are forward-only (F3); the pre-migration copy is the **only** undo. If the backup dir
  is unwritable, the disk is too full, the snapshot or its level-2 verification fails, **the open fails** and the file is
  untouched. `--no-backup` (accepted by `aeon serve` and the `db` verbs; **a flag, never an environment variable** —
  persistent silent opt-outs are how data gets lost) proceeds without a copy and prints, in the same breath, that none was
  taken. Its effect is limited to this one decision.
- **Where it goes — the bootstrap constraint:** `backup.dir` is a *setting* stored in the database being migrated, so it is
  **not readable yet**. The pre-migration backup therefore uses **`AEON_BACKUP_DIR` (env) else the default**
  `<state dir>/backups`, in a `pre-migration/` subdirectory. `backup.list` scans the effective dir **and** this one;
  `doctor` warns when they differ. (S44's CS4 precedence — env beats db beats default — is consistent: env is the only
  layer available at that moment.)
- **Retention:** the last `backup.keepPreMigration` (default **5**) are kept; older pre-migration backups are pruned by the
  same fenced function (BK8) after a **successful** migration, never before.
- **Recording:** the `backup_run` table may not exist yet (this slice's own migration creates it — BK10). The hook keeps
  the outcome in memory and `Open` writes the row **right after `Migrate` succeeds** (the table then exists). A refusal
  cannot write a row (the store is not opened); it is reported on stderr/log and surfaces in `doctor` as the absence of a
  recent backup. Recorded as a known limitation.
- **Concurrency:** two processes opening with pending migrations is a pre-existing race the framework's lock handles;
  the single-flight lock (BK4) makes the second one wait/refuse for the backup, not corrupt it.

## BK7 — Scheduled backups: a daemon maintenance task, **not** a trigger (and why AU1 is not touched)

S40's AU1 and S49 AN2 lock **authority records**: triggers and schedules decide *when an agent acts*, so they are
human-authored, audited, never seeded, and carry the three locks. A backup ticker decides nothing an agent does: it starts
no loop, calls no tool, spends no model money, touches no policy, and writes no `event_trigger`/`trigger_schedule`/
`trigger_fire` row. It is the same kind of thing as recovery, `ReconcileLoops` and the worktree reconciler (F15): daemon
**maintenance** with an operational setting. It is therefore:

- **not** an automation, not listed by S49's unified view, and **not** configurable by agents;
- **on by default** — the larger risk is data loss, the cost is disk, and the cost is visible (sizes in `backup.list`).
  `backup.enabled` (default `true`), `backup.interval` (default `24h`), `backup.keep…` (BK8);
- **restart-safe by construction:** the task wakes at start and hourly, reads the latest **successful** `backup_run` row
  and runs when `now − last_success ≥ interval` (no cron window to miss; a week of downtime is one catch-up, not seven);
- **single-flight** (BK4), **bounded** (context with a deadline = `backup.timeout`, default 30 min), and **polite**: it runs
  on a reader connection, at most one at a time, and skips (recorded `skipped: busy`) when the daemon is shutting down;
- **shutdown:** SIGTERM cancels its context (F12's `shutdownGrace` covers it); the partial is removed immediately or by
  the next start's sweep;
- audited as `actor_kind='daemon'` in `backup_run` (BK10), emits `backup.finished`/`backup.failed`/`backup.stale` events
  (BK16).

A guard (T6) asserts the ticker's package never imports `internal/trigger` write paths, never calls `loop.Start*`, and that
a full scheduled run changes **zero** rows in the trigger tables.

## BK8 — Retention, and the one delete this slice adds

Policy (settings, BK11): keep the **latest `backup.keep`** (default 7), plus the newest of each of the last
**`backup.keepDaily`** days (default 7) and **`backup.keepWeekly`** ISO weeks (default 4); `reason=pre-migration` backups
are governed only by `keepPreMigration` (BK6) and are never pruned by the others. Always retained, whatever the settings:
**the newest `verified` backup**.

All deletion goes through **one** function, `backup.removeBackupFile(dir, name)`:

1. `dir` must equal the resolved backup dir or its `pre-migration/` child (resolved with `EvalSymlinks` once at start);
2. `name` must match the package constant regex for `.aeonbak` (or the `.partial`/`.tmp-*` sweep patterns);
3. `Lstat` must say **regular file** (a symlink or directory is refused, never followed);
4. only then `os.Remove`.

Guards (T4): a **source walk** over `internal/backup` and `cmd/aeon` asserts `os.Remove`, `os.RemoveAll` and
`syscall.Unlink` appear **only** inside that function and the restore code's documented moves (T7), seeing ≥ 20 files; a
**canary** feeds it a path outside the dir, `state.db`, a symlink to `state.db`, a directory, `..`-traversal and a
look-alike name — each must be refused. Prune takes **no path argument from a person or an agent**: it works from the
directory listing and the policy; `aeon backup prune` has `--dry-run` (default shows the plan) and needs `--yes`.

## BK9 — Restore: human, CLI-only, daemon-down

Restore is **not a socket method, not a tool, not a catalog spec** — it overwrites the file the daemon owns, so a daemon
cannot do it by definition, and an agent or the narrow surface must never be able to ask. It exists as
`aeon backup restore FILE` with **only a direct door** (P5's offline half is the whole implementation), and the CLI
refuses to run it through a daemon. Preconditions and steps (each a test, T7):

1. **No daemon:** a *non-mutating* probe (`endpoint.Probe()`, new — `Claim()` reclaims stale sockets, F11, and must not be
   used here) dials the endpoint; anything answering ⇒ refuse naming the endpoint and peer.
2. **No other holder of the file:** open the live `state.db` with `locking_mode=EXCLUSIVE` and `BEGIN EXCLUSIVE`; `SQLITE_BUSY`
   ⇒ refuse ("another process has the database open: `aeon watch`, a CLI verb or a daemon on a different endpoint"). (A
   missing `state.db` is allowed: restore onto a fresh machine.)
3. **Verify the backup first** at level 2 (BK5) — checksum, integrity, fk, fingerprint, row counts — and refuse a corrupt or
   **newer-schema** backup. An **older**-schema backup is allowed: the next start migrates it (and takes its own
   pre-migration backup of the restored data, BK6).
4. **Print the plan and require `--yes`**: the live file's size and last-modified, the backup's created-at, schema and row
   counts, what will be kept (`state.db.pre-restore-<ts>/`), and what the backup does not contain (BK2).
5. **Stage:** extract to `<state dir>/.restore-<id>/state.db` (same filesystem as the target, 0600), `fsync`, re-verify the
   staged file, run the **drill** (BK5 level 3) on a *copy* so the live-to-be file is never opened for writing here.
6. **Keep the replaced database as a set:** move `state.db`, `state.db-wal` and `state.db-shm` (whichever exist) into
   `<state dir>/state.db.pre-restore-<UTC ts>/`. **The WAL and SHM move with it** — a stale `-wal` next to a restored `state.db`
   is the classic way to corrupt a restore, so the target directory must contain **no** `state.db-wal`/`-shm` when the
   restored file lands. This directory is never matched by the backup naming regex, so retention cannot touch it.
7. **Swap:** `rename` the staged file to `state.db`, `fsync` the directory, remove `.restore-<id>`; `chmod 0600`.
8. Do **not** start the daemon and do **not** open the store (no migration here); print what to do next.

Recovery from a bad restore is a documented manual `mv` of the pre-restore directory's three files back (recorded; no
"undo" verb — P9 states rollback is not offered). If a step fails after step 6 begins, the command restores the moved files
and says so; every failure path is tested with fault injection (T7).

Differs from the gap doc: G10 sketched `store.backup`/`store.restore` as **human-door daemon methods**. Restore cannot be a
daemon method (it needs the daemon down); backup methods are named `backup.*`.

## BK10 — The `backup_run` record (append-only)

One timestamped migration (`<date +%Y%m%d%H%M%S>_backup_run.sql`, never a legacy edit):

| Column | Meaning |
| --- | --- |
| `id INTEGER PRIMARY KEY AUTOINCREMENT` | |
| `kind` | CHECK `backup`, `verify`, `prune`, `restore-plan`, `export` |
| `reason` | CHECK `manual`, `scheduled`, `pre-migration`, `post-restore-check` |
| `actor_kind` | CHECK `user`, `daemon`, `cli` (never `agent` — agents have no write path here) |
| `started_at`, `finished_at` | ISO-8601 UTC (I9) |
| `outcome` | CHECK `ok`, `failed`, `refused`, `skipped` |
| `error_kind` | NULL or CHECK `disk-full`, `dir-unwritable`, `busy`, `lock-held`, `integrity`, `fk`, `checksum`, `newer-schema`, `io`, `cancelled`, `unknown` |
| `detail` | bounded text ≤ 512 bytes: a **fixed template** from enums and numbers, never raw error text (SEC-033) |
| `file_name` | basename only, NULL when none; matches the naming regex |
| `size_bytes`, `sha256`, `applied_migrations` | NULL where not applicable |
| `verify_level` | NULL, `written`, `verified`, `drill` |
| `free_space_state` | CHECK `ok`, `low`, `unknown` (BK17) |

`BEFORE UPDATE` and `BEFORE DELETE` triggers `RAISE(ABORT, 'backup_run is append-only')`. Rows are small and kept forever;
`file_name` is a *reference*, not a foreign key — history outlives the file (P3). `backup.status` computes "last success",
"last verified", "age" from these rows. `RecordBackupRun` is one store function; a package grep test asserts no other
writer. A restore is **not** recorded in the restored database (the file is replaced); the CLI prints the audit line and
writes `restore-<ts>.log` beside the pre-restore directory (plain text, no secrets).

## BK11 — Settings, with a seam that degrades to the environment

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `backup.enabled` | bool | `true` | the scheduled task only; manual and pre-migration are never switched off by it |
| `backup.dir` | path | `<state dir>/backups` | absolute; created 0700; **not read by the pre-migration hook** (BK6) |
| `backup.interval` | duration | `24h` | min `15m` |
| `backup.timeout` | duration | `30m` | per run |
| `backup.keep` / `keepDaily` / `keepWeekly` | int | `7` / `7` / `4` | `keep` ≥ 1 |
| `backup.keepPreMigration` | int | `5` | ≥ 1 |
| `backup.verifyAfter` | bool | `true` | level 2 after each run |
| `backup.staleAfter` | duration | `48h` | when `doctor`/events call the last success stale |

Code reads settings through a small interface (`backup.Settings{ String/Bool/Duration/Int(key) (value, source) }`).
**Before S44:** the implementation reads `AEON_BACKUP_DIR`, `AEON_BACKUP_ENABLED`, `AEON_BACKUP_INTERVAL`, … (names are the
key with dots → underscores, upper-cased, prefix `AEON_`), then defaults. **After S44 (T10):** S44's key registry declares
the same keys (descriptions, types, ranges — **no credential**, CS5), and the implementation becomes env → `setting` table →
default (S44 CS4). Adoption changes one constructor; the keys, defaults and the pre-migration exception do not move. The
paths are validated at set time and again at use time (absolute, not inside another process's state dir, not a symlink to
elsewhere — T4).

## BK12 — The daemon surface (P34: socket first), classification, and guards

| Method | Params | Result | Door |
| --- | --- | --- | --- |
| `backup.run` | `{reason?: "manual", verify?: bool}` | `{run: BackupRunView, backup?: BackupView}` | human socket (writes a file) |
| `backup.list` | `{limit?, includePreMigration?}` | `{backups: BackupView[], dirs: [...]}` newest first, each with size, age, schema, reason, verify state, `canRestoreWithThisBinary` | read; open |
| `backup.verify` | `{file, drill?: bool}` | `{run, report}` | human socket (heavy: CPU/disk) |
| `backup.status` | `{}` | `{enabled, dir, lastSuccess?, lastVerified?, ageSeconds?, state: "never"\|"fresh"\|"stale"\|"failing", nextDueAt?, running?, settings: [{key,value,source}], db: {bytes, walBytes, journalMode, applied, pending}}` | read; open (the agent twin) |
| `backup.prune` | `{dryRun?: bool}` | `{plan: [...], removed: [...]}` | human socket |
| `export.run` | `{sets[], dir, includeMemory?}` | `{dir, sets: [{name, files, rows, state}]}` | human socket (writes files) |
| `export.sets` | `{}` | `{sets: [{name, state: "available"\|"unavailable", tables, rows, description}]}` | read; open (agent twin) |

- `BackupView` carries the file *basename* and manifest facts; **no absolute path of anything but the configured dir**; no
  database content. `file` params take a **basename** (resolved inside the known dirs) or an absolute path for `verify`
  only when the CLI direct door runs it; the daemon method refuses path separators (no arbitrary-file reads on the wire).
- **Restore is absent** (BK9). Settings are changed through S44's `settings.*` (or the environment before S44) — S62 adds no
  second settings writer.
- New error-code block (≥ 1 code, T0 records the free range): `ErrBackupRefused` (reason in `what`, three-part data:
  what / why / do) for busy, lock-held, dir-unwritable, disk-full, etc.
- **Guards (P4, sibling tests, never editing an existing guard body):** the positive half (the seven are registered,
  `RequiresInitialize`, not `Debug`, absent when their service is nil — V13); the negative half: absent from `narrow.Actions()`
  and `BoundaryStatement()`, from `AgentServiceMethods()`, and **no `restore` method or spec exists anywhere** (literal and
  constant grep over non-test files of `internal/narrow`, `internal/agentsvc`, `internal/tools`, `internal/records`,
  `internal/protocol` — ≥ 50 files seen); `records.Catalog()` has exactly two specs for this slice
  (`backup_status`, `export_sets`), both `Mutates:false`; a **canary** feeds each negative predicate a synthetic violation.
  S47's surface table must classify these seven methods (T0 records whether S47 has merged; if so a guard asserts the
  classification).
- Method set and the CLI verbs are a **parity test in both directions** (P5), with `restore` the one explicit CLI-only
  exception (listed, with this rule as its reason).

## BK13 — Agent tools: read-only, two

`backup_status` (twin of `backup.status`) and `export_sets` (twin of `export.sets`), `Mutates:false`, class safe, schemas
`additionalProperties:false`, descriptions ≥ 40 chars, registered-not-granted, `TestRegistryExactNames` bumped by exactly
**two**, counted and dated. They return counts, ages and states — never a path outside the configured dir's name, never a
manifest field that names a file's content. **There is deliberately no write tool** (rule 2's "every management operation
gets a tool" has a recorded exception): `backup.run`, `verify`, `prune` and `export.run` write files outside the store (or
spend disk and CPU), restore overwrites the database, and a loop that can trigger heavy disk work or choose a directory is
a primitive this slice will not add. An agent that notices a stale backup says so to a person (S58), who runs it.

## BK14 — Export of managed records: a registry, an allow-list, a deterministic tree

`aeon export --records agents,policies,… --dir DIR` writes a **read-only snapshot of managed records as text**, for review
and for `git diff`. It is not a backup (no history, no transcripts) and has **no importer**: restore is whole-database
only, and a person who wants a record back copies it by hand through the normal management surface.

- **Registry:** `internal/export` holds `Exporter{Name, Description, Tables []string, Read func(ctx, Reader) ([]File, error)}`.
  Each management slice **declares its exporter beside its seed set** (rule 6's pattern) — this slice ships the exporters
  for records that exist **today**; later slices add theirs. An exporter whose table is absent in the file is reported
  **`unavailable (slice not built)`**, never as an empty export (P31's third state).
- **Sets shipped now (T0 confirms each accessor/table):** `agents` (S43 `agent` rows, content verbatim), `policies`,
  `routes` (provider, model, base_url, prices, extra_body — **no key**), `components` (the S44-extra tree: parent, settings,
  roots), `triggers` (schedules + event triggers: authority records, exported read-only), `goals` (epics, goals, briefs
  metadata), `settings` (S44, if its table exists: **non-sensitive** keys only), `memory` (learnings — opt-in
  `--include-memory`, because they are personal). Blueprint, MCP and seed-ledger exporters land with S48/S46/S43-extra.
- **Allow-list, not deny-list:** each exporter names the columns it writes; the generic reader (`store.ExportRows`) takes
  table and column identifiers **only from Go constants** (validated against `sqlite_master`; no value from a person or a
  tool becomes an identifier). A planted-secret test seeds credential-looking strings in every free-text column the
  exporters read and asserts the output contains them **only where the column is a declared content field** — and a
  separate test asserts the **forbidden columns** (any column named like a key/secret/token) are never selected.
- **Deterministic:** files sorted by a stable key; JSON with sorted keys and a fixed indent; **no timestamps inside record
  files** (a top-level `export.json` carries `generated_at`, per-set counts and the schema count); LF endings; the same
  store exported twice is byte-identical (acceptance 7).
- **Layout:** `DIR/export.json`, `DIR/README.md` (what this is, what it is not), `DIR/<set>/<id-or-slug>.json`. `DIR` must be
  absolute and **empty or non-existent** (refuse otherwise — no merging into a git working tree by accident), 0700/0600.
- **Doors:** `export.run`/`export.sets` (BK12), direct door for the CLI when the daemon is down (read-only open with
  `SkipMigrations` so an export never migrates), no agent write tool (BK13).

## BK15 — The CLI (P5), `db` unchanged, `doctor` gains database facts

`aeon backup run [--no-verify] [--json]`, `list [--json]`, `verify FILE [--drill] [--json]`, `status [--json]`,
`prune [--dry-run] [--yes]`, `restore FILE [--yes] [--plan]`; `aeon export …` (BK14).

- Each verb except `restore` is a **client of its socket method**; `selectDoor()` dials once — "no daemon" ⇒ the direct
  door and the fixed stderr line `daemon not running — applied directly to the store`; any other dial failure refuses
  (P5). The **direct door never runs migrations**: it opens with `SkipMigrations: true` (so a backup of an old-schema store
  captures it as it is, and `aeon backup run` can never trigger a migration). That is a **listed adaptation** to the
  `Options.SkipMigrations` doc comment ("no CLI path sets it") — comment only, T5.
- The CLI re-validates nothing; `--json` is the wire result verbatim; `--yes` is a seatbelt; no flag accepts a secret.
- `aeon db info|upgrade|query` keep their behaviour and bytes (T1 goldens). `db info` additionally prints one **backup
  line** after its existing output (new line, existing lines untouched — a listed golden change).
- **`aeon doctor`** (today takes no arguments, F18) adds four lines, all store-free (a directory scan + manifest reads, plus
  `backup.status` when a daemon answers): `database   <path> · <bytes> + <wal bytes> WAL · journal <mode> · <N> applied /
  <M> pending`, `backups    last <age> (verified|unverified) · <count> kept · <dir>` / `backups    never backed up — run
  \`aeon backup run\``, `backups    stale: last success <age> ago (limit <staleAfter>)`, and `backups    pre-migration dir differs
  from backup.dir (…)`. `doctor` **never opens the store** (opening migrates).

## BK16 — Events and the S58 contract

Emitted with fixed payloads (ids, enums, numbers; no paths beyond the basename): `backup.finished`
`{runId, reason, sizeBytes, verifyLevel}`, `backup.failed` `{runId, reason, errorKind}`, `backup.stale`
`{ageSeconds, limitSeconds}` (emitted once per transition to stale, and re-armed by the next success), `backup.pruned`
`{removed}`. `notifyHintFor`'s closed set (F16) is **S58's** to extend — this slice adds no hint, so no new desktop
notification until S58 decides to. The kinds are registered in `protocol/events.go` with the S62 comment so S58 can
subscribe to a stable name.

## BK17 — Portability and honesty

Six build targets exist (S1). Unix gets `0600`/`0700` and `Statfs`; **Windows** has neither POSIX modes nor `Statfs` the
same way: the implementation is build-tagged (`diskfree_unix.go` / `diskfree_windows.go`), file modes are best-effort and
`doctor` says "permissions are the user profile's ACL on this platform" instead of claiming 0600; the free-space check is
the third state **"cannot say"** where unavailable; the daemon-down probe uses the endpoint package's named-pipe path
(F11). Network and unusual filesystems (NFS, SMB, FAT, exFAT) can break `rename` atomicity and `fsync` semantics: the backup
dir and state dir are checked for a **same-filesystem** rename on restore (`EXDEV` ⇒ refuse with a sentence) and a
`doctor` line flags a backup dir on a different device than the state dir as *informational* (that is often what a person
wants — a second disk). Every "I cannot tell" is a stated state, never a pass.

## BK18 — Security notes and the common "Do NOT"

No new outward surface (nothing on the narrow surface, nothing outbound); the backup dir and files are the person's
files under the person's account (P26). No secret is read, written, logged or placed in a manifest, audit row or event.
Errors carry fixed templates. The only deletes are BK8's fenced function and the documented moves in restore.

Do not edit `schema.go` or a shipped migration; do not edit an existing guard body; do not add an environment-variable
opt-out for the pre-migration backup; do not accept a path from an agent; do not stamp a placeholder actor; do not put
raw error text in an audit row; do not run a backup on the writer connection; do not open the store from `doctor`; do not
add a dependency; do not edit `contract.ts` by hand; do not soften a failed or skipped check.

## Open decisions (each with a recommended default; none blocks starting)

| # | Decision | Default |
| --- | --- | --- |
| OD1 | Snapshot mechanism | **Online backup API**, `VACUUM INTO` as documented fallback (BK3) |
| OD2 | Pre-migration backup failure | **Refuse the open** unless `--no-backup` (BK6) |
| OD3 | Schedule default | **On, every 24 h**, plus pre-migration (BK7) |
| OD4 | Include vector/FTS data | **Always verbatim** (reindex costs embedding spend and time, S22/S54); a `--slim` variant is **not** built |
| OD5 | Restore location of the replaced db | A **directory** `state.db.pre-restore-<ts>/` holding the db/WAL/SHM set (instead of three loose suffixed files) |
| OD6 | Single-flight lock staleness | 6 h or dead pid ⇒ reclaim and record |
| OD7 | Compression of `.aeonbak` | **None** by default; optional gzip later |
| OD8 | Pre-migration `backup.dir` | **env or default only** (the setting is in the db being migrated) |
| OD9 | Encryption of the backup | **Deferred**; permissions are the control (P26); revisit with S59's secret handling |
| OD10 | Export of memory (learnings) | **Opt-in** `--include-memory` |
| OD11 | Backup name constant | `aeon-state` (no user text in file names) |
