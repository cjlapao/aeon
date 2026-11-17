package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func recoveryMessages(uses ...model.Block) []model.Message {
	return []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
		{Role: model.RoleAssistant, Content: uses}}
}

func recoveryUse(id, name string) model.Block {
	return model.Block{Kind: model.BlockToolUse, ID: id, Name: name, Input: []byte(`{"path":"x"}`)}
}

func TestDetermineCaseTable(t *testing.T) {
	use := recoveryUse("one", "bash")
	other := recoveryUse("two", "read")
	result := model.ToolResultBlock("one", "ok", false)
	completed := "done"
	validReceipt := RecoveryReceipt{ToolUseID: "one", Tool: "bash", ArgDigest: toolArgDigest("bash", use.Input), Completed: true, Result: &completed}
	classes := map[string]string{"bash": "unsafe", "read": "safe", "edit": "detectable", "question": "safe"}
	cases := []struct {
		name     string
		msgs     []model.Message
		receipts []RecoveryReceipt
		want     Case
		action   CallAction
		reason   string
	}{
		{"empty", nil, nil, CaseB, "", ""},
		{"user tail", []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}, nil, CaseB, "", ""},
		{"text assistant", recoveryMessages(model.TextBlock("hello")), nil, CaseA, "", ""},
		{"answered", append(recoveryMessages(use), model.Message{Role: model.RoleUser, Content: []model.Block{result}}), nil, CaseA, "", ""},
		{"two answered", append(recoveryMessages(use, other), model.Message{Role: model.RoleUser, Content: []model.Block{result, model.ToolResultBlock("two", "ok", false)}}), nil, CaseA, "", ""},
		{"partly answered", append(recoveryMessages(use, other), model.Message{Role: model.RoleUser, Content: []model.Block{result}}), nil, CaseC, ReRun, ""},
		{"safe", recoveryMessages(other), nil, CaseC, ReRun, ""},
		{"detectable", recoveryMessages(recoveryUse("one", "edit")), nil, CaseC, ReRun, ""},
		{"unsafe absent", recoveryMessages(use), nil, CaseC, ReRun, ""},
		{"unsafe complete", recoveryMessages(use), []RecoveryReceipt{validReceipt}, CaseC, RecoverResult, ""},
		{"unsafe mismatch", recoveryMessages(use), []RecoveryReceipt{{ToolUseID: "one", Tool: "bash", ArgDigest: "wrong", Completed: true, Result: &completed}}, CaseC, FailStep, "digest-mismatch"},
		{"unsafe incomplete", recoveryMessages(use), []RecoveryReceipt{{ToolUseID: "one", Tool: "bash"}}, CaseC, FailStep, "started but not completed"},
		{"unsafe null result", recoveryMessages(use), []RecoveryReceipt{{ToolUseID: "one", Tool: "bash", ArgDigest: validReceipt.ArgDigest, Completed: true}}, CaseC, FailStep, "without a result"},
		{"unknown receipt", recoveryMessages(use), []RecoveryReceipt{{ToolUseID: "other", Tool: "bash"}}, CaseUnclassifiable, "", "absent tool_use"},
		{"orphan result", []model.Message{{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock("missing", "x", false)}}}, nil, CaseUnclassifiable, "", "no earlier tool_use"},
		{"missing class", recoveryMessages(recoveryUse("one", "missing")), nil, CaseUnclassifiable, "", "re-run class"},
		{"question shape", recoveryMessages(recoveryUse("one", "question")), nil, CaseC, ReRun, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineCase(tt.msgs, tt.receipts, classes)
			if got.Case != tt.want {
				t.Fatalf("case = %s, want %s: %+v", got.Case, tt.want, got)
			}
			if tt.action != "" && (len(got.Calls) != 1 || got.Calls[0].Action != tt.action) {
				t.Fatalf("calls = %+v", got.Calls)
			}
			if tt.reason != "" {
				reason := got.Reason
				if len(got.Calls) > 0 {
					reason += got.Calls[0].Reason
				}
				if !strings.Contains(reason, tt.reason) {
					t.Fatalf("reason = %q", reason)
				}
			}
		})
	}
}

func TestDetermineCaseIsPure(t *testing.T) {
	msgs := recoveryMessages(recoveryUse("one", "bash"))
	receipts := []RecoveryReceipt{{ToolUseID: "one", Tool: "bash"}}
	beforeMsgs := append([]model.Message(nil), msgs...)
	beforeReceipts := append([]RecoveryReceipt(nil), receipts...)
	a := DetermineCase(msgs, receipts, map[string]string{"bash": "unsafe"})
	b := DetermineCase(msgs, receipts, map[string]string{"bash": "unsafe"})
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(msgs, beforeMsgs) || !reflect.DeepEqual(receipts, beforeReceipts) {
		t.Fatal("classifier changed its result or inputs")
	}
}

func TestDetermineCaseNeverPanics(t *testing.T) {
	for _, msgs := range [][]model.Message{{}, {{Role: model.RoleAssistant}}, {{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockToolResult, ToolUseID: "x"}}}}, {{Role: model.RoleUser, Content: []model.Block{model.TextBlock("x")}}, {Role: model.RoleAssistant, Content: []model.Block{recoveryUse("x", "bash"), recoveryUse("x", "bash")}}}} {
		_ = DetermineCase(msgs, nil, nil)
	}
}

func TestSuspendedShapeIsNotSpecialCased(t *testing.T) {
	got := DetermineCase(recoveryMessages(recoveryUse("q", "question")), nil, map[string]string{"question": "safe"})
	if got.Case != CaseC {
		t.Fatalf("question shape classified as %s", got.Case)
	}
}

func TestDetermineCaseFailStepWinsOverReRun(t *testing.T) {
	read := recoveryUse("r", "read")
	bash := recoveryUse("b", "bash")
	got := DetermineCase(recoveryMessages(read, bash), []RecoveryReceipt{{ToolUseID: "b", Tool: "bash"}},
		map[string]string{"read": "safe", "bash": "unsafe"})
	if got.Case != CaseC || len(got.Calls) != 2 || got.Calls[0].Action != ReRun || got.Calls[1].Action != FailStep {
		t.Fatalf("plan=%+v", got)
	}
	order := []string{}
	opts, spy := recoveryOpts("read", "safe", &order)
	req := recoveryFixture("read")
	result := RecoverStep(context.Background(), req, got, opts)
	if result.Outcome != OutcomeFailed || len(spy.recorded()) != 0 {
		t.Fatalf("result=%+v calls=%v", result, spy.recorded())
	}
}
