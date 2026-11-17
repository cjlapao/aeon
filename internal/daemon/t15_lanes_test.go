package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestFeatureLaneVerifiableOnceProductManagerShips — development-map-pipeline
// carries the last two future rows: feature-issue (:23, product-manager,
// produces feature.md) and task-breakdown (:28, project-manager, produces
// tasks/). T15 graduates the first; the second is ASSERTED still unverifiable
// — the half-flip discipline from t13, same reason: a test green on a
// half-flip cannot tell T15 from T16.
func TestFeatureLaneVerifiableOnceProductManagerShips(t *testing.T) {
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
	featureRows, breakdownRows := 0, 0
	for _, d := range report.Diagnostics {
		if d.State != blueprint.Unverifiable {
			continue
		}
		switch {
		case strings.HasPrefix(d.Path, "lanes[feature-issue]"):
			featureRows++
			t.Errorf("feature lane still cannot verify: %s at %s", d.Msg, d.Path)
		case strings.HasPrefix(d.Path, "lanes[task-breakdown]"):
			breakdownRows++
		}
	}
	if !report.OK(false) {
		t.Fatalf("standard mode refused: %+v", report.Diagnostics)
	}
	// Measured flip at T16 (2026-10-26): the rows are gone — this guard once
	// asserted exactly 1 so T15 could not pass as T16; it did its job and now
	// asserts the other side of the same fact. t16_lanes_test.go owns the
	// whole-blueprint zero assertion.
	if breakdownRows != 0 {
		t.Fatalf("task-breakdown unverifiable rows = %d, want 0 (T16 shipped project-manager)", breakdownRows)
	}
}
