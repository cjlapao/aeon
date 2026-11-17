package history

import (
	"fmt"
	"math"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// Rollup is the P32 floor (H6) over a set of costs: a number that is never
// bare when it cannot be exact, with the count of unknowns travelling with
// it. `{Total, Floor, UnknownCount, StepCount}` — the shape T1's spec
// names, because the render it drives is "at least $X (K of N steps unknown)"
// and the count of unknowns is what makes the floor honest rather than a
// total a person would believe. An aggregate over a range containing an
// unknown is itself uncertain (P32); collapsing that uncertainty into the
// number is exactly the shortcut this slice exists to refuse.
type Rollup struct {
	// Total is the FLOOR: the sum of every known total and every partial
	// floor. An unknown cost contributes NOTHING to it — it contributes a
	// count instead (UnknownCount), because a missing number is never a zero.
	Total float64
	// Floor is true when any contributing step was partial or unknown. A
	// rollup with Floor false is a bare total; a rollup with Floor true is a
	// lower bound, and only the former may ever render without "at least".
	Floor bool
	// UnknownCount is the render's K: how many contributing steps had an
	// unknown cost. A partial step also makes Floor true but is not counted
	// here — a partial HAS a number (a floor), it is just not the total.
	UnknownCount int
	// StepCount is the render's N: every step that contributed, whatever its
	// state. A rollup over zero steps has StepCount 0 and renders a bare zero
	// (there is nothing to floor about), never "at least $0.00".
	StepCount int
}

// costFloor is the one USD a single step contributes to a floor: the known
// total, a partial floor, or nothing. It mirrors store.costQuery's
// SUM(cost_usd) exactly — an unknown (or NULL) cost is never added as a
// zero, so a floor can never read as a total.
func costFloor(c model.Cost) float64 {
	switch c.State {
	case model.CostKnown:
		return c.USD
	case model.CostPartial:
		return c.USDAtLeast
	default:
		return 0
	}
}

// addStep folds one step's cost into the rollup. It is the ONE place a
// step's cost enters a rollup, so the floor cannot be computed any other
// way (a second add site is a second truth, and the floor is the slice's
// single most load-bearing rule).
func (r *Rollup) addStep(c model.Cost) {
	r.Total += costFloor(c)
	r.StepCount++
	switch c.State {
	case model.CostUnknown:
		r.UnknownCount++
		r.Floor = true
	case model.CostPartial:
		r.Floor = true
	}
}

// Add merges another rollup into this one — the one place two floors meet,
// so the across-many-loops rollup is the sum of the per-loop floors and
// nothing else. A floor in either operand floors the result.
func (r *Rollup) Add(other Rollup) {
	r.Total += other.Total
	r.StepCount += other.StepCount
	r.UnknownCount += other.UnknownCount
	if other.Floor {
		r.Floor = true
	}
}

// toRollup converts a store.RunCost (the per-loop floor the daemon already
// renders) into this slice's shape. The two floors agree by construction —
// both sum cost_usd and both keep the unknowns as a count — so a loop's
// figure is identical whether it is read through the store or through this
// package, which is the whole point of H6 generalising the per-loop floor
// rather than inventing a second one. The store's Exact is true only when
// nothing was partial or unknown, so Floor and UnknownCount follow from it.
func toRollup(rc store.RunCost) Rollup {
	return Rollup{
		Total:        rc.USDAtLeast,
		Floor:        !rc.Exact,
		UnknownCount: rc.Unknown,
		StepCount:    rc.Steps,
	}
}

// Aggregate is the rollup across a set of per-loop costs (H6: "a rollup over
// N steps across M loops"). It is the sum of the per-loop floors through
// Add, so an unknown in any loop floors the whole — the count of unknowns
// travels with the number, never a footnote a person has to go find.
func Aggregate(loops []Rollup) Rollup {
	var out Rollup
	for _, r := range loops {
		out.Add(r)
	}
	return out
}

// Render is the ONE renderer of a rollup (H6): "at least $X (K of N steps
// unknown)" whenever the floor is not exact, a bare "$X" only when every
// step is known. Mirrors the house's one-formatter discipline (Verdict.
// Sentence, InForceAnswer.Render) — a second place that phrases a cost is a
// second truth, and the slice's single most load-bearing rule (H6) is that
// no surface prints a bare number the store itself would not. The count of
// unknowns travels with the number; a bare figure is only ever printed when
// there is nothing to floor about.
func (r Rollup) Render() string {
	// A rollup over no steps is a bare zero, never "at least $0.00" — there
	// is nothing the number is the floor of.
	if r.StepCount == 0 {
		return fmt.Sprintf("$%s", money(r.Total))
	}
	if !r.Floor {
		return fmt.Sprintf("$%s", money(r.Total))
	}
	s := fmt.Sprintf("at least $%s", money(r.Total))
	if r.UnknownCount > 0 {
		s += fmt.Sprintf(" (%d of %d steps unknown)", r.UnknownCount, r.StepCount)
	}
	return s
}

// money renders a USD figure the way the existing cost view does: two
// decimals, no thousands separator (the existing per-loop floor renders the
// same way, and a second format is a second truth). A floor that rounds to
// zero is still printed as $0.00 with its "at least" — the lower bound is
// the point, and an empty string would read as a total.
func money(usd float64) string {
	if math.IsNaN(usd) {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", usd)
}
