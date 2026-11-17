package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

type questionSpy struct {
	routeFn    func(int64) (string, bool)
	steps      *spyStepStore
	asked      int
	askErr     error
	orderAtAsk []string
	answered   int
	answer     AnsweredQuestion
	resume     ResumeSnapshot
	replaced   chan struct{}

	// continued records the transcripts handed to ContinueWithMessages (S31
	// T2's `step.continue {message}` path); markedRunning counts BOTH flips,
	// because the message path marks running through the same atomic store
	// method the message-less path does with MarkRunning.
	continued   [][]model.Message
	continueErr error

	// status/pauseReason answer StepState. Zero value for every test in THIS
	// file, which exercises `answer` and never reads them; the `step.continue`
	// tests (step_continue_test.go) set them explicitly.
	status      string
	pauseReason string

	markedRunning bool
}

func (s *questionSpy) AskStep(_ int64, _, _ string, _ runtime.PendingQuestion) (int64, error) {
	s.asked++
	s.orderAtAsk = s.steps.order()
	if s.askErr != nil {
		return 0, s.askErr
	}
	return 9, nil
}
func (s *questionSpy) CheckStepQuestion(_, _ int64) (AnsweredQuestion, error) {
	if s.answer.Refusal != "" {
		return s.answer, nil
	}
	return AnsweredQuestion{}, nil
}
func (s *questionSpy) AnswerStep(_, _ int64, _ string) (AnsweredQuestion, error) {
	s.answered++
	return s.answer, nil
}
func (s *questionSpy) LoadResume(_ int64) (ResumeSnapshot, error) { return s.resume, nil }
func (s *questionSpy) ReplaceStepMessages(_ int64, _ []model.Message) error {
	if s.replaced != nil {
		close(s.replaced)
	}
	return nil
}
func (s *questionSpy) MarkRunning(_ int64) error {
	s.markedRunning = true
	return nil
}
func (s *questionSpy) ContinueWithMessages(_ int64, msgs []model.Message) error {
	if s.continueErr != nil {
		return s.continueErr
	}
	s.continued = append(s.continued, msgs)
	s.markedRunning = true
	return nil
}

func (s *questionSpy) StepState(_ int64) (string, string, error) { return s.status, s.pauseReason, nil }

// StepRoute answers Q6 for the answer door (S18 T8). The spy is the step's
// own saved route by default — no loop owns it — and the cross-provider test
// sets `routeFn` to speak as a loop that switched.
func (s *questionSpy) StepRoute(id int64) (string, bool) {
	if s.routeFn != nil {
		return s.routeFn(id)
	}
	return "", false
}

type resumeSpy struct {
	*scriptedRunner
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	called  int
	route   model.Route
	// lastOpts/lastReq for the DEF-034 forwarding assertions (the RunOpts
	// produced by continueAnsweredStep, read from the call itself).
	lastOpts runtime.RunOpts
	lastReq  runtime.ResumeRequest
}

func (r *resumeSpy) Resume(_ context.Context, req runtime.ResumeRequest, opts runtime.RunOpts) runtime.StepResult {
	r.mu.Lock()
	r.called++
	r.route = req.Request.Route
	r.lastOpts = opts
	r.lastReq = req
	r.mu.Unlock()
	close(r.entered)
	<-r.release
	return runtime.StepResult{Kind: runtime.ResultComplete, Outcome: runtime.OutcomeOK,
		Messages: req.Messages, Manifest: req.Manifest}
}

func TestSuspensionPersistsHistoryBeforeAskingAndThenSetsStatus(t *testing.T) {
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps}
	runner := &scriptedRunner{result: runtime.StepResult{Kind: runtime.ResultSuspended,
		Question: &runtime.PendingQuestion{Kind: "clarify", Body: "Which approach?", ToolUseID: "q1"}}}
	tbl, events := stepTable(t, runner, steps)
	tbl.Questions = questions
	out, err := callStep(t, tbl, StepParams{Prompt: "choose", Tools: []string{"read", "question"}, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "suspended" || out.Question == nil || out.Question.ID != 9 {
		t.Fatalf("result: %+v", out)
	}
	if !reflect.DeepEqual(questions.orderAtAsk, []string{"CreateRunningStep", "AppendStepMessages", "SaveStepContext"}) {
		t.Fatalf("writes before Ask: %v", questions.orderAtAsk)
	}
	if got := steps.order(); !reflect.DeepEqual(got, []string{"CreateRunningStep", "AppendStepMessages", "SaveStepContext", "FinishStep"}) {
		t.Fatalf("write order: %v", got)
	}
	if steps.finalStatus != "suspended" {
		t.Fatalf("status: %s", steps.finalStatus)
	}
	found := false
	for _, e := range events.all() {
		if e.Kind == "question.asked" {
			found = true
		}
	}
	if !found {
		t.Fatal("question.asked was not emitted")
	}
}

func TestSuspensionPersistenceFailureLeavesNoQuestion(t *testing.T) {
	steps := &spyStepStore{contextErr: errors.New("disk full")}
	questions := &questionSpy{steps: steps}
	runner := &scriptedRunner{result: runtime.StepResult{Kind: runtime.ResultSuspended,
		Question: &runtime.PendingQuestion{Kind: "free", Body: "What next?", ToolUseID: "q1"}}}
	tbl, _ := stepTable(t, runner, steps)
	tbl.Questions = questions
	out, err := callStep(t, tbl, StepParams{Prompt: "choose", WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if questions.asked != 0 || steps.finalStatus == "suspended" {
		t.Fatalf("unsafe suspension: asked=%d status=%q", questions.asked, steps.finalStatus)
	}
	if len(steps.messages) == 0 && len(runner.result.Messages) != 0 {
		t.Fatal("transcript was not attempted")
	}
	if !strings.Contains(out.Reason, "manifest") {
		t.Fatalf("failure not reported: %s", out.Reason)
	}
	if out.Outcome != "failed" {
		t.Fatalf("failed persistence was presented as %s", out.Outcome)
	}
}

func TestAnswerQueuesResumeAndRefusesSecondAnswer(t *testing.T) {
	steps := &spyStepStore{}
	base := &scriptedRunner{}
	runner := &resumeSpy{scriptedRunner: base, entered: make(chan struct{}), release: make(chan struct{})}
	tbl, events := stepTable(t, runner, steps)
	request := runtime.StepRequest{Handler: runtime.DefaultHandler("choose", []string{"read"}), WorkspaceRoot: t.TempDir(), Route: pricedRoute("r1", "openai")}
	raw, err := json.Marshal(stepSnapshot{Request: request, LoopSlug: "step", ContextManifest: runtime.ContextManifest{MaxIterations: 5}})
	if err != nil {
		t.Fatal(err)
	}
	questions := &questionSpy{steps: steps, resume: ResumeSnapshot{Manifest: raw, Messages: []model.Message{{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system")}}}},
		answer: AnsweredQuestion{Question: runtime.PendingQuestion{ID: 9, ToolUseID: "q1"}, Answer: "yes", AnsweredAt: "2026-10-17T10:00:00Z"}, replaced: make(chan struct{})}
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")
	res, err := tbl.Dispatch(context.Background(), "c1", MethodAnswer, mustJSON(t, AnswerParams{StepID: 4, Answer: "yes"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.(AnswerResponse).Status != "running" {
		t.Fatalf("response: %+v", res)
	}
	select {
	case <-runner.entered:
	default:
	}
	// The model continuation may be scheduled, but the request has returned
	// while the continuation is still blocked on its own gate.
	close(runner.release)
	<-questions.replaced
	runner.mu.Lock()
	resumedProvider := runner.route.Provider
	runner.mu.Unlock()
	if resumedProvider != "anthropic" {
		t.Fatalf("resume did not use the current provider: %s", resumedProvider)
	}
	if questions.answered != 1 {
		t.Fatalf("answers: %d", questions.answered)
	}
	found := false
	for _, e := range events.all() {
		if e.Kind == "question.answered" {
			found = true
			var payload struct {
				QuestionID int64  `json:"questionId"`
				Answer     string `json:"answer"`
			}
			if err := json.Unmarshal(e.Payload, &payload); err != nil || payload.QuestionID != 9 || payload.Answer != "yes" {
				t.Fatalf("question.answered payload: %s (%v)", e.Payload, err)
			}
		}
	}
	if !found {
		t.Fatal("question.answered was not emitted")
	}
	questions.answer.Refusal = "question 9 was already answered at 2026-10-17T10:00:00Z"
	_, err = tbl.Dispatch(context.Background(), "c1", MethodAnswer, mustJSON(t, AnswerParams{StepID: 4, QuestionID: 9, Answer: "no"}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrQuestionAnswered.Code || !strings.Contains(e.Data.What, "2026-10-17T10:00:00Z") {
		t.Fatalf("second answer: %v", err)
	}
	questions.answer = AnsweredQuestion{Refusal: "step 4 has no pending question"}
	_, err = tbl.Dispatch(context.Background(), "c1", MethodAnswer, mustJSON(t, AnswerParams{StepID: 4, Answer: "no"}))
	if !errors.As(err, &e) || e.Code != ErrNoPendingQuestion.Code {
		t.Fatalf("no pending: %v", err)
	}
}

// TestActionAskReusesThePermissionVerbs pins S16's Q5 answer from the
// answer side: the code-action ask dispatches through the SAME verbs S11
// minted for permission asks, so one affordance answers both (U14) and a
// code action can never grow an answer vocabulary of its own. The
// negative half is the load-bearing one: a word that is not exactly
// "approve" never executes an outward-facing effect.
func TestActionAskReusesThePermissionVerbs(t *testing.T) {
	if strings.Join(ActionAskOptions, ",") != "approve,deny" {
		t.Fatalf("the verbs are S11's exactly, got %v", ActionAskOptions)
	}
	if ok, err := ResolveActionAsk("approve"); !ok || err != nil {
		t.Fatalf("approve must execute: %v %v", ok, err)
	}
	if ok, err := ResolveActionAsk("deny"); ok || err != nil {
		t.Fatalf("deny must refuse without erroring: %v %v", ok, err)
	}
	for _, junk := range []string{"", "yes", "y", "Approve", "approve ", "LGTM", "merge-now"} {
		if ok, err := ResolveActionAsk(junk); ok || err == nil {
			t.Fatalf("%q must be refused outright — silence is not consent and neither is a typo", junk)
		}
	}
}

// TestResumeDoesNotAssumeTheOriginalRoute (S18 T8, Q6) — the assumption this
// task existed to lift. The step's saved request still names claude-route:
// it suspended there, honestly. But the LOOP was steered to gpt-route while
// it waited, and a suspended step resumed on the route the loop is on NOW is
// the slice's claim — a loop that resumed on the route it began on could
// never be resumed on another provider at all. The step outside any loop
// keeps its own row's opinion (the spy default is ok=false; that half is
// pinned by TestAnswerQueuesResumeAndRefusesSecondAnswer above).
func TestResumeDoesNotAssumeTheOriginalRoute(t *testing.T) {
	steps := &spyStepStore{}
	base := &scriptedRunner{}
	runner := &resumeSpy{scriptedRunner: base, entered: make(chan struct{}), release: make(chan struct{})}
	tbl, _ := stepTable(t, runner, steps, pricedRoute("claude-route", "anthropic"), pricedRoute("gpt-route", "openai"))
	request := runtime.StepRequest{Handler: runtime.DefaultHandler("choose", []string{"read"}),
		WorkspaceRoot: t.TempDir(), Route: pricedRoute("claude-route", "anthropic")}
	raw, err := json.Marshal(stepSnapshot{Request: request, LoopSlug: "attr",
		ContextManifest: runtime.ContextManifest{MaxIterations: 5}})
	if err != nil {
		t.Fatal(err)
	}
	questions := &questionSpy{steps: steps,
		resume: ResumeSnapshot{Manifest: raw, Messages: []model.Message{
			{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system")}}}},
		answer:   AnsweredQuestion{Question: runtime.PendingQuestion{ID: 9, ToolUseID: "q1"}, Answer: "yes", AnsweredAt: "2026-10-17T10:00:00Z"},
		replaced: make(chan struct{}),
		routeFn:  func(int64) (string, bool) { return "gpt-route", true },
	}
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAnswer, mustJSON(t, AnswerParams{StepID: 4, Answer: "yes"})); err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	<-questions.replaced
	runner.mu.Lock()
	got := runner.route
	runner.mu.Unlock()
	if got.ID != "gpt-route" || got.Provider != "openai" {
		t.Fatalf("the resume ran on %s/%s — the loop's current route governs, not the saved request's (Q6)", got.ID, got.Provider)
	}
}
