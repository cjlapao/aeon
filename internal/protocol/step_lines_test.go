package protocol

// The payload mapping for S31 T5 (D8): the write line counts reach the WIRE,
// spelled `path` / `linesAdded` / `linesRemoved`, and stay ABSENT for every
// tool that is not a write — an event stream that fills zeros reads as
// "this call changed nothing" when the truth is "this call is not about a
// file".

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

func TestStepEventPayloadCarriesTheWriteCountsOnTheWire(t *testing.T) {
	p := stepEventPayload(runtime.StepEvent{
		Kind: runtime.EventToolEnd, Tool: "write", ToolUseID: "w1", Iteration: 3,
		Path: "notes/plan.md", LinesAdded: 4, LinesRemoved: 1,
	}, 12, "default")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"stepId":12`, `"toolUseId":"w1"`, `"path":"notes/plan.md"`,
		`"linesAdded":4`, `"linesRemoved":1`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the wire lacks %s: %s", want, raw)
		}
	}
}

func TestNonWriteToolEndStaysQuietAboutCounts(t *testing.T) {
	p := stepEventPayload(runtime.StepEvent{
		Kind: runtime.EventToolEnd, Tool: "read", ToolUseID: "r1",
	}, 12, "default")
	raw, _ := json.Marshal(p)
	for _, absent := range []string{`"path"`, `"linesAdded"`, `"linesRemoved"`} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("a read published %s — zeros would read as a claim about a file: %s", absent, raw)
		}
	}
}

func TestStepEventPayloadNamesTheDefaultAgent(t *testing.T) {
	// T5/D1's clause, enforced in the one helper: an event built with no
	// agent reads `default` — the screen never invents a label, and the
	// stream never carries an empty identity that matches nobody.
	p := stepEventPayload(runtime.StepEvent{Kind: runtime.EventStepStarted}, 7, "")
	if p.Agent != "default" {
		t.Fatalf("agent = %q, want default", p.Agent)
	}
	p = stepEventPayload(runtime.StepEvent{Kind: runtime.EventStepStarted}, 7, "read-only")
	if p.Agent != "read-only" {
		t.Fatalf("a named agent was overwritten: %q", p.Agent)
	}
}
