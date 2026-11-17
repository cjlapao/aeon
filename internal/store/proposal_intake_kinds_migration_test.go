package store

// proposal_intake_kinds_migration_test.go — S61 T5's migration drill, the
// M2-name shape S57 set and S60 T9 re-walked (P9: fresh vs upgraded,
// identical). The eighteenth and nineteenth kinds arrived as a table REBUILD
// — SQLite cannot widen a CHECK in place — and the only proof that matters
// is that an upgraded store carries the byte-identical schema of a fresh one
// AND every pre-existing proposal row, byte for byte, through the
// DROP/RENAME. The copy guard inside the migration asserts totality at apply
// time on every machine; this test asserts it here with SEEDED rows (the
// guard would pass vacuously on an empty table), and the CHECK's teeth are
// tasted from both sides: BOTH new kinds land after the upgrade, and the
// five names that must stay unnameable still die.

import (
	"strings"
	"testing"
)

const s61ProposalKindMigration = "20261105110000_proposal_kinds_intake"

// s61ProposalKindVintage splits the full migration set at S61 T5's file
// three ways — strictly-earlier (the vintage a store upgrades FROM), mine,
// strictly-later. The third half exists because the proposal CHECK is
// widened more than once: a two-way split applies a LATER kind rebuild
// BEFORE mine, and the two rebuilds then clobber each other's CHECK (S61 T5
// measured exactly that drift walking S60's drill after adding this file).
func s61ProposalKindVintage(t *testing.T) (before, mine, after []Migration) {
	t.Helper()
	for _, m := range Migrations() {
		switch {
		case m.Name == s61ProposalKindMigration:
			mine = append(mine, m)
		case m.Name < s61ProposalKindMigration:
			before = append(before, m)
		default:
			after = append(after, m)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("the S61 T5 migration set = %d entries, want 1", len(mine))
	}
	return before, mine, after
}

// TestProposalKindWidenedByRebuild is the spec's named drill for this slice:
// the rebuild keeps rows, both new kinds arrive, and the forbidden names
// stay refused.
func TestProposalKindWidenedByRebuild(t *testing.T) {
	fresh := openMigrated(t)
	freshDump := schemaDump(t, fresh)

	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	before, mine, after := s61ProposalKindVintage(t)
	applyMigrations(t, g, before)

	seed := []struct {
		kind, payload, status, byKind, byID string
	}{
		{"blueprint_save", `{"name":"b","content":"line one\nline \"two\"", "createOnly":true,"note":"comma, note"}`, "proposed", "agent", "planner"},
		{"mergequeue_enqueue", `{"componentRoot":"/srv/a","branch":"feature/x,\"q\"","reason":"green"}`, "committed", "user", "cjlapao"},
		{"roadmap_idea", `{"componentRoot":"/srv/a","title":"quote\" this, \" and that","note":"n"}`, "proposed", "agent", "implementer"},
	}
	for i, s := range seed {
		if err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
			VALUES (?, ?, ?, '2026-11-05T12:00:0' || ? || 'Z', ?, ?)`,
			s.kind, s.payload, s.status, i, s.byKind, s.byID).Error; err != nil {
			t.Fatalf("seed proposal %d: %v", i, err)
		}
	}
	pre, preSum := tableFingerprint(t, g, "proposal")
	preLedger := ledgerCount(t, g)

	applyMigrations(t, g, mine)

	// The three seeded kinds (one decided, one with comma/quote-laced
	// payload — quoting is where copy scripts die) survived the DROP/RENAME
	// byte for byte.
	post, postSum := tableFingerprint(t, g, "proposal")
	if post != pre || postSum != preSum {
		t.Errorf("the proposal table changed across the kind rebuild: (%d,%s) -> (%d,%s)", pre, preSum, post, postSum)
	}
	// The transient guards did their jobs and left no furniture behind.
	for _, guard := range []string{"proposal_20261105110000_copy_guard", "proposal_20261105110000_fk_guard"} {
		if countCatalogueEntries(t, g, "table", guard) != 0 {
			t.Errorf("the transient guard table %s survived the migration", guard)
		}
	}
	// The CHECK from both sides: BOTH new kinds now land…
	for _, kind := range []string{"charter_save", "intake_plan"} {
		if err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
			VALUES (?, '{}', 'proposed', '2026-11-05T13:00:00Z', 'agent', 'implementer')`, kind).Error; err != nil {
			t.Errorf("the kind %s is refused after the upgrade: %v", kind, err)
		}
	}
	// …and the five names that must STAY unnameable still die at the CHECK
	// (the migration header's deliberate absences: append-only charter
	// history, and intake.commit being a method, not a kind).
	for _, kind := range []string{"charter_revert", "charter_delete", "charter_widen", "intake_commit", "intake_publish"} {
		err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
			VALUES (?, '{}', 'proposed', '2026-11-05T13:00:00Z', 'agent', 'implementer')`, kind).Error
		if err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("kind %q was %v after the rebuild — the widened CHECK let a forbidden name in", kind, err)
		}
	}
	if got := ledgerCount(t, g); got != preLedger+1 {
		t.Errorf("ledger rows %d -> %d, want +1", preLedger, got)
	}
	// Then the walk continues to the head — the dump comparison is an
	// upgraded store against a fresh one, so both must stand at the same
	// last file (empty today; the half exists for the next rebuild).
	applyMigrations(t, g, after)
	if upgradedDump := schemaDump(t, g); upgradedDump != freshDump {
		t.Errorf("the upgraded schema drifted from the fresh one:\n--- fresh ---\n%s\n--- upgraded ---\n%s", freshDump, upgradedDump)
	}
}
