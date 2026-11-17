package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// scheduled_fire_test.go — the clock's LIVE leg (P34's proof-by-process):
// a schedule row in a real store, a real `aeon serve` process, and a fire
// that starts a real loop through the daemon's own path. Everything the
// daemon-package tests prove about rows is proven HERE about the process:
// the scheduler really runs on daemon start (StartLoops beside the reaper),
// really answers due instants within its tick, and really cannot make a
// loop attended, outward-enabled, or anonymous about who started it.
//
// The blueprint is S16's outward fixture — `forge.openPR`, effect class
// outward — so the fired loop's first step meets the wall the design is
// about: unattended outward is denied at once (measured as DENY-AT-ONCE,
// not the design's word "suspend": the loop parks `blocked` carrying the
// S10 sentence; nothing runs outwardly — R-14).

func TestScheduledFireIsUnattendedAndStopsAtOutwardLive(t *testing.T) {
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	ws := t.TempDir()
	bpPath, err := filepath.Abs(filepath.Join("..", "..", "internal", "blueprint", "testdata", "code-action.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Seed BEFORE the daemon starts — the schedule is owed answers from the
	// moment the process is up, and covered_until 90 s back means the first
	// tick (≤15 s) has a due instant to answer.
	s, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: "slow-walk", Kind: "schedule", Blueprint: bpPath, Workspace: ws,
		Expr: "*/1 * * * *", Zone: "UTC", CreatedAt: now.Format(time.RFC3339),
		CoveredUntil: now.Add(-90 * time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	// T7's default deny makes a fresh install fire NOTHING — both live
	// legs author the operator's allow BEFORE the process starts, which
	// is also the honest demo procedure.
	ps := policy.NewStore(policyRepo(s))
	if rej := ps.Add(context.Background(), policy.Policy{
		ID: "allow-schedule-live", Type: policy.TriggerAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"trigger":"slow-walk","state":"allow"}`),
	}, now); rej != nil {
		t.Fatalf("seeding the allow: %s", rej.Reason)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The process, exactly the S19 harness's way: isolated endpoint and
	// state, provider keys REMOVED (I7), logs to a file for the record.
	logPath := filepath.Join(base, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(base, "aeon.sock")
	cmd := exec.Command(aeonBinary, "serve")
	cmd.Env = append(os.Environ(),
		"LOCALRPC_ENDPOINT_PATH="+endpoint,
		"XDG_STATE_HOME="+stateDir,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		logFile.Close()
	}()

	s, err = store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	// Poll for the fired answer. The poll OBSERVES (the tick is the
	// daemon's); it does not make the fire happen (D14's line).
	var loopID string
	deadline := time.Now().Add(45 * time.Second)
	for loopID == "" {
		hist, err := s.TriggerFireHistory(ctx, "slow-walk", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hist {
			if h.Outcome == "fired" && h.LoopID != "" {
				loopID = h.LoopID
				break
			}
		}
		if loopID == "" {
			if time.Now().After(deadline) {
				raw, _ := os.ReadFile(logPath)
				t.Fatalf("no fired row within 45 s — daemon log:\n%s", raw)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	// The three questions, read off the row the PROCESS wrote.
	attended, err := s.LoopAttended(ctx, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if attended == nil || *attended {
		t.Fatalf("live: clock-started loop attended=%v — the start authors unattended", attended)
	}
	outward, err := s.LoopAllowOutward(ctx, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if outward {
		t.Fatal("live: a schedule granted outward — there is no field to ask with")
	}
	startedBy, triggerID, err := s.LoopStartedBy(ctx, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "trigger" || triggerID != "slow-walk" {
		t.Fatalf("live: provenance %q/%q", startedBy, triggerID)
	}

	// And the outward wall catches it: the loop parks blocked carrying the
	// deny sentence, never having run anything outwardly.
	deadline = time.Now().Add(30 * time.Second)
	var status, reason string
	for {
		pin, err := s.ReadPinnedLoop(loopID)
		if err == nil {
			status = pin.Loop.Status
			if st, err := loop.FromPinned(pin); err == nil {
				reason = st.LastReason
			}
			if status == "blocked" {
				break
			}
			if status == "done" || status == "failed" {
				t.Fatalf("the outward step did not park the loop; it went %q (reason %q)", status, reason)
			}
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(logPath)
			t.Fatalf("loop never parked at the outward wall (status %q reason %q) — daemon log:\n%s", status, reason, raw)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !strings.Contains(reason, "outward") || !strings.Contains(reason, "deni") {
		t.Fatalf("the blocked loop must carry the outward deny sentence (R-14's measured reality), got %q", reason)
	}
	// Nothing spawned: no receipt row exists for any step of the fired
	// loop (S11's structural order — verdict first, receipt before exec; a
	// denied command provably never started).
	pin, err := s.ReadPinnedLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := s.ListSteps(store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range steps {
		receipts, err := s.Receipts(ctx, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(receipts) != 0 {
			t.Fatalf("an outward step DENIED left %d receipts on step %d — a denied command provably never started", len(receipts), st.ID)
		}
	}
}

// TestReconcileAtStartupIsOneCatchUpLive — TR3 proved against the process,
// not the row: the daemon is GENUINELY down for minutes (the seeded
// frontier sits three minutes back before the binary ever starts), and the
// startup reconcile must answer that outage with exactly ONE fire and ONE
// range row — the burst case in its real shape. The T4 unit tests walk an
// injected instant; this one walks a real silence.
func TestReconcileAtStartupIsOneCatchUpLive(t *testing.T) {
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	ws := t.TempDir()
	bpPath, err := filepath.Abs(filepath.Join("..", "..", "internal", "blueprint", "testdata", "code-action.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// Three minutes of silence the daemon never saw — real down-time, not a
	// fixture timestamp the process is invited to believe.
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: "slow-walk", Kind: "schedule", Blueprint: bpPath, Workspace: ws,
		Expr: "*/1 * * * *", Zone: "UTC", CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		CoveredUntil: now.Add(-3 * time.Minute).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	// T7's default deny makes a fresh install fire NOTHING — both live
	// legs author the operator's allow BEFORE the process starts, which
	// is also the honest demo procedure.
	ps := policy.NewStore(policyRepo(s))
	if rej := ps.Add(context.Background(), policy.Policy{
		ID: "allow-schedule-live", Type: policy.TriggerAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"trigger":"slow-walk","state":"allow"}`),
	}, now); rej != nil {
		t.Fatalf("seeding the allow: %s", rej.Reason)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	logFile, err := os.Create(filepath.Join(base, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(aeonBinary, "serve")
	cmd.Env = append(os.Environ(),
		"LOCALRPC_ENDPOINT_PATH="+filepath.Join(base, "aeon.sock"),
		"XDG_STATE_HOME="+stateDir,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		logFile.Close()
	}()

	s, err = store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	// Reconcile runs BEFORE the ticker — the answer must be on disk within
	// the startup second, not at the first 15 s tick.
	deadline := time.Now().Add(10 * time.Second)
	var fires []store.TriggerFireRow
	var gaps []store.TriggerGapRow
	for {
		fires, err = s.TriggerFireHistory(ctx, "slow-walk", 50)
		if err != nil {
			t.Fatal(err)
		}
		gaps, err = s.TriggerGaps(ctx, "slow-walk", 50)
		if err != nil {
			t.Fatal(err)
		}
		var fired bool
		for _, f := range fires {
			if f.Outcome == "fired" && f.LoopID != "" {
				fired = true
			}
		}
		if fired && len(gaps) > 0 {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(filepath.Join(base, "daemon.log"))
			t.Fatalf("startup reconcile left fires=%+v gaps=%+v — log:\n%s", fires, gaps, raw)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// ONE fire caught up the three-minute silence; the middle is a single
	// range row (0034's shape) with the honest count — not a row per instant.
	var firedCount int
	var loopID string
	for _, f := range fires {
		if f.Outcome == "fired" {
			firedCount++
			loopID = f.LoopID
		}
	}
	if firedCount != 1 {
		t.Fatalf("three missed minutes fired %d loops — TR3 says one, always one", firedCount)
	}
	if len(gaps) != 1 {
		t.Fatalf("the missed middle is ONE range row, got %+v", gaps)
	}
	if gaps[0].Missed < 1 {
		t.Fatalf("the range row must carry the honest count, got %d", gaps[0].Missed)
	}
	startedBy, triggerID, err := s.LoopStartedBy(ctx, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "trigger" || triggerID != "slow-walk" {
		t.Fatalf("the catch-up loop carries provenance %q/%q — reconcile fires through the SAME door", startedBy, triggerID)
	}
}
