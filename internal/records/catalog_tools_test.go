package records

// catalog_tools_test.go — S47 T7 (TC9, TC1, OL3, 2026-11-02): the two specs of
// the agent's own tool view, walked where they are defined.
//
// The catalog is the definition of a tool — its name, its socket twin, its class,
// whether it writes, and the schema a caller must satisfy — so these four checks
// run here rather than trusting a reader somewhere else to have looked. They are
// the sibling of `TestCatalogHasTheFiveMcpSpecs` (S46 T8) and the family is its
// own: `tools` did not exist before this slice (`tools.grants` is a socket method
// and was never a catalog action), and `TestNoWriteToolInTheToolsFamily` is what
// keeps TC1 true as the family grows — a `Mutates: true` row in this family would
// be S47 shipping a write tool, which the whole slice ruled out.

import (
	"encoding/json"
	"testing"
)

func TestCatalogHasTheTwoToolsSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		required []string
	}{
		"tools_mine": {"tools.mine.list", false, nil},
		"tool_show":  {"tools.mine.show", false, []string{"name"}},
	}
	got := map[string]Spec{}
	for _, sp := range Catalog() {
		if sp.Family != "tools" {
			continue
		}
		if _, dup := got[sp.Name]; dup {
			t.Fatalf("%s appears in the catalog twice — one name, one meaning", sp.Name)
		}
		got[sp.Name] = sp
	}
	if len(got) != len(want) {
		t.Fatalf("family tools holds %d specs, want exactly %d — the family is this slice's, and a name arriving here without an amendment is how a write sneaks into a read-only slice", len(got), len(want))
	}
	for name, w := range want {
		sp, ok := got[name]
		if !ok {
			t.Fatalf("%s is missing from the catalog", name)
		}
		if sp.Method != w.method {
			t.Errorf("%s twins to %q, want %q — the socket name is part of the definition", name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates = %v, want %v (TC1: this slice ships zero write tools)", name, sp.Mutates, w.mutates)
		}
		if sp.Class != "safe" {
			t.Errorf("%s class = %q, want safe — a read that is not safe is a read with a side effect", name, sp.Class)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s has a %d-byte description; a model chooses tools by reading this sentence", name, len(sp.Description))
		}
		var schema struct {
			Type                 string                     `json:"type"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
		}
		if err := json.Unmarshal(sp.Schema, &schema); err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
		if schema.Type != "object" {
			t.Errorf("%s schema type = %q, want object", name, schema.Type)
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("%s does not close its schema — with additionalProperties open, an smuggled `agent` field is accepted and silently ignored, which is worse than refused (OL3)", name)
		}
		if len(schema.Required) != len(w.required) {
			t.Errorf("%s requires %v, want %v", name, schema.Required, w.required)
		}
		for _, r := range w.required {
			found := false
			for _, have := range schema.Required {
				if have == r {
					found = true
				}
			}
			if !found {
				t.Errorf("%s does not require %q", name, r)
			}
		}
	}
}

// TestRecordsParityToolsFamily walks the family in BOTH directions: every spec has
// a socket twin that is a dot-name nobody else in the catalog claims, and the two
// names this slice promised are the only ones present. The direction that matters
// most is the one a generator cannot fake: a spec whose `Method` is empty or
// duplicated would give the agent a tool whose door answers somebody else's
// question.
func TestRecordsParityToolsFamily(t *testing.T) {
	byMethod := map[string]string{}
	tools := 0
	for _, sp := range Catalog() {
		if sp.Method == "" {
			t.Fatalf("%s has no socket twin — a tool with no door is a tool that answers only one caller", sp.Name)
		}
		if owner, dup := byMethod[sp.Method]; dup {
			t.Fatalf("%s and %s both twin to %s — one door, one meaning", owner, sp.Name, sp.Method)
		}
		byMethod[sp.Method] = sp.Name
		if sp.Family == "tools" {
			tools++
		}
	}
	if tools != 2 {
		t.Errorf("family tools holds %d specs, want 2", tools)
	}
	for _, want := range []string{"tools_mine", "tool_show"} {
		found := false
		for _, sp := range Catalog() {
			if sp.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not in the catalog at all", want)
		}
	}
}

// TestMineSchemasCarryNoIdentity is rule 1 stated against the definition rather
// than the wire type: neither schema may carry a field that names a subject. Not
// `agent`, not `actor`, not a `scope` that could smuggle one, and not an "include
// what I do not hold" that would turn an inventory into a survey of refusals. The
// schemas close with `additionalProperties:false`, so a field OUTSIDE this list is
// refused too — `TestCatalogHasTheTwoToolsSpecs` pins that half; this one pins
// that the closed set is the right set.
func TestMineSchemasCarryNoIdentity(t *testing.T) {
	forbidden := map[string]bool{
		"agent": true, "agents": true, "actor": true, "slug": true, "scope": true,
		"include_ungranted": true, "all": true, "subject": true, "target": true,
		"component": true, "loop": true, "project": true, "author": true,
	}
	for _, sp := range Catalog() {
		if sp.Family != "tools" {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &schema); err != nil {
			t.Fatalf("%s: %v", sp.Name, err)
		}
		for key := range schema.Properties {
			if forbidden[key] {
				t.Errorf("%s carries %q — the subject is the stamp on the step, never a param (OL3), and an inventory of what you hold has no knob for what you do not", sp.Name, key)
			}
		}
	}
}

// TestNoWriteToolInTheToolsFamily is TC1's own guard, and it is deliberately
// narrower than the whole-catalog mutator counts elsewhere: this family is the one
// S47 creates, and the slice's ruling is that it stays read-only forever. A
// `Mutates: true` here is a write tool arriving under the name of the agent's
// self-inspection door.
func TestNoWriteToolInTheToolsFamily(t *testing.T) {
	n := 0
	for _, sp := range Catalog() {
		if sp.Family != "tools" {
			continue
		}
		n++
		if sp.Mutates {
			t.Errorf("%s Mutates = true — TC1: the tools family is reads, and a write in the agent's own-view family is the door that changes what the other door reports", sp.Name)
		}
		if sp.Class != "safe" {
			t.Errorf("%s class = %q — every member of this family is a read, so `safe` is the only honest class", sp.Name, sp.Class)
		}
	}
	if n == 0 {
		t.Fatal("the tools family is empty — these guards would pass vacuously")
	}
}
