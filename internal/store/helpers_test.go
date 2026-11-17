package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/testutil/dbfixture"
)

// The real user may already be running Aeon. Capture the pre-suite baseline so
// D19 detects files this suite created without accusing an existing daemon.
var realStateBefore = map[string]bool{}

func realStateCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{filepath.Join(os.TempDir(), "aeon", "state.db")}
	}
	return []string{
		filepath.Join(home, ".local", "state", "aeon", "state.db-wal"),
		filepath.Join(os.TempDir(), "aeon", "state.db"),
	}
}

// TestMain quiets the framework's migration logging.
//
// The framework's migration runner logs every migration at INFO through
// logrus, and every test in this package opens a store. Without this, one
// `go test` run prints several hundred lines of DDL progress and a real
// failure is somewhere in the middle of it.
//
// Warn rather than Fatal: a migration that FAILS still says so, which is the
// thing worth seeing.
func TestMain(m *testing.M) {
	// The cold-resume test re-executes this binary as a CHILD PROCESS (D17).
	// The dispatch happens before the test framework starts, so the child runs
	// one program and prints one report rather than running the suite again.
	if os.Getenv(childEnv) != "" {
		os.Exit(coldReadChild())
	}
	for _, candidate := range realStateCandidates() {
		_, err := os.Stat(candidate)
		realStateBefore[candidate] = err == nil
	}
	logrus.SetLevel(logrus.WarnLevel)
	os.Exit(m.Run())
}

// fixedClock is a deterministic clock, so a test can assert an exact ISO 8601
// string rather than a shape (I9).
func fixedClock() func() time.Time {
	t := time.Date(2026, 10, 15, 9, 41, 12, 345_000_000, time.UTC)
	return func() time.Time { return t }
}

var emptyStoreFixture dbfixture.Template

// openTemp opens a store in a temp directory, closed on cleanup.
//
// EVERY test uses it. D19's rule is not "avoid the real store" but "there is
// no way to reach it from here": no test in this package names a path outside
// t.TempDir(), and `no test creates the real state directory` asserts it.
func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := emptyStoreFixture.Dir(t, "state.db", func(dir string) error {
		s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
		if err != nil {
			return err
		}
		return s.Close()
	})
	// This is already the current schema. Migration and upgrade tests use
	// openAt or Open directly; row tests need no repeated ledger traversal.
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open cloned store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func openAt(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open a store in %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// emptyEnv is an environment that says nothing, so a test exercises the
// fallback chain rather than whatever the developer's shell happens to hold.
func emptyEnv(string) string { return "" }

// seedLoop creates a loop and returns its scope, for the tests whose subject
// is something hanging off a loop.
func seedLoop(t *testing.T, s *Store, slug, root string) Scope {
	t.Helper()
	if _, err := s.CreateLoop(NewLoop{Slug: slug, ComponentRoot: root, Goal: "a goal"}); err != nil {
		t.Fatalf("create loop %s in %s: %v", slug, root, err)
	}
	return Scope{LoopSlug: slug, ComponentRoot: root}
}

func seedStep(t *testing.T, s *Store, sc Scope) Step {
	t.Helper()
	st, err := s.CreateStep(sc, NewStep{Phase: "build", Agent: "golang-engineer"})
	if err != nil {
		t.Fatalf("create a step in %s: %v", sc, err)
	}
	return st
}

// The exact strings the fidelity tests assert on.
//
// A trailing newline and an internal one are both present ON PURPOSE: the
// failure being ruled out is a column or an encoder that normalises whitespace,
// and a signature trimmed by one byte is rejected by Anthropic on the next
// tool-use turn (P21). They are constants so the write and the assertion
// cannot drift.
const (
	thinkingText      = "Let me check: 17 × 23.\n  17 × 20 = 340, 17 × 3 = 51, so 391.\n"
	thinkingSignature = "ErUBCkYIBBgCKkBhZW9uLXMtNC1zaWduYXR1cmUtZml4dHVyZS0wMDAxLXRyYWlsaW5nLW5ld2xpbmUtbWF0dGVycw==\n"
)

// testProvenance is the producer stamp S18's M26 requires on every assistant
// row that AppendMessages accepts. Fixtures use it because a fabricated
// transcript in a test is still an assistant turn, and the gate that
// protects the record does not know which transcript is fiction.
func testProvenance() *model.Provenance {
	return &model.Provenance{Route: "test-route", Provider: "test-provider", Model: "test-model"}
}

// signedTranscript is a transcript with all four block kinds and a signed
// thinking block.
func signedTranscript() []model.Message {
	return []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("you are terse")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("what is 17 × 23?")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{
			model.ThinkingBlock(thinkingText, thinkingSignature),
			model.TextBlock("391."),
			model.ToolUseBlock("toolu_01", "calculator", []byte(`{"expr":"17*23"}`)),
		}},
		{Role: model.RoleUser, Content: []model.Block{
			model.ToolResultBlock("toolu_01", "391", false),
		}},
	}
}

// packageFiles lists this package's non-test sources, for the grep-shaped
// tests.
//
// A grep over the SOURCES rather than over the repository, because the rules it
// enforces (reads use the reader pool; only open.go touches the filesystem) are
// about this package. The repository-wide greps live in store_test.go.
func packageFiles(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || hasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(b)
	}
	if len(out) < 5 {
		// A guard on the guard: a grep test that reads no files passes, and
		// then reports nothing forever.
		t.Fatalf("expected this package to have several sources, found %d", len(out))
	}
	return out
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// mustParseISO parses the store's own timestamp format, for a test that needs
// to advance the clock.
func mustParseISO(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// repoHit is one grep match, for the repository-wide invariants.
type repoHit struct {
	file string
	line string
}

// grepRepo walks the module's Go sources and returns matching lines.
//
// It walks `internal/` and `cmd/` from the module root, which is two levels up
// from this package. The repository-wide invariants (only the store writes
// event.id; no SQL outside the store) are about the whole module, so a grep
// over this package alone would assert nothing.
func grepRepo(t *testing.T, pattern *regexp.Regexp) []repoHit {
	t.Helper()
	root := filepath.Join("..", "..")
	var hits []repoHit
	for _, dir := range []string{"internal", "cmd", "scripts"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			// Test files are excluded: they legitimately write rows directly to
			// set up a state the store's own API refuses to create.
			if hasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			for _, line := range strings.Split(string(b), "\n") {
				if pattern.MatchString(line) {
					hits = append(hits, repoHit{file: rel, line: strings.TrimSpace(line)})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return hits
}

// itoa and fmtSscan keep the child program's helpers in one place.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func fmtSscan(s string, into *int64) (int, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	*into = n
	return 1, nil
}

// frozenMigrationCount is the count the "this task adds no migration"
// guards pin: the frozen legacy block (1–43, immutable — schema.go's
// forward-only rule) plus every timestamped file under migrations/ — the
// sanctioned additive path (migrations/README.md, amended 2026-10-30 for
// parallel worktrees). A hard-coded total would have to be edited by every
// slice that adds a legitimate file — exactly the per-slice merge conflict
// the file mechanism exists to delete; the FROZEN block is what these
// guards actually protect, and the files are additive by design.
func frozenMigrationCount() int {
	return frozenLegacyCount() + frozenFileCount()
}

// frozenFileCount is the sanctioned additive half: the timestamped files
// under migrations/ (their IDs are 14-digit timestamps; the legacy IDs are
// small integers, which is what tells the two halves apart).
func frozenFileCount() int {
	extra, err := parseTimestampedMigrations(migrationsDirFS, "migrations")
	if err != nil {
		panic(err)
	}
	return len(extra)
}

// frozenLegacyCount counts the numbered literals in schema.go. It is not
// a literal: main grew the block from 43 to 45 (0044 S29's bug, 0045 S37's
// agent_import widening — numbered entries, not files) while the S30
// worktree was cut at 43, and a hardcoded 43 here would have made every
// additive guard in the repo wrong on main. The block is still frozen —
// nobody edits a shipped entry — it is just no longer assumed to be 43.
func frozenLegacyCount() int {
	n := 0
	for _, m := range Migrations() {
		if m.ID > 1_000_000_000_000 {
			break // the first 14-digit timestamp ID is a file, not a literal
		}
		n++
	}
	return n
}

// backupRunMigrationIndex finds the backup_run migration by its file's slug —
// by identity, not position. S62 T5's vintage tests assumed it was the LAST
// file, and S57 T7 (2026-11-02) proved the assumption contingent: merge_watch
// landed with a later timestamp, and the "everything but the last" slice
// dropped the wrong file. An index by name makes "the store from the day
// before backup_run" a fact about the name, not an accident of ordering —
// the same robustness S62 applied to its fresh-open pin ("a new migration
// file cannot break this test").
func backupRunMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_backup_run") {
			return i
		}
	}
	panic("the backup_run migration is missing from Migrations()")
}

// mergeWatchMigrationIndex is the same identity-not-position fix one
// migration later: S57 T8's proposal kinds file (20261102132731) landed
// after merge_watch in file order, so "everything but the last" would
// drop the wrong file.
func mergeWatchMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_merge_watch") {
			return i
		}
	}
	panic("the merge_watch migration is missing from Migrations()")
}
