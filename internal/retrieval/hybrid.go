//go:build !noretrieval

package retrieval

import (
	"context"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/model"
)

// hybrid.go — V3 as code: vector plus keyword, fused, because "pure
// embedding search over code is mediocre, and SQLite has FTS5 natively. The
// two failure modes are complementary." An exact identifier is a literal —
// FTS5 wins and vectors wander; a concept hides under several names —
// vectors win and FTS5 has no shared literal to match. A fusion that lets
// one source dominate has one failure mode rather than none, and is WORSE
// than either alone because it looks like it is doing both (D13).
//
// The method, stated (the task's own demand): **reciprocal rank fusion**.
// Each source contributes `1/(k + rank)` at the rank it awarded — k=60
// (the stock RR smoothing constant; its exact value is not load-bearing,
// WHAT is load-bearing is that only RANKS enter this arithmetic). A cosine
// similarity and a BM25 penalty are different quantities with different
// ranges, units and sign conventions; a weighted sum of them is "a number
// with no meaning that happens to sort". A rank is the only thing the two
// lists genuinely have in common. TestFusionDoesNotCompareScoresDirectly
// pins that nothing else feeds the sum.

// FusedHit is a query result: a location, a fusion score, and how it got
// there. No Body field exists on this type or on Hit (V4/D8/I5) — the
// property test over EVERY result has no field to leak through.
type FusedHit struct {
	Hit
	Score   float64
	Sources []string // which lists ranked it: "keyword", "vector", or both
}

// rankedList is one index's ordered answer with its tag: the tag survives
// into each fused hit so a report can count contributions per source —
// D13's proof is countable, not vibes.
type rankedList struct {
	tag  string
	hits []Hit
}

// fuseRanked is the entry point the query calls: same math, real tags.
func fuseRanked(lists ...rankedList) []FusedHit {
	const k = 60
	type acc struct {
		hit     Hit
		score   float64
		sources []string
	}
	by := map[[3]any]*acc{}
	var order [][3]any
	for _, l := range lists {
		for rank, h := range l.hits {
			id := [3]any{h.Source, h.LineStart, h.LineEnd}
			a, ok := by[id]
			if !ok {
				a = &acc{hit: h}
				by[id] = a
				order = append(order, id)
			}
			a.score += 1.0 / float64(k+rank+1)
			if !hasTag(a.sources, l.tag) {
				a.sources = append(a.sources, l.tag)
			}
		}
	}
	out := make([]FusedHit, 0, len(by))
	for _, id := range order {
		a := by[id]
		out = append(out, FusedHit{Hit: a.hit, Score: a.score, Sources: a.sources})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Hit.Source != out[j].Hit.Source {
			return out[i].Hit.Source < out[j].Hit.Source
		}
		return out[i].Hit.LineStart < out[j].Hit.LineStart
	})
	return out
}

func hasTag(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Search is one query over one NAMED corpus (O8, I7 — no default; a
// default corpus is a query whose scope depends on a setting the caller
// did not state). It hits both indexes and fuses; availability is checked
// FIRST so an empty index answers the reason (D2), never a confident empty
// list — a listing that matches nothing must never read as "there was
// nothing there" (S10's audit discipline, same shape).
//
// When no route is configured (no-provider), the vector half is absent —
// not empty — and the answer is the keyword list with KeywordOnly set: the
// honest degraded shape D4 demanded, available at T1 and honoured here.
func (x *Index) Search(ctx context.Context, root, corpus, query string, route *model.Route, emb Embedder, limit int) ([]FusedHit, Availability, error) {
	if !ValidCorpus(corpus) {
		return nil, Availability{}, fmt.Errorf("retrieval: refused corpus %q — a query NAMES its corpus (the corpora are %q and %q; there is no default)", corpus, CorpusCode, CorpusHistory)
	}
	if query == "" {
		return nil, Availability{}, fmt.Errorf("retrieval: refused an empty query at %s — an empty string matches nothing and answers like it found everything", root)
	}
	if limit <= 0 {
		limit = 10
	}

	// retrieval's own Route is the availability view (model, dim); the
	// model.Route is the route the SEAM speaks — one shape for "can I",
	// the S18 type for "call it" (T3's split, kept).
	view := &Route{}
	if route != nil {
		view.Model = route.Model
		// The dimension is NOT taken from the caller's claim: it is the
		// row's recorded data (V6) — Availability compares the two and
		// names a mismatch rather than trusting a number typed in.
		if row, ok, err := x.s.RetrievalCorpusRow(root, corpus); err != nil {
			return nil, Availability{}, err
		} else if ok {
			view.Dim = row.EmbedDim
		}
	}
	avail, err := x.Availability(ctx, root, corpus, view)
	if err != nil {
		return nil, avail, err
	}
	if !avail.Available {
		// index-empty is the answer here: NOT zero results — the reason
		// (D2). The caller gets the Availability, not a shrug.
		msg := fmt.Sprintf("retrieval: refused the query at %s (%s) — %s", root, corpus, reasonSentence(avail.Reasons))
		// V17 (D9/O3): a mismatch refuses AND prices AND reindexes nothing.
		// The estimate runs the same comparison walk and writes nothing —
		// a refusal that had already reindexed would be the lie V17 exists
		// to prevent (the reindex spy at zero is the gate). A walk failure
		// degrades the money paragraph, never the refusal: a query that
		// cannot run without a price was still refused.
		if hasMismatch(avail.Reasons) && route != nil {
			// The refusal names both spaces (V17's example sentence):
			// "model-mismatch" alone asks the operator to go find which
			// two models disagree.
			if row, ok, err := x.s.RetrievalCorpusRow(root, corpus); err == nil && ok {
				msg += fmt.Sprintf(": the index was built with %q and route %q is %q", row.EmbedModel, route.ID, route.Model)
			}
			if est, err := x.EstimateReindex(ctx, root, corpus, route); err == nil {
				msg += "\n\n  " + est.Render()
				if est.RouteID != "" {
					msg += fmt.Sprintf("\n\n  To proceed:  aeon index rebuild --corpus %s --embed-route %s", corpus, est.RouteID)
				} else {
					msg += "\n\n  To proceed:  configure an embedding route, then aeon index rebuild"
				}
			}
		}
		return nil, avail, fmt.Errorf("%s", msg)
	}

	keyword, _, err := x.KeywordSearch(ctx, root, corpus, query, view, limit*2)
	if err != nil {
		return nil, avail, err
	}

	var vector []Hit
	if route != nil && emb != nil && !avail.KeywordOnly {
		vectors, _, _, err := emb.EmbedBatch(ctx, route, []string{query})
		if err != nil {
			// A query-time embedding failure degrades to keyword-only,
			// visibly: KeywordOnly flips true on the returned Availability.
			// The alternative — failing the whole query because one call
			// flaked — would make the index unavailable whenever the
			// provider is, and the keyword half is right there.
			avail.KeywordOnly = true
		} else {
			vector, err = x.VectorSearch(root, corpus, vectors[0], limit*2)
			if err != nil {
				return nil, avail, err
			}
		}
	}

	fused := fuseRanked(
		rankedList{tag: "keyword", hits: keyword},
		rankedList{tag: "vector", hits: vector},
	)
	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused, avail, nil
}

func reasonSentence(rs []Reason) string {
	if len(rs) == 0 {
		return "no reason was recorded"
	}
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += " and "
		}
		out += string(r)
	}
	return out
}
