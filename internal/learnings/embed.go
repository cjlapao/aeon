package learnings

import (
	"context"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/model"
)

// Embedder is the seam to the route: the one place a learning's text
// becomes a vector. Production passes ModelClientEmbedder, the thin
// wrapper over *model.Client; tests pass a fake. A nil embedder is the
// normal, working state (D5): a learning with no route has no vector, and
// a query without one returns its tag-filtered set unranked.
type Embedder interface {
	// Embed sends texts to the route's embeddings endpoint and returns the
	// vectors, the usage and the cost, priced at the one site.
	Embed(ctx context.Context, route *model.Route, texts []string) (vectors [][]float32, usage model.Usage, cost model.Cost, err error)
}

// ModelClientEmbedder is the production Embedder: a thin wrapper over the
// model package's own Embed, holding no credential (the route's key is
// read by the client). It exists so the route claim is testable through a
// fake, and so the cost is priced at model's one site, not a second
// formula here.
type ModelClientEmbedder struct{ C *model.Client }

var _ Embedder = ModelClientEmbedder{}

func (e ModelClientEmbedder) Embed(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	if e.C == nil {
		return nil, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: model.ReasonNotAttempted},
			fmt.Errorf("learnings: refused an embedding — the model client behind this seam is nil (the entry point wires ModelClientEmbedder with a live *model.Client)")
	}
	return e.C.Embed(ctx, *route, texts)
}

// rankWithinSet is the embedding ranker (T3): it embeds the query and
// reorders the tag-filtered set by similarity WITHIN that set only (D6).
// A learning the tag filter excluded is never pulled in by a similar
// vector — that is the rule the name means. A query with no route, a
// failed embed, or a vec0 with no rows is the normal state (D5): the set
// is returned unranked, in the store's own order, not an error.
func (l *Learnings) rankWithinSet(ctx context.Context, queryText string, set []Learning) []Learning {
	if l.route == nil || l.embedder == nil || queryText == "" {
		return set // no route, no ranking — the tag-filtered set stands
	}
	vectors, _, _, err := l.embedder.Embed(ctx, l.route, []string{queryText})
	if err != nil || len(vectors) == 0 || len(vectors[0]) == 0 {
		return set // the route did not answer; the tag-filtered set stands (D5)
	}
	qvec := vectors[0]
	hits, err := l.s.VectorSearchLearning(l.route.Model, len(qvec), qvec, len(set))
	if err != nil {
		return set
	}
	if len(hits) == 0 {
		return set // nothing embedded for the set; the unranked set stands (D5)
	}
	// D6: keep only the hits that are in the tag-filtered set. A hit for a
	// learning the tag filter excluded is dropped — the embedding ranks
	// within the set, it never widens it.
	inSet := make(map[int64]float64, len(hits))
	for _, h := range hits {
		for _, lr := range set {
			if lr.ID == h.LearningID {
				inSet[lr.ID] = h.Score
				break
			}
		}
	}
	// Reorder: a scored learning precedes an unscored one, and among scored
	// learnings the higher score precedes — the stable sort keeps the
	// store's order for equal scores and for the unscored tail, so the
	// result is a reordering of the set, never a different set.
	sort.SliceStable(set, func(i, j int) bool {
		si, oki := inSet[set[i].ID]
		sj, okj := inSet[set[j].ID]
		switch {
		case oki && okj:
			return si > sj
		case oki:
			return true // i scored, j not → i first
		case okj:
			return false // i not scored, j scored → j first
		default:
			return false // both unscored → keep the store's order
		}
	})
	return set
}

// EmbedPending embeds every learning in a scope that has no vector yet
// (T3's embedding pass, the retrieval EmbedPending precedent). It records
// the dimension the route produced, ensures the on-demand vec0, and
// upserts each missing vector — a learning whose body did not move but
// whose vector is absent is re-embedded, one with a vector is left. The
// pass is idempotent: a second run with nothing new embeds nothing.
func (l *Learnings) EmbedPending(ctx context.Context, scope Scope, scopeKey string) (int, error) {
	if l.route == nil || l.embedder == nil {
		return 0, fmt.Errorf("learnings: refused an embedding pass — no route is configured (a learning with no route has no vector, the normal state, D5)")
	}
	learnings, err := l.List(scope, scopeKey)
	if err != nil {
		return 0, err
	}
	if len(learnings) == 0 {
		return 0, nil
	}
	// Embed the first learning to fix the dimension the route produces —
	// the dimension is data (the vec0 name carries it), so it comes from
	// the wire, not from the model name.
	vectors, _, _, err := l.embedder.Embed(ctx, l.route, []string{learnings[0].Body})
	if err != nil {
		return 0, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return 0, fmt.Errorf("learnings: the route produced no vector for %d learnings — nothing to embed", len(learnings))
	}
	dim := len(vectors[0])
	if _, err := l.s.EnsureLearningVecTable(l.route.Model, dim); err != nil {
		return 0, err
	}
	// Resume: skip the learnings that already have a vector at this
	// dimension — a body that did not move keeps its vector.
	have, err := l.s.LearningVecIDs(l.route.Model, dim)
	if err != nil {
		return 0, err
	}
	haveSet := make(map[int64]bool, len(have))
	for _, id := range have {
		haveSet[id] = true
	}
	// Embed the rest, one at a time (a learning is one vector, not
	// chunks), upserting as they come.
	embedded := 0
	for _, lr := range learnings {
		if haveSet[lr.ID] {
			continue
		}
		vecs, _, _, err := l.embedder.Embed(ctx, l.route, []string{lr.Body})
		if err != nil {
			return embedded, err
		}
		if len(vecs) == 0 {
			continue
		}
		if err := l.s.UpsertLearningVec(lr.ID, l.route.Model, dim, vecs[0]); err != nil {
			return embedded, err
		}
		embedded++
	}
	return embedded, nil
}
