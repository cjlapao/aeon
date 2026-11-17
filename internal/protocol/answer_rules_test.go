package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

func useBlock(name, input string) model.Block {
	return model.Block{Kind: model.BlockToolUse, ID: "t1", Name: name, Input: json.RawMessage(input)}
}

func TestEveryPermissionAnswerCollapsesToApproveOrDeny(t *testing.T) {
	want := map[string]string{
		runtime.AnswerApprove: "approve", runtime.AnswerApproveLoop: "approve", runtime.AnswerAlwaysAllow: "approve",
		runtime.AnswerDeny: "deny", runtime.AnswerAlwaysDeny: "deny", "anything else": "deny",
	}
	for in, out := range want {
		if got := permissionResume("permission", in); got != out {
			t.Errorf("%q resumes as %q, want %q", in, got, out)
		}
	}
	if permissionResume("clarify", "yes please") != "yes please" {
		t.Error("a non-permission answer must reach the step as the person wrote it")
	}
	for _, o := range runtime.PermissionOptions() {
		if IsRuleAnswer(o) != (o == runtime.AnswerApproveLoop || o == runtime.AnswerAlwaysAllow || o == runtime.AnswerAlwaysDeny) {
			t.Errorf("IsRuleAnswer(%q) is wrong", o)
		}
	}
}

func TestARuleAnswerNamesTheExactCallAndTheRightTarget(t *testing.T) {
	bash := useBlock("bash", `{"command":"go test ./..."}`)
	p, err := permissionRule(runtime.AnswerAlwaysAllow, "golang-engineer-loop", "loop-1", bash)
	if err != nil || p.Type != "command_access" || p.Action != "allow" || p.Target != (PolicyTargetP{Kind: "agent", ID: "golang-engineer-loop"}) || p.Source != "operator" {
		t.Fatalf("always allow bash: %+v %v", p, err)
	}
	if !strings.Contains(string(p.Spec), `"command":"go test ./..."`) || !strings.Contains(string(p.Spec), `"state":"allow"`) {
		t.Fatalf("the spec must name the exact command: %s", p.Spec)
	}
	loop, _ := permissionRule(runtime.AnswerApproveLoop, "a", "loop-1", bash)
	if loop.Target != (PolicyTargetP{Kind: "loop", ID: "loop-1"}) || loop.ID == p.ID {
		t.Fatalf("for this loop must target the loop and be a different rule: %+v", loop)
	}
	deny, _ := permissionRule(runtime.AnswerAlwaysDeny, "a", "", useBlock("write", `{"path":"x"}`))
	if deny.Type != "tool_access" || deny.Action != "deny" || !strings.Contains(string(deny.Spec), `"toolName":"write"`) {
		t.Fatalf("always deny of a tool: %+v", deny)
	}
	again, _ := permissionRule(runtime.AnswerAlwaysAllow, "golang-engineer-loop", "loop-1", bash)
	if again.ID != p.ID {
		t.Fatal("the same answer must make the same rule id, so a repeat writes it once")
	}
}

func TestARuleAnswerRefusesWhatItCannotNameExactly(t *testing.T) {
	if _, err := permissionRule(runtime.AnswerAlwaysAllow, "a", "", useBlock("bash", `{"command":"rm -rf build/*"}`)); err == nil {
		t.Fatal("a command with a wildcard must not become a rule: it would mean more than this command")
	}
	if _, err := permissionRule(runtime.AnswerAlwaysAllow, "a", "", useBlock("bash", "{\"command\":\"a\\nb\"}")); err == nil {
		t.Fatal("a multi-line script must not become a rule")
	}
	if _, err := permissionRule(runtime.AnswerApproveLoop, "a", "", useBlock("read", `{}`)); err == nil {
		t.Fatal("'for this loop' with no loop must refuse")
	}
	if _, err := permissionRule(runtime.AnswerAlwaysAllow, "a", "", useBlock("bash", `not json`)); err == nil {
		t.Fatal("an unreadable command must refuse")
	}
}
