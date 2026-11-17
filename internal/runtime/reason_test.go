package runtime

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S17 T9 walk finding F-δ — the reject's REASON. The walk's §2/§4 check
// is "the redo's input contains the rejection's reason"; the engine
// refuses a reason-less redo at the door ("a bare retry re-runs the call
// that just failed and spends the round reproducing the same output",
// L32). So a handoff that reports reject WITH a reason and loses the
// reason at the runtime boundary makes every live reject undeletable:
// the first real reject blocks the loop with an empty reason. The
// engine-side tests inject ModelResult directly and could never see it.

func TestRejectHandoffCarriesItsReasonToTheResult(t *testing.T) {
	spy := newSpyTools("read")
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{Text: "```json\n{\"status\":\"reject\",\"reason\":\"the json output omits the flag field that the spec requires\"}\n```"}),
		Tools:  spy,
	})
	if res.Outcome != OutcomeReject {
		t.Fatalf("outcome %q, want reject", res.Outcome)
	}
	if res.Reason != "the json output omits the flag field that the spec requires" {
		t.Fatalf("the handoff's reason must be the step result's reason — a reject that arrives reasonless blocks the loop at the redo door (F-δ): got %q", res.Reason)
	}
}

func TestOKHandoffReasonTravelsTooAndAbsenceStaysEmpty(t *testing.T) {
	spy := newSpyTools("read")
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{Text: "```json\n{\"status\":\"ok\",\"reason\":\"two criteria proven, one unverified but non-blocking\"}\n```"}),
		Tools:  spy,
	})
	if res.Reason != "two criteria proven, one unverified but non-blocking" {
		t.Fatalf("ok handoffs carry reasons too (the backlog consumes them): got %q", res.Reason)
	}

	spy2 := newSpyTools("read")
	st2 := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res2 := RunLoop(context.Background(), st2, LoopDeps{
		Client: newFakeClient(scriptedTurn{Text: "```json\n{\"status\":\"ok\"}\n```"}),
		Tools:  spy2,
	})
	if res2.Reason != "" {
		t.Fatalf("no reason in the handoff means empty, not a fabricated sentence: got %q", res2.Reason)
	}
}
