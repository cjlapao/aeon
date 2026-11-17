package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/database"
)

// Every fixture is a file under t.TempDir(), created by a real Open (so it
// carries the store's WAL shape) and closed before the lock is taken: the
// lock under test is the one on the FILE, not on a live pool.

// TestLockExclusiveOnAMissingFileIsAllowed pins the fresh-machine door: a
// restore of a box that never had a store locks "nothing" and proceeds.
func TestLockExclusiveOnAMissingFileIsAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the fixture file exists before the test: %v", err)
	}
	release, err := LockExclusive(path)
	if err != nil {
		t.Fatalf("LockExclusive on a missing file: %v", err)
	}
	if release == nil {
		t.Fatal("the missing-file release is nil; it must be a callable no-op")
	}
	release()
	release() // idempotent, even on the no-op
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the lock CREATED the file it was asked about — a lock must not mint a database")
	}
}

// TestLockExclusiveRefusesWhileHeld: the same file, locked twice, refuses
// the second with the busy class. In-process or not, the lock is on the
// file, and a file held by one connection is held against every other.
func TestLockExclusiveRefusesWhileHeld(t *testing.T) {
	path := lockFile(t)

	release, err := LockExclusive(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	again, err := LockExclusive(path)
	if err == nil {
		if again != nil {
			again()
		}
		t.Fatal("the second LockExclusive succeeded while the first held the file")
	}
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("second lock error = %v, want ErrLockBusy", err)
	}
	if !strings.Contains(err.Error(), "held by another process") {
		t.Fatalf("the refusal should name the held-file fact: %v", err)
	}

	release()
	if _, err := LockExclusive(path); err != nil {
		t.Fatalf("after release the lock must be free: %v", err)
	}
}

// TestLockExclusiveReleasesIdempotently: release twice, no panic, and the
// lock is actually gone (a release that only looked idempotent while still
// holding would defeat the restore's put-back, which re-locks nothing but
// does re-read the file).
func TestLockExclusiveReleasesIdempotently(t *testing.T) {
	path := lockFile(t)
	release, err := LockExclusive(path)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	release()
	release()
	release()
	if _, err := LockExclusive(path); err != nil {
		t.Fatalf("the idempotent releases left the lock held: %v", err)
	}
}

// TestLockExclusiveBlocksOnARollbackModeReadTransaction is the shape the
// spec's holder test rides: a read transaction in rollback journal mode
// holds a SHARED lock, and a SHARED lock blocks BEGIN EXCLUSIVE — a real
// SQLITE_BUSY from a reader. (A WAL-mode reader holds no file lock at all;
// that case is the lock's documented non-claim, covered by the move-set
// togetherness instead.)
func TestLockExclusiveBlocksOnARollbackModeReadTransaction(t *testing.T) {
	path := lockFile(t)

	holder, err := sql.Open(database.DriverName, "file:"+path+"?_busy_timeout=0")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	if _, err := holder.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatalf("holder to rollback mode: %v", err)
	}
	if _, err := holder.Exec("BEGIN"); err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	// A REAL table, read to completion: measured, a catalog read of
	// sqlite_master takes no SHARED lock, and a data read that never runs
	// (Query left unconsumed) takes none either — the SHARED lock is held
	// from the stepped read until the transaction ends. retrieval_chunk
	// exists at every vintage past 0040, and this fixture is at current.
	var n int
	if err := holder.QueryRow("SELECT count(*) FROM retrieval_chunk").Scan(&n); err != nil {
		t.Fatalf("holder read: %v", err)
	}
	// The read transaction is open; its SHARED lock is the whole fixture.

	_, err = LockExclusive(path)
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("LockExclusive with a read transaction held: %v, want ErrLockBusy", err)
	}

	if _, err := holder.Exec("ROLLBACK"); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}
	if _, err := LockExclusive(path); err != nil {
		t.Fatalf("after the read transaction ended the lock must be free: %v", err)
	}
}

// TestLockExclusiveRefusalNamesThePath: a refusal that does not name the
// file it refused on is one the operator cannot act on.
func TestLockExclusiveRefusalNamesThePath(t *testing.T) {
	path := lockFile(t)
	release, err := LockExclusive(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer release()
	_, err = LockExclusive(path)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("the refusal does not name the file: %v", err)
	}
}

// lockFile opens a real fixture store (WAL, migrated, closed) and returns
// its database path: s.path is the resolved FILE, not a directory.
func lockFile(t *testing.T) string {
	t.Helper()
	s, err := Open(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open fixture store: %v", err)
	}
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatalf("close fixture store: %v", err)
	}
	return path
}
