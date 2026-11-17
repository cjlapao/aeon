package model_test

// embed_wire_test.go — the route claim, checked at the wire: the embedding
// call goes through THE client (one key door, one cost site, adapter
// registry shared), speaks /embeddings, and refuses the provider that
// cannot answer with the door shut. No server is started: the transport is
// the house scriptedTransport, and the recorded body lives in
// testdata/embed-openai-small.json (recorded, not live — TestAgainst...
// discipline holds across packages).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"

	_ "github.com/cjlapao/aeon/internal/model/adapters" // the registry init, exactly as production imports it
)

func embedTestdata(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "embed-openai-small.json"))
	if err != nil {
		t.Fatalf("read the recorded embedding fixture: %v", err)
	}
	return raw
}

// embedTransport is the model_test-side scripted transport: same shape as
// the in-package house fake (which package model_test cannot see), and
// deliberately minimal — one scripted answer, remembering the request.
type embedTransport struct {
	status      int
	body        string
	calls       int
	lastRequest *http.Request
}

func (s *embedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.calls++
	s.lastRequest = r
	if s.calls > 1 {
		return nil, fmt.Errorf("the transport was called %d times; only one answer is scripted", s.calls)
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    r,
	}, nil
}

func embedClient(t *testing.T, status int, body string) (*model.Client, *embedTransport) {
	t.Helper()
	tr := &embedTransport{status: status, body: body}
	return &model.Client{HTTP: &http.Client{Transport: tr}}, tr
}

func TestEmbedGoesThroughTheOneClient(t *testing.T) {
	body := string(embedTestdata(t))
	c, tr := embedClient(t, 200, body)
	in := 0.02
	route := model.Route{ID: "openai-small", Provider: "openai", Model: "text-embedding-3-small",
		APIKey: "sk-test-never-a-real-one", Prices: &model.Prices{InputUSDPerMTok: &in}}

	texts := []string{"func alpha", "func beta", "func gamma"}
	vectors, usage, cost, err := c.Embed(context.Background(), route, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 3 || len(vectors[0]) != 4 {
		t.Fatalf("vectors %+v — three texts, four dimensions each, per the recorded fixture", vectors)
	}
	if usage.InputTokens == nil || *usage.InputTokens != 42 {
		t.Fatalf("usage %+v — prompt_tokens must land in InputTokens (embeddings bill INPUT)", usage)
	}
	if cost.State != model.CostKnown || cost.USD <= 0 {
		t.Fatalf("a metered embedding call costs %v — V10: the same path as everything else", cost)
	}

	// The WIRE: one call, /embeddings, bearer from the route's key, the
	// texts in order in the input array.
	if tr.calls != 1 {
		t.Fatalf("%d calls — this test scripts one answer", tr.calls)
	}
	req := tr.lastRequest
	if req == nil {
		t.Fatal("the transport never saw a request")
	}
	if !strings.HasSuffix(req.URL.Path, "/embeddings") {
		t.Errorf("path %q — the embeddings endpoint is /embeddings, not the chat door", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test-never-a-real-one" {
		t.Errorf("Authorization %q — the key crosses through the SAME door chat uses (one place a key is read, §4c's cousin)", got)
	}
	raw, _ := io.ReadAll(req.Body)
	var sent struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("request body is not the OpenAI embeddings shape: %v", err)
	}
	if sent.Model != "text-embedding-3-small" || len(sent.Input) != 3 || sent.Input[0] != "func alpha" {
		t.Errorf("wire body %+v — model named, texts in order (position IS the pairing)", sent)
	}
}

func TestEmbedUnmeteredRouteIsUnknownWithAReason(t *testing.T) {
	body := string(embedTestdata(t))
	c, _ := embedClient(t, 200, body)
	route := model.Route{ID: "local-embed", Provider: "openai", Model: "nomic-embed-text", NoPrice: true}
	_, _, cost, err := c.Embed(context.Background(), route, []string{"x", "y", "z"})
	if err != nil {
		t.Fatalf("an unmetered route must still embed: %v", err)
	}
	if cost.State != model.CostUnknown || cost.Reason != model.ReasonNoPriceForRoute {
		t.Fatalf("unmetered cost %v — unknown WITH the reason (M30); $0.00 would be a claim", cost)
	}
}

func TestEmbedRefusals(t *testing.T) {
	c, tr := embedClient(t, 200, "{}")

	// Empty batch: refused before the wire, naming the route.
	_, _, cost, err := c.Embed(context.Background(), model.Route{ID: "r1", Provider: "openai", Model: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "r1") {
		t.Fatalf("empty batch: %v — must refuse and name the route", err)
	}
	if cost.State != model.CostUnknown || cost.Reason != model.ReasonNotAttempted {
		t.Errorf("a call never made reports %v — call-not-attempted is the honest unknown", cost)
	}
	if tr.calls != 0 {
		t.Error("an empty batch reached the wire")
	}

	// Provider that speaks no embeddings: refused at the adapter door, and
	// ANTHROPIC is that provider (the registry's only non-speaker).
	_, _, _, err = c.Embed(context.Background(), model.Route{ID: "claude", Provider: "anthropic", Model: "claude"}, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "anthropic") || !strings.Contains(err.Error(), "embeddings") {
		t.Fatalf("anthropic embed: %v — must name the provider and the fact (no parallel path is built for a provider that cannot answer)", err)
	}

	// Unknown provider: the registry's O6 failure, verbatim.
	_, _, _, err = c.Embed(context.Background(), model.Route{ID: "x", Provider: "nope"}, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "no adapter for provider") {
		t.Fatalf("unknown provider: %v", err)
	}

	// A 400 with the provider's own sentence, surfaced not swallowed.
	bad, _ := embedClient(t, 400, `{"error":{"message":"model not found","type":"invalid_request_error"}}`)
	_, _, _, err = bad.Embed(t.Context(), model.Route{ID: "r2", Provider: "openai", Model: "missing-model"}, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "model not found") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("provider 400 surfaced as %v — the provider knows which model does not exist; Aeon only repeats it", err)
	}
}

func TestEmbedRefusesMiscountedVectors(t *testing.T) {
	// The pairing is positional; a response that answers 2 vectors for 3
	// texts is refused, never truncated into a plausible wrong index.
	c, _ := embedClient(t, 200, `{"data":[{"index":0,"embedding":[1,0,0,0]},{"index":1,"embedding":[0,1,0,0]}],"usage":{"prompt_tokens":3}}`)
	_, _, _, err := c.Embed(context.Background(), model.Route{ID: "r3", Provider: "openai", Model: "m"}, []string{"a", "b", "c"})
	if err == nil || !strings.Contains(err.Error(), "2 vectors for 3 texts") {
		t.Fatalf("miscount: %v", err)
	}
	// A hole in the indices is refused too (200 OK with data[1].index=5).
	c2, _ := embedClient(t, 200, `{"data":[{"index":0,"embedding":[1,0,0,0]},{"index":5,"embedding":[0,1,0,0]}]}`)
	_, _, _, err = c2.Embed(context.Background(), model.Route{ID: "r4", Provider: "openai", Model: "m"}, []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("index hole: %v", err)
	}
	// Out-of-order arrival must still pair by INDEX, not arrival order.
	c3, _ := embedClient(t, 200, `{"data":[{"index":1,"embedding":[0,1,0,0]},{"index":0,"embedding":[1,0,0,0]}]}`)
	vectors, _, _, err := c3.Embed(context.Background(), model.Route{ID: "r5", Provider: "openai", Model: "m"}, []string{"a", "b"})
	if err != nil {
		t.Fatalf("reordered answer refused: %v — the spec orders by index, arrival is not information", err)
	}
	if vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("pairing followed arrival, not index: %v", vectors)
	}
}

func TestEmbedFixturesAreRecorded(t *testing.T) {
	// The floor: the fixture exists, parses, and no test file in this
	// package's embedding half opens a server.
	var f struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(embedTestdata(t), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Data) < 3 || f.Usage == nil || f.Usage.PromptTokens == 0 {
		t.Fatal("the fixture thinned below what the suite asserts")
	}
	src, err := os.ReadFile("embed_wire_test.go")
	if err != nil {
		t.Fatal(err)
	}
	srcStr := string(src)
	// Assembled: the hunter cannot contain the hunted.
	for _, b := range []string{"http" + "test", "net/" + "Dial", "New" + "Server"} {
		if strings.Contains(srcStr, b) {
			t.Errorf("the embed wire test reached for a server (%q) — the scriptedTransport is the house fake and it is enough", b)
		}
	}
}
