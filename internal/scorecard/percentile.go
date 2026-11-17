package scorecard

// percentile.go — median and p90 by NEAREST RANK, documented as the
// design's alternative to a mean (RT7: wall times are skewed; the mean
// of a skew is a number that lies politely).
//
// The rule: sort ascending; the p-quantile is the value at 1-based index
// ceil(p·N). For an even N the median is therefore the LOWER middle — a
// deliberate, stated choice: a "median" that averaged the two middles
// would report times no loop actually took. N == 0 answers 0 only where
// the caller has already refused to state a value (the metric's state is
// cannot-say then; 0 never reaches a card as a number).

import (
	"math"
	"sort"
)

// percentile returns the nearest-rank value at p (0 < p ≤ 1).
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p * float64(n)))
	if idx < 1 {
		idx = 1
	}
	if idx > n {
		idx = n
	}
	return sorted[idx-1]
}

// median sorts and takes the 0.5 rank. Callers pass samples; the sort is
// on a copy — the samples slice keeps its own order (N8's discipline
// applies to arguments too).
func median(samples []float64) float64 {
	cp := append([]float64(nil), samples...)
	sort.Float64s(cp)
	return percentile(cp, 0.5)
}
