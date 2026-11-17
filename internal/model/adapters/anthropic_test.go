package adapters

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// --- ToWire ------------------------------------------------------------------

func TestAnthropicSystemIsHoistedToATopLevelParameterAndRemovedFromTheArray(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("be brief")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)

	if body["system"] != "be brief" {
		t.Fatalf("system = %#v, want the hoisted prompt", body["system"])
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 — the system prompt must be REMOVED from the array, "+
			"not duplicated into it", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "user" {
		t.Errorf("the remaining message is %#v, want the user turn", msgs[0])
	}
}

// Two system messages join rather than one winning silently.
func TestAnthropicMultipleSystemMessagesAreJoined(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("first")}},
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("second")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}
	system, _ := wireBody(t, req)["system"].(string)
	if !strings.Contains(system, "first") || !strings.Contains(system, "second") {
		t.Fatalf("system = %q, want both parts — one silently winning is a prompt the caller "+
			"did not write", system)
	}
}

func TestAnthropicNoSystemMessageMeansTheParameterIsOmittedNotEmpty(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req)["system"]; present {
		t.Error("`system` was sent when there is none. An empty string is a system prompt that " +
			"says nothing, which is a different thing from not having one")
	}
}

// **The merge rule.** The row of the provider table most easily missed, because
// it works with one tool call and fails with two.
func TestAnthropicTwoToolResultsForOneTurnBecomeExactlyOneUserMessage(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("two cities")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("toolu_1", "get_weather", json.RawMessage(`{"location":"Lisbon"}`)),
			model.ToolUseBlock("toolu_2", "get_weather", json.RawMessage(`{"location":"Porto"}`)),
		}},
		// Two SEPARATE canonical messages, each with one result — the shape a
		// caller naturally builds, and the one this provider rejects.
		{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock("toolu_1", "18C", false)}},
		{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock("toolu_2", "17C", false)}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	var resultMsgs []map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		content, ok := mm["content"].([]any)
		if !ok || len(content) == 0 {
			continue
		}
		if first, ok := content[0].(map[string]any); ok && first["type"] == "tool_result" {
			resultMsgs = append(resultMsgs, mm)
		}
	}

	if len(resultMsgs) != 1 {
		t.Fatalf("got %d messages carrying tool results, want EXACTLY 1. N separate tool-role "+
			"messages are rejected by this provider — and it works with one tool call, which is "+
			"why this is the row that gets missed", len(resultMsgs))
	}
	content := resultMsgs[0]["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("the merged message carries %d blocks, want both results", len(content))
	}
	if resultMsgs[0]["role"] != "user" {
		t.Errorf("the merged message's role is %v, want user", resultMsgs[0]["role"])
	}
	// Order preserved: results must match their calls.
	if content[0].(map[string]any)["tool_use_id"] != "toolu_1" {
		t.Errorf("the results were reordered: %#v", content)
	}
}

func TestAnthropicMaxTokensIsAlwaysPresent(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)
	if _, present := body["max_tokens"]; !present {
		t.Error("max_tokens is absent, and this provider REJECTS a call without it (M16)")
	}
	if _, wrong := body["max_completion_tokens"]; wrong {
		t.Error("max_completion_tokens is present; that is the other provider's field name")
	}
}

func TestAnthropicToolDeclarationsUseInputSchema(t *testing.T) {
	a := newAnthropic()
	const schema = `{"type":"object","properties":{"location":{"type":"string"}}}`
	req, err := a.ToWire(model.ModelRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Tools:    []model.ToolSpec{{Name: "get_weather", Description: "look up", Parameters: json.RawMessage(schema)}},
		Route:    testRoute(ProviderAnthropic),
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := wireBody(t, req)["tools"].([]any)[0].(map[string]any)

	if _, present := tool["input_schema"]; !present {
		t.Error("the tool has no `input_schema`")
	}
	if _, wrong := tool["parameters"]; wrong {
		t.Error("the tool uses `parameters`, which is the other provider's name")
	}
	if _, wrapped := tool["function"]; wrapped {
		t.Error("the tool is wrapped in a `function` object, which is the other provider's shape")
	}
}

func TestAnthropicToolUseInputIsSentAsAnObjectNotAString(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather?")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("toolu_1", "get_weather", json.RawMessage(`{"location":"Lisbon"}`)),
		}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	assistant := msgs[len(msgs)-1].(map[string]any)
	block := assistant["content"].([]any)[0].(map[string]any)

	input, isObject := block["input"].(map[string]any)
	if !isObject {
		t.Fatalf("input is %T, want an OBJECT — a JSON string is the other provider's shape, "+
			"and sending one here produces a tool call whose arguments the model cannot read",
			block["input"])
	}
	if input["location"] != "Lisbon" {
		t.Fatalf("input = %#v", input)
	}
}

// > *"When extended thinking is enabled, you must pass thinking blocks back
// > unmodified, including the signature field. The API verifies the signature
// > and rejects requests where thinking blocks have been altered."*
//
// — the provider's extended-thinking documentation, quoted because it is the
// constraint this test exists for. **S7 recovers a step by replaying a stored
// transcript**, so a signature mangled here fails much later, in a different
// slice, with an error about something else.
func TestAnthropicASignedThinkingBlockRoundTripsByteIdentically(t *testing.T) {
	// 1. Stream the signed fixture.
	a := newAnthropic()
	deltas := drainDeltas(a, framesFromFixture(t, "anthropic", "signed-thinking"))

	// The signature must NOT have appeared as content.
	for _, d := range deltas {
		if d.Kind == model.DeltaThinking && strings.Contains(d.Text, "ErUBCkY") {
			t.Fatalf("the signature leaked into a thinking delta: %q", d.Text)
		}
	}

	// 2. Reassemble the canonical message from what Finalise exposes.
	thinking := a.CompletedThinking()
	if len(thinking) != 1 {
		t.Fatalf("CompletedThinking returned %d blocks, want 1 — without this the signature "+
			"never reaches storage, because the streamed deltas do not carry it", len(thinking))
	}

	const wantThinking = "The user wants the weather. I should call the tool rather than guess."
	const wantSignature = "ErUBCkYIBBgCKkB3+Zm3/Lx1FIXTUREsignature==/abc+def=="

	if thinking[0].Text != wantThinking {
		t.Errorf("thinking text:\n  got  %q\n  want %q", thinking[0].Text, wantThinking)
	}
	// **The exact string**, not its presence.
	if thinking[0].Signature != wantSignature {
		t.Fatalf("THE SIGNATURE CHANGED:\n  got  %q\n  want %q\n"+
			"The provider verifies this and rejects an altered one; the failure would surface "+
			"in S7's recovery, not here", thinking[0].Signature, wantSignature)
	}

	// 3. Pass it back through ToWire and assert the emitted block is deep-equal
	//    to the fixture's original.
	//
	// The turn carries its PRODUCER (S18 T5, M27): it came out of Anthropic's
	// own signed fixture two lines above, so stamping `anthropic` here is
	// stating the truth of the scenario, not flattering the adapter. An
	// unstamped turn is no one's, and T5 withholds it — see replay_test.go
	// for the turn that is not this provider's and does not replay.
	prov := model.NewProvenance(testRoute(ProviderAnthropic))
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather?")}},
		{Role: model.RoleAssistant, Content: []model.Block{thinking[0], model.TextBlock("Checking now.")}, Provenance: &prov},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	assistant := msgs[len(msgs)-1].(map[string]any)
	emitted := assistant["content"].([]any)[0].(map[string]any)

	want := map[string]any{
		"type":      "thinking",
		"thinking":  wantThinking,
		"signature": wantSignature,
	}
	if !reflect.DeepEqual(emitted, want) {
		t.Fatalf("the wire block is not deep-equal to the original:\n  got  %#v\n  want %#v",
			emitted, want)
	}
}

func TestAnthropicSignatureDeltaIsAccumulatedButNeverYieldedAsContent(t *testing.T) {
	a := newAnthropic()
	frames := make(chan model.Frame, 5)
	for _, data := range []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reasoning"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-PART-1"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-PART-2"}}`,
		`{"type":"content_block_stop","index":0}`,
	} {
		frames <- model.Frame{Data: data}
	}
	close(frames)

	for _, d := range drainDeltas(a, frames) {
		if strings.Contains(d.Text, "SIG-PART") {
			t.Fatalf("a signature was yielded as content in a %s delta: %q.\n"+
				"It is replay metadata, not display content — a client rendering it shows a "+
				"reader a base64 blob", d.Kind, d.Text)
		}
	}

	// Accumulated across BOTH deltas: a signature can arrive in pieces.
	thinking := a.CompletedThinking()
	if len(thinking) != 1 || thinking[0].Signature != "SIG-PART-1SIG-PART-2" {
		t.Fatalf("the signature was not accumulated across deltas: %+v", thinking)
	}
}

func TestAnthropicFinaliseExposesCompletedThinkingBlocksWithSignatures(t *testing.T) {
	a := newAnthropic()
	drainDeltas(a, framesFromFixture(t, "anthropic", "signed-thinking"))

	blocks := a.CompletedThinking()
	if len(blocks) == 0 {
		t.Fatal("no completed thinking blocks")
	}
	for i, b := range blocks {
		if b.Kind != model.BlockThinking {
			t.Errorf("block %d has kind %q", i, b.Kind)
		}
		if b.Signature == "" {
			t.Errorf("block %d has no signature; S5 would persist an unsigned thinking block "+
				"and S7's replay would be rejected", i)
		}
		if err := b.Validate(); err != nil {
			t.Errorf("block %d does not validate: %v", i, err)
		}
	}
}

// --- FromWire ----------------------------------------------------------------

func TestAnthropicUsageIsAccumulatedFromBothMessageStartAndMessageDelta(t *testing.T) {
	a := newAnthropic()
	drainDeltas(a, framesFromFixture(t, "anthropic", "text-only"))

	usage, stop, _ := a.Finalise()
	// input comes from message_start, output from message_delta — two separate
	// events, which is why accumulation must add rather than replace.
	if usage.InputTokens == nil || *usage.InputTokens != 12 {
		t.Errorf("inputTokens = %v, want 12 from message_start", usage.InputTokens)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != 5 {
		t.Errorf("outputTokens = %v, want 5 from message_delta — if this is nil, the second "+
			"event replaced the first instead of adding to it", usage.OutputTokens)
	}
	if stop != "end_turn" {
		t.Errorf("stopReason = %q", stop)
	}
}

func TestAnthropicCacheCreationMapsToCacheWriteAndCacheReadToCacheRead(t *testing.T) {
	a := newAnthropic()
	frames := make(chan model.Frame, 2)
	frames <- model.Frame{Data: `{"type":"message_start","message":{"usage":{"input_tokens":10,` +
		`"cache_creation_input_tokens":200,"cache_read_input_tokens":300}}}`}
	close(frames)
	drainDeltas(a, frames)

	usage, _, _ := a.Finalise()
	if usage.CacheWriteTokens == nil || *usage.CacheWriteTokens != 200 {
		t.Errorf("cacheWriteTokens = %v, want 200 from cache_creation_input_tokens", usage.CacheWriteTokens)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 300 {
		t.Errorf("cacheReadTokens = %v, want 300 from cache_read_input_tokens", usage.CacheReadTokens)
	}
	// Crossing these two would price a call at the wrong rate — cache writes
	// cost more than reads — and the figure would look plausible.
	if usage.CacheWriteTokens != nil && *usage.CacheWriteTokens == 300 {
		t.Error("cache read and write are crossed; a write costs more than a read, so the " +
			"figure would be wrong and plausible")
	}
}

func TestAnthropicXAPIKeyAndVersionAreSetAndAuthorizationIsAbsent(t *testing.T) {
	a := newAnthropic()
	route := testRoute(ProviderAnthropic)
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}},
	}, Route: route})
	if err != nil {
		t.Fatal(err)
	}

	if req.Header.Get("x-api-key") != route.APIKey {
		t.Errorf("x-api-key = %q, want the route's key", req.Header.Get("x-api-key"))
	}
	if got := req.Header.Get("anthropic-version"); got != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", got, anthropicVersion)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("an Authorization header is set. Copying the other adapter's header set is the " +
			"natural mistake here, and it fails with a confusing 401")
	}
	if !strings.HasSuffix(req.URL.Path, "/messages") {
		t.Errorf("path = %q, want /messages", req.URL.Path)
	}
}

func TestAnthropicCompleteIDUpFrontReassemblesFragmentedArguments(t *testing.T) {
	a := newAnthropic()
	deltas := drainDeltas(a, framesFromFixture(t, "anthropic", "tool-call"))

	var tools []*model.Block
	for _, d := range deltas {
		if d.Kind == model.DeltaToolUse {
			tools = append(tools, d.Block)
		}
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(tools))
	}
	// The id arrives COMPLETE on content_block_start here, unlike OpenAI's
	// first-fragment-only id.
	if tools[0].ID != "toolu_FIXTURE1" || tools[0].Name != "get_weather" {
		t.Fatalf("id or name lost: %+v", tools[0])
	}
	var args map[string]any
	if err := json.Unmarshal(tools[0].Input, &args); err != nil {
		t.Fatalf("the reassembled input_json_delta fragments are not valid JSON: %v", err)
	}
	if args["location"] != "Lisbon" || args["unit"] != "c" {
		t.Fatalf("arguments = %s", tools[0].Input)
	}
}

func TestAnthropicTwoToolCallsInOneTurnBothArriveComplete(t *testing.T) {
	a := newAnthropic()
	var tools []*model.Block
	for _, d := range drainDeltas(a, framesFromFixture(t, "anthropic", "two-tool-calls")) {
		if d.Kind == model.DeltaToolUse {
			tools = append(tools, d.Block)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(tools))
	}
	if tools[0].ID == tools[1].ID {
		t.Fatal("both tool calls have the same id; their blocks were keyed wrongly")
	}
	for i, b := range tools {
		var args map[string]any
		if err := json.Unmarshal(b.Input, &args); err != nil {
			t.Fatalf("tool %d has invalid input: %v", i, err)
		}
		if args["location"] == nil {
			t.Errorf("tool %d lost its arguments: %s", i, b.Input)
		}
	}
	if tools[0].Input == nil || string(tools[0].Input) == string(tools[1].Input) {
		t.Error("the two tool calls' arguments are identical; their buffers were crossed")
	}
}

func TestAnthropicNoPartialToolUseIsEverYielded(t *testing.T) {
	for _, scenario := range []string{"tool-call", "two-tool-calls", "text-only", "signed-thinking", "usage-absent"} {
		t.Run(scenario, func(t *testing.T) {
			a := newAnthropic()
			for _, d := range drainDeltas(a, framesFromFixture(t, "anthropic", scenario)) {
				if d.Kind != model.DeltaToolUse {
					continue
				}
				if d.Block == nil || d.Block.ID == "" || d.Block.Name == "" || d.Block.Input == nil {
					t.Errorf("a partial tool_use was yielded: %+v", d.Block)
				}
				if err := d.Block.Validate(); err != nil {
					t.Errorf("a yielded tool_use does not validate: %v", err)
				}
			}
		})
	}
}

func TestAnthropicAFixtureWithNoUsageYieldsAUsageWithNothingSet(t *testing.T) {
	a := newAnthropic()
	drainDeltas(a, framesFromFixture(t, "anthropic", "usage-absent"))
	usage, _, _ := a.Finalise()
	if !usage.Empty() {
		t.Fatalf("usage = %+v, want every field nil", usage)
	}
}

// M4 — content mapping to no canonical kind is dropped and COUNTED.
func TestAnthropicUnmappedContentIncrementsDroppedAndDoesNotBreakTheStream(t *testing.T) {
	a := newAnthropic()
	deltas := drainDeltas(a, framesFromFixture(t, "anthropic", "unmapped-content"))

	_, _, dropped := a.Finalise()
	if dropped == 0 {
		t.Fatal("a redacted_thinking block was dropped SILENTLY. A silent drop is how a block " +
			"disappears between two releases and nobody can say when (M4)")
	}
	// And the text after it still arrives — one unmapped block must not end the
	// stream.
	var text string
	for _, d := range deltas {
		if d.Kind == model.DeltaText {
			text += d.Text
		}
	}
	if !strings.Contains(text, "Answer after redacted reasoning") {
		t.Errorf("the text after the unmapped block was lost: %q", text)
	}
}

// A ping is a keep-alive and must not count as dropped, or `dropped` becomes a
// measure of how long the stream took.
func TestAnthropicAPingIsNotCountedAsDropped(t *testing.T) {
	a := newAnthropic()
	drainDeltas(a, framesFromFixture(t, "anthropic", "text-only"))
	_, _, dropped := a.Finalise()
	if dropped != 0 {
		t.Fatalf("dropped = %d on a clean fixture containing a ping; a keep-alive is not "+
			"unmapped content", dropped)
	}
}

// ── S31 T4: the steered turn's shape ────────────────────────────────────────

// TestAnthropicSteeredTurnPutsResultsThenTextInOneUserMessage pins the exact
// canonical shape the runtime builds when a steer overtakes a turn of tool
// calls (LW2): ONE user message, the not-run results first, the person's
// words after. If the adapter dropped the text or promoted it above the
// results, the model would read an instruction before learning the calls
// never happened — the ordering is the meaning.
func TestAnthropicSteeredTurnPutsResultsThenTextInOneUserMessage(t *testing.T) {
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("survey the repo")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.ToolUseBlock("toolu_a", "read", json.RawMessage(`{"path":"a.md"}`)),
			model.ToolUseBlock("toolu_b", "read", json.RawMessage(`{"path":"b.md"}`)),
		}},
		// The steered message: both calls closed not-run, then the steer's text.
		{Role: model.RoleUser, Content: []model.Block{
			model.ToolResultBlock("toolu_a", "not run: the person steered before this tool ran", true),
			model.ToolResultBlock("toolu_b", "not run: the person steered before this tool ran", true),
			model.TextBlock("stop reading, just summarize"),
		}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}
	msgs := wireBody(t, req)["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" {
		t.Fatalf("the steered turn's role = %v, want user", last["role"])
	}
	content := last["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("the steered turn carries %d blocks, want result, result, text", len(content))
	}
	for i, wantID := range []string{"toolu_a", "toolu_b"} {
		b := content[i].(map[string]any)
		if b["type"] != "tool_result" || b["tool_use_id"] != wantID || b["is_error"] != true {
			t.Fatalf("block %d = %#v, want an isError tool_result for %s", i, b, wantID)
		}
	}
	text := content[2].(map[string]any)
	if text["type"] != "text" || text["text"] != "stop reading, just summarize" {
		t.Fatalf("the person's words are not last in their own turn: %#v", text)
	}
}
