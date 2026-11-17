package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
)

// S36-mcp-dispatch T3 — the dial phase, tested with a fake mcpClient injected
// through the dialServer seam (no real subprocess). The seam is a variable,
// so a test overrides it and restores it; production is mcp.Dial.

// fakeClient is a controllable mcpClient. block makes ListTools hang until the
// context is done (the budget, in production) — the timeout/parallel tests use
// it.
type fakeClient struct {
	tools    []mcp.RemoteTool
	listErr  error
	block    bool
	callText string
	callErr  error
	calls    int
	closed   int
}

func (f *fakeClient) ListTools(ctx context.Context) ([]mcp.RemoteTool, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

func (f *fakeClient) CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error) {
	f.calls++
	if f.callErr != nil {
		return mcp.CallResult{}, f.callErr
	}
	return mcp.CallResult{Text: f.callText}, nil
}

func (f *fakeClient) Close() error {
	f.closed++
	return nil
}

// setMCPConfig points the shared config-read path (Q1) at a fixture config, so
// buildMCPBundle's loadMCPConfig reads the test's config, not the real store.
// S46 T3: that path reads the TABLES now — the helper seeds the same fixture
// fact as rows (mcp_seed_test.go); call sites unchanged.
func setMCPConfig(t *testing.T, cfg mcp.Config) {
	t.Helper()
	isolateStoreEnv(t) // the leaked-DATABASE-keys pin (policy_golden_test.go:179)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := t.TempDir() + "/mcp.json"
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AEON_MCP_CONFIG", path)
	seedMCPConfigRows(t, cfg)
}

// setDial overrides the one dial path for the duration of the test.
func setDial(t *testing.T, f func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error)) {
	t.Helper()
	old := dialServer
	dialServer = f
	t.Cleanup(func() { dialServer = old })
}

func allowPolicy(agent, qualified string) policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: "allow"})
	return policy.Policy{
		ID:     "t3-allow-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: policy.Allow,
		Spec:   spec,
	}
}

func testTransport() mcp.TransportConfig {
	return mcp.TransportConfig{Type: "stdio", Command: "unused"}
}

func hasWithheld(wh []daemon.MCPWithheld, q string) bool {
	for _, w := range wh {
		if w.Qualified == q {
			return true
		}
	}
	return false
}

func withheldSentence(wh []daemon.MCPWithheld, q string) string {
	for _, w := range wh {
		if w.Qualified == q {
			return w.Sentence
		}
	}
	return ""
}

func TestZeroOfferedMeansZeroDials(t *testing.T) {
	// No servers → GrantView offers only core tools → T2 excludes core → zero
	// offered → zero dials (MD8 / MC11's spirit, carried here).
	setMCPConfig(t, mcp.Config{})
	dials := 0
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		dials++
		return nil, errors.New("dial attempted with no offered tools")
	})

	bundle, err := buildMCPBundle(context.Background(), "agent-a", nil, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bundle.Specs) != 0 {
		t.Errorf("no offered tools but %d specs: %v", len(bundle.Specs), bundle.Specs)
	}
	if dials != 0 {
		t.Errorf("dials = %d, want 0 (MD8: no offered tools → no dials)", dials)
	}
	if bundle.Close == nil {
		t.Errorf("Close must not be nil even with no dials")
	}
	if err := bundle.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestLiveSchemaWins(t *testing.T) {
	// The config carries NO schema (ToolConfig has no InputSchema), so the
	// only schema that can reach a spec is the server's live one. A live
	// schema therefore proves the spec was built from the live ListTools,
	// not synthesized from config (MD2).
	liveSchema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{tools: []mcp.RemoteTool{{Name: "search", InputSchema: liveSchema, Description: "live desc"}}}, nil
	})

	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{allowPolicy("agent-a", "alpha.search")}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bundle.Specs) != 1 {
		t.Fatalf("specs = %d, want 1: %v", len(bundle.Specs), bundle.Specs)
	}
	spec := bundle.Specs[0]
	if spec.Name != "alpha.search" {
		t.Errorf("spec name = %q, want alpha.search", spec.Name)
	}
	if string(spec.Parameters) != string(liveSchema) {
		t.Errorf("spec parameters = %s, want the live schema %s (MD2: live wins, never synthesized)", spec.Parameters, liveSchema)
	}
	if len(spec.Parameters) == 0 {
		t.Errorf("spec parameters empty: the schema must come from the live server")
	}
}

func TestOfferedButNotLiveIsWithheld(t *testing.T) {
	// "present" is live; "gone" is offered by the config but absent from the
	// server's live list → withheld with the named sentence (never silently
	// dropped or silently kept).
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "present", Rerun: "safe", Effect: "local"}, {Name: "gone", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.present", "alpha.gone"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{tools: []mcp.RemoteTool{{Name: "present", InputSchema: json.RawMessage(`{"type":"object"}`)}}}, nil
	})

	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{
		allowPolicy("agent-a", "alpha.present"),
		allowPolicy("agent-a", "alpha.gone"),
	}, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bundle.Specs) != 1 {
		t.Errorf("specs = %d, want 1 (only the live tool): %v", len(bundle.Specs), bundle.Specs)
	}
	if bundle.Specs[0].Name != "alpha.present" {
		t.Errorf("the live tool should be the only spec, got %v", bundle.Specs)
	}
	if !hasWithheld(bundle.Withheld, "alpha.gone") {
		t.Fatalf("alpha.gone not withheld: %v", bundle.Withheld)
	}
	// The exact named sentence — a second wording would be two truths (MD9).
	want := noLongerListedSentence("alpha.gone", "alpha")
	if got := withheldSentence(bundle.Withheld, "alpha.gone"); got != want {
		t.Errorf("withheld sentence = %q, want %q", got, want)
	}
}

func TestDialTimeoutWithholdsNotHangs(t *testing.T) {
	// A server that never answers: the per-server budget bounds the wait
	// (not a longer parent ctx), and the offered tool degrades to withheld.
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	// The fake's ListTools blocks until the budget's ctx is done.
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{block: true}, nil
	})

	// A long-lived parent ctx: only the budget bounds the wait.
	start := time.Now()
	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{allowPolicy("agent-a", "alpha.search")}, time.Now())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a timed-out dial must not fail the build (the step starts anyway): %v", err)
	}
	if len(bundle.Specs) != 0 {
		t.Errorf("a timed-out server must offer no specs: %v", bundle.Specs)
	}
	if !hasWithheld(bundle.Withheld, "alpha.search") {
		t.Errorf("alpha.search should be withheld on timeout")
	}
	if elapsed > mcpDialBudget+5*time.Second {
		t.Errorf("elapsed %v exceeds the budget+slack; the per-server budget is not the bound", elapsed)
	}
	t.Logf("elapsed against budget %v: %v", mcpDialBudget, elapsed)
}

func TestDialsAreParallelAcrossServers(t *testing.T) {
	// Three servers, each hanging for the budget: parallel ≈ one budget,
	// sequential ≈ three. Assert the wall time is roughly one budget.
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{
			{Name: "alpha", Transport: testTransport(), Tools: []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}},
			{Name: "beta", Transport: testTransport(), Tools: []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}},
			{Name: "gamma", Transport: testTransport(), Tools: []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}},
		},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search", "beta.search", "gamma.search"}}},
	})
	ps := []policy.Policy{
		allowPolicy("agent-a", "alpha.search"),
		allowPolicy("agent-a", "beta.search"),
		allowPolicy("agent-a", "gamma.search"),
	}
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{block: true}, nil
	})

	start := time.Now()
	if _, err := buildMCPBundle(context.Background(), "agent-a", ps, time.Now()); err != nil {
		t.Fatalf("build: %v", err)
	}
	elapsed := time.Since(start)
	// Parallel: within ~one budget (plus slack). Sequential would be ~3×.
	if elapsed > 2*mcpDialBudget {
		t.Errorf("wall time %v > 2× budget: dials are not parallel (sequential would be ~%v)", elapsed, 3*mcpDialBudget)
	}
	t.Logf("three servers, each hanging for the budget: wall %v (budget %v)", elapsed, mcpDialBudget)
}

func TestUnreachableServerStepStartsAnyway(t *testing.T) {
	// An unreachable server: the offered tool is withheld (not an error), so
	// the step starts with the tool simply absent from its toolset
	// (criterion 4's build-time half).
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return nil, errors.New("mcp: server \"alpha\": initialize: connection refused")
	})

	bundle, err := buildMCPBundle(context.Background(), "agent-a", []policy.Policy{allowPolicy("agent-a", "alpha.search")}, time.Now())
	if err != nil {
		t.Fatalf("an unreachable server must not fail the build (the step starts anyway): %v", err)
	}
	if len(bundle.Specs) != 0 {
		t.Errorf("an unreachable server must offer no specs: %v", bundle.Specs)
	}
	if !hasWithheld(bundle.Withheld, "alpha.search") {
		t.Errorf("the unreachable server's tool must be withheld: %v", bundle.Withheld)
	}
}
