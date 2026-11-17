package protocol

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// T-stop: `handleStep`'s integration with the daemon's cancel registry (via
// `Table.TrackStep`/`CancelStep`/`StepWasStopped`), ahead of the `step.stop`
// RPC method itself — this proves the WIRING, which is the part a hand-rolled
// mock of the hooks could get wrong in either direction (never cancel, or
// cancel the wrong step).

// blockingRunner blocks Run until released, and reports whether its context
// was cancelled before release — the one fact a user-initiated stop has to
// produce for this test to mean anything.
type blockingRunner struct {
	*scriptedRunner
	entered chan struct{}
	release chan struct{}

	mu        sync.Mutex
	cancelled bool
}

func newBlockingRunner() *blockingRunner {
	b := &blockingRunner{scriptedRunner: &scriptedRunner{}, entered: make(chan struct{}), release: make(chan struct{})}
	b.scriptedRunner.result = runtime.StepResult{
		Kind: runtime.ResultComplete, Outcome: runtime.OutcomeFailed,
		Reason: "the step was cancelled: context canceled",
	}
	return b
}

func (b *blockingRunner) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	close(b.entered)
	select {
	case <-ctx.Done():
		b.mu.Lock()
		b.cancelled = true
		b.mu.Unlock()
	case <-b.release:
	}
	return b.scriptedRunner.Run(ctx, req, opts)
}

func (b *blockingRunner) wasCancelled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancelled
}

func TestStoppingARunningStepCancelsItsContext(t *testing.T) {
	runner := newBlockingRunner()
	steps := &spyStepStore{}
	tbl, _ := stepTable(t, StepRunner(runner.scriptedRunner), steps)
	// Swap in the real StepRunner interface methods onto our blocking
	// wrapper: scriptedRunner satisfies Compile/BuildTools already (embedded),
	// and we override Run. Go's embedding gives us that for free — `tbl.Runner`
	// just needs to be the wrapper, not the embedded field.
	tbl.Runner = stepRunnerAdapter{runner}

	done := make(chan struct {
		res StepResult
		err error
	}, 1)
	go func() {
		res, err := callStep(t, tbl, StepParams{Prompt: "x", WorkspaceRoot: "/ws"})
		done <- struct {
			res StepResult
			err error
		}{res, err}
	}()

	select {
	case <-runner.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the run never started")
	}

	if !tbl.CancelStep(1) {
		t.Fatal("CancelStep found nothing tracked for step 1")
	}
	close(runner.release)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("step returned an error: %v", got.err)
		}
		if !runner.wasCancelled() {
			t.Fatal("the step's own context was never cancelled — CancelStep did not reach it")
		}
		// The RESPONSE must read "suspended, stopped by the user" — not
		// whatever generic cancellation text the runtime produced.
		if got.res.Kind != "suspended" {
			t.Errorf("Kind is %q, want suspended", got.res.Kind)
		}
		if got.res.PauseReason != "user" {
			t.Errorf("PauseReason is %q, want user", got.res.PauseReason)
		}
		if got.res.Outcome != "" {
			t.Errorf("Outcome is %q, want empty — a stop is not an outcome", got.res.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the step call never returned after being released")
	}

	// And the STORE agrees: status suspended, pause_reason user, via the exact
	// writes the spy recorded.
	if steps.finalStatus != "suspended" {
		t.Errorf("stored status is %q, want suspended", steps.finalStatus)
	}
	if steps.pauseReason != "user" {
		t.Errorf("stored pause reason is %q, want user", steps.pauseReason)
	}
}

func TestCancellingAStepThatIsNotRunningFindsNothing(t *testing.T) {
	tbl, _ := stepTable(t, StepRunner(&scriptedRunner{}), &spyStepStore{})
	if tbl.CancelStep(999) {
		t.Fatal("CancelStep found a step nobody started")
	}
}

func TestStoppingOneStepDoesNotCancelAConcurrentOne(t *testing.T) {
	// Two steps, same daemon, same table — one stop must reach exactly the
	// step it named. Run them through the real tracker rather than the spy's
	// private map, which is the part worth re-proving at this layer: the fake
	// tracker's own unit tests already cover the map; this proves handleStep
	// hands it the right ID.
	runnerA := newBlockingRunner()
	runnerB := newBlockingRunner()
	tbl, _ := stepTable(t, StepRunner(&scriptedRunner{}), &spyStepStore{})

	tbl.Runner = stepRunnerAdapter{runnerA}
	doneA := make(chan struct{})
	go func() { callStep(t, tbl, StepParams{Prompt: "a", WorkspaceRoot: "/ws"}); close(doneA) }()
	<-runnerA.entered

	tbl.Runner = stepRunnerAdapter{runnerB}
	doneB := make(chan struct{})
	go func() { callStep(t, tbl, StepParams{Prompt: "b", WorkspaceRoot: "/ws"}); close(doneB) }()
	<-runnerB.entered

	if !tbl.CancelStep(1) {
		t.Fatal("CancelStep(1) found nothing")
	}
	close(runnerA.release)
	<-doneA

	select {
	case <-runnerB.entered:
	default:
	}
	if runnerB.wasCancelled() {
		t.Fatal("stopping step 1 cancelled step 2's context too")
	}
	close(runnerB.release)
	<-doneB
}

// stepRunnerAdapter lets a *blockingRunner stand in for StepRunner: Go cannot
// override an embedded method's RECEIVER type across an interface boundary
// directly on the struct literal used elsewhere in this file, so the adapter
// is the one-line fix rather than touching the shared scriptedRunner.
type stepRunnerAdapter struct{ b *blockingRunner }

func (a stepRunnerAdapter) Compile(_ context.Context, in runtime.CompileInput, deps runtime.ContextDeps) runtime.CompileResult {
	return a.b.scriptedRunner.Compile(context.Background(), in, deps)
}
func (a stepRunnerAdapter) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	return a.b.Run(ctx, req, opts)
}
func (a stepRunnerAdapter) BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error) {
	return a.b.scriptedRunner.BuildTools(granted, workspaceRoot, agent, frozen)
}
