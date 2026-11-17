package daemon

// # Terminals in the daemon (S31 T7, D9)
//
// The daemon owns the PTY registry exactly the way it owns `stepCancels`:
// shared cross-call state the method table must not hold for itself. The
// machine half — creack/pty, the reader goroutine, the 1 MiB ring, the
// 16 ms / 32 KiB coalescer, the group kill, the 15-minute reaper — is
// `internal/terminal`; this file is the WIRE to it.
//
// Three decisions worth naming:
//
//   - Delivery bypasses this file and the EventLog entirely (LW7): the
//     manager was handed `d.notify` — the transport's Notify — at birth, and
//     keystroke-rate bytes never touch the durable seq or the shared ring.
//   - The manager's typed refusals become protocol errors HERE, at the one
//     boundary that knows both vocabularies. The protocol package never
//     imports `internal/terminal` and never execs.
//   - A terminal outlives a connection (LW9) and does not outlive the
//     daemon: `onConnectionClosed` only detaches; `Shutdown` closes all,
//     group-killed — the spawn.go mechanism, measured by a freed port.

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/terminal"
)

// initTerminals builds the registry. The Notifier is d.notify — bound at
// birth, not per call — because output is a STREAM and this package must not
// see a byte of it. The bounds stay at the package defaults (the task's
// Notes record each one's measured reality; tests shorten them through the
// package's own Options).
func (d *Daemon) initTerminals() *terminal.Manager {
	return terminal.New(terminal.Options{
		Notifier: func(connID, method string, params any) error {
			return d.notify(connID, method, params)
		},
		Log: d.log,
	})
}

// Shutdown stops the reaper and closes every terminal. The order is the
// argument: the reaper stops FIRST so it cannot notify into a transport that
// is already closing; the shells then die as process groups (LW9 — a
// terminal outlives a connection and a window reload, but never the daemon
// that owns its pty master).
func (d *Daemon) Shutdown() {
	// Drain the fire-and-forget answer/continue goroutines FIRST: each keeps
	// writing the transcript and checkpoint after its request returned, so the
	// store must not close while one is in flight — a store closed out from
	// under a continuation panics database/sql (nil *sql.DB). This runs before
	// the transport closes and before the store's own close, so no continuation
	// writes into a dead store. (LW9's ordering: the drain is the daemon's,
	// like the reaper, and the store outlives the goroutines it started.)
	if d.Table != nil {
		d.Table.WaitContinuations()
	}
	// S64: drain the async workers before anything closes under them. Each
	// live worker gets its cancellation and the manager's grace window to
	// write its own ending; whatever is still running when the window
	// closes is swept to interrupted — the row outlives this process, and
	// the next daemon's Recover speaks for anything that slips past even
	// the sweep. The store must outlive these goroutines (same bargain as
	// the continuations above).
	if d.workers != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*5*time.Second)
		if err := d.workers.Shutdown(ctx); err != nil && d.log != nil {
			d.log.Infof("async workers could not drain cleanly: %v", err)
		}
		cancel()
	}
	// S58 T5: drain the dispatcher after the workers — queued channel
	// deliveries finish (bounded: a finite queue, each send capped by its
	// own timeout) while the store is still open to receive their outcome
	// rows. A delivery still in flight when the window closes simply has
	// no row; that is a crash-shaped gap, honestly unnamed in the log,
	// not a silent loss the daemon pretends did not happen.
	if d.channels != nil {
		d.channels.Stop()
	}
	if d.termCancel != nil {
		d.termCancel()
	}
	if d.terminals != nil {
		d.terminals.CloseAll()
	}
}

// terminalDoor adapts the manager to protocol.TerminalManager and, at the
// same single point, translates the manager's typed refusals onto the
// -31510s (or onto ErrBadParams for input that is not valid UTF-8 — the
// caller's bug, named).
type terminalDoor struct{ m *terminal.Manager }

func (door terminalDoor) Open(p protocol.TerminalOpenParams) (protocol.TerminalOpenResult, error) {
	s, err := door.m.Open(terminal.OpenParams{ComponentRoot: p.ComponentRoot, Cols: p.Cols, Rows: p.Rows})
	if err != nil {
		return protocol.TerminalOpenResult{}, terminalError(err)
	}
	return protocol.TerminalOpenResult{TerminalID: s.ID, Shell: s.Shell, Cwd: s.Cwd}, nil
}

func (door terminalDoor) Input(id, data string) error {
	return terminalError(door.m.Input(id, data))
}

func (door terminalDoor) Resize(id string, cols, rows int) error {
	return terminalError(door.m.Resize(id, cols, rows))
}

func (door terminalDoor) Attach(connID, id string) (protocol.TerminalAttachResult, error) {
	a, err := door.m.Attach(connID, id)
	if err != nil {
		return protocol.TerminalAttachResult{}, terminalError(err)
	}
	// The ring's raw bytes are encoded ONCE, here (Q3): a shell produces
	// bytes, and the socket speaks strings.
	return protocol.TerminalAttachResult{
		Scrollback: base64.StdEncoding.EncodeToString(a.Scrollback),
		Seq:        a.Seq, Exited: a.Exited, ExitCode: a.ExitCode,
	}, nil
}

func (door terminalDoor) Detach(connID, id string) error {
	return terminalError(door.m.Detach(connID, id))
}

func (door terminalDoor) Close(id string) (protocol.TerminalCloseResult, error) {
	c, err := door.m.Close(id)
	if err != nil {
		return protocol.TerminalCloseResult{}, terminalError(err)
	}
	return protocol.TerminalCloseResult{ExitCode: c.ExitCode}, nil
}

func (door terminalDoor) List(componentRoot string) []protocol.TerminalView {
	var out []protocol.TerminalView
	for _, v := range door.m.List(componentRoot) {
		out = append(out, protocol.TerminalView{
			TerminalID: v.TerminalID, ComponentRoot: v.ComponentRoot, Cwd: v.Cwd,
			Shell: v.Shell, CreatedAt: v.CreatedAt, Attached: v.Attached,
			Exited: v.Exited, ExitCode: v.ExitCode,
		})
	}
	return out
}

// terminalError is the ONE translation point.
func terminalError(err error) error {
	switch e := err.(type) {
	case nil:
		return nil
	case *terminal.NotFoundError:
		return protocol.ErrTerminalNotFound.WithWhat(
			"no live terminal carries terminalId %q", e.ID)
	case *terminal.LimitError:
		return protocol.ErrTerminalLimit.WithWhat(
			"project root %q already has %d terminals; the bound is %d per root", e.Root, e.Count, e.Limit)
	case *terminal.ExitedError:
		return protocol.ErrTerminalExited.WithWhat(
			"the shell of terminal %q exited on its own; the session is kept only to serve its scrollback", e.ID)
	case *terminal.UnsupportedError:
		return protocol.ErrTerminalUnsupported.WithWhat("no PTY exists for %s", e.OS)
	case *terminal.BadInputError:
		return protocol.ErrBadParams{Err: fmt.Errorf("input for terminal %q was refused: %s", e.ID, e.Reason)}
	default:
		return err
	}
}
