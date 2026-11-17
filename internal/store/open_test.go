package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T1's tests. Two of them look pedantic and are not: the cascade, and
// vec_version on both pools.

func TestTheStatePathFallsBackThroughOptsTheFrameworkConfigAndXDG(t *testing.T) {
	// The chain is the FRAMEWORK's config plane (the 2026-10-11 redirect),
	// with AEON_STATE_DIR above it and the XDG fallback below. There is no
	// second resolver and no second config struct: internal/paths resolves the
	// default, internal/appenv feeds it into DATABASE_SQLITE_STORAGE_PATH, and
	// this reads those.
	home := t.TempDir()
	cases := []struct {
		name string
		opts Options
		env  map[string]string
		want string
	}{
		{
			name: "opts.Dir wins over everything",
			opts: Options{Dir: filepath.Join(home, "explicit")},
			env: map[string]string{
				"AEON_STATE_DIR":               filepath.Join(home, "aeon-state"),
				"DATABASE_SQLITE_STORAGE_PATH": filepath.Join(home, "framework"),
				"XDG_STATE_HOME":               filepath.Join(home, "xdg"),
			},
			want: filepath.Join(home, "explicit", "state.db"),
		},
		{
			name: "the framework's storage path and file name",
			env: map[string]string{
				"DATABASE_SQLITE_STORAGE_PATH": filepath.Join(home, "framework"),
				"DATABASE_SQLITE_FILE_NAME":    "aeon.db",
				"XDG_STATE_HOME":               filepath.Join(home, "xdg"),
			},
			want: filepath.Join(home, "framework", "aeon.db"),
		},
		{
			name: "AEON_STATE_DIR, when the framework's config says nothing",
			env: map[string]string{
				"AEON_STATE_DIR": filepath.Join(home, "aeon-state"),
				"XDG_STATE_HOME": filepath.Join(home, "xdg"),
			},
			want: filepath.Join(home, "aeon-state", "state.db"),
		},
		{
			name: "XDG_STATE_HOME/aeon",
			env:  map[string]string{"XDG_STATE_HOME": filepath.Join(home, "xdg")},
			want: filepath.Join(home, "xdg", "aeon", "state.db"),
		},
		{
			name: "~/.local/state/aeon, when nothing is set",
			env:  map[string]string{"HOME": filepath.Join(home, "person")},
			want: filepath.Join(home, "person", ".local", "state", "aeon", "state.db"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			// The environment is INJECTED, never set on the process (I7). A
			// test that did os.Setenv here would change what every other test
			// in this package reads.
			opts.Env = func(k string) string { return tc.env[k] }
			got, err := resolveConfig(opts)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got.path != tc.want {
				t.Errorf("path\n  got  %s\n  want %s", got.path, tc.want)
			}
		})
	}
}

func TestAnIgnoredRelativeXDGStateHomeFallsThroughToHome(t *testing.T) {
	// The XDG spec requires an absolute path. A relative one would resolve
	// against the process's working directory, so for a daemon the store would
	// move depending on how it was launched.
	home := t.TempDir()
	got, err := resolveConfig(Options{Env: func(k string) string {
		return map[string]string{"XDG_STATE_HOME": "relative/path", "HOME": home}[k]
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "aeon", "state.db")
	if got.path != want {
		t.Errorf("path\n  got  %s\n  want %s", got.path, want)
	}
}

func TestTheWindowsFallbackResolvesToLocalAppData(t *testing.T) {
	// Skipped with a named reason on POSIX: paths.StoreDirFor branches on
	// runtime.GOOS, which a test cannot change. The case is exercised by the
	// Windows CI target, and recorded here so its absence is visible rather
	// than assumed.
	if os.PathSeparator != '\\' {
		t.Skip("the Windows arm of paths.StoreDirFor is only reachable on Windows; the Windows CI target runs it")
	}
	got, err := resolveConfig(Options{Env: func(k string) string {
		return map[string]string{"LocalAppData": `C:\Users\p\AppData\Local`}[k]
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(got.path, "aeon") {
		t.Errorf("path %s does not name aeon", got.path)
	}
}

func TestForeignKeysAreOnProvenByACascade(t *testing.T) {
	// D10: proven by a CASCADE, not by a pragma read-back. Reading the pragma
	// back proves the statement RAN; a cascade proves it took EFFECT. Measured:
	// with the wrong DSN spelling the pragma reads 0 and the child row survives
	// — and nothing else in the suite would notice.
	s := openTemp(t)
	sc := seedLoop(t, s, "cascades", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatalf("append: %v", err)
	}

	before, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before != 4 {
		t.Fatalf("expected 4 transcript rows before the delete, got %d", before)
	}

	if _, err := s.w.Exec(`DELETE FROM step WHERE id = ?`, st.ID); err != nil {
		t.Fatalf("delete the step: %v", err)
	}

	after, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Errorf("the cascade did not happen: %d step_message rows survived their step. "+
			"That means foreign_keys is OFF on this connection, whatever the pragma reads back", after)
	}
}

func TestTheCascadeHoldsOnAConnectionCreatedAfterTheFirst(t *testing.T) {
	// The specific bug a connect hook has: database/sql creates connections
	// LAZILY, so a hook registered once can be missed by a connection the pool
	// opens later. The symptom is a cascade that works in the first test and
	// silently stops working under load.
	s := openTemp(t)

	// Force the reader pool to open several connections, so the one the
	// assertion runs on is not the first this process created. They are opened
	// CONCURRENTLY — taken one at a time, the pool hands back the same
	// connection and nothing new is created.
	ctx := context.Background()
	var closers []func() error
	for i := 0; i < 3; i++ {
		c, err := s.r.Conn(ctx)
		if err != nil {
			t.Fatalf("open reader connection %d: %v", i, err)
		}
		closers = append(closers, c.Close)
	}
	for _, close := range closers {
		_ = close()
	}

	sc := seedLoop(t, s, "later-conn", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM step WHERE id = ?`, st.ID); err != nil {
		t.Fatalf("delete the step: %v", err)
	}
	n, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the cascade did not happen on a later connection: %d rows survived", n)
	}
}

func TestBothPoolsCarryEveryPragma(t *testing.T) {
	// Asserted PER POOL, by querying. The DSN is never inspected: the wrong
	// spelling produces a DSN that LOOKS configured and a connection that is
	// not (measured — `_pragma=journal_mode(WAL)` is another driver's and is
	// silently ignored by this one).
	s := openTemp(t)

	for _, p := range []struct {
		name string
		q    func(string) (string, error)
	}{
		{"writer", func(pragma string) (string, error) {
			var v string
			err := s.w.QueryRow("PRAGMA " + pragma).Scan(&v)
			return v, err
		}},
		{"reader", func(pragma string) (string, error) {
			var v string
			err := s.r.QueryRow("PRAGMA " + pragma).Scan(&v)
			return v, err
		}},
	} {
		t.Run(p.name, func(t *testing.T) {
			for pragma, want := range map[string]string{
				"journal_mode": "wal",
				"foreign_keys": "1",
				"busy_timeout": "5000",
				"synchronous":  "1", // NORMAL
			} {
				got, err := p.q(pragma)
				if err != nil {
					t.Fatalf("read %s: %v", pragma, err)
				}
				if strings.ToLower(got) != want {
					t.Errorf("%s on the %s pool: got %q, want %q", pragma, p.name, got, want)
				}
			}
		})
	}
}

func TestTheWriterPoolHasMaxOpenConnsOne(t *testing.T) {
	// DB15, structurally. Asserted on the pool's own stats rather than on the
	// constant, so a change to Open that forgot to apply it would fail here.
	s := openTemp(t)
	if got := s.w.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("the writer pool allows %d connections; DB15 requires exactly 1", got)
	}
	if got := s.r.Stats().MaxOpenConnections; got != DefaultReadPoolSize {
		t.Errorf("the reader pool allows %d connections, want %d", got, DefaultReadPoolSize)
	}
}

func TestASecondWriterBlocksRatherThanErroring(t *testing.T) {
	// Q6, and DB15 depends on the answer. Measured: database/sql QUEUES the
	// second writer at the pool, and the wait is interruptible by context. If
	// it errored instead, DB15's whole argument — predictable serialisation
	// rather than a busy_timeout retry loop — would be wrong.
	s := openTemp(t)

	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	blocked := make(chan error, 1)
	go func() {
		_, err := s.w.Exec(`INSERT INTO event (kind, payload, created_at) VALUES ('second','{}','now')`)
		blocked <- err
	}()

	select {
	case err := <-blocked:
		_ = tx.Rollback()
		t.Fatalf("the second writer returned immediately (err=%v) instead of waiting at the pool. "+
			"DB15 assumes it blocks: if it errors, a busy second write becomes a failure rather than a queue", err)
	case <-time.After(150 * time.Millisecond):
		// Still waiting, which is the answer DB15 needs.
	}

	if _, err := tx.Exec(`INSERT INTO event (kind, payload, created_at) VALUES ('first','{}','now')`); err != nil {
		t.Fatalf("the holding transaction could not write: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-blocked; err != nil {
		t.Errorf("the queued writer failed after the lock was released: %v", err)
	}
}

func TestAQueuedWriterRespectsItsContext(t *testing.T) {
	// The other half of Q6's answer: the wait is not unbounded. A caller with a
	// deadline gets it, rather than inheriting the pool's patience.
	s := openTemp(t)
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = s.w.ExecContext(ctx, `INSERT INTO event (kind, payload, created_at) VALUES ('x','{}','now')`)
	if err == nil {
		t.Fatal("a write that should have waited past its deadline succeeded")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("the queued write failed with %v, which is not the context's doing", err)
	}
}

func TestVecVersionAnswersOnBothPoolsWithNoLoadStep(t *testing.T) {
	// Q2 and D18. An extension registered for one pool is a retrieval slice
	// that works in tests and fails under load — so BOTH are asked.
	//
	// And there is no loading step to perform: the bindings register it through
	// sqlite3_auto_extension() at init, so a failure here means the binary was
	// built without cgo.
	s := openTemp(t)
	w, r, err := s.VecVersion()
	if err != nil {
		t.Fatalf("vec_version: %v", err)
	}
	if w == "" || r == "" {
		t.Fatalf("vec_version answered %q on the writer and %q on the reader", w, r)
	}
	if w != r {
		t.Errorf("the two pools report different extension versions: writer %q, reader %q", w, r)
	}
}

func TestVecVersionAnswersOnEveryReaderConnection(t *testing.T) {
	// The stronger form of Q2: not "the pool answers once" but "every
	// connection the pool opens answers". Four are forced open at the same
	// time, because the pool reuses one otherwise and the test would prove
	// nothing about the second.
	s := openTemp(t)
	ctx := context.Background()

	var closers []func() error
	for i := 0; i < 4; i++ {
		c, err := s.r.Conn(ctx)
		if err != nil {
			t.Fatalf("open reader connection %d: %v", i, err)
		}
		var v string
		if err := c.QueryRowContext(ctx, "select vec_version()").Scan(&v); err != nil {
			t.Errorf("vec_version on reader connection %d: %v", i, err)
		}
		if v == "" {
			t.Errorf("reader connection %d answered an empty version", i)
		}
		closers = append(closers, c.Close)
	}
	for _, close := range closers {
		_ = close()
	}
	if got := s.r.Stats().OpenConnections; got < 2 {
		t.Errorf("the pool only ever opened %d connection(s), so this test did not check a second one", got)
	}
}

func TestNoExtensionLoadingPathExists(t *testing.T) {
	// DB18: vectors are always present, so there is nothing to load. A
	// LoadExtension call appearing here would mean somebody wrote the
	// extension-missing state this slice is supposed not to have.
	for name, src := range packageFiles(t) {
		if strings.Contains(src, "LoadExtension") || strings.Contains(src, "load_extension") {
			t.Errorf("%s loads an extension; DB18 says the extension is statically linked and there is "+
				"no extension-missing state to handle", name)
		}
	}
}

func TestOnlyOpenGoReadsTheEnvironment(t *testing.T) {
	// I7: no ambient state. The state path resolves once, in open.go, from an
	// injectable environment — so a test can exercise the fallback chain
	// without mutating the process, and two stores in one process cannot
	// disagree about where they are.
	for name, src := range packageFiles(t) {
		if name == "open.go" {
			continue
		}
		if strings.Contains(src, "os.Getenv") {
			t.Errorf("%s reads os.Getenv; the state path resolves only in open.go (I7)", name)
		}
	}
}

func TestOnlyOpenGoTouchesTheFilesystem(t *testing.T) {
	// DB10: the store writes no files. open.go legitimately creates the state
	// directory; everything else must go through the database.
	//
	// store.go's os.Stat is allowed and named: reporting a file's SIZE is not
	// writing to it, and `db info` prints it.
	//
	// backup.go is the one designed exception (S62 T2, BK3.2): the snapshot
	// artefact is a FILE — the store's one legitimate file writer. It may
	// create its parent, claim the destination (O_EXCL) and remove it on
	// failure. It may not rename, and it may not write arbitrary bytes
	// (the driver writes the pages).
	calls := []string{"os.Create", "os.WriteFile", "os.MkdirAll", "os.Remove", "os.OpenFile", "os.Rename"}
	for name, src := range packageFiles(t) {
		if name == "open.go" {
			continue
		}
		banned := calls
		if name == "backup.go" {
			banned = []string{"os.Create", "os.WriteFile", "os.Rename"}
		}
		for _, call := range banned {
			if strings.Contains(src, call) {
				t.Errorf("%s calls %s; the store writes no files (DB10) and only open.go creates its "+
					"directory", name, call)
			}
		}
	}
}

func TestNoTestCreatesTheRealStateDirectory(t *testing.T) {
	// D19. Every test in this package passes an explicit Dir or an injected
	// environment, so there is no path from here to a real store — this asserts
	// the absence rather than trusting it.
	//
	// It checks the REAL XDG location for a file this suite would have created,
	// and it does not create it either: a test that mkdir'd to check would be
	// the thing it is testing for.
	for _, candidate := range realStateCandidates() {
		if _, err := os.Stat(candidate); err == nil && !realStateBefore[candidate] {
			// A -wal file is the tell: it exists only while a database has been
			// opened in WAL mode, and this suite is the only thing in the
			// repository that opens one.
			t.Errorf("%s exists; a test may have opened the real store", candidate)
		}
	}
}

func TestTheGuessesAreStatedAsGuesses(t *testing.T) {
	// O7: the reader pool size and busy_timeout are numbers to calibrate, and
	// D11's QA case is expected to CHANGE them rather than pass. A number
	// presented as settled is a number nobody revisits.
	src := packageFiles(t)["open.go"]
	for _, phrase := range []string{"GUESSES", "DefaultBusyTimeout", "DefaultReadPoolSize"} {
		if !strings.Contains(src, phrase) {
			t.Errorf("open.go no longer mentions %q; the pool size and busy_timeout must be stated as guesses (O7)", phrase)
		}
	}
}

func TestARawDSNIsHonouredAndGetsThePragmasItIsMissing(t *testing.T) {
	// The framework documents SQLiteConfig.DSN as opened AS-IS, and Aeon
	// honours that — it is the operator's path business. But a raw DSN without
	// _foreign_keys is a store whose cascades silently do not happen, so the
	// pragmas are added when absent and left alone when present.
	dir := t.TempDir()
	raw := "file:" + filepath.Join(dir, "raw.db") + "?_busy_timeout=99"

	got, err := resolveConfig(Options{Env: func(k string) string {
		if k == "DATABASE_SQLITE_DSN" {
			return raw
		}
		return ""
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(got.writerDSN, "_busy_timeout=99") {
		t.Errorf("the operator's busy_timeout was overridden: %s", got.writerDSN)
	}
	for _, want := range []string{"_journal_mode=WAL", "_foreign_keys=1"} {
		if !strings.Contains(got.writerDSN, want) {
			t.Errorf("the raw DSN did not gain %s: %s", want, got.writerDSN)
		}
	}

	s, err := Open(Options{Env: func(k string) string {
		if k == "DATABASE_SQLITE_DSN" {
			return raw
		}
		return ""
	}})
	if err != nil {
		t.Fatalf("open with a raw DSN: %v", err)
	}
	defer s.Close()
	var fk int
	if err := s.w.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Error("a raw DSN opened without foreign keys, so its cascades would silently not happen")
	}
}

func TestTheReaderPoolSizeComesFromTheFrameworksPoolConfig(t *testing.T) {
	// PoolConfig is ONE pool and DB15 needs two, so the contract is read for
	// the half it can describe — the reader. The writer is pinned at one in
	// code, which is a decision outranking configuration (the same shape as a
	// service pin).
	got, err := resolveConfig(Options{Dir: t.TempDir(), Env: func(k string) string {
		if k == "DATABASE_POOL_MAX_OPEN_CONNS" {
			return "9"
		}
		return ""
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.readPoolSize != 9 {
		t.Errorf("the reader pool size is %d; the framework's config said 9", got.readPoolSize)
	}
}
