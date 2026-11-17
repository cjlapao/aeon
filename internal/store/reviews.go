package store

// reviews.go — the decision ledger (OL8). review is APPEND-ONLY: there is no
// UPDATE and no DELETE function in this package for it, and
// records_guard_test.go greps the package to keep that true. A decision that
// can be edited is not a ledger, and the claimed-FROM answers in
// DecideGoal/DecideBrief read this table to name WHO decided first.

import (
	"database/sql"
	"fmt"
)

// Review is one recorded decision. Subject coordinates are (kind, id,
// version): a goal's version-2 approval never rewrites its version-1
// rejection, and a brief's history shows every rewrite that was judged.
type Review struct {
	ID             int64  `json:"id"`
	SubjectKind    string `json:"subjectKind"`
	SubjectID      string `json:"subjectId"`
	SubjectVersion int    `json:"subjectVersion"`
	Decision       string `json:"decision"`
	ByKind         string `json:"byKind"`
	ByID           string `json:"byId"`
	Reason         string `json:"reason"`
	DecidedAt      string `json:"decidedAt"`
}

// ListReviews returns a subject's decision history, oldest first — the order
// a person reading an audit trail expects.
func (s *Store) ListReviews(subjectKind, subjectID string) ([]Review, error) {
	rows, err := s.r.Query(`SELECT id, subject_kind, subject_id, subject_version, decision, by_kind, by_id, reason, decided_at
		FROM review WHERE subject_kind = ? AND subject_id = ? ORDER BY decided_at, id`, subjectKind, subjectID)
	if err != nil {
		return nil, fmt.Errorf("store: list reviews: %w", err)
	}
	defer rows.Close()
	var out []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.SubjectKind, &r.SubjectID, &r.SubjectVersion,
			&r.Decision, &r.ByKind, &r.ByID, &r.Reason, &r.DecidedAt); err != nil {
			return nil, fmt.Errorf("store: scan review: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// latestReview reads the most recent decision for one exact subject version.
// The claimed-FROM losers' answer names WHO decided and WHEN, and this is the
// read that supplies it.
func (s *Store) latestReview(subjectKind, subjectID string, version int) (Review, bool) {
	row := s.r.QueryRow(`SELECT id, subject_kind, subject_id, subject_version, decision, by_kind, by_id, reason, decided_at
		FROM review WHERE subject_kind = ? AND subject_id = ? AND subject_version = ?
		ORDER BY decided_at DESC, id DESC LIMIT 1`, subjectKind, subjectID, version)
	var r Review
	err := row.Scan(&r.ID, &r.SubjectKind, &r.SubjectID, &r.SubjectVersion,
		&r.Decision, &r.ByKind, &r.ByID, &r.Reason, &r.DecidedAt)
	if err != nil {
		// No prior decision, or a read failure too — the caller's answer is
		// the row's CURRENT state either way, so an unclaimed "nobody yet"
		// is the honest reading of both.
		return Review{}, false
	}
	return r, true
}

// reviewInsert writes one decision inside an already-open transaction (the
// only writer of review rows in this package, and only ever reached from
// DecideGoal/DecideBrief — records_guard_test.go pins the pair). by_kind is
// the literal 'user' because decideRefusal has already proven the actor
// human; the schema CHECK refuses anything else outright.
func reviewInsert(tx *sql.Tx, subjectKind, subjectID string, version int, decision, byID, reason, at string) error {
	_, err := tx.Exec(`INSERT INTO review (subject_kind,subject_id,subject_version,decision,by_kind,by_id,reason,decided_at)
		VALUES (?,?,?,?,'user',?,?,?)`, subjectKind, subjectID, version, decision, byID, reason, at)
	return err
}
