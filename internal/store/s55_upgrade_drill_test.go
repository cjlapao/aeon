package store

import (
	"hash/fnv"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// S55 T12 — the close-out migration drills (brief.md §4.10, T12 step 2).
//
// The three S55 migrations (20261101100001/02/03) add SIX new tables
// (loop_control_event, loop_control_request, budget, budget_event,
// watchdog_rule, watchdog_event) and backfill one migrated budget row per
// epic that has a budget_usd. Nothing is rebuilt and no pre-existing table is
// altered, so an upgrade from the previous vintage must carry every existing
// row through untouched. These drills pin that with row counts AND per-table
// data digests (a count alone would let a migration that rewrites a column
// pass), plus the backfill's moved = total guard, the append-only triggers,
// pragma_foreign_key_check, and an idempotent re-open. Rollback is NOT
// offered (stated, per T12 step 2): a Migration carries one forward SQL
// string and no down half — the only DROP in the three files is the budget
// migration's own zz_s55_budget_guard scratch table, created and dropped
// inside the same migration.

// s55NewTables are the six tables the three S55 migrations add.
var s55NewTables = []string{
	"loop_control_event", "loop_control_request",
	"budget", "budget_event",
	"watchdog_rule", "watchdog_event",
}

// s55Tables is a table's row count plus a digest over its data: FNV-64a of
// every row rendered as `rowid|col1|col2…` ordered by rowid, with the column
// list measured AT DIGEST TIME by the caller (the caller snapshots the
// pre-upgrade columns so a column added by a later migration cannot change a
// pre-existing row's rendering). An empty table digests to (0, 0).
func s55TableDigest(t *testing.T, g *gorm.DB, table string, cols []string) (int, uint64) {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM ` + quoteS55(table)).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if n == 0 {
		return 0, 0
	}
	if !s55HasRowid(t, g, table) {
		return n, 0 // count only — no rowid to render
	}
	parts := make([]string, 0, len(cols)+1)
	parts = append(parts, "rowid")
	for _, c := range cols {
		parts = append(parts, "COALESCE(CAST("+quoteS55(c)+` AS TEXT), '')`)
	}
	q := "SELECT " + strings.Join(parts, " || '|' || ") + " FROM " + quoteS55(table) + " ORDER BY rowid"
	var rows []string
	if err := g.Raw(q).Scan(&rows).Error; err != nil {
		t.Fatalf("digest %s: %v", table, err)
	}
	h := fnv.New64a()
	for _, r := range rows {
		_, _ = h.Write([]byte(r))
		_, _ = h.Write([]byte{'\n'})
	}
	return n, h.Sum64()
}

func quoteS55(ident string) string { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }

// s55ForeignKeyViolations is the drill's read of pragma_foreign_key_check
// (the same query the tree's other schema tests run).
func s55ForeignKeyViolations(t *testing.T, g *gorm.DB) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n).Error; err != nil {
		t.Fatalf("pragma_foreign_key_check: %v", err)
	}
	return n
}

// s55HasRowid probes whether the table's rows can be rendered into a digest.
// VIRTUAL TABLES (an FTS5 index, the sqlite-vec table) and WITHOUT ROWID
// tables (the FTS5 _config shadow) have no rowid. The DATA they index or
// describe lives in real tables (which do get full digests), so a count is
// the honest check for them.
func s55HasRowid(t *testing.T, g *gorm.DB, table string) bool {
	t.Helper()
	var one int64
	err := g.Raw("SELECT rowid FROM " + quoteS55(table) + " LIMIT 1").Scan(&one).Error
	return err == nil
}

// s55Columns returns a table's live column names, in schema order.
func s55Columns(t *testing.T, g *gorm.DB, table string) []string {
	t.Helper()
	var info []struct {
		Cid  int
		Name string
	}
	if err := g.Raw("PRAGMA table_info(" + quoteS55(table) + ")").Scan(&info).Error; err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	cols := make([]string, 0, len(info))
	for _, c := range info {
		cols = append(cols, c.Name)
	}
	return cols
}

// s55SeedVintage plants rows in the pre-existing families the S55 code reads:
// one root component, two epics (one funded — the backfill's source — one not),
// one goal on the funded epic, and one parked loop with its pin and a done
// step. Every INSERT is one the tree's own tests already use at this vintage.
func s55SeedVintage(t *testing.T, g *gorm.DB) {
	t.Helper()
	seedComponent(t, g, "cmp-s55")
	if err := g.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,budget_usd,created_at,updated_at)
		VALUES ('epc-s55-funded','cmp-s55','funded','o','a',40,'2026-11-01T09:00:00Z','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed funded epic: %v", err)
	}
	if err := g.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,budget_usd,created_at,updated_at)
		VALUES ('epc-s55-unfunded','cmp-s55','unfunded','o','a',NULL,'2026-11-01T09:00:00Z','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed unfunded epic: %v", err)
	}
	if err := g.Exec(`INSERT INTO goal (id,component_id,epic_id,title,type,source_kind,created_by_kind,created_by_id,created_at,updated_at)
		VALUES ('g-s55','cmp-s55','epc-s55-funded','goal','feature','manual','user','u','2026-11-01T09:00:00Z','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed goal: %v", err)
	}
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
		VALUES ('l-s55','s55-drill','t','g','/r/s55-drill','/r/s55-drill','standard','preflight','waiting','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','person','')`).Error; err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	if err := g.Exec(`INSERT INTO loop_blueprint (loop_id,blueprint_id,blueprint_version,content,runtime_json,updated_at)
		VALUES ('l-s55','two-step',1,'id: two-step','{"position":{"lane":""}}','2026-01-02T00:00:00Z')`).Error; err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	if err := g.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
		VALUES ('s55-drill','/r/s55-drill','build','x','done','2026-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatalf("seed step: %v", err)
	}
}

// TestS55FreshApply is the drill's fresh half: a new store applies every
// migration, the six new tables exist, no foreign key is violated, and a
// re-open is idempotent (the ledger does not grow twice).
func TestS55FreshApply(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	defer s.Close()

	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Fatalf("fresh apply recorded %d migrations, want %d", len(applied), len(Migrations()))
	}
	for _, table := range s55NewTables {
		countTable(t, s.GormDB(), table) // FATALs if the table is absent
	}
	if n := s55ForeignKeyViolations(t, s.GormDB()); n != 0 {
		t.Fatalf("pragma_foreign_key_check found %d violations after fresh apply", n)
	}
	// Idempotent re-open: the ledger grows by zero.
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

// TestS55UpgradeFromThePreviousVintage is the drill's upgrade half: a
// database parked at the vintage just before the three S55 migrations, seeded
// with rows in every family S55 reads, is opened by the current code and must
// come through with every pre-existing row untouched, the backfill applied,
// and nothing else changed.
func TestS55UpgradeFromThePreviousVintage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	full := Migrations()
	// 2026-11-02 merge reconciliation: main's S46/S56 migrations now sort
	// AFTER S55's three (mcp_tables, autonomy, the proposal kinds), so the
	// position cut full[:len(full)-3] would park the drill with S55's
	// tables already built — the premise broken. Cut before S55's first
	// migration BY NAME instead: the premise is "a vintage parked just
	// before S55", independent of what lands after S55.
	cut := len(full)
	for i, m := range full {
		if m.Name == "20261101100001_loop_control" {
			cut = i
			break
		}
	}
	if cut == len(full) {
		t.Fatal("S55's first migration (20261101100001_loop_control) is not in Migrations() — the drill's premise is broken")
	}
	applyMigrations(t, g, full[:cut]) // everything before S55's three files
	s55SeedVintage(t, g)

	// Snapshot: every pre-existing table's columns, row count and data digest,
	// measured AT THIS VINTAGE so the comparison is apples to apples.
	type snap struct{ cols []string }
	var tables []string
	if err := g.Raw(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).Scan(&tables).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}
	before := map[string]struct {
		cols   []string
		count  int
		digest uint64
	}{}
	for _, table := range tables {
		cols := s55Columns(t, g, table)
		n, d := s55TableDigest(t, g, table, cols)
		before[table] = struct {
			cols   []string
			count  int
			digest uint64
		}{cols, n, d}
	}
	if len(before) < 30 {
		t.Fatalf("the parked vintage only has %d tables — too few to prove the upgrade preserved anything", len(before))
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
		// The ledger legitimately GROWS: it records the upgrade that just
		// happened (64 -> 67 at the 2026-11-02 merge: the main side added
		// S46's mcp migration after the S55 files), it is not data the
		// upgrade preserves. The growth is asserted separately, below, as
		// applied == len(full).
		if table == "_migrations" {
			continue
		}
		n, d := s55TableDigest(t, s.GormDB(), table, b.cols)
		if n != b.count {
			t.Errorf("%s had %d rows before the upgrade and %d after", table, b.count, n)
		}
		if d != b.digest {
			t.Errorf("%s's data changed across the upgrade (digest %x -> %x) — the three S55 migrations add tables, they do not touch pre-existing rows", table, b.digest, d)
		}
	}
	for _, table := range s55NewTables {
		if _, ok := before[table]; ok {
			t.Fatalf("%s existed before the S55 migrations — the drill's premise is broken", table)
		}
		countTable(t, s.GormDB(), table) // FATALs if absent
	}
	if n := s55ForeignKeyViolations(t, s.GormDB()); n != 0 {
		t.Fatalf("pragma_foreign_key_check found %d violations after the upgrade", n)
	}
	// The backfill: the funded epic earned exactly one migrated budget row and
	// one created event; the unfunded epic earned neither.
	var funded, unfunded int
	if err := s.GormDB().Raw(`SELECT count(*) FROM budget WHERE id = 'bud-epc-s55-funded' AND scope = 'epic' AND subject_id = 'epc-s55-funded' AND usd_cap = 40 AND origin = 'migrated'`).Scan(&funded).Error; err != nil {
		t.Fatal(err)
	}
	if funded != 1 {
		t.Errorf("the funded epic's migrated budget row is missing (found %d)", funded)
	}
	if err := s.GormDB().Raw(`SELECT count(*) FROM budget WHERE subject_id = 'epc-s55-unfunded'`).Scan(&unfunded).Error; err != nil {
		t.Fatal(err)
	}
	if unfunded != 0 {
		t.Errorf("the unfunded epic earned a budget row (found %d)", unfunded)
	}
	var events int
	if err := s.GormDB().Raw(`SELECT count(*) FROM budget_event WHERE actor_id = 'migration' AND kind = 'created'`).Scan(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("created backfill events = %d, want 1 (the moved = total guard)", events)
	}
}

// TestS55WatchdogEventIsAppendOnly is the third append-only twin T1's set
// shipped without (it pinned loop_control_event and budget_event). T12's
// step-2 list names all three event tables.
func TestS55WatchdogEventIsAppendOnly(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO watchdog_event (rule_id,at,kind,actor_kind,actor_id)
		VALUES ('wd-1','2026-11-01T10:00:00Z','created','system','watchdog')`).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := g.Exec(`UPDATE watchdog_event SET kind = 'disabled' WHERE rule_id = 'wd-1'`).Error; err == nil {
		t.Errorf("an UPDATE of an append-only watchdog_event was allowed")
	}
	if err := g.Exec(`DELETE FROM watchdog_event WHERE rule_id = 'wd-1'`).Error; err == nil {
		t.Errorf("a DELETE of an append-only watchdog_event was allowed")
	}
}
