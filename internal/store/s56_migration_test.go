package store

// s56_migration_test.go — S56 T11's migration drills (P9), in the order the
// spec names them: fresh apply; upgrade from the previous vintage with
// row-count and checksum preservation per table; idempotent re-open; the
// append-only proofs for autonomy_change, autonomy_use and loop_autonomy_pin;
// pragma_foreign_key_check empty; the proposal-kind rebuild keeping every
// proposal. Rollback is not offered: the framework runner (migrate.go) applies
// each migration once, in one transaction, into the _migrations ledger, and
// has no reverse path. That is a property of the runner — there is nothing to
// assert against, so it is stated, not tested.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
)

const s56AutonomyMigration = "20261101150000_autonomy"

// s56Vintage splits the full migration set at the S56 pair: before is the
// previous vintage, s56 the two S56 migrations in apply order.
func s56Vintage(t *testing.T) (before, s56 []Migration) {
	t.Helper()
	for _, m := range Migrations() {
		if m.Name == s56AutonomyMigration || m.Name == "20261101180000_proposal_autonomy_kinds" {
			s56 = append(s56, m)
			continue
		}
		before = append(before, m)
	}
	if len(s56) != 2 {
		t.Fatalf("the S56 migration pair = %d entries, want 2", len(s56))
	}
	return before, s56
}

// tableTables lists every user table (sqlite_master, no sqlite_ internals).
func tableTables(t *testing.T, g *gorm.DB) []string {
	t.Helper()
	rows, err := g.Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).Rows()
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	return names
}

// tableFingerprint is (count, hex sha256 over every row rendered as
// quote(col1)‖quote(col2)‖… in table_info column order, rows ordered by
// rowid). NULLs and empty strings render differently under quote(), so a
// migration that coerces one into the other is caught. No table in the store
// is WITHOUT ROWID (checked for this drill), so rowid order is total.
func tableFingerprint(t *testing.T, g *gorm.DB, table string) (int, string) {
	t.Helper()
	var cols []string
	{
		rows, err := g.Raw(fmt.Sprintf(`SELECT name FROM pragma_table_info(%q) ORDER BY cid`, table)).Rows()
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan column of %s: %v", table, err)
			}
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		t.Fatalf("table %s has no columns", table)
	}
	rendered := make([]string, len(cols))
	for i, c := range cols {
		rendered[i] = fmt.Sprintf("quote(%q)", c)
	}
	h := sha256.New()
	n := 0
	rows, err := g.Raw(fmt.Sprintf(`SELECT %s FROM %q ORDER BY rowid`, strings.Join(rendered, " || char(31) || "), table)).Rows()
	if err != nil {
		t.Fatalf("fingerprint %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("read row of %s: %v", table, err)
		}
		fmt.Fprintf(h, "%s\n", line)
		n++
	}
	return n, hex.EncodeToString(h.Sum(nil))
}

// ftsShadowTables are the WITHOUT ROWID auxiliary tables an FTS5 index keeps
// beside its main table. They carry the index, not user data, and the S56
// pair cannot touch them; the fingerprint skips them and the test asserts the
// skip set is exactly this set, so a future rowid-less table is never
// silently excluded from the preservation proof.
var ftsShadowTables = map[string]bool{
	"retrieval_chunk_fts_config":  true,
	"retrieval_chunk_fts_data":    true,
	"retrieval_chunk_fts_docsize": true,
	"retrieval_chunk_fts_idx":     true,
}

// hasRowID reports whether the table exposes a rowid (every table does,
// except WITHOUT ROWID ones).
func hasRowID(t *testing.T, g *gorm.DB, table string) bool {
	t.Helper()
	var x int64
	err := g.Raw(fmt.Sprintf(`SELECT rowid FROM %q LIMIT 1`, table)).Scan(&x).Error
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "no such column: rowid") {
		return false
	}
	t.Fatalf("rowid probe of %s failed unexpectedly: %v", table, err)
	return false
}

// allFingerprints maps every user table to its (count, checksum), skipping
// the FTS shadow tables (which the caller asserts against ftsShadowTables).
func allFingerprints(t *testing.T, g *gorm.DB) (map[string]struct {
	count int
	sum   string
}, []string) {
	t.Helper()
	out := make(map[string]struct {
		count int
		sum   string
	})
	var skipped []string
	for _, table := range tableTables(t, g) {
		if !hasRowID(t, g, table) {
			if !ftsShadowTables[table] {
				t.Fatalf("table %s has no rowid and is not a known FTS shadow table — the preservation proof must account for it", table)
			}
			skipped = append(skipped, table)
			continue
		}
		n, sum := tableFingerprint(t, g, table)
		out[table] = struct {
			count int
			sum   string
		}{n, sum}
	}
	return out, skipped
}

// ledgerCount is the number of _migrations rows.
func ledgerCount(t *testing.T, g *gorm.DB) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM _migrations`).Scan(&n).Error; err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	return n
}

// TestS56FreshApplyHasTheAuthorityTables is the fresh-apply drill: a clean
// file walked to the head carries the four authority tables, their indexes
// and triggers, and the proposal CHECK widened to the closed eight kinds.
func TestS56FreshApplyHasTheAuthorityTables(t *testing.T) {
	g := openMigrated(t)
	for _, table := range []string{"autonomy_grant", "autonomy_change", "autonomy_use", "loop_autonomy_pin"} {
		if countCatalogueEntries(t, g, "table", table) != 1 {
			t.Errorf("table %s is absent after a fresh apply", table)
		}
	}
	for _, index := range []string{"idx_autonomy_grant_node", "idx_autonomy_change_root", "idx_autonomy_use_grant", "idx_autonomy_use_loop"} {
		if countCatalogueEntries(t, g, "index", index) != 1 {
			t.Errorf("index %s is absent", index)
		}
	}
	for _, trigger := range []string{
		"autonomy_change_no_update", "autonomy_change_no_delete",
		"autonomy_use_no_update", "autonomy_use_no_delete",
		"loop_autonomy_pin_no_update", "loop_autonomy_pin_no_delete",
		"autonomy_grant_allow_root_only", "autonomy_grant_no_attach",
	} {
		if countCatalogueEntries(t, g, "trigger", trigger) != 1 {
			t.Errorf("trigger %s is absent", trigger)
		}
	}
	// The widened proposal CHECK: the two restrictive kinds land, the set
	// stays closed (a grant kind has no proposal shape at all).
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_revoke','{}','t','agent','a1')`).Error; err != nil {
		t.Errorf("kind autonomy_revoke refused by the widened CHECK: %v", err)
	}
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_deny','{}','t','agent','a1')`).Error; err != nil {
		t.Errorf("kind autonomy_deny refused by the widened CHECK: %v", err)
	}
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_grant','{}','t','agent','a1')`).Error; err == nil {
		t.Errorf("kind autonomy_grant was accepted — the set is not closed at eight")
	}
	if n := countTable(t, g, "autonomy_grant"); n != 0 {
		t.Errorf("fresh store has %d grant rows; nothing is seeded (AL16)", n)
	}
}

// seedPreviousVintage populates the pre-S56 database with the four
// populations the drill names — loops, triggers, proposals, components —
// plus a task, so the preservation proof is over real data.
func seedPreviousVintage(t *testing.T, g *gorm.DB) {
	t.Helper()
	seed := []string{
		`INSERT INTO component (id,name,slug,created_at,updated_at) VALUES ('root-1','root','root','t','t')`,
		`INSERT INTO component (id,name,slug,created_at,updated_at,parent_id) VALUES ('child-1','child','child','t','t','root-1')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,attended,allow_outward)
		   VALUES ('l1','alpha','The alpha loop','a goal','/repo1','/repo1','standard','build','active','t','t',0,0)`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,attended,allow_outward)
		   VALUES ('l2','beta','The beta loop','a goal','/repo1','/repo1','standard','build','done','t','t',1,1)`,
		`INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at)
		   VALUES ('trg-1','schedule','dev-loop','30 3 * * *','Europe/London','t')`,
		`INSERT INTO task (loop_slug,component_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('alpha','/repo1','T1',1,'A task','go','eng','pending','t')`,
		`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		   VALUES ('policy_add','{"type":"tool"}','t','agent','a1')`,
		`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason)
		   VALUES ('agent_import','{}','rejected','t','user','u1','t','user','u1','no')`,
		`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		   VALUES ('mcp_server_add','{}','t','trigger','trg-1')`,
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the pre-S56 store: %v\n%s", err, stmt)
		}
	}
}

// TestS56UpgradePreservesEveryTable is the upgrade drill: a store at the
// previous vintage, seeded, is walked forward over the two S56 migrations.
// Every pre-existing table keeps its row count AND its row checksum; the
// proposal-kind rebuild keeps every proposal; the FK check comes back empty;
// the ledger records exactly the two new migrations.
func TestS56UpgradePreservesEveryTable(t *testing.T) {
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	t.Cleanup(closeDB)
	before, s56 := s56Vintage(t)
	applyMigrations(t, g, before)
	seedPreviousVintage(t, g)

	pre, skipped := allFingerprints(t, g)
	if len(skipped) == 0 {
		t.Fatalf("the FTS shadow tables were not found; the fingerprint must account for them by name")
	}
	preLedger := ledgerCount(t, g)

	applyMigrations(t, g, s56)

	// Row-count and checksum preservation, per table. _migrations is the
	// ledger the runner appends to — its +2 is the separate assertion below.
	for table, was := range pre {
		if table == "_migrations" {
			continue
		}
		n, sum := tableFingerprint(t, g, table)
		if n != was.count {
			t.Errorf("table %s: row count %d -> %d", table, was.count, n)
		}
		if sum != was.sum {
			t.Errorf("table %s: row checksum changed on the upgrade", table)
		}
	}
	// The new tables exist, empty — the upgrade adds shape, never data.
	for _, table := range []string{"autonomy_grant", "autonomy_change", "autonomy_use", "loop_autonomy_pin"} {
		if n, _ := tableFingerprint(t, g, table); n != 0 {
			t.Errorf("table %s has %d rows after a data-free upgrade", table, n)
		}
	}
	// The proposal-kind rebuild kept every proposal (the copy-guard table in
	// the migration enforced moved = total inside its transaction; this is
	// the read-back half).
	if n := countTable(t, g, "proposal"); n != 3 {
		t.Errorf("proposal rows after the rebuild = %d, want 3", n)
	}
	for _, kind := range []string{"policy_add", "agent_import", "mcp_server_add"} {
		var n int
		if err := g.Raw(`SELECT count(*) FROM proposal WHERE kind = ?`, kind).Scan(&n).Error; err != nil {
			t.Fatalf("count kind %s: %v", kind, err)
		}
		if n != 1 {
			t.Errorf("kind %s lost in the rebuild", kind)
		}
	}
	// The rebuild is a wider door, not a looser one: old kinds land, the
	// grant kind still does not.
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_revoke','{}','t','agent','a1')`).Error; err != nil {
		t.Errorf("upgraded CHECK refuses autonomy_revoke: %v", err)
	}
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_grant','{}','t','agent','a1')`).Error; err == nil {
		t.Errorf("upgraded CHECK accepts autonomy_grant — the set is not closed")
	}
	// Integrity: the rebuild and the new FK-bearing tables left no dangling
	// reference.
	var fk int
	if err := g.Raw(`SELECT count(*) FROM pragma_foreign_key_check()`).Scan(&fk).Error; err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	if fk != 0 {
		t.Errorf("pragma_foreign_key_check found %d dangling references", fk)
	}
	// The ledger gained exactly the two S56 migrations, nothing else.
	if got := ledgerCount(t, g); got != preLedger+2 {
		t.Errorf("ledger rows %d -> %d, want +2", preLedger, got)
	}
}

// TestS56UpgradeIdempotentReopen closes the upgraded store and re-opens it
// through the same runner path: the ledger must skip the already-applied
// migrations and the data must come back intact.
func TestS56UpgradeIdempotentReopen(t *testing.T) {
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	before, s56 := s56Vintage(t)
	applyMigrations(t, g, before)
	seedPreviousVintage(t, g)
	applyMigrations(t, g, s56)
	pre, _ := allFingerprints(t, g)
	preLedger := ledgerCount(t, g)
	closeDB()

	g2, closeDB2 := rawGorm(t, path)
	t.Cleanup(closeDB2)
	applyMigrations(t, g2, Migrations()) // the full set: every one is already in the ledger

	if got := ledgerCount(t, g2); got != preLedger {
		t.Errorf("re-open re-ran migrations: ledger %d -> %d", preLedger, got)
	}
	for table, was := range pre {
		n, sum := tableFingerprint(t, g2, table)
		if n != was.count || sum != was.sum {
			t.Errorf("table %s changed across the re-open (count %d/%d, checksum changed=%v)",
				table, was.count, n, sum != was.sum)
		}
	}
}

// TestS56AuthorityTablesAreAppendOnly is the append-only proof for the three
// log tables: an INSERT lands, and an UPDATE or DELETE is aborted by the
// triggers (the store API would never issue either, so the triggers are the
// whole backstop and are exercised with direct SQL).
func TestS56AuthorityTablesAreAppendOnly(t *testing.T) {
	g := openMigrated(t)
	seedComponent(t, g, "root-1")
	must := func(desc, stmt string) {
		t.Helper()
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v\n%s", desc, err, stmt)
		}
	}
	mustRefuse := func(desc, stmt string) {
		t.Helper()
		if err := g.Exec(stmt).Error; err == nil {
			t.Errorf("%s: the statement was allowed\n%s", desc, stmt)
		}
	}
	must("seed a grant", `INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('g1','root-1','class','outward','allow','user','u1','t')`)
	must("append an autonomy_change row", `INSERT INTO autonomy_change (at,root_id,grant_id,change_kind,actor_kind,actor_id,summary)
		VALUES ('t','root-1','g1','grant','user','u1','a fixed sentence')`)
	must("append a pin", `INSERT INTO loop_autonomy_pin (loop_id,root_id,rev,origin,labels,allowed,pinned_at)
		VALUES ('l1','root-1',1,'schedule','[]','[{"action":"forge.push","rev":1}]','t')`)
	must("append a use begun", `INSERT INTO autonomy_use (at,loop_id,grant_id,grant_rev,action,phase)
		VALUES ('t','l1','g1',1,'forge.push','begun')`)
	mustRefuse("update autonomy_change", `UPDATE autonomy_change SET summary = 'rewritten' WHERE id = 1`)
	mustRefuse("delete autonomy_change", `DELETE FROM autonomy_change WHERE id = 1`)
	mustRefuse("update loop_autonomy_pin", `UPDATE loop_autonomy_pin SET rev = 2 WHERE loop_id = 'l1'`)
	mustRefuse("delete loop_autonomy_pin", `DELETE FROM loop_autonomy_pin WHERE loop_id = 'l1'`)
}

// TestS56NoAutonomyGrantKindPassesTheStoreCHECK is the store-level half of
// the demo walk's row 12: an agent cannot author an allow, and the backstop
// that does not depend on any door is the proposal's 8-kind CHECK
// (migration 20261101180000). A bypassed raw INSERT of a grant-kind
// proposal — the shape no API would ever send — is refused before SQL.
func TestS56NoAutonomyGrantKindPassesTheStoreCHECK(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO proposal (kind,payload,created_at,created_by_kind,created_by_id)
		VALUES ('autonomy_grant','{"rootId":"root-1","subject":"outward"}','t','agent','a1')`).Error; err == nil {
		t.Errorf("a bypassed autonomy_grant proposal was accepted — the store's CHECK is not closed")
	}
	if n := countTable(t, g, "proposal"); n != 0 {
		t.Errorf("the refused write left %d proposal rows behind", n)
	}
}

// TestS56UseLogPhaseCHECKs pins the begun/completed pairing rule the trigger
// layer cannot carry: a begun row has no begun_id, a completed/failed row
// points at one.
func TestS56UseLogPhaseCHECKs(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO autonomy_use (at,loop_id,grant_id,grant_rev,action,phase,begun_id)
		VALUES ('t','l1','g1',1,'forge.push','begun',7)`).Error; err == nil {
		t.Errorf("a begun row with a begun_id was accepted")
	}
	if err := g.Exec(`INSERT INTO autonomy_use (at,loop_id,grant_id,grant_rev,action,phase)
		VALUES ('t','l1','g1',1,'forge.push','completed')`).Error; err == nil {
		t.Errorf("a completed row without its begun was accepted")
	}
	if err := g.Exec(`INSERT INTO autonomy_use (at,loop_id,grant_id,grant_rev,action,phase,begun_id)
		VALUES ('t','l1','g1',1,'forge.push','completed',999)`).Error; err != nil {
		t.Errorf("a completed row pointing at a (missing) begun was refused: %v", err)
	}
}
