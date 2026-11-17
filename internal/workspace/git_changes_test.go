package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=a@b", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The changes view counts a tracked edit from git, an untracked file by its lines, a binary file as having no
// count, and its totals cover only the counts it knows.
func TestGitChangesCountsEveryKindOfChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "init")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o644) // +2 -1
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\ny\n"), 0o644)                // untracked, 2 lines
	_ = os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0, 1, 2, 0}, 0o644)              // untracked binary

	res := GitChanges(context.Background(), dir)
	if res.State != StateOK {
		t.Fatalf("state %v: %s", res.State, res.Reason)
	}
	by := map[string]ChangedFileStat{}
	for _, f := range res.Files {
		by[f.Path] = f
	}
	if a := by["a.txt"]; a.Status != "modified" || a.Added == nil || *a.Added != 2 || *a.Removed != 1 {
		t.Errorf("a.txt: %+v", a)
	}
	if n := by["new.txt"]; n.Status != "untracked" || n.Added == nil || *n.Added != 2 {
		t.Errorf("new.txt: %+v", n)
	}
	if b := by["bin.dat"]; !b.Binary || b.Added != nil {
		t.Errorf("bin.dat must have no count: %+v", b)
	}
	if res.TotalAdded != 4 || res.TotalRemoved != 1 {
		t.Errorf("totals %d/%d, want 4/1", res.TotalAdded, res.TotalRemoved)
	}
}
