package loop

// S55 T3 — the control core's behaviour (LC2–LC4), exercised through the
// in-process Stop/Resume/Cancel operations with the daemon-supplied ControlHooks
// faked and the REAL store behind them. The eligibility functions are pinned by
// control_test.go; these are the operations that orchestrate the store CAS (T2)
// and the hooks (T3), so the daemon and loop.show cannot disagree.
//
// The park is a compare-and-set (the store's ParkLoopStopped): a stop that
// reaches a non-running loop is a no-op that names the remedy, never an error
// (LC2.4); a cancel routes its terminal through the engine's ONE writer
// (endStatus → emitIfOver → EndRun), so the run state reads `cancelled`, never
// a second status (LC4).

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// --- fixtures --------------------------------------------------------------

// eventLog captures the loop events a control operation emits, so a test can
// assert the one-writer's emissions (one loop.waiting per changing stop,
// loop.finished with the loop's status per a cancel) without a real daemon.
type eventLog struct {
	mu       sync.Mutex
	kinds    []string
	payloads []any
}

func (e *eventLog) emit(kind string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.kinds = append(e.kinds, kind)
	e.payloads = append(e.payloads, payload)
}

func (e *eventLog) count(kind string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, k := range e.kinds {
		if k == kind {
			n++
		}
	}
	return n
}

// maps returns the map payloads of the named kind, in order; a non-map payload
// is skipped (the control core's emits are all maps, so this is a no-op filter
// today but keeps the assertion honest if one is not).
func (e *eventLog) maps(kind string) []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []map[string]any
	for i, k := range e.kinds {
		if k != kind {
			continue
		}
		if m, ok := e.payloads[i].(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// fakeHooks is the daemon-supplied ControlHooks a control test stands in for:
// it records each seam's call so a test can assert the fixed order (steps, then
// runner, then the park) and that a no-op reaches none of them.
type fakeHooks struct {
	stopped    []int64
	stopErr    error
	wait       bool
	started    int
	stopCalled bool
	waitCalled bool
}

func (f *fakeHooks) hooks() ControlHooks {
	return ControlHooks{
		StopSteps: func(loopID string) ([]int64, error) {
			f.stopCalled = true
			return f.stopped, f.stopErr
		},
		Wait: func(loopID string, d time.Duration) bool {
			f.waitCalled = true
			return f.wait
		},
		StartRunner: func(ctx context.Context, loopID string) bool {
			f.started++
			return true
		},
	}
}

// newControlLoop pins a fresh loop (status 'running', with the loop_run row
// Start creates) and returns its id, a Deps whose Event is a captured log, and
// the log. The store is the REAL one (the operation under test must talk to it).
func newControlLoop(t *testing.T) (string, Deps, *eventLog) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "ctrl.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "ctrl", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard",
	}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := &eventLog{}
	dep := Deps{Store: s, Event: ev.emit}
	return st.ID, dep, ev
}

// setControlState moves a loop to a target status/lastReason through the
// engine's OWN write path (save → SavePinnedRuntime), so the fixture is the
// same shape the engine produces, not a hand-rolled row. The runtime JSON
// carries lastReason (the CAS reads it from there, not from a loop column).
func setControlState(t *testing.T, dep Deps, id, status, lastReason string) {
	t.Helper()
	st, err := Pinned(context.Background(), dep.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = status
	st.LastReason = lastReason
	if err := save(dep, st); err != nil {
		t.Fatal(err)
	}
}

// writeBlueprintRaw writes an arbitrary blueprint (writeBlueprint's header is
// fixed, so a blueprint that needs on_abort / questions is written whole).
func writeBlueprintRaw(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

// --- Stop ------------------------------------------------------------------

// TestStopParksRunningAndEmitsOnce — LC2.1/2/3: a stop on a running loop parks
// it waiting/stopped in one compare-and-set, emits exactly one loop.waiting
// naming the first parked step, records one control event with the person's
// reason, and reports the parked steps.
func TestStopParksRunningAndEmitsOnce(t *testing.T) {
	id, dep, ev := newControlLoop(t)
	h := &fakeHooks{stopped: []int64{123}, wait: true}

	res, refusal, err := Stop(context.Background(), dep, id, userActor, "person said stop", h.hooks())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if refusal != nil {
		t.Fatalf("Stop on a running loop refused: %v", refusal)
	}
	if !res.Stopped || res.Status != "waiting" || res.Note != "" {
		t.Fatalf("Stop = %+v, want stopped/waiting/no-note", res)
	}
	if len(res.ParkedSteps) != 1 || res.ParkedSteps[0] != 123 {
		t.Fatalf("ParkdSteps = %v, want [123]", res.ParkedSteps)
	}
	// The store: the loop row is waiting, the runtime's lastReason is stopped.
	st, err := Pinned(context.Background(), dep.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "waiting" || st.LastReason != "stopped" {
		t.Fatalf("pinned = %s/%s, want waiting/stopped", st.Status, st.LastReason)
	}
	// Exactly one loop.waiting, naming the first parked step (P31: a between-
	// steps stop carries no stepId, never a zero that reads as "the first").
	if got := ev.count("loop.waiting"); got != 1 {
		t.Fatalf("loop.waiting emitted %d times, want 1", got)
	}
	w := ev.maps("loop.waiting")
	if w[0]["reason"] != "stopped" || w[0]["stepId"] != int64(123) {
		t.Fatalf("loop.waiting payload = %+v, want reason=stopped stepId=123", w[0])
	}
	// One control event (the audit), with the reason in its own column.
	events, err := dep.Store.ListControlEvents(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "stop" || events[0].Reason != "person said stop" {
		t.Fatalf("control events = %+v, want one stop with the reason", events)
	}
}

// TestStopNoopsNameTheRemedy — LC2.4: a stop that reaches a non-running loop is
// a no-op that NAMES the remedy, never an error and never a second park. The
// same CheckStop the daemon runs as a gate and loop.show renders is what
// decides this, so the button and the engine cannot disagree.
func TestStopNoopsNameTheRemedy(t *testing.T) {
	cases := []struct {
		status, lastReason, wantKind, wantDetail string
	}{
		{"waiting", "stopped", "stopped", "the loop is stopped — resume it (or cancel it)"},
		{"waiting", "", "waiting-question", "the loop is waiting on a question — answer it, or cancel the loop"},
		{"blocked", "cap-hit", "cap-hit", "the loop is blocked on a budget cap — raise it (loop.raise)"},
		{"blocked", "stuck", "stuck", "the loop is blocked (stuck) — resume it"},
		{"idle", "", "idle", "the loop is idle — close it (loop.close)"},
		{"done", "", "terminal", "the loop is done — it is finished; a stop has nothing to do"},
		{"aborted", "", "terminal", "the loop is aborted — it is finished; a stop has nothing to do"},
	}
	for _, c := range cases {
		t.Run(c.status+"/"+c.lastReason, func(t *testing.T) {
			id, dep, ev := newControlLoop(t)
			setControlState(t, dep, id, c.status, c.lastReason)
			h := &fakeHooks{stopped: []int64{}, wait: true}

			res, refusal, err := Stop(context.Background(), dep, id, userActor, "reason", h.hooks())
			if err != nil {
				t.Fatalf("Stop on %s/%s = error %v — a no-op is a result, not an error", c.status, c.lastReason, err)
			}
			if refusal == nil {
				t.Fatalf("Stop on %s/%s = eligible, want a refusal", c.status, c.lastReason)
			}
			if refusal.Kind != c.wantKind || refusal.Detail != c.wantDetail {
				t.Fatalf("refusal = {kind:%q detail:%q}, want {kind:%s detail:%s}",
					refusal.Kind, refusal.Detail, c.wantKind, c.wantDetail)
			}
			if res.Stopped {
				t.Fatalf("Stop on %s parked (Stopped=true) — a no-op must not park", c.status)
			}
			if res.Status != c.status {
				t.Fatalf("result.Status = %q, want %q (the state the loop already is)", res.Status, c.status)
			}
			if res.Note != c.wantDetail {
				t.Fatalf("result.Note = %q, want %q (the remedy the button renders)", res.Note, c.wantDetail)
			}
			// The no-op writes nothing and reaches no seam: no event, no control
			// event, the park CAS and the hooks never ran.
			if ev.count("loop.waiting") != 0 {
				t.Fatalf("a no-op emitted loop.waiting — it must not")
			}
			if events, _ := dep.Store.ListControlEvents(id, 0); len(events) != 0 {
				t.Fatalf("a no-op wrote %d control events — it must not", len(events))
			}
			if h.stopCalled || h.waitCalled || h.started != 0 {
				t.Fatalf("a no-op reached the seams (stop=%v wait=%v started=%d) — it must not",
					h.stopCalled, h.waitCalled, h.started)
			}
		})
	}
}

// --- Resume ---------------------------------------------------------------

// TestResumeThreeShapes — LC3: a resume moves the two eligible states (a
// parked step's waiting/stopped and a watchdog's blocked/stuck) back to
// running in one compare-and-set, clears the lastReason the pin carried, starts
// a fresh runner, and records one control event. At the loop level the parked
// and between-steps shapes are the SAME CAS — the daemon's ResumeLoop decides
// which (from the step row's status), and the exactly-once message it carries
// rides the very seam step.continue proves (blueprint_stop_test.go); both
// reach the engine's Resume here. Named for the daemon's three paths; the two
// eligible CAS states are what the core proves.
func TestResumeThreeShapes(t *testing.T) {
	cases := []struct {
		name, status, lastReason string
	}{
		{"parked-step", "waiting", "stopped"},
		{"between-steps", "waiting", "stopped"},
		{"watchdog-stuck", "blocked", "stuck"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, dep, ev := newControlLoop(t)
			setControlState(t, dep, id, c.status, c.lastReason)
			h := &fakeHooks{wait: true}

			res, refusal, err := Resume(context.Background(), dep, id, userActor, h.hooks())
			if err != nil {
				t.Fatalf("Resume: %v", err)
			}
			if refusal != nil {
				t.Fatalf("Resume on %s/%s refused: %v", c.status, c.lastReason, refusal)
			}
			if !res.Resumed || res.Status != "running" {
				t.Fatalf("Resume = %+v, want resumed/running", res)
			}
			// The store: the loop row is running, and the lastReason the pin
			// carried is cleared so a second resume cannot re-eligible.
			st, err := Pinned(context.Background(), dep.Store, id)
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != "running" || st.LastReason != "" {
				t.Fatalf("pinned = %s/%s, want running/empty lastReason", st.Status, st.LastReason)
			}
			if h.started != 1 {
				t.Fatalf("StartRunner called %d times, want 1 (a fresh runner re-runs the loop)", h.started)
			}
			// One control event (the audit): kind resume.
			events, err := dep.Store.ListControlEvents(id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].Kind != "resume" {
				t.Fatalf("control events = %+v, want one resume", events)
			}
			// A resume emits no loop.waiting (it is not a park).
			if ev.count("loop.waiting") != 0 {
				t.Fatalf("a resume emitted loop.waiting — it is not a park")
			}
		})
	}
}

// TestResumeRefusalsAreOneFunction — LC3: a resume that reaches a non-eligible
// state is refused by the SAME CheckResume the daemon runs as a gate and
// loop.show renders, so the button and the engine cannot disagree. The op does
// the CAS in no case — it refuses first.
func TestResumeRefusalsAreOneFunction(t *testing.T) {
	cases := []struct {
		status, lastReason, wantKind string
	}{
		{"running", "", "running"},
		{"waiting", "", "waiting-question"},
		{"blocked", "cap-hit", "cap-hit"},
		{"idle", "", "idle"},
	}
	for _, c := range cases {
		t.Run(c.status+"/"+c.lastReason, func(t *testing.T) {
			id, dep, ev := newControlLoop(t)
			setControlState(t, dep, id, c.status, c.lastReason)
			h := &fakeHooks{wait: true}

			res, refusal, err := Resume(context.Background(), dep, id, userActor, h.hooks())
			if err != nil {
				t.Fatalf("Resume on %s/%s = error %v — a refusal is a result", c.status, c.lastReason, err)
			}
			if refusal == nil {
				t.Fatalf("Resume on %s/%s = eligible, want a refusal", c.status, c.lastReason)
			}
			if refusal.Kind != c.wantKind {
				t.Fatalf("refusal.Kind = %q, want %q", refusal.Kind, c.wantKind)
			}
			if res.Resumed {
				t.Fatalf("Resume on %s/%s moved the loop — a refusal must not", c.status, c.lastReason)
			}
			// The op refused before the CAS and the seams: no runner, no event,
			// no control event, the state is unchanged.
			if ev.count("loop.waiting") != 0 {
				t.Fatalf("a refused resume emitted loop.waiting — it must not")
			}
			if h.started != 0 {
				t.Fatalf("a refused resume started a runner — it must not")
			}
			if events, _ := dep.Store.ListControlEvents(id, 0); len(events) != 0 {
				t.Fatalf("a refused resume wrote %d control events — it must not", len(events))
			}
			st, err := Pinned(context.Background(), dep.Store, id)
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != c.status {
				t.Fatalf("state moved to %q, want %q (a refusal changes nothing)", st.Status, c.status)
			}
		})
	}
}

// --- Cancel ---------------------------------------------------------------

// onAbortBlueprint is a loop with an on_abort handler lane: a cancel that ran
// the abort lane would leave the position at `cleanup` and the loop running;
// the cancel that routes through the engine's ONE writer instead says aborted
// with the position cleared — so a cleared position is the proof the on_abort
// lane did NOT run (LC4.3).
const onAbortBlueprint = `id: cancel-test
schemaVersion: 1
version: 1
on_abort: cleanup
lanes:
  - id: work
    step: {kind: model-step, agent: default}
  - id: cleanup
    when: {field: flags.aborted, op: eq, value: true}
    step: {kind: model-step, agent: default}
`

func newCancelLoop(t *testing.T) (string, Deps, *eventLog) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "cancel-test.yaml")
	writeBlueprintRaw(t, path, onAbortBlueprint)
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "cancel-test", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard",
	}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := &eventLog{}
	dep := Deps{Store: s, Event: ev.emit}
	return st.ID, dep, ev
}

// setAbortedFlag plants the aborted flag on an otherwise-running loop, the one
// state the store's "already cancelled" refusal (Kind=cancelled) guards against
// — distinct from the realistic second cancel (which is terminal at CheckCancel).
func setAbortedFlag(t *testing.T, dep Deps, id string) {
	t.Helper()
	st, err := Pinned(context.Background(), dep.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = "running"
	st.Flags["aborted"] = true
	if err := save(dep, st); err != nil {
		t.Fatal(err)
	}
}

// TestCancelIsTerminalAndSkipsOnAbort — LC4 / LC4.3: a cancel routes its
// terminal through the engine's ONE writer (endStatus → emitIfOver), so the
// loop says aborted with the position cleared, the on_abort lane does NOT run
// (a dispatch would leave the position at it), the run settles cancelled, a
// control event carries the reason in its own column, and loop.finished says
// the loop's status. A second cancel is refused and changes nothing.
func TestCancelIsTerminalAndSkipsOnAbort(t *testing.T) {
	id, dep, ev := newCancelLoop(t)
	h := &fakeHooks{wait: true}

	res, refusal, err := Cancel(context.Background(), dep, id, userActor, "person said stop it", h.hooks())
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if refusal != nil {
		t.Fatalf("Cancel on a running loop refused: %v", refusal)
	}
	if !res.Cancelled || res.Status != "aborted" {
		t.Fatalf("Cancel = %+v, want cancelled/aborted", res)
	}
	st, err := Pinned(context.Background(), dep.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "aborted" {
		t.Fatalf("status = %q, want aborted (the one-writer's terminal)", st.Status)
	}
	// The on_abort lane did NOT run: the position is cleared, not cleanup (the
	// abort lane would leave the loop running at it). The one-writer routed the
	// terminal straight through endStatus, not through on_abort.
	if st.Position != (Position{}) {
		t.Fatalf("position = %+v, want cleared — the on_abort lane would have left it at cleanup", st.Position)
	}
	if st.StepStates["cleanup"].Status == "done" {
		t.Fatal("the on_abort lane ran (cleanup is done) — a cancel must not run it (LC4.3)")
	}
	// The run settled cancelled, not a second status.
	runs, err := dep.Store.ListRuns(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].State != "cancelled" {
		t.Fatalf("runs = %+v, want one run settled cancelled (LC4)", runs)
	}
	// The control event (the audit) carries the reason in its own column.
	events, err := dep.Store.ListControlEvents(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "cancel" || events[0].Reason != "person said stop it" {
		t.Fatalf("control events = %+v, want one cancel with the reason", events)
	}
	// loop.finished says the loop's status (aborted), once.
	if got := ev.count("loop.finished"); got != 1 {
		t.Fatalf("loop.finished emitted %d times, want 1", got)
	}
	if m := ev.maps("loop.finished"); len(m) != 1 || m[0]["status"] != "aborted" {
		t.Fatalf("loop.finished payload = %v, want status=aborted", m)
	}

	// A second cancel is refused (the loop is now terminal) and changes nothing.
	again, refusal2, err := Cancel(context.Background(), dep, id, userActor, "again", h.hooks())
	if err != nil {
		t.Fatalf("second Cancel: %v", err)
	}
	if again.Cancelled {
		t.Fatal("a second cancel committed — a terminal loop must refuse it")
	}
	if refusal2 == nil {
		t.Fatal("a second cancel was not refused")
	}
	againSt, err := Pinned(context.Background(), dep.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	if againSt.Status != "aborted" || againSt.Position != (Position{}) {
		t.Fatalf("the second cancel moved the loop: %s/%+v", againSt.Status, againSt.Position)
	}
	if events2, _ := dep.Store.ListControlEvents(id, 0); len(events2) != 1 {
		t.Fatalf("a second cancel wrote a control event: %d rows, want 1 (the first)", len(events2))
	}
}

// TestCancelAlreadyCancelledRefused — LC4: a cancel on a loop whose aborted
// flag is already set is refused by the store's own guard (Kind=cancelled),
// distinct from the realistic second cancel (which is terminal at CheckCancel).
func TestCancelAlreadyCancelledRefused(t *testing.T) {
	id, dep, ev := newControlLoop(t)
	setAbortedFlag(t, dep, id)
	h := &fakeHooks{wait: true}

	res, refusal, err := Cancel(context.Background(), dep, id, userActor, "again", h.hooks())
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if refusal == nil || refusal.Kind != "cancelled" {
		t.Fatalf("Cancel = %+v (refusal %v), want a refused 'cancelled'", res, refusal)
	}
	if res.Cancelled {
		t.Fatal("an already-cancelled loop was cancelled again")
	}
	// No terminal write: the loop is still running (the flag was planted, the
	// status never moved), and no loop.finished rode.
	if st, _ := Pinned(context.Background(), dep.Store, id); st.Status != "running" {
		t.Fatalf("status = %q, want running (the refused cancel wrote no terminal)", st.Status)
	}
	if ev.count("loop.finished") != 0 {
		t.Fatal("a refused cancel emitted loop.finished")
	}
}

// TestCancelCommitsEvenIfRunnerWaitTimesOut — LC4.1: a cancel does not hang on
// a runner that will not settle. When the Wait seam times out (false) the
// cancel still commits — the runner's next advance sees the terminal loop and
// exits — so a hung runner never blocks a person's cancel.
func TestCancelCommitsEvenIfRunnerWaitTimesOut(t *testing.T) {
	id, dep, ev := newControlLoop(t)
	h := &fakeHooks{wait: false} // the runner never settles within the bound

	res, refusal, err := Cancel(context.Background(), dep, id, userActor, "now", h.hooks())
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if refusal != nil {
		t.Fatalf("Cancel refused on a timeout: %v", refusal)
	}
	if !res.Cancelled || res.Status != "aborted" {
		t.Fatalf("Cancel = %+v, want cancelled/aborted — a timeout must not block the cancel", res)
	}
	// It committed: the terminal is in the store and the run settled.
	if st, _ := Pinned(context.Background(), dep.Store, id); st.Status != "aborted" {
		t.Fatalf("status = %q, want aborted (the cancel committed past the timeout)", st.Status)
	}
	runs, err := dep.Store.ListRuns(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].State != "cancelled" {
		t.Fatalf("runs = %+v, want one run settled cancelled past the timeout", runs)
	}
	// And it spoke its terminal: exactly one loop.finished, the loop's status.
	if got := ev.count("loop.finished"); got != 1 {
		t.Fatalf("loop.finished emitted %d times, want 1 (the cancel committed past the timeout)", got)
	}
}

// TestCancelRunStateIsCancelled — LC4: the run's state reads cancelled, mapped
// from the loop's aborted terminal by runStateFor (the one mapping, never a
// second). A cancel settles the run row through emitIfOver → EndRun, and the
// state the run row records is cancelled — the run ended, the loop's terminal
// is aborted, and the two agree through the single mapping.
func TestCancelRunStateIsCancelled(t *testing.T) {
	id, dep, _ := newControlLoop(t)
	h := &fakeHooks{wait: true}

	res, refusal, err := Cancel(context.Background(), dep, id, userActor, "now", h.hooks())
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if refusal != nil {
		t.Fatalf("Cancel refused: %v", refusal)
	}
	if !res.Cancelled {
		t.Fatalf("Cancel = %+v, want cancelled", res)
	}
	// The run row the cancel settled: its state is cancelled (runStateFor maps
	// the loop's aborted terminal to the run's cancelled), not a second status.
	runs, err := dep.Store.ListRuns(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1 (the run Start created)", len(runs))
	}
	if runs[0].State != "cancelled" {
		t.Fatalf("run state = %q, want cancelled (LC4: the run's state is cancelled, not a second terminal)", runs[0].State)
	}
	// And the mapping itself: runStateFor is the one function the run state
	// reads — aborted maps to cancelled, nothing else does.
	if got := runStateFor(LoopState{Status: "aborted"}); got != "cancelled" {
		t.Fatalf("runStateFor(aborted) = %q, want cancelled", got)
	}
}
