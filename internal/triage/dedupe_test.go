package triage

// T4's gates, dedupe half: the suggest-only discipline, the project scope,
// and the one write path. Against a real store in a temp directory — house
// style, never the real one (§5).

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

const testDim = 4

func plant(t *testing.T, s *store.Store, b Bug, vec []float32) {
	t.Helper()
	if _, err := s.EnsureBugVecTable(b.ComponentRoot, testDim); err != nil {
		t.Fatalf("ensure bug vec: %v", err)
	}
	if err := s.UpsertBugVec(b.ComponentRoot, testDim, b.ID, vec); err != nil {
		t.Fatalf("upsert bug vec: %v", err)
	}
}

// TestDuplicateOfNeverSetWithoutHumanOriginatedCall is D5/N3: the one write
// path (ConfirmDuplicate) refuses any confirmer that is not a person. An
// agent, an empty value, or a model name cannot set duplicate_of — only a
// person can, and a test plants the machine-side attempt and the human-side
// success to prove the split.
func TestDuplicateOfNeverSetWithoutHumanOriginatedCall(t *testing.T) {
	r, _ := openTriageStore(t, "/repo/dedup")
	primary := mustCreate(t, r, "/repo/dedup", NewBug{Title: "primary", Body: "b"})
	dup := mustCreate(t, r, "/repo/dedup", NewBug{Title: "duplicate", Body: "b"})

	// a machine confirmer is refused
	if _, ref := r.ConfirmDuplicate(dup.ID, "/repo/dedup", primary.ID, "agent:golang-engineer"); ref == nil {
		t.Error("an agent confirmer was allowed to set duplicate_of — D5/N3 forbids it")
	}
	if _, ref := r.ConfirmDuplicate(dup.ID, "/repo/dedup", primary.ID, ""); ref == nil {
		t.Error("an empty confirmer was allowed to set duplicate_of — D5/N3 forbids it")
	}
	// a person confirmer is the only write path
	_, ref := r.ConfirmDuplicate(dup.ID, "/repo/dedup", primary.ID, "person")
	if ref != nil {
		t.Fatalf("a person confirmer was refused: %s", ref.Reason)
	}
	fresh, ok, err := r.Show(dup.ID)
	if err != nil || !ok {
		t.Fatalf("re-read dup: %v", err)
	}
	if fresh.DuplicateOf == nil || *fresh.DuplicateOf != primary.ID {
		t.Errorf("duplicate_of is %v, want %d — the person confirmation is the one write path", fresh.DuplicateOf, primary.ID)
	}
}

// TestDuplicateSuggestionIsProjectScopedOnly is D6/N2: a candidate from a
// different project never appears, even with an identical embedding. The
// bug_vec table is per-project (its name carries a hash of the root), so a
// query of one project can never read another's — the scope is the table,
// not a WHERE clause.
func TestDuplicateSuggestionIsProjectScopedOnly(t *testing.T) {
	rA, sA := openTriageStore(t, "/repo/A")
	rB, sB := openTriageStore(t, "/repo/B")

	// an identical embedding in a DIFFERENT project
	bA := mustCreate(t, rA, "/repo/A", NewBug{Title: "A bug", Body: "b"})
	bB := mustCreate(t, rB, "/repo/B", NewBug{Title: "B bug", Body: "b"})
	vec := []float32{1, 0, 0, 0}
	plant(t, sA, bA, vec)
	plant(t, sB, bB, vec)

	// a query in project A must not return project B's bug
	cands, err := rA.SuggestCandidates(bA, testDim, vec, 10)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	for _, c := range cands {
		if c.ID == bB.ID {
			t.Errorf("a candidate from a different project (%d) appeared in %s's results — D6/N2 forbids cross-project", bB.ID, bA.ComponentRoot)
		}
	}
}

// TestNoAutoCloseUnderAnyThreshold is R3's named risk, made unreachable: a
// near-perfect (identical) similarity match still requires an explicit
// ConfirmDuplicate — nothing closes or transitions a bug on SuggestCandidates
// alone. The suggestion is a READ; the bug's own state is untouched by it.
func TestNoAutoCloseUnderAnyThreshold(t *testing.T) {
	r, s := openTriageStore(t, "/repo/noauto")
	a := mustCreate(t, r, "/repo/noauto", NewBug{Title: "alpha", Body: "b"})
	b := mustCreate(t, r, "/repo/noauto", NewBug{Title: "beta", Body: "b"})
	// identical embeddings — a perfect match
	vec := []float32{0, 1, 0, 0}
	plant(t, s, a, vec)
	plant(t, s, b, vec)

	cands, err := r.SuggestCandidates(b, testDim, vec, 10)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(cands) == 0 {
		t.Fatalf("a perfect match returned no candidates — the mechanism must surface the match")
	}
	// the suggestion must not have touched either bug's state
	fresh, ok, err := r.Show(b.ID)
	if err != nil || !ok {
		t.Fatalf("re-read b: %v", err)
	}
	if fresh.Validation != ValidationUnvalidated {
		t.Errorf("a suggestion changed b's validation state to %q — the suggestion is a read (D5)", fresh.Validation)
	}
	if fresh.DuplicateOf != nil {
		t.Errorf("a suggestion set duplicate_of to %v — only ConfirmDuplicate may (D5/N3)", *fresh.DuplicateOf)
	}
}
