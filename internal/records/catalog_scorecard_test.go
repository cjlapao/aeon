package records

// catalog_scorecard_test.go — S63 T6 (RT11, RT3, standing rule 2): the
// scorecard family's three, checked as DATA. This package imports nothing
// outside the standard library (TestRecordsCatalogStillImportsNothing owns
// that law), so every claim here is about the specs themselves: names,
// twins, classes, descriptions, and — the load-bearing one — the SHAPE of
// the input schemas, because the schema is where "the subject is stamped,
// never chosen" is enforced before any service runs.

import (
	"encoding/json"
	"strings"
	"testing"
)

// scorecardSpecs returns the family's rows, failing on the exact count.
func scorecardSpecs(t *testing.T) []Spec {
	t.Helper()
	var got []Spec
	for _, sp := range Catalog() {
		if sp.Family == "scorecard" {
			got = append(got, sp)
		}
	}
	if len(got) != 3 {
		t.Fatalf("the scorecard family has %d specs, want exactly 3 (T6)", len(got))
	}
	return got
}

func TestCatalogHasTheThreeScorecardSpecs(t *testing.T) {
	want := map[string]string{
		"scorecard_mine":      "scorecard.agent",
		"scorecard_component": "scorecard.component",
		"digest_learned":      "digest.learned",
	}
	seen := map[string]bool{}
	for _, sp := range scorecardSpecs(t) {
		exp, ok := want[sp.Name]
		if !ok {
			t.Errorf("unexpected scorecard spec %q — the family is exactly the three", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != exp {
			t.Errorf("%s twinned to %q, want the dot-name twin %q", sp.Name, sp.Method, exp)
		}
		if sp.Class != "safe" {
			t.Errorf("%s class %q, want safe — a measurement never mutates (RT3)", sp.Name, sp.Class)
		}
		if sp.Mutates {
			t.Errorf("%s Mutates:true — NO WRITE EXISTS IN THIS FAMILY (RT11)", sp.Name)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s description is %d bytes, want at least 40", sp.Name, len(sp.Description))
		}
		low := strings.ToLower(sp.Description)
		if !strings.Contains(low, "measurement") || !strings.Contains(low, "not an instruction") {
			t.Errorf("%s description does not carry RT3's shape — a measurement, not an instruction: %s", sp.Name, sp.Description)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("scorecard spec %q is missing — every one has a socket twin to name", name)
		}
	}
}

// The schema IS the scope rule: no identity property can exist, and an
// unknown key is a parse error, never a shrug (additionalProperties:false).
func TestScorecardSchemasCarryNoIdentity(t *testing.T) {
	identity := map[string]bool{"agent": true, "slug": true, "id": true, "root": true, "rootid": true,
		"component": true, "componentid": true, "componentroot": true, "actor": true, "agentkind": true}
	for _, sp := range scorecardSpecs(t) {
		var s struct {
			Type                 string                    `json:"type"`
			AdditionalProperties *bool                     `json:"additionalProperties"`
			Properties           map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &s); err != nil {
			t.Fatalf("%s: schema does not decode: %v", sp.Name, err)
		}
		if s.Type != "object" {
			t.Errorf("%s: schema type %q, want object", sp.Name, s.Type)
		}
		if s.AdditionalProperties == nil || *s.AdditionalProperties {
			t.Errorf("%s: additionalProperties is not false — a smuggled subject would PARSE, which is the whole failure RT11 forbids", sp.Name)
		}
		for k := range s.Properties {
			if identity[strings.ToLower(k)] {
				t.Errorf("%s: property %q names an identity — the subject is STAMPED by the service (F13), never chosen by the caller", sp.Name, k)
			}
			if k != "since" && k != "until" && k != "last" {
				t.Errorf("%s: property %q is neither since, until nor last — the family's inputs are the window and nothing else", sp.Name, k)
			}
		}
	}
}

// The parity the family must keep (forward direction, data-only — the
// protocol package's records_parity_test walks the live table both ways):
// three names, three distinct twins, none shared with another spec.
func TestRecordsParityScorecardFamily(t *testing.T) {
	twins := map[string]string{}
	for _, sp := range Catalog() {
		if sp.Family != "scorecard" {
			continue
		}
		if prev, dup := twins[sp.Method]; dup {
			t.Errorf("%s and %s share the twin %q — one door, one tool", prev, sp.Name, sp.Method)
		}
		twins[sp.Method] = sp.Name
	}
	if len(twins) != 3 {
		t.Errorf("the family has %d distinct twins, want 3", len(twins))
	}
	for _, sp := range scorecardSpecs(t) {
		if !strings.Contains(sp.Method, ".") {
			t.Errorf("%s: method %q is not a socket twin name (dot form)", sp.Name, sp.Method)
		}
	}
}
