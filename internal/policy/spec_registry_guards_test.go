package policy_test

// The structural guards over the policy package's own source.
//
// Three claims, each read off the AST rather than grepped (the house idiom,
// T2/T3/T4: a grep over text hits these very words in the guard's own
// comments), and each proven able to fire on synthetic source in its own
// self-test:
//
//   - nothing branches on the policy type (PL3): a later slice adds a type by
//     calling RegisterType, never by editing a `switch`, because the slice
//     that forgets a case gets a `default:` and a default that permits is how
//     an unimplemented type becomes a permission;
//   - registration happens in init and nowhere else (SEC-010): the registry is
//     a plain map read unsynchronised by every Evaluate, so a runtime write is
//     a data race;
//   - no package-level slice of rows and no blocklist-shaped name (Q1): the
//     default deny is PL5's decision, not a list compiled into the binary.
//
// The functions take source TEXT, not a filename, so a self-test can hand them
// exactly the code the rule forbids.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

func TestNoSwitchOnPolicyTypeAnywhereInThePackage(t *testing.T) {
	// The claim PL3's upward seam rests on: S11/S14/S16/S17/S18/S20 add a type
	// by arriving with a schema, a validator and an evaluator, and never by
	// editing a branch. A switch on the type is exactly the thing that rots —
	// the slice that forgets a case gets `default:`, and a default that permits
	// is how an unimplemented type becomes a permission.
	for _, file := range productionSources(t) {
		for _, hit := range findPolicyTypeSwitches(readSource(t, file)) {
			t.Errorf("%s branches on the policy type: %s; a type arrives by RegisterType, never by being "+
				"added to a switch (PL3) — and a default branch is where a permit hides", file, hit)
		}
	}
}
func TestTheNoSwitchGuardCanFail(t *testing.T) {
	// A guard that cannot fire guards nothing (T2's TestTheLockCanFail). Every
	// shape the engine must never grow is fed to the same finder as synthetic
	// source, alongside the shapes it must leave alone.
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"a switch on p.Type",
			"package policy\n\nfunc f(p Policy) int {\n\tswitch p.Type {\n\tcase ToolAccess:\n\t\treturn 1\n\t}\n\treturn 0\n}\n", true},
		{"a switch on r.Type by wire string",
			"package policy\n\nfunc f(r Request) int {\n\tswitch r.Type {\n\tcase \"command_access\":\n\t\treturn 1\n\t}\n\treturn 0\n}\n", true},
		{"a type switch",
			"package policy\n\nfunc f(v any) int {\n\tswitch v.(type) {\n\tcase string:\n\t\treturn 1\n\t}\n\treturn 0\n}\n", true},
		{"a switch whose case names a type",
			"package policy\n\nfunc f(x int) int {\n\tswitch x {\n\tcase CostCap:\n\t\treturn 1\n\t}\n\treturn 0\n}\n", true},
		{"the action switch the engine does have",
			"package policy\n\nfunc f(p Policy) {\n\tswitch p.Action {\n\tcase Deny:\n\t}\n}\n", false},
		{"the target-kind switch the engine does have",
			"package policy\n\nfunc f(t Target) {\n\tswitch t.Kind {\n\tcase AllAgents:\n\t}\n}\n", false},
		{"a reflect-kind switch in the schema reader",
			"package policy\n\nimport \"reflect\"\n\nfunc f(v reflect.Value) int {\n\tswitch v.Kind() {\n\tcase reflect.Pointer:\n\t\treturn 1\n\t}\n\treturn 0\n}\n", false},
		{"an expression-less switch",
			"package policy\n\nfunc f(n int) int {\n\tswitch {\n\tcase n > 0:\n\t\treturn 1\n\t}\n\treturn 0\n}\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := findPolicyTypeSwitches(c.src)
			if c.want && len(hits) == 0 {
				t.Errorf("the guard missed a switch on the policy type: %s", c.src)
			}
			if !c.want && len(hits) > 0 {
				t.Errorf("the guard cried wolf on legitimate code: %v", hits)
			}
		})
	}
}
func TestRegisterTypeIsCodeTimeOnly(t *testing.T) {
	// SEC-010. The registry is a plain map read by every Evaluate with no lock,
	// which is sound only because nothing writes it after init. A registration
	// during a request is a data race with a map that can be read half-written,
	// so it is refused here rather than discovered under load — the same
	// argument that keeps this package from reading a clock.
	//
	// Test files may register from a Test function (a package runs its tests one
	// at a time, and T3's dynamic-registration case needs it); production code
	// may register only from init.
	var offenders []string
	for _, file := range moduleSources(t) {
		isTest := strings.HasSuffix(file, "_test.go")
		for _, hit := range findRuntimeRegistrations(readSource(t, file), isTest) {
			offenders = append(offenders, fmt.Sprintf("%s %s", file, hit))
		}
	}
	for _, o := range offenders {
		t.Errorf("%s calls RegisterType outside init; the registry map is read unsynchronised by every "+
			"Evaluate, so a runtime registration is a data race (SEC-010) — register from a package init", o)
	}
}
func TestTheCodeTimeRegistrationGuardCanFail(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		isTest bool
		want   bool
	}{
		{
			"a handler that registers mid-request",
			"package policy\n\nfunc Handle(w Request) {\n\tRegisterType(ToolAccess, v{}, e{})\n}\n",
			false, true,
		},
		{
			"a qualified call from a method",
			"package other\n\nfunc (s *S) Start() {\n\tpolicy.RegisterType(ToolAccess, v{}, e{})\n}\n",
			false, true,
		},
		{
			"a package-level initializer",
			"package policy\n\nvar _ = func() { RegisterType(ToolAccess, v{}, e{}) }()\n",
			false, true,
		},
		{
			"the init the design wants",
			"package policy\n\nfunc init() {\n\tRegisterType(ToolAccess, v{}, e{})\n}\n",
			false, false,
		},
		{
			"a test registering a fixture type",
			"package policy_test\n\nfunc TestX(t *testing.T) {\n\tpolicy.RegisterType(ToolAccess, v{}, e{})\n}\n",
			true, false,
		},
		{
			"someone else's RegisterType, in an init",
			"package other\n\nfunc init() {\n\tthings.RegisterType(ToolAccess, v{}, e{})\n}\n",
			false, false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := findRuntimeRegistrations(c.src, c.isTest)
			if c.want && len(hits) == 0 {
				t.Errorf("the guard missed a runtime registration: %s", c.src)
			}
			if !c.want && len(hits) > 0 {
				t.Errorf("the guard cried wolf: %v", hits)
			}
		})
	}
}

// ── the source-reading guards, as pure functions over text ───────────────────

// productionSources lists the package's own non-test files: the guards bind the
// binary that ships, not the suite that checks it.
func productionSources(tb testing.TB) []string {
	tb.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		tb.Fatalf("read the package directory: %v", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		files = append(files, e.Name())
	}
	if len(files) < 5 {
		tb.Fatalf("only %d production files found; these guards would pass almost vacuously", len(files))
	}
	return files
}

// moduleSources lists every .go file in the module, the way T4's sole-formatter
// guard does: the registration rule binds the whole module, not one package.
func moduleSources(tb testing.TB) []string {
	tb.Helper()
	root := moduleRoot(tb)
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		tb.Fatalf("walk the module: %v", err)
	}
	if len(files) < 50 {
		tb.Fatalf("only %d files scanned under %s; this guard would pass almost vacuously", len(files), root)
	}
	return files
}

func readSource(tb testing.TB, path string) string {
	tb.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read %s: %v", path, err)
	}
	return string(src)
}

func parseSource(src string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "guard.go", src, 0)
	return f, fset, err
}

// packageLevelNames returns every package-level var/const name.
func packageLevelNames(src string) []string {
	f, _, err := parseSource(src)
	if err != nil {
		return nil
	}
	var names []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.VAR && gd.Tok != token.CONST) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if n.Name != "_" {
					names = append(names, n.Name)
				}
			}
		}
	}
	return names
}

// packageLevelRowSlices reports a package-level slice of Policy rows: the shape
// a built-in blocklist would take if one were ever added.
func packageLevelRowSlices(src string) []string {
	f, _, err := parseSource(src)
	if err != nil {
		return nil
	}
	var hits []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, value := range vs.Values {
				lit, ok := value.(*ast.CompositeLit)
				if !ok {
					continue
				}
				arr, ok := lit.Type.(*ast.ArrayType)
				if !ok || rowElementName(arr.Elt) != "Policy" {
					continue
				}
				hits = append(hits, fmt.Sprintf("var %s is a package-level slice of policy rows — a rule "+
					"built into the binary rather than authored by an operator", identNames(vs.Names)))
			}
		}
	}
	return hits
}

func rowElementName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return rowElementName(t.X)
	}
	return ""
}

func identNames(names []*ast.Ident) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n.Name)
	}
	return strings.Join(parts, ", ")
}

// policyTypeConsts are the seven Go constant names; their wire forms are read
// from the closed set itself. A switch naming either is a switch on the type.
var policyTypeConsts = []string{"ToolAccess", "CommandAccess", "RouteAccess", "EffectClass",
	"CostCap", "SkillAccess", "TriggerAccess"}

func policyWireValues() []string {
	var out []string
	for _, t := range policy.AllPolicyTypes() {
		out = append(out, string(t))
	}
	return out
}

// findPolicyTypeSwitches reports the switch statements a later slice would have
// to edit to add a type: a type switch, a switch on something's `.Type`, or a
// switch whose cases name one of the seven by constant or by wire string.
// Switches on the action and the target kind are legitimate — the other two
// closed sets, and neither grows by registration.
func findPolicyTypeSwitches(src string) []string {
	f, fset, err := parseSource(src)
	if err != nil {
		return []string{"unparseable source"}
	}
	wire := policyWireValues()
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.TypeSwitchStmt); ok {
			// A type switch is its own node, and it is the most absolute form of
			// "branch on what this is": the list of types lives in the branch.
			hits = append(hits, fmt.Sprintf("line %d: a type switch",
				fset.Position(n.Pos()).Line))
			return true
		}
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		where := fmt.Sprintf("line %d", fset.Position(sw.Pos()).Line)
		if sw.Tag == nil {
			return true // expression-less switch: an if-chain, no dimension switched on
		}
		if isTypeSwitch(sw.Tag) {
			hits = append(hits, where+": a type switch")
			return true
		}
		if sel, ok := sw.Tag.(*ast.SelectorExpr); ok && sel.Sel.Name == "Type" {
			hits = append(hits, where+": a switch on .Type")
		}
		for _, cl := range sw.Body.List {
			cc, ok := cl.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cc.List {
				name, literal := caseName(e)
				switch {
				case name == "" || literal && !slices.Contains(wire, name):
				case slices.Contains(policyTypeConsts, name):
					hits = append(hits, fmt.Sprintf("%s: a case naming %s", where, name))
				case literal:
					hits = append(hits, fmt.Sprintf("%s: a case naming the wire value %q", where, name))
				}
			}
		}
		return true
	})
	return hits
}

// isTypeSwitch reports a `switch x.(type)` — the other shape a per-type branch
// takes, and the one a map-based registry must never grow. The tag of a Go type
// switch is a statement or an expression depending on the form, hence ast.Node.
func isTypeSwitch(tag ast.Node) bool {
	switch t := tag.(type) {
	case *ast.AssignStmt:
		for _, rhs := range t.Rhs {
			if ta, ok := rhs.(*ast.TypeAssertExpr); ok && ta.Type == nil {
				return true
			}
		}
	case *ast.ExprStmt:
		if ta, ok := t.X.(*ast.TypeAssertExpr); ok && ta.Type == nil {
			return true
		}
	case *ast.TypeAssertExpr:
		return t.Type == nil
	}
	return false
}

// caseName returns the identifier a case names (bare or qualified) and whether
// it was written as a string literal.
func caseName(e ast.Expr) (string, bool) {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name, false
	case *ast.SelectorExpr:
		return t.Sel.Name, false
	case *ast.BasicLit:
		if t.Kind == token.STRING {
			if unquoted, err := strconv.Unquote(t.Value); err == nil {
				return unquoted, true
			}
		}
	}
	return "", false
}

// findRuntimeRegistrations reports RegisterType call sites that are not in an
// init (production files) or not in a Test function or init (test files),
// matching the bare and the qualified form alike. Each hit names the function
// or var it was found in; the caller names the file.
func findRuntimeRegistrations(src string, isTest bool) []string {
	f, _, err := parseSource(src)
	if err != nil {
		return []string{"unparseable source"}
	}
	var hits []string
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name == "init" || (isTest && strings.HasPrefix(d.Name.Name, "Test")) {
				continue
			}
			for range registerTypeCalls(d.Body) {
				hits = append(hits, fmt.Sprintf("inside func %s", d.Name.Name))
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range vs.Values {
					for range registerTypeCallsInExpr(value) {
						hits = append(hits, fmt.Sprintf("in package-level var %s", identNames(vs.Names)))
					}
				}
			}
		}
	}
	return hits
}

// registeredTypeNames returns, sorted, the type names production code registers.
func registeredTypeNames(tb testing.TB) []string {
	tb.Helper()
	var got []string
	for _, file := range productionSources(tb) {
		f, _, err := parseSource(readSource(tb, file))
		if err != nil {
			tb.Fatalf("%s: %v", file, err)
		}
		got = append(got, registerTypeTypeArgs(f)...)
	}
	slices.Sort(got)
	return got
}

func registerTypeCalls(body *ast.BlockStmt) []string {
	var hits []string
	if body == nil {
		return hits
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isRegisterTypeCall(call) {
			hits = append(hits, "call")
		}
		return true
	})
	return hits
}

func registerTypeCallsInExpr(e ast.Expr) []string {
	var hits []string
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isRegisterTypeCall(call) {
			hits = append(hits, "call")
		}
		return true
	})
	return hits
}

func isRegisterTypeCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "RegisterType"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "RegisterType"
	}
	return false
}

// registerTypeTypeArgs reads the first argument of each RegisterType call: the
// name of the constant it passes, or "<not-a-constant>" — which the enumeration
// also refuses, because a computed type is a type nobody can audit.
func registerTypeTypeArgs(f *ast.File) []string {
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isRegisterTypeCall(call) || len(call.Args) == 0 {
			return true
		}
		switch arg := call.Args[0].(type) {
		case *ast.Ident:
			names = append(names, arg.Name)
		case *ast.SelectorExpr:
			names = append(names, arg.Sel.Name)
		default:
			names = append(names, "<not-a-constant>")
		}
		return true
	})
	return names
}
