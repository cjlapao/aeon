package store

// S62 T1 · BK characterization goldens at the STORE level.
//
// These pin what `Open` does and leaves today, on the untouched code, so the
// slice can prove what it changed and what it did not. Every header names the
// later task expected to change it and how:
//
//   - the file-listing goldens are the baseline for "the snapshot/backup never
//     leaves sidecars" (T2/T4/T7); T5's pre-migration backup keeps that
//     baseline: its copy lands in the backup dir, never beside the store;
//   - `TestInfoJSONShapeBeforeBackup` is the pin T9's added `Info` fields
//     change;
//   - `TestSkipMigrationsOpensWithoutApplying` pins the open mode T7's direct
//     restore door will rely on.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// dirNames lists a directory's entry names, sorted for a stable comparison.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestOpenLeavesExactlyTheStateFiles pins the state directory after a clean
// Open+Close on a FRESH directory.
//
// T2/T4/T7 will change the world around this, not this list: a snapshot, an
// export and a pre-restore directory all live OUTSIDE the state dir (the
// backup directory is configured separately), and the snapshot is a copy, not
// a sidecar. So the golden stands unless a task starts leaving -wal/-shm or
// any other file here — which is exactly what the slice must not do. After a
// clean close SQLite checkpoints the WAL and removes both sidecars, so the
// directory holds exactly one file.
func TestOpenLeavesExactlyTheStateFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	want := []string{"state.db"}
	got := dirNames(t, dir)
	if len(got) != len(want) {
		t.Fatalf("after Open+Close the state dir holds %v, want exactly %v — the snapshot/backup tasks (T2/T4/T7) must not change this", got, want)
	}
	if got[0] != want[0] {
		t.Fatalf("after Open+Close the state dir holds %v, want exactly %v", got, want)
	}
}

// TestOpenMigratesAnOldStoreOnlyAfterACopy supersedes T1's
// TestOpenMigratesAnOldStoreWithoutAnyCopy, which pinned the gap: a store at
// an older schema gained its pending migrations on Open with nothing in
// front of it. S62 T5 closed that gap with the PreMigrate hook, and this is
// the store-level pin of what the hook changes and what it must not.
//
// What it pins:
//
//   - the hook runs BEFORE Migrate: at the hook's moment the pending set is
//     exactly the migrations from the backup_run file onward (its first is
//     the one that creates the table), and the backup_run table does not
//     exist yet. The copy precedes the first pending migration's first byte;
//   - the hook's file appears in the BACKUP directory, not the state
//     directory: the state dir still holds exactly state.db after the
//     migrating open, so the snapshot/backup never leaves a sidecar next to
//     the store it copied;
//   - the hook's RunRecord is written by Open AFTER the migration: the row
//     exists after the open, with exactly the facts the hook returned —
//     deferred because its table did not exist at the hook's moment;
//   - a hook-less open is still a no-op open: the gate is a nil check, and
//     today's every other caller pays nothing (covered beside this test at
//     the backup level).
func TestOpenMigratesAnOldStoreOnlyAfterACopy(t *testing.T) {
	dir := t.TempDir()

	// A store at the vintage before backup_run: every migration before the
	// backup_run file. S57 T7 (2026-11-02): by name, not "the last" —
	// merge_watch landed after backup_run, and the positional shortcut
	// dropped the wrong file.
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	all := Migrations()
	idx := backupRunMigrationIndex()
	if idx < 1 {
		t.Fatal("the migration set is too small for a vintage test")
	}
	applyMigrations(t, old.GormDB(), all[:idx])
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	// The backup directory, where the copy is owed — beside the state dir,
	// the way the production layout keeps the two apart.
	bkDir := t.TempDir()
	marker := filepath.Join(bkDir, "pre-migration", "hook-marker")

	var (
		hookCalls    int
		pendingNames []string
		tableAtHook  = -1
	)
	appliedBefore := int64(idx)
	s, err := Open(Options{
		Dir: dir,
		PreMigrate: func(ctx context.Context, s *Store, pending []Migration) (PreMigrateResult, error) {
			hookCalls++
			for _, m := range pending {
				pendingNames = append(pendingNames, m.Name)
			}
			// At the hook's moment: does the table the pending migration
			// creates exist yet? Zero is the contract — the copy precedes
			// the first pending migration's first byte.
			var n int
			if err := s.GormDB().Raw(`SELECT count(*) FROM sqlite_master WHERE name = 'backup_run'`).Scan(&n).Error; err != nil {
				return PreMigrateResult{}, fmt.Errorf("probe the table at the hook's moment: %w", err)
			}
			tableAtHook = n
			if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
				return PreMigrateResult{}, err
			}
			if err := os.WriteFile(marker, []byte("copy made\n"), 0o600); err != nil {
				return PreMigrateResult{}, err
			}
			return PreMigrateResult{RunRecord: &NewBackupRun{
				Kind:              "backup",
				Reason:            "pre-migration",
				ActorKind:         "daemon",
				StartedAt:         s.Now(),
				Outcome:           "ok",
				Detail:            "snapshot verified and published",
				FileName:          "aeon-state-20261102T120000Z-v999.aeonbak",
				AppliedMigrations: &appliedBefore,
				VerifyLevel:       "verified",
				FreeSpaceState:    "ok",
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}

	// The open happened: every migration applied, exactly one hook call,
	// the pending set at the hook's moment was exactly the migrations from
	// backup_run onward (the first of which creates the table), and the
	// table was ABSENT at that moment.
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != len(all) {
		t.Fatalf("after the migrating open %d migrations are applied, want all %d", len(applied), len(all))
	}
	if hookCalls != 1 {
		t.Fatalf("the hook ran %d times, want exactly once", hookCalls)
	}
	wantPending := len(all) - idx
	if len(pendingNames) != wantPending || pendingNames[0] != all[idx].Name {
		t.Fatalf("the hook saw pending %v, want the %d migrations from %q onward (the one that creates backup_run first)", pendingNames, wantPending, all[idx].Name)
	}
	if tableAtHook != 0 {
		t.Fatalf("backup_run existed at the hook's moment (n=%d); the copy must precede the migration that creates it", tableAtHook)
	}

	// The copy is in the backup directory; the state dir is unchanged.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the hook's copy is missing from the backup dir: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := dirNames(t, dir); len(got) != 1 || got[0] != "state.db" {
		t.Fatalf("the migrating open changed the state dir: %v, want exactly [state.db] — the copy lives in the backup dir, never beside the store", got)
	}

	// The deferred row: written by Open after Migrate, with the hook's facts.
	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("re-open for the row: %v", err)
	}
	defer s2.Close()
	runs, err := s2.ListBackupRuns(BackupRunFilter{})
	if err != nil {
		t.Fatalf("list the rows: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d rows, want exactly the hook's one: %+v", len(runs), runs)
	}
	r := runs[0]
	if r.Kind != "backup" || r.Reason != "pre-migration" || r.ActorKind != "daemon" ||
		r.Outcome != "ok" || r.FileName == nil || *r.FileName != "aeon-state-20261102T120000Z-v999.aeonbak" ||
		r.VerifyLevel == nil || *r.VerifyLevel != "verified" ||
		r.AppliedMigrations == nil || *r.AppliedMigrations != appliedBefore {
		t.Fatalf("the deferred row did not carry the hook's facts: %+v", r)
	}
}

// TestOpenOnAFreshDirPendingEqualsAll pins the fresh-store truth: right after
// an open there is nothing pending, and the ledger holds every migration —
// the count is taken from Migrations(), never a literal, so a new migration
// file cannot break this test.
func TestOpenOnAFreshDirPendingEqualsAll(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if want := len(Migrations()); len(applied) != want {
		t.Fatalf("a fresh open applied %d migrations, want all %d", len(applied), want)
	}
	for _, a := range applied {
		if a.Status != "applied" {
			t.Fatalf("fresh open left migration %s with status %q", a.Name, a.Status)
		}
	}
	pending, err := s.Pending()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("a fresh open reports %d pending migrations, want none: %v", len(pending), pending)
	}
}

// TestSkipMigrationsOpensWithoutApplying pins the open mode T7's direct
// restore door will rely on (the spec's `SkipMigrations:true` door): the store
// opens on a schema it does not migrate, and the ledger is ABSENT — not empty.
// Absent matters: it is the F4-style distinction between "a fresh shape" and
// "a store whose migrations failed", and the restore door must not be able to
// confuse the two by writing a ledger row of its own.
func TestSkipMigrationsOpensWithoutApplying(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), SkipMigrations: true})
	if err != nil {
		t.Fatalf("open with SkipMigrations: %v", err)
	}
	defer s.Close()

	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = '_migrations'`).Scan(&n); err != nil {
		t.Fatalf("probe the ledger: %v", err)
	}
	if n != 0 {
		t.Fatalf("SkipMigrations left a _migrations table behind (n=%d); the direct door must open without touching the ledger", n)
	}
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'agent'`).Scan(&n); err != nil {
		t.Fatalf("probe the schema: %v", err)
	}
	if n != 0 {
		t.Fatal("SkipMigrations applied schema DDL; it must open without applying anything")
	}
	// The applied read is an ERROR, not an empty slice: there is no ledger to
	// read from, and a caller that treated that as "zero migrations" would
	// report a store as fresh.
	if _, err := s.AppliedMigrations(); err == nil {
		t.Fatal("AppliedMigrations on a SkipMigrations open should report the missing ledger, and did not")
	}
}

// TestInfoJSONShapeBeforeBackup serialises the Info struct and pins its shape,
// so T9's added fields are VISIBLE when they land.
//
// T9 will change this test: it adds the backup state to `Info` (the last
// backup's name and age, or the honest "never"), which shows up here as new
// top-level keys and a new entry in wantInfoKeys. The per-migration and
// per-table content is deliberately NOT pinned — a new migration file from any
// slice must not force an edit of this golden; only the SHAPE is S62's
// contract.
func TestInfoJSONShapeBeforeBackup(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	in, err := s.Info()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	blob, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// The top-level keys, in marshalled order (Go marshals structs in field
	// order, so this is the declared field order).
	keys := jsonKeyOrder(t, blob)
	wantInfoKeys := []string{
		"Path", "Bytes", "WALBytes", "JournalMode", "ForeignKeys",
		"Applied", "Pending", "Tables",
		"VecWriterVersion", "VecReaderVersion",
		"ReadPoolSize", "WriterPoolSize", "BusyTimeoutMS",
	}
	if len(keys) != len(wantInfoKeys) {
		t.Fatalf("Info serialises with %d keys %v, want exactly %d (%v) — T9's added fields change this list on purpose", len(keys), keys, len(wantInfoKeys), wantInfoKeys)
	}
	for i := range wantInfoKeys {
		if keys[i] != wantInfoKeys[i] {
			t.Fatalf("Info key %d = %q, want %q (order is part of the shape)", i, keys[i], wantInfoKeys[i])
		}
	}

	// One applied entry: its own key shape.
	var shaped struct {
		Applied []map[string]any `json:"Applied"`
	}
	if err := json.Unmarshal(blob, &shaped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(shaped.Applied) != len(Migrations()) {
		t.Fatalf("Applied holds %d entries, want one per migration (%d)", len(shaped.Applied), len(Migrations()))
	}
	wantEntryKeys := map[string]bool{"Name": true, "Status": true, "AppliedAt": true}
	for i, e := range shaped.Applied {
		if len(e) != len(wantEntryKeys) {
			t.Fatalf("Applied[%d] has keys %v, want exactly %v", i, e, wantEntryKeys)
		}
		for k := range e {
			if !wantEntryKeys[k] {
				t.Fatalf("Applied[%d] has unexpected key %q", i, k)
			}
		}
	}

	// The values that are facts today, not measurements: the journal mode,
	// the pragma that took effect, the pool shape, the busy timeout.
	if in.JournalMode != "wal" {
		t.Fatalf("JournalMode = %q, want wal", in.JournalMode)
	}
	if !in.ForeignKeys {
		t.Fatal("ForeignKeys is off; the open is supposed to set it")
	}
	if in.ReadPoolSize != 4 || in.WriterPoolSize != 1 {
		t.Fatalf("pool shape = %d readers / %d writer, want 4 / 1", in.ReadPoolSize, in.WriterPoolSize)
	}
	if in.BusyTimeoutMS != 5000 {
		t.Fatalf("BusyTimeoutMS = %d, want 5000", in.BusyTimeoutMS)
	}
	if in.VecWriterVersion == "" || in.VecWriterVersion != in.VecReaderVersion {
		t.Fatalf("vec versions = %q / %q, want one non-empty version on both pools", in.VecWriterVersion, in.VecReaderVersion)
	}
}

// jsonKeyOrder returns the top-level object's keys in the order they appear in
// the marshalled bytes — a plain map would lose the order, and the order is
// what makes an added field visible at its position.
func jsonKeyOrder(t *testing.T, blob []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(blob))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatal("Info did not marshal to a JSON object")
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode key: %v", err)
		}
		k, ok := tok.(string)
		if !ok {
			t.Fatal("an object key was not a string")
		}
		keys = append(keys, k)
		// Skip the value of whatever shape it is.
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("decode value: %v", err)
		}
	}
	return keys
}
