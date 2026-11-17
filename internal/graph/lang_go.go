package graph

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
)

// Go — the first language proven end to end (T2), chosen because this
// repository is written in it, so every fixture doubles as a real-world
// test (plan.md "How we will do it").
//
// # The per-language mapping, as built (design.md CG4's Go column)
//
//   - calls     → call_expression (identifier callee, or selector_expression)
//   - imports   → import_declaration / import_spec inside import_group
//   - inherits  → NOT EMITTED, EVER. Go has no inheritance (D3); the test
//     parse_test.TestGoHasNoInheritsEdge pins this against a file using
//     struct embedding, which must produce mixes_in, never inherits.
//   - mixes_in  → struct embedding (field_declaration carrying a type and no
//     field name) AND interface embedding (a bare type inside interface_type
//     — the closest structural match there, recorded here as the Q3 answer
//     design.md's table left open)
//   - uses      → type_identifier anywhere (signature, field, body)
//   - references→ identifier uses of declared names not covered above
//
// # Symbol qualification (Q3's other half)
//
// A package-level function Foo in package order is "order.Foo"; a method
// is "order.Recv.Method"; a file is its project-relative path; an imported
// package is its import path verbatim. The rule is stated here rather than
// discovered per task because the query layer's nameMatches reads it: the
// DEMO asks explain("ProcessOrder") bare and re-asks fully qualified.

func init() {
	Register(Entry{
		Name:     "go",
		Exts:     []string{".go"},
		Language: golang.GetLanguage,
		Walk:     walkGo,
	})
}

func walkGo(root *sitter.Node, src []byte, e *Emitter) {
	pkg := goPackageName(root, src, e.file)

	// Pass one: the declared names of THIS file. The references pass needs
	// them so an identifier becomes a structural edge only when it names
	// something declared here — `sum` as a local is not a reference to a
	// function, and without this pass every local would claim one.
	declared := map[string]bool{}
	declared[e.file] = true // the file node names itself
	for i := 0; i < int(root.NamedChildCount()); i++ {
		decl := root.NamedChild(i)
		if decl == nil {
			continue
		}
		switch decl.Type() {
		case "function_declaration":
			if n := childText(decl, src, "name", "identifier"); n != "" {
				e.Node(KindFunction, pkg+"."+n, startLine(decl), endLine(decl))
				declared[pkg+"."+n] = true
				declared[n] = true
			}
		case "method_declaration":
			n := childText(decl, src, "name", "identifier")
			r := goReceiverName(decl, src)
			if n != "" && r != "" {
				e.Node(KindMethod, pkg+"."+r+"."+n, startLine(decl), endLine(decl))
				declared[pkg+"."+r+"."+n] = true
				declared[n] = true
			}
		case "type_declaration":
			// tree-sitter-go nests the name inside a type_spec (measured
			// against this binding's grammar, not guessed): `type X
			// struct{…}` is type_declaration › type_spec › {name, type}.
			// A grouped `type ( … )` holds several specs.
			for i := 0; i < int(decl.NamedChildCount()); i++ {
				spec := decl.NamedChild(i)
				if spec == nil || spec.Type() != "type_spec" {
					continue
				}
				n := childText(spec, src, "name", "type_identifier", "identifier")
				if n == "" {
					continue
				}
				kind := KindType
				if t := spec.ChildByFieldName("type"); t != nil && t.Type() == "interface_type" {
					kind = KindInterface
				}
				e.Node(kind, pkg+"."+n, startLine(decl), endLine(decl))
				declared[pkg+"."+n] = true
				declared[n] = true
			}
		}
	}

	// Pass two: the relations, one DFS with the enclosing declared symbol
	// as the edge's origin.
	type frame struct {
		n     *sitter.Node
		scope string
	}
	stack := []frame{{n: root, scope: e.file}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.n
		scope := f.scope

		switch n.Type() {
		case "function_declaration":
			if name := childText(n, src, "name", "identifier"); name != "" {
				scope = pkg + "." + name
			}
		case "method_declaration":
			name := childText(n, src, "name", "identifier")
			r := goReceiverName(n, src)
			if name != "" && r != "" {
				scope = pkg + "." + r + "." + name
			}
		case "type_declaration":
			// The scope inside a type body is the type itself, so an
			// embedded field's mixes_in names the type that embeds, not
			// the file (measured: without this the edge hung off the
			// file node — right relation, wrong subject).
			for i := 0; i < int(n.NamedChildCount()); i++ {
				spec := n.NamedChild(i)
				if spec != nil && spec.Type() == "type_spec" {
					if tn := childText(spec, src, "name", "type_identifier"); tn != "" {
						scope = pkg + "." + tn
					}
				}
			}
		case "import_declaration", "import_spec":
			path := goImportPath(n, src)
			if path != "" {
				e.Node(KindPackage, path, 1, 1)
				e.Edge(EdgeImports, e.file, path)
			}
		case "call_expression":
			callee := goCallee(n, src)
			if callee != "" {
				e.Edge(EdgeCalls, scope, callee)
			}
		case "type_identifier":
			// A name inside a type_declaration's own `name` field is the
			// declaration, not a use of itself.
			if !inField(n, "name") {
				t := text(n, src)
				if t != "" && t != pkg && declared[t] {
					e.Edge(EdgeUses, scope, t)
				}
			}
		case "identifier":
			if t := text(n, src); t != "" && t != pkg && declared[t] && refPosition(n) {
				e.Edge(EdgeReference, scope, t)
			}
		case "field_declaration":
			// Go's embedding (D3's mixes_in, and design.md's "closest
			// structural match" note): a field with a type and no field
			// name. Never inherits — the CHECK and the test both refuse.
			if n.ChildByFieldName("name") == nil {
				emb := goEmbeddedName(n, src)
				if emb != "" {
					e.Edge(EdgeMixesIn, scope, emb)
				}
			}
		case "interface_type":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				if c == nil {
					continue
				}
				if c.Type() == "type_identifier" || c.Type() == "qualified_type" {
					// Interface embedding: mixes_in (recorded above).
					if emb := text(c, src); emb != "" {
						e.Edge(EdgeMixesIn, scope, strings.TrimPrefix(emb, "*"))
					}
				}
			}
		}

		for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
			if c := n.NamedChild(i); c != nil {
				stack = append(stack, frame{n: c, scope: scope})
			}
		}
	}
}

// goPackageName reads the package clause — the qualifier every symbol in
// the file hangs under. A file with no clause (a testdata fragment) falls
// back to the file stem: a name is needed, and the stem is the nearest
// honest one.
func goPackageName(root *sitter.Node, src []byte, fileRel string) string {
	for i := 0; i < int(root.NamedChildCount()); i++ {
		c := root.NamedChild(i)
		if c != nil && c.Type() == "package_clause" {
			// Measured: this grammar names the clause's child
			// package_identifier, not identifier.
			id := firstTyped(c, "package_identifier")
			if id == nil {
				id = firstTyped(c, "identifier")
			}
			if id != nil {
				if t := text(id, src); t != "" {
					return t
				}
			}
		}
	}
	return stemOf(fileRel)
}

// goReceiverName pulls the receiver type off a method_declaration:
// func (r *Repo) Method() → "Repo".
func goReceiverName(m *sitter.Node, src []byte) string {
	recv := m.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	var t string
	var find func(*sitter.Node)
	find = func(x *sitter.Node) {
		if x == nil || t != "" {
			return
		}
		if x.Type() == "type_identifier" {
			t = text(x, src)
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			find(x.NamedChild(i))
		}
	}
	find(recv)
	return strings.TrimPrefix(t, "*")
}

// goCallee names what a call_expression calls: `Foo(...)` → "Foo";
// `pkg.Foo(...)` → "pkg.Foo"; `r.Method(...)` → "r.Method" (the resolver
// tries it whole, then by final component — r is a variable, not a
// package, so only the suffix can hit, which is what inferred is for).
func goCallee(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return text(fn, src)
	case "selector_expression":
		sel := fn.ChildByFieldName("selector")
		if sel == nil {
			return text(fn, src)
		}
		operand := fn.ChildByFieldName("operand")
		// pkg.Foo keeps its qualifier; r.Method keeps the text and lets
		// the resolver fall through to the suffix.
		return text(operand, src) + "." + text(sel, src)
	default:
		t := text(fn, src)
		if i := strings.LastIndexByte(t, '.'); i >= 0 {
			return t[i+1:]
		}
		return t
	}
}

// goImportPath squeezes the quoted path from an import node (single or
// spec inside a group).
func goImportPath(n *sitter.Node, src []byte) string {
	var out string
	var find func(*sitter.Node)
	find = func(x *sitter.Node) {
		if x == nil || out != "" {
			return
		}
		if strings.Contains(x.Type(), "string") {
			out = strings.Trim(text(x, src), `"`)
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			find(x.NamedChild(i))
		}
	}
	find(n)
	return out
}

// goEmbeddedName names the embedded type of a nameless field_declaration:
// `Repo` or `*Repo` or `pkg.Type`.
func goEmbeddedName(field *sitter.Node, src []byte) string {
	var out string
	var find func(*sitter.Node)
	find = func(x *sitter.Node) {
		if x == nil || out != "" {
			return
		}
		switch x.Type() {
		case "type_identifier", "qualified_type":
			out = strings.TrimPrefix(text(x, src), "*")
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			find(x.NamedChild(i))
		}
	}
	find(field)
	return out
}

// refPosition keeps `references` to real uses: skip the identifier that IS
// a declaration's name, a selector (the call already emitted its edge), a
// field/argument label, and the package clause.
func refPosition(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return true
	}
	switch p.Type() {
	case "package_clause":
		return false
	}
	if isFieldOf(n, p, "name", "selector", "field", "operator", "package") {
		return false
	}
	switch p.Type() {
	case "method_spec", "field_declaration", "parameter_declaration", "method_declaration":
		return false
	}
	return true
}

// --- small AST helpers shared by this file ---------------------------------

func inField(n *sitter.Node, field string) bool {
	p := n.Parent()
	return p != nil && isFieldOf(n, p, field)
}

func isFieldOf(n, p *sitter.Node, fields ...string) bool {
	for _, f := range fields {
		if c := p.ChildByFieldName(f); c == n {
			return true
		}
	}
	return false
}

func firstTyped(n *sitter.Node, typ string) *sitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if c := n.NamedChild(i); c != nil && c.Type() == typ {
			return c
		}
	}
	return nil
}
