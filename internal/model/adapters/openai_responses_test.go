package adapters

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// The Responses adapter: the second OpenAI wire format.
//
// It exists because `/v1/chat/completions` refuses function tools while
// reasoning is on. So the test that matters most is the one chat/completions
// cannot pass: **tools and reasoning in the same request**.

func responsesRoute() model.Route {
	r := testRoute(ProviderOpenAI)
	r.OpenAIEndpoint = model.OpenAIEndpointResponses
	return r
}

func TestTheResponsesEndpointSendsToolsAndReasoningTogether(t *testing.T) {
	// The whole point of the endpoint. On chat/completions this combination is
	// a 400; here it is the normal case.
	route := responsesRoute()
	route.ReasoningEffort = "medium"

	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(route))
	if err != nil {
		t.Fatal(err)
	}

	if got := req.URL.Path; !strings.HasSuffix(got, "/responses") {
		t.Errorf("the request went to %q", got)
	}
	body := wireBody(t, req)

	// An OBJECT, not a scalar: `reasoning_effort: "medium"` is the other
	// endpoint's spelling and a 400 here.
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning is %T, want an object", body["reasoning"])
	}
	if reasoning["effort"] != "medium" {
		t.Errorf("reasoning.effort is %v", reasoning["effort"])
	}
	if _, present := body["reasoning_effort"]; present {
		t.Error("the chat endpoint's scalar spelling was also sent")
	}

	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools is %v", body["tools"])
	}
	// FLAT, unlike chat/completions' nested `function` object — the difference
	// most likely to be copied wrong, because the two look so similar.
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "read" {
		t.Errorf("the tool is %v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Error("the tool is nested under `function`, which is the chat endpoint's shape")
	}
}

func TestTheResponsesEndpointUsesItsOwnFieldNames(t *testing.T) {
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(responsesRoute()))
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)

	for _, want := range []string{"input", "max_output_tokens", "store"} {
		if _, present := body[want]; !present {
			t.Errorf("the body has no %q", want)
		}
	}
	for _, unwanted := range []string{"messages", "max_completion_tokens", "stream_options"} {
		if _, present := body[unwanted]; present {
			t.Errorf("the body carries %q, which belongs to chat/completions", unwanted)
		}
	}

	// `store: false` — Aeon already owns the transcript, so a second copy on
	// the provider's side buys nothing and leaves somebody's repository
	// contents where they did not put it.
	if body["store"] != false {
		t.Errorf("store is %v, want false", body["store"])
	}
}

func TestATranscriptBecomesTopLevelInputItems(t *testing.T) {
	// The shape that differs most: a tool call and a tool result are SIBLINGS
	// of the messages, not content inside them. One canonical user message
	// carrying two tool results becomes two items.
	messages := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("be terse")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("read both")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.TextBlock("reading"),
			model.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"a.md"}`)),
			model.ToolUseBlock("call_2", "read", json.RawMessage(`{"path":"b.md"}`)),
		}},
		{Role: model.RoleUser, Content: []model.Block{
			model.ToolResultBlock("call_1", "A", false),
			model.ToolResultBlock("call_2", "no such file", true),
		}},
	}

	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(model.ModelRequest{
		Route: responsesRoute(), Messages: messages, MaxOutputTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}

	input, ok := wireBody(t, req)["input"].([]any)
	if !ok {
		t.Fatalf("input is %T", wireBody(t, req)["input"])
	}

	var kinds []string
	for _, item := range input {
		m := item.(map[string]any)
		if role, isMsg := m["role"]; isMsg {
			kinds = append(kinds, "message:"+role.(string))
			continue
		}
		kinds = append(kinds, m["type"].(string))
	}

	want := []string{
		"message:system", "message:user",
		"message:assistant", "function_call", "function_call",
		"function_call_output", "function_call_output",
	}
	if len(kinds) != len(want) {
		t.Fatalf("the items are %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("item %d is %q, want %q (all: %v)", i, kinds[i], want[i], kinds)
		}
	}

	// The call ids round-trip, which is what pairs a result with its call.
	raw, _ := json.Marshal(input)
	for _, needle := range []string{`"call_id":"call_1"`, `"call_id":"call_2"`} {
		if !strings.Contains(string(raw), needle) {
			t.Errorf("the input does not carry %s", needle)
		}
	}
	// And a FAILED result is distinguishable from a successful one (O3). This
	// endpoint has no error flag, so the marker is in the text — stated
	// explicitly rather than dropped.
	if !strings.Contains(string(raw), "ERROR: no such file") {
		t.Errorf("a failed tool result is indistinguishable from a successful one:\n%s", raw)
	}
}

func TestAssistantTextUsesOutputTextAndUserTextUsesInputText(t *testing.T) {
	// Two different content types for what the canonical format calls one
	// thing. Sending `input_text` in an assistant message is a 400.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(model.ModelRequest{
		Route: responsesRoute(),
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("question")}},
			{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("answer")}},
		},
		MaxOutputTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}

	input := wireBody(t, req)["input"].([]any)
	first := input[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	second := input[1].(map[string]any)["content"].([]any)[0].(map[string]any)

	if first["type"] != "input_text" {
		t.Errorf("the user's text is %q", first["type"])
	}
	if second["type"] != "output_text" {
		t.Errorf("the assistant's text is %q", second["type"])
	}
}

func TestAThinkingBlockIsNotSentAndIsCounted(t *testing.T) {
	// Reasoning is referenced on this endpoint by an id the provider issued or
	// by its encrypted content, neither of which a canonical `thinking` block
	// carries. Sending the text as an assistant message would put the model's
	// private reasoning into the conversation as if it had said it.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(model.ModelRequest{
		Route: responsesRoute(),
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
			{Role: model.RoleAssistant, Content: []model.Block{
				{Kind: model.BlockThinking, Text: "PRIVATE-REASONING", Signature: "sig"},
				model.TextBlock("hello"),
			}},
		},
		MaxOutputTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(wireBody(t, req)["input"])
	if strings.Contains(string(raw), "PRIVATE-REASONING") {
		t.Error("the thinking text was sent as conversation")
	}
	// Counted, so it is visible rather than silently skipped (M4) — and
	// counted as a REQUEST-direction thinking drop (S18 T5), not in the
	// response direction's `dropped`: the two numbers answer different
	// questions and the step's record carries them apart (O1).
	if _, _, dropped := adapter.Finalise(); dropped != 0 {
		t.Errorf("the request-direction drop leaked into the response-direction count: %d", dropped)
	}
	td, ok := adapter.(interface{ ThinkingDropped() int })
	if !ok {
		t.Fatal("the openai dispatcher does not forward ThinkingDropped — the count would be a wrapper's silent zero")
	}
	if n := td.ThinkingDropped(); n != 1 {
		t.Errorf("ThinkingDropped = %d, want 1", n)
	}
}

// --- the stream --------------------------------------------------------------

func TestTheResponsesStreamYieldsReasoningTextAndACompleteToolCall(t *testing.T) {
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	// ToWire first: the dispatcher chooses the wire there, which is the one
	// piece of order the client always satisfies.
	if _, err := adapter.ToWire(withTools(responsesRoute())); err != nil {
		t.Fatal(err)
	}

	var text, thinking strings.Builder
	var tools []model.Block
	var stops []string
	for d := range adapter.FromWire(context.Background(),
		framesFromFixture(t, "openai", "responses-tool-call")) {
		switch d.Kind {
		case model.DeltaText:
			text.WriteString(d.Text)
		case model.DeltaThinking:
			thinking.WriteString(d.Text)
		case model.DeltaToolUse:
			tools = append(tools, *d.Block)
		case model.DeltaStop:
			stops = append(stops, d.Reason)
		}
	}

	if got := text.String(); got != "I will read the README first." {
		t.Errorf("text is %q", got)
	}
	// A reasoning SUMMARY becomes `thinking` with no signature, which S3
	// permits deliberately: not every provider signs.
	if got := thinking.String(); !strings.Contains(got, "Deciding which file") {
		t.Errorf("thinking is %q", got)
	}

	if len(tools) != 1 {
		t.Fatalf("%d tool calls, want 1", len(tools))
	}
	if tools[0].ID != "call_FIXTURE1" || tools[0].Name != "read" {
		t.Errorf("the tool call is %+v", tools[0])
	}
	// COMPLETE arguments, reassembled from three fragments.
	if string(tools[0].Input) != `{"path":"README.md"}` {
		t.Errorf("the arguments are %s", tools[0].Input)
	}

	if len(stops) != 1 || stops[0] != "tool_use" {
		t.Errorf("stops are %v, want one tool_use", stops)
	}

	usage, stop, dropped := adapter.Finalise()
	if usage.InputTokens == nil || *usage.InputTokens != 412 {
		t.Errorf("input tokens are %v", usage.InputTokens)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != 48 {
		t.Errorf("output tokens are %v", usage.OutputTokens)
	}
	if usage.ReasoningTokens == nil || *usage.ReasoningTokens != 16 {
		t.Errorf("reasoning tokens are %v", usage.ReasoningTokens)
	}
	if stop != "tool_use" {
		t.Errorf("the stop reason is %q", stop)
	}
	// Structural events are ignored, not counted: `dropped` must measure lost
	// CONTENT, not how chatty the protocol is.
	if dropped != 0 {
		t.Errorf("%d blocks were counted as dropped on a stream that lost nothing", dropped)
	}
}

func TestATruncatedResponseReportsTheCapRatherThanLookingComplete(t *testing.T) {
	// This endpoint says `response.incomplete` where chat/completions says
	// `finish_reason: length`. A stop reason of "completed" on a truncated
	// answer is how a caller concludes the model was finished.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ToWire(withTools(responsesRoute())); err != nil {
		t.Fatal(err)
	}

	for range adapter.FromWire(context.Background(),
		framesFromFixture(t, "openai", "responses-incomplete")) {
	}

	_, stop, _ := adapter.Finalise()
	if stop != "max_output_tokens" {
		t.Errorf("the stop reason is %q, want the cap named", stop)
	}
}

func TestTheDispatcherPicksTheWireFromTheRoute(t *testing.T) {
	// One provider id, two wire formats. The route decides, per call.
	cases := []struct {
		endpoint string
		wantPath string
	}{
		{"", "/chat/completions"},
		{model.OpenAIEndpointChat, "/chat/completions"},
		{model.OpenAIEndpointResponses, "/responses"},
	}

	for _, tc := range cases {
		t.Run("endpoint="+tc.endpoint, func(t *testing.T) {
			route := testRoute(ProviderOpenAI)
			route.OpenAIEndpoint = tc.endpoint

			adapter, err := model.AdapterFor(ProviderOpenAI)
			if err != nil {
				t.Fatal(err)
			}
			req, err := adapter.ToWire(withTools(route))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(req.URL.Path, tc.wantPath) {
				t.Errorf("endpoint %q went to %q, want %q", tc.endpoint, req.URL.Path, tc.wantPath)
			}
		})
	}
}

func TestADefaultedEndpointIsStillTheChatOneSoNothingChangesForOldRoutes(t *testing.T) {
	// Every route written before this field existed must behave identically.
	route := testRoute(ProviderOpenAI)
	if route.OpenAIEndpoint != "" {
		t.Fatal("the fixture route already names an endpoint")
	}
	if route.EffectiveOpenAIEndpoint() != model.OpenAIEndpointChat {
		t.Errorf("an unset endpoint resolves to %q", route.EffectiveOpenAIEndpoint())
	}
}

func TestTheDispatcherForwardsOwedToolErrors(t *testing.T) {
	// The client reads this by type assertion on the adapter it was given —
	// which is the dispatcher. A wrapper that satisfied the interface and
	// answered "none" for both wires would be a hole nothing notices, because
	// the owed errors only matter when a model emits malformed JSON.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(interface{ OwedToolErrors() []string }); !ok {
		t.Fatal("the dispatcher does not expose OwedToolErrors, so D6's debt is lost")
	}

	// And it forwards from the chosen wire: a malformed-argument stream on the
	// chat endpoint still reports its debt through the dispatcher.
	if _, err := adapter.ToWire(withTools(testRoute(ProviderOpenAI))); err != nil {
		t.Fatal(err)
	}
	for range adapter.FromWire(context.Background(),
		framesFromFixture(t, "openai", "malformed-tool-arguments")) {
	}
	owed := adapter.(interface{ OwedToolErrors() []string }).OwedToolErrors()
	if len(owed) == 0 {
		t.Error("the owed tool error did not reach the dispatcher's caller")
	}
}

func TestFromWireBeforeToWireYieldsNothingRatherThanPanicking(t *testing.T) {
	// The client always calls them in order. A future caller that does not
	// should get an empty stream it can see, rather than a nil dereference
	// four frames from the cause.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan model.Frame, 1)
	frames <- model.Frame{Event: "response.created", Data: `{"type":"response.created"}`}
	close(frames)

	n := 0
	for range adapter.FromWire(context.Background(), frames) {
		n++
	}
	if n != 0 {
		t.Errorf("%d deltas from an adapter that was never given a request", n)
	}
}

func TestTheEndpointNeverReachesAnotherProvidersBody(t *testing.T) {
	// A route carrying OpenAI's endpoint must not change what Anthropic is
	// sent — the same way `ReasoningEffort` must not.
	route := testRoute(ProviderAnthropic)
	route.OpenAIEndpoint = model.OpenAIEndpointResponses

	adapter, err := model.AdapterFor(ProviderAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(route))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.URL.Path, "/messages") {
		t.Errorf("the Anthropic request went to %q", req.URL.Path)
	}
	body := wireBody(t, req)
	for _, unwanted := range []string{"input", "store", "reasoning"} {
		if _, present := body[unwanted]; present {
			t.Errorf("the Anthropic body carries %q", unwanted)
		}
	}
}

func TestTheAcceptedEndpointsAreTwoAndEmptyMeansChat(t *testing.T) {
	if !model.ValidOpenAIEndpoint("") {
		t.Error("empty is not accepted; every existing route would break")
	}
	for _, bad := range []string{"CHAT", "v1/responses", "response", "completions"} {
		if model.ValidOpenAIEndpoint(bad) {
			t.Errorf("%q is accepted", bad)
		}
	}
	if len(model.OpenAIEndpoints()) != 2 {
		t.Errorf("the set is %v", model.OpenAIEndpoints())
	}
}

// ── S69 (AT6): the widened block union on /v1/responses ─────────────────────
//
// This wire carries ALL of it: `image` becomes an `input_image` part with a
// data URL, `document` an `input_file` with `filename` and `file_data`. The
// shapes follow the `input_text` part the file already builds around the
// content-parts loop — sibling parts beside the person's words, which stay
// first and untouched (AT7).

// The image part, pinned whole. `image_url` is a PLAIN STRING here —
// chat/completions nests the same data URL under an object — and the pair of
// shapes is the difference most likely to be copied wrong, so both are pinned
// by their own adapter's test.
func TestTheResponsesEndpointSendsImagesAsInputImageWithADataURL(t *testing.T) {
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(model.ModelRequest{
		Route: responsesRoute(),
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{
			model.TextBlock("look at this"),
			model.ImageDataBlock("chart.png", "image/png", attachPNGBytes),
		}}},
		MaxOutputTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}

	input := wireBody(t, req)["input"].([]any)
	user := input[0].(map[string]any)
	if user["role"] != "user" {
		t.Fatalf("first item is %#v, want the user message", user)
	}
	parts := user["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("the turn carries %d parts, want the words and the image beside them", len(parts))
	}
	// AT7: the words lead, byte-exact.
	if want := map[string]any{"type": "input_text", "text": "look at this"}; !reflect.DeepEqual(parts[0], want) {
		t.Fatalf("the person's words were altered or demoted:\n  got  %#v\n  want %#v", parts[0], want)
	}
	// The part, whole: not its presence — its exact shape.
	want := map[string]any{
		"type":      "input_image",
		"image_url": "data:image/png;base64," + attachPNGBytes,
	}
	if got := parts[1].(map[string]any); !reflect.DeepEqual(got, want) {
		t.Fatalf("the input_image part is not the pinned shape:\n  got  %#v\n  want %#v", got, want)
	}
	if _, _, dropped := adapter.Finalise(); dropped != 0 {
		t.Errorf("%d blocks counted dropped on a request that lost nothing", dropped)
	}
}

// The document part, and the name fallback the wire needs an answer for:
// `filename` has no optional form, so a paste (Validate permits an empty
// Name) must be labelled, and labelled honestly — the plain word that says
// what the part IS, never an invented name.
func TestTheResponsesEndpointSendsDocumentsAsInputFile(t *testing.T) {
	tests := []struct {
		name         string
		block        model.Block
		wantFilename string
	}{
		{
			name:         "a named document keeps its name",
			block:        model.DocumentDataBlock("spec.pdf", "application/pdf", attachPDFBytes),
			wantFilename: "spec.pdf",
		},
		{
			name:         "a paste with no name falls back to the word attachment",
			block:        model.DocumentDataBlock("", "application/pdf", attachPDFBytes),
			wantFilename: "attachment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := model.AdapterFor(ProviderOpenAI)
			if err != nil {
				t.Fatal(err)
			}
			req, err := adapter.ToWire(model.ModelRequest{
				Route: responsesRoute(),
				Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{
					model.TextBlock("read this"),
					tc.block,
				}}},
				MaxOutputTokens: 256,
			})
			if err != nil {
				t.Fatal(err)
			}

			input := wireBody(t, req)["input"].([]any)
			parts := input[0].(map[string]any)["content"].([]any)
			want := map[string]any{
				"type":      "input_file",
				"filename":  tc.wantFilename,
				"file_data": "data:application/pdf;base64," + attachPDFBytes,
			}
			if got := parts[1].(map[string]any); !reflect.DeepEqual(got, want) {
				t.Fatalf("the input_file part is not the pinned shape:\n  got  %#v\n  want %#v", got, want)
			}
		})
	}
}

// The name fallback has a middle rung: the tail of the pinned document's name.
// It is reachable only from a block that carries BOTH bytes and a reference —
// which Validate refuses at every door (the either/or of AT5) — so it is
// pinned at the function that owns the label, by calling the builder directly,
// rather than left as a branch no test has ever stood in.
func TestTheResponsesInputFileNameFallsBackToTheDocumentTail(t *testing.T) {
	hand := model.Block{
		Kind:      model.BlockDocument,
		MediaType: "application/pdf",
		Data:      attachPDFBytes,
		Document:  "@attachments/ff00ff00-spec.pdf",
		Rev:       2,
	}
	parts, _, _, err := responsesInput([]model.Message{{Role: model.RoleUser, Content: []model.Block{
		model.TextBlock("see"), hand,
	}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := parts[0].(map[string]any)["content"].([]any)[1].(map[string]any)
	if got["filename"] != "ff00ff00-spec.pdf" {
		t.Fatalf("filename = %v, want the document's tail — an expanded reference still knows WHICH document it was", got["filename"])
	}
	if got["type"] != "input_file" || got["file_data"] != "data:application/pdf;base64,"+attachPDFBytes {
		t.Fatalf("the rest of the part changed shape: %#v", got)
	}
}

// Reference-form blocks (and images or documents on a role whose content
// parts carry no such shape) are dropped and COUNTED — the same discipline
// this file already pins for thinking. The count reaches the step's record
// through the dispatcher, so the assertion is made through `AdapterFor`, the
// way the client reads it.
func TestTheResponsesReferenceFormAttachmentsAreCountedNotHalfSent(t *testing.T) {
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(model.ModelRequest{
		Route: responsesRoute(),
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{
				model.TextBlock("see the chart"),
				model.ImageRefBlock("chart.png", "image/png", "@attachments/ab12cd34-chart.png", 3),
				model.DocumentRefBlock("spec.pdf", "application/pdf", "@attachments/ff00ff00-spec.pdf", 1),
			}},
			{Role: model.RoleAssistant, Content: []model.Block{
				model.TextBlock("here you are"),
				model.ImageDataBlock("out.png", "image/png", attachPNGBytes),
			}},
		},
		MaxOutputTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Nothing of theirs on the wire — not the part types, not the bytes, not
	// the names; and every word still rides (AT7/AT8: an unsendable rider
	// never takes the sentence with it).
	body := wireJSON(t, req)
	for _, forbidden := range []string{"input_image", "input_file", "data:image", "data:application", "chart.png", "spec.pdf", "out.png"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("an unsendable attachment reached the responses wire (%q):\n%s", forbidden, body)
		}
	}
	for _, word := range []string{"see the chart", "here you are"} {
		if !strings.Contains(body, word) {
			t.Errorf("the words were dropped with the unsendable attachments (%q):\n%s", word, body)
		}
	}

	td, ok := adapter.(interface{ AttachmentDropped() int })
	if !ok {
		t.Fatal("the dispatcher does not forward AttachmentDropped from the responses wire — the count would be a wrapper's silent zero")
	}
	if n := td.AttachmentDropped(); n != 3 {
		t.Fatalf("AttachmentDropped = %d, want 3 — two reference-form blocks and one image on a role with no part for it, each counted (M4)", n)
	}
	// And the counts stayed apart (O1).
	_, _, dropped := adapter.Finalise()
	if dropped != 0 {
		t.Errorf("request-direction attachment drops leaked into the response-direction count: %d", dropped)
	}
}
