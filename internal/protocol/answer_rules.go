package protocol

// answer_rules.go — answering a permission question can also write the rule that makes the next one
// unnecessary. The three rule answers ("approve for this loop", "always allow", "always deny") are the
// person's act at the human door: the rule is authored through the SAME policy authoring door and boundary a
// person's own `aeon policy add` uses, as the operator, with the connection's actor on the audit row. The
// step itself only ever sees "approve" or "deny" (the runtime's one-call approval).
//
// What a rule names, from the blocked call itself (never from anything a model wrote as prose):
//   - a shell command: that exact command (command_access). A command containing `*` or a newline is refused
//     for the rule answers — a pattern or a multi-line script is not "this command" — and the question stays
//     open so the person can answer approve or deny instead.
//   - any other tool: that tool (tool_access).
// "for this loop" targets the loop; "always" targets the agent the step runs as.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// IsRuleAnswer reports whether an answer is one of the three that also author a rule.
func IsRuleAnswer(answer string) bool {
	switch answer {
	case runtime.AnswerApproveLoop, runtime.AnswerAlwaysAllow, runtime.AnswerAlwaysDeny:
		return true
	}
	return false
}

// resumeVerb maps any permission answer to what the step is resumed with.
func resumeVerb(answer string) string {
	switch answer {
	case runtime.AnswerApprove, runtime.AnswerApproveLoop, runtime.AnswerAlwaysAllow:
		return runtime.AnswerApprove
	}
	return runtime.AnswerDeny
}

func offered(options []string, answer string) bool {
	for _, o := range options {
		if o == answer {
			return true
		}
	}
	return false
}

// blockedUse finds the tool_use block a permission question is about.
func blockedUse(messages []model.Message, id string) (model.Block, bool) {
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolUse && b.ID == id {
				return b, true
			}
		}
	}
	return model.Block{}, false
}

// permissionRule builds the authoring params one rule answer stands for.
func permissionRule(answer, subject, loopSlug string, use model.Block) (PolicyAddParams, error) {
	action := "allow"
	if answer == runtime.AnswerAlwaysDeny {
		action = "deny"
	}
	target := PolicyTargetP{Kind: "agent", ID: subject}
	if answer == runtime.AnswerApproveLoop {
		if loopSlug == "" {
			return PolicyAddParams{}, fmt.Errorf("this step does not belong to a loop, so there is no loop to approve for — answer approve, or always allow")
		}
		target = PolicyTargetP{Kind: "loop", ID: loopSlug}
	}
	typ, key, spec := "tool_access", use.Name, map[string]string{"toolName": use.Name, "state": action}
	if use.Name == "bash" {
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(use.Input, &in) != nil || strings.TrimSpace(in.Command) == "" {
			return PolicyAddParams{}, fmt.Errorf("the command of this call could not be read, so no rule can name it")
		}
		if strings.ContainsAny(in.Command, "*\n\r") {
			return PolicyAddParams{}, fmt.Errorf("this command contains a wildcard or a line break, so a rule for it would mean more than this command — answer approve or deny instead")
		}
		typ, key, spec = "command_access", in.Command, map[string]string{"command": in.Command, "state": action}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return PolicyAddParams{}, err
	}
	sum := sha256.Sum256([]byte(typ + "\x00" + target.Kind + "\x00" + target.ID + "\x00" + key + "\x00" + action))
	return PolicyAddParams{
		ID: "answer." + hex.EncodeToString(sum[:])[:16], Type: typ, Target: target, Action: action, Spec: raw, Source: "operator",
	}, nil
}

// authorRuleAnswer writes the rule a rule-answer stands for, before the question is answered. A refusal (no
// authoring door, an unreadable call, the boundary's own refusal) leaves the question pending and says why.
func (t *Table) authorRuleAnswer(ctx context.Context, actor Actor, answer, subject, loopSlug string, use model.Block) error {
	if t.PolicyAuthoring == nil {
		return ErrPolicyAuthoringRefused.WithWhat("this daemon has no policy authoring door, so %q cannot write a rule — answer approve or deny", answer)
	}
	params, err := permissionRule(answer, subject, loopSlug, use)
	if err != nil {
		return ErrPolicyAuthoringRefused.WithWhat("%v", err)
	}
	if _, err := t.PolicyAuthoring.Add(ctx, actor, params); err != nil {
		// A rule that already stands is not a failure: the same answer given twice writes it once.
		if strings.Contains(err.Error(), "already") {
			return nil
		}
		return err
	}
	return nil
}
