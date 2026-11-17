package daemon

// S55 T7 — the daemon's budget boundary, end to end (E1, E2, E3) and the
// in-process `loop.raise` op in both shapes.
//
// The decision lives in the pure engine (internal/budget); the store owns
// the rows; these tests own the seam's contract — that the daemon reads the
// right budgets for the right loop, that a hit at the pre-dispatch boundary
// parks the loop without a step row, that a hit in-step suspends the step
// without a question, that the raise lifts the cap first and moves the loop
// in the hit's shape, and that every one of it leaves its audit trail
// (budget_event hit/raised, loop_control_event raise).
//
// The spend the loop-scope budget reads is the one the checkpoints write:
// after each priced model turn the cumulative USD lands on the step row
// (SaveRunningCheckpoint → SetStepCost), and SpendFor sums the loop's step
// rows — so a model that reports a known cost makes itself visible to the
// NEXT iteration's check. costedModel below is the scripted provider that
// reports it; gatedModel's zero-cost turns would leave the floor at zero and
// prove nothing about the price path.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// costedModel plays scripted turns like gatedModel, but each turn reports the
// cost a priced provider reports (Usage beside CostKnown) — the number the
// budget's spend reads off the step row. enter/release hold a turn in flight
// exactly as gatedModel's do, which is how a test creates the window between
// "the spend the budget has not seen yet" and "the spend it must now see".
type costedModel struct {
	mu    sync.Mutex
	turns []costedTurn
	calls int
	// sent records the Messages each call actually carried — the
	// exactly-once evidence at the provider boundary, the gatedModel shape.
	sent map[int][]model.Message
}

type costedTurn struct {
	enter   chan struct{} // closed when the call starts; nil = immediate
	release chan struct{} // waited on before completing; nil = immediate
	deltas  []string
	// toolUses makes the turn a WORKING turn. A turn with no tool use is a
	// finishing turn: the step ends with it (after one tool-free handoff
	// retry, if its text is not a handoff), so a plain-text turn leaves the
	// step with no NEXT iteration — and the in-step check has no iteration
	// left in which to see the spend it is meant to trip on. The read is the
	// work that keeps the step's loop open.
	toolUses []model.Block
	usage    model.Usage
	costUSD  float64 // CostKnown; a turn without a price reports nothing
}

// readUse is the `read notes.txt` tool use a working costed turn carries (the
// fixture writes the file; the read-only agent is granted and allowed the read).
func readUse(id string) model.Block {
	return model.Block{Kind: model.BlockToolUse, ID: id, Name: "read",
		Input: json.RawMessage(`{"path":"notes.txt"}`)}
}

func (m *costedModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *costedModel) snapshot(idx int, msgs []model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = map[int][]model.Message{}
	}
	m.sent[idx] = append([]model.Message(nil), msgs...)
}

func (m *costedModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	m.mu.Lock()
	idx := m.calls
	m.calls++
	var turn costedTurn
	if idx < len(m.turns) {
		turn = m.turns[idx]
	} else {
		turn = costedTurn{deltas: []string{okJSON}}
	}
	m.mu.Unlock()
	m.snapshot(idx, req.Messages)

	if turn.enter != nil {
		close(turn.enter)
	}
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if turn.release != nil {
			select {
			case <-turn.release:
			case <-ctx.Done():
				return
			}
		}
		for _, text := range turn.deltas {
			select {
			case deltas <- model.Delta{Kind: model.DeltaText, Text: text}:
			case <-ctx.Done():
				return
			}
		}
		for i := range turn.toolUses {
			block := turn.toolUses[i]
			select {
			case deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}:
			case <-ctx.Done():
				return
			}
		}
		result := model.StreamResult{Attempts: 1, Usage: turn.usage}
		if turn.costUSD > 0 {
			result.Cost = model.Cost{State: model.CostKnown, USD: turn.costUSD}
		}
		select {
		case results <- result:
		case <-ctx.Done():
		}
	}()
	return deltas, results, errs
}

// costedFixture builds the same real-daemon-over-real-store fixture
// gatedFixture builds, with a costed model in place of the gated one.
func costedFixture(t *testing.T, turns []costedTurn) (*streamFixture, *costedModel) {
	t.Helper()
	s := recoveryStore(t)
	root := t.TempDir()
	// The working turns read notes.txt; the file must exist for the read to
	// do work, and the read-only agent needs the allow rule the steer tests
	// seed (without it the gate denies the read and the step suspends on a
	// question, not on the cap the test is proving).
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("budget notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedRules(t, s, time.Now().UTC(),
		toolRulePolicy("allow-read-for-read-only", "read-only", "read", policy.Allow))
	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	m := &costedModel{turns: turns}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: m, Logger: w, Notify: w.notify,
		// S55 T8: the budget's human doors stamp the actor from the
		// CONNECTION (the daemon's ActorFor seam, the kernel's peer
		// credential). The T7 tests never call a budget door, so this wiring
		// changes nothing for them; the T8 door tests set w.conns with the
		// peer they want stamped and read it back in the audit.
		Connections: func() []localrpc.ConnInfo { return w.conns },
	})
	t.Cleanup(func() { d.Table.WaitContinuations() })
	return &streamFixture{d: d, w: w, s: s, root: root, route: route}, m
}

// capFloat is the *float64 a budget cap needs (nil would be NO cap — P31's
// two empties; a zero cap is a real cap that sits at the floor).
func capFloat(v float64) *float64 { return &v }

// makeLoopBudget creates a loop-scope lifetime USD budget over the loop and
// returns its id. The loop row must exist (the subject-exists rule), so this
// runs after the start, inside the windows the tests hold open.
func makeLoopBudget(t *testing.T, s *store.Store, loopID string, cap float64) string {
	t.Helper()
	b, ref, err := s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, store.NewBudgetIn{
		Scope: "loop", Subject: loopID, Window: "lifetime", USD: capFloat(cap),
	})
	if err != nil {
		t.Fatalf("create the loop budget: %v", err)
	}
	if ref.Kind != "" {
		t.Fatalf("the loop budget was refused: %s", ref)
	}
	return b.ID
}

// capOf reads a budget's current USD cap back from the store.
func capOf(t *testing.T, s *store.Store, budgetID string) float64 {
	t.Helper()
	b, err := s.GetBudget(budgetID)
	if err != nil {
		t.Fatalf("read the budget: %v", err)
	}
	if b.USD == nil {
		t.Fatalf("budget %s lost its USD cap", budgetID)
	}
	return *b.USD
}

// kindsOfBudgetEvents returns the audit's kinds in order for one budget.
func kindsOfBudgetEvents(t *testing.T, s *store.Store, budgetID string) []string {
	t.Helper()
	events, err := s.ListBudgetEvents(budgetID, 0)
	if err != nil {
		t.Fatalf("list the budget events: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// blockedOnCapHit waits for the loop to settle on the cap's block (status
// blocked, lastReason cap-hit) — the state both hit shapes end in. On timeout
// it dumps what the loop actually did (status, pin, wire events) so a stall is
// readable from the failure line.
func blockedOnCapHit(t *testing.T, f *streamFixture, connID, subID, loopID string) loop.RuntimeState {
	t.Helper()
	var st loop.RuntimeState
	deadline := time.Now().Add(15 * time.Second)
	for {
		p, err := f.s.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatal(err)
		}
		var r loop.RuntimeState
		if err := json.Unmarshal([]byte(p.RuntimeJSON), &r); err != nil {
			t.Fatal(err)
		}
		if p.Loop.Status == "blocked" && r.LastReason == "cap-hit" {
			st = r
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the loop to park on the cap — loop status %s, lastReason %q, runtime %s; wire events %v",
				p.Loop.Status, r.LastReason, p.RuntimeJSON, eventKinds(streamEvents(f.w, connID, subID)))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitLoopDone waits for the loop to finish (status done). Unlike
// waitLoopSettled, it does not return for a loop parked waiting: after an E2
// raise the loop is ALREADY waiting (the raise's CAS), and the parked step's
// re-run moves it to done in the background — settling is not finishing.
func waitLoopDone(t *testing.T, f *streamFixture, loopID string) store.PinnedLoop {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		pin, err := f.s.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatalf("read pinned loop: %v", err)
		}
		if pin.Loop.Status == "done" {
			return pin
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s did not finish in time; status %s (runtime %s)", loopID, pin.Loop.Status, pin.RuntimeJSON)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitLoopSettledDiag is waitLoopSettled with a failure that dumps the wire
// view (event kinds plus the pin) — for debugging a stall, not the happy path.
func waitLoopSettledDiag(t *testing.T, f *streamFixture, connID, subID, loopID string) store.PinnedLoop {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		pin, err := f.s.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatalf("read pinned loop: %v", err)
		}
		if pin.Loop.Status != "running" && !f.d.loopRunner.Active(loopID) {
			return pin
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s did not settle in time; status %s active %v; wire events %v; runtime %s",
				loopID, pin.Loop.Status, f.d.loopRunner.Active(loopID),
				eventKinds(streamEvents(f.w, connID, subID)), pin.RuntimeJSON)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestInStepCapSuspendsTheStepAndTheRaiseReRunsItFromTheCheckpoint is the E2
// shape end to end: a loop-scope cap the spend crosses MID-STEP suspends the
// step (pause reason `cap` — not a question: nobody is being asked), parks
// the loop blocked/cap-hit with the hit's numbers on the pin, refuses the
// bare `step.continue` with a pointer at the raise, and then the raise lifts
// the cap first (audit row) and re-runs the parked step through the
// step-continue seam — the loop finishes where the cap stopped it.
func TestInStepCapSuspendsTheStepAndTheRaiseReRunsItFromTheCheckpoint(t *testing.T) {
	enter0, release0 := make(chan struct{}), make(chan struct{})
	f, m := costedFixture(t, []costedTurn{
		{deltas: []string{"pricing the work"}, enter: enter0, release: release0,
			toolUses: []model.Block{readUse("t1")},
			usage:    model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 12},
		{deltas: []string{okJSON}},
	})
	initConn(t, f.d, context.Background(), "c1", "raiser")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "solo"))

	<-enter0 // the first (and only pre-cap) model call is in flight
	step1, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "solo", 0)
	if !ok {
		t.Fatal("no step.started for the in-flight step")
	}
	budgetID := makeLoopBudget(t, f.s, loopID, 10) // the cap the spend will cross
	close(release0)                                // the checkpoint now carries $12 — the next check must see it

	st := blockedOnCapHit(t, f, "c1", sub.SubscriptionID, loopID)

	// The pin carries the hit's numbers, and the step is the suspension.
	if hit := st.CapHit; hit == nil {
		t.Fatalf("the pin parked on the cap carries no hit: %+v", st)
	} else {
		if hit.BudgetID != budgetID || hit.Dimension != "usd" || hit.Cap != 10 || hit.SpendFloor != 12 {
			t.Fatalf("cap hit = %+v, want budget %s usd floor 12 cap 10", hit, budgetID)
		}
		if !hit.Exact {
			t.Fatalf("the hit's floor was fully priced and reads as inexact: %+v", hit)
		}
	}
	row, err := f.s.ReadStep(step1)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "suspended" || row.PauseReason != "cap" {
		t.Fatalf("step row = %s/%q, want suspended/cap", row.Status, row.PauseReason)
	}
	// OD5: a cap is not a question — no question row exists to answer.
	if _, ok, err := f.s.PendingStepQuestion(step1); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("a capped step left a question row — a cap has no one to answer")
	}
	if n := m.callCount(); n != 1 {
		t.Fatalf("the model was called %d times before the cap — the suspension is before the next call, not after", n)
	}

	// The bare continue is refused and names the door that applies.
	raw, _ := json.Marshal(protocol.StepContinueParams{StepID: step1})
	_, err = f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodStepContinue, raw)
	if err == nil {
		t.Fatal("step.continue on a capped step was admitted — it must refuse and name the raise")
	}
	// The refusal is a *protocol.Error: WithWhat copies the registry entry, so
	// errors.Is cannot see the sentinel and Error() renders only the code and
	// message — the specifics ride Data.What (the house assertion pattern).
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrStepNotPaused.Code {
		t.Fatalf("the capped step's continue refused with %v, want ErrStepNotPaused", err)
	}
	if !strings.Contains(pe.Data.What, "loop.raise") {
		t.Fatalf("the capped step's continue refused without naming the raise door: %s", pe.Data.What)
	}

	// The raise: the cap lifts first, then the loop moves in the step's shape.
	res, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, 100, "")
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if refusal != nil {
		t.Fatalf("the raise was refused: %s", refusal)
	}
	if !res.Raised || res.How != "step" || res.Status != "waiting" || res.Budget != budgetID {
		t.Fatalf("raise result = %+v, want raised step/waiting on %s", res, budgetID)
	}
	if got := capOf(t, f.s, budgetID); got != 100 {
		t.Fatalf("the cap read %v after the raise, want 100", got)
	}
	events, err := f.s.ListBudgetEvents(budgetID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawHit, sawRaised bool
	for _, e := range events {
		switch e.Kind {
		case "hit":
			sawHit = true
			if e.Dimension != "usd" || e.SpendFloor == nil || *e.SpendFloor != 12 || e.NewValue == nil || *e.NewValue != 10 ||
				e.ActorKind != "system" || e.Exact == nil || !*e.Exact {
				t.Fatalf("the hit audit row is wrong: %+v", e)
			}
		case "raised":
			sawRaised = true
			if e.OldValue == nil || *e.OldValue != 10 || e.NewValue == nil || *e.NewValue != 100 ||
				e.ActorKind != "user" || e.ActorID != "u1" {
				t.Fatalf("the raised audit row is wrong: %+v", e)
			}
		}
	}
	if !sawHit || !sawRaised {
		t.Fatalf("budget audit = %v, want a hit and a raised row", kindsOfBudgetEvents(t, f.s, budgetID))
	}
	ctrl, err := f.s.ListControlEvents(loopID, 50)
	if err != nil {
		t.Fatal(err)
	}
	sawRaise := false
	for _, e := range ctrl {
		if e.Kind == "raise" {
			sawRaise = true
			wantHow := `"how":"step"`
			wantCap := `"cap":100`
			if !strings.Contains(e.Detail, wantHow) || !strings.Contains(e.Detail, wantCap) {
				t.Fatalf("the raise control event detail = %s, want how step and cap 100", e.Detail)
			}
		}
	}
	if !sawRaise {
		t.Fatalf("no raise control event among %v", ctrl)
	}

	// The parked step re-runs from its checkpoint and the loop finishes.
	waitFor(t, "the re-run's model call", func() bool { return m.callCount() == 2 })
	pin := waitLoopDone(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status = %q after the raise, want done (runtime %s)", pin.Loop.Status, pin.RuntimeJSON)
	}
	row, err = f.s.ReadStep(step1)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "done" {
		t.Fatalf("the raised step read %q, want done", row.Status)
	}
	// The re-run saw the transcript once (exactly-once at the provider
	// boundary): the raised step's second call carried the first turn.
	if n := m.callCount(); n != 2 {
		t.Fatalf("the model was called %d times for the whole run, want 2", n)
	}
	waitFor(t, "loop.finished on the wire", func() bool {
		return eventCount(f, "c1", sub.SubscriptionID, "loop.finished") >= 1
	})
}

// TestPreDispatchCapParksTheLoopAndTheRaiseReDispatchsThroughARunner is the
// E1 shape: the cap is already exhausted BEFORE the second step is dispatched
// (a zero cap sits at the floor — no spend needs seeding). The block must
// happen with NO step row for the undelivered step — the loop parks where it
// stands, its last step still the first, done one. The raise answers
// `runner` (not `step`: nothing is suspended), and the fresh runner
// re-dispatches against the raised cap, where the same dispatch now passes.
func TestPreDispatchCapParksTheLoopAndTheRaiseReDispatchsThroughARunner(t *testing.T) {
	enter0, release0 := make(chan struct{}), make(chan struct{})
	f, m := costedFixture(t, []costedTurn{
		{deltas: []string{"the first step"}, enter: enter0, release: release0,
			usage: model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 5},
		{deltas: []string{okJSON}},
		{deltas: []string{okJSON}},
	})
	initConn(t, f.d, context.Background(), "c1", "raiser")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "one", "two"))

	<-enter0 // lane one's first call is in flight; no budget exists yet
	step1, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "one", 0)
	if !ok {
		t.Fatal("no step.started for lane one")
	}
	// A zero cap: the floor (0 or more) already sits on it. Created while the
	// loop runs, so lane one's dispatch (already made) is not re-judged.
	budgetID := makeLoopBudget(t, f.s, loopID, 0)
	close(release0)

	// Lane one completes; the ladder's next dispatch meets the cap.
	st := blockedOnCapHit(t, f, "c1", sub.SubscriptionID, loopID)
	if hit := st.CapHit; hit == nil {
		t.Fatalf("the pin parked on the cap carries no hit: %+v", st)
	} else if hit.BudgetID != budgetID || hit.Dimension != "usd" || hit.Cap != 0 {
		t.Fatalf("cap hit = %+v, want %s usd at cap 0", hit, budgetID)
	}
	if st.LastStepID != step1 {
		t.Fatalf("lastStepId = %d, want lane one's %d — the undelivered step was never a step", st.LastStepID, step1)
	}
	if got := eventCount(f, "c1", sub.SubscriptionID, "step.started"); got != 1 {
		t.Fatalf("step.started fired %d times, want 1 — a pre-dispatch hit leaves no step row", got)
	}
	if m.callCount() != 2 {
		t.Fatalf("the model was called %d times, want 2 (lane one's two turns, nothing after)", m.callCount())
	}

	// A raise that does not clear the floor is refused, and says why.
	if _, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, st.CapHit.SpendFloor, ""); err != nil {
		t.Fatalf("raise at the floor: %v", err)
	} else if refusal == nil || refusal.Kind != "below-floor" {
		t.Fatalf("raising to the floor itself (%v) was refused as %v, want below-floor", st.CapHit.SpendFloor, refusal)
	}

	// The real raise: the cap lifts, the loop goes back to running, a fresh
	// runner re-dispatches, and the same dispatch that was refused now passes.
	res, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, 100, "")
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if refusal != nil {
		t.Fatalf("the raise was refused: %s", refusal)
	}
	if !res.Raised || res.How != "runner" || res.Status != "running" {
		t.Fatalf("raise result = %+v, want raised runner/running", res)
	}
	pin := waitLoopSettledDiag(t, f, "c1", sub.SubscriptionID, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status = %q after the raise, want done (runtime %s)", pin.Loop.Status, pin.RuntimeJSON)
	}
	if m.callCount() != 3 {
		t.Fatalf("the model was called %d times, want 3 (lane two ran after the raise)", m.callCount())
	}
	if got := eventCount(f, "c1", sub.SubscriptionID, "step.started"); got != 2 {
		t.Fatalf("step.started fired %d times, want 2 after the re-dispatch", got)
	}
	if got := capOf(t, f.s, budgetID); got != 100 {
		t.Fatalf("the cap read %v after the raise, want 100", got)
	}

	// A finished loop has no cap left to raise: the refusal names the state.
	if _, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, 200, ""); err != nil {
		t.Fatalf("raise on the finished loop: %v", err)
	} else if refusal == nil || refusal.Kind != "terminal" {
		t.Fatalf("raising a done loop was refused as %v, want terminal", refusal)
	}
}

// TestRaiseWithoutACapRecordRefusesAndPointsAtResume simulates the raise's
// crash window: the E2 raise's CAS committed (the loop moved waiting/cap-hit)
// and the process died before the kick — the pin then carries the cap state
// but NO cap record (the CAS consumed it). A raise cannot lift a budget it
// cannot name, so it refuses with `no-cap` and points at the resume door,
// which admits exactly this state.
func TestRaiseWithoutACapRecordRefusesAndPointsAtResume(t *testing.T) {
	f, _ := costedFixture(t, []costedTurn{{deltas: []string{okJSON}}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "solo"))
	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("setup loop status = %q, want done", pin.Loop.Status)
	}
	// The crash window, written the way a live one would be read: status
	// waiting, lastReason cap-hit, no capHit on the pin.
	if err := f.s.SavePinnedRuntime(loopID, "waiting",
		`{"status":"waiting","lastReason":"cap-hit"}`); err != nil {
		t.Fatalf("write the crash-window pin: %v", err)
	}
	res, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, 100, "")
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if refusal == nil || refusal.Kind != "no-cap" {
		t.Fatalf("raise on the cap-less pin = %+v / %v, want the no-cap refusal", res, refusal)
	}
	if !strings.Contains(refusal.Detail, "resume") {
		t.Fatalf("the no-cap refusal must point at the resume door: %s", refusal.Detail)
	}
	if res.Raised {
		t.Fatalf("the refusal reported a raise that did not happen: %+v", res)
	}
}

// TestTheSeamReReadsTheBudgetBetweenChecks is N8: the seam caches nothing,
// because a floor read at one check is a stale floor at the next. The same
// in-step spend that passed under the cap the check-before saw (5 < 10)
// trips the cap the check-after sees (8 >= 4) — the lowering happened
// BETWEEN the checks, and only a seam that re-reads on every check can make
// the second one honest.
func TestTheSeamReReadsTheBudgetBetweenChecks(t *testing.T) {
	enter0, release0 := make(chan struct{}), make(chan struct{})
	enter1, release1 := make(chan struct{}), make(chan struct{})
	f, m := costedFixture(t, []costedTurn{
		{deltas: []string{"turn one"}, enter: enter0, release: release0,
			toolUses: []model.Block{readUse("t1")},
			usage:    model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 5},
		{deltas: []string{"turn two"}, enter: enter1, release: release1,
			toolUses: []model.Block{readUse("t2")},
			usage:    model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 3},
		{deltas: []string{okJSON}},
	})
	initConn(t, f.d, context.Background(), "c1", "raiser")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "solo"))

	<-enter0 // the first check (no budget) already passed; install the cap now
	budgetID := makeLoopBudget(t, f.s, loopID, 10)
	close(release0) // checkpoint $5

	// The second check reads 5 < 10 and lets turn two run.
	<-enter1
	// The lowering: between the second and the third check.
	if _, ref, err := f.s.UpdateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, budgetID,
		store.NewBudgetIn{USD: capFloat(4)}); err != nil {
		t.Fatalf("lower the cap: %v", err)
	} else if ref.Kind != "" {
		t.Fatalf("the cap was lowered and the update was refused: %s", ref)
	}
	close(release1) // checkpoint $8

	st := blockedOnCapHit(t, f, "c1", sub.SubscriptionID, loopID)
	if hit := st.CapHit; hit == nil {
		t.Fatalf("the pin parked on the cap carries no hit: %+v", st)
	} else {
		if hit.SpendFloor != 8 || hit.Cap != 4 {
			t.Fatalf("cap hit = floor %v cap %v, want floor 8 cap 4 — the check must read the LOWERED cap", hit.SpendFloor, hit.Cap)
		}
	}
	if n := m.callCount(); n != 2 {
		t.Fatalf("the model was called %d times, want 2 — the suspension is before the third call", n)
	}

	// The raise re-runs the parked step from its checkpoint; the re-run's own
	// check reads 8 < 100 and passes.
	res, refusal, err := f.d.RaiseLoop(context.Background(), store.Actor{Kind: "user", ID: "u1"}, loopID, 100, "")
	if err != nil || refusal != nil {
		t.Fatalf("raise: %v / %v", err, refusal)
	}
	if res.How != "step" {
		t.Fatalf("raise how = %q, want step", res.How)
	}
	pin := waitLoopDone(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status = %q after the raise, want done (runtime %s)", pin.Loop.Status, pin.RuntimeJSON)
	}
	if n := m.callCount(); n != 3 {
		t.Fatalf("the model was called %d times, want 3 (the re-run's one turn)", n)
	}
}

// TestStartRefusesUnderAnExhaustedWorkspaceBudget is E3: the only budget
// check a loop that does not exist yet can trip is one over its GROUND — the
// workspace (or epic/component) that would contain it. The refusal must
// happen before any row exists (the nothing-half-written rule), name the hit
// with its sentence and its remedy, and leave no loop behind. Remove the
// cap and the same start goes through.
func TestStartRefusesUnderAnExhaustedWorkspaceBudget(t *testing.T) {
	f, _ := costedFixture(t, nil)

	// A zero workspace cap: the floor (0) already sits on it.
	zero := 0.0
	b, ref, err := f.s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, store.NewBudgetIn{
		Scope: "workspace", Subject: "workspace", Window: "lifetime", USD: &zero,
	})
	if err != nil {
		t.Fatalf("create the workspace budget: %v", err)
	}
	if ref.Kind != "" {
		t.Fatalf("the workspace budget was refused: %s", ref)
	}

	_, err = f.d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: writeStreamBlueprint(t, "read-only", "solo"), Goal: "under the cap",
		WorkspaceRoot: f.root, Route: f.route.ID,
	})
	if err == nil {
		t.Fatal("the start went through under an exhausted workspace cap — E3 refused nothing")
	}
	// House pattern: the refusal is a WithWhat COPY of the sentinel, so
	// errors.Is cannot see it; the code is compared, and the what names
	// the hit (dimension) and its remedy.
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrLoopControlRefused.Code {
		t.Fatalf("the start refusal is %v, want ErrLoopControlRefused", err)
	}
	if !strings.Contains(pe.Data.What, "usd") || !strings.Contains(pe.Data.What, "raise it (aeon budget set) or start later") {
		t.Fatalf("the refusal must name the hit and its remedy, got: %q", pe.Data.What)
	}
	// The nothing-half-written rule: no loop row was created.
	loops, err := f.s.ListLoops(store.ComponentScope{ComponentRoot: f.root})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 0 {
		t.Fatalf("the refused start left %d loop rows: %+v", len(loops), loops)
	}
	// The hit is audited, with the empty loop id (there is no loop).
	events, err := f.s.ListBudgetEvents(b.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	sawHit := false
	for _, e := range events {
		if e.Kind == "hit" {
			sawHit = true
			if e.LoopID != "" || e.Dimension != "usd" {
				t.Fatalf("the start's hit audit row is wrong: %+v", e)
			}
		}
	}
	if !sawHit {
		t.Fatalf("budget audit = %v, want a hit row", kindsOfBudgetEvents(t, f.s, b.ID))
	}

	// Clear the cap and the same start goes through.
	if err := f.s.RemoveBudget(store.BudgetActor{Kind: "user", ID: "u1"}, b.ID); err != nil {
		t.Fatalf("remove the workspace budget: %v", err)
	}
	res, err := f.d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: writeStreamBlueprint(t, "read-only", "solo"), Goal: "under the cap",
		WorkspaceRoot: f.root, Route: f.route.ID,
	})
	if err != nil {
		t.Fatalf("the start still refused after the cap was removed: %v", err)
	}
	loopID, _ := res.(map[string]any)["id"].(string)
	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("the admitted start settled at %q, want done", pin.Loop.Status)
	}
}

// TestQueueReasonReadsTheEngineShape pins the one T7 change to the reactor's
// reason read: the loop engine speaks its blocked reason as a STRUCT
// (*loop.BlockedReason — the kind and the detail), and a string is the flat
// form. A blocked loop whose reason is read as empty raises a queue question
// that says "blocked" without saying why — the plausible wrong answer the
// question exists to avoid.
func TestQueueReasonReadsTheEngineShape(t *testing.T) {
	if got := queueReason(map[string]any{"reason": &loop.BlockedReason{Kind: "cap-hit", Detail: "the cap was reached"}}); got != "the cap was reached" {
		t.Fatalf("the struct reason read as %q, want its Detail", got)
	}
	if got := queueReason(map[string]any{"reason": "a flat reason"}); got != "a flat reason" {
		t.Fatalf("the flat reason read as %q", got)
	}
	if got := queueReason(map[string]any{}); got != "" {
		t.Fatalf("a reasonless event read as %q, want empty", got)
	}
	if got := queueReason(map[string]any{"reason": 42}); got != "" {
		t.Fatalf("an unrecognised reason shape read as %q, want empty", got)
	}
}

// TestBlockedQueueCarriesTheCapReason is the queue's side of the cap: a
// queue item whose loop blocks (on the cap, or on anything — the queue never
// diagnoses, CG2) halts the queue with a gate question that carries the
// blocked reason's DETAIL — a cap that halted a queue says which cap, at
// which floor, against which number.
func TestBlockedQueueCarriesTheCapReason(t *testing.T) {
	f, _ := costedFixture(t, nil)

	// The queue's two pointers need their targets to exist: the component
	// row (queue_item.component_root's FK) and the loop row the item will
	// be stamped with (the (component_root, loop_slug) FK) — the gate
	// question the block raises rides that loop's slug, so it must be a
	// real one, not a name.
	if _, err := f.s.CreateComponent(store.NewComponent{Name: "q", Slug: "q", Root: f.root}); err != nil {
		t.Fatalf("seed the component: %v", err)
	}
	slug := "qloop-slug"
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"},
		"dev-loop", 1, queueBlueprintYAML, "{}"); err != nil {
		t.Fatalf("seed the pinned loop: %v", err)
	}

	// An item attached to a loop slug, in the only state that may block:
	// running (CG1's table: running → blocked, and nothing else reaches it).
	item, err := f.s.AppendQueueItem(store.NewQueueItemRow{
		ComponentRoot: f.root, WorkspaceRoot: f.root, Blueprint: "read-only",
		Status: string(queue.StatusPending), Goal: "the queued thing",
	})
	if err != nil {
		t.Fatalf("append the queue item: %v", err)
	}
	q := queue.New(f.s)
	goal := item.Goal
	for _, to := range []queue.Status{queue.StatusReady, queue.StatusRunning} {
		it, ok, err := q.Show(item.ID)
		if err != nil || !ok {
			t.Fatalf("setup re-read of the item: %v / %v", err, ok)
		}
		if _, r := q.Transition(it, to); r != nil {
			t.Fatalf("setup transition to %s: %s", to, r.Reason)
		}
	}
	if ok, err := f.s.SetQueueItemLoop(item.ID, f.root, slug); err != nil || !ok {
		t.Fatalf("attach the item to the loop slug: %v / %v", err, ok)
	}

	detail := "over the usd cap — floor 12 against cap 10"
	f.d.handleQueueTerminal(context.Background(), protocol.KindLoopBlocked, f.root, slug, "blocked", detail, "cap-hit")

	it, ok, err := q.Show(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != queue.StatusBlocked {
		t.Fatalf("queue item status = %s, want blocked", it.Status)
	}
	question, ok, err := f.s.LatestLoopGateQuestion(f.root, slug)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the blocked queue raised no gate question")
	}
	if !strings.Contains(question.Body, detail) {
		t.Fatalf("the gate question dropped the blocked reason:\n%s", question.Body)
	}
	if !strings.Contains(question.Body, goal) {
		t.Fatalf("the gate question does not name the item:\n%s", question.Body)
	}
}
