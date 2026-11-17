package model_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	_ "github.com/cjlapao/aeon/internal/model/adapters"
)

// The trace keeps the wire on both sides: the body the adapter built (with the route's extra body and the
// per-call effort in it) and the raw reply, and it reports what the adapter read from that reply — so a server
// that sends reasoning under a name the adapter does not read shows up as raw text beside thinkingChars 0.
func TestTraceKeepsTheRequestAndTheRawReply(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	route := model.Route{ID: "r", Provider: "openai", Model: "qwen", BaseURL: srv.URL, APIKey: "sk-secret-123456", MaxOutputTokens: 256,
		ExtraBody: json.RawMessage(`{"chat_template_kwargs":{"enable_thinking":false}}`)}
	req := model.ModelRequest{Route: route, ReasoningEffort: "low",
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}}}
	res := (&model.Client{}).Trace(context.Background(), req)

	if res.Error != "" || res.Status != 200 {
		t.Fatalf("trace failed: %+v", res)
	}
	if gotAuth == "" {
		t.Error("the adapter sent no credential, so the trace did not exercise the real request")
	}
	body := string(res.RequestBody)
	for _, want := range []string{`"reasoning_effort":"low"`, `"chat_template_kwargs":{"enable_thinking":false}`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body is missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "sk-secret") || strings.Contains(res.URL, "sk-secret") || strings.Contains(res.Raw, "sk-secret") {
		t.Error("the credential leaked into the trace result")
	}
	if !strings.Contains(res.Raw, "reasoning_content") || res.Text != "Hi" || res.ThinkingChars != 3 {
		t.Errorf("raw/text/thinking not reported: raw=%q text=%q thinking=%d", res.Raw, res.Text, res.ThinkingChars)
	}
}

func TestTraceReportsAFailureAsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Unrecognized request argument supplied: reasoning_effort"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	route := model.Route{ID: "r", Provider: "openai", Model: "m", BaseURL: srv.URL, MaxOutputTokens: 64}
	res := (&model.Client{}).Trace(context.Background(), model.ModelRequest{Route: route, ReasoningEffort: "low",
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}}})
	if res.Status != 400 || !strings.Contains(res.Raw, "reasoning_effort") || res.Error == "" || len(res.RequestBody) == 0 {
		t.Fatalf("a refused request must come back as data with its body and the server's words: %+v", res)
	}
}

// A key in the query string must not come back through a transport error, the URL, or a server that echoes it.
func TestTraceScrubsAKeyFromErrorsAndEchoes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key sk-secret-123456", http.StatusUnauthorized)
	}))
	url := srv.URL
	route := model.Route{ID: "r", Provider: "openai", Model: "m", BaseURL: url, APIKey: "sk-secret-123456", MaxOutputTokens: 64}
	msgs := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}}
	echoed := (&model.Client{}).Trace(context.Background(), model.ModelRequest{Route: route, Messages: msgs})
	if strings.Contains(echoed.Raw, "sk-secret") || strings.Contains(echoed.Error, "sk-secret") {
		t.Errorf("an echoed key survived: %+v", echoed)
	}
	srv.Close() // now the call fails in transport, and the error quotes the URL
	failed := (&model.Client{}).Trace(context.Background(), model.ModelRequest{Route: route, Messages: msgs})
	if failed.Error == "" || strings.Contains(failed.Error, "sk-secret") || strings.Contains(failed.URL, "sk-secret") {
		t.Errorf("a transport error leaked or was empty: %+v", failed)
	}
}
