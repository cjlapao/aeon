package records

// catalog_platform_test.go — S54 T4 (SP3, SP4): the platform family's
// catalog census. What each test pins, in order:
//
//   - the family carries EXACTLY ONE spec — `index_status`, a safe
//     non-mutating read twinned to `platform.status` — the schema is the
//     empty object (the scope is the step's stamp, not a param, OL3);
//   - no operation of the platform plane reaches a spec: no reindex, no
//     rebuild, no graph build/reparse/delete, dotted or underscored, as a
//     name or as a Method, anywhere in the catalog (SP3 — an operation
//     spends a person's money and may discard derived data);
//   - the proposal vocabulary carries no platform kind: an ask cannot
//     carry what a tool may not file (SP4's "no proposal kind").

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
)

func TestCatalogHasTheOnePlatformSpec(t *testing.T) {
	var got []Spec
	for _, sp := range Catalog() {
		if sp.Family == "platform" {
			got = append(got, sp)
		}
	}
	if len(got) != 1 {
		names := make([]string, len(got))
		for i, sp := range got {
			names[i] = sp.Name
		}
		t.Fatalf("the platform family holds %d specs (%v), want exactly index_status (SP4)", len(got), names)
	}
	sp := got[0]
	if sp.Name != "index_status" || sp.Method != "platform.status" || sp.Class != "safe" || sp.Mutates {
		t.Errorf("the platform spec is %s/%s class %s mutates=%v, want index_status/platform.status safe non-mutating",
			sp.Name, sp.Method, sp.Class, sp.Mutates)
	}
	// The empty schema, spoken exactly: an object, no properties, nothing
	// else permitted. A scope parameter here would be the OL3 violation the
	// whole door exists to avoid.
	var doc map[string]any
	if err := json.Unmarshal(sp.Schema, &doc); err != nil {
		t.Fatalf("the spec schema is not JSON: %v", err)
	}
	if doc["type"] != "object" || doc["additionalProperties"] != false {
		t.Errorf("the spec schema is %s", sp.Schema)
	}
	if props, ok := doc["properties"].(map[string]any); !ok || len(props) != 0 {
		t.Errorf("the spec schema carries input %s — index_status is scoped by the step, not by a param", sp.Schema)
	}
	// The tool's own sentence must carry the door the operations live
	// behind, so a model reading the vocabulary finds the law beside the
	// read, not a mystery refusal later.
	if !strings.Contains(sp.Description, "person's door") {
		t.Errorf("the description hides the human door: %s", sp.Description)
	}
}

func TestNoPlatformOperationReachesTheCatalog(t *testing.T) {
	// SP3's four operations and every tool-shaped spelling of them,
	// scanned across EVERY spec's name and method — the family census above
	// says the platform family has one row; this says no operation sneaks
	// into another family's clothing.
	words := []string{"reindex", "reparse", "graph_build", "graph.delete", "graph_delete",
		"graph.build", "index_rebuild", "index_build", "rebuild"}
	for _, sp := range Catalog() {
		hay := strings.ToLower(sp.Name + "|" + sp.Method)
		for _, w := range words {
			if strings.Contains(hay, w) {
				t.Errorf("spec %s (%s) names the operation %q — operations are a person's door (SP3)", sp.Name, sp.Method, w)
			}
		}
	}
}

func TestNoPlatformProposalKindExists(t *testing.T) {
	// SP4's second half: the tool does not exist, so no ask may carry it
	// either. The kind vocabulary is closed (desk); it stays platform-free.
	for _, k := range desk.Kinds() {
		lower := strings.ToLower(string(k))
		if strings.Contains(lower, "platform") || strings.Contains(lower, "reindex") ||
			strings.Contains(lower, "reparse") || strings.Contains(lower, "rebuild") {
			t.Errorf("proposal kind %q reaches the platform plane — SP4 rules out any such kind", k)
		}
	}
}
