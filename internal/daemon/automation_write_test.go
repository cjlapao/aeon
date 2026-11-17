package daemon

// automation_write_test.go — S49 T4's daemon rules as tests: every person's
// write audits ONCE and only for a real change; trigger.update speaks
// trigger.add's refusal sentences byte for byte and can never enable;
// archive is a state — the door carries the health answer it just changed,
// the second archive changes nothing and audits nothing, and enable refuses
// an archived row until unarchive says otherwise; fire-now audits the fire
// row it recorded, by id; and the store's backstop sentence still stands
// behind the door for whoever bypasses it.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func writeWorld(t *testing.T) (*store.Store, *Daemon) {
	t.Helper()
	s, _ := t14Env(t)
	d := New(Options{Store: s})
	if d.Table.AutomationWrite == nil {
		t.Fatal("the write seam is unwired on a store-backed daemon")
	}
	return s, d
}

var personDoor = protocol.Actor{Kind: "user", ID: "operator"}

func audits(t *testing.T, s *store.Store, source, id string) []store.AutomationChange {
	t.Helper()
	cs, err := s.AutomationChangeList(context.Background(), source, id, 50)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func countOp(cs []store.AutomationChange, op string) int {
	n := 0
	for _, c := range cs {
		if c.Op == op {
			n++
		}
	}
	return n
}

func addSchedule(t *testing.T, d *Daemon, id, expr string) protocol.TriggerControlResult {
	t.Helper()
	raw, err := d.Table.TriggerAdd(context.Background(), personDoor, protocol.TriggerAddParams{
		ID: id, Blueprint: "dev-loop", Expr: expr, Zone: "UTC", Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("trigger.add %s: %v", id, err)
	}
	res, ok := raw.(protocol.TriggerControlResult)
	if !ok {
		t.Fatalf("trigger.add returned %T", raw)
	}
	return res
}

func TestPersonScheduleWritesAuditOnceEach(t *testing.T) {
	s, d := writeWorld(t)
	ctx := context.Background()
	if res := addSchedule(t, d, "sch-audit", "0 * * * *"); !res.Found {
		t.Fatalf("add: %+v", res)
	}
	cs := audits(t, s, "schedule", "sch-audit")
	if len(cs) != 1 || cs[0].Op != "create" || cs[0].Summary != "created schedule sch-audit" || cs[0].ActorID != "operator" {
		t.Fatalf("create audit = %+v", cs)
	}
	if _, err := d.Table.TriggerPause(ctx, personDoor, "sch-audit"); err != nil {
		t.Fatal(err)
	}
	cs = audits(t, s, "schedule", "sch-audit")
	if countOp(cs, "pause") != 1 {
		t.Fatalf("pause audits = %d — want one: %+v", countOp(cs, "pause"), cs)
	}
	// Pause again is the idempotent answer control.go always gave — and it
	// writes nothing, so it audits nothing.
	if _, err := d.Table.TriggerPause(ctx, personDoor, "sch-audit"); err != nil {
		t.Fatal(err)
	}
	if countOp(audits(t, s, "schedule", "sch-audit"), "pause") != 1 {
		t.Fatal("the idempotent pause audited a second time")
	}
	if _, err := d.Table.TriggerResume(ctx, personDoor, "sch-audit"); err != nil {
		t.Fatal(err)
	}
	if countOp(audits(t, s, "schedule", "sch-audit"), "resume") != 1 {
		t.Fatal("resume did not audit once")
	}
	// The row carries the person as its creator, stamped by the store.
	row, err := s.TriggerSchedule(ctx, "sch-audit")
	if err != nil || row.CreatedByID != "operator" || row.CreatedByKind != "user" {
		t.Fatalf("creator: %+v err=%v", row, err)
	}
}

func TestFireNowAuditsTheFireRowByID(t *testing.T) {
	s, d := writeWorld(t)
	ctx := context.Background()
	addSchedule(t, d, "sch-fire", "0 * * * *")
	// Pause first so fire-now answers did_not_fire through TR12 WITHOUT
	// starting a loop this test has no runner bargain for.
	if _, err := d.Table.TriggerPause(ctx, personDoor, "sch-fire"); err != nil {
		t.Fatal(err)
	}
	raw, err := d.Table.TriggerFireNow(ctx, personDoor, "sch-fire")
	if err != nil {
		t.Fatal(err)
	}
	res := raw.(protocol.TriggerControlResult)
	if !res.Found || res.Outcome == "" {
		t.Fatalf("fire-now answer: %+v", res)
	}
	cs := audits(t, s, "schedule", "sch-fire")
	if countOp(cs, "fire_now") != 1 {
		t.Fatalf("fire_now audits = %d: %+v", countOp(cs, "fire_now"), cs)
	}
	for _, c := range cs {
		if c.Op == "fire_now" {
			id, ok, err := s.LatestTriggerFireID(ctx, "sch-fire")
			if err != nil || !ok {
				t.Fatalf("the fire row is missing: ok=%v err=%v", ok, err)
			}
			if !strings.Contains(c.Summary, "fire "+itoa(id)) || !strings.Contains(c.Summary, "outcome "+res.Outcome) {
				t.Errorf("fire-now summary %q must name the fire id (%d) and the outcome (%s)", c.Summary, id, res.Outcome)
			}
		}
	}
	// A ghost id answers and audits NOTHING (no fire row was recorded).
	ghost, err := d.Table.TriggerFireNow(ctx, personDoor, "sch-ghost")
	if err != nil {
		t.Fatal(err)
	}
	if g := ghost.(protocol.TriggerControlResult); g.Found || g.Outcome != "" {
		t.Fatalf("ghost fire-now: %+v", g)
	}
	if n := countOp(audits(t, s, "schedule", "sch-ghost"), "fire_now"); n != 0 {
		t.Fatal("a ghost fire-now audited")
	}
}

func TestUpdateSpeaksAddSentencesAndNeverEnables(t *testing.T) {
	s, d := writeWorld(t)
	ctx := context.Background()
	addSchedule(t, d, "sch-eq", "0 * * * *")
	// add again with a broken cron: the validator speaks BEFORE the
	// exists-check does, and its sentence is what the door answers.
	bad := addSchedule(t, d, "sch-eq", "@bad cron")
	if bad.Found || bad.Reason == "" {
		t.Fatalf("add on a bad cron should answer the validator: %+v", bad)
	}
	// update with the same broken cron on the same id answers the SAME
	// bytes — one validator, one sentence, no second grammar.
	upd, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-eq", Cron: ptr("@bad cron")})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Changed || upd.Reason == "" {
		t.Fatalf("update on a bad cron: %+v", upd)
	}
	if upd.Reason != bad.Reason {
		t.Errorf("update said %q, add said %q — the sentences must be byte-identical", upd.Reason, bad.Reason)
	}
	// A row with no clock cannot gain one: the webhook's own sentence.
	raw, err := d.Table.TriggerAdd(ctx, personDoor, protocol.TriggerAddParams{
		ID: "sch-w", Kind: "webhook", Blueprint: "dev-loop", Workspace: t.TempDir(), SecretEnv: "AEON_HOOK"})
	if err != nil {
		t.Fatal(err)
	}
	if res := raw.(protocol.TriggerControlResult); !res.Found {
		t.Fatalf("add webhook: %+v", res)
	}
	uw, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-w", Cron: ptr("0 7 * * *")})
	if err != nil {
		t.Fatal(err)
	}
	if uw.Changed || !strings.Contains(uw.Reason, "not on a tick") {
		t.Fatalf("webhook gained a clock: %+v", uw)
	}
	// update NEVER enables: a paused row edited stays paused.
	addSchedule(t, d, "sch-p", "0 * * * *")
	if _, err := d.Table.TriggerPause(ctx, personDoor, "sch-p"); err != nil {
		t.Fatal(err)
	}
	before := auditCount(t, s, "schedule", "sch-p")
	ur, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-p", Goal: ptr("renamed goal")})
	if err != nil {
		t.Fatal(err)
	}
	if !ur.Changed || ur.After.Paused != true || ur.After.Enabled {
		t.Fatalf("update enabled a paused row: %+v", ur.After)
	}
	if len(ur.NextFires) == 0 {
		t.Error("a schedule update owed the next instants and answered none")
	}
	if auditCount(t, s, "schedule", "sch-p") != before+1 {
		t.Error("the real edit must audit exactly one update")
	}
	// The no-op edit: changed false, and NO audit.
	u2, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-p", Goal: ptr("renamed goal")})
	if err != nil || u2.Changed {
		t.Fatalf("no-op update: changed=%v err=%v", u2.Changed, err)
	}
	if auditCount(t, s, "schedule", "sch-p") != before+1 {
		t.Error("an unchanged update audited")
	}
}

func auditCount(t *testing.T, s *store.Store, source, id string) int {
	t.Helper()
	return len(audits(t, s, source, id))
}

func TestUpdateGateKeepsProvenanceAndRefusesBrokenBlueprints(t *testing.T) {
	s, d := writeWorld(t)
	ctx := context.Background()
	addSchedule(t, d, "sch-bp", "0 * * * *")
	res, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-bp", Blueprint: ptr("blueprint-that-was-never-born")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Reason == "" {
		t.Fatalf("a missing blueprint answered %+v — the update door is stricter than add", res)
	}
	row, err := s.TriggerSchedule(ctx, "sch-bp")
	if err != nil || row.Blueprint != "dev-loop" {
		t.Fatalf("the refused update moved the row: %+v err=%v", row, err)
	}
	// created_by survives an update: the UPDATE statement never names the
	// provenance or archive columns.
	if row.CreatedByID != "operator" {
		t.Fatalf("provenance lost: %+v", row)
	}
}

func TestArchiveDoorsAreStatesNotDeletes(t *testing.T) {
	s, d := writeWorld(t)
	ctx := context.Background()
	addSchedule(t, d, "sch-arch", "0 * * * *")
	res, err := d.Table.AutomationWrite.Archive(ctx, personDoor, protocol.AutomationArchiveParams{
		Source: "schedule", ID: "sch-arch", Reason: "rotta retired"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Before.Archived || !res.After.Archived || !res.After.Paused {
		t.Fatalf("archive: %+v", res)
	}
	if res.Health.CanFire {
		t.Error("an archived automation says it can fire — AN6 is a lie in the view")
	}
	if !strings.Contains(strings.Join(res.Health.Reasons, " "), "archived") {
		t.Errorf("health reasons = %v — the archived word must be said", res.Health.Reasons)
	}
	if n := countOp(audits(t, s, "schedule", "sch-arch"), "archive"); n != 1 {
		t.Fatalf("archive audits = %d", n)
	}
	// The second archive: nothing changed, nothing audited.
	again, err := d.Table.AutomationWrite.Archive(ctx, personDoor, protocol.AutomationArchiveParams{
		Source: "schedule", ID: "sch-arch", Reason: "again"})
	if err != nil || again.Changed {
		t.Fatalf("re-archive: changed=%v err=%v", again.Changed, err)
	}
	if n := countOp(audits(t, s, "schedule", "sch-arch"), "archive"); n != 1 {
		t.Fatal("the idempotent archive audited twice")
	}
	// An archived row can still be UPDATED (fields survive the archive) but
	// never resumed or enabled through another door — and the update keeps
	// it archived.
	u, err := d.Table.AutomationWrite.TriggerUpdate(ctx, personDoor, protocol.TriggerUpdateParams{
		ID: "sch-arch", Goal: ptr("still edited from the archive")})
	if err != nil || !u.Changed || !u.After.Archived || !u.After.Paused {
		t.Fatalf("updating an archived row: %+v err=%v", u, err)
	}
	// Unarchive clears the state and leaves the paused state standing.
	un, err := d.Table.AutomationWrite.Unarchive(ctx, personDoor, protocol.AutomationArchiveParams{
		Source: "schedule", ID: "sch-arch"})
	if err != nil || !un.Changed || un.After.Archived || !un.After.Paused {
		t.Fatalf("unarchive: %+v err=%v", un, err)
	}
	// Wrong family under the wrong source is named, not silently missed.
	if _, err := d.Table.AutomationWrite.Archive(ctx, personDoor, protocol.AutomationArchiveParams{
		Source: "event", ID: "sch-arch", Reason: "family mix"}); err == nil {
		t.Fatal("archiving a schedule under source=event answered no error")
	}
	// Unknown id answers the unknown door, not an empty result.
	var perr *protocol.Error
	_, err = d.Table.AutomationWrite.Archive(ctx, personDoor, protocol.AutomationArchiveParams{
		Source: "schedule", ID: "sch-never", Reason: "ghost"})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrAutomationUnknown.Code {
		t.Fatalf("ghost archive: %v — -31890", err)
	}
}

func TestEnableRefusesArchivedUntilUnarchive(t *testing.T) {
	s, d := writeWorld(t)
	tr := e2eTrigger(t, s, "", "dev-loop")
	w := d.Table.TriggerWrites
	if _, err := w.DisableEventTrigger(personDoor, tr.ID, "quiet for now"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Table.AutomationWrite.Archive(context.Background(), personDoor, protocol.AutomationArchiveParams{
		Source: "event", ID: tr.ID, Reason: "moved"}); err != nil {
		t.Fatal(err)
	}
	_, err := w.EnableEventTrigger(personDoor, tr.ID)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrAutomationRefused.Code {
		t.Fatalf("enable-while-archived: %v — ErrAutomationRefused (AN6)", err)
	}
	if !strings.Contains(perr.Data.What, "archived") {
		t.Errorf("refusal %q must name the archive", perr.Data.What)
	}
	if _, err := d.Table.AutomationWrite.Unarchive(context.Background(), personDoor, protocol.AutomationArchiveParams{
		Source: "event", ID: tr.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := w.EnableEventTrigger(personDoor, tr.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("enable after unarchive: %+v err=%v", got, err)
	}
}

func TestEventWritesAuditAndTheBackstopStands(t *testing.T) {
	s, d := writeWorld(t)
	w := d.Table.TriggerWrites
	tr, err := w.CreateEventTrigger(personDoor, protocol.EventTriggerCreateParams{
		Name: "audited", Match: json.RawMessage(`{"event":"goal.approved"}`), Blueprint: "dev-loop", MaxRunsPerHour: 5})
	if err != nil {
		t.Fatal(err)
	}
	cs := audits(t, s, "event", tr.ID)
	if countOp(cs, "create") != 1 {
		t.Fatalf("event create audits: %+v", cs)
	}
	if _, err := w.UpdateEventTrigger(personDoor, protocol.EventTriggerUpdateParams{ID: tr.ID, Name: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	if countOp(audits(t, s, "event", tr.ID), "update") != 1 {
		t.Fatal("event rename did not audit once")
	}
	// The unchanged update writes nothing and audits nothing.
	if _, err := w.UpdateEventTrigger(personDoor, protocol.EventTriggerUpdateParams{ID: tr.ID, Name: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	if countOp(audits(t, s, "event", tr.ID), "update") != 1 {
		t.Fatal("the no-op event update audited")
	}
	if _, err := w.DisableEventTrigger(personDoor, tr.ID, "quiet"); err != nil {
		t.Fatal(err)
	}
	if countOp(audits(t, s, "event", tr.ID), "disable") != 1 {
		t.Fatal("disable did not audit")
	}
	// The store's backstop sentence, for whoever bypasses the door:
	if _, err := w.CreateEventTrigger(protocol.Actor{Kind: "agent", ID: "x"}, protocol.EventTriggerCreateParams{
		Name: "nope", Match: json.RawMessage(`{"event":"goal.approved"}`), Blueprint: "dev-loop", MaxRunsPerHour: 5}); err == nil ||
		!strings.Contains(err.Error(), "written by a person") {
		t.Fatalf("the agent backstop: %v", err)
	}
}
