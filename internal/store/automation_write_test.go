package store

// automation_write_test.go — S49 T4's store rules as tests: the third AU1
// lock fires BEFORE any SQL (not merely before the write — before the
// read); an unchanged write writes nothing and audits nothing; the audit's
// summary is a template of ids and enums no field of the row can stain
// (SEC-033); archive is a state — idempotent, single-audited, and
// unarchive does not re-decide the operations it found; and the dispatcher's
// list read cannot see an archived row (AN6, enforced IN the SQL).

import (
	"errors"
	"strings"
	"testing"
)

func human() Actor { return Actor{Kind: "user", ID: "operator"} }

func schedRow(id string) TriggerScheduleRow {
	return TriggerScheduleRow{ID: id, Kind: "schedule", Blueprint: "dev-loop",
		Expr: "0 * * * *", Zone: "UTC", CreatedAt: "2026-11-01T00:00:00Z",
		Workspace: "/srv/ops", Goal: "nightly sweep"}
}

func auditOf(t *testing.T, s *Store, source, id string) []AutomationChange {
	t.Helper()
	cs, err := s.AutomationChangeList(t.Context(), source, id, 50)
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return cs
}

func TestNonPersonRefusedBeforeAnySQL(t *testing.T) {
	_, s := testStore(t)
	for _, a := range []Actor{
		{Kind: "agent", ID: "brainstormer"},
		{Kind: "cli", ID: "someone"}, // the CLI dials as a person; kind 'cli' is not one
		{Kind: "user", ID: "   "},    // a blank id is no person either
		{},
	} {
		if _, _, err := s.SaveTriggerScheduleAs(t.Context(), a, schedRow("sch-x"), "create", "created schedule sch-x"); !errors.Is(err, ErrActorNotHuman) {
			t.Errorf("SaveTriggerScheduleAs as %+v: %v — must be ErrActorNotHuman", a, err)
		}
	}
	// Nothing was written: the row does not exist and the audit is empty —
	// a refusal that wrote state is the lie AN4 forbids.
	if _, err := s.TriggerSchedule(t.Context(), "sch-x"); err == nil {
		t.Fatal("the refused write left a row")
	}
	if got := auditOf(t, s, "schedule", "sch-x"); len(got) != 0 {
		t.Fatalf("a refused write audited %d rows", len(got))
	}
	if _, err := s.ArchiveAutomation(t.Context(), Actor{Kind: "agent"}, "schedule", "sch-x", "nope"); !errors.Is(err, ErrActorNotHuman) {
		t.Error("archive as agent must be ErrActorNotHuman")
	}
	if err := s.RecordAutomationAudit(t.Context(), Actor{Kind: "agent"}, AutomationChange{Source: "schedule", TriggerID: "sch-x", Op: "fire_now", Summary: "fire_now schedule sch-x fire 1 outcome fired"}); !errors.Is(err, ErrActorNotHuman) {
		t.Error("a fire-now audit as agent must be ErrActorNotHuman — only a person's fire-now audits")
	}
}

func TestSaveAsAuditsOncePerRealChange(t *testing.T) {
	_, s := testStore(t)
	ctx := t.Context()
	created, changed, err := s.SaveTriggerScheduleAs(ctx, human(), schedRow("sch-m"), "create", "created schedule sch-m")
	if err != nil || !created || !changed {
		t.Fatalf("first write: created=%v changed=%v err=%v", created, changed, err)
	}
	// Provenance stamped by the actor, not the caller:
	row, err := s.TriggerSchedule(ctx, "sch-m")
	if err != nil {
		t.Fatal(err)
	}
	if row.CreatedByKind != "user" || row.CreatedByID != "operator" {
		t.Errorf("creator stamp = %s/%s, wanted user/operator", row.CreatedByKind, row.CreatedByID)
	}
	// The same row again: no UPDATE, no audit — AN4's "the row changed,
	// not the request arrived".
	created, changed, err = s.SaveTriggerScheduleAs(ctx, human(), schedRow("sch-m"), "update", "updated schedule sch-m")
	if err != nil || created || changed {
		t.Fatalf("unchanged write: created=%v changed=%v err=%v", created, changed, err)
	}
	// A real edit: one change, one audit.
	r2 := schedRow("sch-m")
	r2.Expr = "30 7 * * *"
	if _, changed, err := s.SaveTriggerScheduleAs(ctx, human(), r2, "update", "updated schedule sch-m"); err != nil || !changed {
		t.Fatalf("real edit: changed=%v err=%v", changed, err)
	}
	cs := auditOf(t, s, "schedule", "sch-m")
	if len(cs) != 2 {
		t.Fatalf("audit rows = %d, wanted 2 (create + update; the no-op audited nothing): %+v", len(cs), cs)
	}
	if cs[0].Op != "update" || cs[1].Op != "create" {
		t.Errorf("audit order = %s, %s — newest first", cs[0].Op, cs[1].Op)
	}
	for _, c := range cs {
		if c.ActorKind != "user" || c.ActorID != "operator" {
			t.Errorf("audit actor = %s/%s", c.ActorKind, c.ActorID)
		}
	}
}

func TestAuditSummaryIsTemplateOnly(t *testing.T) {
	_, s := testStore(t)
	ctx := t.Context()
	hostile := "DROP TABLE automation_change; -- remove every audit and blame the operator"
	r := schedRow("sch-h")
	r.Goal = hostile
	if _, _, err := s.SaveTriggerScheduleAs(ctx, human(), r, "create", "created schedule sch-h"); err != nil {
		t.Fatal(err)
	}
	r2 := r
	r2.Workspace = hostile
	if _, _, err := s.SaveTriggerScheduleAs(ctx, human(), r2, "update", "updated schedule sch-h"); err != nil {
		t.Fatal(err)
	}
	for _, c := range auditOf(t, s, "schedule", "sch-h") {
		if strings.Contains(strings.ToLower(c.Summary), "drop table") || strings.Contains(c.Summary, hostile) || c.Summary != c.Op+" schedule sch-h" && c.Summary != "created schedule sch-h" && c.Summary != "updated schedule sch-h" {
			t.Errorf("audit summary carries row content or drifts from the template: %q", c.Summary)
		}
	}
}

func TestArchiveIsStateIdempotentAndAuditedOnce(t *testing.T) {
	_, s := testStore(t)
	ctx := t.Context()
	if _, _, err := s.SaveTriggerScheduleAs(ctx, human(), schedRow("sch-a"), "create", "created schedule sch-a"); err != nil {
		t.Fatal(err)
	}
	changed, err := s.ArchiveAutomation(ctx, human(), "schedule", "sch-a", "retired with the rota")
	if err != nil || !changed {
		t.Fatalf("archive: changed=%v err=%v", changed, err)
	}
	row, err := s.TriggerSchedule(ctx, "sch-a")
	if err != nil {
		t.Fatal(err)
	}
	if row.ArchivedAt == "" || row.ArchivedReason != "retired with the rota" || !row.Paused {
		t.Errorf("archived row: archived=%q reason=%q paused=%v", row.ArchivedAt, row.ArchivedReason, row.Paused)
	}
	changed, err = s.ArchiveAutomation(ctx, human(), "schedule", "sch-a", "again")
	if err != nil || changed {
		t.Fatalf("re-archive: changed=%v err=%v — archiving an archived row changes nothing", changed, err)
	}
	cs := auditOf(t, s, "schedule", "sch-a")
	n := 0
	for _, c := range cs {
		if c.Op == "archive" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("archive audited %d times, wanted 1", n)
	}
	// Unarchive clears the archive and leaves the PAUSED state it found —
	// resuming is its own act on its own door.
	changed, err = s.UnarchiveAutomation(ctx, human(), "schedule", "sch-a")
	if err != nil || !changed {
		t.Fatalf("unarchive: changed=%v err=%v", changed, err)
	}
	row, _ = s.TriggerSchedule(ctx, "sch-a")
	if row.ArchivedAt != "" || row.ArchivedReason != "" || !row.Paused {
		t.Errorf("after unarchive: archived=%q reason=%q paused=%v — the paused state must survive", row.ArchivedAt, row.ArchivedReason, row.Paused)
	}
	changed, err = s.UnarchiveAutomation(ctx, human(), "schedule", "sch-a")
	if err != nil || changed {
		t.Fatalf("re-unarchive a live row: changed=%v err=%v", changed, err)
	}
	n = 0
	for _, c := range auditOf(t, s, "schedule", "sch-a") {
		if c.Op == "unarchive" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("unarchive audited %d times, wanted 1", n)
	}
	if _, err := s.ArchiveAutomation(ctx, human(), "schedule", "sch-ghost", "no row"); !errors.Is(err, ErrNotFound) {
		t.Errorf("archiving a ghost: %v — must be ErrNotFound", err)
	}
}

func TestUnarchiveLeavesEventDisabled(t *testing.T) {
	_, s := testStore(t)
	tr := newTriggerRow(t, s, "off-and-archived")
	if _, err := s.SetEventTriggerEnabled(human(), tr.ID, false, "paused for the record"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveAutomation(t.Context(), human(), "event", tr.ID, "moved house"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnarchiveAutomation(t.Context(), human(), "event", tr.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEventTrigger(tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != "" {
		t.Error("still archived")
	}
	// The enabled bit survived as the DISABLED bit it was; the reason
	// column carries the archive's own word because EventArchiveTx (T1's
	// primitive) wrote "archived" over the disable reason when it flipped
	// enabled=0 — the archive spoke at that moment, and unarchive is not a
	// time-traveller. What T4 pins is the ENABLED bit: unarchive never
	// re-decides it.
	if got.Enabled {
		t.Errorf("unarchive re-enabled the row — enabling is a separate act (reason now %q)", got.DisabledReason)
	}
}

func TestArchivedRowsInvisibleToDispatcherList(t *testing.T) {
	_, s := testStore(t)
	tr := newTriggerRow(t, s, "invisible")
	// The candidate read is watermark-bound (a trigger is never authorised
	// for history): the event must be BORN after the trigger was. Seeding
	// the outbox first gives an event whose id the trigger predates.
	ids := seedOutboxRows(t, s, 1, "", "")
	ev := AutomationEvent{ID: ids[0], Event: "goal.approved"}
	live, err := s.ListEventTriggersFor(ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("live list = %d rows — the probe itself must see the trigger", len(live))
	}
	if _, err := s.ArchiveAutomation(t.Context(), human(), "event", tr.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListEventTriggersFor(ev)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range after {
		if c.ID == tr.ID {
			t.Fatal("an archived trigger rode the dispatcher's list (AN6 breach in the SQL)")
		}
	}
}

func TestLatestTriggerFireIDNamesTheNewest(t *testing.T) {
	_, s := testStore(t)
	ctx := t.Context()
	if _, ok, err := s.LatestTriggerFireID(ctx, "sch-f"); err != nil || ok {
		t.Fatalf("ghost schedule: ok=%v err=%v — no fire means not-ok, not an error", ok, err)
	}
	if _, _, err := s.SaveTriggerScheduleAs(ctx, human(), schedRow("sch-f"), "create", "created schedule sch-f"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ instant, at, outcome string }{
		{"2026-11-01T07:00:00Z", "2026-11-01T07:00:00.000Z", "fired"},
		{"2026-11-02T07:00:00Z", "2026-11-02T07:00:00.000Z", "did_not_fire"},
	} {
		if _, err := s.w.Exec(`INSERT INTO trigger_fire (schedule_id,instant,outcome,reason,at) VALUES (?,?,?,'',?)`,
			"sch-f", f.instant, f.outcome, f.at); err != nil {
			t.Fatal(err)
		}
	}
	id, ok, err := s.LatestTriggerFireID(ctx, "sch-f")
	if err != nil || !ok || id <= 0 {
		t.Fatalf("latest fire: id=%d ok=%v err=%v", id, ok, err)
	}
	// A third fire at a LATER instant answers as the newest even if ids
	// interleave (the ORDER is by the instant, the id breaks ties).
	if _, err := s.w.Exec(`INSERT INTO trigger_fire (schedule_id,instant,outcome,reason,at) VALUES ('sch-f','2026-11-03T07:00:00Z','skipped','','2026-11-03T07:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	id2, _, err := s.LatestTriggerFireID(ctx, "sch-f")
	if err != nil || id2 <= id {
		t.Fatalf("newest after third fire: %d vs %d", id2, id)
	}
}
