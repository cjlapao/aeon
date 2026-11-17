package loop

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// TestFutureHandlerDoorRefusesTheNamedFollowUp — the unavailable-handler
// door survived S13 T16's emptying of futureAgents: with no real name left,
// the door is pinned directly against the diagnostic the validator produces
// for a declared follow-up (blueprint's TestTheThirdStateSurvivesAnEmptyFollowUpList
// pins the generator; this pins the refusal). A blueprint that could run
// nothing durable is never made durable.
func TestFutureHandlerDoorRefusesTheNamedFollowUp(t *testing.T) {
	bp := blueprint.Blueprint{ID: "x", SchemaVersion: 1, Lanes: []blueprint.Lane{{ID: "triage", Step: &blueprint.Step{Kind: blueprint.ModelStep, Agent: "zz-phantom-follow-up"}}}}
	report := blueprint.Report{Diagnostics: []blueprint.Diagnostic{{
		State: blueprint.Unverifiable, Kind: "agent-registry",
		Path: "lanes[triage].step.agent",
		Msg:  "agent body not shipped — S13 named it a follow-up (no body file in the registry)",
	}}}
	refusal, refused := futureHandlerRefusal(bp, report)
	if !refused {
		t.Fatal("the door let a guaranteed-to-block loop through")
	}
	for _, want := range []string{"unavailable-handler", "not shipped", "lanes[triage].step.agent"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("refusal %q must contain %q", refusal.Error(), want)
		}
	}
	// And a clean report passes the door — the door is the exception, not a toll.
	bp2 := blueprint.Blueprint{ID: "y", SchemaVersion: 1, Lanes: []blueprint.Lane{{ID: "build", Step: &blueprint.Step{Kind: blueprint.ModelStep, Agent: "golang-engineer"}}}}
	if _, refused := futureHandlerRefusal(bp2, blueprint.Report{}); refused {
		t.Fatal("a registered agent must walk through the door untouched")
	}
}
