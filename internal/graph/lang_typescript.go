package graph

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// TypeScript — the second language (T3), added precisely because it is
// structurally different where it matters: TS HAS inherits and mixes_in
// where Go has neither (design.md CG4), so adding it through the same
// registry proves the registry was not shaped around Go's own AST. It is
// the second language this repository is written in (ui/ is React + TS),
// so the fixture doubles as a real-world test just as Go's does.
//
// # The per-language mapping, as built (CG4's TypeScript column)
//
//   - calls      → call_expression (identifier callee, or member_expression)
//   - imports    → import_statement (the `from` source string)
//   - inherits   → extends: class extends clause AND interface
//     extends_type_clause (design.md guessed "extends on a class"; the
//     interface form is the same relation and is recorded here as the
//     measured correction T3's notes call for)
//   - mixes_in   → implements: the implements clause (design.md's guess,
//     confirmed by the grammar) — mixin composition is expressed with
//     helpers/functions in TS idiom, not a syntax the grammar names, so
//     implements is what the relation maps onto here
//   - uses       → type_identifier (annotations, generics, heritage)
//   - references → identifier uses of declared names not covered above
//
// # Symbol qualification
//
// Module-scoped: a file's basename without extension is the module
// ("order.ts" declares ProcessOrder → "order.ProcessOrder"; a class method
// is "order.Order.handle"). Two same-named exports in two modules are two
// symbols and query `ambiguous` lists both — D8's fixture case.

func init() {
	Register(Entry{
		Name:     "typescript",
		Exts:     []string{".ts", ".mts", ".cts"},
		Language: typescript.GetLanguage,
		Walk:     walkTypeScript,
	})
	Register(Entry{
		Name:     "tsx",
		Exts:     []string{".tsx"},
		Language: tsx.GetLanguage,
		Walk:     walkTypeScript,
	})
}

func walkTypeScript(root *sitter.Node, src []byte, e *Emitter) {
	mod := stemOf(e.file)

	declared := map[string]bool{e.file: true}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		decl := root.NamedChild(i)
		if decl == nil {
			continue
		}
		switch decl.Type() {
		case "function_declaration", "generator_function_declaration":
			if n := childText(decl, src, "name", "identifier"); n != "" {
				e.Node(KindFunction, mod+"."+n, startLine(decl), endLine(decl))
				declared[mod+"."+n] = true
				declared[n] = true
			}
		case "class_declaration":
			if n := childText(decl, src, "name", "type_identifier", "identifier"); n != "" {
				e.Node(KindClass, mod+"."+n, startLine(decl), endLine(decl))
				declared[mod+"."+n] = true
				declared[n] = true
			}
		case "interface_declaration":
			if n := childText(decl, src, "name", "type_identifier", "identifier"); n != "" {
				e.Node(KindInterface, mod+"."+n, startLine(decl), endLine(decl))
				declared[mod+"."+n] = true
				declared[n] = true
			}
		case "enum_declaration", "type_alias_declaration":
			if n := childText(decl, src, "name", "identifier", "type_identifier"); n != "" {
				e.Node(KindType, mod+"."+n, startLine(decl), endLine(decl))
				declared[mod+"."+n] = true
				declared[n] = true
			}
		case "lexical_declaration", "variable_declaration":
			// `const handle = (…) => {…}` declares a module-level
			// function the grammar files under the declarator, not as a
			// function_declaration. Same node, same kind.
			for j := 0; j < int(decl.NamedChildCount()); j++ {
				d := decl.NamedChild(j)
				if d == nil || d.Type() != "variable_declarator" {
					continue
				}
				v := d.ChildByFieldName("value")
				if v == nil || (v.Type() != "arrow_function" && v.Type() != "function_expression" && v.Type() != "function") {
					continue
				}
				if n := childText(d, src, "name", "identifier"); n != "" {
					e.Node(KindFunction, mod+"."+n, startLine(decl), endLine(decl))
					declared[mod+"."+n] = true
					declared[n] = true
				}
			}
		}
	}

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
		case "function_declaration", "generator_function_declaration":
			if name := childText(n, src, "name", "identifier"); name != "" {
				scope = mod + "." + name
			}
		case "class_declaration", "class":
			if name := childText(n, src, "name", "type_identifier", "identifier"); name != "" && n.Type() == "class_declaration" {
				scope = mod + "." + name
			}
		case "method_definition":
			name := childText(n, src, "name", "property_identifier")
			if name != "" {
				// The scope chain already sits on the class when the
				// descent entered its declaration; method_definition's
				// own name hangs there.
				if strings.HasPrefix(scope, mod+".") {
					e.Node(KindMethod, scope+"."+name, startLine(n), endLine(n))
					scope = scope + "." + name
				} else {
					e.Node(KindMethod, mod+"."+name, startLine(n), endLine(n))
					scope = mod + "." + name
				}
			}
		case "import_statement":
			path := tsImportSource(n, src)
			if path != "" {
				e.Node(KindPackage, path, 1, 1)
				e.Edge(EdgeImports, e.file, path)
			}
		case "call_expression":
			callee := tsCallee(n, src)
			if callee != "" {
				e.Edge(EdgeCalls, scope, callee)
			}
		case "new_expression":
			if ctor := childText(n, src, "constructor", "type_identifier", "identifier"); ctor != "" {
				e.Edge(EdgeCalls, scope, ctor)
			}
		case "extends_clause", "extends_type_clause":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if c := n.NamedChild(i); c != nil {
					if t := tsHeritageName(c, src); t != "" {
						e.Edge(EdgeInherits, scope, t)
					}
				}
			}
		case "implements_clause":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if c := n.NamedChild(i); c != nil {
					if t := tsHeritageName(c, src); t != "" {
						e.Edge(EdgeMixesIn, scope, t)
					}
				}
			}
		case "type_identifier":
			if !inField(n, "name") {
				if t := text(n, src); t != "" && declared[t] {
					e.Edge(EdgeUses, scope, t)
				}
			}
		case "identifier":
			if t := text(n, src); t != "" && declared[t] && tsRefPosition(n) {
				e.Edge(EdgeReference, scope, t)
			}
		}

		for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
			if c := n.NamedChild(i); c != nil {
				stack = append(stack, frame{n: c, scope: scope})
			}
		}
	}
}

// tsImportSource reads the `from "…"` string of an import_statement (or
// the bare `import "…"` side-effect form).
func tsImportSource(n *sitter.Node, src []byte) string {
	if s := n.ChildByFieldName("source"); s != nil {
		return strings.Trim(text(s, src), `"'`)
	}
	var out string
	var find func(*sitter.Node)
	find = func(x *sitter.Node) {
		if x == nil || out != "" {
			return
		}
		if strings.Contains(x.Type(), "string") {
			out = strings.Trim(text(x, src), `"'`)
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			find(x.NamedChild(i))
		}
	}
	find(n)
	return out
}

// tsCallee names a call_expression's callee: foo(...) → "foo";
// obj.method(...) → "obj.method" (resolved whole, then by final
// component — the receiver is usually a variable, so the suffix carries
// the meaning, and `inferred` says so).
func tsCallee(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "member_expression", "property_identifier":
		// Measured: a chained member callee's TEXT carries the whole
		// `this.get("/orders").then` including arguments; the name is
		// everything before the first paren.
		t := text(fn, src)
		if i := strings.IndexByte(t, '('); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		return t
	default:
		t := text(fn, src)
		if i := strings.LastIndexByte(t, '.'); i >= 0 {
			return t[i+1:]
		}
		return t
	}
}

// tsHeritageName extracts the super/interface type from a heritage clause
// child (type_identifier, or member_expression for qualified names).
func tsHeritageName(n *sitter.Node, src []byte) string {
	switch n.Type() {
	case "type_identifier", "identifier", "member_expression", "generic_type":
		t := text(n, src)
		if i := strings.IndexByte(t, '<'); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		return t
	}
	return ""
}

func tsRefPosition(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return true
	}
	if isFieldOf(n, p, "name", "property", "object", "operator", "field", "left", "right", "source") {
		return false
	}
	switch p.Type() {
	case "property_identifier", "pair", "method_definition", "shorthand_property_identifier_pattern", "class_body":
		return false
	}
	if p.Type() == "pair" || p.Type() == "method_signature" || p.Type() == "method_definition" {
		return false
	}
	return true
}
