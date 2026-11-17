package store

import (
	"testing"

	"gorm.io/gorm"
)

// This file proves the S55 LC2/LC6/LC17 migrations do what they claim, not just
// that they apply: the tables and their CHECKs exist, the append-only triggers
// abort, a request moves once, and the epic->budget backfill (LC13) is a
// one-to-one, guarded copy. The SQL lives only in the migration files (I2);
// these tests read it back. Every setup INSERT and the assertions under test
// go through g.Exec so the rows are real and the per-row triggers fire (gorm's
// Create over a no-primary-key struct did not insert, and an UPDATE that matches
// zero rows never fires a per-row trigger — a false pass).

// openMigrated opens a fresh file, applies every migration, and returns a gorm
// handle. It defers the close.
func openMigrated(t *testing.T) *gorm.DB {
	t.Helper()
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	t.Cleanup(closeDB)
	applyMigrations(t, g, Migrations())
	return g
}

// seedComponent inserts a minimal root component (the rest of the columns have
// defaults from the component_tree migration) so an epic's FK is satisfied.
func seedComponent(t *testing.T, g *gorm.DB, id string) {
	t.Helper()
	if err := g.Exec(`INSERT INTO component (id,name,slug,created_at,updated_at) VALUES (?,?,?,?,?)`,
		id, id, id, "2026-11-01T09:00:00Z", "2026-11-01T09:00:00Z").Error; err != nil {
		t.Fatalf("seed component %s: %v", id, err)
	}
}

func TestS55LoopControlTablesExist(t *testing.T) {
	g := openMigrated(t)
	for _, table := range []string{"loop_control_event", "loop_control_request"} {
		if countCatalogueEntries(t, g, "table", table) != 1 {
			t.Errorf("table %s is not present", table)
		}
	}
	if countCatalogueEntries(t, g, "index", "idx_loop_control_event_loop") != 1 {
		t.Errorf("idx_loop_control_event_loop is not present")
	}
	if countCatalogueEntries(t, g, "index", "idx_loop_control_request_open") != 1 {
		t.Errorf("idx_loop_control_request_open is not present")
	}
	if countCatalogueEntries(t, g, "trigger", "loop_control_event_no_update") != 1 {
		t.Errorf("loop_control_event_no_update trigger is absent")
	}
	if countCatalogueEntries(t, g, "trigger", "loop_control_event_no_delete") != 1 {
		t.Errorf("loop_control_event_no_delete trigger is absent")
	}
}

func TestS55LoopControlEventIsAppendOnly(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO loop_control_event (loop_id,at,kind,actor_kind,actor_id)
		VALUES ('loop-1','2026-11-01T10:00:00Z','stop','user','1000')`).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := g.Exec(`UPDATE loop_control_event SET reason = 'x' WHERE loop_id = 'loop-1'`).Error; err == nil {
		t.Errorf("an UPDATE of an append-only event was allowed")
	}
	if err := g.Exec(`DELETE FROM loop_control_event WHERE loop_id = 'loop-1'`).Error; err == nil {
		t.Errorf("a DELETE of an append-only event was allowed")
	}
}

func TestS55LoopControlRequestMovesOnce(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO loop_control_request (loop_id,requested_at,by_agent,reason)
		VALUES ('loop-1','2026-11-01T10:00:00Z','bob','looping uselessly')`).Error; err != nil {
		t.Fatalf("insert request: %v", err)
	}
	if err := g.Exec(`UPDATE loop_control_request SET status = 'honored' WHERE loop_id = 'loop-1'`).Error; err != nil {
		t.Fatalf("open->honored refused: %v", err)
	}
	if err := g.Exec(`UPDATE loop_control_request SET status = 'dismissed' WHERE loop_id = 'loop-1'`).Error; err == nil {
		t.Errorf("a decided request changed again")
	}
	if err := g.Exec(`UPDATE loop_control_request SET status = 'open' WHERE loop_id = 'loop-1'`).Error; err == nil {
		t.Errorf("a decided request was re-opened")
	}
	if err := g.Exec(`DELETE FROM loop_control_request WHERE loop_id = 'loop-1'`).Error; err == nil {
		t.Errorf("a decided request was deleted")
	}
}

func TestS55BudgetCHECKs(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO budget (id,scope,subject_id,window,usd_cap,tokens_cap,wall_ms_cap,steps_cap,created_at,updated_at)
		VALUES ('bud-1','component','cmp-1','lifetime',NULL,NULL,NULL,NULL,'2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err == nil {
		t.Errorf("a budget with no dimension was accepted")
	}
	if err := g.Exec(`INSERT INTO budget (id,scope,subject_id,window,usd_cap,created_at,updated_at)
		VALUES ('bud-2','component','cmp-1','lifetime',0,'2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err != nil {
		t.Errorf("usd_cap = 0 (a real cap) was refused: %v", err)
	}
	if err := g.Exec(`INSERT INTO budget (id,scope,subject_id,window,usd_cap,created_at,updated_at)
		VALUES ('bud-3','component','cmp-1','lifetime',-1,'2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err == nil {
		t.Errorf("a negative cap was accepted")
	}
	if err := g.Exec(`INSERT INTO budget (id,scope,subject_id,window,usd_cap,created_at,updated_at)
		VALUES ('bud-4','workspace','not-workspace','lifetime',5,'2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err == nil {
		t.Errorf("a workspace budget with a non-workspace subject was accepted")
	}
}

func TestS55BudgetEventIsAppendOnly(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO budget_event (budget_id,at,kind,actor_kind,actor_id)
		VALUES ('bud-1','2026-11-01T10:00:00Z','created','system','migration')`).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := g.Exec(`UPDATE budget_event SET kind = 'hit' WHERE budget_id = 'bud-1'`).Error; err == nil {
		t.Errorf("an UPDATE of an append-only budget_event was allowed")
	}
	if err := g.Exec(`DELETE FROM budget_event WHERE budget_id = 'bud-1'`).Error; err == nil {
		t.Errorf("a DELETE of an append-only budget_event was allowed")
	}
}

func TestS55WatchdogRuleCHECKs(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO watchdog_rule (id,scope,subject_id,signal,threshold,action,created_at,updated_at)
		VALUES ('wd-1','workspace','workspace','no-activity',1500,'notify','2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err != nil {
		t.Fatalf("a valid watchdog rule was refused: %v", err)
	}
	if err := g.Exec(`INSERT INTO watchdog_rule (id,scope,subject_id,signal,threshold,action,created_at,updated_at)
		VALUES ('wd-2','workspace','workspace','bogus',1,'notify','2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err == nil {
		t.Errorf("a watchdog rule with a bad signal was accepted")
	}
	if err := g.Exec(`INSERT INTO watchdog_rule (id,scope,subject_id,signal,threshold,action,created_at,updated_at)
		VALUES ('wd-3','workspace','workspace','no-activity',-1,'notify','2026-11-01T10:00:00Z','2026-11-01T10:00:00Z')`).Error; err == nil {
		t.Errorf("a watchdog rule with a negative threshold was accepted")
	}
}

// TestS55EpicBackfillMatchesEpicWithBudget proves the LC13 backfill is a
// one-to-one, guarded copy: a pre-vintage database with a funded epic and an
// unfunded one gets exactly one migrated budget row (for the funded epic) plus
// its created event; the unfunded epic earns neither.
func TestS55EpicBackfillMatchesEpicWithBudget(t *testing.T) {
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	t.Cleanup(closeDB)

	// Everything up to (but not including) the budget migration.
	full := Migrations()
	var before, budgetSet []Migration
	for _, m := range full {
		if m.Name == "20261101100002_budget" {
			budgetSet = []Migration{m}
			continue
		}
		before = append(before, m)
	}
	applyMigrations(t, g, before)

	seedComponent(t, g, "cmp-1")
	if err := g.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,budget_usd,created_at,updated_at)
		VALUES ('epc-1','cmp-1','funded','o','a',40,'2026-11-01T09:00:00Z','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed funded epic: %v", err)
	}
	if err := g.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,budget_usd,created_at,updated_at)
		VALUES ('epc-2','cmp-1','unfunded','o','a',NULL,'2026-11-01T09:00:00Z','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed unfunded epic: %v", err)
	}

	applyMigrations(t, g, budgetSet)

	var budgets int
	if err := g.Raw(`SELECT count(*) FROM budget WHERE origin = 'migrated'`).Scan(&budgets).Error; err != nil {
		t.Fatalf("count migrated budgets: %v", err)
	}
	if budgets != 1 {
		t.Fatalf("migrated budgets = %d, want 1 (one per funded epic)", budgets)
	}
	type migratedBudget struct {
		ID      string  `gorm:"column:id"`
		Subject string  `gorm:"column:subject_id"`
		Cap     float64 `gorm:"column:usd_cap"`
	}
	var b migratedBudget
	if err := g.Raw(`SELECT id, subject_id, usd_cap FROM budget WHERE origin = 'migrated'`).Scan(&b).Error; err != nil {
		t.Fatalf("read the migrated budget: %v", err)
	}
	if b.ID != "bud-epc-1" || b.Subject != "epc-1" || b.Cap != 40 {
		t.Errorf("migrated budget = %s/%s/%v, want bud-epc-1/epc-1/40", b.ID, b.Subject, b.Cap)
	}
	var events int
	if err := g.Raw(`SELECT count(*) FROM budget_event WHERE actor_id = 'migration'`).Scan(&events).Error; err != nil {
		t.Fatalf("count created events: %v", err)
	}
	if events != 1 {
		t.Errorf("created events = %d, want 1", events)
	}
}
