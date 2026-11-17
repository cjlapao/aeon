package records

// catalog_memory_test.go — S52 T4 (LM1, LM4, LM7): the memory family's
// four specs, asserted where the catalog is DATA. Two safe reads of the
// agent's OWN readable set, the agent's ONE memory write (capped,
// stamped), and the one ask (class detectable, Mutates:true) that files
// the INERT learning_retract row a person commits. What the NAMES are
// refused by lives in cmd/aeon's T3 guards and TestRegistryCountIsExactly
// AccountedFor (no retract/restore/edit/relay tool, no default grant, one
// author row); what is refused HERE is the shape of the four that do
// exist:
//
//   - exactly four in the family, each twinned to its learnings.mine.*
//     dot-name, each carrying the class and mutability it was granted
//     with;
//   - `additionalProperties: false` on all four — the strict decode is
//     the identity wall, and an open schema is a door a caller walks
//     through with an `actor` in its pocket;
//   - the property sets are EXACTLY the lists below: no actor, no agent,
//     no agentSlug, no told or relayed nameable anywhere; and only
//     learning_add names a scope at all — the readable set is resolved
//     from the stamped step, never typed (LM1), which is why the query
//     schema cannot even ask whose memory to widen to.

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestCatalogHasTheFourMemorySpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
	}{
		"learning_query":           {"learnings.mine.query", false, "safe", nil},
		"learning_show":            {"learnings.mine.show", false, "safe", []string{"id"}},
		"learning_add":             {"learnings.mine.add", true, "detectable", []string{"scope", "body", "provenance"}},
		"learning_propose_retract": {"learnings.mine.proposeRetract", true, "detectable", []string{"id", "reason"}},
	}
	seen := map[string]bool{}
	for _, sp := range Catalog() {
		if sp.Family != "memory" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family memory but is not one of the four — the plane ships two reads, ONE write and ONE ask (LM7)", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q — the dot-name IS the agent-scoped door, and a pair that moved is a different door", sp.Name, sp.Method, w.method)
		}
		if sp.Class != w.class || sp.Mutates != w.mutates {
			t.Errorf("%s is class %q mutates=%v, want %q mutates=%v", sp.Name, sp.Class, sp.Mutates, w.class, w.mutates)
		}
		got := requiredOf(t, sp)
		sort.Strings(got)
		exp := append([]string(nil), w.required...)
		sort.Strings(exp)
		if len(got) != len(exp) {
			t.Errorf("%s requires %v, want %v", sp.Name, got, exp)
		} else {
			for i := range got {
				if got[i] != exp[i] {
					t.Errorf("%s requires %v, want %v", sp.Name, got, exp)
					break
				}
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog — the family is exactly the four", name)
		}
	}
}

func TestMemorySchemasAreStrictAndExactlyTheirProperties(t *testing.T) {
	props := map[string][]string{
		"learning_query":           {"limit", "queryText", "tags"},
		"learning_show":            {"id"},
		"learning_add":             {"body", "provenance", "scope", "scopeKey", "tags"},
		"learning_propose_retract": {"id", "reason"},
	}
	seen := map[string]bool{}
	for _, sp := range Catalog() {
		if sp.Family != "memory" {
			continue
		}
		seen[sp.Name] = true
		var doc struct {
			Type                 string                     `json:"type"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &doc); err != nil {
			t.Fatalf("%s schema does not decode: %v", sp.Name, err)
		}
		if doc.Type != "object" {
			t.Errorf("%s schema is type %q, want object", sp.Name, doc.Type)
		}
		if doc.AdditionalProperties == nil || *doc.AdditionalProperties {
			t.Errorf("%s schema is not additionalProperties:false — an open schema is an identity smuggler", sp.Name)
		}
		var names []string
		for k := range doc.Properties {
			names = append(names, k)
		}
		sort.Strings(names)
		exp := append([]string(nil), props[sp.Name]...)
		sort.Strings(exp)
		if len(names) != len(exp) {
			for i := range names {
				if i >= len(exp) || names[i] != exp[i] {
					t.Errorf("%s properties = %v, want exactly %v — the readable set and the actor are stamped, never typed (LM1, LM4)", sp.Name, names, exp)
					break
				}
			}
		}
		for _, banned := range []string{"actor", "actorKind", "agent", "agentSlug", "agent_slug", "source"} {
			if _, ok := doc.Properties[banned]; ok {
				t.Errorf("%s names %q — a param naming WHO is a parse error by design (OL3, LM4)", sp.Name, banned)
			}
		}
	}
}

func TestMemoryProvenanceEnumHasNoPersonWord(t *testing.T) {
	var sp = specNamed("learning_add")
	if sp == nil {
		t.Fatal("learning_add is not in the catalog")
	}
	var doc struct {
		Properties struct {
			Provenance struct {
				Enum []string `json:"enum"`
			} `json:"provenance"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(sp.Schema, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Properties.Provenance.Enum) != 2 {
		t.Fatalf("provenance enum = %v, want exactly observed and assumed", doc.Properties.Provenance.Enum)
	}
	for _, v := range doc.Properties.Provenance.Enum {
		if v != "observed" && v != "assumed" {
			t.Errorf("provenance enum names %q — told is a person's word and relayed the board hook's; neither is nameable at an agent door (LM4)", v)
		}
	}
}

func specNamed(name string) *Spec {
	cat := Catalog()
	for i := range cat {
		if cat[i].Name == name {
			return &cat[i]
		}
	}
	return nil
}

func requiredOf(t *testing.T, sp Spec) []string {
	t.Helper()
	var doc struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(sp.Schema, &doc); err != nil {
		t.Fatalf("%s schema does not decode: %v", sp.Name, err)
	}
	return doc.Required
}
