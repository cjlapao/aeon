package daemon

// # Terminals on the real wire (S31 T7, D9)
//
// The package tests prove the shell mechanics; this file proves the DAEMON
// mounted them: an actual `/bin/sh` on an actual PTY, opened by a socket
// call, typing, answering, exiting — with the two claims that make this
// S31's design rather than any design checked at the only place they can
// be:
//
//   - LW7: a terminal's whole life leaves the durable event log UNTOUCHED
//     (`events.Head()` identical before and after; the attached connection
//     holds a live subscription and still receives zero `event`
//     notifications — keystroke-rate bytes never spend the ring that real
//     events need).
//   - The notifications are `terminal.output` / `terminal.exit` with
//     base64 payloads, exactly as brief §5 spells them.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type termDispatch = map[string]any

func termFixture(t *testing.T) (*Daemon, *wire, *store.Store, string) {
	t.Helper()
	w := &wire{}
	s := recoveryStore(t)
	root := t.TempDir()
	d := New(Options{Store: s, Logger: w, Notify: w.notify})
	initConn(t, d, context.Background(), "c1", "terminal-window")
	// A live subscription on the very connection that will carry terminal
	// traffic: LW7's proof needs a subscriber that could have been flooded.
	subscribe(t, d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{}})
	registerComponent(t, s, "termroot", root)
	t.Cleanup(d.Shutdown) // terminals die with the daemon — LW9
	return d, w, s, root
}

func termMap(t *testing.T, d *Daemon, connID string, m protocol.MethodName, params any) (termDispatch, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.Table.Dispatch(context.Background(), connID, m, raw)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m2 termDispatch
	if err := json.Unmarshal(out, &m2); err != nil {
		t.Fatal(err)
	}
	return m2, nil
}

func termMust(t *testing.T, d *Daemon, connID string, m protocol.MethodName, params any) termDispatch {
	t.Helper()
	got, err := termMap(t, d, connID, m, params)
	if err != nil {
		t.Fatalf("%s: %v", m, err)
	}
	return got
}

// pollUntil reads the wire until `hit` accepts a notification, or the
// deadline passes — terminal delivery is a stream, and streams are polled,
// not slept on.
func pollUntil(t *testing.T, w *wire, connID, method string, hit func(raw json.RawMessage) bool) json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, n := range w.delivered() {
			if n.ConnID == connID && n.Method == method && hit(n.Raw) {
				return n.Raw
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s to %s before the deadline", method, connID)
	return nil
}

func openTerminal(t *testing.T, d *Daemon, root string, cols, rows int) string {
	t.Helper()
	got := termMust(t, d, "c1", protocol.MethodTerminalOpen,
		protocol.TerminalOpenParams{ComponentRoot: root, Cols: cols, Rows: rows})
	id, _ := got["terminalId"].(string)
	if id == "" {
		t.Fatalf("open returned no id: %v", got)
	}
	// Open does NOT attach — the window that wants bytes must attach (the
	// seat is per connection, LW9).
	termMust(t, d, "c1", protocol.MethodTerminalAttach, protocol.TerminalIDParams{TerminalID: id})
	return id
}

func TestTerminalFullLifeOnTheWire(t *testing.T) {
	d, w, _, root := termFixture(t)
	headBefore := d.events.Head()
	// Baseline of event notifications the transport legitimately sends for
	// the lifecycle itself (connected/subscribed): LW7's claim is that the
	// TERMINAL adds none, so the count must not move from here.
	eventsBefore := countEventNotifs(w, "c1")

	id := openTerminal(t, d, root, 80, 24)

	termMust(t, d, "c1", protocol.MethodTerminalInput,
		// The marker is computed by the shell, never a substring of the typed
		// command: a PTY echoes input through the same reader, and an assertion
		// the ECHO alone satisfies proves the pipe, not the shell.
		protocol.TerminalInputParams{TerminalID: id, Data: "echo \"hi-$(printf %s T7)\"\n"})
	raw := pollUntil(t, w, "c1", "terminal.output", func(raw json.RawMessage) bool {
		var p struct {
			TerminalID string `json:"terminalId"`
			Data       string `json:"data"`
		}
		if json.Unmarshal(raw, &p) != nil || p.TerminalID != id {
			return false
		}
		b, err := base64.StdEncoding.DecodeString(p.Data)
		return err == nil && strings.Contains(string(b), "hi-T7")
	})
	var outP struct {
		Seq  uint64 `json:"seq"`
		Data string `json:"data"`
	}
	_ = json.Unmarshal(raw, &outP)
	if outP.Seq == 0 || outP.Data == "" {
		t.Fatalf("output payload not per brief §5: %s", raw)
	}

	// exit 3 → `terminal.exit {terminalId, exitCode:3}` — the code, not a
	// signal, exactly as cmd.Wait reports it.
	termMust(t, d, "c1", protocol.MethodTerminalInput,
		protocol.TerminalInputParams{TerminalID: id, Data: "exit 3\n"})
	pollUntil(t, w, "c1", "terminal.exit", func(raw json.RawMessage) bool {
		var p struct {
			TerminalID string `json:"terminalId"`
			ExitCode   *int   `json:"exitCode"`
			Signal     string `json:"signal"`
		}
		if json.Unmarshal(raw, &p) != nil || p.TerminalID != id {
			return false
		}
		return p.ExitCode != nil && *p.ExitCode == 3 && p.Signal == ""
	})

	// The exited session is KEPT (LW9): attach still serves the tail.
	got := termMust(t, d, "c1", protocol.MethodTerminalAttach, protocol.TerminalIDParams{TerminalID: id})
	b, err := base64.StdEncoding.DecodeString(got["scrollback"].(string))
	if err != nil || !strings.Contains(string(b), "hi-T7") {
		t.Fatalf("the kept session lost its scrollback: %v (%q, %v)", got, b, err)
	}
	if got["exited"] != true {
		t.Fatalf("attach does not report the exit: %v", got)
	}
	if code, _ := got["exitCode"].(float64); code != 3 {
		t.Fatalf("attach lost the exit code: %v", got)
	}

	// LW7: NONE of this touched the durable log.
	if head := d.events.Head(); head != headBefore {
		t.Fatalf("terminal traffic spent event seqs: %d → %d", headBefore, head)
	}
	if got := countEventNotifs(w, "c1"); got != eventsBefore {
		t.Fatalf("terminal traffic rode the event stream (LW7): %d event notifications, was %d",
			got, eventsBefore)
	}
}

func countEventNotifs(w *wire, connID string) int {
	n := 0
	for _, sent := range w.delivered() {
		if sent.ConnID == connID && sent.Method == "event" {
			n++
		}
	}
	return n
}

func TestTerminalResizeReachesTheShell(t *testing.T) {
	d, w, _, root := termFixture(t)
	id := openTerminal(t, d, root, 80, 24)
	termMust(t, d, "c1", protocol.MethodTerminalResize,
		protocol.TerminalResizeParams{TerminalID: id, Cols: 120, Rows: 30})
	termMust(t, d, "c1", protocol.MethodTerminalInput,
		protocol.TerminalInputParams{TerminalID: id, Data: "stty size\n"})
	pollUntil(t, w, "c1", "terminal.output", func(raw json.RawMessage) bool {
		var p struct {
			TerminalID string `json:"terminalId"`
			Data       string `json:"data"`
		}
		if json.Unmarshal(raw, &p) != nil || p.TerminalID != id {
			return false
		}
		b, err := base64.StdEncoding.DecodeString(p.Data)
		return err == nil && strings.Contains(string(b), "30 120")
	})
}

func TestTerminalOpenRefusesAnUnregisteredRootNamingIt(t *testing.T) {
	d, _, _, _ := termFixture(t)
	outside := t.TempDir()
	_, err := termMap(t, d, "c1", protocol.MethodTerminalOpen,
		protocol.TerminalOpenParams{ComponentRoot: outside})
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != protocol.ErrComponentRootUnknown.Code {
		t.Fatalf("got %v, want the root named at -31524", err)
	}
	if !strings.Contains(e.Data.What, outside) {
		t.Fatalf("the refusal did not name the path (I4): %s", e.Data.What)
	}
}

func TestNinthTerminalMapsToTheNamedLimitOnTheWire(t *testing.T) {
	d, _, _, root := termFixture(t)
	for i := 0; i < 8; i++ {
		got := termMust(t, d, "c1", protocol.MethodTerminalOpen,
			protocol.TerminalOpenParams{ComponentRoot: root, Cols: 80, Rows: 24})
		if id, _ := got["terminalId"].(string); id == "" {
			t.Fatalf("terminal %d never opened: %v", i+1, got)
		}
	}
	_, err := termMap(t, d, "c1", protocol.MethodTerminalOpen,
		protocol.TerminalOpenParams{ComponentRoot: root})
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != protocol.ErrTerminalLimit.Code {
		t.Fatalf("the ninth open: got %v, want -31512", err)
	}
	if !strings.Contains(e.Data.What, root) || !strings.Contains(e.Data.What, "8") {
		t.Fatalf("the refusal did not name the root and the count: %s", e.Data.What)
	}
}

func TestConnectionCloseDetachesButTheShellLives(t *testing.T) {
	d, w, _, root := termFixture(t)
	id := openTerminal(t, d, root, 80, 24)
	termMust(t, d, "c1", protocol.MethodTerminalInput,
		protocol.TerminalInputParams{TerminalID: id, Data: "echo \"before-$(printf %s close)\"\n"})
	pollUntil(t, w, "c1", "terminal.output", func(raw json.RawMessage) bool {
		var p struct {
			TerminalID string `json:"terminalId"`
			Data       string `json:"data"`
		}
		if json.Unmarshal(raw, &p) != nil || p.TerminalID != id {
			return false
		}
		b, err := base64.StdEncoding.DecodeString(p.Data)
		return err == nil && strings.Contains(string(b), "before-close")
	})

	// The transport's death detaches the SEAT; the shell keeps running
	// (LW9) — proven by the session still being listed, and by the closed
	// connection receiving nothing further.
	before := len(w.delivered())
	d.onConnectionClosed("c1")
	if err := d.terminals.Input(id, "echo \"after-$(printf %s close)\"\n"); err != nil {
		t.Fatalf("input to the still-live shell: %v", err)
	}
	time.Sleep(150 * time.Millisecond) // past two coalescer windows
	for _, n := range w.delivered()[before:] {
		if n.Method == "terminal.output" || n.Method == "terminal.exit" {
			t.Fatalf("a closed connection still received terminal traffic: %s %s", n.Method, n.Raw)
		}
	}
	views := d.terminals.List(root)
	if len(views) != 1 || views[0].TerminalID != id || views[0].Attached {
		t.Fatalf("the shell should be alive and detached: %+v", views)
	}
}
