package runtime

// The steer tests (S31 T4 — D3, D5, LW1–LW3, LW6, Q4).
//
// # What this file rules out
//
// The failure D3 exists for is a mid-flight instruction that is EITHER lost
// (the person typed, the model never read it, nothing says so) OR violent (it
// yanks a running call out of the air, or runs a half-answered tool). Every
// test here asserts the third shape: the instruction arrives at the NEXT
// boundary, exactly once, in the reading order a model can reason about —
// and where it lands on top of tool calls, those calls provably did not
// happen (the spy records zero) and each carries its own honest answer.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// queuedSteers plays batches at each drain — the scripted client's shape for
// a queue: drain N gets batch N, later drains see an empty queue (the rows
// the checkpoint delivered are never handed back).
type queuedSteers struct {
	mu      sync.Mutex
	batches [][]Steer
	calls   int
}

func (q *queuedSteers) Pending(context.Context) []Steer {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.calls < len(q.batches) {
		b := q.batches[q.calls]
		q.calls++
		return b
	}
	return nil
}

func steerText(id int64, text string) Steer { return Steer{ID: id, Text: text} }

func countEvents(events []StepEvent, kind string) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func eventsOf(events []StepEvent, kind string) []StepEvent {
	var out []StepEvent
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// lastUserBlocks returns the content of the LAST user message of a request.
func lastUserBlocks(req model.ModelRequest) []model.Block {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == model.RoleUser {
			return req.Messages[i].Content
		}
	}
	return nil
}

func occurrencesOfText(msgs []model.Message, text string) int {
	n := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockText && b.Text == text {
				n++
			}
		}
	}
	return n
}

// ── the boundaries ───────────────────────────────────────────────────────────

func TestSteerDuringATextOnlyTurnArrivesAtTheNextCallOnce(t *testing.T) {
	// LW1 made concrete: the FIRST call is untouched (no steer in it), the
	// instruction reaches the model exactly once, and the turn it would have
	// finished on does NOT finish — the model, now informed, decides.
	steerTextMsg := "actually, summarize rather than report"
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{
			nil,                           // boundary 1: nothing queued yet before the first call
			{steerText(11, steerTextMsg)}, // boundary 2: queued during the call
		}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if client.requestCount() != 2 {
		t.Fatalf("%d calls, want 2: the steered turn must NOT finish the step", client.requestCount())
	}
	if n := occurrencesOfText(client.request(0).Messages, steerTextMsg); n != 0 {
		t.Fatalf("the in-flight call carried the steer (%d times) — LW1 says the call in the air always finishes untouched", n)
	}
	if n := occurrencesOfText(client.request(1).Messages, steerTextMsg); n != 1 {
		t.Fatalf("the steer reached the next request %d times, want exactly 1", n)
	}
	blocks := lastUserBlocks(client.request(1))
	if len(blocks) == 0 || blocks[len(blocks)-1].Kind != model.BlockText ||
		blocks[len(blocks)-1].Text != steerTextMsg {
		t.Fatal("the instruction does not lead the trailing user turn of the next request")
	}
	events := sink.all()
	if n := countEvents(events, EventSteerDelivered); n != 1 {
		t.Fatalf("%d delivered events, want 1", n)
	}
	if e := eventsOf(events, EventSteerDelivered)[0]; e.SteerID != 11 {
		t.Fatalf("delivered event names steer %d, want 11", e.SteerID)
	}
	if n := countEvents(events, EventToolSkipped); n != 0 {
		t.Fatalf("%d skipped events for a tool-free turn", n)
	}
}

func TestSteerWhileTheTurnEndsInToolCallsRunsNoneAndAnswersEach(t *testing.T) {
	// LW2's full shape: two calls, zero run (the spy records none), each
	// answered with the not-run result, the steer's text AFTER the results in
	// the SAME user message, in that reading order.
	msg := "stop reading, just summarize"
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "read", `{"path":"a.md"}`),
			toolUse("t2", "read", `{"path":"b.md"}`),
		}},
		scriptedTurn{Text: okHandoff},
	)
	spy := newSpyTools("read", "write")
	sink := &recordingSink{}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{
			nil,
			{steerText(21, msg)},
		}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if calls := spy.recorded(); len(calls) != 0 {
		t.Fatalf("%d tools ran (%v) — a steered turn dispatches NOTHING", len(calls), calls)
	}
	req := client.request(1)
	blocks := lastUserBlocks(req)
	if len(blocks) != 3 {
		t.Fatalf("trailing user turn carries %d blocks, want 2 results + text", len(blocks))
	}
	for i, want := range []string{"t1", "t2"} {
		b := blocks[i]
		if b.Kind != model.BlockToolResult || b.ToolUseID != want || !b.IsError {
			t.Fatalf("block %d is %v(%s, isError=%v), want an isError tool_result for %s", i, b.Kind, b.ToolUseID, b.IsError, want)
		}
		if !strings.Contains(b.Content, "not run: the person steered before this tool ran") {
			t.Fatalf("result %d does not carry LW2's sentence: %q", i, b.Content)
		}
	}
	if blocks[2].Kind != model.BlockText || blocks[2].Text != msg {
		t.Fatal("the steer's text does not follow the results in the same message")
	}
	if n := occurrencesOfText(req.Messages, msg); n != 1 {
		t.Fatalf("the steer appears %d times in the next request, want exactly 1", n)
	}
	events := sink.all()
	skips := eventsOf(events, EventToolSkipped)
	if len(skips) != 2 {
		t.Fatalf("%d skipped events, want one per call", len(skips))
	}
	if skips[0].Tool != "read" || skips[0].ToolUseID != "t1" || skips[0].Reason != "steered" {
		t.Fatalf("first skip event = %+v", skips[0])
	}
	delivered := eventsOf(events, EventSteerDelivered)
	if len(delivered) != 1 || len(delivered[0].SkippedTools) != 2 {
		t.Fatalf("delivered event = %+v, want steerId + the two skipped tool_use ids", delivered)
	}
}

func TestSteeredQuestionToolDoesNotSuspend(t *testing.T) {
	// LW3: suspension is the model's act, not a trapdoor the person's timing
	// can fall through. The question rides the same not-run closure as any
	// other call, and the step keeps running.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("q1", "question", `{"text":"approve?","options":["yes","no"]}`),
		}},
		scriptedTurn{Text: okHandoff},
	)
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{
			nil,
			{steerText(31, "no, ship it")},
		}},
	})
	if res.Kind == ResultSuspended {
		t.Fatal("a steered question suspended — LW3 says the person's message answers it, not a question row")
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	results := toolResultsFor(client.request(1).Messages, "q1")
	if len(results) != 1 || !results[0].IsError ||
		!strings.Contains(results[0].Content, "not run: the person steered") {
		t.Fatalf("the question call was not closed as not-run: %+v", results)
	}
}

// ── the overrides (D5, LW6) ──────────────────────────────────────────────────

// S18 T8 evolved this pin (O5, N9). It used to assert the route reached the
// NEXT CALL of the SAME step. That is exactly what a provider cannot digest:
// a tool_use born in one format answered in another's. The switch is now
// refused mid-step — the steer's text still lands, the route defers — and
// the LOOP applies it before the next dispatch. The property this test
// always guarded (the in-flight call is never yanked — D5) survives,
// strengthened: NO call of this step carries the new route.
func TestRouteOnlySteerIsDeferredToTheNextStepAndAnnounced(t *testing.T) {
	fast := &model.Route{ID: "fast", Provider: "anthropic", Model: "claude-fast", MaxOutputTokens: 512}
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{nil, {{ID: 41, Route: fast}}}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	for k := range client.turns {
		if got := client.request(k).Route.ID; got == "fast" {
			t.Fatalf("call %d of THIS step carried the steered route — mid-step switches are refused (O5)", k)
		}
	}
	if res.PendingRoute != "fast" {
		t.Fatalf("the step owed the loop its switch and did not: PendingRoute = %q", res.PendingRoute)
	}
	// Transcript unchanged: a route-only steer injects NO text.
	if len(client.request(1).Messages) != len(client.request(0).Messages)+1 {
		t.Fatalf("transcript grew by %d messages, want only the assistant turn",
			len(client.request(1).Messages)-len(client.request(0).Messages))
	}
	changes := eventsOf(sink.all(), EventRouteChanged)
	if len(changes) != 0 {
		t.Fatalf("route.changed fired for a refused switch: %+v", changes)
	}
	refused := eventsOf(sink.all(), EventRouteDeferred)
	if len(refused) != 1 || refused[0].Route != "fast" {
		t.Fatalf("route.deferred = %+v, want one refusal naming the route", refused)
	}
	if !strings.Contains(refused[0].Text, "next step") {
		t.Errorf("the refusal %q does not say the switch applies at the next step (O5)", refused[0].Text)
	}
}

// S18 T8 evolved this pin. LW6 guarded the same pairing from two sides
// (recompile on a mid-step switch); with O5 the mid-step switch no longer
// exists, so the refused pair cannot reach a wire mid-step AT ALL — the
// stronger shape of the same guarantee. The lowering itself remains
// compile's job at the next dispatch, where the step is STARTED on the
// route+tools pair.
func TestSteeringOntoARouteThatRefusesToolsWithReasoningReachesNoWire(t *testing.T) {
	chatHigh := model.Route{ID: "chat-high", Provider: model.ProviderOpenAI,
		Model: "gpt-chat", OpenAIEndpoint: string(model.OpenAIEndpointChat), ReasoningEffort: "high"}
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{nil, {{ID: 51, Route: &chatHigh}}}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	for k := range client.turns {
		req := client.request(k)
		if req.Route.ID == "chat-high" {
			t.Fatalf("call %d reached a wire on the refused pair (tools + chat reasoning) — O5", k)
		}
	}
	if res.PendingRoute != "chat-high" {
		t.Fatalf("PendingRoute = %q, want the switch owed to the loop", res.PendingRoute)
	}
	if res.PendingEffort != "" {
		t.Fatalf("PendingEffort = %q — nobody NAMED an effort; the pair must not carry a frozen dial, "+
			"compile re-derives it at the next dispatch", res.PendingEffort)
	}
}

func TestExplicitEffortFromASteerPassesThroughTheRule(t *testing.T) {
	chatHigh := model.Route{ID: "chat-high", Provider: model.ProviderOpenAI,
		Model: "gpt-chat", OpenAIEndpoint: string(model.OpenAIEndpointChat), ReasoningEffort: "high"}
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{nil, {{ID: 52, Route: &chatHigh, ReasoningEffort: "medium"}}}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	// S18 T8 (O5/N9): with a route in the same steer, the NAMED effort rides
	// the deferral — it belongs to the route that may not speak this step.
	if res.PendingRoute != "chat-high" || res.PendingEffort != "medium" {
		t.Fatalf("deferred pair = (%q, %q), want (chat-high, medium) — the rule's explicit-wins clause "+
			"survives the wait: the name the person typed is what the next dispatch compiles with",
			res.PendingRoute, res.PendingEffort)
	}
	if got := client.request(1).ReasoningEffort; got == "medium" {
		t.Fatal("the named effort spoke on a wire this step — effort deferred with its route")
	}

	// Effort ALONE is the same route's dial, and still applies at the next
	// call: deferral is about the provider, not the person's words.
	client2 := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	res2 := RunLoop(context.Background(), testState(testHandler(), nil), LoopDeps{
		Client: client2, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{nil, {{ID: 53, ReasoningEffort: "medium"}}}},
	})
	if res2.PendingRoute != "" {
		t.Fatalf("an effort-only steer invented a route switch: %q", res2.PendingRoute)
	}
	if got := client2.request(1).ReasoningEffort; got != "medium" {
		t.Fatalf("an effort the person NAMED = %q, want it kept on the next call (the rule's explicit-wins clause)", got)
	}
}

// ── the queue's manners ──────────────────────────────────────────────────────

func TestTwoSteersInOneTurnArriveInOrderInOneMessage(t *testing.T) {
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{
			nil,
			{steerText(61, "first words"), steerText(62, "second words")},
		}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	blocks := lastUserBlocks(client.request(1))
	if len(blocks) < 2 || blocks[len(blocks)-2].Text != "first words" || blocks[len(blocks)-1].Text != "second words" {
		t.Fatalf("the two instructions are not in queue order in one trailing turn: %+v", blocks)
	}
	if n := countEvents(sink.all(), EventSteerDelivered); n != 2 {
		t.Fatalf("%d delivered events, want one per steer", n)
	}
}

func TestSteersBuyNoIterations(t *testing.T) {
	// Q4: a steer buys nothing — not even a turn. The instruction is DELIVERED
	// (durable, honest: the transcript will show what was said) and the step
	// then meets its bound exactly where it would have without the steer.
	spy := newSpyTools("read")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)}},
	)
	checkpoints := &flakyCheckpoint{ok: 100}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	st.Manifest.MaxIterations = 1
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, Checkpoint: checkpoints,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{
			nil,
			{steerText(71, "too late to matter")},
		}},
	})
	if res.Outcome != OutcomeBlocked || !strings.Contains(res.Reason, "MaxIterations") {
		t.Fatalf("a steered step at its bound = %q/%q, want blocked on MaxIterations — the steer "+
			"must not buy room", res.Outcome, res.Reason)
	}
	if len(spy.recorded()) != 0 {
		t.Fatal("tools ran despite the steer")
	}
	// The delivery still happened: the last checkpoint carried BOTH the text
	// and the ids — the person's words are in the record even though the step
	// ran out of room before the model could answer them.
	var delivered *Checkpoint
	for i := range checkpoints.snapshots {
		if len(checkpoints.snapshots[i].SteerIDs) == 1 {
			delivered = &checkpoints.snapshots[i]
		}
	}
	if delivered == nil {
		t.Fatal("the steer was never delivered into a checkpoint")
	}
	if n := occurrencesOfText(delivered.Messages, "too late to matter"); n != 1 {
		t.Fatal("the delivered checkpoint does not carry the instruction exactly once")
	}
	if delivered.SteerIDs[0] != 71 || delivered.Iteration != 1 {
		t.Fatalf("delivered ids/iteration = %v/%d, want [71]/1", delivered.SteerIDs, delivered.Iteration)
	}
}

func TestSteerDeliveredInTheSameCheckpointAsItsText(t *testing.T) {
	// D4's runtime half of the exactly-once argument: there is no Save that
	// contains the steer's text without naming its row, and no Save that
	// names the row without containing the text. (The store half — one
	// transaction — is pinned in the store's own file.)
	client := newFakeClient(
		scriptedTurn{Text: okHandoff},
		scriptedTurn{Text: okHandoff},
	)
	checkpoints := &flakyCheckpoint{ok: 100}
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Checkpoint: checkpoints, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers: &queuedSteers{batches: [][]Steer{nil, {steerText(81, "pair me with my checkpoint")}}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	// The invariant is about the FIRST save carrying the text: the steer's
	// text outlives its delivery (it stays in every later transcript snapshot
	// — that is the point), so what must be inseparable is the text's ARRIVAL
	// and the ids: whichever save first contains the words is the save that
	// names the rows.
	firstText := -1
	for i, cp := range checkpoints.snapshots {
		if occurrencesOfText(cp.Messages, "pair me with my checkpoint") > 0 {
			firstText = i
			break
		}
	}
	if firstText < 0 {
		t.Fatal("no checkpoint ever carried the instruction")
	}
	if ids := checkpoints.snapshots[firstText].SteerIDs; len(ids) != 1 || ids[0] != 81 {
		t.Fatalf("the save that first carried the text names %v, want [81] — text and delivery "+
			"must arrive in ONE write", ids)
	}
}
