package policy

// The vocabulary's contract (S45 T2, PM6): the closed sets come from
// types.go, the authorship matrix is COMPUTED from the rule, and the spec
// descriptors come from the registered validators. A test that hardcoded
// any of the three would itself be a restatement — so every expectation
// below is taken from the engine's own exported doors (AllPolicyTypes and
// friends, ValidatorFor), and the matrix's deepest property is proven at
// the seam: swap the rule and watch the matrix follow. A restated matrix
// could not follow; a computed one must.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestVocabularyMatchesClosedSets(t *testing.T) {
	v := Vocabulary()

	wantTypes := AllPolicyTypes()
	if len(v.Types) != len(wantTypes) {
		t.Fatalf("types: %d entries, engine has %d", len(v.Types), len(wantTypes))
	}
	for i, want := range wantTypes {
		if v.Types[i] != string(want) {
			t.Errorf("types[%d] = %q, engine says %q (order is the closed set's own)", i, v.Types[i], want)
		}
	}
	// Same equality, in the engine's own order, for the other three sets.
	checkStrs(t, "targetKinds", v.TargetKinds, AllTargetKinds())
	checkStrs(t, "sources", v.Sources, AllSources())
	checkStrs(t, "actions", v.Actions, AllActions())

	// A type that arrives in types.go without a descriptor FAILS here (the
	// set comparison, not a count — a count fails the day the set grows,
	// which is the day the vocabulary must absorb it silently).
	if len(v.Specs) != len(wantTypes) {
		t.Fatalf("specs: %d descriptors, engine compiles %d types", len(v.Specs), len(wantTypes))
	}
	for i, want := range wantTypes {
		if v.Specs[i].Type != string(want) {
			t.Errorf("specs[%d].Type = %q, want %q (AllPolicyTypes order)", i, v.Specs[i].Type, want)
		}
	}
}

func checkStrs[V ~string](t *testing.T, name string, got []string, want []V) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d entries, engine has %d", name, len(got), len(want))
	}
	for i := range want {
		if got[i] != string(want[i]) {
			t.Errorf("%s[%d] = %q, engine says %q", name, i, got[i], want[i])
		}
	}
}

func TestAuthorshipMatrixIsComputedFromCheckAuthorship(t *testing.T) {
	// The seam pin: the matrix reaches the rule through a value, and that
	// value IS the rule (one home, PL1; the store_test.go count guard pins
	// there are no other call sites).
	if reflect.ValueOf(authorshipRule).Pointer() != reflect.ValueOf(checkAuthorship).Pointer() {
		t.Fatal("authorshipRule is no longer checkAuthorship — the matrix is not computed from the rule")
	}

	v := Vocabulary()
	if len(v.Authorship) != len(AllSources())*len(AllActions()) {
		t.Fatalf("matrix has %d cells, rule admits %d sources × %d actions",
			len(v.Authorship), len(AllSources()), len(AllActions()))
	}
	i := 0
	for _, s := range AllSources() {
		for _, a := range AllActions() {
			e := v.Authorship[i]
			i++
			if e.Source != string(s) || e.Action != string(a) {
				t.Fatalf("cell %d is (%s,%s), expected (%s,%s)", i-1, e.Source, e.Action, s, a)
			}
			rej := checkAuthorship(Policy{Action: a, Source: s})
			if rej == nil {
				if !e.Allowed || e.Reason != "" {
					t.Errorf("cell (%s,%s): rule says yes, matrix said allowed=%v reason=%q", s, a, e.Allowed, e.Reason)
				}
				continue
			}
			if e.Allowed || e.Reason != rej.Reason {
				t.Errorf("cell (%s,%s): rule said %q, matrix said allowed=%v reason=%q", s, a, rej.Reason, e.Allowed, e.Reason)
			}
		}
	}
	// One named spot-check of the shape the UI depends on: a non-operator
	// may never author an Allow (PL1), and the refusal sentence is the
	// boundary's own words, not a paraphrase.
	if !v.Authorship[0].Allowed || v.Authorship[0].Source != "operator" || v.Authorship[0].Action != "allow" {
		t.Error("operator/allow must be allowed — PL1 says an operator can")
	}
	for _, e := range v.Authorship {
		if e.Source == "operator" {
			continue
		}
		if e.Action == "allow" {
			if e.Allowed || e.Reason == "" {
				t.Errorf("%s/allow: PL1 refuses this, matrix said allowed=%v reason=%q", e.Source, e.Allowed, e.Reason)
			}
		} else if !e.Allowed {
			t.Errorf("%s/%s: PL1 admits this (a Deny or Ask from any source), matrix refused: %q", e.Source, e.Action, e.Reason)
		}
	}

	// THE computed-not-restated proof: swap the seam for a rule with the
	// OPPOSITE opinion and the matrix must follow it, reasons and all. A
	// restated matrix — even one that matched today cell for cell — could
	// not survive this, which is exactly why the test performs it.
	prev := authorshipRule
	defer func() { authorshipRule = prev }()
	authorshipRule = func(p Policy) *Reject {
		if p.Action == Deny {
			return &Reject{Reason: "seam: this build refuses every Deny"}
		}
		return nil
	}
	flipped := Vocabulary()
	for _, e := range flipped.Authorship {
		if e.Action == "deny" {
			if e.Allowed || e.Reason != "seam: this build refuses every Deny" {
				t.Errorf("the matrix did not follow the swapped rule at %s/deny: allowed=%v reason=%q", e.Source, e.Allowed, e.Reason)
			}
		} else if !e.Allowed {
			t.Errorf("the swapped rule says yes to %s/%s and the matrix still refused: %q", e.Source, e.Action, e.Reason)
		}
	}
	// And with the rule restored, the answer is byte-identical to the first
	// one (no cached opinion: every call recomputes from the live rule).
	authorshipRule = prev
	if !reflect.DeepEqual(Vocabulary(), v) {
		t.Error("Vocabulary() drifted after restoring the rule — a computed answer cannot drift")
	}
}

func TestSpecDescriptorsCoverEveryType(t *testing.T) {
	// One hand-written, schema-independent probe spec per type. The test
	// asks the VALIDATOR whether each candidate state is acceptable and
	// compares to the descriptor's enum — the UI form can only offer what
	// the write boundary accepts, and this is the pin that says it does.
	v := Vocabulary()
	probes := map[PolicyType]func(act Action) json.RawMessage{
		ToolAccess:    func(a Action) json.RawMessage { return spec(`{"toolName":"bash","state":%q}`, a) },
		CommandAccess: func(a Action) json.RawMessage { return spec(`{"command":"git*","state":%q}`, a) },
		RouteAccess:   func(a Action) json.RawMessage { return spec(`{"route":"remote:x*","state":%q}`, a) },
		EffectClass:   func(a Action) json.RawMessage { return spec(`{"effect":"read","state":%q}`, a) },
		CostCap:       func(Action) json.RawMessage { return json.RawMessage(`{"token":"gpt-tokens","max":1000}`) },
		SkillAccess:   func(a Action) json.RawMessage { return spec(`{"skill":"acme/deploy","state":%q}`, a) },
		TriggerAccess: func(a Action) json.RawMessage { return spec(`{"trigger":"webhook","state":%q}`, a) },
	}
	// Expected field names + requiredness + kind, per compiled type —
	// taken from the design's spec tables (a schema drift fails HERE, not
	// in the UI's form builder).
	type wantField struct {
		name     string
		kind     string
		required bool
	}
	wants := map[PolicyType][]wantField{
		ToolAccess:    {{name: "toolName", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
		CommandAccess: {{name: "command", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
		RouteAccess:   {{name: "route", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
		EffectClass:   {{name: "effect", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
		CostCap:       {{name: "token", kind: "string", required: true}, {name: "max", kind: "number", required: true}},
		SkillAccess:   {{name: "skill", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
		TriggerAccess: {{name: "trigger", kind: "string", required: true}, {name: "state", kind: "enum", required: true}},
	}

	for _, typ := range AllPolicyTypes() {
		desc := descFor(t, v.Specs, typ)
		want := wants[typ]
		if len(desc.Fields) != len(want) {
			t.Fatalf("%s: %d fields, expected %d (%v)", typ, len(desc.Fields), len(want), desc.Fields)
		}
		for i, w := range want {
			f := desc.Fields[i]
			if f.Name != w.name || f.Kind != w.kind || f.Required != w.required {
				t.Errorf("%s field %d = {%s %s required=%v}, want {%s %s required=%v}",
					typ, i, f.Name, f.Kind, f.Required, w.name, w.kind, w.required)
			}
			if f.Description == "" {
				t.Errorf("%s.%s has no form-help text — a blank beside a form field teaches nothing", typ, f.Name)
			}
			if f.Kind == "number" && len(f.EnumValues) != 0 {
				t.Errorf("%s.%s: a number field carries enum values", typ, f.Name)
			}
			if w.kind == "enum" {
				// The enum equals EXACTLY what the validator accepts — probed
				// through the door a write goes through, not this file's belief.
				val, ok := ValidatorFor(typ)
				if !ok {
					t.Fatalf("%s has no registered validator", typ)
				}
				var accepted []string
				for _, a := range AllActions() {
					p := Policy{ID: "probe", Type: typ, Target: Target{Kind: AllAgents}, Action: a, Source: Operator, Spec: probes[typ](a)}
					if val.Validate(p) == nil {
						accepted = append(accepted, string(a))
					}
				}
				if !reflect.DeepEqual(f.EnumValues, accepted) {
					t.Errorf("%s.%s enum = %v, but the validator accepts %v — the form would offer a value the write refuses (or hide one it accepts)",
						typ, f.Name, f.EnumValues, accepted)
				}
			}
		}
	}
}

func spec(format string, a Action) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(format, string(a)))
}

func descFor(t *testing.T, specs []SpecDescriptor, typ PolicyType) SpecDescriptor {
	t.Helper()
	for _, d := range specs {
		if d.Type == string(typ) {
			return d
		}
	}
	t.Fatalf("no descriptor for %s — a compiled type the UI cannot build a form for", typ)
	return SpecDescriptor{}
}
