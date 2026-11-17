// Package store is Aeon's memory: the schema, the migrations, and every SQL
// statement in the program.
//
// # What this package owns, and what the framework owns
//
// S4 was redirected on 2026-10-11, by the user: use the framework's database
// contracts and expand only what is missing. The split that came out of
// measuring rather than assuming —
//
//	THE FRAMEWORK OWNS            config (SQLiteConfig, PoolConfig), the Store
//	                              lifecycle contract, the schema lock, the
//	                              migration runner and its _migrations ledger
//	AEON OWNS                     the connection, the schema, the statements
//
// The connection is Aeon's for ONE measured reason. The framework's own SQLite
// arm opens `modernc.org/sqlite` — a pure-Go translation of SQLite, which
// cannot host a C extension:
//
//	sql.Open("sqlite", …)  →  select vec_version()  →  no such function
//	its composed DSN       →  journal_mode=delete, foreign_keys=0
//
// Aeon needs `mattn/go-sqlite3` (B3: CGO is on, sqlite-vec is a C extension,
// and S22 reads it), WAL, and foreign keys. So the handle is opened here and
// handed to the framework through `database.WithDB` — a PUBLISHED construction
// mode, not a way round one: injected mode skips connect and config and runs
// the lock, the runner and the store lifecycle on the handle it is given.
//
//	internal/bootstrap  →  database.New(database.WithDB(st.GormDB()))
//
// Everything else stays the framework's, and the path still comes from its
// config plane: internal/appenv feeds DATABASE_SQLITE_STORAGE_PATH and
// DATABASE_SQLITE_FILE_NAME from internal/paths, which is the one resolver.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/core/database/common"
	"gorm.io/gorm"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/paths"
)

// The numbers below are GUESSES (O7), stated as such because D11's QA case is
// expected to change them rather than pass.
const (
	// DefaultBusyTimeout is how long a statement waits for the database's own
	// lock before giving up. It only matters BETWEEN PROCESSES: within one
	// process the writer pool is at one connection, so a second writer waits at
	// the pool (measured: it blocks, and the wait is interruptible by context).
	// Five seconds is long enough to cover another process's transaction and
	// short enough that a wedged peer surfaces as a timeout rather than a hang.
	DefaultBusyTimeout = 5 * time.Second

	// DefaultReadPoolSize is how many concurrent readers the reader pool opens.
	// Four is a guess sized at "a window, a CLI verb and a loop, plus one".
	// WAL permits any number; the cost of each is a file handle and some memory.
	DefaultReadPoolSize = 4

	// writerPoolSize is NOT a guess and NOT configurable (DB15).
	//
	// SQLite in WAL permits one writer. With a larger pool, two goroutines
	// writing both get connections and the second meets SQLITE_BUSY, which
	// busy_timeout then papers over — turning a design property into a retry
	// loop whose latency nobody can forecast. At one, the second writer waits
	// at the POOL, predictably. That is a code-level decision outranking
	// configuration, the same shape as a service pin.
	writerPoolSize = 1
)

// storeFileName matches internal/appenv's default, and is used only when
// nothing in the framework's config says otherwise.
const storeFileName = "state.db"

// Options configures Open. Every field has a working zero value.
type Options struct {
	// Dir overrides where the database lives, above everything else. For tests
	// and for `--store`. The directory is created if missing.
	Dir string

	// File overrides the file name inside Dir.
	File string

	// Env is the environment the framework's database config is read from.
	// Injectable so the fallback chain is testable without mutating the
	// process environment (I7). Defaults to os.Getenv.
	Env func(string) string

	// Clock is the source of every timestamp written (I9). Injectable so a test
	// can assert an exact ISO 8601 string rather than a shape.
	Clock func() time.Time

	// ReadPoolSize and BusyTimeout override the guesses above. Zero means the
	// default; the framework's DATABASE_POOL_MAX_OPEN_CONNS sets ReadPoolSize
	// when this is zero, because PoolConfig is one pool and the reader is the
	// one a person would want to size.
	ReadPoolSize int
	BusyTimeout  time.Duration

	// SkipMigrations opens without running the ledger. For the migration tests
	// themselves, which need a database at a prior version; set by the
	// backup/export direct doors, which must capture a store as it is, and by
	// no CLI path. A skipped open also never calls PreMigrate (S62 T5): a
	// door that opens as-is is a door that migrates nothing, and the
	// pre-migration backup guards a migration.
	SkipMigrations bool

	// PreMigrate, when non-nil, is called with the store open and the
	// migration unapplied — between the GORM wrap and Migrate() — with the
	// exact pending set Migrate is about to run (S62 T5, BK6). The
	// pre-migration backup is its one production caller. nil is today's
	// behaviour exactly: a nil hook, a skipped open, an empty store and a
	// current store all cost the caller nothing and touch nothing.
	PreMigrate PreMigrateFunc
}

// Store is the memory. Two pools, by design.
type Store struct {
	// w is the ONLY writer: SetMaxOpenConns(1) (DB15). Every INSERT, UPDATE,
	// DELETE and DDL goes through it.
	w *sql.DB

	// r is the read pool. Reads MUST NOT use the writer (O6, I6): a read on the
	// single-connection writer serialises behind every write, and the symptom
	// is a UI that stalls whenever a loop is busy — diagnosed as a UI problem.
	r *sql.DB

	// gw is GORM over the writer's handle, which is what the framework's
	// migration runner and store lifecycle need. It adds no second connection:
	// gorm.io/driver/sqlite's Dialector{Conn:…} adopts the *sql.DB.
	gw *gorm.DB

	path  string
	clock func() time.Time

	// closed says Close has run. The pools deliberately STAY non-nil: a
	// statement against a closed *sql.DB answers `sql: database is closed`,
	// while a nil one segfaults inside database/sql before any caller can
	// say what happened. The case that started this is the daemon's own
	// background read — queue_reactor.go dispatches every terminal event to a
	// goroutine, so a loop that finishes as a test tears down reads a store
	// that has just closed. That read must return an error the reactor logs
	// ("terminal lookup failed"), not end the process.
	closed atomic.Bool

	readPoolSize int
	busyTimeout  time.Duration
}

// Open resolves the path, opens both pools, and migrates (DB8).
//
// It is the only function in Aeon that opens a database, and the only one that
// touches the filesystem (asserted by a test over the package's sources).
func Open(opts Options) (*Store, error) {
	cfg, err := resolveConfig(opts)
	if err != nil {
		return nil, err
	}

	// B3's ordering constraint: sqlite-vec registers through
	// sqlite3_auto_extension(), which applies to connections opened AFTER it.
	// Calling it here as well as in main() costs nothing (it is once-guarded)
	// and means a test or a CLI verb that never booted the daemon still gets
	// vectors.
	database.RegisterExtensions()

	if cfg.dir != "" {
		// 0700: the file is the whole database, so no other user needs to
		// traverse to it (P26 — access control is the filesystem). The same
		// mode the framework uses for its own data directory.
		if err := os.MkdirAll(cfg.dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: create state directory %s: %w", cfg.dir, err)
		}
	}

	w, err := sql.Open(database.DriverName, cfg.writerDSN)
	if err != nil {
		return nil, fmt.Errorf("store: open writer: %w", err)
	}
	w.SetMaxOpenConns(writerPoolSize)
	w.SetMaxIdleConns(writerPoolSize)

	r, err := sql.Open(database.DriverName, cfg.readerDSN)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("store: open reader: %w", err)
	}
	r.SetMaxOpenConns(cfg.readPoolSize)
	r.SetMaxIdleConns(cfg.readPoolSize)

	s := &Store{
		w:            w,
		r:            r,
		path:         cfg.path,
		clock:        opts.Clock,
		readPoolSize: cfg.readPoolSize,
		busyTimeout:  cfg.busyTimeout,
	}
	if s.clock == nil {
		s.clock = time.Now
	}

	// Ping the writer before anything else, so an unusable path fails HERE
	// rather than at the first statement. sql.Open is lazy: it validates
	// nothing, so a store that "opened" can still be a directory that does not
	// exist.
	if err := w.Ping(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store: %s is not usable: %w", cfg.path, err)
	}
	if err := database.CheckFeatures(w); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store: %w", err)
	}

	gw, err := gorm.Open(database.GormDialector(w), database.GormConfig())
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store: wrap the writer for the framework's contracts: %w", err)
	}
	s.gw = gw

	// The between-the-open-and-the-migration gate (S62 T5, BK6). A hook
	// error closes the store and fails the open with the hook's own
	// sentence — the migration is refused, and the store is untouched.
	premig, err := runPreMigrateGate(opts, cfg, s)
	if err != nil {
		_ = s.Close()
		return nil, err
	}

	if !opts.SkipMigrations {
		if err := s.Migrate(); err != nil {
			_ = s.Close()
			return nil, err
		}
	}

	// The hook's aftermath runs ONLY after a successful migration: the row
	// needs the table the pending migration may have just created, and a
	// failed migration prunes nothing — its backup is the undo (BK6).
	if err := applyPreMigrateAftermath(premig, s); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Close releases both pools. Safe to call twice.
//
// It says so on `closed` and leaves the handles in place — see the field's
// comment. Everything that asked "is this store open?" asks `Closed()` now.
func (s *Store) Close() error {
	var errs []error
	s.closed.Store(true)
	if s.r != nil {
		if err := s.r.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.w != nil {
		if err := s.w.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Closed reports that Close has run. A background read that arrives after the
// daemon shut its store down must be able to ASK rather than dereference a nil
// pool: `sql: database is closed` is an answer a caller can log and act on, a
// segfault is not (P3 — the failure is said, not hidden, and it is not fatal).
func (s *Store) Closed() bool { return s.closed.Load() }

// Path is the database file. Printed first by `aeon db info`, because every
// later slice's debugging starts by finding the file (B10).
func (s *Store) Path() string { return s.path }

// GormDB is the handle to hand the framework's database service through
// database.WithDB. It is the WRITER's: the framework runs DDL and the migration
// ledger on it, and both are writes.
func (s *Store) GormDB() *gorm.DB { return s.gw }

// ReadPoolSize and BusyTimeout report the configured guesses, for `db info`.
func (s *Store) ReadPoolSize() int          { return s.readPoolSize }
func (s *Store) BusyTimeout() time.Duration { return s.busyTimeout }

// Now is the store's clock, in ISO 8601 UTC (I9).
//
// Matching v1's format exactly so the two databases are comparable by eye
// during the cut-over — which is a real operation someone performs once, under
// pressure, by reading rows side by side.
func (s *Store) Now() string { return isoUTC(s.clock()) }

func isoUTC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// VecVersion asks the extension its version, on the pool named.
//
// There is no loading step and no extension-missing state (DB18): the bindings
// register it through sqlite3_auto_extension() at init, so a failure here means
// the binary was built without cgo — a different problem from a misconfigured
// machine, and it should read differently.
func (s *Store) VecVersion() (writer, reader string, err error) {
	if err := s.w.QueryRow("select vec_version()").Scan(&writer); err != nil {
		return "", "", fmt.Errorf("store: vec_version on the writer pool: %w", err)
	}
	if err := s.r.QueryRow("select vec_version()").Scan(&reader); err != nil {
		return writer, "", fmt.Errorf("store: vec_version on the reader pool: %w", err)
	}
	return writer, reader, nil
}

// --- the path and the DSN ----------------------------------------------------

type resolved struct {
	path         string
	dir          string
	writerDSN    string
	readerDSN    string
	readPoolSize int
	busyTimeout  time.Duration
}

// resolveConfig reads the FRAMEWORK's SQLite configuration and composes the two
// DSNs.
//
// There is no second config struct and no second path resolver (the redirect's
// point 4). The chain, highest first:
//
//	opts.Dir / opts.File            tests, and `--store`
//	DATABASE_SQLITE_DSN             the framework's documented raw-DSN hatch
//	DATABASE_SQLITE_STORAGE_PATH    what internal/appenv sets, from internal/paths
//	  + DATABASE_SQLITE_FILE_NAME
//	paths.StoreDirFor(env)          AEON_STATE_DIR → XDG_STATE_HOME → ~/.local/state
//
// The last row is reached by a CLI verb that never called appenv.Apply(), which
// is most of them. Without it, `aeon db info` would resolve the framework's own
// default of "." — the process's working directory — and report a different
// store depending on which shell it was run from.
func resolveConfig(opts Options) (resolved, error) {
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}

	out := resolved{
		readPoolSize: opts.ReadPoolSize,
		busyTimeout:  opts.BusyTimeout,
	}
	if out.busyTimeout <= 0 {
		out.busyTimeout = DefaultBusyTimeout
	}
	if out.readPoolSize <= 0 {
		out.readPoolSize = readPoolFromConfig(env)
	}

	sqliteCfg := common.SQLiteConfig{
		StoragePath: env("DATABASE_SQLITE_STORAGE_PATH"),
		FileName:    env("DATABASE_SQLITE_FILE_NAME"),
		DSN:         env("DATABASE_SQLITE_DSN"),
	}

	switch {
	case opts.Dir != "":
		file := opts.File
		if file == "" {
			file = storeFileName
		}
		out.dir = opts.Dir
		out.path = filepath.Join(opts.Dir, file)

	case strings.TrimSpace(sqliteCfg.DSN) != "":
		// The framework opens a raw DSN AS-IS and creates no directory. Aeon
		// honours that: the path business is the operator's, so the only thing
		// added is the pragmas, and only when they are absent.
		return withRawDSN(sqliteCfg.DSN, out)

	case strings.TrimSpace(sqliteCfg.StoragePath) != "":
		file := strings.TrimSpace(sqliteCfg.FileName)
		if file == "" {
			file = storeFileName
		}
		out.dir = sqliteCfg.StoragePath
		out.path = filepath.Join(sqliteCfg.StoragePath, file)

	default:
		dir, err := paths.StoreDirFor(env)
		if err != nil {
			return resolved{}, fmt.Errorf("store: resolve the state directory: %w", err)
		}
		out.dir = dir
		out.path = filepath.Join(dir, storeFileName)
	}

	abs, err := filepath.Abs(out.path)
	if err != nil {
		return resolved{}, fmt.Errorf("store: absolute path for %s: %w", out.path, err)
	}
	out.path = abs
	out.dir = filepath.Dir(abs)
	out.writerDSN = composeDSN(abs, out.busyTimeout, true)
	out.readerDSN = composeDSN(abs, out.busyTimeout, false)
	return out, nil
}

// readPoolFromConfig reads the framework's PoolConfig for the READER pool.
//
// PoolConfig is one pool and DB15 needs two, so the contract is read for the
// half it can describe: the reader. The writer is pinned at one in code and
// says why (writerPoolSize). The framework's own default is 100, which is not
// a useful number for SQLite, so it is only honoured when explicitly set.
func readPoolFromConfig(env func(string) string) int {
	if raw := strings.TrimSpace(env("DATABASE_POOL_MAX_OPEN_CONNS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return DefaultReadPoolSize
}

// composeDSN builds the DSN, pragmas included (DB16).
//
// # The pragmas go in the DSN, and the syntax is the driver's
//
// `foreign_keys` is PER CONNECTION and defaults off, and database/sql creates
// connections lazily — so a connect hook registered once can be missed by a
// connection the pool opens later, and the symptom is a cascade that silently
// does not happen. In the DSN it is part of what opening means.
//
// The syntax matters and the wrong one fails SILENTLY. Measured against
// mattn/go-sqlite3:
//
//	?_journal_mode=WAL&_foreign_keys=1   journal=wal,    fk=1, cascade fires
//	?_pragma=journal_mode(WAL)&…         journal=delete, fk=0, cascade does NOT
//
// The second is another driver's spelling. It does not error — it is ignored,
// so a store that looks configured runs without WAL and without foreign keys.
// Hence: assert the pragmas by QUERYING them, and foreign keys by a CASCADE
// (D10). Reading the pragma back proves the statement ran, not that it took
// effect.
func composeDSN(path string, busy time.Duration, writer bool) string {
	q := url.Values{}
	q.Set("_journal_mode", "WAL")
	q.Set("_foreign_keys", "1")
	q.Set("_busy_timeout", strconv.Itoa(int(busy.Milliseconds())))
	// NORMAL rather than FULL: in WAL, NORMAL loses at most the tail of the
	// most recent commits on a power cut and never corrupts the file, and FULL
	// costs an fsync per commit on a store written once per loop iteration.
	q.Set("_synchronous", "NORMAL")

	if writer {
		// BEGIN IMMEDIATE on the writer, so the write lock is taken at BEGIN
		// where busy_timeout applies. A deferred transaction that upgrades to a
		// write mid-way gets SQLITE_BUSY *immediately* — busy_timeout does not
		// cover a lock upgrade — and that is exactly the failure D11 forbids
		// when a second PROCESS is writing (the demo's `debug hammer` beside a
		// CLI verb).
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}

// withRawDSN honours DATABASE_SQLITE_DSN, adding only what is missing.
//
// The framework documents this field as opened as-is, overriding the composed
// path. Aeon does not override the operator's DSN — but a raw DSN without
// `_foreign_keys` is a store whose cascades silently do not happen, so the
// pragmas are appended when absent and left alone when present.
func withRawDSN(dsn string, out resolved) (resolved, error) {
	trimmed := strings.TrimSpace(dsn)
	out.path = rawDSNPath(trimmed)
	out.dir = "" // as-is: no directory is created, matching the framework

	out.writerDSN = addMissingPragmas(trimmed, out.busyTimeout, true)
	out.readerDSN = addMissingPragmas(trimmed, out.busyTimeout, false)
	return out, nil
}

func addMissingPragmas(dsn string, busy time.Duration, writer bool) string {
	base, query, _ := strings.Cut(dsn, "?")
	q, err := url.ParseQuery(query)
	if err != nil {
		// An unparseable query string is the operator's business, and rewriting
		// it would change what they asked for. Opened as-is; it will fail at
		// Ping with the driver's own message.
		return dsn
	}
	defaults := url.Values{}
	defaults.Set("_journal_mode", "WAL")
	defaults.Set("_foreign_keys", "1")
	defaults.Set("_busy_timeout", strconv.Itoa(int(busy.Milliseconds())))
	if writer {
		defaults.Set("_txlock", "immediate")
	}
	for k, v := range defaults {
		if q.Get(k) == "" {
			q.Set(k, v[0])
		}
	}
	return base + "?" + q.Encode()
}

// rawDSNPath extracts a reportable path from a raw DSN, for `db info`.
//
// An in-memory DSN has no file, and saying so is better than printing a URL
// and letting a person look for it on disk.
func rawDSNPath(dsn string) string {
	base, query, _ := strings.Cut(dsn, "?")
	if q, err := url.ParseQuery(query); err == nil && q.Get("mode") == "memory" {
		return strings.TrimPrefix(base, "file:") + " (in memory)"
	}
	return strings.TrimPrefix(base, "file:")
}
