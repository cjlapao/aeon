package graph

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T6 — the boundary that keeps the graph an INDEX and not a router
// (D10, CG9). The temptation is structural, not moral: a graph edge
// looks similarity-shaped, and the dispatch-by-tech-tag resolver decides
// which agent handles a step by similarity — so the wrong implementation
// is one import away in each direction. The prose says "don't"; this test
// makes the import itself fail the build of the test suite, in BOTH
// directions, and reads the AST (go/parser), not grep, so comments and
// strings cannot spoof it.
//
// The resolver side is internal/handler (the tech-tag dispatch lives
// there) plus the daemon's own dispatch files (automation.go,
// blueprint.go). The daemon package as a whole may import this one —
// daemon.New wires the write-hook subscription and the tool seam (T4/T5),
// which is composition, not resolution — so the guard names the dispatch
// files and does not pardon them by silence.

func packageFileImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string][]string{}
	for _, pkg := range pkgs {
		for file, f := range pkg.Files {
			var imps []string
			for _, imp := range f.Imports {
				imps = append(imps, strings.Trim(imp.Path.Value, `"`))
			}
			out[file] = imps
		}
	}
	return out
}

// TestGraphNeverImportsTheResolver — direction one.
func TestGraphNeverImportsTheResolver(t *testing.T) {
	imports := packageFileImports(t, ".")
	for file, imps := range imports {
		for _, imp := range imps {
			if strings.Contains(imp, "internal/handler") || strings.Contains(imp, "internal/daemon") ||
				strings.Contains(imp, "dispatch") || strings.Contains(imp, "internal/automation") {
				t.Errorf("%s imports %q — the graph does not route work, it indexes code (D10, CG9)", file, imp)
			}
		}
	}
}

// TestResolverNeverImportsTheGraph — direction two, at the exact files
// that decide which agent handles a step.
func TestResolverNeverImportsTheGraph(t *testing.T) {
	dirs := []string{"../handler"}
	for _, dir := range dirs {
		imports := packageFileImports(t, dir)
		for file, imps := range imports {
			for _, imp := range imps {
				if strings.HasSuffix(imp, "/internal/graph") {
					t.Errorf("%s imports %q — the dispatch resolver must never read the graph (CG9): the four-state refusal it answers with when the graph is absent (D10) only means something because the resolver CANNOT consult it", file, imp)
				}
			}
		}
	}
	// The daemon's dispatch files, named rather than the package:
	// daemon.go importing graph is T5's wiring; these two files are the
	// ones that choose agents.
	daemonDir := "../daemon"
	for _, name := range []string{"automation.go", "blueprint.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(daemonDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse daemon/%s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), "/internal/graph") {
				t.Errorf("daemon/%s imports internal/graph — dispatch must not read the graph (CG9)", name)
			}
		}
	}
}

// TestFourStatesAreDistinctAndTotal — the same assertion from both sides
// of T6's sentence: the states are four VALUES, not two plus a guess.
func TestFourStatesAreDistinctAndTotal(t *testing.T) {
	seen := map[State]bool{}
	for _, s := range AllStates {
		if seen[s] {
			t.Errorf("state %q listed twice", s)
		}
		seen[s] = true
	}
	if len(AllStates) != 4 {
		t.Fatalf("AllStates has %d entries, want exactly 4 — found/ambiguous/not-found/not-built (D8, CG7)", len(AllStates))
	}
	for _, want := range []State{StateFound, StateAmbiguous, StateNotFound, StateNotBuilt} {
		if !seen[want] {
			t.Errorf("state %q missing from AllStates", want)
		}
	}
}
