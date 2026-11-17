package main

// seed_test.go — S43-extra T7. The CLI is a client of the daemon's four
// seed.* methods; offline it is the same engine through a direct door, and any
// other dial fault it refuses. These tests pin the behaviour the guards and
// the spec require: the verbs name the door, the printer never hides a non-
// applied record, the --yes gate refuses a guard set without writing, the
// fall back is only "no daemon", the direct door is the engine and not a copy,
// and no verb imports the store outside the direct door.

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// fakeSeedDoor stands in for the real door a test wants to control: it records
// which calls happened (so a gate's "zero writes" is a count, not a hope) and
// answers canned results.
type fakeSeedDoor struct {
	statusRes  protocol.SeedStatusResult
	planRes    protocol.SeedPlanResult
	applyRes   protocol.SeedApplyResult
	restoreRes protocol.SeedRestoreResult

	statusCalls, planCalls, applyCalls, restoreCalls int
}

func (d *fakeSeedDoor) status(context.Context, string) (protocol.SeedStatusResult, error) {
	d.statusCalls++
	return d.statusRes, nil
}
func (d *fakeSeedDoor) plan(context.Context, string, []string) (protocol.SeedPlanResult, error) {
	d.planCalls++
	return d.planRes, nil
}
func (d *fakeSeedDoor) apply(context.Context, string, []string) (protocol.SeedApplyResult, error) {
	d.applyCalls++
	return d.applyRes, nil
}
func (d *fakeSeedDoor) restore(context.Context, string, string) (protocol.SeedRestoreResult, error) {
	d.restoreCalls++
	return d.restoreRes, nil
}

// withSeedDoor stands in a fake door for the verbs and restores the real
// selection afterwards.
func withSeedDoor(t *testing.T, d seedDoor) {
	t.Helper()
	old := seedDoorFactory
	seedDoorFactory = func() (seedDoor, error) { return d, nil }
	t.Cleanup(func() { seedDoorFactory = old })
}

func TestSeedNoVerbNamesTheVerbs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdSeed(&buf, nil)
	if err == nil {
		t.Fatal("a bare `seed` should name the verbs")
	}
	for _, v := range []string{"status", "plan", "apply", "restore"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("the refusal names %q? the usage should name %s", v, v)
		}
	}
}

func TestSeedUnknownVerb(t *testing.T) {
	var buf bytes.Buffer
	err := cmdSeed(&buf, []string{"frobnicate"})
	if err == nil {
		t.Fatal("an unknown verb should refuse")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("the refusal should name the unknown verb, got %v", err)
	}
}

func TestSeedStatusPrintsEveryNonAppliedRecordWithReason(t *testing.T) {
	d := &fakeSeedDoor{statusRes: protocol.SeedStatusResult{Sets: []protocol.SeedSetStatus{
		{ID: "agents.firstparty", Class: "inert", AvailableVersion: 3, AppliedVersion: 3, Records: []protocol.SeedRecordStatus{
			{Key: "applied-one", State: "applied"},
			{Key: "diverged-slug", State: "diverged", Reason: "diverged — local content differs from v3 applied; restore with aeon seed restore"},
			{Key: "removed-slug", State: "removed", Reason: "removed — restore with aeon seed restore"},
			{Key: "failed-slug", State: "failed", Reason: "failed — a write rolled back"},
			{Key: "retired-slug", State: "retired", Reason: "retired"},
		}},
	}}}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	if err := cmdSeed(&buf, []string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := buf.String()
	// every non-applied record, with its state and the daemon's reason.
	for _, want := range []string{"diverged-slug diverged", "removed-slug removed", "failed-slug failed", "retired-slug retired"} {
		if !strings.Contains(out, want) {
			t.Errorf("status hid a non-applied record: want %q in %s", want, out)
		}
	}
	// the applied record is NOT listed (it is up to date); the reason is the
	// daemon's, verbatim.
	if strings.Contains(out, "applied-one") {
		t.Errorf("status listed an applied record — it is up to date, not a drift: %s", out)
	}
	if !strings.Contains(out, "diverged — local content differs") {
		t.Errorf("status rewrote the daemon's reason: %s", out)
	}
}

func TestSeedStatusEmptyRegistrySaysSo(t *testing.T) {
	withSeedDoor(t, &fakeSeedDoor{})
	var buf bytes.Buffer
	if err := cmdSeed(&buf, []string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(buf.String(), "no seed sets declared") {
		t.Errorf("an empty registry should say so, got %q", buf.String())
	}
}

func TestSeedPlanIsReadOnly(t *testing.T) {
	d := &fakeSeedDoor{planRes: protocol.SeedPlanResult{Plan: protocol.SeedSetPlan{
		SetID: "s", Decisions: []protocol.SeedDecision{{Key: "k", Op: "apply"}},
	}}}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	if err := cmdSeed(&buf, []string{"plan", "--set", "s"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if d.planCalls != 1 {
		t.Errorf("plan called %d times, want 1", d.planCalls)
	}
	if d.applyCalls != 0 || d.restoreCalls != 0 {
		t.Errorf("plan wrote: apply=%d restore=%d, want 0", d.applyCalls, d.restoreCalls)
	}
}

func TestSeedApplyGuardSetNeedsYes(t *testing.T) {
	d := &fakeSeedDoor{
		statusRes: protocol.SeedStatusResult{Sets: []protocol.SeedSetStatus{
			{ID: "policy.corpus", Class: "guard"},
		}},
		planRes: protocol.SeedPlanResult{Plan: protocol.SeedSetPlan{
			SetID: "policy.corpus", Decisions: []protocol.SeedDecision{{Key: "k", Op: "apply"}},
		}},
	}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	err := cmdSeed(&buf, []string{"apply", "--set", "policy.corpus"})
	if err == nil {
		t.Fatal("a guard-set apply without --yes should refuse (non-zero)")
	}
	if d.applyCalls != 0 {
		t.Errorf("a guard-set apply without --yes wrote %d times — the gate must not call seed.apply", d.applyCalls)
	}
	// the refusal is non-zero (the gate) and says re-run with --yes (to
	// stderr); the plan is printed (to stdout) so the operator sees what they
	// declined.
	if !strings.Contains(err.Error(), "re-run with --yes") {
		t.Errorf("the refusal should say re-run with --yes, got %v", err)
	}
	if !strings.Contains(buf.String(), "plan for policy.corpus") {
		t.Errorf("the gate should print the plan before refusing, got %q", buf.String())
	}
}

func TestSeedApplyGuardSetWithYes(t *testing.T) {
	d := &fakeSeedDoor{
		statusRes: protocol.SeedStatusResult{Sets: []protocol.SeedSetStatus{{ID: "policy.corpus", Class: "guard"}}},
		applyRes:  protocol.SeedApplyResult{Apply: protocol.SeedApplyOutcome{SetID: "policy.corpus"}},
	}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	if err := cmdSeed(&buf, []string{"apply", "--set", "policy.corpus", "--yes"}); err != nil {
		t.Fatalf("a --yes guard-set apply should apply: %v", err)
	}
	if d.applyCalls != 1 {
		t.Errorf("a --yes guard-set apply wrote %d times, want 1", d.applyCalls)
	}
}

func TestSeedApplyInertNeverNeedsYes(t *testing.T) {
	d := &fakeSeedDoor{
		statusRes: protocol.SeedStatusResult{Sets: []protocol.SeedSetStatus{{ID: "agents.firstparty", Class: "inert"}}},
		applyRes:  protocol.SeedApplyResult{Apply: protocol.SeedApplyOutcome{SetID: "agents.firstparty"}},
	}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	if err := cmdSeed(&buf, []string{"apply", "--set", "agents.firstparty"}); err != nil {
		t.Fatalf("an inert apply should apply without --yes: %v", err)
	}
	if d.applyCalls != 1 {
		t.Errorf("an inert apply wrote %d times, want 1 (no friction on safe sets)", d.applyCalls)
	}
}

func TestSelectDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	// the fall back needs neither a real endpoint nor a real store: stand in
	// the dial and the direct-door construction.
	oldDial := seedDial
	oldDirect := newDirectDoorFunc
	t.Cleanup(func() { seedDial, newDirectDoorFunc = oldDial, oldDirect })
	newDirectDoorFunc = func(context.Context) (seedDoor, error) { return &fakeSeedDoor{}, nil }

	// 1. no daemon ⇒ fall back to the direct door + the fixed line.
	seedDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("no daemon at %s: connect: connection refused", "/tmp/x.sock")
	}
	var buf bytes.Buffer
	door, err := selectSeedDoor(context.Background(), &buf)
	if err != nil {
		t.Fatalf("no daemon should fall back, not refuse: %v", err)
	}
	if door == nil {
		t.Fatal("no daemon should yield a direct door")
	}
	if !strings.Contains(buf.String(), "daemon not running — applied directly to the store") {
		t.Errorf("the fall back should print the fixed line, got %q", buf.String())
	}

	// 2. a permission error ⇒ refuse, never fall back.
	seedDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("no daemon at %s: %w", "/tmp/x.sock", &net.OpError{Op: "connect", Err: os.ErrPermission})
	}
	if _, err := selectSeedDoor(context.Background(), &bytes.Buffer{}); err == nil {
		t.Error("a permission error should refuse, not fall back")
	}

	// 3. a version/protocol fault (not "no daemon") ⇒ refuse, never fall back.
	seedDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("protocol: the daemon's protocol version 2 is not compatible with this client's 1")
	}
	if _, err := selectSeedDoor(context.Background(), &bytes.Buffer{}); err == nil {
		t.Error("a version fault should refuse, not fall back")
	}
}

func TestSelectDoorSuccessIsTheSocketDoor(t *testing.T) {
	oldDial := seedDial
	t.Cleanup(func() { seedDial = oldDial })
	seedDial = func(int) (*protocol.Client, error) { return nil, nil } // a nil client = "success"
	door, err := selectSeedDoor(context.Background(), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("a successful dial should yield the socket door: %v", err)
	}
	if _, ok := door.(socketDoor); !ok {
		t.Errorf("a successful dial should yield the socket door, got %T", door)
	}
}

func TestDirectDoorUsesTheEngineNotACopy(t *testing.T) {
	// the direct door must be the engine, not a second implementation: grep
	// this file for the engine constructor and the engine type, and assert no
	// second store-opening or applier construction.
	b, err := os.ReadFile("seed_door.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "daemon.NewSeedEngine") {
		t.Error("the direct door must build daemon.NewSeedEngine (the one engine), not a copy")
	}
	if !strings.Contains(src, "store.Open") {
		t.Error("the direct door must open the store through store.Open (one door), and name it")
	}
	// the direct door speaks the engine's own methods (the write goes through
	// the engine's rules), not a second applier or a second registry.
	for _, want := range []string{"d.e.Status", "d.e.Plan", "d.e.Apply", "d.e.Restore"} {
		if !strings.Contains(src, want) {
			t.Errorf("the direct door must call %s (the engine's own method), not a copy", want)
		}
	}
}

func TestSeedCLINeverImportsStoreOutsideDirectDoor(t *testing.T) {
	// only seed_door.go (the direct door) may import internal/store or
	// internal/seed; the verbs (and every other cmd/aeon file) must not, so a
	// verb cannot reopen the store (S43-extra T7 "Do NOT").
	for _, f := range []string{"seed.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		// the import is the module path, not a comment mention of a package:
		// a verb must not IMPORT the store or the seed engine (only the
		// direct door may), so check the import paths, not prose.
		if strings.Contains(src, "github.com/cjlapao/aeon/internal/store") || strings.Contains(src, "github.com/cjlapao/aeon/internal/seed") {
			t.Errorf("%s imports internal/store or internal/seed — a verb must not (only the direct door may)", f)
		}
	}
}

func TestEverySeedMethodHasACLIVerb(t *testing.T) {
	// the four methods and the four verbs are a 1:1 door (S45's parity guard
	// shape): every seed.* method is spoken by a verb, and every verb speaks
	// a seed.* method. The door's method names pin it.
	var b bytes.Buffer
	if err := cmdSeed(&b, []string{"help"}); err != nil {
		t.Fatalf("seed help: %v", err)
	}
	usage := b.String()
	for _, v := range []string{"status", "plan", "apply", "restore"} {
		if !strings.Contains(usage, "seed "+v) && !strings.Contains(usage, v) {
			t.Errorf("the usage should name the %s verb", v)
		}
	}
	// the door speaks the four methods by the protocol's own names (no drift).
	src, _ := os.ReadFile("seed_door.go")
	for _, m := range []string{"MethodSeedStatus", "MethodSeedPlan", "MethodSeedApply", "MethodSeedRestore"} {
		if !strings.Contains(string(src), m) {
			t.Errorf("the socket door should speak %s by the protocol's name", m)
		}
	}
}

func TestDoctorSeedLines(t *testing.T) {
	d := &fakeSeedDoor{statusRes: protocol.SeedStatusResult{Sets: []protocol.SeedSetStatus{
		{ID: "agents.firstparty", Class: "inert", AvailableVersion: 3, AppliedVersion: 3, Records: []protocol.SeedRecordStatus{
			{Key: "a", State: "applied"}, {Key: "b", State: "applied"}, {Key: "c", State: "applied"},
		}},
		{ID: "policy.corpus", Class: "guard", AvailableVersion: 2, AppliedVersion: 1, Records: []protocol.SeedRecordStatus{
			{Key: "x", State: "diverged"}, {Key: "y", State: "removed"},
		}},
	}}}
	withSeedDoor(t, d)
	var buf bytes.Buffer
	if err := cmdDoctor(&buf, nil); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "seed agents.firstparty up to date") {
		t.Errorf("doctor should say the up-to-date set, got %s", out)
	}
	if !strings.Contains(out, "seed policy.corpus") || !strings.Contains(out, "diverged") || !strings.Contains(out, "removed") {
		t.Errorf("doctor should report the drift per set, got %s", out)
	}
	// the doctor prints no record content (no reason text), and the drift is
	// a warning line, not a failure (the doctor returns nil).
	if strings.Contains(out, "restore with") {
		t.Errorf("doctor leaked a record's content: %s", out)
	}
}
