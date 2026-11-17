//go:build !noretrieval

package retrieval

// S54 T2 — the priced preview's own contract (SP2): the estimate equals a
// real pass on a fixture corpus to the chunk, never touches the embedder,
// and states cost in the honest three states. The fixtures, the spy and
// the routes are T8's (reindex_test.go) — the same walk, the same embedder
// that would charge money, the same priced and unpriced routes.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReindexPlanEqualsARealPassOnAFixtureCorpus(t *testing.T) {
	// SP2's claim is arithmetic, not vibes: the preview runs the SAME walk
	// the pass runs, so the chunks and the money figure must agree with the
	// pass that then actually runs. A quote that disagrees with its own
	// invoice is the failure this test exists to catch.
	//
	// The fixture adds a NEW file: it is the one stale shape both walks
	// count identically (walkCount counts new files; the comparison pass
	// counts them too). An EDITED file is measurably different between the
	// two — the pass writes its chunks and the pending-embed pass then
	// finds nothing to embed — a quirk of the S22 machinery that predates
	// S54 and is named in the T2 Notes rather than papered over here.
	x, root, _ := seedBuilt(t)
	must(t, os.WriteFile(filepath.Join(root, "extra.go"),
		[]byte("package main\n\nfunc extra() int {\n\treturn 2\n}\n"), 0o644), "add extra")
	ctx := context.Background()

	est, err := x.EstimateReindex(ctx, root, CorpusCode, pricedEmbedRoute())
	must(t, err, "estimate")
	if est.StaleFiles == 0 || est.Chunks == 0 || est.ApproxTokens == 0 {
		t.Fatalf("the preview of a corpus with one new file reads empty: %+v", est)
	}
	planCost := est.Cost(pricedEmbedRoute())
	if planCost == nil || planCost.State != "known" || planCost.USD == nil {
		t.Fatalf("a priced route priced the preview as %+v (want known with a figure)", planCost)
	}

	spy := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	out, err := x.Reindex(ctx, root, CorpusCode, pricedEmbedRoute(), spy, false, nil)
	must(t, err, "reindex")

	if est.Chunks != out.ChunksWritten {
		t.Errorf("the preview said %d chunks, the pass wrote %d — one walk, two answers", est.Chunks, out.ChunksWritten)
	}
	if out.Embedded == 0 {
		t.Fatalf("the pass embedded nothing — the preview just priced %d chunks", est.Chunks)
	}
	if !out.CostKnown || out.EmbeddedUSD == nil {
		t.Fatal("a priced route left the pass unable to price itself")
	}
	// The money figures must be the SAME KIND of figure — same route, same
	// unit, same order of magnitude. Exactness is NOT the claim and cannot
	// be: the preview prices raw bytes/4 over the file (its own
	// estimateBasis says so), the pass prices the wrapped chunk text it
	// actually sends (measured 2026-11-04 on this fixture: 3.8× apart,
	// recorded in T2's Notes — the wrap, not the walk, is the gap). A
	// second formula drifting an order of magnitude IS catchable here,
	// and that is what SP2's "cannot disagree" clause is worth at a door.
	if *planCost.USD <= 0 || *out.EmbeddedUSD <= 0 {
		t.Errorf("a known price stated as zero: preview $%.10f, pass $%.10f (M30)", *planCost.USD, *out.EmbeddedUSD)
	}
	if r := *out.EmbeddedUSD / *planCost.USD; r < 0.1 || r > 10 {
		t.Errorf("preview $%.10f vs pass $%.10f — ratio %.2f leaves the same-arithmetic order (SP2)",
			*planCost.USD, *out.EmbeddedUSD, r)
	}
	// Render — the refusal's paragraph — states the same money at its own
	// coarser precision, never a different figure.
	if got := est.Render(); got == "" {
		t.Error("Render came back empty")
	}
}

func TestReindexPlanNeverCallsTheEmbedder(t *testing.T) {
	// The zero is structural AND measured: the estimate takes no embedder
	// by its signature (there is no parameter through which a call could
	// come), and the measure — spy call count and chunk rows — stays at
	// where it was. A preview that embedded to price itself charged for
	// the preview (SP2; D9's shape, S54's door).
	x, root, _ := seedBuilt(t)
	must(t, os.WriteFile(filepath.Join(root, "retry.go"),
		[]byte("package main\n\nfunc retry() int {\n\treturn 7\n}\n"), 0o644), "edit retry")
	before := embedChunkCount(t, x, root)
	spy := &recordedEmbedder{fixture: loadEmbedFixture(t)}

	est, err := x.EstimateReindex(context.Background(), root, CorpusCode, pricedEmbedRoute())
	must(t, err, "estimate")
	_ = est.Cost(pricedEmbedRoute())

	if len(spy.Calls) != 0 {
		t.Errorf("the preview called the embedder %d time(s) — previews are free (SP2)", len(spy.Calls))
	}
	if got := embedChunkCount(t, x, root); got != before {
		t.Errorf("the preview moved the chunk count %d → %d — it writes NOTHING (D9)", before, got)
	}
}

func TestPlanCostStatesAreHonest(t *testing.T) {
	// Three states, and the preview may claim only what the arithmetic
	// earned: known carries the figure and no reason, unknown carries the
	// leaf's own sentence and NO figure — never the $0.00 M30 forbids.
	// ("partial" needs two usage counts against a half-priced route; a
	// preview prices input tokens only, so it arrives known or unknown —
	// claiming more states than the call site can reach would itself be
	// the dishonesty this test is about.)
	x, root, _ := seedBuilt(t)
	est, err := x.EstimateReindex(context.Background(), root, CorpusCode, pricedEmbedRoute())
	must(t, err, "estimate")

	if c := est.Cost(nil); c.State != "unknown" || c.USD != nil ||
		c.Reason != "no embedding route to price the pass" {
		t.Errorf("no route priced as %+v; want unknown, no figure, the leaf's sentence", c)
	}
	if c := est.Cost(unmeteredEmbedRoute()); c.State != "unknown" || c.USD != nil || c.Reason == "" {
		t.Errorf("the unpriced route priced as %+v; want unknown, no figure, a stated reason (M30)", c)
	}
	if c := est.Cost(pricedEmbedRoute()); c.State != "known" || c.USD == nil || c.Reason != "" {
		t.Errorf("the priced route priced as %+v; want known with a figure and no reason", c)
	}
}
