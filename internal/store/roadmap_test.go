package store

// S26 T1 — the roadmap item's shape is the DATABASE's claim, not Go's.
// The house CHECK discipline (queue_test.go is the sibling, schema_test.go's
// TestTheCheckConstraintsRejectEveryInvalidValue the template): every
// column but the tested one valid, the refusal must be SQLite naming a
// CHECK, and each gate carries its own positive control — a constraint
// that rejects everything passes the negative half and breaks the slice.

import (
	"strings"
	"testing"
)

// roadmapItemSQL is the honest minimal row: every NOT NULL column named
// (provenance included, and it has NO default on purpose — D5 makes the
// writer answer where the item came from), so a case failing on the CHECK
// it claims is the case actually being tested.
const roadmapItemSQL = `INSERT INTO roadmap_item
	(component_root, title, status, provenance, created_at, updated_at)
	VALUES (?,?,?,?,?,?)`

// seedRoadmapComponent registers a component WITH its root — the parent
// rows every roadmap_item FK needs, exactly as queue_test.go's seed helper
// does for queue_item (same composite loop FK, same component_root FK).
func seedRoadmapComponent(t *testing.T, s *Store, slug, root string) string {
	t.Helper()
	c, err := s.CreateComponent(NewComponent{Name: slug, Slug: slug, Root: root})
	if err != nil {
		t.Fatalf("seed component %s at %s: %v", slug, root, err)
	}
	return c.Component.ID
}

func TestRoadmapItemEightStatusesClosed(t *testing.T) {
	// R1: exactly eight, no ninth — and the positive control walks all
	// eight, because a CHECK that admits seven while Go's vocabulary says
	// eight is T2's phantom failure waiting to happen.
	s := openTemp(t)
	seedRoadmapComponent(t, s, "stat", "/repo/stat")

	for _, bad := range []string{"queued", "IDEA", "idea ", "failed", "canceled", "blocked", "paused", ""} {
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/stat", "a title", bad, "authored", "now", "now"); err == nil {
			t.Fatalf("status %q was accepted — the eight are closed", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("status %q refused for the wrong reason: %v", bad, err)
		}
	}
	for _, ok := range []string{"idea", "ideating", "brief-agreed", "planned", "ready", "running", "done", "cancelled"} {
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/stat", "a title", ok, "authored", "now", "now"); err != nil {
			t.Errorf("the eighth-list status %q was refused: %v", ok, err)
		}
	}
}

func TestPriorityThreeStateClosed(t *testing.T) {
	// D2, and S29's own locked words: known, assumed, unknown — never a
	// number, never collapsed. The DEFAULT is part of the claim: an item
	// nobody has prioritized yet is honestly UNKNOWN, which is what makes
	// "never silently presented as low" the database's guarantee too.
	s := openTemp(t)
	seedRoadmapComponent(t, s, "prio", "/repo/prio")

	for _, bad := range []string{"low", "1", "KNOWN", "unknown ", "must_be_known", ""} {
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, priority_state, provenance, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, "/repo/prio", "t", "idea", bad, "authored", "now", "now"); err == nil {
			t.Fatalf("priority_state %q was accepted — the three are closed", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("priority_state %q refused for the wrong reason: %v", bad, err)
		}
	}
	for _, ok := range []string{"known", "assumed", "unknown"} {
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, priority_state, provenance, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, "/repo/prio", "t", "idea", ok, "authored", "now", "now"); err != nil {
			t.Errorf("the legal priority_state %q was refused: %v", ok, err)
		}
	}

	t.Run("the default is the honest unknown", func(t *testing.T) {
		var state string
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/prio", "undecided", "idea", "authored", "now", "now"); err != nil {
			t.Fatal(err)
		}
		if err := s.r.QueryRow(`SELECT priority_state FROM roadmap_item WHERE title = 'undecided'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "unknown" {
			t.Errorf("an item nobody prioritized reads %q — D2's honest absence is the DEFAULT, not a caller's habit", state)
		}
	})

	t.Run("a label on an unknown priority is unwritable", func(t *testing.T) {
		// The confident wrong answer D2 exists to prevent: the word
		// "unknown" and a label in the same row contradict each other.
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, priority_state, priority_label, provenance, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`, "/repo/prio", "t", "idea", "unknown", "P1", "authored", "now", "now"); err == nil {
			t.Fatal("a labelled 'unknown' priority was accepted — D2 is not closed at the database")
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("refused, but not on the CHECK: %v", err)
		}
		// Positive control: a label rides known and assumed, and their
		// ABSENCE is legal too (T1 makes the label meaningful-when-not-
		// unknown, not mandatory).
		for _, pair := range [][2]string{{"known", "P1"}, {"assumed", "probably P2"}, {"known", ""}} {
			label := pair[1]
			var lp *string
			if label != "" {
				lp = &label
			}
			if _, err := s.w.Exec(`INSERT INTO roadmap_item
				(component_root, title, status, priority_state, priority_label, provenance, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?)`, "/repo/prio", "t "+pair[0]+label, "idea", pair[0], lp, "authored", "now", "now"); err != nil {
				t.Errorf("priority_state %q with label %q was refused: %v", pair[0], label, err)
			}
		}
	})
}

func TestProvenanceThreeStateClosed(t *testing.T) {
	// D5's closed vocabulary, borrowed from existing words — trigger
	// (S19/S20) and triage's hand-off — and NO invented ones:
	// "webhook-created" is the exact kind of new coinage §5's locked
	// decision refuses. No default on purpose: whoever writes the row
	// answers where the item came from.
	s := openTemp(t)
	seedRoadmapComponent(t, s, "prov", "/repo/prov")

	for _, bad := range []string{"webhook-created", "manual", "AUTHORED", "authored ", "", "agent"} {
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/prov", "t", "idea", bad, "now", "now"); err == nil {
			t.Fatalf("provenance %q was accepted — the three are closed", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("provenance %q refused for the wrong reason: %v", bad, err)
		}
	}
	for _, ok := range []string{"authored", "triggered", "triaged"} {
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/prov", "t", "idea", ok, "now", "now"); err != nil {
			t.Errorf("the legal provenance %q was refused: %v", ok, err)
		}
	}
	t.Run("absence is refused, not defaulted", func(t *testing.T) {
		// provenance is NOT NULL: a row that dodges the question is a row
		// D5 forbids.
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, created_at, updated_at)
			VALUES (?,?,?,?,?)`, "/repo/prov", "t", "idea", "now", "now"); err == nil {
			t.Fatal("a provenance-less row was accepted — NOT NULL is load-bearing for D5")
		} else if !strings.Contains(err.Error(), "NOT NULL") {
			t.Errorf("refused, but not on NOT NULL: %v", err)
		}
	})
}

func TestEstimateRangeAllPresentOrAbsent(t *testing.T) {
	// D3: an estimate is a range with its basis, never a bare figure —
	// the same all-present-or-all-absent CHECK 0041 gave the queue's
	// size, applied here so a partial estimate is unwritable at the
	// database rather than discouraged in Go.
	s := openTemp(t)
	seedRoadmapComponent(t, s, "est", "/repo/est")

	estSQL := `INSERT INTO roadmap_item
		(component_root, title, status, provenance, estimate_min, estimate_max, estimate_basis, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`
	const ok = "authored"

	one, two, three := 1, 2, 3
	cases := []struct {
		name     string
		min, max *int
		basis    *string
	}{
		{"only the min", &one, nil, nil},
		{"min and max, no basis", &one, &two, nil},
		{"basis with no bounds", nil, nil, strptr("three files, one day")},
		{"empty basis", &one, &two, strptr("")},
		{"min above max", &two, &one, strptr("reversed")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.w.Exec(estSQL, "/repo/est", tc.name, "idea", ok, tc.min, tc.max, tc.basis, "now", "now")
			if err == nil {
				t.Fatalf("%q was accepted — D3's never-a-bare-figure is not closed at the database", tc.name)
			}
			if !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Errorf("%q refused for the wrong reason: %v", tc.name, err)
			}
		})
	}
	t.Run("the complete range and the complete absence both write", func(t *testing.T) {
		if _, err := s.w.Exec(estSQL, "/repo/est", "full", "idea", ok, &one, &three, strptr("two files, half a day each"), "now", "now"); err != nil {
			t.Errorf("the honest range was refused: %v", err)
		}
		if _, err := s.w.Exec(roadmapItemSQL, "/repo/est", "none", "idea", ok, "now", "now"); err != nil {
			t.Errorf("no estimate yet must be the ordinary state: %v", err)
		}
	})
}

func TestRoadmapItemLoopSlugFKIsEnforced(t *testing.T) {
	// The "no sentinel, the column is simply unset" discipline S24's T1
	// established: NULL is the pre-materialization state (and satisfies
	// the composite FK); a slug that names no loop is a foreign key
	// violation, not a dangling pointer waiting to confuse the renderer.
	s := openTemp(t)
	seedRoadmapComponent(t, s, "fk", "/repo/fk")

	if _, err := s.w.Exec(roadmapItemSQL, "/repo/fk", "unmaterialized", "idea", "authored", "now", "now"); err != nil {
		t.Fatalf("the unset slug is the ordinary state: %v", err)
	}
	_, err := s.w.Exec(`INSERT INTO roadmap_item
		(component_root, title, status, provenance, loop_slug, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, "/repo/fk", "ghost", "running", "authored", "no-such-loop", "now", "now")
	if err == nil {
		t.Fatal("a loop_slug naming no loop was accepted — the composite FK is not enforced")
	} else if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Errorf("refused, but not on the FK: %v", err)
	}
}

func strptr(s string) *string { return &s }
