package blueprint

import "testing"

// A model step may declare its own bound on model calls: nil inherits, 0 is
// "no bound", N is a bound; a negative number and a non-model step are refused.

func kindsOf(r Report) map[string]bool {
	out := map[string]bool{}
	for _, d := range r.Diagnostics {
		out[d.Kind] = true
	}
	return out
}

func TestAStepMayDeclareItsOwnBound(t *testing.T) {
	for _, value := range []string{"0", "25"} {
		raw := []byte("id: bounded\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: first\n    step: {kind: model-step, agent: default, max_iterations: " + value + "}\n")
		r := Analyze(raw, ValidationOptions{Agents: []string{"default"}})
		if k := kindsOf(r); k["range"] || k["step-shape"] || k["unknown-key"] {
			t.Errorf("max_iterations: %s was refused: %+v", value, r.Diagnostics)
		}
	}
}

func TestANegativeBoundAndANonModelStepAreRefused(t *testing.T) {
	neg := Analyze([]byte("id: b\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: first\n    step: {kind: model-step, agent: default, max_iterations: -1}\n"),
		ValidationOptions{Agents: []string{"default"}})
	if !kindsOf(neg)["range"] {
		t.Errorf("a negative bound must be refused: %+v", neg.Diagnostics)
	}
	gate := Analyze([]byte("id: b\nschemaVersion: 1\nversion: 1\nquestions:\n  q: {text: ok, options: [a]}\nlanes:\n  - id: first\n    step: {kind: gate, question: q, max_iterations: 5}\n"),
		ValidationOptions{Agents: []string{"default"}})
	if !kindsOf(gate)["step-shape"] {
		t.Errorf("a gate cannot carry a model bound: %+v", gate.Diagnostics)
	}
}
