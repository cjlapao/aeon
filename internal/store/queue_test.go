package store

// S24 T1 — the queue's shape is the DATABASE's claim, not Go's. Each gate
// here is the house CHECK discipline (schema_test.go's
// TestTheCheckConstraintsRejectEveryInvalidValue is the template): the
// insert is attempted with every column but the tested one valid, the
// refusal must be SQLite naming a CHECK, and each gate carries its own
// positive control — a constraint that rejects everything passes the
// negative half and breaks the slice.

import (
	"strings"
	"testing"
)

// queueItemSQL is the honest full row: every NOT NULL column named, so a
// case failing on the CHECK it claims (and not on a missing column) is the
// case actually being tested.
const queueItemSQL = `INSERT INTO queue_item
	(component_root, position, status, blueprint, goal, workspace_root, created_at, updated_at)
	VALUES (?,?,?,?,?,?,?,?)`

// seedQueueComponent registers a component WITH its root — the parent
// rows every queue_item FK needs (CreateLoop alone does not create them:
// the loop table's root column predates the tier and is not itself the
// FK target).
func seedQueueComponent(t *testing.T, s *Store, slug, root string) string {
	t.Helper()
	c, err := s.CreateComponent(NewComponent{Name: slug, Slug: slug, Root: root})
	if err != nil {
		t.Fatalf("seed component %s at %s: %v", slug, root, err)
	}
	return c.Component.ID
}

func TestQueueAdvanceAndSizingModeCheckedVocabulary(t *testing.T) {
	// D3 and D7 as CHECKs. The DEFAULTS are part of the claim (R4/CG6):
	// 'ask' and 'manual' are what a component gets when nobody has
	// decided, and an upgrade of a database full of old rows must produce
	// the safe settings, not the eager ones.
	s := openTemp(t)
	cid := seedQueueComponent(t, s, "adv", "/repo/adv")

	t.Run("the defaults are the safe pair", func(t *testing.T) {
		var adv, sizing string
		if err := s.r.QueryRow(`SELECT queue_advance, sizing_mode FROM component WHERE id = ?`, cid).Scan(&adv, &sizing); err != nil {
			t.Fatal(err)
		}
		if adv != "ask" || sizing != "manual" {
			t.Errorf("a plain upgrade carried %q/%q — the defaults are R4's answer to every existing row", adv, sizing)
		}
	})

	cases := []struct {
		name, sql, bad string
	}{
		{"queue_advance outside the two", `UPDATE component SET queue_advance = ? WHERE id = ?`, "immediate"},
		{"queue_advance misspelt", `UPDATE component SET queue_advance = ? WHERE id = ?`, "auto "},
		{"queue_advance wrong-case (TEXT CHECKs are exact)", `UPDATE component SET queue_advance = ? WHERE id = ?`, "ASK"},
		{"sizing_mode outside the two", `UPDATE component SET sizing_mode = ? WHERE id = ?`, "guess"},
		{"sizing_mode carrying the OTHER vocabulary word", `UPDATE component SET sizing_mode = ? WHERE id = ?`, "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.w.Exec(tc.sql, tc.bad, cid); err == nil {
				t.Fatalf("%q was accepted — the CHECK is missing or wrong", tc.bad)
			} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Errorf("refused, but not on the CHECK: %v", err)
			}
		})
	}
	for _, pair := range [][2]string{{"auto", "manual"}, {"ask", "skill"}, {"ask", "manual"}, {"auto", "skill"}} {
		if _, err := s.w.Exec(`UPDATE component SET queue_advance = ?, sizing_mode = ? WHERE id = ?`, pair[0], pair[1], cid); err != nil {
			t.Errorf("the legal pair %q/%q was refused: %v", pair[0], pair[1], err)
		}
	}
}

func TestQueueItemSevenStatusesClosed(t *testing.T) {
	// CG1: exactly seven, no eighth — and the positive control walks all
	// seven, because a CHECK that admits six while the Go vocabulary says
	// seven is T2's phantom failure waiting to happen.
	s := openTemp(t)
	seedQueueComponent(t, s, "stat", "/repo/stat")

	for _, bad := range []string{"queued", "PENDING", "running ", "failed", "", "canceled"} {
		if _, err := s.w.Exec(queueItemSQL, "/repo/stat", 1, bad, "dev-loop", "goal", "/ws", "now", "now"); err == nil {
			t.Fatalf("status %q was accepted — the seven are closed", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("status %q refused for the wrong reason: %v", bad, err)
		}
	}
	for i, ok := range []string{"pending", "sizing", "ready", "running", "done", "blocked", "cancelled"} {
		if _, err := s.w.Exec(queueItemSQL, "/repo/stat", 10+i, ok, "dev-loop", "goal", "/ws", "now", "now"); err != nil {
			t.Errorf("the seventh-list status %q was refused: %v", ok, err)
		}
	}
}

func TestQueueItemScopedByProjectRoot(t *testing.T) {
	// The queue belongs to a component-with-a-root; a row naming a root
	// that has never been seen is refused by the FK, not filtered out
	// later by a JOIN that quietly drops it.
	s := openTemp(t)
	seedQueueComponent(t, s, "scope", "/repo/real")

	if _, err := s.w.Exec(queueItemSQL, "/repo/never-was", 1, "pending", "dev-loop", "goal", "/ws", "now", "now"); err == nil {
		t.Fatal("a queue row for an unknown root was accepted — the FK is inert (did foreign_keys on?)")
	} else if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Errorf("refused, but not on the FK: %v", err)
	}
	if _, err := s.w.Exec(queueItemSQL, "/repo/real", 1, "pending", "dev-loop", "goal", "/ws", "now", "now"); err != nil {
		t.Fatalf("the honest row was refused: %v", err)
	}
	// D10's shape: the loop columns are ABSENT until materialization — a
	// row is a complete queue item with no loop at all.
	var slug *string
	if err := s.r.QueryRow(`SELECT loop_slug FROM queue_item WHERE component_root = ?`, "/repo/real").Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug != nil {
		t.Errorf("a never-materialized item carries loop_slug %v — absence is NULL, no sentinel (D10)", *slug)
	}
	// And the materialized pointer must name a real loop of the SAME root:
	// a row pointing at another root's loop is a lie about provenance.
	if _, err := s.w.Exec(`UPDATE queue_item SET loop_slug = 'ghost' WHERE component_root = ?`, "/repo/real"); err == nil {
		t.Fatal("loop_slug='ghost' was accepted — the composite FK to loop is inert")
	} else if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Errorf("the materialization FK refused for the wrong reason: %v", err)
	}
	seedLoop(t, s, "mate", "/repo/real")
	if _, err := s.w.Exec(`UPDATE queue_item SET loop_slug = 'mate' WHERE component_root = ?`, "/repo/real"); err != nil {
		t.Fatalf("pointing at a real loop of the same root was refused: %v", err)
	}
	// D8 as SQL: the size is one fact or none.
	cases := []struct {
		name        string
		min, max    any
		basis       any
		wantRefused bool
	}{
		{"a bare number", 3, 5, nil, true},
		{"a basis without numbers", nil, nil, "three sessions of TDD", true},
		{"an inverted range", 5, 3, "measured", true},
		{"an empty basis", 3, 5, "", true},
		{"the honest range", 3, 5, "two sessions of TDD", false},
		{"no size at all", nil, nil, nil, false},
	}
	for i, tc := range cases {
		_, err := s.w.Exec(`INSERT INTO queue_item (component_root, position, status, blueprint, goal, workspace_root, size_min, size_max, size_basis, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, "/repo/real", 100+i, "pending", "dev-loop", "goal", "/ws", tc.min, tc.max, tc.basis, "now", "now")
		if tc.wantRefused && err == nil {
			t.Errorf("%s was accepted — D8 is a suggestion, not a constraint", tc.name)
		}
		if !tc.wantRefused && err != nil {
			t.Errorf("%s was refused: %v", tc.name, err)
		}
	}
	// Attendedness keeps the loop.attended three states (D5): 1, 0, NULL.
	for _, att := range []any{1, 0, nil} {
		if _, err := s.w.Exec(`INSERT INTO queue_item (component_root, position, status, blueprint, goal, workspace_root, attended, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, "/repo/real", 200, "pending", "dev-loop", "goal", "/ws", att, "now", "now"); err != nil {
			t.Errorf("attended=%v refused — the three states are D5's contract: %v", att, err)
		}
	}
	if _, err := s.w.Exec(`INSERT INTO queue_item (component_root, position, status, blueprint, goal, workspace_root, attended, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, "/repo/real", 201, "pending", "dev-loop", "goal", "/ws", 2, "now", "now"); err == nil {
		t.Error("attended=2 was accepted — booleans are 0/1/NULL here as they are on loop")
	}
}

// TestTheSelectionDoorsHaltOnAnyBlocked is CG2's clause in the one read
// every advancer goes through — the T9 walk found `queue.advance` starting
// item 2 OVER a blocked item 1, because the D2 running-clause alone answers
// "what may start" and had no word about `blocked`. Both selection doors
// (next-ready and the sizing dispatch's first-pending) halt together, and
// each carries its positive control BEFORE and AFTER the halt — a clause
// that rejected everything would fail here, not silently pass.
func TestTheSelectionDoorsHaltOnAnyBlocked(t *testing.T) {
	s := openTemp(t)
	seedQueueComponent(t, s, "cg2", "/repo/cg2")
	insert := func(pos int, status string) {
		t.Helper()
		if _, err := s.w.Exec(queueItemSQL, "/repo/cg2", pos, status, "dev-loop", "goal", "/repo/cg2", "now", "now"); err != nil {
			t.Fatalf("insert %d/%s: %v", pos, status, err)
		}
	}
	insert(1, "ready")
	insert(2, "ready")
	insert(3, "pending")

	// Positive controls before the halt: both doors answer.
	if row, ok, err := s.NextReadyQueueItem("/repo/cg2"); err != nil || !ok || row.Position != 1 {
		t.Fatalf("the ready door did not open before any block: %v %v", ok, err)
	}
	if row, ok, err := s.FirstPendingQueueItem("/repo/cg2"); err != nil || !ok || row.Position != 3 {
		t.Fatalf("the sizing door did not open before any block: %v %v", ok, err)
	}

	// The blocked row — positioned LAST on purpose: a position-ordered walk
	// that merely stopped early would pass for the wrong reason. The halt is
	// not about order; it is unconditional (CG2).
	insert(4, "blocked")
	if _, ok, err := s.NextReadyQueueItem("/repo/cg2"); err != nil || ok {
		t.Fatalf("a ready item started over a blocked row — CG2 halts unconditionally: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.FirstPendingQueueItem("/repo/cg2"); err != nil || ok {
		t.Fatalf("a sizing run dispatched over a blocked row — the halt covers the whole queue: ok=%v err=%v", ok, err)
	}

	// Resolve the halt (the person cancels — D12's door) and BOTH doors open
	// again. Without this control the two refusals could be a broken query.
	if _, err := s.w.Exec(`UPDATE queue_item SET status = 'cancelled' WHERE component_root = ? AND status = 'blocked'`, "/repo/cg2"); err != nil {
		t.Fatal(err)
	}
	if row, ok, err := s.NextReadyQueueItem("/repo/cg2"); err != nil || !ok || row.Position != 1 {
		t.Fatalf("the ready door stayed shut after the halt was resolved: %v %v", ok, err)
	}
	if row, ok, err := s.FirstPendingQueueItem("/repo/cg2"); err != nil || !ok || row.Position != 3 {
		t.Fatalf("the sizing door stayed shut after the halt was resolved: %v %v", ok, err)
	}
}
