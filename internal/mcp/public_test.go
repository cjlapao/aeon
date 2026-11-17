package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T6: the public tier — MC5's freedom, L22's friction, MC4's revocation.
//
// The adoption story this tier carries: a company with only MCP servers
// writes agents with no tools of their own, pointed at public ones, and has
// a working system. Freedom is about DECLARATION (declare = have, no grant
// ceremony); it is safe only because the effect class carries the risk.

// ── policy helpers (shared across this package's tests) ───────────────────
//
// The authoring shape is the working form from S10's own tests: a ToolAccess
// row whose spec names the tool, target agent:<name>. These construct
// `policy.Policy` directly — the CLI write path is exercised in S10 and at
// the CLI seam in T9; what the registry sees is the same struct either way.

func allowPolicy(agent, qualified string) []policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: "allow"})
	return []policy.Policy{{
		ID:     "allow-" + agent + "-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: policy.Allow,
		Spec:   spec,
	}}
}

func denyAllAgents(qualified string) policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: "deny"})
	return policy.Policy{
		ID:     "deny-all-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny,
		Spec:   spec,
	}
}

func askRule(agent, qualified string) policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: "ask"})
	return policy.Policy{
		ID:     "ask-" + agent + "-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: policy.Ask,
		Spec:   spec,
	}
}

// publicTwoDeclares is the tier's shape: one public server, two agents that
// declared its tools, one that declared nothing.
func publicTwoDeclares() Config {
	return Config{
		Public: []ServerConfig{{
			Name:      "alpha",
			Transport: TransportConfig{Type: TransportStdio, Command: "unused"},
			Tools: []ToolConfig{
				{Name: "search", Rerun: "safe", Effect: "local"},
				{Name: "post", Rerun: "unsafe", Effect: "outward"},
			},
		}},
		Agents: map[string]AgentConfig{
			"agent-a": {Declares: []string{"alpha.search", "alpha.post"}},
			"agent-b": {Declares: []string{"alpha.search"}},
			"agent-c": {},
		},
	}
}

// TestTwoAgentsShareOnePublicTool is the adoption story at its smallest:
// two agents declare one public tool and BOTH call it — no per-agent grant
// step, no operator ceremony, one client behind one server.
func TestTwoAgentsShareOnePublicTool(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	if err := r.AttachClient("alpha", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	for _, agent := range []string{"agent-a", "agent-b"} {
		res, err := r.Call(context.Background(), agent, "alpha.search", json.RawMessage(`{"q":`+`"aeon"}`), allowPolicy(agent, "alpha.search"), time.Now())
		if err != nil || res.IsError || res.AskRequired {
			t.Fatalf("%s's call to the shared public tool failed (err=%v res=%+v)", agent, err, res)
		}
		if !strings.Contains(res.Content, `{"results":["alpha","beta"]}`) {
			t.Errorf("%s got %q", agent, res.Content)
		}
	}
	// ONE client served both — the call counter proves neither got a
	// private copy of the server (there is no such thing in MC5).
	n, err := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"search"}`))
	if err != nil || n.Text != "2" {
		t.Errorf("expected 2 invocations on the shared server, got %q (err %v)", n.Text, err)
	}
}

// TestAgentThatDoesNotDeclarePublicToolDoesNotHaveIt is MC5's boundary:
// public means AVAILABLE TO DECLARE, not granted to all. The non-declarer's
// answer is `unknown tool` with the same framing as a never-existent name —
// "does not have it" is invisibility, not a denial it could probe.
func TestAgentThatDoesNotDeclarePublicToolDoesNotHaveIt(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	for _, s := range r.ToolSpecsFor("agent-c") {
		if strings.HasPrefix(s.Name, "alpha.") {
			t.Errorf("non-declarer's prompt carries public tool %q — public ≠ granted-to-all", s.Name)
		}
	}
	res, err := r.Call(context.Background(), "agent-c", "alpha.search", nil,
		allowPolicy("agent-c", "alpha.search"), time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// Even WITH an allow policy the non-declarer gets unknown — the policy
	// cannot conjure a tool its own config never declared: the declaration
	// is the agent's act, the allow is the operator's, neither substitutes
	// for the other.
	if !strings.HasPrefix(res.Content, "unknown tool:") || !res.IsError {
		t.Errorf("non-declarer's answer = %q (IsError=%v), want `unknown tool:`", res.Content, res.IsError)
	}
}

// TestAllAgentsDenyClosesPublicDoor is MC4 paying off: the grant was a
// policy, so the revocation is a policy. NO code change, NO config edit —
// the same Registry object, same config file, one additional policy row.
// And the refusal is S10's sentence VERBATIM, compared byte-for-byte with
// what policy.Evaluate renders in this very test.
func TestAllAgentsDenyClosesPublicDoor(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	before := allowPolicy("agent-a", "alpha.search")
	res, err := r.Call(context.Background(), "agent-a", "alpha.search", nil, before, time.Now())
	if err != nil || res.IsError {
		t.Fatalf("the door was closed before the deny: %+v %v", res, err)
	}

	ps := append(before, denyAllAgents("alpha.search"))
	res, err = r.Call(context.Background(), "agent-a", "alpha.search", nil, ps, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.IsError {
		t.Fatal("the all-agents deny did not close the door")
	}
	// S10's ONE formatter, verbatim — the byte-exactness is the assertion.
	want := policy.Evaluate(ps, policy.Request{
		Type: policy.ToolAccess, Agent: "agent-a", MCPServer: "alpha", Tool: "alpha.search",
	}, time.Now()).Sentence()
	if res.Content != want {
		t.Errorf("refusal drifted from S10's sentence:\n got %q\nwant %q", res.Content, want)
	}
	// A deny beats the allow at PL4 (deny > ask > allow, specificity never
	// rescues an allow) — and the server never heard a word.
	n, err := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"search"}`))
	if err != nil || n.Text != "1" {
		t.Errorf("server heard %s calls, want 1 (the pre-deny one only; err %v)", n.Text, err)
	}
}

// TestOutwardPublicToolStillAsks is the half of MC5 that makes the other
// half safe: the freedom is about declaration, never about effects. Even
// UNDER a ToolAccess allow, the outward effect asks at use — and asking
// means the server was NOT called.
func TestOutwardPublicToolStillAsks(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	res, err := r.Call(context.Background(), "agent-a", "alpha.post", nil, allowPolicy("agent-a", "alpha.post"), time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.AskRequired {
		t.Fatalf("outward public tool ran without asking under a bare allow: %+v (L22/MC5 violation)", res)
	}
	if res.IsError {
		t.Errorf("an ask is not an error (three-state honesty): %+v", res)
	}
	if !strings.Contains(res.Content, "outward") || !strings.Contains(res.Content, "alpha.post") {
		t.Errorf("the ask must name the tool and the effect that summoned it: %q", res.Content)
	}
	// The decisive half: asking first, calling anyway, is not consent —
	// the server must have heard NOTHING.
	n, err := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"post"}`))
	if err != nil || n.Text != "0" {
		t.Errorf("outward tool was invoked %s times while owed a consent decision (err %v)", n.Text, err)
	}
}

// TestReadonlyPublicToolDoesNotAsk is the friction half: the LOCAL/SAFE
// tool under the same allow runs silently. Friction sits where the danger
// is, not on every declaration.
func TestReadonlyPublicToolDoesNotAsk(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	res, err := r.Call(context.Background(), "agent-b", "alpha.search", nil, allowPolicy("agent-b", "alpha.search"), time.Now())
	if err != nil || res.IsError || res.AskRequired {
		t.Fatalf("read-only public tool asked or failed: %+v %v (every tool asking every time is how consent fatigue is manufactured)", res, err)
	}
}

// TestMatchedAskRuleAsksWithS10Sentence: an explicit `ask` rule routes even
// a local tool to a human — S10's shape for it, verbatim, and still no call.
func TestMatchedAskRuleAsksWithS10Sentence(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	ps := append(allowPolicy("agent-b", "alpha.search"), askRule("agent-b", "alpha.search"))
	res, err := r.Call(context.Background(), "agent-b", "alpha.search", nil, ps, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.AskRequired || res.IsError {
		t.Fatalf("matched ask rule did not route to a human: %+v", res)
	}
	want := policy.Evaluate(ps, policy.Request{
		Type: policy.ToolAccess, Agent: "agent-b", MCPServer: "alpha", Tool: "alpha.search",
	}, time.Now()).Sentence()
	if res.Content != want {
		t.Errorf("ask did not carry S10's sentence verbatim:\n got %q\nwant %q", res.Content, want)
	}
	n, err := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"search"}`))
	if err != nil || n.Text != "0" {
		t.Errorf("asked tool was called %s times (err %v)", n.Text, err)
	}
}

// TestNoAllowMeansUnavailableMC10 is the default-deny face MC10 names for
// MCP tools specifically: with no allow rule at all, the call refuses and
// the sentence says NO ALLOW — not "denied by policy" (no rule said that),
// not "unknown" (the tool exists for this agent).
func TestNoAllowMeansUnavailableMC10(t *testing.T) {
	cfg := publicTwoDeclares()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("alpha", c)

	res, err := r.Call(context.Background(), "agent-a", "alpha.search", nil, nil, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(res.Content, "unavailable:") || !strings.Contains(res.Content, "MC10") {
		t.Errorf("no-allow answer = %q (IsError=%v), want an `unavailable:` MC10 refusal", res.Content, res.IsError)
	}
	if strings.Contains(strings.ToLower(res.Content), "denied") {
		t.Errorf("no-allow refusal says 'denied' — nobody denied anything, no rule matched: %q", res.Content)
	}
	n, err := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"search"}`))
	if err != nil || n.Text != "0" {
		t.Errorf("unallowed tool was called %s times (err %v)", n.Text, err)
	}
}

// TestPublicToolsAreNamespacedByServer is MC8 at the prompt surface:
// `alpha.search`, never `search` — two servers offering `search` stay two
// tools (the registry-level twin ran in T1; this is the one agents see).
func TestPublicToolsAreNamespacedByServer(t *testing.T) {
	cfg := publicTwoDeclares()
	r := mustRegistry(t, cfg)
	for _, s := range r.ToolSpecsFor("agent-a") {
		if s.Name == "search" || s.Name == "post" {
			t.Errorf("unqualified public tool name %q reached the agent (MC8)", s.Name)
		}
	}
}

// TestPolicyCannotConjureWhatConfigNeverDeclared closes the seam the whole
// design depends on: policy grants USE, config declares EXISTENCE, and
// neither crosses into the other. A policy naming a tool the public server
// does not carry changes nothing — and, critically, says nothing:
// the answer is the same `unknown tool:` framing as any phantom name.
func TestPolicyCannotConjureWhatConfigNeverDeclared(t *testing.T) {
	cfg := publicTwoDeclares()
	r := mustRegistry(t, cfg)
	res, err := r.Call(context.Background(), "agent-a", "alpha.nonexistent", nil,
		allowPolicy("agent-a", "alpha.nonexistent"), time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if want := "unknown tool: alpha.nonexistent"; res.Content != want {
		t.Errorf("policy-named phantom answered %q, want exactly %q (no policy disclosure)", res.Content, want)
	}
}
