package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// # Terminal sessions in the daemon (S31 — T7)
//
// The person's own shell, in a registered project root, on the socket only
// they can open (P26). open, input, resize, attach (with bounded scrollback),
// detach, close, list. Output and exit are NOT events — they reach attached
// connections as the `localrpc` notifications `terminal.output` /
// `terminal.exit` (LW7: a terminal is a stream, not history; keystroke-rate
// bytes through the durable `seq` and the shared ring would evict real
// events).
//
// Not policy-governed (LW8 — policy governs what AGENTS do, not the person's
// shell) and must never reach the narrow HTTP surface (the T9 guard test
// mirrors the policy-authoring guard). Windows refuses `terminal.open` with a
// named `unsupported` (LW12). Bounds per LW9/LW10: 1 MiB scrollback ring,
// ≤ 8 terminals per root, output coalesced per 16 ms / 32 KiB, detached
// sessions reaped after 15 minutes, all killed as process groups on shutdown.
//
// # Where each piece lives
//
// This file is the DOOR: decode, the registered-root check on the two
// root-named methods, and named refusals. The machine half — creack/pty, the
// reader goroutine, the ring, the coalescer, the group kill, the reaper — is
// `internal/terminal`, and the daemon hands it in through the seam below
// (this package may not exec; the adapter translates the manager's typed
// refusals into these codes). Output delivery is NOT a seam: the manager
// notifies attached connections directly through the transport's Notify,
// which is precisely how it bypasses the EventLog.

// TerminalManager is the daemon-owned PTY registry as the table needs it —
// the shape `internal/terminal.Manager` presents, with this package's wire
// types. Errors arriving here are already classified *Errors (the daemon's
// adapter maps NotFound/limit/exited/unsupported/UTF-8 refusals onto the
// -31510s); the handlers pass them through untouched.
type TerminalManager interface {
	Open(p TerminalOpenParams) (TerminalOpenResult, error)
	Input(terminalID, data string) error
	Resize(terminalID string, cols, rows int) error
	Attach(connID, terminalID string) (TerminalAttachResult, error)
	Detach(connID, terminalID string) error
	Close(terminalID string) (TerminalCloseResult, error)
	List(componentRoot string) []TerminalView
}

// ── params and results (pinned by conformance/terminal/*.json) ───────────────

type TerminalOpenParams struct {
	ComponentRoot string `json:"componentRoot"`
	Cols          int    `json:"cols,omitempty"`
	Rows          int    `json:"rows,omitempty"`
}

type TerminalOpenResult struct {
	TerminalID string `json:"terminalId"`
	Shell      string `json:"shell"`
	Cwd        string `json:"cwd"`
}

type TerminalInputParams struct {
	TerminalID string `json:"terminalId"`
	Data       string `json:"data"`
}

type TerminalResizeParams struct {
	TerminalID string `json:"terminalId"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
}

type TerminalIDParams struct {
	TerminalID string `json:"terminalId"`
}

// TerminalAttachResult carries the scrollback base64-encoded — a shell
// produces bytes, not strings (Q3), and this is the one place they are
// encoded for the wire.
type TerminalAttachResult struct {
	Scrollback string `json:"scrollback"`
	Seq        uint64 `json:"seq"`
	Exited     bool   `json:"exited,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
}

// TerminalCloseResult reports an exit code only when the shell had ALREADY
// exited on its own — a killed shell died by signal and has no code to
// report.
type TerminalCloseResult struct {
	ExitCode *int `json:"exitCode,omitempty"`
}

type TerminalListParams struct {
	ComponentRoot string `json:"componentRoot"`
}

type TerminalView struct {
	TerminalID    string `json:"terminalId"`
	ComponentRoot string `json:"componentRoot"`
	Cwd           string `json:"cwd"`
	Shell         string `json:"shell"`
	CreatedAt     string `json:"createdAt"`
	Attached      bool   `json:"attached"`
	Exited        bool   `json:"exited"`
	ExitCode      *int   `json:"exitCode,omitempty"`
}

// ── the handlers ─────────────────────────────────────────────────────────────

func (t *Table) terminalsDoor() error {
	if t.Terminals == nil {
		return ErrNotWired.WithWhat("no terminal manager is wired into this daemon")
	}
	return nil
}

func (t *Table) handleTerminalOpen(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalOpenParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The SAME registered-root door the git reads use: a shell starts only
	// where the person said a project lives.
	if err := t.workspaceDoor(p.ComponentRoot); err != nil {
		return nil, err
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	return t.Terminals.Open(p)
}

func (t *Table) handleTerminalInput(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalInputParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.TerminalID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("terminalId is required")}
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	// Input is a STRING (Q3): invalid UTF-8 is refused by the manager as
	// named bad params — handing the shell broken bytes is the caller's bug,
	// and a refusal is kinder than mojibake in a prompt.
	if err := t.Terminals.Input(p.TerminalID, p.Data); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (t *Table) handleTerminalResize(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalResizeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.TerminalID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("terminalId is required")}
	}
	if p.Cols < 1 || p.Rows < 1 {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"cols and rows must be at least 1 to resize terminal %q — the window named %d×%d",
			p.TerminalID, p.Cols, p.Rows)}
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	if err := t.Terminals.Resize(p.TerminalID, p.Cols, p.Rows); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (t *Table) handleTerminalAttach(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.TerminalID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("terminalId is required")}
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	// The CALLING connection is the one that attaches — never a connID the
	// params claim. The scrollback tail rides back so a reloaded window
	// catches up (LW9: a window reload must not kill a running `make test`).
	res, err := t.Terminals.Attach(sess.ConnID, p.TerminalID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleTerminalDetach(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.TerminalID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("terminalId is required")}
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	// Detaching stops DELIVERY; the shell keeps running (LW9 — a terminal
	// outlives a connection; only the reaper, a close, or the daemon's own
	// death ends it).
	if err := t.Terminals.Detach(sess.ConnID, p.TerminalID); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (t *Table) handleTerminalClose(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.TerminalID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("terminalId is required")}
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	// The kill is a GROUP kill (spawn.go's mechanism): a shell that
	// backgrounded a server takes the server with it.
	return t.Terminals.Close(p.TerminalID)
}

func (t *Table) handleTerminalList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TerminalListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := t.workspaceDoor(p.ComponentRoot); err != nil {
		return nil, err
	}
	if err := t.terminalsDoor(); err != nil {
		return nil, err
	}
	// A BARE array, like step.steers: the listing is the answer, not a
	// wrapper with one field. An empty root answers `[]` — it looked, and
	// found nothing (state, not absence).
	views := t.Terminals.List(p.ComponentRoot)
	if views == nil {
		views = []TerminalView{}
	}
	return views, nil
}
