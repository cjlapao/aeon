package main

// S62 T5 · the parser-level guard for the pre-migration door.
//
// BK6's ordering contract — copy before the first migration — holds only
// while every door that opens the store carries the hook. The hook is
// wired in ONE place: storeOpenOptions (store_open.go), which every door
// builds its Options through. This guard walks the package's non-test
// source with go/parser and refuses any store.Open call that does not:
//
//   - pass storeOpenOptions(...) — the hooked door (serve, the verbs), or
//   - pass drillOpenOptions(...) — the one documented exception: the
//     verify drill's door, which migrates but must not re-run the net on
//     a copy that already had it, or
//   - pass a store.Options literal that sets SkipMigrations: true — a
//     door that opens without a migration has no migration to protect
//     (the direct read doors T9 will add).
//
// A raw store.Options{...} with none of those is a new door that skipped
// the safety net — the failure this guard exists to make impossible
// before it is possible.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeOpenCall reports one store.Open call site found in the walk.
type storeOpenCall struct {
	file string
	line int
	what string // a short description of the argument, for the report
}

// TestEveryProductionOpenGoesThroughTheHook (T5's spec name) walks every
// non-test Go file in this package and pins the rule above.
func TestEveryProductionOpenGoesThroughTheHook(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("list the package dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		files = append(files, n)
	}
	if len(files) < 10 {
		t.Fatalf("the walk saw only %d non-test files in %d total; the guard must see the whole package (got %d)", len(files), len(entries), len(files))
	}

	fset := token.NewFileSet()
	var found []storeOpenCall
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Open" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "store" {
				return true
			}
			if len(call.Args) != 1 {
				t.Errorf("%s:%d: store.Open with %d arguments; the door takes exactly one Options",
					name, fset.Position(call.Pos()).Line, len(call.Args))
				return true
			}
			what, ok := openDoorKind(call.Args[0])
			if !ok {
				t.Errorf("%s:%d: store.Open without the pre-migration door — pass storeOpenOptions(...), drillOpenOptions(...), or a store.Options literal with SkipMigrations: true. A door that skips the hook skips the safety net (BK6).",
					name, fset.Position(call.Pos()).Line)
			}
			found = append(found, storeOpenCall{
				file: name,
				line: fset.Position(call.Pos()).Line,
				what: what,
			})
			return true
		})
	}

	// The walk must have met the real doors: serve, the verbs' one shared
	// opener, and the drill's constructor. Fewer than three means the walk
	// is blind, not the package clean.
	if len(found) < 3 {
		t.Fatalf("the walk found only %d store.Open call(s) %v; serve, the verbs and the drill owe three — the guard is blind", len(found), found)
	}
	for _, c := range found {
		t.Logf("door: %s:%d — %s", c.file, c.line, c.what)
	}

	// The canary: the guard must refuse a bare literal and accept the two
	// sanctioned shapes. A guard that accepts the canary has no teeth.
	canaries := []struct {
		name string
		src  string
		want bool
	}{
		{"bare literal is refused", "store.Open(store.Options{})", false},
		{"a literal with a hook is still refused", "store.Open(store.Options{PreMigrate: h})", false},
		{"storeOpenOptions is accepted", "store.Open(storeOpenOptions(true, \"cli\"))", true},
		{"drillOpenOptions is accepted", "store.Open(drillOpenOptions(dir))", true},
		{"a SkipMigrations literal is accepted", "store.Open(store.Options{SkipMigrations: true})", true},
	}
	for _, c := range canaries {
		got := canaryOpen(t, c.src)
		if got != c.want {
			t.Errorf("canary %q: the guard says %v, want %v — the canary must prove the guard has teeth", c.name, got, c.want)
		}
	}
}

// openDoorKind classifies one store.Open argument against the rule. It
// reports a short description for the log and whether the argument is one
// of the sanctioned shapes.
func openDoorKind(arg ast.Expr) (string, bool) {
	switch a := arg.(type) {
	case *ast.CallExpr:
		if fn, ok := a.Fun.(*ast.Ident); ok {
			switch fn.Name {
			case "storeOpenOptions":
				return "storeOpenOptions (the hooked door)", true
			case "drillOpenOptions":
				return "drillOpenOptions (the drill's door — the one documented exception)", true
			}
			return "a call to " + fn.Name, false
		}
		return "an unnamed call", false
	case *ast.CompositeLit:
		sel, ok := a.Type.(*ast.SelectorExpr)
		if !ok {
			return "a non-store.Options literal", false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "store" || sel.Sel.Name != "Options" {
			return "a non-store.Options literal", false
		}
		for _, elt := range a.Elts {
			kev, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return "a store.Options literal with a positional element", false
			}
			key, ok := kev.Key.(*ast.Ident)
			if !ok || key.Name != "SkipMigrations" {
				continue
			}
			// The parser keeps the booleans as identifiers, not literals:
			// `true` is a pre-declared ident in the AST.
			switch v := kev.Value.(type) {
			case *ast.BasicLit:
				if v.Value != "true" {
					return "a store.Options literal with SkipMigrations set to something but true", false
				}
				return "a store.Options literal with SkipMigrations: true (a door with no migration to protect)", true
			case *ast.Ident:
				if v.Name != "true" {
					return "a store.Options literal with SkipMigrations set to something but true", false
				}
				return "a store.Options literal with SkipMigrations: true (a door with no migration to protect)", true
			}
			return "a store.Options literal with SkipMigrations set to an expression, not the literal true", false
		}
		return "a store.Options literal without the door", false
	default:
		return "an expression that is not a door", false
	}
}

// TestPreMigrationIgnoresTheSettingsTable (T10, BK6's static proof): the
// hook's Settings field in store_open.go — the ONE constructor for every
// hooked open — must be backup.NewEnvSettings(). The hook runs at the
// moment the setting table is inside the very database being copied from,
// so its read seam must be the one that cannot reach that table.
// EnvSettings has no database state at all (its only field is an env
// lookup), so a hook wired to it performs no database read by construction;
// this guard makes the wiring check static — an edit that swaps in
// NewConfigSettings is a diff a reviewer has to see, not a drift.
func TestPreMigrationIgnoresTheSettingsTable(t *testing.T) {
	src, err := os.ReadFile("store_open.go")
	if err != nil {
		t.Fatalf("read store_open.go: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "store_open.go", src, 0)
	if err != nil {
		t.Fatalf("parse store_open.go: %v", err)
	}
	checked := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewPreMigrate" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "backup" {
			return true
		}
		line := fset.Position(call.Pos()).Line
		if len(call.Args) < 1 {
			t.Errorf("store_open.go:%d: backup.NewPreMigrate without the Deps argument", line)
			return true
		}
		deps, ok := call.Args[0].(*ast.CompositeLit)
		if !ok {
			t.Errorf("store_open.go:%d: the hook's Deps is not a backup.Deps literal — the guard cannot check its Settings field", line)
			return true
		}
		checked++
		if !hookSettingsIsEnvSettings(deps) {
			t.Errorf("store_open.go:%d: the hook's Settings is %s, want backup.NewEnvSettings() — the hook runs before the setting table exists in the copy and must not read it (BK6)",
				line, settingsConstructor(deps))
		}
		return true
	})
	if checked == 0 {
		t.Fatal("store_open.go builds no backup.NewPreMigrate — the guard is blind to the wiring it exists to see")
	}

	// The canaries: the guard must accept the sanctioned constructor and
	// refuse a settings store or a bare variable — a guard that accepts
	// them has no teeth.
	canaries := []struct {
		name string
		src  string
		want bool
	}{
		{"EnvSettings is accepted", "backup.NewPreMigrate(backup.Deps{Settings: backup.NewEnvSettings()}, backup.PreMigrateOptions{})", true},
		{"a config store is refused", "backup.NewPreMigrate(backup.Deps{Settings: backup.NewConfigSettings(cs)}, backup.PreMigrateOptions{})", false},
		{"a bare variable is refused", "backup.NewPreMigrate(backup.Deps{Settings: s}, backup.PreMigrateOptions{})", false},
	}
	for _, c := range canaries {
		body := "package main\nimport backup \"github.com/cjlapao/aeon/internal/backup\"\nvar cs backup.Settings\nvar s backup.Settings\nvar _ = " + c.src + "\n"
		fset2 := token.NewFileSet()
		f2, err := parser.ParseFile(fset2, "canary.go", body, 0)
		if err != nil {
			t.Fatalf("parse the canary %q: %v", c.src, err)
		}
		var got bool = true
		found := false
		ast.Inspect(f2, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewPreMigrate" {
				return true
			}
			deps, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			got = hookSettingsIsEnvSettings(deps)
			found = true
			return false
		})
		if !found {
			t.Fatalf("the canary %q carried no backup.Deps literal", c.src)
		}
		if got != c.want {
			t.Errorf("canary %q: the guard says %v, want %v — the canary must prove the guard has teeth", c.name, got, c.want)
		}
	}
}

// hookSettingsIsEnvSettings checks one backup.Deps literal: it must set
// Settings, and the value must be exactly backup.NewEnvSettings() — the
// zero-argument constructor, no variable, no wrapper.
func hookSettingsIsEnvSettings(deps *ast.CompositeLit) bool {
	for _, elt := range deps.Elts {
		kev, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kev.Key.(*ast.Ident)
		if !ok || key.Name != "Settings" {
			continue
		}
		call, ok := kev.Value.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "backup" && sel.Sel.Name == "NewEnvSettings"
	}
	return false
}

// settingsConstructor names the Settings value a failed literal carried, for
// the error line: the constructor called, or the variable used.
func settingsConstructor(deps *ast.CompositeLit) string {
	for _, elt := range deps.Elts {
		kev, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kev.Key.(*ast.Ident)
		if !ok || key.Name != "Settings" {
			continue
		}
		if call, ok := kev.Value.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					return "a call to " + pkg.Name + "." + sel.Sel.Name + "(...)"
				}
			}
			return "an unnamed call"
		}
		if id, ok := kev.Value.(*ast.Ident); ok {
			return "the variable " + id.Name
		}
		return "an expression"
	}
	return "no Settings field at all"
}

// canaryOpen parses one expression's use as a store.Open argument and runs
// it through openDoorKind. The body is fixed and small; the only thing
// under test is the classifier.
func canaryOpen(t *testing.T, src string) bool {
	t.Helper()
	body := "package main\nimport store \"github.com/cjlapao/aeon/internal/store\"\nvar _ = " + src + "\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "canary.go", body, 0)
	if err != nil {
		t.Fatalf("parse the canary %q: %v", src, err)
	}
	var arg ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Open" {
			return true
		}
		arg = call.Args[0]
		return false
	})
	if arg == nil {
		t.Fatalf("the canary %q carried no store.Open argument", src)
	}
	_, ok := openDoorKind(arg)
	return ok
}
