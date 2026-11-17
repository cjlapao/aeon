package continuous

// seams_local_test.go — S27 T6's CN24 in source form: the core speaks to
// S55, S56 and S58 only through the local interfaces this package declares
// (types.go's BudgetOK/GrantAbsent facts, the Record/Start seam the daemon
// adapter fills). A single import of an owning slice's package here would
// weld the scheduler to a sibling: the daemon-side adapter
// (daemon/continuous_seams.go) is the ONE file allowed to import them, and
// this guard fails if the boundary ever moves back into the core.

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenCoreImports are the owning slices' packages — budgets (S55),
// autonomy (S56), and anything on the daemon side that bridges them
// (store, action, loop, daemon itself). The core's own domain imports
// (queue, roadmap, trigger) are not listed: they are the plan's inputs,
// read as facts.
var forbiddenCoreImports = []string{
	"/internal/store", "/internal/budget", "/internal/autonomy",
	"/internal/action", "/internal/loop", "/internal/daemon",
}

func TestSeamsAreLocalInterfaces(t *testing.T) {
	here := "."
	if _, file, _, ok := runtime.Caller(0); ok {
		here = filepath.Dir(file)
	}
	files, err := filepath.Glob(filepath.Join(here, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no package files found in %q", here)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		pf, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		checked++
		for _, imp := range pf.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbiddenCoreImports {
				if strings.HasSuffix(path, bad) {
					t.Errorf("%s imports %q — the core must reach S55/S56/S57/S58 only through this package's local seams (CN24); the adapters live in internal/daemon", filepath.Base(f), path)
				}
			}
		}
	}
	if checked < 3 {
		t.Fatalf("the guard read %d files — it must read the whole core", checked)
	}
}
