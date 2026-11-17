package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// QuestionParams is the request the runtime persists when it suspends. A nil
// Options slice means free-form; a present slice means a structured choice.
type QuestionParams struct {
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	// Questions is the multi-question form: several questions asked in ONE call and answered in one go.
	Questions []SubQuestion `json:"questions,omitempty"`
}

// SubQuestion is one question of a form.
type SubQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
}

// ParseQuestion validates arguments before a question is persisted. The tool's
// Run function is intentionally unreachable; the runtime calls this parser.
func ParseQuestion(input json.RawMessage) (QuestionParams, error) {
	var raw struct {
		Text      string        `json:"text"`
		Options   *[]string     `json:"options"`
		Kind      string        `json:"kind"`
		Questions []SubQuestion `json:"questions"`
	}
	if err := json.Unmarshal(input, &raw); err != nil {
		return QuestionParams{}, fmt.Errorf("question arguments are not valid JSON: %w", err)
	}
	if len(raw.Questions) > 8 {
		return QuestionParams{}, fmt.Errorf("a question form holds at most 8 questions")
	}
	for i, q := range raw.Questions {
		if strings.TrimSpace(q.Text) == "" {
			return QuestionParams{}, fmt.Errorf("questions[%d].text is required", i)
		}
	}
	if len(raw.Questions) == 1 && strings.TrimSpace(raw.Text) == "" {
		raw.Text, raw.Options = raw.Questions[0].Text, &raw.Questions[0].Options
	}
	if len(raw.Questions) > 1 && strings.TrimSpace(raw.Text) == "" {
		raw.Text = raw.Questions[0].Text
	}
	if strings.TrimSpace(raw.Text) == "" {
		return QuestionParams{}, fmt.Errorf("question text is required")
	}
	kind := raw.Kind
	if kind == "" {
		kind = "free"
	}
	switch kind {
	case "clarify", "gate", "permission", "free":
	default:
		return QuestionParams{}, fmt.Errorf("question kind %q is invalid; use clarify, gate, permission, or free", kind)
	}
	p := QuestionParams{Text: raw.Text, Kind: kind}
	if len(raw.Questions) > 1 {
		p.Questions = raw.Questions
	}
	if raw.Options != nil {
		if len(*raw.Options) == 0 {
			return QuestionParams{}, fmt.Errorf("question options must contain at least one choice when present")
		}
		p.Options = append([]string{}, (*raw.Options)...)
		for _, option := range p.Options {
			if strings.TrimSpace(option) == "" {
				return QuestionParams{}, fmt.Errorf("question options cannot contain an empty choice")
			}
		}
	}
	return p, nil
}

// Question is a declaration only. K18 requires the runtime to suspend before
// this tool's execution path can be reached.
func Question() Tool {
	return Tool{Class: RerunSafe, Spec: model.ToolSpec{
		Name:        "question",
		Description: "Ask the user only when a decision is theirs: a trade-off, preference, irreversible action, or ambiguity with materially different outcomes. Do not ask what the code, task, or another tool can answer. Do not ask permission for work the task already requested. Several questions at once are ONE call: pass `questions` (an array of {text, options}) and the person answers them all in one go. Never send more than one question call in a turn. Asking suspends this step, possibly for hours; the answer arrives as this tool's result when the step resumes.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"the question to show the user (or the intro of a questions form)"},"questions":{"type":"array","maxItems":8,"description":"several questions asked together and answered in one go; each may carry its own options","items":{"type":"object","properties":{"text":{"type":"string"},"options":{"type":"array","items":{"type":"string"},"minItems":1}},"required":["text"],"additionalProperties":false}},"options":{"type":"array","items":{"type":"string"},"minItems":1,"description":"possible responses; omit for a free-form answer"},"kind":{"type":"string","enum":["clarify","gate","permission","free"],"default":"free"}},"required":[],"additionalProperties":false}`),
	}, Run: func(context.Context, ToolContext, json.RawMessage) Result {
		panic("K18: question is recognised by name and suspends in the runtime; executing it is a bug")
	}}
}
