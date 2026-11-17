package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// # Voice: speech in, speech out, and the operator's turn
//
// Orion, the voice agent in the app, needs three things from the daemon and nothing else:
//
//   - `voice.transcribe` turns a recording into text with a route that has the `asr` capability;
//   - `voice.speak` turns a sentence into audio with a route that has the `tts` capability;
//   - `voice.turn` runs ONE model call for the operator, with the tools the app offers, and hands back what the model
//     said and which tools it asked for. The app runs the tools (as the person, through the same doors it uses
//     everywhere) and calls again with the results, so the tool loop lives where the person's authority and the
//     spoken confirmations live, and the daemon grows no second way to act.
//
// Routes are chosen by the caller by id; a route without the capability is refused by name, so a chat model is never
// sent a recording.

const (
	MethodVoiceTranscribe MethodName = "voice.transcribe"
	MethodVoiceSpeak      MethodName = "voice.speak"
	MethodVoiceTurn       MethodName = "voice.turn"
	// MethodVoiceEmbed embeds one short text with an `embedding` route and reports the vector's size: the check a person
	// runs from the model page to see the route answers. It is here because it is the same shape as the other model checks.
	MethodVoiceEmbed MethodName = "voice.embed"
)

// Largest recording and sentence accepted. A voice clip is seconds long; past this something is wrong upstream.
const (
	maxVoiceAudioBytes = 12 << 20
	maxVoiceTextRunes  = 4000
)

// VoiceModels is what a daemon's model client offers voice. It is optional on the transport, like Tracer.
type VoiceModels interface {
	Transcribe(ctx context.Context, route model.Route, audio []byte, mime, language string) (string, error)
	Speak(ctx context.Context, route model.Route, text, voice, format string) ([]byte, string, error)
}

// EmbedModels is the optional embedding half of the model transport (`*model.Client` satisfies it).
type EmbedModels interface {
	Embed(ctx context.Context, route model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error)
}

// VoiceDraft is a route as it stands in the model form, not yet saved, so a person can test a model before keeping it.
// It is used for one call and stored nowhere. When it names an existing route by `id` and leaves the key (or extra
// body) out, the saved route's key (or extra body) is used, so testing an edit of a saved model needs no re-typing of
// the key, but only when the draft's provider and base URL are still the saved route's own. The key travels the same socket `routes.set` uses and is not echoed back.
type VoiceDraft struct {
	ID           string   `json:"id,omitempty"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	BaseURL      string   `json:"baseUrl,omitempty"`
	APIKey       string   `json:"apiKey,omitempty"`
	AudioAPI     string   `json:"audioApi,omitempty"`
	ExtraBody    string   `json:"extraBody,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// VoiceEmbedParams: a route (or an unsaved draft of one) and a short text.
type VoiceEmbedParams struct {
	Route string      `json:"route,omitempty"`
	Draft *VoiceDraft `json:"draft,omitempty"`
	Text  string      `json:"text"`
}

// VoiceEmbedResult: how many numbers one text became, and what the provider charged in tokens.
type VoiceEmbedResult struct {
	Dimensions  int `json:"dimensions"`
	InputTokens int `json:"inputTokens,omitempty"`
}

// VoiceTranscribeParams: the recording as base64 with its media type.
type VoiceTranscribeParams struct {
	Route    string      `json:"route,omitempty"`
	Draft    *VoiceDraft `json:"draft,omitempty"`
	Audio    string      `json:"audio"`
	Mime     string      `json:"mime,omitempty"`
	Language string      `json:"language,omitempty"`
}

// VoiceTranscribeResult is what was said.
type VoiceTranscribeResult struct {
	Text string `json:"text"`
}

// VoiceSpeakParams: one sentence (or a few) to say.
type VoiceSpeakParams struct {
	Route  string      `json:"route,omitempty"`
	Draft  *VoiceDraft `json:"draft,omitempty"`
	Text   string      `json:"text"`
	Voice  string      `json:"voice,omitempty"`
	Format string      `json:"format,omitempty"`
}

// VoiceSpeakResult is the audio, base64, with its media type.
type VoiceSpeakResult struct {
	Audio string `json:"audio"`
	Mime  string `json:"mime"`
}

// VoiceToolSpec is one tool the operator may call.
type VoiceToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// VoiceToolCall is a tool the model asked for.
type VoiceToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// VoiceMessage is one turn of the operator's conversation: a person's words, the assistant's reply (with the tools it
// asked for), or a tool's result.
type VoiceMessage struct {
	Role      string          `json:"role"` // user | assistant | tool
	Text      string          `json:"text,omitempty"`
	ToolCalls []VoiceToolCall `json:"toolCalls,omitempty"`
	// For role tool: which call this answers, and whether it failed.
	ToolUseID string `json:"toolUseId,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
}

// VoiceTurnParams is one model call for the operator.
type VoiceTurnParams struct {
	Route    string          `json:"route"`
	System   string          `json:"system,omitempty"`
	Messages []VoiceMessage  `json:"messages"`
	Tools    []VoiceToolSpec `json:"tools,omitempty"`
}

// VoiceTurnResult is what the model said and the tools it asked for.
type VoiceTurnResult struct {
	Text      string          `json:"text"`
	ToolCalls []VoiceToolCall `json:"toolCalls,omitempty"`
}

func (t *Table) voiceRoute(id, capability string) (model.Route, error) {
	if strings.TrimSpace(id) == "" {
		return model.Route{}, ErrInvalidRoute.WithWhat("%s needs a route id", capability)
	}
	if t.Routes == nil {
		return model.Route{}, ErrProbeUnavailable.WithWhat("this daemon has no routes configured")
	}
	route, ok := t.Routes.Route(id)
	if !ok {
		return model.Route{}, ErrUnknownRoute.WithWhat("route %q is not configured", id)
	}
	if capability != "" {
		has := false
		for _, c := range route.Capabilities {
			if c == capability {
				has = true
			}
		}
		if !has {
			return model.Route{}, ErrBadParams{Err: fmt.Errorf("route %q does not declare the %q capability; set it on the model in Models first", id, capability)}
		}
	}
	return route, nil
}

// voiceDraftRoute builds the route a draft describes for one call, filling the key and extra body from the saved route it
// names when the draft leaves them out. The capability the call needs must be one the draft declares: the same rule a
// saved route is held to.
func (t *Table) voiceDraftRoute(d *VoiceDraft, capability string) (model.Route, error) {
	provider := strings.TrimSpace(d.Provider)
	if provider == "" || strings.TrimSpace(d.Model) == "" {
		return model.Route{}, ErrInvalidRoute.WithWhat("a draft needs a provider and a model name to be tested")
	}
	if !model.ValidAudioAPI(strings.TrimSpace(d.AudioAPI)) {
		return model.Route{}, ErrInvalidRoute.WithWhat("the draft sets audioApi %q; the accepted values are %s", d.AudioAPI, strings.Join(model.AudioAPIs(), ", "))
	}
	route := model.Route{
		ID: "draft", Provider: provider, Model: strings.TrimSpace(d.Model), BaseURL: strings.TrimSpace(d.BaseURL),
		APIKey: d.APIKey, AudioAPI: strings.TrimSpace(d.AudioAPI), Capabilities: d.Capabilities, MaxOutputTokens: 1,
	}
	if d.ExtraBody != "" {
		compact, err := ValidateExtraBody(d.ExtraBody)
		if err != nil {
			return model.Route{}, ErrInvalidRoute.WithWhat("the draft's extra body: %v", err)
		}
		route.ExtraBody = json.RawMessage(compact)
	}
	if id := strings.TrimSpace(d.ID); id != "" && t.Routes != nil {
		// The saved key is lent only to the endpoint it was saved for. A draft that points the same id at another provider
		// or base URL would otherwise send the stored secret to a host the caller chose; such a draft must carry its own key.
		if saved, ok := t.Routes.Route(id); ok && saved.Provider == provider && strings.TrimSuffix(strings.TrimSpace(saved.BaseURL), "/") == strings.TrimSuffix(route.BaseURL, "/") {
			if route.APIKey == "" {
				route.APIKey = saved.APIKey
			}
			if d.ExtraBody == "" {
				route.ExtraBody = saved.ExtraBody
			}
		}
	}
	if capability != "" {
		has := false
		for _, c := range route.Capabilities {
			has = has || c == capability
		}
		if !has {
			return model.Route{}, ErrBadParams{Err: fmt.Errorf("the model does not declare the %q capability; tick it on the model first", capability)}
		}
	}
	return route, nil
}

// voiceRouteOrDraft is voiceRoute for a call that may carry a draft instead of a route id.
func (t *Table) voiceRouteOrDraft(id string, d *VoiceDraft, capability string) (model.Route, error) {
	if d != nil {
		return t.voiceDraftRoute(d, capability)
	}
	return t.voiceRoute(id, capability)
}

func (t *Table) handleVoiceTranscribe(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p VoiceTranscribeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	vm, _ := t.Models.(VoiceModels)
	if vm == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model transport that can transcribe")
	}
	route, err := t.voiceRouteOrDraft(p.Route, p.Draft, "asr")
	if err != nil {
		return nil, err
	}
	audio, err := base64.StdEncoding.DecodeString(p.Audio)
	if err != nil || len(audio) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("audio must be non-empty base64")}
	}
	if len(audio) > maxVoiceAudioBytes {
		return nil, ErrBadParams{Err: fmt.Errorf("the recording is %d bytes; the limit is %d", len(audio), maxVoiceAudioBytes)}
	}
	text, err := vm.Transcribe(ctx, route, audio, p.Mime, p.Language)
	if err != nil {
		return nil, ErrProviderFailed.WithWhat("%v", err)
	}
	return VoiceTranscribeResult{Text: text}, nil
}

func (t *Table) handleVoiceSpeak(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p VoiceSpeakParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	vm, _ := t.Models.(VoiceModels)
	if vm == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model transport that can speak")
	}
	route, err := t.voiceRouteOrDraft(p.Route, p.Draft, "tts")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(p.Text)
	if text == "" || len([]rune(text)) > maxVoiceTextRunes {
		return nil, ErrBadParams{Err: fmt.Errorf("text must be between 1 and %d characters", maxVoiceTextRunes)}
	}
	audio, mime, err := vm.Speak(ctx, route, text, p.Voice, p.Format)
	if err != nil {
		return nil, ErrProviderFailed.WithWhat("%v", err)
	}
	return VoiceSpeakResult{Audio: base64.StdEncoding.EncodeToString(audio), Mime: mime}, nil
}

func (t *Table) handleVoiceTurn(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p VoiceTurnParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if t.Models == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model transport")
	}
	route, err := t.voiceRoute(p.Route, "")
	if err != nil {
		return nil, err
	}
	if len(p.Messages) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("voice.turn needs at least one message")}
	}
	req := model.ModelRequest{Route: route, MaxOutputTokens: route.MaxOutputTokens}
	if req.MaxOutputTokens <= 0 {
		req.MaxOutputTokens = 1024
	}
	if p.System != "" {
		req.Messages = append(req.Messages, model.Message{Role: model.RoleSystem, Content: []model.Block{model.TextBlock(p.System)}})
	}
	var pendingResults []model.Block
	flush := func() {
		if len(pendingResults) > 0 {
			req.Messages = append(req.Messages, model.Message{Role: model.RoleUser, Content: pendingResults})
			pendingResults = nil
		}
	}
	for _, m := range p.Messages {
		switch m.Role {
		case "user":
			flush()
			req.Messages = append(req.Messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(m.Text)}})
		case "assistant":
			flush()
			var blocks []model.Block
			if m.Text != "" {
				blocks = append(blocks, model.TextBlock(m.Text))
			}
			for _, c := range m.ToolCalls {
				in := c.Input
				if len(in) == 0 {
					in = json.RawMessage(`{}`)
				}
				blocks = append(blocks, model.ToolUseBlock(c.ID, c.Name, in))
			}
			if len(blocks) > 0 {
				req.Messages = append(req.Messages, model.Message{Role: model.RoleAssistant, Content: blocks})
			}
		case "tool":
			pendingResults = append(pendingResults, model.ToolResultBlock(m.ToolUseID, m.Text, m.IsError))
		default:
			return nil, ErrBadParams{Err: fmt.Errorf("message role %q is not one of user, assistant, tool", m.Role)}
		}
	}
	flush()
	for _, tl := range p.Tools {
		params := tl.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		req.Tools = append(req.Tools, model.ToolSpec{Name: tl.Name, Description: tl.Description, Parameters: params})
	}
	deltas, results, errs := t.Models.Stream(ctx, req)
	var out VoiceTurnResult
	var text strings.Builder
	for d := range deltas {
		switch d.Kind {
		case model.DeltaText:
			text.WriteString(d.Text)
		case model.DeltaToolUse:
			if d.Block != nil {
				out.ToolCalls = append(out.ToolCalls, VoiceToolCall{ID: d.Block.ID, Name: d.Block.Name, Input: d.Block.Input})
			}
		}
	}
	for range results {
	}
	if err := <-errs; err != nil {
		return nil, ErrProviderFailed.WithWhat("%v", err)
	}
	out.Text = strings.TrimSpace(text.String())
	return out, nil
}

func (t *Table) handleVoiceEmbed(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p VoiceEmbedParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	em, _ := t.Models.(EmbedModels)
	if em == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model transport that can embed")
	}
	route, err := t.voiceRouteOrDraft(p.Route, p.Draft, "embedding")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(p.Text)
	if text == "" || len([]rune(text)) > maxVoiceTextRunes {
		return nil, ErrBadParams{Err: fmt.Errorf("text must be between 1 and %d characters", maxVoiceTextRunes)}
	}
	vectors, usage, _, err := em.Embed(ctx, route, []string{text})
	if err != nil {
		return nil, ErrProviderFailed.WithWhat("%v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, ErrProviderFailed.WithWhat("the embedding route answered without a vector")
	}
	out := VoiceEmbedResult{Dimensions: len(vectors[0])}
	if usage.InputTokens != nil {
		out.InputTokens = *usage.InputTokens
	}
	return out, nil
}
