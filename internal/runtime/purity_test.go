package runtime

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestRuntimeStaysPure (S55 T7) pins the dependency direction the whole cap
// design rests on: the step runtime is a leaf over `model` and the standard
// library. The budget engine (internal/budget) and the store live on the
// DAEMON side; the daemon resolves the step's applicable budgets, evaluates
// them with the pure engine, and hands the answer in as plain data —
// BudgetVerdict / CapHit — through RunOpts. A seam that leaked the other way
// (the runtime importing store or budget) would need a test to point at the
// real store or a real budget row, which is the rule this package's other
// guards exist to keep out.
//
// go/parser rather than a grep: an import is a statement with a shape, and
// parsing catches the string-in-a-comment false positive a grep would need a
// second pass to delete.
func TestRuntimeStaysPure(t *testing.T) {
	forbidden := []string{
		"github.com/cjlapao/aeon/internal/store",
		"github.com/cjlapao/aeon/internal/budget",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, ban := range forbidden {
				if path == ban {
					t.Errorf("%s imports %s — the runtime must stay a leaf over model (the budget and store sides are injected as plain data)", name, path)
				}
			}
		}
	}
	if checked < 5 {
		t.Fatalf("expected several sources, found %d — a parse of nothing passes forever", checked)
	}
}
