package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

// ParseFile turns one source file into node and edge drafts — the tree
// traversal plan.md §3 promised ("tree-sitter already produced an AST;
// walking it into rows is a tree traversal, not a search problem").
// Nothing here touches the store: parsing produces DRAFTS, and build.go's
// resolver is what turns symbolic references into rows with real ids. That
// split is what lets T4 re-link without re-parsing the world.

// NodeDraft is one symbol with its location, before resolution. Symbol is
// already qualified (lang_go.go and lang_typescript.go state each
// language's qualification rule); Kind is one of this package's Kind
// constants.
type NodeDraft struct {
	File      string
	Kind      string
	Symbol    string
	Language  string
	StartLine int
	EndLine   int
}

// EdgeDraft is one symbolic relation, before resolution: From is a symbol
// THIS file just drafted (or its file node), To is a candidate symbol
// text. Resolution — same-file exact hit, multi-candidate guess, or
// nothing — happens in build.go, and the extracted/inferred tag is decided
// THERE, because only the resolver knows whether the target lives in this
// file or across a boundary (D4). An unresolvable To is dropped, never
// written dangling: an edge to nothing is a structural claim the graph
// cannot support.
type EdgeDraft struct {
	From string
	Kind string
	To   string
}

// FileParse is one file's whole parse: the drafts, deduplicated. The File
// node (Kind "file", symbol = the project-relative path) is always emitted
// first — it is what per-file rebuild anchors to (T4) and what `imports`
// edges leave from (an import is a file-level fact in every grammar that
// has one).
type FileParse struct {
	File  string
	Nodes []NodeDraft
	Edges []EdgeDraft
}

// MaxFileBytes bounds what the parser is fed. A file above it is skipped
// like an unregistered extension is: the honest answer for a 40 MB
// generated bundle is "no nodes from this", not a multi-second stall in a
// rebuild the loop is waiting on. (Vendored code deserves the same
// treatment as generated code; both are structure nobody is asking about.)
const MaxFileBytes = 2 << 20 // 2 MiB

// parseTimeout bounds one file's parse. tree-sitter parses fast; this is
// the guard against a pathological grammar input hanging a rebuild step,
// so a bad file becomes "no nodes from this" rather than a frozen rebuild.
const parseTimeout = 10 * time.Second

// ParseFile parses one file by path. root is the project root the file's
// relative row key is computed against; absPath is read from disk.
func ParseFile(root, absPath string) (*FileParse, error) {
	entry, ok := LookupByPath(absPath)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, absPath)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("graph parse stat %s: %w", absPath, err)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("graph parse %s: file is %d bytes, over the %d-byte parse bound — skipped like an unknown extension", absPath, info.Size(), MaxFileBytes)
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("graph parse read %s: %w", absPath, err)
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("graph parse %s: not under project root %s", absPath, root)
	}
	return parseSource(entry, filepath.ToSlash(rel), src)
}

// parseSource is the whole of it: feed the bytes to the statically-linked
// grammar (CG2, CG3 — no CLI, no network, no load step), then walk. A nil
// tree or a nil root is an infrastructure error naming the file; a root
// with zero named children is a legal empty file, not an error.
func parseSource(entry Entry, relFile string, src []byte) (*FileParse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), parseTimeout)
	defer cancel()

	p := sitter.NewParser()
	p.SetLanguage(entry.Language())
	tree, err := p.ParseCtx(ctx, nil, src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		return nil, fmt.Errorf("graph parse %s: tree-sitter produced no tree (extension %s, grammar %s): %v", relFile, extOf(relFile), entry.Name, err)
	}

	e := &Emitter{file: relFile, lang: entry.Name}
	e.Node(KindFile, relFile, 1, countLines(src))

	if entry.Walk != nil {
		entry.Walk(tree.RootNode(), src, e)
	} else {
		genericWalk(tree.RootNode(), src, e, entry.Maps)
	}
	nodes := e.dedupe()
	return &FileParse{File: relFile, Nodes: nodes, Edges: e.edges}, nil
}

// Emitter is what a walker writes into. It owns dedupe; walkers own meaning.
type Emitter struct {
	file  string
	lang  string
	nodes []NodeDraft
	edges []EdgeDraft
}

// Node records one symbol. startLine/endLine are 1-based and inclusive.
func (e *Emitter) Node(kind, symbol string, startLine, endLine int) {
	if symbol == "" || kind == "" || startLine < 1 || endLine < startLine {
		return // a half-known symbol is not a node; silence here is cheaper than a wrong row
	}
	e.nodes = append(e.nodes, NodeDraft{
		File: e.file, Kind: kind, Symbol: symbol, Language: e.lang,
		StartLine: startLine, EndLine: endLine,
	})
}

// Edge records one symbolic relation. Both endpoints must be symbols this
// parse is claiming; the resolver later decides whether the edge is real.
func (e *Emitter) Edge(kind, from, to string) {
	if kind == "" || from == "" || to == "" || from == to {
		return
	}
	e.edges = append(e.edges, EdgeDraft{From: from, Kind: kind, To: to})
}

// dedupe collapses identical nodes and identical (from, kind, to) edges —
// two AST shapes may legitimately describe the same symbol (a Go
// type_declaration wrapping a struct_type, for instance), and two
// identical rows would make `ambiguous` fire on a parse artifact rather
// than on the code.
func (e *Emitter) dedupe() []NodeDraft {
	seenN := map[NodeDraft]bool{}
	nodes := e.nodes[:0]
	for _, n := range e.nodes {
		if seenN[n] {
			continue
		}
		seenN[n] = true
		nodes = append(nodes, n)
	}
	// Re-dedupe into a fresh slice: writing back onto e.nodes[:0] while
	// ranging over the same memory is the kind of aliasing that passes
	// today and corrupts tomorrow.
	out := make([]NodeDraft, 0, len(nodes))
	out = append(out, nodes...)

	seenE := map[EdgeDraft]bool{}
	edges := make([]EdgeDraft, 0, len(e.edges))
	for _, ed := range e.edges {
		if seenE[ed] {
			continue
		}
		seenE[ed] = true
		edges = append(edges, ed)
	}
	e.edges = edges
	return out
}

// Edges returns the deduplicated edge drafts (dedupe() must have run —
// parseSource always runs it).
func (f *FileParse) EdgesList() []EdgeDraft { return f.Edges }

// --- shared walker helpers -------------------------------------------------

// text is a node's source, single-line and bounded — callee and import
// paths are short, and a node whose text blew past the bound is telling us
// something is wrong with which node we reached for.
func text(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	t := string(n.Content(src))
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	if len(t) > 160 {
		t = t[:160]
	}
	return t
}

// childText reads a field's text, or falls back to the first named child
// matching one of the wanted node types.
func childText(n *sitter.Node, src []byte, field string, wantTypes ...string) string {
	if n == nil {
		return ""
	}
	if field != "" {
		if c := n.ChildByFieldName(field); c != nil {
			return text(c, src)
		}
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		for _, w := range wantTypes {
			if c.Type() == w {
				return text(c, src)
			}
		}
	}
	return ""
}

func startLine(n *sitter.Node) int { return int(n.StartPoint().Row) + 1 }
func endLine(n *sitter.Node) int   { return int(n.EndPoint().Row) + 1 }

func extOf(path string) string { return strings.ToLower(filepath.Ext(path)) }

func countLines(src []byte) int {
	return strings.Count(string(src), "\n") + 1
}

// genericWalk is the bulk walker (T3): every registered language without a
// bespoke Walk gets its function/type/import/call node types from Maps,
// so registering a grammar is data, not code. It descends the whole tree
// iteratively (a pathological deep file must not blow the goroutine
// stack), tracking the enclosing declared symbol so nested functions land
// under their parent's qualified name.
func genericWalk(root *sitter.Node, src []byte, e *Emitter, m *Maps) {
	type frame struct {
		n     *sitter.Node
		scope string
	}
	stack := []frame{{n: root, scope: stemOf(e.file)}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.n

		kind := ""
		sym := ""
		switch {
		case contains(m.FunctionTypes, n.Type()):
			kind, sym = KindFunction, qualify(f.scope, nameOf(n, src))
		case contains(m.TypeTypes, n.Type()):
			kind, sym = KindType, qualify(f.scope, nameOf(n, src))
		}
		scope := f.scope
		if sym != "" {
			e.Node(kind, sym, startLine(n), endLine(n))
			scope = sym
		}

		switch {
		case contains(m.ImportTypes, n.Type()):
			path := importPath(n, src)
			if path != "" {
				// The package node carries the import path as its file:
				// an external package has no file IN this project, and a
				// row key that said one would be a lie.
				e.Node(KindPackage, path, 1, 1)
				e.Edge(EdgeImports, e.file, path)
			}
		case contains(m.CallTypes, n.Type()):
			callee := childText(n, src, m.CallTargetField)
			if callee == "" {
				callee = firstNamedText(n, src)
			}
			if callee != "" {
				e.Edge(EdgeCalls, scope, callee)
			}
		}

		for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
			if c := n.NamedChild(i); c != nil {
				stack = append(stack, frame{n: c, scope: scope})
			}
		}
	}
}

// nameOf pulls a declaration's name: the "name" field first (tree-sitter's
// convention), then the first identifier-ish child.
func nameOf(n *sitter.Node, src []byte) string {
	if t := childText(n, src, "name", "identifier", "type_identifier", "property_identifier", "constant"); t != "" {
		return t
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		if strings.Contains(c.Type(), "identifier") || c.Type() == "constant" || c.Type() == "name" {
			return text(c, src)
		}
	}
	return ""
}

func firstNamedText(n *sitter.Node, src []byte) string {
	if n.NamedChildCount() > 0 {
		return text(n.NamedChild(0), src)
	}
	if n.ChildCount() > 0 {
		return text(n.Child(0), src)
	}
	return ""
}

// importPath squeezes the path out of an import node: the quoted string if
// the grammar keeps one, else the identifier text of its last significant
// child (Rust's use_declaration has no string to quote).
func importPath(n *sitter.Node, src []byte) string {
	var sb strings.Builder
	var walkImport func(*sitter.Node)
	walkImport = func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch {
		case strings.Contains(x.Type(), "string"):
			s := strings.Trim(text(x, src), `"'`)
			if s != "" {
				sb.WriteString(s)
			}
			return
		case x.Type() == "escape_sequence":
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			walkImport(x.NamedChild(i))
		}
	}
	walkImport(n)
	if s := sb.String(); s != "" {
		return s
	}
	// No string: take the dotted identifier chain.
	var parts []string
	var collect func(*sitter.Node)
	collect = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if x.ChildCount() == 0 || strings.Contains(x.Type(), "identifier") {
			if t := text(x, src); t != "" {
				parts = append(parts, t)
			}
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			collect(x.NamedChild(i))
		}
	}
	collect(n)
	return strings.Join(parts, ".")
}

// stemOf is the generic walker's module prefix: the file's basename minus
// extension. "src/sub/a.py" declares foo → "a.foo". The two bespoke
// walkers use their own rule where the language dictates one (Go names by
// its package clause). Bare-name queries still reach these symbols:
// nameMatches is how the query layer reads a final component.
func stemOf(relFile string) string {
	base := filepath.Base(relFile)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// nameMatches is D8's "exact-name match" for the query layer: a query
// hits a qualified symbol either as written ("order.ProcessOrder") or as
// the final component of it ("ProcessOrder"). Two files both declaring
// Validate then produce two nameMatches — which is exactly the ambiguous
// state DEMO §4 walks, and the reason the answer lists every candidate
// instead of silently picking one (D4's tag, read at query time).
func nameMatches(symbol, query string) bool {
	return symbol == query || strings.HasSuffix(symbol, "."+query)
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// qualify builds a nested symbol name. scope is either a file (a module-
// level declaration — the name is kept as written, e.g. "ProcessOrder" or
// "mod.fn" per the language's own rule above it) or an enclosing symbol
// (a nested declaration — the name hangs under its parent with a dot).
func qualify(scope, name string) string {
	if name == "" {
		return ""
	}
	return scope + "." + name
}
