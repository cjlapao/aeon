//go:build !windows

package terminal

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUnknownTerminalIdsAreRefusedByName(t *testing.T) {
	m, _ := testManager(t)
	for name, call := range map[string]func() error{
		"input":  func() error { return m.Input("t-nope", "x") },
		"resize": func() error { return m.Resize("t-nope", 80, 24) },
		"attach": func() error { _, err := m.Attach("c1", "t-nope"); return err },
		"detach": func() error { return m.Detach("c1", "t-nope") },
		"close":  func() error { _, err := m.Close("t-nope"); return err },
	} {
		err := call()
		var nf *NotFoundError
		if !errors.As(err, &nf) {
			t.Errorf("%s on an unknown id returned %v, want *NotFoundError", name, err)
			continue
		}
		if !strings.Contains(err.Error(), "t-nope") {
			t.Errorf("%s refusal does not name the id: %v", name, err)
		}
	}
}

func TestNinthTerminalOnOneRootIsRefusedNamingRootAndCount(t *testing.T) {
	// LW10: ≤ 8 per project root, refused WITH the root and the count.
	m, _ := testManager(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	for i := 0; i < DefaultMaxPerRoot; i++ {
		if _, err := m.Open(OpenParams{ComponentRoot: rootA, Cols: 80, Rows: 24}); err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
	}
	_, err := m.Open(OpenParams{ComponentRoot: rootA, Cols: 80, Rows: 24})
	var lim *LimitError
	if !errors.As(err, &lim) {
		t.Fatalf("the 9th open returned %v, want *LimitError", err)
	}
	if lim.Root != rootA || lim.Count != 8 || lim.Limit != 8 {
		t.Errorf("refusal names root=%q count=%d limit=%d, want %q/8/8", lim.Root, lim.Count, lim.Limit, rootA)
	}
	if !strings.Contains(err.Error(), rootA) || !strings.Contains(err.Error(), "8") {
		t.Errorf("the message does not name root and count: %v", err)
	}
	// A DIFFERENT root is not blocked by this root's fullness.
	if _, err := m.Open(OpenParams{ComponentRoot: rootB, Cols: 80, Rows: 24}); err != nil {
		t.Errorf("a second root was blocked by the first's bound: %v", err)
	}
	// Closing frees a slot (the registry, not a counter, holds the truth).
	for _, v := range m.List(rootA) {
		if _, err := m.Close(v.TerminalID); err != nil {
			t.Fatal(err)
		}
		break
	}
	if _, err := m.Open(OpenParams{ComponentRoot: rootA, Cols: 80, Rows: 24}); err != nil {
		t.Errorf("open after a close was still refused: %v", err)
	}
	m.CloseAll()
}

func TestInputMustBeAValidUTF8String(t *testing.T) {
	// Q3/LW10: input is a UTF-8 STRING (output is base64 the other way).
	m, _ := testManager(t)
	s := openShell(t, m, t.TempDir())
	err := m.Input(s.ID, "echo \xff\xfe-not-utf8\n")
	var bi *BadInputError
	if !errors.As(err, &bi) {
		t.Fatalf("invalid UTF-8 input returned %v, want *BadInputError", err)
	}
	if !strings.Contains(err.Error(), s.ID) {
		t.Errorf("the refusal does not name the terminal: %v", err)
	}
	m.CloseAll()
}

func TestDetachStopsDeliveryWhileTheShellKeepsRunning(t *testing.T) {
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach("c2", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Detach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, "echo AFTER-DETACH\n"); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, r, "c2", "AFTER-DETACH")
	if strings.Contains(decodedOutputs(r, "c1"), "AFTER-DETACH") {
		t.Error("a detached connection still receives output")
	}
	// LW9: detached is not dead. c2 still counts the session as attached;
	// only when the LAST attacher goes does Attached fall.
	views := m.List("")
	if len(views) != 1 || views[0].Exited || !views[0].Attached {
		t.Fatalf("list after one detach is %+v, want one running still-attached session", views)
	}
	if err := m.Detach("c2", s.ID); err != nil {
		t.Fatal(err)
	}
	views = m.List("")
	if len(views) != 1 || views[0].Exited || views[0].Attached {
		t.Fatalf("list after the last detach is %+v, want one running detached session", views)
	}
	m.CloseAll()
}

func TestDetachConnLeavesNothingAttachedButAlive(t *testing.T) {
	// The onConnectionClosed hook, minus the daemon (the daemon-level test
	// drives the hook itself).
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	m.DetachConn("c1")
	m.DetachConn("c-none") // attached to nothing: never an error
	if err := m.Input(s.ID, "echo STILL-ALIVE\n"); err != nil {
		t.Fatalf("input after the connection closed: %v", err)
	}
	waitFor(t, "the still-alive shell to run the command", 8*time.Second, func() bool {
		return len(m.List("")) == 1 && !m.List("")[0].Exited
	})
	if len(outputsTo(r, "c1")) != 0 {
		t.Error("output reached the connection that closed")
	}
	m.CloseAll()
}

func TestReaperTakesADetachedSessionPastTheTimeout(t *testing.T) {
	// LW9 with an injected clock and interval: ReapOnce sweeps exactly the
	// detached-and-idle ones, kills their groups, and leaves attached shells
	// alone no matter how old.
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

	idle := openShell(t, m, t.TempDir()) // never attached → idle since open
	kept := openShell(t, m, t.TempDir()) // attached → survives the sweep
	if _, err := m.Attach("c1", kept.ID); err != nil {
		t.Fatal(err)
	}

	now = now.Add(16 * time.Minute)
	if n := m.ReapOnce(); n != 1 {
		t.Fatalf("reaped %d, want 1", n)
	}
	views := m.List("")
	if len(views) != 1 || views[0].TerminalID != kept.ID {
		t.Fatalf("list after reap is %+v, want only the attached session", views)
	}
	// The reaped process was actually reaped and killed, not just forgotten.
	select {
	case <-idle.waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the reaped session's process was never waited on — terminate did not run")
	}

	// Detach restarts the idle clock: detach now, wait past the timeout with
	// the clock, and it goes.
	if err := m.Detach("c1", kept.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	if n := m.ReapOnce(); n != 1 {
		t.Fatalf("reaped %d on the second sweep, want 1", n)
	}
	if got := len(m.List("")); got != 0 {
		t.Fatalf("list is %d, want empty", got)
	}
}

func TestOutputIsCoalescedNotOneNotificationPerRead(t *testing.T) {
	// LW10's bound, asserted as a property (the exact 16 ms/32 KiB split is
	// timing and would be a flaky assertion; the CAPS are not): every
	// notification carries at most the read buffer, and a 100 KiB burst
	// arrives in a handful of chunks, not hundreds.
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, "yes aaaaaaaaaa | head -c 100000; echo BURST-DONE\n"); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, r, "c1", "BURST-DONE")
	outs := outputsTo(r, "c1")
	if len(outs) == 0 {
		t.Fatal("no output")
	}
	for _, p := range outs {
		b, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > DefaultFlushBytes {
			t.Errorf("a notification carried %d bytes, over the %d cap", len(b), DefaultFlushBytes)
		}
	}
	if len(outs) > 40 {
		t.Errorf("%d notifications for 100 KiB — coalescing did not coalesce", len(outs))
	}
	m.CloseAll()
}

func TestTheRingKeepsTheTailAndWraps(t *testing.T) {
	// The 1 MiB behaviour above is asserted end to end; this pins the wrap
	// arithmetic the big test cannot see.
	rr := NewRing(8)
	rr.Write([]byte("12345"))
	if got := string(rr.Bytes()); got != "12345" {
		t.Errorf("fill got %q", got)
	}
	rr.Write([]byte("67890")) // wraps: last 8 of "1234567890"
	if got := string(rr.Bytes()); got != "34567890" {
		t.Errorf("wrap got %q, want 34567890", got)
	}
	rr.Write([]byte("ABCDEFGHIJKLMNOP")) // longer than the ring: tail only
	if got := string(rr.Bytes()); got != "IJKLMNOP" {
		t.Errorf("oversize write got %q, want the tail IJKLMNOP", got)
	}
	if rr.Len() != 8 {
		t.Errorf("Len is %d, want the cap 8", rr.Len())
	}
}

func TestCloseReportsExitCodeOnlyWhenAlreadyExited(t *testing.T) {
	m, r := testManager(t)
	s := openShell(t, m, t.TempDir())
	if _, err := m.Attach("c1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, "exit 0\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "exit", 8*time.Second, func() bool { _, ok := exitFor(r, "c1"); return ok })
	res, err := m.Close(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Errorf("close of an already-exited session reported %+v, want exitCode 0", res)
	}
	if _, err := m.Close(s.ID); err == nil {
		t.Error("a second close found the session; close removes it")
	}

	// Closing a LIVE shell kills it and reports no code (it died by signal).
	live := openShell(t, m, t.TempDir())
	liveRes, err := m.Close(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if liveRes.ExitCode != nil {
		t.Errorf("a killed shell reported exitCode %v", *liveRes.ExitCode)
	}
	select {
	case <-live.waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not wait for the child to die")
	}
}
