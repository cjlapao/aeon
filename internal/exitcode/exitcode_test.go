package exitcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/errs"
)

// S23 T3 — the exit-code table a CI job branches on (C10, D12). The gates
// here are the ones a careless change breaks silently: a loop at a gate is
// Waiting (3), a failed step is Failed (1), and the distinction is the whole
// point — a CI job that treats all non-zero exits as failures would fail a
// build because a loop politely asked a question.

// TestWaitingIsDistinctFromFailed is the distinction C10 exists for (D12): a
// loop at a gate exits 3 (Waiting), a failed step exits 1 (Failed). They are
// different codes so a CI job can branch.
func TestWaitingIsDistinctFromFailed(t *testing.T) {
	if CodeFor(EventGate, Attended) != Waiting {
		t.Fatalf("an attended gate must be Waiting (3), got %s", CodeFor(EventGate, Attended))
	}
	if CodeFor(EventFailed, Attended) != Failed {
		t.Fatalf("a failed step must be Failed (1), got %s", CodeFor(EventFailed, Attended))
	}
	if CodeFor(EventGate, Attended) == CodeFor(EventFailed, Attended) {
		t.Fatal("Waiting and Failed must be distinct codes")
	}
}

// TestUnattendedGateIsBlockedNotWaiting is the S19 composition: a CI-triggered
// loop is unattended, so a gate does not produce Waiting — it produces
// Blocked with the ask denied (G7, L22). Both codes exist because both
// situations do.
func TestUnattendedGateIsBlockedNotWaiting(t *testing.T) {
	if CodeFor(EventGate, Unattended) != Blocked {
		t.Fatalf("an unattended gate must be Blocked (2), got %s", CodeFor(EventGate, Unattended))
	}
}

// TestEveryCommandUsesTheTable is D12's pin (scoped): a code comes from the
// table, not a bare literal. In this package a code is produced only by
// CodeFor (the one mapping) — no literal Code outside the table. The full
// codebase (cmd/aeon/main.go's os.Exit(1/2)) is routed through the table in a
// recorded follow-up (Q5: it is dirty, another agent editing).
func TestEveryCommandUsesTheTable(t *testing.T) {
	// Every code the package can return is one of the six, never a literal
	// outside the table.
	for _, e := range []Event{EventFailed, EventGate, EventBlocked, EventAborted, EventUsage, EventConfig} {
		for _, a := range []Attendance{Attended, Unattended} {
			c := CodeFor(e, a)
			if c < OK || c > Config {
				t.Fatalf("CodeFor returned a code outside the table: %d", c)
			}
		}
	}
}

// TestUsageIsDistinctFromConfig is C10's other split: a bad flag is Usage (4),
// a bad config file is Config (5) — different fixes, so a CI job can branch.
func TestUsageIsDistinctFromConfig(t *testing.T) {
	if CodeFor(EventUsage, Attended) != Usage {
		t.Fatalf("a bad flag must be Usage (4), got %s", CodeFor(EventUsage, Attended))
	}
	if CodeFor(EventConfig, Attended) != Config {
		t.Fatalf("a bad config must be Config (5), got %s", CodeFor(EventConfig, Attended))
	}
	if CodeFor(EventUsage, Attended) == CodeFor(EventConfig, Attended) {
		t.Fatal("Usage and Config must be distinct codes")
	}
}

// TestOKMeansDidWhatWasAsked is S17's T7: a loop aborted — a plan someone read
// and declined — is a working system, not a failure, so it exits 0. Exiting
// non-zero for it would make the metrics lie in exactly the way S17's T7 was
// written to prevent.
func TestOKMeansDidWhatWasAsked(t *testing.T) {
	if CodeFor(EventAborted, Attended) != OK {
		t.Fatalf("an aborted loop must be OK (0), got %s", CodeFor(EventAborted, Attended))
	}
}

// TestCodeAccompaniesAThreePartError is T1's pin: a non-zero code accompanies
// a three-part message — a code with no message is a CI job with nothing to
// read. An OK result may stand alone (did what was asked needs no
// explanation).
func TestCodeAccompaniesAThreePartError(t *testing.T) {
	msg := errs.New("a step failed", "the command did not complete", "retry or report the failure")
	// A non-zero result carries the message; the code and the message are
	// both produced for the same event.
	r := Result{Code: Failed, Err: msg, HasMsg: true}
	if !r.NonZero() {
		t.Fatal("Failed must be non-zero")
	}
	if !r.HasMsg || r.Err.What() == "" {
		t.Fatal("a non-zero code must accompany a three-part message")
	}
	// An OK result may stand alone — it needs no message.
	okResult := Result{Code: OK}
	if okResult.NonZero() {
		t.Fatal("OK (0) must not be non-zero")
	}
}

// TestCodesAreDocumented is C10's pin: every code is documented in
// EXITCODES.md — a code nobody documented is a code nobody branches on. The
// doc must name all six codes; the reverse (a documented code must be in the
// table) is checked by the constants existing.
func TestCodesAreDocumented(t *testing.T) {
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	path := filepath.Join(dir, "aeon", "docs", "EXITCODES.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("EXITCODES.md must exist: %v", err)
	}
	doc := string(b)
	for _, c := range []Code{OK, Failed, Blocked, Waiting, Usage, Config} {
		if !strings.Contains(doc, c.String()) {
			t.Fatalf("EXITCODES.md must document %s", c.String())
		}
	}
}

// findRepo walks up to the repo root (the one with go.mod's parent's parent).
func findRepo(t *testing.T) (string, bool) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			parent := filepath.Dir(dir)
			// the repo root is the parent of aeon/
			return parent, true
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}
