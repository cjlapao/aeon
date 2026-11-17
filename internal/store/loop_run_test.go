package store

import (
	"path/filepath"
	"testing"
)

// loop_run_test.go pins migration 0037 (S39 CR2): the runs table, the
// step.run_seq column, and the run-1 backfill. No Go behaviour reads
// loop_run yet — that is T2 — so these tests are the migration's whole
// contract: shape, backfill CASEs, CHECKs, CASCADE, and the ledger entry
// applied exactly once. (T0-Q2: loop_blueprint is NOT re-keyed; every
// statement that assumes one row per loop keeps working.)

// seedPin plants one loop (+ pin and/or step) in a store parked at
// migration 36, so the next open runs 0037 over the seed. Columns speak
// component_root — the live name since 0035 renamed it (T0-Q4: the
// immutable DDL still says project_root; the live shape does not).
func seedPin(t *testing.T, dir string, id, slug, status, startedBy, triggerID string, withPin bool, seedStep bool) {
	t.Helper()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	applyMigrations(t, g, Migrations()[:36])
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, slug, "t", "g", "/r/"+slug, "/r/"+slug, "standard", "preflight", status,
		"2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", startedBy, triggerID).Error; err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	if withPin {
		if err := g.Exec(`INSERT INTO loop_blueprint (loop_id,blueprint_id,blueprint_version,content,runtime_json,updated_at)
			VALUES (?,?,?,?,?,?)`, id, "two-step", 1, "id: two-step", `{"position":{"lane":""}}`, "2026-01-02T00:00:00Z").Error; err != nil {
			t.Fatalf("seed pin: %v", err)
		}
	}
	if seedStep {
		if err := g.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
			VALUES (?,?,?,?,?,?)`, slug, "/r/"+slug, "build", "x", "done", "2026-01-01T00:00:00Z").Error; err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}
}

func openAfter0037(t *testing.T, dir string) *Store {
	t.Helper()
	return openAt(t, dir) // runs 0037 on the way up
}

func TestLoopRunMigrationCreatesTableAndStepRunSeq(t *testing.T) {
	dir := t.TempDir()
	s := openAfter0037(t, dir)
	want := []string{"loop_id", "run_seq", "blueprint", "blueprint_version", "content",
		"runtime_json", "state", "started_by_kind", "started_by_id", "kickoff_message_id",
		"goal_id", "goal_version", "task_id", "started_at", "ended_at"}
	got := tableColumns(t, s, "loop_run")
	if len(got) != len(want) {
		t.Fatalf("loop_run has %d columns %v, want the 15 in order %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loop_run column %d is %q, want %q (order is the contract)", i, got[i], want[i])
		}
	}
	cols := tableColumns(t, s, "step")
	if len(cols) == 0 || cols[len(cols)-1] != "run_seq" {
		t.Fatalf("step must END with run_seq (ALTER), got %v", cols)
	}
}

func TestLoopRunMigrationBackfillsOneRunPerPinnedLoop(t *testing.T) {
	dir := t.TempDir()
	seedPin(t, dir, "L1", "a", "done", "", "", true, true)
	s := openAfter0037(t, dir)
	var runSeq int
	var state, endedAt string
	var runtime string
	if err := s.r.QueryRow(`SELECT run_seq, state, COALESCE(ended_at,''), COALESCE(runtime_json,'') FROM loop_run WHERE loop_id='L1'`).
		Scan(&runSeq, &state, &endedAt, &runtime); err != nil {
		t.Fatalf("backfill wrote no run 1: %v", err)
	}
	if runSeq != 1 || state != "done" {
		t.Fatalf("run 1 is seq %d state %q, want 1/done", runSeq, state)
	}
	if endedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("ended_at is %q, want the loop's updated_at", endedAt)
	}
	if runtime == "" {
		t.Fatal("a finished loop's run snapshot must keep runtime_json")
	}
	var stepRun int
	if err := s.r.QueryRow(`SELECT run_seq FROM step WHERE loop_slug='a'`).Scan(&stepRun); err != nil || stepRun != 1 {
		t.Fatalf("the loop's step has run_seq %d (err %v), want 1", stepRun, err)
	}
}

func TestLoopRunMigrationBackfillsARunningLoopAsRunning(t *testing.T) {
	dir := t.TempDir()
	seedPin(t, dir, "L2", "b", "running", "", "", true, false)
	s := openAfter0037(t, dir)
	var state, endedAt, runtime string
	if err := s.r.QueryRow(`SELECT state, COALESCE(ended_at,'<null>'), COALESCE(runtime_json,'<null>') FROM loop_run WHERE loop_id='L2'`).
		Scan(&state, &endedAt, &runtime); err != nil {
		t.Fatalf("backfill refused a running loop: %v", err)
	}
	if state != "running" || endedAt != "<null>" || runtime != "<null>" {
		t.Fatalf("running backfill is state=%q ended=%q runtime=%q, want running/null/null (the end CHECK ties ended_at to the state)", state, endedAt, runtime)
	}
}

func TestLoopRunMigrationMapsStartedByToTheRunsStarter(t *testing.T) {
	dir := t.TempDir()
	seedPin(t, dir, "L3", "c", "running", "trigger", "t1", true, false)
	seedPin(t, dir, "L4", "d", "running", "", "", true, false)
	s := openAfter0037(t, dir)
	var kind, who string
	if err := s.r.QueryRow(`SELECT started_by_kind, started_by_id FROM loop_run WHERE loop_id='L3'`).Scan(&kind, &who); err != nil {
		t.Fatal(err)
	}
	if kind != "trigger" || who != "t1" {
		t.Fatalf("provenance lost: %s/%s, want trigger/t1 (S20's 0033 columns)", kind, who)
	}
	if err := s.r.QueryRow(`SELECT started_by_kind, started_by_id FROM loop_run WHERE loop_id='L4'`).Scan(&kind, &who); err != nil {
		t.Fatal(err)
	}
	if kind != "user" || who != "" {
		t.Fatalf("no-provenance loop is %s/%q, want user/''", kind, who)
	}
}

func TestLoopRunMigrationLeavesAStepWithoutALoopPinUnnumbered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations()[:36])
	// A loop with NO pin row and its own step: the UPDATE is scoped by the
	// loop_blueprint JOIN, so its step must keep run_seq NULL. (A step whose
	// loop row does not exist at all cannot be seeded here — step's FK
	// (component_root, loop_slug) references loop, and the framework opens
	// with foreign_keys=1; the JOIN scoping is what this test owns.)
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		VALUES ('L5','e','t','g','/r/e','/r/e','standard','preflight','running','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := g.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
		VALUES ('e','/r/e','build','x','done','2026-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}
	closeDB()
	s := openAfter0037(t, dir)
	var runSeq *int
	if err := s.r.QueryRow(`SELECT run_seq FROM step WHERE loop_slug='e'`).Scan(&runSeq); err != nil {
		t.Fatalf("the unpinned loop's step must survive the upgrade: %v", err)
	}
	if runSeq != nil {
		t.Fatalf("a step whose loop has no pin got run_seq=%d, want NULL — the UPDATE is scoped by the loop_blueprint JOIN", *runSeq)
	}
}

func TestLoopRunStateCHECKRefusesAFifthValue(t *testing.T) {
	dir := t.TempDir()
	s := openAfter0037(t, dir)
	seedRunLoopRow(t, s, "L6", "f")
	if _, err := s.w.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_at)
		VALUES ('L6',2,'two-step',1,'x','waiting','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("state='waiting' must be refused — waiting/blocked are the LOOP's condition, derived from loop.status (DB6), never a run state")
	}
}

func TestLoopRunEndCHECKTiesEndedAtToState(t *testing.T) {
	dir := t.TempDir()
	s := openAfter0037(t, dir)
	seedRunLoopRow(t, s, "L7", "h")
	if _, err := s.w.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_at,ended_at)
		VALUES ('L7',2,'two-step',1,'x','running','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`); err == nil {
		t.Fatal("a running run with ended_at must be refused")
	}
	if _, err := s.w.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_at)
		VALUES ('L7',2,'two-step',1,'x','done','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a done run without ended_at must be refused")
	}
}

func TestLoopRunIsDeletedWithItsLoop(t *testing.T) {
	dir := t.TempDir()
	seedPin(t, dir, "L8", "i", "running", "", "", true, false)
	s := openAfter0037(t, dir)
	if n := countTable(t, s.gw, "loop_run"); n != 1 {
		t.Fatalf("precondition: %d rows, want 1", n)
	}
	if _, err := s.w.Exec(`DELETE FROM loop WHERE id='L8'`); err != nil {
		t.Fatal(err)
	}
	if n := countTable(t, s.gw, "loop_run"); n != 0 {
		t.Fatalf("loop_run survived its loop: %d rows, want 0 (ON DELETE CASCADE)", n)
	}
}

func TestTheLedgerRecordsTheLoopRunMigrationExactlyOnceAcrossTwoOpens(t *testing.T) {
	// Copy of TestTheLedgerRecords0021ExactlyOnceAcrossTwoOpens (steers_test.go):
	// opening the store twice is the ordinary case (a restart, or the CLI
	// beside a running daemon); a migration applied twice is a CREATE TABLE
	// that failed the second time and left the ledger lying.
	dir := t.TempDir()
	for open := 1; open <= 2; open++ {
		s := openAt(t, dir)
		applied, err := s.AppliedMigrations()
		if err != nil {
			t.Fatalf("open %d: %v", open, err)
		}
		var n int
		var status string
		for _, a := range applied {
			if a.Name == "0037_loop_run" {
				n++
				status = a.Status
			}
		}
		if n != 1 {
			t.Fatalf("open %d: %d ledger rows named 0037_loop_run, want exactly 1", open, n)
		}
		if status != "applied" {
			t.Fatalf("ledger status is %q, want applied", status)
		}
	}
}

// seedRunLoopRow plants a bare loop for the CHECK/CASCADE tests (the runs
// table's own constraints are exercised with explicit rows).
func seedRunLoopRow(t *testing.T, s *Store, id, slug string) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		VALUES (?,?,'t','g','/r','/r','standard','preflight','running','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, id, slug); err != nil {
		t.Fatal(err)
	}
}
