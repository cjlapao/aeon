package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// S46 T12 — the close-out migration drills (brief §5 criteria 1–2 & 11, T12
// step 2). The two S46 migrations — 20261101131955_mcp_tables (six tables,
// two indexes, two append-only triggers) and 20261101193000_proposal_mcp_grant_revoke
// (the proposal CHECK rebuild widened by mcp_grant_revoke) — are pinned here
// as DRILLS: a fresh apply builds everything and the rebuilt CHECK accepts
// the new kind and refuses an unknown one; an upgrade from the vintage just
// BEFORE S46's first migration (a store with S43-family agents, proposals of
// every kind valid at that vintage, policies and a loop) carries every row
// through untouched — counts AND data digests, the proposal table included,
// although 193000 rebuilds it (its copy moves every row; the explicit ids
// keep the digest stable under ORDER BY rowid, which IS ORDER BY id for an
// INTEGER PRIMARY KEY). Unrelated tables (policy, loop, agent) are unchanged;
// the ledger is the only thing allowed to grow. Rollback is NOT offered
// (forward-only, stated): a Migration carries one forward SQL string and no
// down half.

// s46NewTables are the six tables S46's first migration adds.
var s46NewTables = []string{
	"mcp_server", "mcp_server_tool", "mcp_agent", "mcp_declare", "mcp_import", "mcp_change",
}

// s46FirstMigration names S46's first migration BY NAME so the drill's
// premise — "a vintage parked just before S46" — holds independently of what
// lands after S46 (the S55 drill's lesson, applied here on day one).
const s46FirstMigration = "20261101131955_mcp_tables"

// TestS46FreshApply is drill 1: a new store applies everything; the six MCP
// tables, the two indexes and the two append-only triggers exist; the
// rebuilt proposal CHECK accepts mcp_grant_revoke and refuses an unknown
// kind; no foreign key is violated; a re-open is idempotent.
func TestS46FreshApply(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Fatalf("fresh apply recorded %d migrations, want %d", len(applied), len(Migrations()))
	}
	for _, table := range s46NewTables {
		countTable(t, s.GormDB(), table) // FATALs if absent
	}
	for _, idx := range []string{"idx_mcp_server_agent", "idx_mcp_change_server"} {
		if n := countCatalogueEntries(t, s.GormDB(), "index", idx); n != 1 {
			t.Fatalf("index %s: %d entries, want 1", idx, n)
		}
	}
	for _, trg := range []string{"mcp_change_no_update", "mcp_change_no_delete"} {
		if n := countCatalogueEntries(t, s.GormDB(), "trigger", trg); n != 1 {
			t.Fatalf("trigger %s: %d entries, want 1", trg, n)
		}
	}
	// The rebuilt proposal CHECK: the new kind is admitted…
	if _, err := s.w.Exec(`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id)
		VALUES (900001,'mcp_grant_revoke','{"agent":"a","qualified":"s.t"}','proposed','2026-11-02T00:00:00Z','agent','engineer')`); err != nil {
		t.Fatalf("the rebuilt CHECK refuses mcp_grant_revoke: %v", err)
	}
	// …and an unknown kind is refused.
	if _, err := s.w.Exec(`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id)
		VALUES (900002,'bogus_kind','{}','proposed','2026-11-02T00:00:00Z','user','u')`); err == nil {
		t.Fatal("the rebuilt CHECK admitted an unknown kind")
	} else if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("unknown-kind insert failed for the wrong reason: %v", err)
	}
	if n := s55ForeignKeyViolations(t, s.GormDB()); n != 0 {
		t.Fatalf("pragma_foreign_key_check found %d violations after fresh apply", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer s2.Close()
	applied2, err := s2.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied2) != len(Migrations()) {
		t.Fatalf("re-open recorded %d migrations, want %d (idempotent)", len(applied2), len(Migrations()))
	}
}

// TestS46UpgradeFromThePreS46Vintage is drill 2: a database parked at the
// vintage just before S46's first migration, seeded above, is opened by the
// current code; every pre-existing table — proposal included — comes through
// with identical count AND digest; the six MCP tables arrive empty (an
// upgrade adopts nothing without a legacy file); FK check clean; re-open
// idempotent.
func TestS46UpgradeFromThePreS46Vintage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	full := Migrations()
	cut := len(full)
	for i, m := range full {
		if m.Name == s46FirstMigration {
			cut = i
			break
		}
	}
	if cut == len(full) {
		t.Fatalf("%s is not in Migrations() — the drill's premise is broken", s46FirstMigration)
	}
	applyMigrations(t, g, full[:cut])

	seed := []string{
		`INSERT INTO agent (slug,name,created_at,updated_at) VALUES ('tester','Tester','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
		`INSERT INTO agent (slug,name,created_at,updated_at) VALUES ('golang-engineer','Golang Engineer','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
		`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id)
			VALUES (1,'policy_add','{"rule":{"type":"tool_access"}}','proposed','2026-11-01T00:00:00Z','agent','golang-engineer')`,
		`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id)
			VALUES (2,'policy_remove','{"policyID":"pol.1"}','committed','2026-11-01T00:00:00Z','agent','golang-engineer','2026-11-01T01:00:00Z','user','operator')`,
		`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason)
			VALUES (3,'seed_corpus','{"agents":["tester"]}','rejected','2026-11-01T00:00:00Z','user','operator','2026-11-01T01:00:00Z','user','operator','scope creep')`,
		`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id)
			VALUES (4,'agent_import','{"pkgPath":"/tmp/pkg"}','proposed','2026-11-01T00:00:00Z','agent','golang-engineer')`,
		`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
			VALUES ('pol.drill','command_access','agent','tester','allow','{"prefix":"git push"}','operator',NULL,'2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
			VALUES ('l-drill','s46-drill','t','g','/r/s46-drill','/r/s46-drill','standard','preflight','waiting','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','person','')`,
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the parked vintage: %v\n%s", err, stmt)
		}
	}

	// Snapshot every table's count and data digest AT THIS VINTAGE.
	var tables []string
	if err := g.Raw(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).Scan(&tables).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}
	type snap struct {
		count  int
		digest uint64
	}
	before := map[string]snap{}
	for _, table := range tables {
		if table == "_migrations" {
			continue // the ledger legitimately grows: it records this upgrade
		}
		n, d := s55TableDigest(t, g, table, s55Columns(t, g, table))
		before[table] = snap{n, d}
	}
	if len(before) < 30 {
		t.Fatalf("the parked vintage only has %d tables — too few to prove the upgrade preserved anything", len(before))
	}
	if p := before["proposal"]; p.count != 4 {
		t.Fatalf("the parked vintage seeded %d proposals, want 4 — the drill proves nothing", p.count)
	}

	// The upgrade: the current code opens the same file and applies the rest.
	s, err := Open(Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the parked database: %v", err)
	}
	defer s.Close()
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(full) {
		t.Fatalf("ledger holds %d after the upgrade, want %d", len(applied), len(full))
	}
	for table, b := range before {
		n, d := s55TableDigest(t, s.GormDB(), table, s55Columns(t, s.GormDB(), table))
		if n != b.count || d != b.digest {
			t.Fatalf("table %s changed across the upgrade: %d rows digest %d before, %d rows digest %d after",
				table, b.count, b.digest, n, d)
		}
	}
	// The MCP tables arrive EMPTY: an upgrade adopts nothing — adoption needs
	// the legacy file and its own door (T4), never a migration side effect.
	for _, table := range s46NewTables {
		if n := countTable(t, s.GormDB(), table); n != 0 {
			t.Fatalf("mcp table %s holds %d rows after an upgrade with no legacy file — migrations adopt NOTHING", table, n)
		}
	}
	if n := s55ForeignKeyViolations(t, s.GormDB()); n != 0 {
		t.Fatalf("pragma_foreign_key_check found %d violations after the upgrade", n)
	}
}

// TestS46ForwardOnly states (the drill's last half): the runner offers no
// down half anywhere — a Migration is one forward SQL string. The append-only
// half of drill 5 is already pinned positively by T1's
// TestMcpChangeIsAppendOnly (UPDATE and DELETE both fail at the triggers);
// this test pins the FORWARD-ONLY claim itself.
func TestS46ForwardOnly(t *testing.T) {
	for _, m := range Migrations() {
		if strings.Contains(strings.ToLower(m.SQL), "drop table if exists mcp_") {
			t.Fatalf("migration %s drops an MCP table — the slice is FORWARD-ONLY, no rollback may hide in a migration", m.Name)
		}
	}
}
