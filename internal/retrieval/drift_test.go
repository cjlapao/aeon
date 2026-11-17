//go:build !noretrieval

package retrieval

// drift_test.go — S71 T3: the cost the status would not otherwise say (G1).
// A stale code corpus's status carries DriftFigures — the changed/added/
// removed split and a three-state Cost. A priced route prices known; no
// route and an unmetered route are unknown with NO number, never a
// fabricated $0.00 (O3/M30). The figures come from the SAME walk the status
// reads, so the cost cannot disagree with the work it prices.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func codeCorpus(t *testing.T, rep *StatusReport) StatusCorpus {
	t.Helper()
	for _, c := range rep.Corpora {
		if c.Corpus == CorpusCode {
			return c
		}
	}
	t.Fatalf("status report has no code corpus")
	return StatusCorpus{}
}

func TestStatusPricesThePass(t *testing.T) {
	x, root, _ := seedBuilt(t)
	// A file whose content moved is the change the pass would make. Its mtime
	// moves, so the gate re-reads it and the walk finds it stale (Changed —
	// the store already held it).
	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 11\n}\n"), 0o644), "edit escalate")

	// A priced route prices the pass: known, a number, never $0.00 (the pass
	// has tokens to spend, so a positive figure, not a zero that reads
	// "free").
	rep, err := x.Status(context.Background(), root, pricedEmbedRoute())
	must(t, err, "status")
	code := codeCorpus(t, rep)
	if code.Drift == nil {
		t.Fatal("a code corpus must carry drift figures")
	}
	if code.Drift.Changed == 0 {
		t.Errorf("the moved file must read as Changed, got %+v", code.Drift)
	}
	if code.Drift.Cost == nil {
		t.Fatal("a priced pass must carry a cost")
	}
	if code.Drift.Cost.State != "known" || code.Drift.Cost.USD == nil {
		t.Errorf("a priced pass must price known with a number, got %+v", code.Drift.Cost)
	}
	if code.Drift.Cost.USD != nil && *code.Drift.Cost.USD <= 0 {
		t.Errorf("a pass with tokens to price must not read $0.00, got %v", *code.Drift.Cost.USD)
	}

	// No route: the cost is unknown (could-not-price), with NO number — never
	// a fabricated $0.00 that reads as "free" for a route nobody configured.
	rep2, err := x.Status(context.Background(), root, nil)
	must(t, err, "status no-route")
	code2 := codeCorpus(t, rep2)
	if code2.Drift == nil {
		t.Fatal("no route must still carry a drift figure (the split is known)")
	}
	if code2.Drift.Cost == nil || code2.Drift.Cost.State != "unknown" || code2.Drift.Cost.USD != nil {
		t.Errorf("no route must read unknown with no number, got %+v", code2.Drift.Cost)
	}

	// An unmetered route: the same honest absence — unknown, no number, a
	// reason in words, never $0.00.
	rep3, err := x.Status(context.Background(), root, unmeteredEmbedRoute())
	must(t, err, "status unmetered")
	code3 := codeCorpus(t, rep3)
	if code3.Drift.Cost == nil || code3.Drift.Cost.State != "unknown" || code3.Drift.Cost.USD != nil {
		t.Errorf("an unmetered route must read unknown with no number, got %+v", code3.Drift.Cost)
	}
	if code3.Drift.Cost != nil && code3.Drift.Cost.Reason == "" {
		t.Errorf("an unknown cost must say why in words (O3), got %+v", code3.Drift.Cost)
	}
}

func TestStaleThreeStates(t *testing.T) {
	// S71 T5: three-state honesty (P31). The walk's three answers are kept
	// apart, never collapsed: a source that vanished is Removed (a pass
	// would prune it — definite); a clean corpus has no drift at all (Drift
	// is nil, not a fabricated $0.00 that reads "free" for a pass that would
	// change nothing); and the gate fails OPEN to a re-read — a source it
	// cannot determine (no recorded mtime) is a candidate, never a false
	// "fresh". A source the walk cannot read or hash is a Skip (could-not-
	// determine), named, never fresh or stale — the existing walk's Skips.
	x, root, _ := seedBuilt(t)

	// (a) A clean corpus has no drift — not a $0.00, not a confident "fresh".
	rep, err := x.Status(context.Background(), root, pricedEmbedRoute())
	must(t, err, "status")
	if codeCorpus(t, rep).Drift != nil {
		t.Errorf("a clean corpus must have Drift == nil (no change; a no-op pass is not a $0.00)")
	}

	// (b) A vanished source is Removed — definite (a pass would prune it),
	// never a false "fresh" and never a false "stale".
	must(t, os.Remove(filepath.Join(root, "retry.go")), "remove retry")
	rep2, err := x.Status(context.Background(), root, pricedEmbedRoute())
	must(t, err, "status")
	c2 := codeCorpus(t, rep2)
	if c2.Drift == nil {
		t.Fatal("a vanished source must produce a drift (Removed)")
	}
	if c2.Drift.Removed != 1 {
		t.Errorf("a vanished source must read Removed (a pass would prune it), got %+v", c2.Drift)
	}

	// (c) The gate fails open: a source with no recorded mtime (a new file)
	// is a candidate (re-read), never a false "fresh" — so it reads Added,
	// not fresh. This is the could-not-determine the gate must never read as
	// "fresh".
	must(t, os.WriteFile(filepath.Join(root, "brand_new.go"), []byte("package main\n\nfunc new() int {\n\treturn 1\n}\n"), 0o644), "add brand new")
	rep3, err := x.Status(context.Background(), root, pricedEmbedRoute())
	must(t, err, "status")
	c3 := codeCorpus(t, rep3)
	if c3.Drift == nil || c3.Drift.Added != 1 {
		t.Errorf("a new file (no recorded mtime) must read Added — the gate fails open to a re-read, not a false \"fresh\"; got %+v", c3.Drift)
	}
}
