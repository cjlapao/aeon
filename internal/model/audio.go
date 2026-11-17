package model

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// audio.go — speech in and speech out are ROUTES too, exactly as embedding is (V16): the same Client, the same HTTP
// configuration, the same adapter registry, the same key handling. A route that can transcribe carries the capability
// "asr"; one that can speak carries "tts". What an endpoint's JSON or multipart looks like is the adapter's business
// (the optional interfaces below), so nothing above this directory learns a provider's shape (D14).

// Transcriber is the optional adapter half of speech recognition.
type Transcriber interface {
	// TranscribeToWire builds the request for one recording. No I/O.
	TranscribeToWire(route Route, audio []byte, mime, language string) (*http.Request, error)
	// TranscribeFromWire reads the answer into the recognised text.
	TranscribeFromWire(status int, body []byte) (string, error)
}

// Speaker is the optional adapter half of speech synthesis.
type Speaker interface {
	// SpeakToWire builds the request for one piece of text. No I/O.
	SpeakToWire(route Route, text, voice, format string) (*http.Request, error)
	// SpeakFromWire reads the answer into audio bytes and their media type. A non-2xx answer is an error carrying the
	// provider's own words.
	SpeakFromWire(status int, contentType string, body []byte) ([]byte, string, error)
}

// maxAudioReply bounds what a speech call may send back: a sentence of speech is a few hundred KB; past this the
// endpoint is malfunctioning.
const maxAudioReply = 32 << 20

// Transcribe sends a recording to the route's transcription endpoint and returns what was said. Empty audio is refused
// before the wire: a call that can only return nothing is a caller bug.
func (c *Client) Transcribe(ctx context.Context, route Route, audio []byte, mime, language string) (string, error) {
	if len(audio) == 0 {
		return "", fmt.Errorf("model: refused a transcription call with no audio (route %q)", route.ID)
	}
	adapter, err := AdapterFor(route.Provider)
	if err != nil {
		return "", err
	}
	t, ok := adapter.(Transcriber)
	if !ok {
		return "", fmt.Errorf("model: refused a transcription call through provider %q (route %q) — that provider does not speak a speech-to-text endpoint", route.Provider, route.ID)
	}
	req, err := t.TranscribeToWire(route, audio, mime, language)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("model: transcription call to %s did not answer (route %q): %w", req.URL.String(), route.ID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("model: reading the transcription (route %q): %w", route.ID, err)
	}
	text, err := t.TranscribeFromWire(resp.StatusCode, raw)
	return text, withCall(err, req, route)
}

// Speak sends text to the route's speech endpoint and returns the audio and its media type.
func (c *Client) Speak(ctx context.Context, route Route, text, voice, format string) ([]byte, string, error) {
	if text == "" {
		return nil, "", fmt.Errorf("model: refused a speech call with no text (route %q)", route.ID)
	}
	adapter, err := AdapterFor(route.Provider)
	if err != nil {
		return nil, "", err
	}
	s, ok := adapter.(Speaker)
	if !ok {
		return nil, "", fmt.Errorf("model: refused a speech call through provider %q (route %q) — that provider does not speak a text-to-speech endpoint", route.Provider, route.ID)
	}
	req, err := s.SpeakToWire(route, text, voice, format)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient().Do(req.WithContext(ctx))
	if err != nil {
		return nil, "", fmt.Errorf("model: speech call to %s did not answer (route %q): %w", req.URL.String(), route.ID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioReply))
	if err != nil {
		return nil, "", fmt.Errorf("model: reading the speech audio (route %q): %w", route.ID, err)
	}
	audio, mime, err := s.SpeakFromWire(resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	return repairWAV(audio), mime, withCall(err, req, route)
}
