//go:build !noretrieval

package retrieval

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Index is the real Backend (this file and availability.go are the whole
// implementation surface for T1): it owns no SQL — every statement is in
// internal/store/retrieval.go, the house pattern — and holds a store handle
// as the domain file beside it does.
//
// One Index serves every root it is asked about (V12 keeps roots apart by a
// COLUMN, and the store filters per query); the optionality boundary is this
// file's build tag, not an instance flag.
type Index struct {
	s *store.Store
}

// New wires the real backend to the store. There is no constructor option
// that turns retrieval "off": absence is a build configuration (V13), and an
// off switch inside the real build would put the empty-but-present shape
// back on the table — the shape §1 of the demo exists to forbid.
func New(s *store.Store) *Index { return &Index{s: s} }

// Built reports the presence. It is a constant true with a point: the
// consumer's branch is on the CONFIGURATION, and this is the only file that
// could lie about it.
func (x *Index) Built() bool { return true }

// EnsureCorpus creates the corpus row for (root, name) if absent (idempotent)
// and refuses anything the closed set does not name. The refusal is a
// *Reject-shaped error naming the thing refused (the house refusal rule): the
// corpus a caller named, what the corpora are, and WHY the third kind is not
// here yet — the validator rejecting 'concluded' naming Stage 5 is the same
// shape S11 used for the six tools that did not exist yet.
func (x *Index) EnsureCorpus(ctx context.Context, root, corpus string) error {
	return x.CreateCorpus(ctx, root, corpus, "")
}

// CreateCorpus is EnsureCorpus with the future door visible: kind "" (or
// "retrieved") is the only kind this slice writes, and "concluded" is
// REJECTED naming Stage 5 rather than silently accepted — the same shape
// S11 used for the six tools that did not exist yet. The rejection lives on
// the door a caller actually calls, not in a comment: when Stage 5 lands it
// edits this branch with the validator beside it, and until then nobody can
// create a belief-shaped corpus by typing the word.
func (x *Index) CreateCorpus(ctx context.Context, root, corpus, kind string) error {
	switch kind {
	case "", "retrieved":
	case "concluded":
		return fmt.Errorf(
			"retrieval: refused kind %q — not-yet-implemented: concluded memory is Stage 5's, its columns are reserved in migration 0040 and its validator (where a belief says told/observed/assumed and records N) does not exist yet",
			kind)
	default:
		return fmt.Errorf("retrieval: refused kind %q — the kinds are retrieved, and concluded (Stage 5, refused above)", kind)
	}
	if !ValidCorpus(corpus) {
		return fmt.Errorf(
			"retrieval: refused corpus %q — the corpora are %q and %q (V2); a concluded-memory corpus is Stage 5's, its columns are reserved in migration 0040 and its kind is not-yet-implemented here — naming it today would accept a belief-shaped corpus with no validator for the belief",
			corpus, CorpusCode, CorpusHistory)
	}
	_, err := x.s.EnsureRetrievalCorpus(root, corpus)
	return err
}

// SetCorpusEmbedding records the model and dimension that built the corpus's
// vectors (V6) — the record the mismatch gate reads. It goes through the
// store's own gate (a positive dimension, a named model) rather than a local
// one, so there is exactly one place that decides what a recorded embedding
// is.
func (x *Index) SetCorpusEmbedding(root, corpus, model string, dim int) error {
	c, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return fmt.Errorf("retrieval: refused embedding record — corpus %q at root %q does not exist; create it before claiming what built it", corpus, root)
	}
	return x.s.SetRetrievalCorpusEmbedding(c.ID, model, dim)
}

// EnsureVecTable creates the vec0 table for (root, corpus) at dim (idempotent,
// name-derived — see the store method for why the table is not a migration).
func (x *Index) EnsureVecTable(root, corpus string, dim int) (string, error) {
	if !ValidCorpus(corpus) {
		return "", fmt.Errorf("retrieval: refused vec table — corpus %q is not one of the two (O8 names its corpus; a vec table for no corpus indexes nothing)", corpus)
	}
	return x.s.EnsureRetrievalVecTable(root, corpus, dim)
}

// InsertChunk writes one chunk (V4's range, V8's hash, the indexed body).
// The body is indexed, never returned — Hit has no field for it.
func (x *Index) InsertChunk(root, corpus, source string, lineStart, lineEnd int, hash, body string) error {
	if !ValidCorpus(corpus) {
		return fmt.Errorf("retrieval: refused chunk — corpus %q is not one of the two (V2)", corpus)
	}
	c, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("retrieval: refused chunk for %s/%s — the corpus row does not exist; EnsureCorpus first, so a chunk never belongs to a corpus nobody created", root, corpus)
	}
	return x.s.InsertRetrievalChunk(c.ID, source, lineStart, lineEnd, hash, body)
}

// Availability answers the four reasons for one corpus, computed fresh on
// every call (V18's "the index reports its staleness" is T8; the reasons are
// T1's, and caching them is how a state stops being the index's own answer).
func (x *Index) Availability(ctx context.Context, root, corpus string, route *Route) (Availability, error) {
	if !ValidCorpus(corpus) {
		return Availability{}, fmt.Errorf("retrieval: refused availability for %q — name a corpus (O8): %q or %q", corpus, CorpusCode, CorpusHistory)
	}
	row, exists, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return Availability{}, err
	}
	chunks, err := x.s.RetrievalChunkCount(root, corpus)
	if err != nil {
		return Availability{}, err
	}
	return availabilityFor(exists, chunks, row, route), nil
}

// KeywordSearch runs the FTS5 half and returns results WITH the availability
// that labels them (D4): a caller can render "keyword-only — no embedding
// route configured" beside the hits from the same answer, because a
// degraded result that does not say so is indistinguishable from an empty
// index. A query that cannot run (a mismatch) returns no hits and the
// reasons that refuse it — refusing is Availability's job, not a silent
// empty.
func (x *Index) KeywordSearch(ctx context.Context, root, corpus, query string, route *Route, limit int) ([]Hit, Availability, error) {
	av, err := x.Availability(ctx, root, corpus, route)
	if err != nil {
		return nil, Availability{}, err
	}
	if !av.Available {
		return nil, av, nil
	}
	rows, err := x.s.KeywordSearchRetrieval(root, corpus, query, limit)
	if err != nil {
		return nil, av, err
	}
	hits := make([]Hit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, Hit{Source: r.Source, LineStart: r.LineStart, LineEnd: r.LineEnd, Score: r.Score})
	}
	return hits, av, nil
}

// VectorSearch is the second half of V3's query: the same chunks through
// vec0's nearest-neighbours. Availability governs (T1's answers arrive
// here before any SQL): with no embedding pass ever run there is no table,
// and the honest answer is empty-with-availability, never an error about a
// missing table — that absence is the index-empty / no-provider state the
// caller was already told.
func (x *Index) VectorSearch(root, corpus string, query []float32, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 10
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if !ok || row.EmbedDim == 0 {
		return nil, nil
	}
	rows, err := x.s.VectorSearchRetrieval(root, corpus, row.EmbedDim, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, Hit{Source: r.Source, LineStart: r.LineStart, LineEnd: r.LineEnd, Score: r.Score})
	}
	return hits, nil
}
