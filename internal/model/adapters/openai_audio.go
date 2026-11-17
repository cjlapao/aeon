package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// ── speech: the OpenAI-compatible transcription and speech endpoints ───────────────────────────────────────────────

func (d *openAIDispatch) TranscribeToWire(route model.Route, audio []byte, mime, language string) (*http.Request, error) {
	return d.chat.TranscribeToWire(route, audio, mime, language)
}
func (d *openAIDispatch) TranscribeFromWire(status int, body []byte) (string, error) {
	return d.chat.TranscribeFromWire(status, body)
}
func (d *openAIDispatch) SpeakToWire(route model.Route, text, voice, format string) (*http.Request, error) {
	return d.chat.SpeakToWire(route, text, voice, format)
}
func (d *openAIDispatch) SpeakFromWire(status int, contentType string, body []byte) ([]byte, string, error) {
	return d.chat.SpeakFromWire(status, contentType, body)
}

func audioFileName(mime string) string {
	switch {
	case strings.Contains(mime, "webm"):
		return "speech.webm"
	case strings.Contains(mime, "ogg"):
		return "speech.ogg"
	case strings.Contains(mime, "wav"):
		return "speech.wav"
	case strings.Contains(mime, "mp4"), strings.Contains(mime, "m4a"):
		return "speech.m4a"
	case strings.Contains(mime, "mpeg"), strings.Contains(mime, "mp3"):
		return "speech.mp3"
	}
	return "speech.webm"
}

// TranscribeToWire is `POST {base}/audio/transcriptions`, a multipart form with the model, the file and the language.
func (a *openAI) TranscribeToWire(route model.Route, audio []byte, mime, language string) (*http.Request, error) {
	if route.EffectiveAudioAPI() == model.AudioAPIChat {
		return chatTranscribeToWire(route, audio, mime, language)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", route.Model); err != nil {
		return nil, err
	}
	if err := mw.WriteField("response_format", "json"); err != nil {
		return nil, err
	}
	if language != "" {
		if err := mw.WriteField("language", language); err != nil {
			return nil, err
		}
	}
	part, err := mw.CreateFormFile("file", audioFileName(mime))
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(route.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	httpReq, err := http.NewRequest(http.MethodPost, base+"/audio/transcriptions", &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	return httpReq, nil
}

func (a *openAI) TranscribeFromWire(status int, body []byte) (string, error) {
	var wire struct {
		Text    string `json:"text"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return "", fmt.Errorf("model/openai: the transcription answer is not JSON (HTTP %d): %s", status, truncForLog(body))
	}
	if status < 200 || status > 299 {
		msg := ""
		if wire.Error != nil {
			msg = wire.Error.Message
		}
		return "", fmt.Errorf("model/openai: transcription refused (HTTP %d): %s", status, msg)
	}
	// An omni model answers through chat completions: the words are the message.
	if wire.Text == "" && len(wire.Choices) > 0 {
		return strings.TrimSpace(wire.Choices[0].Message.Content), nil
	}
	return strings.TrimSpace(wire.Text), nil
}

type speakWireRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// SpeakToWire is `POST {base}/audio/speech`, JSON in, audio bytes out.
func (a *openAI) SpeakToWire(route model.Route, text, voice, format string) (*http.Request, error) {
	switch route.EffectiveAudioAPI() {
	case model.AudioAPIChat:
		return chatSpeakToWire(route, text, voice)
	case model.AudioAPISynthesize:
		return synthesizeToWire(route, text, voice, format, "/synthesize", false)
	case model.AudioAPISynthesizeStream:
		return synthesizeToWire(route, text, voice, format, "/synthesize/stream", true)
	}
	if voice == "" {
		voice = "alloy"
	}
	if format == "" {
		format = "mp3"
	}
	raw, err := json.Marshal(speakWireRequest{Model: route.Model, Input: text, Voice: voice, ResponseFormat: format})
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(route.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	httpReq, err := http.NewRequest(http.MethodPost, base+"/audio/speech", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	return httpReq, nil
}

func (a *openAI) SpeakFromWire(status int, contentType string, body []byte) ([]byte, string, error) {
	if status < 200 || status > 299 {
		var wire struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := truncForLog(body)
		if json.Unmarshal(body, &wire) == nil && wire.Error != nil {
			msg = wire.Error.Message
		}
		return nil, "", fmt.Errorf("model/openai: speech refused (HTTP %d): %s", status, msg)
	}
	if len(body) == 0 {
		return nil, "", fmt.Errorf("model/openai: the speech endpoint answered with no audio")
	}
	// Some servers (an omni model's chat completions, a synthesize endpoint) wrap the audio in JSON as base64.
	if strings.Contains(contentType, "json") || body[0] == '{' {
		return audioFromJSON(body)
	}
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	return body, contentType, nil
}
