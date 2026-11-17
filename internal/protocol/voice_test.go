package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

type fakeVoice struct {
	routes   map[string]model.Route
	gotAudio []byte
	messages []model.Message
}

func (f *fakeVoice) Route(id string) (model.Route, bool) { r, ok := f.routes[id]; return r, ok }
func (f *fakeVoice) RouteIDs() []string                  { return nil }
func (f *fakeVoice) Transcribe(_ context.Context, _ model.Route, audio []byte, _, _ string) (string, error) {
	f.gotAudio = audio
	return "hello orion", nil
}
func (f *fakeVoice) Speak(_ context.Context, _ model.Route, text, _, _ string) ([]byte, string, error) {
	return []byte("A:" + text), "audio/mpeg", nil
}
func (f *fakeVoice) Embed(_ context.Context, _ model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	return [][]float32{{0.1, 0.2, 0.3}}, model.Usage{}, model.Cost{}, nil
}
func (f *fakeVoice) Stream(_ context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	f.messages = req.Messages
	d := make(chan model.Delta, 2)
	r := make(chan model.StreamResult)
	e := make(chan error, 1)
	b := model.ToolUseBlock("c1", "loops_list", json.RawMessage(`{}`))
	d <- model.Delta{Kind: model.DeltaText, Text: "Checking. "}
	d <- model.Delta{Kind: model.DeltaToolUse, Block: &b}
	close(d)
	close(r)
	close(e)
	return d, r, e
}

// Voice calls need the right capability on the route they name, carry the audio and text through, and a turn hands
// the model's tool calls back without running them.
func TestVoiceMethodsGateByCapabilityAndCarryTheirPayloads(t *testing.T) {
	f := &fakeVoice{routes: map[string]model.Route{
		"whisper":  {ID: "whisper", Capabilities: []string{"asr"}},
		"speech":   {ID: "speech", Capabilities: []string{"tts"}},
		"chat":     {ID: "chat", Capabilities: []string{"text"}},
		"embedder": {ID: "embedder", Capabilities: []string{"embedding"}},
		"keyed":    {ID: "keyed", Provider: "openai", APIKey: "saved-key", Capabilities: []string{"tts"}},
	}}
	tbl := &Table{Routes: f, Models: f}
	ctx := context.Background()
	mk := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

	if res, err := tbl.handleVoiceEmbed(ctx, nil, mk(VoiceEmbedParams{Route: "embedder", Text: "hi"})); err != nil || res.(VoiceEmbedResult).Dimensions != 3 {
		t.Fatalf("embed: %+v %v", res, err)
	}
	if _, err := tbl.handleVoiceEmbed(ctx, nil, mk(VoiceEmbedParams{Route: "chat", Text: "hi"})); err == nil || !strings.Contains(err.Error(), "embedding") {
		t.Fatalf("a chat model must not be asked to embed: %v", err)
	}

	// An unsaved draft is tested without being saved, inherits a saved route's key, and is held to its own capabilities.
	if res, err := tbl.handleVoiceEmbed(ctx, nil, mk(VoiceEmbedParams{Draft: &VoiceDraft{Provider: "openai", Model: "m", Capabilities: []string{"embedding"}}, Text: "hi"})); err != nil || res.(VoiceEmbedResult).Dimensions != 3 {
		t.Fatalf("draft embed: %+v %v", res, err)
	}
	if _, err := tbl.handleVoiceEmbed(ctx, nil, mk(VoiceEmbedParams{Draft: &VoiceDraft{Provider: "openai", Model: "m", Capabilities: []string{"text"}}, Text: "hi"})); err == nil || !strings.Contains(err.Error(), "embedding") {
		t.Fatalf("a draft without the capability must be refused: %v", err)
	}
	if _, err := tbl.handleVoiceSpeak(ctx, nil, mk(VoiceSpeakParams{Draft: &VoiceDraft{Provider: "openai", Model: "m", AudioAPI: "soap", Capabilities: []string{"tts"}}, Text: "hi"})); err == nil {
		t.Fatal("an unknown audioApi in a draft must be refused")
	}
	// The stored key is never lent to a different endpoint than the one it was saved for.
	if r, err := tbl.voiceDraftRoute(&VoiceDraft{ID: "keyed", Provider: "openai", Model: "m", BaseURL: "http://attacker.example/v1", Capabilities: []string{"tts"}}, "tts"); err != nil || r.APIKey != "" {
		t.Fatalf("a draft pointing a saved id at another host must not get its key: %+v %v", r, err)
	}
	if r, err := tbl.voiceDraftRoute(&VoiceDraft{ID: "keyed", Provider: "anthropic", Model: "m", Capabilities: []string{"tts"}}, "tts"); err != nil || r.APIKey != "" {
		t.Fatalf("a draft changing the provider must not get the key: %+v %v", r, err)
	}
	if r, err := tbl.voiceDraftRoute(&VoiceDraft{ID: "keyed", Provider: "openai", Model: "m", Capabilities: []string{"tts"}}, "tts"); err != nil || r.APIKey != "saved-key" {
		t.Fatalf("a draft naming a saved route must use its key: %+v %v", r, err)
	}

	res, err := tbl.handleVoiceTranscribe(ctx, nil, mk(VoiceTranscribeParams{Route: "whisper", Audio: base64.StdEncoding.EncodeToString([]byte("PCM"))}))
	if err != nil || res.(VoiceTranscribeResult).Text != "hello orion" || string(f.gotAudio) != "PCM" {
		t.Fatalf("transcribe: %+v %v", res, err)
	}
	if _, err := tbl.handleVoiceTranscribe(ctx, nil, mk(VoiceTranscribeParams{Route: "chat", Audio: "UENN"})); err == nil || !strings.Contains(err.Error(), "asr") {
		t.Fatalf("a chat model must not be sent a recording: %v", err)
	}
	sp, err := tbl.handleVoiceSpeak(ctx, nil, mk(VoiceSpeakParams{Route: "speech", Text: "Hi."}))
	if err != nil || sp.(VoiceSpeakResult).Mime != "audio/mpeg" {
		t.Fatalf("speak: %+v %v", sp, err)
	}
	if _, err := tbl.handleVoiceSpeak(ctx, nil, mk(VoiceSpeakParams{Route: "whisper", Text: "Hi."})); err == nil || !strings.Contains(err.Error(), "tts") {
		t.Fatalf("a recogniser must not be asked to speak: %v", err)
	}
	turn, err := tbl.handleVoiceTurn(ctx, nil, mk(VoiceTurnParams{Route: "chat", System: "s", Messages: []VoiceMessage{
		{Role: "user", Text: "how many agents"},
		{Role: "assistant", ToolCalls: []VoiceToolCall{{ID: "c0", Name: "loops_list"}}},
		{Role: "tool", ToolUseID: "c0", Text: "[]"},
	}, Tools: []VoiceToolSpec{{Name: "loops_list", Description: "List."}}}))
	if err != nil {
		t.Fatal(err)
	}
	out := turn.(VoiceTurnResult)
	if out.Text != "Checking." || len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "loops_list" {
		t.Fatalf("turn: %+v", out)
	}
	// system, user, assistant(tool call), user(tool result)
	if len(f.messages) != 4 || f.messages[3].Content[0].Kind != model.BlockToolResult {
		t.Fatalf("the tool result must follow its call as a user message: %+v", f.messages)
	}
}
