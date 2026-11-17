package scorecard

// compare.go — RT8's comparison is TWO READS (rule 11): two independent
// Compute calls with identical rules, two cards side by side. No delta
// is computed, stored, or cached; a person reads two numbers. The seam
// `compute` exists so the twin-read is TESTABLE without a store (the
// test swaps a counter in), not so anything can hook production.

import "context"

var compute = Compute

// Comparison is the pair — labelled by the windows that produced them,
// so the two numbers cannot be confused for one.
type Comparison struct {
	A     Window `json:"windowA"`
	B     Window `json:"windowB"`
	CardA Card   `json:"cardA"`
	CardB Card   `json:"cardB"`
}

// Compare reads the same subject over two windows. Nothing else.
func Compare(ctx context.Context, d Deps, subj Subject, wA, wB Window) (Comparison, error) {
	cardA, err := compute(ctx, d, subj, wA)
	if err != nil {
		return Comparison{}, err
	}
	cardB, err := compute(ctx, d, subj, wB)
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{A: cardA.Window, B: cardB.Window, CardA: cardA, CardB: cardB}, nil
}
