package main

// tools_guard_predicates_test.go — S47 T9: the predicates the eight guards in
// tools_guard_test.go decide with, and the canaries that prove they bite.
//
// The split is by concern: this file is the CHECKS and their proofs of bite, that
// one is the CLAIMS about the tree. Every predicate here is a pure function over a
// value the caller supplies — the real assertion and the canary below call the same
// code with the same shape, one with what the tree holds and one with a violation
// planted in it — which is what makes "the canary passed" evidence about the guard
// rather than about a different code path. Nothing here walks the tree or builds a
// daemon; nothing there decides.
//
// The discipline is S46's (mcp_amend_test.go): a guard nobody has seen fail is a
// rumour. The canaries feed synthetic violations; where a predicate takes a walked
// map, the violation is an overlay entry, never a file written into the tree.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
)

// --- the predicates (the canary feeds THESE) ---------------------------------

// methodFact is the slice of a registered method a guard reads. A struct rather
// than a *protocol.Method so the canary can synthesise a violation without
// touching a table.
type methodFact struct {
	Name               string
	RequiresInitialize bool
	Debug              bool
}

func s47MethodSetOffenses(methods []methodFact) []string {
	var bad []string
	seen := map[string]bool{}
	for _, m := range methods {
		if !strings.HasPrefix(m.Name, "tools.") && !strings.HasPrefix(m.Name, "surface.") {
			continue
		}
		seen[m.Name] = true
		allowed := false
		for _, want := range s47Methods {
			if string(want) == m.Name {
				allowed = true
				break
			}
		}
		newcomer := !allowed && s47NeighbourMethods[m.Name] == ""
		// The write-shape check runs on newcomers. It does not run on a named
		// neighbour, whose name predates this slice: S34's read `tools.grants`
		// answers a question ABOUT grants and is a read. The exemption is by exact
		// name in s47NeighbourMethods — visible, countable, and a new
		// `tools.grantSomething` still lands in the newcomer branch and is caught.
		if newcomer && toolsWriteShapeRe.MatchString(m.Name) {
			bad = append(bad, m.Name+" is write-shaped — TC1 ships no write method, tool or verb")
		}
		if newcomer {
			bad = append(bad, m.Name+" is registered in the tools./surface. space and is neither one of the eight S47 doors nor a named neighbour")
		}
		if !allowed {
			continue // the neighbour's init/debug discipline is S34's claim, not this slice's
		}
		if !m.RequiresInitialize {
			bad = append(bad, m.Name+" does not require initialize — a connection could read the catalog before the daemon knows who is calling")
		}
		if m.Debug {
			bad = append(bad, m.Name+" is Debug — a Debug method is excluded from every public projection, and these are the app's doors")
		}
	}
	for _, want := range s47Methods {
		if !seen[string(want)] {
			bad = append(bad, fmt.Sprintf("%s is not registered at all — a missing read is P34's failure mode", want))
		}
	}
	return bad
}

func s47SurfaceReadOffenses(entries []protocol.SurfaceEntry) []string {
	var bad []string
	byName := map[string]protocol.SurfaceEntry{}
	for _, e := range entries {
		byName[string(e.Method)] = e
	}
	for _, want := range s47Methods {
		e, ok := byName[string(want)]
		if !ok {
			bad = append(bad, string(want)+" carries no surface classification")
			continue
		}
		if e.Kind != protocol.SurfaceRead {
			bad = append(bad, fmt.Sprintf("%s is classified %s — every door this slice adds reports and changes nothing (TC1)", want, e.Kind))
		}
	}
	return bad
}

func toolsCatalogSpecOffenses(specs []records.Spec) []string {
	var bad []string
	seen := map[string]bool{}
	for _, s := range specs {
		if !strings.EqualFold(s.Family, "tools") {
			continue
		}
		seen[s.Name] = true
		if s.Mutates {
			bad = append(bad, s.Name+" mutates — the `tools` family holds the two own-view reads and nothing else (TC1)")
		}
		if s.Class != "safe" {
			bad = append(bad, s.Name+" is class "+s.Class+" — a read this slice adds is `safe`")
		}
	}
	for _, want := range []string{"tools_mine", "tool_show"} {
		if !seen[want] {
			bad = append(bad, want+" is gone from the records catalog — the agent's own view has no door")
		}
	}
	return bad
}

func toolsVerbOffenses(verbs []string) []string {
	var bad []string
	for _, v := range verbs {
		low := strings.ToLower(v)
		for _, banned := range []string{"add", "create", "update", "delete", "remove", "set", "grant", "revoke", "import", "apply", "commit", "seed", "repair", "sync", "clear", "prune", "install"} {
			if strings.Contains(low, banned) {
				bad = append(bad, fmt.Sprintf("the verb %q contains %q — a client with a write verb is TC1 breached from the terminal", v, banned))
			}
		}
	}
	return bad
}

// toolRowReaderOffenses reports any non-test file outside the closed reader set
// that reaches a `tool`-table accessor.
func toolRowReaderOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		short := repoRelGuard(path)
		if toolRowReaderFiles[short] {
			continue
		}
		if strings.HasPrefix(short, "internal/toolseed/") || strings.HasPrefix(short, "internal/store/") {
			continue // the store defines the accessors; the seed applier is the only writer
		}
		if toolRowAccessorRe.MatchString(src) {
			bad = append(bad, short+" reaches the tool table — the catalog is the only reader beside the store and the seed package (TC2)")
		}
	}
	return bad
}

func toolsOnNarrow(actions []narrow.Action) []string {
	banned := map[string]bool{}
	for _, m := range s47Methods {
		banned[string(m)] = true
	}
	var bad []string
	for _, a := range actions {
		if banned[string(a.SocketMethod)] {
			bad = append(bad, a.Name+" projects "+string(a.SocketMethod)+" — these reads are the person's door and the agent's own view, never the narrow projection (TC6)")
		}
	}
	return bad
}

func toolsOnAgentServices(ms []protocol.AgentServiceMethod) []string {
	banned := map[string]bool{}
	for _, m := range s47Methods {
		banned[string(m)] = true
	}
	var bad []string
	for _, m := range ms {
		if banned[string(m.Name)] {
			bad = append(bad, string(m.Name)+" is in the agent-to-agent enumeration — an agent reaches its own view through the records catalog, not by enumerating the surface")
		}
	}
	return bad
}

func toolsLiteralOffenses(files map[string]string, needles []string) []string {
	var bad []string
hit:
	for path, src := range files {
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				bad = append(bad, repoRelGuard(path)+" names "+needle)
				continue hit
			}
		}
	}
	return bad
}

func runtimeRowOffenses(files map[string]string) []string {
	var bad []string
hit:
	for path, src := range files {
		for _, needle := range runtimeReadNeedles {
			if strings.Contains(src, needle) {
				bad = append(bad, repoRelGuard(path)+" names "+needle+" — the executor and the schema are code; the row is a display copy whose only reader is the catalog (TC2)")
				continue hit
			}
		}
	}
	return bad
}

// capabilityPair is one tool's declaration as the completeness check reads it.
type capabilityPair struct {
	Name         string
	Capabilities []string
	Declared     bool
}

func capabilityOffenses(pairs []capabilityPair, vocabulary map[string]bool, allowEmpty map[string]bool) []string {
	var bad []string
	for _, p := range pairs {
		if !p.Declared {
			bad = append(bad, p.Name+" has no Meta at all — every registry tool declares a family, capabilities and governed-by (TC3)")
			continue
		}
		if len(p.Capabilities) == 0 {
			if !allowEmpty[p.Name] {
				bad = append(bad, p.Name+" declares no capability and is not on the named empty set")
			}
			continue
		}
		for _, c := range p.Capabilities {
			if !vocabulary[c] {
				bad = append(bad, p.Name+" declares "+c+", which is outside the closed capability vocabulary")
			}
		}
	}
	return bad
}

func unclassifiedMethodOffenses(registered []string, entries []protocol.SurfaceEntry) []string {
	classified := map[string]bool{}
	for _, e := range entries {
		classified[string(e.Method)] = true
	}
	var bad []string
	for _, name := range registered {
		if !classified[name] {
			bad = append(bad, name+" is registered on the daemon and carries no surface classification — add its entry to the plane table that owns its family (TC6)")
		}
	}
	return bad
}

func ownViewScopingOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		short := repoRelGuard(path)
		wanted := false
		for _, name := range ownViewHandlerFiles {
			if strings.Contains(short, name) {
				wanted = true
				break
			}
		}
		if !wanted {
			continue
		}
		for _, needle := range ownViewUnscopedNeedles {
			if strings.Contains(src, needle) {
				bad = append(bad, short+" reaches the unscoped person read "+needle+" — the own view answers SELF through the scoped path, and a peer's private world is not in its input (MC2)")
			}
		}
	}
	return bad
}

// repoRelGuard shortens a walked path to repository-relative form, the form the
// named-file maps use.
func repoRelGuard(path string) string {
	short := filepath.ToSlash(path)
	for _, cut := range []string{"internal/", "cmd/"} {
		if i := strings.Index(short, cut); i >= 0 {
			return short[i:]
		}
	}
	return short
}

// --- guard 8: every predicate is shown to bite --------------------------------

// baseFacts is the table as it should be: all eight doors registered, initialised,
// not Debug. The canaries mutate a COPY of this, so an offense can only come from
// the one thing the subtest changed.
func baseFacts() []methodFact {
	var out []methodFact
	for _, m := range s47Methods {
		out = append(out, methodFact{Name: string(m), RequiresInitialize: true})
	}
	return out
}

func TestToolsGuardsHaveTeeth(t *testing.T) {
	t.Run("WriteShapedMethod", func(t *testing.T) {
		facts := append(baseFacts(), methodFact{Name: "tools.update", RequiresInitialize: true})
		bad := s47MethodSetOffenses(facts)
		if len(bad) != 2 { // once write-shaped, once unlisted
			t.Fatalf("a registered `tools.update` beside a complete table produced %d offenses, want the guard to shout twice: %v", len(bad), bad)
		}
	})
	t.Run("MissingMethod", func(t *testing.T) {
		bad := s47MethodSetOffenses(nil)
		if len(bad) != len(s47Methods) {
			t.Fatalf("an empty table produced %d offenses, want one per door (%d)", len(bad), len(s47Methods))
		}
	})
	t.Run("DebugOrUninitialisedDoor", func(t *testing.T) {
		facts := baseFacts()
		facts[0].RequiresInitialize = false
		if bad := s47MethodSetOffenses(facts); len(bad) != 1 || !strings.Contains(bad[0], "initialize") {
			t.Fatalf("a door that skips initialize slipped through: %v", bad)
		}
		facts = baseFacts()
		facts[0].Debug = true
		if bad := s47MethodSetOffenses(facts); len(bad) != 1 || !strings.Contains(bad[0], "Debug") {
			t.Fatalf("a Debug catalog door slipped through: %v", bad)
		}
	})
	t.Run("ClassificationNotRead", func(t *testing.T) {
		entries := []protocol.SurfaceEntry{{Method: protocol.MethodToolsList, Kind: protocol.SurfaceWrite}}
		for _, m := range s47Methods[1:] {
			entries = append(entries, protocol.SurfaceEntry{Method: m, Kind: protocol.SurfaceRead})
		}
		if bad := s47SurfaceReadOffenses(entries); len(bad) != 1 {
			t.Fatalf("a catalog read classified `write` slipped through: %v", bad)
		}
	})
	t.Run("MutatingCatalogSpec", func(t *testing.T) {
		specs := append(records.Catalog(), records.Spec{Name: "tools_disable", Method: "tools.disable", Family: "tools", Class: "safe", Mutates: true})
		bad := toolsCatalogSpecOffenses(specs)
		if len(bad) != 1 || !strings.Contains(bad[0], "tools_disable") {
			t.Fatalf("a mutating tools-family spec slipped through: %v", bad)
		}
	})
	t.Run("MutatingVerb", func(t *testing.T) {
		if bad := toolsVerbOffenses([]string{"tools list", "tools apply"}); len(bad) != 1 {
			t.Fatalf("a write-shaped verb slipped through: %v", bad)
		}
	})
	t.Run("SecondReaderOfTheToolTable", func(t *testing.T) {
		files := map[string]string{"internal/runtime/loop.go": "package runtime\nfunc f(){ _, _ := st.ToolList(ctx) }"}
		bad := toolRowReaderOffenses(files)
		if len(bad) != 1 || !strings.Contains(bad[0], "internal/runtime/loop.go") {
			t.Fatalf("a second reader of the tool table slipped through: %v", bad)
		}
		if bad := toolRowReaderOffenses(map[string]string{"internal/store/tool.go": "st.ToolList(ctx)"}); len(bad) != 0 {
			t.Fatalf("the named reader was accused: %v", bad)
		}
	})
	t.Run("NarrowProjectionCanary", func(t *testing.T) {
		bad := toolsOnNarrow([]narrow.Action{{Name: "aeon_surface_list", SocketMethod: protocol.MethodSurfaceList}})
		if len(bad) != 1 {
			t.Fatalf("a narrow action projecting surface.list slipped through: %v", bad)
		}
	})
	t.Run("AgentServicesCanary", func(t *testing.T) {
		bad := toolsOnAgentServices([]protocol.AgentServiceMethod{{Name: protocol.MethodToolsList}})
		if len(bad) != 1 {
			t.Fatalf("tools.list in the agent-services enumeration slipped through: %v", bad)
		}
	})
	t.Run("LiteralInAForeignPackage", func(t *testing.T) {
		files := walkedSources(nil, map[string]string{
			"internal/tools/hidden.go": `package tools // "tools.list" appears here`,
		})
		bad := toolsLiteralOffenses(files, s47LiteralNeedles)
		if len(bad) != 1 {
			t.Fatalf("a foreign package naming a person's read slipped through: %v", bad)
		}
	})
	t.Run("RuntimeNamesTheRow", func(t *testing.T) {
		files := map[string]string{"internal/runtime/step.go": "package runtime // var _ store.ToolRow"}
		if bad := runtimeRowOffenses(files); len(bad) != 1 {
			t.Fatalf("the runtime naming a tool row slipped through: %v", bad)
		}
	})
	t.Run("ToolWithoutMetaOrVocabulary", func(t *testing.T) {
		vocab := map[string]bool{"workspace_read": true}
		empty := map[string]bool{}
		if bad := capabilityOffenses([]capabilityPair{{Name: "newcomer"}}, vocab, empty); len(bad) != 1 {
			t.Fatalf("a tool with no Meta slipped through: %v", bad)
		}
		if bad := capabilityOffenses([]capabilityPair{{Name: "newcomer", Capabilities: []string{"secret_power"}}}, vocab, empty); len(bad) != 1 {
			t.Fatalf("a capability outside the vocabulary slipped through: %v", bad)
		}
		if bad := capabilityOffenses([]capabilityPair{{Name: "todo", Declared: true}}, vocab, empty); len(bad) != 1 {
			t.Fatalf("an unexplained empty capability set slipped through: %v", bad)
		}
	})
	t.Run("UnclassifiedRegisteredMethod", func(t *testing.T) {
		bad := unclassifiedMethodOffenses([]string{"surface.get", "brand.new.method"}, []protocol.SurfaceEntry{{Method: protocol.MethodSurfaceGet}})
		if len(bad) != 1 || !strings.Contains(bad[0], "brand.new.method") {
			t.Fatalf("a registered, unclassified method slipped through: %v", bad)
		}
	})
	t.Run("OwnViewReachingUnscoped", func(t *testing.T) {
		files := map[string]string{"internal/daemon/tools_catalog_mine.go": "func f(){ s.ToolsList(ctx, f) }"}
		if bad := ownViewScopingOffenses(files); len(bad) == 0 {
			t.Fatal("the own view reaching the unscoped person read slipped through")
		}
		if bad := ownViewScopingOffenses(map[string]string{"internal/daemon/other.go": "func f(){ s.ToolsList(ctx, f) }"}); len(bad) != 0 {
			t.Fatalf("a file outside the own-view handlers was accused: %v", bad)
		}
	})
	t.Run("UnattributedRegistryName", func(t *testing.T) {
		table := []registryContribution{{Slice: "S11", Names: []string{"read"}, Why: "the base"}}
		bad := registryAttributionOffenses([]string{"read", "newcomer"}, table)
		if len(bad) != 1 || !strings.Contains(bad[0], "newcomer") {
			t.Fatalf("a name attributed to no slice slipped through: %v", bad)
		}
		if bad := registryAttributionOffenses([]string{"read"}, []registryContribution{{Slice: "S11", Names: []string{"read"}, Why: "the base"}, {Slice: "S99", Names: []string{"read"}, Why: "also mine"}}); len(bad) != 1 {
			t.Fatalf("a double attribution slipped through: %v", bad)
		}
		if bad := registryAttributionOffenses([]string{"read"}, []registryContribution{{Slice: "S11", Names: []string{"read", "vanished"}, Why: "the base"}}); len(bad) != 1 {
			t.Fatalf("a row claiming a name the registry no longer holds slipped through: %v", bad)
		}
	})
}
