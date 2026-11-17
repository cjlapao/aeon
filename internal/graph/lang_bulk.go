package graph

import (
	"github.com/smacker/go-tree-sitter/bash"
	clang "github.com/smacker/go-tree-sitter/c"
	cpp "github.com/smacker/go-tree-sitter/cpp"
	csharp "github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/cue"
	"github.com/smacker/go-tree-sitter/dockerfile"
	"github.com/smacker/go-tree-sitter/elixir"
	"github.com/smacker/go-tree-sitter/elm"
	"github.com/smacker/go-tree-sitter/groovy"
	"github.com/smacker/go-tree-sitter/hcl"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/ocaml"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/protobuf"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/sql"
	"github.com/smacker/go-tree-sitter/svelte"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/toml"
	"github.com/smacker/go-tree-sitter/yaml"
)

// The bulk half of T3: every grammar the chosen binding ships prebuilt,
// registered mechanically — data plus a fixture, not an investigation per
// language (plan.md T3 §3). The count is STATED AS MEASURED: this binding
// is smacker/go-tree-sitter
// v0.0.0-20240827094217-dd81d9e9be82, and enumerating the modules that
// export GetLanguage gives 31 grammars — 29 top-level language directories
// plus the two nested under typescript/ (typescript and tsx). json is NOT
// among them and is named as a gap (T3 §4: name it, do not paper over
// it). Graphify's "~40" describes Graphify's own tool and is NOT this
// binding's figure (D2, R2) — see tasks/T3.md's notes for the full list.
//
// The two languages proven with bespoke walkers (Go in lang_go.go,
// TypeScript/TSX in lang_typescript.go) prove the registry generalizes;
// everything below rides the generic walker (registry.go) on per-language
// Maps. Where a grammar's node-type vocabulary makes a relation kind
// meaningless (css, html, json, toml, yaml, sql, …) the Maps are empty and
// the language contributes file nodes only — parsing without lying: no
// invented functions. design.md CG4: "a language with no match for a given
// relation simply never emits that edge kind — it is not forced to."

func init() {
	Register(Entry{Name: "bash", Exts: []string{".sh", ".bash"}, Language: bash.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"}, CallTypes: []string{"command"}}})
	Register(Entry{Name: "c", Exts: []string{".c", ".h"}, Language: clang.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"},
			TypeTypes:   []string{"struct_specifier", "enum_specifier", "type_definition"},
			ImportTypes: []string{"preproc_include"}, CallTypes: []string{"call_expression"}}})
	Register(Entry{Name: "cpp", Exts: []string{".cpp", ".cc", ".hpp"}, Language: cpp.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"},
			TypeTypes:   []string{"class_specifier", "struct_specifier", "enum_specifier"},
			ImportTypes: []string{"preproc_include"}, CallTypes: []string{"call_expression"}}})
	Register(Entry{Name: "csharp", Exts: []string{".cs"}, Language: csharp.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"method_declaration", "constructor_declaration"},
			TypeTypes:   []string{"class_declaration", "struct_declaration", "interface_declaration", "enum_declaration"},
			ImportTypes: []string{"using_directive"}, CallTypes: []string{"invocation_expression"}}})
	Register(Entry{Name: "css", Exts: []string{".css"}, Language: css.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "cue", Exts: []string{".cue"}, Language: cue.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "dockerfile", Exts: []string{".dockerfile"}, Language: dockerfile.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "elixir", Exts: []string{".ex", ".exs"}, Language: elixir.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "elm", Exts: []string{".elm"}, Language: elm.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_declaration"}}})
	Register(Entry{Name: "groovy", Exts: []string{".groovy"}, Language: groovy.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "hcl", Exts: []string{".tf", ".hcl"}, Language: hcl.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "html", Exts: []string{".html", ".htm"}, Language: html.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "java", Exts: []string{".java"}, Language: java.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"method_declaration", "constructor_declaration"},
			TypeTypes:   []string{"class_declaration", "interface_declaration", "enum_declaration"},
			ImportTypes: []string{"import_declaration"}, CallTypes: []string{"method_invocation"}}})
	Register(Entry{Name: "javascript", Exts: []string{".js", ".jsx", ".mjs"}, Language: javascript.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_declaration", "generator_function_declaration", "method_definition"},
			TypeTypes: []string{"class_declaration"}, ImportTypes: []string{"import_statement"},
			CallTypes: []string{"call_expression", "new_expression"}}})
	Register(Entry{Name: "kotlin", Exts: []string{".kt"}, Language: kotlin.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_declaration"},
			TypeTypes: []string{"class_declaration", "object_declaration"},
			CallTypes: []string{"call_expression"}}})
	Register(Entry{Name: "lua", Exts: []string{".lua"}, Language: lua.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_declaration"}, CallTypes: []string{"function_call"}}})
	Register(Entry{Name: "ocaml", Exts: []string{".ml"}, Language: ocaml.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "php", Exts: []string{".php"}, Language: php.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"},
			TypeTypes:   []string{"class_declaration", "interface_declaration"},
			ImportTypes: []string{"namespace_use_declaration"},
			CallTypes:   []string{"function_call", "member_call_expression"}}})
	Register(Entry{Name: "protobuf", Exts: []string{".proto"}, Language: protobuf.GetLanguage,
		Maps: &Maps{TypeTypes: []string{"message", "enum", "service"}}})
	Register(Entry{Name: "python", Exts: []string{".py"}, Language: python.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"},
			TypeTypes:   []string{"class_definition"},
			ImportTypes: []string{"import_statement", "import_from_statement"},
			CallTypes:   []string{"call"}}})
	Register(Entry{Name: "ruby", Exts: []string{".rb"}, Language: ruby.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"method"},
			TypeTypes: []string{"class", "module"},
			CallTypes: []string{"call"}}})
	Register(Entry{Name: "rust", Exts: []string{".rs"}, Language: rust.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_item"},
			TypeTypes:   []string{"struct_item", "enum_item", "trait_item", "impl_item"},
			ImportTypes: []string{"use_declaration"},
			CallTypes:   []string{"call_expression", "method_call_expression"}}})
	Register(Entry{Name: "scala", Exts: []string{".scala"}, Language: scala.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_definition"},
			TypeTypes: []string{"class_definition", "object_definition", "trait_definition"},
			CallTypes: []string{"call"}}})
	Register(Entry{Name: "sql", Exts: []string{".sql"}, Language: sql.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "svelte", Exts: []string{".svelte"}, Language: svelte.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "swift", Exts: []string{".swift"}, Language: swift.GetLanguage,
		Maps: &Maps{FunctionTypes: []string{"function_declaration"},
			TypeTypes:   []string{"class_declaration", "protocol_declaration", "struct_declaration", "enum_declaration"},
			ImportTypes: []string{"import_declaration"}, CallTypes: []string{"call_expression"}}})
	Register(Entry{Name: "toml", Exts: []string{".toml"}, Language: toml.GetLanguage,
		Maps: &Maps{}})
	Register(Entry{Name: "yaml", Exts: []string{".yaml", ".yml"}, Language: yaml.GetLanguage,
		Maps: &Maps{}})
}
