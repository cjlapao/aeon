package protocol

// environment_propose_test.go — S59 T7 (EV16): the SHAPE of the plane's one
// agent door, beside blueprint_propose_test.go's and the policy/autonomy
// twins'. What lives here is the DOOR: registered iff the records service
// stands (V13), gated and non-debug like every write on this table,
// dispatching through the SAME RecordsAPI.Do the tool door calls (one
// implementation, two doors — a handler here that answered for itself would
// be the second door EV16 forbids), and refusing the plane's identity and
// value vocabulary by KEY before anything else runs (EV7, EV17).
//
// The behaviour behind the Do — the stamped author, the shared validator,
// execenv.Restricts, the cap, the inertness of the row — is the daemon's
// records_environment_test.go and the desk's environment_kinds_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// envProposeSpy is the records seam with a memory: the tool name AND the raw
// params it was handed, and a refusal sentence to answer with. Built on
// recordNameSpy so the five decision methods stay one definition.
type envProposeSpy struct {
	*recordNameSpy
	params   json.RawMessage
	calls    int
	sentence string
}

func (s *envProposeSpy) Do(ctx context.Context, sess *Session, tool string, params json.RawMessage) (any, string, error) {
	s.last = tool
	s.params = params
	s.calls++
	if s.sentence != "" {
		return nil, s.sentence, nil
	}
	return map[string]any{"ok": true}, "", nil
}

// TestEnvironmentProposeRegistrationIsConditional is the V13 half: no records
// service, no method — ABSENT (ErrUnknownMethod), never registered-and-
// refusing. The records service is what honours what makes the door safe (the
// stamped agent actor, the shared create validator running execenv.Restricts,
// the per-agent open-proposal cap), so an unwired runtime does not advertise
// it. The four READS are not this test's subject: RegisterEnvironment and
// RegisterSecretRef own them over the plane's own seams (T6's guard pins
// theirs), and registerRecords skips the whole family so no door is ever
// projected twice.
func TestEnvironmentProposeRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("RegisterEnvironmentProposeRestrict registered over a nil records seam — V13 says absent, not refusing")
	}
	if _, ok := tbl.Lookup(MethodEnvironmentProposeRestrict); ok {
		t.Error("environment.proposeRestrict stands without the records service — the mutating door was registered-and-refusing")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodEnvironmentProposeRestrict, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("RegisterEnvironmentProposeRestrict refused a wired seam")
	}
	got, ok := tbl.Lookup(MethodEnvironmentProposeRestrict)
	if !ok {
		t.Fatal("environment.proposeRestrict did not register with the service wired")
	}
	if !got.RequiresInitialize {
		t.Error("environment.proposeRestrict is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)")
	}
	if got.Debug {
		t.Error("environment.proposeRestrict is Debug — product surface (PM10)")
	}
	// Idempotent: a second call is a no-op, not a second door (t.register
	// panics on a duplicate, so this is a crash test as much as a shape one).
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Error("RegisterEnvironmentProposeRestrict is not idempotent over the same wired seam")
	}
}

// TestEnvironmentProposeDispatchesThroughDo pins the ONE implementation: the
// socket twin dispatches into RecordsAPI.Do with the catalog tool name and the
// caller's params untouched, so the socket door and the tool door answer
// through the same function.
func TestEnvironmentProposeDispatchesThroughDo(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	tbl := newTestTable(t)
	tbl.Records = spy
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("RegisterEnvironmentProposeRestrict refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	body := `{"environmentId":"aeon-scrubbed","spec":"backend: host-scrubbed\n","reason":"drop AWS_SECRET_ACCESS_KEY"}`
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodEnvironmentProposeRestrict, json.RawMessage(body)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if spy.last != "environment_propose_restrict" {
		t.Errorf("Do saw tool %q, want the catalog name environment_propose_restrict — the socket door went around the catalog", spy.last)
	}
	if string(spy.params) != body {
		t.Errorf("Do received %s, want the caller's params verbatim (%s) — a door that re-writes the ask before the service sees it is a second authority", spy.params, body)
	}
}

// TestEnvironmentProposeRefusesIdentityAndValueKeys is EV7/EV17 at the wire:
// the plane's key gate runs BEFORE the records service, so a `revision`, a
// `source`, an `actor` or a `value` never reaches the service at all — the
// baseline is read from the row, the actor is stamped from the step, and a
// value has no door in either direction.
func TestEnvironmentProposeRefusesIdentityAndValueKeys(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"revision", `{"environmentId":"e","spec":"{}","reason":"r","revision":7}`},
		{"source", `{"environmentId":"e","spec":"{}","reason":"r","source":"/etc/passwd"}`},
		{"sourceKind", `{"environmentId":"e","spec":"{}","reason":"r","sourceKind":"key-file"}`},
		{"actor", `{"environmentId":"e","spec":"{}","reason":"r","actor":{"kind":"agent","id":"a"}}`},
		{"origin", `{"environmentId":"e","spec":"{}","reason":"r","origin":"system"}`},
		{"legacy", `{"environmentId":"e","spec":"{}","reason":"r","legacy":true}`},
		{"createdBy", `{"environmentId":"e","spec":"{}","reason":"r","createdBy":"someone"}`},
	} {
		spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
		tbl := newTestTable(t)
		tbl.Records = spy
		if !tbl.RegisterEnvironmentProposeRestrict() {
			t.Fatal("RegisterEnvironmentProposeRestrict refused")
		}
		init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
		if _, err := tbl.Dispatch(context.Background(), "spy-"+tc.name, MethodInitialize, init); err != nil {
			t.Fatalf("initialize: %v", err)
		}
		_, err := tbl.Dispatch(context.Background(), "spy-"+tc.name, MethodEnvironmentProposeRestrict, json.RawMessage(tc.body))
		if err == nil {
			t.Errorf("%s: the door accepted a %s key and dispatched it — the plane's key gate is off (EV7, EV17)", tc.name, tc.name)
			continue
		}
		if spy.calls != 0 {
			t.Errorf("%s: the service was still called (%d) after the key gate should have refused — the gate must run FIRST", tc.name, spy.calls)
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Errorf("%s: the refusal %q does not name the key it refused", tc.name, err)
		}
	}
}

// TestEnvironmentProposeCarriesTheRefusalSentence: a domain refusal from the
// service (the widening list, the cap, the unknown row) travels to the caller
// as the params error with the service's OWN words — not paraphrased, not
// dropped, and never as an answer that reads like a proposal.
func TestEnvironmentProposeCarriesTheRefusalSentence(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}, sentence: "refused: environment_restrict aeon-scrubbed would WIDEN — spec.egress"}
	tbl := newTestTable(t)
	tbl.Records = spy
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("RegisterEnvironmentProposeRestrict refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := tbl.Dispatch(context.Background(), "spy", MethodEnvironmentProposeRestrict,
		json.RawMessage(`{"environmentId":"aeon-scrubbed","spec":"{}","reason":"widen egress"}`))
	if err == nil {
		t.Fatal("a refusal sentence came back as a success — an agent would believe its ask was filed")
	}
	if !strings.Contains(err.Error(), "would WIDEN") {
		t.Errorf("the sentence did not travel verbatim: %v", err)
	}
	if !errors.As(err, new(ErrBadParams)) {
		t.Errorf("the refusal is %T, want ErrBadParams — the params error is how a catalog door says no (DB19's shape at the wire)", err)
	}
}

// TestEnvironmentProposeParamsHaveNoRoomForAnIdentity is the type-level half of
// the rule: three fields, and nothing the step is supposed to stamp. A schema
// can be smuggled a key the type does not have; a type cannot be smuggled one.
func TestEnvironmentProposeParamsHaveNoRoomForAnIdentity(t *testing.T) {
	raw, err := json.Marshal(EnvironmentProposeRestrictParams{EnvironmentID: "e", Spec: "{}", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 {
		t.Errorf("the params carry %d keys (%v), want exactly environmentId, spec and reason", len(m), keysOf(m))
	}
	for _, banned := range []string{"backend", "actor", "actorKind", "origin", "legacy", "source", "sourceKind", "revision", "status", "kind", "value", "secret", "token", "createdBy"} {
		if _, ok := m[banned]; ok {
			t.Errorf("the propose params carry %q — identity, backend and value are the service's and the person's, never the caller's (EV7, EV16)", banned)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// envProposeConformanceRecords is the records seam the conformance walker
// mounts for the plane's ONE agent door: it answers the fixture's own result
// bytes and counts its calls, so a door-refusal fixture can assert the plane
// was never consulted and a success fixture can assert it was consulted
// exactly once (rule 4 — the tool door and this method are one implementation).
type envProposeConformanceRecords struct {
	*recordNameSpy
	answer json.RawMessage
	calls  int
}

func (s *envProposeConformanceRecords) Do(_ context.Context, _ *Session, tool string, _ json.RawMessage) (any, string, error) {
	s.calls++
	s.last = tool
	if len(s.answer) == 0 {
		return nil, "refused: the conformance seam answers nothing unless the fixture says what it answers", nil
	}
	return s.answer, "", nil
}
