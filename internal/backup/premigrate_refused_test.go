package backup

// S62 T5 · behaviour 3 — the refused-open family, split from
// premigrate_test.go at the SIZE1 line (2026-11-02, post-merge follow-up):
// the test that a failed pre-migration run refuses the open, and the two
// witnesses it asserts with.
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// TestPreMigrateFailureRefusesOpen is behaviour 3: a failed run refuses the
// open with the reason, the dir and --no-backup named; the store is
// byte-identical; the ledger is unchanged (the migration did not run, so
// there is no table and no row); nothing is left in the backup dir.
// assertRefusedOpen is the shared witness for the refusal subtests: the
// store stands byte-identical, the migration never ran (so the table the
// PENDING migration creates does not exist at all), and the backup dir
// holds nothing.
func assertRefusedOpen(t *testing.T, stateDir, bkDir, before string) {
	t.Helper()
	tbl := pendingTable(t)
	if got := fileHash(t, filepath.Join(stateDir, "state.db")); got != before {
		t.Fatal("a refused open changed state.db's bytes; the migration was refused, so the store must stand untouched")
	}
	ro, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the ledger: %v", err)
	}
	var n int
	if err := ro.GormDB().Raw(`SELECT count(*) FROM sqlite_master WHERE name = '` + tbl + `'`).Scan(&n).Error; err != nil {
		t.Fatalf("probe the table: %v", err)
	}
	_ = ro.Close()
	if n != 0 {
		t.Fatalf("a refused open left a %s table behind; the migration it creates must not have run", tbl)
	}
	if got := premigFiles(t, bkDir); len(got) != 0 {
		t.Fatalf("a refused open left %v in the backup dir, want nothing", got)
	}
}

// assertHookRefusal checks the hook's sentence: it names the dir the copy
// was owed to and the one door off it (--no-backup), plus the subtest's own
// fragment for the reason.
func assertHookRefusal(t *testing.T, err error, bkDir string, fragment string) {
	t.Helper()
	sentence := err.Error()
	if !strings.Contains(sentence, filepath.Join(bkDir, "pre-migration")) {
		t.Fatalf("the refusal does not name the dir the copy was owed to:\n%s", sentence)
	}
	if !strings.Contains(sentence, "--no-backup") {
		t.Fatalf("the refusal does not name the one door off it (--no-backup):\n%s", sentence)
	}
	if !strings.Contains(sentence, fragment) {
		t.Fatalf("the refusal does not name its reason (%s):\n%s", fragment, sentence)
	}
}

func TestPreMigrateFailureRefusesOpen(t *testing.T) {
	t.Run("dir-unwritable", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		// The pre-migration path exists as a FILE: MkdirAll cannot make a
		// directory where a file stands, and the refusal is the same one
		// an unwritable dir produces — no root, no chmod, no timing.
		if err := os.WriteFile(filepath.Join(bkDir, "pre-migration"), []byte("a file stands where the dir is owed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := fileHash(t, filepath.Join(stateDir, "state.db"))
		_, err := store.Open(store.Options{
			Dir:        stateDir,
			PreMigrate: NewPreMigrate(testDeps(t, nil, bkDir), PreMigrateOptions{ActorKind: "cli", Out: os.Stderr}),
		})
		if err == nil {
			t.Fatal("the open succeeded over a backup dir that cannot hold the copy; it must refuse")
		}
		assertHookRefusal(t, err, bkDir, "make the backup dir")
		assertRefusedOpen(t, stateDir, bkDir, before)
	})
	t.Run("lock-held", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		premigDir := filepath.Join(bkDir, "pre-migration")
		if err := os.MkdirAll(premigDir, 0o700); err != nil {
			t.Fatal(err)
		}
		// A live holder in this process: the single-flight refuses an
		// earlier call the way it refuses another process.
		lock, err := Acquire(premigDir, "pre-migration", time.Now())
		if err != nil {
			t.Fatalf("plant the holder: %v", err)
		}
		t.Cleanup(func() { _ = lock.Release() })

		before := fileHash(t, filepath.Join(stateDir, "state.db"))
		_, oerr := store.Open(store.Options{
			Dir:        stateDir,
			PreMigrate: NewPreMigrate(testDeps(t, nil, bkDir), PreMigrateOptions{ActorKind: "cli", Out: os.Stderr}),
		})
		if oerr == nil {
			t.Fatal("the open succeeded while the lock is held; the single-flight must refuse")
		}
		assertHookRefusal(t, oerr, bkDir, "holds the backup lock")
		assertRefusedOpen(t, stateDir, bkDir, before)
	})
	t.Run("disk-full", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		// T4's platform seam: the run's free-space classifier stands in
		// with a low value, the way a full disk would read.
		orig := freeSpace
		t.Cleanup(func() { freeSpace = orig })
		freeSpace = func(string) (uint64, FreeState) { return 1024, FreeLow }

		before := fileHash(t, filepath.Join(stateDir, "state.db"))
		_, err := store.Open(store.Options{
			Dir:        stateDir,
			PreMigrate: NewPreMigrate(testDeps(t, nil, bkDir), PreMigrateOptions{ActorKind: "cli", Out: os.Stderr}),
		})
		if err == nil {
			t.Fatal("the open succeeded over a disk the copy would not fit on; it must refuse")
		}
		assertHookRefusal(t, err, bkDir, "not enough free space")
		assertRefusedOpen(t, stateDir, bkDir, before)
	})
	t.Run("integrity-failure", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		// The file check fails on the copy: a snapshot that cannot pass
		// integrity_check is not a verified undo, and the hook must treat
		// it as a failure, not a success.
		deps := testDeps(t, nil, bkDir)
		deps.CheckFile = func(ctx context.Context, path string) (store.FileReport, error) {
			return store.FileReport{IntegrityOK: false, IntegrityDetail: []string{"main: simulated corruption"}}, nil
		}
		before := fileHash(t, filepath.Join(stateDir, "state.db"))
		_, err := store.Open(store.Options{
			Dir:        stateDir,
			PreMigrate: NewPreMigrate(deps, PreMigrateOptions{ActorKind: "cli", Out: os.Stderr}),
		})
		if err == nil {
			t.Fatal("the open succeeded over a copy that fails integrity; it must refuse")
		}
		assertHookRefusal(t, err, bkDir, "integrity")
		assertRefusedOpen(t, stateDir, bkDir, before)
	})
}
