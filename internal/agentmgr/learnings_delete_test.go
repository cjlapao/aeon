package agentmgr

// S52 LM7 — an agent that has learned things still owns memory: delete is
// refused while any learning is scoped to its slug, active or retracted,
// and the refusal names the count. An agent with no memory deletes exactly
// as before — the guard refuses the inheritance, it does not gate deletes.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/store"
)

func TestDeleteRefusesWhileMemoryIsScopedToTheAgent(t *testing.T) {
	s := openAgentStore(t)
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateVerified), nil, boolOverride(false))
	ctx := context.Background()
	for _, slug := range []string{"memory-holder", "clean-agent"} {
		if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: slug, Name: slug}, "2026-11-04T00:00:00Z"); err != nil {
			t.Fatalf("seed agent %s: %v", slug, err)
		}
	}
	l := learnings.New(s)
	actor := learnings.Actor{Kind: "user", ID: "1000"}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "memory-holder", Body: "b1", Provenance: learnings.ProvenanceObserved}, actor); ref != nil {
		t.Fatalf("seed learning: %s", ref.Reason)
	}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "memory-holder", Body: "b2", Provenance: learnings.ProvenanceObserved}, actor); ref != nil {
		t.Fatalf("seed learning: %s", ref.Reason)
	}
	// (The retracted-rows-still-count half of LM7 is proven at the store,
	// TestLearningsScopedToCounts — the stamp's own door is T2's.)

	err := mgr.Delete(ctx, testPerson, "memory-holder")
	if err == nil {
		t.Fatal("the delete of a memory-holding agent went through — its learnings would name nobody (LM7)")
	}
	if !strings.Contains(err.Error(), "2 learning(s)") || !strings.Contains(err.Error(), "memory-holder") {
		t.Fatalf("refusal %q does not name the agent and its count", err)
	}
	if _, ok, _ := s.AgentBySlug("memory-holder"); !ok {
		t.Fatal("the refused delete removed the row — a refusal is a refusal")
	}
	if err := mgr.Delete(ctx, testPerson, "clean-agent"); err != nil {
		t.Fatalf("the delete of an agent with no memory refused: %v", err)
	}
}
