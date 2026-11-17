package runtime

// mailbox_test.go pins S70's runtime half: the peer's words arrive at the
// next boundary exactly once with their frame above byte-identical words
// (AT7), the stamp rides the SAME Save that persists the fold (D4's law at
// the runtime end), an ask is answered honestly — filed, refused, or
// suspended through the ONE constructor with the blocked tool_use left
// unanswered for the answer to resume.

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

type queuedMailbox struct {
	batches [][]MailboxMessage
	calls   int
}

func (q *queuedMailbox) Pending(context.Context) []MailboxMessage {
	if q.calls < len(q.batches) {
		b := q.batches[q.calls]
		q.calls++
		return b
	}
	return nil
}

type scriptedDoor struct {
	outcome AskOutcome
	err     error
	inputs  []json.RawMessage
}

func (d *scriptedDoor) Ask(_ context.Context, in json.RawMessage) (AskOutcome, error) {
	d.inputs = append(d.inputs, in)
	return d.outcome, d.err
}

type recordingWriter struct{ saved []Checkpoint }

func (w *recordingWriter) Save(_ context.Context, cp Checkpoint) error {
	w.saved = append(w.saved, cp)
	return nil
}

func askSpecs() []model.ToolSpec {
	return []model.ToolSpec{{Name: "read"}, {Name: "ask_agent"}}
}

// countContaining counts text blocks whose text CONTAINS needle — the
// framed fold never equals the bare body, and AT7 is about the body
// surviving byte-identical inside the wire text, not being the whole of it.
func countContaining(msgs []model.Message, needle string) int {
	n := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockText && strings.Contains(b.Text, needle) {
				n++
			}
		}
	}
	return n
}

func TestMailboxMessageArrivesAtTheNextBoundaryOnce(t *testing.T) {
	body := "the risk section names three unknowns\n  — quote them verbatim "
	drain := &queuedMailbox{batches: [][]MailboxMessage{
		nil,
		{{ID: 9, FromAgent: "author", Body: body}},
	}}
	writer := &recordingWriter{}
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Checkpoint:  writer, MailboxDrain: drain,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if n := countContaining(client.request(0).Messages, body); n != 0 {
		t.Errorf("the first call carried the peer's words (%d) — the boundary is the moment, not before", n)
	}
	if n := countContaining(client.request(1).Messages, body); n != 1 {
		t.Fatalf("the words reached the wire %d times, want exactly 1", n)
	}
	blocks := lastUserBlocks(client.request(1))
	last := blocks[len(blocks)-1]
	if last.Kind != model.BlockText {
		t.Fatal("the peer's words do not end the trailing user turn")
	}
	// AT7: frame above, words byte-identical below the blank line — the
	// composition is composition, never editing.
	if !strings.HasSuffix(last.Text, body) {
		t.Errorf("the words are not byte-identical on the wire: %q", last.Text)
	}
	if !strings.HasPrefix(last.Text, "message from author (mailbox 9):\n\n") {
		t.Errorf("the frame line above the words is wrong: %q", last.Text)
	}
	// The delivered event names the row, and the Save that carried the fold
	// named the stamp — one write, D4.
	if n := countEvents(sink.all(), EventAgentMessageDelivered); n != 1 {
		t.Fatalf("%d delivered events, want 1", n)
	}
	if e := eventsOf(sink.all(), EventAgentMessageDelivered)[0]; e.MessageID != 9 {
		t.Errorf("the delivered event names message %d, want 9", e.MessageID)
	}
	var joined bool
	for _, cp := range writer.saved {
		if len(cp.MailboxIDs) == 1 && cp.MailboxIDs[0] == 9 {
			if occurrencesOfText(cp.Messages, "message from author (mailbox 9):\n\n"+body) != 1 {
				t.Fatal("the Save that named the stamp did not contain its words — that is D4's window")
			}
			joined = true
		}
	}
	if !joined {
		t.Fatal("no checkpoint paired the fold with its delivery ids")
	}
}

func TestAskFilesAnswersAndTheStepKeepsSpeaking(t *testing.T) {
	door := &scriptedDoor{outcome: AskOutcome{MessageID: 5, Thread: 2,
		Result: "asked author (message 5 in thread 2); it lands at their next turn boundary — no answer is awaited"}}
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("a1", "ask_agent", `{"to":"author","text":"x","wait":false}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, MailboxAsk: door,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if len(door.inputs) != 1 || !json.Valid(door.inputs[0]) {
		t.Fatalf("the door saw %d inputs; the RAW arguments must arrive intact for the ONE parser", len(door.inputs))
	}
	blocks := lastUserBlocks(client.request(1))
	var answered bool
	for _, b := range blocks {
		if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
			answered = true
			if b.IsError || !strings.Contains(b.Content, "message 5 in thread 2") {
				t.Errorf("the ask's tool_use got %q (err %v), want the door's honest answer", b.Content, b.IsError)
			}
		}
	}
	if !answered {
		t.Fatal("the ask's tool_use went unanswered")
	}
	if n := countEvents(sink.all(), EventAgentMessageAsked); n != 1 || eventsOf(sink.all(), EventAgentMessageAsked)[0].MessageID != 5 {
		t.Errorf("the stream does not announce the filed ask: %d events", n)
	}
}

func TestAskRefusalIsAnHonestAnswerNotAHalt(t *testing.T) {
	door := &scriptedDoor{err: context.DeadlineExceeded} // any refusal sentence
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("a1", "ask_agent", `{"to":"author","text":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: &recordingSink{},
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, MailboxAsk: door,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("a refused ask killed the step (%q): %q — a refusal is an answer", res.Outcome, res.Reason)
	}
	for _, m := range client.request(1).Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
				if !b.IsError {
					t.Error("the refusal arrived as a success")
				}
				return
			}
		}
	}
	t.Fatal("the refused ask left its tool_use unanswered")
}

func TestWaitTrueSuspendsThroughTheOneConstructorAndStaysUnanswered(t *testing.T) {
	door := &scriptedDoor{outcome: AskOutcome{MessageID: 11, Thread: 4, Suspends: true,
		Question: &PendingQuestion{Body: "waiting for author to answer message 11 (thread 4)"}}}
	client := newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("t1", "read", `{"path":"a.md"}`),
		toolUse("a1", "ask_agent", `{"to":"author","text":"ship it?"}`),
	}})
	sink := &recordingSink{}
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, MailboxAsk: door,
	})
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %q, want suspended — wait:true is suspension, not waiting (P2)", res.Kind)
	}
	if res.Question == nil || res.Question.ToolUseID != "a1" {
		t.Fatalf("the suspension must name the blocked tool_use for the answer to resume: %+v", res.Question)
	}
	if res.Question.Kind != "free" {
		t.Errorf("question kind %q — the loop guarantees the row vocabulary", res.Question.Kind)
	}
	if len(res.Messages) > 0 {
		// The blocked ask stays UNANSWERED (the answer resumes it); the
		// ordinary read DID answer.
		var sawRead, sawAsk bool
		for _, m := range res.Messages {
			for _, b := range m.Content {
				if b.Kind == model.BlockToolResult && b.ToolUseID == "t1" {
					sawRead = true
				}
				if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
					sawAsk = true
				}
			}
		}
		if !sawRead {
			t.Error("the ordinary tool before the ask did not get its result recorded before the pause")
		}
		if sawAsk {
			t.Error("the blocked ask answered itself — the PEER's answer must resume it")
		}
	}
	if n := countEvents(sink.all(), EventAgentMessageAsked); n != 1 {
		t.Errorf("%d asked events, want 1", n)
	}
	if client.requestCount() != 1 {
		t.Errorf("%d model calls — suspension is exiting, no further speaking (P2)", client.requestCount())
	}
}

func TestAskSuspensionClosesLaterAsksUnrunAndNeverFilesThem(t *testing.T) {
	door := &scriptedDoor{outcome: AskOutcome{MessageID: 12, Thread: 5, Suspends: true,
		Question: &PendingQuestion{Body: "waiting"}}}
	client := newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("a1", "ask_agent", `{"to":"author","text":"one"}`),
		toolUse("a2", "ask_agent", `{"to":"reviewer","text":"two"}`),
	}})
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: &recordingSink{},
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, MailboxAsk: door,
	})
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %q, want suspended", res.Kind)
	}
	if len(door.inputs) != 1 {
		t.Fatalf("the door filed %d asks; the ask AFTER the suspending one never runs and never files", len(door.inputs))
	}
	var closed bool
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "a2" {
				closed = true
				if !b.IsError || !strings.Contains(b.Content, "did not run") {
					t.Errorf("the later ask closed dishonestly: %q (err %v)", b.Content, b.IsError)
				}
			}
		}
	}
	if !closed {
		t.Fatal("the later ask was left silent — a silent tool_use becomes a lost result the day the step resumes")
	}
}

// ── the gate arm (D12's parity is structural: the ask is judged by the
// SAME gate, at the SAME instant, with the SAME formatter — these tests
// pin that the judgement reaches the ask and the door never runs behind a
// refusal or a pending permission) ──────────────────────────────────────

func askGateOn(tools ...string) *scriptedGate {
	return &scriptedGate{decide: func(req PolicyGateRequest, at time.Time) PolicyGateDecision {
		for _, t := range tools {
			if req.Tool == t {
				return PolicyGateDecision{Allowed: false, Asked: true,
					Sentence: "ask: confirm the ask with the operator", RuleID: "confirm-ask", RuleSubject: "agent:peer"}
			}
		}
		if req.Tool == "ask_agent_denied" {
			return PolicyGateDecision{Allowed: false, Sentence: "deny: asks are closed on this lane"}
		}
		return PolicyGateDecision{Allowed: true, Sentence: "allowed: test"}
	}}
}

func TestGateRefusalOfAnAskIsHonestAndFilesNothing(t *testing.T) {
	door := &scriptedDoor{outcome: AskOutcome{MessageID: 99, Result: "should never run"}}
	gate := &scriptedGate{decide: func(req PolicyGateRequest, at time.Time) PolicyGateDecision {
		if req.Tool == "ask_agent" {
			return PolicyGateDecision{Allowed: false, Sentence: "deny: asks are closed on this lane"}
		}
		return PolicyGateDecision{Allowed: true, Sentence: "allowed: test"}
	}}
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("a1", "ask_agent", `{"to":"author","text":"x","wait":false}`)}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(askSpecs()...)
	req.StepID = 12
	sink := &recordingSink{}
	opts := runOpts(client, newSpyTools("read"), sink)
	opts.Policy = gate
	opts.MailboxAsk = door
	res := RunStep(context.Background(), req, opts)
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q — a refused ask is answered, not fatal", res.Outcome, res.Reason)
	}
	if len(door.inputs) != 0 {
		t.Fatal("the door filed behind a refusal — the gate is not advisory")
	}
	if len(gate.requests) == 0 || gate.requests[0].Tool != "ask_agent" || !gate.requests[0].ModelRequested || gate.requests[0].StepID != 12 {
		t.Errorf("the ask was not judged as a model-emitted tool call of step 12: %+v", gate.requests)
	}
	var refused bool
	for _, m := range client.request(1).Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
				refused = true
				if !b.IsError || !strings.Contains(b.Content, "asks are closed on this lane") {
					t.Errorf("the refusal lost the gate's sentence (P31 verbatim): %q (err %v)", b.Content, b.IsError)
				}
			}
		}
	}
	if !refused {
		t.Fatal("the refused ask left its tool_use unanswered")
	}
	if n := countEvents(sink.all(), EventPolicyDecided); n != 1 {
		t.Errorf("%d policy events, want 1 — the verdict is on the stream (P31)", n)
	}
}

func TestGateAskedAboutAnAskSuspendedByTheOperator(t *testing.T) {
	door := &scriptedDoor{outcome: AskOutcome{MessageID: 1}}
	gate := askGateOn("ask_agent")
	client := newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("t1", "read", `{"path":"a.md"}`),
		toolUse("a1", "ask_agent", `{"to":"author","text":"ship it?"}`),
	}})
	req := testRequest(askSpecs()...)
	req.HasOperator = true
	sink := &recordingSink{}
	opts := runOpts(client, newSpyTools("read"), sink)
	opts.Policy = gate
	opts.MailboxAsk = door
	res := RunStep(context.Background(), req, opts)
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %q, want suspended — an attended gate ask suspends like S11's (P2)", res.Kind)
	}
	if len(door.inputs) != 0 {
		t.Fatal("the ask filed behind a pending permission — Asked is not Allowed")
	}
	q := res.Question
	if q == nil || q.Kind != "permission" || q.ToolUseID != "a1" {
		t.Fatalf("permission-shape suspension expected on the ask's block: %+v", q)
	}
	// The ordinary read answered BEFORE the pause; the blocked ask did not.
	var sawRead, sawAsk bool
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "t1" {
				sawRead = true
			}
			if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
				sawAsk = true
			}
		}
	}
	if !sawRead || sawAsk {
		t.Errorf("read answered %v, ask answered %v — the pause records the run, leaves the blocked ask for the answer", sawRead, sawAsk)
	}
}

func TestGateAskedWithNoAnswererDeniesAtOnceNoQuestionRow(t *testing.T) {
	gate := askGateOn("ask_agent")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("a1", "ask_agent", `{"to":"author","text":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(askSpecs()...) // HasOperator false: nobody can answer
	opts := runOpts(client, newSpyTools("read"), &recordingSink{})
	opts.Policy = gate
	opts.MailboxAsk = &scriptedDoor{}
	res := RunStep(context.Background(), req, opts)
	if res.Kind == ResultSuspended {
		t.Fatal("a question answered by nobody must deny NOW, not suspend (S11)")
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q: %q", res.Outcome, res.Reason)
	}
	var denied bool
	for _, m := range client.request(1).Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "a1" {
				denied = true
				if !b.IsError || !strings.Contains(b.Content, "confirm the ask") {
					t.Errorf("the at-once denial lost the sentence: %q", b.Content)
				}
			}
		}
	}
	if !denied {
		t.Fatal("the denied ask left its tool_use unanswered")
	}
}

// ── AT10: the AS5 stack dump ─────────────────────────────────────────────
//
// The mailbox's two seams run ON the step's own goroutine: the drain's read
// and the ask's door are ordinary calls on the stack RunLoop itself is
// walking — no hand-off, no parked goroutine, no hidden hand. The stack
// captured inside each seam must name RunLoop (the same walk) and carry no
// frame from the packages a step may never reach while it speaks — daemon,
// store, agentsvc. Suspension is a ROW (P2), never a waiting goroutine;
// this test makes the shape visible instead of argued.

type stackDoor struct {
	capture *string
	outcome AskOutcome
}

func (d stackDoor) Ask(context.Context, json.RawMessage) (AskOutcome, error) {
	*d.capture = string(debug.Stack())
	return d.outcome, nil
}

type stackDrain struct {
	capture *string
	msgs    []MailboxMessage
}

func (d stackDrain) Pending(context.Context) []MailboxMessage {
	*d.capture = string(debug.Stack())
	return d.msgs
}

func TestMailboxSeamsWalkTheStepsOwnStack(t *testing.T) {
	var askStack, drainStack string
	door := stackDoor{capture: &askStack, outcome: AskOutcome{MessageID: 3, Thread: 3, Result: "asked"}}
	drain := stackDrain{capture: &drainStack,
		msgs: []MailboxMessage{{ID: 9, FromAgent: "author", Body: "peer words"}}}
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("a1", "ask_agent", `{"to":"author","text":"one","wait":false}`)}},
		scriptedTurn{Text: okHandoff})
	st := testState(testHandler(), askSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("ask_agent"), Sink: &recordingSink{},
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		MailboxAsk:  door, MailboxDrain: drain,
	})
	if res.Kind != ResultComplete {
		t.Fatalf("the step ended %q — the wait:false ask answers and the walk continues", res.Kind)
	}
	for name, stack := range map[string]string{"ask door": askStack, "boundary drain": drainStack} {
		if stack == "" {
			t.Fatalf("the %s never ran — the seam did not execute inside the step", name)
		}
		if !strings.Contains(stack, "runtime.RunLoop") {
			t.Errorf("the %s ran off the step's own stack — no RunLoop frame: %s", name, stack)
		}
		for _, ban := range []string{"internal/daemon", "internal/store", "internal/agentsvc"} {
			if strings.Contains(stack, ban) {
				t.Errorf("the %s reached into %s — a step speaking must touch none of them (K22/AS5)", name, ban)
			}
		}
	}
}
