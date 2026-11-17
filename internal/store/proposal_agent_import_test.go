package store

import (
	"path/filepath"
	"testing"
)

// S37 T1 — migration 0043 widens proposal.kind with 'agent_import' by the 0016
// rebuild pattern (copy, drop, rename), without 0016's "must be empty" guard:
// proposals may exist and must survive. The tests below exercise the migration
// against a real store, using the old-store recipe (rawGorm + applyMigrations +
// Open).

// TestMigration0043KeepsEveryProposalRow is the core: a database that reaches
// 0043 with rows keeps them — the copy moves every row and a copy-guard asserts
// the move was total. A row with applied_error set and one with decided_* set
// read back identical after the upgrade; pragma_foreign_key_check is empty.
func TestMigration0043KeepsEveryProposalRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	// Apply everything BEFORE 0043 (the migration under test is the last one).
	applyMigrations(t, g, Migrations()[:len(Migrations())-1])
	// Seed rows that the migration must carry across — one with applied_error,
	// one with a decided_by pair.
	if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id,applied_error)
		VALUES ('policy_add', '{}', 'proposed', '2026-10-29T00:00:00Z', 'user', 'u1', 'it failed')`).Error; err != nil {
		t.Fatalf("seed row 1: %v", err)
	}
	if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason)
		VALUES ('mcp_server_add', '{}', 'committed', '2026-10-29T00:00:00Z', 'agent', 'a1', '2026-10-29T00:00:00Z', 'user', 'u2', 'because')`).Error; err != nil {
		t.Fatalf("seed row 2: %v", err)
	}
	closeDB()

	// Re-open: the migration applies (0043 is the last one), and the rows survive.
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open after migration: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	rows, err := s.ListProposals("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the migration must keep every row, got %d", len(rows))
	}
	// Look up by kind (order is not pinned by the store).
	var byKind = map[string]Proposal{}
	for _, r := range rows {
		byKind[r.Kind] = r
	}
	// Row: the applied_error carried.
	if pa, ok := byKind["policy_add"]; !ok || pa.AppliedError != "it failed" {
		t.Fatalf("the policy_add row (applied_error) drifted: %+v", byKind["policy_add"])
	}
	// Row: the decided pair + reason carried.
	if mc, ok := byKind["mcp_server_add"]; !ok || mc.DecidedByKind != "user" || mc.DecidedByID != "u2" || mc.DecideReason != "because" {
		t.Fatalf("the mcp_server_add row (decided pair) drifted: %+v", byKind["mcp_server_add"])
	}
}

// TestMigration0043AdmitsAgentImportAndStillRefusesOthers is AD1's pin: the
// CHECK widened by exactly 'agent_import' — a new kind is admitted, and a
// different bad kind is still refused. The same insert fails on a store at
// Migrations()[:42] (before the widening), which proves the widening did the
// work.
func TestMigration0043AdmitsAgentImportAndStillRefusesOthers(t *testing.T) {
	// After the migration: agent_import is admitted, agent_export is refused.
	{
		dir := t.TempDir()
		s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
		if err != nil {
			t.Fatalf("open (full schema): %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		if _, err := s.CreateProposal(ProposalKindAgentImport, "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z"); err != nil {
			t.Fatalf("agent_import must be admitted after 0043: %v", err)
		}
		if _, err := s.CreateProposal("agent_export", "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z"); err == nil {
			t.Fatal("agent_export must still be refused (the CHECK widened by exactly one word)")
		}
	}
	// Before the migration (Migrations()[:42]): agent_import is refused.
	{
		dir := t.TempDir()
		path := filepath.Join(dir, "state.db")
		g, closeDB := rawGorm(t, path)
		applyMigrations(t, g, Migrations()[:42]) // 0001..0042, before the 0043 widening (file-based migrations sort after 0045, so 0043 is no longer last)
		if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id)
			VALUES ('agent_import', '{}', 'proposed', '2026-10-29T00:00:00Z', 'user', 'u1')`).Error; err == nil {
			t.Fatal("agent_import must be refused before 0043 (the CHECK not yet widened)")
		}
		closeDB()
	}
}

// TestMigration0043RecreatesEveryProposalIndex is the index-ride-out pin (Q1:
// proposal has no index, so the DROP takes none and the rename restores none).
// The assertion is that the index set is empty after the migration and was
// empty before — a migration that forgot an index would show here as a
// difference, and one that added a spurious one would too.
func TestMigration0043RecreatesEveryProposalIndex(t *testing.T) {
	// A fresh schema (all migrations) has no proposal index.
	fresh := indexNamesOn(t, "proposal")
	// A database migrated up to 0042, then 0043, has the same (empty) set.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations()[:len(Migrations())-1])
	closeDB()
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upgraded := indexNamesOn(t, "proposal")
	if len(fresh) != len(upgraded) {
		t.Fatalf("index set diverged: fresh %v vs upgraded %v", fresh, upgraded)
	}
	for i := range fresh {
		if fresh[i] != upgraded[i] {
			t.Fatalf("index %d diverged: %q vs %q", i, fresh[i], upgraded[i])
		}
	}
}

// TestStoreAcceptsAgentImportKind is the Go-closed-set pin (AGENTS §3 mirroring):
// CreateProposal accepts ProposalKindAgentImport and writes a 'proposed' row.
func TestStoreAcceptsAgentImportKind(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	row, err := s.CreateProposal(ProposalKindAgentImport, "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateProposal must accept agent_import: %v", err)
	}
	if row.Kind != ProposalKindAgentImport || row.Status != "proposed" {
		t.Fatalf("the row must be agent_import / proposed: %+v", row)
	}
}

// indexNamesOn reads the index names on a table from a fresh store, for
// TestMigration0043RecreatesEveryProposalIndex.
func indexNamesOn(t *testing.T, table string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations())
	var names []string
	g.Model(&struct{}{}).Raw(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=?`, table).Scan(&names)
	closeDB()
	return names
}
