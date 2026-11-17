package store

// S50 T7 (close-out, P9) — the slice's upgrade drill, kept as a test
// rather than a one-off so the next slice can re-run the promise instead
// of re-making it: open a database at the PRE-S50 vintage, stage the rows
// S50's migrations rebuild or extend (roadmap items carrying epics and
// estimates, proposals in every kind the pre-S50 vintage admitted), record
// a checksum of the data, migrate the file forward through S50's own two
// migrations, and demand the DATA unchanged — row counts, contents hash —
// and the SHAPE equal to a fresh database. This is the mechanical half of
// "an upgrade from before S50 never rewrites anyone's roadmap or desk".

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// s50FirstMigrationIndex is the index of the slice's FIRST migration —
// everything below it is the pre-S50 vintage the drill upgrades FROM.
func s50FirstMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_roadmap_change") {
			return i
		}
	}
	panic("the roadmap_change migration is missing from Migrations()")
}

// dataChecksum hashes the full contents of the two tables the drill
// promises to preserve, in id order, in one canonical spelling.
func dataChecksum(t *testing.T, s *Store) string {
	t.Helper()
	h := sha256.New()
	for _, q := range []string{
		`SELECT id, component_root, title, epic, priority_state, priority_label,
		        estimate_min, estimate_max, estimate_basis, status, provenance,
		        brief_path, plan_path, loop_slug, created_at, updated_at
		   FROM roadmap_item ORDER BY id`,
		`SELECT id, kind, payload, status, created_by_kind, created_by_id,
		        decided_by_kind, decided_by_id, decide_reason, created_at, decided_at
		   FROM proposal ORDER BY id`,
	} {
		rows, err := s.GormDB().Raw(q).Rows()
		if err != nil {
			t.Fatalf("checksum query: %v", err)
		}
		cols, _ := rows.Columns()
		fmt.Fprintf(h, "%s\n", q)
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("checksum scan: %v", err)
			}
			for i, v := range vals {
				fmt.Fprintf(h, "%d:%v|", i, v)
			}
			fmt.Fprintln(h)
		}
		rows.Close()
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestS50CloseoutUpgradePreservesRoadmapRowsAndDesk(t *testing.T) {
	idx := s50FirstMigrationIndex()
	all := Migrations()

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	applyMigrations(t, old.GormDB(), all[:idx])
	seedRoadmapComponent(t, old, "before", "/srv/before")
	epic, label, basis := "S26", "P1", "two doors and a render"
	lo, hi := 2, 5
	if _, err := old.CreateRoadmapItem(NewRoadmapItemRow{
		ComponentRoot: "/srv/before", Title: "an item from before the slice",
		Epic: &epic, PriorityState: "known", PriorityLabel: &label,
		EstimateMin: &lo, EstimateMax: &hi, EstimateBasis: &basis,
		Provenance: "authored",
	}); err != nil {
		t.Fatalf("seed the epic'd item: %v", err)
	}
	if _, err := old.CreateRoadmapItem(NewRoadmapItemRow{
		ComponentRoot: "/srv/before", Title: "an unknown-priority item", Provenance: "triaged",
	}); err != nil {
		t.Fatalf("seed the unknown item: %v", err)
	}
	// Every kind the pre-S50 vintage admitted — the fourteen the T5 test
	// staged, minus none, none added: the CHECK at that vintage refuses a
	// fifteenth, which is exactly what the drill upgrades FROM.
	for _, kind := range []string{
		"policy_add", "policy_remove", "seed_corpus", "mcp_server_add",
		"mcp_server_remove", "agent_import", "autonomy_revoke", "autonomy_deny",
		"mcp_grant_revoke", "verify_command", "merge_rule", "blueprint_save",
		"environment_restrict", "automation_disable",
	} {
		if _, cerr := old.CreateProposal(kind, `{"drill":true}`, Actor{Kind: "agent", ID: "drill"}, "2026-11-04T00:00:00Z"); cerr != nil {
			t.Fatalf("stage the pre-S50 %s proposal: %v", kind, cerr)
		}
	}
	before := dataChecksum(t, old)
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	s, err := Open(Options{Dir: dir, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("migrating open through S50: %v", err)
	}
	defer func() { _ = s.Close() }()
	after := dataChecksum(t, s)
	if before != after {
		t.Fatalf("S50's migrations MOVED data across the vintage boundary:\n before %s\n after  %s", before, after)
	}
	// Fresh and upgraded agree on shape, the T5 way:
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
}
