package store

// ════════════════════════════════════════════════════════════════════════════
// Audited roadmap writes — the ONE write path every production roadmap
// mutation travels (S50 T1, RM2).
//
// Each method is one transaction on the single writer (RoadmapWithTx): the
// item write and its roadmap_change row commit together or not at all, so
// an audit can never name a write the table did not make, and a refused
// audit rolls the write back with it (TestAuditAndWriteAreOneTransaction is
// the proof, not the comment). No-op writes — the CAS lost, the row absent
// — earn NO audit row: nothing happened, and an audit of nothing is noise
// that buries the signal (PM8's rule, kept).
//
// The SQL is SHARED with the ctx variants in roadmap.go by const, so the
// two paths cannot drift (the house rule for a write with two execution
// points). The shape checks are shared the same way.
//
// These methods do NOT validate the transition machine — ValidateTransition
// and IsReady belong to the door (internal/roadmap), the one machine. These
// refuse malformed ACTORS and AUDITS before SQL, and refuse the same row
// shapes the plain writers refuse.
// ════════════════════════════════════════════════════════════════════════════

import (
	"database/sql"
	"fmt"
)

// CreateRoadmapItemAudited adds one item at `idea` and appends its 'add'
// audit row in the same transaction. The audit's ItemID is set HERE (the id
// does not exist until the INSERT lands); callers own op/summary/actor.
func (s *Store) CreateRoadmapItemAudited(in NewRoadmapItemRow, a RoadmapAudit) (RoadmapItemRow, error) {
	if err := checkNewRoadmapItemShape(in); err != nil {
		return RoadmapItemRow{}, err
	}
	if in.PriorityState == "" {
		in.PriorityState = "unknown"
	}
	a.Op = "add"   // this writer's op is a fact, not a caller's option
	a.Summary = "" // composed below, once the id exists — "added item %d" is the fixed template (RM2)
	now := s.Now()
	var id int64
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(createRoadmapItemSQL,
			in.ComponentRoot, in.Title, in.Epic, "idea", in.PriorityState, in.PriorityLabel,
			in.EstimateMin, in.EstimateMax, in.EstimateBasis, in.Provenance, in.BriefPath, in.PlanPath,
			now, now)
		if err != nil {
			return fmt.Errorf("store: create roadmap item in %s: %w", in.ComponentRoot, err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: create roadmap item id: %w", err)
		}
		a.ItemID = id
		a.Summary = fmt.Sprintf("added item %d", id)
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return RoadmapItemRow{}, err
	}
	row, ok, err := s.RoadmapItemByID(id)
	if err != nil {
		return RoadmapItemRow{}, err
	}
	if !ok {
		return RoadmapItemRow{}, fmt.Errorf("store: roadmap item %d vanished between commit and read", id)
	}
	return row, nil
}

// TransitionRoadmapItemAudited is the claimed-FROM move WITH its audit in
// the same transaction. The loser of a race (affected 0) commits nothing —
// no move, and no audit row for a move that did not happen. `op` names the
// verb the move serves ('transition', 'ideate', 'plan', 'materialize',
// 'cancel'); the door composes the fixed-template summary.
func (s *Store) TransitionRoadmapItemAudited(id int64, from, to string, a RoadmapAudit) (bool, error) {
	a.ItemID = id
	now := s.Now()
	claimed := false
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(transitionRoadmapItemSQL, to, now, id, from)
		if err != nil {
			return fmt.Errorf("store: transition roadmap item %d %s→%s: %w", id, from, to, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: transition roadmap item %d: %w", id, err)
		}
		if n != 1 {
			return nil // the honest loser: nothing moved, nothing to audit
		}
		claimed = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// RoadmapItemLandingAudited is the plan/brief landing: the pointer(s) and
// the status move in ONE transaction earning exactly ONE audit row — the
// reactor lands the artifact and the status as a single fact ("the brief
// landed, so the item is brief-agreed"), and two rows for one landing would
// make the history read as two events. The CAS still decides: an item that
// moved under the landing earns no audit.
func (s *Store) RoadmapItemLandingAudited(id int64, briefPath, planPath *string, from, to string, a RoadmapAudit) (bool, error) {
	if err := checkRoadmapArtifactPointers(id, briefPath, planPath); err != nil {
		return false, err
	}
	sets, args := roadmapArtifactSets(briefPath, planPath)
	a.ItemID = id
	now := s.Now()
	claimed := false
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(transitionRoadmapItemSQL, to, now, id, from)
		if err != nil {
			return fmt.Errorf("store: land roadmap item %d %s→%s: %w", id, from, to, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: land roadmap item %d: %w", id, err)
		}
		if n != 1 {
			return nil // the honest loser: the item moved under the landing — no pointers land either
		}
		if len(sets) > 0 {
			if _, err := tx.Exec(`UPDATE roadmap_item SET `+joinRoadmapSets(sets)+` WHERE id = ?`,
				append(args, now, id)...); err != nil {
				return fmt.Errorf("store: land roadmap item %d artifacts: %w", id, err)
			}
		}
		claimed = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// SetRoadmapItemEstimateAudited writes the whole range with its basis and
// audits 'estimate' beside it, one transaction (D3's law unchanged).
func (s *Store) SetRoadmapItemEstimateAudited(id int64, min, max int, basis string, a RoadmapAudit) (bool, error) {
	if err := checkRoadmapEstimateShape(id, min, max, basis); err != nil {
		return false, err
	}
	a.ItemID = id
	now := s.Now()
	wrote := false
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(setRoadmapEstimateSQL, min, max, basis, now, id)
		if err != nil {
			return fmt.Errorf("store: set roadmap item %d estimate: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: set roadmap item %d estimate: %w", id, err)
		}
		if n != 1 {
			return nil
		}
		wrote = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// SetRoadmapItemPriorityAudited writes the pair TOGETHER and audits
// 'priority' beside it (D2 unchanged).
func (s *Store) SetRoadmapItemPriorityAudited(id int64, state string, label *string, a RoadmapAudit) (bool, error) {
	a.ItemID = id
	now := s.Now()
	wrote := false
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(setRoadmapPrioritySQL, state, label, now, id)
		if err != nil {
			return fmt.Errorf("store: set roadmap item %d priority: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: set roadmap item %d priority: %w", id, err)
		}
		if n != 1 {
			return nil
		}
		wrote = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// SetRoadmapItemLoopAudited records the materialization link and audits
// 'materialize' beside it. The claimed guard is unchanged: the record lands
// only while the item still wears requireStatus.
func (s *Store) SetRoadmapItemLoopAudited(id int64, componentRoot, loopSlug, requireStatus string, a RoadmapAudit) (bool, error) {
	a.ItemID = id
	now := s.Now()
	wrote := false
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(setRoadmapLoopSQL, loopSlug, now, id, componentRoot, requireStatus)
		if err != nil {
			return fmt.Errorf("store: record roadmap item %d loop %s: %w", id, loopSlug, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: record roadmap item %d loop: %w", id, err)
		}
		if n != 1 {
			return nil
		}
		wrote = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}

// RoadmapItemEdit is the narrow named set RM1 allows an edit to touch:
// title, epic, and the two artifact pointers — each nil means UNSET (do
// not touch), a pointer means write THIS value (epic clears by pointing
// at the empty string; paths never point at empty, the sentinel rule
// above stands). Status, priority, estimate and provenance are absent by
// type: the edit cannot reach them, so no caller can.
type RoadmapItemEdit struct {
	Title     *string
	Epic      *string
	BriefPath *string
	PlanPath  *string
}

// Empty reports whether the edit names nothing — the caller's
// changed:false answer comes from here, before any SQL.
func (e RoadmapItemEdit) Empty() bool {
	return e.Title == nil && e.Epic == nil && e.BriefPath == nil && e.PlanPath == nil
}

// UpdateRoadmapItemAudited writes the RM1 field set and its 'update' audit
// row in ONE transaction. The audit op is this writer's fact, not a
// caller's option — the nine admit 'update' precisely for this door. A
// lost race (the item vanished under a zero-affected UPDATE) audits
// nothing: the write did not happen.
func (s *Store) UpdateRoadmapItemAudited(id int64, e RoadmapItemEdit, a RoadmapAudit) (bool, error) {
	if err := checkRoadmapArtifactPointers(id, e.BriefPath, e.PlanPath); err != nil {
		return false, err
	}
	if e.Title != nil && *e.Title == "" {
		return false, fmt.Errorf("store: roadmap item %d: clearing a title is not an edit — a row without a name is not a plan of intent", id)
	}
	var sets []string
	var args []any
	if e.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *e.Title)
	}
	if e.Epic != nil {
		sets = append(sets, "epic = ?")
		args = append(args, *e.Epic)
	}
	if e.BriefPath != nil {
		sets = append(sets, "brief_path = ?")
		args = append(args, *e.BriefPath)
	}
	if e.PlanPath != nil {
		sets = append(sets, "plan_path = ?")
		args = append(args, *e.PlanPath)
	}
	if len(sets) == 0 {
		return false, nil
	}
	a.ItemID = id
	a.Op = "update"
	wrote := false
	now := s.Now()
	err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE roadmap_item SET `+joinRoadmapSets(sets)+` WHERE id = ?`,
			append(args, now, id)...)
		if err != nil {
			return fmt.Errorf("store: update roadmap item %d: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: update roadmap item %d: %w", id, err)
		}
		if n != 1 {
			return nil
		}
		wrote = true
		return RoadmapChangeInsertTx(tx, a, now)
	})
	if err != nil {
		return false, err
	}
	return wrote, nil
}
