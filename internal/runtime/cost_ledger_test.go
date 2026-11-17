package runtime

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// TestUnmeteredStepStillCompletes — M30's other half, and the one a nervous
// implementation gets wrong: `unknown` is an ACCOUNTING state, not a failure.
// A laptop running a local model has a step that succeeded and whose cost is
// genuinely unknown to Aeon; refusing or failing it would make the unmetered
// route unusable, which is exactly the pressure that turns renderings into
// `$0.00` lies (D9). The cost travels honestly and the step completes.
func TestUnmeteredStepStillCompletes(t *testing.T) {
	unmetered := model.Cost{State: model.CostUnknown, Reason: model.ReasonNoPriceForRoute}
	client := newFakeClient(scriptedTurn{Text: okHandoff, Cost: unmetered})
	res := RunStep(context.Background(), testRequest(), runOpts(client, nil, nil))
	if res.Kind != ResultComplete || res.Outcome != OutcomeOK {
		t.Fatalf("a step with an unpriceable cost ended %s/%s (%s) — unknown cost is not failure (M30)",
			res.Kind, res.Outcome, res.Reason)
	}
	if res.Cost.State != model.CostUnknown || res.Cost.Reason != model.ReasonNoPriceForRoute {
		t.Errorf("the unknown cost was laundered into %+v on the way out — M30's reason must reach the record", res.Cost)
	}
	if res.Cost.USD != 0 || res.Cost.USDAtLeast != 0 {
		t.Errorf("the unmetered step acquired a figure (%+v) — unknown means nobody priced it (M9)", res.Cost)
	}
}
