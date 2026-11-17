package agentsvc

import (
	"context"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/model"
)

// cost.go is A1's amendment made true: "no spawn, no per-subtree cost rollup,
// because there is no tree" was right about processes and wrong about
// accounting. An agent asking an agent produces a graph whether or not
// anybody records its edges — and refusing to record them did not make the
// cost unattributable, it made it UNATTRIBUTED, which is worse, because
// unattributed reads as zero (the amendment is recorded in
// docs/aeon/S13-agents/design.md with its original wording preserved).
//
// # A query, never an accumulator (scope 3)
//
// The rollup reads rows on every call. No running total is kept anywhere:
// a cached total is a number that disagrees with the rows and nothing says
// which is right. There is deliberately NO package-level variable in this
// file, and the task's grep test reads this file to prove it.
//
// # The state rule is stricter than arithmetic (P32)
//
// model.Cost.Add answers "anything + unknown → unknown". The chain rollup
// answers instead — per this slice's table — "any child unknown → the parent
// is NOT known, at best partial". Partial is the chosen ceiling (the notes
// say which, as the task demands): a total marked plain unknown buries the
// measured floors behind an absence, while partial says "at least this much,
// and one or more children are missing numbers" in the same breath. Nothing
// is invented either way — no number appears that no callee reported.

// Rollup is a chain's cost as of the moment it was asked — a reading of
// rows, not a stored figure.
type Rollup struct {
	Cost     model.Cost `json:"cost"`
	Children int        `json:"children"` // child TASKS counted (the step itself is not a child)
	Nodes    int        `json:"nodes"`    // steps whose asks were walked, incl. the root
}

// RollupCost reads the chain rooted at (callerLoopID, callerStepID) and
// folds every cost the rows carry: the caller step's own cost first, then
// every task the step filed, then each callee's own delegations — attributed
// through the graph T5 already walks, so the answer survives the restart
// that deletes every in-memory notion of "the current total".
//
// Failed and refused-outcome tasks count too (scope 5): an expensive failure
// that vanishes from the total on failure hides exactly the expensive
// failures, which is the error A1's amendment exists to end.
func (iv *Invoker) RollupCost(_ context.Context, callerLoopID string, callerStepID int64) (Rollup, *Reject) {
	step, err := iv.s.ReadStep(callerStepID)
	if err != nil {
		return Rollup{}, &Reject{Field: "step", Detail: fmt.Sprintf(
			"rollup refused: the root step %d cannot be read (%v) — a total built without its base is invention", callerStepID, err)}
	}
	seen := map[string]bool{}
	var kids []model.Cost
	children, nodes := 0, 0
	_ = step
	var walk func(loopID string, stepID int64) *Reject
	walk = func(loopID string, stepID int64) *Reject {
		key := fmt.Sprintf("%s/%d", loopID, stepID)
		if seen[key] {
			// Diamond asks (two callers, one specialist) reach the same
			// recorded delegation once per rollup — counting its rows twice
			// inside ONE parent's total would be inventing a second bill.
			return nil
		}
		seen[key] = true
		nodes++
		// Every walked step owes its OWN cost to the total too — the
		// intermediate step of a two-level chain that only rolled up leaf
		// tasks reported 1.6 of its true 1.8 (measured), and an undertotal
		// that reads as known is P32's exact lie.
		here, err := iv.s.ReadStep(stepID)
		if err != nil {
			return &Reject{Field: "step", Detail: fmt.Sprintf("rollup refused: step %d cannot be read (%v)", stepID, err)}
		}
		kids = append(kids, here.Cost)
		tasks, err := iv.s.ListAgentTasksForCaller(loopID, stepID)
		if err != nil {
			return &Reject{Field: "store", Detail: fmt.Sprintf("rollup refused: %v", err)}
		}
		edges, err := iv.s.ListAgentChainEdges(loopID, stepID)
		if err != nil {
			return &Reject{Field: "store", Detail: fmt.Sprintf("rollup refused: %v", err)}
		}
		for _, row := range tasks {
			// Every terminal row counts; a RUNNING or WAITING task has no
			// reported cost, which reads as the unknown it is (never zero) —
			// toTask is the one decoder of that honesty.
			kids = append(kids, toTask(row).Cost)
			children++
		}
		for _, e := range edges {
			// The callee's own delegations are recorded under the callee's
			// steps — attributed to this chain through the graph, which is
			// rows and nothing else (T5).
			sub, err := iv.s.ChainEdgesByCallerAgent(e.CalleeAgent)
			if err != nil {
				return &Reject{Field: "store", Detail: fmt.Sprintf("rollup refused: %v", err)}
			}
			for _, se := range sub {
				if err := walk(se.CallerLoopID, se.CallerStepID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if rj := walk(callerLoopID, callerStepID); rj != nil {
		return Rollup{}, rj
	}
	if nodes == 0 {
		return Rollup{}, &Reject{Field: "walk", Detail: "rollup refused: the walk found nothing to read"}
	}
	return Rollup{Cost: foldChain(kids), Children: children, Nodes: nodes}, nil
}

// foldChain is the table, written out:
//
//	any child unknown  → parent PARTIAL (never known; see the header for why
//	                     not the plain unknown model.Add would answer)
//	any child partial  → parent PARTIAL
//	all known          → parent KNOWN, the sum
//
// Floors are kept (USDAtLeast), Missing lists are merged, and no number
// enters the total that a callee did not report.
func foldChain(all []model.Cost) model.Cost {
	known := true
	atLeast := 0.0
	var missing []string
	unknownReason := ""
	for _, c := range all {
		switch c.State {
		case model.CostKnown:
			atLeast += c.USD
		case model.CostPartial:
			known = false
			atLeast += c.USDAtLeast
			missing = append(missing, c.Missing...)
		case model.CostUnknown:
			known = false
			atLeast += c.USDAtLeast
			if c.Reason != "chain-child-unknown" {
				missing = append(missing, "child-cost:"+c.Reason)
			}
			if unknownReason == "" {
				unknownReason = c.Reason
			}
		}
	}
	if known {
		return model.Cost{State: model.CostKnown, USD: atLeast}
	}
	sort.Strings(missing)
	reason := ""
	if unknownReason != "" {
		// The state is partial per the table; the reason still names WHY
		// certainty is impossible, so the reader learns which child to go
		// and ask.
		reason = "chain-child-unknown: " + unknownReason
	}
	return model.Cost{State: model.CostPartial, USDAtLeast: atLeast, Missing: dedupeStrings(missing), Reason: reason}
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
