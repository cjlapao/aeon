package adapters

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// The wire half of the slice's claim (S18 T8): what the round trip LOOKS
// like at each endpoint, for a transcript that spans both. The runtime's
// three legs live in runtime/crossprovider_test.go; this file pins the two
// sentences that complete the claim:
//
//   - the OpenAI wire carries NO thinking — neither the anthropic-signed
//     block (which the store keeps verbatim) nor any other — and every
//     withheld block is counted (M4, O1);
//   - the Anthropic wire replays ITS OWN block byte-identically, signature
//     and all, even when foreign turns sit beside it in the same transcript
//     (M17) — and it withholds the foreign block, counting that too (M27,
//     keyed on the PRODUCER, O6).

// crossTranscript: an anthropic turn with a signed thinking block, a tool
// round, then an openai turn carrying a thinking block of its own (the
// canonical format permits carrying what a wire can express; the adapter
// decides what its provider may see).
func crossTranscript() []model.Message {
	return []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			{Kind: model.BlockThinking, Text: "reasoned about it", Signature: "sig-anthropic-abc"},
			{Kind: model.BlockToolUse, ID: "t1", Name: "read", Input: []byte(`{"path":"a.md"}`)},
		}, Provenance: &model.Provenance{Route: "claude-route", Provider: "anthropic", Model: "claude-opus"}},
		{Role: model.RoleUser, Content: []model.Block{
			{Kind: model.BlockToolResult, ToolUseID: "t1", Content: "the readme"},
		}},
		{Role: model.RoleAssistant, Content: []model.Block{
			{Kind: model.BlockThinking, Text: "gpt mused"},
			{Kind: model.BlockText, Text: "answered on openai"},
		}, Provenance: &model.Provenance{Route: "gpt-route", Provider: "openai", Model: "gpt-mini"}},
	}
}

func crossRequest(route model.Route) model.ModelRequest {
	return model.ModelRequest{Route: route, Messages: crossTranscript(),
		MaxOutputTokens: 1024,
		Tools:           []model.ToolSpec{{Name: "read", Description: "read", Parameters: []byte(`{"type":"object"}`)}}}
}

// wireJSON reads the request body as one re-marshalled JSON string: the
// assertions below are substring checks over the WHOLE wire (absence is as
// important as presence), which the parsed-map helper cannot give.
func wireJSON(t *testing.T, httpReq *http.Request) string {
	t.Helper()
	var parsed map[string]any
	raw, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("the wire body is not JSON: %v", err)
	}
	again, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return string(again)
}

func TestOpenAIRequestHasNoThinking(t *testing.T) {
	// The store keeps every thinking block (the runtime legs assert the
	// canonical transcript survives untouched); the WIRE carries none, and
	// the count of what fell is the adapter's — reaching the step record
	// through StreamResult (T5's path), never invented downstream.
	a := newOpenAI()
	httpReq, err := a.ToWire(crossRequest(testRoute(model.ProviderOpenAI)))
	if err != nil {
		t.Fatal(err)
	}
	body := wireJSON(t, httpReq)
	for _, forbidden := range []string{"thinking", "sig-anthropic-abc", "reasoned about it", "gpt mused"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the openai wire carries %q — no such field exists on that endpoint; the block belongs to the record, not the request (M27)", forbidden)
		}
	}
	if got := a.ThinkingDropped(); got != 2 {
		t.Errorf("ThinkingDropped = %d, want 2 — both withheld blocks counted, the anthropic-signed one first "+
			"(a drop nobody counted is M4's lost accounting)", got)
	}
}

func TestAnthropicReplayIsByteIdentical(t *testing.T) {
	// THE round-trip sentence: after the transcript has visited another
	// provider and come back, anthropic still receives its own signature
	// VERBATIM — the exact bytes it produced, in a block whose Text matches
	// too. This is the leg the store assertions protect: if the record had
	// leaked a drop, this request would be the (late, elsewhere) failure.
	a := newAnthropic()
	httpReq, err := a.ToWire(crossRequest(testRoute("anthropic")))
	if err != nil {
		t.Fatal(err)
	}
	body := wireJSON(t, httpReq)
	if !strings.Contains(body, "sig-anthropic-abc") || !strings.Contains(body, "reasoned about it") {
		t.Fatalf("the own-provider signed block was not replayed verbatim: %s", body)
	}
	if strings.Contains(body, "gpt mused") {
		t.Error("a foreign provider's thinking block reached anthropic's wire — signatures only ever go to their producer (M27, O6)")
	}
	if got := a.ThinkingDropped(); got != 1 {
		t.Errorf("ThinkingDropped = %d, want 1 — only the foreign block was withheld", got)
	}
	// The replayed block is not a reconstruction: compare the JSON bytes of
	// the block, not the presence of its parts (M17's standard is the
	// provider's, and the provider compares bytes).
	if !strings.Contains(body, `"signature":"sig-anthropic-abc"`) {
		t.Errorf("the signature field is not byte-shaped as produced: %s", body)
	}
}
