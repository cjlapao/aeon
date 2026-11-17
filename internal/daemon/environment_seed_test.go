package daemon

// environment_seed_test.go — S59 T6 (EV19): the standard environment set,
// applied by the daemon's own start hook, and the two things that make the
// slice work at all on a machine that has never seen Aeon:
//
//   - the `host-scrubbed` system row exists after a start, and nothing about a
//     second start changes it;
//   - the workspace is bound to it, because EV2 has no fallback and an
//     unbound workspace refuses every command — and a start must never move
//     that binding once a person has made their own choice.
//
// The tests ride `ApplyInertAtStart`, the hook serve calls, rather than calling
// the applier by hand: a set that only works when invoked directly is a set the
// daemon does not actually apply.

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv/envseed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

func TestStandardEnvironmentSetIsDeclaredInEveryDoorsEngine(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s) // the one engine builder every door holds
	statuses, err := e.Status(context.Background(), envseed.SetID)
	if err != nil {
		t.Fatalf("seed.status(%s): %v", envseed.SetID, err)
	}
	if len(statuses) != 1 {
		t.Fatalf("%d status rows for the set, want 1", len(statuses))
	}
	if statuses[0].Class != seed.ClassInert {
		t.Errorf("class = %q, want inert: a set applied at start with no human in the loop may not widen anything", statuses[0].Class)
	}
	if statuses[0].AvailableVersion != envseed.SetVersion {
		t.Errorf("available version = %d, want %d", statuses[0].AvailableVersion, envseed.SetVersion)
	}
	if len(statuses[0].Records) != 1 || statuses[0].Records[0].Key != envseed.SystemScrubbedSlug {
		t.Fatalf("the set's records = %+v, want the one system environment", statuses[0].Records)
	}
}

func TestStartSeedsTheSystemEnvironmentAndTheFirstBinding(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s)
	ctx := context.Background()

	ApplyInertAtStart(ctx, e)

	row, err := s.GetEnvironment(envseed.SystemScrubbedSlug)
	if err != nil {
		t.Fatalf("the seeded environment is not there after a start: %v", err)
	}
	if row.Origin != "system" {
		t.Errorf("origin = %q, want system (EV19: the set wrote it, and only the seed path may say so)", row.Origin)
	}
	if row.Backend != "host-scrubbed" {
		t.Errorf("backend = %q, want host-scrubbed", row.Backend)
	}
	if row.Legacy != 0 {
		t.Errorf("legacy = %d, want 0: the seed's whole purpose is an environment that does not inherit (EV1)", row.Legacy)
	}
	if row.Revision != 1 {
		t.Errorf("revision = %d, want 1 after one apply", row.Revision)
	}

	binding, source, err := s.BindingFor("")
	if err != nil {
		t.Fatalf("binding for the workspace: %v", err)
	}
	if source != "workspace" || binding.EnvironmentID != row.ID {
		t.Errorf("effective binding = %q from %q, want the seeded environment from the workspace scope",
			binding.EnvironmentID, source)
	}

	// Both writes are changing writes, so both are audited (EV17) — and by the
	// system actor the seed path stamps, not by a user the daemon invents.
	changes, err := s.ListChanges(store.EnvironmentChangeFilter{SubjectKind: "environment", SubjectID: row.ID})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Action != "create" {
		t.Fatalf("environment audit = %+v, want one create row", changes)
	}
	if changes[0].ActorKind != store.SeedActorKind || changes[0].ActorID != store.SeedActorID {
		t.Errorf("audit actor = %s/%s, want %s/%s",
			changes[0].ActorKind, changes[0].ActorID, store.SeedActorKind, store.SeedActorID)
	}
	if changes[0].SpecAfter == nil || *changes[0].SpecAfter != row.Spec {
		t.Errorf("the audit row does not carry the spec the row holds (EV17's diffability)")
	}
	bindingChanges, err := s.ListChanges(store.EnvironmentChangeFilter{SubjectKind: "binding", SubjectID: "workspace"})
	if err != nil {
		t.Fatalf("binding changes: %v", err)
	}
	if len(bindingChanges) != 1 || bindingChanges[0].Action != "bind" {
		t.Fatalf("binding audit = %+v, want one bind row", bindingChanges)
	}

	// The ledger says applied: `seed.status` is what a person reads when they
	// wonder whether the daemon shipped what it says it shipped.
	statuses, err := e.Status(ctx, envseed.SetID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(statuses) != 1 || len(statuses[0].Records) != 1 {
		t.Fatalf("status after a start = %+v, want one set with one record", statuses)
	}
	rec := statuses[0].Records[0]
	if rec.State != seed.StateApplied || rec.AppliedVersion != envseed.SetVersion {
		t.Errorf("the record's state = %q appliedVersion=%d, want %q and %d (the ledger is what a person reads to know the daemon shipped what it says)",
			rec.State, rec.AppliedVersion, seed.StateApplied, envseed.SetVersion)
	}
	if rec.Reason != "" {
		t.Logf("the record carries a reason: %s", rec.Reason)
	}
}

func TestASecondStartWritesNothing(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s)
	ctx := context.Background()

	ApplyInertAtStart(ctx, e)
	first, err := s.GetEnvironment(envseed.SystemScrubbedSlug)
	if err != nil {
		t.Fatalf("read after the first start: %v", err)
	}

	// A restart — the ordinary case, and the one an idempotence bug hides: the
	// engine plans a skip for a present row, and even a plan that did not skip
	// lands in a store path that refuses to touch it.
	ApplyInertAtStart(ctx, e)

	second, err := s.GetEnvironment(envseed.SystemScrubbedSlug)
	if err != nil {
		t.Fatalf("read after the second start: %v", err)
	}
	if second.ID != first.ID || second.Revision != first.Revision || second.Spec != first.Spec || second.UpdatedAt != first.UpdatedAt {
		t.Errorf("the second start changed the row: rev %d→%d updatedAt %q→%q",
			first.Revision, second.Revision, first.UpdatedAt, second.UpdatedAt)
	}
	if n := countEnvironmentChanges(t, s, "environment", second.ID); n != 1 {
		t.Errorf("%d audit rows for the environment after two starts, want 1 (a write that wrote nothing is not a changing write)", n)
	}
	if n := countEnvironmentChanges(t, s, "binding", "workspace"); n != 1 {
		t.Errorf("%d audit rows for the workspace binding after two starts, want 1", n)
	}
}

func TestAStartNeverMovesAPersonsWorkspaceBinding(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s)
	ctx := context.Background()
	ApplyInertAtStart(ctx, e)

	system, err := s.GetEnvironment(envseed.SystemScrubbedSlug)
	if err != nil {
		t.Fatalf("the seeded row is missing: %v", err)
	}
	// The person creates their own environment and binds the workspace to it —
	// through the store's door path, which is the path the panel and CLI use.
	theirs, ref, err := s.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: "project-default", Name: "Project default", Backend: "host-scrubbed", Spec: system.Spec,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the person's environment: ref=%v err=%v", ref, err)
	}
	if _, ref, err := s.BindEnvironment(store.Actor{Kind: "user", ID: "person"},
		store.NewBinding{ScopeKind: "workspace", EnvironmentID: theirs.ID}); ref != nil || err != nil {
		t.Fatalf("the person's bind failed: ref=%v err=%v", ref, err)
	}

	// A restart re-applies the set. The first-binding rule says the answer is
	// "a binding already exists" and nothing else.
	ApplyInertAtStart(ctx, e)

	binding, source, err := s.BindingFor("")
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	if source != "workspace" || binding.EnvironmentID != theirs.ID {
		t.Errorf("after a restart the workspace is bound to %q from %q, want the person's %q",
			binding.EnvironmentID, source, theirs.ID)
	}
}

// TestTheSeededRowIsWhatAResolutionFinds ties EV19 to EV2 and EV6: the row the
// seed writes is the row the resolver reads. A seeded environment the resolver
// could not use — an unparseable spec, a slug the downgrade does not know — is
// a daemon that seeds a table nobody can read.
func TestTheSeededRowIsWhatAResolutionFinds(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s)
	ApplyInertAtStart(context.Background(), e)

	r := newStepExecResolver(s)
	if r.scrubbedSystem() == nil {
		t.Fatalf("scrubbedSystem() is nil after the seed applied: EV6's downgrade target must exist (its absence is a refusal, not an inherit)")
	}

	c, err := r.Preview(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if c.refusal != nil {
		t.Fatalf("an unattended workspace preview refused after the seed applied: %s · %s", c.refusal.What, c.refusal.Do)
	}
	if c.res.Env.Slug != envseed.SystemScrubbedSlug || c.res.Env.Backend != "host-scrubbed" {
		t.Errorf("preview resolved %s/%s, want the seeded host-scrubbed environment", c.res.Env.Slug, c.res.Env.Backend)
	}
	if c.child.Legacy() {
		t.Errorf("the previewed child inherits the daemon's environment; the seeded row must not")
	}
	if got := scopeOf(c.in, c.bound); got != "workspace" {
		t.Errorf("the preview says the binding came from %q, want workspace", got)
	}
	names := map[string]bool{}
	for _, v := range c.child.Names() {
		names[v.Name] = true
	}
	for _, want := range []string{"PATH", "HOME"} {
		if !names[want] {
			t.Errorf("the previewed child is missing %s, which the shipped allowlist names", want)
		}
	}
	if names["ANTHROPIC_API_KEY"] {
		t.Errorf("the previewed child carries the daemon's provider key: host-scrubbed scrubbed nothing")
	}
}

// countEnvironmentChanges counts a subject's audit rows. The store's own test
// file has the same helper for its package; the daemon asks through the read
// the doors use instead of the raw query, so this file cannot pass by reading
// a table the doors would not show.
func countEnvironmentChanges(t *testing.T, s *store.Store, subjectKind, subjectID string) int {
	t.Helper()
	rows, err := s.ListChanges(store.EnvironmentChangeFilter{SubjectKind: subjectKind, SubjectID: subjectID, Limit: 100})
	if err != nil {
		t.Fatalf("list changes for %s/%s: %v", subjectKind, subjectID, err)
	}
	return len(rows)
}
