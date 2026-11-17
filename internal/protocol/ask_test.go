package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// fakeStreamer is the model layer, scripted. No provider, no transport, no
// credential — the whole method is exercised from here.
type fakeStreamer struct {
	mu sync.Mutex

	texts []string
	usage model.Usage
	cost  model.Cost
	stop  string
	err   error
	owed  []string
	block *model.Block
	// delay is applied before each delta, so a cancellation test has a window.
	delay time.Duration
	// started is closed when Stream is first called.
	started chan struct{}
}

func (f *fakeStreamer) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)

	if f.started != nil {
		close(f.started)
		f.started = nil
	}

	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)

		cancelled := false
		for _, text := range f.texts {
			if f.delay > 0 {
				select {
				case <-time.After(f.delay):
				case <-ctx.Done():
					cancelled = true
				}
			}
			if cancelled || ctx.Err() != nil {
				cancelled = true
				break
			}
			select {
			case deltas <- model.Delta{Kind: model.DeltaText, Text: text}:
			case <-ctx.Done():
				cancelled = true
			}
			if cancelled {
				break
			}
		}
		if f.block != nil && !cancelled {
			select {
			case deltas <- model.Delta{Kind: model.DeltaToolUse, Block: f.block}:
			case <-ctx.Done():
			}
		}

		results <- model.StreamResult{
			Usage: f.usage, Cost: f.cost, StopReason: f.stop, OwedToolErrors: f.owed, Attempts: 1,
		}
		if f.err != nil {
			errs <- f.err
		} else if cancelled {
			errs <- context.Canceled
		}
	}()

	return deltas, results, errs
}

// askTable builds a table with the model layer wired and events recorded.
func askTable(t *testing.T, streamer Streamer, routes ...model.Route) (*Table, *eventLog) {
	t.Helper()
	tbl := newTestTable(t)
	tbl.Routes = NewStaticRoutes(routes...)
	tbl.Models = streamer

	log := &eventLog{}
	tbl.OnEvent = log.record
	return tbl, log
}

type recordedEvent struct {
	Kind    string
	Payload json.RawMessage
}

// eventLog is the recorder behind Table.OnEvent. The streaming goroutine
// askStreaming spawns (ask.go) publishes into it while the test goroutine
// polls it — the writer ALWAYS took the mutex; the readers used to take it
// nowhere, which is the race -race caught in the T12 harness. Every read of
// the recorder now goes through a locked snapshot, so writer and reader are
// synchronised by the SAME mutex rather than by one half of a handshake.
type eventLog struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (l *eventLog) record(kind string, payload any) {
	raw, _ := json.Marshal(payload)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, recordedEvent{Kind: kind, Payload: raw})
}

// all returns a snapshot copy: the caller iterates freely while the stream
// keeps publishing.
func (l *eventLog) all() []recordedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]recordedEvent(nil), l.events...)
}

func (l *eventLog) matching(kind string) []recordedEvent {
	var matching []recordedEvent
	for _, e := range l.all() {
		if e.Kind == kind {
			matching = append(matching, e)
		}
	}
	return matching
}

func pricedRoute(id, provider string) model.Route {
	return model.Route{
		ID: id, Provider: provider, Model: "m", MaxOutputTokens: 1024,
		Prices: &model.Prices{InputUSDPerMTok: model.Float64Ptr(3), OutputUSDPerMTok: model.Float64Ptr(15)},
	}
}

func waitForEvents(t *testing.T, events *eventLog, kind string, n int) []recordedEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		matching := events.matching(kind)
		if len(matching) >= n {
			return matching
		}
		time.Sleep(5 * time.Millisecond)
	}
	var got []string
	for _, e := range events.all() {
		got = append(got, e.Kind)
	}
	t.Fatalf("timed out waiting for %d %s events; saw %v", n, kind, got)
	return nil
}

func TestAskWithASubscriptionIDReturnsAskIDAndStreamsDeltaThenDone(t *testing.T) {
	streamer := &fakeStreamer{
		texts: []string{"Hello", " world"},
		usage: model.Usage{InputTokens: model.IntPtr(100), OutputTokens: model.IntPtr(50)},
		cost:  model.Cost{State: model.CostKnown, USD: 0.00105},
		stop:  "end_turn",
	}
	tbl, events := askTable(t, streamer, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	subRes, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"ask.*"}}}))
	if err != nil {
		t.Fatal(err)
	}
	subID := subRes.(SubscribeResult).SubscriptionID

	res, err := tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "hi", SubscriptionID: subID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	ask := res.(AskResult)

	if ask.AskID == "" {
		t.Fatal("no askId")
	}
	// In streaming mode the result carries NOTHING else — usage and cost
	// arrive on ask.done, and leaving them out is what stops a client reading
	// a half-finished answer as the whole one.
	if ask.Text != "" || ask.Usage != nil || ask.Cost != nil {
		t.Errorf("the streaming result carries buffered fields: %+v", ask)
	}

	deltas := waitForEvents(t, events, KindAskDelta, 2)
	var text string
	for _, e := range deltas {
		var p AskDeltaPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.AskID != ask.AskID {
			t.Errorf("a delta carries askId %q, want %q", p.AskID, ask.AskID)
		}
		text += p.Text
	}
	if text != "Hello world" {
		t.Errorf("streamed text = %q", text)
	}

	done := waitForEvents(t, events, KindAskDone, 1)
	if len(done) != 1 {
		t.Fatalf("got %d ask.done events, want exactly 1", len(done))
	}
	var payload AskDonePayload
	if err := json.Unmarshal(done[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Cost.State != model.CostKnown {
		t.Errorf("cost = %+v", payload.Cost)
	}
	if payload.StopReason != "end_turn" {
		t.Errorf("stopReason = %q", payload.StopReason)
	}
}

// Through the real registry, so the subscriber's filter and cursor apply (M23).
func TestASubscriberToAskStarReceivesTheDeltas(t *testing.T) {
	streamer := &fakeStreamer{texts: []string{"one", "two"}, stop: "end_turn"}
	tbl, _ := askTable(t, streamer, pricedRoute("r1", "anthropic"))

	// A real publisher: the daemon's fan-out, reduced to what a test asserts.
	var mu sync.Mutex
	received := map[string][]string{}
	tbl.OnEvent = func(kind string, payload any) {
		ev, err := tbl.Events.Append(kind, "", payload)
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, sub := range tbl.Subs.Matching(ev.Kind, ev.ComponentRoot) {
			received[sub.ID] = append(received[sub.ID], ev.Kind)
		}
	}

	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	askSub, _ := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"ask.*"}}}))
	otherSub, _ := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"client.*"}}}))

	askID := askSub.(SubscribeResult).SubscriptionID
	otherID := otherSub.(SubscribeResult).SubscriptionID

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "hi", SubscriptionID: askID,
	})); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received[askID])
		mu.Unlock()
		if n >= 3 { // two deltas and one done
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received[askID]) < 3 {
		t.Fatalf("the ask.* subscriber received %v", received[askID])
	}
	// And the client.* subscriber received NOTHING: the filter applies to model
	// output exactly as it does to everything else, which is the point of M23.
	if len(received[otherID]) != 0 {
		t.Fatalf("the client.* subscriber received ask events: %v — model output goes through "+
			"the ordinary subscription machinery, so an unrelated filter must not match it",
			received[otherID])
	}
}

func TestASubscriptionIDFromAnotherConnectionIsANamedError(t *testing.T) {
	tbl, _ := askTable(t, &fakeStreamer{}, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	initialized(t, tbl, "c2", PROTOCOL_VERSION)

	// c1 subscribes; c2 tries to use its id.
	subRes, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe, mustJSON(t, SubscribeParams{}))
	if err != nil {
		t.Fatal(err)
	}
	stolen := subRes.(SubscribeResult).SubscriptionID

	_, err = tbl.Dispatch(context.Background(), "c2", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "hi", SubscriptionID: stolen,
	}))
	if err == nil {
		t.Fatal("one connection streamed into another connection's subscription")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31012 {
		t.Fatalf("want -31012, got %v", err)
	}
	if !strings.Contains(e.Data.What, stolen) {
		t.Errorf("the error does not name the id: %q", e.Data.What)
	}
}

func TestANonexistentSubscriptionIDIsANamedErrorNotIgnored(t *testing.T) {
	tbl, events := askTable(t, &fakeStreamer{texts: []string{"hi"}}, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "hi", SubscriptionID: "s-never-existed",
	}))
	if err == nil {
		t.Fatal("a nonexistent subscription was accepted. The client would wait for a stream " +
			"that will never arrive — which looks identical to a slow model")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31012 {
		t.Fatalf("want -31012, got %v", err)
	}
	// And nothing was called: the subscription is validated BEFORE the call, so
	// a bad id does not cost money.
	time.Sleep(50 * time.Millisecond)
	if len(events.all()) != 0 {
		t.Errorf("events were emitted for a refused ask: %+v", events.all())
	}
}

func TestAskWithoutASubscriptionIDReturnsABufferedReply(t *testing.T) {
	streamer := &fakeStreamer{
		texts: []string{"Hello", " there"},
		usage: model.Usage{InputTokens: model.IntPtr(10), OutputTokens: model.IntPtr(5)},
		cost:  model.Cost{State: model.CostKnown, USD: 0.000105},
		stop:  "stop",
	}
	tbl, events := askTable(t, streamer, pricedRoute("r1", "openai"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "r1", Prompt: "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	ask := res.(AskResult)

	if ask.Text != "Hello there" {
		t.Errorf("text = %q", ask.Text)
	}
	if ask.Usage == nil || ask.Cost == nil {
		t.Fatalf("buffered mode must carry usage and cost: %+v", ask)
	}
	if ask.Cost.State != model.CostKnown {
		t.Errorf("cost = %+v", ask.Cost)
	}
	if ask.Truncated {
		t.Error("a short reply reported truncation")
	}
	// No events: without a subscription there is nobody to send them to.
	if len(events.all()) != 0 {
		t.Errorf("buffered mode emitted events: %+v", events.all())
	}
}

func TestExceedingTheBufferBoundTruncatesAndReportsIt(t *testing.T) {
	// Two chunks, the second of which overflows.
	big := strings.Repeat("x", AskBufferBound)
	streamer := &fakeStreamer{
		texts: []string{big, "this will not fit"},
		usage: model.Usage{InputTokens: model.IntPtr(1), OutputTokens: model.IntPtr(99999)},
		cost:  model.Cost{State: model.CostKnown, USD: 1.5},
		stop:  "length",
	}
	tbl, _ := askTable(t, streamer, pricedRoute("r1", "openai"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "r1", Prompt: "write a novel"}))
	if err != nil {
		t.Fatal(err)
	}
	ask := res.(AskResult)

	if !ask.Truncated {
		t.Fatal("the buffer bound was exceeded and `truncated` is false. A reply that stops " +
			"mid-sentence with no explanation reads as a model failure")
	}
	if len(ask.Text) > AskBufferBound {
		t.Errorf("the buffer grew to %d, past its %d bound", len(ask.Text), AskBufferBound)
	}
	// The cost is still the WHOLE call's: every token was bought, including the
	// ones that did not fit.
	if ask.Cost.State != model.CostKnown || ask.Cost.USD != 1.5 {
		t.Errorf("cost = %+v, want the whole call's", ask.Cost)
	}
}

// The bound is a guess and says so, or nobody revisits it.
func TestTheBufferBoundIsDocumentedAsAGuess(t *testing.T) {
	src, err := readProtocolSource("ask.go")
	if err != nil {
		t.Fatal(err)
	}
	// The DECLARATION, not the first mention of the name — which is inside the
	// doc comment this test is looking for, so searching for the name alone
	// sliced the comment off and the test failed on correct code.
	idx := strings.Index(src, "const AskBufferBound")
	if idx < 0 {
		t.Fatal("AskBufferBound is not declared as a const")
	}
	doc := src[:idx]
	if !strings.Contains(doc, "A guess") {
		t.Error("AskBufferBound is not documented as a guess")
	}
	if !strings.Contains(doc, "Revisit") {
		t.Error("AskBufferBound has no named trigger for revisiting")
	}
}

// **A new method and two new event kinds are additive** (T2). This is the first
// slice to add a method since the protocol existed, so it is the first chance
// for the rule to be broken.
func TestProtocolVersionIsUnchangedByThisAddition(t *testing.T) {
	if PROTOCOL_VERSION != 2 {
		t.Fatalf("PROTOCOL_VERSION = %d, want 2. The history this pin carries: S3's method and event kinds were ALL "+
			"ADDITIVE and left version 1 at 1. S38 T2 moved it to 2 against the BREAKING column of the rule beside "+
			"the constant — OL1 RENAMED methods (`project.*` became `component.*`) and RENAMED the scope field "+
			"(`projectRoot`→`componentRoot`) in every params and result that carries it; MinSupportedVersion went "+
			"with it because nothing ever spoke version 1 (brief §0.2). If a later slice bumped it past 2, its "+
			"design must say why against that rule", PROTOCOL_VERSION)
	}
}

func TestAnUnknownRouteReturnsANamedErrorListingTheConfiguredIDs(t *testing.T) {
	tbl, _ := askTable(t, &fakeStreamer{},
		pricedRoute("anthropic-sonnet", "anthropic"), pricedRoute("openai-gpt4o", "openai"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "gpt-5-ultra", Prompt: "hi"}))
	if err == nil {
		t.Fatal("an unknown route was accepted")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31100 {
		t.Fatalf("want -31100, got %v", err)
	}
	for _, want := range []string{"gpt-5-ultra", "anthropic-sonnet", "openai-gpt4o"} {
		if !strings.Contains(e.Data.What, want) {
			t.Errorf("the error does not name %q: %q", want, e.Data.What)
		}
	}
}

func TestNoRoutesConfiguredSaysSoRatherThanListingNothing(t *testing.T) {
	tbl, _ := askTable(t, &fakeStreamer{})
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "anything", Prompt: "hi"}))
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want an Aeon error, got %v", err)
	}
	if !strings.Contains(e.Data.What, "no routes are configured") {
		t.Errorf("the error lists an empty set instead of saying the set is empty: %q", e.Data.What)
	}
}

// M15 — a client disconnecting mid-ask still reports the spend.
func TestAClientDisconnectingMidAskStillEmitsAskDoneWithTheUsageSeen(t *testing.T) {
	streamer := &fakeStreamer{
		texts: []string{"one", "two", "three", "four"},
		usage: model.Usage{InputTokens: model.IntPtr(100), OutputTokens: model.IntPtr(20)},
		cost:  model.Cost{State: model.CostKnown, USD: 0.0006},
		delay: 40 * time.Millisecond,
	}
	tbl, events := askTable(t, streamer, pricedRoute("r1", "anthropic"))

	ctx, cancel := context.WithCancel(context.Background())
	if err := tbl.Sessions.Open(ctx, &Session{ConnID: "c1", Version: PROTOCOL_VERSION}); err != nil {
		t.Fatal(err)
	}
	subRes, err := tbl.Dispatch(ctx, "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"ask.*"}}}))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tbl.Dispatch(ctx, "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "hi", SubscriptionID: subRes.(SubscribeResult).SubscriptionID,
	})); err != nil {
		t.Fatal(err)
	}

	// Let one delta through, then disconnect.
	waitForEvents(t, events, KindAskDelta, 1)
	cancel()

	done := waitForEvents(t, events, KindAskDone, 1)
	var payload AskDonePayload
	if err := json.Unmarshal(done[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Usage.InputTokens == nil {
		t.Fatalf("ask.done carries no usage after a disconnect: %+v. A cancelled ask that "+
			"reports nothing loses the spend, and those tokens were still bought (M15)", payload)
	}
	if payload.Error == nil {
		t.Fatal("ask.done does not record that the call was cut")
	}
	// The structured error, so a client renders three parts wherever the
	// failure came from.
	if payload.Error.Data.Todo == "" {
		t.Error("the error on ask.done carries no `todo`; in streaming mode this is the only " +
			"place a client learns what to do")
	}
}

func TestAProviderFailureIsAnAeonErrorCarryingTheProvidersWords(t *testing.T) {
	streamer := &fakeStreamer{err: errors.New("model: anthropic answered 404: no such model")}
	tbl, _ := askTable(t, streamer, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "r1", Prompt: "hi"}))
	if err == nil {
		t.Fatal("a provider failure was not reported")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31102 {
		t.Fatalf("want -31102, got %v", err)
	}
	if !strings.Contains(e.Data.What, "404") {
		t.Errorf("the provider's own words were discarded: %q", e.Data.What)
	}
}

func TestAskRequiresAPrompt(t *testing.T) {
	tbl, _ := askTable(t, &fakeStreamer{}, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	for _, prompt := range []string{"", "   ", "\n\t"} {
		_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
			mustJSON(t, AskParams{Route: "r1", Prompt: prompt}))
		if err == nil {
			t.Errorf("accepted a blank prompt %q", prompt)
		}
	}
}

// A daemon with no model layer says so rather than panicking.
func TestAskWithoutAModelLayerSaysSo(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "r1", Prompt: "hi"}))
	if err == nil {
		t.Fatal("ask succeeded with no model layer wired")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want an Aeon error, got %v", err)
	}
	if !strings.Contains(e.Data.What, "no model layer") {
		t.Errorf("the error does not say what is missing: %q", e.Data.What)
	}
}

// A tool call reaches the client as a complete block. Nothing executes it at
// S3 (N3); it is visible so a person can see what the model asked for.
func TestAToolCallReachesTheClientAsACompleteBlock(t *testing.T) {
	streamer := &fakeStreamer{
		texts: []string{"checking"},
		block: &model.Block{
			Kind: model.BlockToolUse, ID: "tu_1", Name: "get_weather",
			Input: json.RawMessage(`{"location":"Lisbon"}`),
		},
		stop: "tool_use",
	}
	tbl, events := askTable(t, streamer, pricedRoute("r1", "anthropic"))
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	subRes, _ := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"ask.*"}}}))
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "r1", Prompt: "weather?", SubscriptionID: subRes.(SubscribeResult).SubscriptionID,
	})); err != nil {
		t.Fatal(err)
	}

	for _, e := range waitForEvents(t, events, KindAskDelta, 2) {
		var p AskDeltaPayload
		_ = json.Unmarshal(e.Payload, &p)
		if p.Kind != string(model.DeltaToolUse) {
			continue
		}
		if p.Block == nil {
			t.Fatal("a tool_use delta carries no block")
		}
		if err := p.Block.Validate(); err != nil {
			t.Errorf("the block does not validate: %v", err)
		}
		return
	}
	t.Fatal("no tool_use delta reached the client")
}
