package daemon

// S31 T1 — loop steps stream live (D1, D2).
//
// Before this slice a step run by a loop published NOTHING: RunStepForLoop
// carried no SubscriptionID, handleStep fell to runtime.DiscardSink{}, and
// Daemon.publish passed an empty project root, so a subscriber filtered by
// Filter{ComponentRoot} could not select a loop step's stream at all (brief §2,
// CODEMAP (a)/(g)). These tests are the contract the fix must satisfy:
//
//   - the project subscriber receives, IN ORDER, loop.started,
//     loop.step.dispatched(stepId), step.started(stepId, loopId, lane, step,
//     agent), tool start/end, step.done, loop.advanced, … , loop.finished;
//   - every one of them carries the step's componentRoot on the envelope, which
//     is what makes CODEMAP (g)'s load-bearing property assertable: a foreign
//     root's subscriber receives NONE of them (an empty-root event matches
//     EVERY subscriber, so "empty" here would leak the stream);
//   - cursor replay (subscribe {after: 0}) answers the same sequence from the
//     durable table — the live stream and the replay are one log, not two.
//
// And the Q1 measurement: the events/sec and `seq` allocations a long
// model.delta reply costs the durable allocator. It is recorded in the task
// notes, not asserted as a threshold — a fabricated bound fails CI on a slow
// box and hides the number the coalescing decision is supposed to read.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// streamModel plays scripted turns over the streaming interface — the same
// shape as scriptedModel in step_test.go, extended with MANY text deltas per
// turn, which is what a streamed reply actually is (and what Q1 measures).
type streamModel struct {
	turns []streamTurn
	calls int
}

type streamTurn struct {
	deltas   []string
	toolUses []model.Block
}

func (m *streamModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	index := m.calls
	m.calls++
	var turn streamTurn
	if index < len(m.turns) {
		turn = m.turns[index]
	} else {
		turn = streamTurn{deltas: []string{`{"status":"ok","summary":"out of script"}`}}
	}

	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		for _, text := range turn.deltas {
			deltas <- model.Delta{Kind: model.DeltaText, Text: text}
		}
		for i := range turn.toolUses {
			block := turn.toolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}
		}
		results <- model.StreamResult{Attempts: 1}
	}()
	return deltas, results, errs
}

// streamFixture builds a real daemon over a real store with one scripted
// model, and returns the pieces a stream assertion needs.
type streamFixture struct {
	d     *Daemon
	w     *wire
	s     *store.Store
	root  string
	route model.Route
}

func newStreamFixture(t *testing.T, turns []streamTurn) *streamFixture {
	t.Helper()
	s := recoveryStore(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello streaming\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The gate denies everything over an empty set (PL5), so the `read` call
	// the two-step script makes needs a real allow row — seeded through the
	// write boundary, the same door `aeon policy` opens.
	seedRules(t, s, time.Now().UTC(),
		toolRulePolicy("allow-read-for-read-only", "read-only", "read", policy.Allow))

	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: &streamModel{turns: turns}, Logger: w, Notify: w.notify,
	})
	// Drain the fire-and-forget answer/continue goroutines before the store
	// closes (recoveryStore above registered the close; this runs before it,
	// LIFO). A continuation an answer spawned must not write the transcript into
	// a store the cleanup is closing — that is the panic that landed on a later
	// test. No-op when no continuation is in flight.
	t.Cleanup(func() { d.Table.WaitContinuations() })
	return &streamFixture{d: d, w: w, s: s, root: root, route: route}
}

// writeStreamBlueprint pins a blueprint of one direct model-step lane per id.
// A non-empty agent runs the steps through that registered handler; the empty
// agent takes the ad-hoc default handler. (Ladder lanes need `iterate`, which
// drags in the manifest's collection fields — direct lanes are the shape the
// S8 built-in two-step uses, and the position resolution is what a stream
// reader sees, which is what this exercises.)
func writeStreamBlueprint(t *testing.T, agent string, ids ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("id: stream-two-step\nschemaVersion: 1\nversion: 1\nlanes:\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "  - id: %s\n    step: {id: %s, kind: model-step, agent: %s}\n", id, id, agent)
	}
	path := filepath.Join(t.TempDir(), "stream-two-step.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func startStreamLoop(t *testing.T, f *streamFixture, blueprintPath string) string {
	t.Helper()
	res, err := f.d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: blueprintPath, Goal: "stream live", WorkspaceRoot: f.root, Route: f.route.ID,
	})
	if err != nil {
		t.Fatalf("start blueprint loop: %v", err)
	}
	loopID, ok := res.(map[string]any)["id"].(string)
	if !ok || loopID == "" {
		t.Fatalf("loop start returned no id: %#v", res)
	}
	return loopID
}

func waitLoopSettled(t *testing.T, f *streamFixture, loopID string) store.PinnedLoop {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		pin, err := f.s.ReadPinnedLoop(loopID)
		if err != nil {
			t.Fatalf("read pinned loop: %v", err)
		}
		if pin.Loop.Status != "running" && !f.d.loopRunner.Active(loopID) {
			return pin
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s did not settle in time; status %s active %v",
				loopID, pin.Loop.Status, f.d.loopRunner.Active(loopID))
		}
		time.Sleep(time.Millisecond)
	}
}

// streamEvents returns the events delivered to one connection for one
// subscription, in delivery order.
func streamEvents(w *wire, connID, subID string) []protocol.Event {
	var out []protocol.Event
	for _, sent := range w.delivered() {
		if sent.ConnID == connID && sent.Method == "event" && sent.Event.SubscriptionID == subID {
			out = append(out, sent.Event)
		}
	}
	return out
}

func eventKinds(events []protocol.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}

// stepPayloadProbe decodes the enriched step-event payload without naming
// Go fields the implementation has not added yet — the WIRE is the contract.
type stepPayloadProbe struct {
	StepID  int64  `json:"stepId"`
	LoopID  string `json:"loopId"`
	Lane    string `json:"lane"`
	Step    string `json:"step"`
	Agent   string `json:"agent"`
	Tool    string `json:"tool"`
	Result  string `json:"result"`
	IsError bool   `json:"isError"`
	Outcome string `json:"outcome"`
}

func decodeStepPayload(t *testing.T, ev protocol.Event) stepPayloadProbe {
	t.Helper()
	var p stepPayloadProbe
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("payload of %s does not decode: %v\n%s", ev.Kind, err, ev.Payload)
	}
	return p
}

func decodeMapPayload(t *testing.T, ev protocol.Event) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("payload of %s does not decode: %v\n%s", ev.Kind, err, ev.Payload)
	}
	return p
}

// twoStepScript plays, per step: one `read` tool call, then a handoff.
func twoStepScript() []streamTurn {
	read := []model.Block{toolUse("read-1", "read", `{"path":"notes.txt"}`)}
	ok := []string{`{"status":"ok","summary":"done"}`}
	return []streamTurn{
		{toolUses: read}, {deltas: ok},
		{toolUses: []model.Block{toolUse("read-2", "read", `{"path":"notes.txt"}`)}}, {deltas: ok},
	}
}

func TestLoopStepEventsStreamLiveToTheComponentSubscriber(t *testing.T) {
	f := newStreamFixture(t, twoStepScript())
	initConn(t, f.d, context.Background(), "watcher", "watcher")
	sub := subscribe(t, f.d, "watcher", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: f.root, Kinds: []string{"step.*", "loop.*"}},
	})

	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "draft", "polish"))
	settled := waitLoopSettled(t, f, loopID)
	if settled.Loop.Status != "done" {
		t.Fatalf("loop status %q, want done (runtime %s)", settled.Loop.Status, settled.RuntimeJSON)
	}

	events := streamEvents(f.w, "watcher", sub.SubscriptionID)
	want := []string{
		"loop.started",
		// Each model call is one step.turn.completed after its text and before its tools (loop-replay step 2).
		"loop.step.dispatched", "step.started", "step.turn.completed", "step.tool.start", "step.policy.decided", "step.tool.end", "step.turn.completed", "step.done", "loop.advanced",
		"loop.step.dispatched", "step.started", "step.turn.completed", "step.tool.start", "step.policy.decided", "step.tool.end", "step.turn.completed", "step.done", "loop.advanced",
		"loop.finished",
	}
	if got := eventKinds(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("delivered kinds\n got %v\nwant %v", got, want)
	}

	// D1: every event of this loop's stream carries the step's project root on
	// the envelope — that is what makes it filterable, and what makes the
	// foreign-root test below mean something.
	for _, ev := range events {
		if ev.ComponentRoot != f.root {
			t.Errorf("%s (seq %d): envelope componentRoot %q, want %q — an empty root matches "+
				"every subscriber and would leak this stream across projects", ev.Kind, ev.Seq, ev.ComponentRoot, f.root)
		}
	}

	// D2: each dispatch names the ladder move and the step id the step stream
	// then carries — the link the loop view uses. And each step event carries
	// the full identity.
	stepIDByDispatch := map[string]int64{}
	for i, ev := range events {
		switch ev.Kind {
		case "loop.step.dispatched":
			p := decodeMapPayload(t, ev)
			for field, want := range map[string]any{
				"loopId": loopID, "agent": "read-only", "route": "r1",
			} {
				if p[field] != want {
					t.Errorf("dispatch %d: %s = %#v, want %#v", i, field, p[field], want)
				}
			}
			stepName, _ := p["step"].(string)
			if stepName != "draft" && stepName != "polish" {
				t.Errorf("dispatch %d: step %q, want draft|polish", i, stepName)
			}
			if lane, _ := p["lane"].(string); lane != stepName {
				t.Errorf("dispatch %d: lane %q, want %q (direct lanes)", i, lane, stepName)
			}
			stepID, _ := p["stepId"].(float64)
			if stepID == 0 {
				t.Fatalf("dispatch %d carries no stepId: %v", i, p)
			}
			stepIDByDispatch[stepName] = int64(stepID)
			// D2 ordering: the dispatch is IMMEDIATELY followed by that step's
			// own first event, and its stepId matches.
			if i+1 >= len(events) || events[i+1].Kind != "step.started" {
				t.Fatalf("dispatch %d is not followed by step.started: %v", i, eventKinds(events)[max(i, 0):min(i+3, len(events))])
			}
			first := decodeStepPayload(t, events[i+1])
			if first.StepID != int64(stepID) {
				t.Errorf("dispatch %d names stepId %v but step.started carries %d — the link is broken",
					i, int64(stepID), first.StepID)
			}
		case "step.started":
			p := decodeStepPayload(t, ev)
			if p.LoopID != loopID || p.Lane == "" {
				t.Errorf("step.started %d: loopId=%q lane=%q, want %q and a lane", i, p.LoopID, p.Lane, loopID)
			}
			if p.Step != "draft" && p.Step != "polish" {
				t.Errorf("step.started %d: step %q, want draft|polish", i, p.Step)
			}
			if p.Agent != "read-only" {
				t.Errorf("step.started %d: agent %q, want read-only", i, p.Agent)
			}
			if want := stepIDByDispatch[p.Step]; p.StepID != want {
				t.Errorf("step.started %d: stepId %d, want the dispatched %d", i, p.StepID, want)
			}
		case "step.tool.end":
			p := decodeStepPayload(t, ev)
			if p.Tool != "read" || p.IsError {
				t.Errorf("step.tool.end %d: tool %q isError %v, want read/false", i, p.Tool, p.IsError)
			}
		case "step.done":
			p := decodeStepPayload(t, ev)
			if p.Outcome != "ok" {
				t.Errorf("step.done %d: outcome %q, want ok", i, p.Outcome)
			}
			if p.StepID == 0 {
				t.Errorf("step.done %d carries no stepId", i)
			}
		}
	}
	if stepIDByDispatch["draft"] == 0 || stepIDByDispatch["polish"] == 0 ||
		stepIDByDispatch["draft"] == stepIDByDispatch["polish"] {
		t.Fatalf("the two steps were not dispatched with distinct ids: %v", stepIDByDispatch)
	}
}

func TestForeignComponentSubscriberReceivesNoLoopStepEvents(t *testing.T) {
	// CODEMAP (g): an EMPTY-root event matches every subscriber, including
	// project-scoped ones. So this assertion is only real if the loop's events
	// carry the step's REAL root — which the loop step sink now publishes with.
	f := newStreamFixture(t, twoStepScript())
	initConn(t, f.d, context.Background(), "watcher", "watcher")
	initConn(t, f.d, context.Background(), "foreign", "foreign")
	watcherSub := subscribe(t, f.d, "watcher", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: f.root, Kinds: []string{"step.*", "loop.*"}},
	})
	elsewhere := filepath.Join(t.TempDir(), "other-project")
	foreignSub := subscribe(t, f.d, "foreign", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: elsewhere, Kinds: []string{"step.*", "loop.*"}},
	})

	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "draft", "polish"))
	waitLoopSettled(t, f, loopID)

	foreign := streamEvents(f.w, "foreign", foreignSub.SubscriptionID)
	if len(foreign) != 0 {
		t.Fatalf("foreign-root subscriber received %d events (%v) — a loop's step stream leaked across projects",
			len(foreign), eventKinds(foreign))
	}

	// And the watcher DID receive its stream — the leak assertion above must
	// not pass because nothing arrived for anybody.
	watcher := streamEvents(f.w, "watcher", watcherSub.SubscriptionID)
	if len(watcher) == 0 {
		t.Fatal("the watcher received nothing; the foreign assertion would be vacuous")
	}
}

func TestCursorReplayReturnsTheSameLoopStepSequence(t *testing.T) {
	f := newStreamFixture(t, twoStepScript())
	initConn(t, f.d, context.Background(), "watcher", "watcher")
	sub := subscribe(t, f.d, "watcher", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: f.root, Kinds: []string{"step.*", "loop.*"}},
	})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "draft", "polish"))
	waitLoopSettled(t, f, loopID)
	live := streamEvents(f.w, "watcher", sub.SubscriptionID)
	if len(live) == 0 {
		t.Fatal("the live stream is empty; the replay assertion below would be vacuous")
	}

	// Replay through a FRESH daemon over the same store, not the live one:
	// the live daemon would answer from its bounded ring and prove nothing
	// about the durable `seq` cursor the task demands. The fresh EventLog has
	// no ring, so the replay is served by EventBacking.Range against the
	// `event` table — which is exactly where an empty stored componentRoot
	// would (correctly) exclude the row from a root-filtered cursor and this
	// assertion would catch it.
	seen := uint64(0)
	w2 := &wire{}
	d2 := New(Options{Store: f.s, Logger: w2, Notify: w2.notify})
	initConn(t, d2, context.Background(), "replayer", "replayer")
	replaySub := subscribe(t, d2, "replayer", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: f.root, Kinds: []string{"step.*", "loop.*"}},
		After:  &seen,
	})
	replayed := streamEvents(w2, "replayer", replaySub.SubscriptionID)

	if !reflect.DeepEqual(eventKinds(replayed), eventKinds(live)) {
		t.Fatalf("replay kinds\n got %v\nwant %v", eventKinds(replayed), eventKinds(live))
	}
	for i := range live {
		var a, b map[string]any
		if err := json.Unmarshal(live[i].Payload, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(replayed[i].Payload, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("replay %d (%s): payload %#v, live %#v", i, live[i].Kind, b, a)
		}
		if replayed[i].ComponentRoot != live[i].ComponentRoot {
			t.Errorf("replay %d (%s): componentRoot %q, live %q", i, live[i].Kind, replayed[i].ComponentRoot, live[i].ComponentRoot)
		}
	}
	_ = loopID
}

// TestAdHocStepEventsCarryStepIDAndComponentRoot covers the other half of D1's
// sink fix: the AD-HOC `step` path (with a SubscriptionID) published through
// stepSink without StepID and with an empty root.
func TestAdHocStepEventsCarryStepIDAndComponentRoot(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: &streamModel{turns: []streamTurn{
			{toolUses: []model.Block{toolUse("a1", "read", `{"path":"notes.txt"}`)}},
			{deltas: []string{`{"status":"ok","summary":"ad-hoc"}`}},
		}},
		Logger: w, Notify: w.notify,
	})
	initConn(t, d, context.Background(), "viewer", "viewer")
	sub := subscribe(t, d, "viewer", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: root, Kinds: []string{"step.*"}},
	})

	raw, err := json.Marshal(protocol.StepParams{
		Prompt: "read the notes", WorkspaceRoot: root, SubscriptionID: sub.SubscriptionID, LoopSlug: "ad-hoc-stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := d.Table.Dispatch(context.Background(), "viewer", protocol.MethodStep, raw)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	res, ok := response.(protocol.StepResult)
	if !ok || res.StepID == 0 {
		t.Fatalf("step result: %#v", response)
	}

	events := streamEvents(w, "viewer", sub.SubscriptionID)
	if len(events) == 0 {
		t.Fatal("the ad-hoc step streamed nothing to its subscriber")
	}
	for _, ev := range events {
		p := decodeStepPayload(t, ev)
		if p.StepID != res.StepID {
			t.Errorf("%s: stepId %d, want %d — stepSink dropped the id", ev.Kind, p.StepID, res.StepID)
		}
		if ev.ComponentRoot != root {
			t.Errorf("%s: envelope componentRoot %q, want %q", ev.Kind, ev.ComponentRoot, root)
		}
	}
}

// TestLongModelDeltaStreamLoadIsMeasured answers brief §10 Q1 with a real
// number: how many events per second, and how many durable `seq` allocations,
// a step streaming a long model.delta reply costs the event log. Nothing here
// asserts a performance threshold — the notes record the number, and the
// coalescing decision is taken from it rather than from a guess.
func TestLongModelDeltaStreamLoadIsMeasured(t *testing.T) {
	const deltas = 10_000

	f := newStreamFixture(t, nil)
	// The scripted turns are installed per-run instead: the default handler
	// (no agent) needs no seeded allow rule, and one text-only turn keeps the
	// measurement about delta events alone.
	deltasTurn := make([]string, 0, deltas)
	full := `{"status":"ok","summary":"` + strings.Repeat("alpha bravo charlie delta ", 3000) + `"}`
	chunk := (len(full) + deltas - 1) / deltas
	for i := 0; i < deltas; i++ {
		lo := i * chunk
		if lo >= len(full) {
			deltasTurn = append(deltasTurn, "")
			continue
		}
		hi := lo + chunk
		if hi > len(full) {
			hi = len(full)
		}
		deltasTurn = append(deltasTurn, full[lo:hi])
	}
	f.d.Table.Models = &streamModel{turns: []streamTurn{{deltas: deltasTurn}}}

	initConn(t, f.d, context.Background(), "watcher", "watcher")
	sub := subscribe(t, f.d, "watcher", protocol.SubscribeParams{
		Filter: protocol.Filter{ComponentRoot: f.root, Kinds: []string{"model.delta"}},
	})

	headBefore := f.d.Table.Events.Head()
	started := time.Now()
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "default", "draft"))
	settled := waitLoopSettled(t, f, loopID)
	elapsed := time.Since(started)
	headAfter := f.d.Table.Events.Head()

	if settled.Loop.Status != "done" {
		t.Fatalf("loop status %q, want done (runtime %s)", settled.Loop.Status, settled.RuntimeJSON)
	}

	events := streamEvents(f.w, "watcher", sub.SubscriptionID)
	var lastSeq uint64
	for _, ev := range events {
		if ev.Seq <= lastSeq {
			t.Fatalf("seq %d did not advance past %d — the allocator is not monotonic", ev.Seq, lastSeq)
		}
		lastSeq = ev.Seq
	}

	allocated := headAfter - headBefore
	perSecond := float64(len(events)) / elapsed.Seconds()
	t.Logf("Q1 measurement: %d model.delta events streamed through the daemon in %v — "+
		"~%.0f events/sec end to end, %d durable seq allocations (%d rows) for the whole run",
		len(events), elapsed.Round(time.Millisecond), perSecond, allocated, allocated)

	// Streamed text is published in joined chunks (delta_coalesce.go): far fewer events than tokens, and not one character
	// lost or reordered. The queue the app reads from is bounded, so this is what keeps state events from being dropped.
	if len(events) == 0 || len(events) >= deltas/10 {
		t.Fatalf("%d events for %d deltas: the chunks were not joined", len(events), deltas)
	}
	var got strings.Builder
	for _, ev := range events {
		var p struct {
			Text string `json:"text"`
		}
		raw, _ := json.Marshal(ev.Payload)
		_ = json.Unmarshal(raw, &p)
		got.WriteString(p.Text)
	}
	if got.String() != full {
		t.Fatalf("the joined text (%d bytes) is not the streamed text (%d bytes)", got.Len(), len(full))
	}
	// Every event allocates exactly one seq.
	if allocated < uint64(len(events)) {
		t.Fatalf("%d seq allocations for %d events — an allocator went missing", allocated, len(events))
	}
}
