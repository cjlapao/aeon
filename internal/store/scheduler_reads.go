package store

// scheduler_reads.go — S27 T6: the two reads the cross-slice seams need,
// kept out of scheduler.go (whose 907 lines stay under the line). Both are
// plain facts for the daemon's adapters: the item's declared autonomy cap
// (CN3's narrowing) and the loops a stopAll command must put down (CN18).

import (
	"errors"
	"fmt"
)

// ItemAutonomyCap reads one item's declared narrowing. The vocabulary is the
// T2 CHECK on both tables: grant (the default), outward, none. An unknown
// subject kind is a programming error, not a miss — the scheduler starts
// queue and roadmap items and nothing else.
func (s *Store) ItemAutonomyCap(subjectKind string, subjectID int64) (string, error) {
	table := ""
	switch subjectKind {
	case "queue":
		table = "queue_item"
	case "roadmap":
		table = "roadmap_item"
	default:
		return "", fmt.Errorf("store: item autonomy cap: subject kind %q files no cap", subjectKind)
	}
	var cap string
	err := s.r.QueryRow(`SELECT autonomy_cap FROM `+table+` WHERE id = ?`, subjectID).Scan(&cap)
	if err != nil {
		return "", fmt.Errorf("store: item autonomy cap for %s %d: %w", subjectKind, subjectID, err)
	}
	return cap, nil
}

// SchedulerLoopToStop is one loop CN18's stopAll command must put down: a
// loop the scheduler started that is still live. Blocked and parked loops
// are NOT among them — a parked loop is S55's to raise (CN21: the scheduler
// never restarts and never un-parks).
type SchedulerLoopToStop struct {
	ID            string
	Slug          string
	Status        string
	ComponentRoot string
}

// SchedulerLoopsToStop answers them in a stable order (root, then slug) so
// one stopAll ledger reads the same twice.
func (s *Store) SchedulerLoopsToStop() ([]SchedulerLoopToStop, error) {
	rows, err := s.r.Query(`SELECT id, slug, status, component_root FROM loop
		WHERE started_by = 'scheduler' AND status IN ('running','waiting')
		ORDER BY component_root, slug`)
	if err != nil {
		return nil, errors.Join(errors.New("store: read the scheduler's live loops"), err)
	}
	defer rows.Close()
	var out []SchedulerLoopToStop
	for rows.Next() {
		var l SchedulerLoopToStop
		if err := rows.Scan(&l.ID, &l.Slug, &l.Status, &l.ComponentRoot); err != nil {
			return nil, errors.Join(errors.New("store: scan a scheduler loop to stop"), err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetItemAutonomyCap narrows one item's declared cap — the vocabulary is the
// T2 CHECK, and only narrowing words are ever written here (a widening has
// no representation: cap is an item's own promise, never a grant). The
// planner surface (T8) is the plane's person-facing door; this is the one
// write the scheduler's seams and their tests seed through.
func (s *Store) SetItemAutonomyCap(subjectKind string, subjectID int64, cap string) error {
	table := ""
	switch subjectKind {
	case "queue":
		table = "queue_item"
	case "roadmap":
		table = "roadmap_item"
	default:
		return fmt.Errorf("store: item autonomy cap: subject kind %q files no cap", subjectKind)
	}
	switch cap {
	case "grant", "outward", "none":
	default:
		return fmt.Errorf("store: item autonomy cap %q is not grant, outward or none", cap)
	}
	res, err := s.w.Exec(`UPDATE `+table+` SET autonomy_cap = ? WHERE id = ?`, cap, subjectID)
	if err != nil {
		return fmt.Errorf("store: set item autonomy cap for %s %d: %w", subjectKind, subjectID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: set item autonomy cap for %s %d: %w", subjectKind, subjectID, ErrNotFound)
	}
	return nil
}
