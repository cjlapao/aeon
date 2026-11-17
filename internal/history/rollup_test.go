package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// known, partial and unknown costs, the three states P31 names.
var (
	costKnown   = model.Cost{State: model.CostKnown, USD: 0.05}
	costPartial = model.Cost{State: model.CostPartial, USDAtLeast: 0.03}
	costUnknown = model.Cost{State: model.CostUnknown}
)

// TestRollupFloorsOnAnyUnknownCost is P32: an aggregate over a range
// containing an unknown cost is itself uncertain, and the unknown must travel
// with the number (H6). A fixture with one unknown-cost step among many known
// ones floors (Floor: true); an all-known fixture does not (Floor: false).
// Both are asserted, because the gate is the difference between a floor and a
// total a person would believe — the slice's single most load-bearing rule.
func TestRollupFloorsOnAnyUnknownCost(t *testing.T) {
	// All-known: a total, never a floor.
	known := Rollup{}
	known.addStep(costKnown)
	known.addStep(costKnown)
	known.addStep(costKnown)
	if known.Floor {
		t.Errorf("an all-known rollup is a floor (got %+v) — it must be a bare total (P32)", known)
	}
	if known.UnknownCount != 0 || known.StepCount != 3 {
		t.Errorf("an all-known rollup counts wrong (got %+v)", known)
	}

	// One unknown among the known: a floor, and the unknown travels with it.
	unknown := Rollup{}
	unknown.addStep(costKnown)
	unknown.addStep(costKnown)
	unknown.addStep(costUnknown)
	if !unknown.Floor {
		t.Errorf("a rollup with one unknown cost is not a floor (got %+v) — P32 is broken: an unknown must floor", unknown)
	}
	if unknown.UnknownCount != 1 {
		t.Errorf("the unknown is not counted (got %+v) — the count of unknowns must travel with the number (H6)", unknown)
	}

	// A partial is also a floor (a number, but a lower bound, not a total),
	// though it is not an unknown — a partial has a number.
	partial := Rollup{}
	partial.addStep(costKnown)
	partial.addStep(costPartial)
	if !partial.Floor {
		t.Errorf("a rollup with a partial cost is not a floor (got %+v) — a partial is a lower bound, not a total", partial)
	}
	if partial.UnknownCount != 0 {
		t.Errorf("a partial is counted as an unknown (got %+v) — a partial has a number, it is a floor not an unknown", partial)
	}

	// The across-many-loops rollup (H6) floors if ANY loop floors: one
	// unknown in one loop floors the whole.
	agg := Aggregate([]Rollup{
		{Total: 0.05, Floor: false, StepCount: 1}, // a known loop
		{Total: 0.03, Floor: true, UnknownCount: 1, StepCount: 1}, // a floor loop
		{Total: 0.00, Floor: false, StepCount: 1}, // a known zero
	})
	if !agg.Floor {
		t.Errorf("the across-loops rollup is not a floor when one member is (got %+v) — H6: a floor in any loop floors the whole", agg)
	}
	if agg.UnknownCount != 1 || agg.StepCount != 3 {
		t.Errorf("the across-loops rollup counts wrong (got %+v)", agg)
	}
}

// TestRollupRenderIsOneFormatter is H6's render discipline: "at least $X (K of
// N steps unknown)" whenever the floor is not exact, a bare "$X" only when
// every step is known — and the count of unknowns travels with the number,
// never a footnote a person has to go find. A second place that phrases a
// cost is a second truth, so the render is one function (mirroring the
// house's one-formatter discipline).
func TestRollupRenderIsOneFormatter(t *testing.T) {
	// A floor with an unknown renders "at least … (K of N … unknown)".
	if got := (Rollup{Total: 0.08, Floor: true, UnknownCount: 1, StepCount: 3}).Render(); got != "at least $0.08 (1 of 3 steps unknown)" {
		t.Errorf("a floored rollup renders wrong: got %q", got)
	}
	// A floor with only a partial (no unknown) renders "at least …" with no
	// "(… unknown)" — a partial has a number, so it is a floor not an unknown.
	if got := (Rollup{Total: 0.06, Floor: true, UnknownCount: 0, StepCount: 2}).Render(); got != "at least $0.06" {
		t.Errorf("a partial-only floor renders wrong: got %q", got)
	}
	// A bare total (all known) renders a number, never "at least".
	if got := (Rollup{Total: 0.12, Floor: false, UnknownCount: 0, StepCount: 3}).Render(); got != "$0.12" {
		t.Errorf("a bare total renders wrong: got %q", got)
	}
	// A rollup over no steps is a bare zero, never "at least $0.00" — there
	// is nothing the number is the floor of.
	if got := (Rollup{Total: 0, Floor: false, UnknownCount: 0, StepCount: 0}).Render(); got != "$0.00" {
		t.Errorf("an empty rollup renders wrong: got %q", got)
	}
}

// TestLoopCostFloorsThroughStore pins that the per-loop floor read through the
// store (LoopCost) and converted to this slice's shape floors exactly as the
// rollup does — so a loop's figure is identical whether read through the
// store or through this package (H6 generalising the per-loop floor, not a
// second one). A loop with a known step and an unknown step floors.
func TestLoopCostFloorsThroughStore(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	loop, sc := seedLoop(t, s, "/proj", "costed")

	seedStep(t, s, sc, store.NewStep{Phase: "build", Agent: "gpt"}, costKnown)
	seedStep(t, s, sc, store.NewStep{Phase: "test", Agent: "gpt"}, costUnknown)

	// LoopCost takes the loop's id (a surrogate), not its slug — the merge
	// reads it the same way, so this is the same read a live entry uses.
	rc, err := s.LoopCost(loop.ID)
	if err != nil {
		t.Fatalf("loop cost: %v", err)
	}
	r := toRollup(rc)
	if !r.Floor {
		t.Errorf("a loop with an unknown-cost step is not a floor through the store (got %+v) — the store's floor and this slice's must agree", r)
	}
	if r.UnknownCount != 1 {
		t.Errorf("the store's unknown count is wrong (got %+v)", r)
	}
	if r.StepCount != 2 {
		t.Errorf("the store's step count is wrong (got %+v)", r)
	}
}
