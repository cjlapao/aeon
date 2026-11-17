package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// The fake client, the tool spy, and a handler — shared by every test in this
// package.
//
// # Why the client is SCRIPTED rather than a stub
//
// D18: no unit test contacts a provider. A scripted client is what makes the
// whole loop testable offline — each turn is declared, so a test can say "the
// model calls a tool, then reports" and assert what the loop did with it.
//
// It also RECORDS EVERY REQUEST, which is what D4 needs: the tool-free retry
// is asserted on the request object, and a client that only counted calls
// could not show it.

// scriptedTurn is one model turn the fake client will produce.
type scriptedTurn struct {
	// Text is emitted as text deltas, split so a test can observe streaming.
	Text string

	// Thinking is emitted as thinking deltas.
	Thinking string

	// ToolUses are emitted as tool-use deltas, in order.
	ToolUses []model.Block

	// Usage and Cost are this turn's accounting.
	Usage model.Usage
	Cost  model.Cost

	// Err fails the call after any deltas have been sent, which is how a
	// provider failure mid-stream is exercised.
	Err error

	// ThinkingDropped is this turn's request-direction thinking-drop count
	// (S18 T5) — what the model client collected from the adapter, which the
	// loop accumulates into the step's record.
	ThinkingDropped int

	// OwedToolErrors is S3's report of tool calls whose arguments would not
	// parse (Q6).
	OwedToolErrors []string

	// StopReason is how the provider ended the turn ("length" when the output cap did).
	StopReason string
}

// fakeClient plays a script and records what it was asked.
type fakeClient struct {
	mu       sync.Mutex
	turns    []scriptedTurn
	requests []model.ModelRequest
	calls    int
}

func newFakeClient(turns ...scriptedTurn) *fakeClient {
	return &fakeClient{turns: turns}
}

func (f *fakeClient) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	index := f.calls
	f.calls++
	var turn scriptedTurn
	if index < len(f.turns) {
		turn = f.turns[index]
	} else {
		// Past the script. A test that drives more turns than it declared gets
		// a valid report rather than a panic, so the failure is the assertion
		// rather than the harness.
		turn = scriptedTurn{Text: `{"status":"ok","summary":"ran out of script"}`}
	}
	f.mu.Unlock()

	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)

	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)

		if turn.Thinking != "" {
			deltas <- model.Delta{Kind: model.DeltaThinking, Text: turn.Thinking}
		}
		// Split into two, so a test can tell a streaming implementation from a
		// buffering one.
		if turn.Text != "" {
			half := len(turn.Text) / 2
			if half > 0 {
				deltas <- model.Delta{Kind: model.DeltaText, Text: turn.Text[:half]}
			}
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.Text[half:]}
		}
		for i := range turn.ToolUses {
			block := turn.ToolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}
		}

		results <- model.StreamResult{
			Usage:           turn.Usage,
			Cost:            turn.Cost,
			OwedToolErrors:  turn.OwedToolErrors,
			ThinkingDropped: turn.ThinkingDropped,
			Attempts:        1,
			StopReason:      turn.StopReason,
		}
		if turn.Err != nil {
			errs <- turn.Err
		}
	}()

	return deltas, results, errs
}

func (f *fakeClient) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeClient) request(i int) model.ModelRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[i]
}

// --- the tool spy ------------------------------------------------------------

// toolCall records ONE call's entry and exit times.
//
// # Why both, and not just the order
//
// D3's whole point. A spy recording call ORDER passes against an
// `errgroup.Wait()` implementation — every tool is still called, and still in
// order. Only entry and exit can show that two calls OVERLAPPED, which is what
// "serial" means.
type toolCall struct {
	Name  string
	Input string
	Enter time.Time
	Exit  time.Time
}

// spyTools is a ToolCaller that records timings and plays canned results.
type spyTools struct {
	mu      sync.Mutex
	calls   []toolCall
	results map[string]ToolOutcome

	// delay is how long each call takes. Non-zero is what makes an overlap
	// detectable: two instantaneous calls cannot be shown not to overlap.
	delay time.Duration

	// granted is what Granted() reports.
	granted []string

	// panicOn makes the named tool panic, for the boundary test.
	panicOn string
}

func newSpyTools(granted ...string) *spyTools {
	return &spyTools{
		results: map[string]ToolOutcome{},
		granted: granted,
		delay:   5 * time.Millisecond,
	}
}

func (s *spyTools) with(name string, out ToolOutcome) *spyTools {
	s.results[name] = out
	return s
}

func (s *spyTools) Call(ctx context.Context, name string, input json.RawMessage) ToolOutcome {
	enter := time.Now()
	if name == s.panicOn {
		panic("the spy was told to panic for " + name)
	}

	time.Sleep(s.delay)

	s.mu.Lock()
	s.calls = append(s.calls, toolCall{
		Name: name, Input: string(input), Enter: enter, Exit: time.Now(),
	})
	s.mu.Unlock()

	if out, ok := s.results[name]; ok {
		return out
	}
	return ToolOutcome{Content: fmt.Sprintf("%s ran", name), Summary: name + " ran"}
}

func (s *spyTools) Granted() []string { return s.granted }

func (s *spyTools) recorded() []toolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]toolCall(nil), s.calls...)
}

// overlaps reports the first pair of recorded calls whose intervals overlap.
//
// THE assertion for D3. Returns the two calls so a failure names them rather
// than saying "concurrency detected".
func (s *spyTools) overlaps() (a, b toolCall, found bool) {
	calls := s.recorded()
	for i := 0; i < len(calls); i++ {
		for j := i + 1; j < len(calls); j++ {
			if calls[i].Exit.After(calls[j].Enter) && calls[j].Exit.After(calls[i].Enter) {
				return calls[i], calls[j], true
			}
		}
	}
	return toolCall{}, toolCall{}, false
}

// --- fixtures ----------------------------------------------------------------

// testHandler is a handler with everything a compile needs.
func testHandler(tools ...string) StepHandler {
	return StepHandler{
		Name:            "test-handler",
		System:          "you are a test",
		Sections:        []SectionSpec{{Title: "Task", Text: "do the thing", Category: CategorySystemPrompt, Destination: SectionTranscript}},
		Tools:           tools,
		HandoffSchema:   `{"type":"object","required":["status"]}`,
		MaxOutputTokens: 1024,
	}
}

func testRoute() model.Route {
	return model.Route{ID: "test", Provider: "anthropic", Model: "claude-test", MaxOutputTokens: 1024}
}

// testState builds a LoopState ready to run.
func testState(handler StepHandler, tools []model.ToolSpec) *LoopState {
	manifest := ContextManifest{
		MaxIterations: MaxIterations,
		DeadlineAt:    time.Now().Add(time.Hour),
	}
	req := StepRequest{
		Handler:       handler,
		Messages:      OpeningTranscript(handler, "", "do the thing", nil),
		Manifest:      manifest,
		Route:         testRoute(),
		Tools:         tools,
		WorkspaceRoot: "/workspace",
	}
	return &LoopState{Request: req, Messages: req.Messages, Manifest: &req.Manifest}
}

// toolSpec is a minimal model-facing declaration.
func toolSpec(name string) model.ToolSpec {
	return model.ToolSpec{Name: name, Description: name, Parameters: json.RawMessage(`{"type":"object"}`)}
}

// toolUse builds a tool_use block.
func toolUse(id, name, input string) model.Block {
	return model.ToolUseBlock(id, name, json.RawMessage(input))
}

// okHandoff is a valid report.
const okHandoff = "```json\n{\"status\":\"ok\",\"summary\":\"did the thing\"}\n```"

// recordingSink captures events, and can be made to panic.
type recordingSink struct {
	mu       sync.Mutex
	events   []StepEvent
	panicOn  string
	observed []int
}

func (r *recordingSink) Emit(e StepEvent) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.observed = append(r.observed, e.Iteration)
	r.mu.Unlock()

	if e.Kind == r.panicOn {
		panic("the sink was told to panic on " + e.Kind)
	}
}

func (r *recordingSink) all() []StepEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]StepEvent(nil), r.events...)
}

// flakyCheckpoint is a CheckpointWriter that succeeds for the first `ok`
// Save calls and fails every call after — the mid-step persistence failure
// every checkpoint call must SPEAK, never eat. It keeps the snapshots of the
// saves that succeeded, so a test can read the last persisted state.
type flakyCheckpoint struct {
	ok        int
	saves     int
	snapshots []Checkpoint
}

func (f *flakyCheckpoint) Save(_ context.Context, cp Checkpoint) error {
	f.saves++
	if f.saves <= f.ok {
		f.snapshots = append(f.snapshots, cp)
		return nil
	}
	return errors.New("the checkpoint row could not be written")
}

// toolResultsFor returns EVERY tool_result answering toolUseID. The ask
// suspension's sibling closure is pinned by "exactly one" — toolResultFor's
// first-hit shape cannot see a double answer, and a re-answered closure or a
// re-run after resume would both be doubles.
func toolResultsFor(msgs []model.Message, toolUseID string) []model.Block {
	var out []model.Block
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == toolUseID {
				out = append(out, b)
			}
		}
	}
	return out
}

// packageSources reads this package's non-test sources, for the grep-shaped
// invariants (I3, I4).
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(b)
	}
	if len(out) < 5 {
		t.Fatalf("expected several sources, found %d — a grep over nothing passes forever", len(out))
	}
	return out
}
