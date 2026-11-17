//go:build !noretrieval

package retrieval

// S22 T8 — refuse, price, reindex nothing; the per-corpus figure; the
// incremental pass. The measures are the store's own rows (the reindex spy
// at zero is a CHUNK COUNT and an embedder CALL COUNT, not a log line) and
// the walk is the production one (reconcileCode apply=false), so an
// estimate that disagrees with its pass would fail here rather than in a
// terminal.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// seedBuilt indexes AND embeds two files, returning the index, its root and
// the embedder the build used (so a later spy's silence is comparable).
func seedBuilt(t *testing.T) (*Index, string, *recordedEmbedder) {
	t.Helper()
	x, rootFn := embedIndex(t)
	root := rootFn()
	must(t, os.WriteFile(filepath.Join(root, "retry.go"), []byte("package main\n\nfunc retry() int {\n\treturn 3\n}\n"), 0o644), "seed retry")
	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 9\n}\n"), 0o644), "seed escalate")
	ctx := context.Background()
	_, err := x.IndexTree(ctx, root, CorpusCode)
	must(t, err, "index tree")
	emb := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	_, err = x.EmbedPending(ctx, root, CorpusCode, pricedEmbedRoute(), emb)
	must(t, err, "embed")
	return x, root, emb
}

func otherEmbedRoute() *model.Route {
	in := 0.13
	return &model.Route{ID: "other-embed", Provider: "openai", Model: "text-embedding-3-large", Prices: &model.Prices{InputUSDPerMTok: &in}}
}

func TestMismatchRefusesAndReindexesNothing(t *testing.T) {
	// V17/D9/C7: the query is refused, both spaces are named, and the
	// reindex spy — the store's rows and the embedder's calls — is at
	// ZERO. An edited file sits un-indexed on purpose: if refusal quietly
	// reconciled, the chunk count would move.
	x, root, _ := seedBuilt(t)
	must(t, os.WriteFile(filepath.Join(root, "retry.go"), []byte("package main\n\nfunc retry() int {\n\treturn 4\n}\n"), 0o644), "edit retry")
	before := embedChunkCount(t, x, root)
	spy := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	_, _, err := x.Search(context.Background(), root, CorpusCode, "retry", otherEmbedRoute(), spy, 5)
	if err == nil {
		t.Fatal("the mismatch answered a query — V6 calls that result the worst failure here")
	}
	msg := err.Error()
	for _, want := range []string{"text-embedding-3-small", "text-embedding-3-large", "Nothing was reindexed."} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q: %s", want, msg)
		}
	}
	if len(spy.Calls) != 0 {
		t.Errorf("the refusing query embedded %d batch(es) (D9)", len(spy.Calls))
	}
	if got := embedChunkCount(t, x, root); got != before {
		t.Errorf("the refusal moved the chunk count %d → %d — refusing reindexes NOTHING (C7)", before, got)
	}
	// The deliberate door agrees: without the force restatement it refuses
	// the mismatch too, and still writes nothing.
	if _, err := x.Reindex(context.Background(), root, CorpusCode, otherEmbedRoute(), spy, false, nil); err == nil {
		t.Fatal("Reindex accepted a foreign embedding space un-restated (V17)")
	}
	if len(spy.Calls) != 0 || embedChunkCount(t, x, root) != before {
		t.Error("the refused Reindex touched chunks or the provider (D9)")
	}
}

func TestMismatchReportsAnEstimateWithItsBasis(t *testing.T) {
	// O3: labelled, with file and chunk counts and the tokenisation basis,
	// and it tells the operator the exact door.
	x, root, _ := seedBuilt(t)
	_, _, err := x.Search(context.Background(), root, CorpusCode, "retry", otherEmbedRoute(), nil, 5)
	if err == nil {
		t.Fatal("no refusal")
	}
	msg := err.Error()
	for _, want := range []string{"estimate:", "files", "~", "chunks", "bytes/token", "$", "To proceed:", "other-embed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the priced refusal lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "exactly") {
		t.Errorf("the refusal states an exact figure for work not yet done (O3): %s", msg)
	}
}

func TestUnmeteredPortionsExcludedFromTheEstimate(t *testing.T) {
	// M30: no price, no number. $0.00 would read as "free", and nobody
	// configured free.
	x, root, _ := seedBuilt(t)
	_, _, err := x.Search(context.Background(), root, CorpusCode, "retry", unmeteredEmbedRoute(), nil, 5)
	if err == nil {
		t.Fatal("no refusal")
	}
	msg := err.Error()
	if strings.Contains(msg, "$") {
		t.Errorf("an unmetered route produced a money figure (M30): %s", msg)
	}
	for _, want := range []string{"cannot be priced", "no figure"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the unmetered estimate does not say so (%q): %s", want, msg)
		}
	}
}

func TestStalenessIsPerCorpus(t *testing.T) {
	// O4/D10/C8: two figures, and the code corpus moving NEVER moves the
	// history figure — structural, because the history pass does not
	// consult the tree at all.
	x, root, _ := seedBuilt(t)
	ctx := context.Background()
	must(t, x.EnsureCorpus(ctx, root, CorpusHistory), "ensure history")
	// The daemon's conversion point feeds rendered handoffs through the
	// chunk door; the test uses the same door directly (IndexHandoff
	// renders, putHistoryChunk stores — the pair the daemon calls).
	rendered, err := x.IndexHandoff(ctx, root, "hloop", 7, map[string]string{"summary": "the retry brief was rejected"})
	must(t, err, "render handoff")
	must(t, x.putHistoryChunk(ctx, root, "handoff:hloop/7", 7, rendered), "seed history")
	h0, err := x.Staleness(ctx, root, CorpusHistory)
	must(t, err, "history staleness")
	if h0.Files == 0 || h0.PendingEmbed == 0 {
		t.Fatalf("the seeded handoff left history reading empty: %+v", h0)
	}
	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 10\n}\n"), 0o644), "edit escalate")
	c1, err := x.Staleness(ctx, root, CorpusCode)
	must(t, err, "code staleness")
	if c1.StaleFiles != 1 {
		t.Errorf("one edited file read as %d stale (O4 wants the code figure to see it)", c1.StaleFiles)
	}
	must(t, os.WriteFile(filepath.Join(root, "retry.go"), []byte("package main\n\nfunc retry() int {\n\treturn 5\n}\n"), 0o644), "edit retry")
	c2, err := x.Staleness(ctx, root, CorpusCode)
	must(t, err, "code staleness 2")
	if c2.StaleFiles != 2 {
		t.Errorf("two edited files read as %d stale", c2.StaleFiles)
	}
	h1, err := x.Staleness(ctx, root, CorpusHistory)
	must(t, err, "history staleness 2")
	if h1.Files != h0.Files || h1.StaleFiles != h0.StaleFiles || h1.PendingEmbed != h0.PendingEmbed {
		t.Errorf("code edits moved the history figures: %+v → %+v (O4)", h0, h1)
	}
}

func TestStaleIndexIsReportedNotHidden(t *testing.T) {
	// "A stale index that claims currency is worse" — status says stale,
	// and the cheap query-path signal (PendingChunkCount) agrees.
	x, root, _ := seedBuilt(t)
	ctx := context.Background()
	must(t, os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n\nfunc fresh() int {\n\treturn 1\n}\n"), 0o644), "add file")
	_, err := x.IndexTree(ctx, root, CorpusCode) // new.go's chunks land, embedding NOT run
	must(t, err, "index tree 2")
	// AND the tree moves again after that pass — stale rows awaiting the
	// next deliberate walk.
	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 12\n}\n"), 0o644), "edit escalate")
	rep, err := x.Status(ctx, root, pricedEmbedRoute())
	must(t, err, "status")
	var code *StatusCorpus
	for i := range rep.Corpora {
		if rep.Corpora[i].Corpus == CorpusCode {
			code = &rep.Corpora[i]
		}
	}
	if code == nil || code.PendingEmbed == 0 || code.StaleFiles == 0 {
		t.Fatalf("status hid the half-done pass: %+v", rep.Corpora)
	}
	n, err := x.PendingChunkCount(root, CorpusCode)
	must(t, err, "pending count")
	if n != code.PendingEmbed {
		t.Errorf("the query-path signal (%d) disagrees with status (%d) — one fact, one source", n, code.PendingEmbed)
	}
}

func TestReindexPassTakesChangedChunksOnly(t *testing.T) {
	// V8's economy, measured at the embedder: the second pass embeds the
	// changed chunks, not the corpus again (T2's per-chunk hash pays here
	// or it is decoration).
	x, root, first := seedBuilt(t)
	ctx := context.Background()
	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 11\n}\n"), 0o644), "edit escalate")
	must(t, os.WriteFile(filepath.Join(root, "extra.go"), []byte("package main\n\nfunc extra() int {\n\treturn 2\n}\n"), 0o644), "add extra")
	spy := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	out, err := x.Reindex(ctx, root, CorpusCode, pricedEmbedRoute(), spy, false, nil)
	must(t, err, "reindex")
	if out.ChunksWritten == 0 {
		t.Error("the reindex wrote no chunks though two files moved")
	}
	firstTexts, spyTexts := 0, 0
	for _, c := range first.Calls {
		firstTexts += c.Texts
	}
	for _, c := range spy.Calls {
		spyTexts += c.Texts
	}
	if spyTexts >= firstTexts {
		t.Errorf("the incremental pass embedded %d chunks against the first pass's %d — V8's economy is missing", spyTexts, firstTexts)
	}
	if spyTexts == 0 {
		t.Error("the incremental pass embedded nothing — changed chunks must reach the embedder")
	}
}

func TestRebuildOntoAnotherModelNeedsTheRestatement(t *testing.T) {
	// force is the V17 money restatement: with it, the foreign corpus is
	// wiped (chunks AND vectors AND the recorded embedding) and rebuilt
	// under the new route; the wipe's evidence is the row re-stamped.
	x, root, _ := seedBuilt(t)
	ctx := context.Background()
	spy := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	out, err := x.Reindex(ctx, root, CorpusCode, otherEmbedRoute(), spy, true, nil)
	must(t, err, "forced rebuild")
	if out.Embedded == 0 || len(spy.Calls) == 0 {
		t.Error("the forced rebuild embedded nothing — a wipe without a pass leaves the corpus worse than before")
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusCode)
	must(t, err, "row")
	if !ok || row.EmbedModel != otherEmbedRoute().Model {
		t.Errorf("the corpus row still records %q, not the forced route (V6's record must follow the pass)", row.EmbedModel)
	}
}

func TestNoFilesystemWatcher(t *testing.T) {
	// N2: no watcher, no inotify, no polling loop over the tree — the
	// loop TRIGGERS (V8) and everything else is deliberate. Scanned: the
	// retrieval package's non-test sources plus the daemon's trigger and
	// the CLI verb (the whole reindex surface).
	sources := []string{
		"reindex.go", "code.go", "stale.go", "index.go", "hybrid.go", "availability.go", "embed.go",
		"../daemon/reindex.go", "../daemon/step.go", "../../cmd/aeon/index.go",
	}
	banned := []string{"fsnotify", "inotify", "time.NewTicker", "time.Tick(", "NotifyOn", "StartWatcher"}
	for _, name := range sources {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, tok := range banned {
			if strings.Contains(string(raw), tok) {
				t.Errorf("%s contains %q — N2 forbids watching the tree entirely", name, tok)
			}
		}
	}
}
