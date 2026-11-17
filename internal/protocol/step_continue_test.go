package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// T-continue: `step.continue`'s preconditions, its route/effort override, and
// the end-to-end proof that a continued step can be stopped again — the same
// cancel registry reaching a SECOND run under the same step id.

func continueSnapshot(t *testing.T, req runtime.StepRequest) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(stepSnapshot{Request: req, LoopSlug: "step",
		ContextManifest: runtime.ContextManifest{MaxIterations: 5}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pausedQuestions(t *testing.T, steps *spyStepStore, req runtime.StepRequest) *questionSpy {
	return &questionSpy{
		steps: steps, status: "suspended", pauseReason: "user",
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, req),
			Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}},
		},
		replaced: make(chan struct{}, 1),
	}
}

func TestContinueRefusesAStepThatIsRunningOrDoneOrWaitingOnAQuestion(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		pauseReason string
	}{
		{"running", "running", ""},
		{"done", "done", ""},
		{"a real question, not a stop", "suspended", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := &spyStepStore{}
			questions := &questionSpy{steps: steps, status: tc.status, pauseReason: tc.pauseReason}
			tbl, _ := stepTable(t, &scriptedRunner{}, steps)
			tbl.Questions = questions
			initializedOnce(t, tbl, "c1")

			_, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
				mustJSON(t, StepContinueParams{StepID: 4}))
			var e *Error
			if !errors.As(err, &e) || e.Code != ErrStepNotPaused.Code {
				t.Fatalf("status=%q pauseReason=%q: got %v, want ErrStepNotPaused", tc.status, tc.pauseReason, err)
			}
			if !strings.Contains(e.Data.What, tc.status) {
				t.Errorf("the refusal does not name the actual status: %s", e.Data.What)
			}
		})
	}
}

func TestContinueRefusesAStepStillTrackedAsRunning(t *testing.T) {
	// The defensive second check: the ROW says suspended/user, but the live
	// registry still has it tracked — a state that should not happen, and
	// which must still refuse rather than start a second run on top of one
	// already in flight.
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user"}
	tbl, _ := stepTable(t, &scriptedRunner{}, steps)
	tbl.Questions = questions
	tbl.StepIsRunning = func(int64) bool { return true }
	initializedOnce(t, tbl, "c1")

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrStepNotPaused.Code {
		t.Fatalf("got %v, want ErrStepNotPaused", err)
	}
}

func TestContinueRejectsAnInvalidReasoningEffortBeforeTouchingTheStore(t *testing.T) {
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user"}
	tbl, _ := stepTable(t, &scriptedRunner{}, steps)
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4, ReasoningEffort: "extremely hard"}))
	if err == nil {
		t.Fatal("an invalid reasoning effort was accepted")
	}
	if !strings.Contains(err.Error(), "none, minimal, low, medium, high") {
		t.Errorf("the refusal does not list the accepted values: %v", err)
	}
	if questions.asked != 0 {
		t.Error("the step ran despite the bad input")
	}
}

func TestContinueUsesTheStepsOwnRouteWhenNoOverrideIsGiven(t *testing.T) {
	// Mirrors `answer`'s own resume: no override means "replay through this
	// route id's CURRENT config", which is what lets a `routes.set` edit made
	// while a step was paused reach it on continue.
	steps := &spyStepStore{}
	base := &scriptedRunner{}
	runner := &resumeSpy{scriptedRunner: base, entered: make(chan struct{}), release: make(chan struct{})}
	tbl, _ := stepTable(t, runner, steps, pricedRoute("r1", "anthropic"))

	req := runtime.StepRequest{Handler: runtime.DefaultHandler("x", []string{"read"}),
		WorkspaceRoot: t.TempDir(), Route: pricedRoute("r1", "openai") /* stale, on purpose */}
	questions := pausedQuestions(t, steps, req)
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")

	res, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue, mustJSON(t, StepContinueParams{StepID: 4}))
	if err != nil {
		t.Fatal(err)
	}
	if res.(AnswerResponse).Status != "running" {
		t.Fatalf("response: %+v", res)
	}
	close(runner.release)
	select {
	case <-questions.replaced:
	case <-time.After(2 * time.Second):
		t.Fatal("the continuation never ran")
	}

	runner.mu.Lock()
	provider := runner.route.Provider
	runner.mu.Unlock()
	if provider != "anthropic" {
		t.Errorf("the resumed route is %q, want the CURRENT config for route r1 (anthropic), not the stale openai", provider)
	}
}

func TestContinueOverridesTheRouteAndReasoningEffort(t *testing.T) {
	steps := &spyStepStore{}
	base := &scriptedRunner{}
	runner := &resumeSpy{scriptedRunner: base, entered: make(chan struct{}), release: make(chan struct{})}
	tbl, _ := stepTable(t, runner, steps,
		pricedRoute("r1", "anthropic"), pricedRoute("r2", "openai"))

	req := runtime.StepRequest{Handler: runtime.DefaultHandler("x", []string{"read"}),
		WorkspaceRoot: t.TempDir(), Route: pricedRoute("r1", "anthropic"), ReasoningEffort: "none"}
	questions := pausedQuestions(t, steps, req)
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue, mustJSON(t, StepContinueParams{
		StepID: 4, Route: "r2", ReasoningEffort: "medium",
	}))
	if err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	select {
	case <-questions.replaced:
	case <-time.After(2 * time.Second):
		t.Fatal("the continuation never ran")
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.route.ID != "r2" || runner.route.Provider != "openai" {
		t.Errorf("the resumed route is %+v, want r2/openai", runner.route)
	}
	if runner.lastReq.Request.ReasoningEffort != "medium" {
		t.Errorf("the resumed reasoning effort is %q, want medium", runner.lastReq.Request.ReasoningEffort)
	}
}

func TestContinueMarksTheStepRunningSoAnswerRefusesIt(t *testing.T) {
	// This is `MarkRunning`'s OTHER effect (clearing pause_reason is tested at
	// the store layer); here it is enough that it was called, which is what
	// makes the step look "running" to everything downstream.
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user",
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, runtime.StepRequest{
				Handler: runtime.DefaultHandler("x", []string{"read"}), WorkspaceRoot: t.TempDir(),
				Route: pricedRoute("r1", "anthropic"),
			}),
		}}
	tbl, _ := stepTable(t, &resumeSpy{scriptedRunner: &scriptedRunner{}, entered: make(chan struct{}), release: closedChan(t)}, steps)
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4})); err != nil {
		t.Fatal(err)
	}
	if !questions.markedRunning {
		t.Fatal("MarkRunning was never called")
	}
}

func closedChan(t *testing.T) chan struct{} {
	t.Helper()
	ch := make(chan struct{})
	close(ch)
	return ch
}

// --- S31 T2 (D6): `step.continue {message}` — the stop-and-inject half -------

func TestContinueWithMessageFlipsAndDeliversThroughTheAtomicMethod(t *testing.T) {
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user",
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, runtime.StepRequest{
				Handler: runtime.DefaultHandler("x", []string{"read"}), WorkspaceRoot: "/ws",
				Route: pricedRoute("r1", "anthropic"),
			}),
			// Ends with a USER turn (the shape a stop right after tool
			// results leaves): the message must MERGE into it, not stack a
			// second consecutive user message (the same provider-alternation
			// rule S31's steer injection follows).
			Messages: []model.Message{
				{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
				{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("half done")}},
				{Role: model.RoleUser, Content: []model.Block{model.TextBlock("read ok")}},
			},
		}}
	root := "/ws"
	tbl, _ := stepTable(t, &resumeSpy{scriptedRunner: &scriptedRunner{}, entered: make(chan struct{}), release: closedChan(t)}, steps)
	tbl.Questions = questions
	tbl.LookupLoopID = func(slug, componentRoot string) string {
		if slug == "step" && componentRoot == root {
			return "loop-9"
		}
		return ""
	}
	var events []Event
	tbl.OnEvent = func(kind string, payload any) {
		events = append(events, Event{Kind: kind})
	}
	initializedOnce(t, tbl, "c1")

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4, Message: "  use the table form  "})); err != nil {
		t.Fatal(err)
	}

	// Exactly-once rides on the SINGLE store call — the transcript AND the
	// flip go through ContinueWithMessages, not MarkRunning.
	if len(questions.continued) != 1 {
		t.Fatalf("ContinueWithMessages called %d times, want exactly 1", len(questions.continued))
	}
	if !questions.markedRunning {
		t.Fatal("the atomic flip must count as marking running")
	}
	got := questions.continued[0]
	if len(got) != 3 {
		t.Fatalf("messages = %d, want 3 (merged into the trailing user turn, not appended)", len(got))
	}
	last := got[2]
	if last.Role != model.RoleUser || len(last.Content) != 2 ||
		last.Content[0].Text != "read ok" ||
		last.Content[1].Kind != model.BlockText || last.Content[1].Text != "use the table form" {
		t.Fatalf("the trailing user turn is %+v, want the original text THEN the trimmed person text", last.Content)
	}
}

func TestContinueWithMessageAppendsWhenTheTranscriptEndsWithAssistant(t *testing.T) {
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user",
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, runtime.StepRequest{
				Handler: runtime.DefaultHandler("x", []string{"read"}), WorkspaceRoot: "/ws",
				Route: pricedRoute("r1", "anthropic"),
			}),
			Messages: []model.Message{
				{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
				{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("stopped here")}},
			},
		}}
	tbl, _ := stepTable(t, &resumeSpy{scriptedRunner: &scriptedRunner{}, entered: make(chan struct{}), release: closedChan(t)}, steps)
	tbl.Questions = questions
	var stepContinued []any
	tbl.OnEvent = func(kind string, payload any) {
		if kind == "step.continued" {
			stepContinued = append(stepContinued, payload)
		}
	}
	initializedOnce(t, tbl, "c1")

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4, Message: "and mention the Go version"})); err != nil {
		t.Fatal(err)
	}
	got := questions.continued[0]
	if len(got) != 3 || got[2].Role != model.RoleUser || got[2].Content[0].Text != "and mention the Go version" {
		t.Fatalf("want a NEW trailing user turn with the text, got %+v", got)
	}
	// Brief §5: `step.continued` carries stepId (+ loopId?) — and it is the
	// identity envelope (agent, root) rather than a route restatement.
	if len(stepContinued) != 1 {
		t.Fatalf("step.continued emitted %d times, want 1", len(stepContinued))
	}
	evp, ok := stepContinued[0].(StepEventPayload)
	if !ok || evp.StepID != 4 || evp.Agent != runtime.DefaultHandlerName || evp.LoopID != "" || evp.componentRoot != "/ws" {
		t.Fatalf("step.continued payload = %+v (root %q), want stepId 4, the ad-hoc agent, no loop (LookupLoopID unset), root /ws",
			evp, evp.componentRoot)
	}
}

func TestContinueMessageRefusalNamesTheStoreCondition(t *testing.T) {
	steps := &spyStepStore{}
	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user",
		continueErr: errors.New("the step is no longer suspended (affected 0)"),
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, runtime.StepRequest{
				Handler: runtime.DefaultHandler("x", []string{"read"}), WorkspaceRoot: "/ws",
				Route: pricedRoute("r1", "anthropic"),
			}),
			Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}},
		}}
	tbl, _ := stepTable(t, &resumeSpy{scriptedRunner: &scriptedRunner{}, entered: make(chan struct{}), release: closedChan(t)}, steps)
	tbl.Questions = questions
	initializedOnce(t, tbl, "c1")

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 4, Message: "sneak in"}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrResumeUnavailable.Code {
		t.Fatalf("got %v, want ErrResumeUnavailable", err)
	}
	if !strings.Contains(e.Data.What, "no longer suspended") {
		t.Errorf("the refusal must carry the store's condition: %s", e.Data.What)
	}
}

// --- the end-to-end round trip: stop, continue, stop again ------------------

func TestAContinuedStepCanBeStoppedAgain(t *testing.T) {
	steps := &spyStepStore{}
	runnerA := newBlockingRunner()
	tbl, _ := stepTable(t, StepRunner(&scriptedRunner{}), steps)
	tbl.Runner = stepRunnerAdapter{runnerA}

	done := make(chan struct{})
	go func() { callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}); close(done) }()
	<-runnerA.entered
	if !tbl.CancelStep(1) {
		t.Fatal("the first stop found nothing")
	}
	close(runnerA.release)
	<-done

	if steps.finalStatus != "suspended" || steps.pauseReason != "user" {
		t.Fatalf("after the first stop: status=%q pauseReason=%q", steps.finalStatus, steps.pauseReason)
	}

	// Now continue it, through a SECOND blocking run, and stop THAT one too.
	runnerB := newBlockingRunner()
	resumeAdapter := &resumeRunnerAdapter{runnerB}
	tbl.Runner = resumeAdapter

	questions := &questionSpy{steps: steps, status: "suspended", pauseReason: "user",
		resume: ResumeSnapshot{
			Manifest: continueSnapshot(t, runtime.StepRequest{
				Handler: runtime.DefaultHandler("x", []string{"read"}), WorkspaceRoot: "/ws",
				Route: pricedRoute("r1", "anthropic"),
			}),
		}}
	tbl.Questions = questions

	// S31 T2 (D6): when this SECOND run is stopped too, the handback to the
	// loop must say `stopped` — not the runtime's generic cancelled kind,
	// which the loop's advance would read as a completed step and move the
	// ladder on for a step that never finished.
	var handedKind string
	handed := make(chan struct{}, 1)
	tbl.OnStepContinued = func(_ int64, kind, _, _ string, _ int64, _ json.RawMessage, _ string) {
		handedKind = kind
		select {
		case handed <- struct{}{}:
		default:
		}
	}

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodStepContinue,
		mustJSON(t, StepContinueParams{StepID: 1})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runnerB.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the continuation never started running")
	}
	if !tbl.CancelStep(1) {
		t.Fatal("the second stop found nothing — the continuation was not tracked")
	}
	close(runnerB.release)

	// Give the detached goroutine a moment to persist the second stop. Eight
	// writes total: CreateRunningStep/AppendStepMessages/SaveStepContext/
	// SetPauseReason/FinishStep for the first run, then
	// AppendStepMessages(none, resume path skips it)/SaveStepContext/
	// SetPauseReason/FinishStep for the second — the count, not a specific
	// last entry, is the robust thing to wait for.
	deadline := time.Now().Add(2 * time.Second)
	for len(steps.order()) < 8 || steps.finalStatus != "suspended" {
		if time.Now().After(deadline) {
			t.Fatalf("the second stop was never persisted: writes=%v status=%q",
				steps.order(), steps.finalStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if steps.pauseReason != "user" {
		t.Errorf("the final pause reason is %q, want user", steps.pauseReason)
	}
	if !runnerB.wasCancelled() {
		t.Fatal("the continuation's own context was never cancelled")
	}
	select {
	case <-handed:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop handback never came for the second stop")
	}
	if handedKind != "stopped" {
		t.Fatalf("the handback kind is %q, want stopped — anything else lets the loop move the ladder", handedKind)
	}
}

// resumeRunnerAdapter lets a *blockingRunner stand in for `resumeRunner` too.
type resumeRunnerAdapter struct{ b *blockingRunner }

func (a *resumeRunnerAdapter) Compile(_ context.Context, in runtime.CompileInput, deps runtime.ContextDeps) runtime.CompileResult {
	return a.b.scriptedRunner.Compile(context.Background(), in, deps)
}
func (a *resumeRunnerAdapter) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	return a.b.Run(ctx, req, opts)
}
func (a *resumeRunnerAdapter) BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error) {
	return a.b.scriptedRunner.BuildTools(granted, workspaceRoot, agent, frozen)
}
func (a *resumeRunnerAdapter) Resume(ctx context.Context, req runtime.ResumeRequest, opts runtime.RunOpts) runtime.StepResult {
	close(a.b.entered)
	select {
	case <-ctx.Done():
		a.b.mu.Lock()
		a.b.cancelled = true
		a.b.mu.Unlock()
	case <-a.b.release:
	}
	a.b.mu.Lock()
	out := a.b.scriptedRunner.result
	a.b.mu.Unlock()
	if out.Kind == "" {
		out.Kind = runtime.ResultComplete
		out.Outcome = runtime.OutcomeOK
	}
	return out
}
