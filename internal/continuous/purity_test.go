package continuous

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestPackageIsPure enforces the CN1 import contract over every
// non-test file: the standard library, and exactly the three internal
// packages the spec admits — internal/trigger (StillGoing, Schedule,
// LoadZone), internal/queue and internal/roadmap (the status and priority
// vocabularies). internal/store, internal/daemon and internal/protocol do
// not appear here, so the core cannot reach the world it only previews.
func TestPackageIsPure(t *testing.T) {
	allowed := map[string]bool{
		"github.com/cjlapao/aeon/internal/trigger": true,
		"github.com/cjlapao/aeon/internal/queue":   true,
		"github.com/cjlapao/aeon/internal/roadmap": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(path, "github.com/cjlapao/aeon/internal/") {
				continue // stdlib (and this package's own module) — allowed by shape
			}
			if !allowed[path] {
				t.Errorf("%s imports %s — the pure core speaks to three packages, and this one is not among them", e.Name(), path)
			}
		}
		// database/sql and any goroutine statement, wherever they hide:
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GoStmt:
				t.Errorf("%s starts a goroutine — the core is a function, not a process", e.Name())
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "sql" {
					t.Errorf("%s reaches for the database: sql.%s", e.Name(), x.Sel.Name)
				}
			}
			return true
		})
	}
}

// TestNoTimeNowOutsideTheSystemClock is the trigger's S20 law, adopted:
// the package holds exactly ONE wall-clock read — SystemClock.Now in
// clock.go. Plan and Preview receive `now` as data; a tick path reading
// the wall directly is a path no test can move, and preview drifting from
// the tick begins exactly there.
func TestNoTimeNowOutsideTheSystemClock(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	counts := map[string]int{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, e.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && sel.Sel.Name == "Now" {
					counts[e.Name()]++
				}
			}
			return true
		})
	}
	if counts["clock.go"] != 1 {
		t.Fatalf("clock.go holds %d time.Now reads, want exactly 1 (SystemClock.Now)", counts["clock.go"])
	}
	for file, n := range counts {
		if file != "clock.go" && n > 0 {
			t.Errorf("%s reaches the wall directly %d times — no test can move this path", file, n)
		}
	}
}
