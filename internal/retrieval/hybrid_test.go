//go:build !noretrieval

package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// hybrid_test.go — V3 and D13 earned, not assumed: an exact identifier that
// vectors wander at and FTS5 nails; a concept FTS5 cannot see at all and
// vectors find; and across a QUERY SET both sources still contributing.
// The embedding seam here is a FAKE with stated rules — its vectors are a
// lookup table keyed by literal substrings, deterministic by construction.
// That is exactly what these gates can prove: the FUSION works whatever
// each list's quality is, because fusion ranks lists rather than weighing
// numbers. Model quality is the provider's claim, not this slice's.

// topicEmbedder is the fake whose opinions are a table:
// a text containing a registered marker embeds to that marker's vector; a
// registered QUERY string embeds to its table vector; anything else is a
// test bug (an error, visibly).
type topicEmbedder struct {
	marks  map[string][]float32 // marker substring -> vector
	quers  map[string][]float32 // exact query -> vector
	Calls  int
	Failed int
}

func (f *topicEmbedder) EmbedBatch(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	f.Calls++
	in := 0
	out := make([][]float32, len(texts))
	for i, t := range texts {
		in += len(t)
		if v, ok := f.quers[t]; ok {
			out[i] = v
			continue
		}
		matched := false
		for mark, v := range f.marks {
			if strings.Contains(t, mark) {
				out[i] = v
				matched = true
				break
			}
		}
		if !matched {
			// a chunk nobody mapped is a test bug, and a silent zero-vector
			// would let it retrieve as "far from everything" forever
			f.Failed++
			return nil, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: model.ReasonNotAttempted},
				fmt.Errorf("topic fake: no vector for text %q — register a marker; a zero vector is a lie that sorts", t)
		}
	}
	usage := model.Usage{InputTokens: model.IntPtr(in)}
	return out, usage, model.ComputeCost(usage, route.Prices), nil
}

// The fixture tree: three files, three roles. `x1-marker` etc. are the fake's
// hook words; every query is checked against the bodies so no FTS5 token
// they share can exist (the "FTS5 alone would have missed it" claim is
// then provable, not asserted).
const hyphenFiles = `package main

// resolveSocketPath is where the socket lives on disk.
func resolveSocketPath(base string) string {
	return base + "/rpc.sock"
}
`

const backoffFiles = `package main

// x1-marker
func clampBackoff(n int) int {
	sleep := n * 2
	if sleep > 60 {
		return 60
	}
	return sleep
}
`

const proseFiles = `# notes on the queue

The queue drains FIFO. Nothing here shares a token with the structural
identifier or the sleep concept; y2-marker keeps it findable.
`

func hybridFixture(t *testing.T) (*Index, string, *topicEmbedder, *model.Route) {
	t.Helper()
	x := openStoreT4(t)
	root := ensure2Root(t, x)
	embedSeedTree(t, x, root)

	fake := &topicEmbedder{
		marks: map[string][]float32{
			"x1-marker":         {0.99, 0.01, 0.0, 0.0},
			"y2-marker":         {0.98, 0.0, 0.02, 0.0},
			"resolveSocketPath": {0.0, 1.0, 0.0, 0.0}, // far from its own structural query
		},
		quers: map[string][]float32{
			qStructural: {0.0, 0.0, 1.0, 0.0},
			qConceptual: {0.97, 0.03, 0.0, 0.0},
			qProse:      {0.96, 0.0, 0.04, 0.0},
		},
	}
	in := 0.02
	route := &model.Route{ID: "test-embed", Provider: "openai", Model: "test-embedding", Prices: &model.Prices{InputUSDPerMTok: &in}}
	// (the availability view's Dim comes from the row, V6 — Search reads
	// it there; the route itself has no dimension field, and inventing one
	// would be a caller claim the row already answers)
	return x, root, fake, route
}

const (
	qStructural = "resolveSocketPath"
	qConceptual = "how does retry escalation work"
	qProse      = "queue drains FIFO"
)

// openStoreT4 opens a store without seeding any corpus: every hybrid test
// decides its own roots (a corpus is a decision, T1's door).
func openStoreT4(t *testing.T) *Index {
	t.Helper()
	return New(openStore(t))
}

func ensure2Root(t *testing.T, x *Index) string {
	t.Helper()
	root := t.TempDir()
	must(t, x.EnsureCorpus(context.Background(), root, CorpusCode), "ensure")
	return root
}

func embedSeedTree(t *testing.T, x *Index, root string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(root, "util.go"), []byte(hyphenFiles), 0o644), "seed util")
	must(t, os.WriteFile(filepath.Join(root, "backoff.go"), []byte(backoffFiles), 0o644), "seed backoff")
	must(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte(proseFiles), 0o644), "seed notes")
	_, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "index tree")
}

func embedAll(t *testing.T, x *Index, root string, fake *topicEmbedder, route *model.Route) {
	t.Helper()
	_, err := x.EmbedPending(context.Background(), root, CorpusCode, route, fake)
	must(t, err, "embed all")
}

// --- the gates ---------------------------------------------------------------

func TestStructuralQueryWonByFTS(t *testing.T) {
	x, root, fake, route := hybridFixture(t)
	embedAll(t, x, root, fake, route)

	// The vectors ALONE would have missed it: the fake's query vector is
	// the one corner of the space no real chunk points to — provably,
	// because the util chunk's own marker vector is elsewhere.
	vec, err := x.VectorSearch(root, CorpusCode, fake.quers[qStructural], 10)
	must(t, err, "vector list")
	for _, h := range vec {
		if h.Source == "util.go" {
			// not "absent" (vec0 returns them all at k=10) — but NEVER first
			if vec[0].Source == "util.go" {
				t.Fatal("the vector list put the identifier chunk first — the fake was configured to make it miss, and the gate must fail if it cannot")
			}
			break
		}
	}

	hits, _, err := x.Search(context.Background(), root, CorpusCode, qStructural, route, fake, 5)
	must(t, err, "search")
	if len(hits) == 0 {
		t.Fatal("no fused results for an exact identifier that is LITERALLY in a chunk")
	}
	if hits[0].Source != "util.go" || !hasTag(hits[0].Sources, "keyword") {
		t.Fatalf("top fused hit %+v — the exact identifier must arrive through FTS5 on top (V3)", hits[0])
	}
}

func TestConceptualQueryWonByVectors(t *testing.T) {
	x, root, fake, route := hybridFixture(t)
	embedAll(t, x, root, fake, route)

	// FTS5 ALONE would have missed it: no token of the query occurs in
	// backoff.go's body — checked HERE, not claimed. (The gate name is
	// stripped from the scanned text: the hunter cannot contain the
	// hunted, fourth time.)
	rawBody, err := os.ReadFile(filepath.Join(root, "backoff.go"))
	must(t, err, "read backoff")
	bodyTokens := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(string(rawBody)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		bodyTokens[tok] = true
	}
	// clampBackoff is ONE token (camelCase) — equal token, not substring,
	// is what FTS5 compares, so equality is the honest check.
	for _, w := range strings.Fields(strings.ToLower(qConceptual)) {
		if bodyTokens[w] {
			t.Fatalf("query token %q IS a token of the conceptual chunk's body — FTS5 would match and the gate proves nothing", w)
		}
	}
	kw, _, err := x.KeywordSearch(context.Background(), root, CorpusCode, qConceptual, &Route{Model: route.Model}, 10)
	must(t, err, "keyword list")
	for _, h := range kw {
		if h.Source == "backoff.go" {
			t.Fatalf("FTS5 found the conceptual chunk — the bodies and the query share a token (%q), so this gate would prove nothing", qConceptual)
		}
	}

	hits, _, err := x.Search(context.Background(), root, CorpusCode, qConceptual, route, fake, 5)
	must(t, err, "search")
	if len(hits) == 0 {
		t.Fatal("no fused results for the concept query")
	}
	if hits[0].Source != "backoff.go" || !hasTag(hits[0].Sources, "vector") {
		t.Fatalf("top fused hit %+v, want the backoff chunk via vectors (V3's other half)", hits[0])
	}
}

func TestBothAppearInFusedResults(t *testing.T) {
	// Across a QUERY SET, not one query — a single query proves one source
	// worked; the set is the only way to see the other did not stop
	// contributing (D13).
	x, root, fake, route := hybridFixture(t)
	embedAll(t, x, root, fake, route)

	bySource := map[string]int{}
	total := 0
	for _, q := range []string{qStructural, qConceptual, qProse} {
		hits, _, err := x.Search(context.Background(), root, CorpusCode, q, route, fake, 5)
		must(t, err, "search "+q)
		for _, h := range hits {
			for _, s := range h.Sources {
				bySource[s]++
			}
			total++
		}
	}
	if total == 0 {
		t.Fatal("the query set fused nothing")
	}
	if bySource["keyword"] == 0 || bySource["vector"] == 0 {
		t.Fatalf("a source stopped contributing: %+v — D13: fusion that lets one dominate is worse than either alone (it LOOKS like both)", bySource)
	}
}

func TestFusionDoesNotCompareScoresDirectly(t *testing.T) {
	// The method, from the file's bytes: only RANKS feed the sum. No
	// hit.Score term may appear in the fusion arithmetic — a weighted sum
	// of a cosine and a BM25 is "a number with no meaning that happens to
	// sort" (the task's own sentence).
	src := mustRead(t, "hybrid.go")
	body := src[srcIndex(t, src, "func fuseRanked"):]
	body = body[:srcIndex(t, body, "\n\n")]
	// Needles assembled at runtime — the scanner's own bytes must not
	// contain the words it hunts (the hunter cannot contain the hunted;
	// this bit me for the fourth time today).
	// Assembled needles (hunter/hunted, fourth time). What is banned is a
	// READ of a SOURCE Hit's Score — `hit.Score` / `h.Score` — the only way
	// an incomparable magnitude could enter the sum. What is NOT banned, and
	// what the tie-break below legitimately does: compare the FUSED score
	// (`out[i].Score`) — a rank-derived quantity is comparable with itself,
	// which is precisely the line RRF draws.
	for _, banned := range []string{"h" + "it.Score", "h." + "Score", "bm" + "25", "cos" + "ine", "dis" + "tance"} {
		if strings.Contains(body, banned) {
			t.Errorf("the fusion body reads a source score via %q — only ranks feed the sum; ranking the fused output is fine, reading a list's own magnitude is not", banned)
		}
	}
	if !strings.Contains(body, "1.0 / float64(k") {
		t.Fatal("fuseRanked does not sum reciprocal ranks — the method is not RRF as stated")
	}
	if !strings.Contains(src, "reciprocal"+" rank") {
		t.Error("the method is not STATED (the task demands it: state it and why it does not compare scores)")
	}
	// And behaviorally: a rank-0 hit from a list whose scores are ~0 still
	// beats its own rank-9 — ordering follows ranks, not magnitudes.
	a := Hit{Source: "a.go", LineStart: 1, LineEnd: 2, Score: 1e9}
	b := Hit{Source: "b.go", LineStart: 1, LineEnd: 2, Score: 1e-9}
	fused := fuseRanked(rankedList{tag: "vector", hits: []Hit{b, a}})
	if fused[0].Hit.Source != "b.go" {
		t.Fatalf("fusion followed the huge Score (%+v) — ranks are the only input", fused[0])
	}
}

func srcIndex(t *testing.T, hay, needle string) int {
	t.Helper()
	i := strings.Index(hay, needle)
	if i < 0 {
		t.Fatalf("%q not found", needle)
	}
	return i
}

func TestNoResultCarriesChunkText(t *testing.T) {
	// The PROPERTY over every result (D8, I5) — not per query. First the
	// structural half: the types have nowhere to put text.
	for _, typ := range []any{FusedHit{}, Hit{}} {
		rt := reflect.TypeOf(typ)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			low := strings.ToLower(f.Name)
			if strings.Contains(low, "body") || strings.Contains(low, "text") || strings.Contains(low, "content") {
				t.Fatalf("%s has field %q — V4: results are locations; the field is how the text leaks", rt.Name(), f.Name)
			}
		}
	}
	// Then the behavioral half: a token planted in every body appears in NO
	// rendered result.
	x, root, fake, route := hybridFixture(t)
	const token = "zyx-needle-body-token"
	must(t, os.WriteFile(filepath.Join(root, "needle.go"), []byte("package main\n\nfunc needle() { // "+token+"\n\tprintln(1)\n}\n"), 0o644), "seed needle")
	_, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "index needle")
	fake.marks[token] = []float32{0.5, 0.5, 0.0, 0.0}
	_, err = x.EmbedPending(context.Background(), root, CorpusCode, route, fake)
	must(t, err, "embed needle")

	for _, q := range []string{qStructural, qConceptual, qProse, "needle"} {
		hits, _, err := x.Search(context.Background(), root, CorpusCode, q, route, fake, 5)
		must(t, err, "search "+q)
		for _, h := range hits {
			rendered := fmt.Sprintf("%+v", h)
			if strings.Contains(rendered, token) {
				t.Fatalf("query %q rendered chunk text into %+v", q, h)
			}
			if raw, err := json.Marshal(h); err == nil && strings.Contains(string(raw), token) {
				t.Fatalf("query %q marshalled chunk text into %s", q, raw)
			}
		}
	}
}

func TestResultsAreLineRanges(t *testing.T) {
	x, root, fake, route := hybridFixture(t)
	embedAll(t, x, root, fake, route)
	hits, _, err := x.Search(context.Background(), root, CorpusCode, qStructural, route, fake, 5)
	must(t, err, "search")
	if len(hits) == 0 {
		t.Fatal("no results")
	}
	for _, h := range hits {
		if h.LineStart < 1 || h.LineEnd < h.LineStart {
			t.Fatalf("hit %+v is not a 1-indexed line range (T2's bounds)", h)
		}
		raw, err := os.ReadFile(filepath.Join(root, h.Source))
		if err != nil {
			t.Fatalf("hit names %q which is not under the root: %v", h.Source, err)
		}
		lines := strings.Count(string(raw), "\n") + 1
		if h.LineEnd > lines {
			t.Fatalf("hit %+v ends past the end of its file (%d lines)", h, lines)
		}
	}
}

func TestQueryMustNameItsCorpus(t *testing.T) {
	x, root, fake, route := hybridFixture(t)
	embedAll(t, x, root, fake, route)

	_, _, err := x.Search(context.Background(), root, "", qStructural, route, fake, 5)
	if err == nil || !strings.Contains(err.Error(), "NAMES its corpus") {
		t.Fatalf("an unnamed corpus was not refused: %v — no default (O8, I7)", err)
	}
	_, _, err = x.Search(context.Background(), root, "everything", qStructural, route, fake, 5)
	if err == nil || !strings.Contains(err.Error(), `"everything"`) {
		t.Fatalf("an invented corpus was not refused naming the value: %v", err)
	}
	// And no default lurks in the door: an empty query is refused too, not
	// treated as "match all".
	_, _, err = x.Search(context.Background(), root, CorpusCode, "", route, fake, 5)
	if err == nil {
		t.Fatal("an empty query returned results — it matches nothing and must say so")
	}
}

func TestEmptyIndexReportsIndexEmpty(t *testing.T) {
	// Not zero results — the availability reason (D2). A listing that
	// matches nothing must never read as "there was nothing there".
	x, ensure := embedIndex(t)
	_ = ensure
	root := ensure2Root(t, x)
	_, _, err := x.Search(context.Background(), root, CorpusCode, qStructural, nil, nil, 5)
	if err == nil || !strings.Contains(err.Error(), string(ReasonIndexEmpty)) {
		t.Fatalf("an unindexed corpus answered %v — it must answer the REASON (D2), never a confident empty list", err)
	}
}
