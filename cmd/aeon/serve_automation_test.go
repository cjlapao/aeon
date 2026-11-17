package main

// serve_automation_test.go — the wiring order is a property, so it is pinned
// like one: read serve.go and check the ORDER of the statements, not merely
// their presence. A dispatcher that starts before StartLoops finished
// reconciles claims against loops that recovery has not yet re-adopted —
// plausible, wrong, and exactly what a reviewer's eye misses in a diff.

import (
	"os"
	"strings"
	"testing"
)

func TestServeStartsAutomationAfterRecoveryAndJoinsItAtShutdown(t *testing.T) {
	src, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := "d.RunAutomation(recoveryCtx)"
	if n := strings.Count(s, start); n != 1 {
		t.Fatalf("%d occurrences of %q — the daemon must own exactly one dispatcher goroutine", n, start)
	}
	iStart := strings.Index(s, start)
	// Gated: it waits for the recovery goroutine, and the gate line sits
	// before the start it guards.
	iGate := strings.Index(s, "<-recoveryDone")
	if iGate < 0 || iGate > iStart {
		t.Fatal("the dispatcher must wait on <-recoveryDone BEFORE d.RunAutomation — never race loop recovery (AU13)")
	}
	if !strings.Contains(s, "if noRecover {\n\t\t\treturn\n\t\t}") {
		t.Fatal("--no-recover must skip automation too — a daemon that skips recovery also skips dispatch")
	}
	// Shutdown: the dispatcher is joined only after the recovery join, so
	// Reconcile's claims are settled before the transport starts closing.
	iAutoJoin := strings.Index(s, "<-autoDone")
	iRecoveryJoin := strings.LastIndex(s, "<-recoveryDone") // the shutdown-block join (gate is the first)
	if iAutoJoin < 0 {
		t.Fatal("no <-autoDone join — the dispatcher would outlive the daemon")
	}
	if iAutoJoin < iRecoveryJoin {
		t.Fatal("the dispatcher is joined before recovery — the shutdown order is recovery, then automation")
	}
	if !strings.Contains(s, "RunAutomation") || !strings.Contains(s, "automation dispatcher stopped") {
		t.Fatal("the dispatcher's stop must be an Infof line, not a fatal (the serve pattern)")
	}
}
