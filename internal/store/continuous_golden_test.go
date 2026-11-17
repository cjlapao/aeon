package store

// S27 T1 — characterization goldens at the store seam, on the UNTOUCHED
// code: (a) the queue hold is QUEUE words only (F4 — blind to loop rows;
// CN8's own gate, not this SQL, is where S27 adds loop sight), (b) the
// claim-before-start CAS (a second `ready→running` loses), and (c) the
// CURRENT loop_run DDL, snapshotted verbatim so T2's widening of
// started_by_kind is a diff against a recorded before-picture.
//
// WHO MAY CHANGE THESE AND HOW:
//   - loop_run_ddl.json — T2 may change it ONLY by shipping a new
//     timestamped migration; the CHECK widened to admit `scheduler`, the
//     old bytes preserved in git as the before. No in-place edit is legal
//     (§3: forward-only), and this golden is the witness that any change
//     came with a migration number.
//   - the hold tests — never change: if S27 teaches the HOLD itself to see
//     loop rows, that inverts F4 and CN8 in one move; the change belongs in
//     the scheduler's readiness gate instead, and these tests say so.
//
// Deviation from the task's file list, recorded: the `IsReady` table lives
// in internal/roadmap/continuous_golden_test.go — `store` may not import
// `roadmap` (roadmap imports store; the test would be an import cycle).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func s27Check(t *testing.T, name string, got any) {
	t.Helper()
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("testdata", "continuous_golden", name+".json")
	if os.Getenv("S27_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (%v) — generate with S27_GOLDEN_UPDATE=1 on the PRE-change code", path, err)
	}
	if string(want) != string(raw) {
		t.Errorf("golden %s drifted.\n--- got ---\n%s\n--- want ---\n%s", name, raw, want)
	}
}

func TestLoopRunDDLSnapshot(t *testing.T) {
	s := openTemp(t)
	type ddl struct {
		Name string `json:"name"`
		Type string `json:"type"`
		SQL  string `json:"sql"`
	}
	var want []ddl
	rows, err := s.r.Query(`SELECT name, type, sql FROM sqlite_master
		WHERE tbl_name = 'loop_run' AND name NOT LIKE 'sqlite_autoindex%'
		ORDER BY type DESC, name ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var d ddl
		if err := rows.Scan(&d.Name, &d.Type, &d.SQL); err != nil {
			t.Fatal(err)
		}
		want = append(want, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("no loop_run DDL found")
	}
	s27Check(t, "loop_run_ddl", want)
}

// TestNextReadyQueueItemHoldsAreQueueWords drives the SQL verbatim:
// `running` and `blocked` QUEUE rows hold the plane; a ready row BEHIND
// them waits; and a RUNNING LOOP in the same component root is invisible
// to the hold (F4). This is the before-picture CN8 respects.
func TestNextReadyQueueItemHoldsAreQueueWords(t *testing.T) {
	s := openTemp(t)
	const root = "/r/hold"
	if _, err := s.CreateComponent(NewComponent{Name: "hold", Slug: "hold", Root: root}); err != nil {
		t.Fatal(err)
	}
	// A table of cases, each on a fresh database: the golden serializes
	// which head (if any) the SQL names.
	type holdCaseRow struct {
		Status string `json:"status"`
		Goal   string `json:"goal"`
	}
	type holdCase struct {
		Name       string        `json:"name"`
		Rows       []holdCaseRow `json:"rows"`
		PersonLoop bool          `json:"personLoop"`
		Head       string        `json:"head"` // "" when the hold refuses everything
	}
	run := func(t *testing.T, c holdCase) {
		t.Helper()
		s := openTemp(t)
		if _, err := s.CreateComponent(NewComponent{Name: "hold", Slug: "hold", Root: root}); err != nil {
			t.Fatal(err)
		}
		for _, r := range c.Rows {
			if _, err := s.AppendQueueItem(NewQueueItemRow{ComponentRoot: root, Status: r.Status, Blueprint: "dev-loop", Goal: r.Goal, WorkspaceRoot: root}); err != nil {
				t.Fatal(err)
			}
		}
		if c.PersonLoop {
			if _, err := s.CreatePinnedLoop(NewLoop{Slug: "persons-loop", ComponentRoot: root, Root: root, Goal: "a person works", Status: "running"},
				"dev-loop", 1, "id: dev-loop\nschemaVersion: 1\nversion: 1\nlanes: []\n", "{}"); err != nil {
				t.Fatal(err)
			}
		}
		head := ""
		if item, found, err := s.NextReadyQueueItem(root); err != nil {
			t.Fatal(err)
		} else if found {
			head = item.Goal
		}
		c.Head = head
		s27Check(t, "next_ready_"+c.Name, c)
	}
	run(t, holdCase{Name: "plain_ready", Rows: []holdCaseRow{{Status: "ready", Goal: "first"}, {Status: "ready", Goal: "second"}}})
	run(t, holdCase{Name: "running_holds", Rows: []holdCaseRow{{Status: "running", Goal: "in flight"}, {Status: "ready", Goal: "waits behind"}}})
	run(t, holdCase{Name: "blocked_holds", Rows: []holdCaseRow{{Status: "blocked", Goal: "halted"}, {Status: "ready", Goal: "waits behind"}}})
	run(t, holdCase{Name: "person_loop_invisible", Rows: []holdCaseRow{{Status: "ready", Goal: "queue says go"}}, PersonLoop: true})
}

// TestTransitionQueueItemClaimedFromLosesTheRace: the claim is a CAS —
// the second `ready→running` for the same row gets FALSE, and a row that
// already moved refuses a stale `from`. S27's claim-before-start rides
// this exact edge (scheduler_start admits only `ready`).
func TestTransitionQueueItemClaimedFromLosesTheRace(t *testing.T) {
	s := openTemp(t)
	const root = "/r/claim"
	if _, err := s.CreateComponent(NewComponent{Name: "claim", Slug: "claim", Root: root}); err != nil {
		t.Fatal(err)
	}
	row, err := s.AppendQueueItem(NewQueueItemRow{ComponentRoot: root, Status: "ready", Blueprint: "dev-loop", Goal: "one slot", WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	won, err := s.TransitionQueueItem(row.ID, "ready", "running")
	if err != nil || !won {
		t.Fatalf("first claim: %v %v", won, err)
	}
	lost, err := s.TransitionQueueItem(row.ID, "ready", "running")
	if err != nil {
		t.Fatal(err)
	}
	if lost {
		t.Fatal("a second claim on a RUNNING row succeeded — the CAS is not a CAS")
	}
	// And a stale FROM on a row that moved elsewhere loses too.
	if _, err := s.TransitionQueueItem(row.ID, "running", "done"); err != nil {
		t.Fatal(err)
	}
	stale, err := s.TransitionQueueItem(row.ID, "ready", "running")
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("a done row was re-claimed from `ready`")
	}
}
