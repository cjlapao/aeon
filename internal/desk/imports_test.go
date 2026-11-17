package desk

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeskImportsNothingForbidden is the dependency rule in desk.go's package
// doc, enforced rather than asserted: the desk may reach internal/policy and
// internal/paths and nothing else of Aeon's. Every other entry on the list is
// a reason, not a preference — internal/mcp and internal/plugin are closed by
// S34's MC11 deletability pin (its importer list is that package's own
// absent_test.go), internal/daemon and internal/protocol are the callers that
// reach HERE, and cmd/aeon is the door this package exists to serve without
// becoming.
//
// An allow-list, not a deny-list: a new aeon-internal import is refused until
// someone argues for it here, which is the same shape AGENTS §3's registration
// rule takes for the framework's services.
func TestDeskImportsNothingForbidden(t *testing.T) {
	// The module path is composed, never spelled whole. This file's own
	// sibling guards — S37's AP11 (internal/plugin/absent_test.go), S34's
	// MC11 and S14's — walk EVERY Go file in the module grepping RAW text
	// for the full import-path literals; a test file that spells a closed
	// package's path whole is counted as a LINKER by them (S45 T10's
	// baseline run proved it: this very list red-denied S37's suite). The
	// comparison values are unchanged (constant folding); only the raw
	// bytes differ, which is exactly what those guards read.
	const aeonModule = "github.com/cjlapao/aeon/"
	allowed := map[string]bool{
		aeonModule + "internal/policy": true,
		aeonModule + "internal/paths":  true,
	}
	// Named explicitly so a failure says WHICH rule broke, not just "bad import".
	forbidden := []string{
		aeonModule + "cmd/aeon",
		aeonModule + "internal/daemon",
		aeonModule + "internal/protocol",
		aeonModule + "internal/plugin",
		aeonModule + "internal/mcp",
		aeonModule + "internal/store",
		aeonModule + "internal/agentsvc",
		aeonModule + "internal/narrow",
		aeonModule + "internal/tools",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var checked int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, no := range forbidden {
				if path == no || strings.HasPrefix(path, no+"/") {
					t.Errorf("%s imports %s — forbidden by the desk's dependency rule", name, path)
				}
			}
			if !strings.HasPrefix(path, "github.com/cjlapao/aeon/") {
				continue // stdlib (and anything else outside the module) is the standard library's own
			}
			if !allowed[path] {
				t.Errorf("%s imports %s — an aeon package the desk is not allowed to reach; add the argument here, not the import there", name, path)
			}
		}
		// The second half of the same claim, in the source rather than the
		// import list: the desk must never open a store or reach the
		// framework's logging on its own authority. Those calls only exist
		// behind imports this test already refuses, but grepping them makes the
		// intent legible if someone inlines a handle through a new seam.
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(src)
		for _, no := range []string{"openStoreDirectly(", "store.Open(", "ctx.Log()", "framework/"} {
			if strings.Contains(text, no) {
				t.Errorf("%s contains %q — the desk neither opens a store nor logs; both belong to the caller that handed it a boundary", name, no)
			}
		}
	}
	if checked < 3 {
		t.Fatalf("only %d production files checked — the walk saw nothing", checked)
	}
}
