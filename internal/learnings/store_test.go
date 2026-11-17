package learnings

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func openLearnings(t *testing.T) (*Learnings, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// S52 T1 (fixture growth, deviation 1 in the T1 Notes): the §3 agent
	// check moved from handler.Lookup to the agent table — SLUG is the
	// authority now — so the two agent names the pinned tests below scope
	// learnings to must have rows for the check to resolve them. The test
	// bodies are untouched, hash-pinned as they were; only the fixture
	// grows to the new authority.
	for _, slug := range []string{"golang-engineer", "imported-agent"} {
		if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: slug, Name: slug}, "2026-11-04T00:00:00Z"); err != nil {
			t.Fatalf("seed the %s agent row the S52 slug check reads: %v", slug, err)
		}
	}
	return New(s), s
}

func mustAppend(t *testing.T, l *Learnings, in AppendInput) Learning {
	t.Helper()
	got, ref := l.Append(in)
	if ref != nil {
		t.Fatalf("append (%s/%s/%s): %s", in.Scope, in.ScopeKey, in.Provenance, ref.Reason)
	}
	return got
}

// TestAgentScopeValidatedNotFKed — design §3: the agent scope is validated
// by a Go-level check, never a SQL foreign key. A learning scoped to a
// real S13 body writes; one scoped to a name that is neither a body nor a
// non-revoked plugin_agent is refused here, not by a schema that has
// nothing to enforce it.
func TestAgentScopeValidatedNotFKed(t *testing.T) {
	l, _ := openLearnings(t)
	// Positive control: a real S13 body (handler.Lookup) is a valid scope.
	got := mustAppend(t, l, AppendInput{
		Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "forgets nil-map checks",
		Provenance: ProvenanceObserved,
	})
	if got.Scope != ScopeAgent || got.ScopeKey != "golang-engineer" {
		t.Fatalf("the agent-scoped learning did not round-trip: %+v", got)
	}
	// Negative: a name that is neither a body nor a plugin agent is refused
	// by the Go door, not accepted by a schema with no FK to enforce it.
	_, ref := l.Append(AppendInput{Scope: ScopeAgent, ScopeKey: "not-an-agent", Body: "x", Provenance: ProvenanceTold})
	if ref == nil {
		t.Fatal("a learning scoped to a nonexistent agent must be refused — a schema with no FK cannot enforce this, so the check must live in Go (design §3)")
	}
}

// TestRevokedPluginAgentRefused — the §3 check's sharpest edge: a disabled
// plugin agent still exists (it can be re-enabled), so its scope is valid;
// a revoked one does not, so its scope is refused — revoked is not merely
// disabled.
func TestRevokedPluginAgentRefused(t *testing.T) {
	l, s := openLearnings(t)
	// A disabled plugin agent is a real agent (not revoked) — a valid scope.
	if err := s.CreatePluginAgent("imported-agent", "local", "2026-10-30T06:00:00Z"); err != nil {
		t.Fatalf("create plugin agent: %v", err)
	}
	got, ref := l.Append(AppendInput{Scope: ScopeAgent, ScopeKey: "imported-agent", Body: "x", Provenance: ProvenanceTold})
	if ref != nil {
		t.Fatalf("a disabled plugin agent is a real agent and its scope must be valid: %s", ref.Reason)
	}
	if got.ScopeKey != "imported-agent" {
		t.Fatalf("the disabled plugin agent's learning did not round-trip: %+v", got)
	}
	// Revoking it makes the scope invalid — the refused answer names the
	// revocation, not a not-found. (fromState names the record's current
	// state, disabled — the empty form is the import landing, none →
	// disabled, which would not move the record to revoked.)
	if err := s.TransitionPluginAgent("imported-agent", "disabled", "revoked", "superseded", "2026-10-30T06:01:00Z"); err != nil {
		t.Fatalf("revoke plugin agent: %v", err)
	}
	_, ref = l.Append(AppendInput{Scope: ScopeAgent, ScopeKey: "imported-agent", Body: "x", Provenance: ProvenanceTold})
	if ref == nil {
		t.Fatal("a learning scoped to a REVOKED plugin agent must be refused — revoked is not merely disabled, and a disabled agent is a valid scope but a revoked one is not (design §3)")
	}
}

// TestRelaxedIsWriteProtected — M1: the public Append refuses relayed; the
// board-answer hook (AppendRelaxed) is the one caller permitted to set it.
func TestRelaxedIsWriteProtected(t *testing.T) {
	l, _ := openLearnings(t)
	_, ref := l.Append(AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "x", Provenance: ProvenanceRelayed})
	if ref == nil {
		t.Fatal("the public Append must refuse relayed — a hand-authored relaxed makes the label lie about its own source (M1)")
	}
	got, ref := l.AppendRelayed(AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "the board answered", Provenance: ProvenanceRelayed})
	if ref != nil {
		t.Fatalf("the board-answer hook may write relayed: %s", ref.Reason)
	}
	if got.Provenance != ProvenanceRelayed {
		t.Fatalf("AppendRelaxed must set relayed, got %s", got.Provenance)
	}
}

// TestTagFilterNeverDependsOnEmbeddingRoute — D5: the tag filter works with
// no embedding route configured (the normal state, not a degraded one). A
// query filters by tag and returns the right set without any route.
func TestTagFilterNeverDependsOnEmbeddingRoute(t *testing.T) {
	l, _ := openLearnings(t)
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "wraps errors", Provenance: ProvenanceTold, Tags: []string{"auth"}})
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "uses go routines", Provenance: ProvenanceTold, Tags: []string{"concurrency"}})

	// No embedding route is configured (rank is nil) — the query still
	// filters by tag and returns the one learning tagged auth.
	res, err := l.Query(context.Background(), QueryFilter{Tags: []TagPattern{{Pattern: "auth"}}})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) != 1 || res[0].Body != "wraps errors" {
		t.Fatalf("tag filter without a route returned %+v, want the one auth-tagged learning — the filter must never depend on an embedding route (D5)", res)
	}
}

// TestEveryResultCarriesProvenanceAndStrength — M3: every result carries
// its provenance, and an observed learning carries its count; the label is
// never collapsed into a single score.
func TestEveryResultCarriesProvenanceAndStrength(t *testing.T) {
	l, _ := openLearnings(t)
	count := 3
	mustAppend(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "observed: nil maps", Provenance: ProvenanceObserved, ObservedCount: &count})
	mustAppend(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "told: wrap errors", Provenance: ProvenanceTold})
	mustAppend(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "assumed: maybe race", Provenance: ProvenanceAssumed})

	res, err := l.List(ScopeAgent, "golang-engineer")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("list returned %d, want 3", len(res))
	}
	// Each carries its own provenance; the observed one carries its count
	// and the others carry none — the label is never collapsed.
	byBody := map[string]Learning{}
	for _, lr := range res {
		byBody[lr.Body] = lr
	}
	if byBody["observed: nil maps"].Provenance != ProvenanceObserved || byBody["observed: nil maps"].ObservedCount == nil || *byBody["observed: nil maps"].ObservedCount != 3 {
		t.Fatalf("the observed learning must carry its count (M3): %+v", byBody["observed: nil maps"])
	}
	if byBody["told: wrap errors"].Provenance != ProvenanceTold || byBody["told: wrap errors"].ObservedCount != nil {
		t.Fatalf("a told learning must not carry a count: %+v", byBody["told: wrap errors"])
	}
	if byBody["assumed: maybe race"].Provenance != ProvenanceAssumed {
		t.Fatalf("the assumed learning must carry its provenance: %+v", byBody["assumed: maybe race"])
	}
}
