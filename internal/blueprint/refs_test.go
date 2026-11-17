package blueprint

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cjlapao/aeon/internal/handler"
)

// refs_test.go — S48 T2 rule 1: ONE extraction, two uses. References must
// agree with what Analyze validates, over every shipped blueprint and every
// fixture — the derived `blueprint_ref` table is only trustworthy if it
// never disagrees with the validator that gates the same content.

// independentWalk extracts the same names through a generic YAML map walk —
// deliberately NOT References' code, so agreement is a real comparison and
// not a tautology. It recognises a STEP mapping by its `kind` key (so
// verify.run's `params.agent` — a gate identity, not a step agent — is not
// mistaken for one) and reads `params.tech` as the resolver tag.
func independentWalk(t *testing.T, raw []byte) map[Ref]bool {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	out := map[Ref]bool{}
	scalar := func(n *yaml.Node) string {
		if n != nil && n.Kind == yaml.ScalarNode {
			return n.Value
		}
		return ""
	}
	get := func(n *yaml.Node, key string) *yaml.Node {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
		return nil
	}
	var walkSeq func(n *yaml.Node)
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		isStep := get(n, "kind") != nil
		if isStep {
			if v := scalar(get(n, "agent")); v != "" {
				if v == "by-tech-tag" {
					out[Ref{Kind: "resolver", Target: v}] = true
				} else {
					out[Ref{Kind: "agent", Target: v}] = true
				}
			}
			if v := scalar(get(n, "action")); v != "" {
				out[Ref{Kind: "action", Target: v}] = true
			}
			if v := scalar(get(n, "skill")); v != "" && strings.Contains(v, "/") {
				parts := strings.SplitN(v, "/", 2)
				if parts[0] != "" && parts[1] != "" && !strings.Contains(parts[1], "/") {
					out[Ref{Kind: "skill", Target: parts[0]}] = true
				}
			}
			if p := get(n, "params"); p != nil && p.Kind == yaml.MappingNode {
				if v := scalar(get(p, "tech")); v != "" {
					out[Ref{Kind: "resolver", Target: v}] = true
				}
			}
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "converse" && v.Kind == yaml.ScalarNode && v.Value != "" {
				out[Ref{Kind: "converse", Target: v.Value}] = true
			}
			switch v.Kind {
			case yaml.MappingNode:
				walk(v)
			case yaml.SequenceNode:
				walkSeq(v)
			}
		}
	}
	walkSeq = func(n *yaml.Node) {
		for _, item := range n.Content {
			if item.Kind == yaml.MappingNode {
				walk(item)
			}
		}
	}
	if len(doc.Content) > 0 {
		walk(doc.Content[0])
	}
	return out
}

func TestReferencesEqualAnalyzeForAllShippedAndFixtures(t *testing.T) {
	files := []string{}
	for _, name := range StandardNames() {
		files = append(files, filepath.Join("blueprints", name+".yaml"))
	}
	files = append(files, filepath.Join("testdata", "code-action.yaml"), filepath.Join("testdata", "verify-run-fixture.yaml"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		bp, err := Load(raw)
		if err != nil {
			t.Fatalf("%s: load: %v", f, err)
		}
		refs := References(&bp)
		got := map[Ref]bool{}
		for _, r := range refs {
			got[r] = true
		}
		want := independentWalk(t, raw)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: References disagrees with the independent walk\n References: %v\n walk:       %v", f, sortedRefs(refs), sortedRefs(refsOf(want)))
		}
		// The Analyze half: hand the validator EXACTLY the agent names
		// References found. If References missed one Analyze will judge,
		// unknown-agent fires; if References invented one, it sits unused in
		// the list (harmless to Analyze) — the walk comparison above is what
		// refuses inventions.
		var agents []string
		for _, r := range refs {
			if r.Kind == "agent" {
				agents = append(agents, r.Target)
			}
		}
		// The shipped fixtures add `verify.run` (S57) to the s16 mirror.
		actions := append(append([]string(nil), s16ActionSet...), "verify.run")
		opts := ValidationOptions{Mode: "standard", Agents: append(agents, handler.Names()...), Resolvers: handler.Resolvers(), Actions: actions, AgentTools: handler.ToolsFor}
		rep := Analyze(raw, opts)
		for _, d := range rep.Diagnostics {
			if d.Kind == "unknown-agent" || d.Kind == "unknown-action" {
				t.Fatalf("%s: Analyze refused %s at %s — References did not surface what Analyze judged", f, d.Kind, d.Path)
			}
		}
	}
}

func TestReferencesAreSortedAndDeduped(t *testing.T) {
	bp := &Blueprint{
		Converse: "chat",
		Lanes: []Lane{
			{ID: "a", Step: &Step{Kind: ModelStep, Agent: "author"}},
			{ID: "b", Ladder: []Step{
				{ID: "x", Kind: ModelStep, Agent: "author"}, // dupe agent
				{ID: "y", Kind: CodeAction, Action: "git.commitAndPush"},
				{ID: "z", Kind: ModelStep, Agent: "by-tech-tag", Params: map[string]any{"tech": "go"}},
			}},
		},
	}
	refs := References(bp)
	if !sort.SliceIsSorted(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].Target < refs[j].Target
	}) {
		t.Fatalf("not sorted: %v", refs)
	}
	seen := map[Ref]bool{}
	for _, r := range refs {
		if seen[r] {
			t.Fatalf("duplicate %v", r)
		}
		seen[r] = true
	}
	want := []Ref{{"action", "git.commitAndPush"}, {"agent", "author"}, {"converse", "chat"}, {"resolver", "by-tech-tag"}, {"resolver", "go"}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
}

func TestResolverNamesEqualTheRegistry(t *testing.T) {
	// The mirror rule, pinned from the testable side (the s16ActionSet
	// precedent): handler.resolvers is closed code; this list must equal it.
	got := append([]string(nil), resolverNames...)
	want := handler.Resolvers()
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolverNames %v != handler.Resolvers() %v — update the mirror in refs.go with the registry", got, want)
	}
}

func sortedRefs(in []Ref) []Ref {
	out := append([]Ref(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func refsOf(set map[Ref]bool) []Ref {
	var out []Ref
	for r := range set {
		out = append(out, r)
	}
	return sortedRefs(out)
}
