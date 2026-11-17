package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// T8's tests. Three of these are the slice's load-bearing properties.

func TestToolsExecuteSeriallyInEmissionOrder(t *testing.T) {
	// D3, and the spy is the point.
	//
	// It records ENTRY AND EXIT and asserts no two intervals overlap. A spy
	// recording only call ORDER passes against an `errgroup.Wait()`
	// implementation — every tool is still called, and still in order — so
	// order alone cannot see concurrency.
	//
	// Serial execution is what makes transcripts reproducible and stops two
	// tools writing one file in an order nobody chose.
	spy := newSpyTools("read")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "read", `{"path":"a.md"}`),
			toolUse("t2", "read", `{"path":"b.md"}`),
			toolUse("t3", "read", `{"path":"c.md"}`),
		}},
		scriptedTurn{Text: okHandoff},
	)

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}

	calls := spy.recorded()
	if len(calls) != 3 {
		t.Fatalf("%d tools ran, want 3", len(calls))
	}

	// Emission order, asserted separately from serialness so a failure says
	// WHICH property broke.
	wantOrder := []string{`{"path":"a.md"}`, `{"path":"b.md"}`, `{"path":"c.md"}`}
	for i, call := range calls {
		if call.Input != wantOrder[i] {
			t.Errorf("call %d was %s, want %s — tools must run in EMISSION order", i, call.Input, wantOrder[i])
		}
	}

	if a, b, found := spy.overlaps(); found {
		t.Errorf("two tools overlapped: %s [%s..%s] and %s [%s..%s]. Tools execute serially (K9) — a "+
			"parallel loop produces transcripts nobody can reproduce and file writes in an order "+
			"nobody chose",
			a.Name, a.Enter.Format(time.StampMicro), a.Exit.Format(time.StampMicro),
			b.Name, b.Enter.Format(time.StampMicro), b.Exit.Format(time.StampMicro))
	}
}

func TestTheOverlapDetectorWouldCatchConcurrency(t *testing.T) {
	// The negative control on the spy itself. Without this, a broken
	// `overlaps()` would make the test above pass against a parallel loop —
	// which is exactly the failure D3 is written to prevent, one level up.
	spy := newSpyTools("read")
	now := time.Now()
	spy.calls = []toolCall{
		{Name: "a", Enter: now, Exit: now.Add(10 * time.Millisecond)},
		{Name: "b", Enter: now.Add(5 * time.Millisecond), Exit: now.Add(15 * time.Millisecond)},
	}
	if _, _, found := spy.overlaps(); !found {
		t.Fatal("the overlap detector did not notice two clearly overlapping intervals, so the " +
			"serial-execution test proves nothing")
	}

	// And it must not report an overlap for genuinely sequential calls.
	spy.calls = []toolCall{
		{Name: "a", Enter: now, Exit: now.Add(5 * time.Millisecond)},
		{Name: "b", Enter: now.Add(6 * time.Millisecond), Exit: now.Add(10 * time.Millisecond)},
	}
	if a, b, found := spy.overlaps(); found {
		t.Fatalf("the detector reported an overlap for sequential calls: %+v %+v", a, b)
	}
}

func TestAnInvalidHandoffRetriesOnceWithNoToolsInTheRequest(t *testing.T) {
	// D4, and **the assertion is on the REQUEST OBJECT**.
	//
	// Not that a retry happened — that a retry offered NO TOOLS. A retry that
	// can call tools can start new work: a step that already did its job doing
	// it again, differently, while ostensibly just restating its report. A
	// retry offered tools it happens not to use is still wrong, and a test
	// counting attempts cannot see it.
	client := newFakeClient(
		scriptedTurn{Text: "I am not going to produce JSON."},
		scriptedTurn{Text: okHandoff},
	)

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})

	if res.Outcome != OutcomeOK {
		t.Fatalf("the retry did not recover: outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if client.requestCount() != 2 {
		t.Fatalf("%d requests were made, want exactly 2 — one retry, no more (K4)", client.requestCount())
	}

	first := client.request(0)
	if len(first.Tools) == 0 {
		t.Fatal("the FIRST request carried no tools, so this test is not checking what it says")
	}

	retry := client.request(1)
	if retry.Tools != nil {
		t.Errorf("THE RETRY CARRIED %d TOOL(S): %v.\n"+
			"The retry must carry no tools at all — not an empty slice, nil, so the field is absent "+
			"from the wire. A retry that can call tools can start new work, and a step that already "+
			"did its job could do it again, differently, while ostensibly restating its report",
			len(retry.Tools), toolNames(retry.Tools))
	}

	// And the retry told the model WHY, because a model told "that was not
	// valid JSON" produces another invalid object.
	lastUser := lastUserText(retry.Messages)
	if !strings.Contains(lastUser, "did not contain a usable handoff") {
		t.Errorf("the retry did not carry the validation error: %q", lastUser)
	}
	if !strings.Contains(lastUser, "Do not call any tools") {
		t.Errorf("the retry did not tell the model the work was done: %q", lastUser)
	}
}

func TestASecondHandoffFailureYieldsFailedWithTheTranscriptIntact(t *testing.T) {
	// A model that cannot produce valid JSON twice will not produce it on the
	// fifth attempt, and each attempt costs money. The transcript and the
	// partial usage survive, because the step spent that money (M15).
	client := newFakeClient(
		scriptedTurn{Text: "no JSON here", Usage: model.Usage{InputTokens: model.IntPtr(100)}},
		scriptedTurn{Text: "still no JSON", Usage: model.Usage{InputTokens: model.IntPtr(50)}},
	)

	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome %q, want failed", res.Outcome)
	}
	if client.requestCount() != 2 {
		t.Errorf("%d requests, want 2 — the retry is exactly one", client.requestCount())
	}
	if len(res.Messages) < 4 {
		t.Errorf("the transcript has %d messages; it must survive a failure, because it is the "+
			"record of what was spent", len(res.Messages))
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 150 {
		t.Errorf("usage is %+v; both turns' tokens were bought and must both be counted", res.Usage)
	}
	if !strings.Contains(res.Reason, "twice") {
		t.Errorf("the reason does not say the model failed twice: %q", res.Reason)
	}
}

func TestExceedingMaxIterationsYieldsBlockedNamingTheBoundAndItsValue(t *testing.T) {
	// D5 and O2. A caller seeing `failed` files a bug report; one seeing
	// `blocked` raises the budget — so the outcome and the reason both matter.
	spy := newSpyTools("read")
	spy.delay = 0
	// A model that calls a tool forever.
	turns := make([]scriptedTurn, 10)
	for i := range turns {
		turns[i] = scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{"path":"a"}`)}}
	}
	client := newFakeClient(turns...)

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	st.Manifest.MaxIterations = 3

	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome %q, want blocked — the work did not fail, it ran out of room", res.Outcome)
	}
	if !strings.Contains(res.Reason, "MaxIterations") {
		t.Errorf("the reason does not name the bound: %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "3") {
		t.Errorf("the reason does not name the bound's VALUE: %q. \"Blocked\" alone tells a caller to "+
			"raise something without saying what", res.Reason)
	}
	if res.Manifest.IterationsUsed != 3 {
		t.Errorf("iterations used %d, want exactly the bound", res.Manifest.IterationsUsed)
	}
}

func TestExceedingTheDeadlineYieldsBlockedNamingTheBound(t *testing.T) {
	// The other bound. Checked against an INJECTED clock, so the test does not
	// wait thirty minutes.
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	st := testState(testHandler(), nil)
	st.Manifest.DeadlineAt = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools(),
		// An hour past the deadline.
		Clock:       func() time.Time { return time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC) },
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome %q, want blocked", res.Outcome)
	}
	if !strings.Contains(res.Reason, "DeadlineAt") {
		t.Errorf("the reason does not name the bound: %q", res.Reason)
	}
	if client.requestCount() != 0 {
		t.Errorf("%d requests were made past the deadline; the bound is checked BEFORE the call, "+
			"because the call is the expensive part", client.requestCount())
	}
}

func TestAllFourOutcomesAreProduced(t *testing.T) {
	// One case each, so every branch of the union is exercised — and so a
	// fifth outcome appearing would have nowhere to go.
	cases := []struct {
		name  string
		turns []scriptedTurn
		setup func(*LoopState)
		want  Outcome
	}{
		{
			name:  "ok",
			turns: []scriptedTurn{{Text: "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```"}},
			want:  OutcomeOK,
		},
		{
			name:  "reject",
			turns: []scriptedTurn{{Text: "```json\n{\"status\":\"reject\",\"summary\":\"the answer is no\"}\n```"}},
			want:  OutcomeReject,
		},
		{
			name:  "blocked",
			turns: []scriptedTurn{{ToolUses: []model.Block{toolUse("t", "read", `{}`)}}},
			setup: func(st *LoopState) { st.Manifest.MaxIterations = 1 },
			want:  OutcomeBlocked,
		},
		{
			name:  "failed",
			turns: []scriptedTurn{{Text: "no json"}, {Text: "still none"}},
			want:  OutcomeFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpyTools("read")
			spy.delay = 0
			st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
			if tc.setup != nil {
				tc.setup(st)
			}
			res := RunLoop(context.Background(), st, LoopDeps{
				Client: newFakeClient(tc.turns...), Tools: spy,
				HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
			})
			if res.Outcome != tc.want {
				t.Errorf("outcome %q, want %q (reason: %s)", res.Outcome, tc.want, res.Reason)
			}
			// Every outcome is a COMPLETE result at S5 — never suspended.
			if res.Kind != ResultComplete {
				t.Errorf("kind %q, want complete", res.Kind)
			}
		})
	}
}

func TestARejectIsNotAFailure(t *testing.T) {
	// A step that ran correctly and whose answer is "no" is a SUCCESSFUL step.
	// Conflating the two makes a correct refusal look like a defect, and the
	// person raises a bug instead of reading the summary.
	client := newFakeClient(scriptedTurn{
		Text: "```json\n{\"status\":\"reject\",\"summary\":\"the README does cover testing\"}\n```"})
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res.Outcome != OutcomeReject {
		t.Fatalf("outcome %q", res.Outcome)
	}
	if res.Reason != "" {
		t.Errorf("a reject carries a reason (%q); the summary is in the handoff, and a reason here "+
			"would read as a failure", res.Reason)
	}
	if res.Handoff == nil {
		t.Fatal("a reject has no handoff; the report is the whole point of the outcome")
	}
}

func TestAnUnparseableToolArgumentBecomesAToolErrorAndTheLoopContinues(t *testing.T) {
	// Q6. S3 reports tool calls whose ARGUMENTS would not parse rather than
	// inventing a block for them, because only this layer knows what a tool
	// result looks like in a transcript. So the debt is paid here — and a model
	// emitting bad arguments is NORMAL, so the loop continues.
	client := newFakeClient(
		scriptedTurn{OwedToolErrors: []string{"call-1"}},
		scriptedTurn{Text: okHandoff},
	)
	spy := newSpyTools("read")

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q — an unparseable argument must not fail the step", res.Outcome, res.Reason)
	}
	if len(spy.recorded()) != 0 {
		t.Errorf("a tool ran for an unparseable call: %+v", spy.recorded())
	}

	// The transcript holds an IsError tool_result, so every tool_use is
	// answered and the transcript stays valid.
	found := false
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "call-1" {
				found = true
				if !b.IsError {
					t.Error("the owed tool result is not marked IsError")
				}
				if !strings.Contains(b.Content, "valid JSON") {
					t.Errorf("the result does not say what was wrong: %q", b.Content)
				}
			}
		}
	}
	if !found {
		t.Error("no tool_result was written for the unparseable call, so its tool_use is unanswered " +
			"and the transcript is invalid")
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the transcript is invalid: %s", v.Reason)
	}
}

func TestAToolTheStepLacksBecomesAnErrorNamingWhatIsAvailable(t *testing.T) {
	// O6. Not a step failure — the model can recover from being told — and not
	// silence, which it cannot.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"ls"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	// The spy reports only `read` as granted, and returns the refusal the real
	// toolset would.
	spy := newSpyTools("read")
	spy.with("bash", ToolOutcome{
		IsError: true,
		Content: "there is no tool called \"bash\" in this step. The tools you have are: read.",
	})

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q — asking for a missing tool must not fail the step", res.Outcome)
	}
	var content string
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.IsError {
				content = b.Content
			}
		}
	}
	if !strings.Contains(content, "read") {
		t.Errorf("the refusal does not name what IS available: %q", content)
	}
}

func TestTheLoopHonoursContextCancellationPromptly(t *testing.T) {
	spy := newSpyTools("read")
	spy.delay = 0
	turns := make([]scriptedTurn, 20)
	for i := range turns {
		turns[i] = scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{}`)}}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before it starts

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	done := make(chan StepResult, 1)
	go func() {
		done <- RunLoop(ctx, st, LoopDeps{
			Client: newFakeClient(turns...), Tools: spy,
			HandoffSpec: HandoffSpec{Required: []string{"status"}},
		})
	}()

	select {
	case res := <-done:
		if res.Outcome != OutcomeFailed {
			t.Errorf("outcome %q, want failed — a cancelled step did not run out of room, something "+
				"stopped it", res.Outcome)
		}
		if !strings.Contains(res.Reason, "cancelled") {
			t.Errorf("the reason does not say it was cancelled: %q", res.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled loop did not return within two seconds")
	}
}

func TestIterationsAreCountedOnTheManifestNotInALocal(t *testing.T) {
	// T8's point 3. S6 persists the count across a suspension, and it can only
	// do that if the loop increments the manifest it was given.
	spy := newSpyTools("read")
	spy.delay = 0
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{}`)}},
		scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{}`)}},
		scriptedTurn{Text: okHandoff},
	)

	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	manifest := st.Manifest // the same pointer the loop was given

	RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if manifest.IterationsUsed != 3 {
		t.Errorf("the caller's manifest says %d iterations; the loop must increment the manifest it "+
			"was given, so S6 can persist the count across a suspension", manifest.IterationsUsed)
	}
}

func TestNothingProducesSuspended(t *testing.T) {
	// D14 / K21, by exhausting the fake client's scripts.
	//
	// Every shape of turn: text, a tool call, a failure, an unparseable
	// argument, a bound. None may produce `suspended` — S6 adds that path, and
	// shipping the variant unused means S6 fills it in rather than reshaping a
	// type every switch depends on.
	scripts := [][]scriptedTurn{
		{{Text: okHandoff}},
		{{Text: "no json"}, {Text: "none"}},
		{{ToolUses: []model.Block{toolUse("t", "read", `{}`)}}, {Text: okHandoff}},
		{{Err: context.DeadlineExceeded}},
		{{OwedToolErrors: []string{"x"}}, {Text: okHandoff}},
		{{Text: "```json\n{\"status\":\"reject\",\"summary\":\"no\"}\n```"}},
		{{Text: "```json\n{\"status\":\"blocked\",\"summary\":\"could not\"}\n```"}},
	}

	for i, script := range scripts {
		spy := newSpyTools("read")
		spy.delay = 0
		st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
		st.Manifest.MaxIterations = 2
		res := RunLoop(context.Background(), st, LoopDeps{
			Client: newFakeClient(script...), Tools: spy,
			HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
		})
		if res.Kind == ResultSuspended {
			t.Errorf("script %d produced a suspended result; nothing in S5 may (K21)", i)
		}
		if res.Question != nil {
			t.Errorf("script %d populated Question; S6 fills that field", i)
		}
	}
}

func TestAProviderFailureKeepsThePartialAccounting(t *testing.T) {
	// M15: a cancelled or failed call still spent what it spent, and losing
	// the accounting loses the spend.
	client := newFakeClient(scriptedTurn{
		Text:  "partial answer",
		Usage: model.Usage{InputTokens: model.IntPtr(400), OutputTokens: model.IntPtr(20)},
		Cost:  model.Cost{State: model.CostPartial, USDAtLeast: 0.004, Missing: []string{"cacheWriteUsdPerMTok"}},
		Err:   context.Canceled,
	})

	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})

	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome %q", res.Outcome)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 400 {
		t.Errorf("usage was lost: %+v", res.Usage)
	}
	if res.Cost.State != model.CostPartial {
		t.Errorf("cost state %q, want partial — the floor was measured and must survive", res.Cost.State)
	}
	if len(res.Cost.Missing) == 0 {
		t.Error("the partial cost lost its Missing list")
	}
}

func TestThereIsExactlyOneLoopImplementation(t *testing.T) {
	// I4. S6 and S7 build on this loop, and a second copy would drift — and
	// drift is what breaks resume: a resumed step running a slightly different
	// loop produces a transcript the original could not have produced.
	sources := packageSources(t)
	var withLoop []string
	for name, src := range sources {
		if strings.Contains(src, "func RunLoop(") {
			withLoop = append(withLoop, name)
		}
	}
	if len(withLoop) != 1 {
		t.Errorf("RunLoop is defined in %v; there must be exactly one loop", withLoop)
	}
}

// --- helpers -----------------------------------------------------------------

func toolNames(specs []model.ToolSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func lastUserText(messages []model.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != model.RoleUser {
			continue
		}
		var b strings.Builder
		for _, blk := range messages[i].Content {
			if blk.Kind == model.BlockText {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}

// --- S11 T12: the ask suspension ---------------------------------------------
//
// The gate can answer `Asked`. These tests are the honest half: a step that
// somebody CAN answer suspends exactly like the question tool (P2 — exiting,
// no process, the blocked tool_use unanswered for the answer to resume); a
// step nobody can be vouched for answering is denied NOW, verbatim sentence,
// and never grows a question (the adapter creates question rows only for
// suspended results — the runtime-level assertion here is that no
// PendingQuestion escapes at all). And no ask is ever answered from a cache
// (QAP-13): every identical call re-asks the gate and re-suspends.

// askGate answers the bash command question with an ask of the named rule
// and everything else with an allow — the shape the production gate returns
// for a matching command_access ask rule (daemon/policy_gate.go's Asked
// half, mirrored in plain strings).
func askGate(sentence, ruleID, ruleSubject string) *scriptedGate {
	return &scriptedGate{decide: func(req PolicyGateRequest, at time.Time) PolicyGateDecision {
		if req.Tool != "bash" {
			return PolicyGateDecision{Allowed: true, Sentence: "allowed: test"}
		}
		return PolicyGateDecision{
			Allowed: false, Asked: true, Sentence: sentence,
			RuleID: ruleID, RuleSubject: ruleSubject, Command: req.Command,
		}
	}}
}

const testAskSentence = `asked: agent:test-handler · "confirm-push" · target agent:boss · source operator · no expiry`
const testAskCommand = `git push --force`

func TestAskSuspendsTheStep(t *testing.T) {
	// Attended (HasOperator true): the ask suspends like S6's question tool,
	// the question names the three things a human needs (QAP-12 — rule,
	// agent, command), ToolUseID is the block's own id, and the dispatcher
	// is NEVER reached for the blocked call (spy at zero for bash).
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("read", "bash")
	client := newFakeClient(
		// The sibling read runs FIRST and its result is recorded — the
		// question-tool suspension shape, mirrored (see the S6 sibling test).
		scriptedTurn{ToolUses: []model.Block{
			toolUse("r1", "read", `{"path":"a.md"}`),
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
		}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(toolSpec("read"), toolSpec("bash"))
	req.HasOperator = true
	req.StepID = 7
	sink := &recordingSink{}

	opts := runOpts(client, spy, sink)
	opts.Policy = gate
	res := RunStep(context.Background(), req, opts)

	if res.Kind != ResultSuspended {
		t.Fatalf("kind %q (outcome %q, reason %q), want suspended — an attended ask must suspend like the question tool",
			res.Kind, res.Outcome, res.Reason)
	}
	q := res.Question
	if q == nil {
		t.Fatal("suspended without a question — the step waits on a question nobody was given")
	}
	if q.Kind != "permission" {
		t.Errorf("question kind %q, want permission (question.go's enum)", q.Kind)
	}
	if q.ToolUseID != "t1" {
		t.Errorf("question ToolUseID %q, want the blocked block's own id t1 — the answer resumes THAT call", q.ToolUseID)
	}
	if !reflect.DeepEqual(q.Options, PermissionOptions()) {
		t.Errorf("question options %v, want the five permission answers", q.Options)
	}
	// QAP-12 + SEC-051: the body names the deciding rule, the agent, and the
	// command — the command through strconv.Quote, and the verdict sentence
	// verbatim (the one formatter).
	for _, want := range []string{
		"confirm-push", "agent:boss", "test-handler",
		strconv.Quote(testAskCommand), testAskSentence,
	} {
		if !strings.Contains(q.Body, want) {
			t.Errorf("the question body is missing %q:\n%s", want, q.Body)
		}
	}

	// The blocked call never dispatched; the sibling did.
	calls := spy.recorded()
	if len(calls) != 1 || calls[0].Name != "read" {
		t.Errorf("tools ran: %+v — want only the sibling read; an asked call must not reach the dispatcher", calls)
	}

	// The gate was asked about both calls, and the bash question carried the
	// command PARSED FROM THE BLOCK INPUT by the runtime (T10's wiring duty).
	reqs, _ := gate.log()
	if len(reqs) != 2 {
		t.Fatalf("the gate was consulted %d times, want 2 (read then bash)", len(reqs))
	}
	if reqs[0].Tool != "read" || reqs[0].Command != "" {
		t.Errorf("the read's gate question carried Tool=%q Command=%q, want read and the empty command (only bash carries one)",
			reqs[0].Tool, reqs[0].Command)
	}
	bash := reqs[1]
	if bash.Tool != "bash" || bash.Command != testAskCommand {
		t.Errorf("the bash gate question carried Tool=%q Command=%q, want bash and %q — the command must reach the gate from the block's own input",
			bash.Tool, bash.Command, testAskCommand)
	}
	if bash.Agent != "test-handler" || bash.StepID != 7 || !bash.ModelRequested {
		t.Errorf("the bash gate question carries Agent=%q StepID=%d ModelRequested=%v, want test-handler/7/true (trusted fields)",
			bash.Agent, bash.StepID, bash.ModelRequested)
	}

	// The transcript is the question-tool shape: the sibling answered, the
	// blocked tool_use unanswered (the answer resumes it), still valid.
	if _, ok := toolResultFor(res.Messages, "r1"); !ok {
		t.Error("the sibling read's result was not recorded before the suspension")
	}
	if _, ok := toolResultFor(res.Messages, "t1"); ok {
		t.Error("the blocked call was answered at suspension — the ANSWER answers it, not the suspension")
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the suspended transcript is invalid: %s", v.Reason)
	}

	// The stream stops the spinner honestly (not an error, not a result).
	var sawEnd bool
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "bash" {
			sawEnd = true
			if e.IsError || !strings.Contains(e.Result, "suspended") {
				t.Errorf("the suspension's tool.end carries result=%q isError=%v, want a non-error suspension note", e.Result, e.IsError)
			}
		}
	}
	if !sawEnd {
		t.Error("no step.tool.end for the suspended call — watchers would spin on a tool that will never run")
	}
	if res.Manifest.IterationsUsed != 1 {
		t.Errorf("iterations %d, want 1 — the suspension costs exactly the turn it ran", res.Manifest.IterationsUsed)
	}
}

func TestUnattendedAskDenies(t *testing.T) {
	// No operator (HasOperator false — the same reading a never-declared loop
	// gets, because the adapter collapses NULL to false fail-closed): the ask
	// is answered by nobody, so it is a deny — the verdict sentence verbatim
	// as an IsError tool_result, the tool never runs, the loop continues, and
	// NO PendingQuestion escapes (the adapter creates question rows only for
	// suspended results; nil here is the runtime-level half of "no question
	// row", with the adapter-level half proven in internal/daemon).
	cases := []struct {
		name string
		set  func(*StepRequest)
	}{
		{"explicit-false", func(r *StepRequest) { r.HasOperator = false }},
		{"zero-value", func(r *StepRequest) {}}, // nobody said true = nobody may be assumed
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := askGate(testAskSentence, "confirm-push", "agent:boss")
			spy := newSpyTools("bash")
			client := newFakeClient(
				scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`)}},
				scriptedTurn{Text: okHandoff},
			)
			req := testRequest(toolSpec("bash"))
			tc.set(&req)
			sink := &recordingSink{}

			opts := runOpts(client, spy, sink)
			opts.Policy = gate
			res := RunStep(context.Background(), req, opts)

			if res.Kind != ResultComplete || res.Outcome != OutcomeOK {
				t.Fatalf("kind %q outcome %q (reason %q), want complete/ok — a deny is a step that carried on, and it must not suspend",
					res.Kind, res.Outcome, res.Reason)
			}
			if res.Question != nil {
				t.Errorf("a question escaped an operator-less step: %+v — nobody answers it; the deny must stand alone", res.Question)
			}
			blk, ok := toolResultFor(res.Messages, "t1")
			if !ok {
				t.Fatal("the asked call was never answered — its tool_use is dangling")
			}
			if !blk.IsError || blk.Content != testAskSentence {
				t.Errorf("the model was handed (%q, isError=%v), want the verdict sentence VERBATIM and true", blk.Content, blk.IsError)
			}
			if n := len(spy.recorded()); n != 0 {
				t.Errorf("the asked tool ran %d time(s): %+v — ask-before-answer never dispatches", n, spy.recorded())
			}
			var sawEnd bool
			for _, e := range sink.all() {
				if e.Kind == EventToolEnd && e.Tool == "bash" {
					sawEnd = true
					if e.Result != testAskSentence || !e.IsError {
						t.Errorf("the deny event carries result=%q isError=%v, want the sentence and true", e.Result, e.IsError)
					}
				}
			}
			if !sawEnd {
				t.Error("no step.tool.end carried the denial")
			}
		})
	}
}

func TestSecondIdenticalAskProducesNewQuestion(t *testing.T) {
	// QAP-13: no ask caching. The SAME command asked twice in one step — once
	// before the suspension, once after the answer resumed it — produces TWO
	// questions: the gate is re-consulted, the step re-suspends, and nothing
	// about the first question's answer (or its body) short-circuits the
	// second. "approve"/"deny" verdict handling on the resume side is T13's
	// token path; the runtime's duty here is that no cached ask answer exists
	// to short-circuit.
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("b1", "bash", `{"command":"`+testAskCommand+`"}`)}},
		// After the first answer, the model asks the IDENTICAL command again
		// (a new block id, as a provider emits).
		scriptedTurn{ToolUses: []model.Block{toolUse("b2", "bash", `{"command":"`+testAskCommand+`"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(toolSpec("bash"))
	req.HasOperator = true
	req.StepID = 9

	opts := runOpts(client, spy, nil)
	opts.Policy = gate

	first := RunStep(context.Background(), req, opts)
	if first.Kind != ResultSuspended || first.Question == nil || first.Question.ToolUseID != "b1" {
		t.Fatalf("first run: %+v — want suspension on b1", first)
	}

	second := ResumeStep(context.Background(), ResumeRequest{
		Request: req, Messages: first.Messages, Manifest: first.Manifest,
		Question: *first.Question, Answer: "deny",
	}, opts)
	if second.Kind != ResultSuspended || second.Question == nil {
		t.Fatalf("after the answer, the identical ask completed instead of suspending again: kind %q question %+v — an ask answered once must not be treated as answered forever",
			second.Kind, second.Question)
	}
	if second.Question.ToolUseID != "b2" {
		t.Errorf("second question ToolUseID %q, want b2 — a NEW question for the NEW call", second.Question.ToolUseID)
	}
	if second.Question.Body != first.Question.Body {
		t.Errorf("the second question differs from the first for an identical call:\n first: %s\nsecond: %s",
			first.Question.Body, second.Question.Body)
	}
	if second.Question == first.Question {
		t.Error("the second suspension returned the SAME question pointer — that is a cached question, not a new one")
	}
	reqs, _ := gate.log()
	if len(reqs) != 2 {
		t.Fatalf("the gate saw %d questions, want 2 — a second identical ask re-asks the gate; a cached answer is QAP-13", len(reqs))
	}
	for i, r := range reqs {
		if r.Command != testAskCommand {
			t.Errorf("gate question %d carried command %q, want %q", i, r.Command, testAskCommand)
		}
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the asked tool ran %d time(s) across the two suspensions: %+v", n, spy.recorded())
	}
	// The answer to the first question became b1's tool_result — the block is
	// genuinely answered, not silently skipped (S6's answer path, mirrored).
	if _, ok := toolResultFor(second.Messages, "b1"); !ok {
		t.Error("the first question's answer never reached b1's tool_use — the transcript would carry a dangling block")
	}
}

// --- mid-turn asks (review round 1, the Critical) -----------------------------
//
// Every ask test above places the asked block LAST in its turn or alone. The
// Critical was what those shapes cannot see: when the ask hits a NON-FINAL
// block of a multi-tool_use turn, everything after it never runs — and an
// unanswered tool_use is legal ONLY in the final assistant turn (format.go).
// The moment the answer appends a new turn, a silent sibling is a lost tool
// result forever: provider-invalid, unresumable. So the suspension itself
// must close them, honestly. These two tests put [ask, sibling...] on the
// loop and assert the closure, the zero dispatch, the suspended validity,
// and — the moment the old code died — the POST-RESUME validity.

func TestAskMidTurnClosesUnrunSiblings(t *testing.T) {
	// [bash(ask), read, read] in ONE turn: the ask hits FIRST, so both reads
	// are not-yet-run siblings. Each must be answered EXACTLY ONCE at
	// suspension with the honest IsError closure; the blocked bash stays
	// unanswered for the answer; nothing dispatches at all (a closure is a
	// block and an event, never a call); the transcript validates before AND
	// after the resume appends a new assistant turn.
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash", "read")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
			toolUse("r1", "read", `{"path":"a.md"}`),
			toolUse("r2", "read", `{"path":"b.md"}`),
		}},
		scriptedTurn{Text: okHandoff}, // the turn the answer releases
	)
	req := testRequest(toolSpec("bash"), toolSpec("read"))
	req.HasOperator = true
	req.StepID = 7
	sink := &recordingSink{}

	opts := runOpts(client, spy, sink)
	opts.Policy = gate
	res := RunStep(context.Background(), req, opts)

	if res.Kind != ResultSuspended || res.Question == nil || res.Question.ToolUseID != "t1" {
		t.Fatalf("kind=%q question=%+v, want suspended on t1", res.Kind, res.Question)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the mid-turn ask ran %d tool(s): %+v — neither the asked call nor a closed sibling may reach the dispatcher", n, spy.recorded())
	}
	if _, ok := toolResultFor(res.Messages, "t1"); ok {
		t.Error("the blocked call was answered at suspension — the ANSWER answers it")
	}
	for _, id := range []string{"r1", "r2"} {
		blocks := toolResultsFor(res.Messages, id)
		if len(blocks) != 1 {
			t.Fatalf("sibling %s has %d tool_results at suspension, want exactly 1", id, len(blocks))
		}
		if !blocks[0].IsError {
			t.Errorf("the closure for %s is not IsError — a non-error would read as a result", id)
		}
		for _, want := range []string{"read did not run", "pending permission question for t1", "not made and not approved", "re-issue"} {
			if !strings.Contains(blocks[0].Content, want) {
				t.Errorf("the closure for %s is missing %q:\n%s", id, want, blocks[0].Content)
			}
		}
	}
	// The suspended transcript is valid: t1 is the only unanswered tool_use
	// and it sits in the final assistant turn (format.go's rule).
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Fatalf("the suspended mid-turn transcript is invalid: %s", v.Reason)
	}
	// The gate was asked about the blocked call only — siblings that will
	// never run never pass the gate, because nothing is being dispatched.
	if reqs, _ := gate.log(); len(reqs) != 1 || reqs[0].Tool != "bash" {
		t.Errorf("the gate saw %+v, want exactly the one bash question — closures are not dispatch attempts", reqs)
	}
	// Watchers' spinners stop honestly: one error tool.end per sibling.
	var ends int
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "read" && e.IsError && strings.Contains(e.Result, "did not run") {
			ends++
		}
	}
	if ends != 2 {
		t.Errorf("%d sibling error tool.end events, want 2 — watchers would spin on reads that will never run", ends)
	}

	// The moment the old code died: the answer appends t1's tool_result and
	// RunLoop streams a NEW assistant turn — after which any un-closed
	// sibling sits outside the final assistant turn, which format.go calls
	// a lost tool result. With the closure, the whole post-resume transcript
	// still validates, each sibling keeps EXACTLY one result, and the
	// blocked call was answered by the answer.
	second := ResumeStep(context.Background(), ResumeRequest{
		Request: req, Messages: res.Messages, Manifest: res.Manifest,
		Question: *res.Question, Answer: "approve",
	}, opts)
	if v := model.ValidateTranscript(second.Messages); !v.OK {
		t.Errorf("post-resume transcript invalid: %s — un-closed siblings dangle outside the final assistant turn", v.Reason)
	}
	// An approval runs the blocked call (once) and the model reads ITS result, not the word "approve".
	if blk, ok := toolResultFor(second.Messages, "t1"); !ok || blk.Content != "bash ran" {
		t.Errorf("the approved call's result never reached t1: found=%v block=%+v", ok, blk)
	}
	for _, id := range []string{"r1", "r2"} {
		if n := len(toolResultsFor(second.Messages, id)); n != 1 {
			t.Errorf("sibling %s has %d results after resume, want exactly 1 — the closure must not be re-answered or re-run", id, n)
		}
	}
	if second.Kind != ResultComplete || second.Outcome != OutcomeOK {
		t.Errorf("post-resume kind=%q outcome=%q, want complete/ok — the step continues after the answer", second.Kind, second.Outcome)
	}
}

func TestAskMidTurnClosesTheQuestionSibling(t *testing.T) {
	// The question blocks count too. They run only AFTER all ordinary calls,
	// so a permission ask before one leaves the model's question a
	// not-yet-run sibling: the human's answer resumes the blocked CALL, not
	// the model's question, and a silent q1 would dangle the same way. It
	// gets the same honest IsError — the question truly did not get asked.
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash", "question")
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
			toolUse("q1", "question", `{"text":"which target?","kind":"free"}`),
		}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(toolSpec("bash"), toolSpec("question"))
	req.HasOperator = true
	sink := &recordingSink{}

	opts := runOpts(client, spy, sink)
	opts.Policy = gate
	res := RunStep(context.Background(), req, opts)

	if res.Kind != ResultSuspended || res.Question == nil || res.Question.ToolUseID != "t1" || res.Question.Kind != "permission" {
		t.Fatalf("kind=%q question=%+v, want a permission suspension on t1 — the human answers the blocked call, not the model's question",
			res.Kind, res.Question)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the question sibling dispatched %d tool(s): %+v", n, spy.recorded())
	}
	blocks := toolResultsFor(res.Messages, "q1")
	if len(blocks) != 1 {
		t.Fatalf("question sibling q1 has %d tool_results at suspension, want exactly 1 (closed)", len(blocks))
	}
	if !blocks[0].IsError || !strings.Contains(blocks[0].Content, "question did not run") ||
		!strings.Contains(blocks[0].Content, "pending permission question for t1") {
		t.Errorf("q1 was not closed honestly: (%q, isError=%v)", blocks[0].Content, blocks[0].IsError)
	}
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Fatalf("suspended transcript invalid: %s", v.Reason)
	}

	second := ResumeStep(context.Background(), ResumeRequest{
		Request: req, Messages: res.Messages, Manifest: res.Manifest,
		Question: *res.Question, Answer: "deny",
	}, opts)
	if v := model.ValidateTranscript(second.Messages); !v.OK {
		t.Errorf("post-resume transcript invalid: %s — q1 dangles outside the final assistant turn", v.Reason)
	}
	if n := len(toolResultsFor(second.Messages, "q1")); n != 1 {
		t.Errorf("q1 has %d results after resume, want exactly 1 — the closure stands, the question is not re-asked", n)
	}
}

func TestAskMidTurnClosesSiblingsUnderCheckpointWriter(t *testing.T) {
	// The closure owes the transcript shape the ordinary path maintains
	// under a checkpoint writer — ONE user message for the turn, replaced
	// wholesale with every result — plus its persist: the closed turn is
	// checkpointed like every per-result append, so a death before the
	// caller's suspended-write replays from rows that already carry the
	// closures, not a half-closed turn. Three arms: the ask hits the FIRST
	// block (the result message is still to be created), the ask hits after
	// a result (wholesale replace, never a second message), and an
	// unsavable closure FAILS the step rather than suspending on an unsaved
	// transcript. RunLoop is entered directly (as receiptRun does) so the
	// save count is the loop's own: #1 model turn, #2 assistant turn, #3
	// the closure's persist.
	cases := []struct {
		name   string
		blocks []model.Block
		fail   bool
	}{
		{"ask-first-creates-the-result-message", []model.Block{
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
			toolUse("r1", "read", `{"path":"a.md"}`),
		}, false},
		{"ask-after-a-result-replaces-it-wholesale", []model.Block{
			toolUse("r0", "read", `{"path":"z.md"}`),
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
			toolUse("r1", "read", `{"path":"a.md"}`),
		}, false},
		{"unsavable-closure-fails-instead-of-suspending", []model.Block{
			toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`),
			toolUse("r1", "read", `{"path":"a.md"}`),
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := askGate(testAskSentence, "confirm-push", "agent:boss")
			spy := newSpyTools("bash", "read")
			client := newFakeClient(scriptedTurn{ToolUses: tc.blocks}, scriptedTurn{Text: okHandoff})
			st := testState(testHandler("bash", "read"), []model.ToolSpec{toolSpec("bash"), toolSpec("read")})
			st.Request.HasOperator = true
			// #1 and #2 are the loop's own opening checkpoints; the arm under
			// test is save #3 — the closed turn.
			cp := &flakyCheckpoint{ok: 99}
			if tc.fail {
				cp.ok = 2 // the model turn and assistant turn save; the closure does not
			}
			res := RunLoop(context.Background(), st, LoopDeps{
				Client: client, Tools: spy, Checkpoint: cp, Policy: gate,
				HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
			})

			if tc.fail {
				if res.Kind != ResultComplete || res.Outcome != OutcomeFailed ||
					!strings.Contains(res.Reason, "the tool result could not be checkpointed") {
					t.Fatalf("kind=%q outcome=%q reason=%q — want failed naming the closure's checkpoint",
						res.Kind, res.Outcome, res.Reason)
				}
				if res.Question != nil {
					t.Error("a failed step carried a question — the suspension was never reached")
				}
				if cp.saves != 3 {
					t.Errorf("the writer saw %d saves, want 3 (model turn, assistant, the failed closure)", cp.saves)
				}
				return
			}

			if res.Kind != ResultSuspended || res.Question == nil || res.Question.ToolUseID != "t1" {
				t.Fatalf("kind=%q question=%+v, want suspended on t1", res.Kind, res.Question)
			}
			blocks := toolResultsFor(res.Messages, "r1")
			if len(blocks) != 1 || !blocks[0].IsError || !strings.Contains(blocks[0].Content, "did not run") {
				t.Errorf("the r1 closure is wrong: %d results %+v", len(blocks), blocks)
			}
			if _, ok := toolResultFor(res.Messages, "t1"); ok {
				t.Error("the blocked call was answered at suspension")
			}
			// The closure lives in EXACTLY ONE user message — the wholesale
			// replace, not an append beside the message the per-result path
			// created.
			var carriers int
			for _, m := range res.Messages {
				if m.Role != model.RoleUser {
					continue
				}
				for _, b := range m.Content {
					if b.Kind == model.BlockToolResult && b.ToolUseID == "r1" {
						carriers++
						break
					}
				}
			}
			if carriers != 1 {
				t.Errorf("r1's closure lives in %d user messages, want exactly 1 — wholesale replace, never append", carriers)
			}
			// And it was PERSISTED: the last snapshot is the closed turn.
			if len(cp.snapshots) == 0 {
				t.Fatal("no checkpoint was written — the closed turn would live only in memory")
			}
			final := cp.snapshots[len(cp.snapshots)-1]
			if len(toolResultsFor(final.Messages, "r1")) != 1 {
				t.Error("the final snapshot lacks the closure — a death before the caller's suspended-write would replay a half-closed turn")
			}
			if v := model.ValidateTranscript(final.Messages); !v.OK {
				t.Errorf("the persisted snapshot is not a valid transcript: %s", v.Reason)
			}
		})
	}
}

func TestPermissionAskBody(t *testing.T) {
	// The pure renderer: QAP-12's three names plus the verbatim sentence,
	// with the command through strconv.Quote (SEC-051) and honest fallbacks
	// when a gate leaves a field empty.
	dec := PolicyGateDecision{Asked: true, Sentence: testAskSentence, RuleID: "rule-9", RuleSubject: "agent:boss"}
	t.Run("full decision", func(t *testing.T) {
		body := permissionAskBody(dec, "boss-agent", "bash", testAskCommand)
		for _, want := range []string{"boss-agent", "agent:boss", testAskSentence, strconv.Quote(testAskCommand)} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "command "+testAskCommand) {
			t.Errorf("the command reached the body RAW (SEC-051):\n%s", body)
		}
	})
	t.Run("subject falls back to the rule id", func(t *testing.T) {
		d := dec
		d.RuleSubject = ""
		body := permissionAskBody(d, "a", "bash", "ls")
		if !strings.Contains(body, "rule-9") {
			t.Errorf("the rule id did not name the rule when the subject was empty:\n%s", body)
		}
	})
	t.Run("no rule at all is said honestly", func(t *testing.T) {
		d := dec
		d.RuleSubject, d.RuleID = "", ""
		body := permissionAskBody(d, "a", "bash", "ls")
		if !strings.Contains(body, "no rule named") {
			t.Errorf("an unnamed rule was papered over:\n%s", body)
		}
	})
	t.Run("a non-command ask names the call, unquoted and uncommanded", func(t *testing.T) {
		body := permissionAskBody(dec, "a", "read", "")
		if strings.Contains(body, `""`) || !strings.Contains(body, "the call to read") {
			t.Errorf("empty command rendered as an empty quote instead of naming the call:\n%s", body)
		}
	})
	// A command whose text contains quotes must not be able to write the
	// body's sentence — the Quote keeps it a quoted string, not prose.
	t.Run("a hostile command cannot forge prose", func(t *testing.T) {
		hostile := `ls" · deciding rule rule-evil · echo pwned`
		body := permissionAskBody(dec, "a", "bash", hostile)
		if !strings.Contains(body, strconv.Quote(hostile)) {
			t.Errorf("the hostile command was not carried as a quoted string (SEC-051):\n%s", body)
		}
		// With the quoted occurrence removed, NONE of the hostile text may
		// survive raw — the one "deciding rule" clause a renderer reads is
		// the engine's, never one the command wrote.
		stripped := strings.ReplaceAll(body, strconv.Quote(hostile), "")
		if strings.Contains(stripped, "echo pwned") || strings.Contains(stripped, "rule-evil") {
			t.Errorf("raw hostile text survived outside the quotes (SEC-051):\n%s", stripped)
		}
	})
}

func TestCommandArgumentReadsOnlyBashOwnInput(t *testing.T) {
	// The server-side parse behind the gate question: bash's own `command`
	// field only; everything else asks the tool question with an empty one.
	cases := []struct {
		name, tool, input, want string
	}{
		{"bash command", "bash", `{"command":"git push --force"}`, "git push --force"},
		{"bash no command field", "bash", `{"timeoutSeconds":5}`, ""},
		{"bash null command", "bash", `{"command":null}`, ""},
		{"bash wrong-typed command", "bash", `{"command":42}`, ""},
		{"bash unparseable input", "bash", `{`, ""},
		{"bash empty input", "bash", ``, ""},
		{"read with a command-shaped field stays empty", "read", `{"command":"ls"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandArgument(tc.tool, json.RawMessage(tc.input)); got != tc.want {
				t.Errorf("commandArgument(%q, %s) = %q, want %q", tc.tool, tc.input, got, tc.want)
			}
		})
	}
}
