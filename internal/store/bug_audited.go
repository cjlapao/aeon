package store

// ════════════════════════════════════════════════════════════════════════════
// Audited bug writes — the ONE write path every production bug mutation
// travels (S51 T1, TM2).
//
// Each method is one transaction on the single writer (BugWithTx): the bug
// write and its bug_change row commit together or not at all, so an audit
// can never name a write the table did not make, and a refused audit rolls
// the write back with it (TestAuditAndWriteAreOneTransaction is the proof,
// not the comment). No-op writes — the CAS lost, the row absent — earn NO
// audit row: nothing happened, and an audit of nothing is noise that buries
// the signal (PM8's rule, kept).
//
// The SQL is SHARED with the plain writers in bug.go through the unexported
// *Stmt helpers, so the two paths cannot drift (the house rule for a write
// with two execution points).
//
// Each writer OWNS its op — file, transition, priority, duplicate, handoff
// are facts of the writer, not caller's options; `update` arrives with the
// title/body editor (S51 T2) and `validate` is appended outside any pair
// (BugChangeAppend, the one exception). The door composes the fixed-
// template summary; these refuse malformed ACTORS and AUDITS before SQL —
// including TM2's net: an agent actor on anything but `file` dies here
// first and at the schema CHECK second.
// ════════════════════════════════════════════════════════════════════════════

import (
	"database/sql"
	"fmt"
)

// CreateBugAudited adds one bug entering at `unvalidated` and appends its
// 'file' audit row in the same transaction. The audit's BugID is set HERE
// (the id does not exist until the INSERT lands); the caller owns the actor.
func (s *Store) CreateBugAudited(in NewBugRow, a BugAudit) (BugRow, error) {
	if in.ComponentRoot == "" || in.Title == "" || in.Body == "" {
		return BugRow{}, fmt.Errorf("store: a bug needs a component root, a title and a body — the report itself is the body")
	}
	if in.FiledBy == "" {
		return BugRow{}, fmt.Errorf("store: a bug names who filed it — an agent name or 'person' (D7); a claim with no filer is a record that cannot answer who looked")
	}
	if in.PriorityState == "" {
		in.PriorityState = "unknown"
	}
	if in.PriorityState == "unknown" && in.PriorityLabel != nil {
		return BugRow{}, fmt.Errorf("store: a priority label on an unknown priority contradicts itself (D2) — set the state or drop the label")
	}
	a.BugID = 0 // composed below, once the id exists
	a.Op = "file"
	now := s.Now()
	var id int64
	err := s.BugWithTx(func(tx *sql.Tx) error {
		var err error
		id, err = createBugInsert(tx, in, now)
		if err != nil {
			return err
		}
		a.BugID = id
		a.Summary = fmt.Sprintf("filed bug %d", id) // the fixed template (TM2)
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return BugRow{}, err
	}
	row, ok, err := s.BugByID(id)
	if err != nil {
		return BugRow{}, err
	}
	if !ok {
		return BugRow{}, fmt.Errorf("store: bug %d vanished between commit and read", id)
	}
	return row, nil
}

// TransitionBugAudited is the claimed-FROM move WITH its 'transition' audit
// in the same transaction. The loser of a race (affected 0) commits nothing
// — no move, and no audit row for a move that did not happen. The reactor's
// moves carry actor {system, reactor}: a person can see WHAT MOVED ON ITS
// OWN (TM2); the door composes the fixed-template summary.
func (s *Store) TransitionBugAudited(id int64, from, to string, a BugAudit) (bool, error) {
	a.BugID = id
	a.Op = "transition"
	now := s.Now()
	claimed := false
	err := s.BugWithTx(func(tx *sql.Tx) error {
		moved, err := transitionBugStmt(tx, id, from, to, now)
		if err != nil {
			return err
		}
		if !moved {
			return nil // the honest loser: nothing moved, nothing to audit
		}
		claimed = true
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// SetBugPriorityAudited writes the state/label pair TOGETHER (D2 unchanged)
// and audits 'priority' beside it, one transaction.
func (s *Store) SetBugPriorityAudited(id int64, componentRoot, state string, label *string, a BugAudit) (bool, error) {
	if state == "unknown" && label != nil {
		return false, fmt.Errorf("store: bug %d: a priority label on an unknown priority contradicts itself (D2)", id)
	}
	a.BugID = id
	a.Op = "priority"
	now := s.Now()
	wrote := false
	err := s.BugWithTx(func(tx *sql.Tx) error {
		done, err := setBugPriorityStmt(tx, id, componentRoot, state, label, now)
		if err != nil {
			return err
		}
		if !done {
			return nil
		}
		wrote = true
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// SetBugDuplicateOfAudited is the ONLY write path to duplicate_of (D5/N3),
// now carrying its 'duplicate' audit in the same transaction. The actor law
// says who may ask: user or cli — a person at a door; the door's own
// human-originated check stays beside it.
func (s *Store) SetBugDuplicateOfAudited(id int64, componentRoot string, of *int64, a BugAudit) (bool, error) {
	a.BugID = id
	a.Op = "duplicate"
	now := s.Now()
	wrote := false
	err := s.BugWithTx(func(tx *sql.Tx) error {
		done, err := setBugDuplicateOfStmt(tx, id, componentRoot, of, now)
		if err != nil {
			return err
		}
		if !done {
			return nil
		}
		wrote = true
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// SetBugRoadmapItemIDAudited records the hand-off (D8) and audits 'handoff'
// beside it. The stamp-once rule is the caller's (the door refuses a second
// hand-off); a zero-affected record audits nothing.
func (s *Store) SetBugRoadmapItemIDAudited(id int64, componentRoot string, roadmapItemID int64, a BugAudit) (bool, error) {
	a.BugID = id
	a.Op = "handoff"
	now := s.Now()
	wrote := false
	err := s.BugWithTx(func(tx *sql.Tx) error {
		done, err := setBugRoadmapItemIDStmt(tx, id, componentRoot, roadmapItemID, now)
		if err != nil {
			return err
		}
		if !done {
			return nil
		}
		wrote = true
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// UpdateBugFieldsAudited edits title and/or body — the ONLY editable fields
// (TM1: state, priority, duplicate_of, roadmap_item_id, filed_by and the
// timestamps except updated_at are untouched) — and audits 'update' beside
// it, one transaction. The writer OWNS the op and composes the summary
// ITSELF from the field NAMES that were written: "edited bug N (title)",
// "(body)", "(title, body)" — a fixed template, no user text rides it
// (SEC-033). The caller checked the values; zero pointers is refused here
// too, so an audit naming no field is impossible.
func (s *Store) UpdateBugFieldsAudited(id int64, title, body *string, a BugAudit) (bool, error) {
	if title == nil && body == nil {
		return false, fmt.Errorf("store: bug %d: an update names at least one field — an edit of nothing earns no audit row", id)
	}
	a.BugID = id
	a.Op = "update"
	fields := "title, body"
	if title == nil {
		fields = "body"
	} else if body == nil {
		fields = "title"
	}
	a.Summary = fmt.Sprintf("edited bug %d (%s)", id, fields) // the fixed template (TM1)
	now := s.Now()
	wrote := false
	err := s.BugWithTx(func(tx *sql.Tx) error {
		done, err := updateBugFieldsStmt(tx, id, title, body, now)
		if err != nil {
			return err
		}
		if !done {
			return nil // no row, no edit, no audit
		}
		wrote = true
		return BugChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}
