package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// A permission question is answered by a person, and the answer decides the blocked call: approve runs
// that one call now, deny leaves it unrun, and the approval is spent by the call it was for.

func askedBashRun(t *testing.T, extra ...scriptedTurn) (ResumeRequest, *spyTools, *scriptedGate) {
	t.Helper()
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash").with("bash", ToolOutcome{Content: "pushed"})
	turns := append([]scriptedTurn{{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`)}}}, extra...)
	req := testRequest(toolSpec("bash"))
	req.HasOperator = true
	req.StepID = 9
	opts := runOpts(newFakeClient(turns...), spy, &recordingSink{})
	opts.Policy = gate
	res := RunStep(context.Background(), req, opts)
	if res.Kind != ResultSuspended || res.Question == nil || res.Question.Kind != "permission" {
		t.Fatalf("fixture did not suspend on a permission question: %+v", res)
	}
	return ResumeRequest{Request: req, Messages: res.Messages, Manifest: res.Manifest, Question: *res.Question, Usage: res.Usage, Cost: res.Cost}, spy, gate
}

func resumeWith(req ResumeRequest, answer string, spy *spyTools, gate *scriptedGate, turns ...scriptedTurn) StepResult {
	req.Answer = answer
	opts := RunOpts{Client: newFakeClient(turns...), Tools: spy, Sink: &recordingSink{}, Policy: gate, HandoffSpec: DefaultHandoffSpec(), StatusField: "status"}
	return ResumeStep(context.Background(), req, opts)
}

func TestApproveRunsTheBlockedCallOnceAndTheModelSeesItsRealResult(t *testing.T) {
	req, spy, gate := askedBashRun(t)
	res := resumeWith(req, "approve", spy, gate, scriptedTurn{Text: okHandoff})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	calls := spy.recorded()
	if len(calls) != 1 || calls[0].Name != "bash" {
		t.Fatalf("tools ran %+v — an approved call must run exactly once", calls)
	}
	out, ok := toolResultFor(res.Messages, "t1")
	if !ok || out.Content != "pushed" || out.IsError {
		t.Fatalf("the blocked call's result = %+v, want the tool's real output, not the word approve", out)
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Fatalf("transcript invalid: %s", v.Reason)
	}
}

func TestDenyLeavesTheCallUnrunAndSaysAPersonRefused(t *testing.T) {
	req, spy, gate := askedBashRun(t)
	res := resumeWith(req, "deny", spy, gate, scriptedTurn{Text: okHandoff})
	if len(spy.recorded()) != 0 {
		t.Fatalf("a denied call ran: %+v", spy.recorded())
	}
	out, _ := toolResultFor(res.Messages, "t1")
	if !out.IsError || !strings.Contains(out.Content, "a person denied") {
		t.Fatalf("the refusal = %+v", out)
	}
}

func TestAnythingButApproveIsNotAnApproval(t *testing.T) {
	for _, answer := range []string{"", "yes", "Approve", "approve it", "ok"} {
		req, spy, gate := askedBashRun(t)
		resumeWith(req, answer, spy, gate, scriptedTurn{Text: okHandoff})
		if len(spy.recorded()) != 0 {
			t.Fatalf("answer %q ran the call: silence and typos are not consent", answer)
		}
	}
}

func TestApprovingOneCallDoesNotApproveTheNext(t *testing.T) {
	req, spy, gate := askedBashRun(t)
	// After the approved run the model asks for the very same command again: it asks again.
	res := resumeWith(req, "approve", spy, gate, scriptedTurn{ToolUses: []model.Block{toolUse("t2", "bash", `{"command":"`+testAskCommand+`"}`)}})
	if len(spy.recorded()) != 1 {
		t.Fatalf("the repeat ran: %+v — one approval is one call", spy.recorded())
	}
	if res.Kind != ResultSuspended || res.Question == nil || res.Question.ToolUseID != "t2" {
		t.Fatalf("the identical second call must ask again: %+v", res)
	}
}

func TestApprovalIsBoundToTheExactBlock(t *testing.T) {
	st := &LoopState{approved: &approvedCall{ToolUseID: "t1", Tool: "bash", Digest: toolArgDigest("bash", []byte(`{"command":"ls"}`))}}
	for name, use := range map[string]model.Block{
		"another block id":   toolUse("t9", "bash", `{"command":"ls"}`),
		"another tool":       toolUse("t1", "read", `{"command":"ls"}`),
		"different argument": toolUse("t1", "bash", `{"command":"rm -rf /"}`),
	} {
		if st.consumeApproval(use) {
			t.Fatalf("%s consumed the approval", name)
		}
	}
	if !st.consumeApproval(toolUse("t1", "bash", `{"command":"ls"}`)) || st.consumeApproval(toolUse("t1", "bash", `{"command":"ls"}`)) {
		t.Fatal("the exact call consumes it once, and never twice")
	}
}

func TestAModelMadePermissionQuestionNeverRunsAnything(t *testing.T) {
	msgs := []model.Message{{Role: model.RoleAssistant, Content: []model.Block{toolUse("q1", "question", `{"text":"ok?","kind":"permission"}`)}}}
	if _, ok := blockedCall(msgs, "q1"); ok {
		t.Fatal("a question the model wrote must not be a call an approval can run")
	}
	msgs[0].Content[0] = toolUse("a1", "ask_agent", `{}`)
	if _, ok := blockedCall(msgs, "a1"); ok {
		t.Fatal("ask_agent is not an approvable call either")
	}
}
