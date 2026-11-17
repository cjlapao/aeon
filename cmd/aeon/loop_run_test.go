package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLoopShowJSONKeepsDaemonGateAndWorkspaceFields(t *testing.T) {
	raw := []byte(`{"id":"loop-1","root":"/tmp/work","status":"waiting","questionId":2,"gateAnswers":["approve","revise","abort"],"gatePrompt":"Approve?"}`)
	var detail loopDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Root != "/tmp/work" || !reflect.DeepEqual(detail.GateAnswers, []string{"approve", "revise", "abort"}) {
		t.Fatalf("loop show discarded fields from the daemon: root=%q answers=%v", detail.Root, detail.GateAnswers)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	if err := json.Unmarshal(encoded, &shown); err != nil {
		t.Fatal(err)
	}
	if shown["root"] != "/tmp/work" || len(shown["gateAnswers"].([]any)) != 3 || shown["gatePrompt"] != "Approve?" {
		t.Fatalf("loop show --json lost the gate snapshot: %s", encoded)
	}
}

// TestLoopShowRendersTheLedger (S18 T7, D16, P34) — the table the task
// exists to produce, from the daemon's figures. Two renderers (this and the
// app panel), one computation (model.AttributeCosts): the CLI decodes what
// `loop.show` answered and prints it, formatting and never summing. The
// spec's own shape — anthropic / openai / local — is the fixture.
func TestLoopShowRendersTheLedger(t *testing.T) {
	raw := []byte(`{"id":"l1","status":"running","costAttribution":{
		"groups":[
			{"routeId":"claude-route","provider":"anthropic","model":"claude-opus-4-1","steps":2,"cost":{"state":"known","usd":0.0412}},
			{"routeId":"gpt-route","provider":"openai","model":"gpt-4.1-mini","steps":1,"cost":{"state":"known","usd":0.018}},
			{"routeId":"local","provider":"ollama","model":"qwen3.5:latest","steps":1,"cost":{"state":"unknown","reason":"no-price-for-route"}},
			{"steps":1,"cost":{"state":"unknown","reason":"provider-omitted-usage"}}],
		"total":{"state":"unknown","usdAtLeast":0.0592,"reason":"no-price-for-route"}}}`)
	var detail loopDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("loop.show's ledger does not decode into the CLI: %v — the app and the CLI would read different ledgers", err)
	}
	if detail.CostAttribution == nil || len(detail.CostAttribution.Groups) != 4 {
		t.Fatalf("the daemon's ledger was discarded at the CLI boundary: %+v", detail.CostAttribution)
	}
	var buf bytes.Buffer
	writeCostLedger(&buf, detail.CostAttribution)
	out := buf.String()
	for _, want := range []string{
		"cost by route:", "anthropic", "openai", "ollama",
		"$0.04120", "at least", "0.0592", "route 'local' is unmetered",
		"(unattributed)", "total",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the ledger table misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$0.00") {
		t.Errorf("the ledger printed the forbidden $0.00 (D9):\n%s", out)
	}
	if !strings.Contains(out, "(2 steps)") || !strings.Contains(out, "(1 step)") {
		t.Errorf("step counts wrong:\n%s", out)
	}
	// The unattributed line reads as absence, not as a priced route.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "(unattributed)") && strings.Contains(line, "route '") {
			t.Errorf("the unattributed line invented a route name: %q", line)
		}
	}
}
