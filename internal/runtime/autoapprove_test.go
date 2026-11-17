package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// A person's standing pre-approval answers an ASK, in an attended or an unattended step, and nothing else.

func runAskedWith(t *testing.T, approve *AutoApprove, hasOperator bool, tool string) (StepResult, *spyTools) {
	t.Helper()
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash", "read").with("bash", ToolOutcome{Content: "ran"})
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", tool, `{"command":"`+testAskCommand+`"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(toolSpec("bash"), toolSpec("read"))
	req.HasOperator = hasOperator
	req.AutoApprove = approve
	opts := runOpts(client, spy, &recordingSink{})
	opts.Policy = gate
	return RunStep(context.Background(), req, opts), spy
}

func TestFullAccessRunsAnAskedCallWithoutSuspending(t *testing.T) {
	res, spy := runAskedWith(t, &AutoApprove{All: true, By: "user:1"}, true, "bash")
	if res.Kind == ResultSuspended || len(spy.recorded()) != 1 {
		t.Fatalf("full access must run the asked call, not suspend: kind=%s calls=%+v", res.Kind, spy.recorded())
	}
}

func TestPreApprovedToolsAnswerAsksEvenWhenNobodyIsThere(t *testing.T) {
	res, spy := runAskedWith(t, &AutoApprove{Tools: []string{"bash"}}, false, "bash")
	if len(spy.recorded()) != 1 || res.Kind == ResultSuspended {
		t.Fatalf("a pre-approved tool must run in an unattended loop: kind=%s calls=%+v", res.Kind, spy.recorded())
	}
	// A tool that is not on the list is still denied there, as before.
	_, spy = runAskedWith(t, &AutoApprove{Tools: []string{"read"}}, false, "bash")
	if len(spy.recorded()) != 0 {
		t.Fatalf("a tool outside the list ran: %+v", spy.recorded())
	}
}

func TestNoPreApprovalLeavesTheAskExactlyAsItWas(t *testing.T) {
	res, spy := runAskedWith(t, nil, true, "bash")
	if res.Kind != ResultSuspended || len(spy.recorded()) != 0 {
		t.Fatalf("with no pre-approval an attended ask must still suspend: kind=%s", res.Kind)
	}
}

func TestFullAccessNeverSoftensADeny(t *testing.T) {
	deny := &scriptedGate{decide: func(PolicyGateRequest, time.Time) PolicyGateDecision {
		return PolicyGateDecision{Allowed: false, Sentence: "refused: agent:test · denied by rule"}
	}}
	spy := newSpyTools("bash").with("bash", ToolOutcome{Content: "ran"})
	req := testRequest(toolSpec("bash"))
	req.AutoApprove = &AutoApprove{All: true}
	opts := runOpts(newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"rm x"}`)}}, scriptedTurn{Text: okHandoff}), spy, &recordingSink{})
	opts.Policy = deny
	RunStep(context.Background(), req, opts)
	if len(spy.recorded()) != 0 {
		t.Fatalf("full access ran a call the policy DENIED: %+v", spy.recorded())
	}
}
