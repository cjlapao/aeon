package protocol

// policy_authoring_test.go — S45 T4 (PM1/PM7.3): the dispatch shape of the
// human door, tested against a RECORDING seam. The behaviour (does the row
// land, does the audit fire) is daemon/policy_authoring_test.go's subject;
// this file pins what the PROTOCOL owns: the order of the checks (the
// CONNECTION's actor first, params after — a caller cannot probe the
// decoder with an unattributable connection), the identity-key refusal,
// the notAfter shape pre-check, and the two named spec tests for
// conditional registration (the guards file owns the sweep; these are the
// task table's names).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// recordingAuthoring captures what the handlers hand the seam — proof the
// actor arrived from ActorFor and the params arrived decoded.
type recordingAuthoring struct {
	addActor  Actor
	addParams PolicyAddParams
	addCalls  int

	removeActor Actor
	removeID    string
	removeCalls int

	seedActor  Actor
	seedParams SeedCorpusParams
	seedCalls  int

	// removeResult/seedResult let a test answer like the daemon would.
	removeResult PolicyRemoveResult
	seedResult   SeedCorpusResult
}

func (r *recordingAuthoring) Add(_ context.Context, actor Actor, p PolicyAddParams) (PolicyAddResult, error) {
	r.addCalls++
	r.addActor, r.addParams = actor, p
	return PolicyAddResult{}, nil
}
func (r *recordingAuthoring) Remove(_ context.Context, actor Actor, id string) (PolicyRemoveResult, error) {
	r.removeCalls++
	r.removeActor, r.removeID = actor, id
	if r.removeResult.Policy != nil || r.removeResult.Removed {
		return r.removeResult, nil
	}
	return PolicyRemoveResult{Removed: false,
		Before: InForceView{Answers: []PolicyInForceRow{}},
		After:  InForceView{Answers: []PolicyInForceRow{}}}, nil
}
func (r *recordingAuthoring) SeedCorpus(_ context.Context, actor Actor, p SeedCorpusParams) (SeedCorpusResult, error) {
	r.seedCalls++
	r.seedActor, r.seedParams = actor, p
	if r.seedResult.DryRun || r.seedResult.Seeded > 0 {
		return r.seedResult, nil
	}
	return SeedCorpusResult{PerAgent: []SeedAgentCount{}, DryRun: p.DryRun}, nil
}

func authoringTable(t *testing.T, seam PolicyAuthoring, actor Actor, actorErr bool) (*Table, *recordingAuthoring) {
	t.Helper()
	tbl := newTestTable(t)
	rec, ok := seam.(*recordingAuthoring)
	if !ok {
		rec = &recordingAuthoring{}
		seam = rec
	}
	tbl.PolicyAuthoring = seam
	if !tbl.RegisterPolicyAuthoring() {
		t.Fatal("RegisterPolicyAuthoring returned false with a wired seam")
	}
	if !actorErr {
		a := actor
		tbl.ActorFor = func(context.Context, *Session) Actor { return a }
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, rec
}

var okActor = Actor{Kind: "user", ID: "4321"}

func TestPolicyAuthoringMethodsAbsentWhenServiceNil(t *testing.T) {
	// The task table's name, restating the guard file's claim at dispatch:
	// a NewTable without the service has NONE of the three names, and
	// Dispatch answers the SENTINEL ErrUnknownMethod — registered-and-
	// refusing (a handler that always answers "the door is closed") would
	// be V13's forbidden shape: it tells the caller the door exists.
	tbl := newTestTable(t)
	if _, ok := tbl.Lookup(MethodPolicyAdd); ok {
		t.Fatal("policy.add is registered without the seam")
	}
	for _, name := range []MethodName{MethodPolicyAdd, MethodPolicyRemove, MethodPolicySeedCorpus} {
		_, err := tbl.Dispatch(context.Background(), "c1", name, nil)
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Fatalf("%s dispatch: err = %T (%v), want ErrUnknownMethod", name, err, err)
		}
	}
}

func TestPolicyAuthoringMethodsNotDebugAndRequireInit(t *testing.T) {
	// The task table's name: the door is a PRODUCT surface (Debug false —
	// what keeps it off the narrow surface is T6's PM10 negatives, never
	// the Debug accident) and no authoring write arrives on an unnegotiated
	// connection (RequiresInitialize true).
	tbl := newTestTable(t)
	tbl.PolicyAuthoring = &recordingAuthoring{}
	tbl.RegisterPolicyAuthoring()
	for _, name := range []MethodName{MethodPolicyAdd, MethodPolicyRemove, MethodPolicySeedCorpus} {
		m, ok := tbl.Lookup(name)
		if !ok {
			t.Fatalf("%s missing with the seam wired", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s does not require Initialize", name)
		}
		// And the gate is real, not a field nobody reads: before
		// initialize, the refusal is the initialize refusal.
		bare := newTestTable(t)
		bare.PolicyAuthoring = &recordingAuthoring{}
		bare.RegisterPolicyAuthoring()
		if _, err := bare.Dispatch(context.Background(), "c-new", name, nil); err == nil {
			t.Errorf("%s dispatched before initialize", name)
		}
	}
}

// The actor is the CONNECTION's, stamped before params are read at all.
func TestPolicyAuthoringActorFromTableNotParams(t *testing.T) {
	tbl, rec := authoringTable(t, nil, okActor, false)
	raw := mustJSON(t, PolicyAddParams{
		ID: "a1", Type: "tool_access",
		Target: PolicyTargetP{Kind: "agent", ID: "boss"},
		Action: "deny",
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	})
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyAdd, raw); err != nil {
		t.Fatalf("add: %v", err)
	}
	if rec.addActor != okActor {
		t.Errorf("seam saw actor %+v, want %+v (ActorFor's stamp)", rec.addActor, okActor)
	}
	if rec.addParams.ID != "a1" || string(rec.addParams.Spec) != `{"toolName":"bash","state":"deny"}` {
		t.Errorf("params did not arrive decoded intact: %+v", rec.addParams)
	}

	// ORDER (Behaviour 1): actor BEFORE params — with no resolvable peer,
	// even MALFORMED params answer ErrPolicyNoActor, never the decode
	// error: an unattributable connection cannot probe the decoder.
	tbl2, _ := authoringTable(t, nil, okActor, true) // no ActorFor seam at all
	_, err := tbl2.Dispatch(context.Background(), "c1", MethodPolicyAdd, json.RawMessage(`{oops`))
	pe := asProtocolError(t, err)
	if pe.Code != ErrPolicyNoActor.Code {
		t.Fatalf("err code = %d, want ErrPolicyNoActor — the actor is resolved FIRST (never a placeholder, never after decode)", pe.Code)
	}
}

func TestPolicyAuthoringNoActorRefusesAllThree(t *testing.T) {
	// uid "0" (T0's unresolvable peer) and the empty id both refuse — for
	// EVERY authoring method, and BEFORE the seam is touched.
	for _, id := range []string{"0", ""} {
		tbl, rec := authoringTable(t, nil, Actor{Kind: "user", ID: id}, false)
		_, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyAdd,
			mustJSON(t, PolicyAddParams{ID: "x", Type: "tool_access",
				Target: PolicyTargetP{Kind: "agent", ID: "b"}, Action: "deny",
				Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)}))
		pe := asProtocolError(t, err)
		if pe.Code != ErrPolicyNoActor.Code {
			t.Errorf("add with actor id %q: code %d, want ErrPolicyNoActor", id, pe.Code)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyRemove, mustJSON(t, PolicyRemoveParams{ID: "x"})); err == nil {
			t.Errorf("remove with actor id %q succeeded", id)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodPolicySeedCorpus, mustJSON(t, SeedCorpusParams{DryRun: true})); err == nil {
			t.Errorf("seed with actor id %q succeeded", id)
		}
		if rec.addCalls+rec.removeCalls+rec.seedCalls != 0 {
			t.Errorf("the seam was reached with actor id %q (%d calls)", id, rec.addCalls+rec.removeCalls+rec.seedCalls)
		}
	}
}

// The identity keys refuse BY NAME — the package-wide decodeParams ignores
// unknown fields, so the door is the one that must refuse them (the
// proposals_agent_import precedent).
func TestPolicyAuthoringRefusesIdentityKeys(t *testing.T) {
	tbl, rec := authoringTable(t, nil, okActor, false)
	for _, key := range []string{"actor", "status", "createdBy", "acceptedBy"} {
		raw := json.RawMessage(`{"id":"k","type":"tool_access","target":{"kind":"agent","id":"b"},` +
			`"action":"deny","spec":{"toolName":"bash","state":"deny"},"` + key + `":"me"}`)
		_, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyAdd, raw)
		var bp ErrBadParams
		if !errors.As(err, &bp) {
			t.Errorf("%s: err = %T (%v), want ErrBadParams", key, err, err)
			continue
		}
		if !strings.Contains(bp.Error(), `"`+key+`"`) {
			t.Errorf("%s: the refusal must name the key it refused: %q", key, bp.Error())
		}
	}
	if rec.addCalls != 0 {
		t.Errorf("the seam saw %d calls despite the identity-key refusals", rec.addCalls)
	}
}

// notAfter is RFC3339-or-null AT THE EDGE: a malformed instant is the
// caller's shape error (-32602), never a row stored with an unreadable
// time and never the boundary's problem.
func TestPolicyAuthoringNotAfterShapeRefused(t *testing.T) {
	tbl, rec := authoringTable(t, nil, okActor, false)
	raw := json.RawMessage(`{"id":"t1","type":"tool_access","target":{"kind":"agent","id":"b"},` +
		`"action":"deny","spec":{"toolName":"bash","state":"deny"},"notAfter":"tomorrow, maybe"}`)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyAdd, raw)
	var bp ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("err = %T (%v), want ErrBadParams", err, err)
	}
	if !strings.Contains(bp.Error(), "notAfter") {
		t.Errorf("the refusal must name notAfter: %q", bp.Error())
	}
	if rec.addCalls != 0 {
		t.Error("the seam ran despite the shape refusal")
	}
	// null and omitted both pass the edge check (PL7's absence).
	for _, tail := range []string{`"notAfter":null`, `"notAfter":"2999-01-01T00:00:00Z"`} {
		ok := json.RawMessage(`{"id":"t2","type":"tool_access","target":{"kind":"agent","id":"b"},` +
			`"action":"deny","spec":{"toolName":"bash","state":"deny"},` + tail + `}`)
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyAdd, ok); err != nil {
			t.Errorf("params with %s: %v", tail, err)
		}
	}
}

// The seam's answers travel intact — including the {removed:false,
// policy:null} no-op (DB19 is a RESULT, not an error) and the dry-run seed
// counts.
func TestPolicyAuthoringResultsTravelIntact(t *testing.T) {
	seam := &recordingAuthoring{
		removeResult: PolicyRemoveResult{Removed: true,
			Before: InForceView{Target: "agent:b", Answers: []PolicyInForceRow{{PolicyID: "r", State: "in-force", Render: "r: in force"}}},
			After:  InForceView{Target: "agent:b", Answers: []PolicyInForceRow{}},
			Policy: &PolicyDetail{PolicyView: PolicyView{ID: "r"}}},
		seedResult: SeedCorpusResult{PerAgent: []SeedAgentCount{{Agent: "a", Seeded: 1, Unchanged: 2}},
			Seeded: 1, Unchanged: 2, DryRun: true},
	}
	tbl, _ := authoringTable(t, seam, okActor, false)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyRemove, mustJSON(t, PolicyRemoveParams{ID: "r"}))
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := res.(PolicyRemoveResult)
	if !got.Removed || got.Policy == nil || got.Policy.ID != "r" || got.Before.Answers[0].Render != "r: in force" {
		t.Errorf("remove result mangled in transit: %+v", got)
	}
	// A no-op removal from the seam: the SAME wire shape the daemon gives.
	seam.removeResult = PolicyRemoveResult{Removed: false,
		Before: InForceView{Answers: []PolicyInForceRow{}},
		After:  InForceView{Answers: []PolicyInForceRow{}}}
	res, err = tbl.Dispatch(context.Background(), "c1", MethodPolicyRemove, mustJSON(t, PolicyRemoveParams{ID: "gone"}))
	if err != nil {
		t.Fatalf("absent remove must not error (DB19): %v", err)
	}
	if no := res.(PolicyRemoveResult); no.Removed || no.Policy != nil {
		t.Errorf("the no-op wire shape drifted: %+v", no)
	}

	sres, err := tbl.Dispatch(context.Background(), "c1", MethodPolicySeedCorpus,
		mustJSON(t, SeedCorpusParams{Agents: []string{"a"}, DryRun: true}))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	sc := sres.(SeedCorpusResult)
	if !sc.DryRun || sc.Seeded != 1 || sc.Unchanged != 2 || len(sc.PerAgent) != 1 || sc.PerAgent[0].Agent != "a" {
		t.Errorf("seed result mangled: %+v", sc)
	}
}

func asProtocolError(t *testing.T, err error) *Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %T (%v), want *Error", err, err)
	}
	return e
}
