package daemon

// S50 T2 (RM3) — the characterization golden for the plan host's move
// from a daemon string constant to a seeded blueprint record. The YAML
// file below is the constant's bytes VERBATIM; this test pins that
// identity so the move cannot smuggle a content change, and pins that
// the seeded row carries the same bytes the release embedded.

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// oldPlanHostBlueprintYAML is the deleted constant, pasted as the
// characterization fixture: if these two ever disagree, the daemon's
// planning host changed bytes somewhere RM3's move never authorized.
const oldPlanHostBlueprintYAML = "id: plan\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: project-planner}\n"

func TestPlanHostRecordBytesEqualTheOldConstant(t *testing.T) {
	raw, ok, err := blueprint.EmbeddedSource().Get("roadmap-plan")
	if err != nil || !ok {
		t.Fatalf("embedded %q: ok=%v err=%v", "roadmap-plan", ok, err)
	}
	if string(raw) != oldPlanHostBlueprintYAML {
		t.Fatalf("the moved bytes differ from the old constant:\nhave %q\nhad  %q", raw, oldPlanHostBlueprintYAML)
	}
	// T0's recorded digest for the constant, kept as a third voice —
	// file bytes, old constant and this sha must all agree.
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "08aa5c6c34760d0bb6f0888ec1e1df490483c6b5d970bf688770491f3e8aa394" {
		t.Fatalf("sha256 = %s, want the digest T0 recorded", got)
	}
	bp, err := blueprint.Load(raw)
	if err != nil {
		t.Fatalf("the moved bytes do not load: %v", err)
	}
	if bp.Version != 1 {
		t.Fatalf("version %d, want 1 — a content change would have had to bump it and this test forbids content changes", bp.Version)
	}
}

func TestBlueprintsStandardCarriesRoadmapPlan(t *testing.T) {
	// The store side of the same identity: after the seed set applies,
	// the named read returns the release's bytes, and dispatch's
	// read-by-name path finds the row.
	f := newRoadmapFixture(t)
	raw, ok, err := StoreBlueprintSource(f.s).Get("roadmap-plan")
	if err != nil || !ok {
		t.Fatalf("seeded store carries no %q: ok=%v err=%v", "roadmap-plan", ok, err)
	}
	if string(raw) != oldPlanHostBlueprintYAML {
		t.Fatal("the seeded row bytes drifted from the constant they moved from")
	}
}
