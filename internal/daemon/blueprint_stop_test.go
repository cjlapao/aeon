package daemon

// S31 T2 — a person stops a loop step and the loop PARKS (D6).
//
// These are the integration-level proofs the brief pins (§5, D6):
//
//   - `step.stop` on a step running inside `loop.start` suspends the STEP
//     (`suspended`, `pause_reason` `user` — told apart from a real question
//     by the reason, never by a different status) and moves the LOOP to
//     `waiting` with reason `stopped`, holding no runner goroutine;
//   - the events say the same story and nothing else: `step.stopped` carries
//     `stepId` + `loopId`, `loop.waiting` carries `reason:"stopped"` +
//     `stepId`, and no `step.done` with a failed/reject outcome ever appears
//     — `failed` accuses the model, `blocked` accuses the ladder, and both
//     are wrong for "the person pressed stop";
//   - `step.continue {message}` resumes the parked ladder: the message
//     reaches the model exactly ONCE (checkpointed with the flip to running,
//     so retry cannot double it and a crash cannot lose it), the step then
//     completes, and the ladder advances on its own;
//   - a second stop during the continuation re-parks (it does NOT block),
//     and a final continue without a message takes the loop to done;
//   - stopping a step that already finished answers `stopped: false`
//     honestly, and a stop between tool turns leaves the second tool never
//     run at all.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// gatedModel plays scripted turns and lets a test freeze a call between its
// deltas and its result — the window `step.stop` is aimed at. After release
// it still emits, because a cancelled run's sink may simply stop reading:
// every send picks ctx.Done over blocking forever.
type gatedModel struct {
	mu    sync.Mutex
	turns []gatedTurn
	calls int
	// sent records the Messages each call actually carried, so "exactly
	// once" is measured at the provider boundary and not at the row.
	sent map[int][]model.Message
}

type gatedTurn struct {
	enter    chan struct{} // closed when the call starts; nil = immediate
	release  chan struct{} // waited on before completing; nil = immediate
	deltas   []string
	toolUses []model.Block
}

func (m *gatedModel) snapshot(idx int, msgs []model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = map[int][]model.Message{}
	}
	m.sent[idx] = append([]model.Message(nil), msgs...)
}

// callCount is the locked read of the call counter: Stream writes m.calls
// under the mutex, and a test polling for "the model was reached" must read
// it the same way. The race detector caught the bare read this replaces in
// S39's close-out -race pass (test-side only — both racing accesses were in
// test code, which is why the official suite never blinked).
func (m *gatedModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *gatedModel) lastSent(idx int) []model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent[idx]
}

const okJSON = `{"status":"ok","summary":"all good"}`

func (m *gatedModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	m.mu.Lock()
	idx := m.calls
	m.calls++
	var turn gatedTurn
	if idx < len(m.turns) {
		turn = m.turns[idx]
	} else {
		turn = gatedTurn{deltas: []string{okJSON}}
	}
	m.mu.Unlock()
	m.snapshot(idx, req.Messages)

	if turn.enter != nil {
		close(turn.enter)
	}
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if turn.release != nil {
			select {
			case <-turn.release:
			case <-ctx.Done():
				return
			}
		}
		for _, text := range turn.deltas {
			d := model.Delta{Kind: model.DeltaText, Text: text}
			select {
			case deltas <- d:
			case <-ctx.Done():
				return
			}
		}
		for i := range turn.toolUses {
			block := turn.toolUses[i]
			select {
			case deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case results <- model.StreamResult{Attempts: 1}:
		case <-ctx.Done():
		}
	}()
	return deltas, results, errs
}

func gatedFixture(t *testing.T, turns []gatedTurn, extra ...policy.Policy) (*streamFixture, *gatedModel) {
	t.Helper()
	s := recoveryStore(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello streaming\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedRules(t, s, time.Now().UTC(), extra...)
	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	m := &gatedModel{turns: turns}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: m, Logger: w, Notify: w.notify,
	})
	// Drain the fire-and-forget answer/continue goroutines before the store
	// closes. recoveryStore above already registered the store close; this runs
	// before it (LIFO), so a continuation an answer spawned cannot write the
	// transcript into a store the cleanup is closing — the panic that lands on
	// a later test. No-op when no continuation is in flight.
	t.Cleanup(func() { d.Table.WaitContinuations() })
	return &streamFixture{d: d, w: w, s: s, root: root, route: route}, m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// stepIDFromEvents finds the in-flight step id from its step.started event
// (optionally for a given ladder step name), through the wire — the same
// view a real subscriber has.
func stepIDFromEvents(t *testing.T, f *streamFixture, connID, subID, ladderStep string, except int64) (int64, bool) {
	for _, ev := range streamEvents(f.w, connID, subID) {
		if ev.Kind != "step.started" {
			continue
		}
		p := decodeStepPayload(t, ev)
		if p.StepID > 0 && p.StepID != except && (ladderStep == "" || p.Step == ladderStep) {
			return p.StepID, true
		}
	}
	return 0, false
}

func dispatchJSON(t *testing.T, f *streamFixture, connID string, method protocol.MethodName, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.d.Table.Dispatch(context.Background(), connID, method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("%s response does not decode: %v\n%s", method, err, out)
	}
	return m
}

func eventCount(f *streamFixture, connID, subID, kind string) int {
	n := 0
	for _, ev := range streamEvents(f.w, connID, subID) {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func occurrences(msgs []model.Message, text string) int {
	n := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Kind == model.BlockText && strings.Contains(b.Text, text) {
				n++
			}
		}
	}
	return n
}

func runtimeOf(t *testing.T, f *streamFixture, loopID string) loop.RuntimeState {
	t.Helper()
	pin, err := f.s.ReadPinnedLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	var st loop.RuntimeState
	if err := json.Unmarshal([]byte(pin.RuntimeJSON), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStopParksTheLoopContinueDeliversTheMessageOnceAndReStopReParks(t *testing.T) {
	gate1, gate2 := make(chan struct{}), make(chan struct{})
	enter1, enter2 := make(chan struct{}), make(chan struct{})
	f, m := gatedFixture(t, []gatedTurn{
		{deltas: []string{"staring at the repo"}, enter: enter1, release: gate1},
		{deltas: []string{okJSON}},
		{deltas: []string{"starting the second"}, enter: enter2, release: gate2},
		{deltas: []string{okJSON}},
	})
	initConn(t, f.d, context.Background(), "c1", "stopper")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "survey", "report"))

	<-enter1
	var step1 int64
	waitFor(t, "the first step.started", func() bool {
		id, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "survey", 0)
		step1 = id
		return ok
	})

	// --- stop mid-model ------------------------------------------------------
	resp := dispatchJSON(t, f, "c1", protocol.MethodStepStop, protocol.StepStopParams{StepID: step1})
	if resp["stopped"] != true {
		t.Fatalf("stop response = %v, want stopped:true", resp)
	}
	close(gate1)

	waitFor(t, "step.stopped on the wire", func() bool {
		return eventCount(f, "c1", sub.SubscriptionID, "step.stopped") == 1
	})
	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "waiting" {
		t.Fatalf("loop status = %q, want waiting", pin.Loop.Status)
	}
	st := runtimeOf(t, f, loopID)
	if st.LastReason != "stopped" || st.LastStepID != step1 {
		t.Fatalf("runtime = reason %q step %d, want stopped/%d", st.LastReason, st.LastStepID, step1)
	}
	if f.d.loopRunner.Active(loopID) {
		t.Fatal("the runner holds a goroutine for a parked loop")
	}

	// The step row: suspended with a PERSON's reason, not a question's.
	row, err := f.s.ReadStep(step1)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "suspended" || row.PauseReason != "user" {
		t.Fatalf("step row = %s/%q, want suspended/user", row.Status, row.PauseReason)
	}

	// The events: exactly what §5 names, and no accusation.
	for _, ev := range streamEvents(f.w, "c1", sub.SubscriptionID) {
		switch ev.Kind {
		case "step.stopped":
			p := decodeStepPayload(t, ev)
			if p.StepID != step1 || p.LoopID != loopID {
				t.Fatalf("step.stopped payload = %+v, want stepId %d loopId %s", p, step1, loopID)
			}
		case "loop.waiting":
			pl := decodeMapPayload(t, ev)
			if pl["reason"] != "stopped" || pl["stepId"] != float64(step1) {
				t.Fatalf("loop.waiting payload = %v, want reason stopped stepId %d", pl, step1)
			}
		case "step.done":
			p := decodeStepPayload(t, ev)
			if p.Outcome == "failed" || p.Outcome == "reject" {
				t.Fatalf("a stopped step published step.done outcome=%q — parking is not failing", p.Outcome)
			}
		}
	}

	// --- continue WITH a message ---------------------------------------------
	msgText := "also count the test files"
	resp = dispatchJSON(t, f, "c1", protocol.MethodStepContinue,
		protocol.StepContinueParams{StepID: step1, Message: msgText})
	if resp["status"] != "running" {
		t.Fatalf("continue response = %v, want status running", resp)
	}
	waitFor(t, "the continuation's model call", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.sent[1]
		return ok
	})
	if n := occurrences(m.lastSent(1), msgText); n != 1 {
		t.Fatalf("the model saw the message %d times, want exactly 1 (D6 exactly-once)", n)
	}
	// step.continued rode the wire with the loop named.
	waitFor(t, "step.continued", func() bool {
		return eventCount(f, "c1", sub.SubscriptionID, "step.continued") == 1
	})
	for _, ev := range streamEvents(f.w, "c1", sub.SubscriptionID) {
		if ev.Kind == "step.continued" {
			p := decodeStepPayload(t, ev)
			if p.StepID != step1 || p.LoopID != loopID {
				t.Fatalf("step.continued payload = %+v, want stepId %d loopId %s", p, step1, loopID)
			}
		}
	}

	// The first step completes and the LADDER MOVES — call 2 is step two.
	<-enter2
	if step2, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "report", step1); !ok || step2 == 0 {
		t.Fatal("the ladder never advanced after the continuation")
	}

	// --- stop the continuation's successor ------------------------------------
	step2, _ := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "report", step1)
	resp = dispatchJSON(t, f, "c1", protocol.MethodStepStop, protocol.StepStopParams{StepID: step2})
	if resp["stopped"] != true {
		t.Fatalf("second stop response = %v", resp)
	}
	close(gate2)
	pin = waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "waiting" {
		t.Fatalf("after the second stop status = %q, want waiting (re-parked, NOT blocked)", pin.Loop.Status)
	}
	if got := eventCount(f, "c1", sub.SubscriptionID, "loop.waiting"); got != 2 {
		t.Fatalf("loop.waiting emitted %d times, want 2 (one per stop)", got)
	}

	// --- continue WITHOUT a message: the loop reaches its end -----------------
	resp = dispatchJSON(t, f, "c1", protocol.MethodStepContinue, protocol.StepContinueParams{StepID: step2})
	if resp["status"] != "running" {
		t.Fatalf("second continue response = %v", resp)
	}
	waitFor(t, "the second continuation's model call", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.sent[3]
		return ok
	})
	pin = waitLoopSettled(t, f, loopID)
	// The handback that finishes the loop is ASYNC: after `step.continue`
	// returns, the detached run completes, reports through OnStepContinued,
	// CompleteSuspendedStep records the step and kicks the runner, and the
	// runner's advance writes `done`. `waitLoopSettled` alone would answer
	// immediately with the pre-continue `waiting` — a settled state the
	// runner holds no goroutine for. Wait for the transition, then assert.
	waitFor(t, "the loop to finish after the last continue", func() bool {
		p, err := f.s.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatal(err)
		}
		pin = p
		return p.Loop.Status == "done"
	})
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status = %q, want done", pin.Loop.Status)
	}
	// And wait for the runner to release the loop entirely: `Active` going
	// false means the goroutine has finished EMITTING (loop.finished reads
	// the store through loopView) — a test that returns while it still runs
	// races its own deferred store close. The event is watched FIRST because
	// the completing hand-back emits it before the runner would even be
	// consulted, and the row can read `done` microseconds before the last
	// store-reading emit of that goroutine finishes.
	waitFor(t, "loop.finished on the wire", func() bool {
		return eventCount(f, "c1", sub.SubscriptionID, "loop.finished") >= 1
	})
	waitLoopSettled(t, f, loopID)

	// Store-level exactly-once, after everything settled: the message sits in
	// the FIRST step's transcript once — not zero (crash-lost) and not two
	// (retry-doubled).
	tr := f.s.ReadTranscript(step1)
	if !tr.OK {
		t.Fatalf("transcript of the continued step: %s", tr.Reason())
	}
	if n := occurrences(tr.Messages, msgText); n != 1 {
		t.Fatalf("the stored transcript carries the message %d times, want 1", n)
	}
}

func TestStopAStepThatAlreadyFinishedReportsNotStopped(t *testing.T) {
	f, _ := gatedFixture(t, []gatedTurn{{deltas: []string{okJSON}}})
	initConn(t, f.d, context.Background(), "c1", "stopper")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "solo"))
	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status = %q, want done", pin.Loop.Status)
	}
	stepID, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "solo", 0)
	if !ok {
		t.Fatal("no step.started for the finished step")
	}
	resp := dispatchJSON(t, f, "c1", protocol.MethodStepStop, protocol.StepStopParams{StepID: stepID})
	if resp["stopped"] != false {
		t.Fatalf("stopping a finished step answered %v, want stopped:false", resp)
	}
	row, err := f.s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "done" {
		t.Fatalf("the finished step became %q", row.Status)
	}
}

func TestStopBetweenToolTurnsNeverRunsTheSecondTool(t *testing.T) {
	gate := make(chan struct{})
	enter := make(chan struct{})
	f, _ := gatedFixture(t, []gatedTurn{
		{toolUses: []model.Block{toolUse("w1", "write", `{"path":"A.md","content":"first\n"}`)}},
		{enter: enter, release: gate, deltas: []string{okJSON}, toolUses: []model.Block{toolUse("w2", "write", `{"path":"B.md","content":"second\n"}`)}},
	}, toolRulePolicy("allow-write", "default", "write", policy.Allow))
	initConn(t, f.d, context.Background(), "c1", "stopper")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "default", "draft", "wrap"))

	<-enter // the SECOND model call is in flight; the first tool already ran.
	_, err := os.Stat(filepath.Join(f.root, "A.md"))
	if err != nil {
		t.Fatalf("the first tool's effect vanished: %v", err)
	}
	var step1 int64
	waitFor(t, "step.started", func() bool {
		id, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "draft", 0)
		step1 = id
		return ok
	})
	resp := dispatchJSON(t, f, "c1", protocol.MethodStepStop, protocol.StepStopParams{StepID: step1})
	if resp["stopped"] != true {
		t.Fatalf("stop between tools answered %v", resp)
	}
	close(gate) // the model now even offers tool two — the run is cancelled.
	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "waiting" {
		t.Fatalf("status = %q, want waiting", pin.Loop.Status)
	}
	if _, err := os.Stat(filepath.Join(f.root, "B.md")); !os.IsNotExist(err) {
		t.Fatalf("the second tool RAN despite the stop (B.md exists: %v)", err)
	}
}
