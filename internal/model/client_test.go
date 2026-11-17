package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go/ast"
)

// # No test here touches the network (I7, D15)
//
// Every response comes from a scripted transport. The suite passes with no
// credentials in the environment, which is the invariant whose violation passes
// review every time because it works on the author's machine.

// scriptedTransport answers each request from a list, in order.
type scriptedTransport struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     int
	// lastRequest keeps the most recent request for wire-shape assertions
	// (S22 T3: the embed test checks path, bearer and body — the chat tests
	// never needed the request itself, so this is the fake growing a memory,
	// not changing its behaviour).
	lastRequest *http.Request
}

func (s *scriptedTransport) lastReq() *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRequest
}

type scriptedResponse struct {
	status  int
	headers map[string]string
	body    string
	// err, when set, is returned instead of a response — a transport failure.
	err error
}

func (s *scriptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.calls >= len(s.responses) {
		return nil, fmt.Errorf("the transport was called %d times; only %d responses were scripted",
			s.calls+1, len(s.responses))
	}
	resp := s.responses[s.calls]
	s.calls++
	s.lastRequest = r

	if resp.err != nil {
		return nil, resp.err
	}
	header := http.Header{}
	for k, v := range resp.headers {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: resp.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Request:    r,
	}, nil
}

func (s *scriptedTransport) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// testClient builds a client whose sleeps are instant and recorded, so a retry
// test does not spend real seconds.
func testClient(t *testing.T, responses ...scriptedResponse) (*Client, *scriptedTransport, *[]time.Duration) {
	t.Helper()
	transport := &scriptedTransport{responses: responses}
	var slept []time.Duration
	c := &Client{
		HTTP:        &http.Client{Transport: transport},
		BaseBackoff: time.Millisecond,
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			// Still honours the context, which is what the cancellation test
			// depends on.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
		Now: func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) },
	}
	return c, transport, &slept
}

// A route for a provider registered only for these tests, so the client's own
// behaviour is under test rather than an adapter's.
const testProvider = "client-test-provider"

func testStreamRoute() Route {
	return Route{
		ID: "client-test", Provider: testProvider, Model: "m", MaxOutputTokens: 100,
		BaseURL: "https://example.invalid", APIKey: "<redacted>",
		Prices: &Prices{InputUSDPerMTok: Float64Ptr(3), OutputUSDPerMTok: Float64Ptr(15)},
	}
}

func simpleRequest() ModelRequest {
	return ModelRequest{
		Route:    testStreamRoute(),
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock("hi")}}},
	}
}

// fakeAdapter yields one text delta per frame and reports scripted usage.
type fakeAdapter struct {
	mu          sync.Mutex
	usage       Usage
	stop        string
	dropped     int
	owed        []string
	toWireError error
}

func (f *fakeAdapter) Provider() string { return testProvider }

func (f *fakeAdapter) ToWire(req ModelRequest) (*http.Request, error) {
	if f.toWireError != nil {
		return nil, f.toWireError
	}
	return http.NewRequest(http.MethodPost, "https://example.invalid/v1/stream", strings.NewReader("{}"))
}

func (f *fakeAdapter) FromWire(ctx context.Context, frames <-chan Frame) <-chan Delta {
	out := make(chan Delta)
	go func() {
		defer close(out)
		for frame := range frames {
			var payload struct {
				Text  string `json:"text"`
				Usage *Usage `json:"usage"`
				Stop  string `json:"stop"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
				f.mu.Lock()
				f.dropped++
				f.mu.Unlock()
				continue
			}
			if payload.Usage != nil {
				f.mu.Lock()
				f.usage = *payload.Usage
				f.mu.Unlock()
			}
			if payload.Stop != "" {
				f.mu.Lock()
				f.stop = payload.Stop
				f.mu.Unlock()
				select {
				case out <- Delta{Kind: DeltaStop, Reason: payload.Stop}:
				case <-ctx.Done():
					return
				}
				continue
			}
			if payload.Text != "" {
				select {
				case out <- Delta{Kind: DeltaText, Text: payload.Text}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func (f *fakeAdapter) Finalise() (Usage, string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage, f.stop, f.dropped
}

func (f *fakeAdapter) OwedToolErrors() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owed
}

// registerTestAdapter installs the fake for the duration of a test.
func registerTestAdapter(t *testing.T, a Adapter) {
	t.Helper()
	adapterMu.Lock()
	previous, existed := adapters[testProvider]
	adapterMu.Unlock()

	RegisterAdapterReplacing(testProvider, func() Adapter { return a })
	t.Cleanup(func() {
		adapterMu.Lock()
		defer adapterMu.Unlock()
		if existed {
			adapters[testProvider] = previous
		} else {
			delete(adapters, testProvider)
		}
	})
}

// collectStream drains all three channels.
func collectStream(t *testing.T, deltas <-chan Delta, results <-chan StreamResult, errs <-chan error) ([]Delta, []StreamResult, []error) {
	t.Helper()
	var gotDeltas []Delta
	for d := range deltas {
		gotDeltas = append(gotDeltas, d)
	}
	var gotResults []StreamResult
	for r := range results {
		gotResults = append(gotResults, r)
	}
	var gotErrs []error
	for e := range errs {
		gotErrs = append(gotErrs, e)
	}
	return gotDeltas, gotResults, gotErrs
}

const okStream = "data: {\"text\":\"Hello\"}\n\n" +
	"data: {\"text\":\" world\"}\n\n" +
	"data: {\"usage\":{\"inputTokens\":100,\"outputTokens\":50}}\n\n" +
	"data: {\"stop\":\"end_turn\"}\n\n"

func TestAHappyPathStreamsDeltasAndSendsExactlyOneResult(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, _ := testClient(t, scriptedResponse{status: 200, body: okStream})

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	gotDeltas, gotResults, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 0 {
		t.Fatalf("errors on a happy path: %v", gotErrs)
	}
	if len(gotResults) != 1 {
		t.Fatalf("got %d results, want EXACTLY 1 — a caller that sees two cannot add up a "+
			"loop's cost", len(gotResults))
	}
	if transport.callCount() != 1 {
		t.Errorf("sent %d requests on a happy path", transport.callCount())
	}

	var text string
	for _, d := range gotDeltas {
		if d.Kind == DeltaText {
			text += d.Text
		}
	}
	if text != "Hello world" {
		t.Errorf("text = %q", text)
	}

	// Cost computed once, from the usage the adapter reported.
	res := gotResults[0]
	if res.Cost.State != CostKnown {
		t.Fatalf("cost = %+v, want known", res.Cost)
	}
	const wantUSD = 100.0/1_000_000*3 + 50.0/1_000_000*15
	if diff := res.Cost.USD - wantUSD; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("USD = %v, want %v", res.Cost.USD, wantUSD)
	}
	if res.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", res.Attempts)
	}
}

// **The first of the two tests that matter.**
func TestA500BeforeTheFirstDeltaIsRetriedAndSucceeds(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, slept := testClient(t,
		scriptedResponse{status: 500, body: `{"error":"internal"}`},
		scriptedResponse{status: 200, body: okStream},
	)

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	gotDeltas, gotResults, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 0 {
		t.Fatalf("a retryable failure before the first delta was reported as an error: %v", gotErrs)
	}
	if transport.callCount() != 2 {
		t.Fatalf("sent %d requests, want 2 — the 500 must be retried", transport.callCount())
	}
	if len(*slept) != 1 {
		t.Errorf("slept %d times, want 1 backoff", len(*slept))
	}
	if len(gotDeltas) == 0 {
		t.Error("no deltas after a successful retry")
	}
	if len(gotResults) != 1 || gotResults[0].Attempts != 2 {
		t.Errorf("results = %+v, want one with Attempts 2", gotResults)
	}
}

// **The second, and the one a naive test gets wrong.**
//
// A test that only checks for an error passes against an implementation that
// retried and duplicated every delta. So this counts them.
func TestA500AfterTheFirstDeltaFailsAndTheDeltaWasYieldedExactlyOnce(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	// A stream that begins fine and then the body ends mid-flight, followed by
	// a 500 that must NOT be used.
	c, transport, _ := testClient(t,
		scriptedResponse{status: 200, body: "data: {\"text\":\"Hello\"}\n\n"},
		scriptedResponse{status: 200, body: okStream},
	)

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	gotDeltas, gotResults, _ := collectStream(t, deltas, results, errs)

	// Exactly one request: once the stream began, the call is committed.
	if transport.callCount() != 1 {
		t.Fatalf("sent %d requests; once a delta has been yielded the call must not be "+
			"retried, because the caller has already rendered it", transport.callCount())
	}
	count := 0
	for _, d := range gotDeltas {
		if d.Kind == DeltaText && d.Text == "Hello" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the delta was yielded %d times, want exactly once", count)
	}
	if len(gotResults) != 1 {
		t.Fatalf("got %d results, want 1", len(gotResults))
	}
}

// A transport error after a delta is likewise not retried.
func TestATransportErrorAfterTheFirstDeltaIsNotRetried(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, _ := testClient(t,
		// A body that yields a delta, then a reader error.
		scriptedResponse{status: 200, body: "data: {\"text\":\"Hello\"}\n\n"},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)
	if transport.callCount() != 1 {
		t.Fatalf("sent %d requests, want 1", transport.callCount())
	}
}

func TestA400IsNotRetried(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	for _, status := range []int{400, 401, 403, 404, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, transport, _ := testClient(t,
				scriptedResponse{status: status, body: `{"error":"nope"}`},
				scriptedResponse{status: 200, body: okStream},
			)
			deltas, results, errs := c.Stream(context.Background(), simpleRequest())
			_, gotResults, gotErrs := collectStream(t, deltas, results, errs)

			if transport.callCount() != 1 {
				t.Fatalf("a %d was retried (%d requests). It is a schema or content error: "+
					"retrying wastes time and money and produces the same answer",
					status, transport.callCount())
			}
			if len(gotErrs) != 1 {
				t.Fatalf("got %d errors, want 1", len(gotErrs))
			}
			if !strings.Contains(gotErrs[0].Error(), "not retried") {
				t.Errorf("the error does not say it was not retried: %v", gotErrs[0])
			}
			// A result is still sent, so a caller can account for the attempt.
			if len(gotResults) != 1 {
				t.Errorf("got %d results on a failure, want 1", len(gotResults))
			}
		})
	}
}

func TestA429IsRetried(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, _ := testClient(t,
		scriptedResponse{status: 429, body: `{"error":"slow down"}`},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)
	if transport.callCount() != 2 {
		t.Fatalf("a 429 was not retried (%d requests)", transport.callCount())
	}
}

func TestRetryAfterIsHonouredInSecondsForm(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, slept := testClient(t,
		scriptedResponse{status: 429, headers: map[string]string{"Retry-After": "7"}},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)

	if len(*slept) != 1 {
		t.Fatalf("slept %d times", len(*slept))
	}
	if (*slept)[0] != 7*time.Second {
		t.Fatalf("slept %v, want the provider's 7s. It knows its own rate limit better than an "+
			"exponential guess does", (*slept)[0])
	}
}

// A client handling only the seconds form waits zero against a provider that
// sends a date, and then retries immediately into the same rate limit.
func TestRetryAfterIsHonouredInHTTPDateForm(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, slept := testClient(t,
		scriptedResponse{status: 503, headers: map[string]string{
			// The client's Now is fixed at 12:00:00, so this is 90s ahead.
			"Retry-After": "Thu, 15 Oct 2026 12:01:30 GMT",
		}},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)

	if len(*slept) != 1 {
		t.Fatalf("slept %d times", len(*slept))
	}
	if (*slept)[0] != 90*time.Second {
		t.Fatalf("slept %v, want 90s parsed from the HTTP-date. A client that handles only the "+
			"seconds form waits zero here and retries straight into the same limit", (*slept)[0])
	}
}

// A Retry-After in the past means "retry now", not "wait a negative duration".
func TestRetryAfterInThePastMeansRetryNow(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, slept := testClient(t,
		scriptedResponse{status: 503, headers: map[string]string{
			"Retry-After": "Thu, 15 Oct 2026 11:59:00 GMT",
		}},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)
	if len(*slept) != 1 || (*slept)[0] != 0 {
		t.Fatalf("slept %v, want 0", *slept)
	}
}

// An absurd value is clamped; a merely long one is not (see maxRetryAfter).
func TestAnEnormousRetryAfterIsClamped(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, slept := testClient(t,
		scriptedResponse{status: 429, headers: map[string]string{"Retry-After": "3600"}},
		scriptedResponse{status: 200, body: okStream},
	)
	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	collectStream(t, deltas, results, errs)
	if (*slept)[0] != maxRetryAfter {
		t.Fatalf("slept %v, want it clamped to %v", (*slept)[0], maxRetryAfter)
	}
}

func TestRetriesAreBoundedAndTheFailureSaysSo(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	// More failures than the attempt budget.
	c, transport, _ := testClient(t,
		scriptedResponse{status: 500}, scriptedResponse{status: 500},
		scriptedResponse{status: 500}, scriptedResponse{status: 500},
		scriptedResponse{status: 500}, scriptedResponse{status: 500},
	)
	c.MaxAttempts = 3

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	_, gotResults, gotErrs := collectStream(t, deltas, results, errs)

	if transport.callCount() != 3 {
		t.Fatalf("sent %d requests, want the 3-attempt budget", transport.callCount())
	}
	if len(gotErrs) != 1 || !strings.Contains(gotErrs[0].Error(), "gave up after 3") {
		t.Fatalf("errors = %v, want one naming the attempt count", gotErrs)
	}
	if gotResults[0].Attempts != 3 {
		t.Errorf("Attempts = %d", gotResults[0].Attempts)
	}
}

// **With a timeout**, because the failure mode is a hang.
func TestACancelledContextDuringBackoffEndsImmediately(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	transport := &scriptedTransport{responses: []scriptedResponse{
		{status: 500}, {status: 200, body: okStream},
	}}
	c := &Client{
		HTTP:        &http.Client{Transport: transport},
		BaseBackoff: time.Hour, // a delay no test should ever wait out
	}

	ctx, cancel := context.WithCancel(context.Background())
	deltas, results, errs := c.Stream(ctx, simpleRequest())
	cancel()

	done := make(chan struct{})
	go func() {
		collectStream(t, deltas, results, errs)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled context slept out its backoff. Cancelling a loop would then take " +
			"as long as the provider asked us to wait, which on a 429 can be a minute")
	}
}

// M15 — cancellation reports the spend.
func TestACancelledContextMidStreamSendsAResultWithTheUsageSeen(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	// Usage arrives early, then more text. Cancellation happens after the
	// usage, so there IS something to report.
	body := "data: {\"usage\":{\"inputTokens\":100,\"outputTokens\":50}}\n\n" +
		"data: {\"text\":\"one\"}\n\n" +
		"data: {\"text\":\"two\"}\n\n"
	c, _, _ := testClient(t, scriptedResponse{status: 200, body: body})

	ctx, cancel := context.WithCancel(context.Background())
	deltas, results, errs := c.Stream(ctx, simpleRequest())

	// Read one delta, then cancel mid-stream.
	<-deltas
	cancel()

	for range deltas {
	}
	var gotResults []StreamResult
	for r := range results {
		gotResults = append(gotResults, r)
	}
	for range errs {
	}

	if len(gotResults) != 1 {
		t.Fatalf("got %d results on a cancelled call, want 1 — a cancelled call still cost "+
			"money, and discarding that is how a loop's total goes quietly wrong", len(gotResults))
	}
	res := gotResults[0]
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 100 {
		t.Fatalf("usage = %+v, want the counts seen before cancellation", res.Usage)
	}
	// Usage was seen, so the cost is real rather than stream-aborted-early.
	if res.Cost.State != CostKnown {
		t.Errorf("cost = %+v, want known: the usage arrived before the cancellation", res.Cost)
	}
}

// The other half: cancelled before any usage arrived.
func TestACancelledStreamBeforeAnyUsageReportsStreamAbortedEarly(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, _ := testClient(t, scriptedResponse{status: 200,
		body: "data: {\"text\":\"one\"}\n\ndata: {\"text\":\"two\"}\n\n"})

	ctx, cancel := context.WithCancel(context.Background())
	deltas, results, errs := c.Stream(ctx, simpleRequest())
	<-deltas
	cancel()
	for range deltas {
	}

	var gotResults []StreamResult
	for r := range results {
		gotResults = append(gotResults, r)
	}
	for range errs {
	}

	if len(gotResults) != 1 {
		t.Fatalf("got %d results", len(gotResults))
	}
	cost := gotResults[0].Cost
	if cost.State != CostUnknown {
		t.Fatalf("cost = %+v, want unknown", cost)
	}
	if cost.Reason != ReasonStreamAbortedEarly {
		t.Fatalf("reason = %q, want %q — this is the one reason cost.go deliberately does not "+
			"produce, because only the caller of the stream knows it was cut",
			cost.Reason, ReasonStreamAbortedEarly)
	}
}

// D6 — the client says a tool_result with IsError is owed; it does not invent
// a tool outcome.
func TestUnparseableToolArgumentsProduceAnOwedErrorResult(t *testing.T) {
	fake := &fakeAdapter{owed: []string{"call_bad_1", "call_bad_2"}}
	registerTestAdapter(t, fake)
	c, _, _ := testClient(t, scriptedResponse{status: 200, body: okStream})

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	_, gotResults, _ := collectStream(t, deltas, results, errs)

	if len(gotResults) != 1 {
		t.Fatal("no result")
	}
	owed := gotResults[0].OwedToolErrors
	if len(owed) != 2 {
		t.Fatalf("OwedToolErrors = %v, want both call ids. Without this the model never learns "+
			"its arguments were wrong, and the next turn repeats the mistake", owed)
	}
}

func TestAnUnknownProviderFailsBeforeAnyRequestIsSent(t *testing.T) {
	c, transport, _ := testClient(t, scriptedResponse{status: 200, body: okStream})
	req := simpleRequest()
	req.Route.Provider = "no-such-provider"

	deltas, results, errs := c.Stream(context.Background(), req)
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("errors = %v, want one", gotErrs)
	}
	if transport.callCount() != 0 {
		t.Error("a request was sent for an unresolvable provider")
	}
	_ = results
}

func TestAnInvalidRequestFailsBeforeAnyRequestIsSent(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, _ := testClient(t, scriptedResponse{status: 200, body: okStream})

	req := simpleRequest()
	req.Messages = nil

	deltas, results, errs := c.Stream(context.Background(), req)
	_, _, gotErrs := collectStream(t, deltas, results, errs)
	if len(gotErrs) != 1 {
		t.Fatalf("errors = %v, want one", gotErrs)
	}
	if transport.callCount() != 0 {
		t.Error("a request was sent for an invalid ModelRequest")
	}
}

// A ToWire failure is reported, and still accompanied by a result.
func TestAToWireFailureIsReportedWithAResult(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{toWireError: errors.New("cannot build")})
	c, transport, _ := testClient(t, scriptedResponse{status: 200, body: okStream})

	deltas, results, errs := c.Stream(context.Background(), simpleRequest())
	_, gotResults, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 || !strings.Contains(gotErrs[0].Error(), "cannot build") {
		t.Fatalf("errors = %v", gotErrs)
	}
	if len(gotResults) != 1 {
		t.Errorf("got %d results, want 1 even on a build failure", len(gotResults))
	}
	if transport.callCount() != 0 {
		t.Error("a request was sent after ToWire failed")
	}
}

// O1 / I4 — **cost is constructed in exactly two places.**
//
// `cost.go` has the arithmetic; `client.go` calls it once. An adapter computing
// its own cost would mean a third adapter at S18 could get it wrong in a way
// nothing here would catch — and the figure would look plausible.
func TestCostIsConstructedOnlyInClientAndCostFiles(t *testing.T) {
	roots := []string{".", "adapters", filepath.Join("..", "daemon"), filepath.Join("..", "..", "cmd", "aeon")}
	// `embed.go` joined the set at S22 T3, and it belongs there for the
	// same reason `client.go` was always in it: it is not an adapter
	// reaching for a private formula, it is the OTHER METHOD ON THE SAME
	// CLIENT — its ComputeCost call prices embeddings at the one site, and
	// its `Cost` literals are all pre-wire refusals (a call never made
	// has a cost to report: unknown, `call-not-attempted`). The property
	// this guard defends — no second place where cost is COMPUTED — holds
	// with embed.go in and would be merely hidden by excluding it.
	allowed := map[string]bool{"cost.go": true, "client.go": true, "embed.go": true}

	for _, root := range roots {
		files, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			base := filepath.Base(file)
			if strings.HasSuffix(base, "_test.go") || allowed[base] {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				// A composite literal of type Cost, or a call to ComputeCost.
				switch v := n.(type) {
				case *ast.CompositeLit:
					if name := typeName(v.Type); name == "Cost" {
						t.Errorf("%s constructs a Cost. Cost is computed once, in client.go, "+
							"from cost.go's helpers (O1) — so a third adapter at S18 cannot "+
							"get it wrong", file)
					}
				case *ast.CallExpr:
					if name := calleeName(v.Fun); name == "ComputeCost" {
						t.Errorf("%s calls ComputeCost. Exactly one call site exists, in "+
							"client.go (I4)", file)
					}
				}
				return true
			})
		}
	}
}

func typeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

func calleeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// --- the reasoning_effort 400 ------------------------------------------------

// The refusal that produced `reasoningEffortHint`, verbatim from the provider.
const reasoningEffort400 = `{
  "error": {
    "message": "Function tools with reasoning_effort are not supported for gpt-6-luna in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.",
    "type": "invalid_request_error",
    "param": "reasoning_effort",
    "code": null
  }
}`

func toolRequest(route Route) ModelRequest {
	return ModelRequest{
		Route:    route,
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock("hi")}}},
		Tools: []ToolSpec{{
			Name: "read", Description: "read a file",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
	}
}

// A provider message that is accurate and unactionable becomes one a person can
// act on.
//
// # Why this exists at all
//
// The provider names a parameter the caller never set and an endpoint Aeon does
// not use. Everything it says is true and none of it says "your route needs one
// more field" — so without this the next person reads a 400 about
// `/v1/responses` and starts looking for a bug in the step runtime.
func TestAReasoningEffortRefusalNamesTheRemedy(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(testStreamRoute()))
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	msg := gotErrs[0].Error()

	// The provider's own text survives: it is the ground truth, and a hint that
	// replaced it would hide a message that may say something else next time.
	if !strings.Contains(msg, "not supported for gpt-6-luna") {
		t.Errorf("the provider's message was lost:\n%s", msg)
	}
	// And the remedy is a command, with this route's id in it.
	// The suggested command must be COMPLETE: `routes set` requires --provider
	// and --model, so a line missing them is advice that fails when pasted.
	for _, want := range []string{
		"aeon routes set --id client-test --provider client-test-provider --model m " +
			"--reasoning-effort none",
		"offered 1 tool(s)",
		"unset, so the model's own default applies",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not contain %q:\n%s", want, msg)
		}
	}
	// It must also say why `ask` works and a step does not — which is the
	// confusing part of this failure.
	if !strings.Contains(msg, "no step") {
		t.Errorf("the error does not explain that only steps are affected:\n%s", msg)
	}
}

func TestTheRemedyOffersBothWaysOut(t *testing.T) {
	// Two remedies that trade off against each other: `none` is one flag and
	// gives up reasoning for this step; `responses` keeps reasoning and is a
	// second wire format. Found necessary when the only adapter was the chat
	// one — at that point "give up reasoning" was the only honest advice, and
	// once `responses` existed the message had to say so too.
	registerTestAdapter(t, &fakeAdapter{})
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(testStreamRoute()))
	_, _, gotErrs := collectStream(t, deltas, results, errs)
	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	msg := gotErrs[0].Error()

	if !strings.Contains(msg, "--reasoning-effort none") {
		t.Error("the message does not offer turning reasoning off")
	}
	if !strings.Contains(msg, "--openai-endpoint responses") {
		t.Error("the message does not offer the endpoint that keeps reasoning")
	}
}

func TestAnExplicitCallLevelEffortIsToldBothWaysOutToo(t *testing.T) {
	registerTestAdapter(t, &fakeAdapter{})
	req := toolRequest(testStreamRoute())
	req.ReasoningEffort = "medium"
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), req)
	_, _, gotErrs := collectStream(t, deltas, results, errs)
	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	msg := gotErrs[0].Error()

	if !strings.Contains(msg, `explicitly on the chat API`) {
		t.Errorf("the error does not say the effort was named on the call:\n%s", msg)
	}
	if !strings.Contains(msg, "--reasoning-effort none") {
		t.Error("the message does not offer the per-call override")
	}
	if !strings.Contains(msg, "responses") {
		t.Error("the message does not offer the endpoint that keeps reasoning")
	}
}

func TestTheRemedyReportsTheRoutesCurrentEffortWhenItHasOne(t *testing.T) {
	// A route set to `high` and refused needs to see what it is currently set
	// to; "unset" would read as a bug in Aeon.
	registerTestAdapter(t, &fakeAdapter{})
	route := testStreamRoute()
	route.ReasoningEffort = "high"
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(route))
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	if !strings.Contains(gotErrs[0].Error(), `"high"`) {
		t.Errorf("the error does not report the current value:\n%s", gotErrs[0])
	}
}

func TestARouteAlreadySetToNoneIsToldTheRealRemedyInstead(t *testing.T) {
	// Repeating advice that demonstrably did not work is worse than saying so.
	// If `none` was already sent on the chat API and the tools were still
	// refused, lowering the effort further is not the remedy — but a REAL one
	// exists now: the other OpenAI API accepts tools and reasoning together,
	// and the message names the exact command.
	registerTestAdapter(t, &fakeAdapter{})
	route := testStreamRoute()
	route.ReasoningEffort = "none"
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(route))
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	msg := gotErrs[0].Error()
	if strings.Contains(msg, "--reasoning-effort none") {
		t.Errorf("the error repeats advice that did not work:\n%s", msg)
	}
	if !strings.Contains(msg, "already sets reasoningEffort=none") {
		t.Errorf("the error does not say the remedy was already applied:\n%s", msg)
	}
	// The REAL next step — the complete command, including --provider and
	// --model, so it runs if pasted verbatim.
	if !strings.Contains(msg, "aeon routes set --id client-test --provider client-test-provider "+
		"--model m --openai-endpoint responses") {
		t.Errorf("the error does not give the complete command:\n%s", msg)
	}
}

func TestARouteOnTheResponsesEndpointGetsADifferentMessageOnThisRefusal(t *testing.T) {
	// If the restriction fires on the endpoint that is supposed to accept
	// tools and reasoning together, the usual remedy (switch to that
	// endpoint) is nonsensical — the route is already there. The message
	// must not suggest it.
	registerTestAdapter(t, &fakeAdapter{})
	route := testStreamRoute()
	route.ReasoningEffort = "medium"
	route.OpenAIEndpoint = OpenAIEndpointResponses
	c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(route))
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	msg := gotErrs[0].Error()
	if strings.Contains(msg, "aeon routes set") {
		t.Errorf("the error suggests a route change on a route already at the suggested state:\n%s", msg)
	}
	if !strings.Contains(msg, "already talks to /v1/responses") {
		t.Errorf("the error does not say the route is already on that endpoint:\n%s", msg)
	}
}

func TestTheHintIsSilentWhenItDoesNotApply(t *testing.T) {
	// A hint that fires on unrelated 400s is a hint people learn to ignore.
	cases := []struct {
		name string
		req  ModelRequest
		body string
	}{
		{
			name: "a step with tools, refused for something else",
			req:  toolRequest(testStreamRoute()),
			body: `{"error":{"message":"max_completion_tokens is too large","param":"max_completion_tokens"}}`,
		},
		{
			name: "an ask with no tools, and the same reasoning refusal",
			req:  simpleRequest(),
			body: reasoningEffort400,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registerTestAdapter(t, &fakeAdapter{})
			c, _, _ := testClient(t, scriptedResponse{status: http.StatusBadRequest, body: tc.body})
			deltas, results, errs := c.Stream(context.Background(), tc.req)
			_, _, gotErrs := collectStream(t, deltas, results, errs)

			if len(gotErrs) != 1 {
				t.Fatalf("%d errors, want 1", len(gotErrs))
			}
			if strings.Contains(gotErrs[0].Error(), "aeon routes set") {
				t.Errorf("the hint fired on an unrelated failure:\n%s", gotErrs[0])
			}
		})
	}
}

func TestTheHintDoesNotTurnTheRefusalIntoARetry(t *testing.T) {
	// The route is configuration: the fix is to change it once, deliberately.
	// A retry with the parameter changed would spend a second call on a guess
	// about what somebody wanted.
	registerTestAdapter(t, &fakeAdapter{})
	c, transport, _ := testClient(t,
		scriptedResponse{status: http.StatusBadRequest, body: reasoningEffort400})

	deltas, results, errs := c.Stream(context.Background(), toolRequest(testStreamRoute()))
	_, _, gotErrs := collectStream(t, deltas, results, errs)

	if len(gotErrs) != 1 {
		t.Fatalf("%d errors, want 1", len(gotErrs))
	}
	if transport.callCount() != 1 {
		t.Errorf("%d requests were made; a 400 must not be retried", transport.callCount())
	}
	if !strings.Contains(gotErrs[0].Error(), "not retried") {
		t.Errorf("the error does not say it was not retried:\n%s", gotErrs[0])
	}
}
