package backup

// The run's behaviour: the round trip on a real store, the atomic publish
// under faults, the single-flight lock and its stale reclaim, the
// three-state free space, and the confidentiality of the manifest and the
// ledger row.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// TestRunRoundTrip: Run on a populated real store yields a file whose
// manifest matches CheckFile of the extracted db — counts, fingerprint,
// sha256 — mode 0600, one file, one ledger row.
func TestRunRoundTrip(t *testing.T) {
	state := t.TempDir()
	bdir := t.TempDir()
	s := openRealStore(t, state)

	// Populate one free-text row: the manifest must count it, never carry it.
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{
		Slug:        "roundtrip-agent",
		Name:        "Round Trip Agent",
		Description: "counts in the manifest, content never",
		Metadata:    `{"note":"the manifest must not echo me"}`,
	}, s.Now()); err != nil {
		t.Fatalf("seed the agent row: %v", err)
	}

	d := RealDeps(s, "0.0.0-test")
	d.StateDir = state
	res, err := Run(context.Background(), d, Options{Dir: bdir, Reason: "manual", ActorKind: "user"})
	if err != nil {
		t.Fatalf("the run: %v", err)
	}
	if !fileRE.MatchString(res.FileName) {
		t.Fatalf("the file name %q does not match the closed name set", res.FileName)
	}
	entries, err := os.ReadDir(bdir)
	if err != nil {
		t.Fatalf("read the backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the backup dir holds %d entries, the run makes exactly one file: %v", len(entries), names(entries))
	}
	fi, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("the published file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("the artefact is mode %o, the run writes 0600 (a backup is confidential)", fi.Mode().Perm())
	}
	// The result reports the database's size (the manifest's db_bytes),
	// and the archive file is that plus the manifest member and tar's
	// block padding — strictly larger, never equal.
	if fi.Size() <= res.SizeBytes {
		t.Fatalf("the archive is %d bytes, the database member is %d — the container must hold more than its database", fi.Size(), res.SizeBytes)
	}

	r, err := Open(res.Path)
	if err != nil {
		t.Fatalf("re-open the published artefact: %v", err)
	}
	defer func() { _ = r.Close() }()
	m := r.Manifest()
	rep, err := store.CheckFile(context.Background(), r.DBPath())
	if err != nil {
		t.Fatalf("check the extracted db: %v", err)
	}
	if !rep.IntegrityOK {
		t.Fatalf("the extracted db fails integrity_check: %v", rep.IntegrityDetail)
	}
	for name, want := range rep.Tables {
		if got := m.Tables[name]; got != want {
			t.Errorf("manifest count for %q is %d, the extracted db has %d", name, got, want)
		}
	}
	if m.DBBytes != res.SizeBytes || m.DBSHA256 != res.SHA256 {
		t.Errorf("the manifest and the result disagree: db_bytes %d/%d, digest %s…/%s…", m.DBBytes, res.SizeBytes, m.DBSHA256[:8], res.SHA256[:8])
	}
	if m.JournalModeInFile != "delete" {
		t.Errorf("the manifest's journal_mode_in_file is %q, a one-file backup is delete", m.JournalModeInFile)
	}
	if m.CredentialsIncluded || !m.Confidential {
		t.Errorf("the manifest says credentials_included=%v confidential=%v — the constants are false and true", m.CredentialsIncluded, m.Confidential)
	}
	if m.Reason != "manual" || m.AeonVersion != "0.0.0-test" {
		t.Errorf("the manifest's reason/version are %q/%q", m.Reason, m.AeonVersion)
	}
	if m.AppliedMigrations != len(store.Migrations()) {
		t.Errorf("the manifest counts %d applied migrations, the store has %d", m.AppliedMigrations, len(store.Migrations()))
	}

	// The ledger row: one, ok, named, with the numbers the file carries.
	rows, err := s.ListBackupRuns(store.BackupRunFilter{Kind: "backup"})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, the run records exactly one", len(rows))
	}
	row := rows[0]
	if row.Outcome != "ok" || row.Reason != "manual" || row.ActorKind != "user" {
		t.Errorf("the row is %s/%s/%s, the run is ok/manual/user", row.Outcome, row.Reason, row.ActorKind)
	}
	if row.FileName == nil || *row.FileName != res.FileName {
		t.Errorf("the row names %v, the run published %s", row.FileName, res.FileName)
	}
	if row.VerifyLevel == nil || *row.VerifyLevel != "verified" {
		t.Errorf("the row's verify level is %v, the default verifies after the run", row.VerifyLevel)
	}
	if row.SizeBytes == nil || *row.SizeBytes != res.SizeBytes || row.SHA256 == nil || *row.SHA256 != res.SHA256 {
		t.Errorf("the row's size/sha do not match the published file")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestAtomicPublishUnderFaults: a fault at each of the five spec points
// (after snapshot, after verify, mid tar write, before rename, after
// rename) never leaves a final-named incomplete file. The four pre-rename
// faults leave nothing at all; the after-rename crash leaves the complete
// file plus the tmp garbage a crash cannot clean up, and SweepPartials
// removes the garbage without touching the file.
func TestAtomicPublishUnderFaults(t *testing.T) {
	applied := firstApplied(3)
	// The five injection points, in pipeline order. The first four are
	// pre-publication faults (a run that failed); the last is a crash once
	// the publish is done (a run whose file stands and whose row never was).
	points := []struct {
		stage    string
		afterPub bool // the fault lands after the rename: the file stands
	}{
		{"snapshot", false},
		{"check", false},
		{"tar", false},
		{"sync", false},
		{"fsync-dir", true},
	}
	crash := errors.New("crash: the process dies here")
	for _, p := range points {
		t.Run(p.stage, func(t *testing.T) {
			bdir := t.TempDir()
			fs := newFakeStore(applied)
			d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
			opts := Options{Dir: bdir, fault: map[string]error{p.stage: crash}}
			res, err := Run(context.Background(), d, opts)

			var finals, partials, tmps []string
			for _, e := range mustReadDir(t, bdir) {
				switch {
				case fileRE.MatchString(e.Name()):
					finals = append(finals, e.Name())
				case partialRE.MatchString(e.Name()):
					partials = append(partials, e.Name())
				case tmpDirRE.MatchString(e.Name()):
					tmps = append(tmps, e.Name())
				}
			}
			if p.afterPub {
				if err != nil {
					t.Fatalf("the after-rename crash is not a failure: the file is a complete fact, got %v", err)
				}
				if res.Recorded {
					t.Fatalf("the crash happens before the ledger row — Recorded must be false")
				}
				if len(finals) != 1 {
					t.Fatalf("the after-rename crash leaves %d final files, the complete one must stand: %v", len(finals), finals)
				}
				if len(partials) != 0 {
					t.Fatalf("the after-rename crash leaves partials: %v", partials)
				}
				// The file must be a complete, readable container.
				r, oerr := Open(filepath.Join(bdir, finals[0]))
				if oerr != nil {
					t.Fatalf("the surviving file is not a complete container: %v", oerr)
				}
				_ = r.Close()
				if len(tmps) != 1 {
					t.Fatalf("a crash after the rename leaves the build dir (a crash cannot clean up): found %v", tmps)
				}
				removed, serr := SweepPartials(bdir)
				if serr != nil {
					t.Fatalf("the sweep: %v", serr)
				}
				if len(removed) != 1 || removed[0] != tmps[0] {
					t.Fatalf("the sweep removed %v, the crash garbage is %v", removed, tmps)
				}
				// The sweep must not have touched the final file.
				entries, _ := os.ReadDir(bdir)
				if len(entries) != 1 || !fileRE.MatchString(entries[0].Name()) {
					t.Fatalf("the sweep left %v behind — it removes garbage, not backups", names(entries))
				}
			} else {
				if err == nil {
					t.Fatalf("the fault at %q did not fail the run", p.stage)
				}
				if len(finals) != 0 {
					t.Fatalf("a %q fault leaves a final-named file — the publish is not atomic: %v", p.stage, finals)
				}
				if len(partials) != 0 {
					t.Fatalf("a %q fault leaves a .partial the run should have removed: %v", p.stage, partials)
				}
				if len(tmps) != 0 {
					t.Fatalf("a %q fault leaves a .tmp-* build dir: %v", p.stage, tmps)
				}
				// The failure recorded a row.
				rows := fs.rowsSnapshot()
				if len(rows) != 1 || rows[0].Outcome != "failed" {
					t.Fatalf("the faulted run recorded %d rows, one failed: %+v", len(rows), rows)
				}
			}
			// Either way the lock is released: a clean run now succeeds.
			res2, err2 := Run(context.Background(), d, Options{Dir: bdir})
			if err2 != nil {
				t.Fatalf("the run after the %q fault: the lock must be released, got %v", p.stage, err2)
			}
			if !fileRE.MatchString(res2.FileName) {
				t.Fatalf("the recovery run published %q", res2.FileName)
			}
		})
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return entries
}

// TestSingleFlightLock: a second Run while the first holds the lock refuses
// and names the holder (the cross-process shape: a live pid, a fresh
// start, a reason); a re-entrant call in this process refuses as its own.
func TestSingleFlightLock(t *testing.T) {
	applied := firstApplied(3)
	bdir := t.TempDir()
	fs := newFakeStore(applied)
	d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())

	// The cross-process shape: a lock file naming a live pid (this one,
	// from a different incarnation — the in-process map is not holding it),
	// a fresh start and a reason.
	other := lockHolder{PID: os.Getpid(), Start: testNow.UTC().Format(createdFormat), Reason: "scheduled"}
	writeHolder(t, bdir, other)
	_, err := Run(context.Background(), d, Options{Dir: bdir})
	// The run's refusal is a RunError of the ledger's kind, carrying the
	// holder's sentence — the lock's own error type stays inside the door.
	var re RunError
	if !errors.As(err, &re) || re.Kind != ErrKindLockHeld {
		t.Fatalf("the run over a live held lock: want a lock-held refusal naming the holder, got %v", err)
	}
	msg := re.What
	for _, want := range []string{strconv.Itoa(other.PID), other.Start, other.Reason} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal %q does not name %q — a refusal must name the holder", msg, want)
		}
	}
	// The refusal recorded a refused row.
	rows := fs.rowsSnapshot()
	if len(rows) != 1 || rows[0].Outcome != "refused" || rows[0].ErrorKind != ErrKindLockHeld {
		t.Fatalf("the held-lock refusal recorded %+v, want one refused row with error_kind lock-held", rows)
	}

	// The in-process shape: a re-entrant Run while this process holds the
	// lock is its own refusal, and the lock is not stolen. The planted
	// holder is gone first: the file would otherwise answer before the
	// in-process map does.
	if err := removeBackupFile(bdir, lockFileName); err != nil {
		t.Fatalf("clear the planted lock: %v", err)
	}
	lock, lerr := Acquire(bdir, "manual", testNow)
	if lerr != nil {
		t.Fatalf("acquire for the re-entrancy case: %v", lerr)
	}
	defer func() { _ = lock.Release() }()
	_, err2 := Run(context.Background(), d, Options{Dir: bdir})
	var re2 RunError
	if !errors.As(err2, &re2) || re2.Kind != ErrKindLockHeld || !strings.Contains(re2.What, "in this process") {
		t.Fatalf("the re-entrant run: want the self-hold refusal, got %v", err2)
	}
	if _, lerr := os.Stat(filepath.Join(bdir, lockFileName)); lerr != nil {
		t.Fatalf("the re-entrant refusal must not have deleted the live lock: %v", lerr)
	}
	// And the first holder still owns it: Release works, then a clean run.
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := Run(context.Background(), d, Options{Dir: bdir}); err != nil {
		t.Fatalf("the run after the release: %v", err)
	}
}

// TestStaleLockReclaimed: a lock whose holder is dead, or whose lock is
// older than the stale rule, is reclaimed — and the reclaim is reported on
// the returned lock and in the run.
func TestStaleLockReclaimed(t *testing.T) {
	applied := firstApplied(3)

	t.Run("dead holder", func(t *testing.T) {
		bdir := t.TempDir()
		dead := deadPID(t)
		old := lockHolder{PID: dead, Start: testNow.UTC().Format(createdFormat), Reason: "manual"}
		writeHolder(t, bdir, old)
		fs := newFakeStore(applied)
		d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
		res, err := Run(context.Background(), d, Options{Dir: bdir})
		if err != nil {
			t.Fatalf("the run over a dead holder's lock: %v", err)
		}
		if !res.LockReclaimed {
			t.Fatalf("the reclaim must be reported on the result")
		}
	})

	t.Run("old lock, live holder", func(t *testing.T) {
		bdir := t.TempDir()
		old := lockHolder{PID: os.Getpid(), Start: testNow.Add(-7 * time.Hour).UTC().Format(createdFormat), Reason: "scheduled"}
		writeHolder(t, bdir, old)
		fs := newFakeStore(applied)
		d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
		res, err := Run(context.Background(), d, Options{Dir: bdir})
		if err != nil {
			t.Fatalf("the run over a 7-hour-old lock: %v", err)
		}
		if !res.LockReclaimed {
			t.Fatalf("the age reclaim must be reported on the result")
		}
	})

	t.Run("corrupt lock file", func(t *testing.T) {
		bdir := t.TempDir()
		if err := os.MkdirAll(bdir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bdir, lockFileName), []byte("not json at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		fs := newFakeStore(applied)
		d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
		res, err := Run(context.Background(), d, Options{Dir: bdir})
		if err != nil {
			t.Fatalf("the run over a corrupt lock file (a file that cannot name its holder cannot prove its claim): %v", err)
		}
		if !res.LockReclaimed {
			t.Fatalf("the reclaim of an unidentifiable lock must be reported")
		}
	})
}

// writeHolder plants a lock file with the given holder.
func writeHolder(t *testing.T, dir string, h lockHolder) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lockFileName), append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// deadPID runs a child to completion and answers its now-dead pid.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the dead-pid child: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait the dead-pid child: %v", err)
	}
	return pid
}

// TestFreeSpaceThreeStates: ok proceeds and records ok; low refuses with
// disk-full and records low; unknown proceeds and records unknown — never
// a silent pass.
func TestFreeSpaceThreeStates(t *testing.T) {
	applied := firstApplied(3)
	orig := freeSpace
	t.Cleanup(func() { freeSpace = orig })

	cases := []struct {
		name     string
		free     uint64
		state    FreeState
		wantErr  bool
		wantKind string
		recorded string
	}{
		{"ok", 1 << 30, FreeOK, false, "", "ok"},
		{"low", 1024, FreeLow, true, ErrKindDiskFull, "low"},
		{"unknown", 0, FreeUnknown, false, "", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bdir := t.TempDir()
			fs := newFakeStore(applied)
			d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
			freeSpace = func(string) (uint64, FreeState) { return c.free, c.state }
			// classifyFreeSpace applies the 1.2× threshold on ok/low: the
			// fake's ok value clears it, the low value does not.
			_, err := Run(context.Background(), d, Options{Dir: bdir})
			if c.wantErr != (err != nil) {
				t.Fatalf("the %s space state: error %v, want failure=%v", c.name, err, c.wantErr)
			}
			if c.wantErr {
				var re RunError
				if !errors.As(err, &re) || re.Kind != c.wantKind {
					t.Fatalf("the refusal is %v, want kind %s", err, c.wantKind)
				}
				if !strings.Contains(re.What, "not enough free space") {
					t.Errorf("the refusal %q does not say what it refused", re.What)
				}
			}
			rows := fs.rowsSnapshot()
			if len(rows) != 1 {
				t.Fatalf("the run recorded %d rows, want one: %+v", len(rows), rows)
			}
			if rows[0].FreeSpaceState != c.recorded {
				t.Errorf("the row's free_space_state is %q, the run measured %q — an unmeasured disk is a state, not a pass", rows[0].FreeSpaceState, c.recorded)
			}
			if c.wantErr && rows[0].Outcome != "refused" {
				t.Errorf("the disk-full row is %q, a policy refusal records refused", rows[0].Outcome)
			}
			if !c.wantErr && rows[0].Outcome != "ok" {
				t.Errorf("the %s row is %q, the run succeeded", c.name, rows[0].Outcome)
			}
		})
	}
}

// TestNothingSecretInManifestOrRun: a fake secret planted in a free-text
// row appears nowhere in the manifest's bytes or the run row — the manifest
// carries counts, and the row carries a fixed template.
func TestNothingSecretInManifestOrRun(t *testing.T) {
	const secret = "sk-fake-secret-0000-do-not-echo"
	state := t.TempDir()
	bdir := t.TempDir()
	s := openRealStore(t, state)
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{
		Slug:     "secret-agent",
		Name:     "Secret Agent",
		Metadata: `{"extra_body":"Authorization: Bearer ` + secret + `"}`,
		Prompt:   "the prompt also carries " + secret,
		Handoff:  `{"note":"` + secret + `"}`,
	}, s.Now()); err != nil {
		t.Fatalf("seed the secret row: %v", err)
	}

	d := RealDeps(s, "0.0.0-test")
	d.StateDir = state
	res, err := Run(context.Background(), d, Options{Dir: bdir})
	if err != nil {
		t.Fatalf("the run: %v", err)
	}

	// The manifest, straight from the file's bytes.
	r, err := Open(res.Path)
	if err != nil {
		t.Fatalf("open the artefact: %v", err)
	}
	m := r.Manifest()
	_ = r.Close()
	man, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal the manifest: %v", err)
	}
	if strings.Contains(string(man), secret) {
		t.Errorf("the manifest's bytes echo the row content — a manifest is counts and digests, never row text")
	}
	for _, field := range []string{m.Name, m.CreatedAt, m.Reason, m.AeonVersion, m.LastMigration, m.SchemaFingerprintSHA, m.DBSHA256} {
		if strings.Contains(field, secret) {
			t.Errorf("the manifest field %q echoes the secret", field)
		}
	}
	for _, entry := range m.OutsideDB {
		if strings.Contains(entry, secret) {
			t.Errorf("the manifest's outside_db echoes the secret")
		}
	}

	// The run row: the fixed template and the numbers, nothing else.
	rows, err := s.ListBackupRuns(store.BackupRunFilter{Kind: "backup"})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	row := rows[0]
	for _, field := range []string{row.Detail, strv(row.FileName), strv(row.SHA256)} {
		if strings.Contains(field, secret) {
			t.Errorf("the run row field %q echoes the row content — the detail is a fixed template", field)
		}
	}
	if !strings.Contains(row.Detail, "snapshot verified") {
		t.Errorf("the detail %q is not the fixed template", row.Detail)
	}
}
