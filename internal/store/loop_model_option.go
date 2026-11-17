package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LoopModelOption is what a person chose for a loop's model after it started. Empty fields mean "no choice".
type LoopModelOption struct {
	LoopID          string
	ReasoningEffort string
	Thinking        string // "", "on", "off"
	SetAt           string
}

// LoopModelOptionGet reads a loop's options; present is false when the person never set any.
func (s *Store) LoopModelOptionGet(ctx context.Context, loopID string) (LoopModelOption, bool, error) {
	var o LoopModelOption
	err := s.r.QueryRowContext(ctx, `SELECT loop_id, reasoning_effort, thinking, set_at FROM loop_model_option WHERE loop_id = ?`, loopID).
		Scan(&o.LoopID, &o.ReasoningEffort, &o.Thinking, &o.SetAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LoopModelOption{}, false, nil
	}
	if err != nil {
		return LoopModelOption{}, false, fmt.Errorf("store: read the model options of loop %s: %w", loopID, err)
	}
	return o, true, nil
}

// LoopModelOptionSet replaces a loop's options. Both empty clears the row, so the route's own settings apply again.
func (s *Store) LoopModelOptionSet(ctx context.Context, loopID, effort, thinking string) error {
	if loopID == "" {
		return fmt.Errorf("store: LoopModelOptionSet needs a loop id")
	}
	if effort == "" && thinking == "" {
		_, err := s.w.ExecContext(ctx, `DELETE FROM loop_model_option WHERE loop_id = ?`, loopID)
		return err
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO loop_model_option (loop_id, reasoning_effort, thinking, set_at) VALUES (?,?,?,?)
		ON CONFLICT(loop_id) DO UPDATE SET reasoning_effort = excluded.reasoning_effort, thinking = excluded.thinking, set_at = excluded.set_at`,
		loopID, effort, thinking, s.Now())
	if err != nil {
		return fmt.Errorf("store: save the model options of loop %s: %w", loopID, err)
	}
	return nil
}
