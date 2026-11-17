//go:build !noretrieval

package retrieval

// T8 — what reindexing is, what it costs, and what it refuses.
//
// The three properties here answer one tension: correctness says a mismatch
// must stop the query (V6: comparing vectors across embedding spaces
// returns plausible, wrong results — the worst failure a retrieval system
// has), and cost says nothing may spend money to fix it unasked (V10:
// indexing a large repository costs real money). V17 settles it as BOTH:
// refuse the query, PRICE the reindex, reindex NOTHING. The estimate is
// labelled an estimate with its basis (O3) — a figure presented as exact
// for work not yet done is the same class of error as a confident total
// over a `partial` cost.
//
// And the pass that does exist never delays a step (V18/D10): the loop
// triggers it after a write, it runs in the background, and a step against
// a slightly stale index is acceptable precisely because V4 made results
// LOCATIONS — a stale hit points at a line that moved and the `read` of
// the current file self-corrects, where a quoted chunk would have been
// silently wrong.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// StatusCorpus and StatusReport are the assembled figures `aeon index
// status` and `retrieval.status` answer with — per corpus (O4), with the
// embedding identity stated because staleness without it is half a
// sentence: an index built by another model is UNUSABLE, not merely stale.
type StatusCorpus struct {
	Corpus       string
	Files        int
	StaleFiles   int
	PendingEmbed int
	Available    bool
	KeywordOnly  bool
	Reasons      []Reason
	Drift        *StatusDrift
}

type StatusReport struct {
	Root       string
	Corpora    []StatusCorpus
	EmbedRoute string
	EmbedModel string
	EmbedDim   int
}

// StatusCost is the retrieval-local three-state price (O3/M30): known,
// partial, or unknown. The entry point maps it to the leaf's search.Cost
// (this package never imports the leaf). USD is nil exactly when no figure
// may be shown — an unmetered route contributes no number, never a
// fabricated $0.00, which would read as "free".
type StatusCost struct {
	USD    *float64
	State  string // "known" | "partial" | "unknown"
	Reason string
}

// StatusDrift is what a reindex pass would change in a corpus, and what it
// would cost — the figures G1 kept out of the status. The entry point maps
// it to the leaf's search.DriftFigures. It is computed from the SAME walk the
// status reads (reconcileCode, comparison-only), so the cost cannot disagree
// with the work it prices.
type StatusDrift struct {
	Changed int
	Added   int
	Removed int
	Cost    *StatusCost
}

// Status assembles both corpora's figures for one root. The availability
// half reuses availabilityFor with Search's self-referential view (dim from
// the row, model from the route): a dimension mismatch can only be
// MEASURED by the wire (V6), so status names what is recorded and refuses
// on what is comparable — the same honesty at the status door as at the
// query door.
func (x *Index) Status(ctx context.Context, root string, route *model.Route) (*StatusReport, error) {
	rep := &StatusReport{Root: root}
	if route != nil {
		rep.EmbedRoute, rep.EmbedModel = route.ID, route.Model
	}
	for _, corpus := range []string{CorpusCode, CorpusHistory} {
		f, err := x.Staleness(ctx, root, corpus)
		if err != nil {
			return nil, err
		}
		sc := StatusCorpus{Corpus: corpus, Files: f.Files, StaleFiles: f.StaleFiles, PendingEmbed: f.PendingEmbed}
		row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
		if err != nil {
			return nil, err
		}
		chunks := f.Files // figures already read the store; availability wants the chunk count
		if ok {
			chunks, err = x.s.RetrievalChunkCount(root, corpus)
			if err != nil {
				return nil, err
			}
			if corpus == CorpusCode && rep.EmbedDim == 0 {
				rep.EmbedDim = row.EmbedDim // recorded dimension, stated once
			}
		}
		view := &Route{}
		if route != nil {
			view.Model = route.Model
			view.Dim = row.EmbedDim
		}
		avail := availabilityFor(ok && chunks > 0, chunks, row, view)
		sc.Available, sc.KeywordOnly, sc.Reasons = avail.Available, avail.KeywordOnly, avail.Reasons
		// The cost the status would not otherwise say (G1): what a reindex pass
		// would change and cost. Only the code corpus walks the tree; the
		// history corpus's figures are its own rows (O4) and its pending embed
		// is a separate figure, so its Drift stays nil — never a $0.00.
		if corpus == CorpusCode {
			drift, err := x.driftCode(ctx, root, corpus, route)
			if err != nil {
				return nil, err
			}
			sc.Drift = drift
		}
		rep.Corpora = append(rep.Corpora, sc)
	}
	return rep, nil
}

// driftCode walks the code corpus — the SAME comparison-only walk the status
// reads (reconcileCode, apply=false) — and answers what a reindex pass would
// change in it and what it would cost. The split is honest: Removed is a
// source the walk found vanished (a stored file no longer on disk); Added is
// an on-disk source the store has never held; Changed is an on-disk source
// the store holds and whose content moved. A source the gate skipped (its
// mtime matched — assumed fresh) is not a change, so it does not count.
// Cost is three-state (O3); it asks the SAME pricing function the real pass
// and the estimate use, so the figure cannot disagree with the work it
// prices.
func (x *Index) driftCode(ctx context.Context, root, corpus string, route *model.Route) (*StatusDrift, error) {
	d := &StatusDrift{}
	// The stored sources: an on-disk stale source in this set is a Change,
	// out of it an Add. Read once — a per-file query would be a second walk.
	stored, _, err := x.storedSources(root, corpus)
	if err != nil {
		return nil, err
	}
	storedSet := map[string]bool{}
	for _, s := range stored {
		storedSet[s] = true
	}
	var tokens int
	_, err = x.reconcileCode(ctx, root, corpus, false, func(rel string, plan *StalePlan) {
		if plan == nil {
			d.Removed++ // vanished: a stored source the walk found on no disk
			return
		}
		if len(plan.Write) > 0 || len(plan.Delete) > 0 {
			if storedSet[rel] {
				d.Changed++
			} else {
				d.Added++
			}
			for _, ch := range plan.Write {
				tokens += len(ch.Body) / bytesPerToken
			}
		}
	})
	if err != nil {
		return nil, err
	}
	// A clean corpus (no change, no add, no removal) has no drift to report —
	// not a $0.00 (O3: a no-op pass costs nothing to say, and a zero would
	// read as "free" for a route that may never be called). The status shows
	// nothing, which is the honest answer for a pass that would change
	// nothing.
	if d.Changed == 0 && d.Added == 0 && d.Removed == 0 {
		return nil, nil
	}
	d.Cost = priceDrift(tokens, route)
	return d, nil
}

// priceDrift turns a pass's tokens into a three-state cost (O3): known when
// the route prices, unknown (with a reason in words) when it does not —
// never a fabricated $0.00, which would read as "free" for a route nobody
// configured a price for. It asks the SAME pricing function the real pass
// and the estimate use (I8/N9), so the figure cannot disagree with the work
// it prices.
func priceDrift(tokens int, route *model.Route) *StatusCost {
	if route == nil {
		return &StatusCost{State: "unknown", Reason: "no embedding route to price the pass"}
	}
	cost := model.ComputeCost(model.Usage{InputTokens: &tokens}, route.Prices)
	switch cost.State {
	case model.CostKnown:
		return &StatusCost{State: "known", USD: &cost.USD}
	case model.CostPartial:
		// some counts arrived; the floor is the figure, the reason names the
		// rest. USD carries the floor (a partial is a lower bound, stated).
		return &StatusCost{State: "partial", USD: &cost.USDAtLeast}
	default:
		reason := cost.Reason
		if reason == "" {
			reason = "the embedding route carries no price"
		}
		return &StatusCost{State: "unknown", Reason: reason}
	}
}

// hasMismatch is the pricing gate: only the two embedding-space mismatches
// get a money paragraph. An empty index is priced by the CLI's build prompt,
// not by a query refusal — a query refused because nothing exists yet has
// no previous build to price against.
func hasMismatch(reasons []Reason) bool {
	for _, r := range reasons {
		if r == ReasonModelMismatch || r == ReasonDimensionMismatch {
			return true
		}
	}
	return false
}

// ReindexEstimate is the priced refusal's arithmetic — every figure derived
// from the SAME walk the real pass runs (reconcileCode), because an estimate
// computed by a second, cheaper walker is a number that disagrees with the
// work it prices.
type ReindexEstimate struct {
	Root   string
	Corpus string

	Files      int // files the walk considered
	StaleFiles int // files whose chunks would be written or pruned by the pass
	Chunks     int // chunks the pass would send to the embedder

	// ApproxTokens is bytes/4 over the chunk bodies — the basis is STATED in
	// Render, because "approximately" with an unstated basis is a guess
	// wearing a decimal point (O3).
	ApproxTokens int

	// USD is nil exactly when no figure may be shown: an unmetered route
	// (no price configured) contributes NO number, and Render says so
	// rather than printing $0.00, which would read as "free" (M30).
	USD       *float64
	Unmetered bool

	EmbedModel string // the route this pass would embed with
	RouteID    string // for the "To proceed" line
}

// bytesPerToken is the estimate's stated tokenisation basis: a quarter of a
// byte per token is the conventional English-text approximation. It is not
// a provider's tokenizer — the figure is an estimate and the label says so;
// what it must never be is unlabelled.
const bytesPerToken = 4

// Render is the refusal's money paragraph, in the shape the brief walked
// (O3): labelled, with its basis, and stating that nothing was done.
func (e *ReindexEstimate) Render() string {
	var b strings.Builder
	b.WriteString("Nothing was reindexed. ")
	if e.USD != nil {
		fmt.Fprintf(&b, "A reindex would cost approximately $%.2f", *e.USD)
	} else {
		b.WriteString("A reindex cannot be priced: the embedding route carries no price")
	}
	fmt.Fprintf(&b, " (estimate: %d files, ~%d chunks, ~%d tokens at %d bytes/token",
		e.Files, e.Chunks, e.ApproxTokens, bytesPerToken)
	if e.Unmetered {
		b.WriteString(", unmetered portions excluded — no figure for the unpriced route")
	}
	b.WriteString(").")
	return b.String()
}

// Cost prices the ESTIMATE in the three states (S54 T2, SP2/S33): the
// figure is carried only when known, a partial carries its floor stated,
// an unknown carries a sentence — never a bare number and never the $0.00
// M30 forbids. It is one line over the SAME priceDrift the status drift
// uses, on the SAME token count this walk produced: an estimate priced by
// a second formula is a figure that can disagree with the work it prices
// (I8/N9), and Render's coarser unmetered sentence stays Render's — the
// refusal's paragraph was written before the three-state floor existed
// and says the same truth at the same precision it always has.
func (e *ReindexEstimate) Cost(route *model.Route) *StatusCost {
	return priceDrift(e.ApproxTokens, route)
}

// EstimateReindex walks and compares and writes NOTHING (D9: the refusal
// that had already indexed anything would be the lie V17 exists to prevent
// — TestMismatchRefusesAndReindexesNothing keeps the spy at zero). The
// corpus row is not required: pricing a build nobody has started yet is
// `aeon index build`'s first question.
func (x *Index) EstimateReindex(ctx context.Context, root, corpus string, route *model.Route) (*ReindexEstimate, error) {
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused to price corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	if root == "" {
		return nil, fmt.Errorf("retrieval: refused to price an index for an empty root (V12)")
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("retrieval: refused to price %s: %w", root, err)
	}
	if corpus == CorpusHistory {
		return x.estimateHistory(root, corpus, route)
	}
	return x.estimateCode(ctx, root, corpus, route)
}

func (x *Index) estimateCode(ctx context.Context, root, corpus string, route *model.Route) (*ReindexEstimate, error) {
	est := &ReindexEstimate{Root: root, Corpus: corpus}
	if route != nil {
		est.EmbedModel, est.RouteID = route.Model, route.ID
	}
	_, err := x.reconcileCode(ctx, root, corpus, false, func(_ string, plan *StalePlan) {
		est.Files++      // the walk's own count — a re-walk would be a second truth
		if plan == nil { // vanished: staleness, not embedding work
			est.StaleFiles++
			return
		}
		if len(plan.Write) > 0 || len(plan.Delete) > 0 {
			est.StaleFiles++
		}
		for _, ch := range plan.Write {
			est.Chunks++
			est.ApproxTokens += len(ch.Body) / bytesPerToken
		}
	})
	if err != nil {
		return nil, err
	}
	priceEstimate(est, route)
	return est, nil
}

// walkCount reuses the same comparison walk to count files the walk
// considered (indexed + moved); a second counter would be a second truth.
func (x *Index) walkCount(ctx context.Context, root, corpus string) (int, error) {
	res, err := x.reconcileCode(ctx, root, corpus, false, nil)
	if err != nil {
		return 0, err
	}
	stale := 0
	for _, s := range res.Skips {
		if strings.HasPrefix(s.Reason, "file vanished since the last index") {
			stale++
		}
	}
	return res.Indexed + stale, nil
}

// estimateHistory prices the history corpus: its chunks are rows written by
// the loop (handoffs, chat, indexed files), and its embedding work is
// exactly the chunks that still have no vector. No tree walk — a history
// figure moved by a source file changing would be the O4 wrong.
func (x *Index) estimateHistory(root, corpus string, route *model.Route) (*ReindexEstimate, error) {
	est := &ReindexEstimate{Root: root, Corpus: corpus}
	if route != nil {
		est.EmbedModel, est.RouteID = route.Model, route.ID
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if !ok {
		priceEstimate(est, route)
		return est, nil // never built: zero known work, stated as zero, not as absent
	}
	pending, err := x.pendingChunks(row)
	if err != nil {
		return nil, err
	}
	for _, ch := range pending {
		est.Chunks++
		est.ApproxTokens += len(ch.Body) / bytesPerToken
	}
	est.Files = len(x.corpusSources(row.ID))
	est.StaleFiles = est.Files // every history row still lacking a vector is staleness of its source
	priceEstimate(est, route)
	return est, nil
}

// priceEstimate turns tokens into a figure or into the honest absence.
// Embedding is billed as INPUT tokens, so the route's input price is the
// only price the estimate may use; a route without one contributes no
// number (M30) — $0.00 would read as "free", which nobody configured.
func priceEstimate(est *ReindexEstimate, route *model.Route) {
	if route == nil {
		est.USD, est.Unmetered = nil, true
		return
	}
	// The arithmetic belongs to internal/model (I8/N9 — the same rule
	// T5's pass obeys): the estimate asks the SAME pricing function the
	// real calls use, with the estimated tokens stood in for usage.
	tokens := est.ApproxTokens
	cost := model.ComputeCost(model.Usage{InputTokens: &tokens}, route.Prices)
	if cost.State == model.CostKnown {
		usd := cost.USD
		est.USD = &usd
		return
	}
	est.USD, est.Unmetered = nil, true
}

// --- the deliberate pass ----------------------------------------------------

// ReindexOutcome mirrors the shared shape; kept local so this package never
// imports the leaf it serves (the entry point maps the two).
type ReindexOutcome struct {
	Corpus        string
	Files         int
	ChunksWritten int
	Deleted       int
	Skips         int
	Embedded      int
	EmbeddedUSD   *float64
	CostKnown     bool
}

// Reindex runs one DELIBERATE pass: reconcile the code corpus (or take the
// history corpus's rows as they stand) and embed what has no vector. It is
// the identical function `aeon index rebuild` and `retrieval.reindex` call
// (D15/P34) — there is no second reindex to forget to keep in step.
//
// The corpus row is ensured here because a deliberate rebuild may be the
// first index that corpus ever had; an automatic path never reaches this
// function at all (V21), so no auto-fire can mint a corpus row as a
// side effect.
func (x *Index) Reindex(ctx context.Context, root, corpus string, route *model.Route, emb Embedder, force bool, progress func(done, total int)) (*ReindexOutcome, error) {
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused to reindex corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	if route == nil || route.Model == "" {
		return nil, fmt.Errorf("retrieval: refused to reindex at %s — a pass needs a route with a model (embedding without a route is the no-provider state V19 names, not a call)", root)
	}
	if emb == nil {
		return nil, fmt.Errorf("retrieval: refused to reindex at %s — no Embedder seam (retrieval sends no requests of its own)", root)
	}
	if root == "" {
		return nil, fmt.Errorf("retrieval: refused to reindex an empty root (V12)")
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("retrieval: refused to reindex %s: %w", root, err)
	}
	if err := x.EnsureCorpus(ctx, root, corpus); err != nil {
		return nil, err
	}
	// V17 at the pass itself, not only at the query: embedding onto an
	// index recorded under another model would make the corpus answer with
	// two spaces' vectors at once — contamination, not staleness. Refused
	// and priced; only a caller who RESTATED the intent (force — the CLI's
	// --embed-route move) wipes and rebuilds, and the wipe is loud in the
	// same sentence.
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if ok && row.EmbedModel != "" && row.EmbedModel != route.Model {
		if !force {
			est, estErr := x.EstimateReindex(ctx, root, corpus, route)
			msg := fmt.Sprintf("retrieval: refused to reindex %s at %s — the index was built with %q and route %q is %q; a half-reindexed corpus answers with two embedding spaces at once, which V6 names the worst failure here",
				corpus, root, row.EmbedModel, route.ID, route.Model)
			if estErr == nil {
				msg += "\n\n  " + est.Render()
				msg += fmt.Sprintf("\n\n  To proceed:  aeon index rebuild --corpus %s --embed-route %s", corpus, route.ID)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if _, err := x.s.DeleteRetrievalChunksForCorpus(row.ID); err != nil {
			return nil, err
		}
		if row.EmbedDim > 0 {
			if err := x.s.DropRetrievalVecTable(row.ComponentRoot, row.Name, row.EmbedDim); err != nil {
				return nil, err
			}
		}
		if err := x.s.ResetRetrievalCorpusEmbedding(row.ID); err != nil {
			return nil, err
		}
	}
	out := &ReindexOutcome{Corpus: corpus}
	if corpus == CorpusCode {
		// The progress total is a first comparison pass — an honest total
		// before the work, not a percentage that re-denominates as it goes.
		total, err := x.walkCount(ctx, root, corpus)
		if err != nil {
			return nil, err
		}
		done := 0
		res, err := x.reconcileCode(ctx, root, corpus, true, func(_ string, _ *StalePlan) {
			done++
			if progress != nil {
				progress(done, total)
			}
		})
		if err != nil {
			return nil, err
		}
		out.Files, out.ChunksWritten, out.Deleted, out.Skips = total, res.Written, res.Deleted, len(res.Skips)
	} else {
		// History: the tree's prose files are (re)indexed by their own door
		// as the loop writes them; a deliberate history reindex reconciles
		// every prose file it can see and then embeds what lacks a vector.
		mds, err := markdownFiles(root)
		if err != nil {
			return nil, err
		}
		for i, m := range mds {
			rel, err := filepath.Rel(root, m)
			if err != nil {
				continue
			}
			if _, err := x.IndexHistoryFile(ctx, root, rel); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(i+1, len(mds))
			}
		}
		out.Files = len(mds)
	}
	pass, err := x.EmbedPending(ctx, root, corpus, route, emb)
	if err != nil {
		return nil, err
	}
	out.Embedded = pass.Embedded
	out.CostKnown = pass.Cost.State == model.CostKnown
	if pass.Cost.State == model.CostKnown {
		usd := pass.Cost.USD
		out.EmbeddedUSD = &usd
	}
	return out, nil
}

// markdownFiles lists the prose files of the tree with the walk's skip
// discipline (.git never descended, symlinks never followed).
func markdownFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skipped corners are named by their counters upstream
		}
		if d.IsDir() {
			if path != root && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") || strings.EqualFold(filepath.Ext(path), ".markdown") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("retrieval: walk for prose files at %s: %w", root, err)
	}
	return out, nil
}

// --- staleness, per corpus (O4) ---------------------------------------------

// CorpusFigures are one corpus's staleness figures — the retrieval-local
// shape the entry point maps into the leaf's CorpusStatus. Files counts what
// the corpus holds or saw; StaleFiles says how much of it disagrees with
// the tree (code) or still lacks a vector (history); PendingEmbed says a
// pass stopped half-done, which is staleness, not health.
type CorpusFigures struct {
	Corpus       string
	Files        int
	StaleFiles   int
	PendingEmbed int
}

// Staleness computes the figures for ONE corpus. It is the per-corpus rule
// (O4) made structural: neither corpus's branch consults the other's data,
// so a code change cannot move a history figure by construction, not by
// convention (TestStalenessIsPerCorpus).
func (x *Index) Staleness(ctx context.Context, root, corpus string) (*CorpusFigures, error) {
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused staleness for corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	if corpus == CorpusHistory {
		return x.stalenessHistory(root, corpus)
	}
	return x.stalenessCode(ctx, root, corpus)
}

func (x *Index) stalenessCode(ctx context.Context, root, corpus string) (*CorpusFigures, error) {
	f := &CorpusFigures{Corpus: CorpusCode}
	_, err := x.reconcileCode(ctx, root, corpus, false, func(_ string, plan *StalePlan) {
		f.Files++
		if plan == nil || len(plan.Write) > 0 || len(plan.Delete) > 0 {
			f.StaleFiles++
		}
	})
	if err != nil {
		return nil, err
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if ok {
		pending, err := x.pendingChunks(row)
		if err != nil {
			return nil, err
		}
		f.PendingEmbed = len(pending)
	}
	return f, nil
}

func (x *Index) stalenessHistory(root, corpus string) (*CorpusFigures, error) {
	f := &CorpusFigures{Corpus: CorpusHistory}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if !ok {
		return f, nil // never built: zero rows, stated — the walk of a tree it does not consult would be the O4 wrong
	}
	pending, err := x.pendingChunks(row)
	if err != nil {
		return nil, err
	}
	f.PendingEmbed = len(pending)
	f.Files = len(x.corpusSources(row.ID))
	f.StaleFiles = pendingSources(pending)
	return f, nil
}

// pendingChunks is the same filter EmbedPending applies to resume (the rows
// answer, never a progress counter): chunks of the corpus with no vector.
func (x *Index) pendingChunks(row store.RetrievalCorpus) ([]pendingChunk, error) {
	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	if err != nil {
		return nil, err
	}
	have := map[int64]bool{}
	if row.EmbedDim > 0 {
		ids, err := x.s.VecChunkIDs(row.ComponentRoot, row.Name, row.EmbedDim)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			have[id] = true
		}
	}
	var out []pendingChunk
	for _, ch := range chunks {
		if have[ch.ID] {
			continue
		}
		out = append(out, pendingChunk{Source: ch.Source, Body: ch.Body})
	}
	return out, nil
}

// PendingChunkCount is the cheap staleness signal a query path may afford:
// rows stored without vectors, answered from the store alone (no tree
// walk). It is what lets a search answer say "the last pass stopped
// half-done" without paying a comparison per query; the full per-corpus
// drift figures belong to Staleness, which status calls deliberately.
func (x *Index) PendingChunkCount(root, corpus string) (int, error) {
	if !ValidCorpus(corpus) {
		return 0, fmt.Errorf("retrieval: refused pending-chunk count for corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil || !ok {
		return 0, err
	}
	pending, err := x.pendingChunks(row)
	if err != nil {
		return 0, err
	}
	return len(pending), nil
}

// pendingChunk is the estimate's view of one chunk still lacking a vector.
type pendingChunk struct {
	Source string
	Body   string
}

// corpusSources lists the distinct sources stored for a corpus row.
func (x *Index) corpusSources(corpusID int64) []string {
	chunks, err := x.s.RetrievalChunksForEmbedding(corpusID)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, ch := range chunks {
		if !seen[ch.Source] {
			seen[ch.Source] = true
			out = append(out, ch.Source)
		}
	}
	return out
}

func pendingSources(pending []pendingChunk) int {
	seen := map[string]bool{}
	n := 0
	for _, p := range pending {
		if !seen[p.Source] {
			seen[p.Source] = true
			n++
		}
	}
	return n
}
