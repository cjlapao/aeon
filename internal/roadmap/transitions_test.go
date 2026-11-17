package roadmap

// T2's gates, pure half. The transition test is machine.go's discipline
// (enumerated FROM the table, driven by the full 8×8 cross product — a
// hand-written list of legal transitions passes forever after somebody
// adds one), the way internal/queue drove its 7×7 for CG1.

import "testing"

func TestTransitionTableIsExhaustiveAndClosed(t *testing.T) {
	// R1: the table is the whole machine. Every listed pair succeeds;
	// every pair NOT listed (and not a self-transition) is refused —
	// driven from the 8×8 cross product, never from a hand-list.
	table := AllowedTransitions()
	edges := 0
	for from, targets := range table {
		if !ValidStatus(from) {
			t.Errorf("%q in the table is not one of the eight", from)
		}
		for _, to := range targets {
			edges++
			if !ValidStatus(to) {
				t.Errorf("the table's target %q (from %q) is not one of the eight", to, from)
				continue
			}
			if res := ValidateTransition(from, to); !res.OK {
				t.Errorf("%s → %s is in the table and was refused: %s", from, to, res.Reason)
			}
		}
	}
	if edges != 11 {
		t.Errorf("the table carries %d edges, want 11 (5 forward rungs + running→done + 5 off-ramps to cancelled from every pre-loop rung)", edges)
	}
	for _, from := range Statuses() {
		if _, listed := table[from]; !listed {
			t.Errorf("status %q is absent from the table — every rung needs an entry, terminal ones included (as empty)", from)
		}
		// A self-transition is idempotent, not wrong (machine.go's carried
		// rule — the retried reactor write depends on it).
		if res := ValidateTransition(from, from); !res.OK {
			t.Errorf("%s → %s (self) refused: %s — a retried reactor write must be idempotent", from, from, res.Reason)
		}
		for _, to := range Statuses() {
			if from == to {
				continue
			}
			legal := false
			for _, s := range table[from] {
				if s == to {
					legal = true
				}
			}
			res := ValidateTransition(from, to)
			if legal && !res.OK {
				t.Errorf("%s → %s is listed and was refused: %s", from, to, res.Reason)
			}
			if !legal && res.OK {
				t.Errorf("%s → %s is NOT listed and was accepted — the machine has a phantom edge", from, to)
			}
			if !res.OK {
				// House refusal rule: name the alternative.
				if res.Reason == "" {
					t.Errorf("%s → %s refused with no reason", from, to)
				}
				if len(res.Legal) != len(table[from]) {
					t.Errorf("%s → %s refusal carries %d legal targets, want the table's %d", from, to, len(res.Legal), len(table[from]))
				}
			}
		}
	}

	// The two edges S24's discipline says must be ABSENT, pinned by name
	// so a future "tidy-up" that adds one fails here and not in a demo.
	if res := ValidateTransition(StatusRunning, StatusCancelled); res.OK {
		t.Error("running → cancelled was accepted — a loop in flight is the loop's own record (D11 carried), the absence is the rule")
	}
	if res := ValidateTransition(StatusDone, StatusCancelled); res.OK {
		t.Error("done → cancelled was accepted — done is terminal")
	}
}

func TestCancelFromAnyNonTerminalState(t *testing.T) {
	// D7's promise read the other way: EVERY non-terminal rung can reach
	// cancelled (the off-ramp is total over the non-terminals), and
	// exactly the terminal two cannot.
	for _, from := range Statuses() {
		res := ValidateTransition(from, StatusCancelled)
		switch from {
		case StatusCancelled:
			// cancelled → cancelled is the machine's idempotent self-rule
			// (a retried reactor write), not a reopened off-ramp.
			if !res.OK {
				t.Errorf("the self-transition %s → cancelled refused: %s", from, res.Reason)
			}
		case StatusRunning, StatusDone:
			if res.OK {
				t.Errorf("%s → cancelled was accepted; only the five pre-loop rungs may cancel (running is the loop's own record — S24's identical rule)", from)
			}
		default:
			if !res.OK {
				t.Errorf("%s → cancelled was refused: %s — every non-terminal rung owes the off-ramp (D7)", from, res.Reason)
			}
		}
	}
	// The refusal for running names WHY (the house refusal rule at its
	// most load-bearing: a person cancelling a running item needs to
	// learn the loop is the thing to stop).
	res := ValidateTransition(StatusRunning, StatusCancelled)
	if res.Legal == nil || len(res.Legal) != 1 || res.Legal[0] != StatusDone {
		t.Errorf("the running refusal carries legal=%v, want [done] — name the alternative", res.Legal)
	}
}

func TestForwardPathIsTheOnlyForwardPath(t *testing.T) {
	// R1's diagram walked edge by edge — and skipping a rung is refused
	// (idea → ready is exactly the shortcut that would let an unbriefed,
	// unplanned item into the running queue; D4 says it cannot, and the
	// table says it first).
	path := []Status{StatusIdea, StatusIdeating, StatusBriefAgreed, StatusPlanned, StatusReady, StatusRunning, StatusDone}
	for i := 0; i+1 < len(path); i++ {
		if res := ValidateTransition(path[i], path[i+1]); !res.OK {
			t.Errorf("R1's own edge %s → %s refused: %s", path[i], path[i+1], res.Reason)
		}
	}
	for _, jump := range [][2]Status{
		{StatusIdea, StatusBriefAgreed}, {StatusIdea, StatusReady}, {StatusIdeating, StatusReady},
		{StatusBriefAgreed, StatusReady}, {StatusPlanned, StatusRunning}, {StatusReady, StatusDone},
	} {
		if res := ValidateTransition(jump[0], jump[1]); res.OK {
			t.Errorf("rung-skipping %s → %s was accepted — the machine runs one rung at a time", jump[0], jump[1])
		}
	}
}
