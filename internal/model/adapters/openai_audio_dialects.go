package adapters

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// The speech APIs other than OpenAI's own `/audio/*` pair (route.AudioAPI). Two families:
//
//   - `synthesize` / `synthesize-stream`: a JSON request to `{base}/synthesize` or `{base}/synthesize/stream` that
//     answers with raw audio bytes (or, when the server wraps it, base64 in JSON). The request is
//     {model, text, voice_id, container, stream}; a field a server wants beyond that is reached through the route's extra
//     body, which is merged into every call. The streamed audio is read whole before it plays.
//   - `chat`: an omni model that hears and speaks through chat completions, so one route serves both `asr` and `tts`.
//     Hearing sends the recording as an `audio_url` data URI (the form vLLM accepts for any container, so a browser's
//     webm needs no conversion); speaking asks for the `audio` modality and reads the base64 audio from the message.

func baseOf(route model.Route) string {
	base := strings.TrimSuffix(route.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return base
}

func postJSON(route model.Route, path string, body any) (*http.Request, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseOf(route)+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if route.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+route.APIKey)
	}
	return req, nil
}

// mergeExtra lays the route's extra body over a request, the one escape hatch for a field a server names differently.
func mergeExtra(route model.Route, body map[string]any) map[string]any {
	if len(route.ExtraBody) == 0 {
		return body
	}
	var extra map[string]any
	if json.Unmarshal(route.ExtraBody, &extra) == nil {
		for k, v := range extra {
			body[k] = v
		}
	}
	return body
}

func synthesizeToWire(route model.Route, text, voice, format, path string, stream bool) (*http.Request, error) {
	if format == "" {
		format = "wav"
	}
	// {text, voice_id, container, stream}, plus the route's model name: some servers answer a request without a
	// model with a 500. The route's extra body is merged last, so it can override or add any field.
	body := map[string]any{"model": route.Model, "text": text, "container": format, "stream": stream}
	if voice != "" {
		body["voice_id"] = voice
	}
	return postJSON(route, path, mergeExtra(route, body))
}

func chatSpeakToWire(route model.Route, text, voice string) (*http.Request, error) {
	audio := map[string]any{"format": "wav"}
	if voice != "" {
		audio["voice"] = voice
	}
	return postJSON(route, "/chat/completions", mergeExtra(route, map[string]any{
		"model": route.Model,
		"messages": []map[string]any{
			{"role": "system", "content": "You are a text-to-speech voice. Say the user's message aloud exactly as written, adding and answering nothing."},
			{"role": "user", "content": text},
		},
		"modalities": []string{"text", "audio"},
		"audio":      audio,
	}))
}

func chatTranscribeToWire(route model.Route, audio []byte, mime, language string) (*http.Request, error) {
	if mime == "" {
		mime = "audio/webm"
	}
	prompt := "Transcribe this audio. Reply with only the words that were spoken, nothing else."
	if language != "" {
		prompt += " The language is " + language + "."
	}
	uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(audio)
	return postJSON(route, "/chat/completions", mergeExtra(route, map[string]any{
		"model": route.Model,
		"messages": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "audio_url", "audio_url": map[string]any{"url": uri}},
			{"type": "text", "text": prompt},
		}}},
	}))
}

// audioFromJSON finds base64 audio in the shapes servers use: an omni chat completion
// (choices[0].message.audio.data), or a synthesize answer with the audio under `audio`, `audio_base64` or `data`.
func audioFromJSON(body []byte) ([]byte, string, error) {
	var wire struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data   string `json:"data"`
					Format string `json:"format"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
		Audio       string `json:"audio"`
		AudioBase64 string `json:"audio_base64"`
		Data        string `json:"data"`
		Format      string `json:"format"`
		MIME        string `json:"mime"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, "", fmt.Errorf("model/openai: the speech answer is not audio and not JSON: %s", truncForLog(body))
	}
	encoded, format := wire.Audio, wire.Format
	if encoded == "" {
		encoded = wire.AudioBase64
	}
	if encoded == "" {
		encoded = wire.Data
	}
	if len(wire.Choices) > 0 && wire.Choices[0].Message.Audio.Data != "" {
		encoded, format = wire.Choices[0].Message.Audio.Data, wire.Choices[0].Message.Audio.Format
	}
	if encoded == "" {
		return nil, "", fmt.Errorf("model/openai: the speech answer carries no audio: %s", truncForLog(body))
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", fmt.Errorf("model/openai: the speech audio is not valid base64: %w", err)
	}
	mime := wire.MIME
	if mime == "" {
		switch strings.ToLower(format) {
		case "mp3", "mpeg":
			mime = "audio/mpeg"
		case "opus", "ogg":
			mime = "audio/ogg"
		case "flac":
			mime = "audio/flac"
		default:
			mime = "audio/wav"
		}
	}
	return raw, mime, nil
}
