package protocol

// retro_test.go — S63 T5 (RT13): the retro doors at the door, and one
// door in particular: retro.backfill, the plane's only write, is a
// person's door. Four things this file pins, before any store exists:
//
//   - the actor is the CONNECTION's person — a nil identity, uid 0 and
//     a blank id all refuse, and no param may name an actor instead;
//   - the write is CONFIRMED — dry-run is the default and the only
//     thing a first call can get; writing needs `yes: true` resent;
//   - the reads take no actor at all (facts are facts);
//   - a nil seam leaves all four ABSENT (V13).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// backfillSpy remembers the actor and params the last Backfill call
// carried, so the tests can assert what the door handed DOWN.
type backfillSpy struct {
	actor   Actor
	params  RetroBackfillParams
	calls   int
	reached bool
}

type retroSpySeam struct{ spy *backfillSpy }

func (s retroSpySeam) List(context.Context, RetroListParams) (RetroListResult, error) {
	s.spy.calls++
	s.spy.reached = true
	return RetroListResult{}, nil
}
func (s retroSpySeam) Show(context.Context, RetroShowParams) (RetroShowResult, error) {
	return RetroShowResult{}, nil
}
func (s retroSpySeam) Status(context.Context) (RetroStatusResult, error) {
	return RetroStatusResult{}, nil
}
func (s retroSpySeam) Backfill(_ context.Context, a Actor, p RetroBackfillParams) (RetroBackfillWire, error) {
	s.spy.actor = a
	s.spy.params = p
	s.spy.calls++
	s.spy.reached = true
	return RetroBackfillWire{}, nil
}

func retroTable(t *testing.T, actor func(context.Context, *Session) Actor) (*Table, *backfillSpy) {
	t.Helper()
	tbl := newTestTable(t)
	spy := &backfillSpy{}
	tbl.ActorFor = actor
	tbl.Retro = retroSpySeam{spy: spy}
	if !tbl.RegisterRetro() {
		t.Fatal("RegisterRetro refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, spy
}

// TestRetroBackfillIsAPersonDoor: the three refusals that happen BEFORE
// any parameter is read — no identity available, uid 0, blank id. The
// answer is the plane's refusal voice with its own sentence.
func TestRetroBackfillIsAPersonDoor(t *testing.T) {
	for name, actor := range map[string]func(context.Context, *Session) Actor{
		"no identity available": nil,
		"uid zero":              userActorFor("0"),
		"blank id":              userActorFor("   "),
	} {
		tbl, spy := retroTable(t, actor)
		_, err := tbl.Dispatch(context.Background(), "c1", MethodRetroBackfill, json.RawMessage(`{}`))
		var perr *Error
		if !errors.As(err, &perr) || perr.Code != ErrScorecardRefused.Code {
			t.Errorf("%s: backfill answered %v, want the plane's refusal", name, err)
		}
		if spy.reached {
			t.Errorf("%s: the service ran for a caller who is not a person", name)
		}
	}
}

// TestRetroBackfillWritesOnlyWhenConfirmed: the first call always
// previews; a write needs yes:true, and the refusal to write-silently
// happens at the door (ErrBadParams, fromDoor shape) with the sentence
// that names the confirmation.
func TestRetroBackfillWritesOnlyWhenConfirmed(t *testing.T) {
	tbl, spy := retroTable(t, userActorFor("peer-1"))
	// default: dry run, no yes needed.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRetroBackfill, nil); err != nil {
		t.Fatalf("the preview call refused: %v", err)
	}
	if !spy.params.DryRunOrTrue() {
		t.Error("the default call did not preview")
	}
	// dryRun:false WITHOUT yes — the door refuses; the service must not run.
	spy.calls, spy.reached = 0, false
	_, err := tbl.Dispatch(context.Background(), "c1", MethodRetroBackfill, json.RawMessage(`{"dryRun":false}`))
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("a write without yes:true answered %v, want the confirmation refusal", err)
	}
	if spy.reached {
		t.Error("an unconfirmed write reached the service — RT13 says the first call can only preview")
	}
	// dryRun:false WITH yes — the person confirmed; the service runs,
	// carrying the CONNECTION's actor.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRetroBackfill, json.RawMessage(`{"dryRun":false,"yes":true}`)); err != nil {
		t.Fatalf("the confirmed write refused: %v", err)
	}
	if spy.actor.Kind != "user" || spy.actor.ID != "peer-1" {
		t.Errorf("the write carried actor %+v, want the connection's user:peer-1", spy.actor)
	}
}

// DryRunOrTrue reads the wire's optional flag the way the door does.
func (p RetroBackfillParams) DryRunOrTrue() bool {
	return p.DryRun == nil || *p.DryRun
}

// TestRetroReadsRefuseTypedIdentity — list/show/status accept no actor,
// no source, no identity key: fact rows carry their own source and the
// door does not let a caller relabel it.
func TestRetroReadsRefuseTypedIdentity(t *testing.T) {
	tbl, _ := retroTable(t, userActorFor("peer-1"))
	for _, tc := range []struct {
		method MethodName
		params string
	}{
		{MethodRetroList, `{"actor":{"kind":"agent","id":"x"}}`},
		{MethodRetroShow, `{"outcomeId":7,"source":"recorder"}`},
		{MethodRetroStatus, `{"uid":"1"}`},
		{MethodRetroShow, `{"outcomeId":0}`},
	} {
		_, err := tbl.Dispatch(context.Background(), "c1", tc.method, json.RawMessage(tc.params))
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s accepted (%s): %v", tc.method, tc.params, err)
		}
	}
}

func TestRetroMethodsAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterRetro() {
		t.Fatal("RegisterRetro claimed a nil seam is wired")
	}
	for _, m := range retroMethodNames() {
		if _, ok := tbl.Lookup(m); ok {
			t.Errorf("%s registered over no seam", m)
		}
	}
}
