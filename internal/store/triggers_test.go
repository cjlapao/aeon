package store

// triggers_test.go — S40 T3: the trigger store and the fire ledger. The
// human lock is tested where it lives (before any SQL — a refusal that ran
// SQL first would move the table, and the row count after the refusal is
// how these tests read the difference), and the ledger is tested as AU8
// built it: the UNIQUE is the claim, NULL is in-flight, deciding is
// claimed-FROM, and the vocabulary is closed at both ends.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var fireAt = time.UnixMilli(1_800_000_000_000).UTC()

func goalMatch(t *testing.T) Match {
	t.Helper()
	m, err := ParseMatch([]byte(`{"event":"goal.approved"}`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func newTriggerRow(t *testing.T, s *Store, name string) Trigger {
	t.Helper()
	tr, err := s.CreateEventTrigger(autoUser, NewEventTrigger{
		Name: name, Match: goalMatch(t), Blueprint: "dev-loop", MaxRunsPerHour: 5,
	})
	if err != nil {
		t.Fatalf("create trigger %s: %v", name, err)
	}
	return tr
}

func seedOutboxRows(t *testing.T, s *Store, n int, comp, root string) []int64 {
	t.Helper()
	var ids []int64
	for i := 0; i < n; i++ {
		if _, err := s.w.Exec(`INSERT INTO automation_event (event,subject_kind,subject_id,root_id,component_id,created_at)
			VALUES ('goal.approved','goal',?, ?,?,'2026-10-28T06:00:00.000Z')`, fmt.Sprintf("g-%d", i), root, comp); err != nil {
			t.Fatalf("seed outbox: %v", err)
		}
	}
	rows, err := s.ReadAutomationEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestOnlyAPersonCanWriteATrigger(t *testing.T) {
	s := openTemp(t)
	bad := []Actor{{Kind: "agent", ID: "x"}, {Kind: "user", ID: "  "}, {Kind: "", ID: ""}, {Kind: "trigger", ID: "clock"}}
	for _, a := range bad {
		if _, err := s.CreateEventTrigger(a, NewEventTrigger{Name: "n", Match: goalMatch(t), Blueprint: "b", MaxRunsPerHour: 5}); !errors.Is(err, ErrActorNotHuman) {
			t.Errorf("CreateEventTrigger(%+v) = %v, want ErrActorNotHuman", a, err)
		}
		if _, err := s.UpdateEventTrigger(a, "whatever", EventTriggerEdit{}); !errors.Is(err, ErrActorNotHuman) {
			t.Errorf("UpdateEventTrigger(%+v) = %v, want ErrActorNotHuman", a, err)
		}
		if _, err := s.SetEventTriggerEnabled(a, "whatever", false, "x"); !errors.Is(err, ErrActorNotHuman) {
			t.Errorf("SetEventTriggerEnabled(%+v) = %v, want ErrActorNotHuman", a, err)
		}
	}
	if n := countTable(t, s.gw, "event_trigger"); n != 0 {
		t.Errorf("%d rows after only refusals — SQL ran before the human check", n)
	}
	// And a refusal must not touch an existing row either.
	tr := newTriggerRow(t, s, "keep")
	if _, err := s.UpdateEventTrigger(Actor{Kind: "agent", ID: "x"}, tr.ID, EventTriggerEdit{}); !errors.Is(err, ErrActorNotHuman) {
		t.Error("an agent edited a trigger")
	}
	fresh, err := s.GetEventTrigger(tr.ID)
	if err != nil || fresh.UpdatedAt != tr.UpdatedAt {
		t.Errorf("the row moved under a refused edit: %v (err %v)", fresh.UpdatedAt, err)
	}
}

func TestCreateTriggerStoresCanonicalMatchAndFromOutboxID(t *testing.T) {
	s := openTemp(t)
	seedOutboxRows(t, s, 3, "/r/web", "prj1")
	m := goalMatch(t)
	tr, err := s.CreateEventTrigger(autoUser, NewEventTrigger{
		Name: "on-approved", RootID: "prj1", Match: m, Blueprint: "dev-loop", MaxRunsPerHour: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr.FromOutboxID != 3 {
		t.Errorf("from_outbox_id = %d, want 3 (head at birth, in the create transaction)", tr.FromOutboxID)
	}
	if tr.MatchJSON != m.Canonical() {
		t.Errorf("stored match %s is not the canonical %s", tr.MatchJSON, m.Canonical())
	}
	if tr.CreatedByKind != "user" || tr.CreatedByID != "op" {
		t.Errorf("created_by = %s/%s, want user/op", tr.CreatedByKind, tr.CreatedByID)
	}
	if !tr.Enabled || tr.Source != "event" {
		t.Errorf("new trigger enabled=%v source=%q, want true/event", tr.Enabled, tr.Source)
	}
}

func TestCreateTriggerValidates(t *testing.T) {
	s := openTemp(t)
	base := func() NewEventTrigger {
		return NewEventTrigger{Name: "v", Match: goalMatch(t), Blueprint: "b", MaxRunsPerHour: 5}
	}
	cases := []struct {
		mutate func(*NewEventTrigger)
		want   string
	}{
		{func(in *NewEventTrigger) { in.MaxRunsPerHour = 0 }, "1–1000"},
		{func(in *NewEventTrigger) { in.MaxRunsPerHour = 1001 }, "1–1000"},
		{func(in *NewEventTrigger) { neg := -1.0; in.MaxCostPerDayUSD = &neg }, "negative"},
		{func(in *NewEventTrigger) { in.Name = "" }, "1–80"},
		{func(in *NewEventTrigger) { in.Blueprint = "" }, "blueprint"},
	}
	for _, c := range cases {
		in := base()
		c.mutate(&in)
		_, err := s.CreateEventTrigger(autoUser, in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("mutation returned %v, want it to name %q", err, c.want)
		}
	}
	if _, err := s.CreateEventTrigger(autoUser, base()); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateEventTrigger(autoUser, base())
	if err == nil || !strings.Contains(err.Error(), `trigger "v" already exists`) {
		t.Errorf("duplicate name returned %v, want the sentence naming the name", err)
	}
	if n := countTable(t, s.gw, "event_trigger"); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

func TestUpdateTriggerEditsOnlyWhatWasGivenAndBumpsUpdatedAt(t *testing.T) {
	s := openTemp(t)
	tr := newTriggerRow(t, s, "editme")
	// Baseline the clock by hand: the store's clock is frozen, so "later"
	// is proven against a timestamp the test pushed into the past.
	if _, err := s.w.Exec(`UPDATE event_trigger SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}
	bp := "plan-loop"
	got, err := s.UpdateEventTrigger(autoUser, tr.ID, EventTriggerEdit{Blueprint: &bp})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blueprint != "plan-loop" {
		t.Errorf("blueprint = %q, want plan-loop", got.Blueprint)
	}
	if got.Name != tr.Name || got.MatchJSON != tr.MatchJSON || got.MaxRunsPerHour != tr.MaxRunsPerHour {
		t.Errorf("untouched fields moved: %q/%s/%d vs %q/%s/%d", got.Name, got.MatchJSON, got.MaxRunsPerHour, tr.Name, tr.MatchJSON, tr.MaxRunsPerHour)
	}
	if got.CreatedByKind != tr.CreatedByKind || got.CreatedByID != tr.CreatedByID || got.FromOutboxID != tr.FromOutboxID {
		t.Errorf("provenance moved: %s/%s/%d", got.CreatedByKind, got.CreatedByID, got.FromOutboxID)
	}
	if got.UpdatedAt <= "2000-01-01T00:00:00.000Z" {
		t.Errorf("updated_at = %q, want it bumped past the baseline", got.UpdatedAt)
	}
	// Clearing the cost cap is its own intention.
	cost := 4.0
	if _, err := s.UpdateEventTrigger(autoUser, tr.ID, EventTriggerEdit{MaxCostPerDayUSD: &cost}); err != nil {
		t.Fatal(err)
	}
	withCost, _ := s.GetEventTrigger(tr.ID)
	if withCost.MaxCostPerDayUSD == nil || *withCost.MaxCostPerDayUSD != 4.0 {
		t.Error("cost cap was not stored")
	}
	cleared, err := s.UpdateEventTrigger(autoUser, tr.ID, EventTriggerEdit{ClearMaxCost: true})
	if err != nil || cleared.MaxCostPerDayUSD != nil {
		t.Errorf("ClearMaxCost left %v (err %v), want NULL", cleared.MaxCostPerDayUSD, err)
	}
}

func TestDisableNeedsAReasonAndEnableClearsIt(t *testing.T) {
	s := openTemp(t)
	tr := newTriggerRow(t, s, "disable")
	if _, err := s.SetEventTriggerEnabled(autoUser, tr.ID, false, "  "); err == nil {
		t.Error("disabling without a reason succeeded (AU1 refuses it)")
	}
	off, err := s.SetEventTriggerEnabled(autoUser, tr.ID, false, "vendor API is down")
	if err != nil || off.Enabled || off.DisabledReason != "vendor API is down" {
		t.Errorf("disable: enabled=%v reason=%q err=%v", off.Enabled, off.DisabledReason, err)
	}
	// A no-op answers the row with updated_at unmoved.
	before := off.UpdatedAt
	again, err := s.SetEventTriggerEnabled(autoUser, tr.ID, false, "anything")
	if err != nil || again.UpdatedAt != before {
		t.Errorf("a no-op disable bumped updated_at: %q vs %q (err %v)", again.UpdatedAt, before, err)
	}
	on, err := s.SetEventTriggerEnabled(autoUser, tr.ID, true, "")
	if err != nil || !on.Enabled || on.DisabledReason != "" {
		t.Errorf("enable: enabled=%v reason=%q err=%v", on.Enabled, on.DisabledReason, err)
	}
}

func TestListEventTriggersForIncludesDisabledAndHonoursFromOutboxIDAndScope(t *testing.T) {
	s := openTemp(t)
	ids := seedOutboxRows(t, s, 2, "/r/web", "prj1") // ids 1,2
	tr := newTriggerRow(t, s, "hist")                // from_outbox_id = 2
	if _, err := s.SetEventTriggerEnabled(autoUser, tr.ID, false, "paused"); err != nil {
		t.Fatal(err)
	}
	// The event that predates the trigger (id 1, from == 2) never sees it.
	old, err := s.ReadAutomationEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListEventTriggersFor(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a trigger born after event %d was offered for it (%v) — history is not its to dispatch", ids[0], got)
	}
	// A later event sees it EVEN DISABLED — the dispatcher records skipped(disabled).
	newer, err := s.ReadAutomationEvent(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	newer.ID = 99 // the dispatcher reads past the head: same row, newer cursor position
	got, err = s.ListEventTriggersFor(newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != tr.ID {
		t.Fatalf("disabled trigger was %v, want [hist] — disabled must be offered so the skip is recorded, not swallowed", got)
	}
	// Scope: a component-scoped trigger ignores another component's event.
	comp, err := ParseMatch([]byte(`{"event":"goal.approved"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEventTrigger(autoUser, NewEventTrigger{
		Name: "scoped", ComponentID: "cmpA", Match: comp, Blueprint: "b", MaxRunsPerHour: 5,
	}); err != nil {
		t.Fatal(err)
	}
	newer.ComponentID = "cmpB"
	got, _ = s.ListEventTriggersFor(newer)
	for _, g := range got {
		if g.Name == "scoped" {
			t.Error("a component-A trigger was offered for a component-B event")
		}
	}
	newer.ComponentID = "cmpA"
	got, _ = s.ListEventTriggersFor(newer)
	found := false
	for _, g := range got {
		if g.Name == "scoped" {
			found = true
		}
	}
	if !found {
		t.Error("the component-A trigger was not offered for the component-A event")
	}
}

func TestClaimFireIsUniquePerTriggerAndOutboxRow(t *testing.T) {
	s := openTemp(t)
	in := NewEventFire{TriggerID: "trg1", OutboxID: 10, SubjectKind: "goal", SubjectID: "g1", Depth: 0}
	f, fresh, err := s.ClaimEventFire(in, fireAt)
	if err != nil || !fresh {
		t.Fatalf("first claim: fresh=%v err=%v", fresh, err)
	}
	if f.Outcome != "" || f.DecidedAt != "" {
		t.Errorf("a fresh claim reads outcome %q decided %q, want the undecided NULL state", f.Outcome, f.DecidedAt)
	}
	again, fresh, err := s.ClaimEventFire(in, fireAt)
	if err != nil || fresh {
		t.Fatalf("second claim: fresh=%v err=%v, want fresh=false and no error (the claim says somebody is on it)", fresh, err)
	}
	if again.ID != f.ID {
		t.Errorf("the losing claim returned row %d, want the existing %d", again.ID, f.ID)
	}
	if n := countTable(t, s.gw, "event_trigger_fire"); n != 1 {
		t.Errorf("%d fire rows, want 1", n)
	}
}

func TestDecideFireIsClaimedFromAndClosedVocabulary(t *testing.T) {
	s := openTemp(t)
	f, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "t1", OutboxID: 1, SubjectKind: "goal", SubjectID: "g"}, fireAt)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery sees it in flight.
	open, err := s.OpenEventFires()
	if err != nil || len(open) != 1 || open[0].ID != f.ID {
		t.Errorf("OpenEventFires = %v (err %v), want the one claimed row", open, err)
	}
	if err := s.DecideEventFire(f.ID, EventFireDecision{Outcome: FireStarted, LoopID: "l1", LoopSlug: "auto-t1-1"}, fireAt); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideEventFire(f.ID, EventFireDecision{Outcome: FireRefused, Reason: "depth-exceeded: 4"}, fireAt); !errors.Is(err, ErrEventFireDecided) {
		t.Errorf("second decision = %v, want ErrEventFireDecided", err)
	}
	if row, _ := s.GetEventFire(f.ID); row.Outcome != FireStarted {
		t.Errorf("the first outcome did not stand: %q", row.Outcome)
	}
	// Closed vocabulary, each probe against a fresh claim, row unchanged.
	probes := []struct {
		what string
		d    EventFireDecision
	}{
		{"unknown outcome", EventFireDecision{Outcome: "retried"}},
		{"unknown reason code", EventFireDecision{Outcome: FireSkipped, Reason: "bored: why not"}},
		{"started without a loop", EventFireDecision{Outcome: FireStarted}},
		{"started with a reason", EventFireDecision{Outcome: FireStarted, LoopID: "l", Reason: "happy: fine"}},
		{"bad cost note", EventFireDecision{Outcome: FireStarted, LoopID: "l", CostNote: "cheap"}},
	}
	for i, p := range probes {
		g, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "t1", OutboxID: int64(100 + i), SubjectKind: "goal", SubjectID: "g"}, fireAt)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DecideEventFire(g.ID, p.d, fireAt); err == nil {
			t.Errorf("%s was accepted (AU12's vocabulary is closed)", p.what)
		}
		if row, _ := s.GetEventFire(g.ID); row.Outcome != "" {
			t.Errorf("%s: a refused decision still moved the row to %q", p.what, row.Outcome)
		}
	}
	if err := s.DecideEventFire(999_999, EventFireDecision{Outcome: FireSkipped, Reason: "disabled: x"}, fireAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("deciding an absent fire = %v, want ErrNotFound", err)
	}
}

func TestEveryReasonInTheClosedListIsAccepted(t *testing.T) {
	s := openTemp(t)
	i := 0
	for outcome, codes := range EventFireReasons {
		if outcome == FireStarted {
			f, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "cov", OutboxID: int64(i), SubjectKind: "goal", SubjectID: "g"}, fireAt)
			i++
			if err != nil {
				t.Fatal(err)
			}
			if err := s.DecideEventFire(f.ID, EventFireDecision{Outcome: FireStarted, LoopID: "l1", CostNote: "cost-floor"}, fireAt); err != nil {
				t.Errorf("started: %v", err)
			}
			continue
		}
		for _, code := range codes {
			f, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "cov", OutboxID: int64(i), SubjectKind: "goal", SubjectID: "g"}, fireAt)
			i++
			if err != nil {
				t.Fatal(err)
			}
			if err := s.DecideEventFire(f.ID, EventFireDecision{Outcome: outcome, Reason: code + ": detail text"}, fireAt); err != nil {
				t.Errorf("(%s, %s) was refused: %v — the validator and AU12's list have drifted", outcome, code, err)
			}
		}
	}
}

func TestFireCountsAlwaysReturnsAllFiveOutcomes(t *testing.T) {
	s := openTemp(t)
	m, err := s.EventFireCounts("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 5 {
		t.Fatalf("counts map has %d keys, want 5 (a hidden zero is a P31 breach)", len(m))
	}
	for _, o := range EventFireOutcomes() {
		if m[o] != 0 {
			t.Errorf("empty log counts %s=%d, want 0", o, m[o])
		}
	}
	if _, _, err := s.RecordEventFire(NewEventFire{TriggerID: "c", OutboxID: 1, SubjectKind: "goal", SubjectID: "g"},
		EventFireDecision{Outcome: FireStarted, LoopID: "l"}, fireAt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordEventFire(NewEventFire{TriggerID: "c", OutboxID: 2, SubjectKind: "goal", SubjectID: "g"},
		EventFireDecision{Outcome: FireSkipped, Reason: "disabled: paused"}, fireAt); err != nil {
		t.Fatal(err)
	}
	// A claimed-undecided row belongs to no bucket — it is not an outcome.
	if _, _, err := s.ClaimEventFire(NewEventFire{TriggerID: "c", OutboxID: 3, SubjectKind: "goal", SubjectID: "g"}, fireAt); err != nil {
		t.Fatal(err)
	}
	m, _ = s.EventFireCounts("c")
	if m[FireStarted] != 1 || m[FireSkipped] != 1 || m[FireRefused] != 0 || m[FireLimited] != 0 || m[FireCouldNotSay] != 0 {
		t.Errorf("counts = %v, want started 1, skipped 1, the rest 0 — and the claimed row in none of them", m)
	}
	// The list reads newest first and honours the trigger filter.
	log, err := s.ListEventFires(EventFireFilter{TriggerID: "c"})
	if err != nil || len(log) != 3 || log[0].OutboxID != 3 {
		t.Errorf("fire log = %v (err %v), want 3 rows newest first", log, err)
	}
}

func TestRecordFireWritesClaimAndOutcomeInOneRow(t *testing.T) {
	s := openTemp(t)
	in := NewEventFire{TriggerID: "rec", OutboxID: 7, SubjectKind: "goal", SubjectID: "g"}
	d := EventFireDecision{Outcome: FireSkipped, Reason: "disabled: paused for maintenance"}
	f, fresh, err := s.RecordEventFire(in, d, fireAt)
	if err != nil || !fresh {
		t.Fatalf("record: fresh=%v err=%v", fresh, err)
	}
	if f.Outcome != FireSkipped || f.DecidedAt == "" || f.ClaimedAt == "" {
		t.Errorf("recorded row: outcome %q claimed %q decided %q, want decided at once (never in flight)", f.Outcome, f.ClaimedAt, f.DecidedAt)
	}
	again, fresh, err := s.RecordEventFire(in, d, fireAt)
	if err != nil || fresh {
		t.Errorf("duplicate record: fresh=%v err=%v, want fresh=false with the existing row", fresh, err)
	}
	if again.ID != f.ID {
		t.Errorf("the duplicate returned %d, want the existing %d", again.ID, f.ID)
	}
	if open, _ := s.OpenEventFires(); len(open) != 0 {
		t.Errorf("%v in flight, want none — a row that was never undecided cannot be misread as started", open)
	}
	// A bad decision is refused BEFORE the insert: no row, claimed or decided.
	if _, _, err := s.RecordEventFire(NewEventFire{TriggerID: "rec", OutboxID: 8, SubjectKind: "goal", SubjectID: "g"},
		EventFireDecision{Outcome: FireStarted}, fireAt); err == nil {
		t.Error("record accepted started-without-loop")
	}
	if n := countRows(t, s, `SELECT count(*) FROM event_trigger_fire WHERE outbox_id=8`); n != 0 {
		t.Errorf("the refused record left %d rows", n)
	}
}
