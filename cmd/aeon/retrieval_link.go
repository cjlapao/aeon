//go:build !noretrieval

package main

// retrieval_link.go — S22 T6's single wiring point (D15, I10, P34, V13).
//
// This is THE entry-point exception the retrieval module-walk test pre-
// authorised: cmd/aeon is the only package outside internal/retrieval
// allowed to import it, and this file is the reason. It builds the one
// resolver function value over the real backend and hands it to BOTH doors
// — the `search` tool (tools.SetSearchResolver, which is what puts the tool
// in the registry) and the daemon's `search.query` method (Options.Search)
// — so "one implementation, two transports" is a wiring fact, not a
// convention. The noretrieval sibling is a no-op: there, nothing can ever
// wire the seam, and both doors are absent (D1's other half).

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/retrieval"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

func wireRetrievalSearch(st *store.Store, routes protocol.Routes, dopts *daemon.Options) {
	idx := retrieval.New(st)
	resolver := func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		// The embedding route is a route like any other (V16): whichever
		// configured route speaks the "embedding" purpose, chosen
		// deterministically by id. No such route is the D4 shape, not an
		// error — Search answers the keyword half and flips KeywordOnly,
		// and the tool result says so to the model.
		var route *model.Route
		var emb retrieval.Embedder
		if routes != nil {
			route = tools.EmbedRoute(routes)
		}
		if route != nil {
			emb = retrieval.ModelClientEmbedder{C: &model.Client{}}
		}
		fused, avail, err := idx.Search(ctx, root, corpus, query, route, emb, limit)
		if err != nil {
			// The refusal sentence (index-empty with its reason, D2) is
			// the answer — both doors pass it through verbatim.
			return search.SearchAnswer{}, err
		}
		hits := make([]search.SearchHit, 0, len(fused))
		for _, h := range fused {
			// Locations and score only; the mapping cannot invent text —
			// SearchHit has no field for any (D8).
			hits = append(hits, search.SearchHit{Source: h.Source, LineStart: h.LineStart, LineEnd: h.LineEnd, Score: h.Score})
		}
		ans := search.SearchAnswer{Hits: hits, KeywordOnly: avail.KeywordOnly}
		// T8: a stale index says so — the cheap signal only (rows stored
		// without vectors), because a query must not pay a tree walk for
		// honesty; the full per-corpus drift figures are status's job
		// (O4). V4 is why the query may answer anyway: results are
		// LOCATIONS, so a moved line self-corrects on the `read`.
		if n, err := idx.PendingChunkCount(root, corpus); err == nil && n > 0 {
			ans.Staleness = fmt.Sprintf("%d stored chunks still lack vectors (the last pass stopped half-done) — results are locations, so a read of the current file self-corrects a moved line", n)
		}
		return ans, nil
	}
	tools.SetSearchResolver(resolver)
	dopts.Search = resolver

	// S22 T8: the status and deliberate-reindex doors, over the same
	// Index and the same route discipline (D15/P34 — `aeon index status`
	// and `aeon index rebuild` reach these identical closures through the
	// socket; there is no second reindex implementation anywhere).
	dopts.CorpusStatus = func(ctx context.Context, root string) (search.StatusAnswer, error) {
		var route *model.Route
		if routes != nil {
			route = tools.EmbedRoute(routes)
		}
		rep, err := idx.Status(ctx, root, route)
		if err != nil {
			return search.StatusAnswer{}, err
		}
		out := search.StatusAnswer{Root: rep.Root, EmbedRoute: rep.EmbedRoute, EmbedModel: rep.EmbedModel, EmbedDim: rep.EmbedDim}
		for _, c := range rep.Corpora {
			reasons := make([]string, 0, len(c.Reasons))
			for _, r := range c.Reasons {
				reasons = append(reasons, string(r))
			}
			out.Corpora = append(out.Corpora, search.CorpusStatus{
				Corpus: c.Corpus, Files: c.Files, StaleFiles: c.StaleFiles,
				PendingEmbed: c.PendingEmbed, Available: c.Available,
				Reason: strings.Join(reasons, ","),
				Drift:  mapDrift(c.Drift),
			})
		}
		return out, nil
	}
	dopts.Reindex = func(ctx context.Context, root, corpus, embedRoute string, force bool, progress func(done, total int)) (search.ReindexOutcome, error) {
		var route *model.Route
		if routes != nil {
			if embedRoute != "" {
				if rt, ok := routes.Route(embedRoute); ok {
					route = &rt
				} else {
					return search.ReindexOutcome{}, fmt.Errorf("retrieval.reindex refused route %q — no such route is configured (names must be stated, not guessed)", embedRoute)
				}
			} else {
				route = tools.EmbedRoute(routes)
			}
		}
		if route == nil {
			return search.ReindexOutcome{}, fmt.Errorf("retrieval.reindex refused at %s — no embedding route is configured (the no-provider state V19 names); a reindex embeds, and embedding without a route is not a call", root)
		}
		emb := retrieval.ModelClientEmbedder{C: &model.Client{}}
		out, err := idx.Reindex(ctx, root, corpus, route, emb, force, progress)
		if err != nil {
			return search.ReindexOutcome{}, err
		}
		return search.ReindexOutcome{
			Corpus: out.Corpus, Files: out.Files, ChunksWritten: out.ChunksWritten,
			Deleted: out.Deleted, Skips: out.Skips, Embedded: out.Embedded,
			EmbeddedUSD: out.EmbeddedUSD, CostKnown: out.CostKnown,
		}, nil
	}
	// S54 T2: the priced preview (SP2) — the SAME EstimateReindex walk the
	// query-side priced refusal runs (hybrid.go), over the SAME derived
	// route the status seam resolves, priced by the SAME three-state
	// priceDrift the status drift uses (est.Cost). It takes no embedder
	// by its own signature — a preview that embedded to price itself
	// would charge for the preview, and there is no parameter to pass one
	// through, which is the structural half of T2's zero-calls test.
	dopts.RetrievalPlan = func(ctx context.Context, root, corpus string) (*protocol.RetrievalPlanAnswer, error) {
		if !retrieval.ValidCorpus(corpus) {
			return nil, protocol.ErrPlatformUnknown
		}
		var route *model.Route
		if routes != nil {
			route = tools.EmbedRoute(routes)
		}
		est, err := idx.EstimateReindex(ctx, root, corpus, route)
		if err != nil {
			return nil, err
		}
		ans := &protocol.RetrievalPlanAnswer{
			ComponentRoot: est.Root, Corpus: est.Corpus,
			Files: est.Files, StaleFiles: est.StaleFiles, StaleChunks: est.Chunks,
			EstimatedInputTokens: est.ApproxTokens,
			EstimateBasis: "approximately 4 bytes per token over the chunk bodies — " +
				"the conventional English-text approximation, not a provider's tokenizer",
			EmbedModel: est.EmbedModel,
		}
		if sc := est.Cost(route); sc != nil {
			ans.Cost = &protocol.PlanCost{USD: sc.USD, State: sc.State, Reason: sc.Reason}
		}
		// The row, not the route, is where a dimension is known: the route
		// declares no dim (the embed call reports one, the row records
		// what was built with — status reads its EmbedDim from the same
		// line, reindex.go:109). Same source, same meaning: the row's
		// EmbedModel/EmbedDim are the history a pass would rebuild over,
		// so willReplaceEmbeddings is the corpus's own fact — a never-
		// embedded corpus CREATES vectors, it does not replace them, and
		// the answer says so false.
		if row, ok, err := st.RetrievalCorpusRow(root, corpus); err == nil && ok {
			ans.EmbedDim = row.EmbedDim
			ans.WillReplaceEmbeddings = row.EmbedModel != ""
		}
		return ans, nil
	}
}

// mapDrift maps the retrieval-local drift figures to the leaf's — the entry
// point is the one place the two may meet (the module-walk pre-authorised
// this file). The three-state Cost passes through unchanged; a nil
// retrieval cost (could-not-price) maps to a nil leaf cost, never a $0.00
// (O3).
func mapDrift(d *retrieval.StatusDrift) *search.DriftFigures {
	if d == nil {
		return nil
	}
	out := &search.DriftFigures{Changed: d.Changed, Added: d.Added, Removed: d.Removed}
	if d.Cost != nil {
		// The leaf owns the one Cost constructor (NewCost); the entry point
		// maps into it rather than writing the composite, so O1's guard over
		// the watched packages stays closed. A nil cost passes through nil —
		// never a fabricated $0.00 (O3).
		out.Cost = search.NewCost(d.Cost.USD, d.Cost.State, d.Cost.Reason)
	}
	return out
}
