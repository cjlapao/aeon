package automationview

// deps_test.go — S49's layering rule, mechanically: no file of THIS
// package (non-test) imports the store. The package's whole job is to turn
// already-read facts into words, and the day it opens a database it is a
// second daemon. The differential test in matchtest_test.go (the external
// _test package) may import the store — that is how it proves agreement
// with production without production sneaking into the library.

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestAutomationviewImportsNoStore(t *testing.T) {
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
			if strings.HasSuffix(path, "/internal/store") {
				t.Errorf("%s imports %s — automationview never touches the store", e.Name(), path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no sources to check")
	}
}

// TestAutomationviewAddsNoNewDependency pins the package to what the plan
// allows: the standard library, internal/match and internal/trigger — no
// third-party module ever.
func TestAutomationviewAddsNoNewDependency(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, ".") && !strings.HasPrefix(path, "github.com/cjlapao/aeon/internal/") {
				t.Errorf("%s imports %s; automationview's only aeon imports are internal/match and internal/trigger, and nothing else non-standard", e.Name(), path)
			}
		}
	}
}
