package protocol

// policy_propose_test.go — S45 T7 (PM9): the two propose twins exist as
// socket methods iff the records service stands (V13, the RegisterSeed
// rule), they are gated and non-debug like every write on this table, and
// they dispatch through RecordsAPI.Do — the SAME Do the tools call, so one
// implementation answers both doors. The behaviour of the Do behind them
// (forced source, stamped created_by, cap, inertness) is the daemon's
// records suite (internal/daemon/records_policy_test.go); what lives here
// is the SHAPE of the door.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// recordsListingStub is the minimal RecordsAPI for the method-table tests:
// the handshake listing measures the registration; TestPolicyProposeDis-
// patchesThroughDo proves the dispatch reaches Do with the catalog tool
// name — nothing else in this package needs the records service to answer.
type recordsListingStub struct{}

func (recordsListingStub) Do(ctx context.Context, sess *Session, tool string, params json.RawMessage) (any, string, error) {
	return map[string]any{"tool": tool}, "", nil
}
func (recordsListingStub) DecideGoal(context.Context, *Session, GoalDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (recordsListingStub) DecideBrief(context.Context, *Session, BriefDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (recordsListingStub) DecideTask(context.Context, *Session, TaskDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (recordsListingStub) TaskList(context.Context, *Session, TaskListParams) (TaskListResult, error) {
	return TaskListResult{}, nil
}
func (recordsListingStub) GoalShow(context.Context, *Session, GoalShowParams) (GoalShowResult, string, error) {
	return GoalShowResult{}, "", nil
}

// TestPolicyProposeRegistrationIsConditional is the V13 half: no records
// service, no method — ABSENT (ErrUnknownMethod), never registered-and-
// refusing. These are the catalog's first MUTATING specs, and the service
// is what honours what makes them safe (stamped actor, forced source, the
// per-agent cap), so an unwired runtime does not advertise the door at all.
func TestPolicyProposeRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose registered over a nil records seam — V13 says absent, not refusing")
	}
	for _, m := range []MethodName{MethodPolicyProposeAdd, MethodPolicyProposeRemove} {
		if _, ok := tbl.Lookup(m); ok {
			t.Errorf("%s stands without the records service — the mutating door was registered-and-refusing", m)
		}
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyProposeAdd, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = recordsListingStub{}
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused a wired seam")
	}
	for _, m := range []MethodName{MethodPolicyProposeAdd, MethodPolicyProposeRemove} {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s did not register with the service wired", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — product surface (PM10: Debug is never what keeps a method off the narrow projection)", m)
		}
	}
	// Idempotent: a second call is a no-op, not a second door.
	if !tbl.RegisterPolicyPropose() {
		t.Error("RegisterPolicyPropose is not idempotent over the same wired seam")
	}
}

// TestPolicyProposeDispatchesThroughDo pins the ONE implementation: both
// twins dispatch into RecordsAPI.Do with their catalog tool names, so the
// socket door and the tool door answer through the same function — a
// handler here that answered for itself would be the second door PM9
// forbids.
func TestPolicyProposeDispatchesThroughDo(t *testing.T) {
	rec := &recordNameSpy{}
	tbl := newTestTable(t)
	tbl.Records = rec
	// newTestTable already registered the twins over its stub; replacing
	// the seam keeps the methods (they dispatch through the interface).
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	for _, tc := range []struct {
		method MethodName
		tool   string
	}{
		{MethodPolicyProposeAdd, "policy_propose_add"},
		{MethodPolicyProposeRemove, "policy_propose_remove"},
	} {
		if _, err := tbl.Dispatch(context.Background(), "spy", tc.method, json.RawMessage(`{"id":"x"}`)); err != nil {
			t.Fatalf("%s dispatch: %v", tc.method, err)
		}
		if rec.last != tc.tool {
			t.Errorf("%s dispatched tool %q, want %q — one definition, two doors, through Do", tc.method, rec.last, tc.tool)
		}
	}
}

type recordNameSpy struct{ last string }

func (s *recordNameSpy) Do(ctx context.Context, sess *Session, tool string, params json.RawMessage) (any, string, error) {
	s.last = tool
	return map[string]any{"ok": true}, "", nil
}
func (s *recordNameSpy) DecideGoal(context.Context, *Session, GoalDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (s *recordNameSpy) DecideBrief(context.Context, *Session, BriefDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (s *recordNameSpy) DecideTask(context.Context, *Session, TaskDecideParams) (DecideView, string, error) {
	return DecideView{}, "", nil
}
func (s *recordNameSpy) TaskList(context.Context, *Session, TaskListParams) (TaskListResult, error) {
	return TaskListResult{}, nil
}
func (s *recordNameSpy) GoalShow(context.Context, *Session, GoalShowParams) (GoalShowResult, string, error) {
	return GoalShowResult{}, "", nil
}
