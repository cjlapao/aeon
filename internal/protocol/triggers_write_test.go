package protocol

// triggers_write_test.go — AU1 is a test, not a comment: the actor check is
// the FIRST state-touching step of every write handler, so an agent learns
// nothing about validity, and a user's malformed request is refused by
// naming the field rather than by a silent default.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type countingTriggerWriter struct {
	calls  int
	lastID string
}

func (c *countingTriggerWriter) CreateEventTrigger(a Actor, p EventTriggerCreateParams) (EventTriggerView, error) {
	c.calls++
	if a.Kind != "user" {
		return EventTriggerView{}, errors.New("the handler leaked a non-person to the seam")
	}
	return EventTriggerView{ID: "trg-new", Name: p.Name, CreatedByKind: a.Kind, CreatedByID: a.ID}, nil
}

func (c *countingTriggerWriter) UpdateEventTrigger(a Actor, p EventTriggerUpdateParams) (EventTriggerView, error) {
	c.calls++
	c.lastID = p.ID
	return EventTriggerView{ID: p.ID}, nil
}

func (c *countingTriggerWriter) EnableEventTrigger(a Actor, id string) (EventTriggerView, error) {
	c.calls++
	c.lastID = id
	return EventTriggerView{ID: id, Enabled: true}, nil
}

func (c *countingTriggerWriter) DisableEventTrigger(a Actor, id, reason string) (EventTriggerView, error) {
	c.calls++
	c.lastID = id
	return EventTriggerView{ID: id, Enabled: false, DisabledReason: reason}, nil
}

func writeTable(w EventTriggerWriter, actor func() Actor) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.TriggerWrites = w
	if actor != nil {
		tbl.ActorFor = func(context.Context, *Session) Actor { return actor() }
	}
	return tbl
}

// expectedActorCode is the code each identity case MUST answer: a case
// that NAMED itself a non-person is refused; a case with no name at all is
// the honest absence (T4's two-door split of S40's one shape error).
func expectedActorCode(name string) *Error {
	switch name {
	case "agent", "shapeless":
		return ErrAutomationRefused
	default: // user-without-id, unwired
		return ErrAutomationNoActor
	}
}

func TestEveryTriggerWriteRefusesAnAgentBeforeTouchingTheStore(t *testing.T) {
	cases := []struct {
		name  string
		actor func() Actor
	}{
		{"agent", func() Actor { return Actor{Kind: "agent", ID: "brainstormer"} }},
		{"shapeless", func() Actor { return Actor{} }},
		{"user-without-id", func() Actor { return Actor{Kind: "user", ID: "   "} }},
		{"unwired", nil},
	}
	for _, c := range cases {
		w := &countingTriggerWriter{}
		tbl := writeTable(w, c.actor)
		for _, tc := range []struct {
			m      MethodName
			params any
		}{
			{MethodAutomationTriggerCreate, EventTriggerCreateParams{Name: "n", Match: json.RawMessage(`{"event":"task.created"}`), Blueprint: "two-step", MaxRunsPerHour: 5}},
			{MethodAutomationTriggerUpdate, EventTriggerUpdateParams{ID: "trg1"}},
			{MethodAutomationTriggerEnable, EventTriggerEnableParams{ID: "trg1"}},
			{MethodAutomationTriggerDisable, EventTriggerDisableParams{ID: "trg1", Reason: "paused for the record"}},
		} {
			_, err := callTrigger(t, tbl, tc.m, tc.params)
			// S49 T4: the two honest refusals got their own codes —
			// a named non-person is ErrAutomationRefused (-31893), an
			// unresolvable person ErrAutomationNoActor (-31892). The
			// -32602 era of S40 dressed both as shape errors; the rule
			// this test guards is unchanged: refused FIRST, in the same
			// sentence, the seam never called.
			var perr *Error
			if !errors.As(err, &perr) || (perr.Code != ErrAutomationRefused.Code && perr.Code != ErrAutomationNoActor.Code) {
				t.Fatalf("%s via %s: %v — every non-person is refused by ErrAutomationRefused or ErrAutomationNoActor", c.name, tc.m, err)
			}
			if want := expectedActorCode(c.name); perr.Code != want.Code {
				t.Errorf("%s via %s: code %d, wanted %d (%s)", c.name, tc.m, perr.Code, want.Code, want.Message)
			}
			if !strings.Contains(perr.Data.What, "written by a person") {
				t.Errorf("%s via %s: %q — the refusal names the rule, not just the failure", c.name, tc.m, err)
			}
		}
		if w.calls != 0 {
			t.Fatalf("%s reached the seam %d times — the actor check must come FIRST", c.name, w.calls)
		}
	}
	// The person's door actually opens — one call per method, no more.
	w := &countingTriggerWriter{}
	tbl := writeTable(w, func() Actor { return Actor{Kind: "user", ID: "1000"} })
	if _, err := callTrigger(t, tbl, MethodAutomationTriggerCreate, EventTriggerCreateParams{
		Name: "n", Match: json.RawMessage(`{"event":"task.created"}`), Blueprint: "two-step", MaxRunsPerHour: 5}); err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 {
		t.Fatalf("%d calls for one create — the seam is one door, not a corridor", w.calls)
	}
}

func TestTriggerWriteHandlersValidateRequiredFields(t *testing.T) {
	w := &countingTriggerWriter{}
	tbl := writeTable(w, func() Actor { return Actor{Kind: "user", ID: "1000"} })
	good := EventTriggerCreateParams{Name: "n", Match: json.RawMessage(`{"event":"task.created"}`), Blueprint: "two-step", MaxRunsPerHour: 5}
	cases := []struct {
		name   string
		mut    func(*EventTriggerCreateParams)
		wanted string
	}{
		{"empty name", func(p *EventTriggerCreateParams) { p.Name = "  " }, "name is required"},
		{"missing match", func(p *EventTriggerCreateParams) { p.Match = nil }, "match is required"},
		{"null match", func(p *EventTriggerCreateParams) { p.Match = json.RawMessage("null") }, "match is required"},
		{"empty blueprint", func(p *EventTriggerCreateParams) { p.Blueprint = "" }, "blueprint is required"},
		{"zero rate", func(p *EventTriggerCreateParams) { p.MaxRunsPerHour = 0 }, "maxRunsPerHour"},
	}
	for _, c := range cases {
		p := good
		c.mut(&p)
		_, err := callTrigger(t, tbl, MethodAutomationTriggerCreate, p)
		var bad ErrBadParams
		if !errors.As(err, &bad) || !strings.Contains(err.Error(), c.wanted) {
			t.Errorf("%s: %v — ErrBadParams naming %q", c.name, err, c.wanted)
		}
	}
	if _, err := callTrigger(t, tbl, MethodAutomationTriggerDisable, EventTriggerDisableParams{ID: "trg1", Reason: "  "}); err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Errorf("disable without a reason: %v — a pause must be explainable", err)
	}
	if _, err := callTrigger(t, tbl, MethodAutomationTriggerUpdate, EventTriggerUpdateParams{ID: " "}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("blank id: %v — refused by name", err)
	}
	if w.calls != 0 {
		t.Fatalf("%d seam calls from refused writes — validation belongs before the door", w.calls)
	}
}
