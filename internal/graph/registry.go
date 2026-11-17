package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// # The grammar registry (T2)
//
// Sized for exactly one operation a later task needs: register one more
// grammar (plan.md's T2 scope, §3 — "a map from language name to a parse
// function, nothing more general than that"). Every grammar is one instance
// of the shape sqlite-vec proved (DB18): a C library registered through
// cgo, statically linked into the binary, present in every build with no
// install step and no runtime load. Q1's answer, measured against the
// chosen binding rather than assumed from Graphify's own figure (D2, R2),
// is recorded in tasks/T2.md and tasks/T3.md.
//
// # Registration is an allow-list, decided per language
//
// A grammar enters the registry only with a deliberate Register call in
// this package (lang_go.go, lang_typescript.go, lang_bulk.go). A binding
// shipping one more language in the next version changes nothing here
// until someone registers it — the same discipline B8 gives the daemon's
// service set, applied to grammars.

// WalkFunc turns one parsed syntax tree into node and edge drafts. A
// language registers either a bespoke Walk (the two languages this plan
// proves the mechanism on first — Go and TypeScript, because this very
// repository is written in them) or nil, in which case the generic
// walker drives off the entry's Maps. The bulk half of T3 is bulk because
// the generic walker exists: registration plus a fixture, not a new
// investigation.
type WalkFunc func(root *sitter.Node, src []byte, e *Emitter)

// Maps is the generic walker's data: which AST node type names declare a
// function, a type, an import, and a call, in one grammar. Field names
// follow tree-sitter's grammar conventions; a name the grammar does not
// use simply never matches, which costs nodes on that language's map, never
// correctness on another's.
type Maps struct {
	FunctionTypes []string
	TypeTypes     []string
	ImportTypes   []string
	CallTypes     []string
	// CallTargetField names the child holding the callee (tree-sitter's
	// "function" for most grammars). Empty falls back to the call node's
	// first child.
	CallTargetField string
}

// Entry is one registered grammar.
type Entry struct {
	// Name is the language name as rows record it (graph_node.language).
	Name string
	// Exts are the file extensions this grammar claims, dots included,
	// lowercase (".go"). Extension lookup is the only selection rule at
	// parse time — no content sniffing, no override files.
	Exts []string
	// Language constructs the statically-linked grammar handle. It is a
	// func, not a value, so registering a language costs nothing until a
	// file of that extension is actually parsed.
	Language func() *sitter.Language
	// Walk is the language's AST-to-drafts walker. Nil means use Maps
	// with the generic walker.
	Walk WalkFunc
	// Maps drives the generic walker when Walk is nil.
	Maps *Maps
}

var (
	registryMu sync.RWMutex
	byName     = map[string]Entry{}
	byExt      = map[string]Entry{}
)

// Register adds one grammar. It panics on a double registration of a name
// or an extension: both are build-time mistakes in THIS repository (every
// Register call is here), caught by every test, never a runtime condition a
// caller can hit — the schema.go panic for a bad migration file is the
// precedent for that reasoning.
func Register(e Entry) {
	if e.Name == "" || e.Language == nil || len(e.Exts) == 0 {
		panic("graph: Register needs Name, Language and at least one Ext")
	}
	if e.Walk == nil && e.Maps == nil {
		panic("graph: " + e.Name + " needs a Walk or Maps — a grammar that emits nothing is a parse that lies")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := byName[e.Name]; dup {
		panic("graph: language registered twice: " + e.Name)
	}
	byName[e.Name] = e
	for _, ext := range e.Exts {
		if _, dup := byExt[ext]; dup {
			panic("graph: extension claimed twice: " + ext + " (by " + e.Name + ")")
		}
		byExt[ext] = e
	}
}

// Registered names every registered language, sorted — the measured count
// T3's notes state, straight from the registry rather than from anyone's
// README.
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(byName))
	for n := range byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// LookupByPath selects the grammar for a path by extension, and reports
// whether this slice can parse it at all. An unregistered extension is not
// an error — it is the shape of the answer: the build walk SKIPS what no
// grammar claims, and a project of unsupported languages reads not-built
// for exactly that reason.
func LookupByPath(path string) (Entry, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	e, ok := byExt[strings.ToLower(filepath.Ext(path))]
	return e, ok
}

// ErrUnsupported names the one parse-time refusal: a path whose extension
// no registered grammar claims. parse_test asserts it so the build walk's
// skip stays a decision, not a silent nil path.
var ErrUnsupported = fmt.Errorf("graph: no grammar registered for this file's extension")
