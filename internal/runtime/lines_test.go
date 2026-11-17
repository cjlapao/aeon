package runtime

// The write line counts crossing the runtime (S31 T5, D8): the sink copy is
// verbatim from the tool's Result — the runtime never re-derives a number —
// and `step.tool.end` now answers to its own `toolUseId` (brief §5), so a
// watcher can pair start, end and transcript result without guessing.

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestStepToolEndCarriesTheWriteLineCountsAndItsCallID(t *testing.T) {
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("w1", "write", `{"path":"a.md","content":"x"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	spy := newSpyTools("write").with("write", ToolOutcome{
		Content: "wrote a.md (1 bytes)", Summary: "write a.md · 1 B",
		Path: "a.md", LinesAdded: 4, LinesRemoved: 1,
	})
	sink := &recordingSink{}
	st := testState(testHandler("write"), []model.ToolSpec{toolSpec("write")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy, Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	var ends []StepEvent
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "write" {
			ends = append(ends, e)
		}
	}
	if len(ends) != 1 {
		t.Fatalf("%d write tool.end events, want 1", len(ends))
	}
	e := ends[0]
	if e.ToolUseID != "w1" || e.Path != "a.md" || e.LinesAdded != 4 || e.LinesRemoved != 1 {
		t.Fatalf("the sink copy is not verbatim from the Result: %+v", e)
	}
}

func TestNonFileToolsLeaveTheCountFieldsEmpty(t *testing.T) {
	// The default spy result has no metadata — a `read` end event must not
	// grow invented numbers.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("r1", "read", `{"path":"a.md"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	for _, e := range sink.all() {
		if e.Kind == EventToolEnd && e.Tool == "read" {
			if e.Path != "" || e.LinesAdded != 0 || e.LinesRemoved != 0 {
				t.Fatalf("a read invented write metadata: %+v", e)
			}
			if e.ToolUseID != "r1" {
				t.Fatalf("tool.end lost the call id: %+v", e)
			}
		}
	}
}
