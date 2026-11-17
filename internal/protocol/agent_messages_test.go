package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

// agent_messages_test.go pins MB7's door: it reads, it refuses bad
// questions by name, it answers a named-absent thread as a SAID absence,
// and it is the ONLY mailbox door a socket can knock on.

func TestAgentMessagesRefusesBadQuestionsNamingThem(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.AgentMessages = func(string, *int64) ([]AgentMessageView, bool, error) {
		t.Fatal("the seam must not run for a refused question")
		return nil, false, nil
	}
	for _, tc := range []struct {
		name, params, wants string
	}{
		{"no loop id", `{"thread":3}`, "loopId is required"},
		{"thread zero", `{"loopId":"l-7","thread":0}`, "not a thread id"},
		{"thread negative", `{"loopId":"l-7","thread":-2}`, "not a thread id"},
	} {
		_, err := tab.handleAgentMessages(context.Background(), nil, json.RawMessage(tc.params))
		if err == nil {
			t.Errorf("%s: the question was accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("%s: refusal %q does not name %q", tc.name, err.Error(), tc.wants)
		}
	}
	// No store mounted: the read refuses naming its unavailability — never
	// an invented empty mailbox.
	bare := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if _, err := bare.handleAgentMessages(context.Background(), nil,
		json.RawMessage(`{"loopId":"l-7"}`)); err == nil || !strings.Contains(err.Error(), "store") {
		t.Errorf("with no store mounted the read should refuse naming the store, got %v", err)
	}
}

func TestAgentMessagesAnswersFromTheRowsIncludingSaidAbsence(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	var gotID string
	var gotThread *int64
	tab.AgentMessages = func(loopID string, thread *int64) ([]AgentMessageView, bool, error) {
		gotID, gotThread = loopID, thread
		if thread != nil && *thread == 99 {
			return nil, true, nil // the thread does not exist — SAID, not silent
		}
		return []AgentMessageView{{ID: 12, Thread: 12, FromAgent: "planner", ToAgent: "writer",
			FromStepID: 4, Body: "what framing?", ExpectReply: true, CreatedAt: "2026-10-31T10:00:00Z"}}, false, nil
	}
	res, err := tab.handleAgentMessages(context.Background(), nil, json.RawMessage(`{"loopId":"l-7","thread":12}`))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.(AgentMessagesResponse)
	if !ok {
		t.Fatalf("the response is %T, not the read's shape", res)
	}
	if gotID != "l-7" || gotThread == nil || *gotThread != 12 || got.LoopID != "l-7" || got.Absent {
		t.Errorf("the question or the answer drifted: %q %v %+v", gotID, gotThread, got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "what framing?" || !got.Messages[0].ExpectReply {
		t.Errorf("the rows did not ride: %+v", got.Messages)
	}
	// MB8's byte discipline: the optional fields stay ABSENT (null never
	// marshals as an invented empty, and an empty list marshals [] not null).
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"resultOf"`) || strings.Contains(string(raw), `"deliveredAt"`) {
		t.Errorf("unset optional fields marshalled: %s", raw)
	}

	res, err = tab.handleAgentMessages(context.Background(), nil, json.RawMessage(`{"loopId":"l-7","thread":99}`))
	if err != nil {
		t.Fatal(err)
	}
	got = res.(AgentMessagesResponse)
	if !got.Absent || len(got.Messages) != 0 {
		t.Errorf("a named thread the loop lacks must be a SAID absence, got %+v", got)
	}
	raw, _ = json.Marshal(got)
	if !strings.Contains(string(raw), `"messages":[]`) {
		t.Errorf("the empty list must marshal [], never null (MB8): %s", raw)
	}
}

// TestTheMailboxHasNoAuthoringDoorOnTheSurface (AT13) pins the ruling that
// a person reads the exchange and never authors it: `agent.messages` is the
// only mailbox method the table carries. A row is filed by a step's run
// through the ask door (runtime, behind the policy gate) — never by a
// socket caller. `loop.message` remains the human's door, and it is STEER
// (S39): it speaks to the loop, not as its agents.
func TestTheMailboxHasNoAuthoringDoorOnTheSurface(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	found := false
	for _, name := range tab.Methods() {
		n := string(name)
		if n == string(MethodAgentMessages) {
			found = true
			continue
		}
		if strings.HasPrefix(n, "agent.message") {
			t.Errorf("authoring door %q reached the surface — MB7: rows are filed only by a step's own run", n)
		}
		if strings.Contains(n, "message") && n != "loop.message" {
			t.Errorf("mailbox-named method %q on the surface — the only doors are agent.messages (read) and loop.message (steer)", n)
		}
	}
	if !found {
		t.Fatal("agent.messages is not registered — MB7's read is missing from the surface")
	}
	if m, ok := tab.methods[MethodAgentMessages]; !ok || m.Debug {
		t.Error("agent.messages must be an ordinary public method, not a debug one")
	}
}

// TestMailboxFreeFramesCarryNoMailboxField is AT15's wire half: a step
// event from a loop that never touched the mailbox marshals EXACTLY as it
// did before the mailbox existed — the MessageID field is omitempty, so an
// ordinary `step.tool.start` frame gains no key, no null, no drift. The
// exchange is invisible to loops that do not have one (MB8).
func TestMailboxFreeFramesCarryNoMailboxField(t *testing.T) {
	ordinary := stepEventPayload(runtimeStepEvent("read"), 3, "default")
	raw, err := json.Marshal(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "messageId") || strings.Contains(string(raw), "message_id") {
		t.Errorf("a mailbox-free frame grew a mailbox key (MB8): %s", raw)
	}
	withRow := stepEventPayload(runtimeStepEventWithRow("ask_agent", 12), 3, "default")
	raw, _ = json.Marshal(withRow)
	if !strings.Contains(string(raw), `"messageId":12`) {
		t.Errorf("the ask frame must name its row: %s", raw)
	}
}

func runtimeStepEvent(tool string) runtime.StepEvent {
	return runtime.StepEvent{Kind: "step.tool.start", Tool: tool, Text: "reading"}
}

func runtimeStepEventWithRow(tool string, messageID int64) runtime.StepEvent {
	e := runtimeStepEvent(tool)
	e.MessageID = messageID
	return e
}
