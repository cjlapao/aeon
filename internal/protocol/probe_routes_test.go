package protocol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T3 at the socket: routes.probe (the operator's door), the one-request
// rule and its in-flight lock, the three states on routes.list, and the
// contradiction that refuses at use. The probe IMPLEMENTATION is
// internal/model/probe.go and is tested there; what lives here is the door,
// the cache seam, and the refusal that makes the cache matter.

// probeTransport is the scripted model layer for probe tests: a counter
// (requests are money, D7), a record of what was asked, and a gate for the
// in-flight test.
type probeTransport struct {
	mu    sync.Mutex
	calls int
	last  model.ModelRequest

	deltas  []model.Delta
	results []model.StreamResult
	err     error

	gate  chan struct{} // when non-nil, Stream blocks until it is closed
	gateO sync.Once
}

func (p *probeTransport) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	p.mu.Lock()
	p.calls++
	p.last = req
	gate := p.gate
	p.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}

	d := make(chan model.Delta, len(p.deltas))
	for _, x := range p.deltas {
		d <- x
	}
	close(d)
	r := make(chan model.StreamResult, len(p.results))
	for _, x := range p.results {
		r <- x
	}
	close(r)
	e := make(chan error, 1)
	if p.err != nil {
		e <- p.err
	}
	close(e)
	return d, r, e
}

func (p *probeTransport) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// fakeProbeCache is the daemon's cached verdicts without a database.
type fakeProbeCache struct {
	rows    map[string][]model.ProbeResult
	readErr error
	putErr  error
	puts    int
}

func (f *fakeProbeCache) PutProbeResults(routeID string, results []model.ProbeResult) error {
	f.puts++
	if f.putErr != nil {
		return f.putErr
	}
	if f.rows == nil {
		f.rows = map[string][]model.ProbeResult{}
	}
	var keep []model.ProbeResult
	for _, r := range results {
		if r.State == model.StateVerified || r.State == model.StateContradicted {
			keep = append(keep, r)
		}
	}
	f.rows[routeID] = keep
	return nil
}

func (f *fakeProbeCache) ProbeResults(routeID string) ([]model.ProbeResult, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.rows[routeID], nil
}

func claimingRoute() model.Route {
	r := model.Route{ID: "claiming", Provider: "openai", Model: "some-llm", MaxOutputTokens: 4096}
	r.WireCapabilities = []string{"tools", "streaming-usage", "thinking", "system-role", "parallel-tools"}
	return r
}

func probeTable(t *testing.T, route model.Route, tr *probeTransport, cache *fakeProbeCache) *Table {
	t.Helper()
	tbl := newTestTable(t)
	m := NewMutableRoutes()
	m.Set(route)
	tbl.Routes = m
	tbl.Models = tr
	tbl.Probes = cache
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

func callProbe(t *testing.T, tbl *Table, routeID string) (ProbeRouteResult, error) {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesProbe,
		mustJSON(t, ProbeRouteParams{Route: routeID}))
	if err != nil {
		return ProbeRouteResult{}, err
	}
	return res.(ProbeRouteResult), nil
}

// TestRoutesProbeSocketAndCLICallSameFunction — P34, checked from the
// source rather than from the answers: the probe exists ONCE, in
// internal/model, the socket handler calls it, and the CLI reaches the
// capability only as a client of the socket. A probe implementation under
// cmd/aeon would be a second door with its own drift.
func TestRoutesProbeSocketAndCLICallSameFunction(t *testing.T) {
	handlerSrc := stripProbeComments(mustReadFile(t, filepath.Join("..", "..", "internal", "protocol", "routes.go")))
	if !strings.Contains(handlerSrc, "model.Probe(") {
		t.Error("the socket handler does not call model.Probe — the probe is drifting out of its one home")
	}

	entries, err := os.ReadDir(filepath.Join("..", "..", "cmd", "aeon"))
	if err != nil {
		t.Fatal(err)
	}
	seenProbeMethod := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src := stripProbeComments(mustReadFile(t, filepath.Join("..", "..", "cmd", "aeon", e.Name())))
		if strings.Contains(src, "model.Probe(") {
			t.Errorf("%s implements its own probe — cmd/aeon must be a CLIENT of routes.probe, never a second door (P34)", e.Name())
		}
		if strings.Contains(src, ".Stream(") {
			t.Errorf("%s opens a model stream — the CLI does not send model requests, the daemon does", e.Name())
		}
		if strings.Contains(src, "MethodRoutesProbe") {
			seenProbeMethod = true
		}
	}
	if !seenProbeMethod {
		t.Error("no CLI verb calls routes.probe — `aeon routes probe` is missing, and the socket would be the only door")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// stripProbeComments removes // comments, so the greps judge code and not
// prose that names the forbidden call to forbid it.
func stripProbeComments(src string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestProbeNeverAutoFires — M31/M32's pin: nothing reads the cache into a
// request, no list call, no set call, no table construction sends a probe.
// The transport must stay untouched until routes.probe is typed.
func TestProbeNeverAutoFires(t *testing.T) {
	tr := &probeTransport{deltas: []model.Delta{{Kind: model.DeltaText, Text: "OK"}},
		results: []model.StreamResult{{Usage: model.Usage{InputTokens: model.IntPtr(1)}}}}
	cache := &fakeProbeCache{}
	tbl := probeTable(t, claimingRoute(), tr, cache)

	// Everything a daemon does around routes WITHOUT being asked to probe.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesList, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := callProbeSet(t, tbl); err != nil {
		t.Fatal(err)
	}
	if n := tr.callCount(); n != 0 {
		t.Fatalf("%d model requests without a probe call — a probe that fires on its own charges the operator for someone else's curiosity (M32)", n)
	}

	if _, err := callProbe(t, tbl, "claiming"); err != nil {
		t.Fatal(err)
	}
	if n := tr.callCount(); n != 1 {
		t.Fatalf("%d requests after the operator typed the probe, want exactly 1", n)
	}
}

func callProbeSet(t *testing.T, tbl *Table) (any, error) {
	t.Helper()
	return tbl.Dispatch(context.Background(), "c1", MethodRoutesSet,
		mustJSON(t, pricedParams("second-route")))
}

// TestProbeIsOneRequestPerRoute at the socket: five declared capabilities,
// one transport call — and the one request ASKED for what was claimed.
// (The per-capability verdicts are pinned in internal/model.)
func TestProbeIsOneRequestPerRouteAtTheSocket(t *testing.T) {
	tr := &probeTransport{
		deltas:  []model.Delta{{Kind: model.DeltaThinking, Text: "hmm"}, {Kind: model.DeltaToolUse, Block: &model.Block{Kind: model.BlockToolUse, Name: "probe_say_ok", Input: []byte(`{}`)}}},
		results: []model.StreamResult{{Usage: model.Usage{InputTokens: model.IntPtr(1), OutputTokens: model.IntPtr(1)}}},
	}
	tbl := probeTable(t, claimingRoute(), tr, &fakeProbeCache{})
	res, err := callProbe(t, tbl, "claiming")
	if err != nil {
		t.Fatal(err)
	}
	if tr.callCount() != 1 {
		t.Fatalf("%d requests for five flags, want one (D7)", tr.callCount())
	}
	if len(res.Probes) != 5 {
		t.Errorf("%d verdicts, want one per declared flag", len(res.Probes))
	}
	for _, p := range res.Probes {
		if p.Model != "some-llm" || p.ProbedAt.IsZero() {
			t.Errorf("%s: verdict without timestamp/model reaches the client (O2)", p.Capability)
		}
	}
}

// TestSecondProbeSharesTheInFlightAnswer — the in-flight lock: two operators
// (or one double-click) while a probe runs pay for ONE request, and both
// read the same answer.
func TestSecondProbeSharesTheInFlightAnswer(t *testing.T) {
	tr := &probeTransport{
		deltas:  []model.Delta{{Kind: model.DeltaText, Text: "OK"}},
		results: []model.StreamResult{{Usage: model.Usage{InputTokens: model.IntPtr(1)}}},
		gate:    make(chan struct{}),
	}
	tbl := probeTable(t, claimingRoute(), tr, &fakeProbeCache{})

	first := make(chan ProbeRouteResult, 1)
	go func() {
		res, err := callProbe(t, tbl, "claiming")
		if err != nil {
			t.Errorf("first probe: %v", err)
		}
		first <- res
	}()

	// Give the first caller time to open the flight.
	deadline := time.Now().Add(2 * time.Second)
	for tr.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second := make(chan ProbeRouteResult, 1)
	go func() {
		res, err := callProbe(t, tbl, "claiming")
		if err != nil {
			t.Errorf("second probe: %v", err)
		}
		second <- res
	}()

	time.Sleep(50 * time.Millisecond) // both are now in
	close(tr.gate)                    // release the server

	a, b := <-first, <-second
	if n := tr.callCount(); n != 1 {
		t.Fatalf("%d requests for two concurrent probes of one route, want 1 (D7)", n)
	}
	if len(a.Probes) != len(b.Probes) || len(a.Probes) == 0 {
		t.Fatalf("the waiting caller got %d verdicts, the runner %d — the in-flight answer must be shared", len(b.Probes), len(a.Probes))
	}
}

// TestContradictionRefusesAtProbeAndAtUse — refusing twice (O3): the probe
// itself prints the contradiction, and a later `ask` on the same route is
// refused from the cache without re-sending anything.
func TestContradictionRefusesAtProbeAndAtUse(t *testing.T) {
	tr := &probeTransport{err: &model.StatusError{Provider: "openai", Status: 400, Body: `{"error":"tools are not supported with reasoning_effort"}`}}
	cache := &fakeProbeCache{}
	tbl := probeTable(t, claimingRoute(), tr, cache)

	res, err := callProbe(t, tbl, "claiming")
	if err != nil {
		t.Fatal(err)
	}
	var contradicted *model.ProbeResult
	for i := range res.Probes {
		if res.Probes[i].Capability == model.CapTools && res.Probes[i].State == model.StateContradicted {
			contradicted = &res.Probes[i]
		}
	}
	if contradicted == nil {
		t.Fatalf("the probe did not record the contradiction it was shown: %v", res.Probes)
	}
	if !res.Saved {
		t.Fatal("an earned contradiction that was not cached protects nobody on Friday")
	}

	// At use: an `ask` on the route must refuse WITHOUT touching the
	// transport — the request that would have discovered it again is not
	// paid for twice.
	callsBefore := tr.callCount()
	_, err = tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{
		Route: "claiming", Prompt: "do the thing",
	}))
	if err == nil {
		t.Fatal("ask proceeded on a contradicted declaration")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrRouteContradicted.Code {
		t.Fatalf("want ErrRouteContradicted, got %v", err)
	}
	what := e.Data.What
	for _, want := range []string{"refused", "claiming", "tools", "some-llm"} {
		if !strings.Contains(what, want) {
			t.Errorf("the refusal omits %q, which is the thing that makes it actionable: %s", want, what)
		}
	}
	if !strings.Contains(what, "2026") {
		t.Errorf("the refusal carries no probe timestamp (D6): %s", what)
	}
	if n := tr.callCount(); n != callsBefore {
		t.Errorf("the refused ask still sent %d requests — the use-time refusal must precede the wire", n-callsBefore)
	}
}

// TestProbeFailureLeavesDeclared — a failed probe is the third answer,
// spoken as data, and it touches neither the cache's earned rows nor any
// declaration.
func TestProbeFailureLeavesDeclared(t *testing.T) {
	cache := &fakeProbeCache{rows: map[string][]model.ProbeResult{
		"claiming": {{Capability: model.CapTools, State: model.StateVerified, Detail: "from before",
			ProbedAt: time.Now().Add(-72 * time.Hour), Model: "some-llm"}},
	}}
	tr := &probeTransport{err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused")}
	tbl := probeTable(t, claimingRoute(), tr, cache)

	res, err := callProbe(t, tbl, "claiming")
	if err != nil {
		t.Fatalf("a failed probe is an ANSWER (the third state), not an RPC error: %v", err)
	}
	if res.Failure == "" {
		t.Error("the failure is unspoken")
	}
	if len(res.Probes) != 0 || res.Saved {
		t.Error("a failed probe produced verdicts or claimed to save")
	}
	if len(cache.rows["claiming"]) != 1 || cache.rows["claiming"][0].Detail != "from before" {
		t.Error("a failed probe overwrote the cache — a network blip just contradicted nothing, but it erased what was true")
	}
}

// TestRoutesListPrintsAllThreeStates — declared, verified, contradicted,
// side by side, each entry honest about which it is; the earned two carry
// their timestamp and model, the declared one carries neither because
// neither was ever earned.
func TestRoutesListPrintsAllThreeStates(t *testing.T) {
	cache := &fakeProbeCache{rows: map[string][]model.ProbeResult{
		"claiming": {
			{Capability: model.CapTools, State: model.StateVerified, Detail: "accepted tools",
				ProbedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), Model: "some-llm"},
			{Capability: model.CapStreamingUsage, State: model.StateContradicted, Detail: "no usage arrived",
				ProbedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), Model: "some-llm"},
		},
	}}
	tr := &probeTransport{}
	tbl := probeTable(t, claimingRoute(), tr, cache)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesList, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := res.(RoutesListResult)
	var view RouteView
	for _, v := range list.Routes {
		if v.ID == "claiming" {
			view = v
		}
	}
	if len(view.CapabilityStates) != len(*view.WireCapabilities) {
		t.Fatalf("%d states for %d declared flags — every declared flag must have exactly one state",
			len(view.CapabilityStates), len(*view.WireCapabilities))
	}
	byState := map[string]CapabilityStateView{}
	for _, s := range view.CapabilityStates {
		byState[s.State] = s
	}
	verified, ok := byState[string(model.StateVerified)]
	if !ok || verified.Capability != "tools" || verified.ProbedAt == "" || verified.ProbedModel != "some-llm" {
		t.Errorf("verified state wrong (timestamp/model are O2's requirement): %+v", verified)
	}
	contradicted, ok := byState[string(model.StateContradicted)]
	if !ok || contradicted.Capability != "streaming-usage" || contradicted.Detail != "no usage arrived" {
		t.Errorf("contradicted state wrong (detail renders verbatim): %+v", contradicted)
	}
	declared, ok := byState[string(model.StateDeclared)]
	if !ok {
		t.Fatal("a flag nobody settled reads as no state at all — declared must be SHOWN, not absent")
	}
	if declared.ProbedAt != "" || declared.ProbedModel != "" {
		t.Errorf("a declared state carries a timestamp or model it never earned: %+v", declared)
	}
	if tr.callCount() != 0 {
		t.Error("listing routes sent a probe request")
	}
}

// TestDeclaredIsNotVerified — the cache's rows are the ONLY source of
// verified, and the absence of a row is the ONLY source of declared. A
// stored row for a capability the route no longer declares must not render,
// and an empty cache shows every flag declared, never silently missing.
func TestDeclaredIsNotVerified(t *testing.T) {
	cache := &fakeProbeCache{rows: map[string][]model.ProbeResult{
		// A row for a capability the route never declared — an old probe of
		// a since-withdrawn claim. It must not appear as anyone's state.
		"claiming": {
			{Capability: model.CapParallelTools, State: model.StateContradicted, Detail: "stale",
				ProbedAt: time.Now(), Model: "ghost"},
			{Capability: model.CapTools, State: model.StateVerified, Detail: "earned",
				ProbedAt: time.Now(), Model: "some-llm"},
		},
	}}
	tbl := probeTable(t, model.Route{
		ID: "claiming", Provider: "openai", Model: "some-llm", MaxOutputTokens: 1024,
		WireCapabilities: []string{"tools", "system-role"}, // no parallel-tools anymore
	}, &probeTransport{}, cache)

	res := listRoute(t, tbl, "claiming")
	seen := map[string]CapabilityStateView{}
	for _, s := range res.CapabilityStates {
		seen[s.Capability] = s
	}
	if len(seen) != 2 {
		t.Fatalf("states rendered: %v — an undeclared capability's stale row leaked onto the view", seen)
	}
	if seen["tools"].State != string(model.StateVerified) {
		t.Errorf("tools: %v, want the earned verified", seen["tools"].State)
	}
	if seen["system-role"].State != string(model.StateDeclared) {
		t.Errorf("system-role: %v — no row must read as declared, never as verified and never as missing", seen["system-role"].State)
	}
}

func listRoute(t *testing.T, tbl *Table, id string) RouteView {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesList, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.(RoutesListResult).Routes {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("route %q missing from the list", id)
	return RouteView{}
}

// TestStorelessDaemonSaysItCannotSave — a probe on a daemon with no cache
// still answers (the operator paid for the request) but cannot pretend the
// verdicts survive.
func TestStorelessDaemonSaysItCannotSave(t *testing.T) {
	tr := &probeTransport{deltas: []model.Delta{{Kind: model.DeltaText, Text: "OK"}},
		results: []model.StreamResult{{Usage: model.Usage{InputTokens: model.IntPtr(1)}}}}
	tbl := probeTable(t, claimingRoute(), tr, nil)
	tbl.Probes = nil

	res, err := callProbe(t, tbl, "claiming")
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved {
		t.Error("a daemon with no cache claims to have saved verdicts")
	}
	if !strings.Contains(strings.ToLower(res.Note), "not stored") {
		t.Errorf("no honest note about the missing cache: %q", res.Note)
	}
}

// TestProbeRefusedWithoutTransport — the daemon that cannot ask says so
// (ErrProbeUnavailable) rather than answer `declared` for everything out of
// thin air.
func TestProbeRefusedWithoutTransport(t *testing.T) {
	tbl := probeTable(t, claimingRoute(), &probeTransport{}, &fakeProbeCache{})
	tbl.Models = nil
	_, err := callProbe(t, tbl, "claiming")
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrProbeUnavailable.Code {
		t.Fatalf("want ErrProbeUnavailable, got %v", err)
	}
}

// TestNothingDeclaredIsZeroRequests — nothing declared: the probe has
// nothing to ask, so it asks NOTHING and says so.
func TestNothingDeclaredIsZeroRequests(t *testing.T) {
	tr := &probeTransport{}
	tbl := probeTable(t, model.Route{ID: "quiet", Provider: "openai", Model: "m", MaxOutputTokens: 1024}, tr, &fakeProbeCache{})
	res, err := callProbe(t, tbl, "quiet")
	if err != nil {
		t.Fatal(err)
	}
	if tr.callCount() != 0 {
		t.Errorf("%d requests to check an empty claim — theatre, paid for by the operator", tr.callCount())
	}
	if len(res.Probes) != 0 || res.Note == "" {
		t.Errorf("no verdicts AND no explanation: %+v", res)
	}
}

// TestUnreadableCacheRefusesTheAsk — fail-closed: a cache that cannot be
// read does not license sending a request a probe may have already proved
// would fail.
func TestUnreadableCacheRefusesTheAsk(t *testing.T) {
	cache := &fakeProbeCache{readErr: errors.New("disk unreadable")}
	tr := &probeTransport{}
	tbl := probeTable(t, claimingRoute(), tr, cache)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAsk, mustJSON(t, AskParams{Route: "claiming", Prompt: "x"}))
	if err == nil {
		t.Fatal("ask ran while the probe cache was unreadable")
	}
	if tr.callCount() != 0 {
		t.Error("the request went out anyway")
	}
	if !strings.Contains(refusalText(t, err), "unreadable") {
		t.Errorf("the refusal does not carry the reason: %s", refusalText(t, err))
	}
}
