package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoSecondMergeImplementation is HV1's floor under the whole slice: the
// merge — live loops AND archive headers in one sequence — is legal in exactly
// two places: internal/history (the formatter side) and internal/store (the
// door that owns the loop_archive SQL). `history.coverage` re-uses the merge
// rather than re-imagine it, and this walk makes the alternative un-shippable:
// any other file that reads ListArchives is a second merge, in another file,
// disagreeing quietly with the first.
func TestNoSecondMergeImplementation(t *testing.T) {
	root := moduleRoot(t)
	var offenders []string
	walked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "ui", ".git", "node_modules", "testdata", ".tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/history/") || strings.HasPrefix(rel, "internal/store/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "ListArchives(") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if walked < 100 {
		t.Fatalf("the walk saw only %d production files — a guard that walks almost nothing proves nothing", walked)
	}
	if len(offenders) > 0 {
		t.Errorf("second merge implementations outside internal/history and internal/store: %v — coverage re-uses the merge, it does not repeat it", offenders)
	}
}

// TestCoverageReusesTheMerge is the same law from the inside: coverage's own
// source calls Merge and no SQL at all — if the count came from a second
// query, the walk of this file's bytes is where it would show.
func TestCoverageReusesTheMerge(t *testing.T) {
	raw, err := os.ReadFile("coverage.go")
	if err != nil {
		t.Fatalf("read coverage.go: %v", err)
	}
	src := string(raw)
	if !strings.Contains(src, "Merge(s, opts)") {
		t.Error("coverage.go never calls Merge — it is not counting off the real query")
	}
	for _, banned := range []string{"QueryRow", "s.r.Query", "s.w.Exec", "ListLoops(", "ListArchives("} {
		if strings.Contains(src, banned) {
			t.Errorf("coverage.go contains %q — a second SQL path is a second merge in disguise", banned)
		}
	}
}

// moduleRoot walks up from the test's directory to the go.mod, so the walk
// covers the whole aeon module whatever the working directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test — the module root is unreachable")
		}
		dir = parent
	}
}
