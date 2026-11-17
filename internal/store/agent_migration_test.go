package store

import (
	"context"
	"testing"
	"time"
)

// fixedClockT is the store's fixed clock — the same shape the store's own
// tests use (migration 0040 needs the FTS5 build tag, so these run under
// -tags sqlite_fts5).
func fixedClockT() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
}

// openAgentStoreT opens a migrated store in a temp dir.
func openAgentStoreT(t *testing.T) *Store {
	t.Helper()
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: fixedClockT()})
	if err != nil {
		t.Fatalf("open a store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// plantPolicy inserts a command_access policy row that targets an agent — the
// pre-migration state the name→slug migration (T2) re-points. It reaches the
// store's unexported writer, so it lives in the store package (the agentmgr
// cannot plant one, which is why the delete-ref test lives here, not there).
func plantPolicy(t *testing.T, s *Store, id, targetID string) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at) VALUES (?, ?, 'agent', ?, 'allow', 'spec', 'operator', NULL, ?, ?)`, id, "command_access", targetID, "2026-10-30T12:00:00Z", "2026-10-30T12:00:00Z"); err != nil {
		t.Fatalf("plant the policy: %v", err)
	}
}

// TestAgentReferencedByPolicySeesAPolicyRow is AM4's policy half at the store
// level: the delete-ref check (agentmgr.Delete calls this) reads a real
// policy row. The agentmgr cannot plant a policy row, so the behaviour is
// proven here where the unexported writer is reachable.
func TestAgentReferencedByPolicySeesAPolicyRow(t *testing.T) {
	s := openAgentStoreT(t)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "groot", Name: "Groot", Version: "1"}, "2026-10-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// No reference yet: the delete would proceed.
	if refs, err := s.AgentReferencedByPolicy("groot"); err != nil || len(refs) != 0 {
		t.Fatalf("no reference: got %v, err %v; want empty", refs, err)
	}
	// Plant a policy that targets the slug: now the delete is refused.
	plantPolicy(t, s, "pol-1", "groot")
	refs, err := s.AgentReferencedByPolicy("groot")
	if err != nil || len(refs) != 1 {
		t.Fatalf("with a reference: got %v, err %v; want 1", refs, err)
	}
}

// The rules written for an agent's own tools (ids `grant.<slug>.<tool>`) are the agent's: they go
// with it when it is deleted, so they are not a reference that refuses the delete — while a rule
// someone else wrote about the agent still is.
func TestAgentGrantRulesDoNotRefuseTheDelete(t *testing.T) {
	s := openAgentStoreT(t)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "groot", Name: "Groot", Version: "1"}, "2026-10-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	plantPolicy(t, s, "grant.groot.read", "groot")
	plantPolicy(t, s, "grant.groot.bash.cmd", "groot")
	if refs, err := s.AgentReferencedByPolicy("groot"); err != nil || len(refs) != 0 {
		t.Fatalf("grant rules must not refuse the delete: %v %v", refs, err)
	}
	plantPolicy(t, s, "no-shell-for-groot", "groot")
	refs, err := s.AgentReferencedByPolicy("groot")
	if err != nil || len(refs) != 1 || refs[0] != "no-shell-for-groot" {
		t.Fatalf("a rule someone else wrote still refuses: %v %v", refs, err)
	}
}

// TestMigrateRepointsPolicyNamesToSlugs is T2's name→slug policy migration: a
// policy that targets an agent NAME is re-pointed at its SLUG, idempotent.
func TestMigrateRepointsPolicyNamesToSlugs(t *testing.T) {
	s := openAgentStoreT(t)
	// A first-party agent, by name (backfilled to a row by the migration).
	if err := s.CreatePluginAgent("groot", "/packages/groot", "2026-10-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// A policy that targets the agent by NAME (the pre-migration state).
	plantPolicy(t, s, "pol-1", "groot")
	// The domain's Migrate back-fills the agent row (from plugin_agent)
	// before re-pointing the policy references — mirror it here.
	if _, err := s.MigrateFirstPartyAgentsAudited(context.Background(), userAgentActor, "2026-10-30T12:00:00Z"); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if _, err := s.MigrateAgentPolicyReferences(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The policy now targets the slug, not the name.
	var got string
	if err := s.r.QueryRow(`SELECT target_id FROM policy WHERE id = 'pol-1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "groot" {
		t.Fatalf("policy target_id = %q, want groot (the slug)", got)
	}
	// Re-running the migration is a no-op (idempotent).
	if n, err := s.MigrateAgentPolicyReferences(); err != nil || n != 0 {
		t.Fatalf("second migration: got %d, err %v; want 0 (idempotent)", n, err)
	}
}

// TestMigrateRefusesAnUnresolvedName is the T2 call: a name that resolves to
// no agent refuses the WHOLE migration (all-or-nothing, not a half-migration).
func TestMigrateRefusesAnUnresolvedName(t *testing.T) {
	s := openAgentStoreT(t)
	// A policy that targets a name that resolves to no agent row.
	plantPolicy(t, s, "pol-1", "ghost")
	if _, err := s.MigrateAgentPolicyReferences(); err == nil {
		t.Fatalf("a name with no agent row: expected a refusal, got nil — the migration must be all-or-nothing")
	}
}
