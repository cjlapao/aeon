package records

// catalog_environment_test.go — S59 T7 (EV16, EV7): the execution-environment
// family's five specs, asserted where the catalog is DATA. Four safe reads
// (the tool twins of the person's standing environment.list/show/effective and
// secretref.list doors) and ONE ask (class detectable, Mutates:true) that
// files the INERT environment_restrict row a person commits. The thirteen
// environment writes and the four secretref writes are the person's doors and
// have no tool — refused by name in cmd/aeon (TestEnvironmentAgentSurfaceIsExactlyTheFive);
// what is refused HERE is the shape of the five that do exist:
//
//   - exactly five in the family, each twinned to its dot-name, each carrying
//     the class and mutability it was granted with, none of them default
//     granted (the registry side of that claim lives in internal/tools);
//   - `additionalProperties: false` on all five — the strict decode is the
//     identity wall, and an open schema is a door a caller walks through with
//     an `actor` in its pocket;
//   - no identity member, no state member and — the half this plane adds —
//     no VALUE and no SOURCE member anywhere: a secret reference's whole
//     contract is that its value and its location never cross to a model
//     (EV7, §4c's two types rather than one redaction);
//   - and the ask carries no `backend` and no `revision`: the backend an
//     environment runs on moves by a person through environment.update, and
//     the baseline a tightening is written against is the one the service
//     READS, not one a caller can type (EV16, EV17).

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogHasTheFiveEnvironmentSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
	}{
		"environment_list":             {"environment.list", false, "safe", nil},
		"environment_show":             {"environment.show", false, "safe", nil},
		"environment_effective":        {"environment.effective", false, "safe", nil},
		"secretref_list":               {"secretref.list", false, "safe", nil},
		"environment_propose_restrict": {"environment.proposeRestrict", true, "detectable", []string{"environmentId", "spec", "reason"}},
	}
	seen := map[string]bool{}
	mutators := 0
	for _, sp := range Catalog() {
		if sp.Family != "environment" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family environment but is not one of the five — the plane ships four reads and ONE ask (EV16)", sp.Name)
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
				t.Errorf("%s does not require %q — an ask decided without it is a row a person reads blind", sp.Name, r)
			}
		}
		if sp.Name == "environment_show" {
			// The door refuses both-empty ITSELF (id or slug, and both set
			// must agree), so the schema requires neither — a `required` here
			// would make the tool narrower than its twin and break the parity
			// the pair exists to keep (rule 4).
			for _, m := range []string{"id", "slug"} {
				if _, ok := sch.Properties[m]; !ok {
					t.Errorf("environment_show has no %q member — the door answers id or slug and the tool must reach both", m)
				}
			}
			if len(sch.Required) != 0 {
				t.Errorf("environment_show requires %v — the both-empty refusal is the DOOR's sentence, not a schema's", sch.Required)
			}
		}
		if sp.Mutates {
			mutators++
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog — the agent surface the spec names is not there", name)
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating environment specs, want exactly 1 (the ask) — a second write spec is EV16 broken", mutators)
	}
}

// TestEnvironmentSchemasCarryNoIdentityStateValueOrSource is rule 1 plus this
// plane's own half of §4c, run over ALL FIVE specs rather than only the ask:
// identity and state are stamps and rulings, never inputs (OL3); and a
// credential's value and its location never cross to a model at all (EV7).
// `source` is banned here even though it is a real column on the reference
// row — the person's door answers it, the agent's type has no field for it,
// and an input field that accepted one would be a path a caller could write
// as well as read.
func TestEnvironmentSchemasCarryNoIdentityStateValueOrSource(t *testing.T) {
	// Banned on every one of the five. `origin` and `backend` are NOT here:
	// environment.list FILTERS on both, and the door's own read-key list says
	// so (the one key a read accepts that a write refuses is `origin`). They
	// are banned on the ASK below, where they would be a change of authorship
	// and a change of backend smuggled into a tightening.
	banned := []string{
		"actor", "createdby", "createdbykind", "createdbyid", "proposedby", "author",
		"status", "state", "kind", "decision", "decidedby",
		"rev", "revision", "baserevision", "expectedrevision", "trigger", "appliedat",
		"value", "secret", "secretvalue", "token", "apikey", "key", "password",
		"credential", "contents", "data", "raw", "bytes", "envvalue", "valuelength",
		"source", "sourcekind", "sourcevalue", "path", "file", "filepath",
		"legacy", "provenance",
	}
	// Banned on the ask alone: the fields that make an environment what it is.
	bannedOnTheAsk := []string{"backend", "origin", "expectedrevision", "actor"}
	names := []string{"environment_list", "environment_show", "environment_effective", "secretref_list", "environment_propose_restrict"}
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
		bannedHere := banned
		if name == "environment_propose_restrict" {
			bannedHere = append(append([]string{}, banned...), bannedOnTheAsk...)
		}
		for prop := range sch.Properties {
			p := strings.ToLower(strings.ReplaceAll(prop, "_", ""))
			for _, b := range bannedHere {
				if p == b {
					t.Errorf("%s carries %q — identity, state, the baseline, the backend and every shape of a value are stamps and rulings, never inputs (EV7, EV16, EV17)", name, prop)
				}
			}
		}
		if name == "environment_propose_restrict" && len(sch.Properties) != 3 {
			t.Errorf("the ask carries %d members, want exactly 3 (environmentId, spec, reason) — a fourth is a field the spec never granted, and the baseline the tightening is written against is the one the SERVICE reads", len(sch.Properties))
		}
		// And the schema TEXT must not promise a value: the description of a
		// member is what the model reads when it decides what to put there.
		for prop, raw := range sch.Properties {
			low := strings.ToLower(string(raw))
			for _, phrase := range []string{"the value of", "paste the", "token here", "api key", "secret value"} {
				if strings.Contains(low, phrase) {
					t.Errorf("%s.%s invites a credential into the input (%q) — a credential goes IN through a person's door and never comes out of this one (EV7, §4c)", name, prop, phrase)
				}
			}
		}
	}
}

// TestNoEnvironmentWriteToolExists is EV16 by name, from the catalog side: the
// twenty human doors' write halves appear NOWHERE in the catalog, not even
// under a tool name that does not rhyme with them. The cmd/aeon guard makes
// the same claim about the registry; this one makes it about the data the
// registry is projected from, so a spec arriving without a handler still
// fails.
func TestNoEnvironmentWriteToolExists(t *testing.T) {
	bannedMethods := map[string]string{
		"environment.create":   "EV2's row, by a person",
		"environment.update":   "EV18's spec write with its optimistic-concurrency guard",
		"environment.archive":  "history is a person's judgement",
		"environment.restore":  "the same, in reverse",
		"environment.bind":     "EV2's binding — what a loop runs in",
		"environment.unbind":   "and taking it away",
		"environment.teardown": "EV15's ledger walk over real provisions",
		"secretref.create":     "EV7's pointer to where a credential lives",
		"secretref.update":     "retargeting that pointer",
		"secretref.archive":    "removing it, while environments still name it (EV8)",
		"secretref.restore":    "and putting it back",
	}
	bannedTools := map[string]string{
		"environment_create": "no tool authors an environment", "environment_update": "no tool rewrites a spec",
		"environment_archive": "no tool retires a row", "environment_restore": "no tool restores one",
		"environment_bind": "no tool decides what a loop runs in", "environment_unbind": "nor what stops running in it",
		"environment_teardown":      "no tool walks the provisioning ledger",
		"environment_propose_widen": "there is no widening ask to file (EV16)",
		"environment_propose":       "the ask is named for what it does — restrict",
		"secretref_create":          "no tool points at a credential", "secretref_update": "nor retargets one",
		"secretref_archive": "nor retires one", "secretref_restore": "nor restores one",
		"secretref_propose": "there is no reference proposal kind in this slice",
	}
	for _, sp := range Catalog() {
		if why, bad := bannedMethods[sp.Method]; bad {
			t.Errorf("catalog action %s twinned to %s — %s, and an agent door onto it is EV16's exact refusal", sp.Name, sp.Method, why)
		}
		if why, bad := bannedTools[sp.Name]; bad {
			t.Errorf("the catalog carries %s — %s", sp.Name, why)
		}
	}
}

// TestEnvironmentAgentSpecsAreNotDefaultGranted is the catalog's reminder of
// the registry's rule: registered is not granted (S11). These five are in the
// catalog and in the registry and in NOBODY's default set; a step reaches them
// only through a `tool_access` row an operator added. internal/tools owns the
// registry-side assertion (TestRegistryExactNames, DefaultGrant's pinned
// three); this asserts the catalog did not grow a grant of its own.
func TestEnvironmentAgentSpecsAreNotDefaultGranted(t *testing.T) {
	// The catalog is data and holds no grant; the claim that survives as a
	// test here is that no spec carries a field that WOULD carry one — a
	// `granted`/`defaultGrant` member on a spec is a catalog that started
	// deciding who may call it.
	for _, sp := range Catalog() {
		if sp.Family != "environment" {
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
