//go:build !noretrieval

package retrieval

import "github.com/cjlapao/aeon/internal/store"

// availability.go — the four reasons (V19, D3), computed from the index's own
// state (R5's point: the reasons are properties of the index, so they are
// one file with it and one task, not a model bolted on after the queries).
//
// availabilityFor is PURE: it reads no store, no clock and no route — the
// caller reads the corpus row and the chunk count and hands both over, which
// is what lets TestFourAvailabilityReasons drive every state, including ones
// no constructor can reach, against one function.

// availabilityFor folds (corpus exists?, chunk count, recorded embedding,
// query route) into the answer.
//
// Two rules shape it beyond the obvious:
//
//   - Nothing indexed is the ONLY unavailability that is not a refusal:
//     index-empty says the index is fine and holds nothing, which D2 keeps
//     apart from both "found nothing" (the query ran) and "absent" (Noop —
//     Built is false before this function is ever reached).
//   - A route named by the caller is a CLAIM to query by vector. A mismatch
//     is then a refusal (V17) — the keyword half could still answer, but
//     answering a vector-shaped ask with keyword results because the vectors
//     were quietly dropped is degradation with a borrowed coat; the caller
//     who wants FTS5 asks without a route, and gets KeywordOnly said out
//     loud (D4).
func availabilityFor(exists bool, chunks int, row store.RetrievalCorpus, route *Route) Availability {
	if !exists || chunks == 0 {
		return Availability{Reasons: []Reason{ReasonIndexEmpty}}
	}

	// Chunks exist. The question left is what the query would answer with.
	switch {
	case route == nil:
		// No route at all: FTS5 answers and says so (V9). This is true
		// whether or not the index has vectors — without a route they
		// cannot be queried, and the honest label is the same either way.
		return Availability{Available: true, KeywordOnly: true, Reasons: []Reason{ReasonNoProvider}}

	case row.EmbedModel == "":
		// A route exists, but nothing has been embedded under it (or any
		// other). FTS5 answers; there is no provider to blame, so the
		// label is carried without a reason — KeywordOnly is the sentence
		// (D4), Reasons is for unavailability (see interface.go).
		return Availability{Available: true, KeywordOnly: true}

	case row.EmbedModel != route.Model || row.EmbedDim != route.Dim:
		// Both halves are checked independently and every failing one is
		// named (the demo's §5 refusal says BOTH numbers): a route at
		// 3072 dims by a differently-named model hides nothing behind
		// the first failure, because each reason answers a different
		// question an operator would otherwise ask the source.
		var reasons []Reason
		if row.EmbedModel != route.Model {
			reasons = append(reasons, ReasonModelMismatch)
		}
		if row.EmbedDim != route.Dim {
			reasons = append(reasons, ReasonDimensionMismatch)
		}
		return Availability{Reasons: reasons}

	default:
		return Availability{Available: true}
	}
}
