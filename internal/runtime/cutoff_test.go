package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// A turn the output cap ended (a model that thought for the whole cap and said nothing) is not the model finishing:
// the step is told what happened and given more room, and carries on, instead of being asked for a report on work
// that never happened.
func TestACutOffTurnContinuesInsteadOfFinishing(t *testing.T) {
	client := newFakeClient(
		scriptedTurn{Thinking: "thinking and thinking", StopReason: "length"},
		scriptedTurn{Text: okHandoff},
	)
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}
	if client.requestCount() != 2 {
		t.Fatalf("%d requests, want 2: the cut-off turn and the continuation", client.requestCount())
	}
	second := client.request(1)
	if len(second.Tools) == 0 {
		t.Error("the continuation must still offer the tools: the work was not done")
	}
	if !strings.Contains(lastUserText(second.Messages), "cut off by the output limit") {
		t.Errorf("the model was not told its reply was cut off: %q", lastUserText(second.Messages))
	}
	if second.MaxOutputTokens <= client.request(0).MaxOutputTokens {
		t.Errorf("the continuation got no more room: %d then %d", client.request(0).MaxOutputTokens, second.MaxOutputTokens)
	}
}

// Cut-offs do not go on for ever: after three in a row the ordinary ending takes over.
func TestCutOffsAreBounded(t *testing.T) {
	cut := scriptedTurn{Thinking: "still thinking", StopReason: "length"}
	client := newFakeClient(cut, cut, cut, cut, scriptedTurn{Text: okHandoff}, scriptedTurn{Text: okHandoff})
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	// Three continuations, then the fourth cut-off falls through to the handoff re-ask.
	if got := client.requestCount(); got < 5 || got > 6 {
		t.Fatalf("%d requests: three continuations then the ordinary ending expected", got)
	}
}
