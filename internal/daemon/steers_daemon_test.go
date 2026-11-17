package daemon

// # The steer, over the wire (S31 T4 — D3/D5 through the real door)
//
// The runtime tests prove the loop injects at the boundary; the protocol
// tests prove the door refuses by name. This file proves the whole daemon
// path: a person (a socket connection) queues an instruction while a loop
// step is mid-flight, and the WIRE tells the honest story — the queued
// announcement, the calls that were not run, the delivery that names them,
// and a loop that carries on. The store is the arbiter: the transcript that
// committed is the transcript the model will read.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// gateModel plays scripted turns but holds the FIRST call inside Stream
// until the test releases it — which is how a steer is made to arrive while
// a model call is genuinely in flight (LW1's situation), not near it.
type gateModel struct {
	turns   []streamTurn
	gate    chan struct{}
	entered chan struct{}

	mu    sync.Mutex
	calls int
}

func (g *gateModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	g.mu.Lock()
	index := g.calls
	g.calls++
	g.mu.Unlock()

	if index == 0 {
		close(g.entered)
		select {
		case <-g.gate:
		case <-ctx.Done():
		}
	}
	var turn streamTurn
	if index < len(g.turns) {
		turn = g.turns[index]
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

// steerProbe decodes the steer events' wire fields (the WIRE is the contract;
// the payload Go type is allowed to grow).
type steerProbe struct {
	StepID        int64    `json:"stepId"`
	SteerID       int64    `json:"steerId"`
	HasText       bool     `json:"hasText"`
	Route         string   `json:"route"`
	Tool          string   `json:"tool"`
	ToolUseID     string   `json:"toolUseId"`
	Reason        string   `json:"reason"`
	Iteration     int      `json:"iteration"`
	SkippedTools  []string `json:"skippedTools"`
	FromIteration int      `json:"fromIteration"`
	ReasoningEff  string   `json:"reasoningEffort"`
}

func steerFixtureWith(t *testing.T, turns []streamTurn) (*streamFixture, *gateModel, string) {
	t.Helper()
	s := recoveryStore(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello steering\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedRules(t, s, time.Now().UTC(),
		toolRulePolicy("allow-read-for-read-only", "read-only", "read", policy.Allow))
	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	g := &gateModel{turns: turns, gate: make(chan struct{}), entered: make(chan struct{})}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: g, Logger: w, Notify: w.notify,
	})
	t.Cleanup(d.Shutdown)
	return &streamFixture{d: d, w: w, s: s, root: root, route: route}, g, root
}

// waitStepIDOnWire blocks until a step.started event carrying a stepId
// reaches the subscription — the moment the daemon has committed a running
// step row and a steer has something to be queued for.
func waitStepIDOnWire(t *testing.T, f *streamFixture, subID string) int64 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, ev := range streamEvents(f.w, "c1", subID) {
			if ev.Kind != "step.started" {
				continue
			}
			var p steerProbe
			if err := json.Unmarshal(ev.Payload, &p); err == nil && p.StepID > 0 {
				return p.StepID
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no step.started with a stepId reached the wire")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForKind(t *testing.T, f *streamFixture, subID, kind string) []protocol.Event {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var out []protocol.Event
		for _, ev := range streamEvents(f.w, "c1", subID) {
			if ev.Kind == kind {
				out = append(out, ev)
			}
		}
		if len(out) > 0 {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q event reached the wire", kind)
		}
		time.Sleep(time.Millisecond)
	}
}

func countKind(events []protocol.Event, kind string) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestASteerQueuedMidCallSkipsTheTurnsToolsAndTheLoopCarriesOn is the slice's
// headline, end to end: a tool-bearing turn overtaken by a person's
// instruction. Nothing in the tool ran (no `step.tool.end` ever appears),
// every call was answered as not-run, the instruction reached the model, and
// the loop advanced on a report that came from the steered turn.
func TestASteerQueuedMidCallSkipsTheTurnsToolsAndTheLoopCarriesOn(t *testing.T) {
	f, g, root := steerFixtureWith(t, []streamTurn{
		{toolUses: []model.Block{{Kind: model.BlockToolUse, ID: "t1", Name: "read",
			Input: json.RawMessage(`{"path":"notes.txt"}`)}}},
		{deltas: []string{`{"status":"ok","summary":"summarized without reading"}`}},
	})
	ctx := context.Background()
	initConn(t, f.d, ctx, "c1", "steerer")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: root}})
	blueprint := writeStreamBlueprint(t, "read-only", "work")
	loopID := startStreamLoop(t, f, blueprint)

	stepID := waitStepIDOnWire(t, f, sub.SubscriptionID)
	// step.started precedes request assembly. Queue only once Stream is in
	// flight, otherwise the steer can correctly reach the first request.
	select {
	case <-g.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("model call never entered Stream")
	}

	// The steer arrives WHILE the first model call is in the air (the gate is
	// still shut — LW1's exact situation).
	res := dispatchJSON(t, f, "c1", protocol.MethodStepSteer, protocol.StepSteerParams{
		StepID: stepID, Text: "do not read anything — summarize from what you have",
	})
	if res["state"] != "pending" {
		t.Fatalf("steer response = %v, want state pending — brief §5", res)
	}
	steerID, _ := res["steerId"].(float64)
	if steerID == 0 {
		t.Fatalf("no steerId in %v", res)
	}

	queued := waitForKind(t, f, sub.SubscriptionID, "step.steer.queued")
	var qp steerProbe
	if err := json.Unmarshal(queued[0].Payload, &qp); err != nil {
		t.Fatal(err)
	}
	if qp.StepID != stepID || qp.SteerID != int64(steerID) || !qp.HasText {
		t.Fatalf("the announcement is not about this row: %+v", qp)
	}

	// NOW the in-flight call may finish — with no steer in it, because it was
	// assembled before the row existed (LW1).
	close(g.gate)

	pin := waitLoopSettled(t, f, loopID)
	if pin.Loop.Status != "done" {
		t.Fatalf("loop settled as %s, want done", pin.Loop.Status)
	}

	events := streamEvents(f.w, "c1", sub.SubscriptionID)
	skips := []steerProbe{}
	for _, ev := range events {
		if ev.Kind == "step.tool.skipped" {
			var p steerProbe
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatal(err)
			}
			skips = append(skips, p)
		}
	}
	if len(skips) != 1 {
		t.Fatalf("%d skip events, want one for t1", len(skips))
	}
	if skips[0].Tool != "read" || skips[0].ToolUseID != "t1" || skips[0].Reason != "steered" {
		t.Fatalf("the skip announcement is imprecise: %+v", skips[0])
	}
	// The tool never ran: a real execution publishes tool.start/tool.end
	// around it, and neither exists on the wire.
	if n := countKind(events, "step.tool.end"); n != 0 {
		t.Fatalf("%d step.tool.end events — a steered turn dispatches NOTHING (LW2)", n)
	}
	delivered := []steerProbe{}
	for _, ev := range events {
		if ev.Kind == "step.steer.delivered" {
			var p steerProbe
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatal(err)
			}
			delivered = append(delivered, p)
		}
	}
	if len(delivered) != 1 || delivered[0].SteerID != int64(steerID) {
		t.Fatalf("%d delivery events, want exactly one naming the steer", len(delivered))
	}
	if len(delivered[0].SkippedTools) != 1 || delivered[0].SkippedTools[0] != "t1" {
		t.Fatalf("the delivery does not name the calls it closed: %+v", delivered[0])
	}

	// The store is the arbiter: the committed transcript carries the
	// instruction exactly once, inside a user message that ANSWERS the
	// skipped call (the not-run tool_result) before it.
	tr := f.s.ReadTranscript(stepID)
	if !tr.OK {
		t.Fatalf("transcript unreadable: %+v", tr)
	}
	hits, answerFirst := 0, false
	for _, m := range tr.Messages {
		if m.Role != model.RoleUser {
			continue
		}
		sawResult, sawText := -1, -1
		for i, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "t1" {
				sawResult = i
			}
			if b.Kind == model.BlockText && strings.Contains(b.Text, "do not read anything") {
				sawText = i
			}
		}
		if sawText >= 0 {
			hits++
			if sawResult == -1 || sawResult < sawText {
				answerFirst = true
			}
		}
	}
	if hits != 1 {
		t.Fatalf("the instruction reached the transcript %d times, want exactly once", hits)
	}
	if !answerFirst {
		t.Fatal("the model reads the instruction without its not-run answer leading (LW2 order)")
	}

}

// TestASteerWithNoToolsInFlightIsStillAnnouncedAndDeliveredOnce covers the
// quieter path through the same door: no tool calls at the boundary, so the
// instruction simply joins the next turn — and `step.steers` reads the row
// as delivered, once.
func TestASteerWithNoToolsInFlightIsStillAnnouncedAndDeliveredOnce(t *testing.T) {
	f, g, root := steerFixtureWith(t, []streamTurn{
		{deltas: []string{`{"status":"ok","summary":"first pass"}`}},
		{deltas: []string{`{"status":"ok","summary":"steered pass"}`}},
	})
	ctx := context.Background()
	initConn(t, f.d, ctx, "c1", "steerer")
	sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: root}})
	loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "work"))

	stepID := waitStepIDOnWire(t, f, sub.SubscriptionID)
	// step.started precedes request assembly. Queue only once Stream is in
	// flight, otherwise the steer can correctly reach the first request.
	select {
	case <-g.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("model call never entered Stream")
	}
	res := dispatchJSON(t, f, "c1", protocol.MethodStepSteer,
		protocol.StepSteerParams{StepID: stepID, Text: "make it a table"})
	steerID := int64(res["steerId"].(float64))
	close(g.gate)

	if pin := waitLoopSettled(t, f, loopID); pin.Loop.Status != "done" {
		t.Fatalf("loop settled as %s, want done", pin.Loop.Status)
	}
	events := streamEvents(f.w, "c1", sub.SubscriptionID)
	if n := countKind(events, "step.steer.delivered"); n != 1 {
		t.Fatalf("%d delivery events, want 1", n)
	}
	if n := countKind(events, "step.tool.skipped"); n != 0 {
		t.Fatalf("%d skip events on a tool-free turn", n)
	}
	// The text reached the transcript exactly once.
	once := 0
	for _, m := range f.s.ReadTranscript(stepID).Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockText && strings.Contains(b.Text, "make it a table") {
				once++
			}
		}
	}
	if once != 1 {
		t.Fatalf("the instruction is in the transcript %d times, want once", once)
	}

	// step.steers over the wire: a bare array, the delivered row, timestamped.
	raw, err := json.Marshal(mustDispatchAny(t, f, protocol.MethodStepSteers, protocol.StepSteersParams{StepID: stepID}))
	if err != nil {
		t.Fatal(err)
	}
	var views []map[string]any
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatalf("step.steers must answer a bare array, got %s", raw)
	}
	if len(views) != 1 || views[0]["state"] != "delivered" || views[0]["deliveredAt"] == "" {
		t.Fatalf("the row does not read delivered-once: %s", raw)
	}
	if int64(views[0]["steerId"].(float64)) != steerID {
		t.Fatalf("wrong row: %s", raw)
	}
	// Cancel now must be refused with the state named (P31 through the door).
	_, err = f.d.Table.Dispatch(ctx, "c1", protocol.MethodStepSteerCancel,
		mustJSONBytes(t, protocol.StepSteerCancelParams{SteerID: steerID}))
	var e *protocol.Error
	if err == nil || !errors.As(err, &e) || e.Code != protocol.ErrSteerAlreadySettled.Code {
		t.Fatalf("cancelling a delivered steer = %v, want ErrSteerAlreadySettled", err)
	}
	if !strings.Contains(e.Data.What, "delivered") {
		t.Errorf("the refusal does not name the state found: %s", e.Data.What)
	}
}

func mustDispatchAny(t *testing.T, f *streamFixture, method protocol.MethodName, params any) any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.d.Table.Dispatch(context.Background(), "c1", method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return res
}

func mustJSONBytes(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
