// The exclusive lock door (S62 T7, BK9 step 2).
//
// LockExclusive takes a write lock on a database FILE by path — not through
// the store — because the restore path must refuse while the file is held
// without ever opening it as a store: no migrations, no extension
// registration, no pool, nothing that could change the file before the
// operator's restore decides anything.
//
// What the lock detects, measured on this box (go-sqlite3 v1.14.52) with a
// fixture file and a second connection at busy_timeout=0:
//
//	WAL mode:    another connection with an OPEN transaction — even a bare
//	            deferred BEGIN, no statement — blocks BEGIN EXCLUSIVE.
//	DELETE mode: an open read transaction that has READ a real table blocks;
//	            a bare deferred BEGIN holds no lock and does not.
//
// (A catalog read of sqlite_master takes no SHARED lock either — the holder
// must read a data table for the DELETE-mode case to bite.)
//
// So the lock refuses whenever another session has a transaction open on
// the file, which is exactly "someone is working with this database". What
// it does not refuse: an idle connection with no transaction — nothing to
// protect, and that connection's open handles keep pointing at the inodes
// the restore moves, so its next statement follows the file. The restore
// additionally moves db+wal+shm together, which is the second half of that
// guarantee.
//
// The refusal is immediate (busy_timeout=0): a restore that queues behind a
// busy store would make the operator wait to learn the answer they need to
// act on.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/mattn/go-sqlite3"
)

// ErrLockBusy is the busy class: SQLITE_BUSY and SQLITE_LOCKED both fold
// into it. LOCKED is included because a timed-out or contended BEGIN has
// been measured to report LOCKED where BUSY is expected — the two names for
// "another connection holds the lock" are one answer to the operator.
var ErrLockBusy = errors.New("the database file is held by another process")

// LockExclusive takes an exclusive lock on the database file at path and
// returns its release. The lock is held until release() is called — once,
// idempotently — and never longer than the caller's critical section.
//
// A missing file is not an error: it returns a no-op release and nil, so a
// caller can lock before it knows whether the store exists (the restore's
// plan does). A file that exists and is held returns ErrLockBusy.
func LockExclusive(path string) (func(), error) {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, nil // nothing to hold: the caller Stats for its plan
	}
	if err != nil {
		return nil, fmt.Errorf("store: stat %s: %w", path, err)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("store: %s is not a regular file; the exclusive lock locks a database file, not a directory or a socket", path)
	}

	q := url.Values{}
	q.Set("_busy_timeout", "0")
	dsn := "file:" + path + "?" + q.Encode()
	db, err := sql.Open(database.DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s for the lock: %w", path, err)
	}
	// The busy class can surface at ANY step — connect, the locking-mode
	// pragma, or the BEGIN itself (measured: a held file fails as far up as
	// Ping, "database is locked") — so every step classifies through the
	// same door.
	fail := func(step string, err error) (func(), error) {
		_ = db.Close()
		var se sqlite3.Error
		if errors.As(err, &se) && (se.Code == sqlite3.ErrBusy || se.Code == sqlite3.ErrLocked) {
			return nil, fmt.Errorf("%w (%s): %s", ErrLockBusy, path, se.Error())
		}
		return nil, fmt.Errorf("store: %s: %w", step, err)
	}

	// One connection, held for the life of the lock: the lock lives on a
	// specific connection, and a pool that swaps it out would drop it.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return fail("connect "+path, err)
	}
	// Set EXPLICITLY, not via the DSN: the pragma is per-connection state
	// this door owns, and a DSN passthrough it never asked for would be a
	// setting nobody reviewed.
	if _, err := db.Exec("PRAGMA locking_mode=EXCLUSIVE"); err != nil {
		return fail(path+" will not take exclusive locking mode", err)
	}
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		return fail(path+" will not take the exclusive lock", err)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_, _ = db.Exec("ROLLBACK") // a failed BEGIN left no transaction; the close is the other half
			_ = db.Close()
		})
	}, nil
}
