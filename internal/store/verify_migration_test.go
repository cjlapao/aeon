package store

// verify_migration_test.go — S57 T3's migration drills (P9): fresh apply
// vs upgrade from the previous vintage (schema identical, data preserved,
// ledger +1), idempotent re-open, the append-only proofs for
// verify_command_event, verify_run and verify_run_result, the closed
// CHECKs, and the end fk guard. Rollback is not offered: the framework
// runner applies each migration once, in one transaction, into the
// _migrations ledger — a property of the runner, stated not tested.

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

const s57VerifyMigration = "20261102040520_verify_records"

// s57Vintage splits the full migration set at S57's M1: before is the
// previous vintage, m1 the single new file in apply order.
func s57Vintage(t *testing.T) (before, m1 []Migration) {
	t.Helper()
	for _, m := range Migrations() {
		if m.Name == s57VerifyMigration {
			m1 = append(m1, m)
			continue
		}
		before = append(before, m)
	}
	if len(m1) != 1 {
		t.Fatalf("the S57 M1 migration set = %d entries, want 1", len(m1))
	}
	return before, m1
}

// schemaDump is the whole sqlite_master (type, name, stored DDL, table) in
// a stable order — the comparison "fresh and upgraded are identical" runs
// on, CHECKs included, since they live nowhere else but this text.
func schemaDump(t *testing.T, g *gorm.DB) string {
	t.Helper()
	rows, err := g.Raw(`SELECT type, name, COALESCE(sql, ''), tbl_name
		FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`).Rows()
	if err != nil {
		t.Fatalf("dump sqlite_master: %v", err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var typ, name, sqlText, tbl string
		if err := rows.Scan(&typ, &name, &sqlText, &tbl); err != nil {
			t.Fatalf("read sqlite_master: %v", err)
		}
		sb.WriteString(typ + "\t" + name + "\t" + tbl + "\n" + sqlText + "\n---\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump sqlite_master: %v", err)
	}
	return sb.String()
}

// seedVerifyVintage populates the pre-M1 database with the populations the
// drill names — components (root + child), a loop and a task — so the
// preservation proof is over real data, not empty tables.
func seedVerifyVintage(t *testing.T, g *gorm.DB) {
	t.Helper()
	seed := []string{
		`INSERT INTO component (id,name,slug,created_at,updated_at) VALUES ('vroot','vroot','vroot','t','t')`,
		`INSERT INTO component (id,name,slug,created_at,updated_at,parent_id) VALUES ('vchild','vchild','vchild','t','t','vroot')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,attended,allow_outward)
		   VALUES ('vl1','alpha','The alpha loop','a goal','/repo1','/repo1','standard','build','active','t','t',0,0)`,
		`INSERT INTO task (loop_slug,component_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('alpha','/repo1','VT1',1,'A task','go','eng','pending','t')`,
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the pre-S57 store: %v\n%s", err, stmt)
		}
	}
}

// TestMigrationM1FreshAndUpgradedIdentical — the T3 name: a clean file
// walked to the head and a seeded store upgraded over M1 carry the SAME
// schema (sqlite_master, DDL text included); the upgrade preserved every
// pre-existing row byte for byte; the five new tables are present and
// empty; the ledger gained exactly one row; an idempotent re-open changes
// nothing.
func TestMigrationM1FreshAndUpgradedIdentical(t *testing.T) {
	// Fresh.
	fresh := openMigrated(t)
	freshDump := schemaDump(t, fresh)

	// Upgrade.
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	before, m1 := s57Vintage(t)
	applyMigrations(t, g, before)
	seedVerifyVintage(t, g)
	pre, skipped := allFingerprints(t, g)
	if len(skipped) == 0 {
		t.Fatalf("the FTS shadow tables were not found; the fingerprint must account for them by name")
	}
	preLedger := ledgerCount(t, g)

	applyMigrations(t, g, m1)

	// Every pre-existing table keeps its row count AND its row checksum.
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
	// The five new tables exist, with their indexes and triggers, and the
	// upgrade adds shape, never data.
	for _, table := range []string{"verify_command", "verify_command_event", "verify_run", "verify_run_result", "verify_template"} {
		if n, _ := tableFingerprint(t, g, table); n != 0 {
			t.Errorf("table %s has rows after a data-free upgrade", table)
		}
	}
	for _, index := range []string{
		"uq_verify_command_component_name", "idx_verify_command_component_position",
		"uq_verify_command_event_revision", "idx_verify_command_event_component",
		"uq_verify_run_loop", "idx_verify_run_root_ended", "idx_verify_run_loop_slug", "idx_verify_run_head_sha",
		"uq_verify_template_stack_name", "idx_verify_template_stack",
	} {
		if countCatalogueEntries(t, g, "index", index) != 1 {
			t.Errorf("index %s is absent", index)
		}
	}
	for _, trigger := range []string{
		"verify_command_event_no_update", "verify_command_event_no_delete",
		"verify_run_no_update", "verify_run_no_delete",
		"verify_run_result_no_update", "verify_run_result_no_delete",
	} {
		if countCatalogueEntries(t, g, "trigger", trigger) != 1 {
			t.Errorf("trigger %s is absent", trigger)
		}
	}
	// The end guard: the FK check ran inside the migration and found
	// nothing — the guard table carries the proof (n CHECK n = 0).
	if countCatalogueEntries(t, g, "table", "zz_s57_verify_fk_guard") != 1 {
		t.Fatalf("the end fk guard table is absent")
	}
	var guard int
	if err := g.Raw(`SELECT n FROM zz_s57_verify_fk_guard`).Scan(&guard).Error; err != nil {
		t.Fatalf("read the fk guard: %v", err)
	}
	if guard != 0 {
		t.Errorf("the fk guard recorded %d dangling references, want 0", guard)
	}
	var fk int
	if err := g.Raw(`SELECT count(*) FROM pragma_foreign_key_check()`).Scan(&fk).Error; err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	if fk != 0 {
		t.Errorf("pragma_foreign_key_check found %d dangling references", fk)
	}
	// The ledger gained exactly the one new migration.
	if got := ledgerCount(t, g); got != preLedger+1 {
		t.Errorf("ledger rows %d -> %d, want +1", preLedger, got)
	}

	// Fresh and upgraded are IDENTICAL schemas.
	if upgradedDump := schemaDump(t, g); upgradedDump != freshDump {
		t.Errorf("the upgraded schema drifted from the fresh one:\n--- fresh ---\n%s\n--- upgraded ---\n%s", freshDump, upgradedDump)
	}

	// Idempotent re-open: the runner skips the already-applied migrations
	// and the data comes back intact.
	closeDB()
	g2, closeDB2 := rawGorm(t, path)
	defer closeDB2()
	applyMigrations(t, g2, Migrations())
	if got := ledgerCount(t, g2); got != preLedger+1 {
		t.Errorf("re-open applied migrations again: ledger %d -> %d", preLedger+1, got)
	}
	if n := countTable(t, g2, "component"); n != 2 {
		t.Errorf("component rows after re-open = %d, want 2", n)
	}
	if n := countTable(t, g2, "verify_command"); n != 0 {
		t.Errorf("verify_command rows after re-open = %d, want 0", n)
	}
}

// TestVerifyEventsAreAppendOnly — the three history tables refuse UPDATE
// and DELETE at the trigger layer, with a sentence naming the table, and
// the rows come back intact. A history row that can be edited is a history
// that can be rewritten; the triggers are the backstop the store API
// cannot carry.
func TestVerifyEventsAreAppendOnly(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "a-root")
	user := Actor{Kind: "user", ID: "u1"}

	w, err := s.AddVerifyCommand(user, NewVerifyCommand{ComponentID: "a-root", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300})
	if err != nil || !w.OK() {
		t.Fatalf("seed the gate: %v %v", w, err)
	}
	// A run with one result, finished (the only visible shape).
	draft, err := s.CreateVerifyRun(Actor{Kind: "system", ID: "engine"}, NewVerifyRun{
		LoopSlug: "alpha", ComponentRoot: "a-root", ComponentID: "a-root", Trigger: "loop",
		Position: 0, Attempt: 1, HeadSHA: "abc123",
	})
	if err != nil {
		t.Fatalf("create the run: %v", err)
	}
	if err := draft.AppendVerifyResult(VerifyRunResult{Name: "build", Requirement: "required", Command: "go build ./...", State: "pass"}); err != nil {
		t.Fatalf("append the result: %v", err)
	}
	if _, err := s.FinishVerifyRun(draft, "pass", "", "", "", time.Now()); err != nil {
		t.Fatalf("finish the run: %v", err)
	}

	// UPDATE and DELETE, each refused by name, on all three tables — the
	// sentence must name the table it is protecting.
	cases := []struct {
		stmt  string
		table string
	}{
		{`UPDATE verify_command_event SET summary = 'rewritten' WHERE 1=1`, "verify_command_event is append-only"},
		{`DELETE FROM verify_command_event`, "verify_command_event is append-only"},
		{`UPDATE verify_run SET state = 'pass' WHERE 1=1`, "verify_run is append-only"},
		{`DELETE FROM verify_run`, "verify_run is append-only"},
		{`UPDATE verify_run_result SET state = 'pass' WHERE 1=1`, "verify_run_result is append-only"},
		{`DELETE FROM verify_run_result`, "verify_run_result is append-only"},
	}
	for _, c := range cases {
		err := g.Exec(c.stmt).Error
		if err == nil {
			t.Errorf("%s was allowed — the history is editable", c.stmt)
			continue
		}
		if !strings.Contains(err.Error(), c.table) {
			t.Errorf("%s refused without naming its table: %v", c.stmt, err)
		}
	}
	// The rows are intact: one event (the add), one run, one result.
	if n := countTable(t, g, "verify_command_event"); n != 1 {
		t.Errorf("events after the refused writes = %d, want 1", n)
	}
	if n := countTable(t, g, "verify_run"); n != 1 {
		t.Errorf("runs after the refused writes = %d, want 1", n)
	}
	if n := countTable(t, g, "verify_run_result"); n != 1 {
		t.Errorf("results after the refused writes = %d, want 1", n)
	}
}

// TestVerifyTableCHECKsAreClosed — the CHECK layer, probed from outside the
// store API: the closed sets and bounds the design names, each bad value
// refused by the table itself (the backstop a store bypassed would hit).
func TestVerifyTableCHECKsAreClosed(t *testing.T) {
	g, _ := testStore(t)
	seedComponent(t, g, "c-root")
	bad := []string{
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','cmd','blocking',300,0,1,'user','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','cmd','required',0,0,1,'user','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','cmd','required',601,0,1,'user','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','cmd','required',300,0,1,'forge','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,memory_mb,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','cmd','required',300,0,0,1,'user','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','Build','cmd','required',300,0,1,'user','t','t')`,
		`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at) VALUES ('x','c-root','build','', 'required',300,0,1,'user','t','t')`,
		`INSERT INTO verify_command_event (command_id,component_id,revision,op,actor_kind,actor_id,summary,at) VALUES ('x','c-root',1,'rewrite','user','u1','s','t')`,
		`INSERT INTO verify_command_event (command_id,component_id,revision,op,actor_kind,actor_id,summary,at) VALUES ('x','c-root',1,'add','daemon','u1','s','t')`,
		`INSERT INTO verify_run (id,component_root,component_id,trigger,actor_kind,actor_id,started_at,state) VALUES ('r1','r','r','auto','user','u1','t','pass')`,
		`INSERT INTO verify_run (id,component_root,component_id,trigger,actor_kind,actor_id,started_at,state) VALUES ('r1','r','r','loop','daemon','u1','t','pass')`,
		`INSERT INTO verify_run (id,component_root,component_id,trigger,actor_kind,actor_id,started_at,state) VALUES ('r1','r','r','loop','user','u1','t','green')`,
		`INSERT INTO verify_run_result (run_id,seq,command,name,requirement,command,state) VALUES ('r1',0,'x','build','required','cmd','ok')`,
		`INSERT INTO verify_run_result (run_id,seq,command,name,requirement,command,state) VALUES ('r1',0,'x','build','advisory','cmd','pass')`,
		`INSERT INTO verify_template (id,stack,name,command,requirement,timeout_s,version,created_at,updated_at) VALUES ('t1','Go','build','cmd','required',300,'1','t','t')`,
		`INSERT INTO verify_template (id,stack,name,command,requirement,timeout_s,origin,version,created_at,updated_at) VALUES ('t1','go','build','cmd','required',300,'vendor','1','t','t')`,
	}
	for _, stmt := range bad {
		if err := g.Exec(stmt).Error; err == nil {
			t.Errorf("the CHECK let a bad row through:\n%s", stmt)
		}
	}
	// And the good ones still land (the CHECK is a door, not a wall):
	good := []string{
		`INSERT INTO verify_run (id,component_root,component_id,trigger,actor_kind,actor_id,started_at,state) VALUES ('rg','r','r','manual','cli','u1','t','unverified')`,
		`INSERT INTO verify_run_result (run_id,seq,command,name,requirement,command,state) VALUES ('rg',0,'x','build','advisory','cmd','unverified')`,
	}
	for _, stmt := range good {
		if err := g.Exec(stmt).Error; err != nil {
			t.Errorf("the CHECK refused a good row: %v\n%s", err, stmt)
		}
	}
	// A tail over 2 KiB is refused (VG6's bound, at the table).
	longTail := strings.Repeat("x", 2049)
	if err := g.Exec(`INSERT INTO verify_run_result (run_id,seq,command,name,requirement,command,state,stdout_tail) VALUES ('rg',1,'x','b','required','c','pass',?)`, longTail).Error; err == nil {
		t.Errorf("a 2049-byte tail passed the 2 KiB CHECK")
	}
}
