package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// rpcFakeEnvelope mirrors client.go's wire shape (JSONRPC/ID/Method/Params),
// read-only here — this test never imports it, it decodes the same shape by
// hand so it stays a black-box probe of the transport, not of client.go's
// internals.
type rpcFakeEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// neverCallFake answers initialize and tools/list like a real server, and
// FAILS the test the instant it ever receives tools/call — the fake IS the
// assertion, not a prose claim about Probe's body.
func neverCallFake(t *testing.T, tools []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env rpcFakeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Fatalf("fake: undecodable request: %v", err)
		}
		switch env.Method {
		case "tools/call":
			t.Fatalf("Probe issued a tools/call request — it must never build one")
		case "initialize":
			reply := map[string]any{
				"jsonrpc": "2.0", "id": env.ID,
				"result": map[string]any{
					"protocolVersion": ProtocolVersion,
					"serverInfo":      map[string]any{"name": "never-call-fake", "version": "0"},
				},
			}
			_ = json.NewEncoder(w).Encode(reply)
		case "notifications/initialized":
			w.WriteHeader(http.StatusOK)
		case "tools/list":
			reply := map[string]any{
				"jsonrpc": "2.0", "id": env.ID,
				"result": map[string]any{"tools": tools},
			}
			_ = json.NewEncoder(w).Encode(reply)
		default:
			t.Fatalf("fake: unexpected method %q", env.Method)
		}
	}))
}

func httpConfig(name, url string) ServerConfig {
	return ServerConfig{Name: name, Transport: TransportConfig{Type: TransportHTTP, URL: url}}
}

// TestProbeNeverCallsATool drives Probe's success path against the fake —
// if Probe ever built a tools/call request, the fake itself fails the test.
func TestProbeNeverCallsATool(t *testing.T) {
	srv := neverCallFake(t, []map[string]any{
		{"name": "list_issues", "description": "list open issues"},
	})
	defer srv.Close()
	result := Probe(t.Context(), httpConfig("github", srv.URL), func(string) string { return "" })
	if !result.OK {
		t.Fatalf("expected a successful probe, got %+v", result)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "list_issues" {
		t.Fatalf("tools not reported as the fake listed them: %+v", result.Tools)
	}
	if result.Err != nil {
		t.Fatalf("OK result must carry no error: %+v", result)
	}
}

// TestProbeNeverCallsAToolOnFailurePath proves the same discipline holds
// even when the probe itself fails — a broken server is not a license to
// try a tools/call "just in case".
func TestProbeNeverCallsAToolOnFailurePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env rpcFakeEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		if env.Method == "tools/call" {
			t.Fatalf("Probe issued a tools/call request on its failure path")
		}
		// Every request fails after initialize — proves Probe gives up
		// rather than reaching for anything else, including a call.
		if env.Method == "initialize" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	result := Probe(t.Context(), httpConfig("broken", srv.URL), func(string) string { return "" })
	if result.OK {
		t.Fatalf("a server answering 500 to initialize must not probe OK: %+v", result)
	}
	if result.Err == nil {
		t.Fatal("a failed probe must name why")
	}
}

func TestProbeRespectsContextDeadline(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never answers within the test's short deadline
	}))
	// defer runs LIFO: registering Close() first and the unblock second
	// means the unblock fires FIRST, so Close() never waits on the
	// deliberately stuck in-flight request.
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	result := Probe(ctx, httpConfig("slow", srv.URL), func(string) string { return "" })
	if result.OK {
		t.Fatalf("a server that never answers must not probe OK: %+v", result)
	}
	if result.Err == nil {
		t.Fatal("a timed-out probe must name why")
	}
}
