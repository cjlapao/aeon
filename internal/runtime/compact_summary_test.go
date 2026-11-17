package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func compactFixture() *LoopState {
	st := testState(testHandler(), nil)
	st.Messages = append(st.Messages,
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(strings.Repeat("long history ", 200))}},
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("acknowledged")}},
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.ThinkingBlock("last thought", "signed"), model.TextBlock("continuing")}},
	)
	st.Request.Route.ContextWindow = 400
	return st
}

func TestCompactionSummaryUsesInjectedClientAndRecordsItsCost(t *testing.T) {
	st := compactFixture()
	client := newFakeClient(scriptedTurn{Text: "The earlier exchange established the task.",
		Usage: model.Usage{InputTokens: model.IntPtr(80), OutputTokens: model.IntPtr(12)},
		Cost:  model.Cost{State: model.CostKnown, USD: 0.002}}, scriptedTurn{Text: okHandoff,
		Usage: model.Usage{InputTokens: model.IntPtr(25), OutputTokens: model.IntPtr(10)},
		Cost:  model.Cost{State: model.CostKnown, USD: 0.003}})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: newSpyTools(), HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if client.requestCount() != 2 {
		t.Fatalf("calls = %d, want summary + step", client.requestCount())
	}
	if client.request(0).Tools != nil {
		t.Fatal("summary request offered tools")
	}
	if len(res.Compactions) != 1 {
		t.Fatalf("compactions = %+v", res.Compactions)
	}
	record := res.Compactions[0]
	if record.Cost.USD != 0.002 || res.Cost.USD != 0.005 {
		t.Errorf("record cost=%v total=%v", record.Cost, res.Cost)
	}
	if record.Usage.InputTokens == nil || *record.Usage.InputTokens != 80 {
		t.Errorf("summary usage=%+v", record.Usage)
	}
	if res.Messages[record.Start].Role != model.RoleUser || !strings.Contains(res.Messages[record.Start].Content[0].Text, record.Summary) {
		t.Error("summary did not replace the selected range")
	}
	if record.EstimateAfter >= record.EstimateBefore {
		t.Errorf("estimates did not shrink: %+v", record)
	}
	var emitted bool
	for _, event := range res.Events {
		if event.Kind == EventStepCompacted && event.EstimateBefore == record.EstimateBefore && event.EstimateAfter == record.EstimateAfter {
			emitted = true
		}
	}
	if !emitted {
		t.Fatal("step.compacted was not emitted")
	}
}

func TestInadmissibleRangeCostsNoModelCall(t *testing.T) {
	st := testState(testHandler(), nil)
	st.Messages = append(st.Messages, model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("answer")}})
	client := newFakeClient(scriptedTurn{Text: "should not be bought"})
	selected := Compact(context.Background(), st, LoopDeps{Client: client, Sink: DiscardSink{}}, 1)
	if selected.OK || client.requestCount() != 0 {
		t.Fatalf("selection=%+v calls=%d", selected, client.requestCount())
	}
}

func TestOversizedSummaryIsRefusedWithoutTruncating(t *testing.T) {
	st := compactFixture()
	client := newFakeClient(scriptedTurn{Text: strings.Repeat("x", 5000)})
	result := Compact(context.Background(), st, LoopDeps{Client: client, Sink: DiscardSink{}}, 10000)
	if result.OK || !strings.Contains(result.Reason, "exceeded") {
		t.Fatalf("result=%+v", result)
	}
	if len(st.Compactions) != 0 {
		t.Fatal("oversized summary was stored")
	}
}

func TestNoAdmissibleRangeBlocksBeforeAModelCall(t *testing.T) {
	st := testState(testHandler(), nil)
	st.Messages[1].Content = []model.Block{model.TextBlock(strings.Repeat("task", 300))}
	st.Request.Route.ContextWindow = 100
	client := newFakeClient(scriptedTurn{Text: "should never be called"})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: newSpyTools()})
	if res.Outcome != OutcomeBlocked || !strings.Contains(res.Reason, "cannot compact") || client.requestCount() != 0 {
		t.Fatalf("outcome=%s reason=%s calls=%d", res.Outcome, res.Reason, client.requestCount())
	}
}

func TestExhaustedIterationBoundDoesNotBuyACompaction(t *testing.T) {
	st := compactFixture()
	st.Manifest.IterationsUsed = st.Manifest.MaxIterations
	client := newFakeClient(scriptedTurn{Text: "should never be called"})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: newSpyTools()})
	if res.Outcome != OutcomeBlocked || client.requestCount() != 0 {
		t.Fatalf("outcome=%s calls=%d", res.Outcome, client.requestCount())
	}
}
