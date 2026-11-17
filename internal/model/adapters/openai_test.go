package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// --- helpers -----------------------------------------------------------------

func testRoute(provider string) model.Route {
	return model.Route{
		ID: "test-route", Provider: provider, Model: "test-model",
		BaseURL: "https://example.invalid/v1", APIKey: "<redacted>",
		MaxOutputTokens: 1024,
	}
}

// wireBody decodes the JSON an adapter built, so a test asserts on the wire
// rather than on the adapter's internals.
func wireBody(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	if req == nil {
		t.Fatal("no request")
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("the body is not valid JSON: %v\n%s", err, raw)
	}
	return body
}

// framesFromFixture replays a committed fixture through the real SSE reader, so
// an adapter test exercises the same path production does.
func framesFromFixture(t *testing.T, provider, scenario string) <-chan model.Frame {
	t.Helper()
	path := filepath.Join("..", "testdata", provider, scenario+".sse")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	frames, errs := model.ReadSSE(context.Background(), strings.NewReader(string(raw)))
	go func() {
		for err := range errs {
			t.Errorf("fixture %s failed to parse: %v", path, err)
		}
	}()
	return frames
}

func drainDeltas(a model.Adapter, frames <-chan model.Frame) []model.Delta {
	var out []model.Delta
	for d := range a.FromWire(context.Background(), frames) {
		out = append(out, d)
	}
	return out
}

// --- ToWire ------------------------------------------------------------------

func TestOpenAISystemStaysInTheMessageArray(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("be brief")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	body := wireBody(t, req)
	if _, hoisted := body["system"]; hoisted {
		t.Error("the system prompt was hoisted to a top-level field; that is Anthropic's shape")
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %#v, want two entries", body["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be brief" {
		t.Fatalf("the first message is %#v, want the system prompt", first)
	}
}

func TestOpenAIToolUseInputIsMarshalledIntoFunctionArgumentsAsAString(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather?")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("call_1", "get_weather", json.RawMessage(`{"location":"Lisbon"}`)),
		}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	assistant := msgs[len(msgs)-1].(map[string]any)
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls = %#v", assistant["tool_calls"])
	}
	fn := calls[0].(map[string]any)["function"].(map[string]any)

	args, isString := fn["arguments"].(string)
	if !isString {
		t.Fatalf("function.arguments is %T, want a STRING — this provider sends tool arguments "+
			"as a JSON string, and an object is the shape that works against Anthropic and "+
			"fails here", fn["arguments"])
	}
	if args != `{"location":"Lisbon"}` {
		t.Fatalf("arguments = %q", args)
	}
}

func TestOpenAIAToolRoleMessageBecomesOneWireMessagePerToolResult(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("two cities")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("call_1", "get_weather", json.RawMessage(`{"location":"Lisbon"}`)),
			model.ToolUseBlock("call_2", "get_weather", json.RawMessage(`{"location":"Porto"}`)),
		}},
		// Canonically ONE user message carrying two results.
		{Role: model.RoleUser, Content: []model.Block{
			model.ToolResultBlock("call_1", "18C", false),
			model.ToolResultBlock("call_2", "17C", false),
		}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	var toolMsgs []map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "tool" {
			toolMsgs = append(toolMsgs, mm)
		}
	}
	// TWO wire messages — the mirror image of Anthropic's merge rule, and the
	// row of the provider table easiest to get wrong in the other direction.
	if len(toolMsgs) != 2 {
		t.Fatalf("got %d tool-role messages, want 2 — this provider takes one message per "+
			"result, where Anthropic takes one message containing both", len(toolMsgs))
	}
	ids := []string{toolMsgs[0]["tool_call_id"].(string), toolMsgs[1]["tool_call_id"].(string)}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"call_1", "call_2"}) {
		t.Fatalf("tool_call_ids = %v", ids)
	}
}

func TestOpenAIThinkingBlocksAreOmitted(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ThinkingBlock("secret reasoning", "sig-abc"),
			model.TextBlock("hello"),
		}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(wireBody(t, req))
	for _, leaked := range []string{"secret reasoning", "sig-abc", "thinking"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("a thinking block reached this provider's wire (%q). Its reasoning content "+
				"is opaque and not replayable, so forwarding canonical thinking would send text "+
				"the provider did not produce", leaked)
		}
	}
	// And the text alongside it still arrives.
	if !strings.Contains(string(raw), "hello") {
		t.Error("the text block was dropped along with the thinking block")
	}
}

func TestOpenAIUsesMaxCompletionTokensAndNotMaxTokens(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)

	if _, present := body["max_completion_tokens"]; !present {
		t.Error("max_completion_tokens is absent")
	}
	if _, wrong := body["max_tokens"]; wrong {
		t.Error("max_tokens is present; it is deprecated here and rejected for reasoning models, " +
			"and it is Anthropic's field name")
	}
	if got := body["max_completion_tokens"]; got != float64(1024) {
		t.Errorf("max_completion_tokens = %v, want 1024 from the route", got)
	}
}

func TestOpenAIRequestMaxOutputTokensOverridesTheRoute(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{
		Messages:        []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		MaxOutputTokens: 64,
		Route:           testRoute(ProviderOpenAI),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := wireBody(t, req)["max_completion_tokens"]; got != float64(64) {
		t.Errorf("max_completion_tokens = %v, want the request's 64", got)
	}
}

func TestOpenAIARouteWithNoOutputCapIsRefusedNamingTheRoute(t *testing.T) {
	a := newOpenAI()
	route := testRoute(ProviderOpenAI)
	route.MaxOutputTokens = 0
	_, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Route:    route,
	})
	if err == nil {
		t.Fatal("built a request with no output cap")
	}
	if !strings.Contains(err.Error(), "test-route") {
		t.Errorf("the error does not name the route: %v", err)
	}
}

// M11 — the one escape hatch. An escape hatch that cannot override is not one.
func TestOpenAIExtraBodyMergesLastAndCanOverrideAMappedField(t *testing.T) {
	a := newOpenAI()
	route := testRoute(ProviderOpenAI)
	route.ExtraBody = json.RawMessage(`{"max_completion_tokens":7,"logit_bias":{"50256":-100}}`)

	req, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Route:    route,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)

	if got := body["max_completion_tokens"]; got != float64(7) {
		t.Errorf("max_completion_tokens = %v, want extraBody's 7 — merging last is what makes "+
			"this an escape hatch rather than a suggestion", got)
	}
	if _, present := body["logit_bias"]; !present {
		t.Error("an unmapped extraBody field did not reach the wire")
	}
}

func TestOpenAIInvalidExtraBodyIsRefused(t *testing.T) {
	a := newOpenAI()
	route := testRoute(ProviderOpenAI)
	route.ExtraBody = json.RawMessage(`{not json`)
	if _, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Route:    route,
	}); err == nil {
		t.Fatal("accepted invalid extraBody")
	}
}

// D14's enforcement at the body level: the key set is EXACTLY the mapped set.
// This is what catches a canonical field silently reaching the wire.
func TestOpenAITheBodyKeySetIsExactlyTheMappedSet(t *testing.T) {
	a := newOpenAI()
	temp := 0.5
	req, err := a.ToWire(model.ModelRequest{
		Messages:    []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Tools:       []model.ToolSpec{{Name: "bash", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Temperature: &temp,
		Route:       testRoute(ProviderOpenAI),
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for k := range wireBody(t, req) {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{
		"max_completion_tokens", "messages", "model", "stream", "stream_options",
		"temperature", "tools",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the body's keys are\n  %v\nwant exactly\n  %v\n"+
			"An unexpected key is a canonical field reaching the wire untranslated; a missing "+
			"one is a mapping that stopped happening.", got, want)
	}
}

func TestOpenAITemperatureIsOmittedWhenUnset(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Route:    testRoute(ProviderOpenAI),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req)["temperature"]; present {
		t.Error("temperature was sent when the request did not set one; the provider's default " +
			"and ours would then differ silently")
	}
}

func TestOpenAIToolParametersAreForwardedAsRawJSON(t *testing.T) {
	a := newOpenAI()
	const schema = `{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`
	req, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Tools:    []model.ToolSpec{{Name: "bash", Description: "run", Parameters: json.RawMessage(schema)}},
		Route:    testRoute(ProviderOpenAI),
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := wireBody(t, req)["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	params, _ := json.Marshal(fn["parameters"])

	var want, got any
	_ = json.Unmarshal([]byte(schema), &want)
	_ = json.Unmarshal(params, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the schema was altered in transit:\n  got  %s\n  want %s", params, schema)
	}
}

// --- FromWire ----------------------------------------------------------------

func TestOpenAITextFixtureStreamsIncrementally(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "text-only"))

	var texts []string
	for _, d := range deltas {
		if d.Kind == model.DeltaText {
			texts = append(texts, d.Text)
		}
	}
	// D1: incremental, not one buffered block. Three text deltas in the
	// fixture, so one delta carrying everything is a buffering bug.
	if len(texts) < 3 {
		t.Fatalf("got %d text deltas, want the fixture's 3 — one delta carrying the whole "+
			"reply means the adapter buffered", len(texts))
	}
	if joined := strings.Join(texts, ""); joined != "Hello, world 🌍" {
		t.Fatalf("reassembled text = %q", joined)
	}
	if last := deltas[len(deltas)-1]; last.Kind != model.DeltaStop || last.Reason != "stop" {
		t.Fatalf("the last delta is %+v, want a stop with reason \"stop\"", last)
	}

	usage, stop, dropped := a.Finalise()
	if usage.InputTokens == nil || *usage.InputTokens != 12 {
		t.Errorf("inputTokens = %v, want 12", usage.InputTokens)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != 5 {
		t.Errorf("outputTokens = %v, want 5", usage.OutputTokens)
	}
	if stop != "stop" {
		t.Errorf("stopReason = %q", stop)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d on a clean fixture", dropped)
	}
}

func TestOpenAIIndexKeyedFragmentsReassembleIntoOneCompleteToolUse(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "tool-call"))

	var tools []model.Delta
	for _, d := range deltas {
		if d.Kind == model.DeltaToolUse {
			tools = append(tools, d)
		}
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tool_use deltas, want exactly 1 — the fixture splits the arguments "+
			"across three fragments", len(tools))
	}
	b := tools[0].Block
	if b.ID != "call_FIXTURE1" || b.Name != "get_weather" {
		t.Fatalf("the id or name was lost: %+v", b)
	}
	// The arguments arrived in three pieces and must be one valid object.
	var args map[string]any
	if err := json.Unmarshal(b.Input, &args); err != nil {
		t.Fatalf("the reassembled arguments are not valid JSON: %v\n  %s", err, b.Input)
	}
	if args["location"] != "Lisbon" || args["unit"] != "c" {
		t.Fatalf("arguments = %s", b.Input)
	}
}

func TestOpenAIASecondToolCallAtADifferentIndexAccumulatesIndependently(t *testing.T) {
	a := newOpenAI()
	frames := make(chan model.Frame, 8)
	// Two calls, interleaved by index — which is what a real stream does and
	// what an adapter keyed by anything other than index gets wrong.
	for _, data := range []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_A","type":"function","function":{"name":"first","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_B","type":"function","function":{"name":"second","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":2}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		frames <- model.Frame{Data: data}
	}
	close(frames)

	var tools []*model.Block
	for _, d := range drainDeltas(a, frames) {
		if d.Kind == model.DeltaToolUse {
			tools = append(tools, d.Block)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(tools))
	}
	// Order is first-seen, so the arguments cannot have been crossed.
	if tools[0].ID != "call_A" || string(tools[0].Input) != `{"a":1}` {
		t.Errorf("first tool call = %+v", tools[0])
	}
	if tools[1].ID != "call_B" || string(tools[1].Input) != `{"b":2}` {
		t.Errorf("second tool call = %+v", tools[1])
	}
}

// **The test that makes M8 real rather than intended.** Every delta, inspected.
func TestOpenAINoPartialToolUseIsEverYielded(t *testing.T) {
	for _, scenario := range []string{"tool-call", "text-only", "usage-absent", "malformed-tool-arguments"} {
		t.Run(scenario, func(t *testing.T) {
			a := newOpenAI()
			for _, d := range drainDeltas(a, framesFromFixture(t, "openai", scenario)) {
				if d.Kind != model.DeltaToolUse {
					continue
				}
				if d.Block == nil {
					t.Fatal("a tool_use delta carries no block")
				}
				if d.Block.ID == "" {
					t.Errorf("a yielded tool_use has no ID: %+v", d.Block)
				}
				if d.Block.Name == "" {
					t.Errorf("a yielded tool_use has no Name: %+v", d.Block)
				}
				if d.Block.Input == nil {
					t.Errorf("a yielded tool_use has nil Input: %+v", d.Block)
				}
				if err := d.Block.Validate(); err != nil {
					t.Errorf("a yielded tool_use does not validate: %v", err)
				}
			}
		})
	}
}

// D6 — a model emitting bad JSON is a normal event.
func TestOpenAIUnparseableArgumentsYieldAnOwedErrorAndTheCallContinues(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "malformed-tool-arguments"))

	var tools []model.Delta
	var sawStop bool
	for _, d := range deltas {
		switch d.Kind {
		case model.DeltaToolUse:
			tools = append(tools, d)
		case model.DeltaStop:
			sawStop = true
		}
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tool_use deltas, want 1 — malformed arguments must not swallow the call", len(tools))
	}
	if !sawStop {
		t.Error("the stream did not reach a stop delta; malformed arguments killed the call")
	}
	// The block is still complete and valid, with empty input.
	if err := tools[0].Block.Validate(); err != nil {
		t.Errorf("the tool_use does not validate: %v", err)
	}
	if string(tools[0].Block.Input) != `{}` {
		t.Errorf("Input = %s, want an empty object", tools[0].Block.Input)
	}
	// And the error is OWED, so the runtime can answer with IsError.
	owed := a.OwedToolErrors()
	if len(owed) != 1 || owed[0] != "call_FIXTUREBAD" {
		t.Fatalf("owed errors = %v, want the one call id — without this the model never learns "+
			"its arguments were wrong", owed)
	}
}

// D7's source: the fixture with no usage must produce a Usage with NOTHING set.
func TestOpenAIAFixtureWithNoUsageYieldsAUsageWithNothingSet(t *testing.T) {
	a := newOpenAI()
	drainDeltas(a, framesFromFixture(t, "openai", "usage-absent"))

	usage, _, _ := a.Finalise()
	if !usage.Empty() {
		t.Fatalf("usage = %+v, want every field nil. A `?? 0` here turns 'the provider said "+
			"nothing' into 'the provider said zero', and a zero cost reaches the UI looking "+
			"like a cheap step", usage)
	}
	// And the cost computed from it is unknown, not a known zero.
	cost := model.ComputeCost(usage, &model.Prices{
		InputUSDPerMTok:  model.Float64Ptr(3),
		OutputUSDPerMTok: model.Float64Ptr(15),
	})
	if cost.State != model.CostUnknown {
		t.Fatalf("cost = %+v, want unknown", cost)
	}
}

func TestOpenAIAnUnrecognisedFrameIncrementsDropped(t *testing.T) {
	a := newOpenAI()
	frames := make(chan model.Frame, 3)
	frames <- model.Frame{Data: `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`}
	frames <- model.Frame{Data: `not json at all`}
	frames <- model.Frame{Data: `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`}
	close(frames)

	drainDeltas(a, frames)
	_, _, dropped := a.Finalise()
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1 — unmapped content is dropped and COUNTED (M4), because "+
			"a silent drop is how a block disappears between two releases and nobody can say when", dropped)
	}
}

// Cache and reasoning tokens arrive nested, and only when present.
func TestOpenAICacheAndReasoningTokensAreReadFromTheirNestedFields(t *testing.T) {
	a := newOpenAI()
	frames := make(chan model.Frame, 2)
	frames <- model.Frame{Data: `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,` +
		`"prompt_tokens_details":{"cached_tokens":80},` +
		`"completion_tokens_details":{"reasoning_tokens":20}}}`}
	close(frames)
	drainDeltas(a, frames)

	usage, _, _ := a.Finalise()
	for name, got := range map[string]*int{
		"inputTokens":     usage.InputTokens,
		"outputTokens":    usage.OutputTokens,
		"cacheReadTokens": usage.CacheReadTokens,
		"reasoningTokens": usage.ReasoningTokens,
	} {
		if got == nil {
			t.Errorf("%s is nil", name)
		}
	}
	if usage.CacheReadTokens != nil && *usage.CacheReadTokens != 80 {
		t.Errorf("cacheReadTokens = %d, want 80", *usage.CacheReadTokens)
	}
	// Cache WRITE is not reported by this provider, so it stays absent rather
	// than becoming zero.
	if usage.CacheWriteTokens != nil {
		t.Errorf("cacheWriteTokens = %d, want nil — this provider does not report it, and "+
			"absent is not zero", *usage.CacheWriteTokens)
	}
}

// ── S31 T4: the steered turn's shape ────────────────────────────────────────

// TestOpenAISteeredTurnSplitsIntoToolMessagesThenUser is the chat-completions
// twin of the Anthropic pin: this API has no mixed turn, so the same
// canonical message must arrive as role:"tool" messages (in call order) and
// THEN a user message carrying the person's words — the steer after the
// not-run answers, never merged into them.
func TestOpenAISteeredTurnSplitsIntoToolMessagesThenUser(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("survey the repo")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("call_a", "read", json.RawMessage(`{"path":"a.md"}`)),
			model.ToolUseBlock("call_b", "read", json.RawMessage(`{"path":"b.md"}`)),
		}},
		{Role: model.RoleUser, Content: []model.Block{
			model.ToolResultBlock("call_a", "not run: the person steered before this tool ran", true),
			model.ToolResultBlock("call_b", "not run: the person steered before this tool ran", true),
			model.TextBlock("stop reading, just summarize"),
		}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}
	msgs := wireBody(t, req)["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatalf("%d wire messages, want at least assistant + 2 tool + user", len(msgs))
	}
	t1 := msgs[len(msgs)-3].(map[string]any)
	t2 := msgs[len(msgs)-2].(map[string]any)
	if t1["role"] != "tool" || t1["tool_call_id"] != "call_a" {
		t.Fatalf("first result = %#v, want a tool message for call_a", t1)
	}
	if t2["role"] != "tool" || t2["tool_call_id"] != "call_b" {
		t.Fatalf("second result = %#v, want a tool message for call_b", t2)
	}
	user := msgs[len(msgs)-1].(map[string]any)
	if user["role"] != "user" || user["content"] != "stop reading, just summarize" {
		t.Fatalf("the steer's words = %#v, want a user message AFTER the tool messages", user)
	}
}

// ── S69 (AT6): the widened block union on /v1/chat/completions ──────────────
//
// This wire carries HALF the new union: an image has a documented shape (an
// `image_url` part with a data URL, user turns only), a document has NONE.
// Three duties, each pinned below: the parts array for an image, byte-identity
// for every turn that predates the widening, and a refusal — not a fallback —
// for a document.

// The user turn carrying an image becomes the provider's array form: the words
// FIRST as one text part — the exact string the no-image path sends,
// decoration added nowhere (AT7) — then the image parts in block order.
// `image_url` is an OBJECT here; the responses wire takes the same data URL as
// a plain string, and the pair of shapes is exactly what the two adapters
// exist to keep apart.
func TestOpenAIChatImagesBecomeThePartsArrayWithADataURL(t *testing.T) {
	tests := []struct {
		name    string
		content []model.Block
		// want is the user message's `content` serialized, pinned to the
		// byte: array shape, key names, and data URL included.
		want string
	}{
		{
			name: "words then image",
			content: []model.Block{
				model.TextBlock("what is in this chart?"),
				model.ImageDataBlock("chart.png", "image/png", attachPNGBytes),
			},
			want: `[{"text":"what is in this chart?","type":"text"},` +
				`{"image_url":{"url":"data:image/png;base64,` + attachPNGBytes + `"},"type":"image_url"}]`,
		},
		{
			name: "two images keep their block order",
			content: []model.Block{
				model.TextBlock("both"),
				model.ImageDataBlock("a.png", "image/png", attachPNGBytes),
				model.ImageDataBlock("b.webp", "image/webp", "WUVYRklYVFVSRXdlYnA="),
			},
			want: `[{"text":"both","type":"text"},` +
				`{"image_url":{"url":"data:image/png;base64,` + attachPNGBytes + `"},"type":"image_url"},` +
				`{"image_url":{"url":"data:image/webp;base64,WUVYRklYVFVSRXdlYnA="},"type":"image_url"}]`,
		},
		{
			name:    "an image with no words at all still gets the array, and no empty text part",
			content: []model.Block{model.ImageDataBlock("paste.png", "image/png", attachPNGBytes)},
			want:    `[{"image_url":{"url":"data:image/png;base64,` + attachPNGBytes + `"},"type":"image_url"}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newOpenAI()
			req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
				{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("be brief")}},
				{Role: model.RoleUser, Content: tc.content},
			}, Route: testRoute(ProviderOpenAI)})
			if err != nil {
				t.Fatal(err)
			}

			msgs := wireBody(t, req)["messages"].([]any)
			user := msgs[len(msgs)-1].(map[string]any)
			parts, ok := user["content"].([]any)
			if !ok {
				t.Fatalf("user content is %T, want the PARTS ARRAY when an image rides the turn", user["content"])
			}
			raw, err := json.Marshal(parts)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.want {
				t.Fatalf("the parts array is not the pinned wire bytes:\n  got  %s\n  want %s", raw, tc.want)
			}
			// And the system turn beside it is still a plain string.
			if sys := msgs[0].(map[string]any); sys["content"] != "be brief" {
				t.Errorf("the system turn's content = %#v, want the string — only a user turn WITH an image becomes an array", sys["content"])
			}
		})
	}
}

// THE pin the task names: a text-only request — the shape every route and
// every fixture has always sent — serializes to EXACTLY the bytes it sent
// before the block union widened. String content, no array, same key set.
// Compared on the RAW body, not a re-marshalled one: this is the byte
// identity D13 promises, so this is the test that would fail if the
// parts-array branch had leaked into the string path.
func TestOpenAIChatTextOnlyRequestIsThePinnedPreS69Bytes(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("be brief")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"max_completion_tokens":1024,"messages":[` +
		`{"content":"be brief","role":"system"},` +
		`{"content":"hello","role":"user"}],` +
		`"model":"test-model","stream":true,"stream_options":{"include_usage":true}}`
	if string(raw) != want {
		t.Fatalf("a text-only request no longer serializes as it did before S69:\n  got  %s\n  want %s", raw, want)
	}
}

// AT6, the modality-honesty half: a document has NO shape on
// /v1/chat/completions. The answer is not a drop (the turn would read as if
// the attachment were never made — M4's silence) and not a text fallback
// ("no pretending a PDF is a string of text"). The REQUEST is refused from
// request building, and the refusal is the repo's shape (AGENTS §5): it names
// the route it was aimed at, the document it refused, and the fix — the
// responses wire, where a document is an `input_file`.
//
// capability.go is not consulted, deliberately: its five flags describe what
// a SERVER varies on, and no chat/completions server anywhere accepts a
// document — this is the WIRE's absence, stated at build time. The refusal IS
// the honesty mechanism for it (see the report).
func TestOpenAIChatDocumentIsRefusedNamingTheRouteAndTheFix(t *testing.T) {
	for _, role := range []model.Role{model.RoleUser, model.RoleAssistant} {
		t.Run("document on a "+string(role)+" turn", func(t *testing.T) {
			a := newOpenAI()
			_, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
				{Role: role, Content: []model.Block{
					model.TextBlock("read this"),
					model.DocumentDataBlock("spec.pdf", "application/pdf", attachPDFBytes),
				}},
			}, Route: testRoute(ProviderOpenAI)})
			if err == nil {
				t.Fatal("built a chat/completions request carrying a document — there is no shape for one there, and an invented one is the failure AT6 forbids")
			}
			msg := err.Error()
			for _, want := range []string{"test-route", "chat/completions", "responses", "input_file", "spec.pdf", "application/pdf"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal does not name %q (route, route name, fix, target shape, and the refused thing itself): %v", want, msg)
				}
			}
		})
	}
}

// Two omissions, both counted in the request direction's own number (M4/O1),
// never half-sent: a block still in REFERENCE form skipped the send-time
// expansion door upstream (AT5 — its "bytes" would be a document name), and
// an image on a turn whose role has no part for one (this wire takes image
// parts on USER turns only). The person's words ride regardless (AT7/AT8): an
// unsendable rider never takes the sentence with it.
func TestOpenAIChatReferenceFormAndWrongRoleImagesAreCountedNotSent(t *testing.T) {
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{
			model.TextBlock("see the chart"),
			model.ImageRefBlock("chart.png", "image/png", "@attachments/ab12cd34-chart.png", 3),
		}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.TextBlock("here you are"),
			model.ImageDataBlock("out.png", "image/png", attachPNGBytes),
		}},
	}, Route: testRoute(ProviderOpenAI)})
	if err != nil {
		t.Fatal(err)
	}

	// The body is a one-shot stream, so it is read ONCE and both kinds of
	// assertion run over those raw bytes — stronger than the re-marshalled
	// string `wireJSON` gives, because it is what the wire would have
	// actually carried.
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, forbidden := range []string{"image_url", "data:image", "chart.png", "out.png"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("an unsendable image reached the wire (%q):\n%s", forbidden, body)
		}
	}
	for _, word := range []string{"see the chart", "here you are"} {
		if !strings.Contains(body, word) {
			t.Errorf("the words were dropped with the unsendable image (%q):\n%s", word, body)
		}
	}
	// Both turns stayed plain strings — no array without a sendable image.
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("the wire body is not JSON: %v\n%s", err, raw)
	}
	msgs := parsed["messages"].([]any)
	if got := msgs[0].(map[string]any)["content"]; got != "see the chart" {
		t.Errorf("user content = %#v, want the plain string — no image was sendable, so the pre-S69 shape must stand", got)
	}
	if got := msgs[1].(map[string]any)["content"]; got != "here you are" {
		t.Errorf("assistant content = %#v, want the plain string", got)
	}
	if got := a.AttachmentDropped(); got != 2 {
		t.Fatalf("AttachmentDropped = %d, want 2 — reference form and wrong role, each counted (M4)", got)
	}
	if _, _, dropped := a.Finalise(); dropped != 0 {
		t.Error("request-direction attachment drops leaked into the response-direction `dropped` (O1)")
	}
}

// The client reads the attachment count by TYPE ASSERTION on the dispatcher
// it was handed — the same hole OwedToolErrors and ThinkingDropped have tests
// for. A wrapper that satisfied the interface and answered "nothing was
// dropped" for the wire that withheld an attachment would be invisible from
// every surface that reports the step.
func TestTheDispatcherForwardsAttachmentDropped(t *testing.T) {
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	td, ok := adapter.(interface{ AttachmentDropped() int })
	if !ok {
		t.Fatal("the openai dispatcher does not forward AttachmentDropped — the count would be a wrapper's silent zero")
	}
	if _, err := adapter.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{
			model.TextBlock("see the chart"),
			model.ImageRefBlock("chart.png", "image/png", "@attachments/ab12cd34-chart.png", 3),
		}},
	}, Route: testRoute(ProviderOpenAI)}); err != nil {
		t.Fatal(err)
	}
	if n := td.AttachmentDropped(); n != 1 {
		t.Errorf("AttachmentDropped = %d, want 1 through the dispatcher", n)
	}
}
