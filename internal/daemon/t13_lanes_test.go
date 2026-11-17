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

// TestProposalLaneVerifiableOnceProposalAuthorShips — the proposal-brief
// blueprint's proposal lane (:22, agent: proposal-author, produces:
// proposal.md) is the row this flip graduates. The brief lane is ASSERTED
// still unverifiable on purpose: the flip is exactly one step, and a test
// that went green on a half-flip could not tell T13 from T14. T14 replaces
// this file's pair with the whole-blueprint zero assertion (and T17 pins
// all four blueprints together).
func TestProposalLaneVerifiableOnceProposalAuthorShips(t *testing.T) {
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
	var proposalRows, briefRows int
	for _, d := range report.Diagnostics {
		if d.State != blueprint.Unverifiable {
			continue
		}
		switch {
		case strings.HasPrefix(d.Path, "lanes[proposal]"):
			proposalRows++
			t.Errorf("proposal lane still cannot verify: %s at %s", d.Msg, d.Path)
		case strings.HasPrefix(d.Path, "lanes[brief]"):
			briefRows++
		}
	}
	if !report.OK(false) {
		t.Fatalf("standard mode refused: %+v", report.Diagnostics)
	}
	// Measured flip at T14 (2026-10-26): brief-author shipped, so the brief
	// lane's rows are gone too — this guard once asserted exactly 1 to prove
	// the half-flip could not masquerade as the full one; it did its job and
	// now asserts the other side of the same fact. t14_lanes_test.go owns
	// the whole-blueprint zero assertion.
	if briefRows != 0 {
		t.Fatalf("brief-lane unverifiable rows = %d, want 0 (T14 shipped brief-author)", briefRows)
	}
	if proposalRows != 0 {
		t.Fatalf("proposalRows = %d", proposalRows)
	}
}
