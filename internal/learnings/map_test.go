package learnings

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// Two tight clusters far apart in a 4-dimensional space: the projection must put each cluster's members
// nearer each other than to the other cluster, the links must stay inside a cluster, and the answer must be
// the same on every call.
func clusterVectors() ([]int64, [][]float32) {
	ids := []int64{1, 2, 3, 4, 5, 6}
	vecs := [][]float32{
		{1, 0.05, 0, 0}, {0.98, 0.1, 0, 0}, {1, 0, 0.08, 0},
		{0, 0, 1, 0.05}, {0, 0.05, 0.97, 0.1}, {0, 0, 1, 0},
	}
	return ids, vecs
}

func dist(a, b MapPoint) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

func TestProjectVectorsSeparatesClusters(t *testing.T) {
	ids, vecs := clusterVectors()
	pts, links := ProjectVectors(ids, vecs, 2)
	if len(pts) != 6 {
		t.Fatalf("points = %d, want 6", len(pts))
	}
	within := dist(pts[0], pts[1])
	across := dist(pts[0], pts[3])
	if !(within < across) {
		t.Errorf("a cluster's members (%.1f apart) are not nearer than the other cluster (%.1f)", within, across)
	}
	if len(links) == 0 {
		t.Fatal("no links — every learning has a near neighbour here")
	}
	for _, l := range links {
		if (l.A <= 3) != (l.B <= 3) {
			t.Errorf("link %d–%d crosses the clusters (similarity %.2f)", l.A, l.B, l.Similarity)
		}
		if l.Similarity < MinMapSimilarity {
			t.Errorf("link %d–%d at %.2f is under the floor", l.A, l.B, l.Similarity)
		}
	}
}

func TestProjectVectorsIsDeterministic(t *testing.T) {
	ids, vecs := clusterVectors()
	a, la := ProjectVectors(ids, vecs, 3)
	b, lb := ProjectVectors(ids, vecs, 3)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("point %d differs between runs: %v vs %v", i, a[i], b[i])
		}
	}
	if len(la) != len(lb) {
		t.Fatalf("link count differs between runs: %d vs %d", len(la), len(lb))
	}
}

func TestProjectVectorsStaysInTheScale(t *testing.T) {
	ids, vecs := clusterVectors()
	pts, _ := ProjectVectors(ids, vecs, 2)
	for _, p := range pts {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.Abs(p.X) > mapScale+0.01 || math.Abs(p.Y) > mapScale+0.01 {
			t.Errorf("point %d at (%v, %v) is outside ±%v", p.ID, p.X, p.Y, mapScale)
		}
	}
}

func TestProjectVectorsRefusesNothingAndInventsNothing(t *testing.T) {
	if pts, links := ProjectVectors(nil, nil, 3); pts != nil || links != nil {
		t.Errorf("no vectors gave points %v links %v", pts, links)
	}
}

func TestMapBasisSaysWhatIsMissing(t *testing.T) {
	if got := noVectorsBasis(false); got == "" || got == noVectorsBasis(true) {
		t.Errorf("the no-route and the can-embed bases must differ and be spoken: %q", got)
	}
	r := &MapResult{Dim: 4, Total: 10, Embedded: 6, Omitted: 0}
	if b := projectedBasis(r); !contains(b, "4 of 10") && !contains(b, "10") {
		t.Errorf("the basis does not account for the learnings without vectors: %q", b)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The whole path: learnings embedded into the real vec0, read back through the store, positioned. The
// learnings whose vectors were never stored are counted, not placed.
func TestMapReadsStoredVectorsAndCountsTheRest(t *testing.T) {
	l, _ := openLearnings(t)
	route := &model.Route{ID: "embed", Provider: "openai", Model: "fake-embed"}
	l.SetRoute(route, fakeEmbedder{table: map[string][]float32{
		"alpha one": {1, 0.05, 0, 0}, "alpha two": {0.98, 0.1, 0, 0}, "alpha three": {1, 0, 0.08, 0},
		"beta one": {0, 0, 1, 0.05}, "beta two": {0, 0.05, 0.97, 0.1},
	}})
	for _, body := range []string{"alpha one", "alpha two", "alpha three", "beta one", "beta two"} {
		mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: body, Provenance: ProvenanceTold})
	}
	if _, err := l.EmbedPending(context.Background(), ScopeProject, "/repo"); err != nil {
		t.Fatalf("embed pending: %v", err)
	}
	// A sixth learning arrives after the pass: it has no vector, and the map must say so.
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "late arrival", Provenance: ProvenanceTold})

	res, err := l.Map(BoardFilter{}, 0)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if !res.Projected || res.Total != 6 || res.Embedded != 5 || len(res.Points) != 5 || res.Dim != 4 {
		t.Fatalf("map = projected %v total %d embedded %d points %d dim %d, want true 6 5 5 4", res.Projected, res.Total, res.Embedded, len(res.Points), res.Dim)
	}
	if !res.CanEmbed {
		t.Error("a route is configured, so the answer must say the rest CAN be embedded")
	}
	if !strings.Contains(res.Basis, "1 of 6") {
		t.Errorf("the basis does not say one learning is not on the map: %q", res.Basis)
	}
	for _, p := range res.Points {
		if p.ID == 6 {
			t.Error("the learning with no vector was given a position")
		}
	}
}

func TestMapWithNoVectorsSaysWhy(t *testing.T) {
	l, _ := openLearnings(t)
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: "never embedded", Provenance: ProvenanceTold})
	res, err := l.Map(BoardFilter{}, 0)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if res.Projected || len(res.Points) != 0 || res.Total != 1 || res.Embedded != 0 {
		t.Fatalf("a store with no vectors drew a map: %+v", res)
	}
	if !strings.Contains(res.Basis, "no embedding route") {
		t.Errorf("the no-route state is not spoken: %q", res.Basis)
	}
}
