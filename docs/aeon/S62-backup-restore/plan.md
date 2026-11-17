# Plan — S62 Backup, export & restore

## What is settled before planning

`brief.md` (the measured gap and acceptance) and `design.md` BK1–BK18 + eleven open decisions with defaults. Short
version: the database is the only source of truth (rule 6) and has **no** backup, no restore and no safety net before
forward-only migrations. The slice takes a consistent **online snapshot** with SQLite's backup API into one tar-packed
`.aeonbak` (manifest + `state.db`), publishes it atomically at 0600/0700, **verifies** it in three honest levels, takes one
**automatically inside `store.Open` before any pending migration** (refusing to migrate without it unless `--no-backup`),
runs a **daemon maintenance task** (not a trigger) on a restart-safe interval with an append-only `backup_run` record and
fenced retention, offers a **human, daemon-down, CLI-only restore** that keeps the replaced file set, and a deterministic,
secret-free **export of managed records**. Daemon first (`backup.*`, `export.*`), CLI as a client with an offline direct
door, two read-only agent tools, honest `doctor` lines, a Settings panel.

**This slice is independent and small, and is meant to be built before the autonomy slices (S55–S61, S27).** It needs only
the store. S44's settings are adopted last (T10); until then the same keys come from `AEON_BACKUP_*` environment variables.

## How we will do it

Characterize first (T1 — goldens of `db info`, `db upgrade --dry-run`, the state-dir listing after `Open`, doctor output on
the untouched code). Then the two storage pieces in parallel — the snapshot/verification SQL (T2) and the `backup_run`
migration (T3) — then the file-level core (T4: container, manifest, publish, lock, run/verify/list, retention). The
migration-safety hook (T5) is the highest-value piece and lands right after T4. Then the doors: daemon service, ticker,
events and guards (T6), restore (T7), export (T8), the CLI and doctor (T9). Settings adoption and the UI (T10) wait for S44.
Close-out (T11) runs the drills, including the three that matter: a backup under concurrent writes, `kill -9` mid-backup,
and a restore round trip.

## Task sequence

### T0 — Re-verify, inventory, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens: `db info`, `db upgrade --dry-run`, the state dir after `Open`, doctor ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — The store's half: online snapshot, file checks, row counts ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T3.

### T3 — The `backup_run` migration and its store accessors ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1.** Parallel-safe with T2.

### T4 — `internal/backup`: container, manifest, publish, lock, run/verify/list/status, fenced retention ([`tasks/T4.md`](tasks/T4.md))
**Depends: T2, T3.**

### T5 — The pre-migration backup inside `store.Open`, `--no-backup` ([`tasks/T5.md`](tasks/T5.md))
**Depends: T4.**

### T6 — The daemon: service, maintenance task, `backup.*` methods, events, read-only tools, guards ([`tasks/T6.md`](tasks/T6.md))
**Depends: T4.** Parallel-safe with T5, T7, T8.

### T7 — Restore: CLI-only, daemon-down, atomic, keeps the replaced set ([`tasks/T7.md`](tasks/T7.md))
**Depends: T2, T4.** Parallel-safe with T5, T6, T8.

### T8 — Export of managed records: registry, allow-list reads, `export.*`, `aeon export` ([`tasks/T8.md`](tasks/T8.md))
**Depends: T1.** Its daemon methods extend T6's guards (listed there); parallel-safe with T4–T7.

### T9 — The CLI client, offline direct door, `doctor` and `db info` lines ([`tasks/T9.md`](tasks/T9.md))
**Depends: T5, T6, T7, T8.**

### T10 — Adopt S44 settings; the Settings → Backups panel ([`tasks/T10.md`](tasks/T10.md))
**Depends: T6, T9, and S44 merged** (S44 T8's generic section for the settings half; the list/actions half needs only T6).

### T11 — Close-out: acceptance re-run, drills, demo walk, the record ([`tasks/T11.md`](tasks/T11.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 ─┐
         ├─ T3 ─┴─ T4 ─┬─ T5 ─────────┐
         │              ├─ T6 ─────────┤
         │              └─ T7 ─────────┼─ T9 ─ T10 ─ T11
         └─ T8 ───────────────────────┘      (S44 merged)
```

## Risks

| Risk | Mitigation |
| --- | --- |
| A backup is inconsistent under concurrent writes, or loses vec0/FTS5 data | online backup API on a reader connection, `step(-1)`; paired-insert invariant test with a hammering writer; `vec_version()` + FTS5 `MATCH` + row-count equality on the copy (T2) |
| The backup becomes the thing that blocks starting the daemon | the pre-migration hook refuses only when migrations are *pending on a non-empty store*; zero cost otherwise; the refusal names the reason and `--no-backup` (T5) |
| A restore corrupts the store (stale WAL, half-written file, wrong schema) | daemon-down + exclusive-lock proofs, staged file on the same filesystem, WAL/SHM moved with the old db, fault injection on every step, newer-schema refusal (T7) |
| Prune deletes something it should not | one fenced function; regex + dir + `Lstat` regular-file checks; source-walk guard + canary (T4) |
| The ticker looks like an automation and drags AU1 in | recorded argument (BK7), a guard that it touches no trigger table and calls no loop start (T6) |
| A backup leaks a secret | credential stores never included; manifest states `credentials_included:false`/`confidential:true`; no outbound; export is allow-listed and planted-secret tested (T4, T8) |
| Disk fills with backups or a backup fills the disk | free-space check (3-state), retention, `staleAfter`/size shown in `backup.list` and `doctor` (T4, T9) |
| Windows/odd filesystems break atomicity or modes | build-tagged helpers, same-filesystem check on restore, "cannot say" states, honest `doctor` wording (T4, T7, T9) |
| The pre-migration backup dir cannot come from a setting | env-or-default only, `doctor` warns on divergence (BK6/OD8) |
