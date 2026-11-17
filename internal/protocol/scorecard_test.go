package protocol

// scorecard_test.go — S63 T5: the scorecard doors' own behaviour, at
// the door. The daemon's suite answers the NUMBERS; this file answers
// the shape: no identity may be typed in, a subject is required, the
// vocabulary takes no params, a nil seam leaves all five ABSENT (V13),
// and RT3's sentence is ONE constant the plane cannot lose.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func scorecardStubTable(t *testing.T, actor func(context.Context, *Session) Actor) (*Table, *scorecardTableStub) {
	t.Helper()
	tbl := newTestTable(t)
	stub := &scorecardTableStub{}
	tbl.ActorFor = actor
	tbl.Scorecard = stub
	if !tbl.RegisterScorecard() {
		t.Fatal("RegisterScorecard refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, stub
}

// TestScorecardDoorsRefuseIdentityKeys: every one of the five refuses
// `actor` (and its vocabulary) typed into params — by KEY, before the
// service is consulted. A card is about the subject it names; who asks
// changes no number, and a door that accepted an actor would be a door
// where someone could ASK for someone else's numbers with a typed name.
func TestScorecardDoorsRefuseIdentityKeys(t *testing.T) {
	cases := []struct {
		method MethodName
		params string
	}{
		{MethodScorecardAgent, `{"agent":"dev-loops","actor":{"kind":"user","id":"9001"}}`},
		{MethodScorecardBlueprint, `{"blueprint":"dev-loop","uid":"9001"}`},
		{MethodScorecardComponent, `{"id":"aeon","peer":"9001"}`},
		{MethodScorecardCompare, `{"kind":"component","subject":"aeon","windowA":{},"windowB":{},"identity":{"id":"9"}}`},
		{MethodScorecardVocabulary, `{"actorKind":"agent"}`},
	}
	for _, tc := range cases {
		tbl, _ := scorecardStubTable(t, userActorFor("peer-1"))
		_, err := tbl.Dispatch(context.Background(), "c1", tc.method, json.RawMessage(tc.params))
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s accepted an identity key: %v", tc.method, err)
		}
	}
}

func TestScorecardRequiresASubject(t *testing.T) {
	cases := []struct {
		method MethodName
		params string
	}{
		{MethodScorecardAgent, `{"agent":"  "}`},
		{MethodScorecardBlueprint, `{}`},
		{MethodScorecardComponent, `{"id":""}`},
		{MethodScorecardCompare, `{"kind":"agent","subject":""}`},
		{MethodScorecardCompare, `{"kind":"component_root","subject":"aeon","windowA":{},"windowB":{}}`},
	}
	for _, tc := range cases {
		tbl, _ := scorecardStubTable(t, userActorFor("peer-1"))
		_, err := tbl.Dispatch(context.Background(), "c1", tc.method, json.RawMessage(tc.params))
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s accepted params with no usable subject (%s): %v", tc.method, tc.params, err)
		}
	}
}

func TestScorecardVocabularyTakesNoParams(t *testing.T) {
	tbl, _ := scorecardStubTable(t, userActorFor("peer-1"))
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodScorecardVocabulary, nil); err != nil {
		t.Errorf("the vocabulary refused the empty call it must answer: %v", err)
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodScorecardVocabulary, json.RawMessage(`{"days":7}`)); err == nil {
		t.Error("the vocabulary accepted params — the published sets are not the caller's to bend")
	}
}

// TestScorecardStatementIsOneConstant: RT3's sentence lives ONCE in the
// plane. The digest file and the scorecard file both name it; a second
// literal of the sentence somewhere else is a second rule waiting to
// drift, and this test is what the daemon-side pins read against.
func TestScorecardStatementIsOneConstant(t *testing.T) {
	const want = "A score is a measurement of what happened. It does not change what any agent, blueprint or project is allowed or asked to do."
	if ScorecardStatement != want {
		t.Errorf("the sentence drifted:\n got  %q\n want %q", ScorecardStatement, want)
	}
}

func TestScorecardMethodsAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterScorecard() {
		t.Fatal("RegisterScorecard claimed a nil seam is wired")
	}
	for _, m := range scorecardMethodNames() {
		if _, ok := tbl.Lookup(m); ok {
			t.Errorf("%s registered over no seam — V13 says absent, never registered-and-refusing", m)
		}
	}
}
