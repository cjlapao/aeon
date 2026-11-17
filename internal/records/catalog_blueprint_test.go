package records

// catalog_blueprint_test.go — S48 T10 (BP9): the blueprint family's five
// specs. Four reads (the tool twins of the person's standing
// blueprint.list/show/validate/lint doors) and ONE propose (class
// detectable, Mutates:true) — an agent may see, check, and propose; the
// five write doors (save/duplicate/delete/revert and the commit) are the
// person's (rule 3; the cmd/aeon guard refuses any blueprint WRITE tool by
// name, TestNoBlueprintWriteToolExists). Rule 1 is the schema walk: the
// propose schema carries no identity, status, kind, origin or rev member —
// identity is stamped (OL3), the kind is implied, the rev is the daemon's
// counter, the origin is a property of the ROW not of the input (BP1).

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogHasTheFiveBlueprintSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
	}{
		"blueprint_list":     {"blueprint.list", false, "safe", nil},
		"blueprint_show":     {"blueprint.show", false, "safe", []string{"name"}},
		"blueprint_validate": {"blueprint.validate", false, "safe", []string{"content"}},
		"blueprint_lint":     {"blueprint.lint", false, "safe", nil},
		"blueprint_propose":  {"blueprint.propose", true, "detectable", []string{"name", "content"}},
	}
	seen := map[string]bool{}
	bpMutators := 0
	for _, sp := range Catalog() {
		if sp.Family != "blueprint" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family blueprint but is not one of the five — the family ships four reads and one propose (BP9)", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q — the dot-name IS the person's standing door (rule 4)", sp.Name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v — only the propose files an INERT row; a read never mutates", sp.Name, sp.Mutates, w.mutates)
		}
		if sp.Class != w.class {
			t.Errorf("%s class %q, want %q — the propose files a row and must say so (detectable)", sp.Name, sp.Class, w.class)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s description is %d chars — the model must be able to tell this door apart", sp.Name, len(sp.Description))
		}
		var sch struct {
			Required             []string                   `json:"required"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
			t.Fatalf("%s schema: %v", sp.Name, err)
		}
		if sch.AdditionalProperties == nil || *sch.AdditionalProperties {
			t.Errorf("%s does not close its properties — the strict decode IS the identity wall", sp.Name)
		}
		have := map[string]bool{}
		for _, r := range sch.Required {
			have[r] = true
			if _, ok := sch.Properties[r]; !ok {
				t.Errorf("%s requires %q which its properties do not declare", sp.Name, r)
			}
		}
		for _, r := range w.required {
			if !have[r] {
				t.Errorf("%s does not require %q — a propose decided without it is a row a person reads blind", sp.Name, r)
			}
		}
		if sp.Mutates {
			bpMutators++
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog", name)
		}
	}
	if bpMutators != 1 {
		t.Errorf("%d mutating blueprint specs, want exactly 1 (the propose twin) — a second write spec is BP9 broken", bpMutators)
	}
}

// TestProposeSchemaCarriesNoIdentityOrStatus is rule 1 for the family's
// mutating door: no actor, source, status, kind, state, origin or rev
// member anywhere in the props. The author is the step's (stamped by the
// records service), the kind is implied (blueprint_save — there is no
// field for a smuggled verb), the rev is the daemon's counter, and the
// origin is a property of the ROW (a proposal names the blueprint; it is
// not one) — every one of them an input field would be a forgery with a
// home.
func TestProposeSchemaCarriesNoIdentityOrStatus(t *testing.T) {
	banned := []string{
		"actor", "createdby", "createdbykind", "createdbyid", "proposedby", "author",
		"source", "status", "state", "kind", "decision", "decidedby", "origin",
		"rev", "revision", "trigger", "appliedat",
	}
	all := Catalog()
	var sp *Spec
	for i := range all {
		if all[i].Name == "blueprint_propose" {
			sp = &all[i]
			break
		}
	}
	if sp == nil {
		t.Fatal("blueprint_propose is missing from the catalog")
	}
	var sch struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
		t.Fatal(err)
	}
	if len(sch.Properties) != 4 {
		t.Errorf("propose carries %d members, want 4 (name, content, create_only, note)", len(sch.Properties))
	}
	for prop := range sch.Properties {
		p := strings.ToLower(strings.ReplaceAll(prop, "_", ""))
		for _, b := range banned {
			if p == b {
				t.Errorf("blueprint_propose carries %q — identity, status and decisions are stamps and rulings, never inputs (OL3/BP9)", prop)
			}
		}
	}
}
