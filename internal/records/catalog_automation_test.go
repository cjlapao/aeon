package records

// catalog_automation_test.go — S49 T7 (AN9, rules 1–3): the automation
// family's five specs, asserted where the catalog is DATA. Four safe reads
// (the tool twins of the person's standing automation.list/show/fires/health
// doors) and ONE ask (class detectable, Mutates:true) that files the INERT
// automation_disable row a person commits. The eleven automation writes —
// create, update, pause, resume, enable, disable, archive, unarchive, test,
// dryrun, and the fire-now path — are the person's doors and have no tool
// (the cmd/aeon guard TestNoAutomationToolCanCreateEnableUpdateArchiveOrFire
// pins the registry side); what is refused HERE is the shape of the five
// that do exist:
//
//   - exactly five in the family, each twinned to its dot-name, each
//     carrying the class and mutability it was granted with;
//   - `additionalProperties: false` on all five — the strict decode is the
//     identity wall, and an open schema is a door a caller walks through
//     with an `enabled: false` in its pocket (the conformance fixture
//     942 proves the wall stands at the door);
//   - no identity member, no state member and no authority member: the
//     author is the step's stamp (OL3), the switch is the person's hand
//     (AN9), and `source` is the ONLY thing here that smells of identity —
//     because the source is part of the automation's NAME (AN1), an input
//     half of the address, never a claim about who or what may act;
//   - and the ask carries exactly three members: source, id, reason. No
//     `enabled`, no `reason`-adjacent decision field, no revision baseline:
//     a proposal that could carry state would be a command wearing a note.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogHasTheFiveAutomationSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
	}{
		"automation_list":            {"automation.list", false, "safe", nil},
		"automation_show":            {"automation.show", false, "safe", []string{"source", "id"}},
		"automation_fires":           {"automation.fires", false, "safe", nil},
		"automation_health":          {"automation.health", false, "safe", nil},
		"automation_propose_disable": {"automation.proposeDisable", true, "detectable", []string{"source", "id", "reason"}},
	}
	seen := map[string]bool{}
	mutators := 0
	for _, sp := range Catalog() {
		if sp.Family != "automation" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family automation but is not one of the five — the plane ships four reads and ONE narrowing ask (AN9)", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q — the dot-name IS the person's standing door, and a pair that moved is a different door", sp.Name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v — only the ask files an INERT row; a read never mutates", sp.Name, sp.Mutates, w.mutates)
		}
		if sp.Class != w.class {
			t.Errorf("%s class %q, want %q — the ask files a row and must say so (detectable); a read claiming detectable teaches the model to fear the wrong tool", sp.Name, sp.Class, w.class)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s description is %d chars — the model must be able to tell this door apart from its neighbours", sp.Name, len(sp.Description))
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
			t.Errorf("%s leaves additionalProperties open — the strict decode is what refuses a smuggled `enabled` (identity fields are stamps, not inputs)", sp.Name)
		}
		got := map[string]bool{}
		for _, r := range sch.Required {
			got[r] = true
		}
		for _, r := range w.required {
			if !got[r] {
				t.Errorf("%s does not require %q — the door's own sentence demands it and a tool must not answer the softer question", sp.Name, r)
			}
		}
		if len(sch.Required) != len(w.required) {
			t.Errorf("%s requires %v, want exactly %v — a required the door does not enforce narrows the twin and breaks the parity the pair exists to keep (rule 4)", sp.Name, sch.Required, w.required)
		}
		if sp.Mutates {
			mutators++
			if len(sch.Properties) != 3 {
				t.Errorf("the ask carries %d members, want exactly 3 (source, id, reason) — a fourth is a field the spec never granted, and `enabled` as input would be a command wearing a note", len(sch.Properties))
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog — the agent surface the spec names is not there", name)
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating automation specs, want exactly 1 (the ask) — a second write spec is AN9 broken", mutators)
	}
}

// TestSchemasCarryNoIdentityOrAuthorityFields is rule 1 run over ALL FIVE
// specs, not only the ask: identity and state are stamps and rulings, never
// inputs (OL3), and a proposal that could carry `enabled` or `archived`
// would be a widening smuggled inside a note (AN9). `source` is deliberately
// NOT banned — the source is part of the automation's name (AN1), the input
// half of the address a read needs to aim; `outcome`, `blueprint`, `q`,
// `limit` and `beforeId` are the door's own read-keys, accepted by its twin
// at the socket too.
func TestSchemasCarryNoIdentityOrAuthorityFields(t *testing.T) {
	banned := []string{
		"actor", "createdby", "createdbykind", "createdbyid", "proposedby", "author",
		"status", "state", "kind", "decision", "decidedby", "decidedat",
		"enabled", "disabled", "paused", "archived", "archivedat", "archivedreason",
		"revision", "baserevision", "expectedrevision", "appliedat", "firedat",
		"triggerid", "scheduleid", "eventtriggerid",
		"granted", "allowed", "defaultgrant", "permission",
		"value", "secret", "secretvalue", "token", "apikey", "password", "credential",
	}
	names := []string{"automation_list", "automation_show", "automation_fires", "automation_health", "automation_propose_disable"}
	all := Catalog()
	for _, name := range names {
		var sp *Spec
		for i := range all {
			if all[i].Name == name {
				sp = &all[i]
				break
			}
		}
		if sp == nil {
			t.Fatalf("%s is missing from the catalog", name)
		}
		var sch struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
		if len(sch.Properties) == 0 {
			t.Errorf("%s declares no properties at all — a schema that says nothing approves anything", name)
		}
		for prop := range sch.Properties {
			p := strings.ToLower(strings.ReplaceAll(prop, "_", ""))
			for _, b := range banned {
				if p == b {
					t.Errorf("%s carries %q — identity, state and authority are stamps and rulings, never inputs (AU1, AN9, OL3)", name, prop)
				}
			}
		}
		// And no schema TEXT invites a credential or a decision: the
		// description of a member is what the model reads when it decides
		// what to put there.
		for prop, raw := range sch.Properties {
			low := strings.ToLower(string(raw))
			for _, phrase := range []string{"the value of", "paste the", "token here", "api key", "secret value", "set enabled", "set status"} {
				if strings.Contains(low, phrase) {
					t.Errorf("%s.%s invites a credential or a ruling into the input (%q) — a credential goes IN through a person's door and a switch is thrown by a person (AN9)", name, prop, phrase)
				}
			}
		}
	}
}

// TestAutomationAgentSpecsAreNotDefaultGranted is the catalog's reminder of
// the registry's rule: registered is not granted (S11). These five are in
// the catalog and in the registry and in NOBODY's default set; a step
// reaches them only through a `tool_access` row an operator added.
func TestAutomationAgentSpecsAreNotDefaultGranted(t *testing.T) {
	for _, sp := range Catalog() {
		if sp.Family != "automation" {
			continue
		}
		low := strings.ToLower(string(sp.Schema))
		for _, bad := range []string{`"granted"`, `"defaultgrant"`, `"allowed"`, `"actor"`} {
			if strings.Contains(low, bad) {
				t.Errorf("%s's schema carries %s — the catalog projects WHAT a tool is; the policy rows decide who may call it (S11: registered is not granted)", sp.Name, bad)
			}
		}
	}
}
