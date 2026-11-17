package protocol

// automation_write_test.go — S49 T4's protocol rules as tests: the three
// new doors are ABSENT without the seam (V13, not registered-and-refusing);
// the person is resolved from the CONNECTION before the seam sees a byte of
// params; forged identity KEYS are refused by name while an innocent unknown
// field stays ignored (T3's additive-evolution rule, kept whole); and the
// archive doors hold their shape rules (source closed, reason bounded)
// before any identity question is even asked.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeAutomationWrite struct {
	calls          int
	lastActor      Actor
	lastUpdate     TriggerUpdateParams
	lastArchive    AutomationArchiveParams
	unarchiveCalls int
	result         TriggerUpdateResult
	archiveResult  AutomationArchiveResult
}

func (f *fakeAutomationWrite) TriggerUpdate(_ context.Context, a Actor, p TriggerUpdateParams) (TriggerUpdateResult, error) {
	f.calls++
	f.lastActor = a
	f.lastUpdate = p
	return f.result, nil
}

func (f *fakeAutomationWrite) Archive(_ context.Context, a Actor, p AutomationArchiveParams) (AutomationArchiveResult, error) {
	f.calls++
	f.lastActor = a
	f.lastArchive = p
	return f.archiveResult, nil
}

func (f *fakeAutomationWrite) Unarchive(_ context.Context, a Actor, p AutomationArchiveParams) (AutomationArchiveResult, error) {
	f.calls++
	f.unarchiveCalls++
	f.lastActor = a
	f.lastArchive = p
	return f.archiveResult, nil
}

func writeDoorTable(w AutomationWrite, actor func() Actor) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if actor != nil {
		tbl.ActorFor = func(context.Context, *Session) Actor { return actor() }
	}
	tbl.AutomationWrite = w
	tbl.RegisterAutomationWrites()
	return tbl
}

func TestAutomationWriteDoorsAbsentWithoutSeam(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.RegisterAutomationWrites() // no seam: a no-op, not a refusal
	for _, m := range []MethodName{MethodTriggerUpdate, MethodAutomationArchive, MethodAutomationUnarchive} {
		if _, ok := tbl.Lookup(m); ok {
			t.Errorf("%s is registered with no seam wired — V13 wants it ABSENT", m)
		}
	}
	// And the S20 control seams now demand an actor too — a table with the
	// writer wired but the actor door unwired answers the honest absence.
	w := &fakeAutomationWrite{}
	tbl2 := writeDoorTable(w, nil)
	if _, ok := tbl2.Lookup(MethodTriggerUpdate); !ok {
		t.Fatal("the seam is wired and the door is missing")
	}
	_, err := callTrigger(t, tbl2, MethodTriggerUpdate, TriggerUpdateParams{ID: "sch-a"})
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != ErrAutomationNoActor.Code {
		t.Fatalf("unwired actor seam: %v — ErrAutomationNoActor before the seam", err)
	}
	if w.calls != 0 {
		t.Fatal("the seam was reached on an unresolvable person")
	}
}

func TestWriteDoorsRefuseForgedIdentityKeysByName(t *testing.T) {
	w := &fakeAutomationWrite{}
	tbl := writeDoorTable(w, func() Actor { return Actor{Kind: "user", ID: "1000"} })
	forged := []string{"actor", "actorKind", "createdBy", "origin", "changedBy"}
	for _, m := range []MethodName{MethodTriggerUpdate, MethodAutomationArchive, MethodAutomationUnarchive,
		MethodTriggerPause, MethodTriggerResume, MethodTriggerFireNow, MethodTriggerAdd,
		MethodAutomationTriggerCreate, MethodAutomationTriggerUpdate, MethodAutomationTriggerEnable, MethodAutomationTriggerDisable} {
		for _, key := range forged {
			params := json.RawMessage(`{"id":"x","` + key + `":"agent"}`)
			_, err := callTrigger(t, tbl, m, params)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Errorf("%s with forged %q: %v — the shape error, naming the key", m, key, err)
			}
			if !strings.Contains(err.Error(), `"`+key+`"`) {
				t.Errorf("%s with forged %q: %q — the refusal must NAME the key", m, key, err)
			}
			if w.calls != 0 {
				t.Fatalf("%s with forged %q reached the seam", m, key)
			}
		}
	}
	// The innocent unknown stays IGNORED (T3's rule, byte for byte): a
	// FUTURE field must not brick today's client.
	if _, err := callTrigger(t, tbl, MethodAutomationArchive, json.RawMessage(`{"source":"schedule","id":"x","reason":"gone","someFutureField":true}`)); err != nil {
		t.Fatalf("an innocent unknown field broke a write door: %v", err)
	}
}

func TestArchiveDoorsHoldTheirShapeBeforeIdentity(t *testing.T) {
	w := &fakeAutomationWrite{}
	tbl := writeDoorTable(w, func() Actor { return Actor{Kind: "user", ID: "1000"} })
	_, err := callTrigger(t, tbl, MethodAutomationArchive, AutomationArchiveParams{Source: "goal", ID: "x", Reason: "why"})
	if !strings.Contains(err.Error(), "not a source") {
		t.Fatalf("unknown source: %v — the three families answer, the source is part of the name", err)
	}
	_, err = callTrigger(t, tbl, MethodAutomationArchive, AutomationArchiveParams{Source: "schedule", ID: "x"})
	if !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing reason: %v — the archive needs its companion sentence", err)
	}
	long := strings.Repeat("x", 257)
	_, err = callTrigger(t, tbl, MethodAutomationArchive, json.RawMessage(`{"source":"schedule","id":"x","reason":"`+long+`"}`))
	if !strings.Contains(err.Error(), "1–256") {
		t.Fatalf("257-rune reason: %v — the bound is stated, not enforced silently", err)
	}
	// Unarchive does NOT demand a reason (it clears a state; it does not
	// add one).
	if _, err := callTrigger(t, tbl, MethodAutomationUnarchive, AutomationArchiveParams{Source: "event", ID: "trg1"}); err != nil {
		t.Fatalf("unarchive without a reason: %v — the shape belongs to archive alone", err)
	}
	if w.unarchiveCalls != 1 {
		t.Fatalf("unarchive calls = %d", w.unarchiveCalls)
	}
}

func TestUpdateDoorCarriesThePersonsIdentityAndSparseEdits(t *testing.T) {
	w := &fakeAutomationWrite{}
	tbl := writeDoorTable(w, func() Actor { return Actor{Kind: "user", ID: "1000"} })
	cron := "30 7 * * *"
	if _, err := callTrigger(t, tbl, MethodTriggerUpdate, TriggerUpdateParams{ID: "sch-a", Cron: &cron}); err != nil {
		t.Fatal(err)
	}
	if w.lastActor.Kind != "user" || w.lastActor.ID != "1000" {
		t.Errorf("seam saw %+v — the connection's person must ride to the store", w.lastActor)
	}
	if w.lastUpdate.Cron == nil || *w.lastUpdate.Cron != "30 7 * * *" {
		t.Error("the sparse edit was lost on the way to the seam")
	}
	if w.lastUpdate.Blueprint != nil || w.lastUpdate.Goal != nil {
		t.Error("absent fields arrived non-nil — absent must stay absent")
	}
}

func TestS20ControlDoorsNowRefuseAgentsFirst(t *testing.T) {
	// The four S20 doors gained the same lock S40 always had: refused
	// FIRST, before the seam, in the registry's sentence.
	cases := []struct {
		m  MethodName
		fn func(tbl *Table)
	}{
		{MethodTriggerPause, func(tbl *Table) {
			tbl.TriggerPause = func(context.Context, Actor, string) (any, error) { return "reached", nil }
		}},
		{MethodTriggerResume, func(tbl *Table) {
			tbl.TriggerResume = func(context.Context, Actor, string) (any, error) { return "reached", nil }
		}},
		{MethodTriggerFireNow, func(tbl *Table) {
			tbl.TriggerFireNow = func(context.Context, Actor, string) (any, error) { return "reached", nil }
		}},
		{MethodTriggerAdd, func(tbl *Table) {
			tbl.TriggerAdd = func(context.Context, Actor, TriggerAddParams) (any, error) { return "reached", nil }
		}},
	}
	for _, c := range cases {
		tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
		c.fn(tbl)
		tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "brainstormer"} }
		_, err := callTrigger(t, tbl, c.m, map[string]any{"id": "sch-a", "kind": "schedule"})
		var perr *Error
		if !errors.As(err, &perr) || perr.Code != ErrAutomationRefused.Code {
			t.Fatalf("%s as agent: %v — ErrAutomationRefused", c.m, err)
		}
		if !strings.Contains(perr.Data.What, `written by a person; this actor is "agent"`) {
			t.Errorf("%s: %q — S40's sentence verbatim", c.m, perr.Data.What)
		}
	}
}
