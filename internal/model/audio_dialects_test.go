package model_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// A route says how its speech endpoints are spoken: synthesize, its stream twin, or an omni model's chat completions.
func TestSpeechRoutesSpeakTheirChosenAPI(t *testing.T) {
	var paths []string
	var chatBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/synthesize", "/v1/synthesize/stream":
			if !strings.Contains(string(b), `"text":"hello."`) || !strings.Contains(string(b), `"voice_id":"af_heart"`) || !strings.Contains(string(b), `"container":"wav"`) || !strings.Contains(string(b), `"model":"m"`) {
				http.Error(w, "unexpected body "+string(b), 400)
				return
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write([]byte("WAV"))
		case "/v1/chat/completions":
			chatBody = string(b)
			if strings.Contains(chatBody, "audio_url") {
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  start a loop "}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"audio":{"format":"wav","data":"` + base64.StdEncoding.EncodeToString([]byte("OMNI")) + `"}}}]}`))
		}
	}))
	defer srv.Close()
	c := &model.Client{}
	ctx := context.Background()
	route := func(api string) model.Route {
		return model.Route{ID: "r", Provider: "openai", Model: "m", BaseURL: srv.URL + "/v1", AudioAPI: api}
	}

	for _, api := range []string{model.AudioAPISynthesize, model.AudioAPISynthesizeStream} {
		audio, mime, err := c.Speak(ctx, route(api), "hello.", "af_heart", "")
		if err != nil || string(audio) != "WAV" || mime != "audio/wav" {
			t.Fatalf("%s: %q %q %v", api, audio, mime, err)
		}
	}
	if paths[0] != "/v1/synthesize" || paths[1] != "/v1/synthesize/stream" {
		t.Fatalf("paths = %v", paths)
	}

	audio, mime, err := c.Speak(ctx, route(model.AudioAPIChat), "hello.", "Chelsie", "")
	if err != nil || string(audio) != "OMNI" || mime != "audio/wav" {
		t.Fatalf("omni speak: %q %q %v", audio, mime, err)
	}
	if !strings.Contains(chatBody, `"modalities":["text","audio"]`) || !strings.Contains(chatBody, `"voice":"Chelsie"`) {
		t.Fatalf("omni speak body: %s", chatBody)
	}

	text, err := c.Transcribe(ctx, route(model.AudioAPIChat), []byte("PCM"), "audio/webm", "en")
	if err != nil || text != "start a loop" {
		t.Fatalf("omni hear: %q %v", text, err)
	}
	if !strings.Contains(chatBody, "data:audio/webm;base64,") {
		t.Fatalf("omni hear body: %s", chatBody)
	}
}

func TestAnUnknownAudioAPIIsRefused(t *testing.T) {
	if model.ValidAudioAPI("soap") || !model.ValidAudioAPI("") || !model.ValidAudioAPI(model.AudioAPIChat) {
		t.Fatal("ValidAudioAPI wrong")
	}
}

// A failed speech, hearing or embedding call says which address it called, and a 404 off a path with no /v1 names the cause.
func TestAFailedCallSaysWhatItCalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"Not Found"}`, 404)
	}))
	defer srv.Close()
	c := &model.Client{}
	route := model.Route{ID: "embedder", Provider: "openai", Model: "bge-m3", BaseURL: srv.URL}
	_, _, _, err := c.Embed(context.Background(), route, []string{"hi"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"HTTP 404", "POST " + srv.URL + "/embeddings", `route "embedder"`, `model "bge-m3"`, "end in /v1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
	withV1 := route
	withV1.BaseURL = srv.URL + "/v1"
	_, _, _, err = c.Embed(context.Background(), withV1, []string{"hi"})
	if err == nil || strings.Contains(err.Error(), "end in /v1") {
		t.Fatalf("a base URL that already has /v1 must not be told to add it: %v", err)
	}
	if _, _, err = c.Speak(context.Background(), route, "hello.", "", ""); err == nil || !strings.Contains(err.Error(), "/audio/speech") {
		t.Fatalf("speech error must name its address: %v", err)
	}
}

// A streamed WAV carries a placeholder length in its header; the player stops there, mid-sentence. The lengths are made true.
func TestARepairedWAVHasTrueLengths(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	wav := append([]byte("RIFF\xff\xff\xff\xffWAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3e\x00\x00\x00\x7d\x00\x00\x02\x00\x10\x00data\xff\xff\xff\xff"), data...)
	got := model.RepairWAVForTest(wav)
	if le := uint32(got[4]) | uint32(got[5])<<8 | uint32(got[6])<<16 | uint32(got[7])<<24; le != uint32(len(wav)-8) {
		t.Fatalf("RIFF size = %d, want %d", le, len(wav)-8)
	}
	at := len(wav) - len(data) - 4
	if le := uint32(got[at]) | uint32(got[at+1])<<8 | uint32(got[at+2])<<16 | uint32(got[at+3])<<24; le != uint32(len(data)) {
		t.Fatalf("data size = %d, want %d", le, len(data))
	}
	if wav[4] != 0xff {
		t.Fatal("the input must not be modified in place")
	}
	mp3 := []byte("ID3not a wav at all, long enough to pass the length check........")
	if string(model.RepairWAVForTest(mp3)) != string(mp3) {
		t.Fatal("a non-WAV must come back untouched")
	}
}
