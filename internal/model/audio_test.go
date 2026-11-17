package model_test

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	_ "github.com/cjlapao/aeon/internal/model/adapters"
)

// A transcription goes out as the OpenAI-compatible multipart form and comes back as the recognised text; speech goes
// out as JSON and comes back as audio bytes with their media type.
func TestSpeechRoutesSpeakTheOpenAICompatibleEndpoints(t *testing.T) {
	var gotModel, gotLang, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				switch p.FormName() {
				case "model":
					gotModel = string(b)
				case "language":
					gotLang = string(b)
				case "file":
					gotFile = p.FileName() + ":" + string(b)
				}
			}
			if r.Header.Get("Authorization") != "Bearer k" {
				http.Error(w, `{"error":{"message":"bad key"}}`, 401)
				return
			}
			_, _ = w.Write([]byte(`{"text":"  start a new loop  "}`))
		case "/v1/audio/speech":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"input":"hello there."`) {
				http.Error(w, "unexpected body "+string(b), 400)
				return
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("AUDIO"))
		}
	}))
	defer srv.Close()
	route := model.Route{ID: "r", Provider: "openai", Model: "whisper-1", BaseURL: srv.URL + "/v1", APIKey: "k"}
	c := &model.Client{}

	text, err := c.Transcribe(context.Background(), route, []byte("PCM"), "audio/webm", "en")
	if err != nil {
		t.Fatal(err)
	}
	if text != "start a new loop" || gotModel != "whisper-1" || gotLang != "en" || gotFile != "speech.webm:PCM" {
		t.Fatalf("transcription: %q model=%q lang=%q file=%q", text, gotModel, gotLang, gotFile)
	}
	audio, mt, err := c.Speak(context.Background(), route, "hello there.", "", "")
	if err != nil || string(audio) != "AUDIO" || mt != "audio/mpeg" {
		t.Fatalf("speech: %q %q %v", audio, mt, err)
	}
	if _, err := c.Transcribe(context.Background(), route, nil, "audio/webm", ""); err == nil {
		t.Fatal("empty audio must be refused before the wire")
	}
	bad := route
	bad.APIKey = "wrong"
	if _, err := c.Transcribe(context.Background(), bad, []byte("x"), "audio/webm", ""); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("a refusal must carry the provider's words: %v", err)
	}
}
