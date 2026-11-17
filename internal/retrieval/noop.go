package retrieval

import (
	"context"
	"errors"
)

// Noop is the absent state of retrieval (D1, I2): the answer the product
// gives when this slice was never built in. It is the whole content of the
// package under `-tags noretrieval` — the seam compiles, the behaviour is
// legibly not there.
//
// The compile-time assertion below is the mechanism that keeps the seam
// honest: this file and interface.go carry no build tag, so a Backend
// widened by T4 or T7 without widening Noop in the same commit fails to
// build in BOTH configurations, not just the optional one.

var _ Backend = Noop{}

// ErrNotBuilt is the loud misuse answer: a caller that reached KeywordSearch
// without consulting Built() has a bug, and the bug deserves a sentence
// rather than an empty slice — an empty slice from a search is a claim about
// the repository (V13's whole argument), and the no-op must never be able to
// make that claim. It is an error rather than a zero Availability because
// the situation is not a domain outcome — it is a caller who asked an absent
// slice to answer.
var ErrNotBuilt = errors.New("retrieval: refused — this build has no retrieval; the search tool is absent by design (V13), reach for grep (V5)")

// Noop satisfies Backend with no store, no index and no goroutine. Its
// zero-value Availability is ZERO for a reason: there are no reasons, and
// Built() being false is the only honest reading of that. Rendering a no-op
// answer as `index-empty` would be exactly the lie D2 exists to prevent —
// the empty index is one that EXISTS and holds nothing.
type Noop struct{}

// Built reports the absence. Every consumer branches on this first; a
// consumer that does not is misusing the seam, and KeywordSearch below says
// so in terms.
func (Noop) Built() bool { return false }

// Availability answers zero — not "unavailable, index-empty". The corpus
// states belong to an index that is not here.
func (Noop) Availability(context.Context, string, string, *Route) (Availability, error) {
	return Availability{}, nil
}

// KeywordSearch refuses rather than returning nothing — the V13 rule applied
// to the seam itself (see ErrNotBuilt).
func (Noop) KeywordSearch(context.Context, string, string, string, *Route, int) ([]Hit, Availability, error) {
	return nil, Availability{}, ErrNotBuilt
}
