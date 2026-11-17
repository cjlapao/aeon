//go:build !windows

package terminal

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// # The group-kill, measured the way spawn_test.go measured it
//
// A return code tells you the parent died and says NOTHING about its
// children; a shell that backgrounds a server and exits leaves the server
// holding its port. So: the test binary re-executes itself as a TCP port
// holder (grandchild, via the shell), the group is killed, and the port —
// not a return code — is the proof. Negative control included: killing only
// the PARENT must leave the port held, or the positive test proves nothing.
//
// Setsid (creack/pty) makes the shell its own group leader exactly the way
// Setpgid does for spawn.go, so `Kill(-pgid)` must reach the grandchild —
// and this file is where that equivalence is MEASURED rather than asserted.

const (
	portHolderEnv = "AEON_TEST_TERMINAL_PORT_HOLDER"
	portReadyEnv  = "AEON_TEST_TERMINAL_PORT_READY"
)

func TestMain(m *testing.M) {
	if port := os.Getenv(portHolderEnv); port != "" {
		runPortHolder(port, os.Getenv(portReadyEnv))
		return
	}
	os.Exit(m.Run())
}

func runPortHolder(port, readyPath string) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	if readyPath != "" {
		_ = os.WriteFile(readyPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	time.Sleep(10 * time.Second)
	_ = ln.Close()
	os.Exit(0)
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	return port
}

func portHeld(port string) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// backgroundHolder starts the port holder as a BACKGROUND grandchild of the
// session's shell and waits until it provably holds the port. The holder's
// pid lands in readyPath so the negative-control test can clean up a
// reparented survivor.
func backgroundHolder(t *testing.T, m *Manager, r *recorder, connID string) (*Session, string, string) {
	t.Helper()
	port := freePort(t)
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv(portHolderEnv, port)
	t.Setenv(portReadyEnv, ready)

	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach(connID, s.ID); err != nil {
		t.Fatal(err)
	}
	// Single quotes keep the path intact; `&` backgrounds it inside the PTY
	// shell — same group, same session, exactly the survivor scenario.
	cmd := "echo HOLDER-UP; '" + os.Args[0] + "' & sleep 30\n"
	if err := m.Input(s.ID, cmd); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the grandchild to take the port", 8*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil && portHeld(port)
	})
	return s, port, ready
}

func killHolderByPidFile(t *testing.T, ready string) {
	t.Helper()
	raw, err := os.ReadFile(ready)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestCloseKillsTheWHOLEGroupProvenByAFreedPort(t *testing.T) {
	// The positive half (LW9: close kills a backgrounded child).
	m, r := testManager(t)
	s, port, ready := backgroundHolder(t, m, r, "c1")
	defer killHolderByPidFile(t, ready)

	if _, err := m.Close(s.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitFor(t, "the port to be freed by the GROUP kill", 8*time.Second, func() bool {
		return !portHeld(port)
	})
	if _, ok := exitFor(r, "c1"); !ok {
		t.Error("the killed session never announced terminal.exit")
	}
}

func TestCloseAllKillsEveryGroupOnShutdown(t *testing.T) {
	// The daemon-shutdown path: two sessions, each with a backgrounded
	// holder; CloseAll takes both groups (concurrently, so one grace is
	// waited, not two).
	m, r := testManager(t)
	s1, port1, ready1 := backgroundHolder(t, m, r, "c1")
	s2, port2, ready2 := backgroundHolder(t, m, r, "c2")
	defer killHolderByPidFile(t, ready1)
	defer killHolderByPidFile(t, ready2)

	done := make(chan struct{})
	go func() { m.CloseAll(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("CloseAll hung")
	}
	waitFor(t, "both ports freed", 8*time.Second, func() bool {
		return !portHeld(port1) && !portHeld(port2)
	})
	if len(m.List("")) != 0 {
		t.Error("CloseAll left sessions in the registry")
	}
	_ = s1
	_ = s2
}

func TestReaperKillsTheWholeGroupOfADetachedSession(t *testing.T) {
	// LW9's second half, measured with the same instrument: a DETACHED
	// session past its timeout is not just forgotten — its group dies.
	t.Setenv("SHELL", "/bin/sh")
	now := time.Now()
	r := &recorder{}
	m := New(Options{
		Notifier:      r.notify,
		KillGrace:     100 * time.Millisecond,
		DetachTimeout: 15 * time.Minute,
		ReapInterval:  time.Hour,
		Now:           func() time.Time { return now },
	})
	s, port, ready := backgroundHolder(t, m, r, "c1")
	defer killHolderByPidFile(t, ready)
	if err := m.Detach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	if n := m.ReapOnce(); n != 1 {
		t.Fatalf("reaped %d, want 1", n)
	}
	waitFor(t, "the reaped group's port to be freed", 8*time.Second, func() bool {
		return !portHeld(port)
	})
}

func TestKillingOnlyTheParentLeavesThePortHeld(t *testing.T) {
	// The negative control (spawn_test.go's, re-run here for the PTY path).
	// Without this, the group-kill test above would pass against an
	// implementation that kills only the shell — the shell dies either way.
	m, r := testManager(t)
	s, port, ready := backgroundHolder(t, m, r, "c1")
	defer killHolderByPidFile(t, ready)

	// Kill the PROCESS only — never the group. (This bypasses Manager on
	// purpose; it is what a naive implementation would do.)
	if s.cmd == nil || s.cmd.Process == nil {
		t.Fatal("no child to control")
	}
	if err := s.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-s.waitDone
	time.Sleep(300 * time.Millisecond)
	if !portHeld(port) {
		t.Fatal("the port was freed by killing only the parent — the control is not controlling " +
			"anything, and the group-kill tests prove nothing")
	}
	// Clean up the survivor by its own pid (from the ready file), then the
	// registry entry.
	killHolderByPidFile(t, ready)
	waitFor(t, "the port to free after the survivor dies", 8*time.Second, func() bool {
		return !portHeld(port)
	})
	if _, err := m.Close(s.ID); err != nil {
		t.Errorf("closing the half-dead session: %v", err)
	}
}
