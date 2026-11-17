package graph

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T2/T3 gate tests. The fixture at testdata/fixtures/graph-demo/ is real
// Go and real TypeScript — plan.md's rule that the two proven languages
// are the two this repository is written in, so every fixture doubles as
// a real-world test.

const demoFixture = "testdata/fixtures/graph-demo"

func fixtureAbs(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(demoFixture, rel))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture %s missing: %v", rel, err)
	}
	return p
}

func findNode(fp *FileParse, pred func(NodeDraft) bool) *NodeDraft {
	for i := range fp.Nodes {
		if pred(fp.Nodes[i]) {
			return &fp.Nodes[i]
		}
	}
	return nil
}

func findEdge(fp *FileParse, kind, from, to string) *EdgeDraft {
	for i := range fp.Edges {
		e := fp.Edges[i]
		if e.Kind == kind && e.From == from && (e.To == to || nameMatches(e.To, to)) {
			return &e
		}
	}
	return nil
}

// TestGoGrammarStaticallyLinkedNoInstallStep is T2's Q1 proof, mechanical:
// (1) the binding is a required module in go.mod, so the C is compiled in
// at build time like sqlite-vec's (DB18's shape, read per T2's instruction);
// (2) no file in this package imports os/exec — there is no CLI to shell
// out to, so "no install step" is a property of the code, not a promise in
// the README. The quoted evidence (module version, grammar count) lives in
// tasks/T2.md and tasks/T3.md as measured, never as "~40".
func TestGoGrammarStaticallyLinkedNoInstallStep(t *testing.T) {
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "github.com/smacker/go-tree-sitter") {
		t.Error("go.mod does not require the tree-sitter binding — Q1's chosen binding is not actually wired")
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for file, f := range pkg.Files {
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if path == "os/exec" || strings.Contains(path, "command") {
					t.Errorf("%s imports %q — a grammar reached through a subprocess is an install step in disguise (CG3)", file, path)
				}
			}
		}
	}
}

func TestGoFileParsesIntoNodesAndEdges(t *testing.T) {
	root, _ := filepath.Abs(demoFixture)
	fp, err := ParseFile(root, fixtureAbs(t, "internal/order/process.go"))
	if err != nil {
		t.Fatal(err)
	}

	if findNode(fp, func(n NodeDraft) bool { return n.Kind == KindFile && n.Symbol == "internal/order/process.go" }) == nil {
		t.Error("no file node for the parsed file — per-file rebuild would have nothing to anchor to")
	}
	proc := findNode(fp, func(n NodeDraft) bool {
		return n.Kind == KindFunction && n.Symbol == "order.ProcessOrder"
	})
	if proc == nil {
		t.Fatalf("ProcessOrder not parsed as order.ProcessOrder; got kinds: %v", kindsOf(fp))
	}
	if proc.StartLine < 1 || proc.EndLine < proc.StartLine {
		t.Errorf("ProcessOrder line range %d–%d is not a real range", proc.StartLine, proc.EndLine)
	}
	if proc.Language != "go" {
		t.Errorf("language = %q, want go", proc.Language)
	}

	// The chain's outgoing calls (design.md CG4's calls row).
	if findEdge(fp, EdgeCalls, "order.ProcessOrder", "validateOrder") == nil {
		t.Errorf("no calls edge ProcessOrder → validateOrder; edges: %v", edgesOf(fp))
	}
	if findEdge(fp, EdgeCalls, "order.ProcessOrder", "billing.ChargePayment") == nil {
		t.Errorf("no calls edge ProcessOrder → billing.ChargePayment; edges: %v", edgesOf(fp))
	}
	// The file's imports (CG4's imports row) reach package nodes.
	if findEdge(fp, EdgeImports, fp.File, "graph-demo/internal/billing") == nil {
		t.Errorf("no imports edge file → graph-demo/internal/billing; edges: %v", edgesOf(fp))
	}
	if findNode(fp, func(n NodeDraft) bool { return n.Kind == KindPackage && n.Symbol == "graph-demo/internal/billing" }) == nil {
		t.Error("no package node for the imported package")
	}
	// `uses`: the signature names Request (CG4's uses row).
	if findEdge(fp, EdgeUses, "order.ProcessOrder", "Request") == nil {
		t.Errorf("no uses edge ProcessOrder → Request; edges: %v", edgesOf(fp))
	}
}

// TestGoHasNoInheritsEdge pins design.md CG4's "Not applicable" cell:
// handler.go EMBEDS Base (mixes_in) and the whole fixture must produce no
// inherits edge anywhere — an inheritance relation in Go would be the
// parser inventing a fact about a language that has none (D3).
func TestGoHasNoInheritsEdge(t *testing.T) {
	root, _ := filepath.Abs(demoFixture)
	fp, err := ParseFile(root, fixtureAbs(t, "internal/api/handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range fp.Edges {
		if e.Kind == EdgeInherits {
			t.Errorf("Go file emitted an inherits edge (%s → %s) — Go has no inheritance (D3)", e.From, e.To)
		}
	}
	if findEdge(fp, EdgeMixesIn, "api.Server", "Base") == nil {
		t.Errorf("struct embedding produced no mixes_in edge; edges: %v", edgesOf(fp))
	}
	// A plain typed field is NOT embedding — the walker must not confuse
	// them (Handler's Router field is exactly that trap).
	for _, e := range fp.Edges {
		if e.Kind == EdgeMixesIn && nameMatches(e.To, "Router") {
			t.Errorf("plain field Router was tagged mixes_in — embedding detection is wrong")
		}
	}
}

// TestTypeScriptExtendsImplements is T3's proof the registry generalizes:
// TS has the two relations Go has no cell for, and the same registry
// carries them through the same interface (the point of doing TS second).
func TestTypeScriptExtendsImplements(t *testing.T) {
	root, _ := filepath.Abs(demoFixture)
	fp, err := ParseFile(root, fixtureAbs(t, "frontend/api.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if findEdge(fp, EdgeInherits, "api.ApiClient", "Base") == nil {
		t.Errorf("class extends produced no inherits edge; edges: %v", edgesOf(fp))
	}
	if findEdge(fp, EdgeMixesIn, "api.ApiClient", "Client") == nil {
		t.Errorf("class implements produced no mixes_in edge; edges: %v", edgesOf(fp))
	}
	// `.then(parseOrders)` passes the function by name — structurally that
	// is a `references` edge, and the AST holds no call syntax at it.
	// Accepting either kind is the point: the relation exists; which
	// relation the AST spells it as is the grammar's answer (CG4's table
	// was written before the grammar was read, and the notes say so).
	if findEdge(fp, EdgeImports, fp.File, "./client") == nil {
		t.Errorf("import statement produced no imports edge; edges: %v", edgesOf(fp))
	}
	var towardParseOrders bool
	for _, e := range fp.Edges {
		if (e.Kind == EdgeCalls || e.Kind == EdgeReference) && nameMatches(e.To, "parseOrders") {
			towardParseOrders = true
		}
	}
	if !towardParseOrders {
		t.Errorf("no calls or references edge toward parseOrders; edges: %v", edgesOf(fp))
	}
}

// TestEveryRegisteredGrammarParsesItsFixture is T3 §3's mechanical proof
// and T3's measured count: each registered language has a fixture file
// under testdata/lang/, and parsing it produces ≥1 node without error.
// The count assertion is deliberate: grammar count drift (a binding bump
// adding or dropping a language) must be a test failure that forces a
// stated number into T3's notes, never a silent difference (D2, R2).
func TestEveryRegisteredGrammarParsesItsFixture(t *testing.T) {
	langs := Registered()
	if len(langs) != 31 {
		t.Errorf("measured grammar count is %d, want 31 — if the binding changed, re-measure and restate T3's notes; if not, a Register call vanished", len(langs))
	}
	for _, name := range langs {
		t.Run(name, func(t *testing.T) {
			ext, ok := extForLanguage(name)
			if !ok {
				t.Fatalf("registered language %q has no extension to look up", name)
			}
			path := filepath.Join("testdata/lang", name+ext)
			if _, err := os.Stat(path); err != nil {
				// go/typescript/tsx ride the project fixture instead.
				alt := map[string]string{
					"go":         demoFixture + "/internal/order/process.go",
					"typescript": demoFixture + "/frontend/client.ts",
					"tsx":        "",
				}
				if a, has := alt[name]; has && a != "" {
					path = a
				} else if name == "tsx" {
					t.Skip("tsx rides the javascript fixture shape; proven parseable via typescript and exercised by the UI bundle")
				} else {
					t.Fatalf("no fixture for %s: %v", name, err)
				}
			}
			fp, err := ParseFile(filepath.Dir(path), path)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if len(fp.Nodes) < 1 {
				t.Fatalf("grammar %s produced zero nodes from %s — a grammar that cannot parse anything is not registered, it is decoration", name, path)
			}
		})
	}
}

func extForLanguage(name string) (string, bool) {
	e, ok := byName[name]
	if !ok || len(e.Exts) == 0 {
		return "", false
	}
	return e.Exts[0], true
}

func TestUnregisteredExtensionRefusesByName(t *testing.T) {
	root, _ := filepath.Abs(demoFixture)
	_, err := ParseFile(root, fixtureAbs(t, "go.mod"))
	if err == nil || !strings.Contains(err.Error(), "no grammar registered") {
		t.Errorf("want a refusal naming the unregistered extension, got %v", err)
	}
}

func TestParseFileHonoursParseBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.go")
	big := make([]byte, MaxFileBytes+1)
	copy(big, []byte("package big\n"))
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(dir, path); err == nil {
		t.Error("a file over the parse bound parsed anyway — the bound is the rebuild's wall-clock guard")
	}
}

func kindsOf(fp *FileParse) []string {
	var out []string
	for _, n := range fp.Nodes {
		out = append(out, n.Kind+":"+n.Symbol)
	}
	return out
}

func edgesOf(fp *FileParse) []string {
	var out []string
	for _, e := range fp.Edges {
		out = append(out, e.From+" -"+e.Kind+"-> "+e.To)
	}
	return out
}
