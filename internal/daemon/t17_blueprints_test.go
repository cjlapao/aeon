package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestTheFourBlueprintsAnswerCleanOutsideIn — S13 T17's outside-in proof.
// T12–T16 shipped five bodies from the inside (guards, registry, flip);
// this walks the four blueprints that named them through the daemon's own
// Analyze call path with the real registries — handler.Names(),
// handler.Resolvers(), action.Names(), exactly what loop.Start passes.
// Every one must answer ZERO invalid and ZERO unverifiable and pass
// standard mode. The per-blueprint sibling tests (t12/t13/t14/t15/t16)
// pin each flip at the moment it happened; this pins the settled state
// whole — a regression in any one name, or a futureAgents entry silently
// reappearing, lands here first.
func TestTheFourBlueprintsAnswerCleanOutsideIn(t *testing.T) {
	for _, name := range []string{
		"development-map-direct", "conversational-map",
		"proposal-brief", "development-map-pipeline",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", name+".yaml"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report := blueprint.Analyze(raw, blueprint.ValidationOptions{
			Mode:      "standard",
			Agents:    handler.Names(),
			Resolvers: handler.Resolvers(),
			Actions:   action.Names(),
		})
		valid, invalid, unverifiable := report.Counts()
		if invalid != 0 || unverifiable != 0 {
			for _, d := range report.Diagnostics {
				t.Errorf("%s: %s (%s) at %s", name, d.Msg, d.State, d.Path)
			}
			t.Fatalf("%s: counts valid=%d invalid=%d unverifiable=%d, want 0 and 0 on the refusals", name, valid, invalid, unverifiable)
		}
		if !report.OK(false) {
			t.Fatalf("%s: standard mode refused: %+v", name, report.Diagnostics)
		}
	}
}
