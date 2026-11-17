package main

// S62 T5 · the pre-migration safety net at the CLI door.
//
// The store-level seam pin (hook before Migrate, the deferred row after)
// lives in internal/store; the hook's behaviour lives in internal/backup.
// This file is what the DOOR does: the daemon is down, a store from an
// older binary sits on disk, and a plain verb open is the migrating open —
// the backup must happen there, without anyone asking for it.
//
// The behaviours pinned:
//
//  1. `db info` over a behind-vintage store takes the pre-migration
//     backup, migrates, and records the row — and the store is the one
//     that ends up current, not the one that was found;
//  2. `--no-backup` is the one door off it: the store still migrates, the
//     row is the skipped one, the sentence goes to the door's stderr, and
//     nothing is written to the backup dir.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	"github.com/cjlapao/go-backend-scaffolding/framework/database/migrations"
)

// bkSeedVintage opens a store at the state dir's location at the vintage of
// every migration but the last, the way a store from an older binary sits
// on disk, and closes it. The CLI's own open is the one under test, so the
// seed goes through the store seam directly.
func bkSeedVintage(t *testing.T, stateDir string) int {
	t.Helper()
	dir := filepath.Join(stateDir, "aeon")
	st, err := store.Open(store.Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for the vintage seed: %v", err)
	}
	all := store.Migrations()
	if len(all) < 2 {
		t.Fatal("the migration set is too small for a vintage test")
	}
	svc := migrations.NewMigrationService(st.GormDB())
	svc.SetMaxRetries(1)
	for _, m := range all[:len(all)-1] {
		svc.Register(migrations.NewSQLMigrationWorkerFromContent(st.GormDB(), m.Name, m.Description, m.ID, m.SQL, ""))
	}
	if diag := svc.RunAll(appctx.NewContext(context.Background())); diag != nil && diag.HasErrors() {
		_ = st.Close()
		t.Fatalf("seed the vintage: %s", diag.GetSummary())
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close after the seed: %v", err)
	}
	return len(all) - 1
}

// bkPreMigDir is where the door's backup lands: the default backup dir is
// <state>/backups, and the pre-migration family keeps its own subdir.
func bkPreMigDir(stateDir string) string {
	return filepath.Join(stateDir, "aeon", "backups", "pre-migration")
}

// bkAeonbaks lists the .aeonbak files in a dir; absent or not-a-dir is empty.
func bkAeonbaks(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("list %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".aeonbak") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestBKDBInfoOverAVintageStoreTakesTheBackup is the door behaviour: with
// the daemon down, a behind-vintage store, and nothing but a plain
// `db info`, the open migrates — and the copy is made first, because the
// migration is the thing the copy protects.
func TestBKDBInfoOverAVintageStoreTakesTheBackup(t *testing.T) {
	stateDir := t.TempDir()
	appliedBefore := bkSeedVintage(t, stateDir)
	all := store.Migrations()

	got, stderr, err := bkRun(t, stateDir, "db", "info")
	if err != nil {
		t.Fatalf("db info over the vintage store: %v\nstderr: %s", err, stderr)
	}

	// The store is current now: the verb opened it, and the open applied
	// the pending migration. The schema line says so by count.
	if want := fmt.Sprintf("schema  %d migration(s) applied, latest %s", len(all), all[len(all)-1].Name); !strings.Contains(got, want) {
		t.Fatalf("db info does not report the current schema.\nwant a line containing: %s\ngot:\n%s", want, got)
	}

	// The copy: exactly one, in the pre-migration subdir of the default
	// backup dir — the one-file proof, in the door's own hands.
	files := bkAeonbaks(t, bkPreMigDir(stateDir))
	if len(files) != 1 {
		t.Fatalf("the pre-migration dir holds %v, want exactly one backup", files)
	}

	// The ledger: exactly the one row the hook owed, with the door's actor.
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the ledger: %v", err)
	}
	runs, err := st.ListBackupRuns(store.BackupRunFilter{})
	_ = st.Close()
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d rows, want exactly the hook's one: %+v", len(runs), runs)
	}
	r := runs[0]
	if r.Kind != "backup" || r.Reason != "pre-migration" || r.ActorKind != "cli" || r.Outcome != "ok" {
		t.Fatalf("the row's identity is wrong: %+v", r)
	}
	if r.VerifyLevel == nil || *r.VerifyLevel != "verified" {
		t.Fatalf("the row claims verify level %v, want verified — the undo is verified (BK6)", r.VerifyLevel)
	}
	if r.FileName == nil || *r.FileName != files[0] {
		t.Fatalf("the row names %v, want the file on disk %v", r.FileName, files)
	}
	if r.AppliedMigrations == nil || int(*r.AppliedMigrations) != appliedBefore {
		t.Fatalf("the row records %v applied, want the %d the store had at the copy's moment", r.AppliedMigrations, appliedBefore)
	}
}

// TestBKDBInfoNoBackupMigratesAndSaysSo is the flag's door behaviour: the
// store still migrates, the row is the skipped one, nothing is on disk, and
// the fixed sentence goes to stderr — the only place it may go.
func TestBKDBInfoNoBackupMigratesAndSaysSo(t *testing.T) {
	stateDir := t.TempDir()
	bkSeedVintage(t, stateDir)
	all := store.Migrations()

	got, stderr, err := bkRun(t, stateDir, "db", "info", "--no-backup")
	if err != nil {
		t.Fatalf("db info --no-backup: %v\nstderr: %s", err, stderr)
	}

	// It still migrated: --no-backup skips the copy, never the migration.
	if want := fmt.Sprintf("schema  %d migration(s) applied, latest %s", len(all), all[len(all)-1].Name); !strings.Contains(got, want) {
		t.Fatalf("db info --no-backup did not migrate the store.\nwant a line containing: %s\ngot:\n%s", want, got)
	}

	// The sentence, on stderr, verbatim.
	if !strings.Contains(stderr, "no backup was taken before migrating (--no-backup)") {
		t.Fatalf("the door's stderr does not carry the fixed sentence:\n%s", stderr)
	}

	// Nothing on disk: no file, no dir.
	if _, err := os.Stat(bkPreMigDir(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("--no-backup left the pre-migration dir on disk: %v", err)
	}

	// The row: the skipped one, with the door's actor.
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the ledger: %v", err)
	}
	runs, err := st.ListBackupRuns(store.BackupRunFilter{})
	_ = st.Close()
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d rows, want exactly the skipped one: %+v", len(runs), runs)
	}
	r := runs[0]
	if r.Kind != "backup" || r.Reason != "pre-migration" || r.ActorKind != "cli" ||
		r.Outcome != "skipped" || r.Detail != "skipped: operator" {
		t.Fatalf("the skipped row is wrong: %+v", r)
	}
	if r.FileName != nil || r.VerifyLevel != nil {
		t.Fatalf("the skipped row names a file or a verify level: %+v", r)
	}
}

// TestBKServeOverAVintageStoreTakesTheBackup is the daemon leg of behaviour
// 6: the daemon's own boot open is a migrating open too, and it carries the
// net with the same constructor (storeOpenOptions, actor "daemon"). The
// fixture's state dir holds a behind-vintage store; the daemon's start is
// what migrates it, and the copy is made before that migration.
func TestBKServeOverAVintageStoreTakesTheBackup(t *testing.T) {
	stateDir := t.TempDir()
	appliedBefore := bkSeedVintage(t, stateDir)
	all := store.Migrations()

	_ = startDaemonInStateDir(t, stateDir)

	// The socket answers before the boot open finishes (localrpc binds at
	// Init, not Serve), so the ping the fixture waits on is not the event
	// under test. Wait for the fact itself: the store is current. The
	// hook's ordering contract makes the copy precede that moment.
	deadline := time.Now().Add(60 * time.Second)
	for {
		done := false
		if st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), SkipMigrations: true}); err == nil {
			if applied, _, _, aerr := st.AppliedNamesAndPending(); aerr == nil && len(applied) == len(all) {
				done = true
			}
			_ = st.Close()
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not migrate the vintage store within 60s")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The copy: exactly one, in the pre-migration subdir.
	files := bkAeonbaks(t, bkPreMigDir(stateDir))
	if len(files) != 1 {
		t.Fatalf("the pre-migration dir holds %v, want exactly one backup", files)
	}

	// The ledger: the hook's one row, with the DAEMON's actor — the store
	// the daemon holds is the one that was found.
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the ledger: %v", err)
	}
	runs, err := st.ListBackupRuns(store.BackupRunFilter{})
	_ = st.Close()
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d rows, want exactly the hook's one: %+v", len(runs), runs)
	}
	r := runs[0]
	if r.Kind != "backup" || r.Reason != "pre-migration" || r.ActorKind != "daemon" || r.Outcome != "ok" {
		t.Fatalf("the row's identity is wrong: %+v", r)
	}
	if r.VerifyLevel == nil || *r.VerifyLevel != "verified" {
		t.Fatalf("the row claims verify level %v, want verified (BK6)", r.VerifyLevel)
	}
	if r.FileName == nil || *r.FileName != files[0] {
		t.Fatalf("the row names %v, want the file on disk %v", r.FileName, files)
	}
	if r.AppliedMigrations == nil || int(*r.AppliedMigrations) != appliedBefore {
		t.Fatalf("the row records %v applied, want the %d the store had at the copy's moment", r.AppliedMigrations, appliedBefore)
	}
}
