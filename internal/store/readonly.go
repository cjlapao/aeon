package store

// readonly.go — S62 T9 (BK15): the store's read-only facts. I2 says no
// SQL lives outside this package, so the questions a read-only consumer
// may ask of a store's FILE ("what journal mode does it run in, how many
// migrations does its ledger say applied") are answered here, over a
// connection that cannot write, never migrates, and holds no lock a
// writer waits for — and the consumer (the doctor) gets the facts, not
// the SQL.

import (
	"database/sql"

	"github.com/cjlapao/aeon/internal/database"
)

// FileFacts is the answer to "what does that store file say, without
// touching it": one mode=ro connection, no migrations, no lock.
//
// A file that is present but unreadable (a torn write, a permissions
// state) is the zero value with Readable false — a state the caller
// says in its own words (P31), not an error and not a guess. The
// caller stats the file first; the absence itself is not this
// function's.
type FileFacts struct {
	// Readable is whether the connection opened and answered.
	Readable bool
	// JournalMode is the file's journal mode, or "unreadable" when the
	// file is present but the mode could not be read.
	JournalMode string
	// Applied is the ledger's applied count; zero when the file has no
	// _migrations table (not an aeon store, or mid-creation) — a count
	// that says so, not an error.
	Applied int
}

// ReadOnlyFacts reads the schema's read questions from a store file
// without a store: the extensions the file may reference are
// registered (a page written by a sqlite-vec build names the
// extension; a reader without it would die mid-scan), one read-only
// connection on the driver's own name, foreign keys on.
func ReadOnlyFacts(path string) (FileFacts, error) {
	database.RegisterExtensions()
	conn, err := sql.Open(database.DriverName, "file:"+path+"?mode=ro&_foreign_keys=1")
	if err != nil {
		return FileFacts{}, err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		return FileFacts{Readable: false}, nil
	}
	out := FileFacts{Readable: true}

	var mode string
	if err := conn.QueryRow("PRAGMA journal_mode").Scan(&mode); err == nil {
		out.JournalMode = mode
	} else {
		out.JournalMode = "unreadable"
	}

	var tables int
	if err := conn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='_migrations'").Scan(&tables); err == nil && tables > 0 {
		_ = conn.QueryRow("SELECT count(*) FROM _migrations WHERE status='applied'").Scan(&out.Applied)
	}
	if out.Applied < 0 {
		out.Applied = 0
	}
	return out, nil
}
