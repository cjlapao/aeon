package triage

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// openTriageStore opens a throwaway store, seeds the component the bug's
// component_root must name, and returns the door over it. House style: never
// the real store (§5).
func openTriageStore(t *testing.T, root string) (*Triage, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed the component for %s: %v", root, err)
	}
	return New(s), s
}

func mustCreate(t *testing.T, r *Triage, root string, in NewBug) Bug {
	t.Helper()
	if in.FiledBy == "" {
		in.FiledBy = "person"
	}
	b, ref := r.Create(root, in)
	if ref != nil {
		t.Fatalf("create bug %q: %s", in.Title, ref.Reason)
	}
	return b
}

// TestCreateEntersAtUnvalidatedWithHonestDefaults is D7 made literal: a
// filed bug — human or self-filed — starts unvalidated and unknown priority,
// never pre-labeled by the filer.
func TestCreateEntersAtUnvalidatedWithHonestDefaults(t *testing.T) {
	r, _ := openTriageStore(t, "/repo/triage")
	b := mustCreate(t, r, "/repo/triage", NewBug{
		Title: "crash on empty input", Body: "the app crashes when given empty input",
	})
	if b.Validation != ValidationUnvalidated {
		t.Errorf("a created bug is %q, want unvalidated — no attempt has run yet (D1)", b.Validation)
	}
	if b.PriorityState != PriorityUnknown {
		t.Errorf("a created bug is %q priority, want unknown — the honest absence, never low (D2)", b.PriorityState)
	}
	if b.PriorityLabel != "" {
		t.Errorf("an unknown priority carries label %q, but D2 forbids a label on unknown", b.PriorityLabel)
	}
}

// TestSelfFiledBugIndistinguishableFromHumanFiled is D7's whole point: a
// self-filed claim and a human report have the SAME shape (unvalidated,
// unknown priority) until an attempt runs — a flood of self-filed claims
// reads as a backlog of never-looked-at claims, never as many urgent bugs.
func TestSelfFiledBugIndistinguishableFromHumanFiled(t *testing.T) {
	r, _ := openTriageStore(t, "/repo/triage")
	agent := mustCreate(t, r, "/repo/triage", NewBug{Title: "agent claim", Body: "x", FiledBy: "golang-engineer"})
	human := mustCreate(t, r, "/repo/triage", NewBug{Title: "human claim", Body: "x", FiledBy: "person"})
	if agent.Validation != human.Validation || agent.PriorityState != human.PriorityState {
		t.Errorf("self-filed and human-filed bugs must be the same shape until an attempt runs (D7): %q/%q vs %q/%q",
			agent.Validation, agent.PriorityState, human.Validation, human.PriorityState)
	}
}

// TestPriorityIndependentOfValidation is D2 as a property across every
// combination: setting a priority never changes the validation state, and
// setting a validation state never changes the priority — the two fields
// never gate each other, including on an unvalidated bug.
func TestPriorityIndependentOfValidation(t *testing.T) {
	for _, val := range ValidationStates() {
		for _, pr := range PriorityStates() {
			r, _ := openTriageStore(t, "/repo/combos")
			b := mustCreate(t, r, "/repo/combos", NewBug{Title: "combo", Body: "b"})
			// move to the target validation state (unvalidated is the start)
			if val != ValidationUnvalidated {
				if _, ref := r.Transition(b.ID, val); ref != nil {
					t.Fatalf("transition to %s: %s", val, ref.Reason)
				}
			}
			// set a priority, independently of validation state
			label := ""
			if pr != PriorityUnknown {
				label = "P1"
			}
			fresh, ref := r.SetPriority(b.ID, "/repo/combos", pr, label)
			if ref != nil {
				t.Fatalf("set priority %s on %s: %s", pr, val, ref.Reason)
			}
			if fresh.Validation != val {
				t.Errorf("setting priority %s changed the validation state from %s to %s — the two fields must never gate each other (D2)", pr, val, fresh.Validation)
			}
			if fresh.PriorityState != pr {
				t.Errorf("priority is %q, want %s", fresh.PriorityState, pr)
			}
			// and the label-on-unknown contradiction cannot exist
			if fresh.PriorityState == PriorityUnknown && fresh.PriorityLabel != "" {
				t.Errorf("a label %q on an unknown priority — D2's contradiction is unwritable", fresh.PriorityLabel)
			}
		}
	}
}
