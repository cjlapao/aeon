// goal_criteria.go — S61 T1 (GI5): acceptance criteria as goal DATA, versioned
// with the goal. The decision GI5 argues is (c) — a separate versioned table,
// not JSON on `goal` (that would touch the goal DDL S44-extra rebuilt) and not
// `brief.acceptance` (a brief exists per loop only after approval, so it
// cannot be the pre-work definition of done).
//
// Criteria belong to a (goal, version) pair exactly like the component set
// does: UpdateGoal copies them forward to the new version beside
// goal_component (goals.go), so an edit to the TEXT never silently redefines
// the definition of done. ReplaceGoalCriteria is the draft-time rewrite —
// rows for one (goal, version) are replaced wholly or not at all.
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// allCriterionChecks is the closed check_kind set (GI5): how a criterion will
// be shown satisfied. NULL (empty in Go) is a real third state — unchecked,
// never "manual" by accident of omission.
var allCriterionChecks = map[string]bool{
	"manual": true, "command": true, "test": true,
}

const (
	maxCriteriaPerGoal = 100 // GI2's acceptance[] cap — one truth, enforced at both doors
	maxCriterionText   = 512
)

// Criterion is one acceptance criterion at one goal version.
type Criterion struct {
	CriterionID string `json:"criterionId"` // the charter's stable 'ac-<n>' id, carried through
	Ordinal     int    `json:"ordinal"`     // 1-based, assigned here
	Text        string `json:"text"`
	CheckKind   string `json:"checkKind,omitempty"` // manual | command | test | "" = NULL (unchecked)
	CheckRef    string `json:"checkRef,omitempty"`  // command string / test name; stored, never executed here
}

// ReplaceGoalCriteria writes the whole criterion set of one (goal, version) —
// delete-then-insert inside ONE transaction, so a refusal mid-set leaves the
// old rows untouched and a committed set is always the set named. Ordinals
// are assigned 1..n in the given order; ids given by the caller are carried,
// empty ones minted as 'ac-<n>' off the ordinal.
func (s *Store) ReplaceGoalCriteria(goalID string, version int, criteria []Criterion) (*Refusal, error) {
	if version < 1 {
		return &Refusal{Kind: "version-unknown",
			Sentence: fmt.Sprintf("refused: goal version %d is below 1 — goal versions start at 1", version)}, nil
	}
	if len(criteria) > maxCriteriaPerGoal {
		return &Refusal{Kind: "criteria-too-many",
			Sentence: fmt.Sprintf("refused: %d criteria — a goal carries at most %d (GI2)", len(criteria), maxCriteriaPerGoal)}, nil
	}
	seen := map[string]bool{}
	for i, c := range criteria {
		if c.Text == "" {
			return &Refusal{Kind: "criterion-text",
				Sentence: fmt.Sprintf("refused: criterion %d is empty — every criterion is one line a person can point at", i+1)}, nil
		}
		if len(c.Text) > maxCriterionText {
			return &Refusal{Kind: "criterion-text",
				Sentence: fmt.Sprintf("refused: criterion %d is over %d characters — every criterion is one line", i+1, maxCriterionText)}, nil
		}
		if c.CheckKind != "" && !allCriterionChecks[c.CheckKind] {
			return &Refusal{Kind: "check-kind-unknown",
				Sentence: fmt.Sprintf("refused: check kind %q is not one of manual, command, test (empty means unchecked)", c.CheckKind)}, nil
		}
		id := c.CriterionID
		if id == "" {
			id = fmt.Sprintf("ac-%d", i+1)
		}
		if seen[id] {
			return &Refusal{Kind: "criterion-id-duplicate",
				Sentence: fmt.Sprintf("refused: criterion id %q appears twice — ids are stable and unique inside a version", id)}, nil
		}
		seen[id] = true
	}

	tx, err := s.w.Begin()
	if err != nil {
		return nil, fmt.Errorf("store: replace criteria: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The goal version must exist — criteria of a version nobody wrote are
	// orphans waiting for a rename.
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM goal WHERE id = ? AND version = ?`, goalID, version).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &Refusal{Kind: "goal-version-unknown",
				Sentence: fmt.Sprintf("refused: goal %s has no version %d — criteria attach to a version that exists", goalID, version)}, nil
		}
		return nil, fmt.Errorf("store: check goal %s v%d: %w", goalID, version, err)
	}
	if _, err := tx.Exec(`DELETE FROM goal_criterion WHERE goal_id = ? AND goal_version = ?`, goalID, version); err != nil {
		return nil, fmt.Errorf("store: clear criteria of %s v%d: %w", goalID, version, err)
	}
	for i, c := range criteria {
		id := c.CriterionID
		if id == "" {
			id = fmt.Sprintf("ac-%d", i+1)
		}
		if _, err := tx.Exec(`INSERT INTO goal_criterion (goal_id,goal_version,ordinal,criterion_id,text,check_kind,check_ref)
			VALUES (?,?,?,?,?,?,?)`, goalID, version, i+1, id, c.Text, nullable(c.CheckKind), nullable(c.CheckRef)); err != nil {
			return nil, fmt.Errorf("store: insert criterion %s of %s v%d: %w", id, goalID, version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: replace criteria: %w", err)
	}
	return nil, nil
}

// GoalCriteria reads one version's set, ordinal order — the order the person
// wrote it in is part of the text.
func (s *Store) GoalCriteria(goalID string, version int) ([]Criterion, error) {
	rows, err := s.r.Query(`SELECT criterion_id, ordinal, text,
			COALESCE(check_kind,''), COALESCE(check_ref,'')
		FROM goal_criterion WHERE goal_id = ? AND goal_version = ? ORDER BY ordinal`, goalID, version)
	if err != nil {
		return nil, fmt.Errorf("store: list criteria of %s: %w", goalID, err)
	}
	defer rows.Close()
	var out []Criterion
	for rows.Next() {
		var c Criterion
		if err := rows.Scan(&c.CriterionID, &c.Ordinal, &c.Text, &c.CheckKind, &c.CheckRef); err != nil {
			return nil, fmt.Errorf("store: scan criterion: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
