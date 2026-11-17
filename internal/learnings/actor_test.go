package learnings

// S52 T1 — the actor-carrying write: AppendAs is the one production path
// (LM3 audit line, LM4 provenance law), the pinned legacy tests keep their
// no-actor wrappers untouched, and the §3 check now answers to the SLUG.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func TestAppendAsWritesTheAuditLine(t *testing.T) {
	l, s := openLearnings(t)
	got, ref := l.AppendAs(AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "b", Provenance: ProvenanceObserved},
		Actor{Kind: "user", ID: "12345"})
	if ref != nil {
		t.Fatalf("AppendAs refused: %s", ref.Reason)
	}
	lines, err := s.ListLearningChanges(got.ID)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("%d audit lines after one AppendAs, want exactly 1 — the write and its memory are one act", len(lines))
	}
	c := lines[0]
	if c.Op != "add" || c.ActorKind != "user" || c.ActorID != "12345" {
		t.Fatalf("audit line = %+v, want the add by user/12345", c)
	}
	if !strings.Contains(c.Summary, "observed") || !strings.Contains(c.Summary, "project//repo") {
		t.Fatalf("audit summary %q does not say what happened — the template names provenance and scope/key", c.Summary)
	}
}

func TestToldNeedsAPersonKind(t *testing.T) {
	l, _ := openLearnings(t)
	in := AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "b", Provenance: ProvenanceTold}
	for _, kind := range []string{"agent", "system"} {
		if _, ref := l.AppendAs(in, Actor{Kind: kind, ID: "x"}); ref == nil {
			t.Fatalf("a %s actor filed a told learning — told says \"a person said it\", and %s is not a person (LM4)", kind, kind)
		}
	}
	for _, kind := range []string{"user", "cli"} {
		if _, ref := l.AppendAs(in, Actor{Kind: kind, ID: "x"}); ref != nil {
			t.Fatalf("the %s person door refused a told learning: %s", kind, ref.Reason)
		}
	}
	// The same agent actor writing observed is fine — the rule bars the
	// label, not the writer.
	if _, ref := l.AppendAs(AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "b", Provenance: ProvenanceObserved, ObservedCount: intp(1)},
		Actor{Kind: "agent", ID: "loop-1"}); ref != nil {
		t.Fatalf("an agent may file an observed learning: %s", ref.Reason)
	}
}

func TestActorMustBeReal(t *testing.T) {
	l, _ := openLearnings(t)
	in := AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "b", Provenance: ProvenanceObserved}
	if _, ref := l.AppendAs(in, Actor{Kind: "ghost", ID: "x"}); ref == nil {
		t.Fatal("an actor kind outside the four wrote — LM4's closed set must close before SQL")
	}
	if _, ref := l.AppendAs(in, Actor{Kind: "user", ID: ""}); ref == nil {
		t.Fatal("an empty actor id wrote — the audit exists to refuse the anonymous write")
	}
}

func TestRelayedNeedsTheSystemHook(t *testing.T) {
	l, _ := openLearnings(t)
	in := AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "the board answered", Provenance: ProvenanceRelayed}
	if _, ref := l.AppendRelayedAs(in, Actor{Kind: "user", ID: "1000"}); ref == nil {
		t.Fatal("a person-kind actor filed a relayed learning — relayed is a daemon hook's channel-word (LM4)")
	}
	got, ref := l.AppendRelayedAs(in, Actor{Kind: "system", ID: "learnings-board"})
	if ref != nil {
		t.Fatalf("the board actor refused: %s", ref.Reason)
	}
	if got.Provenance != ProvenanceRelayed {
		t.Fatalf("provenance %q, want relayed", got.Provenance)
	}
	if _, ref := l.AppendRelayedAs(AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "b", Provenance: ProvenanceObserved},
		Actor{Kind: "system", ID: "learnings-board"}); ref == nil {
		t.Fatal("AppendRelayedAs accepted a non-relayed input — the hook's door writes the board's label only")
	}
}

func TestAppendAsStillRunsTheShapeChecks(t *testing.T) {
	l, _ := openLearnings(t)
	a := Actor{Kind: "user", ID: "1000"}
	if _, ref := l.AppendAs(AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "", Provenance: ProvenanceObserved}, a); ref == nil {
		t.Fatal("the empty body slipped past the actor path — S28's shape checks ride AppendAs verbatim")
	}
	// §3 rides too: an unknown slug is refused whatever the actor.
	if _, ref := l.AppendAs(AppendInput{Scope: ScopeAgent, ScopeKey: "no-such-slug", Body: "b", Provenance: ProvenanceObserved}, a); ref == nil {
		t.Fatal("an agent-scoped learning on a slug with no agent row wrote — §3 rides the actor path unchanged")
	}
}

func TestLegacyAppendLeavesNoAuditLine(t *testing.T) {
	l, s := openLearnings(t)
	legacy, ref := l.Append(AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "old", Provenance: ProvenanceTold})
	if ref != nil {
		t.Fatalf("the legacy wrapper refused: %s", ref.Reason)
	}
	lines, err := s.ListLearningChanges(legacy.ID)
	if err != nil || len(lines) != 0 {
		t.Fatalf("legacy Append wrote audit lines %+v (%v) — the deviation is explicit: legacy writes are unaudited history, and the history says so", lines, err)
	}
	audited, ref := l.AppendAs(AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "new", Provenance: ProvenanceTold}, Actor{Kind: "user", ID: "1000"})
	if ref != nil {
		t.Fatalf("AppendAs refused: %s", ref.Reason)
	}
	if lines, err := s.ListLearningChanges(audited.ID); err != nil || len(lines) != 1 {
		t.Fatalf("the audited add carries %d lines (err %v), want its own — the census is only half the story", len(lines), err)
	}
	n, err := s.CountAllLearningChanges()
	if err != nil || n != 1 {
		t.Fatalf("audit census %d (err %v), want 1 — the audited add of a legacy-era store is the only line", n, err)
	}
}

func TestAgentScopeIsTheSlugNotTheName(t *testing.T) {
	l, s := openLearnings(t)
	// An agent whose slug and name DIFFER: the scope_key that resolves is
	// the SLUG — a NAME is a string the agent can be renamed past, which is
	// exactly why LM1 keys memory on the slug.
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "worker-one", Name: "Worker One"}, "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	a := Actor{Kind: "user", ID: "1000"}
	if _, ref := l.AppendAs(AppendInput{Scope: ScopeAgent, ScopeKey: "worker-one", Body: "b", Provenance: ProvenanceObserved}, a); ref != nil {
		t.Fatalf("the slug refused a real agent: %s", ref.Reason)
	}
	_, ref := l.AppendAs(AppendInput{Scope: ScopeAgent, ScopeKey: "Worker One", Body: "b", Provenance: ProvenanceObserved}, a)
	if ref == nil {
		t.Fatal("the NAME wrote — from S52 the agent scope resolves by slug alone (LM1); the refusal must name the slug it could not resolve")
	}
	if !strings.Contains(ref.Reason, "Worker One") {
		t.Fatalf("refusal %q does not name the scope_key it refused", ref.Reason)
	}
}

func intp(n int) *int { return &n }
