//go:build !noretrieval

package retrieval

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// embed.go — the embedding pass, as a route (V16, M12): this package holds
// NO HTTP client, NO key read and NO cost computation (I8, N9, enforced by
// TestNoSecondProviderPath as a grep). The provider conversation belongs to
// internal/model — one client, one key door, one cost site — and this file's
// half is the INDEX half: which chunks still need vectors, how they are
// batched, what a failure leaves standing, and what gets recorded so T8 can
// refuse a mismatch.
//
// O5, the rule the whole pass is built around: a failure mid-index does not
// restart it. The indexed portion stands (every vector already stored is a
// row), the failure is recorded per chunk, and a retry RESUMES — "resume" is
// mechanical here, not a promise: the resume state is the rows themselves
// (chunks whose ids are absent from the vec0 table), so a retry after a
// crash, a process restart, or a laptop close picks up exactly where the
// wire failed. A large index that must restart on any failure is one that
// never completes on a flaky connection, and thirty thousand chunks is
// exactly where a flaky connection shows up.

// Embedder is the seam to the route. Production passes ModelClientEmbedder
// around the S18 *model.Client; tests pass a recorded-fixture fake. The
// signatures speak model types on purpose (TestEmbeddingUsesS14Routes
// asserts it by import and by shape): the cost that comes back IS S18's
// three-state Cost, computed by S18's ComputeCost at S18's single site —
// inherited, not rewritten (V10, D11).
type Embedder interface {
	EmbedBatch(ctx context.Context, route *model.Route, texts []string) (vectors [][]float32, usage model.Usage, cost model.Cost, err error)
}

// ModelClientEmbedder is the production Embedder: a thin wrapper over the
// model package's own Embed method. It holds no credential (the route's key is read
// inside model, by the door that always reads it), sends no request of its
// own, and prices nothing itself — it is the type that makes "embedding is
// a route" literal: to embed is to call the client with a Route.
type ModelClientEmbedder struct{ C *model.Client }

// Compile-time proof that the production seam is the S18 client, not a
// parallel path.
var _ Embedder = ModelClientEmbedder{}

func (e ModelClientEmbedder) EmbedBatch(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	if e.C == nil {
		return nil, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: model.ReasonNotAttempted},
			fmt.Errorf("retrieval: refused an embedding batch — the model client behind this seam is nil (the entry point wires ModelClientEmbedder with a live *model.Client)")
	}
	if route == nil {
		return nil, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: model.ReasonNotAttempted},
			fmt.Errorf("retrieval: refused an embedding batch with no route — embedding WITHOUT a route is the no-provider state (V19), not an embedding call")
	}
	return e.C.Embed(ctx, *route, texts)
}

// Batching bounds, stated (the task's own line: per-chunk calls over thirty
// thousand chunks is a rate-limit problem AND a cost problem). 96 chunks per
// call is well inside OpenAI-style request limits and small enough that one
// failed batch costs one retry, not an hour; 256 KiB of text per call bounds
// the request the same way the count cannot (chunks vary in size by orders
// of magnitude). Either bound that cuts a batch is the reason — a single
// bound tuned for one chunk shape fails the other shape.
const (
	maxEmbedBatchChunks = 96
	maxEmbedBatchBytes  = 256 * 1024
)

// ChunkFailure is the per-chunk record O5 demands: not "the pass failed at
// batch 2", but WHICH chunks failed and what the seam said. T8's status
// surface reads these; the retry that resumes does not need them (the rows
// are the resume state) but the human reporting the index does.
type ChunkFailure struct {
	Source    string
	LineStart int
	LineEnd   int
	Err       string
}

// EmbedPass is what one run over one corpus reports.
type EmbedPass struct {
	Embedded int
	Skipped  int // chunks that already had vectors (the resume's savings)
	Failures []ChunkFailure

	// Cost is S18's aggregate over the batches this pass sent: known + known
	// stays known; one batch whose usage never arrived makes the whole
	// aggregate unknown, NOT a too-low total presented as a right one
	// (P32 — the rollup rule S18 already enforces, reached by model.Cost.Add).
	Cost model.Cost

	// Model and Dim are what this pass recorded on the corpus row (V6):
	// either taken from the row (a continuing pass), or learned from the
	// first vector and stored here for T8 to compare against a route.
	Model string
	Dim   int
}

// EmbedPending embeds every chunk of the corpus that has no vector yet,
// through the route, and records model + dimension on the corpus row the
// first time (V6: recording is this task's; refusing on mismatch is T8's).
//
// The order of operations is the honesty of the result: the corpus row is
// stamped with (model, dim) BEFORE the first vector lands, from the route +
// the first measured vector, so a half-completed index still answers
// Availability truthfully — the vectors that exist were made by THAT route
// at THAT dimension, and the chunks lacking vectors are simply not
// vector-searchable yet (the keyword half keeps answering; the availability
// sentence does not overstate what the missing half would have said).
func (x *Index) EmbedPending(ctx context.Context, root, corpus string, route *model.Route, emb Embedder) (*EmbedPass, error) {
	if emb == nil {
		return nil, fmt.Errorf("retrieval: refused an embedding pass for %s at %s — no Embedder was given (retrieval sends no requests of its own; the entry point supplies the seam)", corpus, root)
	}
	if route == nil || route.Model == "" {
		return nil, fmt.Errorf("retrieval: refused an embedding pass for %s at %s — a pass needs a route with a model (embedding without a route is the no-provider state, not a call)", corpus, root)
	}
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("retrieval: refused an embedding pass at %s — corpus %q does not exist; ensure it first", root, corpus)
	}

	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	if err != nil {
		return nil, err
	}

	// The resume filter (O5): chunks whose ids already have vectors are not
	// re-embedded. This reads the rows, not a progress counter, so the
	// filter survives any crash — there is no progress state to lose.
	// (A dim of 0 on the row means "never embedded", so no table to ask.)
	have := map[int64]bool{}
	if row.EmbedDim > 0 {
		ids, err := x.s.VecChunkIDs(root, corpus, row.EmbedDim)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			have[id] = true
		}
	}
	var pending []store.EmbeddingChunk
	skippedByResume := 0
	for _, ch := range chunks {
		if have[ch.ID] {
			skippedByResume++
			continue
		}
		pending = append(pending, ch)
	}

	pass := &EmbedPass{Cost: model.Cost{State: model.CostKnown}, Skipped: skippedByResume}
	firstBatchSent := false

	// The stamp (V6): an already-recorded corpus is compared-by-T8 later;
	// this pass honours whatever the row says and asks for nothing new.
	modelName, dim := row.EmbedModel, row.EmbedDim
	if modelName == "" {
		// Unknown until the first vector comes back: the dimension is a
		// FACT of the wire, not a guess from the model name (V6 makes it
		// data precisely because "text-embedding-3-small" does not tell
		// you 1536 — the response does).
		modelName = route.Model
	}

	for _, batch := range embedBatches(pending) {
		texts := make([]string, len(batch))
		for i, ch := range batch {
			texts[i] = ch.Body
		}
		vectors, _, cost, err := emb.EmbedBatch(ctx, route, texts)
		pass.Cost = pass.Cost.Add(cost)
		if err != nil {
			// O5: the failure is PER CHUNK and the pass continues. Every
			// vector already stored stands; these chunks stay without
			// vectors, which is exactly the state a retry resumes from.
			for _, ch := range batch {
				pass.Failures = append(pass.Failures, ChunkFailure{
					Source: ch.Source, LineStart: ch.LineStart, LineEnd: ch.LineEnd, Err: err.Error(),
				})
			}
			continue
		}
		// First successful vector fixes the dimension (if the row did not
		// already know it) and stamps the row BEFORE any vec insert.
		if !firstBatchSent {
			firstBatchSent = true
			if dim == 0 {
				// The dimension is the length the batch AGREES on — not
				// simply the first vector's length. (Measured, T3: taking
				// vectors[0] meant one corrupt first vector stamped the
				// whole corpus at its wrong dimension and every good
				// vector in the batch was then refused against it. A
				// majority is the honest reading of "what geometry did
				// this route just answer in"; a lone dissent is
				// corruption, and it falls to the per-chunk gate below.)
				dim = modalDimension(vectors)
				if dim == 0 {
					// A zero-length first vector is refused as a batch
					// failure, not stamped: the row is not written with a
					// dimension nothing can query.
					for _, ch := range batch {
						pass.Failures = append(pass.Failures, ChunkFailure{
							Source: ch.Source, LineStart: ch.LineStart, LineEnd: ch.LineEnd,
							Err: "retrieval: refused the first vector — it has no dimensions, so the corpus row would record a geometry nothing can search",
						})
					}
					continue
				}
			}
			if row.EmbedModel == "" {
				if err := x.s.SetRetrievalCorpusEmbedding(row.ID, modelName, dim); err != nil {
					return pass, err
				}
				row.EmbedModel, row.EmbedDim = modelName, dim
			}
			if _, err := x.s.EnsureRetrievalVecTable(root, corpus, dim); err != nil {
				return pass, err
			}
		}
		for i, ch := range batch {
			if len(vectors[i]) != dim {
				// Mid-batch geometry drift is corruption from the provider;
				// refuse THIS chunk (per O5), let its batch-mates land.
				pass.Failures = append(pass.Failures, ChunkFailure{
					Source: ch.Source, LineStart: ch.LineStart, LineEnd: ch.LineEnd,
					Err: fmt.Sprintf("retrieval: refused a vector of %d dimensions where the corpus records %d", len(vectors[i]), dim),
				})
				continue
			}
			if err := x.s.UpsertRetrievalVec(root, corpus, dim, ch.ID, vectors[i]); err != nil {
				pass.Failures = append(pass.Failures, ChunkFailure{
					Source: ch.Source, LineStart: ch.LineStart, LineEnd: ch.LineEnd, Err: err.Error(),
				})
				continue
			}
			pass.Embedded++
		}
	}
	pass.Model, pass.Dim = modelName, dim
	return pass, nil
}

// embedBatches cuts pending chunks at either bound — maxEmbedBatchChunks or
// maxEmbedBatchBytes (of joined body) — whichever comes first, preserving
// store order. A single chunk larger than the byte bound still gets its own
// batch (it must be embedded SOMEHOW; dropping it silently would be the
// index losing a file to a formatting choice).
func embedBatches(pending []store.EmbeddingChunk) [][]store.EmbeddingChunk {
	var out [][]store.EmbeddingChunk
	var cur []store.EmbeddingChunk
	curBytes := 0
	for _, ch := range pending {
		if len(cur) > 0 && (len(cur) >= maxEmbedBatchChunks || curBytes+len(ch.Body) > maxEmbedBatchBytes) {
			out = append(out, cur)
			cur, curBytes = nil, 0
		}
		cur = append(cur, ch)
		curBytes += len(ch.Body)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// modalDimension returns the vector length the batch mostly agrees on (0 if
// every vector is empty). Ties are broken by FIRST agreement — with a
// provider this is not a statistics exercise, it is one number with an
// occasional corrupt outlier; anything fancier would be inventing precision
// the wire does not have.
func modalDimension(vectors [][]float32) int {
	counts := map[int]int{}
	var best, bestN int
	for _, v := range vectors {
		n := len(v)
		if n == 0 {
			continue
		}
		counts[n]++
		if counts[n] > bestN {
			best, bestN = n, counts[n]
		}
	}
	return best
}
