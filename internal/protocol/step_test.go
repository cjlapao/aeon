package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// T10's tests. The one that matters for S7 is the ORDER: the row exists before
// the work starts.

// scriptedRunner is the runtime, scripted. No provider and no real toolset —
// this package's tests are about the handler's bookkeeping.
type scriptedRunner struct {
	mu sync.Mutex

	// result is what Run returns.
	result runtime.StepResult

	// compileFails makes Compile refuse, for the construction-error path.
	compileFails string

	// buildToolsFails makes BuildTools refuse, for the unknown-tool path.
	buildToolsFails error

	// recorded is what the handler asked for.
	compiled []runtime.CompileInput
	granted  [][]string
	ran      int

	// lastOpts/lastReq capture the FINAL Run call's dependencies and request
	// (S10 review, DEF-034: the gate-forwarding assertion has to read what
	// the production handler actually built into RunOpts — a scripted runner
	// that dropped opts on the floor could not fail a missing forward).
	lastOpts runtime.RunOpts
	lastReq  runtime.StepRequest

	// onRun runs inside Run, so a test can observe the world mid-step.
	onRun func()
}

func (r *scriptedRunner) Compile(_ context.Context, in runtime.CompileInput, deps runtime.ContextDeps) runtime.CompileResult {
	r.mu.Lock()
	r.compiled = append(r.compiled, in)
	r.mu.Unlock()

	if r.compileFails != "" {
		return runtime.CompileResult{Reason: r.compileFails}
	}
	return runtime.CompileResult{OK: true, Request: runtime.StepRequest{
		Handler:       in.Handler,
		Route:         in.Route,
		WorkspaceRoot: in.WorkspaceRoot,
		Tools:         in.ToolSpecs,
		Manifest:      runtime.ContextManifest{MaxIterations: 12, IterationsUsed: 2},
	}}
}

func (r *scriptedRunner) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	r.mu.Lock()
	r.ran++
	r.lastOpts = opts
	r.lastReq = req
	hook := r.onRun
	out := r.result
	r.mu.Unlock()

	if hook != nil {
		hook()
	}
	if opts.Sink != nil {
		// One of each, so the sink's wiring is exercised rather than assumed.
		opts.Sink.Emit(runtime.StepEvent{Kind: runtime.EventToolStart, Tool: "read", Iteration: 1})
		opts.Sink.Emit(runtime.StepEvent{Kind: runtime.EventToolEnd, Tool: "read",
			Result: "read a.md", Iteration: 1})
	}
	if out.Kind == "" {
		out.Kind = runtime.ResultComplete
		out.Outcome = runtime.OutcomeOK
	}
	out.Manifest.IterationsUsed = 2
	return out
}

func (r *scriptedRunner) BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error) {
	r.mu.Lock()
	r.granted = append(r.granted, granted)
	r.mu.Unlock()

	if r.buildToolsFails != nil {
		return nil, nil, r.buildToolsFails
	}
	specs := make([]model.ToolSpec, 0, len(granted))
	for _, name := range granted {
		specs = append(specs, model.ToolSpec{Name: name, Description: name,
			Parameters: json.RawMessage(`{"type":"object"}`)})
	}
	return nil, specs, nil
}

// spyStepStore records every write, in order.
type spyStepStore struct {
	mu sync.Mutex

	// writes is the sequence of method names, which is what the order
	// assertion reads.
	writes []string

	nextID       int64
	createErr    error
	appendErr    error
	contextErr   error
	finishErr    error
	pauseErr     error
	messages     []model.Message
	manifest     json.RawMessage
	finalStatus  string
	finalOutcome string
	finalCost    model.Cost
	pauseReason  string
}

func (s *spyStepStore) CreateRunningStep(loopSlug, componentRoot, agent string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, "CreateRunningStep")
	if s.createErr != nil {
		return 0, s.createErr
	}
	s.nextID++
	return s.nextID, nil
}

func (s *spyStepStore) AppendStepMessages(stepID int64, msgs []model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, "AppendStepMessages")
	s.messages = msgs
	return s.appendErr
}

func (s *spyStepStore) SaveStepContext(stepID int64, manifest json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, "SaveStepContext")
	s.manifest = manifest
	return s.contextErr
}

func (s *spyStepStore) FinishStep(stepID int64, status, outcome string, cost model.Cost) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, "FinishStep")
	s.finalStatus, s.finalOutcome, s.finalCost = status, outcome, cost
	return s.finishErr
}

func (s *spyStepStore) SetPauseReason(stepID int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, "SetPauseReason")
	s.pauseReason = reason
	return s.pauseErr
}

func (s *spyStepStore) order() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.writes...)
}

// stepTable wires a table for the step handler.
func stepTable(t *testing.T, runner StepRunner, steps StepStore, routes ...model.Route) (*Table, *eventLog) {
	t.Helper()
	if len(routes) == 0 {
		routes = []model.Route{pricedRoute("r1", "anthropic")}
	}
	tbl, events := askTable(t, &fakeStreamer{}, routes...)
	tbl.Runner = runner
	tbl.Steps = steps
	wireFakeStepTracking(tbl)
	return tbl, events
}

// fakeStepTracker is a minimal, test-local stand-in for the daemon's real
// `stepCancels` (internal/daemon/stepcancel.go) — duplicated rather than
// imported, because `internal/daemon` depends on `internal/protocol` and not
// the other way around. It exists so step_test.go's tables satisfy
// `handleStep`'s refusal when `TrackStep` is nil, without pulling stop/cancel
// semantics into every test that does not care about them.
//
// `TestStepStopActuallyCancelsARunningSteps[Context]`-shaped tests (in
// step_stop_test.go) use a FRESH tracker per table instead, so they can
// assert on cancellation directly; this one is for every OTHER test, which
// only needs the hooks present.
type fakeStepTracker struct {
	mu   sync.Mutex
	byID map[int64]context.CancelFunc
	stop map[int64]bool
}

func newFakeStepTracker() *fakeStepTracker {
	return &fakeStepTracker{byID: map[int64]context.CancelFunc{}, stop: map[int64]bool{}}
}

func (f *fakeStepTracker) track(parent context.Context, id int64) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	f.mu.Lock()
	f.byID[id] = cancel
	f.mu.Unlock()
	return ctx, func() {
		f.mu.Lock()
		delete(f.byID, id)
		f.mu.Unlock()
	}
}

func (f *fakeStepTracker) cancel(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return false
	}
	f.stop[id] = true
	c()
	return true
}

func (f *fakeStepTracker) wasStopped(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	stopped := f.stop[id]
	f.stop[id] = false
	return stopped
}

func wireFakeStepTracking(tbl *Table) {
	tracker := newFakeStepTracker()
	tbl.TrackStep = tracker.track
	tbl.CancelStep = tracker.cancel
	tbl.StepWasStopped = tracker.wasStopped
}

func callStep(t *testing.T, tbl *Table, p StepParams) (StepResult, error) {
	t.Helper()
	initializedOnce(t, tbl, "c1")
	res, err := tbl.Dispatch(context.Background(), "c1", MethodStep, mustJSON(t, p))
	if err != nil {
		return StepResult{}, err
	}
	out, ok := res.(StepResult)
	if !ok {
		t.Fatalf("step returned %T", res)
	}
	return out, nil
}

func TestTheStepRowIsWrittenBeforeTheWorkStarts(t *testing.T) {
	// **This is what makes S7 possible.** A row written only on completion
	// means a crashed step leaves no trace, and S7's recovery query is
	// `status='running'` — it finds nothing if nothing was written before the
	// work began.
	//
	// Asserted by observing the store FROM INSIDE Run: at the moment the
	// runtime is working, the create has already happened and nothing else
	// has.
	steps := &spyStepStore{}
	var duringRun []string
	runner := &scriptedRunner{onRun: func() { duringRun = steps.order() }}

	tbl, _ := stepTable(t, runner, steps)
	if _, err := callStep(t, tbl, StepParams{Prompt: "count the files", WorkspaceRoot: "/ws"}); err != nil {
		t.Fatal(err)
	}

	if len(duringRun) != 1 || duringRun[0] != "CreateRunningStep" {
		t.Fatalf("while the step was running the store had seen %v, want exactly [CreateRunningStep]",
			duringRun)
	}

	// And the full order afterwards: row, transcript, manifest, status.
	want := []string{"CreateRunningStep", "AppendStepMessages", "SaveStepContext", "FinishStep"}
	got := steps.order()
	if len(got) != len(want) {
		t.Fatalf("writes were %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("write %d is %s, want %s (full order %v)", i, got[i], want[i], got)
		}
	}
}

func TestTheStepIdComesBackImmediatelyWithTheOutcome(t *testing.T) {
	// The id, because a step takes tens of seconds and a client that lost its
	// connection would lose the answer with it. The outcome too, because a
	// script calling the method once should not have to subscribe.
	steps := &spyStepStore{}
	runner := &scriptedRunner{result: runtime.StepResult{
		Kind:    runtime.ResultComplete,
		Outcome: runtime.OutcomeOK,
		Usage:   model.Usage{InputTokens: model.IntPtr(10), OutputTokens: model.IntPtr(5)},
		Cost:    model.Cost{State: model.CostKnown, USD: 0.002},
	}}

	tbl, _ := stepTable(t, runner, steps)
	out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err != nil {
		t.Fatal(err)
	}

	if out.StepID == 0 {
		t.Error("no step id came back")
	}
	if out.Outcome != "ok" {
		t.Errorf("outcome is %q", out.Outcome)
	}
	if out.Kind != "complete" {
		t.Errorf("kind is %q", out.Kind)
	}
	if out.Iterations != 2 {
		t.Errorf("iterations is %d", out.Iterations)
	}
	if out.Usage.InputTokens == nil || *out.Usage.InputTokens != 10 {
		t.Errorf("usage did not survive: %+v", out.Usage)
	}
	if out.Cost.State != model.CostKnown {
		t.Errorf("cost state is %q", out.Cost.State)
	}
}

func TestADaemonWithNoRuntimeSaysSoRatherThanPanicking(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Routes = NewStaticRoutes(pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStep,
		mustJSON(t, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}))
	if err == nil {
		t.Fatal("a step on a runtime-less daemon succeeded")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *protocol.Error, got %T", err)
	}
	if e.Code != ErrStepUnavailable.Code {
		t.Errorf("code %d, want %d", e.Code, ErrStepUnavailable.Code)
	}
}

func TestADaemonWithNoStoreRefusesTheStepRatherThanRunningItUnrecorded(t *testing.T) {
	// A step that ran and spent money with nowhere to record it is worse than
	// a step that did not run: the person pays and has nothing to read.
	tbl := newTestTable(t)
	tbl.Routes = NewStaticRoutes(pricedRoute("r1", "anthropic"))
	tbl.Runner = &scriptedRunner{}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodStep,
		mustJSON(t, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}))
	if err == nil {
		t.Fatal("a step on a store-less daemon ran")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrStoreUnavailable.Code {
		t.Errorf("error is %v, want ErrStoreUnavailable", err)
	}
}

func TestAStepWithNoWorkspaceRootIsRefusedAndNamesWhy(t *testing.T) {
	// Q10. The daemon multiplexes projects, so its own working directory is
	// not the client's — a step defaulting to it would read the daemon's files
	// and look like it had worked.
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	_, err := callStep(t, tbl, StepParams{Prompt: "x"})
	if err == nil {
		t.Fatal("a step with no workspace root ran")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *protocol.Error, got %T", err)
	}
	if e.Code != ErrNoWorkspace.Code {
		t.Errorf("code %d, want %d", e.Code, ErrNoWorkspace.Code)
	}
	if runner.ran != 0 {
		t.Error("the step ran anyway")
	}
}

func TestAnEmptyPromptIsRefused(t *testing.T) {
	tbl, _ := stepTable(t, &scriptedRunner{}, &spyStepStore{})
	for _, prompt := range []string{"", "   ", "\n\t"} {
		if _, err := callStep(t, tbl, StepParams{Prompt: prompt, WorkspaceRoot: "/ws"}); err == nil {
			t.Errorf("a step with prompt %q ran", prompt)
		}
	}
}

func TestTheDefaultGrantContainsWorkspaceFileToolsAndNoBash(t *testing.T) {
	// D1 at the protocol boundary: a request that names no tools gets the
	// workspace file tools.
	// **`bash` is never granted by default** — a caller asks for it
	// explicitly.
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	if _, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}); err != nil {
		t.Fatal(err)
	}

	if len(runner.granted) != 1 {
		t.Fatalf("BuildTools was called %d times", len(runner.granted))
	}
	granted := runner.granted[0]
	if strings.Join(granted, ",") != "read,list,write" {
		t.Errorf("the default grant reaching the toolset is %v", granted)
	}
	// The same list reaches Compile, because a step whose permission and whose
	// declaration disagree is a step that fails at turn three.
	if got := runner.compiled[0].Granted; strings.Join(got, ",") != "read,list,write" {
		t.Errorf("Compile was given %v", got)
	}
}

func TestAnExplicitGrantReachesBothTheToolsetAndTheCompiler(t *testing.T) {
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	if _, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", Tools: []string{"read", "bash"},
	}); err != nil {
		t.Fatal(err)
	}

	if got := runner.granted[0]; len(got) != 2 {
		t.Errorf("the toolset was built with %v", got)
	}
	if got := runner.compiled[0].Granted; len(got) != 2 {
		t.Errorf("the compiler was given %v", got)
	}
	// And the specs built from the grant are what the model is told exists.
	if len(runner.compiled[0].ToolSpecs) != 2 {
		t.Errorf("the model would be offered %d tools", len(runner.compiled[0].ToolSpecs))
	}
}

func TestAnUnknownToolIsANamedErrorAndNothingRuns(t *testing.T) {
	runner := &scriptedRunner{buildToolsFails: errors.New(
		"tools: the grant list names wrtie, which do not exist. The tools that do are bash, read")}
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, runner, steps)

	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", Tools: []string{"wrtie"},
	})
	if err == nil {
		t.Fatal("a step with an unknown tool ran")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *protocol.Error, got %T", err)
	}
	if e.Code != ErrUnknownTool.Code {
		t.Errorf("code %d, want %d", e.Code, ErrUnknownTool.Code)
	}
	// The construction error reaches the caller verbatim, because it already
	// names the typo and lists what exists.
	if !strings.Contains(e.Data.What, "wrtie") {
		t.Errorf("the refusal does not name the typo: %q", e.Data.What)
	}
	// NOTHING was written. A step that was never going to run should not
	// leave a row.
	if len(steps.order()) != 0 {
		t.Errorf("the store was written to: %v", steps.order())
	}
	if runner.ran != 0 {
		t.Error("the step ran")
	}
}

func TestAConstructionFailureHappensBeforeAnyRowOrAnyModelCall(t *testing.T) {
	// T3's property, observed from the handler: the failure costs nothing.
	runner := &scriptedRunner{compileFails: "step:ad-hoc declares `bash`, which this step was not granted"}
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, runner, steps)

	_, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err == nil {
		t.Fatal("a step that could not be compiled ran")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrStepNotCompiled.Code {
		t.Fatalf("error is %v, want ErrStepNotCompiled", err)
	}
	if !strings.Contains(e.Data.What, "bash") {
		t.Errorf("the reason did not reach the caller: %q", e.Data.What)
	}
	if len(steps.order()) != 0 {
		t.Errorf("a row was written for a step that never ran: %v", steps.order())
	}
	if runner.ran != 0 {
		t.Error("the step ran")
	}
}

func TestOneConfiguredRouteIsTakenAndSeveralAreRefused(t *testing.T) {
	// One route and no choice: take it, which is what lets the demo be a
	// single command on a fresh machine. Several: refuse, because picking one
	// would spend money on a model the person did not choose.
	t.Run("one route is taken", func(t *testing.T) {
		runner := &scriptedRunner{}
		tbl, _ := stepTable(t, runner, &spyStepStore{}, pricedRoute("only", "anthropic"))
		if _, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}); err != nil {
			t.Fatal(err)
		}
		if got := runner.compiled[0].Route.ID; got != "only" {
			t.Errorf("the route used was %q", got)
		}
	})

	t.Run("several are refused", func(t *testing.T) {
		runner := &scriptedRunner{}
		tbl, _ := stepTable(t, runner, &spyStepStore{},
			pricedRoute("a", "anthropic"), pricedRoute("b", "openai"))
		_, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
		if err == nil {
			t.Fatal("a step with two routes and no choice ran")
		}
		if !strings.Contains(err.Error(), "guessing spends money") &&
			!strings.Contains(errorWhat(err), "guessing spends money") {
			t.Errorf("the refusal does not explain why: %v", err)
		}
		if runner.ran != 0 {
			t.Error("the step ran")
		}
	})

	t.Run("a named route is used", func(t *testing.T) {
		runner := &scriptedRunner{}
		tbl, _ := stepTable(t, runner, &spyStepStore{},
			pricedRoute("a", "anthropic"), pricedRoute("b", "openai"))
		if _, err := callStep(t, tbl, StepParams{
			Prompt: "x", WorkspaceRoot: "/ws", Route: "b",
		}); err != nil {
			t.Fatal(err)
		}
		if got := runner.compiled[0].Route.ID; got != "b" {
			t.Errorf("the route used was %q", got)
		}
	})

	t.Run("an unknown route lists the real ones", func(t *testing.T) {
		tbl, _ := stepTable(t, &scriptedRunner{}, &spyStepStore{}, pricedRoute("a", "anthropic"))
		_, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws", Route: "nope"})
		if err == nil {
			t.Fatal("an unknown route ran")
		}
		if !strings.Contains(errorWhat(err), "a") {
			t.Errorf("the refusal does not list the configured routes: %v", err)
		}
	})
}

func errorWhat(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Data.What
	}
	return err.Error()
}

func TestAnUnknownSubscriptionIsRefusedBeforeTheStepRuns(t *testing.T) {
	// T10's point 4. Silently ignoring it leaves the client waiting for a
	// stream that never arrives, which reads as a hung step — and the step
	// would have run for a minute first.
	runner := &scriptedRunner{}
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, runner, steps)

	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", SubscriptionID: "sub-that-does-not-exist",
	})
	if err == nil {
		t.Fatal("a step with an unknown subscription ran")
	}
	if !strings.Contains(errorWhat(err), "sub-that-does-not-exist") {
		t.Errorf("the refusal does not name the subscription: %v", err)
	}
	if runner.ran != 0 {
		t.Error("the step ran before the subscription was checked")
	}
	if len(steps.order()) != 0 {
		t.Errorf("a row was written: %v", steps.order())
	}
}

func TestStepEventsGoThroughTheOrdinarySubscriptionPath(t *testing.T) {
	// K24/D17. S2 already has subscriptions, filters and a cursor; a second
	// streaming path would mean two things to subscribe to and two places for
	// a client to miss something.
	runner := &scriptedRunner{}
	tbl, events := stepTable(t, runner, &spyStepStore{})

	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	subRes, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"step.*"}}}))
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := subRes.(SubscribeResult)
	if !ok {
		t.Fatalf("subscribe returned %T", subRes)
	}

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodStep, mustJSON(t, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", SubscriptionID: sub.SubscriptionID,
	})); err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, e := range events.all() {
		if strings.HasPrefix(e.Kind, "step.") {
			kinds = append(kinds, e.Kind)
		}
	}
	if len(kinds) != 2 {
		t.Fatalf("the step published %v, want the two the runner emitted", kinds)
	}
	if kinds[0] != runtime.EventToolStart || kinds[1] != runtime.EventToolEnd {
		t.Errorf("the kinds are %v", kinds)
	}

	// The payload carries the tool and the result, because an event naming no
	// tool tells a watcher nothing.
	var payload StepEventPayload
	for _, e := range events.all() {
		if e.Kind == runtime.EventToolEnd {
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if payload.Tool != "read" || payload.Result != "read a.md" {
		t.Errorf("the payload is %+v", payload)
	}
}

func TestAStepWithNoSubscriptionStillRunsAndStillAnswers(t *testing.T) {
	// A step nobody is watching is an ordinary case: a script calling the
	// method once has no subscription and wants the outcome.
	runner := &scriptedRunner{}
	tbl, events := stepTable(t, runner, &spyStepStore{})

	out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "ok" {
		t.Errorf("outcome is %q", out.Outcome)
	}
	for _, e := range events.all() {
		if strings.HasPrefix(e.Kind, "step.") {
			t.Errorf("a step with no subscription published %s", e.Kind)
		}
	}
}

func TestEverythingTheStepProducedIsPersisted(t *testing.T) {
	// The transcript, the manifest and the cost. Each is something a later
	// slice reads: S7 replays the transcript, `step verify` checks the
	// manifest, and the cost is what `db info` totals.
	steps := &spyStepStore{}
	handoff := runtime.Handoff{"status": "ok", "summary": "did it"}
	runner := &scriptedRunner{result: runtime.StepResult{
		Kind:    runtime.ResultComplete,
		Outcome: runtime.OutcomeOK,
		Handoff: &handoff,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do it")}},
			{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("done")}},
		},
		Cost: model.Cost{State: model.CostPartial, USD: 0.01, Missing: []string{"cacheWrite"}},
	}}

	tbl, _ := stepTable(t, runner, steps)
	out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err != nil {
		t.Fatal(err)
	}

	if len(steps.messages) != 2 {
		t.Errorf("%d messages were persisted", len(steps.messages))
	}
	if len(steps.manifest) == 0 {
		t.Error("no manifest was persisted")
	}
	var manifest runtime.ContextManifest
	if err := json.Unmarshal(steps.manifest, &manifest); err != nil {
		t.Fatalf("the persisted manifest is not valid JSON: %v", err)
	}
	if manifest.IterationsUsed != 2 {
		t.Errorf("the persisted manifest reports %d iterations", manifest.IterationsUsed)
	}

	// The cost's STATE survives the trip to the store (S3's D12). A `partial`
	// collapsed to a number is a row claiming a price nobody measured.
	if steps.finalCost.State != model.CostPartial {
		t.Errorf("the persisted cost state is %q", steps.finalCost.State)
	}
	if len(steps.finalCost.Missing) != 1 {
		t.Errorf("the missing list did not survive: %v", steps.finalCost.Missing)
	}
	if steps.finalOutcome != "ok" {
		t.Errorf("the persisted outcome is %q", steps.finalOutcome)
	}

	// And the handoff comes back to the caller as JSON.
	if len(out.Handoff) == 0 {
		t.Error("the handoff did not reach the caller")
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Handoff, &decoded); err != nil {
		t.Fatalf("the handoff is not valid JSON: %v", err)
	}
	if decoded["summary"] != "did it" {
		t.Errorf("the handoff is %v", decoded)
	}
}

func TestAFailedPersistIsReportedAndDoesNotLoseTheRest(t *testing.T) {
	// The step ran and spent money. Losing the record of it because one write
	// failed is the worse outcome — so each failure is reported and none
	// aborts the others.
	steps := &spyStepStore{
		appendErr:  errors.New("disk full"),
		contextErr: errors.New("also disk full"),
	}
	tbl, _ := stepTable(t, &scriptedRunner{}, steps)

	out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err != nil {
		t.Fatalf("a persist failure became a method error: %v", err)
	}
	if out.StepID == 0 {
		t.Error("the step id was lost")
	}
	if out.Outcome != "ok" {
		t.Errorf("the outcome was lost: %q", out.Outcome)
	}
	if !strings.Contains(out.Reason, "disk full") {
		t.Errorf("the reason does not report the failed writes: %q", out.Reason)
	}
	// FinishStep still ran, which is the write that stops the row looking
	// crashed to S7.
	order := steps.order()
	if order[len(order)-1] != "FinishStep" {
		t.Errorf("the final status was not written: %v", order)
	}
}

func TestAStepThatCannotBeRecordedDoesNotRun(t *testing.T) {
	// The create is the one write that must succeed. Running anyway would
	// spend money with nowhere to put the answer.
	runner := &scriptedRunner{}
	steps := &spyStepStore{createErr: errors.New("the store is read-only")}
	tbl, _ := stepTable(t, runner, steps)

	_, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err == nil {
		t.Fatal("a step that could not be recorded ran")
	}
	if runner.ran != 0 {
		t.Error("the runtime was called anyway")
	}
	if !strings.Contains(errorWhat(err), "read-only") {
		t.Errorf("the error does not say why: %v", err)
	}
}

func TestSinkFailuresReachTheCaller(t *testing.T) {
	// A client seeing no events and a successful result would otherwise have
	// no way to tell which half broke.
	runner := &scriptedRunner{result: runtime.StepResult{
		Kind: runtime.ResultComplete, Outcome: runtime.OutcomeOK, SinkFailures: 3,
	}}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
	if err != nil {
		t.Fatal(err)
	}
	if out.SinkFailures != 3 {
		t.Errorf("sinkFailures is %d, want 3", out.SinkFailures)
	}
}

func TestEveryOutcomeComesBackDistinguishable(t *testing.T) {
	// Four outcomes, four answers. A `reject` that looked like a `failed`
	// would make a handler's considered no into a bug report.
	for _, outcome := range runtime.Outcomes() {
		t.Run(string(outcome), func(t *testing.T) {
			steps := &spyStepStore{}
			runner := &scriptedRunner{result: runtime.StepResult{
				Kind: runtime.ResultComplete, Outcome: outcome, Reason: "because",
			}}
			tbl, _ := stepTable(t, runner, steps)

			out, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
			if err != nil {
				t.Fatal(err)
			}
			if out.Outcome != string(outcome) {
				t.Errorf("outcome is %q, want %q", out.Outcome, outcome)
			}
			if steps.finalOutcome != string(outcome) {
				t.Errorf("the persisted outcome is %q", steps.finalOutcome)
			}
		})
	}
}

func TestTheWorkspaceRootIsWhatTheToolsAndTheRowGet(t *testing.T) {
	// One source for the workspace root: the request. No tool reads ambient
	// state (I5), and the row records where the step ran.
	runner := &scriptedRunner{}
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, runner, steps)

	if _, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/the/clients/project",
	}); err != nil {
		t.Fatal(err)
	}

	if got := runner.compiled[0].WorkspaceRoot; got != "/the/clients/project" {
		t.Errorf("the compiler was given %q", got)
	}
}

func TestMaxIterationsIsCarriedToTheCompiler(t *testing.T) {
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	if _, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", MaxIterations: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if got := runner.compiled[0].MaxIterations; got != 3 {
		t.Errorf("MaxIterations reaching the compiler is %d", got)
	}
}

func TestTheStepHandlerIsTheOnlyWriterInThisPackage(t *testing.T) {
	// K22. The writes are four methods on a narrow interface, and the handler
	// is the only caller — so "the handler can persist anything" is not
	// possible by construction, and "something else persists too" is caught
	// here.
	//
	// Tokenized rather than grepped, for this package's usual reason: the
	// interface's own doc comment names every method, and a text search reads
	// prose explaining a rule as a violation of it.
	files, _ := filepath.Glob("*.go")
	if len(files) == 0 {
		t.Fatal("no source files found; this test would pass vacuously")
	}

	writes := []string{"CreateRunningStep", "AppendStepMessages", "SaveStepContext", "FinishStep"}
	found := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		ids := identifiersIn(t, file)
		for _, write := range writes {
			if usesIdentifier(ids, "Steps."+write) {
				found[file] = append(found[file], write)
			}
		}
	}

	for file, calls := range found {
		if file != "step.go" {
			t.Errorf("%s writes through StepStore (%v); the step handler is the only writer", file, calls)
		}
	}
	if len(found["step.go"]) != len(writes) {
		t.Errorf("step.go calls %v, want all four writes — a write that moved is a write nothing "+
			"guards", found["step.go"])
	}
}

// --- the step's reasoning effort ---------------------------------------------

func TestTheStepsReasoningEffortReachesTheCompiler(t *testing.T) {
	// The per-step half of the dial. A chat and a step want different things
	// from the same route, and neither should have to keep its own.
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	if _, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}
	if got := runner.compiled[0].ReasoningEffort; got != "none" {
		t.Errorf("the compiler was given %q", got)
	}
}

func TestAnOmittedReasoningEffortLetsAssemblyDecide(t *testing.T) {
	// Empty is NOT "the route's value" here: it is "let assembly decide", and
	// assembly lowers a reasoning route to `none` when the step offers tools.
	// A handler that substituted the route's value would take that decision
	// away from the one place that knows the step has tools.
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, &spyStepStore{})

	if _, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}); err != nil {
		t.Fatal(err)
	}
	if got := runner.compiled[0].ReasoningEffort; got != "" {
		t.Errorf("the handler sent %q instead of leaving it to assembly", got)
	}
}

func TestAnInvalidStepReasoningEffortIsRefusedBeforeAnythingRuns(t *testing.T) {
	runner := &scriptedRunner{}
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, runner, steps)

	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws", ReasoningEffort: "quite hard",
	})
	if err == nil {
		t.Fatal("an invalid reasoning effort was accepted")
	}
	if !strings.Contains(err.Error(), "none, minimal, low, medium, high") {
		t.Errorf("the refusal does not list the accepted values: %v", err)
	}
	if runner.ran != 0 {
		t.Error("the step ran anyway")
	}
	if len(steps.order()) != 0 {
		t.Errorf("a row was written: %v", steps.order())
	}
}

// ── S68 B6: the chosen-skills note opens the step, never its prompt ─────────

func TestTheChosenSkillsNoteRidesTheStepOpeningNotThePrompt(t *testing.T) {
	// The daemon door resolved the names and composed the skip sentence; the
	// step door's job is to put that sentence where a person reads it — the
	// opening notes channel (the same one S14's decisions use, which the
	// runtime appends to step.started's summary and NOTHING compiles). The
	// prompt bytes are built at Compile, before this seam exists, so a note
	// cannot leak into what the model is shown (CR6's direction).
	steps := &spyStepStore{}
	runner := &scriptedRunner{}
	tbl, _ := stepTable(t, runner, steps)
	const note = "chosen skills skipped (unpickable at send): legacy-notes"
	if _, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws",
		ChosenSkillsNote: note}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range runner.lastReq.Notes {
		if n == note {
			found = true
		}
	}
	if !found {
		t.Fatalf("the note must ride the opening notes, got %v", runner.lastReq.Notes)
	}

	// And the pre-S68 shape is byte-identical: no note sent, no note added —
	// nil stays nil, not an empty slice wearing a choice's clothes (P31).
	if _, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"}); err != nil {
		t.Fatal(err)
	}
	if runner.lastReq.Notes != nil {
		t.Fatalf("a step that chose nothing carries notes: %v", runner.lastReq.Notes)
	}
}
