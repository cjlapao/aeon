package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
)

// S36-mcp-dispatch T4 — dispatch, degradation, lifecycle. The call half is
// tested through the bundle's Call (makeCall) against a controllable fake
// client; the deadline is a var a test shrinks (restored via t.Cleanup), so a
// "never answers" server is bounded without a 60s wall wait.

// blockCallClient is a fakeClient whose CallTool never answers until the
// context is done — the TestCallDeadlineBounds case (a server that never
// answers must be bounded by the dispatch budget, not the parent ctx).
type blockCallClient struct{}

func (blockCallClient) ListTools(ctx context.Context) ([]mcp.RemoteTool, error) {
	return []mcp.RemoteTool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}
func (blockCallClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error) {
	<-ctx.Done()
	return mcp.CallResult{}, ctx.Err()
}
func (blockCallClient) Close() error { return nil }

func setCallDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	old := mcpCallDeadline
	mcpCallDeadline = d
	t.Cleanup(func() { mcpCallDeadline = old })
}

// TestCallDeadlineBounds — a server that never answers is bounded by
// mcpCallDeadline, not the parent ctx. The parent ctx is long-lived; only the
// dispatch budget bounds the wait, and the failure degrades to an error-text
// result (never a hang).
func TestCallDeadlineBounds(t *testing.T) {
	setCallDeadline(t, 50*time.Millisecond)
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return blockCallClient{}, nil
	})

	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
		allowPolicy("agent-a", "alpha.search"),
	}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bundle.Specs) != 1 {
		t.Fatalf("specs = %d, want 1: %v", len(bundle.Specs), bundle.Specs)
	}

	// A long-lived parent ctx: only the dispatch budget bounds the call.
	start := time.Now()
	out, callErr := bundle.Call(context.Background(), "alpha.search", json.RawMessage(`{}`))
	elapsed := time.Since(start)
	if callErr == nil {
		t.Errorf("a call that never answers must not succeed (it must degrade to an error)")
	}
	if out == "" {
		t.Errorf("the degraded result must carry an error text, got empty")
	}
	if elapsed > mcpCallDeadline+2*time.Second {
		t.Errorf("elapsed %v exceeds the dispatch budget+slack; the deadline is not the bound", elapsed)
	}
	t.Logf("blocked call bounded by %v: elapsed %v, result %q", mcpCallDeadline, elapsed, out)
}

// TestCallDegradesToErrorText — a transport error and a server-reported error
// both become an error-text result the composite can mark IsError, never a
// thrown error that kills the step (criterion 4's dispatch half).
func TestCallDegradesToErrorText(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		setMCPConfig(t, mcp.Config{
			Public: []mcp.ServerConfig{{Name: "alpha", Transport: testTransport(),
				Tools: []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}}},
			Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
		})
		setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
			return &fakeClient{tools: []mcp.RemoteTool{{Name: "search", InputSchema: json.RawMessage(`{}`)}}, callErr: errors.New("connection refused")}, nil
		})
		bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
			allowPolicy("agent-a", "alpha.search"),
		}, time.Now())
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		out, callErr := bundle.Call(context.Background(), "alpha.search", json.RawMessage(`{}`))
		if callErr == nil {
			t.Errorf("a transport error must surface as a Go error the composite marks IsError")
		}
		if out == "" {
			t.Errorf("the transport error must carry an error-text result, got empty")
		}
	})
	t.Run("server reported error", func(t *testing.T) {
		setMCPConfig(t, mcp.Config{
			Public: []mcp.ServerConfig{{Name: "alpha", Transport: testTransport(),
				Tools: []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}}},
			Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
		})
		setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
			return &errorResultClient{text: "no results", isError: true}, nil
		})
		bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
			allowPolicy("agent-a", "alpha.search"),
		}, time.Now())
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		out, callErr := bundle.Call(context.Background(), "alpha.search", json.RawMessage(`{}`))
		if callErr == nil {
			t.Errorf("a server-reported error (IsError) must surface as a Go error the composite marks IsError")
		}
		if out != "no results" {
			t.Errorf("the server's error text must be the result, got %q", out)
		}
	})
}

// errorResultClient returns an IsError CallResult — a server that answered but
// said no (the call error path, distinct from a transport error).
type errorResultClient struct {
	text    string
	isError bool
}

func (errorResultClient) ListTools(ctx context.Context) ([]mcp.RemoteTool, error) {
	return []mcp.RemoteTool{{Name: "search", InputSchema: json.RawMessage(`{}`)}}, nil
}
func (e errorResultClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error) {
	return mcp.CallResult{Text: e.text, IsError: e.isError}, nil
}
func (errorResultClient) Close() error { return nil }

// TestWithheldSentencesRideOnComposite — Q3: the withheld candidate lines the
// composite carries (the sentences grants.go wrote, one per candidate) are the
// ones the daemon rides additively on the step.started event. A nil/empty
// bundle carries none (MD8: byte-identical to today).
func TestWithheldSentencesRideOnComposite(t *testing.T) {
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "present", Rerun: "safe", Effect: "local"}, {Name: "gone", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.present", "alpha.gone"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		// Only "present" is live; "alpha.gone" is withheld with the named sentence.
		return &fakeClient{tools: []mcp.RemoteTool{{Name: "present", InputSchema: json.RawMessage(`{}`)}}}, nil
	})
	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
		allowPolicy("agent-a", "alpha.present"),
		allowPolicy("agent-a", "alpha.gone"),
	}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// The composite carries the withheld sentences (the Q3 payload): one line
	// per withheld candidate, the exact sentence grants.go wrote (MD9). The
	// daemon reads these through the composite's Withheld() method; here we
	// read the same source the composite reads — the bundle's Withheld field.
	lines := withheldLines(bundle)
	if len(lines) != 1 {
		t.Fatalf("withheld lines = %d, want 1: %v", len(lines), lines)
	}
	want := noLongerListedSentence("alpha.gone", "alpha")
	if lines[0] != want {
		t.Errorf("withheld line = %q, want the exact named sentence %q (MD9)", lines[0], want)
	}

	// No bundle → no withheld lines (MD8: the event is byte-identical to today).
	empty := &daemon.MCPBundle{}
	if got := withheldLines(empty); len(got) != 0 {
		t.Errorf("an empty bundle must carry no withheld lines, got %v", got)
	}
}

// withheldLines is exactly what the composite's Withheld() method projects:
// one line per withheld candidate, the sentence the daemon rides on the event.
func withheldLines(b *daemon.MCPBundle) []string {
	if b == nil {
		return nil
	}
	lines := make([]string, 0, len(b.Withheld))
	for _, w := range b.Withheld {
		lines = append(lines, w.Sentence)
	}
	return lines
}

// unknownToolClient models a real MCP server: it answers only the tools in its
// live list, and a name the server does not carry (the forged alpha.evil) gets
// the error a real server returns — which makeCall degrades to an error-text
// result, never a silent success (criterion 4's dispatch half).
type unknownToolClient struct {
	live []mcp.RemoteTool
}

func (u unknownToolClient) ListTools(ctx context.Context) ([]mcp.RemoteTool, error) {
	return u.live, nil
}
func (u unknownToolClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error) {
	for _, t := range u.live {
		if t.Name == name {
			return mcp.CallResult{Text: "ok:" + name}, nil
		}
	}
	// The server has no such tool: the failure a real server returns, which
	// makeCall turns into an error-text result the composite marks IsError.
	return mcp.CallResult{}, errors.New("no such tool: " + name)
}
func (u unknownToolClient) Close() error { return nil }

// TestForgedDotNameDegradesNotReachesFirstParty — a model-invented "alpha.evil"
// name that was never offered never lands a live call: the dot routes it to the
// bundle (not the first-party caller), the dialled server has no such tool and
// rejects it, and makeCall degrades that to an error-text result (IsError)
// rather than a silent success. alpha the server WAS dialled (for present);
// the degradation is the forged TOOL the server does not carry — not a missing
// server.
func TestForgedDotNameDegradesNotReachesFirstParty(t *testing.T) {
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "present", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.present"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		// alpha is dialled (present was offered); it answers present but has no
		// "evil" — the forged tool the server does not carry.
		return unknownToolClient{live: []mcp.RemoteTool{{Name: "present", InputSchema: json.RawMessage(`{}`)}}}, nil
	})
	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
		allowPolicy("agent-a", "alpha.present"),
	}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bundle.Specs) != 1 {
		t.Fatalf("specs = %d, want 1: %v", len(bundle.Specs), bundle.Specs)
	}

	// The forged name has a dot, so the composite routes it to the bundle; the
	// dialled server has no such tool and rejects it, so the call degrades to an
	// error text (IsError) — never a silently-missed tool (never a success).
	out, callErr := bundle.Call(context.Background(), "alpha.evil", json.RawMessage(`{}`))
	if callErr == nil {
		t.Errorf("a forged, unknown-to-the-server name must degrade to an error, not a success")
	}
	if out == "" {
		t.Errorf("the forged name must degrade to an error-text result, got empty")
	}
	t.Logf("forged name %q -> result %q", "alpha.evil", out)
}
