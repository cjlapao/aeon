package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/model"
)

// # The OpenAI **Responses** adapter — `/v1/responses`
//
// A second wire format for the same provider, and it exists for one measured
// reason: `/v1/chat/completions` **refuses function tools while reasoning is
// on**, and this endpoint does not.
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// Every Aeon step offers tools, so on chat/completions a step must run with
// reasoning off. Here it does not have to.
//
// # Why this is a separate implementation rather than a flag on the other one
//
// Almost nothing is shared. In both directions:
//
//	                  chat/completions              responses
//	request field     messages                      input
//	user text         "content": "..."              [{"type":"input_text"}]
//	assistant text    "content": "..."              [{"type":"output_text"}]
//	a tool call       choices[].message.tool_calls  a TOP-LEVEL function_call item
//	a tool result     {"role":"tool", …}            a TOP-LEVEL function_call_output
//	tool declaration  {"type":"function",           {"type":"function",
//	                   "function":{name,…}}          name, …}     ← flat
//	reasoning         "reasoning_effort": "low"     "reasoning": {"effort":"low"}
//	output cap        max_completion_tokens         max_output_tokens
//	stream events     one chunk shape               ~10 named response.* events
//	usage             prompt_/completion_tokens     input_/output_tokens
//
// A flag inside the other adapter would be a function with two bodies and a
// shared `if` at every line. The dispatcher in `openai.go` picks one per call.
//
// # What a route must set
//
//	aeon routes set --id openai --provider openai --model gpt-6.1-sol \
//	  --openai-endpoint responses --reasoning-effort medium
//
// # ⚠ UNVERIFIED against api.openai.com
//
// Written from the documented shapes, like every other fixture in this slice
// (S3's I7: no test contacts a provider). The field PATHS below are the claim
// being made, and the first live call is what confirms them — which is why
// `aeon-model.md` QAM-33 exists and says so.
//
// What is most likely to be wrong, in order: the exact `response.*` event names
// for reasoning summaries; whether `store: false` needs `include` to round-trip
// reasoning; and whether `function_call_output` accepts a non-string `output`.

type openAIResponses struct {
	mu sync.Mutex

	// calls accumulates function calls by their output index, which is what
	// every `response.function_call_arguments.*` event is keyed by.
	calls map[int]*responsesCall
	order []int

	usage      model.Usage
	stopReason string
	dropped    int
	owedErrors []string

	// thinkingDropped counts REQUEST-direction thinking drops (S18 T5, M4)
	// separately from `dropped`, which counts response-direction content
	// mapping to no canonical kind. The two omissions answer different
	// questions and must not blur (O1).
	thinkingDropped int

	// attachmentDropped counts REQUEST-direction attachment drops (S69):
	// image or document blocks withheld because they arrived in REFERENCE
	// form (no bytes, a pinned document/rev — an upstream bug against the
	// send-time expansion, AT5) or rode a role whose content parts carry no
	// such shape. Its own number, apart from thinkingDropped and `dropped`
	// (M4, O1): the three answer different questions.
	attachmentDropped int

	// flushedIDs names every call id already yielded (Q11) — same rule,
	// same reason, both wire formats.
	flushedIDs map[string]bool
}

type responsesCall struct {
	callID string
	name   string
	args   strings.Builder
	// done marks a call already yielded, so `response.output_item.done` and the
	// end of the stream cannot emit it twice.
	done bool
}

func newOpenAIResponses() *openAIResponses {
	return &openAIResponses{calls: map[int]*responsesCall{}, flushedIDs: map[string]bool{}}
}

func (*openAIResponses) Provider() string { return ProviderOpenAI }

// --- ToWire ------------------------------------------------------------------

func (a *openAIResponses) ToWire(req model.ModelRequest) (*http.Request, error) {
	route := req.Route
	if err := req.Validate(); err != nil {
		return nil, err
	}

	input, dropped, attachDropped, err := responsesInput(req.Messages, !wireSays(route, model.CapSystemRole))
	if err != nil {
		return nil, err
	}
	if dropped > 0 {
		// Request-direction: these are the thinking blocks this wire cannot
		// carry, counted into their own number (S18 T5), not into `dropped`
		// — which belongs to the response direction.
		a.mu.Lock()
		a.thinkingDropped += dropped
		a.mu.Unlock()
	}
	if attachDropped > 0 {
		// S69: the same discipline for withheld attachments — request
		// direction, own number (M4, O1).
		a.mu.Lock()
		a.attachmentDropped += attachDropped
		a.mu.Unlock()
	}

	outputCap := route.MaxOutputTokens
	if req.MaxOutputTokens > 0 {
		outputCap = req.MaxOutputTokens
	}
	if outputCap <= 0 {
		return nil, fmt.Errorf("model: route %q has no MaxOutputTokens", route.ID)
	}

	body := map[string]any{
		"model":  route.Model,
		"input":  input,
		"stream": true,
		// `max_output_tokens`, this endpoint's name for the cap.
		"max_output_tokens": outputCap,
		// # `store: false`, deliberately
		//
		// The default is true, which keeps the conversation on the provider's
		// side so later calls can reference it by id. Aeon already owns the
		// transcript — it is the thing S4 persists and S7 replays — so storing
		// a second copy on a server buys nothing and leaves a copy of
		// somebody's repository contents where they did not put it.
		//
		// The cost is that reasoning items cannot be referenced back, so a
		// reasoning model re-reasons each turn rather than resuming its own
		// chain. That is a limitation worth stating rather than a bug: the
		// alternative is server-side retention nobody asked for.
		"store": false,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if effort := req.EffectiveReasoningEffort(); effort != "" {
		// An OBJECT here, not a scalar — the other endpoint's spelling is
		// `reasoning_effort: "low"`, and sending that shape to this endpoint is
		// a 400 about an unknown field.
		body["reasoning"] = map[string]any{"effort": effort}
	}
	// Absence-gated like the chat wire (S18 T6, M13): a route declaring no
	// tools gets the key absent, never `tools: []`.
	if len(req.Tools) > 0 && wireSays(route, model.CapTools) {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			// FLAT, unlike chat/completions' nested `function` object. This is
			// the difference most likely to be copied wrong, because the two
			// look so similar.
			tool := map[string]any{"type": "function", "name": t.Name}
			if t.Description != "" {
				tool["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				tool["parameters"] = t.Parameters
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	// ExtraBody merges LAST (M11), so it can override anything above —
	// including `store`, for somebody who does want provider-side retention.
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
		base = defaultOpenAIBaseURL
	}
	httpReq, err := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if route.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	}
	return httpReq, nil
}

// responsesInput turns a canonical transcript into this endpoint's `input`.
//
// # The shape that differs most
//
// A tool call and a tool result are **top-level items**, siblings of the
// messages rather than content inside them. So one canonical user message
// carrying three tool results becomes three items, and an assistant message
// carrying text and a tool call becomes two.
//
// Returns the number of blocks that could not be represented, counted into
// `dropped` for the same reason the response direction counts them (M4): a
// block silently discarded is a transcript that means something different from
// the one that was stored; and separately the number of image/document blocks
// withheld for arriving in reference form or riding a role whose parts carry no
// such shape (S69) — request-direction counts, kept apart (O1).
func responsesInput(messages []model.Message, foldSystem bool) ([]any, int, int, error) {
	var out []any
	var dropped int
	var attachDropped int

	// S18 T6 (M13): the system fold is the chat wire's rule applied here
	// too — declared absence translated on the wire into the first user
	// item's leading part; the canonical transcript keeps its system turn
	// (M1). One provider, two formats, one flag's behaviour on both.
	var pending string
	if foldSystem {
		var kept []model.Message
		for _, m := range messages {
			if m.Role == model.RoleSystem {
				for _, b := range m.Content {
					if b.Kind == model.BlockText && b.Text != "" {
						if pending != "" {
							pending += "\n\n"
						}
						pending += b.Text
					}
				}
				continue
			}
			kept = append(kept, m)
		}
		messages = kept
	}
	folded := false

	for _, m := range messages {
		contentType := "input_text"
		if m.Role == model.RoleAssistant {
			contentType = "output_text"
		}

		// # Items come out in BLOCK order, not grouped by kind
		//
		// The item order IS the turn's order to the provider. An assistant turn
		// that said "reading" and then called a tool must produce the message
		// first — grouping the calls together and appending the text afterwards
		// (which an earlier version did, caught by a test) tells the provider
		// the model spoke after acting.
		//
		// Consecutive text blocks collapse into ONE message item with several
		// content parts, which is what a message with two paragraphs is.
		var parts []any
		if foldSystem && !folded && pending != "" && m.Role == model.RoleUser {
			parts = append(parts, map[string]any{"type": "input_text", "text": pending + "\n\n"})
			folded = true
		}
		flush := func() {
			if len(parts) > 0 {
				out = append(out, map[string]any{"role": string(m.Role), "content": parts})
				parts = nil
			}
		}

		for _, b := range m.Content {
			switch b.Kind {
			case model.BlockText:
				parts = append(parts, map[string]any{"type": contentType, "text": b.Text})

			case model.BlockImage:
				// S69 (AT6): `input_image` with a data URL, a sibling part
				// beside the person's text (AT7 — the words stay first and
				// untouched). Only a USER turn carries input parts; and only
				// the EXPANDED form, so a block still pinned to a document
				// reference never half-sends (AT5): withheld and COUNTED in
				// the attachment number, never silently skipped (M4, O1).
				if b.Data == "" || m.Role != model.RoleUser {
					attachDropped++
					continue
				}
				parts = append(parts, map[string]any{
					"type":      "input_image",
					"image_url": dataURL(b),
				})

			case model.BlockDocument:
				// `input_file` with the bytes inline as the same data URL —
				// this endpoint's honest shape for a document, which is why
				// the chat wire refuses rather than imitating it (AT6).
				if b.Data == "" || m.Role != model.RoleUser {
					attachDropped++
					continue
				}
				parts = append(parts, map[string]any{
					"type":      "input_file",
					"filename":  inputFileName(b),
					"file_data": dataURL(b),
				})

			case model.BlockToolUse:
				flush()
				out = append(out, map[string]any{
					"type":    "function_call",
					"call_id": b.ID,
					"name":    b.Name,
					// A STRING on the wire, as on the other endpoint: the
					// canonical form is raw JSON, and it crosses a type
					// boundary here in both directions.
					"arguments": argumentsString(b.Input),
				})

			case model.BlockToolResult:
				flush()
				content := b.Content
				if b.IsError {
					// # IsError has nowhere to go, so it goes in the text
					//
					// `function_call_output` carries `output` and nothing else
					// — there is no error flag on either OpenAI endpoint. A
					// result that failed and one that succeeded must not look
					// identical to the model (O3), so the marker is explicit
					// rather than dropped. Anthropic has a real field and uses
					// it.
					content = "ERROR: " + content
				}
				out = append(out, map[string]any{
					"type":    "function_call_output",
					"call_id": b.ToolUseID,
					"output":  content,
				})

			case model.BlockThinking:
				// # Not sent, and counted
				//
				// Reasoning is referenced on this endpoint by an item id the
				// provider issued, or by its encrypted content — neither of
				// which a canonical `thinking` block carries. Sending the text
				// as an assistant message would put the model's private
				// reasoning into the conversation as if it had said it.
				//
				// So it is dropped and counted, which is visible, rather than
				// silently skipped.
				dropped++

			default:
				dropped++
			}
		}
		flush()
	}

	if foldSystem && !folded && pending != "" {
		// No user turn to fold into: the instruction rides a leading user
		// item rather than vanishing — silent loss would change the model's
		// behaviour with nothing saying so.
		out = append([]any{map[string]any{
			"role":    string(model.RoleUser),
			"content": []any{map[string]any{"type": "input_text", "text": pending}},
		}}, out...)
	}
	if len(out) == 0 {
		return nil, dropped, attachDropped, fmt.Errorf("model: the transcript produced no input items")
	}
	return out, dropped, attachDropped, nil
}

// inputFileName is an `input_file` part's label: the block's own name when it
// has one, the tail of the pinned document's name when the attachment's own
// name was lost (an expanded reference still knows WHICH document it was), and
// the plain word "attachment" when neither speaks — a label that says what the
// part is without inventing a name for it.
func inputFileName(b model.Block) string {
	if b.Name != "" {
		return b.Name
	}
	if b.Document != "" {
		if tail := path.Base(b.Document); tail != "." && tail != "/" {
			return tail
		}
	}
	return "attachment"
}

// argumentsString renders a tool call's input as the wire's JSON string.
func argumentsString(input json.RawMessage) string {
	if len(input) == 0 {
		return "{}"
	}
	return string(input)
}

// --- FromWire ----------------------------------------------------------------

// The event names this adapter understands. Named rather than inline, because
// they are the part most likely to be wrong and a reader should be able to see
// the whole vocabulary at once.
const (
	evtOutputItemAdded   = "response.output_item.added"
	evtOutputItemDone    = "response.output_item.done"
	evtOutputTextDelta   = "response.output_text.delta"
	evtOutputTextDone    = "response.output_text.done"
	evtFnArgsDelta       = "response.function_call_arguments.delta"
	evtFnArgsDone        = "response.function_call_arguments.done"
	evtReasoningDelta    = "response.reasoning_summary_text.delta"
	evtReasoningTextDone = "response.reasoning_summary_text.done"
	evtCompleted         = "response.completed"
	evtIncomplete        = "response.incomplete"
	evtFailed            = "response.failed"
	evtCreated           = "response.created"
	evtInProgress        = "response.in_progress"
	evtContentPartAdded  = "response.content_part.added"
	evtContentPartDone   = "response.content_part.done"
	evtError             = "error"
)

type responsesEvent struct {
	Type        string `json:"type"`
	OutputIndex int    `json:"output_index"`
	ItemID      string `json:"item_id"`
	Delta       string `json:"delta"`
	Arguments   string `json:"arguments"`

	Item *struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"item"`

	Response *struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Usage *responsesUsage `json:"usage"`
	} `json:"response"`
}

// responsesUsage is this endpoint's accounting. Every field is a pointer for
// the reason `model.Usage`'s are: absent must stay distinguishable from zero,
// and this is where that distinction enters the system (M9).
type responsesUsage struct {
	InputTokens        *int `json:"input_tokens"`
	OutputTokens       *int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func (a *openAIResponses) FromWire(ctx context.Context, frames <-chan model.Frame) <-chan model.Delta {
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
			if strings.TrimSpace(frame.Data) == "" {
				continue
			}

			var ev responsesEvent
			if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
				// Unparseable data is counted, not fatal: a provider adding a
				// field must not break a stream mid-reply.
				a.countDropped()
				continue
			}

			// The `type` in the DATA is authoritative, with the SSE `event:`
			// line as the fallback. Both are sent; trusting only the event line
			// would break the moment a proxy strips it.
			kind := ev.Type
			if kind == "" {
				kind = frame.Event
			}

			switch kind {
			case evtOutputTextDelta:
				if ev.Delta != "" && !emit(model.Delta{Kind: model.DeltaText, Text: ev.Delta}) {
					return
				}

			case evtReasoningDelta:
				// A reasoning SUMMARY, not the reasoning itself: this endpoint
				// does not stream raw chain-of-thought. It maps to `thinking`
				// with **no signature**, which S3 permits deliberately — not
				// every provider signs.
				if ev.Delta != "" && !emit(model.Delta{Kind: model.DeltaThinking, Text: ev.Delta}) {
					return
				}

			case evtOutputItemAdded:
				if ev.Item != nil && ev.Item.Type == "function_call" {
					a.startCall(ev.OutputIndex, ev.Item.CallID, ev.Item.Name)
				}

			case evtFnArgsDelta:
				a.appendArgs(ev.OutputIndex, ev.Delta)

			case evtFnArgsDone:
				// The complete arguments, which REPLACE whatever was
				// accumulated: the fragments are a convenience for showing
				// progress, and this is the authoritative value.
				a.setArgs(ev.OutputIndex, ev.Arguments)

			case evtOutputItemDone:
				if ev.Item != nil && ev.Item.Type == "function_call" {
					if d, ok := a.flushCall(ev.OutputIndex); ok && !emit(d) {
						return
					}
				}

			case evtCompleted, evtIncomplete, evtFailed:
				if ev.Response != nil {
					a.accumulateUsage(ev.Response.Usage)
				}
				// Every tool call the stream produced, before the stop — a
				// caller that stopped reading at the stop event would otherwise
				// never see them.
				for _, d := range a.flushAll() {
					if !emit(d) {
						return
					}
				}
				reason := a.stopFor(kind, ev)
				a.setStopReason(reason)
				if !emit(model.Delta{Kind: model.DeltaStop, Reason: reason}) {
					return
				}

			case evtCreated, evtInProgress, evtOutputTextDone, evtReasoningTextDone,
				evtContentPartAdded, evtContentPartDone:
				// Structural. Explicitly ignored rather than counted as
				// dropped: they carry no content, and counting them would make
				// `dropped` a measure of how chatty the protocol is.

			case evtError:
				a.countDropped()

			default:
				a.countDropped()
			}
		}

		// A stream that ended without a terminal event still owes its tool
		// calls. What was accumulated is not thrown away — the client reports
		// the truncation separately.
		for _, d := range a.flushAll() {
			if !emit(d) {
				return
			}
		}
	}()

	return out
}

// stopFor maps a terminal event to a stop reason.
//
// The strings are this endpoint's own, passed through rather than normalised —
// the canonical format carries the provider's reason verbatim, and inventing a
// shared vocabulary here would hide which provider said what.
func (a *openAIResponses) stopFor(kind string, ev responsesEvent) string {
	switch kind {
	case evtIncomplete:
		if ev.Response != nil && ev.Response.IncompleteDetails != nil &&
			ev.Response.IncompleteDetails.Reason != "" {
			return ev.Response.IncompleteDetails.Reason
		}
		return "incomplete"
	case evtFailed:
		if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Code != "" {
			return "failed:" + ev.Response.Error.Code
		}
		return "failed"
	default:
		// A completed response that produced a tool call stopped FOR the tool
		// call, which is the distinction a caller acts on.
		if a.hasCalls() {
			return "tool_use"
		}
		return "completed"
	}
}

func (a *openAIResponses) startCall(index int, callID, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	call, ok := a.calls[index]
	if !ok {
		call = &responsesCall{}
		a.calls[index] = call
		a.order = append(a.order, index)
	}
	if callID != "" {
		call.callID = callID
	}
	if name != "" {
		call.name = name
	}
}

func (a *openAIResponses) appendArgs(index int, fragment string) {
	if fragment == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	call, ok := a.calls[index]
	if !ok {
		// Arguments before the item was announced. Tolerated rather than
		// dropped: the id arrives with the item, and a reordering on the wire
		// would otherwise lose the whole call.
		call = &responsesCall{}
		a.calls[index] = call
		a.order = append(a.order, index)
	}
	call.args.WriteString(fragment)
}

func (a *openAIResponses) setArgs(index int, arguments string) {
	if arguments == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	call, ok := a.calls[index]
	if !ok {
		call = &responsesCall{}
		a.calls[index] = call
		a.order = append(a.order, index)
	}
	call.args.Reset()
	call.args.WriteString(arguments)
}

// flushCall yields one COMPLETE tool_use and marks it done.
func (a *openAIResponses) flushCall(index int) (model.Delta, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flushLocked(index)
}

// flushAll yields every call not yet yielded, in first-seen order.
func (a *openAIResponses) flushAll() []model.Delta {
	a.mu.Lock()
	defer a.mu.Unlock()

	var out []model.Delta
	for _, index := range a.order {
		if d, ok := a.flushLocked(index); ok {
			out = append(out, d)
		}
	}
	return out
}

func (a *openAIResponses) flushLocked(index int) (model.Delta, bool) {
	call := a.calls[index]
	if call == nil || call.done {
		return model.Delta{}, false
	}
	call.done = true

	// Q11 (S18 T6): the same id discipline as the chat wire — synthesised
	// when absent, suffixed when duplicated, recorded in the id itself.
	call.callID = normalizeCallID(call.callID, index, a.flushedIDs)

	args := call.args.String()
	input := json.RawMessage(args)
	switch {
	case args == "":
		input = json.RawMessage(`{}`)
	case !json.Valid([]byte(args)):
		// **A model emitting malformed arguments is a normal event and must not
		// kill the call** (D6). The tool_use is still yielded, with empty
		// input, and the caller owes the model a tool_result with IsError so it
		// can correct itself.
		input = json.RawMessage(`{}`)
		a.owedErrors = append(a.owedErrors, call.callID)
	}

	return model.Delta{
		Kind:  model.DeltaToolUse,
		Block: &model.Block{Kind: model.BlockToolUse, ID: call.callID, Name: call.name, Input: input},
	}, true
}

func (a *openAIResponses) hasCalls() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.order) > 0
}

func (a *openAIResponses) accumulateUsage(u *responsesUsage) {
	if u == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// Assigned only when PRESENT. A provider that reports no usage must leave
	// the cost `unknown` rather than produce a confident zero (M9, D7).
	if u.InputTokens != nil {
		a.usage.InputTokens = u.InputTokens
	}
	if u.OutputTokens != nil {
		a.usage.OutputTokens = u.OutputTokens
	}
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens != nil {
		a.usage.CacheReadTokens = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens != nil {
		a.usage.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
	}
}

func (a *openAIResponses) countDropped() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropped++
}

func (a *openAIResponses) setStopReason(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopReason = reason
}

func (a *openAIResponses) Finalise() (model.Usage, string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage, a.stopReason, a.dropped
}

// ThinkingDropped reports the request-direction thinking drops for THIS
// request (S18 T5, M4): blocks withheld because this wire references
// reasoning by ids the provider issued, never by canonical content.
func (a *openAIResponses) ThinkingDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinkingDropped
}

// AttachmentDropped reports the S69 request-direction attachment drops: image
// and document blocks withheld because they reached the adapter still in
// reference form (AT5's expansion door was skipped upstream) or rode a role
// whose content parts carry no such shape. Counted apart from thinking and
// from the response-direction `dropped` (M4, O1).
func (a *openAIResponses) AttachmentDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attachmentDropped
}

// OwedToolErrors reports tool calls whose arguments would not parse (D6).
func (a *openAIResponses) OwedToolErrors() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.owedErrors...)
}
