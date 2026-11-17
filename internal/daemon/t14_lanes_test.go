package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestProposalBriefBlueprintFullyVerifiableOnceBriefAuthorShips — T14
// graduates the last two future rows of proposal-brief.yaml (brief :40 and
// self-check :47, both brief-author). With T13's proposal lane already
// valid, the WHOLE blueprint must answer zero unverifiable and zero
// invalid: this is the first shipped blueprint to cross from
// "cannot verify" to fully checkable, which is the F-α half-closure
// disclosed at T17 (authoring lanes are real now; the verbatim-constant
// gap GAP-6 is recorded in the body and the drop record, not papered).
func TestProposalBriefBlueprintFullyVerifiableOnceBriefAuthorShips(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", "proposal-brief.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	report := blueprint.Analyze(raw, blueprint.ValidationOptions{
		Mode:      "standard",
		Agents:    handler.Names(),
		Resolvers: handler.Resolvers(),
		Actions:   action.Names(),
	})
	for _, d := range report.Diagnostics {
		if d.State == blueprint.Unverifiable || d.State == blueprint.Invalid {
			t.Fatalf("proposal-brief still refuses standard validation: %s (%s) at %s", d.Msg, d.State, d.Path)
		}
	}
	if !report.OK(false) {
		t.Fatalf("standard mode refused: %+v", report.Diagnostics)
	}
}
