package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T6 — the OpenAI-compatible path: the OpenAI adapter plus flags, not a
// third adapter (M13). The centre of gravity here is `CAPABILITIES.md`: the
// differences are either expressed as flags or recorded as findings about
// M13, and `TestEveryDifferenceIsAFlagOrAFinding` is what stops the claim
// being true by omission (M31).

// compatRoute declares the given wire flags explicitly — [] means the
// operator declared NONE (which gates), and nil means nobody ever said
// (which never gates, I5).
func compatRoute(caps ...string) model.Route {
	r := testRoute(ProviderOpenAI)
	r.WireCapabilities = append([]string{}, caps...)
	return r
}

func compatResponsesRoute(caps ...string) model.Route {
	r := responsesRoute()
	r.WireCapabilities = append([]string{}, caps...)
	return r
}

// TestNoThirdAdapterFile — D10's file-list pin, because a comment claiming
// "no new adapter file" is a comment. The directory holds exactly the three
// wire files and the registry, and the registry still answers to exactly
// two provider ids. A fourth adapter file is this test failing, loudly,
// before a drift can be discovered in production.
func TestNoThirdAdapterFile(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		got = append(got, name)
	}
	sort.Strings(got)
	// openai_audio*.go: the speech endpoints, split out of openai.go to keep each file under 1000 lines (SIZE1). They are
	// the same OpenAI provider id, not a third adapter.
	want := []string{"anthropic.go", "openai.go", "openai_audio.go", "openai_audio_dialects.go", "openai_responses.go", "registry.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the adapters directory holds %v, want exactly %v — M13's claim is that the "+
			"compat path is flags, not a file; a difference needing a file is a FINDING to record "+
			"in CAPABILITIES.md, and then a decision to raise, not an adapter to add quietly (D10, N7)", got, want)
	}

	providers := model.RegisteredProviders()
	sort.Strings(providers)
	if strings.Join(providers, ",") != "anthropic,openai" {
		t.Errorf("the registry answers to %v, want exactly anthropic and openai", providers)
	}
}

// TestEveryDifferenceIsAFlagOrAFinding — M31 made mechanical. Every row of
// CAPABILITIES.md's difference table must say `flag: <name>` naming a real
// capability, or `finding: <id>` naming a section under Findings. An inline
// `if` that nobody documented lives in neither column, and this test is how
// it stops being invisible.
func TestEveryDifferenceIsAFlagOrAFinding(t *testing.T) {
	raw, err := os.ReadFile("CAPABILITIES.md")
	if err != nil {
		t.Fatalf("CAPABILITIES.md is missing — M31 requires the record to exist as the work happens: %v", err)
	}
	text := string(raw)

	knownFlags := map[string]bool{}
	for _, c := range []model.Capability{
		model.CapTools, model.CapStreamingUsage, model.CapThinking,
		model.CapSystemRole, model.CapParallelTools,
	} {
		knownFlags[string(c)] = true
	}

	rows := 0
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.Contains(line, "flag:") && !strings.Contains(line, "finding:") {
			continue
		}
		if strings.Contains(line, "Expressed as") {
			continue // the header
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			t.Errorf("line %d: a | row without cells: %q", i+1, line)
			continue
		}
		via := cells[2] // the "Expressed as" column
		rows++
		switch {
		case strings.Contains(via, "flag:"):
			name := strings.Trim(strings.SplitN(strings.TrimSpace(strings.SplitAfter(via, "flag:")[1]), " ", 2)[0], ".,;")
			if !knownFlags[name] {
				t.Errorf("row %q names flag %q, which is not one of the five (capability.go): M13's "+
					"flags are a closed set — a sixth is a design decision, not a row", cells[1], name)
			}
		case strings.Contains(via, "finding:"):
			id := strings.Trim(strings.SplitN(strings.TrimSpace(strings.SplitAfter(via, "finding:")[1]), " ", 2)[0], ".,;")
			if !strings.Contains(text, "### finding: "+id) {
				t.Errorf("row %q cites finding %q, but no such section exists under Findings", cells[1], id)
			}
		}
	}
	if rows == 0 {
		t.Fatal("the difference table has no flagged rows at all — the record is vacuous (M31)")
	}
	if !strings.Contains(text, "## Findings about M13") {
		t.Error("the Findings section is missing; its presence is what stops the question being skipped, even when the answer is none")
	}
}

// TestEmptyToolCallIdHandled — Q11, answered **handled**: an absent id is
// synthesised and the synthesis is recorded in the id itself; a duplicate is
// suffixed rather than merged. The reassembler keys by index (S3's M8) so
// calls were never merged in flight — what this pins is that the transcript
// never carries two calls under one address, and never carries a blank.
func TestEmptyToolCallIdHandled(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "empty-tool-call-ids"))

	var ids, cmds []string
	for _, d := range deltas {
		if d.Kind == model.DeltaToolUse && d.Block != nil {
			ids = append(ids, d.Block.ID)
			var args struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(d.Block.Input, &args)
			cmds = append(cmds, args.Command)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("%d tool calls yielded (%v), want all 3 — a duplicate id must not merge two calls (D13)", len(ids), ids)
	}
	if ids[0] == "" {
		t.Error("an empty provider id reached the transcript blank; synthesis is the answer (Q11)")
	}
	if !strings.HasPrefix(ids[0], "aeon-synthesized-") {
		t.Errorf("call 1 id %q, want the aeon-synthesized- prefix that RECORDS the synthesis", ids[0])
	}
	if ids[1] != "call_dup" {
		t.Errorf("the first of a duplicated id pair was renamed to %q; a provider-issued id stays as issued", ids[1])
	}
	if ids[2] != "call_dup-aeon-2" {
		t.Errorf("the duplicate was not suffixed: %q — two calls under one address merge on the way back", ids[2])
	}
	// The calls stay three distinct pieces of work.
	if strings.Join(cmds, ",") != "ls,pwd,date" {
		t.Errorf("arguments crossed between the calls: %v", cmds)
	}

	// The responses wire applies the same rule (one provider, both formats).
	r := newOpenAIResponses()
	rd := drainDeltas(r, framesFromFixture(t, "openai", "empty-tool-call-ids-responses"))
	var rids []string
	for _, d := range rd {
		if d.Kind == model.DeltaToolUse && d.Block != nil {
			rids = append(rids, d.Block.ID)
		}
	}
	if len(rids) != 3 || rids[0] != "aeon-synthesized-0" || rids[1] != "call_dup" || rids[2] != "call_dup-aeon-2" {
		t.Errorf("responses call ids %v, want two distinct synthesised ids", rids)
	}
}

// TestNoToolsKeyWhenAbsent — the key is ABSENT, not empty, exactly as S3
// pinned the handoff retry. An empty array is a different request, and some
// compat servers say so with a 400.
func TestNoToolsKeyWhenAbsent(t *testing.T) {
	tools := []model.ToolSpec{{Name: "bash", Parameters: json.RawMessage(`{"type":"object"}`)}}

	// Declared without tools.
	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Route: compatRoute("streaming-usage"), Tools: tools,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}})
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)
	if _, present := body["tools"]; present {
		t.Fatal(`a tools key (of any shape) was sent on a route declaring no tools — the absence must be the key's absence, not "tools": [] (M13)`)
	}

	// Declared with tools: the key is there.
	a2 := newOpenAI()
	req2, err := a2.ToWire(model.ModelRequest{Route: compatRoute("tools", "streaming-usage"), Tools: tools,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req2)["tools"]; !present {
		t.Error(`a route declaring tools got no tools key — the gate closed on a declaration that said open`)
	}

	// Undeclared: nobody said anything, so the S3 behaviour stands (I5).
	a3 := newOpenAI()
	req3, err := a3.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI), Tools: tools,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req3)["tools"]; !present {
		t.Error("an undeclared route lost the tools key — absence of a declaration is not a declaration of absence (I5)")
	}

	// The responses wire gates identically.
	r := newOpenAIResponses()
	rreq, err := r.ToWire(model.ModelRequest{Route: compatResponsesRoute("streaming-usage"), Tools: tools,
		Messages:        []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}},
		MaxOutputTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, rreq)["tools"]; present {
		t.Error("the responses wire sent a tools key on a declared-tools-less route")
	}
}

// TestUsageFromFinalChunk — measured on Ollama 0.18.0, not assumed: usage
// arrives on a final chunk carrying an EMPTY choices array when asked for,
// and the adapter reads it; asked differently, it never arrives and cost
// falls to unknown (M9) rather than to zero (T7's quarrel).
func TestUsageFromFinalChunk(t *testing.T) {
	// The recorded capture: the usage chunk has `"choices":[]`.
	a := newOpenAI()
	drainDeltas(a, framesFromFixture(t, "openai", "ollama-text-reasoning-usage"))
	usage, _, _ := a.Finalise()
	if usage.InputTokens == nil || *usage.InputTokens != 25 {
		t.Errorf("prompt tokens %v, want the 25 Ollama charged (recorded fixture)", usage.InputTokens)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != 1484 {
		t.Errorf("completion tokens %v, want the 1484 the reasoning model actually spent", usage.OutputTokens)
	}

	// The same server, stream_options not requested: no usage anywhere.
	// Every pointer stays nil — the third state, not a zero (M9).
	b := newOpenAI()
	drainDeltas(b, framesFromFixture(t, "openai", "ollama-usage-absent"))
	usage2, _, _ := b.Finalise()
	if usage2.InputTokens != nil || usage2.OutputTokens != nil {
		t.Errorf("usage invented from a stream that carried none: %+v", usage2)
	}

	// The flag's request-side half: a declared-absent route does not ask —
	// the KEY is absent, since some servers 400 on the unknown key itself.
	route := compatRoute("system-role")
	c := newOpenAI()
	req, err := c.ToWire(model.ModelRequest{Route: route,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req)["stream_options"]; present {
		t.Fatal("stream_options was sent on a route declaring no streaming usage")
	}
	// Undeclared still asks, exactly as at S3 (I5).
	d := newOpenAI()
	req2, err := d.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI),
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req2)["stream_options"]; !present {
		t.Error("an undeclared route lost the usage request — every cost would go unknown on servers that answer it")
	}
}

// TestReasoningDeltaIsRead — the delta-reasoning finding: Ollama's field
// yields thinking deltas so a thinking model is not thirty silent seconds
// (measured: 1,484 reasoning tokens before two words of content). Reading
// it is safe precisely because T5 guarantees none of it comes back.
func TestReasoningDeltaIsRead(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "ollama-text-reasoning-usage"))
	var thinking, text []string
	for _, d := range deltas {
		switch d.Kind {
		case model.DeltaThinking:
			thinking = append(thinking, d.Text)
		case model.DeltaText:
			text = append(text, d.Text)
		}
	}
	if len(thinking) == 0 {
		t.Fatal("delta.reasoning was ignored — a reasoning model streamed with nothing to show (the finding's whole point)")
	}
	if strings.Join(text, "") != "Hello there" {
		t.Errorf("content deltas %q, want the answer separated from the reasoning", strings.Join(text, ""))
	}
}

// TestSystemFoldedWhenNoSystemRole — the fold is a wire translation: the
// canonical transcript keeps its system turn (M1), and the wire carries its
// text inside the first user message with no system role anywhere. Both
// formats, one flag.
func TestSystemFoldedWhenNoSystemRole(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("BE TERSE.")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
	}
	before, _ := json.Marshal(msgs)

	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Route: compatRoute("streaming-usage", "tools"), Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(wireBody(t, req)["messages"])
	if strings.Contains(string(body), `"system"`) {
		t.Error("a system role reached the wire on a route declaring no system-role")
	}
	if !strings.Contains(string(body), "BE TERSE.") {
		t.Error("the system text vanished instead of folding")
	}
	if strings.Contains(string(body), "BE TERSE.") == false {
		t.Fatal("the fold lost the instruction")
	}
	// It folded into the FIRST USER message: that message's content holds
	// both the instruction and the ask.
	var wireMsgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(body, &wireMsgs)
	if len(wireMsgs) != 1 || wireMsgs[0].Role != "user" ||
		!strings.Contains(wireMsgs[0].Content, "BE TERSE.") || !strings.Contains(wireMsgs[0].Content, "go") {
		t.Errorf("fold shape wrong: %+v, want one user message carrying both", wireMsgs)
	}

	after, _ := json.Marshal(msgs)
	if string(before) != string(after) {
		t.Errorf("the fold MUTATED the canonical transcript: %s", after)
	}

	// Undeclared: the system role goes out as-is, the S3 shape unchanged.
	c := newOpenAI()
	req2, err := c.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI), Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if b2, _ := json.Marshal(wireBody(t, req2)["messages"]); !strings.Contains(string(b2), `"system"`) {
		t.Error("an undeclared route folded the system role — I5 again: nobody said the server refuses it")
	}

	// The responses wire: same flag, folded into the first user item.
	r := newOpenAIResponses()
	rreq, err := r.ToWire(model.ModelRequest{Route: compatResponsesRoute("tools", "streaming-usage"),
		Messages: msgs, MaxOutputTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	rbody, _ := json.Marshal(wireBody(t, rreq)["input"])
	if strings.Contains(string(rbody), `"role":"system"`) {
		t.Error("the responses wire sent a system role on a declaring route")
	}
	if !strings.Contains(string(rbody), "BE TERSE.") {
		t.Error("the responses fold lost the instruction")
	}
}

// TestCapabilitiesRecordNamesServersAndVersions — O7: "works with
// OpenAI-compatible servers" is unfalsifiable; "Ollama 0.18.0 with
// qwen3.5:latest, measured 2026-10-26" is checkable. The record names
// servers, versions and models, or it names nothing.
func TestCapabilitiesRecordNamesServersAndVersions(t *testing.T) {
	raw, err := os.ReadFile("CAPABILITIES.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"Ollama", "0.18.0", "qwen3.5", "2026-10-26"} {
		if !strings.Contains(text, want) {
			t.Errorf("the record never names %q — tested-against lines are named from the run that happened (O7)", want)
		}
	}
	for _, generic := range []string{"various servers", "etc.", "and similar"} {
		if strings.Contains(text, generic) {
			t.Errorf("the record contains the unfalsifiable %q (O7)", generic)
		}
	}
	if !strings.Contains(text, "No second server") {
		t.Error("the second-server line is missing — the gap must be NAMED, not left for a reader to assume filled")
	}
}

// TestAgainstRecordedFixturesOnly — I7, on the strength of what the manifest
// says and what this package can reach. The Ollama captures are labelled
// recorded with a date; the Q11 shapes are labelled hand-written and say
// what they are not. And no test here can reach a server at all:
// TestToWirePerformsNoIO installs a panicking transport as the package's
// standing guard, which no compat test escaped by adding a call.
func TestAgainstRecordedFixturesOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Fixtures []struct {
			Path       string `json:"path"`
			Origin     string `json:"origin"`
			CapturedAt string `json:"capturedAt"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	recorded, handwritten := 0, 0
	for _, f := range manifest.Fixtures {
		if !strings.Contains(filepath.Base(f.Path), "ollama") && !strings.Contains(f.Path, "empty-tool-call-ids") {
			continue
		}
		switch f.Origin {
		case "recorded":
			if f.CapturedAt == "" {
				t.Errorf("%s claims recorded with no date (fixtures_test enforces, but this test is about T6's own claim)", f.Path)
			}
			recorded++
		case "hand-written":
			handwritten++
		default:
			t.Errorf("%s has origin %q (O4)", f.Path, f.Origin)
		}
	}
	if recorded < 3 || handwritten < 1 {
		t.Fatalf("T6's evidence is not all accounted for: %d recorded, %d hand-written, want at least 3 and 1", recorded, handwritten)
	}

	// The standing I7 guard for this whole package, asserted from here so T6
	// cannot have quietly needed a server: the ToWire tests run with a
	// transport that PANICS on use.
	src, err := os.ReadFile("registry_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "http.DefaultTransport = panicTransport") {
		t.Error("the panicking transport guard is gone — I7 was structural, and must stay so")
	}
}

// TestReasoningContentDeltaIsRead — the same thinking under the name DeepSeek, vLLM and most Qwen servers use
// (`delta.reasoning_content`): without it those models run silent until the first word of the answer.
func TestReasoningContentDeltaIsRead(t *testing.T) {
	a := newOpenAI()
	deltas := drainDeltas(a, framesFromFixture(t, "openai", "vllm-text-reasoning-content"))
	var thinking, text []string
	for _, d := range deltas {
		switch d.Kind {
		case model.DeltaThinking:
			thinking = append(thinking, d.Text)
		case model.DeltaText:
			text = append(text, d.Text)
		}
	}
	if len(thinking) == 0 {
		t.Fatal("delta.reasoning_content was ignored — the model's thinking never reaches the stream")
	}
	if strings.Join(text, "") != "\n\nHello!" {
		t.Errorf("content deltas %q, want the answer separated from the reasoning", strings.Join(text, ""))
	}
}

// A gateway that reports the call's cost itself (`usage.cost`) gives an exact, priceless cost: the route needs no
// price and the cost is known, where it used to read "not reported".
func TestProviderReportedCostIsKnownWithoutAPrice(t *testing.T) {
	a := newOpenAI()
	drainDeltas(a, framesFromFixture(t, "openai", "vllm-text-reasoning-content"))
	usage, _, _ := a.Finalise()
	if usage.ReportedCostUSD == nil || *usage.ReportedCostUSD != 0.000927 {
		t.Fatalf("usage.cost was not read: %+v", usage)
	}
	cost := model.ComputeCost(usage, nil)
	if cost.State != model.CostKnown || cost.USD != 0.000927 {
		t.Errorf("a route with no price must still report the provider's cost: %+v", cost)
	}
	if unpriced := model.ComputeCost(model.Usage{InputTokens: model.IntPtr(5)}, nil); unpriced.State != model.CostUnknown {
		t.Errorf("no reported cost and no price stays unknown: %+v", unpriced)
	}
}
