package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// conversation_test.go walks the turn end to end (S39 T6, CR3/D2/D3): a
// message to a resting loop starts ONE ordinary step by the conversation
// agent — steerably, recoverably, like any step — and every refusal (a
// step running, a step waiting, a loop not resting) is data with a remedy,
// never a socket error, and never writes the thread.

const turnOK = `{"status":"ok","summary":"hello"}`

// restedLoop builds a pinned loop at the fixture's root and parks it the
// way the engine does: run 1 closed, status idle. converse names the
// blueprint's conversation agent ("" leaves the key off, exercising the
// daemon's default).
func restedLoop(t *testing.T, f *streamFixture, converse string) store.Loop {
	t.Helper()
	return restedLoopWith(t, f, converse, nil)
}

// restedLoopWith is restedLoop with the D2 declaration made at start —
// there is NO setter by design (CR7: the declaration is written beside the
// pin and never re-derived), so tests that care about attendedness create
// the row. nil is the honest never-declared state, which the runtime
// collapses to nobody-can-answer exactly like false (fail-closed).
func restedLoopWith(t *testing.T, f *streamFixture, converse string, attended *bool) store.Loop {
	t.Helper()
	yaml := "id: talk\nschemaVersion: 1\nversion: 1\nafter: converse\n"
	if converse != "" {
		yaml += "converse: " + converse + "\n"
	}
	yaml += "output: []\nlanes:\n  - id: converse\n    step: {kind: model-step, agent: chat}\nquestions: {}\n"
	bp, err := blueprint.Load([]byte(yaml))
	if err != nil {
		t.Fatalf("load the talk blueprint: %v", err)
	}
	st := loop.LoopState{RuntimeState: loop.InitialState(bp)}
	state, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "chat", ComponentRoot: f.root, Root: f.root, Goal: "talk", Status: "running", Attended: attended}, bp.ID, bp.Version, yaml, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.EndRun(l.ID, 1, "done", state); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetLoopStatus(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, "idle"); err != nil {
		t.Fatal(err)
	}
	return l
}

// waitTurnIdle blocks until the daemon's turn guard is clear for the loop:
// releaseTurn is the turn goroutine's LAST act, so "not in the map" means
// every store write and publish it makes has already happened. Tests that
// let a turn run MUST drain it before returning — the same lesson S39 T4
// learned with StartLoops: a daemon goroutine that outlives t.Cleanup
// queries a closed store and panics a LATER test, not this one.
func waitTurnIdle(t *testing.T, d *Daemon, id string) {
	t.Helper()
	waitFor(t, "the turn goroutine to finish", func() bool {
		d.turnMu.Lock()
		defer d.turnMu.Unlock()
		_, busy := d.turns[id]
		return !busy
	})
}

func seedStatusStep(t *testing.T, f *streamFixture, status string) int64 {
	t.Helper()
	s, err := f.s.CreateStep(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, store.NewStep{Agent: "x", Status: store.StepStatus(status)})
	if err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func TestMessageToAnIdleLoopStartsATurn(t *testing.T) {
	f, _ := gatedFixture(t, []gatedTurn{{deltas: []string{turnOK}}})
	ctx := context.Background()
	l := restedLoop(t, f, "")
	initConn(t, f.d, ctx, "c-turn", "ui")
	sub := subscribe(t, f.d, "c-turn", protocol.SubscribeParams{})

	res := dispatchJSON(t, f, "c-turn", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "hello there"})
	if res["turn"] != "started" {
		t.Fatalf("loop.message answered %v, want turn=started", res)
	}
	if _, ok := res["messageId"]; !ok {
		t.Fatalf("the answer must carry the thread row it wrote: %v", res)
	}

	// The agent's reply reaches the thread through the handoff summary —
	// the same structured promise a blueprint step's ok carries.
	waitTurnIdle(t, f.d, l.ID) // the goroutine's LAST act is releasing the guard — drain it first, then count
	waitFor(t, "the agent's note in the thread", func() bool {
		rows, err := f.s.Thread(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, 0)
		if err != nil || len(rows) != 2 {
			return false
		}
		return rows[0].Author == "human" && rows[0].Source == "user" && rows[0].Body == "hello there" &&
			rows[1].Author == "agent" && rows[1].Body == "hello"
	})
	// The stream saw it all: two postings and the dispatch, with the turn
	// named as the step.
	var posted, dispatched int
	var dispatchedPayload map[string]any
	for _, ev := range streamEvents(f.w, "c-turn", sub.SubscriptionID) {
		switch ev.Kind {
		case protocol.KindLoopMessagePosted:
			posted++
		case protocol.KindLoopStepDispatched:
			var p map[string]any
			if err := json.Unmarshal(ev.Payload, &p); err == nil && p["step"] == "turn" {
				dispatched++
				dispatchedPayload = p
			}
		}
	}
	if posted != 2 {
		t.Fatalf("loop.message.posted count = %d, want 2 (the human's message and the agent's reply)", posted)
	}
	if dispatched != 1 {
		t.Fatalf("the turn never named itself on the stream (loop.step.dispatched with step=turn): %d", dispatched)
	}
	if dispatchedPayload["lane"] != "" || dispatchedPayload["agent"] != DefaultConverseAgent {
		t.Fatalf("the dispatch says a turn stands outside the ladder and speaks as the conversation agent: %v", dispatchedPayload)
	}
	// CR9: a resting loop STAYS idle while its turn talks.
	pin, err := f.s.ReadPinnedLoop(l.ID)
	if err != nil || pin.Loop.Status != "idle" {
		t.Fatalf("the loop left rest for a turn: %q (err %v)", pin.Loop.Status, err)
	}
}

func TestTurnModelInputContainsTheGoalAndTheMessage(t *testing.T) {
	f, m := gatedFixture(t, []gatedTurn{{deltas: []string{turnOK}}})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	l := restedLoop(t, f, "")
	res := dispatchJSON(t, f, "c-init", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "why is the build red?"})
	if res["turn"] != "started" {
		t.Fatalf("turn refused: %v", res)
	}
	waitFor(t, "the model call", func() bool { return m.callCount() >= 1 })
	waitTurnIdle(t, f.d, l.ID)
	msgs := m.lastSent(0)
	var text string
	for _, msg := range msgs {
		for _, b := range msg.Content {
			text += b.Text
		}
	}
	// S68 (B2/B3): the goal is INSTRUCTION — it rides the system half now,
	// spoken as the frame, not as a `Goal:` line inside the person's words.
	if !strings.Contains(text, "The loop's goal: talk") {
		t.Fatalf("the frame lost the goal: %q", text)
	}
	// The LAST message is the user message, and it is the typed words ALONE
	// — verbatim, undecorated, carrying no goal line and no thread blob.
	if len(msgs) == 0 || string(msgs[len(msgs)-1].Role) != "user" {
		t.Fatalf("the wire does not end on the person's message: %v", msgs)
	}
	var words string
	for _, b := range msgs[len(msgs)-1].Content {
		words += b.Text
	}
	if words != "why is the build red?" {
		t.Fatalf("the user message is not the operator's words alone (B3): %q", words)
	}
	// B4: the flattened `[author] body` block is gone — real roles replaced
	// it, so no decoration is on the wire at all.
	if strings.Contains(text, "[human]") || strings.Contains(text, "[agent]") {
		t.Fatalf("the thread blob format is back on the wire: %q", text)
	}
	if strings.Contains(text, "user") && strings.Contains(text, "template") {
		t.Fatal("provenance words both present — the prompt shows sources? (CR6)")
	}
}

func TestMessageWhileAStepRunsIsRefusedWithTheSteerRemedy(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	l := restedLoop(t, f, "")
	stepID := seedStatusStep(t, f, "running")
	initConn(t, f.d, context.Background(), "c-ref", "cli")
	res := dispatchJSON(t, f, "c-ref", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "talk over me"})
	refused, ok := res["refused"].(map[string]any)
	if !ok || refused["kind"] != "step-running" {
		t.Fatalf("a running step answered %v, want refused.kind=step-running", res)
	}
	if refused["stepId"] != float64(stepID) {
		t.Fatalf("the refusal must name the step: %v", refused)
	}
	detail, _ := refused["detail"].(string)
	if !strings.Contains(detail, "step.steer") {
		t.Fatalf("the refusal carries the remedy: %q", detail)
	}
	// The order IS the contract: the refusal added NOTHING to the thread.
	rows, err := f.s.Thread(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a refused message wrote %d thread rows — the step check must run BEFORE the note", len(rows))
	}
}

func TestMessageToASuspendedStepIsRefusedNamingAnswer(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-ref2", "cli")
	l := restedLoop(t, f, "")
	seedStatusStep(t, f, "suspended")
	res := dispatchJSON(t, f, "c-ref2", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "hey"})
	refused, ok := res["refused"].(map[string]any)
	if !ok || refused["kind"] != "step-waiting" {
		t.Fatalf("a suspended step answered %v, want refused.kind=step-waiting", res)
	}
	detail, _ := refused["detail"].(string)
	if !strings.Contains(detail, "answer") {
		t.Fatalf("the waiting refusal names its door (answer / step.continue): %q", detail)
	}
}

func TestMessageToARunningLoopIsRefused(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-ref3", "cli")
	ctx := context.Background()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "walk", ComponentRoot: f.root, Root: f.root, Goal: "g"}, "bp", 1,
		"id: bp\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: chat}\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	res := dispatchJSON(t, f, "c-ref3", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "hey"})
	refused, ok := res["refused"].(map[string]any)
	if !ok || refused["kind"] != "not-idle" {
		t.Fatalf("a running loop answered %v, want refused.kind=not-idle", res)
	}
	// And the missing loop is the door's ErrBadParams, not a silent refusal.
	if _, err := f.d.Table.Dispatch(ctx, "c-ref3", protocol.MethodLoopMessage, mustJSON(t, map[string]any{"id": "nope", "text": "x"})); err == nil {
		t.Fatal("a missing loop should be a bad-params error naming the id")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOnlyOneTurnRunsAtATime(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	if !f.d.claimTurn("l1") {
		t.Fatal("the first claim must win")
	}
	if f.d.claimTurn("l1") {
		t.Fatal("a second in-flight turn on one loop must lose — CR9's one-voice promise")
	}
	if !f.d.claimTurn("l2") {
		t.Fatal("a DIFFERENT loop is not busy")
	}
	f.d.releaseTurn("l1")
	if !f.d.claimTurn("l1") {
		t.Fatal("after release the loop can speak again")
	}
}

func TestATurnIsSteerableWhileItRuns(t *testing.T) {
	enter, release := make(chan struct{}), make(chan struct{})
	f, _ := gatedFixture(t, []gatedTurn{{enter: enter, release: release, deltas: []string{turnOK}}})
	ctx := context.Background()
	l := restedLoop(t, f, "")
	initConn(t, f.d, ctx, "c-steer", "ui")
	sub := subscribe(t, f.d, "c-steer", protocol.SubscribeParams{})
	if res := dispatchJSON(t, f, "c-steer", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "long one"}); res["turn"] != "started" {
		t.Fatalf("turn refused: %v", res)
	}
	// The turn's step id rides its own dispatch event (S31 D2's seam) —
	// D2/D3: the person speaks to it with the ORDINARY steer door.
	var stepID float64
	waitFor(t, "the turn's dispatch naming its step id", func() bool {
		for _, ev := range streamEvents(f.w, "c-steer", sub.SubscriptionID) {
			if ev.Kind != protocol.KindLoopStepDispatched {
				continue
			}
			var p map[string]any
			if err := json.Unmarshal(ev.Payload, &p); err == nil && p["step"] == "turn" {
				if id, ok := p["stepId"].(float64); ok {
					stepID = id
					return true
				}
			}
		}
		return false
	})
	res := dispatchJSON(t, f, "c-steer", protocol.MethodStepSteer, map[string]any{"stepId": stepID, "text": "make it shorter"})
	// The ordinary steer door's own vocabulary is the verdict — a steer on
	// a live step lands pending and flips delivered when the step's loop
	// reads it; either way the TURN took it like any step (D2/D3).
	if st, _ := res["state"].(string); st != "pending" && st != "delivered" {
		t.Fatalf("a live turn must take a steer like any step, answered %v", res)
	}
	if res["steerId"] == nil {
		t.Fatalf("no steer row: %v", res)
	}
	close(release)             // let the scripted turn finish...
	waitTurnIdle(t, f.d, l.ID) // ...fully — the cleanup must not race its store writes
}

func TestTurnParamsKeepTheDefaultIterationBound(t *testing.T) {
	st := loop.LoopState{Slug: "chat", Root: "/r", RuntimeState: loop.RuntimeState{Route: "r1"}}
	p := turnParams(st, "chat", "prompt", "", nil, "", nil, nil, "")
	if p.MaxIterations != 0 {
		t.Fatalf("MaxIterations=%d — R3: a turn wears the SAME bound as a blueprint step, never its own cap", p.MaxIterations)
	}
	if p.Agent != "chat" {
		t.Fatalf("the agent argument must arrive verbatim: %q", p.Agent)
	}
	if p.Route != "r1" {
		t.Fatalf("with no route asked for, the loop's own rides: %q", p.Route)
	}
	if p2 := turnParams(st, "chat", "prompt", "", nil, "other", nil, nil, ""); p2.Route != "other" {
		t.Fatalf("an asked-for route wins: %q", p2.Route)
	}
}

func TestATurnLeftRunningByACrashIsRecoveredAndTheLoopStaysIdle(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	ctx := context.Background()
	l := restedLoop(t, f, "")
	stepID := seedStatusStep(t, f, "running")
	got, err := f.d.Recover(ctx, false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("the sweep saw %d running steps, want the abandoned turn (1)", got.Total)
	}
	resolved, err := f.s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status == "running" {
		t.Fatal("recovery left the abandoned turn running — Q7: the sweep reads RunningStepIDs, loop status be damned")
	}
	pin, err := f.s.ReadPinnedLoop(l.ID)
	if err != nil || pin.Loop.Status != "idle" {
		t.Fatalf("recovery moved a resting loop: %q (err %v) — a turn is not a walk position (CR9/Q7)", pin.Loop.Status, err)
	}
}

// TestMessageLoopDoesNotTouchLoopStatus is the DoD grep made executable at
// the one place the temptation lives: nothing in the message path may write
// loop status — read it, yes; write it, never.
func TestMessageLoopDoesNotTouchLoopStatus(t *testing.T) {
	f, _ := gatedFixture(t, []gatedTurn{{deltas: []string{turnOK}}})
	l := restedLoop(t, f, "")
	initConn(t, f.d, context.Background(), "c-idle", "cli")
	res := dispatchJSON(t, f, "c-idle", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "x"})
	if res["turn"] != "started" {
		t.Fatalf("refused: %v", res)
	}
	waitTurnIdle(t, f.d, l.ID)
	pin, err := f.s.ReadPinnedLoop(l.ID)
	if err != nil || pin.Loop.Status != "idle" {
		t.Fatalf("the message path moved loop.status to %q (err %v)", pin.Loop.Status, err)
	}
}
