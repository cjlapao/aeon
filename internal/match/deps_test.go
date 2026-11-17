package match

// deps_test.go — the OTHER half of the S49 T2 extraction's promise. The
// store's guard (internal/store/store_test.go, amended with a dated line)
// pins that the store imports match; this pins that match imports NOTHING
// of ours — one direction, no cycle, and the store-free read side
// (internal/automationview) can run this grammar precisely because the
// grammar reaches for nothing but the standard library.

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestMatchImportsOnlyTheStandardLibrary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "github.com/cjlapao/aeon/") {
				t.Errorf("%s imports %s; internal/match imports no aeon package — that is why the store can import it", e.Name(), path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no sources found to check")
	}
}
