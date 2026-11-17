package protocol

// blueprint_propose_test.go — S48 T10 (BP9): the SHAPE of the blueprint
// propose door, beside policy_propose_test.go's and the autonomy twin's.
// The behaviour behind the Do (stamped author, shared validator, cap,
// inertness) is the daemon's records_blueprint_test.go; what lives here is
// the door: registered iff the records service stands (V13), gated and
// non-debug like every write on this table, and dispatching through the
// SAME RecordsAPI.Do the tool door calls — one implementation, two doors.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestBlueprintProposeRegistrationIsConditional is the V13 half: no
// records service, no method — ABSENT (ErrUnknownMethod), never
// registered-and-refusing. The catalog's blueprint family is four reads
// plus ONE mutating twin, and the service is what honours what makes the
// twin safe (the stamped agent actor, the shared create validator, the
// per-agent cap), so an unwired runtime does not advertise the door at
// all. The four reads are NOT this test's subject: RegisterBlueprintReads
// owns them over the read service (blueprint_manage_test.go pins their
// conditionality).
func TestBlueprintProposeRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterBlueprintPropose() {
		t.Fatal("RegisterBlueprintPropose registered over a nil records seam — V13 says absent, not refusing")
	}
	if _, ok := tbl.Lookup(MethodBlueprintPropose); ok {
		t.Error("blueprint.propose stands without the records service — the mutating door was registered-and-refusing")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodBlueprintPropose, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = recordsListingStub{}
	if !tbl.RegisterBlueprintPropose() {
		t.Fatal("RegisterBlueprintPropose refused a wired seam")
	}
	got, ok := tbl.Lookup(MethodBlueprintPropose)
	if !ok {
		t.Fatal("blueprint.propose did not register with the service wired")
	}
	if !got.RequiresInitialize {
		t.Error("blueprint.propose is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)")
	}
	if got.Debug {
		t.Error("blueprint.propose is Debug — product surface (PM10)")
	}
	// Idempotent: a second call is a no-op, not a second door.
	if !tbl.RegisterBlueprintPropose() {
		t.Error("RegisterBlueprintPropose is not idempotent over the same wired seam")
	}
}

// TestBlueprintProposeDispatchesThroughDo pins the ONE implementation:
// the socket twin dispatches into RecordsAPI.Do with the catalog tool
// name blueprint_propose, so the socket door and the tool door answer
// through the same function — a handler here that answered for itself
// would be the second door PM9/BP9 forbids.
func TestBlueprintProposeDispatchesThroughDo(t *testing.T) {
	rec := &recordNameSpy{}
	tbl := newTestTable(t)
	tbl.Records = rec
	if !tbl.RegisterBlueprintPropose() {
		t.Fatal("RegisterBlueprintPropose refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodBlueprintPropose, json.RawMessage(`{"name":"x","content":"id: x\n"}`)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if rec.last != "blueprint_propose" {
		t.Errorf("Do saw tool %q, want the catalog name blueprint_propose — the socket door went around the catalog", rec.last)
	}
}

// TestBlueprintProposeParamsDecodeThroughTheRegisteredShape round-trips
// the documented params type the conformance fixture carries: name,
// content, create_only, note — and nothing else has a home (the strict
// decode that refuses a smuggled actor lives in the service; this test
// only pins that the WIRE shape is the fixture's shape).
func TestBlueprintProposeParamsDecodeThroughTheRegisteredShape(t *testing.T) {
	raw, err := json.Marshal(BlueprintProposeParams{
		Name: "review-loop", Content: "id: review-loop\n", CreateOnly: true, Note: "whole source",
	})
	if err != nil {
		t.Fatal(err)
	}
	var back BlueprintProposeParams
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != "review-loop" || !back.CreateOnly || back.Note != "whole source" || back.Content != "id: review-loop\n" {
		t.Errorf("round trip lost a field: %+v", back)
	}
	if string(raw) != `{"name":"review-loop","content":"id: review-loop\n","create_only":true,"note":"whole source"}` {
		t.Errorf("wire shape drifted from the fixture's: %s", raw)
	}
}
