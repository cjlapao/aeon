package blueprint

import (
	"github.com/cjlapao/aeon/internal/handler"

	"reflect"
	"strings"
	"testing"
)

func TestAllDiagnosticsReported(t *testing.T) {
	raw := []byte(`id: broken
schemaVersion: 1
version: 1
lanes:
  - id: first
    typo: true
    when: {field: mode, op: gt, value: 3}
    step: {kind: model-step, agent: missing-agent, sets: imaginary}
    on_reject: {goto: absent, counter: retries, max: 1}
  - id: second
    step: {kind: gate, question: absent}
`)
	r := Analyze(raw, ValidationOptions{Agents: []string{"default"}})
	kinds := map[string]bool{}
	for _, d := range r.Diagnostics {
		kinds[d.Kind] = true
	}
	for _, kind := range []string{"unknown-key", "invalid-operator", "unknown-agent", "machine-status", "missing-then", "unknown-goto", "unknown-question"} {
		if !kinds[kind] {
			t.Fatalf("missing %s in %+v", kind, r.Diagnostics)
		}
	}
	if r.OK(false) {
		t.Fatal("invalid report passed")
	}
}

func TestDiagnosticsAreStablyOrdered(t *testing.T) {
	raw := []byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agent: missing}\n    typo: 1\n")
	a := Analyze(raw, ValidationOptions{})
	b := Analyze(raw, ValidationOptions{})
	if !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
	for i := 1; i < len(a.Diagnostics); i++ {
		if a.Diagnostics[i-1].Path > a.Diagnostics[i].Path {
			t.Fatal(a.Diagnostics)
		}
	}
}

func TestCodeActionIsUnverifiableAndStrictFails(t *testing.T) {
	b, err := Load([]byte("id: action\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: push\n    step: {kind: code-action, action: forge.openPR}\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := Validate(b, ValidationOptions{})
	if !r.OK(false) || r.OK(true) || len(r.Diagnostics) != 1 || r.Diagnostics[0].Slice != "S16" {
		t.Fatal(r)
	}
}

// TestByTechTagIsNowARealCheck — D13's flip, asserted at the gate the spec
// names. Before S13 T10, `agent: by-tech-tag` was `unverifiable, arrives at
// S13` and the agent reserve list swallowed the eleven S13 bodies. Now the
// registry is real (handler.Names, handler.Resolvers — the production call
// sites pass exactly these), so: registered agents are VALID, the named
// resolver is a REAL check (valid), a name with no body in the corpus at all
// stays unverifiable, and an unknown RESOLVER name is invalid — the reserve
// list is now the complete set, and "looks like a resolver" is not a
// third state.
func TestByTechTagIsNowARealCheck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state DiagState
		count int
	}{
		{"missing", Invalid, 1},
		{"project-planner", Valid, 0},
		{"by-tech-tag", Valid, 0},
		// Sample retired at T16 (measured 2026-10-26): project-manager shipped
		// and the row went Valid/0 — a Valid row here only re-tests the
		// registry, which the project-planner row already does. The
		// UNVERIFIABLE sample moved to
		// TestTheThirdStateSurvivesAnEmptyFollowUpList, where the declaration
		// is mutated exactly as the next follow-up's commit will declare it;
		// an empty futureAgents must not silently delete the state.
		{"project-manager", Valid, 0},
		{"by-cost-budget", Invalid, 1},
	} {
		b, err := Load([]byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agent: " + tc.name + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		r := Validate(b, ValidationOptions{Agents: handler.Names(), Resolvers: handler.Resolvers()})
		if len(r.Diagnostics) != tc.count || (tc.count == 1 && r.Diagnostics[0].State != tc.state) {
			t.Fatal(tc, r.Diagnostics)
		}
		if tc.count == 0 && !r.OK(false) {
			t.Errorf("%s: the real check must accept it — %v", tc.name, r.Diagnostics)
		}
		if tc.name == "by-cost-budget" && r.Diagnostics[0].Kind != "unknown-agent" {
			t.Errorf("an unknown resolver name must be INVALID, not a new unverifiable state — the reserve list is complete: %+v", r.Diagnostics[0])
		}
	}
}

func TestBackwardGotoNeedsCounterAndCap(t *testing.T) {
	b := Blueprint{ID: "x", SchemaVersion: 1, Lanes: []Lane{{ID: "a", Step: &Step{Kind: Gate, Question: "q"}, OnReject: &OnReject{Goto: "a", Then: Block}}}, Questions: map[string]Question{"q": {Prompt: "go?", Answers: []string{"yes"}}}}
	r := Validate(b, ValidationOptions{})
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Kind != "unbounded-back-edge" {
		t.Fatal(r.Diagnostics)
	}
}

func TestConditionTypeMismatchIsInvalidAtValidation(t *testing.T) {
	b := Blueprint{ID: "x", SchemaVersion: 1, Lanes: []Lane{{ID: "a", When: &Condition{Field: "mode", Op: Gt, Value: 3}, Step: &Step{Kind: ModelStep, Agent: "default"}}}}
	r := Validate(b, ValidationOptions{Agents: []string{"default"}})
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Kind != "invalid-operator" || !strings.Contains(r.Diagnostics[0].Path, "when") {
		t.Fatal(r.Diagnostics)
	}
}

// Validation pass 2026-10-26 (V-2): the at-rest deferral claimed "code action
// registry arrives at S16" — S16 shipped 2026-10-26. The registry EXISTS; the
// only truth left is that this validator was not given the closed list.
// A deferral must never misreport the present tense.
func TestAtRestActionRegistryDeferralTellsTheTruthAboutThePresent(t *testing.T) {
	b := Blueprint{ID: "x", SchemaVersion: 1, Lanes: []Lane{{ID: "a", Step: &Step{Kind: CodeAction, Action: "git.commitAndPush"}}}}
	r := Validate(b, ValidationOptions{Agents: []string{"default"}}) // Actions nil = at rest
	var d *Diagnostic
	for i := range r.Diagnostics {
		if r.Diagnostics[i].Kind == "action-registry" {
			d = &r.Diagnostics[i]
		}
	}
	if d == nil || d.State != Unverifiable {
		t.Fatalf("an at-rest validator must defer with an unverifiable row, not guess: %+v", r.Diagnostics)
	}
	if strings.Contains(d.Msg, "arrives at S16") {
		t.Errorf("the deferral still advertises a closed slice as the future: %q", d.Msg)
	}
	if !strings.Contains(d.Msg, "not given") {
		t.Errorf("the deferral must name the real reason — this validator was not given the registry: %q", d.Msg)
	}
}
