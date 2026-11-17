package store

// S55 T9 — the store layer's invariants, the ones the daemon's behaviour
// tests cannot see: the audit is append-only BY TRIGGER (not by convention),
// a removal is a disable that never deletes (the audit and the id outlive the
// rule), the one write path's no-op rule (LC11: a set that changes nothing
// writes no row), and the availability probe's honest third state (P31): a
// signal whose source a migration did not reach is reported unavailable with
// the missing column named, not silently inert.

import (
	"database/sql"
	"strings"
	"testing"
)

// wdSetIn is the test's one rule, the key every case varies.
var wdSetIn = WatchdogSetIn{
	Scope: "workspace", SubjectID: "workspace", Signal: "no-activity",
	Threshold: 900000, Action: "notify",
	Origin: "user", ActorKind: "user", ActorID: "test",
}

// (1) The audit's append-only-ness is the trigger's, not the writer's
// discipline: a row already exists, and both the UPDATE and the DELETE the
// trigger exists to refuse are tried through the store's own writer — the
// same connection a rogue statement would ride. The trigger's own sentence
// is the refusal, naming the table.
func TestWatchdogEventIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	r, err := s.WatchdogSet(wdSetIn)
	if err != nil {
		t.Fatalf("create a rule (and its kind-'created' audit row): %v", err)
	}
	evs, err := s.ListWatchdogEvents(r.ID, 0)
	if err != nil || len(evs) != 1 {
		t.Fatalf("the create wrote %+v events, want one: %v", evs, err)
	}

	// The UPDATE: the audit row's actor is history, not a field to re-stamp.
	_, err = s.w.Exec(`UPDATE watchdog_event SET actor_id='forged' WHERE rule_id=?`, r.ID)
	if err == nil {
		t.Fatal("the audit row's UPDATE committed — the trigger that refuses it is missing")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("the UPDATE was refused as %q, want the trigger's own sentence naming the table", err.Error())
	}

	// The DELETE: the audit outlives the rule (P3), so no path removes a row.
	_, err = s.w.Exec(`DELETE FROM watchdog_event WHERE rule_id=?`, r.ID)
	if err == nil {
		t.Fatal("the audit row's DELETE committed — the trigger that refuses it is missing")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("the DELETE was refused as %q, want the trigger's own sentence naming the table", err)
	}

	// The row is still there, unchanged: the refusals were total.
	after, err := s.ListWatchdogEvents(r.ID, 0)
	if err != nil || len(after) != 1 || after[0].ActorID != "test" {
		t.Fatalf("after the refused UPDATE and DELETE the audit reads %+v, want the one un-forged row", after)
	}
}

// (2) A removal is a disable: the row survives (so the audit and the fire's
// rule_id keep resolving), the enabled flag goes to 0, and the 'removed'
// event — with the person's actor — is the removal's one row. Re-reading the
// rule by id after the removal must resolve, not 404: a fire recorded before
// the removal must still say which rule fired it.
func TestWatchdogRemoveNeverDeletes(t *testing.T) {
	s := openTemp(t)
	r, err := s.WatchdogSet(WatchdogSetIn{
		Scope: "workspace", SubjectID: "workspace", Signal: "same-position",
		Threshold: 6, Action: "stop",
		Origin: "system", ActorKind: "system", ActorID: "startup",
	})
	if err != nil {
		t.Fatalf("create a rule: %v", err)
	}

	if _, err := s.WatchdogRemove(r.ID, "user", "4321"); err != nil {
		t.Fatalf("remove the rule: %v", err)
	}

	// The row resolves: enabled 0, the key and the birth (id, created_at)
	// kept. (updated_at is not asserted: the test clock is fixed, so the
	// stamp cannot advance, and the equality it would check is a clock
	// artifact, not a removal property.)
	after, err := s.GetWatchdogRule(r.ID)
	if err != nil {
		t.Fatalf("the removed rule no longer resolves (%v) — the row was deleted, and the fire's rule_id would dangle", err)
	}
	if after.Enabled {
		t.Fatal("the removed rule reads enabled — a removal is a disable, not a delete")
	}
	if after.ID != r.ID || after.CreatedAt != r.CreatedAt ||
		after.Scope != r.Scope || after.SubjectID != r.SubjectID ||
		after.Signal != r.Signal || after.Threshold != r.Threshold ||
		after.Action != r.Action || after.Origin != r.Origin {
		t.Fatalf("the removal moved a field it does not own: %+v → %+v — only enabled and updated_at move", r, after)
	}

	// The 'removed' event is the person's act, the audit's one row for it.
	evs, err := s.ListWatchdogEvents(r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawRemoved bool
	for _, e := range evs {
		if e.Kind == "removed" {
			sawRemoved = true
			if e.ActorKind != "user" || e.ActorID != "4321" {
				t.Fatalf("the removal audit is %s/%s, want user/4321 — the removal is the person's act", e.ActorKind, e.ActorID)
			}
		}
	}
	if !sawRemoved {
		t.Fatalf("no kind-'removed' audit row — the removal left no record: %+v", evs)
	}
}

// (3) The one write path's no-op rule (LC11): a set with a key that exists
// and changes nothing writes NO audit row and says so (Changed false); a set
// that flips the row's one editable field (enabled) writes exactly one
// 'changed' row carrying the old and the new. The key itself never moves —
// a rule that changes its key is a different rule — and neither does the
// origin: provenance is birth-fixed (LC18), and the door's update is
// enabled+updated_at only, so a set that tries to move the origin through
// the door leaves it where the row's birth put it.
func TestWatchdogSetNoOpWritesNothing(t *testing.T) {
	s := openTemp(t)
	first, err := s.WatchdogSet(wdSetIn)
	if err != nil {
		t.Fatalf("the create: %v", err)
	}
	if first.Origin != "user" {
		t.Fatalf("the create's origin is %q, want user (the test's birth stamp)", first.Origin)
	}
	evs, _ := s.ListWatchdogEvents(first.ID, 0)
	if len(evs) != 1 || evs[0].Kind != "created" {
		t.Fatalf("the create wrote %+v, want one kind-'created' row", evs)
	}

	// The no-op: the same key, Enabled nil (the row's state is kept) —
	// nothing differs, so no row, and the answer says so.
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	same, changed, err := s.WatchdogSetTx(tx, wdSetIn)
	if err != nil {
		t.Fatalf("the no-op set: %v", err)
	}
	if changed || same.ID != first.ID {
		t.Fatalf("the no-op answered changed=%v id=%s, want false/the same row", changed, same.ID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if evs, _ = s.ListWatchdogEvents(first.ID, 0); len(evs) != 1 {
		t.Fatalf("the no-op set wrote %d events, want 1 (LC11: a set that changes nothing writes nothing)", len(evs))
	}

	// The flip: same key, enabled to false — one 'changed' row with the old
	// and the new values. The set's origin says system (the forgery attempt):
	// the row's origin stays user, because the door's update never moves it.
	off := false
	forged := WatchdogSetIn(wdSetIn)
	forged.Enabled = &off
	forged.Origin = "system"
	tx, err = s.w.Begin()
	if err != nil {
		t.Fatalf("begin the flip: %v", err)
	}
	flipped, changed, err := s.WatchdogSetTx(tx, forged)
	if err != nil {
		t.Fatalf("the flip: %v", err)
	}
	if !changed || flipped.Enabled {
		t.Fatalf("the flip answered changed=%v enabled=%v, want true/false", changed, flipped.Enabled)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	evs, err = s.ListWatchdogEvents(first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Kind != "changed" {
		t.Fatalf("the flip wrote %+v, want one kind-'changed' row on top of the create", evs)
	}
	if evs[0].OldValue == nil || *evs[0].OldValue != 1 || evs[0].NewValue == nil || *evs[0].NewValue != 0 {
		old, new := int64(-1), int64(-1)
		if evs[0].OldValue != nil {
			old = *evs[0].OldValue
		}
		if evs[0].NewValue != nil {
			new = *evs[0].NewValue
		}
		t.Fatalf("the changed row carries old=%d new=%d, want 1→0 (the flip the door committed)", old, new)
	}
	row, err := s.GetWatchdogRule(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Origin != "user" {
		t.Fatalf("the flip moved the origin to %q — provenance is the row's birth (LC18), and the door's update never touches it", row.Origin)
	}
}

// (4) The availability probe's honest third state (P31): on a migrated store
// every signal's source is present, and the probe says so per signal; when a
// source a migration did not reach is absent (here, built the way a real
// partial schema becomes partial — the column dropped), the probe reports
// that ONE signal unavailable with the missing column named, and the other
// signals are still available — the answer is per-signal, not all-or-nothing.
func TestWatchdogSignalAvailabilityNamesTheDroppedSource(t *testing.T) {
	s := openTemp(t)

	// A migrated store: every source present.
	avail, err := s.WatchdogSignalAvailability()
	if err != nil {
		t.Fatalf("the probe on a migrated store: %v", err)
	}
	for sig := range map[string]bool{
		"no-activity": true, "same-position": true,
		"repeat-block-reason": true, "step-over-deadline": true,
	} {
		if !avail[sig].Available {
			t.Fatalf("signal %s reads %+v on a migrated store, want available — the probe over-reported a gap", sig, avail[sig])
		}
	}

	// The partial schema: the step-over-deadline's source column dropped, the
	// way a store a migration did not reach would read.
	if _, err := s.w.Exec(`ALTER TABLE step DROP COLUMN context_manifest`); err != nil {
		t.Fatalf("drop the source column: %v", err)
	}

	avail, err = s.WatchdogSignalAvailability()
	if err != nil {
		t.Fatalf("the probe on a partial store: %v", err)
	}
	dead := avail["step-over-deadline"]
	if dead.Available {
		t.Fatal("step-over-deadline reads available on a store without its source column — a silently inert rule is what the probe exists to prevent")
	}
	if !strings.Contains(dead.Reason, "context_manifest") {
		t.Fatalf("the unavailable signal's reason is %q — it must name the missing source, not a generic fault", dead.Reason)
	}
	// The other three are untouched: the probe is per-signal.
	for _, sig := range []string{"no-activity", "same-position", "repeat-block-reason"} {
		if !avail[sig].Available {
			t.Fatalf("signal %s reads %+v — the probe is per-signal, and a dropped column elsewhere is not its source", sig, avail[sig])
		}
	}
	_ = sql.ErrNoRows // the probe's success answer, named for the record
}
