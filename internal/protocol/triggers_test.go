package protocol

// triggers_test.go — the three reads against a fake reader: the shape rules
// ([] never null, five named outcomes, clamped limits) and the honest
// unwired answer. The mapping to store rows is the daemon's test's job.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeTriggerReader struct {
	triggers []EventTriggerView
	fires    []EventFireView
	counts   map[string]int
	gotFires EventTriggerFiresParams // what the reader actually saw
	err      error
}

func (f *fakeTriggerReader) ListEventTriggers(EventTriggerListParams) ([]EventTriggerView, error) {
	return f.triggers, f.err
}

func (f *fakeTriggerReader) ShowTrigger(id string) (EventTriggerView, bool, error) {
	if f.err != nil {
		return EventTriggerView{}, false, f.err
	}
	for _, v := range f.triggers {
		if v.ID == id {
			return v, true, nil
		}
	}
	return EventTriggerView{}, false, nil
}

func (f *fakeTriggerReader) ListEventFires(p EventTriggerFiresParams) ([]EventFireView, map[string]int, error) {
	f.gotFires = p
	return f.fires, f.counts, f.err
}

func triggerTable(r EventTriggerReader) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.TriggerReads = r
	return tbl
}

func callTrigger(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	meth, ok := tbl.Lookup(m)
	if !ok {
		t.Fatalf("%s is not registered", m)
	}
	return meth.Handle(context.Background(), nil, rawOf(t, params))
}

func TestTriggerListReturnsAnEmptySliceNotNull(t *testing.T) {
	res, err := callTrigger(t, triggerTable(&fakeTriggerReader{}), MethodAutomationTriggerList, EventTriggerListParams{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"triggers":[]`) {
		t.Fatalf("list serialised as %s — an empty page is [], never null", raw)
	}
}

func TestTriggerShowRefusesAnEmptyOrUnknownID(t *testing.T) {
	tbl := triggerTable(&fakeTriggerReader{triggers: []EventTriggerView{{ID: "tr1", Name: "one"}}})
	_, err := callTrigger(t, tbl, MethodAutomationTriggerShow, EventTriggerShowParams{ID: "   "})
	var bad ErrBadParams
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("empty id: %v — ErrBadParams naming the requirement", err)
	}
	_, err = callTrigger(t, tbl, MethodAutomationTriggerShow, EventTriggerShowParams{ID: "tr-nope"})
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), `no trigger "tr-nope"`) {
		t.Fatalf("unknown id: %v — ErrBadParams naming the id", err)
	}
	v, err := callTrigger(t, tbl, MethodAutomationTriggerShow, EventTriggerShowParams{ID: "tr1"})
	if err != nil {
		t.Fatal(err)
	}
	if ev, ok := v.(EventTriggerView); !ok || ev.Name != "one" {
		t.Fatalf("show returned %#v", v)
	}
}

func TestTriggerFiresValidatesOutcomeAndClampsLimit(t *testing.T) {
	fr := &fakeTriggerReader{counts: map[string]int{"started": 0, "refused": 0, "limited": 0, "skipped": 0, "could-not-say": 0}}
	tbl := triggerTable(fr)
	_, err := callTrigger(t, tbl, MethodAutomationTriggerFires, EventTriggerFiresParams{Outcome: "retried"})
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("invented outcome %v — ErrBadParams", err)
	}
	for _, o := range []string{"started", "refused", "limited", "skipped", "could-not-say"} {
		if !strings.Contains(err.Error(), o) {
			t.Errorf("refusal %q does not name %q — the five must be spellable from the message", err, o)
		}
	}
	if _, err := callTrigger(t, tbl, MethodAutomationTriggerFires, EventTriggerFiresParams{Limit: 0}); err != nil {
		t.Fatal(err)
	}
	if fr.gotFires.Limit != 50 {
		t.Errorf("limit 0 reached the reader as %d — want the 50 default", fr.gotFires.Limit)
	}
	if _, err := callTrigger(t, tbl, MethodAutomationTriggerFires, EventTriggerFiresParams{Limit: 9999}); err != nil {
		t.Fatal(err)
	}
	if fr.gotFires.Limit != 500 {
		t.Errorf("limit 9999 reached the reader as %d — want the 500 ceiling", fr.gotFires.Limit)
	}
}

func TestTheTriggerReadsAreHonestlyUnwiredWithoutAReader(t *testing.T) {
	tbl := triggerTable(nil) // a storeless daemon: the seam stays nil
	for _, tc := range []struct {
		m      MethodName
		params any
	}{
		{MethodAutomationTriggerList, EventTriggerListParams{}},
		{MethodAutomationTriggerShow, EventTriggerShowParams{ID: "tr1"}},
		{MethodAutomationTriggerFires, EventTriggerFiresParams{}},
	} {
		_, err := callTrigger(t, tbl, tc.m, tc.params)
		if !errors.Is(err, ErrStoreUnavailable) {
			t.Errorf("%s unwired: %v — want ErrStoreUnavailable, never a panic, never a fake success", tc.m, err)
		}
	}
}

func TestTheTriggerReadMethodsAreAdditiveAndNeedNoVersionBump(t *testing.T) {
	if PROTOCOL_VERSION != 2 {
		t.Fatalf("PROTOCOL_VERSION = %d — the reads are additive; the version rides at 2 (the S39-era pin, as of 0da33b16) and only a breaking change moves it", PROTOCOL_VERSION)
	}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	for _, m := range []MethodName{MethodAutomationTriggerList, MethodAutomationTriggerShow, MethodAutomationTriggerFires} {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s not registered", m)
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s answers before initialize — no socket method may", m)
		}
	}
}
