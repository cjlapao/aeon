package backup

// S62 T5 · the pre-migration safety net, end to end.
//
// These tests run the REAL hook (NewPreMigrate) against REAL stores seeded at
// an older vintage, and open them through the real store.Open seam — the
// way every production door does. The store-level pin of the seam itself
// (hook before Migrate, deferred row after) lives at the store level
// (internal/store, TestOpenMigratesAnOldStoreOnlyAfterACopy); this file is
// what the hook does when it is the real one.
//
// The behaviours pinned, per the task spec:
//
//  1. the copy precedes the first migration (a spy at the hook's moment);
//  2. no work when the store is current or fresh — zero cost, zero rows;
//  3. a failed run refuses the open, and the store stays byte-identical;
//  4. --no-backup is a FLAG and the only switch — no environment variable
//     takes the safety net off;
//  5. (cmd/aeon level) both direct callers are guarded — the parser walk
//     and the e2e live in cmd/aeon (bk_hook_guard_test.go);
//  6. the prune runs only after a successful migration;
//  7. a failed migration leaves the verify-level-2 undo in place;
//  8. (folded into 7) and an in-memory store never takes the hook.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	"github.com/cjlapao/go-backend-scaffolding/framework/database/migrations"
)

// seedOldStore opens a store at dir at the vintage of every migration but
// the last — the way a store from yesterday's binary sits on disk — and
// closes it. It returns the applied count, which the tests assert against
// the manifest, never a literal.
func seedOldStore(t *testing.T, dir string) int {
	t.Helper()
	st, err := store.Open(store.Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
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
		t.Fatalf("close after seeding: %v", err)
	}
	return len(all) - 1
}

// createTableRE reads the first plain CREATE TABLE out of a migration's SQL.
var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?([A-Za-z_][A-Za-z0-9_]*)`)

// pendingTable derives, from the pending (last) migration's own SQL, the
// table that migration creates — the one the vintage store does not have
// yet. The vintage tests probe and plant against it instead of against a
// migration they remember: S57's merge_watch landed as the 77th migration
// on 2026-11-02, moved the pending one off backup_run, and broke every
// test that had hardcoded the name (S62 T7 merge follow-up).
func pendingTable(t *testing.T) string {
	t.Helper()
	all := store.Migrations()
	loc := createTableRE.FindStringSubmatch(all[len(all)-1].SQL)
	if loc == nil {
		t.Fatalf("the last migration %q has no plain CREATE TABLE; the vintage tests cannot derive the table it creates", all[len(all)-1].Name)
	}
	return loc[1]
}

// testDeps is the hook's injection set over a live store, with the backup
// dir pointed at bkDir through a fake environment.
func testDeps(t *testing.T, st *store.Store, bkDir string) Deps {
	t.Helper()
	return Deps{
		Store:     st,
		CheckFile: store.CheckFile,
		Clock:     time.Now,
		Settings:  NewEnvSettingsWith(fakeEnv(map[string]string{"AEON_BACKUP_DIR": bkDir})),
		Version:   "0.0.0-test",
		OutsideDB: DefaultOutsideDB,
	}
}

// fakeEnv merges a fixed map over the real environment: the map wins, the
// real env answers the rest, and a key in neither answers empty.
func fakeEnv(fixed map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := fixed[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
}

// openMigrating is a store.Open with the real hook — the call every
// production door makes.
func openMigrating(t *testing.T, stateDir string, deps Deps, opts PreMigrateOptions) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{
		Dir:        stateDir,
		PreMigrate: NewPreMigrate(deps, opts),
	})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	return st
}

// fileHash is the byte-identity witness for the refusal tests.
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ledgerRows reads every backup_run row from a store.
func ledgerRows(t *testing.T, st *store.Store) []store.BackupRun {
	t.Helper()
	runs, err := st.ListBackupRuns(store.BackupRunFilter{})
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	return runs
}

// premigFiles lists the .aeonbak files in the pre-migration dir. A path
// that exists as a FILE (the refusal fixture) lists as empty: there is
// nothing to list.
func premigFiles(t *testing.T, bkDir string) []string {
	t.Helper()
	return premigFilesIn(t, filepath.Join(bkDir, "pre-migration"))
}

// TestPreMigrateCopyPrecedesFirstMigration is behaviour 1: at the hook's
// moment the pending set is exactly the last migration, and the table that
// migration creates does not exist yet. After the open: the file is in the
// pre-migration dir, it verifies at level 2, and the ledger row the hook
// owed carries the pre-migration facts.
func TestPreMigrateCopyPrecedesFirstMigration(t *testing.T) {
	stateDir := t.TempDir()
	bkDir := t.TempDir()
	appliedBefore := seedOldStore(t, stateDir)
	all := store.Migrations()
	tbl := pendingTable(t)

	// The spy wraps the real hook and records what the moment looked like.
	var (
		pendingAtHook []string
		tableAtHook   = -1
		hookCalls     int
	)
	deps := testDeps(t, nil, bkDir)
	inner := NewPreMigrate(deps, PreMigrateOptions{ActorKind: "cli"})
	st, err := store.Open(store.Options{
		Dir: stateDir,
		PreMigrate: func(ctx context.Context, s *store.Store, pending []store.Migration) (store.PreMigrateResult, error) {
			hookCalls++
			for _, m := range pending {
				pendingAtHook = append(pendingAtHook, m.Name)
			}
			var n int
			if err := s.GormDB().Raw(`SELECT count(*) FROM sqlite_master WHERE name = '` + tbl + `'`).Scan(&n).Error; err != nil {
				return store.PreMigrateResult{}, err
			}
			tableAtHook = n
			return inner(ctx, s, pending)
		},
	})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	defer st.Close()

	if hookCalls != 1 {
		t.Fatalf("the hook ran %d times, want exactly once", hookCalls)
	}
	if len(pendingAtHook) != 1 || pendingAtHook[0] != all[len(all)-1].Name {
		t.Fatalf("the hook saw pending %v, want exactly the last migration %q", pendingAtHook, all[len(all)-1].Name)
	}
	if tableAtHook != 0 {
		t.Fatalf("%s existed at the hook's moment (n=%d); the copy must precede the migration that creates it", tbl, tableAtHook)
	}

	// The file: one, in the pre-migration dir, and it verifies at level 2.
	files := premigFiles(t, bkDir)
	if len(files) != 1 {
		t.Fatalf("the pre-migration dir holds %v, want exactly one backup", files)
	}
	vr, err := Verify(context.Background(), deps, filepath.Join(bkDir, "pre-migration", files[0]), 2)
	if err != nil || !vr.OK {
		t.Fatalf("check the copy: %v (sentence: %s)", err, vr.Sentence)
	}
	if vr.FileReport == nil {
		t.Fatal("the level-2 verify carried no file report")
	}
	if !vr.FileReport.IntegrityOK {
		t.Fatalf("the copy does not pass integrity_check: %v", vr.FileReport.IntegrityDetail)
	}
	if vr.FileReport.JournalMode != "delete" {
		t.Fatalf("the copy's journal mode is %q, want delete — the one-file proof (BK3)", vr.FileReport.JournalMode)
	}

	// The manifest records the vintage it copied, not the one it is about
	// to become.
	if len(vr.FileReport.Applied) != appliedBefore {
		t.Fatalf("the copy's ledger holds %d migrations, want the %d it copied", len(vr.FileReport.Applied), appliedBefore)
	}

	// The row: deferred to after the migration, with the pre-migration facts.
	runs := ledgerRows(t, st)
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d rows, want exactly the hook's one: %+v", len(runs), runs)
	}
	r := runs[0]
	if r.Kind != "backup" || r.Reason != "pre-migration" || r.ActorKind != "cli" || r.Outcome != "ok" {
		t.Fatalf("the row's identity is wrong: %+v", r)
	}
	if r.VerifyLevel == nil || *r.VerifyLevel != "verified" {
		t.Fatalf("the row claims verify level %v, want verified — the undo must be verified (BK6)", r.VerifyLevel)
	}
	if r.FileName == nil || *r.FileName != files[0] {
		t.Fatalf("the row names %v, want the file the hook made %v", r.FileName, files)
	}
	if r.AppliedMigrations == nil || int(*r.AppliedMigrations) != appliedBefore {
		t.Fatalf("the row records %v applied, want the %d the store had at the copy's moment", r.AppliedMigrations, appliedBefore)
	}
}

// TestPreMigrateNoWorkWhenCurrentOrFresh is behaviour 2: the safety net's
// cost on a store that has nothing to lose is zero — no hook call, no file,
// no row, no directory.
func TestPreMigrateNoWorkWhenCurrentOrFresh(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		// A fully migrated store, the way a today's store sits on disk.
		if _, err := store.Open(store.Options{Dir: stateDir}); err != nil {
			t.Fatalf("open the current store: %v", err)
		}
		var calls int
		st, err := store.Open(store.Options{
			Dir: stateDir,
			PreMigrate: func(ctx context.Context, s *store.Store, pending []store.Migration) (store.PreMigrateResult, error) {
				calls++
				return store.PreMigrateResult{}, nil
			},
		})
		if err != nil {
			t.Fatalf("open the current store with a hook: %v", err)
		}
		defer st.Close()
		if calls != 0 {
			t.Fatalf("the hook ran %d times on a current store, want zero", calls)
		}
		if err := os.MkdirAll(bkDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := dirEntries(t, bkDir); len(got) != 0 {
			t.Fatalf("a current open left %v in the backup dir, want nothing", got)
		}
	})
	t.Run("fresh", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		var calls int
		st, err := store.Open(store.Options{
			Dir: stateDir,
			PreMigrate: func(ctx context.Context, s *store.Store, pending []store.Migration) (store.PreMigrateResult, error) {
				calls++
				return store.PreMigrateResult{}, nil
			},
		})
		if err != nil {
			t.Fatalf("open the fresh store with a hook: %v", err)
		}
		defer st.Close()
		if calls != 0 {
			t.Fatalf("the hook ran %d times on a fresh store, want zero — an empty store has nothing to copy", calls)
		}
		if _, err := os.Stat(filepath.Join(bkDir, "pre-migration")); !os.IsNotExist(err) {
			t.Fatalf("a fresh open created the pre-migration dir, want nothing on disk")
		}
	})
}

// dirEntries lists a directory's names, or fails if the directory is absent.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func lenOrZero(s []string) int {
	if s == nil {
		return 0
	}
	return len(s)
}

// TestPreMigrateNoBackupIsTheOnlySwitch is behaviour 4: the flag skips the
// file and records skipped:operator with the sentence on the door's output;
// and no environment variable — plausible or otherwise — takes the safety
// net off.
func TestPreMigrateNoBackupIsTheOnlySwitch(t *testing.T) {
	t.Run("the flag", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		var out bytes.Buffer
		st := openMigrating(t, stateDir, testDeps(t, nil, bkDir), PreMigrateOptions{
			NoBackup:  true,
			ActorKind: "cli",
			Out:       &out,
		})
		defer st.Close()

		if got := out.String(); got != noBackupSentence+"\n" {
			t.Fatalf("the door printed %q, want the fixed sentence %q", got, noBackupSentence)
		}
		if _, err := os.Stat(filepath.Join(bkDir, "pre-migration")); !os.IsNotExist(err) {
			t.Fatal("--no-backup created the pre-migration dir; it must leave nothing on disk")
		}
		runs := ledgerRows(t, st)
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
		if r.FreeSpaceState != "unknown" {
			t.Fatalf("the skipped row claims free-space state %q, want unknown — no check ran", r.FreeSpaceState)
		}
	})
	t.Run("no env opt-out", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)
		// Every plausible name for "turn the backup off", set at once.
		// None of them is a real key, and backup.enabled=false is a real
		// key that still must not suppress the pre-migration copy: that
		// key gates the schedule (T6), never the safety net.
		env := map[string]string{
			"AEON_BACKUP_DIR":      bkDir,
			"AEON_NO_BACKUP":       "1",
			"AEON_SKIP_BACKUP":     "1",
			"AEON_BACKUP_OFF":      "1",
			"AEON_BACKUP_DISABLED": "true",
			"AEON_BACKUP_SKIP":     "1",
			"AEON_BACKUP":          "off",
			"AEON_BACKUP_ENABLED":  "false",
		}
		deps := Deps{
			CheckFile: store.CheckFile,
			Clock:     time.Now,
			Settings:  NewEnvSettingsWith(fakeEnv(env)),
			Version:   "0.0.0-test",
			OutsideDB: DefaultOutsideDB,
		}
		st := openMigrating(t, stateDir, deps, PreMigrateOptions{ActorKind: "cli"})
		defer st.Close()

		files := premigFiles(t, bkDir)
		if len(files) != 1 {
			t.Fatalf("environment variables took the safety net off — the pre-migration dir holds %v, want the one backup. Only --no-backup may do that (BK18).", files)
		}
		runs := ledgerRows(t, st)
		if len(runs) != 1 || runs[0].Outcome != "ok" {
			t.Fatalf("the env-laden open did not record an ok backup: %+v", runs)
		}
	})
}

// plantPreMigrations makes n pre-migration backups in the dir with real
// Run calls at fake past times, so the dir holds a family the prune must
// judge. The store is at the old vintage — which, since S57's 77th
// migration, already carries the ledger table — so the runs' own rows LAND
// and outlive any later prune (P3: history outlives files); the files are
// what the prune judges, and the subtests that count rows take the planted
// ones into account.
func plantPreMigrations(t *testing.T, st *store.Store, bkDir string, n int) {
	t.Helper()
	premigDir := filepath.Join(bkDir, "pre-migration")
	base := time.Now().UTC().Add(-4 * time.Hour)
	one := true
	for i := 0; i < n; i++ {
		stamp := base.Add(time.Duration(i) * time.Hour)
		d := testDeps(t, st, bkDir)
		d.Clock = func() time.Time { return stamp }
		if _, err := Run(context.Background(), d, Options{
			Dir:         premigDir,
			Reason:      "pre-migration",
			ActorKind:   "cli",
			VerifyAfter: &one,
		}); err != nil {
			t.Fatalf("plant backup %d: %v", i, err)
		}
	}
}

// TestPreMigratePruneOnlyAfterSuccess is behaviour 6: a successful migrating
// open prunes the pre-migration family to keepPreMigration; a refused open
// prunes nothing — a failed migration's backup is the undo, and the undo
// that gets pruned is not an undo.
func TestPreMigratePruneOnlyAfterSuccess(t *testing.T) {
	t.Run("success-prunes", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)

		// Three prior pre-migration backups, at past times.
		seed, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
		if err != nil {
			t.Fatalf("re-open for planting: %v", err)
		}
		plantPreMigrations(t, seed, bkDir, 3)
		if err := seed.Close(); err != nil {
			t.Fatal(err)
		}

		// The migrating open, with keepPreMigration=2: the new copy plus
		// the newest planted one survive; the two oldest are pruned.
		env := map[string]string{
			"AEON_BACKUP_DIR":               bkDir,
			"AEON_BACKUP_KEEP_PREMIGRATION": "2",
		}
		deps := testDeps(t, nil, bkDir)
		deps.Settings = NewEnvSettingsWith(fakeEnv(env))
		st := openMigrating(t, stateDir, deps, PreMigrateOptions{ActorKind: "daemon"})
		_ = st.Close()

		got := premigFiles(t, bkDir)
		if len(got) != 2 {
			t.Fatalf("after the successful open the dir holds %d files %v, want exactly 2 (keepPreMigration=2)", len(got), got)
		}
		// The file the just-finished open made must be among the kept. The
		// ledger holds the three planted rows PLUS the open's one: the
		// pruned files' rows stay (P3: history outlives files —
		// premigrate.go records no row and removes none), which the
		// pre-S57 world hid because the vintage store had no ledger table
		// and the planted rows were dropped instead.
		runs := ledgerRows(t, st2(t, stateDir))
		if len(runs) != 4 {
			t.Fatalf("the ledger holds %d rows, want the 3 planted plus the one the open made: %+v", len(runs), runs)
		}
		if runs[0].FileName == nil {
			t.Fatal("the open's row names no file; the kept set cannot be checked")
		}
		var kept bool
		for _, f := range got {
			if f == *runs[0].FileName {
				kept = true
			}
		}
		if !kept {
			t.Fatalf("the open's own backup %s was pruned by its own open; the newest pre-migration backup is never pruned", *runs[0].FileName)
		}
	})
	t.Run("failure-prunes-nothing", func(t *testing.T) {
		stateDir := t.TempDir()
		bkDir := t.TempDir()
		seedOldStore(t, stateDir)

		seed, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
		if err != nil {
			t.Fatalf("re-open for planting: %v", err)
		}
		plantPreMigrations(t, seed, bkDir, 3)
		if err := seed.Close(); err != nil {
			t.Fatal(err)
		}

		// The open that must fail: the pre-migration path stands as a file.
		if err := os.Rename(filepath.Join(bkDir, "pre-migration"), filepath.Join(bkDir, "pre-migration.bak")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bkDir, "pre-migration"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		deps := testDeps(t, nil, bkDir)
		deps.Settings = NewEnvSettingsWith(fakeEnv(map[string]string{
			"AEON_BACKUP_DIR":               bkDir,
			"AEON_BACKUP_KEEP_PREMIGRATION": "1",
		}))
		if _, err := store.Open(store.Options{
			Dir:        stateDir,
			PreMigrate: NewPreMigrate(deps, PreMigrateOptions{ActorKind: "daemon", Out: os.Stderr}),
		}); err == nil {
			t.Fatal("the open succeeded; it must refuse with the dir unwritable")
		}

		// All three planted backups stand: the prune is the AFTER part of
		// a SUCCESS, and this open was not a success.
		dir := filepath.Join(bkDir, "pre-migration.bak")
		got := premigFilesIn(t, dir)
		if len(got) != 3 {
			t.Fatalf("a refused open pruned backups: %d stand, want all 3 — a failed migration's copies are the undo (BK6)", len(got))
		}
	})
}

// st2 re-opens a store read-only for a ledger check.
func st2(t *testing.T, stateDir string) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	return st
}

// premigFilesIn lists the .aeonbak files in an explicit directory; a path
// that is absent or is a file lists as empty.
func premigFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
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

// TestPreMigrateFailedMigrationKeepsUndo is behaviour 7: the pending
// migration is refused (the table it creates already exists), so the open
// fails with the migration's own sentence — and the pre-migration backup
// stands, verified, as the undo. No prune, and the open adds no ledger row.
func TestPreMigrateFailedMigrationKeepsUndo(t *testing.T) {
	stateDir := t.TempDir()
	bkDir := t.TempDir()
	seedOldStore(t, stateDir)

	// Pre-create the table the PENDING (last) migration creates: its plain
	// CREATE TABLE then fails, and the runner rolls back to the previous
	// version with the ledger row marked failed. The table is derived from
	// the migration's own SQL — the pending one moves as slices add
	// migrations (S57's merge_watch moved it off backup_run, 2026-11-02).
	seed, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the fixture: %v", err)
	}
	tbl := pendingTable(t)
	plantPreMigrations(t, seed, bkDir, 3)
	if err := seed.GormDB().Exec(`CREATE TABLE ` + tbl + ` (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("plant the colliding table %s: %v", tbl, err)
	}
	var before int
	if err := seed.GormDB().Raw(`SELECT count(*) FROM backup_run`).Scan(&before).Error; err != nil {
		t.Fatalf("count the planted rows: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	deps := testDeps(t, nil, bkDir)
	deps.Settings = NewEnvSettingsWith(fakeEnv(map[string]string{
		"AEON_BACKUP_DIR":               bkDir,
		"AEON_BACKUP_KEEP_PREMIGRATION": "1",
	}))
	_, oerr := store.Open(store.Options{
		Dir:        stateDir,
		PreMigrate: NewPreMigrate(deps, PreMigrateOptions{ActorKind: "daemon", Out: os.Stderr}),
	})
	if oerr == nil {
		t.Fatal("the open succeeded; the colliding migration must fail it")
	}
	if !strings.Contains(oerr.Error(), "schema is at its previous version") {
		t.Fatalf("the failure is not the migration's refusal (D5):\n%s", oerr.Error())
	}

	// The undo: four files stand (the three planted plus the hook's), the
	// hook's verifies at level 2, and the open added no ledger row — the
	// row is owed to a SUCCESS, and this open was not one. (The three
	// planted rows stand; they were written by the planting, before the
	// open the test is about.)
	files := premigFiles(t, bkDir)
	if len(files) != 4 {
		t.Fatalf("after the failed migration the dir holds %d files %v, want all 4 — a failed migration prunes nothing", len(files), files)
	}
	var newest string
	for _, f := range files {
		if !strings.HasPrefix(f, "aeon-state-") {
			continue
		}
		if newest == "" || f > newest {
			newest = f
		}
	}
	if newest == "" {
		t.Fatalf("no backup file among %v", files)
	}
	vr, err := Verify(context.Background(), deps, filepath.Join(bkDir, "pre-migration", newest), 2)
	if err != nil || !vr.OK {
		t.Fatalf("check the undo: %v (sentence: %s) — it is not an undo", err, vr.Sentence)
	}

	ro, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the ledger: %v", err)
	}
	var n int
	if err := ro.GormDB().Raw(`SELECT count(*) FROM backup_run`).Scan(&n).Error; err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	_ = ro.Close()
	if n != before {
		t.Fatalf("the ledger holds %d rows, want the %d it stood at before the refused open — a refused open adds no row", n, before)
	}
}

// TestPreMigrateInMemoryStoreTakesNoHook is the in-memory leg of behaviour
// 3: a DSN with no file has nothing to copy, so the hook never runs — a row
// naming a non-existent file is a lie the ledger must not hold.
func TestPreMigrateInMemoryStoreTakesNoHook(t *testing.T) {
	dsn := "file:s62pm?mode=memory&cache=shared"
	t.Setenv("DATABASE_SQLITE_DSN", dsn)

	// The first open seeds the vintage in the shared memory database and
	// stays open, so the second open finds pending work.
	seed, err := store.Open(store.Options{SkipMigrations: true})
	if err != nil {
		t.Fatalf("open the in-memory store for seeding: %v", err)
	}
	defer seed.Close()
	all := store.Migrations()
	svc := migrations.NewMigrationService(seed.GormDB())
	svc.SetMaxRetries(1)
	for _, m := range all[:len(all)-1] {
		svc.Register(migrations.NewSQLMigrationWorkerFromContent(seed.GormDB(), m.Name, m.Description, m.ID, m.SQL, ""))
	}
	if diag := svc.RunAll(appctx.NewContext(context.Background())); diag != nil && diag.HasErrors() {
		t.Fatalf("seed the in-memory vintage: %s", diag.GetSummary())
	}

	var calls int
	st, err := store.Open(store.Options{
		PreMigrate: func(ctx context.Context, s *store.Store, pending []store.Migration) (store.PreMigrateResult, error) {
			calls++
			return store.PreMigrateResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("open the in-memory store with a hook: %v", err)
	}
	defer st.Close()
	if calls != 0 {
		t.Fatalf("the hook ran %d times on an in-memory store, want zero — there is no file to copy", calls)
	}
	// And the open still migrated: the skip is the hook's, not the
	// migration's.
	applied, err := st.AppliedMigrations()
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != len(all) {
		t.Fatalf("the in-memory open applied %d migrations, want all %d", len(applied), len(all))
	}
}

// ---------------------------------------------------------------------------
// The race: two processes, one store with pending migrations.
//
// The child is this same test binary, re-executed with S62_PM_CHILD set:
// init() hands it off before any test runs. Role A opens straight away.
// Role B waits for A's lock file to appear — the moment the single-flight
// is live — and then opens: it must be refused by the live holder, or, if
// A's migration committed in the gap, it is the late reader that opens
// cleanly with no pending work. Both outcomes are honest; a deadlock or a
// second backup is not.

func init() {
	if role := os.Getenv("S62_PM_CHILD"); role != "" {
		runPreMigrateChild(role)
	}
}

func runPreMigrateChild(role string) {
	storeDir := os.Getenv("S62_PM_STORE")
	bkDir := os.Getenv("S62_PM_BK")
	if role == "b" {
		deadline := time.Now().Add(120 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(bkDir, "pre-migration", ".backup.lock")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "CHILD-TIMEOUT: the lock never appeared")
				os.Exit(42)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	deps := Deps{
		CheckFile: store.CheckFile,
		Clock:     time.Now,
		Settings:  NewEnvSettings(),
		Version:   "0.0.0-test",
		OutsideDB: DefaultOutsideDB,
	}
	st, err := store.Open(store.Options{
		Dir:        storeDir,
		PreMigrate: NewPreMigrate(deps, PreMigrateOptions{ActorKind: "cli", Out: os.Stderr}),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "OPEN-ERR: %v\n", err)
		os.Exit(1)
	}
	_ = st.Close()
	os.Exit(0)
}

// TestPreMigrateTwoOpenersRace is behaviour 9. The winner's backup is sized
// to hold the lock long enough for the loser to reach the single-flight:
// a 300 MB store's snapshot plus integrity check outlasts the loser's open
// and probe by a wide margin.
func TestPreMigrateTwoOpenersRace(t *testing.T) {
	if os.Getenv("S62_PM_CHILD") != "" {
		t.Skip("we are the child")
	}
	stateDir := t.TempDir()
	bkDir := t.TempDir()
	premigDir := filepath.Join(bkDir, "pre-migration")

	// Seed the vintage, then bloat it so the winner holds the lock for
	// long enough that the loser meets the live holder, not its ghost.
	seedOldStore(t, stateDir)
	bloat, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("re-open for the bloat: %v", err)
	}
	if err := bloat.GormDB().Exec(`CREATE TABLE pm_bulk (id INTEGER PRIMARY KEY, blob BLOB)`).Error; err != nil {
		t.Fatalf("create the bloat table: %v", err)
	}
	// 3000 rows of 100 KiB: 300 MB, one statement — big enough that the
	// winner's snapshot plus integrity check outlasts the loser's open and
	// probe, so the loser meets the LIVE holder, not its ghost.
	if err := bloat.GormDB().Exec(`
		WITH RECURSIVE cnt(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM cnt WHERE x < 3000)
		INSERT INTO pm_bulk (blob) SELECT zeroblob(100000) FROM cnt`).Error; err != nil {
		t.Fatalf("bloat the store: %v", err)
	}
	if err := bloat.Close(); err != nil {
		t.Fatal(err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("re-exec path: %v", err)
	}
	mkChild := func(role string) *exec.Cmd {
		cmd := exec.Command(self)
		cmd.Env = append(os.Environ(),
			"S62_PM_CHILD="+role,
			"S62_PM_STORE="+stateDir,
			"S62_PM_BK="+bkDir,
			"AEON_BACKUP_DIR="+bkDir,
		)
		return cmd
	}
	runChild := func(role string) childResult {
		cmd := mkChild(role)
		var errb bytes.Buffer
		cmd.Stderr = &errb
		err := cmd.Run()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("run the %s child: %v", role, err)
			}
		}
		return childResult{code: code, stderr: errb.String()}
	}

	doneA := make(chan childResult, 1)
	doneB := make(chan childResult, 1)
	go func() { doneA <- runChild("a") }()
	go func() { doneB <- runChild("b") }()

	select {
	case rA := <-doneA:
		select {
		case rB := <-doneB:
			assertRaceOutcome(t, rA, rB, premigDir)
		case <-time.After(180 * time.Second):
			t.Fatal("child b did not finish within 180s; the two openers deadlocked")
		}
	case <-time.After(180 * time.Second):
		t.Fatal("child a did not finish within 180s; the two openers deadlocked")
	}
}

// childResult is one re-exec'd opener's exit: its code and stderr.
type childResult struct {
	code   int
	stderr string
}

func assertRaceOutcome(t *testing.T, rA, rB childResult, premigDir string) {
	t.Helper()
	if rA.code != 0 {
		t.Fatalf("the first opener failed (code %d):\n%s", rA.code, rA.stderr)
	}
	// The loser: refused by the live holder (the expected leg), or a late
	// reader that found no pending work (the gap leg). Both are honest;
	// anything else is a bug.
	if rB.code != 0 && rB.code != 1 {
		t.Fatalf("the second opener ended with code %d:\n%s", rB.code, rB.stderr)
	}
	if rB.code == 1 && !strings.Contains(rB.stderr, "holds the backup lock") {
		t.Fatalf("the second opener failed without naming the held lock:\n%s", rB.stderr)
	}
	if rB.code == 1 {
		t.Log("loser leg: refused by the live holder (the expected leg)")
	} else {
		t.Log("loser leg: late reader — the winner had already migrated before the loser probed")
	}

	// One backup, exactly: the single-flight let one of the two through.
	files := premigFilesIn(t, premigDir)
	if len(files) != 1 {
		t.Fatalf("the race left %d backups %v, want exactly one — the single-flight is the whole point (BK4's twin)", len(files), files)
	}
	// No partial, no lock: the winner published and released.
	for _, e := range dirEntries(t, premigDir) {
		if strings.Contains(e, ".partial") || e == ".backup.lock" {
			t.Fatalf("the race left %q in the pre-migration dir; the winner must publish and release", e)
		}
	}
}
