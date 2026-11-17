package model

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// S18 T3: the probe, against fakes. No test here reaches a live provider
// (I7) — every verdict is made from recorded deltas, results and errors,
// which is also what makes the definitive-400 path testable at all.

type fakeTransport struct {
	calls int
	last  ModelRequest

	deltas  []Delta
	results []StreamResult
	err     error
}

func (f *fakeTransport) Stream(_ context.Context, req ModelRequest) (<-chan Delta, <-chan StreamResult, <-chan error) {
	f.calls++
	f.last = req
	d := make(chan Delta, len(f.deltas))
	for _, x := range f.deltas {
		d <- x
	}
	close(d)
	r := make(chan StreamResult, len(f.results))
	for _, x := range f.results {
		r <- x
	}
	close(r)
	e := make(chan error, 1)
	if f.err != nil {
		e <- f.err
	}
	close(e)
	return d, r, e
}

func allFive() Route {
	r := Route{ID: "claiming", Provider: "openai", Model: "some-llm", MaxOutputTokens: 4096}
	r.WireCapabilities = []string{"tools", "streaming-usage", "thinking", "system-role", "parallel-tools"}
	return r
}

func textDelta(s string) Delta { return Delta{Kind: DeltaText, Text: s} }
func thinkDelta() Delta        { return Delta{Kind: DeltaThinking, Text: "mulling"} }
func toolDelta() Delta {
	return Delta{Kind: DeltaToolUse, Block: &Block{Kind: BlockToolUse, Name: "probe_say_ok", Input: []byte(`{}`)}}
}
func usageResult() StreamResult {
	return StreamResult{Usage: Usage{InputTokens: IntPtr(7), OutputTokens: IntPtr(2)}}
}
func bareResult() StreamResult { return StreamResult{} }

func probeOf(t *testing.T, r Route, tr *fakeTransport) ([]ProbeResult, error) {
	t.Helper()
	fixed := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	return Probe(context.Background(), r, tr, func() time.Time { return fixed })
}

func stateOf(t *testing.T, results []ProbeResult, c Capability) ProbeResult {
	t.Helper()
	for _, p := range results {
		if p.Capability == c {
			return p
		}
	}
	t.Fatalf("no verdict for %q — the probe must judge every DECLARED capability (it judged %v)", c, statesList(results))
	return ProbeResult{}
}

func statesList(rs []ProbeResult) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, string(r.Capability)+"="+string(r.State))
	}
	return out
}

// TestProbeAgainstFixtures — every capability's verdict, from recorded
// responses. The whole Q9 table lives here.
func TestProbeAgainstFixtures(t *testing.T) {
	t.Run("clean stream, nothing comes back: usage contradicted, acceptance verified", func(t *testing.T) {
		tr := &fakeTransport{deltas: []Delta{textDelta("OK")}, results: []StreamResult{bareResult()}}
		rs, err := probeOf(t, allFive(), tr)
		if err != nil {
			t.Fatal(err)
		}
		if got := stateOf(t, rs, CapTools); got.State != StateVerified {
			t.Errorf("tools: %v — the server ACCEPTED the tool declaration, which is what the flag means", got.State)
		}
		if got := stateOf(t, rs, CapStreamingUsage); got.State != StateContradicted {
			t.Errorf("streaming-usage: %v — usage was asked for and did not arrive: the measured absence", got.State)
		}
		if got := stateOf(t, rs, CapThinking); got.State != StateDeclared {
			t.Errorf("thinking: %v — a terse model proves nothing", got.State)
		}
		if got := stateOf(t, rs, CapSystemRole); got.State != StateDeclared {
			t.Errorf("system-role: %v — the unprobable one stays declared", got.State)
		}
		if got := stateOf(t, rs, CapParallelTools); got.State != StateDeclared {
			t.Errorf("parallel-tools: %v", got.State)
		}
	})

	t.Run("usage arrives, thinking block arrives, two tool calls", func(t *testing.T) {
		tr := &fakeTransport{
			deltas:  []Delta{thinkDelta(), toolDelta(), toolDelta()},
			results: []StreamResult{usageResult()},
		}
		rs, err := probeOf(t, allFive(), tr)
		if err != nil {
			t.Fatal(err)
		}
		for c, want := range map[Capability]CapabilityState{
			CapStreamingUsage: StateVerified,
			CapThinking:       StateVerified,
			CapParallelTools:  StateVerified,
			CapTools:          StateVerified,
		} {
			if got := stateOf(t, rs, c); got.State != want {
				t.Errorf("%s: %v, want %v", c, got.State, want)
			}
		}
	})

	t.Run("a 400 naming tools contradicts tools and implicates nothing else", func(t *testing.T) {
		tr := &fakeTransport{err: &StatusError{Provider: "openai", Status: 400, Body: `{"error":"Function tools with reasoning_effort are not supported"}`}}
		rs, err := probeOf(t, allFive(), tr)
		if err != nil {
			t.Fatalf("a naming-400 is EVIDENCE, not a failed probe: %v", err)
		}
		if got := stateOf(t, rs, CapTools); got.State != StateContradicted || !strings.Contains(got.Detail, "Function tools") {
			t.Errorf("tools: %v %q — the provider's own refusal must be quoted", got.State, got.Detail)
		}
		if got := stateOf(t, rs, CapStreamingUsage); got.State != StateDeclared {
			t.Errorf("streaming-usage: %v — the stream never ran; contradiction by association is exactly the invented badge", got.State)
		}
	})

	t.Run("a 400 about something else fails the probe rather than verdicts", func(t *testing.T) {
		tr := &fakeTransport{err: &StatusError{Provider: "openai", Status: 400, Body: `unknown model 'some-llem'`}}
		rs, err := probeOf(t, allFive(), tr)
		if err == nil {
			t.Fatalf("a 400 about the model name is a failed probe, not a capability verdict: %v", statesList(rs))
		}
		if rs != nil {
			t.Error("a failed probe must return no verdicts at all")
		}
	})
}

// TestProbeFailureIsNotAContradiction — the distinction that would be
// missed: "could not check" and "checked and wrong" are different facts.
func TestProbeFailureIsNotAContradiction(t *testing.T) {
	for name, err := range map[string]error{
		"unreachable":     errors.New("dial tcp: connection refused"),
		"unauthorized":    &StatusError{Provider: "openai", Status: 401, Body: "bad key"},
		"not found":       &StatusError{Provider: "openai", Status: 404, Body: "no such model"},
		"server meltdown": &StatusError{Provider: "openai", Status: 500, Body: "boom"},
	} {
		tr := &fakeTransport{err: err}
		rs, perr := probeOf(t, allFive(), tr)
		if perr == nil {
			t.Errorf("%s: a failed probe returned verdicts", name)
		}
		for _, r := range rs {
			if r.State == StateContradicted {
				t.Errorf("%s: failure recorded as a CONTRADICTION of %s — every later step would refuse for a reason that was never true", name, r.Capability)
			}
		}
	}
}

// TestUnprobableCapabilityStaysDeclared — and SAYS SO. An invented
// `verified` badge is worse than an honest `declared`.
func TestUnprobableCapabilityStaysDeclared(t *testing.T) {
	tr := &fakeTransport{deltas: []Delta{textDelta("OK")}, results: []StreamResult{usageResult()}}
	rs, err := probeOf(t, allFive(), tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []Capability{CapSystemRole, CapParallelTools} {
		got := stateOf(t, rs, c)
		if got.State != StateDeclared {
			t.Errorf("%s: %v — cannot be proven by one minimal request, must not be badge-invented", c, got.State)
		}
		if strings.TrimSpace(got.Detail) == "" {
			t.Errorf("%s: stayed declared without saying why", c)
		}
		if got.definitive() {
			t.Errorf("%s: a declared verdict must not be stored — absence of a row IS declared (I5)", c)
		}
	}
}

// TestProbeIsOneRequestPerRoute — D7. One minimal request for the whole
// declared set; zero when nothing is declared.
func TestProbeIsOneRequestPerRoute(t *testing.T) {
	tr := &fakeTransport{deltas: []Delta{textDelta("OK")}, results: []StreamResult{usageResult()}}
	if _, err := probeOf(t, allFive(), tr); err != nil {
		t.Fatal(err)
	}
	if tr.calls != 1 {
		t.Errorf("%d requests for five declared capabilities, want the ONE request (D7)", tr.calls)
	}
	// The one request asked for what was claimed: a tool, and thinking.
	if len(tr.last.Tools) != 1 {
		t.Error("tools were declared and none was offered — the probe would find nothing to accept")
	}
	if tr.last.ThinkingBudgetTokens == 0 {
		t.Error("thinking was declared and none was asked for")
	}
	if len(tr.last.Messages) == 0 || tr.last.Messages[0].Role != RoleSystem {
		t.Error("the probe must exercise the system position it claims to leave unproven")
	}

	silent := allFive()
	silent.WireCapabilities = nil
	tr2 := &fakeTransport{}
	rs, err := probeOf(t, silent, tr2)
	if err != nil || len(rs) != 0 {
		t.Errorf("nothing declared, got %d verdicts, err %v", len(rs), err)
	}
	if tr2.calls != 0 {
		t.Errorf("an undeclared route cost %d requests; the probe has nothing to ask, so it must ask nothing", tr2.calls)
	}
}

// TestProbeNeverRewritesTheDeclaration — the probe records alongside (M28,
// I5); a probe that edited the route would make the contradiction invisible.
func TestProbeNeverRewritesTheDeclaration(t *testing.T) {
	r := allFive()
	before := strings.Join(append([]string{}, r.WireCapabilities...), ",")
	tr := &fakeTransport{err: &StatusError{Provider: "openai", Status: 400, Body: "tools not supported here"}}
	if _, err := probeOf(t, r, tr); err != nil {
		t.Fatal(err)
	}
	if after := strings.Join(r.WireCapabilities, ","); after != before {
		t.Errorf("the contradicting probe rewrote the declaration: %q -> %q", before, after)
	}
}

// TestProbeResultCarriesTimestampAndModel — a cached verdict without them is
// a stale claim wearing a verified badge; without them, nothing can notice.
func TestProbeResultCarriesTimestampAndModel(t *testing.T) {
	tr := &fakeTransport{deltas: []Delta{textDelta("OK")}, results: []StreamResult{usageResult()}}
	rs, err := probeOf(t, allFive(), tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range rs {
		if got.ProbedAt.IsZero() {
			t.Errorf("%s: verdict without a timestamp cannot be aged", got.Capability)
		}
		if got.Model != "some-llm" {
			t.Errorf("%s: probed model %q, want the route's model — a cached verdict must name WHICH model it spoke for", got.Capability, got.Model)
		}
	}
}

// TestContradictionSentence — the one sentence (O3/D6), verbatim on every
// surface: names the route, the capability, when it was probed, and against
// which model.
func TestContradictionSentence(t *testing.T) {
	s := ContradictionSentence("local-wrong", "tools",
		time.Date(2026, 9, 6, 14, 2, 0, 0, time.UTC), "llama3.2:1b", "no tool_calls field")
	for _, want := range []string{"local-wrong", "tools", "2026-09-06T14:02:00Z", "llama3.2:1b", "no tool_calls field", "refused"} {
		if !strings.Contains(s, want) {
			t.Errorf("the sentence omits %q: %s", want, s)
		}
	}
}

// TestUsageAny — M9's every-field-optional shape has ONE definition of
// "nothing at all arrived", which is the streaming-usage verdict's question.
func TestUsageAny(t *testing.T) {
	if (Usage{}).Any() {
		t.Error("an all-nil Usage reports usage — the contradicted verdict would never fire")
	}
	if !(Usage{ReasoningTokens: IntPtr(1)}).Any() {
		t.Error("a lone reasoning-token count IS usage somebody reported")
	}
}
