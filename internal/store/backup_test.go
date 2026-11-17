package store

// backup_test.go — S62 T2's behavioural tests, one per spec row.
//
// The fixture discipline: every store is a throwaway temp dir, and the heavy
// fixture (the bulk BLOB table the cancel and corruption tests need) is built
// small enough to stay fast under -race. The 100 MB timing case runs once by
// hand and is recorded in the task Notes rather than in CI.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/database"
)

// bkBulkRows is the bulk fixture's row count: 2048 rows × 32 KiB ≈ 64 MiB,
// ~16,000 pages — 16 Step batches, so a cancel lands between batches with a
// partial file on disk, and the corruption flip has a data page to hit.
const bkBulkRows = 2048

// bkBulkRowSize is one bulk row's BLOB in bytes (32 KiB: eight pages).
const bkBulkRowSize = 32 * 1024

// bkOpenStore opens a migrated store in a temp dir.
func bkOpenStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// bkAddBulk fills the store with the BLOB fixture and returns the table name.
func bkAddBulk(t *testing.T, s *Store) string {
	t.Helper()
	if _, err := s.w.Exec(`CREATE TABLE zz_bk_bulk (id INTEGER PRIMARY KEY, blob BLOB NOT NULL)`); err != nil {
		t.Fatalf("create the bulk table: %v", err)
	}
	chunk := make([]byte, bkBulkRowSize)
	for i := 0; i < bkBulkRows; i++ {
		if _, err := s.w.Exec(`INSERT INTO zz_bk_bulk (id, blob) VALUES (?, ?)`, i, chunk); err != nil {
			t.Fatalf("bulk row %d: %v", i, err)
		}
	}
	return "zz_bk_bulk"
}

// bkSnapshot takes the snapshot the test needs, failing with the error if it
// does not happen.
func bkSnapshot(t *testing.T, s *Store, dir string) (string, SnapshotInfo) {
	t.Helper()
	dest := filepath.Join(dir, "snap.db")
	info, err := s.SnapshotTo(context.Background(), dest)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return dest, info
}

// bkCheck is CheckFile on the snapshot, fatal on error.
func bkCheck(t *testing.T, path string) FileReport {
	t.Helper()
	rep, err := CheckFile(context.Background(), path)
	if err != nil {
		t.Fatalf("check %s: %v", path, err)
	}
	return rep
}

// bkVecQuery is a vec0 MATCH query vector: a JSON array, the same shape the
// stored vectors use. The value is chosen so the five nearest neighbours are
// unambiguous (no equal distances), which makes the ordering comparable.
func bkVecQuery() string { return "[7.25,0,0,0,0,0,0,0]" }

// Test 1 — a consistent snapshot under concurrent writes: the paired inserts
// and the running sum are invariants that only one instant satisfies.
func TestSnapshotIsConsistentUnderConcurrentWrites(t *testing.T) {
	s := bkOpenStore(t)
	for _, ddl := range []string{
		`CREATE TABLE zz_bk_pair (i INTEGER NOT NULL, tag TEXT NOT NULL CHECK (tag IN ('A','B')), PRIMARY KEY (i, tag))`,
		`CREATE TABLE zz_bk_sum (key TEXT PRIMARY KEY, total INTEGER NOT NULL)`,
	} {
		if _, err := s.w.Exec(ddl); err != nil {
			t.Fatalf("create fixture table: %v", err)
		}
	}
	if _, err := s.w.Exec(`INSERT INTO zz_bk_sum (key, total) VALUES ('n', 0)`); err != nil {
		t.Fatalf("seed the sum: %v", err)
	}

	// The writer: each transaction commits the pair AND the sum bump, so any
	// consistent instant satisfies count(A) = count(B) = sum. It reports each
	// commit, so the test can start the snapshot MID-FLIGHT with known data
	// behind it instead of racing the first commit for existence.
	stop := make(chan struct{})
	committed := make(chan int, 1000)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tx, err := s.w.Begin()
			if err != nil {
				return
			}
			if _, err := tx.Exec(`INSERT INTO zz_bk_pair (i, tag) VALUES (?, 'A'), (?, 'B')`, i, i); err == nil {
				if _, err := tx.Exec(`UPDATE zz_bk_sum SET total = total + 1 WHERE key = 'n'`); err == nil {
					if err := tx.Commit(); err == nil {
						select {
						case committed <- i:
						default:
						}
						continue
					}
				}
			}
			_ = tx.Rollback()
			return
		}
	}()

	// Ten committed pairs behind the snapshot's start: the read instant of a
	// new connection sees every earlier commit, so the snapshot must hold at
	// least these ten.
	for i := 0; i < 10; i++ {
		if _, ok := <-committed; !ok {
			t.Fatal("the writer died before the first ten commits")
		}
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	close(stop)
	wg.Wait()

	// The invariants, read from the SNAPSHOT, not the source.
	rep := bkCheck(t, dest)
	if !rep.IntegrityOK {
		t.Fatalf("the snapshot failed integrity: %v", rep.IntegrityDetail)
	}
	ro, err := bkOpenReadOnly(t, dest)
	if err != nil {
		t.Fatalf("open the snapshot: %v", err)
	}
	defer func() { _ = ro.Close() }()

	var a, b, sum, orphanA int
	if err := ro.QueryRow(`SELECT count(*) FROM zz_bk_pair WHERE tag = 'A'`).Scan(&a); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if err := ro.QueryRow(`SELECT count(*) FROM zz_bk_pair WHERE tag = 'B'`).Scan(&b); err != nil {
		t.Fatalf("count B: %v", err)
	}
	if err := ro.QueryRow(`SELECT total FROM zz_bk_sum WHERE key = 'n'`).Scan(&sum); err != nil {
		t.Fatalf("read the sum: %v", err)
	}
	if err := ro.QueryRow(`SELECT count(*) FROM zz_bk_pair WHERE tag = 'A' AND i NOT IN (SELECT i FROM zz_bk_pair WHERE tag = 'B')`).Scan(&orphanA); err != nil {
		t.Fatalf("count orphan A: %v", err)
	}
	if b < 10 {
		t.Fatalf("the snapshot holds %d B rows, but ten pairs were committed before it started — the commits are missing", b)
	}
	if a != b {
		t.Fatalf("the snapshot mixes two instants: %d A rows against %d B rows", a, b)
	}
	if sum != b {
		t.Fatalf("the snapshot mixes two instants: sum %d against %d committed pairs", sum, b)
	}
	if orphanA != 0 {
		t.Fatalf("the snapshot has %d A rows without their B — the pair transaction split", orphanA)
	}
}

// Test 2 — the virtual-table data arrives verbatim: vec0's shadow tables and
// the FTS5 index are copied, not rebuilt, and answer the same queries.
func TestSnapshotCarriesVectorAndFTSData(t *testing.T) {
	s := bkOpenStore(t)

	// The real on-demand vec0 path (retrieval.go's ensure), 8 dimensions.
	// The corpus name is letters-only by the vec table's identifier gate.
	vecTable, err := s.EnsureRetrievalVecTable("/bk-test", "bkcorpus", 8)
	if err != nil {
		t.Fatalf("ensure the vec table: %v", err)
	}
	for i := 0; i < 8; i++ {
		vec := fmt.Sprintf("[%f,0,0,0,0,0,0,0]", float32(i))
		if _, err := s.w.Exec(fmt.Sprintf(`INSERT INTO %s (embedding, chunk_id) VALUES (?, ?)`, vecTable), vec, i); err != nil {
			t.Fatalf("insert vector %d: %v", i, err)
		}
	}

	// The real FTS path: a corpus row, a chunk row, and the trigger that
	// feeds the index.
	res, err := s.w.Exec(`INSERT INTO retrieval_corpus (component_root, name) VALUES ('/bk-test', 'bk-corpus')`)
	if err != nil {
		t.Fatalf("insert the corpus: %v", err)
	}
	corpusID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("corpus id: %v", err)
	}
	if _, err := s.w.Exec(
		`INSERT INTO retrieval_chunk (corpus_id, source, line_start, line_end, content_hash, body) VALUES (?, 'bk-source', 1, 2, 'bk-hash', 'zebra quantum flight')`,
		corpusID); err != nil {
		t.Fatalf("insert the chunk: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// The source's answers, taken while it is quiesced. The vectors are the
	// eight ids 0..7 on the first axis; the query 7.25 makes the five
	// nearest unambiguous: 7 (0.25), 6 (1.25), 5 (2.25), 4 (3.25), 3 (4.25).
	srcQuery := `SELECT chunk_id FROM ` + vecTable + ` WHERE embedding MATCH ? ORDER BY distance LIMIT 5`
	queryHex := bkVecQuery()
	srcIDs := bkReadIDs(t, s.r, srcQuery, queryHex)
	if fmt.Sprint(srcIDs) != fmt.Sprint([]int{7, 6, 5, 4, 3}) {
		t.Fatalf("the source's nearest neighbours are %v, want [7 6 5 4 3] — the query's distances are unambiguous", srcIDs)
	}
	var srcFts int
	if err := s.r.QueryRow(`SELECT count(*) FROM retrieval_chunk_fts WHERE retrieval_chunk_fts MATCH 'zebra'`).Scan(&srcFts); err != nil {
		t.Fatalf("source fts query: %v", err)
	}

	// The snapshot's answers, read-only.
	rep := bkCheck(t, dest)
	if !rep.IntegrityOK {
		t.Fatalf("the snapshot failed integrity: %v", rep.IntegrityDetail)
	}
	if rep.VecVersion == "" {
		t.Fatal("the snapshot's vec_version() is absent — the extension did not carry over")
	}
	if _, srcVec, err := s.VecVersion(); err == nil && srcVec != rep.VecVersion {
		t.Fatalf("the snapshot reports vec %s, the source %s", rep.VecVersion, srcVec)
	}
	if !rep.FTSUsable {
		t.Fatal("the FTS5 probe failed on the snapshot — the index did not carry over")
	}

	ro, err := bkOpenReadOnly(t, dest)
	if err != nil {
		t.Fatalf("open the snapshot read-only: %v", err)
	}
	defer func() { _ = ro.Close() }()

	snapIDs := bkReadIDs(t, ro, srcQuery, queryHex)
	if fmt.Sprint(srcIDs) != fmt.Sprint(snapIDs) {
		t.Fatalf("the nearest neighbours differ: source %v, snapshot %v", srcIDs, snapIDs)
	}
	var snapFts int
	if err := ro.QueryRow(`SELECT count(*) FROM retrieval_chunk_fts WHERE retrieval_chunk_fts MATCH 'zebra'`).Scan(&snapFts); err != nil {
		t.Fatalf("snapshot fts query: %v", err)
	}
	if snapFts != srcFts {
		t.Fatalf("the FTS MATCH differs: source %d rows, snapshot %d", srcFts, snapFts)
	}

	// The quiesced row counts: the snapshot and the source agree per table.
	srcRep, err := CheckFile(context.Background(), s.Path())
	if err != nil {
		t.Fatalf("check the source: %v", err)
	}
	for table, n := range rep.Tables {
		if srcRep.Tables[table] != n {
			t.Fatalf("table %s: source %d rows, snapshot %d", table, srcRep.Tables[table], n)
		}
	}
}

// Test 3 — one file, no sidecars, the journal mode says delete.
func TestSnapshotIsOneFile(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list the destination: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "snap.db" {
		t.Fatalf("the destination holds %v; a backup is one file, and a half-copied -wal is an undetectable corruption", names)
	}
	rep := bkCheck(t, dest)
	if rep.JournalMode != "delete" {
		t.Fatalf("the artefact's journal mode is %q, want delete — the one-file proof", rep.JournalMode)
	}
}

// Test 4 — a cancelled copy leaves nothing, releases its read instant, and
// says context.Canceled.
func TestSnapshotCancelLeavesNothing(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := s.SnapshotTo(ctx, dest)
		errc <- err
	}()

	// Wait for the copy to be MID-FLIGHT: a partial file on disk.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(dest); err == nil && st.Size() > 2*1024*1024 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	err := <-errc
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled snapshot returned %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the partial destination survived the cancel: %v — a partial file is a backup that lies", err)
	}
	// The read instant is gone: a TRUNCATE checkpoint is the strictest
	// check of that, because it fails while ANY read snapshot is held.
	var busy, logN, checkpointed int
	if err := s.w.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logN, &checkpointed); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if busy != 0 {
		t.Fatal("the cancelled snapshot still holds its read snapshot — the checkpoint is busy")
	}
}

// Test 5 — the writer pool is never touched. The proof is behavioural: the
// writer pool is CLOSED before the snapshot, so any use of it is a loud
// "database is closed" failure, and the snapshot must still succeed.
func TestSnapshotNeverUsesTheWriter(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)

	if err := s.w.Close(); err != nil {
		t.Fatalf("close the writer pool: %v", err)
	}
	// The store's own Close would double-close the writer and report it;
	// release the readers directly instead — the test is over after this.
	t.Cleanup(func() { _ = s.r.Close() })

	dest, info := bkSnapshot(t, s, t.TempDir())
	if info.PagesCopied == 0 {
		t.Fatal("the snapshot copied zero pages; the test proves nothing")
	}
	rep := bkCheck(t, dest)
	if !rep.IntegrityOK {
		t.Fatalf("the writer-free snapshot failed integrity: %v", rep.IntegrityDetail)
	}
}

// Test 6 — an existing destination is refused, and the existing file is
// untouched.
func TestSnapshotRefusesToOverwrite(t *testing.T) {
	s := bkOpenStore(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	sentinel := []byte("the predecessor backup, do not touch")
	if err := os.WriteFile(dest, sentinel, 0o600); err != nil {
		t.Fatalf("seed the destination: %v", err)
	}

	_, err := s.SnapshotTo(context.Background(), dest)
	if err == nil {
		t.Fatal("SnapshotTo overwrote an existing destination")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the refusal does not name the reason: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read the destination back: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatal("the predecessor backup was modified")
	}
}

// Test 7a — CheckFile is read-only: the file's bytes and the directory's
// listing are identical before and after, and nothing new appears beside it.
func TestCheckFileIsReadOnly(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	before := bkHashFile(t, dest)
	beforeListing := bkListing(dir)

	rep := bkCheck(t, dest)
	if !rep.IntegrityOK {
		t.Fatalf("the healthy artefact failed its own check: %v", rep.IntegrityDetail)
	}
	if len(rep.Applied) != len(Migrations()) {
		t.Fatalf("the ledger holds %d names, want all %d", len(rep.Applied), len(Migrations()))
	}

	after := bkHashFile(t, dest)
	afterListing := bkListing(dir)
	if before != after {
		t.Fatal("CheckFile changed the file's bytes")
	}
	if strings.Join(beforeListing, ",") != strings.Join(afterListing, ",") {
		t.Fatalf("CheckFile created or removed files beside the artefact: %v -> %v", beforeListing, afterListing)
	}
}

// Test 7b — a torn page is found: IntegrityOK false, with the check's own
// words in the detail.
func TestCheckFileDetectsCorruption(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	blob, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read the artefact: %v", err)
	}
	// A payload byte flip is invisible to integrity_check (there is no
	// content checksum), so the corruption is STRUCTURAL: zero the b-tree
	// page-type byte of page 2. 0 is never a legal page type, so whatever
	// table page 2 holds, btreeInitPage fails — probed before writing this
	// (a flip in the BLOB payload passes; this one does not).
	pageSize := int(binary.BigEndian.Uint16(blob[16:18]))
	if pageSize == 65536 {
		pageSize = 1
	}
	blob[pageSize] = 0
	corrupt := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corrupt, blob, 0o600); err != nil {
		t.Fatalf("write the corruption: %v", err)
	}

	rep := bkCheck(t, corrupt)
	if rep.IntegrityOK {
		t.Fatal("the corrupted file passed integrity_check")
	}
	if len(rep.IntegrityDetail) == 0 || (len(rep.IntegrityDetail) == 1 && rep.IntegrityDetail[0] == "ok") {
		t.Fatal("the report carries no detail about the corruption")
	}
}

// Test 7c — a dangling foreign key is counted.
func TestCheckFileDetectsFKViolations(t *testing.T) {
	s := bkOpenStore(t)
	bkAddBulk(t, s)
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap.db")
	if _, err := s.SnapshotTo(context.Background(), dest); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// A copy, mutated writable: one chunk pointing at a corpus that does not
	// exist. The retrieval_chunk → retrieval_corpus FK is the pair.
	corrupt := filepath.Join(t.TempDir(), "fk.db")
	if err := bkCopyFile(dest, corrupt); err != nil {
		t.Fatalf("copy the artefact: %v", err)
	}
	database.RegisterExtensions()
	// Foreign keys OFF for the mutation: enforcement would refuse the
	// dangling insert at write time, and the violation we want counted is
	// one that exists in the file anyway — the state a restore must catch.
	mut, err := sql.Open(database.DriverName, "file:"+corrupt+"?_foreign_keys=0")
	if err != nil {
		t.Fatalf("open the copy writable: %v", err)
	}
	if _, err := mut.Exec(`INSERT INTO retrieval_chunk (corpus_id, source, line_start, line_end, content_hash, body) VALUES (999999, 'dangling', 1, 2, 'h', 'body')`); err != nil {
		mut.Close()
		t.Fatalf("insert the dangling row: %v", err)
	}
	if err := mut.Close(); err != nil {
		t.Fatalf("close the copy: %v", err)
	}

	rep := bkCheck(t, corrupt)
	if rep.ForeignKeyViolations == 0 {
		t.Fatal("the dangling foreign key was not counted")
	}
}

// Test 8 — the empty-store predicate (F4) on the five shapes T0 listed, and
// it creates nothing: no ledger appears, and no file but the WAL sidecars'
// normal churn is added to the directory.
func TestEmptyStorePredicateCreatesNothing(t *testing.T) {
	t.Run("brand new, opened without migrations", func(t *testing.T) {
		dir := t.TempDir()
		s, err := Open(Options{Dir: dir, SkipMigrations: true})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		before := bkStateNames(dir)
		applied, pending, empty, err := s.AppliedNamesAndPending()
		if err != nil {
			t.Fatalf("predicate: %v", err)
		}
		after := bkStateNames(dir)
		s.Close()

		if !empty || applied != nil || len(pending) != len(Migrations()) {
			t.Fatalf("a brand-new store is not empty: applied=%v pending=%d empty=%v", applied, len(pending), empty)
		}
		bkAssertNoLedgerCreated(t, dir, before, after)
	})

	t.Run("zero byte file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "state.db"), nil, 0o600); err != nil {
			t.Fatalf("write the zero-byte file: %v", err)
		}
		s, err := Open(Options{Dir: dir, SkipMigrations: true})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		before := bkStateNames(dir)
		applied, pending, empty, err := s.AppliedNamesAndPending()
		if err != nil {
			t.Fatalf("predicate: %v", err)
		}
		after := bkStateNames(dir)
		s.Close()

		if !empty || applied != nil || len(pending) != len(Migrations()) {
			t.Fatalf("a zero-byte file is not empty: applied=%v pending=%d empty=%v", applied, len(pending), empty)
		}
		bkAssertNoLedgerCreated(t, dir, before, after)
	})

	t.Run("freshly migrated is not empty", func(t *testing.T) {
		s := bkOpenStore(t)
		applied, pending, empty, err := s.AppliedNamesAndPending()
		if err != nil {
			t.Fatalf("predicate: %v", err)
		}
		if empty || len(applied) != len(Migrations()) || len(pending) != 0 {
			t.Fatalf("a migrated store is empty: applied=%d pending=%d empty=%v", len(applied), len(pending), empty)
		}
	})

	t.Run("a failed migration is empty, because it rolled back", func(t *testing.T) {
		s, err := Open(Options{Dir: t.TempDir(), SkipMigrations: true})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()
		// The ledger in its framework shape, holding exactly one FAILED row:
		// the migration's transaction rolled back, so it left no work.
		if _, err := s.w.Exec(`CREATE TABLE _migrations (
			id TEXT PRIMARY KEY, name VARCHAR(255) NOT NULL UNIQUE, description TEXT,
			applied_at DATETIME, status VARCHAR(50) NOT NULL DEFAULT 'applied', error TEXT)`); err != nil {
			t.Fatalf("create the ledger: %v", err)
		}
		if _, err := s.w.Exec(`INSERT INTO _migrations (id, name, applied_at, status) VALUES ('bk', '0099_bk_failed', ?, 'failed')`, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("insert the failed row: %v", err)
		}
		applied, pending, empty, err := s.AppliedNamesAndPending()
		if err != nil {
			t.Fatalf("predicate: %v", err)
		}
		if !empty || applied != nil || len(pending) != len(Migrations()) {
			t.Fatalf("a failed-only ledger is not empty: applied=%v pending=%d empty=%v", applied, len(pending), empty)
		}
	})

	t.Run("in memory, it is empty and has no file at all", func(t *testing.T) {
		env := func(k string) string {
			if k == "DATABASE_SQLITE_DSN" {
				return "file::memory:?_foreign_keys=1"
			}
			return os.Getenv(k)
		}
		s, err := Open(Options{Env: env, SkipMigrations: true})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()
		applied, pending, empty, err := s.AppliedNamesAndPending()
		if err != nil {
			t.Fatalf("predicate: %v", err)
		}
		if !empty || applied != nil || len(pending) != len(Migrations()) {
			t.Fatalf("an in-memory store is not empty: applied=%v pending=%d empty=%v", applied, len(pending), empty)
		}
	})
}

// Test 9 — KnownMigrationNames is exactly Migrations()' names.
func TestKnownMigrationNames(t *testing.T) {
	got := KnownMigrationNames()
	all := Migrations()
	if len(got) != len(all) {
		t.Fatalf("KnownMigrationNames holds %d names, the migration set has %d", len(got), len(all))
	}
	for _, m := range all {
		if !got[m.Name] {
			t.Fatalf("the known names are missing %s", m.Name)
		}
	}
}

// Test 10 — I2's driver discipline: mattn/go-sqlite3 is imported by
// internal/database and internal/store, and by nothing else.
func TestDriverImportedOnlyByDatabaseAndStore(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	var (
		walked    int
		importers []string
	)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".tmp" || name == "node_modules" || name == "ui" || strings.HasSuffix(path, ".tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		walked++
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// An import line, not a comment: the line must BE the import
		// (optionally blank-imported), which keeps a mention in a doc
		// comment from counting.
		for _, line := range strings.Split(string(blob), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == `"github.com/mattn/go-sqlite3"` || trimmed == `_ "github.com/mattn/go-sqlite3"` {
				rel, _ := filepath.Rel(root, path)
				if !strings.HasPrefix(rel, filepath.Join("internal", "database")) &&
					!strings.HasPrefix(rel, filepath.Join("internal", "store")) {
					importers = append(importers, rel)
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if walked < 30 {
		t.Fatalf("the walk covered %d go files; that is not the module (the test would pass by missing everything)", walked)
	}
	if len(importers) > 0 {
		t.Fatalf("mattn/go-sqlite3 is imported outside internal/database and internal/store: %v — a second driver user breaks I2", importers)
	}
}

// --- helpers ---------------------------------------------------------------

// bkOpenReadOnly opens a database file with the same read-only connection
// CheckFile uses: mode=ro, foreign keys on, one connection.
func bkOpenReadOnly(t *testing.T, path string) (*sql.DB, error) {
	t.Helper()
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, "file:"+path+"?mode=ro&_foreign_keys=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// bkReadIDs runs a one-column int query and collects the column.
func bkReadIDs(t *testing.T, db *sql.DB, query string, arg any) []int {
	t.Helper()
	rows, err := db.Query(query, arg)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan the ids: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return out
}

// bkHashFile is the sha256 of a file's bytes, hex.
func bkHashFile(t *testing.T, path string) string {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

// bkListing is a directory's entry names.
func bkListing(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// bkStateNames is the state dir's entries, for the predicate's "creates
// nothing" assertion.
func bkStateNames(dir string) []string { return bkListing(dir) }

// bkAssertNoLedgerCreated says the predicate added no file that is not the
// database itself or its WAL sidecars — the churn T0 recorded as normal.
func bkAssertNoLedgerCreated(t *testing.T, dir string, before, after []string) {
	t.Helper()
	allowed := map[string]bool{"state.db": true, "state.db-wal": true, "state.db-shm": true}
	for _, name := range after {
		if !allowed[name] {
			t.Fatalf("the predicate created %s in the state dir", name)
		}
	}
	_ = before
}

func bkCopyFile(src, dest string) error {
	blob, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, blob, 0o600)
}
