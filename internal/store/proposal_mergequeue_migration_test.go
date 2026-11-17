package store

// proposal_mergequeue_migration_test.go — S60 T9's migration drill, the
// M2-name shape S57 set (P9: fresh vs upgraded, identical): the seventeenth
// proposal kind arrived as a table REBUILD — SQLite cannot widen a CHECK in
// place — and the only proof that matters is that an upgraded store carries
// the byte-identical schema of a fresh one AND every pre-existing proposal
// row, byte for byte, through the DROP/RENAME. The copy guard inside the
// migration asserts totality at apply time on every machine; this test
// asserts it here with SEEDED rows (the copy guard would pass vacuously on
// an empty table, and an empty-table pass is exactly the hole drills exist
// for), and the CHECK's teeth are tasted from both sides: the new kind
// lands after the upgrade, and the five forbidden kind names still die.

import (
	"strings"
	"testing"
)

const s60ProposalKindMigration = "20261104193000_proposal_kind_mergequeue"

// s60ProposalKindVintage splits the full migration set at S60 T9's file
// three ways — strictly-earlier (the 93-entry vintage a store upgrades
// FROM), mine, strictly-later. The third half arrived with S61 T5: the
// proposal CHECK is widened by more than one file now, and a two-way split
// applied a LATER kind rebuild BEFORE mine, letting the two rebuilds
// clobber each other's CHECK (measured: fresh at 19 kinds against an
// upgraded store stuck at 17). The drill still tastes its OWN file; the
// dump comparison then walks to the head so both stores stand at the same
// last file.
func s60ProposalKindVintage(t *testing.T) (before, mine, after []Migration) {
	t.Helper()
	for _, m := range Migrations() {
		switch {
		case m.Name == s60ProposalKindMigration:
			mine = append(mine, m)
		case m.Name < s60ProposalKindMigration:
			before = append(before, m)
		default:
			after = append(after, m)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("the S60 T9 migration set = %d entries, want 1", len(mine))
	}
	return before, mine, after
}

func TestProposalKindMergeQueueRebuildKeepsRows(t *testing.T) {
	fresh := openMigrated(t)
	freshDump := schemaDump(t, fresh)

	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	before, mine, after := s60ProposalKindVintage(t)
	applyMigrations(t, g, before)

	seed := []struct {
		kind, payload, status, byKind, byID string
	}{
		{"learning_retract", `{"id":7,"reason":"it was wrong, and it knew it"}`, "proposed", "agent", "implementer"},
		{"roadmap_idea", `{"componentRoot":"/srv/a","title":"quote\" this, \" and that", "epic":"x, y"}`, "proposed", "agent", "planner"},
		{"environment_restrict", `{"environmentId":"env-1","spec":"PATH","baseRevision":3}`, "committed", "user", "cjlapao"},
	}
	for i, s := range seed {
		if err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
			VALUES (?, ?, ?, '2026-11-04T12:00:0' || ? || 'Z', ?, ?)`,
			s.kind, s.payload, s.status, i, s.byKind, s.byID).Error; err != nil {
			t.Fatalf("seed proposal %d: %v", i, err)
		}
	}
	pre, preSum := tableFingerprint(t, g, "proposal")
	preLedger := ledgerCount(t, g)

	applyMigrations(t, g, mine)

	// The whole point: the three seeded kinds (the ones a store in the
	// field actually holds, one decided, one with comma/quote-laced payload
	// — quoting is where copy scripts die) all survived the DROP/RENAME byte for byte.
	post, postSum := tableFingerprint(t, g, "proposal")
	if post != pre || postSum != preSum {
		t.Errorf("the proposal table changed across the kind rebuild: (%d,%s) -> (%d,%s)", pre, preSum, post, postSum)
	}
	// The transient guards did their jobs and left no furniture behind.
	for _, guard := range []string{"proposal_20261104193000_copy_guard", "proposal_20261104193000_fk_guard"} {
		if countCatalogueEntries(t, g, "table", guard) != 0 {
			t.Errorf("the transient guard table %s survived the migration", guard)
		}
	}
	// The CHECK from both sides: the ask kind now lands…
	if err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
		VALUES ('mergequeue_enqueue', '{"componentRoot":"/srv/a","branch":"feature/x","reason":"green"}',
		        'proposed', '2026-11-04T13:00:00Z', 'agent', 'implementer')`).Error; err != nil {
		t.Errorf("the seventeenth kind is refused after the upgrade: %v", err)
	}
	// …and the five names that must STAY unnameable still die at the CHECK.
	for _, kind := range []string{"mergequeue_land", "mergequeue_retry", "mergequeue_reorder", "mergequeue_dequeue_others", "mergequeue_settings"} {
		err := g.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
			VALUES (?, '{}', 'proposed', '2026-11-04T13:00:00Z', 'agent', 'implementer')`, kind).Error
		if err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("kind %q was %v after the rebuild — the widened CHECK let a decision kind in", kind, err)
		}
	}
	if got := ledgerCount(t, g); got != preLedger+1 {
		t.Errorf("ledger rows %d -> %d, want +1", preLedger, got)
	}
	// Walk to the head before comparing dumps — fresh and upgraded must
	// stand at the same last file (S61 T5).
	applyMigrations(t, g, after)
	if upgradedDump := schemaDump(t, g); upgradedDump != freshDump {
		t.Errorf("the upgraded schema drifted from the fresh one:\n--- fresh ---\n%s\n--- upgraded ---\n%s", freshDump, upgradedDump)
	}
}
