package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// S36-mcp-dispatch T5 — the slice's real acceptance test and the edges.
//
// The only fake is the MCP server (setDial); the policy gate is the real
// engine (policy.Evaluate, the same path daemon/policy_gate.go's
// storePolicyGate takes — see mcpGate below), and the loop is the real
// runtime.RunStep. So this proves T1–T4 COMPOSE, not that each compiles
// (criterion 1). It lives in cmd/aeon — the one package that imports the
// daemon (the composite), the runtime (the loop), and internal/mcp (the dial
// the tests inject) together; the spec's proposed internal/daemon home cannot
// reach the dial, and internal/runtime cannot import the daemon.
//
// The ask edges (criterion 3) and the SEC-059 canonicalizer edge follow.

// --- the scripted model client (the loop's only fake) ----------------------

// mcpModelClient plays a script of turns and records how many it was asked.
// It is the cmd/aeon analogue of the runtime's fakeClient (which is unexported
// there); the loop asks for a turn per iteration and drains the channels in
// its own goroutine, so a mutex keeps the counter -race clean.
type mcpModelClient struct {
	mu    sync.Mutex
	turns []mcpScriptTurn
	calls int
}

type mcpScriptTurn struct {
	ToolUses []model.Block
	Text     string
}

func (c *mcpModelClient) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	c.mu.Lock()
	index := c.calls
	c.calls++
	var turn mcpScriptTurn
	if index < len(c.turns) {
		turn = c.turns[index]
	} else {
		turn = mcpScriptTurn{Text: `{"status":"ok","summary":"ran out of script"}`}
	}
	c.mu.Unlock()

	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		for i := range turn.ToolUses {
			b := turn.ToolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &b}
		}
		if turn.Text != "" {
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.Text}
		}
		results <- model.StreamResult{Attempts: 1}
	}()
	return deltas, results, errs
}

// --- the faithful gate (the real engine, not a mock) ----------------------

// mcpGate is the production gate's logic over a supplied policy set — the
// exact path daemon/policy_gate.go's storePolicyGate.Decide takes
// (gateQuestion → policy.Evaluate → the Asked half), minus the store read and
// the audit write (which the acceptance test does not assert). It is not a
// mock: the decision is policy.Evaluate over the operator's rows, decided the
// way the real gate decides, including the ask tier the runtime suspends on.
type mcpGate struct {
	ps []policy.Policy
}

func (g mcpGate) Decide(req runtime.PolicyGateRequest, at time.Time) runtime.PolicyGateDecision {
	// gateQuestion (S11), the one rule the enforcement and the spawn-key read
	// share: the tool name is asked in its canonical form.
	preq := policy.Request{
		Type:      policy.ToolAccess,
		Agent:     req.Agent,
		Loop:      req.Loop,
		LaneType:  req.LaneType,
		MCPServer: req.MCPServer,
		Tool:      tools.CanonicalToolName(req.Tool),
	}
	if preq.Tool == "bash" && req.Command != "" {
		preq.Type = policy.CommandAccess
		preq.Command = req.Command
	}
	v := policy.Evaluate(g.ps, preq, at)
	// The Asked half (S11): a question, not a refusal or a permission.
	if v.Asked {
		return runtime.PolicyGateDecision{
			Allowed: false, Asked: true, Sentence: v.Sentence(), Command: req.Command,
		}
	}
	return runtime.PolicyGateDecision{Allowed: v.Allowed, Sentence: v.Sentence()}
}

// --- fixtures -------------------------------------------------------------

func mcpStepHandler() runtime.StepHandler {
	// The handler name IS the agent the gate is asked about (req.Agent =
	// Handler.Name), so it must match the rows' target (agent-a) — the gate
	// binds no alias here, the names are the same.
	return runtime.StepHandler{
		Name:            "agent-a",
		System:          "you are a test",
		Sections:        []runtime.SectionSpec{{Title: "Task", Text: "do the thing", Category: runtime.CategorySystemPrompt}},
		Tools:           []string{},
		HandoffSchema:   `{"type":"object","required":["status"]}`,
		MaxOutputTokens: 1024,
	}
}

func mcpTestRoute() model.Route {
	return model.Route{ID: "test", Provider: "anthropic", Model: "claude-test", MaxOutputTokens: 1024}
}

// askRow is the operator's ask policy for a tool (the S11 third action): a
// ToolAccess row whose State is ask, so policy.Evaluate routes the question.
func askRow(agent, qualified string) policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: string(policy.Ask)})
	return policy.Policy{
		ID:     "t5-ask-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: policy.Ask,
		Spec:   spec,
	}
}

// mcpStepRequest builds the step the acceptance test runs: the offered specs
// from BuildTools (with the MCP server's live schema), a frozen manifest, the
// route, and the operator/step identity the ask edge turns on.
func mcpStepRequest(handler runtime.StepHandler, specs []model.ToolSpec, root string, hasOperator bool) runtime.StepRequest {
	return runtime.StepRequest{
		Handler:       handler,
		Messages:      runtime.OpeningTranscript(handler, "", "do the thing", nil),
		Manifest:      runtime.ContextManifest{MaxIterations: runtime.MaxIterations, DeadlineAt: time.Now().Add(time.Hour)},
		Route:         mcpTestRoute(),
		Tools:         specs,
		WorkspaceRoot: root,
		StepID:        7,
		HasOperator:   hasOperator,
	}
}

// mcpRunOpts wires the real loop with the composite caller and the faithful
// gate — nothing mocked but the model client and the dial.
func mcpRunOpts(client *mcpModelClient, caller runtime.ToolCaller, gate runtime.PolicyGate) runtime.RunOpts {
	return runtime.RunOpts{
		Client:      client,
		Tools:       caller,
		Sink:        runtime.DiscardSink{},
		HandoffSpec: runtime.DefaultHandoffSpec(),
		StatusField: "status",
		Policy:      gate,
	}
}

// toolResultFor reads the tool_result answering toolUseID from the transcript.
func toolResultFor(msgs []model.Message, toolUseID string) (model.Block, bool) {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == toolUseID {
				return b, true
			}
		}
	}
	return model.Block{}, false
}

// dialFakeAnswering returns a dial that answers every call with one canned
// text — the fake MCP server, the only fake in the chain. The server lists
// the tool live so the offer keeps it (MD2: the live schema wins).
func dialFakeAnswering(answer string) func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
	return func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{
			tools:    []mcp.RemoteTool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}},
			callText: answer,
		}, nil
	}
}

// --- item 1: the one real-loop-step test (criterion 1) ---------------------

func TestRealStepOffersCallsAndRecordsMCPTool(t *testing.T) {
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, dialFakeAnswering("the fake server's answer"))

	// The real StepMCP (buildMCPBundle, with the fake dial) → the real
	// composite (daemon.BuildTools) → the real loop (runtime.RunStep).
	ps := []policy.Policy{allowPolicy("agent-a", "alpha.search")}
	runner := daemon.StepRunner{StepMCP: func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*daemon.MCPBundle, error) {
		return buildMCPBundle(ctx, agent, ps, at)
	}}
	root := t.TempDir()
	caller, specs, err := runner.BuildTools([]string{}, root, "agent-a", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	// The model is offered the qualified name with the server's LIVE schema
	// (MD2) — not a synthesized one.
	var offered bool
	for _, s := range specs {
		if s.Name == "alpha.search" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("alpha.search was not offered: %v", specs)
	}

	// The model calls alpha.search, then hands off. The only fake is the dial.
	client := &mcpModelClient{turns: []mcpScriptTurn{
		{ToolUses: []model.Block{model.ToolUseBlock("t1", "alpha.search", json.RawMessage(`{}`))}},
		{Text: `{"status":"ok","summary":"done"}`},
	}}
	gate := mcpGate{ps: ps}
	opts := mcpRunOpts(client, caller, gate)
	req := mcpStepRequest(mcpStepHandler(), specs, root, true)

	res := runtime.RunStep(context.Background(), req, opts)
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("outcome %s (%s), want ok — the full chain must complete", res.Outcome, res.Reason)
	}
	blk, ok := toolResultFor(res.Messages, "t1")
	if !ok {
		t.Fatal("no tool_result for the MCP call — the result did not land in the transcript")
	}
	if blk.IsError || blk.Content != "the fake server's answer" {
		t.Errorf("the MCP call's result is %+v, want the fake server's own answer (not an error, not a refusal)", blk)
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the transcript is invalid after the MCP call: %s", v.Reason)
	}
}

// --- item 2: the ask edges (criterion 3) -----------------------------------

// TestMCPToolAskParksOnExistingPendingPermission — the attended half. The
// ToolAccess row is ask (not allow): the dispatch suspends on the EXISTING
// pendingPermission path (S11), never an MCP-specific branch — the question
// names the tool (alpha.search) and the block's own id, and the call never
// reaches the dispatcher.
func TestMCPToolAskParksOnExistingPendingPermission(t *testing.T) {
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, dialFakeAnswering("must-not-run"))

	ps := []policy.Policy{askRow("agent-a", "alpha.search")}
	runner := daemon.StepRunner{StepMCP: func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*daemon.MCPBundle, error) {
		return buildMCPBundle(ctx, agent, ps, at)
	}}
	root := t.TempDir()
	caller, specs, err := runner.BuildTools([]string{}, root, "agent-a", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	client := &mcpModelClient{turns: []mcpScriptTurn{
		{ToolUses: []model.Block{model.ToolUseBlock("t1", "alpha.search", json.RawMessage(`{}`))}},
		{Text: `{"status":"ok","summary":"done"}`},
	}}
	opts := mcpRunOpts(client, caller, mcpGate{ps: ps})
	req := mcpStepRequest(mcpStepHandler(), specs, root, true) // attended: HasOperator

	res := runtime.RunStep(context.Background(), req, opts)
	if res.Kind != runtime.ResultSuspended {
		t.Fatalf("kind %s (outcome %s), want suspended — an attended ask must suspend like the question tool",
			res.Kind, res.Outcome)
	}
	q := res.Question
	if q == nil {
		t.Fatal("suspended without a question — the step waits on a question nobody was given")
	}
	if q.ToolUseID != "t1" {
		t.Errorf("question ToolUseID %q, want the blocked block's own id t1 — the answer resumes THAT call", q.ToolUseID)
	}
	// The blocked call never reached the dispatcher: no tool_result was
	// answered for it (the answer, not the suspension, answers it).
	if _, ok := toolResultFor(res.Messages, "t1"); ok {
		t.Error("the blocked call was answered at suspension — the ANSWER answers it, not the suspension")
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the suspended transcript is invalid: %s", v.Reason)
	}
}

// TestMCPToolUnattendedDeniesAtOnce — the unattended half (S11's rule,
// verbatim): no operator, so the ask is answered by nobody — a deny NOW,
// verdict sentence as an IsError tool_result, the call never runs, and NO
// question escapes (no PendingQuestion, so no question row).
func TestMCPToolUnattendedDeniesAtOnce(t *testing.T) {
	setMCPConfig(t, mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: testTransport(),
			Tools:     []mcp.ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]mcp.AgentConfig{"agent-a": {Declares: []string{"alpha.search"}}},
	})
	setDial(t, dialFakeAnswering("must-not-run"))

	ps := []policy.Policy{askRow("agent-a", "alpha.search")}
	runner := daemon.StepRunner{StepMCP: func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*daemon.MCPBundle, error) {
		return buildMCPBundle(ctx, agent, ps, at)
	}}
	root := t.TempDir()
	caller, specs, err := runner.BuildTools([]string{}, root, "agent-a", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	client := &mcpModelClient{turns: []mcpScriptTurn{
		{ToolUses: []model.Block{model.ToolUseBlock("t1", "alpha.search", json.RawMessage(`{}`))}},
		{Text: `{"status":"ok","summary":"done"}`},
	}}
	opts := mcpRunOpts(client, caller, mcpGate{ps: ps})
	req := mcpStepRequest(mcpStepHandler(), specs, root, false) // unattended: no operator

	res := runtime.RunStep(context.Background(), req, opts)
	if res.Kind == runtime.ResultSuspended || res.Question != nil {
		t.Fatalf("an unattended ask must not suspend (kind %s) — no operator means no question row", res.Kind)
	}
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("outcome %s (%s), want ok — the unattended deny is a tool_result, the step continues", res.Outcome, res.Reason)
	}
	blk, ok := toolResultFor(res.Messages, "t1")
	if !ok {
		t.Fatal("no tool_result for the unattended call — the deny must be recorded")
	}
	if !blk.IsError {
		t.Errorf("the unattended ask must deny with an IsError tool_result, got %+v", blk)
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the transcript is invalid after the unattended deny: %s", v.Reason)
	}
}

// --- item 3: the canonicalizer meets the dot (R4, SEC-059) ----------------

// TestCanonicalizerKeepsNameSpacesApart — the first-party alias space and the
// server.tool space must not cross-canonicalize into each other. A row
// authored write_file canonicalises to write (the honest name); a qualified
// name canonicalises to itself (it is not in the first-party registry), so
// the two never match — and vice versa.
func TestCanonicalizerKeepsNameSpacesApart(t *testing.T) {
	// The first-party alias and its honest name really differ from a
	// qualified name: the canonicalizer is the seam that keeps them apart.
	if got := tools.CanonicalToolName("alpha.search"); got != "alpha.search" {
		t.Errorf("a qualified name must canonicalise to itself (identity), got %q", got)
	}
	// A row authored in the alias spelling must not match a qualified call,
	// and a qualified row must not match an alias-spelled call — neither
	// space canonicalises into the other.
	aliasRow := policy.Policy{
		ID: "t5-alias", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "agent-a"},
		Action: policy.Allow,
		Spec:   mustToolAccessSpec(t, "write_file", "allow"),
	}
	qualRow := policy.Policy{
		ID: "t5-qual", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "agent-a"},
		Action: policy.Allow,
		Spec:   mustToolAccessSpec(t, "alpha.search", "allow"),
	}
	at := time.Now()
	// alias row vs a qualified call: no cross-canonical match.
	if v := policy.Evaluate([]policy.Policy{aliasRow}, policy.Request{
		Type: policy.ToolAccess, Agent: "agent-a", Tool: "alpha.search",
	}, at); v.Allowed {
		t.Error("a write_file alias row matched a qualified alpha.search call — the name spaces cross-canonicalized")
	}
	// qualified row vs an alias-spelled call: no cross-canonical match.
	if v := policy.Evaluate([]policy.Policy{qualRow}, policy.Request{
		Type: policy.ToolAccess, Agent: "agent-a", Tool: "write_file",
	}, at); v.Allowed {
		t.Error("a qualified alpha.search row matched a write_file call — the name spaces cross-canonicalized")
	}
	// A row and a call in the SAME space still match (the canonicaliser did
	// not break exactness for the qualified name itself).
	if v := policy.Evaluate([]policy.Policy{qualRow}, policy.Request{
		Type: policy.ToolAccess, Agent: "agent-a", Tool: "alpha.search",
	}, at); !v.Allowed {
		t.Errorf("a qualified row did not match its own qualified call — canonicalisation broke exactness: %s", v.Sentence())
	}
}

func mustToolAccessSpec(t *testing.T, toolName, state string) json.RawMessage {
	t.Helper()
	spec, err := json.Marshal(policy.ToolAccessSpec{ToolName: toolName, State: state})
	if err != nil {
		t.Fatalf("marshal ToolAccessSpec: %v", err)
	}
	return spec
}

// --- the diff check: no MCP-aware branch snuck into loop.go ---------------

// TestNoMCPBranchAddedToLoopGo — the whole point of the slice is that a
// qualified name is just a name to the existing gate, so loop.go's
// policy-gate call site must not gain an MCP-aware branch (an `if
// strings.Contains(use.Name, ".")` special-casing). Checked by reading the
// source, not assumed: the gate is asked about the raw tool name (Tool:
// use.Name) and the dot lives in the daemon's composite, never in the gate.
func TestNoMCPBranchAddedToLoopGo(t *testing.T) {
	// loop.go is one directory up from cmd/aeon and one more up to the module:
	// cmd/aeon -> aeon/internal/runtime/loop.go.
	b, err := os.ReadFile("../../internal/runtime/loop.go")
	if err != nil {
		t.Fatalf("read loop.go: %v", err)
	}
	src := string(b)
	// The gate is asked about the tool in its raw (qualified or not) name — the
	// dot is the router's job in the daemon composite, never the gate's.
	if !strings.Contains(src, "Tool: use.Name") {
		t.Error("loop.go's gate call no longer asks about use.Name — the subject of the question changed")
	}
	// No dot-branch on the tool name at the gate call site: the dot is the
	// router's job in the daemon composite, and a qualified name is just a name
	// to the gate (brief §2). A branch on the tool name would be a second
	// truth for who may call what.
	if strings.Contains(src, "strings.Contains(use.Name") {
		t.Error("loop.go branched on strings.Contains(use.Name, ...) — an MCP-aware branch snuck into the gate")
	}
}
