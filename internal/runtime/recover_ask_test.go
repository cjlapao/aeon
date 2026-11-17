package runtime

// The recovery half of S11's ask suspension. T12's loop-side tests
// (loop_test.go) prove RunStep suspends on an ask; these prove RecoverStep
// does too: a crashed step's re-run is judged by the frozen-set gate, and an
// Asked verdict there must take the SAME exits as the loop — suspend (P2)
// when somebody can answer, deny on the spot when nobody can. The block
// these tests exist for is recover.go's CaseC consumption of
// state.pendingPermission, checked BEFORE the error (an ask returns no block
// and no error, so an error-first read would answer the model with the
// zero-value result of a call that never dispatched).

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestRecoverCaseCAskSuspendsForOperator(t *testing.T) {
	// Attended (HasOperator true): a re-run the gate asks about suspends the
	// recovery exactly like S6's question tool — the question is a
	// permission question for the blocked call, its body names the rule, the
	// agent and the Quoted command (QAP-12), the dispatcher is never reached,
	// and the blocked tool_use stays unanswered for the answer to resume.
	req := recoveryFixture("bash")
	// The re-run block carries a real command (as a crashed step's stored
	// bash tool_use would): the question must quote it.
	req.Messages[len(req.Messages)-1].Content[0].Input = []byte(`{"command":"` + testAskCommand + `"}`)
	req.Request.HasOperator = true
	req.Request.StepID = 77
	order := []string{}
	opts, spy := recoveryOpts("bash", "safe", &order)
	opts.Policy = askGate(testAskSentence, "confirm-push", "agent:boss")

	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC,
		Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: ReRun}}}, opts)

	if got.Kind != ResultSuspended || got.Question == nil {
		t.Fatalf("recovered ask did not suspend: kind=%q question=%+v (outcome %q reason %q)", got.Kind, got.Question, got.Outcome, got.Reason)
	}
	q := got.Question
	if q.Kind != "permission" || q.ToolUseID != "u1" {
		t.Errorf("question kind=%q toolUseID=%q, want permission/u1", q.Kind, q.ToolUseID)
	}
	// The answer set is pinned HERE, on the recovery side (review suggestion
	// 1): the same [approve deny] the loop-side ask offers — asserted in this
	// file rather than argued cross-file from loop_test.go. "Both suspend
	// paths build it identically" is exactly the claim a pin beats.
	if !reflect.DeepEqual(q.Options, PermissionOptions()) {
		t.Errorf("recovery-side question options %v, want the five permission answers — the same answer set the loop-side ask offers", q.Options)
	}
	if !strings.Contains(q.Body, "confirm-push") || !strings.Contains(q.Body, testAskSentence) ||
		!strings.Contains(q.Body, "test-handler") || !strings.Contains(q.Body, strconv.Quote(testAskCommand)) {
		t.Errorf("body missing rule/sentence/agent/command:\n%s", q.Body)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the asked re-run dispatched %d time(s): %+v", n, spy.recorded())
	}
	// The blocked u1 stays unanswered in the suspended transcript.
	for _, m := range got.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "u1" {
				t.Error("the asked re-run was answered at suspension — the answer answers it")
			}
		}
	}
	if v := model.ValidateTranscript(got.Messages); !v.OK {
		t.Errorf("suspended recovery transcript invalid: %s", v.Reason)
	}
}

func TestRecoverCaseCAskDeniesWithoutOperator(t *testing.T) {
	// Unattended (HasOperator false, the never-declared shape the adapter
	// collapses to): nobody can answer, so the ask is a deny on the spot —
	// the verdict sentence verbatim as the tool_result, no question produced
	// at all (the adapter grows question rows only for suspended results),
	// no dispatch, and the recovery carries on to the model call.
	req := recoveryFixture("bash")
	req.Messages[len(req.Messages)-1].Content[0].Input = []byte(`{"command":"` + testAskCommand + `"}`)
	req.Request.HasOperator = false
	order := []string{}
	opts, spy := recoveryOpts("bash", "safe", &order)
	opts.Policy = askGate(testAskSentence, "confirm-push", "agent:boss")

	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC,
		Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: ReRun}}}, opts)

	if got.Kind == ResultSuspended || got.Question != nil {
		t.Fatalf("operator-less recovered ask suspended: kind=%q q=%+v — it must deny on the spot", got.Kind, got.Question)
	}
	if got.Outcome != OutcomeOK {
		t.Fatalf("outcome %q reason %q, want ok — the deny lets the recovery carry on", got.Outcome, got.Reason)
	}
	var blk model.Block
	var ok bool
	for _, m := range got.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "u1" {
				blk, ok = b, true
			}
		}
	}
	if !ok || !blk.IsError || blk.Content != testAskSentence {
		t.Errorf("denied re-run handed the model (%q, isError=%v, found=%v), want the verbatim sentence and true", blk.Content, blk.IsError, ok)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("denied re-run dispatched %d time(s)", n)
	}
}

// --- mid-plan asks (review round 1, the Critical — recovery entry) -----------
//
// The loop-side pair in loop_test.go proved [ask, sibling] on RunLoop. These
// prove the SAME honesty on RecoverStep's CaseC: when the gate asks about a
// re-run that is NOT the plan's last entry, everything after it never re-runs
// — and the old comment promised they would be "decided when the step next
// runs", a path that does not exist (after the answer the step enters
// ResumeStep's RunLoop and nobody revisits this plan). Silent tool_uses after
// a new assistant turn is appended are provider-invalid forever (format.go).
// So the remaining entries close AT SUSPENSION: re-runs get the honest
// "did not run" IsError, entries whose receipt already captured a result keep
// the captured result — that call DID run, and the other sentence would lie.

func TestRecoverCaseCMidPlanAskClosesRemainingReRuns(t *testing.T) {
	// [u1(bash, ask), u2(read, ReRun)] with an operator: suspend on u1, u2
	// closed exactly once with the IsError closure, zero dispatch, valid
	// before and after the resume appends a new assistant turn.
	req := recoveryFixture("bash")
	req.Messages[len(req.Messages)-1].Content = append(req.Messages[len(req.Messages)-1].Content,
		recoveryUse("u2", "read")) // the not-yet-run sibling
	req.Messages[len(req.Messages)-1].Content[0].Input = []byte(`{"command":"` + testAskCommand + `"}`)
	req.Request.HasOperator = true
	req.Request.StepID = 78
	order := []string{}
	opts, spy := recoveryOpts("bash", "safe", &order)
	opts.Policy = askGate(testAskSentence, "confirm-push", "agent:boss")
	sink := &recordingSink{}
	opts.Sink = sink

	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{
		{ToolUseID: "u1", Tool: "bash", Action: ReRun},
		{ToolUseID: "u2", Tool: "read", Action: ReRun},
	}}, opts)

	if got.Kind != ResultSuspended || got.Question == nil || got.Question.ToolUseID != "u1" {
		t.Fatalf("mid-plan recovered ask did not suspend on u1: kind=%q question=%+v", got.Kind, got.Question)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the mid-plan ask ran %d tool(s): %+v — neither the asked re-run nor a closed sibling may dispatch", n, spy.recorded())
	}
	if _, ok := toolResultFor(got.Messages, "u1"); ok {
		t.Error("the blocked re-run was answered at suspension — the answer answers it")
	}
	blocks := toolResultsFor(got.Messages, "u2")
	if len(blocks) != 1 {
		t.Fatalf("sibling u2 has %d tool_results at suspension, want exactly 1", len(blocks))
	}
	if !blocks[0].IsError {
		t.Errorf("the closure for u2 is not IsError — a non-error would read as a result")
	}
	for _, want := range []string{"read did not run", "pending permission question for u1", "not made and not approved", "re-issue"} {
		if !strings.Contains(blocks[0].Content, want) {
			t.Errorf("the closure for u2 is missing %q:\n%s", want, blocks[0].Content)
		}
	}
	if v := model.ValidateTranscript(got.Messages); !v.OK {
		t.Fatalf("suspended mid-plan transcript invalid: %s", v.Reason)
	}
	var sawEnd bool
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "read" && e.IsError && strings.Contains(e.Result, "did not run") {
			sawEnd = true
		}
	}
	if !sawEnd {
		t.Error("no error tool.end for the closed sibling — watchers would spin on a read that never runs")
	}

	// The moment the old code died: the answer resumes u1 and RunLoop streams
	// a new assistant turn. With u2 closed, the post-resume transcript still
	// validates and u2 keeps EXACTLY one result — never re-run, never doubled.
	second := ResumeStep(context.Background(), ResumeRequest{
		Request: req.Request, Messages: got.Messages, Manifest: got.Manifest,
		Question: *got.Question, Answer: "approve",
	}, opts)
	if v := model.ValidateTranscript(second.Messages); !v.OK {
		t.Errorf("post-resume transcript invalid: %s — the un-closed sibling dangles outside the final assistant turn", v.Reason)
	}
	if n := len(toolResultsFor(second.Messages, "u2")); n != 1 {
		t.Errorf("u2 has %d results after resume, want exactly 1 — the closure stands; nobody re-runs it behind the answer", n)
	}
	// An approval runs the blocked call (once) and the model reads ITS result, not the word "approve".
	if blk, ok := toolResultFor(second.Messages, "u1"); !ok || blk.Content != "bash ran" {
		t.Errorf("the approved call's result never reached u1: found=%v block=%+v", ok, blk)
	}
}

func TestRecoverCaseCMidPlanAskKeepsCapturedResults(t *testing.T) {
	// The honesty split inside the remaining-plan closure: u2's unsafe
	// receipt already captured its result — the call DID run before the
	// crash — so it keeps the captured result verbatim, NOT the "did not
	// run" sentence. Lying that a ran call did not run is the same defect
	// wearing the other mask.
	req := recoveryFixture("bash")
	req.Messages[len(req.Messages)-1].Content = append(req.Messages[len(req.Messages)-1].Content,
		recoveryUse("u2", "bash"))
	req.Messages[len(req.Messages)-1].Content[0].Input = []byte(`{"command":"` + testAskCommand + `"}`)
	req.Request.HasOperator = true
	req.Request.StepID = 79
	order := []string{}
	opts, spy := recoveryOpts("bash", "safe", &order)
	opts.Policy = askGate(testAskSentence, "confirm-push", "agent:boss")

	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{
		{ToolUseID: "u1", Tool: "bash", Action: ReRun},
		{ToolUseID: "u2", Tool: "bash", Action: RecoverResult, Result: "captured before the crash", IsError: true},
	}}, opts)

	if got.Kind != ResultSuspended || got.Question == nil {
		t.Fatalf("mid-plan recovered ask did not suspend: kind=%q", got.Kind)
	}
	blocks := toolResultsFor(got.Messages, "u2")
	if len(blocks) != 1 {
		t.Fatalf("u2 has %d tool_results, want exactly 1", len(blocks))
	}
	if blocks[0].Content != "captured before the crash" || !blocks[0].IsError {
		t.Errorf("u2's captured result was not kept verbatim: (%q, isError=%v)", blocks[0].Content, blocks[0].IsError)
	}
	if strings.Contains(blocks[0].Content, "did not run") {
		t.Error("a call whose receipt captured a result was closed as if it had never run — that is the lie this split exists to prevent")
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("the mid-plan ask ran %d tool(s): %+v", n, spy.recorded())
	}
	if v := model.ValidateTranscript(got.Messages); !v.OK {
		t.Errorf("suspended transcript invalid: %s", v.Reason)
	}
}

func TestRecoverCaseCClosureCheckpointFailureIsSpoken(t *testing.T) {
	// The closed turn must be SAVED to be owed: CaseC persists every result
	// through recordResults, and a writer that cannot save fails the step,
	// naming the cause — never a suspension whose closures live only in
	// memory (the mid-step checkpoint is what a death BEFORE the caller's
	// suspended-write replays from). Both arms share the recorder: the
	// closure persist of the suspend branch, and the ordinary per-result
	// append.
	t.Run("closure", func(t *testing.T) {
		req := recoveryFixture("bash")
		req.Messages[len(req.Messages)-1].Content = append(req.Messages[len(req.Messages)-1].Content,
			recoveryUse("u2", "read"))
		req.Messages[len(req.Messages)-1].Content[0].Input = []byte(`{"command":"` + testAskCommand + `"}`)
		req.Request.HasOperator = true
		order := []string{}
		opts, spy := recoveryOpts("bash", "safe", &order)
		opts.Policy = askGate(testAskSentence, "confirm-push", "agent:boss")
		opts.Checkpoint = &flakyCheckpoint{ok: 0}

		got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC, Calls: []CallPlan{
			{ToolUseID: "u1", Tool: "bash", Action: ReRun},
			{ToolUseID: "u2", Tool: "read", Action: ReRun},
		}}, opts)

		if got.Kind != ResultComplete || got.Outcome != OutcomeFailed ||
			!strings.Contains(got.Reason, "recovered results could not be checkpointed") {
			t.Fatalf("kind=%q outcome=%q reason=%q — want failed naming the closure checkpoint",
				got.Kind, got.Outcome, got.Reason)
		}
		if got.Question != nil {
			t.Error("a failed recovery carried a question — it never reached suspension")
		}
		if n := len(spy.recorded()); n != 0 {
			t.Errorf("%d dispatch(es) on the failing path: %+v", n, spy.recorded())
		}
	})
	t.Run("plain-result", func(t *testing.T) {
		// The ordinary append is honest the same way (recordResults is
		// shared): a recovered result that cannot be saved fails the step,
		// naming it — not a carry-on that loses the captured answer.
		req := recoveryFixture("bash")
		order := []string{}
		opts, _ := recoveryOpts("bash", "unsafe", &order)
		opts.Checkpoint = &flakyCheckpoint{ok: 0}
		got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC,
			Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: RecoverResult, Result: "captured"}}}, opts)
		if got.Outcome != OutcomeFailed || !strings.Contains(got.Reason, "recovered result could not be checkpointed") {
			t.Fatalf("outcome=%q reason=%q — want failed naming the result checkpoint", got.Outcome, got.Reason)
		}
	})
}

func TestRecoverReRunErrorStillFailsTheStep(t *testing.T) {
	// The error arm beside the ask branch: an unsafe re-run with no receipt
	// writer fails runTool, and the recovery says so — the pendingPermission
	// check sitting FIRST must not swallow real tool errors. (An ask sets
	// no error; an error sets no pendingPermission; the two arms are exact
	// complements, and this drives the second one.)
	req := recoveryFixture("bash")
	order := []string{}
	opts, spy := recoveryOpts("bash", "unsafe", &order)
	// opts.Receipts stays nil — the refusal is runTool's own.
	got := RecoverStep(context.Background(), req, RecoveryPlan{Case: CaseC,
		Calls: []CallPlan{{ToolUseID: "u1", Tool: "bash", Action: ReRun}}}, opts)
	if got.Outcome != OutcomeFailed || !strings.Contains(got.Reason, "refusing to run unsafe tool \"bash\" without a receipt writer") {
		t.Fatalf("outcome=%q reason=%q — want failed naming the receipt-writer refusal", got.Outcome, got.Reason)
	}
	if got.Question != nil {
		t.Errorf("an error path produced a question: %+v", got.Question)
	}
	if n := len(spy.recorded()); n != 0 {
		t.Errorf("%d dispatch(es) past the refusal", n)
	}
}
