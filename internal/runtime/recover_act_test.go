package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func recoveryFixture(name string) ResumeRequest {
	use := recoveryUse("u1", name)
	st := testState(testHandler(name), []model.ToolSpec{toolSpec(name)})
	msgs := append([]model.Message(nil), st.Messages...)
	msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: []model.Block{use}})
	manifest := *st.Manifest
	manifest.IterationsUsed = 1
	return ResumeRequest{Request: st.Request, Messages: msgs, Manifest: manifest}
}

func recoveryOpts(name, class string, order *[]string) (RunOpts, *classedSpy) {
	spy := &classedSpy{spyTools: newSpyTools(name), class: class, order: order}
	return RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: spy,
		HandoffSpec: HandoffSpec{Required: []string{"status"}}}, spy
}

func TestRecoverCaseAMatchesUninterrupted(t *testing.T) {
	req := recoveryFixture("read")
	req.Messages = append(req.Messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock("u1", "ok", false)}})
	opts, _ := recoveryOpts("read", "safe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseA}, opts)
	want := ResumeStep(context.Background(), ResumeRequest{Request: req.Request, Messages: req.Messages, Manifest: req.Manifest, Recovery: true}, opts)
	if got.Outcome != want.Outcome || len(got.Messages) != len(want.Messages) || got.Manifest.IterationsUsed != want.Manifest.IterationsUsed {
		t.Fatalf("recovered=%+v; uninterrupted=%+v", got, want)
	}
}

func TestRecoverCaseBReIssuesAndRecords(t *testing.T) {
	req := recoveryFixture("read")
	req.Messages = req.Messages[:len(req.Messages)-1]
	opts, _ := recoveryOpts("read", "safe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseB, Reason: "model turn lost"}, opts)
	if got.Outcome != OutcomeOK || got.Manifest.IterationsUsed != 2 {
		t.Fatalf("result=%+v", got)
	}
}

func TestRecoverReRunsSafeTool(t *testing.T) {
	req := recoveryFixture("read")
	order := []string{}
	opts, spy := recoveryOpts("read", "safe", &order)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "read", Action: ReRun}}}, opts)
	if got.Outcome != OutcomeOK || len(spy.recorded()) != 1 {
		t.Fatalf("result=%+v calls=%v", got, spy.recorded())
	}
}

func TestRecoverReRunsUnsafeWithNoReceipt(t *testing.T) {
	req := recoveryFixture("bash")
	order := []string{}
	opts, spy := recoveryOpts("bash", "unsafe", &order)
	opts.Receipts = receiptSpy{order: &order}
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: ReRun}}}, opts)
	if got.Outcome != OutcomeOK || len(spy.recorded()) != 1 || strings.Join(order, ",") != "begin,execute,complete" {
		t.Fatalf("result=%+v order=%v", got, order)
	}
}

func TestRecoverRecoversCapturedResult(t *testing.T) {
	req := recoveryFixture("bash")
	order := []string{}
	opts, spy := recoveryOpts("bash", "unsafe", &order)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: RecoverResult, Result: "captured"}}}, opts)
	if got.Outcome != OutcomeOK || len(spy.recorded()) != 0 || got.Messages[len(req.Messages)].Content[0].Content != "captured" {
		t.Fatalf("result=%+v calls=%v", got, spy.recorded())
	}
}

func TestRecoverFailsOnUnknownOutcome(t *testing.T) {
	req := recoveryFixture("bash")
	order := []string{}
	opts, spy := recoveryOpts("bash", "unsafe", &order)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: FailStep, Reason: "bash receipt started but not completed"}}}, opts)
	if got.Outcome != OutcomeFailed || len(spy.recorded()) != 0 || !strings.Contains(got.Reason, "bash receipt started but not completed") {
		t.Fatalf("result=%+v calls=%v", got, spy.recorded())
	}
}

func TestRecoverFailsOnDigestMismatch(t *testing.T) {
	req := recoveryFixture("bash")
	opts, _ := recoveryOpts("bash", "unsafe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: FailStep, Reason: "digest-mismatch"}}}, opts)
	if got.Outcome != OutcomeFailed || !strings.Contains(got.Reason, "digest-mismatch") {
		t.Fatalf("result=%+v", got)
	}
}

func TestRecoverFailStepSkipsOtherCalls(t *testing.T) {
	req := recoveryFixture("read")
	opts, spy := recoveryOpts("read", "safe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{{ToolUseID: "u1", Tool: "read", Action: ReRun}, {ToolUseID: "u2", Tool: "bash", Action: FailStep, Reason: "unknown-tool-outcome"}}}, opts)
	if got.Outcome != OutcomeFailed || len(spy.recorded()) != 0 {
		t.Fatalf("result=%+v calls=%v", got, spy.recorded())
	}
}

func TestRecoverReadsBoundsFromManifest(t *testing.T) {
	req := recoveryFixture("read")
	req.Manifest.MaxIterations = 1
	req.Messages = append(req.Messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock("u1", "ok", false)}})
	opts, _ := recoveryOpts("read", "safe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseA}, opts)
	if got.Outcome != OutcomeBlocked || got.Manifest.IterationsUsed != 1 {
		t.Fatalf("result=%+v", got)
	}
}

func TestRecoverUnclassifiableFailsWithReason(t *testing.T) {
	req := recoveryFixture("read")
	opts, _ := recoveryOpts("read", "safe", nil)
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseUnclassifiable, Reason: "corrupt transcript"}, opts)
	if got.Outcome != OutcomeFailed || got.Reason != "corrupt transcript" {
		t.Fatalf("result=%+v", got)
	}
}
