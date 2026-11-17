package main

// backup_parity_test.go — S62 T9: the family's structural guards, held
// by parsing the family's own source. The behavioural tests (the
// goldens, backup_cli_test.go) prove what the doors DO; these prove
// what the doors MAY NOT be:
//
//   - the socket's five methods and the CLI's verbs are the same set
//     plus restore, the one verb with no method (BK9: restore is the
//     person's door and never rides the socket);
//   - no store in the CLI's backup files except the direct door — the
//     socket-first verbs must stay clients of the wire, and the one
//     process that may open the store is the one the select fell back
//     to;
//   - the doctor's new legs are store-free at the type level:
//     internal/doctor has no store open of any kind (the pure
//     migration count is the one store fact it reads), and the doctor
//     verb's file never imports the store.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var bkMethodConstRE = regexp.MustCompile(`MethodBackup([A-Z][A-Za-z]*)`)

// bkProtocolBackupMethods scans the protocol's own source for the
// MethodBackup* constants and answers their verb names (the constant
// suffix, lower-cased): run, list, verify, status, prune. A method
// without a CLI verb is a wire door nobody can knock on; the reverse
// is held by the verb test below.
func bkProtocolBackupMethods(t *testing.T) []string {
	t.Helper()
	// The tests run in the package's directory (cmd/aeon); the protocol
	// package is two levels up. The MethodBackup* constants may stand in
	// any of its files, so the scan is the whole package, not one file.
	protoDir := filepath.Join("..", "..", "internal", "protocol")
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var verbs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(protoDir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if m := bkProtocolBackupMethodsIn(f); len(m) > 0 {
			for _, v := range m {
				if !seen[v] {
					seen[v] = true
					verbs = append(verbs, v)
				}
			}
		}
	}
	sort.Strings(verbs)
	return verbs
}

// bkProtocolBackupMethodsIn answers the MethodBackup* verb names one
// parsed file declares.
func bkProtocolBackupMethodsIn(f *ast.File) []string {
	var verbs []string
	seen := map[string]bool{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if m := bkMethodConstRE.FindStringSubmatch(name.Name); m != nil {
					verb := strings.ToLower(m[1])
					if !seen[verb] {
						seen[verb] = true
						verbs = append(verbs, verb)
					}
				}
			}
		}
	}
	sort.Strings(verbs)
	return verbs
}

// bkCLIBackupVerbs parses the family's parent (cmdBackup's switch) and
// answers its verb names, in the order the switch lists them.
func bkCLIBackupVerbs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "backup_restore.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var verbs []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "cmdBackup" {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, ex := range cc.List {
					lit, ok := ex.(*ast.BasicLit)
					if ok && lit.Kind == token.STRING {
						verbs = append(verbs, strings.Trim(lit.Value, `"`))
					}
				}
			}
			return true
		})
	}
	return verbs
}

// TestEveryBackupMethodHasACLIVerb: a wire method with no verb is a
// door the person cannot knock on from the terminal — and a verb the
// protocol does not name (restore) must be the only one.
func TestEveryBackupMethodHasACLIVerb(t *testing.T) {
	verbs := bkCLIBackupVerbs(t)
	verbSet := map[string]bool{}
	for _, v := range verbs {
		verbSet[v] = true
	}
	methods := bkProtocolBackupMethods(t)
	if len(methods) != 5 {
		t.Fatalf("the protocol's backup method set changed: %v — a new method is a new verb, in the same change", methods)
	}
	for _, m := range methods {
		if !verbSet[m] {
			t.Fatalf("the method %q has no CLI verb — the socket is the primary surface and the CLI must be able to reach it", m)
		}
	}
	// The reverse direction, with the one named exception: every verb
	// is a method's, except restore — BK9's door, the one that never
	// rides the socket, so the protocol must not grow a method for it.
	for _, v := range verbs {
		isMethod := false
		for _, m := range methods {
			if m == v {
				isMethod = true
				break
			}
		}
		if !isMethod && v != "restore" {
			t.Fatalf("the verb %q has no protocol method — a verb is either a socket client or restore, and a third kind is a second door", v)
		}
	}
	if !verbSet["restore"] {
		t.Fatalf("restore is the family's parent verb and must stand in the switch: %v", verbs)
	}
	// BK9 at the wire: no MethodBackupRestore exists, and none may.
	entries, err := os.ReadDir(filepath.Join("..", "..", "internal", "protocol"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "internal", "protocol", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "MethodBackupRestore") {
			t.Fatalf("the protocol grew a restore method — BK9: restore is the person's door and never rides the socket (it would be a second writer over the replaced database)")
		}
	}
}

// bkPackageFiles lists the package's non-test source files.
func bkPackageFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// bkImports answers the file's imports, by path.
func bkImports(t *testing.T, name string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}

// TestCLIBackupFilesNeverOpenStoreOutsideTheDirectDoor holds BK12's
// discipline at the level it is true: the FIVE wire verbs stay clients
// of the wire — the files that own them never open the store, and the
// one store a verb may see is the one the select handed it through the
// door. The two store touches the family legitimately carries are
// named where they stand: backup.go's is the db-info renderer's
// parameter type (a store the db verb opened and passed in), and
// backup_restore.go's is RESTORE's — BK9's door, which IS store-side,
// guarded by T7's tests, not here.
func TestCLIBackupFilesNeverOpenStoreOutsideTheDirectDoor(t *testing.T) {
	const storePkg = "github.com/cjlapao/aeon/internal/store"

	// The five-verb file: no store open of any kind, and no call to
	// the direct door's open — the only caller of that is selectDoor,
	// and it stands in this same file, so the count below is 1, not 0.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "backup.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	opens := 0
	directCalls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := ce.Fun.(type) {
		case *ast.Ident:
			if fun.Name == "openDirectDoor" {
				directCalls++
			}
		case *ast.SelectorExpr:
			if id, ok := fun.X.(*ast.Ident); ok && id.Name == "store" {
				if fun.Sel.Name == "Open" {
					opens++
				}
			}
		}
		return true
	})
	if opens != 0 {
		t.Fatalf("backup.go opens the store %d time(s) — the five verbs must stay clients of the wire; the store belongs to the direct door (SkipMigrations, once, under the select)", opens)
	}
	if directCalls != 1 {
		t.Fatalf("backup.go calls the direct door's open %d time(s), want 1 (the select's nobody-is-home branch) — a second call is a fallback the select does not speak for", directCalls)
	}

	// The family's parent: the dispatch opens nothing of the store's —
	// restore's store use (the drill's deps) lives in its own verb
	// function, which BK9 owns.
	f, err = parser.ParseFile(fset, "backup_restore.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	dispatchOpens := 0
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "cmdBackup" {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := ce.Fun.(type) {
			case *ast.Ident:
				if fun.Name == "openDirectDoor" || fun.Name == "openStoreSkipMigrations" {
					dispatchOpens++
				}
			case *ast.SelectorExpr:
				if id, ok := fun.X.(*ast.Ident); ok && id.Name == "store" && fun.Sel.Name == "Open" {
					dispatchOpens++
				}
			}
			return true
		})
	}
	if dispatchOpens != 0 {
		t.Fatalf("the backup dispatch opens the store %d time(s) — the five verbs are wire clients; a store in the dispatch is a second door", dispatchOpens)
	}

	// The direct door remains the one family file that may import the
	// store at all — it opens it itself, SkipMigrations, to be a door.
	direct := bkImports(t, "backup_direct.go")
	if !bkContains(direct, storePkg) {
		t.Fatalf("backup_direct.go lost its store import — the direct door must open the store itself, SkipMigrations, to be a door at all")
	}

	// And the one call site package-wide: openDirectDoor is called
	// exactly once, in the select. A call anywhere else is a fallback
	// the select does not speak for.
	calls := 0
	for _, name := range bkPackageFiles(t) {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == "openDirectDoor" {
				calls++
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("openDirectDoor has %d call sites, want 1 (the select's nobody-is-home branch) — every other call is a fallback the select does not speak for", calls)
	}
}

// bkContains is the small string-slice has (the package already owns the
// bare name in a production file).
func bkContains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// TestDoctorBackupLegsAreStoreFreeAtTheTypeLevel holds the doctor's
// store-free mandate at the level it can be held: internal/doctor has
// no store open of any kind (its one store fact is the pure migration
// count, a function that reads no file), and the doctor verb's file
// never imports the store — its legs are the wire (daemon up) or the
// package's own store-free facts (daemon down). The whole-CLI run's
// store is the SEED leg's, pre-existing: the seed family's direct door
// has always opened it, and T9's mandate is T9's legs, not the seed's
// (the goldens pin the world the whole-CLI run actually leaves).
func TestDoctorBackupLegsAreStoreFreeAtTheTypeLevel(t *testing.T) {
	const storePkg = "github.com/cjlapao/aeon/internal/store"

	// internal/doctor: no store.Open of any kind anywhere in the
	// package — the store import is for the pure Migrations() count.
	entries, err := os.ReadDir(filepath.Join("..", "..", "internal", "doctor"))
	if err != nil {
		t.Fatal(err)
	}
	importedStore := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join("..", "..", "internal", "doctor", e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "store.Open") {
			t.Fatalf("internal/doctor/%s opens the store — the doctor's facts are store-free; an open here is a migration waiting for a read", e.Name())
		}
		for _, imp := range bkImports(t, path) {
			if imp == storePkg {
				importedStore = true
			}
			if strings.HasPrefix(imp, "github.com/cjlapao/aeon/internal/protocol") ||
				strings.HasPrefix(imp, "github.com/cjlapao/aeon/cmd") {
				t.Fatalf("internal/doctor/%s imports %s — the facts package may not reach the wire or the CLI; the doctor is its client", e.Name(), imp)
			}
		}
	}
	if !importedStore {
		t.Fatalf("internal/doctor lost its store import — the pending count is store.Migrations() minus the applied, and a facts package that cannot see the migration set cannot say what is pending")
	}

	// The doctor verb's file: no store import at all. Its one store
	// touch (the mcp leg's SkipMigrations open) goes through the
	// shared helper the direct door owns — never migrated, and never
	// a store the doctor imports.
	for _, imp := range bkImports(t, "doctor.go") {
		if imp == storePkg {
			t.Fatalf("doctor.go imports the store — the doctor's legs are the wire or the store-free facts; the mcp leg's open is the shared SkipMigrations helper, which no file needs to import the store for")
		}
	}
}
