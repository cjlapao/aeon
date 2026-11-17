package records

// catalog_roadmap_test.go — S50 T5 (RM8): the catalog half of the roadmap
// family. Three specs — two safe reads and the ONE detectable ask — each
// twinned to a standing socket door (rule 4: roadmap_list twinned to
// roadmap.board, roadmap_show twinned to roadmap.show), the schemas carrying
// no identity and no rung, and none of the five granted by default. A
// sixth roadmap spec, or a spec twinned to any of the plane's eight LADDER
// writes (add · update · cancel · estimate · priority · ideate · plan ·
// materialize), is RM8 broken, and the walk says so by name.
// 2026-11-05 (S27 T3): 3 → 5, deliberate — the two graph tools the T3 spec
// names; the counts this file pins are amended here, with their dates.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogHasTheFiveRoadmapSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
		props    int
	}{
		"roadmap_list":         {"roadmap.board", false, "safe", nil, 3},
		"roadmap_show":         {"roadmap.show", false, "safe", []string{"id"}, 1},
		"roadmap_propose_idea": {"roadmap.proposeIdea", true, "detectable", []string{"componentRoot", "title", "note"}, 4},
		// S27 T3: the two graph edges. Required is the pair the DOOR enforces
		// (both ids positive, verbatim); a note is the ask's law, not an
		// edge's — the edge IS the sentence.
		"roadmap_depend":   {"roadmap.depend", true, "detectable", []string{"id", "dependsOn"}, 2},
		"roadmap_undepend": {"roadmap.undepend", true, "detectable", []string{"id", "dependsOn"}, 2},
	}
	seen := map[string]bool{}
	mutators := 0
	for _, sp := range Catalog() {
		if sp.Family != "roadmap" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family roadmap but is not one of the five — the plane ships two reads, ONE add-only ask, and the graph's two edges (S27 T3)", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q — the dot-name IS the person's standing door, and a pair that moved is a different door (rule 4). roadmap_list is twinned to roadmap.board per RM8's own table, NOT to a roadmap.list nobody may name", sp.Name, sp.Method, w.method)
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
			t.Errorf("%s leaves additionalProperties open — the strict decode is what refuses a smuggled `status` (the rung is the person's, not an input)", sp.Name)
		}
		if len(sch.Properties) != w.props {
			t.Errorf("%s carries %d members, want exactly %d — a further one is a field the spec never granted", sp.Name, len(sch.Properties), w.props)
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
			// 2026-11-05 (S27 T3): the note law is the ASK's — the sentence a
			// person reads before deciding. An edge asks nothing: it names
			// two rows and its refusal is its own sentence.
			if sp.Name == "roadmap_propose_idea" && !got["note"] {
				t.Errorf("%s does not require the note — RM8's ask-a-person rule: the sentence a person reads before deciding is the ask itself, and an ask with no why is a log line", sp.Name)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog — the agent surface the spec names is not there", name)
		}
	}
	if mutators != 3 {
		// 2026-11-05 (S27 T3): 1 → 3, deliberate — the ask plus the two edge
		// tools. RM8 was never "no mutating spec"; it is "no LADDER write
		// reaches the agent". The eight that remain out are add · update ·
		// cancel · estimate · priority · ideate · plan · materialize, and
		// the door-package walk still refuses any spec twinned to them.
		t.Errorf("%d mutating roadmap specs, want exactly 3 (the ask and the two graph edges, S27 T3) — a fourth is RM8 broken", mutators)
	}
}

// TestRoadmapSchemasCarryNoIdentityOrRung is rule 1 and RM8 run over all
// three specs: identity is a stamp (OL3) and the LADDER is a ruling —
// status, priority, estimate and provenance are what a person GIVES an
// item at add or later, never inputs an ask can carry. `status` is banned
// on the ask and on show; on roadmap_list it is the board door's own read
// key — roadmap.board's socket params take exactly that filter, and rule 4
// says the twin takes what its door takes (the T4 text-guard family, not
// this schema, is what keeps the rung out of writes). `id` on show is the
// row's address, not identity: the same half of an address automation_show
// takes.
func TestRoadmapSchemasCarryNoIdentityOrRung(t *testing.T) {
	type rule struct {
		spec   string
		banned []string
	}
	identity := []string{
		"actor", "actorkind", "createdby", "createdbykind", "createdbyid", "proposedby", "author",
		"decidedby", "decidedat", "decision", "origin", "legacy", "revision", "baserevision",
		"expectedrevision", "appliedat",
		"granted", "allowed", "defaultgrant", "permission",
		"value", "secret", "secretvalue", "token", "apikey", "password", "credential",
	}
	rung := []string{"status", "priority", "prioritystate", "prioritylabel", "estimate", "estimatemin", "estimatemax", "estimatebasis", "provenance", "kind"}
	rules := []rule{
		{"roadmap_list", append(append([]string{}, identity...), "kind")},
		{"roadmap_show", append(append([]string{}, identity...), rung...)},
		{"roadmap_propose_idea", append(append([]string{}, identity...), rung...)},
		// S27 T3: an edge names two rows and nothing else — the rungs stay
		// banned, and `id`/`dependsOn` are addresses, not identity.
		{"roadmap_depend", append(append([]string{}, identity...), rung...)},
		{"roadmap_undepend", append(append([]string{}, identity...), rung...)},
	}
	all := Catalog()
	for _, r := range rules {
		var sp *Spec
		for i := range all {
			if all[i].Name == r.spec {
				sp = &all[i]
				break
			}
		}
		if sp == nil {
			t.Fatalf("%s is missing from the catalog", r.spec)
		}
		var sch struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
			t.Fatalf("%s schema: %v", r.spec, err)
		}
		if len(sch.Properties) == 0 {
			t.Errorf("%s declares no properties at all — a schema that says nothing approves anything", r.spec)
		}
		for prop := range sch.Properties {
			p := strings.ToLower(strings.ReplaceAll(prop, "_", ""))
			for _, b := range r.banned {
				if p == b {
					t.Errorf("%s carries %q — identity is a stamp, the ladder is a ruling, and neither is ever an input (RM8, OL3)", r.spec, prop)
				}
			}
		}
		// And no schema TEXT invites a ruling into the input: the
		// description of a member is what the model reads when it decides
		// what to put there.
		for prop, raw := range sch.Properties {
			low := strings.ToLower(string(raw))
			for _, phrase := range []string{"the value of", "paste the", "token here", "api key", "secret value", "set enabled", "set status", "set the status", "mark it done"} {
				if strings.Contains(low, phrase) {
					t.Errorf("%s.%s invites a credential or a ruling into the input (%q) — the rung is given by a person, at add or later (RM8)", r.spec, prop, phrase)
				}
			}
		}
	}
}

// TestRoadmapAgentSpecsAreNotDefaultGranted is the catalog's reminder of the
// registry's rule: registered is not granted (S11). These three are in the
// catalog and in the registry and in NOBODY's default set; a step reaches
// them only through a `tool_access` row an operator added.
func TestRoadmapAgentSpecsAreNotDefaultGranted(t *testing.T) {
	seen := 0
	for _, sp := range Catalog() {
		if sp.Family != "roadmap" {
			continue
		}
		seen++
		low := strings.ToLower(string(sp.Schema))
		for _, bad := range []string{`"granted"`, `"defaultgrant"`, `"allowed"`, `"actor"`} {
			if strings.Contains(low, bad) {
				t.Errorf("%s's schema carries %s — the catalog projects WHAT a tool is; the policy rows decide who may call it (S11: registered is not granted)", sp.Name, bad)
			}
		}
	}
	if seen != 5 {
		// 2026-11-05 (S27 T3): 3 → 5, deliberate (the two graph edges).
		t.Errorf("the roadmap family carries %d specs, want exactly 5", seen)
	}
	// The name-level half of the claim (the five are in NO DefaultGrant set)
	// is pinned registry-side — internal/tools imports this package, so the
	// catalog cannot import the registry back, and S59/S49 draw the same
	// line the same way. toolset_test.go pins the literal `read,list,write`
	// set the three can never appear in.
}
