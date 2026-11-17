package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// S55 T10 — the seven catalog arms over the daemon. The reads reach the SAME
// seam methods the socket's doors call (one implementation, pinned field for
// field), and the one write is the inert stop request: a row a person sees,
// never a halt. The test daemon is a LITERAL with the store and the three
// plane seams wired — not a full daemon, because a full daemon's live
// loopRunner would START the running loops these arms read, and the test
// would race the runner for the rows it pins.

// t10Daemon builds the store-bearing test daemon with the three S55 seams
// wired and one initialized connection ("c1") for the socket half of the
// comparisons.
func t10Daemon(t *testing.T) (*Daemon, *wire, *store.Store, *RecordsService, string) {
	t.Helper()
	s, root := t14Env(t)
	w := &wire{}
	d := &Daemon{
		store:  s,
		Table:  protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog()),
		log:    w,
		notify: w.notify,
	}
	if d.Table.LoopControl = d.newLoopControlService(); d.Table.LoopControl == nil {
		t.Fatal("the loop-control seam did not wire over a store")
	}
	if !d.Table.RegisterLoopControl() {
		t.Fatal("RegisterLoopControl refused a wired seam")
	}
	if d.Table.BudgetStore = d.newBudgetService(); d.Table.BudgetStore == nil {
		t.Fatal("the budget seam did not wire over a store")
	}
	if !d.Table.RegisterBudget() {
		t.Fatal("RegisterBudget refused a wired seam")
	}
	if d.Table.Watchdog = d.newWatchdogService(); d.Table.Watchdog == nil {
		t.Fatal("the watchdog seam did not wire over a store")
	}
	if !d.Table.RegisterWatchdog() {
		t.Fatal("RegisterWatchdog refused a wired seam")
	}
	rs := NewRecordsService(s)
	rs.d = d
	initConn(t, d, context.Background(), "c1", "t10-client")
	return d, w, s, rs, root
}

// t10Loop creates a RUNNING pinned loop at the test root and one running
// step in it (the identity the arms stamp from), returning the loop id and
// the step id.
func t10Loop(t *testing.T, s *store.Store, root, slug, agent string) (string, int64) {
	t.Helper()
	id := wdLoop(t, s, root, slug, "running", nil)
	step := startLaneStep(t, s, slug, root, agent)
	return id, step
}

func t10params(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// sameJSON is the "field for field" comparison: both doors' answers, encoded
// by the ONE encoder, byte for byte.
func sameJSON(t *testing.T, label string, a, b any) {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("%s: the tool's answer will not encode: %v", label, err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("%s: the socket's answer will not encode: %v", label, err)
	}
	if !bytes.Equal(ab, bb) {
		t.Fatalf("%s: the two doors answered differently — a second implementation has appeared:\ntool:   %s\nsocket: %s", label, ab, bb)
	}
}

// TestToolReadsMatchSocketReads is the "one implementation" pin: each of the
// six reads answers through the SAME seam the socket's door calls, so the
// tool and its twin agree field for field — and the person's half of all
// seven is refused to the socket's door by name (the loop_list precedent).
func TestToolReadsMatchSocketReads(t *testing.T) {
	d, _, s, rs, root := t10Daemon(t)
	loopID, step := t10Loop(t, s, root, "reads", "worker")
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	// The test data the reads must agree about: one budget row on the loop,
	// one watchdog rule on the workspace.
	capUSD := 10.0
	b, rf, err := s.CreateBudget(store.BudgetActor{Kind: "user", ID: "operator"}, store.NewBudgetIn{
		Scope: "loop", Subject: loopID, Window: "lifetime", USD: &capUSD,
	})
	if err != nil || rf.Kind != "" {
		t.Fatalf("budget row: %v (%s)", err, rf.Detail)
	}
	if _, err := s.WatchdogSet(store.WatchdogSetIn{
		Scope: "workspace", SubjectID: "workspace", Signal: "no-activity",
		Threshold: 900000, Action: "notify", Origin: "user", ActorKind: "user", ActorID: "test",
	}); err != nil {
		t.Fatalf("watchdog rule: %v", err)
	}

	// (1) loop_control_state ⇄ loop.control — the socket names the loop, the
	// tool stamps it from the step; same seam, same answer.
	out, f, err := rs.Do(ctx, by, "loop_control_state", nil)
	if f != nil || err != nil {
		t.Fatalf("loop_control_state: %v %v", f, err)
	}
	res, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopControl, t10params(t, protocol.LoopControlParams{ID: loopID}))
	if err != nil {
		t.Fatalf("loop.control: %v", err)
	}
	sameJSON(t, "loop_control_state", out, res)

	// (2) budget_list ⇄ budget.list (the socket wraps the views in its result
	// envelope; the tool returns the views — compare the views).
	out, f, err = rs.Do(ctx, by, "budget_list", nil)
	if f != nil || err != nil {
		t.Fatalf("budget_list: %v %v", f, err)
	}
	res, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodBudgetList, t10params(t, protocol.BudgetListParams{}))
	if err != nil {
		t.Fatalf("budget.list: %v", err)
	}
	sameJSON(t, "budget_list", out, res.(protocol.BudgetListResult).Budgets)

	// (3) budget_show ⇄ budget.show.
	out, f, err = rs.Do(ctx, by, "budget_show", json.RawMessage(fmt.Sprintf(`{"budget_id":%q}`, b.ID)))
	if f != nil || err != nil {
		t.Fatalf("budget_show: %v %v", f, err)
	}
	res, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodBudgetShow, t10params(t, protocol.BudgetShowParams{ID: b.ID}))
	if err != nil {
		t.Fatalf("budget.show: %v", err)
	}
	sameJSON(t, "budget_show", out, res)

	// (4) budget_effective ⇄ budget.effective — the loop is stamped from the
	// step on the tool side, named on the socket side; same tiers.
	out, f, err = rs.Do(ctx, by, "budget_effective", nil)
	if f != nil || err != nil {
		t.Fatalf("budget_effective: %v %v", f, err)
	}
	res, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodBudgetEffective, t10params(t, protocol.BudgetEffectiveParams{LoopID: loopID}))
	if err != nil {
		t.Fatalf("budget.effective: %v", err)
	}
	sameJSON(t, "budget_effective", out, res)

	// (5) budget_spend ⇄ spend.report (the socket normalizes a nil bucket
	// list to [] — normalize both sides before the byte compare). The window
	// is EXPLICIT and shared by both doors: an omitted `until` defaults to
	// each door's own `now`, so two unpinned calls would differ by a
	// millisecond of clock, not of implementation — the byte compare must
	// measure drift only.
	since := "2020-01-01T00:00:00Z"
	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	out, f, err = rs.Do(ctx, by, "budget_spend", json.RawMessage(fmt.Sprintf(`{"group_by":"day","since":%q,"until":%q}`, since, until)))
	if f != nil || err != nil {
		t.Fatalf("budget_spend: %v %v", f, err)
	}
	res, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodSpendReport, t10params(t, protocol.SpendReportParams{GroupBy: "day", Since: since, Until: until}))
	if err != nil {
		t.Fatalf("spend.report: %v", err)
	}
	ta := out.(protocol.SpendReportResult)
	tb := res.(protocol.SpendReportResult)
	if ta.Buckets == nil {
		ta.Buckets = []protocol.SpendReportBucket{}
	}
	if tb.Buckets == nil {
		tb.Buckets = []protocol.SpendReportBucket{}
	}
	sameJSON(t, "budget_spend", ta, tb)

	// (6) watchdog_list ⇄ watchdog.list.
	out, f, err = rs.Do(ctx, by, "watchdog_list", nil)
	if f != nil || err != nil {
		t.Fatalf("watchdog_list: %v %v", f, err)
	}
	res, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodWatchdogList, t10params(t, protocol.WatchdogListParams{}))
	if err != nil {
		t.Fatalf("watchdog.list: %v", err)
	}
	sameJSON(t, "watchdog_list", out, res.(protocol.WatchdogListResult).Rules)

	// The person's half of all seven is the socket's door, by name.
	for _, name := range []string{"loop_control_state", "loop_request_stop", "budget_list", "budget_show", "budget_effective", "budget_spend", "watchdog_list"} {
		_, f, err := rs.Do(context.Background(), Actor{Kind: "user", ID: "operator"}, name, nil)
		if err != nil {
			t.Fatalf("person door %s: %v", name, err)
		}
		if f == nil {
			t.Errorf("person door %s answered — the person's half of these is the socket's door, by name", name)
			continue
		}
		if !strings.Contains(f.Sentence, "the socket already speaks") {
			t.Errorf("person door %s refusal does not name the socket door: %s", name, f.Sentence)
		}
	}
}

// t10Snapshot is the inertness pin: the loop row, its pin (the runtime JSON
// included) and its steps, encoded — the before and after must be byte
// identical for a request that halts nothing (LC15).
func t10Snapshot(t *testing.T, s *store.Store, sc store.Scope, loopID string) []byte {
	t.Helper()
	loopRow, err := s.GetLoop(sc)
	if err != nil {
		t.Fatalf("snapshot: the loop row: %v", err)
	}
	pin, err := s.ReadPinnedLoop(loopID)
	if err != nil {
		t.Fatalf("snapshot: the pin: %v", err)
	}
	steps, err := s.ListSteps(sc)
	if err != nil {
		t.Fatalf("snapshot: the steps: %v", err)
	}
	b, err := json.Marshal(struct {
		Loop  store.Loop       `json:"loop"`
		Pin   store.PinnedLoop `json:"pin"`
		Steps []store.Step     `json:"steps"`
	}{loopRow, pin, steps})
	if err != nil {
		t.Fatalf("snapshot: encode: %v", err)
	}
	return b
}

// TestToolRequestStopIsInert is LC15 made executable through the CATALOG
// arm (the spec's TestRequestStopIsInert name stands in
// loop_control_inert_test.go for the seam the socket door reaches; this is
// the same property pinned at the tool door, so the two cannot drift apart):
// the row commits, the row is stamped from the STEP (never the input), and
// the loop — status, pin, steps — is byte-identical before and after.
// Nothing halted.
func TestToolRequestStopIsInert(t *testing.T) {
	_, _, s, rs, root := t10Daemon(t)
	loopID, step := t10Loop(t, s, root, "inert", "worker")
	sc := store.Scope{LoopSlug: "inert", ComponentRoot: root}
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	before := t10Snapshot(t, s, sc, loopID)
	out, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(`{"reason":"it is burning through the budget"}`))
	if f != nil || err != nil {
		t.Fatalf("loop_request_stop: %v %v", f, err)
	}
	rr, ok := out.(protocol.RequestStopResult)
	if !ok || rr.Status != "open" || rr.RequestID == 0 {
		t.Fatalf("loop_request_stop answered %v — want an open row with an id", out)
	}
	if after := t10Snapshot(t, s, sc, loopID); !bytes.Equal(before, after) {
		t.Fatalf("the request touched the loop — inertness (LC15) means the rows are byte-identical:\nbefore: %s\nafter:  %s", before, after)
	}

	// The row is stamped from the step, never from the input.
	reqs, err := s.ListControlRequests(store.ControlRequestFilter{LoopID: loopID})
	if err != nil {
		t.Fatalf("the request desk: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("the desk has %d rows, want 1", len(reqs))
	}
	r := reqs[0]
	if r.ByAgent != "worker" || r.ByStepID == nil || *r.ByStepID != step {
		t.Fatalf("the row is stamped %s / %v — want the step's agent worker / step %d (OL3)", r.ByAgent, r.ByStepID, step)
	}
	if r.Reason != "it is burning through the budget" || r.Status != "open" {
		t.Fatalf("the row = %+v — want the filed reason, open", r)
	}
}

// TestRequestStopOwnLoopOnly is MC2's write half: another loop's id is
// refused naming it, nothing is filed against it, and the omitted id asks
// about the step's own loop.
func TestRequestStopOwnLoopOnly(t *testing.T) {
	_, _, s, rs, root := t10Daemon(t)
	ownID, step := t10Loop(t, s, root, "mine", "worker")
	otherID := wdLoop(t, s, root, "theirs", "running", nil)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	_, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(fmt.Sprintf(`{"loop_id":%q,"reason":"stop the other one"}`, otherID)))
	if err != nil {
		t.Fatalf("loop_request_stop (other loop): %v", err)
	}
	if f == nil {
		t.Fatal("a stop request against another loop's id was accepted — MC2 says own loop only")
	}
	if !strings.Contains(f.Sentence, otherID) {
		t.Errorf("the refusal does not name the other loop %s: %s", otherID, f.Sentence)
	}
	reqs, err := s.ListControlRequests(store.ControlRequestFilter{LoopID: otherID})
	if err != nil {
		t.Fatalf("the other loop's desk: %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("the other loop's desk has %d rows, want none — nothing was filed", len(reqs))
	}

	// The omission path: no loop_id asks about the own loop.
	out, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(`{"reason":"this loop is stuck"}`))
	if f != nil || err != nil {
		t.Fatalf("loop_request_stop (own, omitted id): %v %v", f, err)
	}
	reqs, err = s.ListControlRequests(store.ControlRequestFilter{LoopID: ownID})
	if err != nil || len(reqs) != 1 {
		t.Fatalf("the own desk: %v (%d rows), want the one filed row", err, len(reqs))
	}
	if out.(protocol.RequestStopResult).RequestID != reqs[0].ID {
		t.Fatalf("the result names a request the desk does not hold")
	}
}

// TestRequestStopLimits is the counted desk (LC15): the sixth open request
// per loop and the twenty-first per agent are refused, each naming the limit
// and the door that shows the open ones.
func TestRequestStopLimits(t *testing.T) {
	t.Run("the sixth per loop is refused naming the limit", func(t *testing.T) {
		_, _, s, rs, root := t10Daemon(t)
		_, step := t10Loop(t, s, root, "capped", "worker")
		ctx := stepctx.WithStepID(context.Background(), step)
		by := Actor{Kind: "agent", ID: "worker"}
		for i := 1; i <= store.ControlRequestOpenPerLoop; i++ {
			if _, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(fmt.Sprintf(`{"reason":"ask %d"}`, i))); f != nil || err != nil {
				t.Fatalf("request %d: %v %v", i, f, err)
			}
		}
		_, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(`{"reason":"the sixth"}`))
		if err != nil {
			t.Fatalf("the sixth request: %v", err)
		}
		if f == nil {
			t.Fatal("the sixth open request per loop was filed — the desk is counted (LC15)")
		}
		if !strings.Contains(f.Sentence, "the limit is 5") {
			t.Errorf("the refusal does not name the limit: %s", f.Sentence)
		}
	})
	t.Run("the twenty-first per agent is refused naming the limit", func(t *testing.T) {
		_, _, s, rs, root := t10Daemon(t)
		ownID, step := t10Loop(t, s, root, "mine", "worker")
		// Twenty open requests already on the agent's desk, spread over four
		// other loops (five each — the per-loop cap is what spreads them, and
		// the own loop stays empty so the AGENT count is what trips).
		for i := 1; i <= 4; i++ {
			slug := fmt.Sprintf("other%d", i)
			otherID := wdLoop(t, s, root, slug, "running", nil)
			otherStep := startLaneStep(t, s, slug, root, "worker")
			for j := 1; j <= store.ControlRequestOpenPerLoop; j++ {
				if _, err := s.AddControlRequest(otherID, "worker", fmt.Sprintf("pre-filed %d/%d", i, j), &otherStep); err != nil {
					t.Fatalf("pre-file %d/%d: %v", i, j, err)
				}
			}
		}
		ctx := stepctx.WithStepID(context.Background(), step)
		by := Actor{Kind: "agent", ID: "worker"}
		_, f, err := rs.Do(ctx, by, "loop_request_stop", json.RawMessage(`{"reason":"the twenty-first"}`))
		if err != nil {
			t.Fatalf("the arm's request: %v", err)
		}
		if f == nil {
			t.Fatal("the twenty-first open request per agent was filed — the desk is counted (LC15)")
		}
		if !strings.Contains(f.Sentence, "the limit is 20") {
			t.Errorf("the refusal does not name the limit: %s", f.Sentence)
		}
		reqs, err := s.ListControlRequests(store.ControlRequestFilter{LoopID: ownID})
		if err != nil {
			t.Fatalf("the own desk: %v", err)
		}
		if len(reqs) != 0 {
			t.Fatalf("the own loop's desk has %d rows, want none — the refusal came before the row", len(reqs))
		}
	})
}

// TestLoopControlStateIsOwnView is MC2's read half: the state read answers
// for the step's OWN loop, a smuggled loop_id is a parse refusal (the schema
// is property-free), and the catalog's schema is empty — there is no field
// a model could type to reach another loop.
func TestLoopControlStateIsOwnView(t *testing.T) {
	_, _, s, rs, root := t10Daemon(t)
	ownID, step := t10Loop(t, s, root, "mine", "worker")
	otherID := wdLoop(t, s, root, "theirs", "running", nil)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	out, f, err := rs.Do(ctx, by, "loop_control_state", nil)
	if f != nil || err != nil {
		t.Fatalf("loop_control_state: %v %v", f, err)
	}
	res := out.(protocol.LoopControlResult)
	if res.LoopID != ownID {
		t.Fatalf("the state read answered for loop %s, want the step's own %s (MC2)", res.LoopID, ownID)
	}

	// A smuggled loop_id has no field to land in: the decode refuses it.
	_, f, err = rs.Do(ctx, by, "loop_control_state", json.RawMessage(fmt.Sprintf(`{"loop_id":%q}`, otherID)))
	if err != nil {
		t.Fatalf("loop_control_state (with a loop_id): %v", err)
	}
	if f == nil {
		t.Fatal("a loop_id smuggled into the state read was answered — the schema is property-free by design (MC2)")
	}
	if !strings.Contains(f.Sentence, "unknown field") {
		t.Errorf("the refusal is not the parse door's: %s", f.Sentence)
	}

	// The catalog half: the state spec's schema has no properties at all.
	var found bool
	for _, sp := range records.Catalog() {
		if sp.Name != "loop_control_state" {
			continue
		}
		found = true
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &schema); err != nil {
			t.Fatalf("the loop_control_state schema does not parse: %v", err)
		}
		if len(schema.Properties) != 0 {
			t.Fatalf("loop_control_state's schema has properties %v — the own loop is stamped, never named", schema.Properties)
		}
	}
	if !found {
		t.Fatal("the catalog has no loop_control_state")
	}
}
