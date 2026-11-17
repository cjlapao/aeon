package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// # `aeon ask`, against a real daemon and a replayed provider
//
// The daemon, the protocol, the adapter, the SSE reader and the cost arithmetic
// are all the real ones. **Only the provider is replaced** — by a local server
// replaying a committed fixture — which is the one part a test must not
// include: no unit test contacts a provider (D15, I7), and no test needs a
// credential.

// fixtureServer replays one fixture on a loopback port.
type fixtureServer struct {
	t    *testing.T
	cmd  *exec.Cmd
	addr string
}

// startFixtureServer builds the replay server once and runs it.
func startFixtureServer(t *testing.T, fixture string, extraArgs ...string) *fixtureServer {
	t.Helper()

	// A free port, so parallel tests do not collide.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	binary := filepath.Join(t.TempDir(), "fixture-server")
	fixtureArgs := []string{"build", "-o", binary, "./scripts/fixture-server"}
	if os.Getenv("AEON_NO_BUILDVCS") == "1" { // rehearsal worktrees: see cli_integration_test.go
		fixtureArgs = []string{"build", "-buildvcs=false", "-o", binary, "./scripts/fixture-server"}
	}
	build := exec.Command("go", fixtureArgs...)
	build.Dir = repoRoot(t)
	build.Env = append(os.Environ(), "GOPRIVATE=github.com/cjlapao/*")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture server: %v\n%s", err, out)
	}

	args := append([]string{
		"-fixture", filepath.Join(repoRoot(t), "internal/model/testdata", fixture),
		"-addr", addr,
		"-delay", "5ms",
	}, extraArgs...)

	cmd := exec.Command(binary, args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait for it to accept, rather than sleeping a guess.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			conn.Close()
			return &fixtureServer{t: t, cmd: cmd, addr: addr}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the fixture server never accepted on %s", addr)
	return nil
}

func (f *fixtureServer) baseURL() string { return "http://" + f.addr + "/v1" }

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// cmd/aeon → aeon
	return filepath.Dir(filepath.Dir(wd))
}

// startDaemonWithRoute starts a daemon whose anthropic route points at the
// replay server. The key is a placeholder: the replay server never reads one,
// which is the point.
func startDaemonWithRoute(t *testing.T, fixture string, serverArgs ...string) (*daemonFixture, *fixtureServer) {
	t.Helper()
	server := startFixtureServer(t, fixture, serverArgs...)
	d := startDaemonWithEnv(t,
		"AEON_ANTHROPIC_BASE_URL="+server.baseURL(),
		"ANTHROPIC_API_KEY=unused-by-the-replay-server",
	)
	return d, server
}

func TestAskPrintsTextIncrementallyNotInOneBlock(t *testing.T) {
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse")

	// Run `ask` with a pipe, and record when each write arrives. The fixture
	// server pauses between frames, so a buffering client produces ONE write
	// and a streaming one produces several.
	cmd := exec.Command(aeonBinary, "ask", "--route", "anthropic", "hello")
	cmd.Env = d.env()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	type write struct {
		at   time.Duration
		text string
	}
	start := time.Now()
	var writes []write
	buf := make([]byte, 4096)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			writes = append(writes, write{at: time.Since(start), text: string(buf[:n])})
		}
		if err != nil {
			break
		}
	}

	var all strings.Builder
	for _, w := range writes {
		all.WriteString(w.text)
	}
	if !strings.Contains(all.String(), "Hello, world") {
		t.Fatalf("the reply did not arrive:\n%s", all.String())
	}

	// **Asserted on arrival, not on content.** The fixture's three text deltas
	// are separated by the server's pause, so a client that buffered would
	// deliver them in one write.
	var textWrites int
	for _, w := range writes {
		if strings.Contains(w.text, "Hello") || strings.Contains(w.text, "world") {
			textWrites++
		}
	}
	if textWrites < 2 {
		t.Fatalf("the reply arrived in %d write(s); the fixture streams it in three frames, so "+
			"this client buffered.\nwrites: %+v", textWrites, writes)
	}
}

// M23 — the client subscribes BEFORE calling ask, which is what makes a missed
// early delta impossible. Asserted on the wire order, from the daemon's view.
func TestAskSubscribesBeforeCallingAsk(t *testing.T) {
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse")

	// A watcher sees `subscription.created` for the ask client's subscription
	// before any ask event exists.
	watch := exec.Command(aeonBinary, "watch")
	watch.Env = d.env()
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watch.Process.Kill(); _, _ = watch.Process.Wait() })

	reader := bufio.NewReader(stdout)
	if _, err := reader.ReadString('\n'); err != nil { // the subscription line
		t.Fatal(err)
	}

	if out, err := d.run("ask", "--route", "anthropic", "hello"); err != nil {
		t.Fatalf("ask failed: %v\n%s", err, out)
	}

	// Read until the first ask event, recording what came before it.
	var kinds []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kind := fields[1]
		kinds = append(kinds, kind)
		if strings.HasPrefix(kind, "ask.") {
			break
		}
	}

	var sawSubscription bool
	for _, k := range kinds {
		if k == "subscription.created" {
			sawSubscription = true
		}
		if strings.HasPrefix(k, "ask.") {
			if !sawSubscription {
				t.Fatalf("an ask event arrived before any subscription was created: %v.\n"+
					"The client must subscribe FIRST, or a fast model's first delta has "+
					"nowhere to go", kinds)
			}
			return
		}
	}
	t.Fatalf("no ask event was observed; saw %v", kinds)
}

func TestAskRouteSelectsTheProvider(t *testing.T) {
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse")

	out, err := d.run("ask", "--route", "anthropic", "hello")
	if err != nil {
		t.Fatalf("ask with a configured route failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Hello, world") {
		t.Errorf("no reply:\n%s", out)
	}

	// An unconfigured route names what IS configured.
	out, err = d.run("ask", "--route", "openai", "hello")
	if err == nil {
		t.Fatalf("ask succeeded for a route with no credential:\n%s", out)
	}
	if !strings.Contains(out, "anthropic") {
		t.Errorf("the refusal does not list the configured route:\n%s", out)
	}
}

func TestTheCostLineRendersKnownPartialAndUnknownDistinctly(t *testing.T) {
	// Three costs, rendered by the real function. A table rather than three
	// daemons, because the rendering is what is under test.
	known := costLine(model.Cost{State: model.CostKnown, USD: 0.00105})
	partial := costLine(model.Cost{
		State: model.CostPartial, USDAtLeast: 0.0004,
		Missing: []string{"cacheWriteTokens", "outputTokens"},
	})
	unknown := costLine(model.Cost{
		State: model.CostUnknown, Reason: model.ReasonProviderOmittedUsage,
	})

	if known == partial || partial == unknown || known == unknown {
		t.Fatalf("two cost states render identically:\n  known   %q\n  partial %q\n  unknown %q",
			known, partial, unknown)
	}
	// S18 T7 evolved this expectation: 0.00105 used to print "$0.00105" —
	// digits that begin "$0.00", the look of free, for a real charge.
	// MoneyString renders sub-cent figures in scientific notation instead
	// (D9: no rendering may read as $0.00, and a skimming eye does not read
	// the digits after the first two zeros).
	if !strings.Contains(known, "1.05e-03") {
		t.Errorf("known does not show the figure: %q", known)
	}
}

// **An unknown cost must never read as free.**
func TestUnknownCostPrintsItsReasonAndNeverZeroOrABareDash(t *testing.T) {
	for _, reason := range []string{
		model.ReasonProviderOmittedUsage,
		model.ReasonNoPriceForRoute,
		model.ReasonStreamAbortedEarly,
	} {
		line := costLine(model.Cost{State: model.CostUnknown, Reason: reason})

		if strings.Contains(line, "$0.00") {
			t.Errorf("an unknown cost rendered as $0.00: %q.\nThat is the exact failure the "+
				"three states exist to prevent — it reaches the UI looking like a cheap step", line)
		}
		if strings.Contains(line, "not reported") == false {
			t.Errorf("an unknown cost does not say so: %q", line)
		}
		// A bare dash says nothing about why.
		if strings.TrimSpace(strings.TrimPrefix(line, "cost")) == "—" {
			t.Errorf("an unknown cost rendered as a bare dash: %q", line)
		}
		// The reason is explained rather than printed as a code.
		if strings.Contains(line, reason) && reason != "" {
			t.Errorf("the raw reason code is printed rather than a sentence: %q", line)
		}
	}
}

func TestPartialCostPrintsAtLeastAndNamesTheMissingFields(t *testing.T) {
	line := costLine(model.Cost{
		State: model.CostPartial, USDAtLeast: 0.0004,
		Missing: []string{"cacheWriteTokens", "outputTokens"},
	})
	if !strings.Contains(line, "at least") {
		t.Errorf("a partial cost does not say `at least`: %q.\nA floor presented as a total is "+
			"a number somebody will add up", line)
	}
	for _, field := range []string{"cacheWriteTokens", "outputTokens"} {
		if !strings.Contains(line, field) {
			t.Errorf("the missing field %q is not named: %q", field, line)
		}
	}
}

// A state this binary does not know must not be rendered as one of the three.
func TestAnUnrecognisedCostStateSaysSoRatherThanGuessing(t *testing.T) {
	line := costLine(model.Cost{State: "from-the-future", USD: 5})
	if strings.Contains(line, "$5") {
		t.Errorf("an unknown state was rendered as a figure: %q", line)
	}
	if !strings.Contains(line, "older than the daemon") {
		t.Errorf("an unknown state does not explain itself: %q", line)
	}
}

func TestAskShowTranscriptPrintsCanonicalJSON(t *testing.T) {
	d, _ := startDaemonWithRoute(t, "anthropic/signed-thinking.sse")

	out, err := d.run("ask", "--route", "anthropic", "--show-transcript", "weather?")
	if err != nil {
		t.Fatalf("ask failed: %v\n%s", err, out)
	}

	idx := strings.Index(out, "transcript")
	if idx < 0 {
		t.Fatalf("no transcript was printed:\n%s", out)
	}
	// Find the JSON array and parse it, so this asserts canonical JSON rather
	// than the presence of a word.
	start := strings.Index(out[idx:], "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end < 0 {
		t.Fatalf("the transcript is not a JSON array:\n%s", out[idx:])
	}
	var msgs []model.Message
	if err := json.Unmarshal([]byte(out[idx+start:end+1]), &msgs); err != nil {
		t.Fatalf("the transcript is not valid canonical JSON: %v\n%s", err, out[idx+start:end+1])
	}
	if len(msgs) < 2 {
		t.Fatalf("the transcript has %d messages", len(msgs))
	}

	var sawThinking bool
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockThinking {
				sawThinking = true
				if b.Text == "" {
					t.Error("the thinking block has no text")
				}
			}
		}
	}
	if !sawThinking {
		t.Error("the transcript has no thinking block, though the fixture streams one")
	}
	// **And it says the signature is absent**, rather than printing a block
	// that looks replayable and would be rejected.
	if !strings.Contains(out, "NO signature") {
		t.Error("the transcript does not say the thinking block carries no signature, so a " +
			"reader would assume it could be replayed")
	}
}

func TestDebugRoundtripReportsIdenticalAndTheSignatureLength(t *testing.T) {
	d := startDaemon(t)

	// A transcript with a signature whose exact length is known. The turn
	// carries the producing route's provenance, because that is the T5
	// precondition for the block to go out at all: the rule keys on the
	// producer, and a stampless turn is nobody's to replay.
	const signature = "ErUBCkYIBBgCKkB3+Zm3/Lx1TESTsignature==/abc+def=="
	own := model.Provenance{Route: "roundtrip", Provider: "anthropic", Model: "roundtrip-model"}
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather?")}},
		{Role: model.RoleAssistant, Provenance: &own, Content: []model.Block{
			model.ThinkingBlock("I should call the tool.", signature),
			model.TextBlock("Checking."),
		}},
	}
	raw, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := d.run("debug", "roundtrip", "--file", path)
	if err != nil {
		t.Fatalf("debug roundtrip failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "IDENTICAL") {
		t.Fatalf("the round trip did not report identical:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("%d bytes", len(signature))) {
		t.Errorf("the signature's length (%d) is not reported:\n%s", len(signature), out)
	}
	if !strings.Contains(out, "validation  ok") {
		t.Errorf("the transcript was not validated:\n%s", out)
	}
}

// A thinking block whose producer is not the target is WITHHELD, said so —
// S18 T5/M27: the difference between the fidelity breaking and the rule
// firing must be readable from this one screen, or every cross-provider
// transcript reads as a bug.
func TestDebugRoundtripReportsTheWithheldBlock(t *testing.T) {
	d := startDaemon(t)

	foreign := model.Provenance{Route: "gpt", Provider: "openai", Model: "gpt-4o"}
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather?")}},
		{Role: model.RoleAssistant, Provenance: &foreign, Content: []model.Block{
			model.ThinkingBlock("I should call the tool.", "ErUBCkYIBBgCKkB3-foreign=="),
			model.TextBlock("Checking."),
		}},
	}
	raw, _ := json.MarshalIndent(msgs, "", "  ")
	path := filepath.Join(t.TempDir(), "foreign.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// Anthropic does not replay what OpenAI produced.
	out, err := d.run("debug", "roundtrip", "--file", path)
	if err != nil {
		t.Fatalf("a withheld block is the rule firing, not a failed round trip: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WITHHELD") {
		t.Fatalf("no WITHHELD line:\n%s", out)
	}
	if !strings.Contains(out, "1 of 1 signature(s) stayed in the record") {
		t.Errorf("the withheld count is not reported:\n%s", out)
	}
	// The answer text still went out — only the thinking block stayed.
	if strings.Contains(out, "CHANGED") {
		t.Errorf("a rule-fire was reported as corruption:\n%s", out)
	}

	// And the other direction: OpenAI carries nobody's thinking at all,
	// whoever produced it — there is no field to put a signature in.
	out, err = d.run("debug", "roundtrip", "--file", path, "--provider", "openai")
	if err != nil {
		t.Fatalf("openai withhold: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WITHHELD") {
		t.Errorf("openai round trip reported no withhold:\n%s", out)
	}
}

// A transcript with no signature says so rather than reporting success.
func TestDebugRoundtripSaysWhenThereIsNoSignatureToTest(t *testing.T) {
	d := startDaemon(t)

	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("hi")}},
	}
	raw, _ := json.Marshal(msgs)
	path := filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := d.run("debug", "roundtrip", "--file", path)
	if err != nil {
		t.Fatalf("debug roundtrip failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no signature to test") {
		t.Errorf("a transcript with no signature reported success without saying so:\n%s", out)
	}
}

func TestDebugRoundtripRefusesAFileThatIsNotATranscript(t *testing.T) {
	d := startDaemon(t)
	path := filepath.Join(t.TempDir(), "junk.json")
	if err := os.WriteFile(path, []byte(`{"hello":"world"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := d.run("debug", "roundtrip", "--file", path)
	if err == nil {
		t.Fatalf("accepted a file that is not a transcript:\n%s", out)
	}
	if !strings.Contains(out, "neither a message array nor a stored transcript") {
		t.Errorf("the refusal does not say what was wrong:\n%s", out)
	}
}

func TestAskRequiresARouteAndAPrompt(t *testing.T) {
	d := startDaemon(t)

	out, err := d.run("ask", "hello")
	if err == nil {
		t.Fatalf("ask ran with no --route:\n%s", out)
	}
	if !strings.Contains(out, "--route is required") {
		t.Errorf("the refusal does not say what is missing:\n%s", out)
	}

	out, err = d.run("ask", "--route", "anthropic")
	if err == nil {
		t.Fatalf("ask ran with no prompt:\n%s", out)
	}
	if !strings.Contains(out, "needs a prompt") {
		t.Errorf("the refusal does not say what is missing:\n%s", out)
	}
}

// A provider failure renders in three parts, with the provider's own words.
func TestAskRendersAProviderFailureInThreeParts(t *testing.T) {
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse", "-status", "404")

	out, err := d.run("ask", "--route", "anthropic", "hello")
	if err == nil {
		t.Fatalf("ask succeeded against a 404:\n%s", out)
	}
	for _, want := range []string{"what", "means", "todo", "-31102"} {
		if !strings.Contains(out, want) {
			t.Errorf("the failure is missing %q:\n%s", want, out)
		}
	}
	// The provider's own words survive, because they are the actionable part.
	if !strings.Contains(out, "404") {
		t.Errorf("the provider's status was discarded:\n%s", out)
	}
	// And a 404 is not retried, which the message says.
	if !strings.Contains(out, "not retried") {
		t.Errorf("the failure does not say whether it was retried:\n%s", out)
	}
}

// The retry path, end to end: a 503 with Retry-After, then the real stream.
// Asserted through the daemon's log, since the retry is invisible to the CLI.
func TestAskRetriesARetryableFailureBeforeAnyOutput(t *testing.T) {
	// The replay server answers 503 for every request, so this asserts the
	// attempt count rather than an eventual success.
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse", "-status", "503", "-retry-after", "0")

	out, _ := d.run("ask", "--route", "anthropic", "hello")
	if !strings.Contains(out, "gave up after") {
		t.Fatalf("a 503 was not retried, or the failure does not say how many attempts were "+
			"made:\n%s", out)
	}
}
