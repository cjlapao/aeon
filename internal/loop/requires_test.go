package loop

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T13 — the requires evaluator is pure: no socket, no daemon, no clock.

func reqBP(reqs ...blueprint.Requirement) *blueprint.Blueprint {
	return &blueprint.Blueprint{ID: "org-needs", SchemaVersion: 1, Version: 1, Requires: reqs}
}

func anyOf(terms ...blueprint.RequirementTerm) blueprint.Requirement {
	return blueprint.Requirement{AnyOf: terms}
}

func TestCheckRequiresAnyOf(t *testing.T) {
	bp := reqBP(anyOf(
		blueprint.RequirementTerm{Type: "goal", State: "approved"},
		blueprint.RequirementTerm{Type: "task", State: "approved"},
	))
	// One clause, two legs: either record satisfies.
	if r := CheckRequires(bp, &store.Goal{ReviewState: "approved"}, nil, nil); !r.OK {
		t.Fatalf("an approved goal did not satisfy the clause: %q", r.Sentence)
	}
	if r := CheckRequires(bp, nil, &store.Task{Manual: true, ApprovalState: "approved"}, nil); !r.OK {
		t.Fatalf("an approved manual task did not satisfy the clause: %q", r.Sentence)
	}
	// Neither given: the sentence names BOTH alternatives (the person must
	// be able to read what would have satisfied it).
	r := CheckRequires(bp, nil, nil, func() []Candidate {
		return []Candidate{{Kind: "goal", ID: "goal-1", Title: "ship", State: "approved"}}
	})
	if r.OK {
		t.Fatal("nothing was given and the clause passed")
	}
	if !strings.Contains(r.Sentence, "goal") || !strings.Contains(r.Sentence, "task") {
		t.Fatalf("the sentence must name both alternatives: %q", r.Sentence)
	}
	if !strings.Contains(r.Sentence, "org-needs") || !strings.Contains(r.Sentence, "none was given") {
		t.Fatalf("the sentence must name the blueprint and the verdict: %q", r.Sentence)
	}
	if len(r.Candidates) != 1 || r.Candidates[0].ID != "goal-1" {
		t.Fatalf("the refusal must carry the offer: %+v", r.Candidates)
	}
	// AND of ORs: a second clause nobody satisfied keeps the refusal closed
	// even though the first clause holds.
	two := reqBP(
		anyOf(blueprint.RequirementTerm{Type: "goal", State: "approved"}),
		anyOf(blueprint.RequirementTerm{Type: "task", State: "approved"}),
	)
	if r := CheckRequires(two, &store.Goal{ReviewState: "approved"}, nil, nil); r.OK {
		t.Fatal("the second clause was skipped")
	}
	if r := CheckRequires(two, &store.Goal{ReviewState: "approved"}, &store.Task{Manual: true, ApprovalState: "approved"}, nil); !r.OK {
		t.Fatal("both clauses were satisfied and the refusal stood")
	}
}

func TestCheckRequiresRejectsUnapprovedGoal(t *testing.T) {
	bp := reqBP(anyOf(blueprint.RequirementTerm{Type: "goal", State: "approved"}))
	if r := CheckRequires(bp, &store.Goal{ReviewState: "in_review"}, nil, nil); r.OK {
		t.Fatal("an in_review goal read as the person's approval")
	}
	// The task twin: a LOOP task is not a prerequisite — the person's
	// approval of a manual task is (OL9).
	if r := CheckRequires(reqBP(anyOf(blueprint.RequirementTerm{Type: "task", State: "approved"})),
		nil, &store.Task{Manual: false, ApprovalState: "approved"}, nil); r.OK {
		t.Fatal("a loop task satisfied a requires clause")
	}
	// No requires at all: OK without reading anything (offer must not run).
	ran := false
	if r := CheckRequires(reqBP(), nil, nil, func() []Candidate { ran = true; return nil }); !r.OK || ran {
		t.Fatal("an empty requires consulted the store")
	}
}

func TestCheckRequiresBoundsTheOffer(t *testing.T) {
	bp := reqBP(anyOf(blueprint.RequirementTerm{Type: "goal", State: "approved"}))
	r := CheckRequires(bp, nil, nil, func() []Candidate {
		var out []Candidate
		for i := 0; i < 45; i++ {
			out = append(out, Candidate{Kind: "goal", ID: fmt.Sprintf("goal-%d", i), Title: "x", State: "approved"})
		}
		return out
	})
	if len(r.Candidates) != 20 {
		t.Fatalf("the refusal dumped %d candidates; the bound is 20", len(r.Candidates))
	}
}
