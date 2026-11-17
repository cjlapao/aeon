package triage

// T2's gates, transitions half: the machine's edges, enumerated never
// hand-listed (machine.go's discipline), against the real store in a temp
// directory — house style, never the real one (§5).

import "testing"

// TestValidationStateNeverTransitionsBack is D1's whole argument: nothing
// moves back to `unvalidated`, ever. Exhaustive over the four states — the
// same discipline S24 and S26 use for their own transition tables.
func TestValidationStateNeverTransitionsBack(t *testing.T) {
	for _, from := range ValidationStates() {
		for _, to := range ValidationStates() {
			res := ValidateTransition(from, to)
			// A self-move is idempotent (a retried reactor write), allowed.
			if from == to {
				if !res.OK {
					t.Errorf("a self-transition %q → %q must be idempotent", from, from)
				}
				continue
			}
			// The only moves that are ever legal are out of unvalidated.
			want := from == ValidationUnvalidated
			if res.OK != want {
				t.Errorf("transition %q → %q is OK=%v, want %v (only unvalidated moves out; nothing moves back, D1)", from, to, res.OK, want)
			}
		}
	}
}

// TestUnvalidatedNeverCollapsesIntoCouldNotTell is D1's named collapse: a
// freshly created bug (unvalidated) and a could-not-tell bug are
// distinguishable by every reader, not just the raw column — a could-not-tell
// is an attempt that ran and was inconclusive, an unvalidated is none has
// run, and the two must never read the same.
func TestUnvalidatedNeverCollapsesIntoCouldNotTell(t *testing.T) {
	r, _ := openTriageStore(t, "/repo/collapse")
	fresh := mustCreate(t, r, "/repo/collapse", NewBug{Title: "never looked", Body: "b"})
	looked := mustCreate(t, r, "/repo/collapse", NewBug{Title: "inconclusive", Body: "b"})
	_, ref := r.Transition(looked.ID, ValidationCouldNotTell)
	if ref != nil {
		t.Fatalf("transition to could-not-tell: %s", ref.Reason)
	}
	// re-read both through the door; they must differ by every reader's eyes
	a, ok, err := r.Show(fresh.ID)
	if err != nil || !ok {
		t.Fatalf("re-read fresh: %v", err)
	}
	b, ok, err := r.Show(looked.ID)
	if err != nil || !ok {
		t.Fatalf("re-read looked: %v", err)
	}
	if a.Validation == b.Validation {
		t.Fatalf("unvalidated and could-not-tell read the same (%q) — the collapse D1 forbids", a.Validation)
	}
	// the two must not even render the same: Validated() separates them
	if a.Validated() == b.Validated() {
		t.Errorf("unvalidated and could-not-tell both Validated()=%v — they must not read the same", a.Validated())
	}
	if a.Validation != ValidationUnvalidated || b.Validation != ValidationCouldNotTell {
		t.Errorf("states are %q / %q, want unvalidated / could-not-tell", a.Validation, b.Validation)
	}
}

// TestOnceValidatedNothingResetsIsD1's re-attempt reading (D3): a bug that
// has landed a validated state cannot move back to unvalidated — a re-attempt
// is a NEW attempt record, not a reset of the bug's own state.
func TestOnceValidatedNothingResets(t *testing.T) {
	r, _ := openTriageStore(t, "/repo/terminal")
	b := mustCreate(t, r, "/repo/terminal", NewBug{Title: "reproduced", Body: "b"})
	if _, ref := r.Transition(b.ID, ValidationReproduced); ref != nil {
		t.Fatalf("transition to reproduced: %s", ref.Reason)
	}
	// now try to move it back to unvalidated — refused, with the reason
	if _, ref := r.Transition(b.ID, ValidationUnvalidated); ref == nil {
		t.Fatal("moving a reproduced bug back to unvalidated was allowed — D1 forbids it")
	}
	// and between the validated states — also refused
	if _, ref := r.Transition(b.ID, ValidationCouldNotTell); ref == nil {
		t.Fatal("moving between validated states was allowed — they are terminal (D3)")
	}
}
