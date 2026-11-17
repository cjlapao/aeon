package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

// SaveRunningCheckpoint atomically replaces the running transcript, snapshot,
// and cost. A crash sees either the previous whole checkpoint or this one.
func (s *Store) SaveRunningCheckpoint(ctx context.Context, stepID int64, msgs []model.Message, manifest json.RawMessage, cost model.Cost) error {
	if verdict := model.ValidateTranscript(msgs); !verdict.OK {
		return fmt.Errorf("store: checkpoint transcript of step %d: %s", stepID, verdict.Reason)
	}
	usd, state, detail, err := encodeCost(cost)
	if err != nil {
		return err
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin checkpoint of step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM step_message WHERE step_id=?`, stepID); err != nil {
		return err
	}
	for i, msg := range msgs {
		content, err := marshalBlocks(msg)
		if err != nil {
			return err
		}
		prov, err := marshalProvenance(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
			VALUES (?,?,?,?,?,?,?)`, stepID, i+1, model.FormatVersion, string(msg.Role), content, prov, s.Now()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, setCostStatement, usd, state, detail, stepID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE step SET context_manifest=? WHERE id=? AND status='running'`, string(manifest), stepID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("store: checkpoint step %d is not running: affected %d: %v", stepID, n, err)
	}
	return tx.Commit()
}

// ContinueStepWithMessages marks a step running AND replaces its transcript in
// ONE transaction — S31 T2, the exactly-once property `step.continue {message}`
// needs (D6). Two separate writes would open the window that deletes this
// guarantee: flip-to-running then crash leaves a running row whose transcript
// lacks the message (lost); replace-then-crash leaves the message in a still
// suspended row, so a retried continue appends it AGAIN (duplicated). One
// transaction closes both: the message reaches the transcript exactly when the
// row stops saying `suspended`, a retried continue is refused by this very
// guard (the row is no longer suspended), and recovery replays from a row that
// already carries the message.
//
// The `status='suspended'` guard is also the race net: a caller that checked
// StepState and then raced (the step resumed under it) gets an error here
// rather than a corrupted transcript.
func (s *Store) ContinueStepWithMessages(ctx context.Context, stepID int64, msgs []model.Message) error {
	if verdict := model.ValidateTranscript(msgs); !verdict.OK {
		return fmt.Errorf("store: continuation transcript of step %d: %s", stepID, verdict.Reason)
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin continuation of step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()
	// The flip first: if the step is not suspended, nothing else in this
	// transaction touches it and the rollback is a no-op read.
	res, err := tx.ExecContext(ctx,
		`UPDATE step SET status='running', outcome=NULL, pause_reason=NULL WHERE id=? AND status='suspended'`, stepID)
	if err != nil {
		return fmt.Errorf("store: continue step %d: %w", stepID, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("store: continue step %d: the step is no longer suspended (affected %d): %v", stepID, n, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM step_message WHERE step_id=?`, stepID); err != nil {
		return err
	}
	for i, msg := range msgs {
		content, err := marshalBlocks(msg)
		if err != nil {
			return err
		}
		prov, err := marshalProvenance(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
			VALUES (?,?,?,?,?,?,?)`, stepID, i+1, model.FormatVersion, string(msg.Role), content, prov, s.Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
