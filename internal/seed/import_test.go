package seed

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeedImportsStdlibOnly enforces the dependency rule (T1): the seed
// package imports the standard library and nothing else — no internal/store,
// database/sql, internal/handler, internal/policy, internal/mcp,
// internal/daemon, internal/protocol. A domain imports seed, never the
// reverse. It parses every non-test file in the package and refuses any
// import that names the aeon module (the only way a non-stdlib import would
// appear) or a bare "internal/..." path.
func TestSeedImportsStdlibOnly(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range entries {
		if strings.HasSuffix(f, "_test.go") {
			continue // test files may import the module under test
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "cjlapao") {
				t.Errorf("%s imports %q — the seed package must import the standard library only", f, p)
			}
			if strings.HasPrefix(p, "internal/") {
				t.Errorf("%s imports the bare internal path %q", f, p)
			}
			if p == "database/sql" {
				t.Errorf("%s imports database/sql — the seed package is stdlib-only", f)
			}
		}
	}
}
