package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// ask_agent.go is S70 MB1's entrance: an agent of this loop asking another
// agent of THIS loop by name (MB2's lane check and the frozen-set judgement
// run above this door, in the runtime — this file owns only the shape of
// what may be said). Like `question`, the tool's Run is unreachable: the
// runtime recognises it by name, validates through ParseAskAgent BEFORE
// anything persists (AT1), and owns every effect (K18's discipline).
//
// The industry arrived here from its own direction: OpenAI's handoffs are
// "represented as tools to the LLM", and the schema is what makes a
// model-initiated ask auditable. Ours is closed at both ends —
// additionalProperties:false on the spec, and a parser that refuses unknown
// keys BY NAME so a typo'd argument is a sentence, not a silent absence.
//
// Registered is not granted (S11): DefaultGrant does NOT contain ask_agent,
// and a loop runs without it until an operator grants tool_access — the
// pin test Toolset_test says so with the registry's exact list.

// AskAgentParams is the validated request the runtime files. Thread nil
// means "begin a conversation" (the store's 0); Wait nil means true — the
// asking agent wants the answer, and queue-only is the opt-in (the board's
// discipline is the default shape).
type AskAgentParams struct {
	To     string
	Text   string
	Thread *int64
	Wait   *bool
}

// ParseAskAgent validates arguments before any row exists. The refusals name
// the field and the requirement (AGENTS §5), and unknown fields are named
// one by one — a model that invents `priority` learns the vocabulary, it
// does not discover that invented fields become nothing.
func ParseAskAgent(input json.RawMessage) (AskAgentParams, error) {
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	var raw struct {
		To     *string `json:"to"`
		Text   *string `json:"text"`
		Thread *int64  `json:"thread"`
		Wait   *bool   `json:"wait"`
	}
	if err := dec.Decode(&raw); err != nil {
		// Name every unknown key the strict pass hid behind one error: the
		// second pass cannot fail on content, only on shape, and shape was
		// already refused — so the sentence below is safe to compose.
		var probe map[string]json.RawMessage
		if e := json.Unmarshal(input, &probe); e == nil {
			var unknown []string
			for k := range probe {
				switch k {
				case "to", "text", "thread", "wait":
				default:
					unknown = append(unknown, k)
				}
			}
			if len(unknown) > 0 {
				return AskAgentParams{}, fmt.Errorf("ask_agent arguments refused: unknown field(s) %v — the vocabulary is to, text, thread, wait", unknown)
			}
		}
		return AskAgentParams{}, fmt.Errorf("ask_agent arguments are not valid: %w", err)
	}
	if raw.To == nil || trimEmpty(*raw.To) {
		return AskAgentParams{}, fmt.Errorf("ask_agent refused: to is required — an ask is addressed to one named agent, never broadcast (D8)")
	}
	if raw.Text == nil || trimEmpty(*raw.Text) {
		return AskAgentParams{}, fmt.Errorf("ask_agent refused: text is required — an ask with nothing to say answers nothing")
	}
	if raw.Thread != nil && *raw.Thread < 1 {
		return AskAgentParams{}, fmt.Errorf("ask_agent refused: thread must be a positive thread id when present, got %d", *raw.Thread)
	}
	p := AskAgentParams{To: *raw.To, Text: *raw.Text, Thread: raw.Thread, Wait: raw.Wait}
	return p, nil
}

func trimEmpty(s string) bool { return strings.TrimSpace(s) == "" }

// AskAgent is the registry entry. Run panics: the runtime recognises the
// tool by name and no execution path through this package is allowed to
// file a row (the question tool's K18, inherited).
func AskAgent() Tool {
	return Tool{Class: RerunDetectable, Spec: model.ToolSpec{
		Name:        "ask_agent",
		Description: "Ask one other agent of THIS loop — by its exact agent name — when only its judgement answers you: its findings, its read of the work, a decision it holds. The ask is filed as a durable message the addressee receives; with wait (default true) your step suspends until the answer arrives, and the answer returns as this tool's result when the step resumes. Addressed to one agent, never to everyone; a loop's lanes are the peers.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"the exact agent name of one agent in this loop's blueprint"},"text":{"type":"string","description":"the ask, in full — the addressee sees the loop's goal around it, not your transcript"},"thread":{"type":"integer","minimum":1,"description":"join an existing conversation by its thread id; omit to begin one"},"wait":{"type":"boolean","default":true,"description":"true (default) suspends this step until the answer arrives; false files the ask and keeps working"}},"required":["to","text"],"additionalProperties":false}`),
	}, Run: func(context.Context, ToolContext, json.RawMessage) Result {
		panic("K18-inherited: ask_agent is recognised by name and files in the runtime; executing it is a bug")
	}}
}
