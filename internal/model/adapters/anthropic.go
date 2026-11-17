package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/model"
)

// # The Anthropic adapter
//
// Where this provider differs from the canonical format, and from the other
// adapter in this directory:
//
//   - **`system` is hoisted** to a top-level parameter, not a message
//   - content is a **typed block array**, not a string
//   - tool arguments are an **object** on the wire, not a JSON string
//   - **tool results merge**: N results for one turn become ONE user message.
//     This is the row most easily missed, because it works with one tool call
//     and fails with two
//   - the header is **`x-api-key`**, not `Authorization: Bearer`. Copying the
//     other adapter's header set is the natural mistake and it fails with a
//     confusing 401
//   - streaming is `content_block_start`/`_delta`/`_stop` with a complete id up
//     front, where OpenAI keys fragments by array index

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com/v1"
	anthropicVersion        = "2023-06-01"
)

type anthropic struct {
	mu sync.Mutex

	// blocks tracks the in-flight content blocks, keyed by the wire's index.
	blocks map[int]*anthropicBlock
	order  []int

	usage      model.Usage
	stopReason string
	dropped    int
	owedErrors []string

	// thinkingDropped counts REQUEST-direction drops (S18 T5, M4): thinking
	// blocks this provider did not produce, withheld from the wire and
	// counted — never silently skipped. Distinct from `dropped`, which is
	// response-direction content that mapped to no canonical kind; a reader
	// asking "why did this turn behave differently" needs to know which
	// direction lost the content (O1).
	thinkingDropped int

	// attachmentDropped counts REQUEST-direction attachment drops (S69):
	// image or document blocks withheld from the wire because they arrived
	// in REFERENCE form (Document+Rev, no Data) — which is a bug upstream of
	// the send-time expansion (AT5), never a licence to send a half block.
	// Distinct from both `dropped` (response direction) and
	// `thinkingDropped`: the three numbers answer different questions and
	// must not blur (M4, O1).
	attachmentDropped int

	// thinking holds the COMPLETED thinking blocks, with their signatures.
	//
	// Exposed by Finalise because S5's runtime must persist them into the
	// transcript, and **a thinking delta yielded for display is not enough**:
	// display text is lossy and the signature never appears in it.
	thinking []model.Block
}

type anthropicBlock struct {
	kind      string
	id        string
	name      string
	text      strings.Builder
	signature strings.Builder
	args      strings.Builder
}

func newAnthropic() *anthropic { return &anthropic{blocks: map[int]*anthropicBlock{}} }

func (*anthropic) Provider() string { return ProviderAnthropic }

// --- ToWire ------------------------------------------------------------------

func (a *anthropic) ToWire(req model.ModelRequest) (*http.Request, error) {
	route := req.Route
	if err := req.Validate(); err != nil {
		return nil, err
	}

	system, messages, thinkingDropped, attachmentDropped, err := anthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	if thinkingDropped > 0 {
		a.mu.Lock()
		a.thinkingDropped += thinkingDropped
		a.mu.Unlock()
	}
	if attachmentDropped > 0 {
		a.mu.Lock()
		a.attachmentDropped += attachmentDropped
		a.mu.Unlock()
	}

	outputCap := route.MaxOutputTokens
	if req.MaxOutputTokens > 0 {
		outputCap = req.MaxOutputTokens
	}
	if outputCap <= 0 {
		// M16: this provider REJECTS a call without it, so a zero is a
		// configuration error caught before the wire rather than a 400.
		return nil, fmt.Errorf("model: route %q has no MaxOutputTokens, which this provider requires",
			route.ID)
	}

	body := map[string]any{
		"model":      route.Model,
		"messages":   messages,
		"stream":     true,
		"max_tokens": outputCap,
	}
	// Omitted ENTIRELY when there is none — an empty string is a system prompt
	// that says nothing, which is different from not having one.
	if system != "" {
		body["system"] = system
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.ThinkingBudgetTokens > 0 {
		body["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": req.ThinkingBudgetTokens,
		}
	}
	// `Route.ReasoningEffort` is DELIBERATELY not mapped here.
	//
	// It is OpenAI's parameter. Anthropic's equivalent dial is the thinking
	// budget above, and the two are not interchangeable: one is an enumerated
	// effort, the other a token allowance. Forwarding the field anyway would
	// produce a 400 about a parameter the person set for a different provider
	// — so a route carrying it is simply ignored here, and a test asserts no
	// `reasoning_effort` key reaches this body.
	//
	// Mapping one onto the other is a translation this layer should not invent:
	// "medium" is not a number of tokens, and picking one would make Aeon the
	// author of a quality/cost tradeoff the person never chose.
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tool := map[string]any{"name": t.Name}
			if t.Description != "" {
				tool["description"] = t.Description
			}
			// `input_schema`, not `parameters` — the other provider's name.
			if len(t.Parameters) > 0 {
				tool["input_schema"] = t.Parameters
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	if len(route.ExtraBody) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(route.ExtraBody, &extra); err != nil {
			return nil, fmt.Errorf("model: route %q has invalid extraBody: %w", route.ID, err)
		}
		for k, v := range extra {
			body[k] = v
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	base := strings.TrimSuffix(route.BaseURL, "/")
	if base == "" {
		base = defaultAnthropicBaseURL
	}
	httpReq, err := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	// **`x-api-key`, and no Authorization header.**
	httpReq.Header.Set("x-api-key", route.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	return httpReq, nil
}

// anthropicMessages hoists the system prompt and merges tool results.
//
// Returns the system text, the message array, the count of thinking blocks
// WITHHELD from this wire because their producer is not this provider
// (S18 T5, M27 — counted here so the number can reach the record, M4/O1),
// and the count of image/document blocks withheld because they arrived in
// REFERENCE form — a bug upstream of the send-time expansion (S69, AT5),
// counted in its own number for the same reason thinking is.
func anthropicMessages(msgs []model.Message) (string, []any, int, int, error) {
	var systemParts []string
	var out []any
	var thinkingDropped int
	var attachmentDropped int

	// pendingResults accumulates tool_result blocks so consecutive ones collapse
	// into a single user message.
	var pendingResults []any
	flushResults := func() {
		if len(pendingResults) == 0 {
			return
		}
		// **ONE user message carrying every result**, in order. N separate
		// messages are rejected by this provider — and it works with one tool
		// call, which is why this is the row that gets missed.
		out = append(out, map[string]any{"role": "user", "content": pendingResults})
		pendingResults = nil
	}

	for _, m := range msgs {
		if m.Role == model.RoleSystem {
			for _, b := range m.Content {
				if b.Kind == model.BlockText {
					systemParts = append(systemParts, b.Text)
				}
			}
			continue
		}

		var content []any
		for _, b := range m.Content {
			switch b.Kind {
			case model.BlockText:
				content = append(content, map[string]any{"type": "text", "text": b.Text})

			case model.BlockThinking:
				// **Only to its producer** (M27). A signature this provider
				// did not issue is not replayable here, and replaying one is
				// a bet on somebody else's validator. The rule keys on WHO
				// PRODUCED the turn, never on whether this target supports
				// thinking (O6): a reasoning-capable target still cannot
				// receive a foreign signature. A nil provenance — a row from
				// before M26 — proves nothing, and proves are not sent;
				// dropped and COUNTED (M4), never silently skipped.
				if m.Provenance == nil || m.Provenance.Provider != ProviderAnthropic {
					thinkingDropped++
					continue
				}
				// **Verbatim, signature included** (M17). This provider rejects
				// a tool-use turn whose prior thinking was altered or dropped,
				// and because S7 recovers a step by replaying a stored
				// transcript, a signature mangled here fails much later and
				// somewhere else.
				block := map[string]any{"type": "thinking", "thinking": b.Text}
				if b.Signature != "" {
					block["signature"] = b.Signature
				}
				content = append(content, block)

			case model.BlockToolUse:
				// `Input` as an OBJECT, not a string. Raw JSON marshals as the
				// object it is, which is the whole reason `Input` is
				// `json.RawMessage` rather than `string`.
				input := b.Input
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    b.ID,
					"name":  b.Name,
					"input": input,
				})

			case model.BlockToolResult:
				result := map[string]any{
					"type":        "tool_result",
					"tool_use_id": b.ToolUseID,
					"content":     b.Content,
				}
				if b.IsError {
					result["is_error"] = true
				}
				pendingResults = append(pendingResults, result)

			case model.BlockImage, model.BlockDocument:
				// S69 (AT6). A block arriving here in REFERENCE form — no
				// bytes, a pinned document/rev — is a bug UPSTREAM: the
				// daemon's send-time door expands references to bytes
				// immediately before the adapters (AT5), and a transcript
				// that skipped that door must not put half a block on the
				// wire. Dropped and COUNTED (M4) in its own request-
				// direction number (O1), exactly as the thinking case above
				// drops and counts — never silently skipped, never
				// half-sent. `countDropped` in spirit; its own counter in
				// fact, because `dropped` belongs to the response
				// direction.
				if b.Data == "" {
					attachmentDropped++
					continue
				}
				// **A sibling block, never sentence decoration** (AT7): the
				// bytes cross in their own typed block beside the person's
				// words, which stay one untouched `text` block.
				content = append(content, anthropicAttachmentBlock(b))
			}
		}

		if len(content) == 0 {
			// A message of nothing but tool results contributes only to the
			// pending merge.
			continue
		}
		if len(pendingResults) > 0 {
			if m.Role == model.RoleUser {
				// Results LEAD the turn they answer (S31 T4, LW2): a steered
				// turn is "these calls did not run" and THEN the person's
				// words, and one provider turn is where both belong — this
				// API accepts tool_result blocks mixed with text in a single
				// user content list, and splitting them into two consecutive
				// user turns would let a reader (or a stricter gateway)
				// mis-attribute the text to the wrong call run.
				content = append(pendingResults, content...)
				pendingResults = nil
			} else {
				// A run of results ends HERE: results answered to the
				// assistant belong to a user turn of their own — never
				// smuggled into the following assistant message.
				flushResults()
			}
		}
		out = append(out, map[string]any{"role": string(m.Role), "content": content})
	}
	flushResults()

	if len(out) == 0 {
		return "", nil, thinkingDropped, attachmentDropped, fmt.Errorf("model: every message mapped to nothing on this provider")
	}
	return strings.Join(systemParts, "\n\n"), out, thinkingDropped, attachmentDropped, nil
}

// anthropicAttachmentBlock renders an image or document block that has ALREADY
// been expanded to bytes (the either/or of AT5 — this is only ever called with
// Data non-empty; the reference form is dropped and counted at the call site).
//
// The shapes are the provider's documented ones: an `image` is a base64 source
// and nothing else; a `document` carries the same source plus — only when
// there is one — a `title`. An empty title is omitted rather than sent empty:
// a paste has no name, and `"title": ""` is an invention about it. The image
// has no title field on this wire, so a name never decorates it.
func anthropicAttachmentBlock(b model.Block) map[string]any {
	block := map[string]any{
		"type": string(b.Kind), // "image" or "document" — the wire names both
		"source": map[string]any{
			"type":       "base64",
			"media_type": b.MediaType,
			"data":       b.Data,
		},
	}
	if b.Kind == model.BlockDocument && b.Name != "" {
		block["title"] = b.Name
	}
	return block
}

// --- FromWire ----------------------------------------------------------------

type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string  `json:"type"`
		Text        string  `json:"text"`
		Thinking    string  `json:"thinking"`
		Signature   string  `json:"signature"`
		PartialJSON string  `json:"partial_json"`
		StopReason  *string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
}

func (a *anthropic) FromWire(ctx context.Context, frames <-chan model.Frame) <-chan model.Delta {
	out := make(chan model.Delta)

	go func() {
		defer close(out)
		emit := func(d model.Delta) bool {
			select {
			case out <- d:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for frame := range frames {
			if frame.Data == "" {
				continue
			}
			var ev anthropicEvent
			if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
				a.countDropped()
				continue
			}

			switch ev.Type {
			case "message_start":
				if ev.Message != nil && ev.Message.Usage != nil {
					a.accumulateUsage(ev.Message.Usage)
				}

			case "content_block_start":
				if ev.ContentBlock == nil {
					a.countDropped()
					continue
				}
				switch ev.ContentBlock.Type {
				case "text", "thinking", "tool_use":
					a.startBlock(ev.Index, ev.ContentBlock.Type, ev.ContentBlock.ID, ev.ContentBlock.Name)
				default:
					// M4: content mapping to none of the four canonical kinds —
					// `redacted_thinking`, or whatever arrives next — is
					// dropped and COUNTED. A silent drop is how a block
					// disappears between two releases and nobody can say when.
					a.countDropped()
				}

			case "content_block_delta":
				if ev.Delta == nil {
					a.countDropped()
					continue
				}
				switch ev.Delta.Type {
				case "text_delta":
					a.appendText(ev.Index, ev.Delta.Text)
					if ev.Delta.Text != "" {
						if !emit(model.Delta{Kind: model.DeltaText, Text: ev.Delta.Text}) {
							return
						}
					}
				case "thinking_delta":
					a.appendText(ev.Index, ev.Delta.Thinking)
					if ev.Delta.Thinking != "" {
						if !emit(model.Delta{Kind: model.DeltaThinking, Text: ev.Delta.Thinking}) {
							return
						}
					}
				case "signature_delta":
					// Accumulated and **never yielded**. The signature is
					// replay metadata, not display content — a client rendering
					// it would show a reader a base64 blob, and a client
					// treating it as text would lose it on the way to storage.
					a.appendSignature(ev.Index, ev.Delta.Signature)
				case "input_json_delta":
					a.appendArgs(ev.Index, ev.Delta.PartialJSON)
				default:
					a.countDropped()
				}

			case "content_block_stop":
				if d, ok := a.finishBlock(ev.Index); ok {
					if !emit(d) {
						return
					}
				}

			case "message_delta":
				if ev.Usage != nil {
					a.accumulateUsage(ev.Usage)
				}
				if ev.Delta != nil && ev.Delta.StopReason != nil {
					a.setStopReason(*ev.Delta.StopReason)
				}

			case "message_stop":
				if !emit(model.Delta{Kind: model.DeltaStop, Reason: a.currentStopReason()}) {
					return
				}

			case "ping":
				// A keep-alive. Explicitly ignored rather than counted as
				// dropped: it carries nothing, and counting it would make
				// `dropped` a measure of how long a stream took.

			case "error":
				a.countDropped()

			default:
				a.countDropped()
			}
		}
	}()

	return out
}

func (a *anthropic) startBlock(index int, kind, id, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.blocks[index]; !exists {
		a.order = append(a.order, index)
	}
	// The id and name arrive COMPLETE here, unlike OpenAI's first-fragment-only
	// id — so there is nothing to accumulate for them.
	a.blocks[index] = &anthropicBlock{kind: kind, id: id, name: name}
}

func (a *anthropic) appendText(index int, s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b := a.blocks[index]; b != nil {
		b.text.WriteString(s)
	}
}

func (a *anthropic) appendSignature(index int, s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b := a.blocks[index]; b != nil {
		b.signature.WriteString(s)
	}
}

func (a *anthropic) appendArgs(index int, s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b := a.blocks[index]; b != nil {
		b.args.WriteString(s)
	}
}

// finishBlock completes a block and returns a delta for the kinds that have one.
func (a *anthropic) finishBlock(index int) (model.Delta, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	b := a.blocks[index]
	if b == nil {
		return model.Delta{}, false
	}
	delete(a.blocks, index)

	switch b.kind {
	case "tool_use":
		args := b.args.String()
		input := json.RawMessage(args)
		if args == "" {
			input = json.RawMessage(`{}`)
		} else if !json.Valid([]byte(args)) {
			input = json.RawMessage(`{}`)
			a.owedErrors = append(a.owedErrors, b.id)
		}
		return model.Delta{Kind: model.DeltaToolUse, Block: &model.Block{
			Kind:  model.BlockToolUse,
			ID:    b.id,
			Name:  b.name,
			Input: input,
		}}, true

	case "thinking":
		// Kept for Finalise, with the signature. Not emitted as a delta: the
		// thinking text was already streamed incrementally, and emitting the
		// completed block again would double it for a consumer that
		// accumulates.
		a.thinking = append(a.thinking, model.ThinkingBlock(b.text.String(), b.signature.String()))
		return model.Delta{}, false

	default:
		// Text was already streamed delta by delta.
		return model.Delta{}, false
	}
}

func (a *anthropic) accumulateUsage(u *anthropicUsage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Only when present. Usage arrives in TWO places on this provider —
	// `message_start` for input and `message_delta` for output — so each call
	// must add rather than replace.
	if u.InputTokens != nil {
		a.usage.InputTokens = u.InputTokens
	}
	if u.OutputTokens != nil {
		a.usage.OutputTokens = u.OutputTokens
	}
	if u.CacheCreationInputTokens != nil {
		a.usage.CacheWriteTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens != nil {
		a.usage.CacheReadTokens = u.CacheReadInputTokens
	}
}

func (a *anthropic) countDropped() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropped++
}

func (a *anthropic) setStopReason(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopReason = reason
}

func (a *anthropic) currentStopReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopReason
}

func (a *anthropic) Finalise() (model.Usage, string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage, a.stopReason, a.dropped
}

// ThinkingDropped reports the request-direction drops for THIS request
// (S18 T5): thinking blocks withheld because their producer was not this
// provider. The client collects it into the call's accounting, and from
// there it reaches the step's record — a drop that counts nowhere is the
// silent drop M4 forbids wearing a new coat (O1).
func (a *anthropic) ThinkingDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinkingDropped
}

// AttachmentDropped reports the S69 request-direction attachment drops: image
// and document blocks withheld from this wire because they reached the adapter
// still in reference form (AT5's expansion door was skipped upstream). Its own
// number, beside ThinkingDropped's and `dropped`'s — the three answer
// different questions and must not blur (M4, O1).
func (a *anthropic) AttachmentDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attachmentDropped
}

// CompletedThinking returns the thinking blocks, signatures included.
//
// **S5's runtime persists these into the transcript**, and S7 replays that
// transcript to recover a step. The streamed thinking deltas are for display
// and are lossy — the signature never appears in them — so this is the only
// path by which a signature reaches storage intact.
func (a *anthropic) CompletedThinking() []model.Block {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]model.Block(nil), a.thinking...)
}

// OwedToolErrors mirrors the other adapter's: tool calls whose arguments would
// not parse, each owed a `tool_result` with IsError (D6).
func (a *anthropic) OwedToolErrors() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.owedErrors...)
}
