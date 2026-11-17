// pure_test.go — the import guard (spec test 17): this package is the
// pure half of the merge gate. It may import the forge's TYPES (the
// facts it evaluates carry them) and the standard library — and
// nothing else. No store (the direction is store→mergerule, pinned the
// other way by the store's own tests), no forge implementation, no
// net, no os/exec, no clock. The guard reads the package's own source
// with go/parser, so it holds on a fresh checkout and names the import
// it refuses.

package mergerule

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeRulePackageIsPure(t *testing.T) {
	// The banned import prefixes: anything that can do I/O, hold a
	// connection or read a clock.
	banned := []string{
		"github.com/cjlapao/aeon/internal/store",
		"github.com/cjlapao/aeon/internal/forge/impl", // a forge implementation, not its types
		"net",
		"os/exec",
		"os",
		"database/sql",
		"time",
		"context",
	}
	// The forge's type package IS admitted (the Facts carry its types).
	// Everything else foreign is refused by the banned list plus the
	// "not the standard library and not this module's types" rule
	// below.
	allowed := map[string]bool{
		"github.com/cjlapao/aeon/internal/forge": true,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no source files found — the guard has nothing to read")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue // the tests may do what the package may not
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if allowed[path] {
				continue
			}
			for _, b := range banned {
				if path == b || strings.HasPrefix(path, b+"/") {
					t.Errorf("%s imports %q — the pure half may not", name, path)
				}
			}
			if !allowed[path] && !isStdlib(path) {
				t.Errorf("%s imports %q — a foreign import needs a named reason", name, path)
			}
		}
	}
}

// isStdlib is the no-dot rule: standard-library packages have no slash
// in a module path, so "strings" and "reflect" pass and anything
// namespaced does not.
func isStdlib(path string) bool {
	return !strings.Contains(path, ".")
}
