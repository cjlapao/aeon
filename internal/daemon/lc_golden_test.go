package daemon

// S55 T4 — the daemon drain's golden and behaviour (LC19), delivered in
// post-T4 form as T1's preflight predicted ("the T1 shutdown golden is the
// arbiter"). A step in flight when the daemon shuts down must be left
// RUNNING at its last checkpoint — not failed, not suspended — so the
// existing recovery walk resumes it exactly as it would after a crash, and
// the interrupted call's cost stays a floor, never a certain number.
//
// The harness is the real store, the real runtime, the real toolset and a
// model that parks IN a model call (or a bash call, where the test needs a
// process to kill) — the state the drain must find. Nothing is adapted away:
// a golden that fakes the step's exit cannot prove the step was not
// committed.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// --- harness ---------------------------------------------------------------

// lcBlueprint writes a one-lane blueprint with a single model step. The agent
// must be NAMED — the validator refuses a model step without one — and
// "default" is the ad-hoc preset (the default workspace grants: no shell).
// A named agent like "chat" runs through that registered handler, which is
// how a step gets bash (the "chat" body is granted one).
func lcBlueprint(t *testing.T, agent string) string {
	t.Helper()
	if agent == "" {
		agent = "default"
	}
	raw := "id: lc-drain\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: " + agent + "}\n"
	path := filepath.Join(t.TempDir(), "lc-drain.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lcDaemon wires a store, a static route, a scripted model and the wire
// recorder as logger. The route is named "lc-route" on purpose: a loop step
// with no configured route is a different failure than the one under test.
func lcDaemon(t *testing.T, s *store.Store, m protocol.Streamer) (*Daemon, *wire) {
	t.Helper()
	w := &wire{}
	route := model.Route{ID: "lc-route", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: m, Logger: w, Notify: w.notify})
	return d, w
}

// lcStart starts the drain test's loop and returns its id.
func lcStart(t *testing.T, d *Daemon, bp, root, slug string) string {
	t.Helper()
	res, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: bp, Goal: "the drain test loop", WorkspaceRoot: root, Slug: slug, Route: "lc-route",
	})
	if err != nil {
		t.Fatalf("start the loop: %v", err)
	}
	loopID, ok := res.(map[string]any)["id"].(string)
	if !ok || loopID == "" {
		t.Fatalf("loop start returned no id: %#v", res)
	}
	return loopID
}

// lcInFlightModel parks IN a model call: each Stream blocks until its context
// is cancelled (release nil) — or, when release is given, until the test
// closes it, which lets a test order the step's death against a person's stop
// bit. The cause each call died with is recorded, so a test can assert the
// drain's typed cause reached the model layer.
//
// entered (when non-nil) closes when the first Stream arrives. A test must
// wait on it before draining or stopping: the pin marks the step in flight at
// MarkStepStarted, which is BEFORE the step's cancel is tracked and before the
// runtime's first model call, so a drain that lands in that window finds a
// step with no model call to die — the test would then assert about a call
// that never happened (and, with -race's slower scheduling, that is the
// window it deterministically lands in).
type lcInFlightModel struct {
	release <-chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	causes  []error
}

func (m *lcInFlightModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	if m.entered != nil {
		select {
		case <-m.entered:
		default:
			close(m.entered)
		}
	}
	go func() {
		if m.release != nil {
			<-m.release
		} else {
			<-ctx.Done()
		}
		m.mu.Lock()
		m.causes = append(m.causes, context.Cause(ctx))
		m.mu.Unlock()
		close(deltas)
		close(results)
		close(errs)
	}()
	return deltas, results, errs
}

func (m *lcInFlightModel) cause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.causes) == 0 {
		return nil
	}
	return m.causes[0]
}

// waitStepInFlight polls the pin until the engine has marked the step in
// flight (running, StepInFlight, LastStepID) and returns the step id.
func waitStepInFlight(t *testing.T, s *store.Store, loopID string) int64 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := loop.Pinned(context.Background(), s, loopID)
		if err == nil && st.StepInFlight && st.LastStepID > 0 {
			return st.LastStepID
		}
		if time.Now().After(deadline) {
			t.Fatalf("the step never went in flight: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// lcDrain runs the daemon's drain with a bounded grace and returns the report.
func lcDrain(t *testing.T, d *Daemon) DrainReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return d.Drain(ctx)
}

// procCmdlineMatches scans /proc for processes whose argv carries the token.
// Asserting a process group is gone by reading the processes — not by
// trusting the tool that was told to kill them — is what makes the orphan
// claim checkable.
func procCmdlineMatches(token string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte(token)) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// --- the tests ---------------------------------------------------------------

// TestShutdownCancelsRunnersWithCause: after Drain every runner has exited,
// the in-flight model call died with the drain's typed cause, and no
// runner-error event was emitted — a shutdown that emits a false error event
// is a step that looks broken when it was only exited.
func TestShutdownCancelsRunnersWithCause(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	m := &lcInFlightModel{entered: make(chan struct{})}
	d, w := lcDaemon(t, s, m)
	loopID := lcStart(t, d, lcBlueprint(t, ""), root, "drain-cause")
	waitStepInFlight(t, s, loopID)
	<-m.entered // the model call has begun — the drain below lands mid-call

	rep := lcDrain(t, d)

	if got := d.loopRunner.ActiveCount(); got != 0 {
		t.Fatalf("%d runner(s) still in flight after the drain", got)
	}
	if !errors.Is(m.cause(), loop.ErrDaemonShutdown) {
		t.Fatalf("the in-flight model call died with cause %v, want ErrDaemonShutdown", m.cause())
	}
	if rep.Runners != 1 || rep.LeftRunning != 0 {
		t.Fatalf("drain report %+v: one runner was in flight and it settled", rep)
	}
	if !strings.Contains(w.logLines(), "loop drain:") {
		t.Errorf("the drain logged no one-line report:\n%s", w.logLines())
	}
	for _, sent := range w.delivered() {
		if sent.Event.Kind == "loop.blocked" {
			t.Fatalf("the shutdown emitted a false blocked event: %+v", sent.Event)
		}
	}
}

// TestShutdownLeavesInFlightStepRunningNotFailed: the T1 golden, updated.
// The step row is left RUNNING at its last checkpoint — not failed, not
// suspended — and the pin stays where MarkStepStarted left it, so the
// recovery walk has exactly what a crash would have left.
func TestShutdownLeavesInFlightStepRunningNotFailed(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	m := &lcInFlightModel{entered: make(chan struct{})}
	d, _ := lcDaemon(t, s, m)
	loopID := lcStart(t, d, lcBlueprint(t, ""), root, "drain-golden")
	stepID := waitStepInFlight(t, s, loopID)
	<-m.entered // the model call has begun — the drain below lands mid-call

	lcDrain(t, d)

	step, err := s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != "running" {
		t.Fatalf("a shutdown-interrupted step was committed %q (outcome %q) — the recovery walk only resumes running rows, so a committed outcome is a lost step",
			step.Status, step.Outcome)
	}
	if step.PauseReason != "" {
		t.Errorf("the interrupted step carries pause reason %q — only a person's stop may set one", step.PauseReason)
	}
	// The checkpoint recovery resumes from: the opening checkpoint was
	// written before the first call, and its transcript is readable.
	if len(step.ContextManifestRaw) == 0 {
		t.Fatal("no checkpoint on the running row — recovery would have nothing to resume from")
	}
	if tr := s.ReadTranscript(stepID); !tr.OK || tr.Count == 0 {
		t.Fatalf("the checkpoint's transcript is unreadable: %+v", tr)
	}
	// The pin stayed where MarkStepStarted left it: running, in flight, at
	// the step.
	st, err := loop.Pinned(context.Background(), s, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "running" || !st.StepInFlight || st.LastStepID != stepID {
		t.Fatalf("the pin moved off the in-flight step: status %q in-flight %v last %d (want running, true, %d)",
			st.Status, st.StepInFlight, st.LastStepID, stepID)
	}
}

// TestShutdownKillsBashProcessGroups: a step running a long sleep in bash is
// gone — the process and its children — after the drain. The group kill
// follows the context cancel; the test proves it through the whole stack by
// reading /proc, not by trusting the kill.
func TestShutdownKillsBashProcessGroups(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	// Both questions the gate asks of a bash call: the tool question (the
	// "chat" body is granted the shell) and the command question (this
	// sleep). Seeded through the real write boundary, like every rule a test
	// uses.
	seedRules(t, s, time.Now().UTC(),
		toolRulePolicy("lc-allow-bash-for-chat", "chat", "bash", policy.Allow),
		commandRulePolicy("lc-allow-sleep", "chat", "sleep *", policy.Allow))
	// A distinctive fractional sleep: findable in /proc, and no other
	// process on the machine runs it.
	token := fmt.Sprintf("300.%07d", time.Now().UnixNano()%10000000)
	m := &scriptedModel{turns: []modelTurn{{toolUses: []model.Block{
		toolUse("u1", "bash", `{"command":"sleep `+token+`"}`),
	}}}}
	d, _ := lcDaemon(t, s, m)
	loopID := lcStart(t, d, lcBlueprint(t, "chat"), root, "drain-bash")
	waitStepInFlight(t, s, loopID)

	// The sleep must actually be running before the drain: a drain that
	// finds nothing would pass vacuously.
	deadline := time.Now().Add(10 * time.Second)
	for len(procCmdlineMatches(token)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the sleep never started; the test would pass vacuously")
		}
		time.Sleep(20 * time.Millisecond)
	}

	rep := lcDrain(t, d)

	// The process and its children are gone.
	deadline = time.Now().Add(10 * time.Second)
	for pids := procCmdlineMatches(token); len(pids) > 0; {
		if time.Now().After(deadline) {
			t.Fatalf("process(es) %v survived the drain — orphaned children", pids)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rep.LeftRunning != 0 {
		t.Fatalf("drain report %+v: the settled runner's loop was accounted for", rep)
	}
}

// TestRecoveryResumesADrainedStep: restart the daemon on the same store.
// Recover classifies the drained step as case B (no model turn was committed
// — the first call was interrupted), re-issues the call with the
// interrupted charge kept as a FLOOR (unknown, never certain), and the
// restarted loop resumes through the recovery seam and completes.
func TestRecoveryResumesADrainedStep(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	m := &lcInFlightModel{entered: make(chan struct{})}
	d1, _ := lcDaemon(t, s, m)
	loopID := lcStart(t, d1, lcBlueprint(t, "default"), root, "drain-recover")
	stepID := waitStepInFlight(t, s, loopID)
	// The recovery classification is the whole point: the interruption must
	// land MID-first-model-call (no turn committed, no receipts) so Recover
	// reads case B. Waiting on entered is what makes that hold under any
	// scheduling, not just the fast one.
	<-m.entered

	lcDrain(t, d1)

	// The golden holds before the restart: the row is running at its
	// checkpoint, the pin still in flight.
	if step, err := s.ReadStep(stepID); err != nil || step.Status != "running" {
		t.Fatalf("pre-restart row: %+v err %v", step, err)
	}

	// Restart on the same store (a real restart: the first daemon's store
	// closes, a second daemon opens it cold).
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	d2, _ := lcDaemon(t, s2, &scriptedModel{turns: []modelTurn{{
		text: `{"status":"ok","summary":"recovered after the drain"}`,
	}}})

	// The recovery scan runs first, exactly as serve's boot order does.
	sum, err := d2.Recover(context.Background(), false)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if sum.Total != 1 || sum.Recovered != 1 || sum.Failed != 0 {
		t.Fatalf("recovery summary %+v: the drained step must recover, not fail", sum)
	}
	step, err := s2.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != "done" || step.Outcome != "ok" {
		t.Fatalf("the recovered step is %q/%q, want done/ok", step.Status, step.Outcome)
	}
	// The interrupted call's cost is a floor: unknown, with the reason that
	// says the stream aborted early — never a certain number.
	record, ok, err := s2.ReadRecovery(stepID)
	if err != nil || !ok {
		t.Fatalf("no recovery record: ok=%v err=%v", ok, err)
	}
	if record.Case != "B" {
		t.Fatalf("recovery case %q, want B (the first call was interrupted, no turn committed)", record.Case)
	}
	if record.PriorCost == nil || record.PriorCost.State != model.CostUnknown || record.PriorCost.Reason != model.ReasonStreamAbortedEarly {
		t.Fatalf("the interrupted charge was not kept as an unknown floor: %+v", record.PriorCost)
	}
	if step.Cost.State != model.CostUnknown {
		t.Fatalf("the step total laundered the interrupted charge into %q", step.Cost.State)
	}

	// Then the startup sweep: the running pin gets a runner, the engine
	// reads the recovered step back through the recovery seam, and the loop
	// completes.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := d2.StartLoops(ctx); err != nil {
		t.Fatalf("start loops: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		pin, err := s2.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatal(err)
		}
		if pin.Loop.Status == "done" && !d2.loopRunner.Active(loopID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the restarted loop never completed: %q", pin.Loop.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestStopDuringShutdownWins: a person's stop and the daemon's shutdown
// together park the loop suspended/user. The order here is the hostile one:
// the shutdown's cancel lands FIRST (the cause on the context chain is
// ErrDaemonShutdown), and the stop bit lands second, before the commit reads
// it. The commit must still park — the stop bit outranks the cause, so a
// person who pressed stop in the last second before SIGTERM does not get
// their stop rewritten as a crash.
func TestStopDuringShutdownWins(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	release := make(chan struct{})
	m := &lcInFlightModel{release: release, entered: make(chan struct{})}
	d, _ := lcDaemon(t, s, m)
	loopID := lcStart(t, d, lcBlueprint(t, ""), root, "drain-stop")
	stepID := waitStepInFlight(t, s, loopID)
	// entered also proves the step's cancel is tracked (tracking happens
	// before the runtime's first model call), so the stop bit below finds
	// it instead of missing a step that is tracked a moment later.
	<-m.entered

	// 1. The shutdown's cancel lands first: the cause is the drain's.
	d.cancelLoops(loop.ErrDaemonShutdown)
	// 2. The person's stop bit lands next — before the commit may read it.
	if !d.stepCancels.stop(stepID) {
		t.Fatal("the stop did not find the in-flight step")
	}
	// 3. The in-flight call dies; the commit reads the stop bit FIRST.
	close(release)
	lcDrain(t, d)

	// The cause WAS the shutdown's — and the stop still won.
	if !errors.Is(m.cause(), loop.ErrDaemonShutdown) {
		t.Fatalf("the model call died with cause %v, want ErrDaemonShutdown (the stop must win DESPITE it)", m.cause())
	}
	step, err := s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != "suspended" || step.PauseReason != "user" {
		t.Fatalf("the person's stop lost: the step is %q (pause %q), want suspended/user", step.Status, step.PauseReason)
	}
	st, err := loop.Pinned(context.Background(), s, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "waiting" || st.LastReason != "stopped" {
		t.Fatalf("the loop did not park on the stop: %q / %q, want waiting / stopped", st.Status, st.LastReason)
	}
}

// TestDrainRefusesNewStarts: during a drain the start family refuses BEFORE
// any row exists — loop.start, loop.run and loop.answer all answer the typed
// "the daemon is shutting down" refusal, and the runner itself refuses a
// spawn, so no work can begin into a daemon that is going away.
func TestDrainRefusesNewStarts(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	d, _ := lcDaemon(t, s, &scriptedModel{})
	bp := lcBlueprint(t, "")

	lcDrain(t, d)

	if !d.loopRunner.Draining() {
		t.Fatal("the drain did not mark the runner draining")
	}
	ctx := context.Background()
	assertDrainRefusal := func(what string, err error) {
		t.Helper()
		var pe *protocol.Error
		if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrLoopControlRefused.Code {
			t.Fatalf("%s: got %v, want the typed ErrLoopControlRefused", what, err)
		}
		if !strings.Contains(pe.Data.What, "shutting down") {
			t.Fatalf("%s: the refusal does not name the shutdown: %q", what, pe.Data.What)
		}
	}

	_, err := d.Table.StartBlueprintLoop(ctx, protocol.LoopStartParams{
		Blueprint: bp, Goal: "too late", WorkspaceRoot: root, Slug: "drain-no-start", Route: "lc-route",
	})
	assertDrainRefusal("loop.start", err)
	if _, gerr := s.GetLoop(store.Scope{LoopSlug: "drain-no-start", ComponentRoot: root}); gerr == nil {
		t.Fatal("the refused start still created a loop row")
	}

	_, err = d.Table.AnswerBlueprintLoop(ctx, protocol.LoopAnswerParams{ID: "no-such-loop", Answer: "yes"})
	assertDrainRefusal("loop.answer", err)

	_, err = d.startRun(ctx, runInput{LoopID: "no-such-loop", Blueprint: bp})
	assertDrainRefusal("loop.run", err)

	// The runner is the safety net for any path a door did not check.
	if d.loopRunner.Start(ctx, "no-such-loop") {
		t.Fatal("the runner spawned a goroutine during the drain")
	}
}

// TestNoBackgroundRunnerStarts: no non-test daemon file may start a runner
// on context.Background() — a runner the drain could never reach, which is
// the abandoned-children bug T4 exists to delete. The positive control
// pins that the start sites ride the daemon-lifetime loopCtx instead.
func TestNoBackgroundRunnerStarts(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "Start(context.Background(") {
			t.Errorf("%s still starts a runner on context.Background() — an un-drainable goroutine", name)
		}
	}
	// Positive control: the start sites exist and ride loopCtx.
	for _, name := range []string{"start_loop.go", "runs.go", "blueprint.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "Start(d.loopCtx") {
			t.Errorf("%s no longer starts runners on d.loopCtx — where did the site go?", name)
		}
	}
}
