package records

import (
	"encoding/json"
	"strings"
	"testing"
)

// S56 T8 (AL11, 2026-11-01): the autonomy family's catalog pins. The
// "no grant kind" property is asserted HERE only on the catalog half (no
// spec twinned to a grant write); the store half (no autonomy_grant kind in
// the CHECK or the Go mirror) lives in internal/store, and the desk half in
// internal/desk — three surfaces, one rule (AL11).

// TestCatalogHasTheFiveAutonomySpecs pins the task's table: exact names,
// dot-name twins, classes and Mutates flags. The three reads are safe and
// non-mutating (they reuse T6's standing methods); the two proposes are
// detectable and the ONLY mutating specs in the family.
func TestCatalogHasTheFiveAutonomySpecs(t *testing.T) {
	want := map[string]struct {
		method  string
		class   string
		mutates bool
	}{
		"autonomy_list":           {"autonomy.list", "safe", false},
		"autonomy_show":           {"autonomy.show", "safe", false},
		"autonomy_effective":      {"autonomy.effective", "safe", false},
		"autonomy_propose_revoke": {"autonomy.proposeRevoke", "detectable", true},
		"autonomy_propose_deny":   {"autonomy.proposeDeny", "detectable", true},
	}
	got := map[string]Spec{}
	for _, sp := range Catalog() {
		if sp.Family == "autonomy" {
			got[sp.Name] = sp
		}
	}
	if len(got) != len(want) {
		t.Fatalf("the autonomy family holds %d specs, want exactly %d: %v", len(got), len(want), mapKeys(got))
	}
	for name, w := range want {
		sp, ok := got[name]
		if !ok {
			t.Fatalf("spec %s is missing from the catalog", name)
		}
		if sp.Method != w.method {
			t.Errorf("%s is twinned to %q, want %q", name, sp.Method, w.method)
		}
		if sp.Class != w.class {
			t.Errorf("%s is class %q, want %q", name, sp.Class, w.class)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v — the two proposes are the family's only mutators", name, sp.Mutates, w.mutates)
		}
	}
}

// TestAutonomySchemasCarryNoIdentityOrState is the "deliberately absent"
// half of the spec: no schema in the family carries an actor/createdBy/
// proposedBy/status/kind/state/conditions TOP-LEVEL field (a nested
// subject.kind is the sanctioned shape), and no description or schema JSON
// anywhere in the family carries the word "allow" — a veto vocabulary that
// names an allow is a smuggle by documentation.
func TestAutonomySchemasCarryNoIdentityOrState(t *testing.T) {
	forbidden := []string{"actor", "createdBy", "proposedBy", "status", "kind", "state", "conditions"}
	for _, sp := range Catalog() {
		if sp.Family != "autonomy" {
			continue
		}
		if i := strings.Index(strings.ToLower(sp.Description), "allow"); i >= 0 {
			t.Errorf("%s: description carries %q at %d — no agent-reachable autonomy schema or hint may name an allow (AL11)", sp.Name, sp.Description[i:i+min(5, len(sp.Description)-i)], i)
		}
		var s map[string]any
		if err := json.Unmarshal(sp.Schema, &s); err != nil {
			t.Fatalf("%s: schema does not parse: %v", sp.Name, err)
		}
		if i := strings.Index(string(sp.Schema), "allow"); i >= 0 {
			t.Errorf("%s: schema JSON carries %q — the word allow has no business in this family", sp.Name, "allow")
		}
		raw, _ := s["properties"].(map[string]any)
		for key := range raw {
			for _, f := range forbidden {
				if key == f {
					t.Errorf("%s: schema carries top-level %q — identity and polarity are stamped or implied, never typed", sp.Name, key)
				}
			}
		}
	}
}

// TestNoGrantTwinAndNoGrantKind is AL11 at the catalog level: no autonomy
// spec is twinned to a grant write (the three person-only doors), and no
// spec's name or method is a grant-twin spelling. The store and desk halves
// of the same rule are tested where those packages live.
func TestNoGrantTwinAndNoGrantKind(t *testing.T) {
	forbiddenTwins := map[string]bool{
		"autonomy.grant":  true,
		"autonomy.deny":   true,
		"autonomy.revoke": true,
	}
	for _, sp := range Catalog() {
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to the person-only door %q — a grant write must never reach an agent (AL11)", sp.Name, sp.Method)
		}
		if strings.Contains(sp.Method, "autonomy.grant") {
			t.Errorf("%s names a grant method %q — no grant twin exists", sp.Name, sp.Method)
		}
	}
}

func mapKeys(m map[string]Spec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
