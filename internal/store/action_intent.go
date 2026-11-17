package store

// action_intent — the ONE table holding action intents (D7, Q6). S16's
// receipt-first action (forge.postReview) writes an intent before the
// outward post and records the returned review id after; a crash between
// the two leaves review_id NULL, which means "posted? unknown — go and
// look", and the resolver reconciles against the forge BY ID (never by
// body text). S7's tool_receipt is a different shape answered by a
// different path; the invariant is that action intents have exactly one
// table and therefore one recovery path.

import (
	"context"
	"database/sql"
	"fmt"
)

// ActionIntent is one receipt row. ReviewID 0 means the column is NULL:
// the post's outcome is unknown and reconciliation must look.
type ActionIntent struct {
	ID          int64  `json:"id"`
	LoopSlug    string `json:"loopSlug"`
	StepID      int64  `json:"stepId"`
	Attempt     int    `json:"attempt"`
	Action      string `json:"action"`
	PR          int    `json:"pr"`
	PreReviewID int64  `json:"preReviewId"`
	ReviewID    int64  `json:"reviewId,omitempty"`
	OpenedAt    string `json:"openedAt"`
	RecordedAt  string `json:"recordedAt,omitempty"`
}

// BeginActionIntent writes the intent BEFORE the effect happens, in its
// own transaction — a receipt that commits after the post is a receipt
// that can be lost with the post. A second begin for the same (step,
// attempt, action, pr) fails under the unique constraint; it never
// overwrites an existing row (an overwrite would erase the very evidence
// recovery reads).
func (s *Store) BeginActionIntent(ctx context.Context, in ActionIntent) error {
	if in.LoopSlug == "" || in.StepID <= 0 || in.Attempt < 1 || in.Action == "" || in.PR < 1 || in.PreReviewID < 0 {
		return fmt.Errorf("store: begin action intent needs loop, step, attempt>=1, action, pr>=1, pre_review_id>=0")
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO action_intent
		(loop_slug, step_id, attempt, action, pr, pre_review_id, opened_at) VALUES (?,?,?,?,?,?,?)`,
		in.LoopSlug, in.StepID, in.Attempt, in.Action, in.PR, in.PreReviewID, s.Now())
	if err != nil {
		return fmt.Errorf("store: begin action intent step %d attempt %d %s pr %d: %w", in.StepID, in.Attempt, in.Action, in.PR, err)
	}
	return nil
}

// RecordActionReview records the returned review id — step 3 of
// intent → post → record. It fills ONLY an unfilled row: re-recording is
// refused rather than silently rewriting history.
func (s *Store) RecordActionReview(ctx context.Context, stepID int64, attempt int, action string, pr int, reviewID int64) error {
	if stepID <= 0 || attempt < 1 || action == "" || pr < 1 || reviewID <= 0 {
		return fmt.Errorf("store: record action review needs step, attempt>=1, action, pr>=1, reviewId>0")
	}
	res, err := s.w.ExecContext(ctx, `UPDATE action_intent SET review_id=?, recorded_at=?
		WHERE step_id=? AND attempt=? AND action=? AND pr=? AND review_id IS NULL`,
		reviewID, s.Now(), stepID, attempt, action, pr)
	if err != nil {
		return fmt.Errorf("store: record action review step %d pr %d: %w", stepID, pr, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: count recorded intent: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("store: no open action intent for step %d attempt %d %s pr %d to record", stepID, attempt, action, pr)
	}
	return nil
}

// OpenActionIntent returns the (step, attempt, action, pr) intent whose
// review id is still NULL — the "go and look" signal. Absence is (nil,
// nil): no open intent is an answer, not a failure.
func (s *Store) OpenActionIntent(ctx context.Context, stepID int64, attempt int, action string, pr int) (*ActionIntent, error) {
	row := s.w.QueryRowContext(ctx, `SELECT id, loop_slug, step_id, attempt, action, pr, pre_review_id,
		COALESCE(review_id, 0), opened_at, COALESCE(recorded_at, '')
		FROM action_intent WHERE step_id=? AND attempt=? AND action=? AND pr=? AND review_id IS NULL`,
		stepID, attempt, action, pr)
	var in ActionIntent
	var rec sql.NullString
	if err := row.Scan(&in.ID, &in.LoopSlug, &in.StepID, &in.Attempt, &in.Action, &in.PR,
		&in.PreReviewID, &in.ReviewID, &in.OpenedAt, &rec); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("store: read open action intent step %d pr %d: %w", stepID, pr, err)
	}
	if rec.Valid {
		in.RecordedAt = rec.String
	}
	return &in, nil
}

// CountActionIntents reports how many action-intent rows the store
// holds — the audit action's read-only count (design.md: audit is
// "read-only over the store"). The consumer is the daemon's action
// Services: `aeon action list` shows audit, and a loop that runs it
// gets the honest number of actions the store remembers.
func (s *Store) CountActionIntents() (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM action_intent`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
