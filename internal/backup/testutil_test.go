package backup

// Test helpers shared by the package's behaviour tests: a fake store that
// satisfies the Run seam, a default file report, and a real throwaway store
// for the round-trip and confidentiality tests (fresh XDG dir per test, per
// the house rule that tests never point at the real store).

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// testNow is the fixed clock: filenames, manifests and ledger stamps are
// exact in every assertion.
var testNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// fakeStore is the Run seam's test implementation. It writes a marker
// snapshot file (so the check seam can stat it), records every ledger row
// it is asked to write, and fails on demand.
type fakeStore struct {
	mu         sync.Mutex
	applied    []string
	info       store.Info
	rows       []store.NewBackupRun
	recordErr  error
	snapErr    error
	snapCalled int
	lastDest   string
}

func newFakeStore(applied []string) *fakeStore {
	return &fakeStore{
		applied: applied,
		info:    store.Info{Bytes: 4 << 20, WALBytes: 64 << 10, JournalMode: "wal"},
	}
}

func (f *fakeStore) SnapshotTo(ctx context.Context, dest string) (store.SnapshotInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapCalled++
	if f.snapErr != nil {
		return store.SnapshotInfo{}, f.snapErr
	}
	f.lastDest = dest
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return store.SnapshotInfo{}, err
	}
	if err := os.WriteFile(dest, []byte("fake-snapshot-bytes"), 0o600); err != nil {
		return store.SnapshotInfo{}, err
	}
	return store.SnapshotInfo{Bytes: 19}, nil
}

func (f *fakeStore) AppliedNamesAndPending() ([]string, []store.Migration, bool, error) {
	return f.applied, nil, len(f.applied) == 0, nil
}

func (f *fakeStore) Info() (store.Info, error) { return f.info, nil }

func (f *fakeStore) RecordBackupRun(in store.NewBackupRun) (store.BackupRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return store.BackupRun{}, f.recordErr
	}
	f.rows = append(f.rows, in)
	return store.BackupRun{ID: int64(len(f.rows))}, nil
}

func (f *fakeStore) Now() string  { return testNow.Format(createdFormat) }
func (f *fakeStore) Path() string { return "/fake/state.db" }

func (f *fakeStore) rowsSnapshot() []store.NewBackupRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.NewBackupRun, len(f.rows))
	copy(out, f.rows)
	return out
}

// firstApplied names the first n of this binary's real migrations, so a
// fake's manifest stays inside KnownMigrationNames.
func firstApplied(n int) []string {
	migs := store.Migrations()
	out := make([]string, 0, n)
	for i := 0; i < n && i < len(migs); i++ {
		out = append(out, migs[i].Name)
	}
	return out
}

// lastApplied names the last n — a manifest built from these still passes
// newerSchema, which is what the round trip wants.
func lastApplied(n int) []string {
	migs := store.Migrations()
	out := make([]string, 0, n)
	for i := len(migs) - n; i < len(migs); i++ {
		out = append(out, migs[i].Name)
	}
	return out
}

// defaultFakeReport is a healthy file report whose counts and fingerprint
// a test can then tamper with, one field at a time.
func defaultFakeReport(applied []string) store.FileReport {
	return store.FileReport{
		IntegrityOK:     true,
		IntegrityDetail: []string{"ok"},
		Tables:          map[string]int64{"agent": 3, "loop": 1},
		Fingerprint:     store.Fingerprint{Lines: []string{"agent", "loop"}},
		Applied:         applied,
		VecVersion:      "v0.1.6",
		JournalMode:     "delete",
	}
}

// fakeDeps assembles a Deps over the fake store with a controllable check
// answer, the fixed clock, and settings that answer nothing but defaults.
func fakeDeps(s *fakeStore, rep store.FileReport, stateDir string) Deps {
	return Deps{
		Store:     s,
		CheckFile: func(ctx context.Context, path string) (store.FileReport, error) { return rep, nil },
		Clock:     func() time.Time { return testNow },
		Settings:  NewEnvSettingsWith(func(string) string { return "" }),
		Version:   "0.0.0-test",
		StateDir:  stateDir,
	}
}

// openRealStore opens a throwaway real store: migrations run, the file
// lives under dir, and the test never touches the real state directory.
func openRealStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: dir})
	if err != nil {
		t.Fatalf("open the throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
