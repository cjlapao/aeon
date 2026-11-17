package loop

// prefix_pin_test.go — S60 T4: names and base are PINNED at loop.start, so
// a later settings edit changes the next loop and never this one.

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/store"
)

// registerComponent registers a component whose primary root is dir, with
// the two S60 settings set (empty means leave the column NULL).
// registerComponent registers a component whose primary root is dir. The
// two S60 settings land through UPDATE, not create — S38 semantics: a ROOT
// insert omits the inheritable columns (the column defaults apply; the
// trigger refuses NULL on a root), so the settings door is update. The
// daemon's create seam passes them and they are dropped the same way
// (pre-existing S38 shape, recorded here for T4's Notes, not changed).
func registerComponent(t *testing.T, s *store.Store, name, dir, prefix, branch string) store.Component {
	t.Helper()
	c, err := s.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: dir})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	if prefix != "" || branch != "" {
		if _, err := s.UpdateComponent(store.ComponentEdit{ID: c.Component.ID, Name: name, Slug: name,
			BranchPrefix: storeNull(prefix), DefaultBranch: storeNull(branch)}); err != nil {
			t.Fatalf("settings for %s: %v", name, err)
		}
	}
	return c.Component
}

func storeNull(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func setPrefix(t *testing.T, s *store.Store, c store.Component, prefix string) {
	t.Helper()
	_, err := s.UpdateComponent(store.ComponentEdit{ID: c.ID, Name: c.Name, Slug: c.Slug,
		BranchPrefix: storeNull(prefix)})
	if err != nil {
		t.Fatalf("set prefix: %v", err)
	}
}

func startPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "one.yaml")
	writeBlueprint(t, path, "  - id: only\n    step: {kind: model-step, agent: default}\n")
	return path
}

func TestNamesPinnedPerLoop(t *testing.T) {
	s := openLoopStore(t)
	dir := t.TempDir()
	c := registerComponent(t, s, "pfx-a", dir, "aeon/", "")
	path := startPath(t)
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "pin-a", ComponentRoot: dir, Root: dir, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.ReadLoopByID(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.FeatureBranch != "aeon/feature/pin-a" || row.BranchMode != "standard" {
		t.Fatalf("pinned row: feature=%q mode=%q — want aeon/feature/pin-a + standard", row.FeatureBranch, row.BranchMode)
	}
	// A settings edit AFTER the pin changes nothing for this loop…
	setPrefix(t, s, c, "x/")
	pin, err := s.ReadPinnedLoop(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := FromPinned(pin)
	if err != nil {
		t.Fatal(err)
	}
	a := actionArgs(&ls, Action{Position: Position{Item: "fix"}})
	if a.Base != "aeon/feature/pin-a" || a.Prefix != "aeon/" || a.FeatureBranch != "aeon/feature/pin-a" {
		t.Fatalf("mid-loop prefix edit leaked in: base=%q prefix=%q", a.Base, a.Prefix)
	}
	// …and the next loop starts under the new prefix.
	st2, err := Start(context.Background(), s, store.NewLoop{Slug: "pin-b", ComponentRoot: dir, Root: dir, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	row2, err := s.ReadLoopByID(st2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row2.FeatureBranch != "x/feature/pin-b" {
		t.Fatalf("new loop pinned %q, want the NEW prefix", row2.FeatureBranch)
	}
}

func TestLegacyLoopWithoutPinStillDerives(t *testing.T) {
	// A row pinned before S60: the four columns empty. Every derived field
	// must read exactly as it did yesterday (behaviour 4 — in-flight loops
	// are never re-derived through a prefix they never carried).
	s := openLoopStore(t)
	dir := t.TempDir()
	c := registerComponent(t, s, "legacy", dir, "aeon/", "main")
	path := startPath(t)
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "old", ComponentRoot: dir, Root: dir, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Erase the pin the way history did it: the columns simply were not
	// written (a hand-backdated row — the store has no writer for this, so
	// the raw handle does it once, honestly, in the test).
	info, err := s.Info()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open(database.DriverName, info.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE loop SET feature_branch=NULL, branch_mode=NULL, default_branch=NULL, base_branch=NULL WHERE id=?`, st.ID); err != nil {
		t.Fatal(err)
	}
	_ = c
	pin, err := s.ReadPinnedLoop(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := FromPinned(pin)
	if err != nil {
		t.Fatal(err)
	}
	a := actionArgs(&ls, Action{Position: Position{Item: "fix"}})
	if a.Base != "feature/old" || a.Prefix != "" || a.FeatureBranch != "" || a.BaseBranch != "" {
		t.Fatalf("an un-pinned loop derived prefix=%q base=%q — must be the bare pre-S60 names", a.Prefix, a.Base)
	}
	names, err := loopNames(&ls, "fix")
	if err != nil || names.TaskBranch() != "task/old/fix" {
		t.Fatalf("loopNames on an un-pinned loop: %q (%v)", names.TaskBranch(), err)
	}
}

func TestBaseResolutionOrder(t *testing.T) {
	// order: with["base_branch"] → effective default_branch → origin/HEAD → empty
	s := openLoopStore(t)
	path := startPath(t)

	// (1) request wins over settings.
	dirA := t.TempDir()
	registerComponent(t, s, "ord-a", dirA, "", "trunk")
	stA, err := Start(context.Background(), s, store.NewLoop{Slug: "ord-a", ComponentRoot: dirA, Root: dirA}, path, nil, map[string]string{"base_branch": "release"})
	if err != nil {
		t.Fatal(err)
	}
	rowA, _ := s.ReadLoopByID(stA.ID)
	if rowA.BaseBranch != "release" || rowA.DefaultBranch != "trunk" {
		t.Fatalf("request lost: base=%q default=%q", rowA.BaseBranch, rowA.DefaultBranch)
	}
	// (2) settings win over origin/HEAD…
	gitDir := realGitRepo(t) // a repo whose origin/HEAD says master
	registerComponent(t, s, "ord-b", gitDir, "", "trunk")
	stB, err := Start(context.Background(), s, store.NewLoop{Slug: "ord-b", ComponentRoot: gitDir, Root: gitDir}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rowB, _ := s.ReadLoopByID(stB.ID)
	if rowB.BaseBranch != "trunk" {
		t.Fatalf("settings lost to origin/HEAD: base=%q", rowB.BaseBranch)
	}
	// (3) …and origin/HEAD answers when nobody configured anything. (A
	// SECOND repo: gitDir's primary root already belongs to ord-b — one
	// primary root per directory is the component_root key, not my bug.)
	gitDir2 := realGitRepo(t)
	registerComponent(t, s, "ord-c", gitDir2, "", "")
	stC, err := Start(context.Background(), s, store.NewLoop{Slug: "ord-c", ComponentRoot: gitDir2, Root: gitDir2}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rowC, _ := s.ReadLoopByID(stC.ID)
	if rowC.BaseBranch != "master" || rowC.DefaultBranch != "master" {
		t.Fatalf("origin/HEAD lost: base=%q default=%q, want master", rowC.BaseBranch, rowC.DefaultBranch)
	}
	// (4) nothing known → empty everywhere, and loop.start still starts
	//     (the queue refuses unknown bases, not here — Do-NT).
	dirD := t.TempDir() // not a git repo at all
	stD, err := Start(context.Background(), s, store.NewLoop{Slug: "ord-d", ComponentRoot: dirD, Root: dirD}, path, nil, nil)
	if err != nil {
		t.Fatalf("an unknown base must never refuse a start: %v", err)
	}
	rowD, _ := s.ReadLoopByID(stD.ID)
	if rowD.BaseBranch != "" || rowD.DefaultBranch != "" {
		t.Fatalf("unknown answered as %q/%q — empty means unknown", rowD.BaseBranch, rowD.DefaultBranch)
	}
	if rowD.FeatureBranch != "feature/ord-d" || rowD.BranchMode != "standard" {
		t.Fatalf("no component still pins no name: feature=%q mode=%q", rowD.FeatureBranch, rowD.BranchMode)
	}
}

// realGitRepo lays a repo with a bare origin, one commit pushed, and the
// remote-tracking HEAD symbolic ref set to master — the exact shape
// `git clone` leaves behind, minus the network.
func realGitRepo(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-b", "master", origin)
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "master")
	run(t, dir, "git", "remote", "add", "origin", origin)
	run(t, dir, "git", "config", "user.email", "t@t")
	run(t, dir, "git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "one")
	run(t, dir, "git", "push", "-u", "origin", "master")
	run(t, dir, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/master")
	return dir
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %s", name, args, strings.TrimSpace(string(out)))
	}
}

func TestNamesByteIdenticalWithEmptySettings(t *testing.T) {
	// The explicit half of "empty settings change nothing" — the goldens
	// pin the whole path; this names the two derivations directly.
	ls := LoopState{Slug: "plain"}
	a := actionArgs(&ls, Action{Position: Position{Item: "t1"}})
	if a.Base != "feature/plain" || a.Prefix != "" || a.BaseBranch != "" || a.FeatureBranch != "" {
		t.Fatalf("empty settings moved Args: %+v", a)
	}
	n, err := loopNames(&ls, "t1")
	if err != nil || n.TaskBranch() != "task/plain/t1" {
		t.Fatalf("empty settings moved names: %q (%v)", n.TaskBranch(), err)
	}
}
