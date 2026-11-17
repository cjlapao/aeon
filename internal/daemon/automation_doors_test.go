package daemon

// automation_doors_test.go — S49 T8: the offline faces of automation_doors.go
// answer like the socket answers, because they ARE the socket's bodies —
// the add/pause sentences arrive from control.go, the list and history map
// through the shared renderers, and archive carries the health it just
// changed. Fire-now is the named exception: it REFUSES offline, in this
// plane's own words, and writes nothing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// offlineCLI is the actor the CLI's direct door carries (design §28's
// person door; the store's requireUser and the frozen created_by_kind CHECK
// admit `user` only — AU1's locks).
var offlineCLI = protocol.Actor{Kind: "user", ID: "cli"}

func TestOfflineControlDoorsAnswerLikeTheSocket(t *testing.T) {
	s, _ := writeWorld(t)
	d := NewAutomationControlDoors(s)
	ctx := context.Background()

	res, err := d.Add(ctx, offlineCLI, protocol.TriggerAddParams{
		ID: "doors-nightly", Blueprint: "dev-loop", Expr: "0 7 * * *", Zone: "UTC", Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("offline add: %v", err)
	}
	if !res.Found || res.TriggerID != "doors-nightly" {
		t.Fatalf("offline add answered %+#v — the validator's answer must arrive whole", res)
	}

	lr, err := d.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var row *protocol.TriggerListView
	for i := range lr.Triggers {
		if lr.Triggers[i].ID == "doors-nightly" {
			row = &lr.Triggers[i]
		}
	}
	if row == nil {
		t.Fatal("the offline List does not see the row the offline Add wrote")
	}
	if row.NextFire == "" {
		t.Error("the offline List stopped computing next-fires — it must run the same library the clock uses")
	}

	pr, err := d.Pause(ctx, offlineCLI, "doors-nightly")
	if err != nil {
		t.Fatalf("offline pause: %v", err)
	}
	if !pr.Found || !strings.Contains(pr.Reason, "paused") {
		t.Fatalf("offline pause answered %+#v — control.go's sentence, verbatim, is required", pr)
	}
	// The audit line carries the pause AND the cli identity: actor_kind is
	// locked to 'user' (the frozen CHECK), so the CLI's identity travels in
	// actor_id — the same fact TestDirectDoorActorIsCliAudited pins at the
	// CLI boundary.
	cs := audits(t, s, "schedule", "doors-nightly")
	if countOp(cs, "pause") != 1 {
		t.Fatalf("the offline pause audited %d lines, want exactly 1 (ops: %+#v)", countOp(cs, "pause"), cs)
	}
	for _, c := range cs {
		if c.Op == "pause" && (c.ActorID != "cli" || c.ActorKind != "user") {
			t.Errorf("pause audited as %s@%s — the offline actor is {user, cli} (actor_id carries the CLI)", c.ActorID, c.ActorKind)
		}
	}

	rr, err := d.Resume(ctx, offlineCLI, "doors-nightly")
	if err != nil || !rr.Found {
		t.Fatalf("offline resume: %v %+#v", err, rr)
	}

	h, err := d.History(ctx, "doors-nightly", 0)
	if err != nil {
		t.Fatalf("offline history: %v", err)
	}
	if len(h.Fires) != 0 {
		t.Errorf("a never-fired trigger reported %d fires", len(h.Fires))
	}
}

func TestOfflineFireNowRefusesByName(t *testing.T) {
	s, _ := writeWorld(t)
	d := NewAutomationControlDoors(s)
	_, err := d.Add(context.Background(), offlineCLI, protocol.TriggerAddParams{
		ID: "doors-fire", Blueprint: "dev-loop", Expr: "0 7 * * *", Zone: "UTC", Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.FireNow(context.Background(), offlineCLI, "doors-fire")
	if err == nil {
		t.Fatal("offline fire-now answered SUCCESS — nothing offline can start a loop, and a silent refusal would be a lie by silence")
	}
	// The CLI prints `Data.What` verbatim (blueprintVerbatim's rule), so
	// the sentence is asserted there, not in Error()'s code-and-message.
	var pe *protocol.Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Data.What, "running daemon") {
		t.Errorf("the refusal does not name the door that can: %#v", pe)
	}
	// Writes nothing: the fire row count for the trigger is untouched.
	cs := audits(t, s, "schedule", "doors-fire")
	if countOp(cs, "fire_now") != 0 {
		t.Errorf("the refused fire-now audited a fire anyway: %+#v", cs)
	}
}

func TestOfflineWriteDoorsArchiveRoundTrip(t *testing.T) {
	s, _ := writeWorld(t)
	cd := NewAutomationControlDoors(s)
	wd := NewAutomationWriteDoors(s)
	ctx := context.Background()
	if _, err := cd.Add(ctx, offlineCLI, protocol.TriggerAddParams{
		ID: "doors-arch", Blueprint: "dev-loop", Expr: "0 7 * * *", Zone: "UTC", Workspace: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	ar, err := wd.Archive(ctx, offlineCLI, protocol.AutomationArchiveParams{Source: "schedule", ID: "doors-arch", Reason: "parked by the offline door"})
	if err != nil {
		t.Fatalf("offline archive: %v", err)
	}
	if !ar.Changed || !ar.After.Archived || ar.Health.CanFire {
		t.Fatalf("offline archive answered %+#v — archived AND not firable, health carried", ar)
	}
	// AN6: the archive is engine-invisible — the clock's own row says paused.
	lr, err := cd.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range lr.Triggers {
		if tr.ID == "doors-arch" && !tr.Paused {
			t.Error("the archive did not set the engine-visible off state — the clock would still fire it")
		}
	}

	ur, err := wd.Unarchive(ctx, offlineCLI, protocol.AutomationArchiveParams{Source: "schedule", ID: "doors-arch"})
	if err != nil {
		t.Fatalf("offline unarchive: %v", err)
	}
	if !ur.Changed || ur.After.Archived || !ur.After.Paused {
		t.Fatalf("offline unarchive answered %+#v — AN3: back, but still disarmed", ur)
	}
}
