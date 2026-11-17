package store

// backup.go is the store's half of S62: everything that needs SQL or the
// driver lives here (I2), and `internal/backup` (S62 T4) builds the container,
// the naming and the retention on top of it without importing SQL at all.
//
// Two capabilities:
//
//   - SnapshotTo — a CONSISTENT ONLINE snapshot of the live database into a
//     file, taken on a READER connection (DB15/O6: the writer pool is never
//     touched, so a backup never queues behind writes and a write never
//     queues behind a backup). The mechanism is SQLite's online backup API
//     (S62 T0, OD1), proven on this driver: it copies page-by-page under WAL,
//     leaves the destination a valid single-file database, and carries the
//     virtual-table data (vec0's shadow tables, the FTS5 indexes) verbatim.
//
//   - CheckFile — read-only verification of a database FILE: integrity,
//     foreign keys, fingerprint, per-table row counts, the ledger, vec and
//     FTS5 availability. It opens the file with a dedicated read-only
//     connection and creates nothing next to it (T0 measured the DSN).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/mattn/go-sqlite3"
)

// snapshotBatchPages is how many pages one Step copies before the context is
// re-checked. Step(-1) would copy the whole database in one call with no
// cancellation point; a batch leaves a check between batches, which is what
// makes a cancelled backup delete its partial file instead of finishing it.
// 1000 pages is ~16 MiB: small enough that a cancel lands within one batch,
// large enough that a 76 MiB store is a few iterations, not thousands.
const snapshotBatchPages = 1000

// SnapshotInfo is what a snapshot produced. Counts only — no path, no
// content: the caller owns where the file went and what is in it.
type SnapshotInfo struct {
	Bytes       int64
	Duration    time.Duration
	PagesCopied int
}

// SnapshotTo takes a consistent online snapshot of the live database into
// destPath.
//
// The reader pool supplies the source connection (the backup reads the
// database's snapshot of its instant — every page is from one consistent
// moment even while writers commit). The destination is opened with the same
// driver (so the same extension registrations apply to it), and the whole
// init → step → finish nests inside BOTH connections' Raw callbacks: the
// *SQLiteConn a Raw callback hands out is valid only while the callback runs,
// and using it after Raw returns segfaults (S62 T0 measured the crash).
//
// On success the destination is switched to journal_mode=DELETE and closed,
// so the artefact is ONE file — no -wal, no -shm (BK3.2): a two-file backup
// copied by half is an undetectable corruption, and a single file is the
// unit the container, the checksum and the restore all think in.
//
// On any failure — including a cancelled context — the destination file is
// removed, because a partial file passed to the container would be a backup
// that claims consistency it does not have.
func (s *Store) SnapshotTo(ctx context.Context, destPath string) (SnapshotInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.r == nil || s.Closed() {
		return SnapshotInfo{}, fmt.Errorf("store: %s is closed", s.path)
	}
	parent := filepath.Dir(destPath)

	// Claim the destination atomically: O_EXCL refuses an existing file (a
	// backup that overwrote its predecessor destroyed the recovery of a
	// recovery), and the 0600 mode is BK3's — the file is the database, so
	// only its owner reads it.
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: snapshot destination parent %s: %w", parent, err)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return SnapshotInfo{}, fmt.Errorf("store: snapshot destination %s already exists; a backup never overwrites another backup", destPath)
		}
		return SnapshotInfo{}, fmt.Errorf("store: create snapshot destination %s: %w", destPath, err)
	}
	_ = f.Close()

	start := time.Now()
	succeeded := false
	defer func() {
		// Every failure path removes the destination, whatever state it is in.
		if !succeeded {
			_ = os.Remove(destPath)
		}
	}()

	// The source: one reader-pool connection for the whole copy. The writer
	// pool is never touched (O6) — the test that proves it closes s.w first.
	srcConn, err := s.r.Conn(ctx)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: snapshot reader connection: %w", err)
	}
	defer func() { _ = srcConn.Close() }()

	// The destination: the same driver name, so the auto-registered
	// extensions (sqlite-vec) apply to it too. _foreign_keys=1 keeps the
	// pragma honest if the file is ever re-opened for anything but a copy.
	database.RegisterExtensions()
	destDB, err := sql.Open(database.DriverName, "file:"+destPath+"?_foreign_keys=1")
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: open snapshot destination: %w", err)
	}
	defer func() { _ = destDB.Close() }()
	destConn, err := destDB.Conn(ctx)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: snapshot destination connection: %w", err)
	}
	defer func() { _ = destConn.Close() }()

	var pages int
	backupErr := srcConn.Raw(func(srcDriver any) error {
		src, ok := srcDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("store: snapshot source is not a sqlite connection: %T", srcDriver)
		}
		return destConn.Raw(func(destDriver any) error {
			dest, ok := destDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("store: snapshot destination is not a sqlite connection: %T", destDriver)
			}
			bk, err := dest.Backup("main", src, "main")
			if err != nil {
				return fmt.Errorf("store: begin the backup: %w", err)
			}
			defer func() { _ = bk.Finish() }()

			// A writer holding the write lock makes Step answer BUSY, which
			// the driver reports as (false, nil) — no error, no progress.
			// Retrying that hot would live-lock a backup against a saturated
			// writer, so a step that copied nothing yields a millisecond
			// before trying again. Progress never sleeps.
			stuck := 0
			lastRemaining := bk.Remaining()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				done, err := bk.Step(snapshotBatchPages)
				if err != nil {
					return fmt.Errorf("store: copy the pages: %w", err)
				}
				if done {
					break
				}
				if remaining := bk.Remaining(); remaining == lastRemaining {
					if stuck++; stuck > 100 {
						time.Sleep(time.Millisecond)
						stuck = 0
					}
				} else {
					stuck = 0
				}
				lastRemaining = bk.Remaining()
			}
			pages = bk.PageCount() - bk.Remaining()
			return nil
		})
	})
	if backupErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SnapshotInfo{}, ctxErr
		}
		return SnapshotInfo{}, backupErr
	}

	// One file: switch the destination off WAL before it closes, so no
	// sidecars are left beside the artefact (T0 measured the result).
	if _, err := destDB.ExecContext(context.Background(), "PRAGMA journal_mode=DELETE"); err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: single-file the destination: %w", err)
	}
	if err := destDB.Close(); err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: close the destination: %w", err)
	}

	st, err := os.Stat(destPath)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("store: stat the snapshot: %w", err)
	}
	succeeded = true
	return SnapshotInfo{
		Bytes:       st.Size(),
		Duration:    time.Since(start),
		PagesCopied: pages,
	}, nil
}

// FileReport is the read-only verdict on one database file. It is the
// verification payload T4's three-level check and T7's restore pre-checks
// consume: every field is a measurement of the file, never of the store.
type FileReport struct {
	// IntegrityOK is true only for a full `PRAGMA integrity_check` that
	// answered exactly `ok`. quick_check is deliberately not used: it skips
	// the b-tree traversal, which is precisely where a torn page hides.
	IntegrityOK bool
	// IntegrityDetail is the check's own answer, at most 20 lines — the
	// report says which check ran by carrying what it said.
	IntegrityDetail []string
	// ForeignKeyViolations counts the `PRAGMA foreign_key_check` rows.
	ForeignKeyViolations int
	// Fingerprint is the normalised schema shape (fingerprint.go).
	Fingerprint Fingerprint
	// Tables is the row count per table and view, names read from
	// sqlite_master and quoted — never supplied by a caller.
	Tables map[string]int64
	// Applied is the ledger's names, oldest first; empty for a file with no
	// ledger (fresh or pre-migration), which is a state, not an error.
	Applied []string
	// VecVersion is the extension's self-report, or "" when the function is
	// absent (a binary without cgo, or a file checked by one).
	VecVersion string
	// FTSUsable is the probe's verdict when the retrieval fts table is in
	// the file; when the table is absent there is nothing to probe, and the
	// report says so by the table being absent from Tables.
	FTSUsable bool
	// JournalMode is the file's own answer, for the one-file proof.
	JournalMode string
}

// integrityDetailCap is how many of the check's own lines the report keeps.
// Twenty is enough to name the problem; a thousand is a log, not a report.
const integrityDetailCap = 20

// CheckFile verifies a database file WITHOUT touching it: a dedicated
// read-only connection (mode=ro, no migrations, no sidecars — T0 measured
// that this DSN creates nothing beside a journal_mode=DELETE artefact), and
// every measurement is a read.
func CheckFile(ctx context.Context, path string) (FileReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := os.Stat(path); err != nil {
		return FileReport{}, fmt.Errorf("store: check %s: %w", path, err)
	}
	// The extension must be registered for THIS process before the connection
	// opens, or vec_version() is a no-such-function on a healthy file.
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, "file:"+path+"?mode=ro&_foreign_keys=1")
	if err != nil {
		return FileReport{}, fmt.Errorf("store: open %s read-only: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	// One connection: the checks share one read instant, and a pool here
	// would only add sidecar pressure to the one thing the file must not
	// gain.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return FileReport{}, fmt.Errorf("store: check %s: the file will not open read-only: %w", path, err)
	}

	rep := FileReport{Tables: map[string]int64{}}

	// Integrity: the FULL check, and its own words in the report.
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return FileReport{}, fmt.Errorf("store: integrity check: %w", err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			return FileReport{}, fmt.Errorf("store: read the integrity check: %w", err)
		}
		if len(rep.IntegrityDetail) < integrityDetailCap {
			rep.IntegrityDetail = append(rep.IntegrityDetail, line)
		}
	}
	if err := rows.Close(); err != nil {
		return FileReport{}, fmt.Errorf("store: read the integrity check: %w", err)
	}
	rep.IntegrityOK = len(rep.IntegrityDetail) == 1 && rep.IntegrityDetail[0] == "ok"

	// Foreign keys: one row per violation.
	fkRows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return FileReport{}, fmt.Errorf("store: foreign key check: %w", err)
	}
	for fkRows.Next() {
		var (
			table, parent string
			rowid, fkID   int64
		)
		if err := fkRows.Scan(&table, &rowid, &parent, &fkID); err != nil {
			_ = fkRows.Close()
			return FileReport{}, fmt.Errorf("store: read the foreign key check: %w", err)
		}
		rep.ForeignKeyViolations++
	}
	if err := fkRows.Close(); err != nil {
		return FileReport{}, fmt.Errorf("store: read the foreign key check: %w", err)
	}

	// Journal mode: the file's own answer.
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&rep.JournalMode); err != nil {
		return FileReport{}, fmt.Errorf("store: read the journal mode: %w", err)
	}

	// The schema shape, through the one definition (fingerprint.go).
	if rep.Fingerprint, err = fingerprint(db); err != nil {
		return FileReport{}, fmt.Errorf("store: fingerprint %s: %w", path, err)
	}

	// The catalogue: names straight from sqlite_master and quoted, so no
	// identifier ever comes from a caller.
	objRows, err := db.QueryContext(ctx, `SELECT type, name FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return FileReport{}, fmt.Errorf("store: read the catalogue: %w", err)
	}
	var objects []string
	for objRows.Next() {
		var (
			kind string
			name string
		)
		if err := objRows.Scan(&kind, &name); err != nil {
			_ = objRows.Close()
			return FileReport{}, fmt.Errorf("store: read the catalogue: %w", err)
		}
		objects = append(objects, name)
	}
	if err := objRows.Close(); err != nil {
		return FileReport{}, fmt.Errorf("store: read the catalogue: %w", err)
	}
	for _, name := range objects {
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+quoteIdent(name)).Scan(&n); err != nil {
			return FileReport{}, fmt.Errorf("store: count %s: %w", name, err)
		}
		rep.Tables[name] = n
	}

	// The ledger: names only, oldest first. A missing ledger is a STATE — a
	// fresh or pre-migration file — not an error; any other failure is.
	ledgerRows, err := db.QueryContext(ctx, `SELECT name FROM _migrations ORDER BY applied_at, name`)
	if err != nil {
		if !strings.Contains(err.Error(), "no such table: _migrations") {
			return FileReport{}, fmt.Errorf("store: read the ledger: %w", err)
		}
	} else {
		for ledgerRows.Next() {
			var name string
			if err := ledgerRows.Scan(&name); err != nil {
				_ = ledgerRows.Close()
				return FileReport{}, fmt.Errorf("store: read the ledger: %w", err)
			}
			rep.Applied = append(rep.Applied, name)
		}
		if err := ledgerRows.Close(); err != nil {
			return FileReport{}, fmt.Errorf("store: read the ledger: %w", err)
		}
	}

	// The vector extension's self-report; absent means the process that ran
	// the check has no cgo build, and the report says "unknown" by "".
	if err := db.QueryRowContext(ctx, "SELECT vec_version()").Scan(&rep.VecVersion); err != nil {
		rep.VecVersion = ""
	}

	// The FTS5 probe: a MATCH against the retrieval index, when the index is
	// in the file. The word is a literal, so the query can only fail if the
	// index is damaged — which is the thing being tested.
	if _, hasFts := rep.Tables["retrieval_chunk_fts"]; hasFts {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM retrieval_chunk_fts WHERE retrieval_chunk_fts MATCH 'chunk'`).Scan(&n); err != nil {
			rep.FTSUsable = false
		} else {
			rep.FTSUsable = true
		}
	}
	return rep, nil
}

// quoteIdent quotes a SQL identifier read from sqlite_master. The names there
// are the database's own, but a name with a quote in it is still a name with
// a quote in it, and doubling the quote is the whole of the quoting rule.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// AppliedNamesAndPending answers the F4 question — "does this store hold
// anything worth a pre-migration backup?" — the way T0 proved it: no ledger
// at all, or a ledger with no applied row, means the store is EMPTY (a fresh
// shape, or a failed migration whose transaction rolled back and left no
// work to back up).
//
// The call creates NOTHING: both probes are reads, and the empty answer never
// initialises the file. That is the property T5's open hook depends on — the
// hook may ask "is there anything to back up?" on every open, including
// opens of brand-new directories, without turning the question into an
// answer.
func (s *Store) AppliedNamesAndPending() (applied []string, pending []Migration, empty bool, err error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = '_migrations'`).Scan(&n); err != nil {
		return nil, nil, false, fmt.Errorf("store: probe the ledger: %w", err)
	}
	if n == 0 {
		return nil, Migrations(), true, nil
	}
	rows, err := s.r.Query(`SELECT name, status FROM _migrations ORDER BY applied_at, name`)
	if err != nil {
		return nil, nil, false, fmt.Errorf("store: read the ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]bool)
	appliedCount := 0
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err != nil {
			return nil, nil, false, fmt.Errorf("store: read the ledger: %w", err)
		}
		seen[name] = true
		if status == "applied" {
			applied = append(applied, name)
			appliedCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("store: read the ledger: %w", err)
	}
	if appliedCount == 0 {
		return nil, Migrations(), true, nil
	}
	for _, m := range Migrations() {
		if !seen[m.Name] {
			pending = append(pending, m)
		}
	}
	return applied, pending, false, nil
}

// KnownMigrationNames is the closed set of migration names, for the
// newer-schema check: a backup whose ledger names a migration the current
// binary does not know is NEWER than the binary, and restoring it would
// downgrade the schema.
func KnownMigrationNames() map[string]bool {
	names := make(map[string]bool, len(Migrations()))
	for _, m := range Migrations() {
		names[m.Name] = true
	}
	return names
}
