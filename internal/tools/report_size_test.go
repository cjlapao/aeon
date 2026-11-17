package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestReportSizeToolKnownShape pins D9's promise at the tool itself: ONE
// shape, three named fields, required, closed — and a successful report
// reads back as the range WITH its basis, not a bare number.
func TestReportSizeToolKnownShape(t *testing.T) {
	tool := ReportSize()
	if tool.Spec.Name != "report_size" {
		t.Fatalf("the tool is named %q — the contract names report_size", tool.Spec.Name)
	}
	if tool.Class != RerunDetectable {
		t.Errorf("class = %q, want detectable (a re-run after a reverted sizing is a new step; the closure's refusal is the one per run)", tool.Class)
	}
	var schema struct {
		Type       string                    `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                  `json:"required"`
		AdditionalProperties *bool           `json:"additionalProperties"`
	}
	if err := json.Unmarshal(tool.Spec.Parameters, &schema); err != nil {
		t.Fatalf("the parameters schema is not JSON: %v", err)
	}
	for _, want := range []string{"sizeMin", "sizeMax", "sizeBasis"} {
		if _, ok := schema.Properties[want]; !ok {
			t.Errorf("the schema is missing %s — the shape is {sizeMin, sizeMax, sizeBasis} (D8/D9)", want)
		}
	}
	if len(schema.Required) != 3 {
		t.Errorf("required = %v, want all three fields required — a partial report is not the known shape", schema.Required)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Error("additionalProperties must be false — extras are how a shape drifts")
	}
	res := tool.Run(context.Background(), ToolContext{}, json.RawMessage(
		`{"sizeMin":3,"sizeMax":8,"sizeBasis":"read three files, counted the call sites"}`))
	if res.IsError {
		t.Fatalf("a clean report must be accepted, not refused: %s", res.Content)
	}
	if !strings.Contains(res.Content, "3-8") || !strings.Contains(res.Content, "read three files") {
		t.Errorf("the content drops the range or the basis: %q", res.Content)
	}
}

// TestReportSizeCalledTwiceRefused is D9 at the tool: the SECOND call in
// the same run is refused — and the FIRST report stands, unrevised. A
// fresh toolset (a new run, e.g. after a failed sizing reverted the item
// to pending) gets a fresh refusal, not a permanent mute.
func TestReportSizeCalledTwiceRefused(t *testing.T) {
	tool := ReportSize()
	first := tool.Run(context.Background(), ToolContext{}, json.RawMessage(
		`{"sizeMin":2,"sizeMax":5,"sizeBasis":"grep found two call sites"}`))
	if first.IsError {
		t.Fatalf("the first report must land: %s", first.Content)
	}
	second := tool.Run(context.Background(), ToolContext{}, json.RawMessage(
		`{"sizeMin":40,"sizeMax":60,"sizeBasis":"changed my mind"}`))
	if !second.IsError {
		t.Fatal("the second report was ACCEPTED — a queue sized by whoever speaks last is not sized (D9)")
	}
	if !strings.Contains(second.Content, "already") {
		t.Errorf("the refusal should say the report already landed: %q", second.Content)
	}
	third := ReportSize().Run(context.Background(), ToolContext{}, json.RawMessage(
		`{"sizeMin":2,"sizeMax":5,"sizeBasis":"new run"}`))
	if third.IsError {
		t.Errorf("a NEW run is a fresh closure — the refusal must not stick across runs: %s", third.Content)
	}
}

func TestReportSizeRefusesShapelessReports(t *testing.T) {
	cases := []struct{ in, why string }{
		{`{"sizeMin":5,"sizeMax":5,"sizeBasis":"   "}`, "the empty basis is D8's bare number"},
		{`{"sizeMin":8,"sizeMax":3,"sizeBasis":"inverted"}`, "the inverted range"},
		{`{"sizeMin":0,"sizeMax":4,"sizeBasis":"zero-floor"}`, "a range including nothing"},
		{`{"sizeMin":3}`, "the partial report — all three or nothing"},
	}
	for _, c := range cases {
		res := ReportSize().Run(context.Background(), ToolContext{}, json.RawMessage(c.in))
		if !res.IsError {
			t.Errorf("%s: accepted (%q)", c.why, res.Content)
		} else if !strings.Contains(res.Content, "D8") {
			t.Errorf("%s: the refusal should name the law it is keeping: %q", c.why, res.Content)
		}
	}
}
