package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Receipt records one unsafe tool call. A nil CompletedAt means its outcome
// is unknown; a non-nil Result containing "" means it completed with empty output.
type Receipt struct {
	ID          int64   `json:"id"`
	StepID      int64   `json:"stepId"`
	ToolUseID   string  `json:"toolUseId"`
	Tool        string  `json:"tool"`
	ArgDigest   string  `json:"argDigest"`
	StartedAt   string  `json:"startedAt"`
	CompletedAt *string `json:"completedAt,omitempty"`
	Result      *string `json:"result,omitempty"`
	IsError     *bool   `json:"isError,omitempty"`
}

// ArgDigest binds a tool's name and exact JSON input bytes. Raw NUL cannot
// occur in valid JSON, and tool names cannot contain NUL, so it separates the
// two fields without an ambiguous concatenation.
func ArgDigest(tool string, input json.RawMessage) string {
	h := sha256.New()
	_, _ = h.Write([]byte(tool))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(input)
	return hex.EncodeToString(h.Sum(nil))
}

// BeginReceipt commits a receipt before an unsafe tool starts. A second begin
// for the same call fails under the unique constraint; it never overwrites.
func (s *Store) BeginReceipt(ctx context.Context, stepID int64, toolUseID, tool, digest string) error {
	if stepID <= 0 || toolUseID == "" || tool == "" || digest == "" {
		return fmt.Errorf("store: begin receipt needs a step, tool use, tool and digest")
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO tool_receipt
		(step_id, tool_use_id, tool, arg_digest, started_at) VALUES (?,?,?,?,?)`,
		stepID, toolUseID, tool, digest, s.Now())
	if err != nil {
		return fmt.Errorf("store: begin receipt for step %d tool %s: %w", stepID, toolUseID, err)
	}
	return nil
}

// CompleteReceipt captures the result before it is added to the transcript.
func (s *Store) CompleteReceipt(ctx context.Context, stepID int64, toolUseID, result string, isError bool) error {
	res, err := s.w.ExecContext(ctx, `UPDATE tool_receipt SET completed_at=?, result=?, is_error=?
		WHERE step_id=? AND tool_use_id=? AND completed_at IS NULL`, s.Now(), result, isError, stepID, toolUseID)
	if err != nil {
		return fmt.Errorf("store: complete receipt for step %d tool %s: %w", stepID, toolUseID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: receipt for step %d tool %s is absent or already completed", stepID, toolUseID)
	}
	return nil
}

// Receipts returns every receipt for a step in creation order.
func (s *Store) Receipts(ctx context.Context, stepID int64) ([]Receipt, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, step_id, tool_use_id, tool, arg_digest, started_at,
		completed_at, result, is_error FROM tool_receipt WHERE step_id=? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read receipts for step %d: %w", stepID, err)
	}
	defer rows.Close()
	out := []Receipt{}
	for rows.Next() {
		var r Receipt
		var completed, result sql.NullString
		var isError sql.NullBool
		if err := rows.Scan(&r.ID, &r.StepID, &r.ToolUseID, &r.Tool, &r.ArgDigest, &r.StartedAt, &completed, &result, &isError); err != nil {
			return nil, fmt.Errorf("store: scan receipt for step %d: %w", stepID, err)
		}
		if completed.Valid {
			r.CompletedAt = &completed.String
		}
		if result.Valid {
			r.Result = &result.String
		}
		if isError.Valid {
			r.IsError = &isError.Bool
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
