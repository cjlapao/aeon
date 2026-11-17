package learnings

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// fakeEmbedder is a table of opinions: a text maps to its vector, and
// anything else gets a default. It is the seam a fake implements, which is
// what makes the route claim testable (the same discipline the retrieval
// topicEmbedder carries).
type fakeEmbedder struct {
	table map[string][]float32
}

func (f fakeEmbedder) Embed(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := f.table[t]
		if !ok {
			v = []float32{0.1, 0.1} // a neutral vector for anything untabled
		}
		out[i] = v
	}
	return out, model.Usage{}, model.Cost{State: model.CostKnown}, nil
}

// TestEmbeddingRanksWithinTagFilterSet — D6: where a route is configured,
// Query ranks the tag-filtered set by similarity to the query, WITHIN that
// set only — a learning the tag filter excluded is never pulled in by a
// similar vector. The result is a reordering of the set, never a wider one.
func TestEmbeddingRanksWithinTagFilterSet(t *testing.T) {
	l, _ := openLearnings(t)
	route := &model.Route{ID: "embed", Provider: "openai", Model: "fake-embed"}
	l.SetRoute(route, fakeEmbedder{table: map[string][]float32{
		// A query and three bodies, in a 2-d space where "auth" is the
		// x-axis. A is closest to the query; B is nearer; C (the
		// un-tagged body) is closest of all but is excluded by the tag.
		"auth":              {1.0, 0.0},
		"close to auth":     {0.9, 0.1},
		"nearer to auth":    {0.5, 0.5},
		"closest but excluded": {1.0, 0.02},
	}})

	// Two tagged learnings (the tag-filtered set) and one un-tagged
	// learning whose vector is closest to the query — the D6 trap.
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "nearer to auth", Provenance: ProvenanceTold, Tags: []string{"auth"}})
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "close to auth", Provenance: ProvenanceTold, Tags: []string{"auth"}})
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "closest but excluded", Provenance: ProvenanceTold, Tags: []string{"other"}})

	// Embed the learnings into the on-demand vec0.
	if _, err := l.EmbedPending(context.Background(), ScopeProject, "/repo"); err != nil {
		t.Fatalf("embed pending: %v", err)
	}

	// Query the tag-filtered set, ranked by "auth".
	res, err := l.Query(context.Background(), QueryFilter{Scope: ScopeProject, ScopeKey: "/repo", Tags: []TagPattern{{Pattern: "auth"}}, QueryText: "auth"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// D6: the result is the tag-filtered set, ranked — never wider. The
	// un-tagged body is absent even though its vector is closest.
	if len(res) != 2 {
		t.Fatalf("the tag-filtered set must stay 2 — the embedding ranks within it, never widening it (D6): got %+v", res)
	}
	for _, lr := range res {
		if lr.Body == "closest but excluded" {
			t.Fatal("the un-tagged learning was pulled in by a similar vector — the embedding must rank within the tag-filtered set only (D6)")
		}
	}
	// Ranking: the body closest to "auth" is first.
	if res[0].Body != "close to auth" {
		t.Fatalf("the ranking did not put the closest body first — got %q first", res[0].Body)
	}
}

// TestNoRouteReturnsUnrankedSet — D5: with no route, Query returns the
// tag-filtered set unranked (the store's order), not an error. A missing
// route is the normal, working state.
func TestNoRouteReturnsUnrankedSet(t *testing.T) {
	l, _ := openLearnings(t)
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "first", Provenance: ProvenanceTold, Tags: []string{"auth"}})
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "second", Provenance: ProvenanceTold, Tags: []string{"auth"}})

	// No route is configured — a QueryText is ignored, the set is returned
	// in the store's order (created_at, id), not an error.
	res, err := l.Query(context.Background(), QueryFilter{Tags: []TagPattern{{Pattern: "auth"}}, QueryText: "auth"})
	if err != nil {
		t.Fatalf("query with no route must not error (D5 — no route is the normal state): %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("no-route query returned %d, want the 2 tag-filtered learnings", len(res))
	}
	if res[0].Body != "first" {
		t.Fatalf("without a route the set keeps its store order (created_at, id) — got %q first", res[0].Body)
	}
}
