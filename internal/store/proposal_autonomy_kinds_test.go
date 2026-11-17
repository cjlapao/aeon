package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// autonomyKindsMigrationIndex is S56 T8's widening position in the runner.
// The original `Migrations()[:len(Migrations())-1]` recipe assumed the
// widening was the LAST migration — true the day it was written, false the
// moment S46 T6's mcp_grant_revoke rebuild (20261101193000, sorting AFTER
// 20261101180000) merged. Tests pin "before S56's rebuild" by NAME now:
// the parallel-widening rule that S46 T6's migration header spells out
// (any later rebuild restates the UNION of kinds; any test locating a
// rebuild by position breaks when a sibling lands beside it).
func autonomyKindsMigrationIndex(t *testing.T) int {
	t.Helper()
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_proposal_autonomy_kinds") {
			return i
		}
	}
	t.Fatal("S56 T8's widening migration is missing from the runner")
	return -1
}

// S56 T8 — migration 20261101180000 widens proposal.kind with
// 'autonomy_revoke' and 'autonomy_deny' by the same rebuild pattern as 0043
// (copy, drop, rename) — the two RESTRICTIVE autonomy proposal kinds (AL11).
// There is no third autonomy kind: no proposal shape creates an allow, and
// T7's line-offense guard walks this file for a line carrying both a grant
// table name and the kind column — this file names neither.

// TestProposalKindMigrationKeepsEveryRow is the copy-guard core: a database
// that reaches the widening with rows keeps them — the copy moves every row
// and the copy-guard asserts the move was total. A row with applied_error set
// and one with decided_* set read back identical after the upgrade.
func TestProposalKindMigrationKeepsEveryRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	// Apply everything BEFORE the widening (by NAME — see
	// autonomyKindsMigrationIndex; the [:len-1] recipe died the day a
	// sibling slice widened after this one).
	applyMigrations(t, g, Migrations()[:autonomyKindsMigrationIndex(t)])
	if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id,applied_error)
		VALUES ('policy_add', '{}', 'proposed', '2026-10-29T00:00:00Z', 'user', 'u1', 'it failed')`).Error; err != nil {
		t.Fatalf("seed row 1: %v", err)
	}
	if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason)
		VALUES ('agent_import', '{}', 'committed', '2026-10-29T00:00:00Z', 'agent', 'a1', '2026-10-29T00:00:00Z', 'user', 'u2', 'because')`).Error; err != nil {
		t.Fatalf("seed row 2: %v", err)
	}
	closeDB()

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
	var byKind = map[string]Proposal{}
	for _, r := range rows {
		byKind[r.Kind] = r
	}
	if pa, ok := byKind["policy_add"]; !ok || pa.AppliedError != "it failed" {
		t.Fatalf("the policy_add row (applied_error) drifted: %+v", byKind["policy_add"])
	}
	if mc, ok := byKind["agent_import"]; !ok || mc.DecidedByKind != "user" || mc.DecidedByID != "u2" || mc.DecideReason != "because" {
		t.Fatalf("the agent_import row (decided pair) drifted: %+v", byKind["agent_import"])
	}
}

// TestProposalKindMigrationAdmitsTheTwoRestrictiveKindsAndStillRefusesOthers
// pins the CHECK widening: the two autonomy kinds are admitted, and a bad
// kind — including an autonomy_grant spelling nobody may author — is still
// refused. The same inserts fail on a store at Migrations()[:len-1] (before
// the widening), which proves the widening did the work.
func TestProposalKindMigrationAdmitsTheTwoRestrictiveKindsAndStillRefusesOthers(t *testing.T) {
	{
		dir := t.TempDir()
		s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
		if err != nil {
			t.Fatalf("open (full schema): %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		for _, k := range []string{ProposalKindAutonomyRevoke, ProposalKindAutonomyDeny} {
			if _, err := s.CreateProposal(k, "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z"); err != nil {
				t.Fatalf("%s must be admitted after the widening: %v", k, err)
			}
		}
		for _, bad := range []string{"autonomy_grant", "autonomy_allow", "grant"} {
			if _, err := s.CreateProposal(bad, "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z"); err == nil {
				t.Fatalf("%s must still be refused (the CHECK widened by exactly the two restrictive kinds)", bad)
			}
		}
	}
	// Before the widening: both kinds are refused.
	{
		dir := t.TempDir()
		path := filepath.Join(dir, "state.db")
		g, closeDB := rawGorm(t, path)
		applyMigrations(t, g, Migrations()[:autonomyKindsMigrationIndex(t)])
		for _, k := range []string{ProposalKindAutonomyRevoke, ProposalKindAutonomyDeny} {
			if err := g.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id)
				VALUES (?, '{}', 'proposed', '2026-10-29T00:00:00Z', 'user', 'u1')`, k).Error; err == nil {
				t.Fatalf("%s must be refused before the widening (the CHECK not yet widened)", k)
			}
		}
		closeDB()
	}
}

// TestProposalKindMigrationIndexSetIsUnchanged is the index-ride-out pin:
// proposal has no index, so the DROP takes none and the rename restores none.
func TestProposalKindMigrationIndexSetIsUnchanged(t *testing.T) {
	fresh := indexNamesOn(t, "proposal")
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations()[:autonomyKindsMigrationIndex(t)])
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

// TestStoreAcceptsTheTwoAutonomyKinds is the Go-closed-set pin (AGENTS §3
// mirroring): CreateProposal accepts both autonomy kinds and writes a
// 'proposed' row for each.
func TestStoreAcceptsTheTwoAutonomyKinds(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, k := range []string{ProposalKindAutonomyRevoke, ProposalKindAutonomyDeny} {
		row, err := s.CreateProposal(k, "{}", Actor{Kind: "user", ID: "u1"}, "2026-10-29T00:00:00Z")
		if err != nil {
			t.Fatalf("CreateProposal must accept %s: %v", k, err)
		}
		if row.Kind != k || row.Status != "proposed" {
			t.Fatalf("the row must be %s / proposed: %+v", k, row)
		}
	}
}
