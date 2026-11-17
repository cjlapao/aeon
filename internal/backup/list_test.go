package backup

// The list and the status machine: what the list shows and hides, the
// latest-verify join (a newer failure loses nothing and erases an older
// success), and the four-state status with its numbers.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// fakeRuns is the ledger seam for the list and status tests: a fixed row
// slice, filtered by kind the way the question asks.
type fakeRuns struct {
	rows []store.BackupRun
	err  error
}

func (f fakeRuns) ListBackupRuns(fl store.BackupRunFilter) ([]store.BackupRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []store.BackupRun
	for _, r := range f.rows {
		if fl.Kind == "" || r.Kind == fl.Kind {
			out = append(out, r)
		}
	}
	return out, nil
}

func sref(s string) *string { return &s }

func TestListIgnoresGarbageAndJoinsVerify(t *testing.T) {
	bdir := t.TempDir()
	applied := firstApplied(3)
	last := applied[len(applied)-1]

	// Two real archives at different times, plus every non-backup shape.
	mNew := baseManifest(3, last)
	mOld := baseManifest(3, last)
	mOld.CreatedAt = testNow.Add(-24 * time.Hour).Format(createdFormat)
	pathNew := writeArchive(t, bdir, FileName(testNow, 3), mNew, nil)
	pathOld := writeArchive(t, bdir, FileName(testNow.Add(-24*time.Hour), 3), mOld, nil)

	partial := pathNew + ".partial"
	if err := os.WriteFile(partial, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bdir, ".tmp-leftover"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bdir, lockFileName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bdir, "notes.txt"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A final-named file whose manifest will not parse: the list shows it,
	// with the refusal as its only fact.
	pathBroken := filepath.Join(bdir, FileName(testNow.Add(-48*time.Hour), 3))
	if err := os.WriteFile(pathBroken, []byte("not a tar at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The ledger: the newest file's newest verify row is an ok at level 2;
	// the older file's newest verify row is a FAILED one, over an older ok.
	newName, oldName := filepath.Base(pathNew), filepath.Base(pathOld)
	failed := "failed"
	ok, level2 := "ok", "verified"
	runs := fakeRuns{rows: []store.BackupRun{
		// newest first
		{Kind: "verify", Outcome: failed, Detail: "the integrity check failed", FileName: sref(oldName)},
		{Kind: "verify", Outcome: ok, VerifyLevel: sref(level2), FileName: sref(newName)},
		{Kind: "verify", Outcome: ok, VerifyLevel: sref(level2), FileName: sref(oldName)},
		{Kind: "backup", Outcome: ok, FileName: sref(newName)},
	}}

	views, err := List([]string{bdir}, runs)
	if err != nil {
		t.Fatalf("list the backup dir: %v", err)
	}
	byName := map[string]BackupView{}
	for _, v := range views {
		byName[v.FileName] = v
	}
	// Exactly the three final-named files: the two archives and the broken
	// one. The partial, the tmp dir, the lock and the foreign file are not
	// backups and are not shown.
	if len(views) != 3 {
		t.Fatalf("the list shows %d entries, the dir holds 3 backups and 4 pieces of not-backup: %v", len(views), viewNames(views))
	}
	if _, shown := byName[filepath.Base(partial)]; shown {
		t.Errorf("a .partial is not a backup and is shown as one")
	}
	for _, name := range []string{".tmp-leftover", lockFileName, "notes.txt"} {
		if _, shown := byName[name]; shown {
			t.Errorf("%s is not a backup and is shown as one", name)
		}
	}
	// Newest first.
	if views[0].FileName != newName || views[1].FileName != oldName {
		t.Errorf("the list is not newest first: %v", viewNames(views))
	}
	// The join: the newer file's newest verify is an ok at a restore level.
	if byName[newName].Verified != "verified" {
		t.Errorf("the newest file's joined verify is %q, its newest row is an ok at level 2", byName[newName].Verified)
	}
	// The older file's newest verify is a FAILURE over an older success:
	// the list shows what the newest attempt says, not the oldest hope.
	if byName[oldName].Verified != "unverified" {
		t.Errorf("the older file's joined verify is %q — a newer failure erases an older success", byName[oldName].Verified)
	}
	// The broken file: shown, with no manifest facts to join.
	broken := byName[filepath.Base(pathBroken)]
	if !broken.CreatedAt.IsZero() || broken.SizeBytes != 0 {
		t.Errorf("the unparseable file should carry no manifest facts: %+v", broken)
	}
	if broken.Verified != "unverified" {
		t.Errorf("the unparseable file's verify state is %q, a file that cannot be read is not verified", broken.Verified)
	}
	// A dir that was never made is an empty answer, not an error.
	if _, err := List([]string{filepath.Join(bdir, "never-made")}, nil); err != nil {
		t.Errorf("a missing backup dir: %v", err)
	}
	// nil ledger: every file answers unverified, and the list still works.
	views2, err := List([]string{bdir}, nil)
	if err != nil {
		t.Fatalf("list with a nil ledger: %v", err)
	}
	for _, v := range views2 {
		if v.Verified != "unverified" {
			t.Errorf("with no ledger the verify state is %q — no rows is unverified, a state", v.Verified)
		}
	}
}

// TestListFlagsNewerSchemas: a file a newer binary made is listed with the
// flag that says it, and nothing is refused because of it.
func TestListFlagsNewerSchemas(t *testing.T) {
	bdir := t.TempDir()
	applied := firstApplied(3)
	m := baseManifest(3, applied[len(applied)-1])
	m.LastMigration = "99999999999999_unknown_future"
	path := writeArchive(t, bdir, nameFor(t, m), m, nil)

	views, err := List([]string{bdir}, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("the list has %d entries, want one", len(views))
	}
	if views[0].CanRestoreWithThisBinary {
		t.Errorf("a newer-schema backup is flagged restorable by this binary")
	}
	if filepath.Base(path) != views[0].FileName {
		t.Errorf("the list named %q, the file is %s", views[0].FileName, filepath.Base(path))
	}
}

func viewNames(views []BackupView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.FileName)
	}
	return out
}

// TestStatusStateMachine: the four states, each with the numbers behind it.
func TestStatusStateMachine(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	stamp := func(d time.Duration) string { return now.Add(-d).UTC().Format(createdFormat) }

	mkDeps := func(t *testing.T) (Deps, string) {
		stateDir := t.TempDir()
		d := fakeDeps(newFakeStore(firstApplied(3)), defaultFakeReport(firstApplied(3)), stateDir)
		d.Clock = func() time.Time { return now }
		return d, DefaultDir(stateDir)
	}

	t.Run("never", func(t *testing.T) {
		d, _ := mkDeps(t)
		runs := fakeRuns{rows: []store.BackupRun{{Kind: "backup", Outcome: "failed"}}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if view.State != StateNever {
			t.Errorf("a failed backup with no success is %q, the machine says never", view.State)
		}
		if view.LastSuccessAt != "" || view.AgeSeconds != 0 {
			t.Errorf("a never machine carries last-success facts: %+v", view)
		}
	})

	t.Run("fresh", func(t *testing.T) {
		d, dir := mkDeps(t)
		runs := fakeRuns{rows: []store.BackupRun{
			{Kind: "backup", Reason: "scheduled", Outcome: "ok", FinishedAt: stamp(time.Hour)},
		}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if view.State != StateFresh {
			t.Errorf("a success an hour ago (stale after 48h) is %q, the machine says fresh", view.State)
		}
		if view.LastSuccessAt != stamp(time.Hour) || view.AgeSeconds != 3600 {
			t.Errorf("the numbers behind fresh: last=%s age=%d", view.LastSuccessAt, view.AgeSeconds)
		}
		// Last success an hour ago, the default interval 24h: the next due
		// instant is 23h from now.
		if wantDue := now.Add(23 * time.Hour).UTC().Format(createdFormat); view.NextDueAt != wantDue {
			t.Errorf("the enabled machine's next due is %q, last success + interval is %s", view.NextDueAt, wantDue)
		}
		if view.Settings[KeyInterval] != "24h0m0s,default" {
			t.Errorf("the settings map shows %q for the interval, the answer is value,source", view.Settings[KeyInterval])
		}
		if len(view.Settings) != len(AllKeys) {
			t.Errorf("the settings map has %d keys, the closed set has %d", len(view.Settings), len(AllKeys))
		}
		if !filepath.IsAbs(view.Dir) || view.Dir != dir {
			t.Errorf("the status dir is %q, the resolved dir is %q", view.Dir, dir)
		}
	})

	t.Run("stale", func(t *testing.T) {
		d, _ := mkDeps(t)
		runs := fakeRuns{rows: []store.BackupRun{
			{Kind: "backup", Reason: "scheduled", Outcome: "ok", FinishedAt: stamp(72 * time.Hour)},
		}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if view.State != StateStale {
			t.Errorf("a success 72h ago is %q, the machine says stale (48h)", view.State)
		}
	})

	t.Run("failing outranks stale", func(t *testing.T) {
		d, _ := mkDeps(t)
		runs := fakeRuns{rows: []store.BackupRun{
			{Kind: "backup", Reason: "scheduled", Outcome: "failed", FinishedAt: stamp(time.Hour)},
			{Kind: "backup", Reason: "scheduled", Outcome: "refused", FinishedAt: stamp(2 * time.Hour)},
			{Kind: "backup", Reason: "manual", Outcome: "ok", FinishedAt: stamp(72 * time.Hour)},
		}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if view.State != StateFailing {
			t.Errorf("two failed scheduled runs over a 72h-old success is %q — broken is told as broken, because the fix is different", view.State)
		}
	})

	t.Run("running says the lock is held, not a corpse", func(t *testing.T) {
		d, dir := mkDeps(t)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeHolder(t, dir, lockHolder{PID: os.Getpid(), Start: now.Format(createdFormat), Reason: "scheduled"})
		runs := fakeRuns{rows: []store.BackupRun{
			{Kind: "backup", Reason: "scheduled", Outcome: "ok", FinishedAt: stamp(time.Hour)},
		}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if !view.Running {
			t.Errorf("a fresh live-held lock: the status says not running — the machine is mid-run")
		}
		// A corpse's lock is not running: the next run will reclaim it.
		dead := deadPID(t)
		writeHolder(t, dir, lockHolder{PID: dead, Start: now.Format(createdFormat), Reason: "scheduled"})
		view, err = Status(d, runs, now)
		if err != nil {
			t.Fatalf("status over a corpse's lock: %v", err)
		}
		if view.Running {
			t.Errorf("a dead holder's lock: the status says running — a corpse is not a run")
		}
	})

	t.Run("the last verify is the newest of any file", func(t *testing.T) {
		d, _ := mkDeps(t)
		runs := fakeRuns{rows: []store.BackupRun{
			{Kind: "backup", Reason: "scheduled", Outcome: "ok", FinishedAt: stamp(time.Hour)},
			{Kind: "verify", Outcome: "ok", FinishedAt: stamp(30 * time.Minute)},
			{Kind: "verify", Outcome: "failed", FinishedAt: stamp(2 * time.Hour)},
		}}
		view, err := Status(d, runs, now)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if view.LastVerifyAt != stamp(30*time.Minute) {
			t.Errorf("the last verify is %q, the newest verify row is %s", view.LastVerifyAt, stamp(30*time.Minute))
		}
	})
}

// TestStatusSettingsRefusal: invalid settings refuse the status with the
// sentence, the same sentence the run would get — one voice for the same
// fact.
func TestStatusSettingsRefusal(t *testing.T) {
	d := fakeDeps(newFakeStore(firstApplied(3)), defaultFakeReport(firstApplied(3)), t.TempDir())
	d.Settings = NewEnvSettingsWith(func(k string) string {
		if k == "AEON_BACKUP_INTERVAL" {
			return "sometimes"
		}
		return ""
	})
	_, err := Status(d, nil, time.Now())
	if err == nil || !strings.Contains(err.Error(), "backup.interval") {
		t.Fatalf("invalid settings: the status refused with %v, the sentence must name the key", err)
	}
}
