package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/stepctx"
)

// ctxSpy records the step identity the DISPATCH stamped into the context —
// the whole of OL3's tool-side provenance promise, observed at the one place
// a tool can see it.
type ctxSpy struct {
	*spyTools
	mu    sync.Mutex
	steps []int64
	oks   []bool
}

func (c *ctxSpy) Call(ctx context.Context, name string, input json.RawMessage) ToolOutcome {
	id, ok := stepctx.StepID(ctx)
	c.mu.Lock()
	c.steps = append(c.steps, id)
	c.oks = append(c.oks, ok)
	c.mu.Unlock()
	return c.spyTools.Call(ctx, name, input)
}

func TestToolCallCarriesTheStepID(t *testing.T) {
	inner := newSpyTools("read")
	inner.delay = 0
	spy := &ctxSpy{spyTools: inner}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	st.Request.StepID = 77 // the server-side stamp (types.go) — what dispatch must forward
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{"path":"a"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if len(spy.steps) != 1 || spy.steps[0] != 77 || !spy.oks[0] {
		t.Fatalf("the fake ToolCaller saw steps %v oks %v, want one call carrying 77", spy.steps, spy.oks)
	}
}

func TestAdHocStepCarriesNoIdentity(t *testing.T) {
	// StepID 0 (a step whose row does not exist yet) must read ABSENT, not as
	// an identity of zero — tools fail closed on absent.
	inner := newSpyTools("read")
	inner.delay = 0
	spy := &ctxSpy{spyTools: inner}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	st.Request.StepID = 0
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t", "read", `{}`)}},
		scriptedTurn{Text: okHandoff},
	)
	if res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}}); res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if len(spy.oks) != 1 || spy.oks[0] {
		t.Fatalf("a step-row-less call reported an identity: %v", spy.oks)
	}
}
