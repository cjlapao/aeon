package runtime

// The S10 policy-gate tests (loop T9). The one that matters is the negative:
// **a denied tool is never called** — proven with the spy at zero, not by
// observing a failure. And the structural claim — dispatch is UNREACHABLE
// without an allowing verdict — is backed here by a textual pin (exactly one
// dispatcher-call expression in loop.go, after the gate guard) because a
// refactor is exactly what turns "before" into "branch nobody takes".
//
// This file is the ONE place in `internal/runtime` that may import
// `internal/policy`: the byte-equality test has to build the expected sentence
// from the same fixture the gate decides on. Non-test runtime files import
// neither policy nor store (K22) — TestThisPackageImportsNothingThatCouldPersist
// and `go list -deps` say so.

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
)

// scriptedGate is the test's PolicyGate: it records every question and the
// instant it was asked, and answers from a function so each test decides what
// "the gate" means.
type scriptedGate struct {
	mu       sync.Mutex
	requests []PolicyGateRequest
	ats      []time.Time
	decide   func(PolicyGateRequest, time.Time) PolicyGateDecision
}

func (g *scriptedGate) Decide(req PolicyGateRequest, at time.Time) PolicyGateDecision {
	g.mu.Lock()
	g.requests = append(g.requests, req)
	g.ats = append(g.ats, at)
	g.mu.Unlock()
	return g.decide(req, at)
}

func (g *scriptedGate) log() ([]PolicyGateRequest, []time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]PolicyGateRequest(nil), g.requests...), append([]time.Time(nil), g.ats...)
}

func denyGate(sentence string) *scriptedGate {
	return &scriptedGate{decide: func(PolicyGateRequest, time.Time) PolicyGateDecision {
		return PolicyGateDecision{Allowed: false, Sentence: sentence}
	}}
}

func allowGate(sentence string) *scriptedGate {
	return &scriptedGate{decide: func(PolicyGateRequest, time.Time) PolicyGateDecision {
		return PolicyGateDecision{Allowed: true, Sentence: sentence}
	}}
}

// engineGate decides with the real engine over a fixture set — the same
// policy.Evaluate a production adapter calls.
func engineGate(ps []policy.Policy) *scriptedGate {
	return &scriptedGate{decide: func(req PolicyGateRequest, at time.Time) PolicyGateDecision {
		v := policy.Evaluate(ps, policy.Request{
			Type: policy.ToolAccess, Agent: req.Agent, Loop: req.Loop,
			LaneType: req.LaneType, MCPServer: req.MCPServer, Tool: req.Tool,
		}, at)
		return PolicyGateDecision{Allowed: v.Allowed, Sentence: v.Sentence()}
	}}
}

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

func TestDeniedToolIsNeverCalled(t *testing.T) {
	// D11. The spy at ZERO is the assertion — a denial demonstrated by the
	// tool's implementation having not run, which is the only way to prove an
	// absence.
	const sentence = `refused: agent:test-handler · "no-read" · target all-agents · source operator · no expiry`
	spy := newSpyTools("read")
	client := newFakeClient(
		// The input CARRIES an "agent" field the model chose: it must not leak
		// into the identity the gate is asked about (PL1's trust boundary).
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"x","agent":"model-named-agent"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	gate := denyGate(sentence)

	sink := &recordingSink{}
	opts := runOpts(client, spy, sink)
	opts.Policy = gate
	res := RunStep(context.Background(), testRequest(toolSpec("read")), opts)

	if n := len(spy.recorded()); n != 0 {
		t.Fatalf("THE DENIED TOOL RAN %d TIME(S): %+v — the dispatch is supposed to be unreachable "+
			"without an allowing verdict", n, spy.recorded())
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s (%s) — the step must carry on after a refusal so the model can report",
			res.Outcome, res.Reason)
	}

	// The refusal reaches the model as an IsError tool_result carrying the
	// sentence — so every tool_use is answered and the transcript stays valid.
	blk, ok := toolResultFor(res.Messages, "t1")
	if !ok {
		t.Fatal("no tool_result was written for the denied call — its tool_use is unanswered")
	}
	if !blk.IsError || blk.Content != sentence {
		t.Errorf("the model was handed (%q, isError=%v), want (%q, isError=true)", blk.Content, blk.IsError, sentence)
	}
	var end *StepEvent
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "read" {
			end = &e
		}
	}
	if end == nil {
		t.Fatal("no step.tool.end event for the refusal")
	}
	if end.Result != sentence || !end.IsError {
		t.Errorf("the refusal event carries result=%q isError=%v, want the sentence and true", end.Result, end.IsError)
	}

	// The identity the gate was asked about is the SERVER-SIDE one.
	reqs, _ := gate.log()
	if len(reqs) != 1 {
		t.Fatalf("the gate was consulted %d time(s), want 1", len(reqs))
	}
	if reqs[0].Agent != "test-handler" {
		t.Errorf("the gate saw Agent=%q, want the compiled request's handler name — identity read from "+
			"the model's arguments is how a model names its way past an agent-scoped deny (PL1)", reqs[0].Agent)
	}
	if reqs[0].Tool != "read" {
		t.Errorf("the gate saw Tool=%q, want read", reqs[0].Tool)
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the transcript is invalid after a refusal: %s", v.Reason)
	}
}

func TestAllowedToolStillRuns(t *testing.T) {
	// The obvious one — and it is how you catch a gate that denies everything
	// (which would otherwise make TestDeniedToolIsNeverCalled pass forever).
	spy := newSpyTools("read")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	gate := allowGate("allowed: agent:test-handler · \"yes-read\" · target all-agents · source operator · no expiry")

	opts := runOpts(client, spy, &recordingSink{})
	opts.Policy = gate
	res := RunStep(context.Background(), testRequest(toolSpec("read")), opts)

	calls := spy.recorded()
	if len(calls) != 1 || calls[0].Name != "read" {
		t.Fatalf("the allowed tool did not run through the dispatcher: %+v", calls)
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s (%s)", res.Outcome, res.Reason)
	}
	blk, ok := toolResultFor(res.Messages, "t1")
	if !ok || blk.IsError || !strings.Contains(blk.Content, "read ran") {
		t.Errorf("the allowed call's result is %+v, want the tool's own output", blk)
	}
}

func TestGateIsStructurallyBeforeDispatch(t *testing.T) {
	// G12 generalised — the structural claim, two ways.
	//
	// (1) Behavioural: a model that keeps asking gets the spy at ZERO.
	// (2) Textual backing: loop.go contains EXACTLY ONE dispatcher-call
	// expression, it sits after the `d.Policy != nil` guard, and `runTool` is
	// the only route from a tool-use block to it (RunLoop and RecoverStep's
	// CaseC both enter through runTool). The early returns on a refusal AND
	// on an ask (S11 — the asked call suspends or denies, never dispatches)
	// are the only paths that skip the call, so the call is reached iff the
	// verdict allowed — a refactor cannot "move the check into a branch
	// nobody takes" without destroying the one expression this pins.
	spy := newSpyTools("read")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "read", `{"path":"a"}`),
			toolUse("t2", "read", `{"path":"b"}`),
		}},
		scriptedTurn{ToolUses: []model.Block{toolUse("t3", "read", `{"path":"c"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	gate := denyGate("refused: agent:test-handler · no allow matched · default deny (PL5)")

	opts := runOpts(client, spy, nil)
	opts.Policy = gate
	RunStep(context.Background(), testRequest(toolSpec("read")), opts)

	if n := len(spy.recorded()); n != 0 {
		t.Fatalf("%d tool calls got through a denying gate: %+v", n, spy.recorded())
	}
	if reqs, _ := gate.log(); len(reqs) != 3 {
		t.Errorf("the gate was consulted %d times, want 3 — every dispatch attempt must pass the gate", len(reqs))
	}

	// S11 extended the behavioural half: an ASKING gate skips the dispatch the
	// same way a denying one does — an asked call never runs, whatever a
	// confused gate said about Allowed, and whether or not somebody can
	// answer. TestGateIsStructurallyBeforeDispatch used to claim "the refusal
	// early-return is the only path that skips the call"; the ask branch is
	// the second, so the claim is tested rather than asserted.
	askPolicy := askGate(testAskSentence, "confirm-push", "agent:boss")
	for _, attended := range []bool{false, true} {
		askSpy := newSpyTools("bash")
		askClient := newFakeClient(
			scriptedTurn{ToolUses: []model.Block{toolUse("a1", "bash", `{"command":"git push --force"}`)}},
			scriptedTurn{Text: okHandoff},
		)
		askOpts := runOpts(askClient, askSpy, nil)
		askOpts.Policy = askPolicy
		req := testRequest(toolSpec("bash"))
		req.HasOperator = attended
		res := RunStep(context.Background(), req, askOpts)
		if n := len(askSpy.recorded()); n != 0 {
			t.Errorf("%d tool calls got through an ASKING gate (attended=%v): %+v", n, attended, askSpy.recorded())
		}
		if attended && res.Kind != ResultSuspended {
			t.Errorf("attended ask did not suspend (kind %q) — the suspend path must still never dispatch", res.Kind)
		}
		if !attended && res.Kind == ResultSuspended {
			t.Errorf("operator-less ask suspended — it must deny on the spot")
		}
	}

	src := packageSources(t)["loop.go"]
	rep, err := auditDispatchPin(src)
	if err != nil {
		t.Errorf("the dispatch pin on loop.go: %v — %s", err, rep)
	}
}

// ── the dispatch pin, identifier-based (T9 review, DEF-033) ─────────────────
//
// T9's pin was textual: count the occurrences of "Tools.Call(" and compare
// offsets. The reviewer's plant proved what it cannot see: `call :=
// d.Tools.Call` is a method VALUE — it contains no "Tools.Call(" — so a
// second route to the dispatcher built that way (with the original call left
// in place to keep the count honest) sailed through a textual pin. This audit
// parses loop.go and walks it: EVERY selector expression `.Tools.Call` is a
// reference, whether called, bound to a variable, passed, or returned, and
// exactly one may exist, directly called, inside runTool, after the gate
// guard. TestDispatchPinRejectsMethodValue is the plant that proves the
// checker is load-bearing against the bug it was written for.

type dispatchRef struct {
	pos        token.Pos
	Pos        token.Position
	Called     bool // appears as the callee of a call expression directly
	InRunTool  bool // lexically inside func runTool
	AfterGuard bool // after the `d.Policy != nil` guard statement
}

func (r dispatchRef) String() string {
	return fmt.Sprintf("%s(called=%v inRunTool=%v afterGuard=%v)", r.Pos, r.Called, r.InRunTool, r.AfterGuard)
}

// auditDispatchPin parses src (the loop.go text) and enforces: exactly one
// `.Tools.Call` reference; it is directly called; it sits inside runTool;
// and it sits after the Policy guard. A nil error means the structural claim
// holds: the dispatcher is unreachable without an allowing verdict.
func auditDispatchPin(src string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "loop.go", src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	// Locate runTool and the guard inside it.
	var runTool *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "runTool" {
			runTool = fd
		}
	}
	if runTool == nil {
		return "", fmt.Errorf("no runTool function — the route the pin guards has moved; re-read the file")
	}
	guardPos := token.NoPos
	ast.Inspect(runTool, func(n ast.Node) bool {
		if ifSt, ok := n.(*ast.IfStmt); ok {
			if be, ok := ifSt.Cond.(*ast.BinaryExpr); ok {
				if sel, ok := be.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Policy" {
					if id, ok := be.Y.(*ast.Ident); ok && id.Name == "nil" {
						guardPos = ifSt.Pos()
					}
				}
			}
		}
		return true
	})
	if !guardPos.IsValid() {
		return "", fmt.Errorf("no `…Policy != nil` guard inside runTool — enforcement has moved or vanished")
	}

	// Collect DIRECTLY-CALLED references first (CallExpr whose Fun is the
	// selector), then every reference; a reference that is not one of the
	// called ones is a method value / binding / alias — the defect.
	type key struct{ pos token.Pos }
	called := map[key]bool{}
	var refs []dispatchRef
	isToolsCall := func(sel *ast.SelectorExpr) bool {
		if sel == nil || sel.Sel.Name != "Call" {
			return false
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		return ok && inner.Sel.Name == "Tools"
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isToolsCall(sel) {
				called[key{sel.Pos()}] = true
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && isToolsCall(sel) {
			refs = append(refs, dispatchRef{
				pos: sel.Pos(), Pos: fset.Position(sel.Pos()),
			})
		}
		return true
	})
	for i := range refs {
		refs[i].Called = called[key{refs[i].pos}]
		refs[i].InRunTool = runTool.Pos() <= refs[i].pos && refs[i].pos <= runTool.End()
		refs[i].AfterGuard = guardPos.IsValid() && guardPos < refs[i].pos
	}

	report := func() string {
		parts := make([]string, 0, len(refs))
		for _, r := range refs {
			parts = append(parts, r.String())
		}
		return strings.Join(parts, "; ")
	}
	if len(refs) != 1 {
		return report(), fmt.Errorf("%d references to .Tools.Call, want exactly 1 — a method value, alias, or "+
			"second route is a second place the gate must guard", len(refs))
	}
	r := refs[0]
	if !r.Called {
		return report(), fmt.Errorf("the single .Tools.Call reference is NOT a direct call — it is a method " +
			"value or binding, which can be invoked from anywhere the gate cannot see")
	}
	if !r.InRunTool {
		return report(), fmt.Errorf("the dispatcher call is outside runTool — runTool must be the only route " +
			"from a tool-use block to the dispatcher")
	}
	if !r.AfterGuard {
		return report(), fmt.Errorf("the dispatcher call precedes the Policy != nil guard — dispatch would be " +
			"reachable on refusal")
	}
	return report(), nil
}

func TestDispatchPinRejectsMethodValue(t *testing.T) {
	// The plant (DEF-033): a loop.go variant where the dispatch also happens
	// through a method VALUE. The original call is left in place — a textual
	// "Tools.Call(" count of 1 still passes this file — so the ONLY way to
	// catch it is knowing that `d.Tools.Call` appearing anywhere that is not
	// a direct call is itself the second route. If this test ever passes
	// against a checker that got weaker, the checker certifies nothing.
	plant := `package runtime

func runTool() {
	if d.Policy != nil {
		dec := d.Policy.Decide(req, at)
		if !dec.Allowed {
			return
		}
	}
	call := d.Tools.Call
	res := call(ctx, use.Name, use.Input)
	res = d.Tools.Call(ctx, use.Name, use.Input)
	_ = res
}
`
	if _, err := auditDispatchPin(plant); err == nil {
		t.Fatal("the audit PASSED a dispatch routed through a method value — the exact defect (DEF-033) " +
			"this checker exists to catch; a textual grep would have passed it too")
	}
	// And the shape the audit is NOT supposed to reject: the single direct
	// call, after the guard. A checker that always errors is as dead as one
	// that never does.
	clean := `package runtime

func runTool() {
	if d.Policy != nil {
		dec := d.Policy.Decide(req, at)
		if !dec.Allowed {
			return
		}
	}
	res := d.Tools.Call(ctx, use.Name, use.Input)
	_ = res
}
`
	if _, err := auditDispatchPin(clean); err != nil {
		t.Fatalf("the audit rejected the clean shape: %v", err)
	}
}

// ── DEF-035: the instant is snapshotted once on the RESUME paths too ────────

func TestResumeStepDecidesAtOneInstant(t *testing.T) {
	// T9 proved the rule for RunStep; DEF-035 names the gap: resume and
	// recovery snapshot their OWN instant (resume.go, recover.go) and were
	// untested. Resume is a step start for policy purposes — a step that
	// suspended overnight is decided at one instant, the resume's, and the
	// clock TICKS between calls so a per-call read cannot pass.
	var ticks int64
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fixed := func() time.Time { return base.Add(time.Duration(atomic.AddInt64(&ticks, 1)) * time.Second) }

	gate := allowGate("allowed")
	opts := runOpts(
		newFakeClient(
			scriptedTurn{ToolUses: []model.Block{
				toolUse("t1", "read", `{"path":"a"}`),
				toolUse("t2", "read", `{"path":"b"}`),
			}},
			scriptedTurn{Text: okHandoff},
		),
		newSpyTools("read"), nil,
	)
	opts.Policy = gate
	opts.Clock = fixed
	req := policyStepRequest(11, toolSpec("read"))
	ResumeStep(context.Background(), ResumeRequest{
		Request: req,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
			{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("working")}},
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("continue")}},
		},
		Manifest: req.Manifest,
		Recovery: true,
	}, opts)

	_, ats := gate.log()
	if len(ats) != 2 {
		t.Fatalf("the gate saw %d calls, want 2", len(ats))
	}
	if !ats[0].Equal(ats[1]) {
		t.Errorf("the resumed step decided its calls at %s and %s — ResumeStep must snapshot the instant "+
			"ONCE at resume start (DEF-035)", ats[0], ats[1])
	}
	if atomic.LoadInt64(&ticks) <= 1 {
		t.Error("the clock never advanced — this test would pass against a frozen clock and prove nothing")
	}
}

func TestRecoverStepReRunsDecideAtOneInstant(t *testing.T) {
	// The recovery half of DEF-035: CaseC re-runs enter through the CaseC
	// block (recover.go), which snapshots its own instant BEFORE re-running
	// any tool — two re-runs, one instant, ticking clock.
	var ticks int64
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fixed := func() time.Time { return base.Add(time.Duration(atomic.AddInt64(&ticks, 1)) * time.Second) }

	gate := allowGate("allowed")
	opts := runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), newSpyTools("bash", "read"), nil)
	opts.Policy = gate
	opts.Clock = fixed
	req := policyStepRequest(12, toolSpec("bash"))
	RecoverStep(context.Background(), ResumeRequest{
		Request: req,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
			{Role: model.RoleAssistant, Content: []model.Block{
				toolUse("u1", "bash", `{"command":"ls"}`),
				toolUse("u2", "read", `{"path":"a"}`),
			}},
		},
		Manifest: req.Manifest,
		Recovery: true,
	}, RecoveryPlan{Case: CaseC, Calls: []CallPlan{
		{ToolUseID: "u1", Tool: "bash", Action: ReRun},
		{ToolUseID: "u2", Tool: "read", Action: ReRun},
	}}, opts)

	_, ats := gate.log()
	if len(ats) != 2 {
		t.Fatalf("the gate saw %d re-run calls, want 2", len(ats))
	}
	if !ats[0].Equal(ats[1]) {
		t.Errorf("recovery re-runs were decided at %s and %s — recover.go snapshots policyAt once before "+
			"the re-run loop (DEF-035)", ats[0], ats[1])
	}
	if atomic.LoadInt64(&ticks) <= 1 {
		t.Error("the clock never advanced — the test proves nothing about a per-call read")
	}
}

func TestRefusalReachingTheModelCarriesTheSentence(t *testing.T) {
	// Not a paraphrase — BYTE-EQUAL to T4's rendering of the same fixture.
	// A refusal the model cannot interpret becomes a retry loop, and the
	// retries are what fill policy_event (the seed's argument).
	fixtureAt := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	p := policy.Policy{
		ID:     "no-bash",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	expected := policy.Evaluate([]policy.Policy{p}, policy.Request{
		Type: policy.ToolAccess, Agent: "test-handler", Tool: "bash",
	}, fixtureAt).Sentence()
	if !strings.HasPrefix(expected, "refused: agent:test-handler") {
		t.Fatalf("the fixture does not render a refusal of this step's subject: %q", expected)
	}

	spy := newSpyTools("bash")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"ls"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	opts := runOpts(client, spy, sink)
	opts.Policy = engineGate([]policy.Policy{p})
	opts.Clock = func() time.Time { return fixtureAt }
	req := testRequest(toolSpec("bash"))
	// The step's deadline was cut against the real clock; this step lives at
	// fixtureAt, so the deadline is removed rather than shifted — a zero
	// deadline is unchecked (loop.go), which is the honest fixture here.
	req.Manifest.DeadlineAt = time.Time{}
	res := RunStep(context.Background(), req, opts)

	blk, ok := toolResultFor(res.Messages, "t1")
	if !ok {
		t.Fatal("the denied call was never answered")
	}
	if blk.Content != expected {
		t.Errorf("the model was handed a paraphrase:\n got: %q\nwant: %q", blk.Content, expected)
	}
	if !blk.IsError {
		t.Error("the refusal reached the model with IsError false")
	}
	var sawEvent bool
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "bash" {
			sawEvent = true
			if e.Result != expected || !e.IsError {
				t.Errorf("the refusal event carries %q/%v, want the sentence byte-equal and true", e.Result, e.IsError)
			}
		}
	}
	if !sawEvent {
		t.Error("no step.tool.end event carried the refusal")
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the refused tool ran %d time(s)", n)
	}
}

func TestAtIsTakenOncePerStepNotPerToolCall(t *testing.T) {
	// Two tool calls, one instant. The clock TICKS between calls (each read
	// advances it a second), so this cannot pass against a per-call clock read
	// — which is the bug: two identical calls straddling an expiry would show
	// one allowed and one refused with nothing to explain it.
	var ticks int64
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fixed := func() time.Time { return base.Add(time.Duration(atomic.AddInt64(&ticks, 1)) * time.Second) }

	gate := allowGate("allowed")
	opts := runOpts(
		newFakeClient(
			scriptedTurn{ToolUses: []model.Block{
				toolUse("t1", "read", `{"path":"a"}`),
				toolUse("t2", "read", `{"path":"b"}`),
			}},
			scriptedTurn{Text: okHandoff},
		),
		newSpyTools("read"), nil,
	)
	opts.Policy = gate
	opts.Clock = fixed
	req := testRequest(toolSpec("read"))
	// Same as the sentence test: the step lives on the injected clock, so the
	// deadline cut against the real clock is removed (a zero deadline is
	// unchecked), or the step would block before its first model call.
	req.Manifest.DeadlineAt = time.Time{}
	RunStep(context.Background(), req, opts)

	_, ats := gate.log()
	if len(ats) != 2 {
		t.Fatalf("the gate saw %d calls, want 2", len(ats))
	}
	if !ats[0].Equal(ats[1]) {
		t.Errorf("the two tool calls were decided at %s and %s — `at` is snapshotted ONCE at step "+
			"start, not read per tool call", ats[0], ats[1])
	}
	if atomic.LoadInt64(&ticks) <= 1 {
		t.Error("the clock never advanced — the test would pass against a frozen clock and prove nothing")
	}
}

func TestTheGateTakesNoClockAndSetsNoAttendedness(t *testing.T) {
	// The grep-shaped invariants of the gate file: `time.Now` has no business
	// in policy_gate.go (the instant is a parameter — D5), and attendedness is
	// READ, never SET (PL14/G14: G14 pins it at loop start; the gate reads the
	// world and never writes it). There is no attendedness field in the
	// runtime as of S10 — and this asserts this slice did not invent a setter
	// for one, in the gate file or anywhere else in the package.
	sources := packageSources(t)
	gate, ok := sources["policy_gate.go"]
	if !ok {
		t.Fatal("policy_gate.go is missing — the whole task lives in this file")
	}
	if strings.Contains(gate, "time.Now") {
		t.Error("policy_gate.go reads the clock; `at` arrives as a parameter (D5), and a per-call read " +
			"straddles an expiry exactly as the seed warned")
	}
	for name, src := range sources {
		if strings.Contains(src, "ttended") {
			t.Errorf("%s mentions attendedness — the gate reads it and never sets it (PL14); if a later "+
				"slice adds the field, its setter belongs where G14 pins it (loop start), not in the gate", name)
		}
	}
}

func TestPolicyVerdictIsOnTheStreamForEveryJudgedCall(t *testing.T) {
	// The chat shows "policy allowed write_file" — an allow that never reached
	// the stream would read as "policy was not consulted" (P31's two-state lie).
	cases := []struct {
		name    string
		gate    *scriptedGate
		verdict string
	}{
		{"allowed", allowGate(`allowed: agent:test-handler · "yes-read"`), "allowed"},
		{"denied", denyGate(`refused: agent:test-handler · "no-read"`), "denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newFakeClient(
				scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"x"}`)}},
				scriptedTurn{Text: okHandoff},
			)
			sink := &recordingSink{}
			opts := runOpts(client, newSpyTools("read"), sink)
			opts.Policy = tc.gate
			RunStep(context.Background(), testRequest(toolSpec("read")), opts)

			var decided []StepEvent
			for _, e := range sink.all() {
				if e.Kind == EventPolicyDecided {
					decided = append(decided, e)
				}
			}
			if len(decided) != 1 {
				t.Fatalf("%d step.policy.decided events, want exactly 1", len(decided))
			}
			got := decided[0]
			if got.Verdict != tc.verdict || got.Tool != "read" || got.ToolUseID != "t1" {
				t.Errorf("decided = {verdict:%q tool:%q toolUseId:%q}, want {%q read t1}",
					got.Verdict, got.Tool, got.ToolUseID, tc.verdict)
			}
			if got.Reason == "" {
				t.Error("the verdict sentence is missing from Reason")
			}
		})
	}
}

func TestNoPolicyEventWithoutAGate(t *testing.T) {
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	RunStep(context.Background(), testRequest(toolSpec("read")), runOpts(client, newSpyTools("read"), sink))
	for _, e := range sink.all() {
		if e.Kind == EventPolicyDecided {
			t.Fatal("a step with no policy gate published a verdict nobody reached")
		}
	}
}

func TestToolStartAndEndCarryTheSameToolUseID(t *testing.T) {
	// Two calls of one tool in one turn: only the id tells a watcher which end
	// closes which start.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "read", `{"path":"a"}`),
			toolUse("t2", "read", `{"path":"b"}`),
		}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	RunStep(context.Background(), testRequest(toolSpec("read")), runOpts(client, newSpyTools("read"), sink))
	var starts, ends []string
	for _, e := range sink.all() {
		switch e.Kind {
		case EventToolStart:
			starts = append(starts, e.ToolUseID)
		case EventToolEnd:
			ends = append(ends, e.ToolUseID)
		}
	}
	if strings.Join(starts, ",") != "t1,t2" || strings.Join(ends, ",") != "t1,t2" {
		t.Errorf("starts=%v ends=%v, want both [t1 t2]", starts, ends)
	}
}

func TestBashCarriesItsWholeCommandAndCappedOutput(t *testing.T) {
	// The summary cuts a command at 60 runes; the chat's command box needs
	// all of it, and the output that came back — bash only, capped.
	long := "grep -rn 'func Test' internal/handler/registry_test.go internal/handler/registry.go | head -20"
	input, _ := json.Marshal(map[string]string{"command": long})
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("b1", "bash", string(input)), toolUse("r1", "read", `{"path":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	RunStep(context.Background(), testRequest(toolSpec("bash"), toolSpec("read")),
		runOpts(client, newSpyTools("bash", "read"), sink))
	for _, e := range sink.all() {
		switch {
		case e.Kind == EventToolStart && e.Tool == "bash":
			if e.Command != long {
				t.Errorf("bash start command = %q, want the whole command", e.Command)
			}
		case e.Kind == EventToolEnd && e.Tool == "bash":
			if e.Output == "" {
				t.Error("bash end carries no output")
			}
		case e.Tool == "read" && (e.Command != "" || e.Output != ""):
			t.Errorf("read leaked content onto the stream: command=%q output=%q", e.Command, e.Output)
		}
	}
}

func TestStreamCapsCutOnRuneBoundaries(t *testing.T) {
	s := strings.Repeat("é", 3000) // 2 bytes each
	out, cut := capBytes(s, streamOutputCap)
	if !cut || len(out) > streamOutputCap || !utf8.ValidString(out) {
		t.Errorf("capBytes: cut=%v len=%d valid=%v", cut, len(out), utf8.ValidString(out))
	}
	if same, cut := capBytes("short", streamOutputCap); cut || same != "short" {
		t.Errorf("capBytes cut a short string: %q %v", same, cut)
	}
	if got := capRunes(strings.Repeat("x", streamCommandCap+5), streamCommandCap); utf8.RuneCountInString(got) != streamCommandCap+1 {
		t.Errorf("capRunes kept %d runes, want %d plus the ellipsis", utf8.RuneCountInString(got), streamCommandCap)
	}
}
