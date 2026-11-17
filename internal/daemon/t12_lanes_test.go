package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestTriageLanesVerifiableOnceTaskCoordinatorShips is the outside-in half
// of T12: it calls blueprint.Analyze the way ValidateBlueprint does
// (blueprint.go:395 — real handler registry, real resolvers, real actions)
// and demands that no lane of the two blueprints whose triage S13 named
// still reads `cannot verify`. Red first: while task-coordinator sits in
// futureAgents with no body, its lanes answer the unverifiable diagnostic.
func TestTriageLanesVerifiableOnceTaskCoordinatorShips(t *testing.T) {
	for _, name := range []string{"development-map-direct", "conversational-map"} {
		raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		report := blueprint.Analyze(raw, blueprint.ValidationOptions{
			Mode:      "standard",
			Agents:    handler.Names(),
			Resolvers: handler.Resolvers(),
			Actions:   action.Names(),
		})
		// Zero unverifiable rows, asserted on Path — the agent NAME lives in
		// the path (lanes[triage].step.agent), not the message; an earlier
		// draft matched the message and passed vacuously (a test that tests
		// nothing, caught before it could go green on wrong grounds).
		for _, d := range report.Diagnostics {
			if d.State == blueprint.Unverifiable {
				t.Fatalf("%s: still cannot verify: %s at %s", name, d.Msg, d.Path)
			}
		}
		if !report.OK(false) {
			t.Fatalf("%s: standard mode refused: %+v", name, report.Diagnostics)
		}
	}
}
