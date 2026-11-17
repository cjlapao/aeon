package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// TestPolicyRepoBridgeRoundTripsThroughTheRealStore exercises the CLI's own
// RepoFuncs bridge against real SQL: every hook converts its row shape both
// ways without losing a field, the error half of the closures fires on a
// closed store, and the whole conversion is total (the daemon's bridge has
// its own tests; this one belongs to the CLI because this bridge is a copy,
// and copies drift).
func TestPolicyRepoBridgeRoundTripsThroughTheRealStore(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open temp store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	pr := policyRepo(s)

	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-100 * 24 * time.Hour) // past the 90-day cap, for the trim leg

	row := policy.Row{
		ID: "bridge-deny", Type: "tool_access", TargetKind: "agent", TargetID: "bridge-test",
		Action: "deny", Source: "operator", Subject: "agent:bridge-test", Origin: "user",
		SpecJSON: `{"toolName":"bash","state":"deny"}`, CreatedAt: now.Format(time.RFC3339Nano),
	}
	if err := pr.Add(ctx, row); err != nil {
		t.Fatalf("Add through the bridge: %v", err)
	}
	rows, err := pr.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll through the bridge: %v", err)
	}
	if len(rows) != 1 || rows[0] != row {
		t.Fatalf("the bridge lost a field on the round trip:\n got %+v\nwant %+v", rows, row)
	}

	if err := pr.AcceptanceRecord(ctx, "bridge-deny", "agent", "bridge-test", "operator:admin", now); err != nil {
		t.Fatalf("AcceptanceRecord through the bridge: %v", err)
	}
	acc, err := pr.AcceptanceList(ctx, "agent", "bridge-test")
	if err != nil {
		t.Fatalf("AcceptanceList through the bridge: %v", err)
	}
	if len(acc) != 1 || acc[0].PolicyID != "bridge-deny" || acc[0].AcceptedBy != "operator:admin" {
		t.Fatalf("acceptance did not round-trip: %+v", acc)
	}

	if err := pr.EventInsert(ctx, "refusal", "agent:bridge-test", "bridge-deny", "the sentence", now); err != nil {
		t.Fatalf("EventInsert through the bridge: %v", err)
	}
	if err := pr.EventInsert(ctx, "refusal", "agent:old-timer", "bridge-deny", "the old sentence", old); err != nil {
		t.Fatalf("EventInsert (old row) through the bridge: %v", err)
	}
	events, err := pr.EventQuery(ctx, "agent:bridge-test", 10)
	if err != nil {
		t.Fatalf("EventQuery through the bridge: %v", err)
	}
	if len(events) != 1 || events[0].PolicyID != "bridge-deny" || events[0].Detail != "the sentence" {
		t.Fatalf("events did not round-trip: %+v", events)
	}
	depth, oldest, err := pr.EventStats(ctx, "")
	if err != nil {
		t.Fatalf("EventStats through the bridge: %v", err)
	}
	if depth != 2 || !oldest.Equal(old) {
		t.Errorf("stats wrong over the real table: depth=%d oldest=%v want 2/%v", depth, oldest, old)
	}
	// F-13 through the REAL store: the filtered answer is that subject's rows
	// only — its own depth and its own oldest — and a subject with no rows
	// answers the honest zero rather than borrowing the neighbour's numbers.
	d2, o2, err := pr.EventStats(ctx, "agent:bridge-test")
	if err != nil {
		t.Fatalf("filtered EventStats through the bridge: %v", err)
	}
	if d2 != 1 || !o2.Equal(now) {
		t.Errorf("filtered stats over the real table: depth=%d oldest=%v want 1/%v", d2, o2, now)
	}
	d3, o3, err := pr.EventStats(ctx, "agent:nobody")
	if err != nil {
		t.Fatalf("empty-subject EventStats through the bridge: %v", err)
	}
	if d3 != 0 || !o3.IsZero() {
		t.Errorf("a subject with no rows answered depth=%d oldest=%v; want 0/zero (the honest empty)", d3, o3)
	}
	if _, _, err := pr.EventStats(ctx, "agent"); err == nil ||
		!strings.Contains(err.Error(), "does not split") {
		t.Errorf("a half-subject depth read was not refused by name: %v", err)
	}
	n, err := pr.EventTrim(ctx, now)
	if err != nil {
		t.Fatalf("EventTrim through the bridge: %v", err)
	}
	if n != 1 {
		t.Errorf("the trim left the over-cap row behind (trimmed %d, want 1)", n)
	}

	// The remove half: true once, false once (the verb prints "no policy
	// called" from that false, so it must be a real answer, not a guess).
	if ok, err := pr.Remove(ctx, "bridge-deny"); err != nil || !ok {
		t.Fatalf("Remove of a live id: %v %v", ok, err)
	}
	if ok, err := pr.Remove(ctx, "bridge-deny"); err != nil || ok {
		t.Fatalf("Remove of a gone id claimed success: %v %v", ok, err)
	}

	// The error half of the converting closures: a cancelled context makes
	// every read hook return its error (never a silent empty), and the
	// closure converts it faithfully.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := pr.ListAll(cctx); err == nil {
		t.Error("ListAll with a cancelled context returned no error")
	}
	if _, err := pr.AcceptanceList(cctx, "agent", "x"); err == nil {
		t.Error("AcceptanceList with a cancelled context returned no error")
	}
	if _, err := pr.EventQuery(cctx, "", 5); err == nil {
		t.Error("EventQuery with a cancelled context returned no error")
	}
	if _, _, err := pr.EventStats(cctx, ""); err == nil {
		t.Error("EventStats with a cancelled context returned no error")
	}
	if _, err := pr.EventTrim(cctx, now); err == nil {
		t.Error("EventTrim with a cancelled context returned no error")
	}
}

// TestOpenPolicyBoundaryDirectlyReportsAStruckStore covers the production
// seam's failure path: the error from store.Open is wrapped ("open the
// store") and returned, not printed-and-forgotten, so every verb can fail
// loudly on it (TestBoundaryOpenFailureFailsEveryVerbLoudest pins the verbs
// half; this pins the opener half).
func TestOpenPolicyBoundaryDirectlyReportsAStruckStore(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker) // the state dir is now inside a FILE
	_, closeFn, err := openPolicyBoundaryDirectly()
	if err == nil {
		closeFn()
		t.Fatal("the direct open succeeded with the state dir inside a regular file")
	}
	if !strings.Contains(err.Error(), "open the store") {
		t.Errorf("the open failure did not name itself: %v", err)
	}
}
