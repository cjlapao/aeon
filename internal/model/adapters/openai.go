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

// # The OpenAI adapter
//
// Four places this provider differs from the canonical format, and each one is
// a place a pass-through would have failed:
//
//   - `system` stays **in** the message array (Anthropic hoists it)
//   - message content is a plain **string** (Anthropic wants typed blocks) —
//     with one S69 exception: a USER turn carrying an image becomes the
//     provider's parts array (`text` part first, then `image_url` parts with
//     a data URL), because this wire has no other field for bytes. A user
//     turn without images keeps the plain string, byte-identical to every
//     pin from before S69. A document has **no shape here at all** — the
//     request is refused for it, naming the responses route as the fix (AT6)
//   - tool arguments are a **JSON string** on the wire (Anthropic sends an
//     object), so `Input` crosses a type boundary in both directions
//   - streaming fragments are keyed by **array index**, and the id arrives on
//     the first fragment only (Anthropic's `content_block_start` carries the
//     complete id up front)
//
// The last one is the subtle one: an adapter keyed by id works until the first
// fragment that has none.

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

type openAI struct {
	mu sync.Mutex

	// toolBuf accumulates argument fragments, keyed by the wire's `index`.
	toolBuf map[int]*toolAccumulator
	// order preserves first-seen index order, so two tool calls are yielded in
	// the order the model emitted them rather than in map order.
	order []int

	usage      model.Usage
	stopReason string
	dropped    int

	// flushedIDs names every tool-call id already yielded (Q11): the
	// synthesis and the duplicate-suffix rules need the whole stream's
	// memory, not just the current call's.
	flushedIDs map[string]bool

	// thinkingDropped counts REQUEST-direction thinking drops (S18 T5, M4):
	// this wire has no field for canonical thinking — no producer's
	// signature makes a difference here, since this provider cannot verify
	// ANY thinking block — so every one is withheld and counted, never
	// silently skipped. Distinct from `dropped` (response direction, M4).
	thinkingDropped int

	// attachmentDropped counts REQUEST-direction attachment drops (S69):
	// image blocks this wire cannot carry — one that arrived in REFERENCE
	// form (no bytes, a pinned document/rev: an upstream bug against the
	// send-time expansion, AT5), or one riding a turn whose role has no
	// part to hold it. Counted, never silently skipped (M4), in its own
	// number beside thinkingDropped (O1). A document on this wire is never
	// counted — the request is REFUSED for it (AT6).
	attachmentDropped int

	// owedErrors names tool calls whose arguments would not parse. The caller
	// owes the model a `tool_result` with IsError for each (D6) — recorded
	// rather than returned, because a model emitting bad JSON is a normal event
	// and must not fail the call.
	owedErrors []string
}

type toolAccumulator struct {
	id   string
	name string
	args strings.Builder
}

func newOpenAI() *openAI {
	return &openAI{toolBuf: map[int]*toolAccumulator{}, flushedIDs: map[string]bool{}}
}

// wireSays answers "did the operator declare this wire flag?" — and an
// UNDECLARED route (nil) says yes to everything, because the absence of a
// declaration is not a declaration of absence (I5). Only a route that
// explicitly declared its capabilities gates anything; the pre-0028 world
// keeps its exact byte-identical requests.
func wireSays(route model.Route, capability model.Capability) bool {
	if route.WireCapabilities == nil {
		return true
	}
	for _, c := range route.WireCapabilities {
		if c == string(capability) {
			return true
		}
	}
	return false
}

// normalizeCallID is the Q11 answer, stated once for both OpenAI wire
// formats: **handled** — an absent id is synthesised as `aeon-synthesized-<n>`
// so the transcript records that the id was synthesised (the name IS the
// record: an S7 replay can tell a provider-issued id from a made-up one
// without a side channel), and a duplicate is suffixed `-aeon-2`, `-aeon-3`
// so two calls never merge into one addressable call (D13, M8).
func normalizeCallID(id string, index int, seen map[string]bool) string {
	if id == "" {
		id = fmt.Sprintf("aeon-synthesized-%d", index)
	}
	if !seen[id] {
		seen[id] = true
		return id
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-aeon-%d", id, n)
		if !seen[candidate] {
			seen[candidate] = true
			return candidate
		}
	}
}

// dataURL is how BOTH OpenAI wires spell an inline attachment (S69, AT6):
// `data:<mediaType>;base64,<data>`. The mediaType is a field the store's
// receipt settled and Validate made mandatory — it is never invented here.
// The two endpoints wrap this string in different shapes (chat nests it under
// an `image_url` OBJECT, responses takes it as a plain string), which is
// exactly the kind of near-identical difference the two adapters exist to
// keep apart.
func dataURL(b model.Block) string {
	return "data:" + b.MediaType + ";base64," + b.Data
}

func (*openAI) Provider() string { return ProviderOpenAI }

// --- ToWire ------------------------------------------------------------------

// ToWire builds the request and sends nothing.
func (a *openAI) ToWire(req model.ModelRequest) (*http.Request, error) {
	route := req.Route
	if err := req.Validate(); err != nil {
		return nil, err
	}

	messages, thinkingDropped, attachmentDropped, err := openAIMessages(req.Messages, route.ID, !wireSays(route, model.CapSystemRole))
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
		// M16's reasoning generalised: a missing cap is a configuration error,
		// and catching it here names the route instead of surfacing a provider
		// 400 about a field the caller never set.
		return nil, fmt.Errorf("model: route %q has no MaxOutputTokens", route.ID)
	}

	body := map[string]any{
		"model":    route.Model,
		"messages": messages,
		"stream":   true,
		// `max_completion_tokens`, not `max_tokens` — the older name is
		// deprecated and rejected for reasoning models.
		"max_completion_tokens": outputCap,
	}
	// S18 T6 (M13): the compat path is THIS adapter plus flags. A route that
	// DECLARED it does not do usage-in-stream gets no request for it — the
	// absence of the key is the gate, not an `include_usage: false`, which
	// some servers answer 400 about the unknown key rather than declining.
	// Undeclared (nil) still asks, because usage bought with no declaration
	// to contradict is the behaviour that made cost work at S3 (I5: nobody
	// said, nobody refused). Measured on Ollama 0.18.0: asking gets usage on
	// a final chunk with an EMPTY choices array; not asking gets no usage
	// anywhere and cost honestly `unknown` (M9).
	if wireSays(route, model.CapStreamingUsage) {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if effort := req.EffectiveReasoningEffort(); effort != "" {
		// Sent only when the route says so. **Not defaulted**, and not
		// inferred from the model's name:
		//
		//   - a model with no reasoning dial answers 400 `Unrecognized request
		//     argument supplied: reasoning_effort`, so injecting it for every
		//     route would break the ones that work today
		//   - deciding which models need it by sniffing their names is
		//     folklore with an expiry date, and the name that breaks it is
		//     always one nobody has seen yet
		//
		// So the route carries the decision, and the client turns the
		// provider's refusal into a message naming the remedy — see
		// `reasoningEffortHint` in internal/model/client.go.
		body["reasoning_effort"] = effort
	}
	// The `tools` key is ABSENT, never an empty array, when the route declared
	// it has none — the same shape S3 pinned for the handoff retry, and for
	// the same reason: some compat servers answer a bare `tools: []` with a
	// 400 about the key, while the key's absence is every server's "no tools
	// this call". A step actually NEEDING tools never gets this far on such a
	// route (T4's pre-call check refuses it first); this is the wire's own
	// discipline, and the one that also protects the ask path.
	if len(req.Tools) > 0 && wireSays(route, model.CapTools) {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			fn := map[string]any{"name": t.Name}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				// Forwarded as RAW JSON (M22). This layer never inspects a
				// schema; parsing it here would mean validating it here, which
				// is a second owner for a contract that already has one.
				fn["parameters"] = t.Parameters
			}
			tools = append(tools, map[string]any{"type": "function", "function": fn})
		}
		body["tools"] = tools
	}

	// ExtraBody merges LAST, so it can override a mapped field (M11). It is the
	// one escape hatch for a provider field this format does not model, and an
	// escape hatch that cannot override is not one.
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
	httpReq, err := http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	// These three and nothing else. A header set "just in case" is a header
	// somebody has to explain later.
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	return httpReq, nil
}

// openAIMessages maps canonical messages onto the wire's array.
//
// Returns the wire array, the count of thinking blocks withheld (S18 T5), and
// the count of image blocks withheld (S69: reference form, or a turn whose role
// has no part to hold one) — both request-direction numbers counted apart
// (M4, O1). A document is never counted: this wire has no shape for one at all,
// so the whole request is REFUSED for it, naming the route and the fix (AT6).
func openAIMessages(msgs []model.Message, routeID string, foldSystem bool) ([]any, int, int, error) {
	var thinkingDropped int
	var attachmentDropped int
	var out []any

	// S18 T6 (M13): a route that DECLARED it takes no system role gets the
	// system text FOLDED into the first user message — an absence translated
	// on the wire while the canonical transcript keeps its system turn
	// (M1). Some compat servers 400 on a role they do not model, so the
	// fold is the difference between a working route and a dead one, and it
	// is a flag rather than a fork (M13).
	var pending string
	if foldSystem {
		var kept []model.Message
		for _, m := range msgs {
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
		msgs = kept
	}
	folded := false

	for _, m := range msgs {
		// A message carrying tool results becomes ONE WIRE MESSAGE PER RESULT,
		// each with role `tool`. This is the mirror image of Anthropic's merge
		// rule, and the row of the provider table that is easiest to get wrong
		// in the opposite direction.
		var results []any
		var text []string
		var toolCalls []any
		var imageParts []any

		for _, b := range m.Content {
			switch b.Kind {
			case model.BlockText:
				text = append(text, b.Text)

			case model.BlockImage:
				// S69 (AT6): images ride this wire as `image_url` parts with
				// a data URL — and only on a USER turn, which is the only
				// content list the endpoint accepts them in, and the only
				// turn attachments are built onto anyway (AT7). A block in
				// REFERENCE form (no bytes, a pinned document/rev) is a bug
				// upstream of the send-time expansion (AT5): never a half
				// block on the wire. Both refusals to send are dropped and
				// COUNTED (M4), in the request direction's own number (O1) —
				// never silently skipped.
				if b.Data == "" || m.Role != model.RoleUser {
					attachmentDropped++
					continue
				}
				imageParts = append(imageParts, map[string]any{
					"type": "image_url",
					// An OBJECT here; the responses wire takes the same
					// data URL as a plain string. Copying either
					// shape onto the other endpoint is the 400.
					"image_url": map[string]any{"url": dataURL(b)},
				})

			case model.BlockDocument:
				// # No shape. So the request is REFUSED, not degraded (AT6).
				//
				// Not dropped and counted like an unmappable thinking block:
				// a counting drop would answer the person's turn — with a
				// PDF riding it — with a plausible reply that never saw the
				// PDF. Not a text fallback either: no pretending a document
				// is a string of bytes-as-sentences. The route speaks the
				// modality's absence by refusing, and names itself and the
				// wire that does carry the shape — the responses route,
				// where a document is an `input_file`.
				return nil, thinkingDropped, attachmentDropped, fmt.Errorf(
					"model: route %q is on /v1/chat/completions, which has no shape for the attached document %q (mediaType %s); switch the route to the responses wire (openaiEndpoint \"responses\"), which maps documents to input_file",
					routeID, b.Name, b.MediaType)

			case model.BlockThinking:
				// OMITTED on this provider, and COUNTED (S18 T5, M4). This
				// wire has no field for canonical thinking at all — its
				// reasoning is opaque and referenced by its own ids — so no
				// producer's provenance changes the answer: there is nothing
				// here that could replay any thinking block, whoever wrote
				// it. Counting is what keeps an omission from being a
				// silent drop (O1: the number reaches the step's record).
				thinkingDropped++
				continue

			case model.BlockToolUse:
				// `Input` crosses a type boundary: canonical raw JSON becomes a
				// JSON STRING on this wire.
				args := string(b.Input)
				if args == "" {
					args = "{}"
				}
				toolCalls = append(toolCalls, map[string]any{
					"id":   b.ID,
					"type": "function",
					"function": map[string]any{
						"name":      b.Name,
						"arguments": args,
					},
				})

			case model.BlockToolResult:
				results = append(results, map[string]any{
					"role":         "tool",
					"tool_call_id": b.ToolUseID,
					"content":      b.Content,
				})
			}
		}

		// Tool results are their own messages and do not combine with text.
		if len(results) > 0 {
			out = append(out, results...)
			if len(text) == 0 && len(toolCalls) == 0 && len(imageParts) == 0 {
				continue
			}
		}

		msg := map[string]any{"role": string(m.Role)}
		if foldSystem && !folded && pending != "" && m.Role == model.RoleUser {
			text = append([]string{pending + "\n\n"}, text...)
			folded = true
		}
		switch {
		case len(imageParts) > 0:
			// S69 (AT6/AT7): the parts array, and THE WORDS LEAD IT. The
			// text joins to exactly the string the no-image path would
			// have sent — an attachment is a sibling part, never sentence
			// decoration — and the image parts follow in block order.
			// A text-only user turn never reaches this branch: its content
			// stays the plain string every wire test pins (byte-identical,
			// no array), which is why the condition is the images, not a
			// per-role flag.
			parts := make([]any, 0, len(text)+len(imageParts))
			if len(text) > 0 {
				parts = append(parts, map[string]any{"type": "text", "text": strings.Join(text, "")})
			}
			parts = append(parts, imageParts...)
			msg["content"] = parts
		case len(text) > 0:
			msg["content"] = strings.Join(text, "")
		case len(toolCalls) > 0:
			// An assistant turn that is only tool calls still needs the field.
			msg["content"] = nil
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		if len(text) > 0 || len(toolCalls) > 0 || len(imageParts) > 0 {
			out = append(out, msg)
		}
	}
	if foldSystem && !folded && pending != "" {
		// A transcript with no user turn still gets its instruction on the
		// wire — dropped silently it would change the model's behaviour with
		// nothing saying so. A leading user message is the fold's honest
		// fallback.
		out = append([]any{map[string]any{
			"role": string(model.RoleUser), "content": pending,
		}}, out...)
	}
	if len(out) == 0 {
		return nil, thinkingDropped, attachmentDropped, fmt.Errorf("model: every message mapped to nothing on this provider")
	}
	return out, thinkingDropped, attachmentDropped, nil
}

// --- FromWire ----------------------------------------------------------------

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content *string `json:"content"`
			// Reasoning is the field Ollama 0.18.0's /v1 endpoint carries
			// thinking in — `delta.reasoning` alongside an empty `content`
			// while the model is thinking (measured 2026-10-26,
			// qwen3.5:latest: 1,484 reasoning tokens before a two-word
			// answer). OpenAI's own chat API has no such field; the compat
			// servers invented it because opaque reasoning leaves nothing
			// replayable to map. It yields thinking deltas — display and
			// event material only: nothing read here ever returns on this
			// wire (T5 drops every thinking block going out), which is what
			// makes reading it safe.
			Reasoning *string `json:"reasoning"`
			// ReasoningContent is the same thing under the name DeepSeek, vLLM and most Qwen servers use.
			ReasoningContent *string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// openAIUsage is named rather than inline so `accumulateUsage` has a readable
// signature. Every field is a pointer for the same reason model.Usage's are:
// absent must be distinguishable from zero, and this is where that distinction
// enters the system.
type openAIUsage struct {
	PromptTokens        *int `json:"prompt_tokens"`
	CompletionTokens    *int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	// Cost is the provider's own figure for the call, in USD, on gateways that report it (`usage.cost`).
	Cost *float64 `json:"cost"`
}

func (a *openAI) FromWire(ctx context.Context, frames <-chan model.Frame) <-chan model.Delta {
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
			var chunk openAIChunk
			if err := json.Unmarshal([]byte(frame.Data), &chunk); err != nil {
				// A frame this adapter cannot read is dropped and COUNTED
				// (M4), never silently ignored.
				a.countDropped()
				continue
			}

			if chunk.Usage != nil {
				a.accumulateUsage(chunk.Usage)
			}

			for _, choice := range chunk.Choices {
				if c := choice.Delta.Content; c != nil && *c != "" {
					if !emit(model.Delta{Kind: model.DeltaText, Text: *c}) {
						return
					}
				}
				r := choice.Delta.Reasoning
				if r == nil || *r == "" {
					r = choice.Delta.ReasoningContent
				}
				if r != nil && *r != "" {
					if !emit(model.Delta{Kind: model.DeltaThinking, Text: *r}) {
						return
					}
				}

				for _, tc := range choice.Delta.ToolCalls {
					// **Keyed by index, not by id.** The id arrives on the
					// first fragment only, so an adapter keyed by id loses
					// every subsequent fragment — which looks like a truncated
					// tool call.
					a.accumulateTool(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
				}

				if choice.FinishReason != nil {
					// The buffers are complete. Everything accumulated is
					// yielded now, in first-seen order.
					for _, d := range a.flushTools() {
						if !emit(d) {
							return
						}
					}
					a.setStopReason(*choice.FinishReason)
					if !emit(model.Delta{Kind: model.DeltaStop, Reason: *choice.FinishReason}) {
						return
					}
				}
			}
		}

		// A stream that ended without a finish_reason still owes its tool
		// calls: a truncated stream is reported by the client, but what was
		// accumulated is not thrown away.
		for _, d := range a.flushTools() {
			if !emit(d) {
				return
			}
		}
	}()

	return out
}

func (a *openAI) accumulateTool(index int, id, name, argFragment string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	acc, ok := a.toolBuf[index]
	if !ok {
		acc = &toolAccumulator{}
		a.toolBuf[index] = acc
		a.order = append(a.order, index)
	}
	// Recorded on first sight and never overwritten with an empty value.
	if id != "" {
		acc.id = id
	}
	if name != "" {
		acc.name = name
	}
	acc.args.WriteString(argFragment)
}

// flushTools yields one COMPLETE tool_use per accumulator and empties the
// buffer, so a second finish_reason cannot yield them twice.
func (a *openAI) flushTools() []model.Delta {
	a.mu.Lock()
	defer a.mu.Unlock()

	var out []model.Delta
	for _, index := range a.order {
		acc := a.toolBuf[index]
		if acc == nil {
			continue
		}
		// Q11 (S18 T6, D13): an id the provider never issued is SYNTHESISED,
		// and the synthesis is recorded in the id itself — the transcript
		// then says, years later, which ids no provider ever vouched for.
		// A duplicate gets a suffix rather than a merge: two calls must stay
		// addressable as two calls, because the tool results pair back by id.
		// (Ollama 0.18.0 issued well-formed ids on every measured call; this
		// is the defence for the servers that do not, pinned by a labelled
		// hand-written fixture.)
		acc.id = normalizeCallID(acc.id, index, a.flushedIDs)

		args := acc.args.String()
		input := json.RawMessage(args)
		if args == "" {
			input = json.RawMessage(`{}`)
		} else if !json.Valid([]byte(args)) {
			// **A model emitting malformed arguments is a normal event and must
			// not kill the call** (D6). The tool_use is still yielded, with
			// empty input, and the caller owes the model a tool_result with
			// IsError so it can correct itself.
			input = json.RawMessage(`{}`)
			a.owedErrors = append(a.owedErrors, acc.id)
		}

		out = append(out, model.Delta{
			Kind: model.DeltaToolUse,
			Block: &model.Block{
				Kind:  model.BlockToolUse,
				ID:    acc.id,
				Name:  acc.name,
				Input: input,
			},
		})
	}
	a.toolBuf = map[int]*toolAccumulator{}
	a.order = nil
	return out
}

func (a *openAI) accumulateUsage(u *openAIUsage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// **Only set a field when the source is present.** A `?? 0` equivalent here
	// is the bug M9 exists to prevent: it turns "the provider said nothing"
	// into "the provider said zero", and a zero cost reaches the UI looking
	// like a cheap step.
	if u.PromptTokens != nil {
		a.usage.InputTokens = u.PromptTokens
	}
	if u.CompletionTokens != nil {
		a.usage.OutputTokens = u.CompletionTokens
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens != nil {
		a.usage.CacheReadTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens != nil {
		a.usage.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.Cost != nil {
		a.usage.ReportedCostUSD = u.Cost
	}
}

func (a *openAI) countDropped() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropped++
}

func (a *openAI) setStopReason(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopReason = reason
}

func (a *openAI) Finalise() (model.Usage, string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage, a.stopReason, a.dropped
}

// ThinkingDropped reports the request-direction thinking drops for THIS
// request (S18 T5) — see the anthropic adapter's twin for why the direction
// is counted separately (M4, O1).
func (a *openAI) ThinkingDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinkingDropped
}

// AttachmentDropped reports the S69 request-direction attachment drops: image
// blocks withheld because they reached the adapter still in reference form
// (AT5's expansion door was skipped upstream) or rode a role this wire has no
// part for. A document is never in this number — a document refuses the whole
// request (AT6), and a count that quietly absorbed a refusal would be the
// silence the refusal exists to prevent.
func (a *openAI) AttachmentDropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attachmentDropped
}

// OwedToolErrors reports tool calls whose arguments would not parse, so the
// runtime can answer each with a `tool_result` carrying IsError (D6).
//
// Not part of the Adapter interface: S5 owns tool execution, and until then
// this is information the client surfaces rather than acts on.
func (a *openAI) OwedToolErrors() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.owedErrors...)
}

// --- the dispatcher ----------------------------------------------------------

// openAIDispatch is the adapter the registry hands out for `openai`.
//
// # Why a dispatcher and not two provider ids
//
// `openai-responses` as a second provider id was the cheaper option — zero
// plumbing, since `AdapterFor` already dispatches on the name. It was rejected:
// the provider IS OpenAI either way. The prices are the same, the credential is
// the same, and `OPENAI_API_KEY` discovery is keyed on the name. Splitting it
// would mean two providers that share everything but a URL, and a
// `--provider openai-responses` that is really a transport detail wearing a
// provider's clothes.
//
// So: one provider, one route field (`openaiEndpoint`), two implementations.
//
// # Why the choice is made in ToWire rather than at construction
//
// `FromWire` and `Finalise` receive no route — only `ToWire` does. The adapter
// instance is already per-call and already stateful (it accumulates tool
// arguments and usage), so remembering which wire was chosen is the same kind
// of state it already holds.
//
// `FromWire` before `ToWire` is a programmer error and yields a closed channel
// rather than a nil-pointer panic: the client always calls them in order, and a
// future caller that does not should get an empty stream it can see rather than
// a crash four frames away.
type openAIDispatch struct {
	chat      *openAI
	responses *openAIResponses

	mu     sync.Mutex
	chosen model.Adapter
}

func newOpenAIDispatch() *openAIDispatch {
	return &openAIDispatch{chat: newOpenAI(), responses: newOpenAIResponses()}
}

// Embedder delegation (S22 T3): the dispatch routes CHAT by wire flag;
// embeddings have no such dial — /embeddings is /embeddings — so the
// dispatch answers the optional interface by handing to its chat adapter,
// which is the openai wire in full.
func (d *openAIDispatch) EmbedToWire(route model.Route, texts []string) (*http.Request, error) {
	return d.chat.EmbedToWire(route, texts)
}

func (d *openAIDispatch) EmbedFromWire(status int, body []byte) ([][]float32, model.Usage, error) {
	return d.chat.EmbedFromWire(status, body)
}

func (*openAIDispatch) Provider() string { return ProviderOpenAI }

func (d *openAIDispatch) ToWire(req model.ModelRequest) (*http.Request, error) {
	var chosen model.Adapter = d.chat
	if req.Route.EffectiveOpenAIEndpoint() == model.OpenAIEndpointResponses {
		chosen = d.responses
	}

	d.mu.Lock()
	d.chosen = chosen
	d.mu.Unlock()

	return chosen.ToWire(req)
}

func (d *openAIDispatch) FromWire(ctx context.Context, frames <-chan model.Frame) <-chan model.Delta {
	chosen := d.current()
	if chosen == nil {
		// Drained rather than ignored, so a caller that reads the frame channel
		// does not block on it forever.
		out := make(chan model.Delta)
		go func() {
			defer close(out)
			for range frames {
			}
		}()
		return out
	}
	return chosen.FromWire(ctx, frames)
}

func (d *openAIDispatch) Finalise() (model.Usage, string, int) {
	chosen := d.current()
	if chosen == nil {
		return model.Usage{}, "", 0
	}
	return chosen.Finalise()
}

// OwedToolErrors forwards the extra the client reads by type assertion (D6).
//
// Without this the dispatcher would satisfy the interface and silently answer
// "no owed errors" for both wires — which is the kind of hole a wrapper
// introduces and nothing notices, so a test asserts it forwards.
func (d *openAIDispatch) OwedToolErrors() []string {
	switch chosen := d.current().(type) {
	case *openAI:
		return chosen.OwedToolErrors()
	case *openAIResponses:
		return chosen.OwedToolErrors()
	default:
		return nil
	}
}

// ThinkingDropped forwards the S18 T5 request-direction count with the same
// care OwedToolErrors demanded: the client reads it by type assertion, and
// a dispatcher that did not forward would answer "nothing was dropped" for
// whichever wire actually withheld thinking blocks — a wrapper's silent
// zero, which is exactly the M4 failure this counter exists to prevent.
func (d *openAIDispatch) ThinkingDropped() int {
	switch chosen := d.current().(type) {
	case *openAI:
		return chosen.ThinkingDropped()
	case *openAIResponses:
		return chosen.ThinkingDropped()
	default:
		return 0
	}
}

// AttachmentDropped forwards the S69 request-direction attachment count with
// the same care ThinkingDropped demanded: the client reads it by type
// assertion, and a dispatcher answering "nothing was dropped" for whichever
// wire withheld an attachment would be the wrapper's silent zero M4 exists to
// prevent.
func (d *openAIDispatch) AttachmentDropped() int {
	switch chosen := d.current().(type) {
	case *openAI:
		return chosen.AttachmentDropped()
	case *openAIResponses:
		return chosen.AttachmentDropped()
	default:
		return 0
	}
}

func (d *openAIDispatch) current() model.Adapter {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.chosen
}

// --- embeddings (S22 T3, V16) -------------------------------------------------
//
// The embedding wire is the SAME adapter, not a second provider path (I8):
// one provider id, one key handling (route.APIKey, the same Bearer line
// ToWire sets), one base-URL convention (the configured base keeps its /v1 —
// OpenAI and ollama's OpenAI-compatible surface both serve /v1/embeddings
// from what the route already holds). The dispatch routes chat to
// chat/completions or /responses by wire flag; embeddings have no such dial,
// so they go to /embeddings and nothing else.

type embedWireRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

func (a *openAI) EmbedToWire(route model.Route, texts []string) (*http.Request, error) {
	raw, err := json.Marshal(embedWireRequest{
		Model: route.Model, Input: texts, EncodingFormat: "float",
	})
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(route.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	httpReq, err := http.NewRequest(http.MethodPost, base+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	// The same three-and-nothing-else discipline as ToWire, with the accept
	// the endpoint actually answers (JSON, not an event stream).
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	return httpReq, nil
}

type embedWireResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage *struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (a *openAI) EmbedFromWire(status int, body []byte) ([][]float32, model.Usage, error) {
	var wire embedWireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, model.Usage{}, fmt.Errorf(
			"model/openai: embedding answer is not JSON (HTTP %d, first %d bytes kept for the log): %s",
			status, min(len(body), 200), truncForLog(body))
	}
	if status < 200 || status > 299 {
		msg := ""
		if wire.Error != nil {
			msg = wire.Error.Message
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
			if len(msg) > 200 {
				msg = msg[:200]
			}
		}
		return nil, model.Usage{}, fmt.Errorf("model/openai: embeddings endpoint answered HTTP %d: %s", status, msg)
	}
	vectors := make([][]float32, len(wire.Data))
	seen := make([]bool, len(wire.Data))
	for _, d := range wire.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			return nil, model.Usage{}, fmt.Errorf(
				"model/openai: embedding index %d outside the %d-texts sent — refusing to guess where a vector belongs", d.Index, len(vectors))
		}
		if seen[d.Index] {
			return nil, model.Usage{}, fmt.Errorf("model/openai: embedding index %d arrived twice — position is the pairing, so a duplicate is corruption, not noise", d.Index)
		}
		seen[d.Index] = true
		vectors[d.Index] = d.Embedding
	}
	for i, s := range seen {
		if !s {
			return nil, model.Usage{}, fmt.Errorf("model/openai: no vector for position %d of %d — a hole in the batch is refused, not left for a zero to fill", i, len(vectors))
		}
	}
	usage := model.Usage{}
	if wire.Usage != nil {
		// Embeddings bill INPUT tokens; there is no output side, so nothing
		// else is populated and ComputeCost prices exactly what happened.
		usage.InputTokens = model.IntPtr(wire.Usage.PromptTokens)
	}
	return vectors, usage, nil
}

// Embedding answers are numbers; the truncation helper chat uses for frames
// serves the same purpose here (keep the evidence, bound the allocation).
func truncForLog(body []byte) string {
	const keep = 200
	if len(body) <= keep {
		return string(body)
	}
	return string(body[:keep])
}
