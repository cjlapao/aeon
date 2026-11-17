package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// T8: a third-party server that hangs must not hang a loop, and a timeout
// must never read as "the tool found nothing".

// lowerCallTimeout pins the test seam; production reads DefaultCallTimeout.
func lowerCallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := callTimeout
	callTimeout = d
	t.Cleanup(func() { callTimeout = old })
}

// TestHangingServerDoesNotHangTheStep is MC9 with a stopwatch: a server
// deliberately stuck for 4s must produce an ANSWER (the timeout rendering)
// in about the bound, not in 4s and not never.
func TestHangingServerDoesNotHangTheStep(t *testing.T) {
	lowerCallTimeout(t, 120*time.Millisecond)
	bin := fakeServerPath(t)
	cfg := classifiedConfig("fakeserver", "hang", "safe", "local", true)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin,
		Args: []string{"-hang", "4s"}}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	if err := r.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}

	start := time.Now()
	res, err := r.Call(context.Background(), "agent-a", "fakeserver.hang", nil,
		allowPolicy("agent-a", "fakeserver.hang"), time.Now())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a bound server failure must be an outcome, not a Go error: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the call took %v with a 120ms bound — the step was hung by somebody else's server", elapsed)
	}
	if !res.TimedOut || !res.IsError || !strings.HasPrefix(res.Content, "timeout:") {
		t.Fatalf("hanging server answered %+v, want the timeout rendering", res)
	}
	if !strings.Contains(res.Content, "fakeserver") || !strings.Contains(res.Content, "fakeserver.hang") {
		t.Errorf("the timeout must name the server and the tool: %q", res.Content)
	}
}

// TestTimeoutIsDistinguishableFromEmptyResultAndFromError is the task's
// bolded line: three outcomes from the SAME fake over the SAME transport,
// each rendering unlike the others, because the failure mode is a model
// reporting "no results" for a hang it never saw the end of.
func TestTimeoutIsDistinguishableFromEmptyResultAndFromError(t *testing.T) {
	lowerCallTimeout(t, 120*time.Millisecond)
	bin := fakeServerPath(t)
	cfg := Config{
		Public: []ServerConfig{{
			Name:      "fakeserver",
			Transport: TransportConfig{Type: TransportStdio, Command: bin, Args: []string{"-hang", "600ms"}},
			Tools: []ToolConfig{
				{Name: "hang", Rerun: "safe", Effect: "local"},
				{Name: "empty", Rerun: "safe", Effect: "local"},
				{Name: "boom", Rerun: "safe", Effect: "local"},
			},
		}},
		Agents: map[string]AgentConfig{"agent-a": {
			Declares: []string{"fakeserver.hang", "fakeserver.empty", "fakeserver.boom"},
		}},
	}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	if err := r.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	ctx := context.Background()

	timeoutRes, err := r.Call(ctx, "agent-a", "fakeserver.hang", nil, allowPolicy("agent-a", "fakeserver.hang"), time.Now())
	if err != nil {
		t.Fatalf("hang: %v", err)
	}
	// The fake is single-threaded (like the client's discipline): the next
	// call queues behind the still-running hang, so wait for the counter
	// query to be answerable — which is the moment the hang is over.
	// (Waiting ON the fake, not around it: if the client had leaked the
	// request, this poll would never clear.)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := c.CallTool(ctx, "calls", json.RawMessage(`{"name":"hang"}`)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake never recovered from the hang — the queued calls were starved, not timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}
	emptyRes, err := r.Call(ctx, "agent-a", "fakeserver.empty", nil, allowPolicy("agent-a", "fakeserver.empty"), time.Now())
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	boomRes, err := r.Call(ctx, "agent-a", "fakeserver.boom", nil, allowPolicy("agent-a", "fakeserver.boom"), time.Now())
	if err != nil {
		t.Fatalf("boom: %v", err)
	}

	// The shapes, pinned apart:
	if !(timeoutRes.TimedOut && timeoutRes.IsError) {
		t.Errorf("timeout flags = %v/%v, want true/true", timeoutRes.TimedOut, timeoutRes.IsError)
	}
	if emptyRes.TimedOut || emptyRes.IsError {
		t.Errorf("empty result flagged as failure: %+v", emptyRes)
	}
	if boomRes.TimedOut || !boomRes.IsError {
		t.Errorf("server-side error flags = %v/%v, want false/true (isError is the server speaking, not the wire dying)", boomRes.TimedOut, boomRes.IsError)
	}
	if timeoutRes.Content == emptyRes.Content || timeoutRes.Content == boomRes.Content || emptyRes.Content == boomRes.Content {
		t.Errorf("two of the three outcomes render identically:\n timeout %q\n empty   %q\n boom    %q", timeoutRes.Content, emptyRes.Content, boomRes.Content)
	}
	// The empty result must READ as an empty result to the model — a clean
	// labelled region, not a refusal:
	if strings.HasPrefix(emptyRes.Content, "unavailable:") || strings.HasPrefix(emptyRes.Content, "timeout:") || strings.HasPrefix(emptyRes.Content, "unknown") {
		t.Errorf("an empty result renders as a refusal: %q", emptyRes.Content)
	}
	if !strings.Contains(emptyRes.Content, "untrusted") {
		t.Errorf("empty result lost its label (T4): %q", emptyRes.Content)
	}
	// The fourth shape: a dead wire is a Go error, told from all three
	// renderings by being an error at all.
	_ = c.Close()
	if _, err := r.Call(ctx, "agent-a", "fakeserver.empty", nil, allowPolicy("agent-a", "fakeserver.empty"), time.Now()); err == nil {
		// After Close the client may answer "not connected" from the
		// attach table only if detached; it is still attached, so either a
		// transport error or a timeout on the closed pipe is the honest
		// answer — what may NOT happen is a clean empty result.
		t.Error("a closed transport answered cleanly — the dead wire became a success")
	}
}

// TestTimeoutIsNotRetriedAutomatically answers N10 with the fake's counter:
// one timeout, ONE invocation. A retry doubles the wait and the model
// cannot tell one hang from two.
func TestTimeoutIsNotRetriedAutomatically(t *testing.T) {
	lowerCallTimeout(t, 120*time.Millisecond)
	bin := fakeServerPath(t)
	cfg := classifiedConfig("fakeserver", "hang", "safe", "local", true)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin, Args: []string{"-hang", "400ms"}}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	if err := r.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	res, err := r.Call(context.Background(), "agent-a", "fakeserver.hang", nil, allowPolicy("agent-a", "fakeserver.hang"), time.Now())
	if err != nil || !res.TimedOut {
		t.Fatalf("want a timeout, got %+v %v", res, err)
	}
	// The fake is single-threaded: the counter answers once the 400ms hang
	// finishes. One attempt, forever.
	deadline := time.Now().Add(3 * time.Second)
	var n CallResult
	for time.Now().Before(deadline) {
		n, err = c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"hang"}`))
		if err == nil && n.Text != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || n.Text != "1" {
		t.Errorf("the hang was invoked %v times (%v), want exactly 1 — an automatic retry makes one hang indistinguishable from two", n, err)
	}
}

// TestCallerBoundShortensNeverLengthens is the bash rule (W13) restated
// here: a caller with its own shorter budget is respected; a caller with
// none gets the ceiling. Lengthening is not expressible.
func TestCallerBoundShortensNeverLengthens(t *testing.T) {
	lowerCallTimeout(t, 2*time.Second)
	bin := fakeServerPath(t)
	cfg := classifiedConfig("fakeserver", "hang", "safe", "local", true)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin, Args: []string{"-hang", "30s"}}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Public[0])
	_ = r.AttachClient("fakeserver", c)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := r.Call(ctx, "agent-a", "fakeserver.hang", nil, allowPolicy("agent-a", "fakeserver.hang"), time.Now())
	if err != nil {
		t.Fatalf("caller-bound call: %v", err)
	}
	if !res.TimedOut || time.Since(start) > time.Second {
		t.Errorf("the caller's 80ms bound was not honoured (elapsed check failed): %+v", res)
	}
	// And no caller-side trick raises the ceiling: a ctx with a 1-hour
	// deadline still gets the package bound (2s here), not an hour.
	long, lcancel := context.WithTimeout(context.Background(), time.Hour)
	defer lcancel()
	start = time.Now()
	res, err = r.Call(long, "agent-a", "fakeserver.hang", nil, allowPolicy("agent-a", "fakeserver.hang"), time.Now())
	if err != nil || !res.TimedOut {
		t.Fatalf("want a bounded timeout outcome, got %+v %v", res, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("a 1-hour caller deadline bought a 1-hour wait — the ceiling is a ceiling (bash's rule): %v", time.Since(start))
	}
}

// TestDeadServerDegradesTheListNotTheStep is scope items 3+4 in one
// fixture: two public servers, one attached and living, one never attached.
// The living server's tools work; the dead one's absence is REPORTED, per
// server, and the step sails on.
func TestDeadServerDegradesTheListNotTheStep(t *testing.T) {
	bin := fakeServerPath(t)
	cfg := Config{
		Public: []ServerConfig{
			{Name: "alpha",
				Transport: TransportConfig{Type: TransportStdio, Command: bin},
				Tools:     []ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}}},
			{Name: "beta",
				Transport: TransportConfig{Type: TransportStdio, Command: bin},
				Tools:     []ToolConfig{{Name: "echo", Rerun: "safe", Effect: "local"}}},
		},
		Agents: map[string]AgentConfig{"agent-a": {Declares: []string{"alpha.search", "beta.echo"}}},
	}
	r := mustRegistry(t, cfg)
	beta := dialFakeAt(t, cfg.Public[1])
	if err := r.AttachClient("beta", beta); err != nil {
		t.Fatalf("AttachClient beta: %v", err)
	}
	// alpha is deliberately never attached: its process is dead, refused,
	// or simply not started — the registry does not care which, and neither
	// should the step.

	down := r.DownServers()
	if len(down) != 1 || down[0] != "alpha" {
		t.Fatalf("DownServers = %v, want [alpha] — degradation is per-server", down)
	}

	// The living server's step proceeds:
	res, err := r.Call(context.Background(), "agent-a", "beta.echo", json.RawMessage(`{"q":"go on"}`),
		allowPolicy("agent-a", "beta.echo"), time.Now())
	if err != nil || res.IsError || res.AskRequired {
		t.Fatalf("one dead server degraded the living one's call: %+v %v", res, err)
	}

	// The dead server's tools are REPORTED absent — the word absent must be
	// in the answer; "unavailable" alone leaves the model guessing between
	// down, unclassified, and ungranted.
	res, err = r.Call(context.Background(), "agent-a", "alpha.search", nil,
		allowPolicy("agent-a", "alpha.search"), time.Now())
	if err != nil {
		t.Fatalf("dead server produced a Go error where an outcome belongs: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(res.Content, "unavailable:") || !strings.Contains(res.Content, "not connected") || !strings.Contains(res.Content, "absent") {
		t.Errorf("dead-server answer = %q, want the reported-absence rendering (N8)", res.Content)
	}

	// The capability view is untouched (config facts); the degradation is
	// a fact about CLIENTS, visible where clients are consulted.
	inCap := 0
	for _, tl := range r.ToolsFor("agent-a") {
		if tl.Qualified == "alpha.search" || tl.Qualified == "beta.echo" {
			inCap++
		}
	}
	if inCap != 2 {
		t.Errorf("a dead server deleted config facts from the capability view (%d of 2) — degradation reports absence, it does not erase declarations", inCap)
	}
}

// TestDownServersIsConfigDerivedNotClientGuessed: a server nobody
// configured can never be "down" (it never existed), and every configured
// name — public or private — is accountable to the question.
func TestDownServersIsConfigDerivedNotClientGuessed(t *testing.T) {
	cfg := privateFixture() // public alpha + reddit-agent private fakeserver
	r := mustRegistry(t, cfg)
	down := r.DownServers()
	want := map[string]bool{"alpha": true, "fakeserver": true}
	if len(down) != len(want) {
		t.Fatalf("DownServers = %v, want exactly %v", down, want)
	}
	for _, n := range down {
		if !want[n] {
			t.Errorf("%q reported down — a name nobody configured must never be mourned", n)
		}
	}
}

// TestBoundCallDiscipline is the small seam asserted directly: no deadline
// arrives → package bound; shorter caller → caller's; longer caller →
// package bound. Three lines, because the WHOLE rule is three lines.
func TestBoundCallDiscipline(t *testing.T) {
	lowerCallTimeout(t, 100*time.Millisecond)
	_, c1, b1 := boundCall(context.Background())
	defer c1()
	if b1 != 100*time.Millisecond {
		t.Errorf("undeadlined ctx got bound %v, want the package bound", b1)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	_, c3, b3 := boundCall(ctx2)
	defer c3()
	if b3 > 10*time.Millisecond+5*time.Millisecond {
		t.Errorf("caller's shorter bound was not honoured: %v", b3)
	}
	ctx4, cancel4 := context.WithTimeout(context.Background(), time.Hour)
	defer cancel4()
	_, c5, b5 := boundCall(ctx4)
	defer c5()
	if b5 > 100*time.Millisecond {
		t.Errorf("a 1-hour caller deadline raised the ceiling to %v — it cannot be lengthened", b5)
	}
	if !isTimeout(context.DeadlineExceeded) || isTimeout(errors.New("other")) {
		t.Error("isTimeout must name exactly the bounded wait giving up")
	}
}
