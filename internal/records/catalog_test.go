package records

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	snake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, sp := range Catalog() {
		if seen[sp.Name] {
			t.Errorf("%s appears twice — a grant list with a duplicate is a grant list nobody trusts", sp.Name)
		}
		seen[sp.Name] = true
		if !snake.MatchString(sp.Name) {
			t.Errorf("%q is not snake_case", sp.Name)
		}
		if !strings.Contains(sp.Method, ".") {
			t.Errorf("%s: method %q is not a socket twin name", sp.Name, sp.Method)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s: description is %d characters — the model cannot tell a %d-character hint apart from another", sp.Name, len(sp.Description), len(sp.Description))
		}
		if sp.Class != "safe" && sp.Class != "detectable" {
			t.Errorf("%s: class %q — unsafe is not on this catalog (a row write needs no receipt ceremony)", sp.Name, sp.Class)
		}
		var s map[string]any
		if err := json.Unmarshal(sp.Schema, &s); err != nil {
			t.Fatalf("%s: schema does not parse: %v", sp.Name, err)
		}
		if s["additionalProperties"] != false {
			t.Errorf("%s: schema must close the door — additionalProperties:false is what makes a smuggled identity field a parse error", sp.Name)
		}
		if raw, ok := s["properties"].(map[string]any); ok {
			for smuggled := range raw {
				switch smuggled {
				case "component_root", "loop_slug", "created_by", "author", "source_step", "agent":
					// These name the WRITER's world. The service stamps them
					// from the step (OL3), so a write schema that even names
					// them invites the argument the stamp exists to end.
					if sp.Mutates {
						t.Errorf("%s: write schema carries %q — identity is stamped by the service, never argued from input (OL3)", sp.Name, smuggled)
					}
				case "root_id":
					// A READ may filter by root — a question, not an
					// identity. A goal/brief/attachment write may not carry
					// it either: its root comes from its loop. But filing
					// a NEW component or epic somewhere is the author's
					// legitimate choice — the container is an argument; the
					// AUTHOR never is.
					if sp.Mutates && (sp.Family == "goal" || sp.Family == "brief" || sp.Family == "task" || sp.Family == "attachment") {
						t.Errorf("%s: write schema carries root_id — its root comes from the step's loop (OL3)", sp.Name)
					}
				}
			}
		}
	}
}

// TestCatalogHasNoAuthorityActions is OL2/OL8's wire, amended 2026-10-31
// for S45 PM9: the human doors stay human, and the two families S45 opened
// — policy and proposal — may appear here ONLY as reads and as the two
// propose twins. The amendment extends the forbidden-name rule to what the
// slice actually permits, it does not delete it: a policy/proposal-named
// spec must be a read (Mutates:false) or one of {policy_propose_add,
// policy_propose_remove} with its exact dot-name twin, and every mutating
// entry beyond those two is still the side door the original test named.
// decide/accept/approve/trigger/mcp stay forbidden across the WHOLE catalog
// (the task family's trigger actions are T10's and join under their own
// audit, never silently through this hole).
func TestCatalogHasNoAuthorityActions(t *testing.T) {
	forbiddenWholeCatalog := []string{"decide", "accept", "approve", "trigger", "mcp"}
	policyProposeTwins := map[string]string{
		"policy_propose_add":    "policy.proposeAdd",
		"policy_propose_remove": "policy.proposeRemove",
	}
	// Amended 2026-11-02 by S46 T8 (MG8/MG9) — the same PM9 shape S45 used
	// for its own family: the mcp NAMED five join, each WITH its exact
	// `mcp.mine.*` dot-name twin, the reads Mutates:false and the two
	// proposes the only Mutates entries in the family. The names outside the
	// map stay forbidden whole-catalog — mcp.add/declare/enable/import/
	// commit meet the sentence below, and T7's TestMcpNegativeDoors plus
	// its catalog subtest (which re-runs this claim against the SAME
	// Catalog()) keep the wider boundary. An mcp-named spec nobody listed
	// is still the side door the original test named.
	mcpNamedFive := map[string]string{
		"mcp_servers":               "mcp.mine.servers",
		"mcp_grants":                "mcp.mine.grants",
		"mcp_status":                "mcp.mine.status",
		"mcp_propose_remove_server": "mcp.mine.proposeRemove",
		"mcp_propose_revoke_grant":  "mcp.mine.proposeRevoke",
	}
	for _, sp := range Catalog() {
		for _, f := range forbiddenWholeCatalog {
			if strings.Contains(sp.Name, f) {
				if want, ok := mcpNamedFive[sp.Name]; ok {
					if sp.Method != want {
						t.Errorf("%s twinned to %q, want %q — S46 T8 names the five mcp.mine.* doors exactly", sp.Name, sp.Method, want)
					}
					if sp.Mutates && !strings.HasPrefix(sp.Name, "mcp_propose_") {
						t.Errorf("%s mutates and is not a propose twin — the agent's MCP surface reads and proposes, it never writes (MG9)", sp.Name)
					}
					continue
				}
				t.Errorf("%s contains %q — the human doors stay human (OL2/OL8): a granted agent's catalog cannot hold one", sp.Name, f)
			}
		}
		if strings.Contains(sp.Name, "policy") || strings.Contains(sp.Name, "proposal") {
			if want, ok := policyProposeTwins[sp.Name]; ok {
				if sp.Method != want {
					t.Errorf("%s twinned to %q, want %q — the two propose twins are named, exactly, by S45 PM9", sp.Name, sp.Method, want)
				}
				continue // the twins are permitted WITH their method; the mutating-shape rules are TestCatalogHasTheSevenPolicySpecs'
			}
			if sp.Mutates {
				t.Errorf("%s mutates and is not one of the two propose twins — an agent may PROPOSE, never decide, add, remove or seed (PM9)", sp.Name)
			}
		}
	}
}

// TestCatalogHasTheSevenPolicySpecs is S45 T7's table pinned: exact names,
// dot-name twins, classes and Mutates flags as the task's table says. The
// five reads are safe and non-mutating; the two propose twins are
// detectable and the ONLY mutating specs in the two families.
func TestCatalogHasTheSevenPolicySpecs(t *testing.T) {
	want := []Spec{
		{Name: "policy_list", Method: "policy.list", Family: "policy", Class: "safe"},
		{Name: "policy_show", Method: "policy.get", Family: "policy", Class: "safe"},
		{Name: "policy_why", Method: "policy.why", Family: "policy", Class: "safe"},
		{Name: "policy_in_force", Method: "policy.inForce", Family: "policy", Class: "safe"},
		{Name: "proposal_list", Method: "proposal.list", Family: "proposal", Class: "safe"},
		{Name: "policy_propose_add", Method: "policy.proposeAdd", Family: "policy", Class: "detectable", Mutates: true},
		{Name: "policy_propose_remove", Method: "policy.proposeRemove", Family: "policy", Class: "detectable", Mutates: true},
	}
	byName := map[string]Spec{}
	for _, sp := range Catalog() {
		byName[sp.Name] = sp
	}
	for _, w := range want {
		got, ok := byName[w.Name]
		if !ok {
			t.Errorf("%s is missing from the catalog — PM9's table names it", w.Name)
			continue
		}
		if got.Method != w.Method {
			t.Errorf("%s twinned to %q, want %q — one definition, two doors", w.Name, got.Method, w.Method)
		}
		if got.Family != w.Family {
			t.Errorf("%s family %q, want %q", w.Name, got.Family, w.Family)
		}
		if got.Class != w.Class {
			t.Errorf("%s class %q, want %q", w.Name, got.Class, w.Class)
		}
		if got.Mutates != w.Mutates {
			t.Errorf("%s Mutates=%v, want %v", w.Name, got.Mutates, w.Mutates)
		}
	}
	// Siblings, additively: the two families exist, and exactly the two
	// propose twins mutate among them.
	families := map[string]bool{}
	mutators := 0
	for _, sp := range Catalog() {
		if sp.Family == "policy" || sp.Family == "proposal" {
			families[sp.Family] = true
			if sp.Mutates {
				mutators++
			}
		}
	}
	if !families["policy"] || !families["proposal"] {
		t.Error("the policy/proposal families are absent — T7's seven rows did not land")
	}
	if mutators != 2 {
		t.Errorf("%d mutating specs in the policy/proposal families, want exactly 2 (the propose twins)", mutators)
	}
}

// TestProposeSchemasCarryNoIdentityOrStatus is OL3's strict-schema rule for
// the two propose specs: no source, actor, createdBy, status or kind field
// anywhere (identity is stamped, status is the store's), and the action
// enum is EXACTLY deny and ask — an allow is not a representable input.
// kind-names-target: the nested target object's `kind` member names the
// TARGET kind, which is policy content, not the proposal's kind column —
// the check therefore walks top-level properties, and the proposal-kind
// field it forbids by name would sit at the top level, where the strict
// schema already closed the door.
func TestProposeSchemasCarryNoIdentityOrStatus(t *testing.T) {
	byName := map[string]Spec{}
	for _, sp := range Catalog() {
		byName[sp.Name] = sp
	}
	for _, name := range []string{"policy_propose_add", "policy_propose_remove"} {
		sp, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		var s map[string]any
		if err := json.Unmarshal(sp.Schema, &s); err != nil {
			t.Fatalf("%s: schema does not parse: %v", name, err)
		}
		if s["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false — a smuggled field must be a parse error", name)
		}
		props, _ := s["properties"].(map[string]any)
		for _, smuggled := range []string{"source", "actor", "createdBy", "status", "kind"} {
			if _, ok := props[smuggled]; ok {
				t.Errorf("%s carries %q — identity is stamped and status is the store's; a schema that names them invites the argument the stamp exists to end (OL3)", name, smuggled)
			}
		}
		if name != "policy_propose_add" {
			continue
		}
		action, ok := props["action"].(map[string]any)
		if !ok {
			t.Fatal("policy_propose_add has no action property")
		}
		raw, _ := action["enum"].([]any)
		if len(raw) != 2 || raw[0] != "deny" || raw[1] != "ask" {
			t.Errorf("the action enum is %v — exactly [\"deny\",\"ask\"], never allow", raw)
		}
	}
}

// TestNoDecisionToolExists is PM9's negative half as POSITIVE data
// (T6's guard asserts it too): no spec in the catalog is twinned to a
// decision or authoring method — proposal.commit/reject/discard,
// policy.add/remove/seedCorpus — and no spec's name speaks one. An agent
// can never decide, and `proposal.discard` is no agent tool either:
// retracting a proposal is a human act in this slice.
func TestNoDecisionToolExists(t *testing.T) {
	forbiddenTwins := []string{
		"proposal.commit", "proposal.reject", "proposal.discard",
		"policy.add", "policy.remove", "policy.seedCorpus",
	}
	for _, sp := range Catalog() {
		for _, f := range forbiddenTwins {
			if sp.Method == f {
				t.Errorf("%s is twinned to %q — no decision or authoring method reaches an agent through the catalog (PM9)", sp.Name, f)
			}
		}
	}
}

// TestCatalogRunStartRow pins S39 T10's row: the one loop action an agent
// may take exists, is detectable (its every attempt leaves a receipt),
// mutates, speaks with at least forty characters of care, and trips no
// forbidden-name wire (run_start names no authority verb — checked, the
// forbidden list passes it).
func TestCatalogRunStartRow(t *testing.T) {
	var found *Spec
	for i, sp := range Catalog() {
		if sp.Name == "run_start" {
			found = &Catalog()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("run_start is missing from the catalog — the agent's one loop action is its row, and no projection runs without it")
	}
	if found.Class != "detectable" {
		t.Errorf("class %q, want detectable — every attempt is a receipt", found.Class)
	}
	if !found.Mutates {
		t.Error("run_start must be marked Mutates — a run is work begun")
	}
	if len(found.Description) < 40 {
		t.Errorf("description is %d chars, want at least 40 — the schema is what the agent trusts", len(found.Description))
	}
	if found.Method != "loop.run" {
		t.Errorf("method %q, want loop.run — one row, two doors", found.Method)
	}
	for _, f := range []string{"decide", "accept", "approve", "trigger", "policy", "mcp", "proposal"} {
		if strings.Contains(found.Name, f) {
			t.Errorf("run_start contains %q — it would trip the human-doors wire (it must not; this pin proves it)", f)
		}
	}
}

func TestCatalogPackageImportsNeitherStoreNorTools(t *testing.T) {
	// The layering pin, measured the only way that counts: the real
	// dependency list. tools imports records (to project it) and daemon
	// imports both — neither may pull the other in.
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(line, "/internal/store") || strings.HasSuffix(line, "/internal/tools") {
			t.Fatalf("the pure-data catalog depends on %s — the layering is the point of this package", line)
		}
	}
}

// TestCatalogHasTheFiveMcpSpecs — S46 T8 (MG8/MG9) by name: the five mcp
// family specs exist, each twinned to its mcp.mine.* door, the three reads
// safe and non-mutating, the two proposes the ONLY mutators of the family
// (and the whole catalog's mutating set grows by exactly those two),
// descriptions the model can act on, proposes requiring their reason.
func TestCatalogHasTheFiveMcpSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		required []string
	}{
		"mcp_servers":               {"mcp.mine.servers", false, nil},
		"mcp_grants":                {"mcp.mine.grants", false, nil},
		"mcp_status":                {"mcp.mine.status", false, nil},
		"mcp_propose_remove_server": {"mcp.mine.proposeRemove", true, []string{"server", "reason"}},
		"mcp_propose_revoke_grant":  {"mcp.mine.proposeRevoke", true, []string{"server", "tool", "reason"}},
	}
	seen := map[string]bool{}
	mcpMutators := 0
	for _, sp := range Catalog() {
		w, ok := want[sp.Name]
		if !ok {
			if sp.Family == "mcp" && sp.Mutates {
				t.Errorf("%s (family mcp) mutates and is not one of the two narrowing proposes — MG9 has no widening kind", sp.Name)
			}
			continue
		}
		seen[sp.Name] = true
		if sp.Family != "mcp" {
			t.Errorf("%s family %q, want mcp", sp.Name, sp.Family)
		}
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q", sp.Name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v — an own-view READ never mutates; a propose files an INERT row", sp.Name, sp.Mutates, w.mutates)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s description is %d chars — the model must be able to tell this door apart", sp.Name, len(sp.Description))
		}
		if len(w.required) == 0 {
			continue
		}
		var sch struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
			t.Fatalf("%s schema: %v", sp.Name, err)
		}
		have := map[string]bool{}
		for _, r := range sch.Required {
			have[r] = true
		}
		for _, r := range w.required {
			if !have[r] {
				t.Errorf("%s does not require %q — a propose without its reason/name is a row a person decides blind", sp.Name, r)
			}
		}
		mcpMutators++
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is absent from the catalog — the agent door is gone", name)
		}
	}
	if mcpMutators != 2 {
		t.Errorf("%d mutating mcp specs, want exactly the two proposes", mcpMutators)
	}
}

// TestMineSchemasCarryNoIdentityOrStatus — MC2/OL3/MG9 at the schema level:
// none of the five input schemas has a member where an agent could name
// another agent, smuggle a status, or pre-author a decision. Schemas close
// their properties; this test names every forbidden member so a future
// "helpful" field is a refused diff.
func TestMineSchemasCarryNoIdentityOrStatus(t *testing.T) {
	five := map[string]bool{
		"mcp_servers": true, "mcp_grants": true, "mcp_status": true,
		"mcp_propose_remove_server": true, "mcp_propose_revoke_grant": true,
	}
	banned := []string{"agent", "actor", "author", "user", "identity", "kind", "status", "source", "project", "loop", "component", "step", "createdby", "state", "decision"}
	for _, sp := range Catalog() {
		if !five[sp.Name] {
			continue
		}
		var sch struct {
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(sp.Schema), &sch); err != nil {
			t.Fatalf("%s schema: %v", sp.Name, err)
		}
		if sch.AdditionalProperties == nil || *sch.AdditionalProperties {
			t.Errorf("%s does not close its properties — the strict decode IS the identity wall", sp.Name)
		}
		for prop := range sch.Properties {
			p := strings.ToLower(prop)
			for _, b := range banned {
				if p == b {
					t.Errorf("%s carries the member %q — identity, status and decisions are stamps and rulings, never inputs (OL3/MC2)", sp.Name, prop)
				}
			}
		}
	}
}

// TestCatalogHasTheNineGateSpecs — S57 T9 by name: the nine gate-family
// specs exist, each twinned to its own door (the six reads reuse T8's
// socket methods; the three new twins get their new ones), family, class
// and mutation flag correct; the THREE mutators are the family's only
// mutators; no spec is twinned to one of the nine human-door writes; and
// the nine descriptions are each at least forty characters AND mutually
// distinct — a model that cannot tell the sibling doors apart picks the
// wrong one.
func TestCatalogHasTheNineGateSpecs(t *testing.T) {
	want := map[string]struct {
		method  string
		family  string
		class   string
		mutates bool
	}{
		"verify_list":               {"verify.list", "verify", "safe", false},
		"verify_show":               {"verify.show", "verify", "safe", false},
		"verify_runs":               {"verify.runs", "verify", "safe", false},
		"mergerule_show":            {"mergerule.get", "mergerule", "safe", false},
		"mergerule_preview":         {"mergerule.preview", "mergerule", "safe", false},
		"dod_list":                  {"dod.list", "dod", "safe", false},
		"verify_propose_add":        {"verify.proposeAdd", "verify", "detectable", true},
		"mergerule_propose_tighten": {"mergerule.proposeTighten", "mergerule", "detectable", true},
		"dod_mark":                  {"dod.markAsAgent", "dod", "detectable", true},
	}
	humanWrites := map[string]bool{
		"verify.add": true, "verify.update": true, "verify.archive": true,
		"verify.restore": true, "verify.applyTemplate": true, "verify.try": true,
		"mergerule.set": true, "mergerule.clear": true, "dod.mark": true,
	}
	seen := map[string]bool{}
	mutators := 0
	ownerByDescription := map[string]string{}
	for _, sp := range Catalog() {
		if sp.Family != "verify" && sp.Family != "mergerule" && sp.Family != "dod" {
			continue
		}
		if other, dup := ownerByDescription[sp.Description]; dup {
			t.Errorf("%s shares its description with %s — the nine are siblings and the model must tell them apart", sp.Name, other)
		}
		ownerByDescription[sp.Description] = sp.Name
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s (family %s) is a gate-family spec nobody declared — a gate door beyond the nine has been smuggled in", sp.Name, sp.Family)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method || sp.Family != w.family || sp.Class != w.class || sp.Mutates != w.mutates {
			t.Errorf("%s = {%s %s %s %v}, want {%s %s %s %v} — one definition, two doors (S38 OL10)", sp.Name, sp.Method, sp.Family, sp.Class, sp.Mutates, w.method, w.family, w.class, w.mutates)
		}
		if humanWrites[sp.Method] {
			t.Errorf("%s is twinned to %q — a human-door write never reaches an agent through the catalog (VG17)", sp.Name, sp.Method)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s's description is %d chars — the model must be able to tell this door apart from its siblings", sp.Name, len(sp.Description))
		}
		if sp.Mutates {
			mutators++
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is absent from the catalog — the agent's gate door is gone", name)
		}
	}
	if mutators != 3 {
		t.Errorf("%d mutating gate-family specs, want exactly the three named twins — a read never mutates and a propose files only an INERT row", mutators)
	}
}

// TestGateProposeSchemasAdmitOnlyTighteningValues — T9's restrictive-only
// at the SCHEMA level: where the domain has an enum with a clear
// direction, the mergerule_propose_tighten schema's closed set for that
// field is EXACTLY the tightening value (a loosening the model cannot
// even type); required_checks is a list (additive — the service's
// Tightens is the second line of defence); and verify_propose_add's
// requirement enum is exactly the gate's closed vocabulary.
func TestGateProposeSchemasAdmitOnlyTighteningValues(t *testing.T) {
	find := func(name string) *Spec {
		for i, sp := range Catalog() {
			if sp.Name == name {
				return &Catalog()[i]
			}
		}
		t.Fatalf("%s is absent — the tightening test has nothing to tighten", name)
		return nil
	}
	var tighten struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(find("mergerule_propose_tighten").Schema), &tighten); err != nil {
		t.Fatalf("mergerule_propose_tighten schema: %v", err)
	}
	fieldsRaw, ok := tighten.Properties["fields"]
	if !ok {
		t.Fatal("mergerule_propose_tighten carries no `fields` object — a rule is proposed whole, in one object")
	}
	var fields struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(fieldsRaw, &fields); err != nil {
		t.Fatalf("the fields object: %v", err)
	}
	for _, col := range []string{"enabled", "requireLocal", "requireDod", "requiredChecks", "checksPolicy", "noChecks", "reviewMode", "aeonReviewAgents", "requireThreadsResolved", "method"} {
		found := false
		for _, r := range fields.Required {
			if r == col {
				found = true
			}
		}
		if !found {
			t.Errorf("fields does not require %q — a half-rule is where a loosening hides (T8's full-rule law)", col)
		}
	}
	wantEnums := map[string][]string{
		"noChecks":     {"refuse"},
		"requireDod":   {"all_met"},
		"reviewMode":   {"both"},
		"checksPolicy": {"reported_green"},
	}
	for col, wantValues := range wantEnums {
		raw, ok := fields.Properties[col]
		if !ok {
			t.Errorf("fields carries no %q", col)
			continue
		}
		var e struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("fields.%s: %v", col, err)
		}
		if e.Type != "string" || len(e.Enum) != len(wantValues) {
			t.Errorf("fields.%s admits %v — where the domain has a direction, the schema admits only the tightening value", col, e)
			continue
		}
		for i, v := range wantValues {
			if e.Enum[i] != v {
				t.Errorf("fields.%s admits %v, want exactly %v — the schema is the first line, Tightens the second", col, e.Enum, wantValues)
			}
		}
	}
	var checks struct {
		Type string `json:"type"`
	}
	if raw, ok := fields.Properties["requiredChecks"]; ok {
		if err := json.Unmarshal(raw, &checks); err != nil {
			t.Fatalf("fields.requiredChecks: %v", err)
		}
		if checks.Type != "array" {
			t.Errorf("requiredChecks is %q, want an additive list (dropping a name is a loosening Tightens refuses)", checks.Type)
		}
	}
	var add struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(find("verify_propose_add").Schema), &add); err != nil {
		t.Fatalf("verify_propose_add schema: %v", err)
	}
	var req struct {
		Type string   `json:"type"`
		Enum []string `json:"enum"`
	}
	if raw, ok := add.Properties["requirement"]; ok {
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("verify_propose_add.requirement: %v", err)
		}
		if len(req.Enum) != 2 || (req.Enum[0] != "required" && req.Enum[0] != "advisory") || (req.Enum[1] != "required" && req.Enum[1] != "advisory") {
			t.Errorf("verify_propose_add.requirement admits %v — the closed set is exactly required|advisory", req.Enum)
		}
	} else {
		t.Error("verify_propose_add carries no requirement — a gate is declared whole")
	}
}

// TestGateProposeAndMarkSchemasCarryNoIdentity — T9/OL3 at the schema
// level for the three MUTATING gate specs: no member where an agent could
// name an identity, smuggle a status, pre-author a decision or pick the
// component (the daemon stamps all of them from the step); every object
// level closes its properties, so a smuggled field is a PARSE error, not
// a shrug.
func TestGateProposeAndMarkSchemasCarryNoIdentity(t *testing.T) {
	three := map[string]bool{
		"verify_propose_add": true, "mergerule_propose_tighten": true, "dod_mark": true,
	}
	banned := []string{"agent", "actor", "author", "user", "identity", "kind", "status", "source", "origin", "createdby", "created_by", "componentid", "component_id"}
	var walk func(sp *Spec, raw json.RawMessage, where string)
	walk = func(sp *Spec, raw json.RawMessage, where string) {
		var sch struct {
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &sch); err != nil {
			t.Fatalf("%s %s: %v", sp.Name, where, err)
		}
		if sch.AdditionalProperties == nil || *sch.AdditionalProperties {
			t.Errorf("%s %s does not close its properties — the strict decode IS the identity wall", sp.Name, where)
		}
		for prop, sub := range sch.Properties {
			p := strings.ToLower(prop)
			for _, b := range banned {
				if p == b {
					t.Errorf("%s %s carries the member %q — identity, status and decisions are stamps, never inputs (OL3)", sp.Name, where, prop)
				}
			}
			var shape struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(sub, &shape); err == nil && shape.Type == "object" {
				walk(sp, sub, where+"."+prop)
			}
		}
	}
	for i, sp := range Catalog() {
		if three[sp.Name] {
			walk(&Catalog()[i], sp.Schema, "schema")
		}
	}
}

// TestProposeSchemasAdmitOnlyTighteningValues pins S57 T9's schema rule:
// where the domain has an enum with a clear direction, the agent's
// tightening schema admits ONLY the tightening value — the loosening
// value is not a representable input, so a model cannot even type the
// argument the service will refuse. The service re-checks every field
// with mergerule.Tightens as the second defence; the schema is the first.
func TestProposeSchemasAdmitOnlyTighteningValues(t *testing.T) {
	byName := map[string]Spec{}
	for _, sp := range Catalog() {
		byName[sp.Name] = sp
	}
	sp, ok := byName["mergerule_propose_tighten"]
	if !ok {
		t.Fatal("mergerule_propose_tighten missing")
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(sp.Schema, &s); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	fieldsRaw, ok := s.Properties["fields"]
	if !ok {
		t.Fatal("no fields property")
	}
	var fields struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(fieldsRaw, &fields); err != nil {
		t.Fatalf("fields does not parse: %v", err)
	}

	enumVals := func(name string) []string {
		raw, ok := fields.Properties[name]
		if !ok {
			t.Fatalf("fields has no %q", name)
		}
		var e struct {
			Enum []string `json:"enum"`
		}
		if err := json.Unmarshal(raw, &e); err != nil || len(e.Enum) == 0 {
			t.Fatalf("fields.%s is not an enum: %s", name, raw)
		}
		return e.Enum
	}

	// The one-way enums: exactly the tightening value, and the loosening
	// value must NOT be representable.
	cases := []struct {
		field     string
		want      []string
		loosening string
	}{
		{"requireDod", []string{"all_met"}, "none"},
		{"checksPolicy", []string{"reported_green"}, "required_only"},
		{"noChecks", []string{"refuse"}, "allow"},
		{"reviewMode", []string{"both"}, "forge"},
	}
	for _, c := range cases {
		got := enumVals(c.field)
		if len(got) != 1 || got[0] != c.want[0] {
			t.Errorf("fields.%s enum is %v — exactly [%s], the tightening value only", c.field, got, c.want[0])
		}
		for _, v := range got {
			if v == c.loosening {
				t.Errorf("fields.%s admits %q — the loosening value is not a representable input", c.field, c.loosening)
			}
		}
	}

	// requiredChecks is additive: an array of strings with no removal
	// affordance (the service refuses a dropped name).
	if raw, ok := fields.Properties["requiredChecks"]; !ok {
		t.Error("fields has no requiredChecks")
	} else {
		var a struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || a.Type != "array" {
			t.Errorf("requiredChecks is not an array: %s", raw)
		}
	}

	// verify_propose_add: the requirement enum is the full closed set
	// (a gate may be advisory or required — the direction is the agent's
	// choice, not a tightening axis), timeoutS is a number, and no
	// identity field is representable.
	sp2, ok := byName["verify_propose_add"]
	if !ok {
		t.Fatal("verify_propose_add missing")
	}
	var s2 struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(sp2.Schema, &s2); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	var req struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(s2.Properties["requirement"], &req); err != nil || len(req.Enum) != 2 {
		t.Errorf("verify_propose_add.requirement is not the two-value closed set: %v", req.Enum)
	}
	for _, v := range []string{"required", "advisory"} {
		found := false
		for _, e := range req.Enum {
			if e == v {
				found = true
			}
		}
		if !found {
			t.Errorf("verify_propose_add.requirement is missing %q", v)
		}
	}
	if _, ok := s2.Properties["componentId"]; ok {
		t.Error("verify_propose_add carries componentId — the component is stamped, never typed (OL3)")
	}
}
