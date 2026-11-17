//go:build !windows

package terminal

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Package-internal tests (the LW10 bounds and the ring are package state;
// the daemon-level behaviour — EventLog untouched, connection-close DETACH,
// named protocol refusals — lives in internal/daemon). Every test uses
// t.TempDir roots and /bin/sh; nothing here can reach the real store.

type sentNote struct {
	ConnID string
	Method string
	Params any
}

type recorder struct {
	mu    sync.Mutex
	notes []sentNote
}

func (r *recorder) notify(connID, method string, params any) error {
	// The payload structs arrive by value and are never mutated after send,
	// so storing them as-is is safe; assertions decode through JSON the same
	// way the wire would.
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notes = append(r.notes, sentNote{ConnID: connID, Method: method, Params: params})
	return nil
}

func (r *recorder) snapshot() []sentNote {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sentNote, len(r.notes))
	copy(out, r.notes)
	return out
}

func outputsTo(r *recorder, connID string) []OutputParams {
	var out []OutputParams
	for _, n := range r.snapshot() {
		if n.ConnID != connID || n.Method != MethodOutput {
			continue
		}
		raw, err := json.Marshal(n.Params)
		if err != nil {
			panic(err)
		}
		var p OutputParams
		if err := json.Unmarshal(raw, &p); err != nil {
			panic(err)
		}
		out = append(out, p)
	}
	return out
}

func decodedOutputs(r *recorder, connID string) string {
	var sb strings.Builder
	for _, p := range outputsTo(r, connID) {
		b, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			panic(fmt.Errorf("terminal.output data is not base64: %w", err))
		}
		sb.Write(b)
	}
	return sb.String()
}

func exitFor(r *recorder, connID string) (ExitParams, bool) {
	for _, n := range r.snapshot() {
		if n.ConnID != connID || n.Method != MethodExit {
			continue
		}
		raw, _ := json.Marshal(n.Params)
		var p ExitParams
		_ = json.Unmarshal(raw, &p)
		return p, true
	}
	return ExitParams{}, false
}

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", within, what)
}

// testManager builds a Manager over a recording notifier with fast bounds.
// The shell is pinned to /bin/sh so a developer's $SHELL (zsh with its own
// prompt bytes) cannot move the goalposts.
func testManager(t *testing.T) (*Manager, *recorder) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	r := &recorder{}
	m := New(Options{
		Notifier:  r.notify,
		KillGrace: 100 * time.Millisecond, // the group kill is measured in grouptest; tests must not wait on it
	})
	return m, r
}

func openShell(t *testing.T, m *Manager, root string) *Session {
	t.Helper()
	s, err := m.Open(OpenParams{ComponentRoot: root, Cols: 100, Rows: 24})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

// waitForOutput polls until connID's decoded stream contains want.
func waitForOutput(t *testing.T, r *recorder, connID, want string) {
	t.Helper()
	waitFor(t, "output containing "+want, 8*time.Second, func() bool {
		return strings.Contains(decodedOutputs(r, connID), want)
	})
}

func TestOpenRunsThePersonsShellInTheComponentRootWithTerm(t *testing.T) {
	m, r := testManager(t)
	root := t.TempDir()
	s := openShell(t, m, root)

	if s.Shell != "/bin/sh" {
		t.Errorf("shell is %q, want /bin/sh", s.Shell)
	}
	if s.Cwd != root {
		t.Errorf("cwd is %q, want %q", s.Cwd, root)
	}
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := m.Input(s.ID, "echo \"TERM=$TERM\"; pwd\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	waitForOutput(t, r, "c1", "TERM=xterm-256color")
	got := decodedOutputs(r, "c1")
	if !strings.Contains(got, root) {
		t.Errorf("pwd did not report the project root %q in:\n%s", root, got)
	}
	// A connection that never attached receives nothing (the attached set
	// is the delivery list — LW7's Notify-per-attached-connection).
	if len(outputsTo(r, "c2")) != 0 {
		t.Error("an unattached connection received output")
	}
	m.CloseAll()
}

func TestResizeIsReflectedInSttySize(t *testing.T) {
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(s.ID, 120, 30); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := m.Input(s.ID, "stty size\n"); err != nil {
		t.Fatal(err)
	}
	// `stty size` prints "rows cols" — the pty honours the resize or the
	// shell wraps at the old width forever (brief §5's point).
	waitForOutput(t, r, "c1", "30 120")
	m.CloseAll()
}

func TestAttachAfterOutputReplaysTheScrollbackWithMonotonicSeq(t *testing.T) {
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	// The asserted marker must NOT be a substring of the typed command: a
	// PTY echoes what you type back through the same reader, so `echo
	// FIRST-CHUNK` would satisfy Contains with the ECHO alone. The
	// substitution computes the bytes only in the program's output.
	if err := m.Input(s.ID, "echo \"FIRST-$(printf %s CHUNK)\"\n"); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, r, "c1", "FIRST-CHUNK")

	// A late attacher (the window reload of LW9) gets the replay and the
	// seq it covers, then continues strictly above it.
	res, err := m.Attach("c2", s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Scrollback), "FIRST-CHUNK") {
		t.Errorf("scrollback does not replay the earlier output: %q", res.Scrollback)
	}
	seen := outputsTo(r, "c1")
	if len(seen) == 0 || res.Seq != seen[len(seen)-1].Seq {
		t.Fatalf("attach seq %d is not the last delivered seq of %v", res.Seq, seen)
	}

	if err := m.Input(s.ID, "echo \"SECOND-$(printf %s CHUNK)\"\n"); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, r, "c2", "SECOND-CHUNK")
	more := outputsTo(r, "c2")
	var last uint64
	for _, p := range more {
		if p.Seq <= res.Seq || p.Seq <= last {
			t.Fatalf("seq is not monotonic after attach: %v then %d", more, p.Seq)
		}
		last = p.Seq
	}

	// And across the whole c1 stream, strictly increasing.
	var prev uint64
	for _, p := range outputsTo(r, "c1") {
		if p.Seq <= prev {
			t.Fatalf("seq %d did not exceed previous %d", p.Seq, prev)
		}
		prev = p.Seq
	}
	m.CloseAll()
}

func TestScrollbackIsCappedAtOneMebibyteAndKeepsTheTail(t *testing.T) {
	m, r := testManager(t) // default bounds: the REAL 1 MiB
	root := t.TempDir()
	s := openShell(t, m, root)
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	// One head marker, ~3 MiB of filler, one tail marker: the ring must
	// keep the tail and lose the head entirely.
	cmd := "printf 'SENTINEL-HEAD\\n'; yes xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx | head -c 3000000; printf 'TAIL-DONE\\n'\n"
	if err := m.Input(s.ID, cmd); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, r, "c1", "TAIL-DONE")
	waitFor(t, "output to settle", 5*time.Second, func() bool { // polls; generous because the full suite loads the CPU
		n := len(outputsTo(r, "c1"))
		time.Sleep(60 * time.Millisecond)
		return len(outputsTo(r, "c1")) == n
	})

	res, err := m.Attach("c2", s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scrollback) != DefaultScrollbackBytes {
		t.Errorf("scrollback is %d bytes, want the 1 MiB cap", len(res.Scrollback))
	}
	tail := string(res.Scrollback)
	if strings.Contains(tail, "SENTINEL-HEAD") {
		t.Error("the head survived the ring; it must keep the TAIL")
	}
	if !strings.Contains(tail, "TAIL-DONE") {
		t.Error("the tail was lost — the TAIL is the point")
	}
	m.CloseAll()
}

func TestExitIsAnnouncedWithItsCodeAndTheSessionIsKept(t *testing.T) {
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, "exit 3\n"); err != nil {
		t.Fatal(err)
	}
	var ep ExitParams
	waitFor(t, "terminal.exit", 8*time.Second, func() bool {
		ep, _ = exitFor(r, "c1")
		return true && ep.TerminalID != ""
	})
	if ep.ExitCode == nil || *ep.ExitCode != 3 {
		t.Fatalf("exit payload is %+v, want exitCode 3", ep)
	}
	if ep.Signal != "" {
		t.Errorf("a clean exit also carries signal %q", ep.Signal)
	}
	// LW9: kept (exited) until close or reap — list shows it, attach still
	// replays its scrollback, and input is refused BY NAME.
	views := m.List("")
	if len(views) != 1 || !views[0].Exited || views[0].ExitCode == nil || *views[0].ExitCode != 3 {
		t.Fatalf("list after exit is %+v, want one exited session with code 3", views)
	}
	res, err := m.Attach("c2", s.ID)
	if err != nil {
		t.Fatalf("attach to an exited-but-kept session: %v", err)
	}
	if !res.Exited || res.ExitCode == nil || *res.ExitCode != 3 {
		t.Errorf("attach result is %+v, want exited with code 3", res)
	}
	var exd *ExitedError
	if err := m.Input(s.ID, "echo nope\n"); asExited(err, &exd) {
		// good
	} else {
		t.Errorf("input after exit returned %v, want ExitedError", err)
	}
	m.CloseAll()
}

func TestExitBySignalCarriesItsSignalAndNoExitCode(t *testing.T) {
	// T8's contract from the other side: the Rust bridge DROPS an exit
	// frame with neither field, so this side must always emit one.
	//
	// SIGKILL, not SIGTERM: a job-control shell (interactive dash) IGNORES
	// SIGTERM — measured here; that is precisely why terminateGroup's second
	// pass is an unconditional SIGKILL in spawn.go and in terminate().
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, "kill -9 $$\n"); err != nil {
		t.Fatal(err)
	}
	var ep ExitParams
	waitFor(t, "terminal.exit by signal", 8*time.Second, func() bool {
		ep, _ = exitFor(r, "c1")
		return ep.TerminalID != ""
	})
	if ep.Signal == "" {
		t.Fatalf("signal death carried neither signal nor code: %+v", ep)
	}
	if ep.Signal != "killed" {
		t.Errorf("signal is %q, want killed", ep.Signal)
	}
	if ep.ExitCode != nil {
		t.Errorf("a signalled death reported exitCode %v; a signalled process has no code", *ep.ExitCode)
	}
	m.CloseAll()
}

// asExited is errors.As shaped for the named refusals without importing
// errors into every test.
func asExited(err error, target **ExitedError) bool {
	if e, ok := err.(*ExitedError); ok {
		*target = e
		return true
	}
	return false
}
