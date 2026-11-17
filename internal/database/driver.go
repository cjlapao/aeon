// Package database registers the SQLite extensions Aeon's store depends on.
//
// # What this package is, and is not
//
// It is NOT the store's connector. S4 was redirected to use the framework's
// database contracts — `SQLiteConfig{StoragePath, FileName, DSN}`, `PoolConfig`
// and the `Store` interface — so the framework owns opening, configuring and
// pooling.
//
// What the framework does not own is the one thing SQLite cannot be asked for
// later: a C extension must be registered BEFORE any connection is opened.
// That is this package.
//
//	RegisterExtensions()  ← must run before the database service initialises
//
// It is the same ordering constraint internal/appenv and internal/secrets have,
// for a third reason.
package database

import (
	"database/sql"
	"fmt"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	// The driver whose connections the extension registers on, imported for
	// its side effect: it registers the "sqlite3" driver name (DriverName
	// below), and sqlite3_auto_extension() applies to every connection opened
	// through it afterwards.
	_ "github.com/mattn/go-sqlite3"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var once sync.Once

// RegisterExtensions registers sqlite-vec on every subsequent connection of the
// sqlite3 driver, via sqlite3_auto_extension().
//
// # Why a function and not a blank import
//
// `sqlite_vec.Auto()` MUST BE CALLED. A blank import of the bindings compiles,
// links the C, and registers nothing — `vec_version()` then reports "no such
// function" at the first query, which reads as a missing extension rather than
// a missing call. Measured; it is the easiest mistake here.
//
// Idempotent, because the ordering constraint means several callers may
// reasonably want to guarantee it has happened.
func RegisterExtensions() {
	once.Do(sqlite_vec.Auto)
}

// VecVersion opens an in-memory database and asks the extension its version.
//
// It exists as a startup probe and as the thing `aeon db info --vectors`
// reports: because the extension is statically linked (B3), there is **no
// extension-missing state to handle** — so a failure here means the binary was
// built wrong, which is a different problem from a misconfigured machine and
// should read differently.
func VecVersion() (string, error) {
	RegisterExtensions()

	db, err := sql.Open(DriverName, ":memory:")
	if err != nil {
		return "", fmt.Errorf("open in-memory sqlite: %w", err)
	}
	defer db.Close()

	var version string
	if err := db.QueryRow("select vec_version()").Scan(&version); err != nil {
		return "", fmt.Errorf("vec_version: %w (the extension is statically linked, so this means the binary was built without cgo)", err)
	}
	return version, nil
}

// CheckFeatures rejects an incorrectly built SQLite driver before migrations
// can modify a store. FTS5 and sqlite-vec are required even in short tests.
func CheckFeatures(db *sql.DB) error {
	var fts5 int
	var vec string
	if err := db.QueryRow("select sqlite_compileoption_used('ENABLE_FTS5'), vec_version()").Scan(&fts5, &vec); err != nil {
		return fmt.Errorf("SQLite requires CGO and sqlite-vec: %w; use CGO_ENABLED=1 and make test-go", err)
	}
	if fts5 != 1 {
		return fmt.Errorf("SQLite lacks FTS5: build with -tags sqlite_fts5; use make test-go or go test -tags sqlite_fts5 ./...")
	}
	return nil
}

// DSNPragmas is the pragma fragment Aeon's store needs, in the syntax
// gorm.io/driver/sqlite's driver actually honours.
//
// # Measured, because the wrong syntax fails SILENTLY
//
//	?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000   journal=wal, fk=1
//	?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)      journal=delete, fk=0
//
// The second form is a different driver's (`glebarez/sqlite`). It does not
// error — it is ignored, so a store that looks configured runs without WAL and
// without foreign keys.
//
// S4 composes this into `SQLiteConfig.DSN`, which the framework documents as a
// raw DSN opened as-is. **Assert the pragmas by querying them, never by
// inspecting the DSN.**
const DSNPragmas = "_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000"

// DriverName is the database/sql driver every Aeon connection opens.
//
// # Why it is named here rather than written at each call site
//
// There are TWO SQLite drivers in this binary, and the difference is invisible
// at the call site:
//
//	"sqlite3"  mattn/go-sqlite3      CGO. Hosts sqlite-vec. Aeon's.
//	"sqlite"   modernc.org/sqlite    pure Go. CANNOT host a C extension —
//	                                 `select vec_version()` answers
//	                                 "no such function". Linked in because
//	                                 the framework's database service imports
//	                                 it; Aeon opens nothing through it.
//
// A one-character typo between those two names is a store that works, passes
// every test that does not ask for a vector, and fails at S22. So the name is
// a constant with the reason attached.
const DriverName = "sqlite3"

// GormDialector wraps an already-open *sql.DB for GORM.
//
// This is the seam that makes "direct SQLite access" and "use the framework's
// contracts" the same thing rather than a choice: the framework's database
// service accepts a *gorm.DB through database.WithDB, GORM accepts a *sql.DB
// through Dialector{Conn:…}, and (*gorm.DB).DB() hands the same handle back.
// Measured: vec_version() answers v0.1.6 through it, AutoMigrate works, and a
// vec0 virtual table creates.
//
// No second connection is opened, and the pool settings stay Aeon's.
func GormDialector(db *sql.DB) gorm.Dialector {
	return sqlite.Dialector{Conn: db}
}

// GormConfig is the GORM configuration Aeon's handle carries.
//
// Silent logging, because this handle exists for DDL and the migration ledger
// — GORM's default logger would print every migration statement to stdout,
// where `aeon serve` prints the endpoint and the store path and a person is
// reading those two lines.
func GormConfig() *gorm.Config {
	return &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
}
