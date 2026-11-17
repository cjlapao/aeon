package protocol

// # The terminal door (S31 T7)
//
// The `internal/terminal` tests prove the shell, the ring, the coalescer, the
// group kill and the reaper. This file proves the DOOR: the registered-root
// check on the two root-named methods, the refusals a caller must be told
// about, the pinning that ATTACH BINDS THE CALLING CONNECTION (a param could
// claim another connection's seat — the session is the only truth), and the
// bare-array list. The adapter's error mapping onto the -31510s is tested
// where the manager's typed refusals actually appear: the daemon's own
// terminal test.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type termSpy struct {
	mu       sync.Mutex
	opened   []TerminalOpenParams
	inputs   []string
	attached string
	closed   []string
	list     []TerminalView

	attachSeq uint64
	openErr   error
	inputErr  error
	closeCode *int
}

func (s *termSpy) Open(p TerminalOpenParams) (TerminalOpenResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return TerminalOpenResult{}, s.openErr
	}
	s.opened = append(s.opened, p)
	return TerminalOpenResult{TerminalID: "t-9", Shell: "/bin/sh", Cwd: p.ComponentRoot}, nil
}

func (s *termSpy) Input(id, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inputErr != nil {
		return s.inputErr
	}
	s.inputs = append(s.inputs, id+":"+data)
	return nil
}

func (s *termSpy) Resize(id string, cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, id+":resize")
	return nil
}

func (s *termSpy) Attach(connID, id string) (TerminalAttachResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached = connID + "|" + id
	// The manager hands raw bytes at the daemon boundary; by the time the
	// result is a protocol type the tail is base64 (Q3) — the spy plays the
	// ADAPTED manager.
	return TerminalAttachResult{Scrollback: "dGFpbA==", Seq: s.attachSeq}, nil
}

func (s *termSpy) Detach(connID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, "detach:"+connID+"|"+id)
	return nil
}

func (s *termSpy) Close(id string) (TerminalCloseResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, id)
	return TerminalCloseResult{ExitCode: s.closeCode}, nil
}

func (s *termSpy) List(string) []TerminalView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list
}

func termTable(t *testing.T) (*Table, *termSpy) {
	t.Helper()
	tbl, _ := stepTable(t, &scriptedRunner{}, &spyStepStore{})
	spy := &termSpy{attachSeq: 41}
	tbl.ComponentRootKnown = func(root string) bool { return root == "/srv/registered" }
	tbl.Terminals = spy
	initializedOnce(t, tbl, "c1")
	return tbl, spy
}

func dispatchTerm(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	return tbl.Dispatch(context.Background(), "c1", m, mustJSON(t, params))
}

func TestTerminalOpenRunsTheDoorThenTheManager(t *testing.T) {
	tbl, spy := termTable(t)
	res, err := dispatchTerm(t, tbl, MethodTerminalOpen,
		TerminalOpenParams{ComponentRoot: "/srv/registered", Cols: 100, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(TerminalOpenResult)
	if got.TerminalID != "t-9" || got.Shell != "/bin/sh" {
		t.Fatalf("the manager's answer did not travel intact: %+v", got)
	}
	if len(spy.opened) != 1 || spy.opened[0].Cols != 100 || spy.opened[0].Rows != 24 {
		t.Fatalf("params lost on the way in: %+v", spy.opened)
	}

	_, err = dispatchTerm(t, tbl, MethodTerminalOpen, TerminalOpenParams{ComponentRoot: "/srv/unregistered"})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrComponentRootUnknown.Code {
		t.Fatalf("unregistered root: got %v", err)
	}
	if !strings.Contains(e.Data.What, "/srv/unregistered") {
		t.Fatalf("the refusal did not name the path (I4): %s", e.Data.What)
	}
}

func TestTerminalAttachBindsTheCallingConnectionNotAParam(t *testing.T) {
	tbl, spy := termTable(t)
	res, err := dispatchTerm(t, tbl, MethodTerminalAttach, TerminalIDParams{TerminalID: "t-9"})
	if err != nil {
		t.Fatal(err)
	}
	// The spy recorded WHO called: the session's connection, always.
	if spy.attached != "c1|t-9" {
		t.Fatalf("attach bound the wrong principal: %q", spy.attached)
	}
	got := res.(TerminalAttachResult)
	if b, err := base64.StdEncoding.DecodeString(got.Scrollback); err != nil || string(b) != "tail" {
		t.Fatalf("scrollback lost or unencoded: %+v (%q, %v)", got, b, err)
	}
	if got.Seq != 41 {
		t.Fatalf("seq lost: %+v", got)
	}

	_, err = dispatchTerm(t, tbl, MethodTerminalDetach, TerminalIDParams{TerminalID: "t-9"})
	if err != nil {
		t.Fatal(err)
	}
	if spy.inputs[len(spy.inputs)-1] != "detach:c1|t-9" {
		t.Fatalf("detach freed the wrong seat: %v", spy.inputs)
	}
}

func TestTerminalRefusalsBeforeTheManager(t *testing.T) {
	// ONE table for all seven: the claim is that none of them reaches the
	// manager, which only a shared spy can show.
	tbl, spy := termTable(t)

	bad := []struct {
		name string
		m    MethodName
		p    any
	}{
		{"input without an id", MethodTerminalInput, TerminalInputParams{Data: "x"}},
		{"attach without an id", MethodTerminalAttach, TerminalIDParams{}},
		{"detach without an id", MethodTerminalDetach, TerminalIDParams{}},
		{"close without an id", MethodTerminalClose, TerminalIDParams{}},
		{"resize with a 0-wide window", MethodTerminalResize, TerminalResizeParams{TerminalID: "t-9", Cols: 0, Rows: 24}},
		{"resize with a negative height", MethodTerminalResize, TerminalResizeParams{TerminalID: "t-9", Cols: 80, Rows: -3}},
		{"list without a root", MethodTerminalList, TerminalListParams{}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dispatchTerm(t, tbl, tc.m, tc.p)
			var bp ErrBadParams
			if !errors.As(err, &bp) {
				t.Fatalf("got %v, want bad params", err)
			}
		})
	}
	spy.mu.Lock()
	empty := len(spy.inputs)
	spy.mu.Unlock()
	if empty != 0 {
		t.Fatalf("a refused call reached the manager: %v", spy.inputs)
	}

	// And the door is not merely refusing everything: the same shape WITH
	// the required field does reach the manager.
	if _, err := dispatchTerm(t, tbl, MethodTerminalDetach, TerminalIDParams{TerminalID: "t-9"}); err != nil {
		t.Fatal(err)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.inputs) != 1 {
		t.Fatalf("the good detach never reached the manager: %v", spy.inputs)
	}
}

func TestTerminalListIsABareArrayAndEmptyMeansEmpty(t *testing.T) {
	tbl, spy := termTable(t)
	res, err := dispatchTerm(t, tbl, MethodTerminalList, TerminalListParams{ComponentRoot: "/srv/registered"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if string(raw) != "[]" {
		t.Fatalf("an empty root must answer the bare `[]` (state, not absence): %s", raw)
	}
	code := 1
	spy.list = []TerminalView{{TerminalID: "t-9", Shell: "/bin/sh", Cwd: "/srv/registered",
		ComponentRoot: "/srv/registered", CreatedAt: "2026-10-24T10:00:00Z", Attached: true, ExitCode: &code}}
	res, _ = dispatchTerm(t, tbl, MethodTerminalList, TerminalListParams{ComponentRoot: "/srv/registered"})
	raw, _ = json.Marshal(res)
	if !strings.HasPrefix(string(raw), "[{") || !strings.Contains(string(raw), `"terminalId":"t-9"`) {
		t.Fatalf("the list is not the pinned bare array: %s", raw)
	}
}

func TestTerminalManagerAbsentRefusesByName(t *testing.T) {
	tbl, _ := termTable(t)
	tbl.Terminals = nil
	_, err := dispatchTerm(t, tbl, MethodTerminalInput, TerminalInputParams{TerminalID: "t-9", Data: "x"})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrNotWired.Code || !strings.Contains(e.Data.What, "no terminal manager") {
		t.Fatalf("got %v, want ErrNotWired naming the missing manager", err)
	}
}

func TestTerminalClassifiedErrorsTravelUntouched(t *testing.T) {
	// The daemon's adapter classifies the manager's refusals before this
	// door ever sees them; the door passes a classified *Error through
	// unchanged — re-wrapping would lose the named What.
	tbl, spy := termTable(t)
	spy.inputErr = ErrTerminalExited.WithWhat("the shell of terminal %q exited on its own", "t-9")
	_, err := dispatchTerm(t, tbl, MethodTerminalInput, TerminalInputParams{TerminalID: "t-9", Data: "ls\n"})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrTerminalExited.Code {
		t.Fatalf("got %v, want -31513 through", err)
	}
	if !strings.Contains(e.Data.What, "t-9") {
		t.Fatalf("the named refusal lost its name: %s", e.Data.What)
	}

	spy.openErr = ErrTerminalLimit.WithWhat("project root %q already has 8 terminals; the bound is 8 per root", "/srv/registered")
	_, err = dispatchTerm(t, tbl, MethodTerminalOpen, TerminalOpenParams{ComponentRoot: "/srv/registered"})
	if !errors.As(err, &e) || e.Code != ErrTerminalLimit.Code {
		t.Fatalf("got %v, want -31512 through", err)
	}
}
