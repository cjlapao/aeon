package backup

// The verify door's behaviour: the three levels, the newer-schema refusal
// that names the unknown migration, and the drill opening the copy — never
// the live state dir — and reporting the migrations that ran.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// TestVerifyLevels: level 1 answers the container's own facts; level 2
// checks the extracted copy against the manifest; level 3 opens the copy.
// Each failure mode keeps the level it reached and says what failed.
func TestVerifyLevels(t *testing.T) {
	applied := firstApplied(3)
	last := applied[len(applied)-1]

	t.Run("healthy levels on a real round trip", func(t *testing.T) {
		state := t.TempDir()
		bdir := t.TempDir()
		s := openRealStore(t, state)
		d := RealDeps(s, "0.0.0-test")
		d.StateDir = state
		res, err := Run(context.Background(), d, Options{Dir: bdir})
		if err != nil {
			t.Fatalf("the run: %v", err)
		}
		for level := 1; level <= 2; level++ {
			rep, err := Verify(context.Background(), d, res.Path, level)
			if err != nil {
				t.Fatalf("level %d on a healthy backup: %v", level, err)
			}
			if !rep.OK {
				t.Fatalf("level %d on a healthy backup is not ok: %s", level, rep.Sentence)
			}
			if rep.NewerSchema {
				t.Fatalf("level %d claims a newer schema for a backup this binary made", level)
			}
		}
	})

	t.Run("level 2 failure keeps the level and names the fault", func(t *testing.T) {
		bdir := t.TempDir()
		fs := newFakeStore(applied)
		path := mustRunForVerify(t, fs, bdir)

		cases := []struct {
			name   string
			mutate func(*store.FileReport)
			want   string
		}{
			{"integrity", func(r *store.FileReport) { r.IntegrityOK = false; r.IntegrityDetail = []string{"corrupt"} }, "integrity_check"},
			{"fk", func(r *store.FileReport) { r.ForeignKeyViolations = 5 }, "foreign-key"},
			{"fingerprint", func(r *store.FileReport) {
				r.Fingerprint = store.Fingerprint{Lines: []string{"agent", "loop", "tampered"}}
			}, "fingerprint"},
			{"count high", func(r *store.FileReport) { r.Tables["agent"] = 99 }, `table "agent"`},
			{"table missing", func(r *store.FileReport) { delete(r.Tables, "loop") }, `table "loop"`},
			{"table extra", func(r *store.FileReport) { r.Tables["ghost"] = 1 }, `table "ghost"`},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				rep := defaultFakeReport(applied)
				c.mutate(&rep)
				d2 := fakeDeps(fs, rep, t.TempDir())
				out, err := Verify(context.Background(), d2, path, 2)
				if err != nil {
					t.Fatalf("the level-2 check: %v", err)
				}
				if out.OK {
					t.Fatalf("the %s fault: level 2 says ok — %s", c.name, out.Sentence)
				}
				if out.Level != 2 {
					t.Errorf("the failure report lost its level: %d", out.Level)
				}
				if !strings.Contains(out.Sentence, c.want) {
					t.Errorf("the sentence %q does not name the fault (want %q)", out.Sentence, c.want)
				}
			})
		}
	})

	t.Run("level 1 on a hostile archive is a refused run", func(t *testing.T) {
		bdir := t.TempDir()
		m := baseManifest(3, last)
		m.DBSHA256 = strings.Repeat("0", 64)
		path := writeArchive(t, bdir, nameFor(t, m), m, nil)
		rep, err := Verify(context.Background(), fakeDeps(newFakeStore(applied), store.FileReport{}, t.TempDir()), path, 1)
		if err == nil || rep.OK {
			t.Fatalf("level 1 over the mismatched digest: want a refusal, got %+v / %v", rep, err)
		}
		var re RunError
		if !errors.As(err, &re) || re.Kind != ErrKindChecksum {
			t.Fatalf("the level-1 refusal kind: got %v, want checksum", err)
		}
	})
}

// mustRunForVerify runs one fake backup into bdir and answers its path.
func mustRunForVerify(t *testing.T, fs *fakeStore, bdir string) string {
	t.Helper()
	applied := fs.applied
	d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
	res, err := Run(context.Background(), d, Options{Dir: bdir})
	if err != nil {
		t.Fatalf("the setup run: %v", err)
	}
	return res.Path
}

// TestNewerSchemaIsRefusedForRestoreLevels: a manifest that names a
// migration this binary does not know is reported at level 1 and refused at
// levels 2 and 3 with the unknown name in the sentence — both the unknown
// name and the over-count shapes.
func TestNewerSchemaIsRefusedForRestoreLevels(t *testing.T) {
	applied := firstApplied(3)
	last := applied[len(applied)-1]

	t.Run("unknown name", func(t *testing.T) {
		bdir := t.TempDir()
		m := baseManifest(3, last)
		m.LastMigration = "99999999999999_unknown_future"
		path := writeArchive(t, bdir, nameFor(t, m), m, nil)
		d := fakeDeps(newFakeStore(applied), defaultFakeReport(applied), t.TempDir())

		l1, err := Verify(context.Background(), d, path, 1)
		if err != nil {
			t.Fatalf("level 1 reads the file's own facts even for a newer schema: %v", err)
		}
		if !l1.OK || !l1.NewerSchema || len(l1.Unknown) != 1 || l1.Unknown[0] != "99999999999999_unknown_future" {
			t.Fatalf("level 1 over the newer schema: %+v — readability is a fact, newer-schema is its flag", l1)
		}

		for level := 2; level <= 3; level++ {
			rep, err := Verify(context.Background(), d, path, level)
			if err != nil {
				t.Fatalf("level %d: %v", level, err)
			}
			if rep.OK {
				t.Fatalf("level %d over a newer schema is ok — restore would run into a downgrade", level)
			}
			if !rep.NewerSchema {
				t.Errorf("level %d did not flag the newer schema", level)
			}
			for _, want := range []string{"99999999999999_unknown_future", "newer aeon"} {
				if !strings.Contains(rep.Sentence, want) {
					t.Errorf("the level-%d sentence %q does not name %q", level, rep.Sentence, want)
				}
			}
		}
	})

	t.Run("over-count", func(t *testing.T) {
		bdir := t.TempDir()
		m := baseManifest(3, last)
		m.AppliedMigrations = len(store.Migrations()) + 5
		path := writeArchive(t, bdir, nameFor(t, m), m, nil)
		d := fakeDeps(newFakeStore(applied), defaultFakeReport(applied), t.TempDir())
		rep, err := Verify(context.Background(), d, path, 2)
		if err != nil || rep.OK {
			t.Fatalf("the over-count manifest: want a level-2 refusal, got %+v / %v", rep, err)
		}
		if !rep.NewerSchema {
			t.Fatalf("the over-count did not flag the newer schema")
		}
		if !strings.Contains(rep.Sentence, "applied count") {
			t.Errorf("the sentence %q does not say what the count says", rep.Sentence)
		}
	})
}

// TestDrillDoesNotTouchTheStateDir: level 3 opens the extracted copy as a
// store (migrations enabled) and reports which migrations ran — and the dir
// it opens is the scratch dir beside the archive, never the live state dir,
// whose listing is equal before and after.
func TestDrillDoesNotTouchTheStateDir(t *testing.T) {
	applied := firstApplied(3)
	stateDir := t.TempDir()
	canary := filepath.Join(stateDir, "canary")
	if err := os.WriteFile(canary, []byte("the live state dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := dirListing(t, stateDir)

	bdir := t.TempDir()
	fs := newFakeStore(applied)

	var openedDir, scratchAtOpen string
	d := fakeDeps(fs, defaultFakeReport(applied), stateDir)
	d.OpenStore = func(dir string) (io.Closer, error) {
		openedDir = dir
		// The real door creates files in the dir it opens; the fake does
		// the same. The scratch dir belongs to the reader's Close (which
		// removes it after the verify), so the listing is taken at the
		// moment of the call, where a file the drill created is a fact.
		if err := os.WriteFile(filepath.Join(dir, ".drill-marker"), []byte("the drill opened here"), 0o600); err != nil {
			return nil, err
		}
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			return nil, rerr
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		scratchAtOpen = strings.Join(names, ",")
		return noopCloser{}, nil
	}

	res, err := Run(context.Background(), d, Options{Dir: bdir})
	if err != nil {
		t.Fatalf("the setup run: %v", err)
	}
	rep, err := Verify(context.Background(), d, res.Path, 3)
	if err != nil {
		t.Fatalf("level 3: %v", err)
	}
	if !rep.OK {
		t.Fatalf("the drill over a known-schema backup: %s", rep.Sentence)
	}

	// It opened the scratch dir beside the archive, not the state dir.
	if openedDir == "" {
		t.Fatalf("the drill never opened a store")
	}
	if openedDir == stateDir || strings.HasPrefix(openedDir, stateDir+string(os.PathSeparator)) {
		t.Fatalf("the drill opened the live state dir (%s) — a drill must open the copy", openedDir)
	}
	if !tmpDirRE.MatchString(filepath.Base(openedDir)) {
		t.Fatalf("the drill opened %q, which is not the fenced scratch name", openedDir)
	}
	if filepath.Dir(openedDir) != bdir {
		t.Fatalf("the drill's dir %s is not beside the archive in %s", openedDir, bdir)
	}
	// At the moment of the open, the scratch held the marker the fake made
	// and the extracted database: the drill opened a real dir with the copy.
	if !strings.Contains(scratchAtOpen, ".drill-marker") || !strings.Contains(scratchAtOpen, "state.db") {
		t.Errorf("at the moment the drill opened the store, the scratch held %q — want the marker and state.db", scratchAtOpen)
	}
	// The scratch is the reader's, and the reader's close removed it: a
	// verify leaves no garbage beside the archive.
	if _, err := os.Stat(openedDir); !os.IsNotExist(err) {
		t.Errorf("the drill's scratch dir %s survived the verify — the reader's close owns its scratch", openedDir)
	}

	// It reported the migrations that ran: every one after the pre-applied set.
	migs := store.Migrations()
	want := map[string]bool{}
	for _, mig := range migs {
		if !inSlice(applied, mig.Name) {
			want[mig.Name] = true
		}
	}
	got := map[string]bool{}
	for _, n := range rep.DrillApplied {
		got[n] = true
	}
	if len(rep.DrillApplied) != len(want) {
		t.Fatalf("the drill reported %d migrations, %d ran", len(rep.DrillApplied), len(want))
	}
	for n := range want {
		if !got[n] {
			t.Errorf("the drill did not report %s as applied", n)
		}
	}
	if !strings.Contains(rep.Sentence, "drill") {
		t.Errorf("the sentence %q does not say it was a drill", rep.Sentence)
	}

	// The live state dir is untouched: equal listing, canary intact.
	if after := dirListing(t, stateDir); !sameListing(before, after) {
		t.Fatalf("the state dir changed under the drill: %v → %v", before, after)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("the canary is gone: %v", err)
	}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

func dirListing(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func sameListing(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func inSlice(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
