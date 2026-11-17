package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// T11's tests. The CLI's own behaviour, with no daemon: the grant list, the
// warning, and the three renderers.

func TestTheDefaultGrantIsReadAndBashIsNeverImplicit(t *testing.T) {
	// D1 at the surface a person actually touches.
	granted, err := grantList("", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(granted, ",") != "read,list,write" {
		t.Errorf("the default grant is %v, want read, list, write", granted)
	}

	// An explicit list that does not name bash does not get it either.
	granted, err = grantList("read", false)
	if err != nil {
		t.Fatal(err)
	}
	if contains(granted, "bash") {
		t.Errorf("bash appeared in %v", granted)
	}
}

func TestAllowBashGrantsBashAndPrintsNoApology(t *testing.T) {
	// T19 retired the every-use stderr warning together with the apology it
	// carried: S11 shipped the command policy that text apologized for, so
	// the warning's premise is gone. The GRANT is untouched — `--allow-bash`
	// still appends "bash" — and the silence is asserted, not assumed: not
	// the retired sentence, not a replacement apology, nothing on stderr.
	for attempt := 1; attempt <= 3; attempt++ {
		stderr := captureStderr(t, func() {
			granted, err := grantList("", true)
			if err != nil {
				t.Fatal(err)
			}
			if !contains(granted, "bash") {
				t.Errorf("--allow-bash did not grant bash: %v", granted)
			}
		})
		if stderr != "" {
			t.Fatalf("attempt %d printed on stderr: %q", attempt, stderr)
		}
	}
}

func TestTheExplicitBashGrantAlsoPrintsNothing(t *testing.T) {
	// `--tools read,bash` is the same decision as `--allow-bash`; the
	// retirement covers the GRANT, not one flag's spelling of it.
	stderr := captureStderr(t, func() {
		granted, err := grantList("read,bash", false)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(granted, "bash") {
			t.Errorf("the grant list is %v", granted)
		}
	})
	if stderr != "" {
		t.Errorf("an explicit bash grant printed on stderr: %q", stderr)
	}
}

func TestNoWarningWhenTheShellIsNotGranted(t *testing.T) {
	// The negative control. A warning on every run is a warning nobody reads.
	stderr := captureStderr(t, func() {
		if _, err := grantList("read", false); err != nil {
			t.Fatal(err)
		}
	})
	if stderr != "" {
		t.Errorf("a read-only step warned: %q", stderr)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	// Restored even when fn leaves through t.Fatal (runtime.Goexit): a leaked
	// pipe in os.Stderr poisons every later test that reads it.
	restored := false
	restore := func() {
		if !restored {
			restored = true
			_ = w.Close()
			os.Stderr = old
		}
	}
	t.Cleanup(restore)
	defer restore()
	done := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = b.ReadFrom(r)
		done <- b.String()
	}()

	fn()

	restore()
	return <-done
}

func TestTheGrantListIsParsedTolerantlyAndNeverSilentlyEmpty(t *testing.T) {
	// A trailing comma is a typo, not a request for an empty grant — and an
	// empty grant would be a step with no tools, which would fail in a way
	// that reads as the model's fault.
	granted, err := grantList("read, ,", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(granted) != 1 || granted[0] != "read" {
		t.Errorf("the grant is %v", granted)
	}

	granted, err = grantList("  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(granted) == 0 {
		t.Error("a whitespace list produced no grant at all")
	}
}

func TestAPromptIsRequired(t *testing.T) {
	var out bytes.Buffer
	err := cmdStepRun(&out, []string{"--workspace", t.TempDir()})
	if err == nil {
		t.Fatal("a step with no prompt was accepted")
	}
	if !strings.Contains(err.Error(), "needs a prompt") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
	// And it does not dial: a missing prompt is a usage error, and an error
	// about the daemon being down would send the person to the wrong problem.
	if strings.Contains(err.Error(), "daemon") {
		t.Errorf("the usage error mentions the daemon: %v", err)
	}
}

func TestTheResultNamesWhatToReadNext(t *testing.T) {
	var out bytes.Buffer
	res := protocol.StepResult{
		StepID: 42, Kind: "complete", Outcome: "ok", Iterations: 3,
		Cost:    model.Cost{State: model.CostKnown, USD: 0.0123},
		Handoff: json.RawMessage(`{"status":"ok","summary":"counted 17 files"}`),
	}
	if err := renderStepResult(&out, res, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()

	for _, want := range []string{"step 42", "ok", "counted 17 files", "iterations 3", "$0.01230",
		"aeon step show 42", "aeon step verify 42"} {
		if !strings.Contains(text, want) {
			t.Errorf("the result does not contain %q:\n%s", want, text)
		}
	}
}

func TestAPartialCostIsNotPrintedAsAKnownOne(t *testing.T) {
	// S3's D12, at the last surface. A `partial` rendered as a plain number is
	// a person believing a price nobody measured.
	var out bytes.Buffer
	res := protocol.StepResult{StepID: 1, Outcome: "ok", Cost: model.Cost{
		State: model.CostPartial, USDAtLeast: 0.004, Missing: []string{"cacheWrite"},
	}}
	if err := renderStepResult(&out, res, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "at least") {
		t.Errorf("a partial cost was printed without `at least`:\n%s", text)
	}
	if !strings.Contains(text, "cacheWrite") {
		t.Errorf("the unpriced parts are not named:\n%s", text)
	}
}

func TestOnlyAFailedOutcomeIsANonZeroExit(t *testing.T) {
	// `reject` and `blocked` are not errors: the step worked and the answer
	// was no, or it ran out of room. A script that treats every non-ok as a
	// crash would retry a considered refusal.
	for _, outcome := range runtime.Outcomes() {
		res := protocol.StepResult{StepID: 1, Outcome: string(outcome)}
		err := failedStepError(res)
		if outcome == runtime.OutcomeFailed {
			if err == nil {
				t.Error("a failed step exited zero")
			}
			continue
		}
		if err != nil {
			t.Errorf("a %s step exited non-zero: %v", outcome, err)
		}
	}
}

func TestSinkFailuresAreShownRatherThanHidden(t *testing.T) {
	var out bytes.Buffer
	res := protocol.StepResult{StepID: 1, Outcome: "ok", SinkFailures: 4}
	if err := renderStepResult(&out, res, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "4 could not be delivered") {
		t.Errorf("the undelivered events are not reported:\n%s", out.String())
	}
}

func TestTheManifestViewShowsTheHashesAndSaysWhatTheyMean(t *testing.T) {
	// K13. The hashes are how a person checks the step read the version they
	// thought it did.
	manifest := runtime.ContextManifest{
		Files: []runtime.FileHash{
			{Path: "docs/plan.md", Hash: "sha256:abc123", Bytes: 2048},
		},
		IterationsUsed: 2,
		MaxIterations:  12,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := renderStepManifest(&out, raw); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"docs/plan.md", "sha256:abc123", "2048", "iterations 2 of 12",
		"AT ASSEMBLY"} {
		if !strings.Contains(text, want) {
			t.Errorf("the manifest view does not contain %q:\n%s", want, text)
		}
	}
}

func TestAManifestWithNoDeclaredFilesExplainsItself(t *testing.T) {
	// The ad-hoc handler declares none — the step READS what it needs, which
	// is the point of having a tool loop. An empty list with no explanation
	// reads as a bug.
	raw, err := json.Marshal(runtime.ContextManifest{MaxIterations: 12})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := renderStepManifest(&out, raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No declared files") {
		t.Errorf("an empty manifest is not explained:\n%s", out.String())
	}
}

func TestAnUnparseableManifestIsPrintedRawRatherThanRefused(t *testing.T) {
	// A manifest this build cannot parse is still the one thing a person has
	// to look at.
	var out bytes.Buffer
	if err := renderStepManifest(&out, json.RawMessage(`{"files":"not an array"}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not an array") {
		t.Errorf("the raw manifest was not shown:\n%s", out.String())
	}
}

func TestTheRequestsViewStatesWhatItCannotShow(t *testing.T) {
	// It reconstructs the call boundaries from the transcript, which cannot
	// show which tools each call OFFERED. Saying so is the difference between
	// a derived view and a misleading one.
	transcript := []map[string]any{
		{"role": "system", "content": []map[string]any{{"kind": "text", "text": "be terse"}}},
		{"role": "user", "content": []map[string]any{{"kind": "text", "text": "count them"}}},
		{"role": "assistant", "content": []map[string]any{{"kind": "tool_use", "name": "read", "id": "t1"}}},
		{"role": "user", "content": []map[string]any{{"kind": "tool_result", "toolUseId": "t1", "content": "x"}}},
		{"role": "assistant", "content": []map[string]any{{"kind": "text", "text": "done"}}},
	}
	raw, err := json.Marshal(transcript)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := renderStepRequests(&out, raw); err != nil {
		t.Fatal(err)
	}
	text := out.String()

	if !strings.Contains(text, "call 1") || !strings.Contains(text, "call 2") {
		t.Errorf("the two calls were not reconstructed:\n%s", text)
	}
	if !strings.Contains(text, "asked for read") {
		t.Errorf("the tool call is not named:\n%s", text)
	}
	if !strings.Contains(text, "2 model call(s)") {
		t.Errorf("the count is wrong:\n%s", text)
	}
	if !strings.Contains(text, "cannot show") {
		t.Errorf("the view does not state its limitation:\n%s", text)
	}
}

func TestAStepWithNoTranscriptSaysSo(t *testing.T) {
	var out bytes.Buffer
	if err := renderStepRequests(&out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no stored transcript") {
		t.Errorf("an empty transcript is not explained: %q", out.String())
	}
}

func TestEveryStepEventKindRendersSomething(t *testing.T) {
	// A kind that renders nothing is a kind a person watching will never see,
	// and the stream is the whole reason the sink exists.
	for _, kind := range runtime.EventKinds() {
		payload, err := json.Marshal(protocol.StepEventPayload{
			Tool: "read", Text: "some text", Summary: "a summary",
			Result: "a result", Iteration: 1, Outcome: "ok",
		})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		renderStepEvent(&out, protocol.Event{Kind: kind, Payload: payload})
		if strings.TrimSpace(stripANSI(out.String())) == "" {
			t.Errorf("%s renders nothing", kind)
		}
	}
}

func stripANSI(s string) string {
	for {
		start := strings.Index(s, "\033[")
		if start < 0 {
			return s
		}
		end := strings.IndexByte(s[start:], 'm')
		if end < 0 {
			return s
		}
		s = s[:start] + s[start+end+1:]
	}
}

func TestATruncatedToolResultStaysOnOneLine(t *testing.T) {
	// The live stream is a person's view of a step. A tool result with 400
	// lines of output would bury everything after it.
	payload, err := json.Marshal(protocol.StepEventPayload{
		Tool: "bash", Result: strings.Repeat("x", 500) + "\nand more",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderStepEvent(&out, protocol.Event{Kind: runtime.EventToolEnd, Payload: payload})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Errorf("the event rendered %d lines", len(lines))
	}
	if len(stripANSI(lines[0])) > 120 {
		t.Errorf("the line is %d characters", len(stripANSI(lines[0])))
	}
}

func TestHelpIsAQuestionRatherThanAFailure(t *testing.T) {
	// `--help` printing the flag list and then exiting non-zero with "flag:
	// help requested" makes a verb look broken to somebody reading it for the
	// first time, and to a script that checks exit codes.
	var out bytes.Buffer
	if err := cmdStepRun(&out, []string{"--help"}); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "step \"PROMPT\"") {
		t.Errorf("--help does not show how to run a step:\n%s", text)
	}
}

func TestTheAllowBashFlagDescribesWhatItGivesAway(t *testing.T) {
	// `--help` is where somebody reads about a flag before using it, so the
	// consequence belongs there — and since T19 the retirement makes the flag
	// description the CLI's ONE pre-decision statement of what the shell
	// grant means: S11's command_access policy, both of its live answers
	// (deny, ask), the bounds, and G20's honesty string verbatim.
	var out bytes.Buffer
	_ = cmdStepRun(&out, []string{"-h"})
	// The flag package prints the defaults to the flag set's output, which
	// this captures via the usage block; the description itself is asserted on
	// the registered flag.
	fs := stepFlagSetForTest()
	f := fs.Lookup("allow-bash")
	if f == nil {
		t.Fatal("there is no --allow-bash flag")
	}
	for _, want := range []string{
		"command_access policy (S11)",
		"an unruled command denies",
		"ask suspends for an answer",
		"timeout", "iteration limit",
	} {
		if !strings.Contains(f.Usage, want) {
			t.Errorf("the flag's description does not mention %q: %s", want, f.Usage)
		}
	}
	if !strings.Contains(f.Usage, tools.BashHonesty) {
		t.Errorf("the flag's description does not carry the honesty string verbatim: %s", f.Usage)
	}
	// The retired apology must appear NOWHERE on this surface. The needle is
	// assembled at runtime on purpose: the guard must not be hit by its own
	// literal (the repo's grep-guard lesson).
	retired := "NO command " + "policy"
	if strings.Contains(f.Usage, retired) {
		t.Errorf("the retired apology is back in the flag's description: %s", f.Usage)
	}
}

func TestTheVerbUsageStatesTheS11PolicyAndTheHonestDefaults(t *testing.T) {
	// The `aeon step` usage block (store_verbs.go) says the same truth the
	// flag does — S11's policy sentence, no retired apology — and its
	// `--tools` default names the REAL default grant (read, list, write),
	// not the one-word fiction the carried finding caught.
	var out bytes.Buffer
	printStepUsage(&out)
	// The usage block is hand-wrapped for the terminal; assertions run on
	// whitespace-normalized text so a line break cannot hide a missing clause.
	text := strings.Join(strings.Fields(out.String()), " ")
	for _, want := range []string{"command_access policy (S11)", "unruled command denies", "ask suspends", "default: read, list, write"} {
		if !strings.Contains(text, want) {
			t.Errorf("the usage block does not mention %q:\n%s", want, text)
		}
	}
	retired := "NO command " + "policy"
	if strings.Contains(text, retired) {
		t.Errorf("the retired apology is back in the usage block:\n%s", text)
	}
}

// --- routes set --reasoning-effort -------------------------------------------

// The two flags say opposite things, and the CLI refuses rather than picking.
//
// Silently preferring one would make the other flag a no-op that looks like it
// worked — and the value at stake decides whether a step can run at all.
func TestTheTwoReasoningFlagsCannotBothBeGiven(t *testing.T) {
	var out bytes.Buffer
	err := cmdRoutesSet(&out, []string{
		"--id", "r", "--provider", "openai", "--model", "m",
		"--reasoning-effort", "none", "--clear-reasoning-effort",
	})
	if err == nil {
		t.Fatal("both flags were accepted")
	}
	if !strings.Contains(err.Error(), "opposite things") {
		t.Errorf("the error does not explain the conflict: %v", err)
	}
	// And it did not dial: a usage error must not be reported as the daemon
	// being down.
	if strings.Contains(err.Error(), "daemon") {
		t.Errorf("the usage error mentions the daemon: %v", err)
	}
}

func TestTheReasoningEffortFlagDescribesWhyNoneMatters(t *testing.T) {
	// `--help` is where somebody reads about a flag before using it, and the
	// reason this one exists is that without `none` a step cannot use tools at
	// all. That belongs in the description, not only in the error somebody hits
	// afterwards.
	var out bytes.Buffer
	if err := cmdRoutesSet(&out, []string{"--help"}); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}
	usage := out.String()
	for _, want := range []string{"none|minimal|low|medium|high", "step use tools", "KEEPS"} {
		if !strings.Contains(usage, want) {
			t.Errorf("the flag list does not mention %q:\n%s", want, usage)
		}
	}
}

func TestARouteListingShowsTheReasoningEffortAndSaysNothingWhenUnset(t *testing.T) {
	// Nothing, rather than the word "default": the field's absence means the
	// provider decides, and printing a word for it would suggest Aeon had
	// chosen one.
	if got := effortSuffix(""); got != "" {
		t.Errorf("an unset effort rendered as %q", got)
	}
	if got := effortSuffix("none"); !strings.Contains(got, "none") {
		t.Errorf("a set effort rendered as %q", got)
	}
}

// --- routes set --openai-endpoint --------------------------------------------

func TestTheOpenAIEndpointFlagDescribesWhatItUnlocks(t *testing.T) {
	// `--help` is where somebody reads about a flag before using it, and the
	// reason this one exists is that /v1/chat/completions refuses tools with
	// reasoning on — the one thing `--reasoning-effort none` cannot work around
	// if reasoning is what somebody actually wants.
	var out bytes.Buffer
	if err := cmdRoutesSet(&out, []string{"--help"}); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}
	usage := out.String()
	for _, want := range []string{"chat|responses", "reason WHILE using tools", "KEEPS"} {
		if !strings.Contains(usage, want) {
			t.Errorf("the flag list does not mention %q:\n%s", want, usage)
		}
	}
}

func TestAnEndpointSuffixSaysNothingForTheDefault(t *testing.T) {
	// Nothing for `chat`, for the same reason `effortSuffix` says nothing when
	// unset: a listing that labels every route with its default teaches
	// nobody anything, and the one that is NOT the default is the fact worth
	// seeing.
	if got := endpointSuffix(""); got != "" {
		t.Errorf("an unset endpoint rendered as %q", got)
	}
	if got := endpointSuffix("chat"); got != "" {
		t.Errorf("the default endpoint rendered as %q", got)
	}
	if got := endpointSuffix("responses"); !strings.Contains(got, "responses") {
		t.Errorf("a non-default endpoint rendered as %q", got)
	}
}

// TestRenderStepEventPrintsWriteLineCounts (S31 T5, D8): the terminal says
// what the call cost the file. The every-kind test above proves tool.end
// renders at all; this proves the numbers reach the line when they exist —
// and that a zero-count call stays exactly as quiet as before.
func TestRenderStepEventPrintsWriteLineCounts(t *testing.T) {
	payload := protocol.StepEventPayload{StepID: 12, Tool: "write", ToolUseID: "w1",
		Result: "wrote notes/plan.md (212 bytes)", Path: "notes/plan.md",
		LinesAdded: 4, LinesRemoved: 1}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderStepEvent(&out, protocol.Event{Kind: runtime.EventToolEnd, Payload: raw})
	line := out.String()
	for _, want := range []string{"+4", "−1", "wrote notes/plan.md"} {
		if !strings.Contains(line, want) {
			t.Errorf("the write line lacks %q: %q", want, line)
		}
	}

	payload.LinesAdded, payload.LinesRemoved = 0, 0
	raw, _ = json.Marshal(payload)
	out.Reset()
	renderStepEvent(&out, protocol.Event{Kind: runtime.EventToolEnd, Payload: raw})
	if strings.Contains(out.String(), "+0") || strings.Contains(out.String(), "−0") {
		t.Errorf("a no-count call printed counts anyway: %q", out.String())
	}
}
