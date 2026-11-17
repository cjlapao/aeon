package paging

// deps_test.go — the OTHER half of S73 T2's promise. The store's import guard
// (internal/store/store_test.go, widened with a dated line for this package)
// pins that the store may import internal/paging; this pins that paging imports
// NOTHING of ours — one direction, no cycle. The store can import the
// convention precisely because the convention reaches for nothing but the
// standard library: no database, no driver, no store, no protocol. If this test
// ever fails, the direction has been reversed somewhere, and the fix is in the
// file that grew the import — not in a wider allow-list.

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestPagingImportsOnlyTheStandardLibrary(t *testing.T) {
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
				t.Errorf("%s imports %s; internal/paging imports no aeon package — that is why the store can import it", e.Name(), path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no sources found to check")
	}
}
