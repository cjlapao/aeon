package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T5 — the drop count's road to the record (O1). The adapters withhold
// and count, the client collects, and the loop accumulates: a step that
// crossed providers carries the number out in its StepResult so the daemon
// can file it beside the cost. These tests pin the runtime's link in that
// chain.

func TestThinkingDropsAccumulateAcrossTheStep(t *testing.T) {
	// Two model calls, each reporting its own drops: three withheld before
	// the tool turn's request, one before the handoff's. The step's number
	// is the total — partial accounting of a different kind from M15's, and
	// lost the same way if nobody accumulates it.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"ls"}`)},
			ThinkingDropped: 3},
		scriptedTurn{Text: okHandoff, ThinkingDropped: 1},
	)
	res := RunStep(context.Background(), testRequest(tool()), runOpts(client, newSpyTools("bash"), nil))
	if res.Kind != ResultComplete {
		t.Fatalf("the step ended %s (%s), want a completion to read the count from", res.Kind, res.Reason)
	}
	if res.ThinkingDropped != 4 {
		t.Errorf("StepResult.ThinkingDropped = %d, want 4 — the per-call counts did not reach the step (O1)",
			res.ThinkingDropped)
	}
}

func TestThinkingDropsRideAFailedStepToo(t *testing.T) {
	// A step that FAILS still spent what it spent, and withheld what it
	// withheld (M15's ranking applied to the drop count): the record of a
	// failed step that shows the spend but not the omissions tells half the
	// story of why the model answered oddly.
	client := newFakeClient(
		scriptedTurn{Text: "no handoff here", ThinkingDropped: 2},
		scriptedTurn{Err: errors.New("the provider refused")},
	)
	res := RunStep(context.Background(), testRequest(), runOpts(client, nil, nil))
	if res.Kind != ResultComplete || res.Outcome != OutcomeFailed {
		t.Fatalf("the step ended %s/%s, want a failure to read the count from", res.Kind, res.Outcome)
	}
	if res.ThinkingDropped != 2 {
		t.Errorf("failed step reports %d, want the 2 drops its first call counted", res.ThinkingDropped)
	}
}

func TestACleanStepReportsNoDrops(t *testing.T) {
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	res := RunStep(context.Background(), testRequest(), runOpts(client, nil, nil))
	if res.ThinkingDropped != 0 {
		t.Errorf("a step nobody dropped from reports %d — the absence is the answer (P31)", res.ThinkingDropped)
	}
}

// TestTheHandoffRetryTurnCarriesItsProvenance — the retry path's assistant
// turn is an assistant turn like any other and must arrive stamped
// (M26, T1). An unstamped turn is no one's thinking to replay (T5), which
// would silently withhold a provider's own blocks on the very next call —
// the P21 break, smuggled in through the branch that builds its append by
// hand instead of sharing it.
func TestTheHandoffRetryTurnCarriesItsProvenance(t *testing.T) {
	client := newFakeClient(
		// Not a handoff: the loop re-asks once.
		scriptedTurn{Text: "blah blah"},
		scriptedTurn{Text: okHandoff},
	)
	res := RunStep(context.Background(), testRequest(), runOpts(client, nil, nil))
	if res.Kind != ResultComplete {
		t.Fatalf("the step ended %s (%s), want the retry to complete", res.Kind, res.Reason)
	}

	assistants := 0
	for i := range res.Messages {
		m := &res.Messages[i]
		if m.Role != model.RoleAssistant {
			continue
		}
		assistants++
		if m.Provenance == nil {
			t.Fatalf("an assistant turn carries NO provenance — its thinking would belong to no "+
				"provider, and T5's replay rule would withhold it from the provider that produced it (M27/P21); %+v",
				m.Content)
		}
		if m.Provenance.Provider != testRoute().Provider || m.Provenance.Route != testRoute().ID {
			t.Errorf("assistant turn %d claims %s/%s, want the route it went out on",
				i, m.Provenance.Provider, m.Provenance.Route)
		}
	}
	// Turn 1 (the failed handoff) + the retry's own answer, both assistant.
	// Pinning the count too: an unstamped retry turn could hide by never
	// being appended in the first place.
	if assistants != 2 {
		t.Fatalf("%d assistant turns, want 2 — the retry path appends its own turn (and stamps it)", assistants)
	}
}
