package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// loop_access.go — a loop's permission mode (the chat's "full access" switch and an unattended loop's
// pre-approved tools). A row exists only while a person has turned something on: no row is the default, ask.
// It is written only through the human door (the daemon's loop.setAccess, which stamps the connection's actor),
// and read by step assembly, which hands the runtime plain data.

// The two modes a row can hold. "ask" is the ABSENCE of a row, never a stored value.
const (
	LoopAccessTools = "tools"
	LoopAccessFull  = "full"
)

// LoopAccess is one loop's stored permission mode.
type LoopAccess struct {
	LoopID    string
	Mode      string
	Tools     []string
	SetByKind string
	SetByID   string
	SetAt     string
}

// LoopAccessGet reads a loop's mode; present is false when the loop is on the default (ask).
func (s *Store) LoopAccessGet(ctx context.Context, loopID string) (LoopAccess, bool, error) {
	if loopID == "" {
		return LoopAccess{}, false, fmt.Errorf("store: LoopAccessGet needs a loop id")
	}
	var a LoopAccess
	var tools string
	err := s.r.QueryRowContext(ctx, `SELECT loop_id, mode, tools_json, set_by_kind, set_by_id, set_at FROM loop_access WHERE loop_id = ?`, loopID).
		Scan(&a.LoopID, &a.Mode, &tools, &a.SetByKind, &a.SetByID, &a.SetAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LoopAccess{}, false, nil
	}
	if err != nil {
		return LoopAccess{}, false, fmt.Errorf("store: read the access mode of loop %s: %w", loopID, err)
	}
	if err := json.Unmarshal([]byte(tools), &a.Tools); err != nil {
		return LoopAccess{}, false, fmt.Errorf("store: the pre-approved tools of loop %s are not a list: %w", loopID, err)
	}
	return a, true, nil
}

// LoopAccessSet turns a mode on for a loop (replacing any earlier one), stamped with who and when.
func (s *Store) LoopAccessSet(ctx context.Context, a LoopAccess) error {
	if a.LoopID == "" || (a.Mode != LoopAccessTools && a.Mode != LoopAccessFull) {
		return fmt.Errorf("store: LoopAccessSet needs a loop id and a mode of tools or full")
	}
	tools := a.Tools
	if tools == nil {
		tools = []string{}
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	_, err = s.w.ExecContext(ctx, `INSERT INTO loop_access (loop_id, mode, tools_json, set_by_kind, set_by_id, set_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(loop_id) DO UPDATE SET mode=excluded.mode, tools_json=excluded.tools_json,
		  set_by_kind=excluded.set_by_kind, set_by_id=excluded.set_by_id, set_at=excluded.set_at`,
		a.LoopID, a.Mode, string(raw), a.SetByKind, a.SetByID, a.SetAt)
	if err != nil {
		return fmt.Errorf("store: set the access mode of loop %s: %w", a.LoopID, err)
	}
	return nil
}

// LoopAccessClear returns a loop to the default (ask). Clearing an already-default loop is not an error.
func (s *Store) LoopAccessClear(ctx context.Context, loopID string) error {
	if loopID == "" {
		return fmt.Errorf("store: LoopAccessClear needs a loop id")
	}
	if _, err := s.w.ExecContext(ctx, `DELETE FROM loop_access WHERE loop_id = ?`, loopID); err != nil {
		return fmt.Errorf("store: clear the access mode of loop %s: %w", loopID, err)
	}
	return nil
}
