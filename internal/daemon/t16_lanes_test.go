package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestPipelineBlueprintFullyVerifiableOnceComponentManagerShips — the last
// future row in the installation (development-map-pipeline :28,
// project-manager) graduates at T16. This is the fourth shipped blueprint
// crossing fully to checkable — T17's four-blueprint pin stands on this
// test plus its three siblings.
func TestPipelineBlueprintFullyVerifiableOnceComponentManagerShips(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", "development-map-pipeline.yaml"))
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
			t.Fatalf("development-map-pipeline still refuses standard validation: %s (%s) at %s", d.Msg, d.State, d.Path)
		}
	}
	if !report.OK(false) {
		t.Fatalf("standard mode refused: %+v", report.Diagnostics)
	}
}
