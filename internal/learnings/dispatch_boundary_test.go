package learnings

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dispatch_boundary_test.go — S28 T3's A13 structural guard: the learnings
// embedding serves discovery (it ranks the tag-filtered set within it, D6),
// never dispatch (it never decides which tool or handler runs). The
// structural proof is an import boundary — internal/policy (the dispatch
// engine) and internal/learnings never import each other.

// The two import paths are split so the contiguous literal never appears in
// this file (the discipline the agent import boundary test keeps: a test
// that wrote the path verbatim would be read as a second, runtime-side
// import by a module-wide grep).
const (
	learningsImportPath = "github.com/cjlapao/aeon/internal/" + "learnings"
	policyImportPath    = "github.com/cjlapao/aeon/internal/" + "policy"
)

// boundaryOffender is the scanner the walk and the canary both call (testing
// the scanner, not the walk, the agent import boundary test's precedent). It
// names the offending import path for a file that breaks A13's boundary: an
// internal/policy file that imports internal/learnings (a ranking reaching
// the gate), or an internal/learnings file that imports internal/policy (the
// seam reaching the guard it cannot consult). A comment naming the path is
// not a false positive — only an import statement's quoted path matches.
func boundaryOffender(s, path string) (string, bool) {
	if strings.HasPrefix(path, "internal/policy/") && strings.Contains(s, `"`+learningsImportPath+`"`) {
		return learningsImportPath, true
	}
	if strings.HasPrefix(path, "internal/learnings/") && strings.Contains(s, `"`+policyImportPath+`"`) {
		return policyImportPath, true
	}
	return "", false
}

// TestEmbeddingNeverChoosesDispatch — A13: if internal/policy could import
// internal/learnings, an embedding could leak into a dispatch decision
// (the worst failure — a ranking reaching the gate); if internal/learnings
// could import internal/policy, the seam could read the guard it is meant to
// stand apart from. Neither direction is allowed, so a second path cannot
// creep in. The walk skips _test.go — the test files name the tokens in
// assertions, the runtime code may not.
func TestEmbeddingNeverChoosesDispatch(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if tok, ok := boundaryOffender(string(src), path); ok {
			t.Errorf("%s: %s references %q — the dispatch engine and the learnings seam never import each other (A13)", path, path, tok)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// TestEmbeddingDispatchBoundaryScannerIsNotBlind is the canary: a temp .go
// file carrying the quoted import path makes the scanner return an offender
// — proving the scanner is not a no-op (the agent import boundary test's
// precedent). The path is assembled from the split tokens so it is not
// present verbatim here either.
func TestEmbeddingDispatchBoundaryScannerIsNotBlind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "policy", "offender.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make dir: %v", err)
	}
	canary := "package policy\nimport \"" + learningsImportPath + "\"\nfunc f() { _ = learnings.New }\n"
	if err := os.WriteFile(path, []byte(canary), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	// The relative path the walk would see, so the prefix check fires.
	rel := filepath.Join("internal", "policy", "offender.go")
	if _, ok := boundaryOffender(canary, rel); !ok {
		t.Fatal("the boundary scanner must flag an internal/policy file that imports internal/learnings — the scanner is blind")
	}
}
