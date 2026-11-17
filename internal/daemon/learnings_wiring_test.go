package daemon

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// learnings_wiring_test.go — S28 T3's daemon-side wiring assertions: the
// learnings embedding route is wired in exactly one place.

// TestLearningsEmbeddingHasExactlyOneProductionCaller — the route-wiring's
// widening guard (the graph's TestGraphServiceHasExactlyOneProductionCaller
// precedent, for the learnings embedding): the learnings embedding route is
// wired in exactly one place — initLearnings, beside the learnings seam. A
// second production caller would be a second way to hand the store a route,
// which the seam exists to delete. The walk skips _test.go (the tests wire
// routes to exercise the seam, exactly as the graph's one-caller test
// allows), and counts the non-test files that build the learnings
// production embedder — the marker learnings.ModelClientEmbedder{, which the
// retrieval link does not use (it is retrieval.ModelClientEmbedder).
func TestLearningsEmbeddingHasExactlyOneProductionCaller(t *testing.T) {
	root := ".."
	var sites []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(src), "learnings.ModelClientEmbedder{") {
			sites = append(sites, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || !strings.HasSuffix(sites[0], "learnings_dispatch.go") {
		t.Errorf("learnings embedder production call sites = %v, want exactly learnings_dispatch.go — the route wires only in a reviewed diff", sites)
	}
}
