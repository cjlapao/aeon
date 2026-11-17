//go:build !noretrieval

package retrieval

import (
	"context"
	"strings"
	"testing"
)

// stale_test.go — V8's mechanism: a hash per chunk makes a reindex
// incremental, and the plan is where the incrementality is MEASURED before
// any embedding (cost) happens. T8 fires these plans; T2 proves they cost
// what they claim.

func staleIndex(t *testing.T) (*Index, func(root, corpus string)) {
	t.Helper()
	s := openStore(t)
	x := New(s)
	ensure := func(root, corpus string) {
		t.Helper()
		if err := x.EnsureCorpus(context.Background(), root, corpus); err != nil {
			t.Fatalf("ensure corpus: %v", err)
		}
	}
	return x, ensure
}

const threeFuncs = `package main

func alpha() int {
	return 1
}

func beta() int {
	return 2
}

func gamma() int {
	return 3
}
`

func TestStalePlanWritesOnlyChangedChunk(t *testing.T) {
	x, ensure := staleIndex(t)
	ensure("/stale", CorpusCode)

	plan, err := x.PlanFile("/stale", CorpusCode, "three.go", []byte(threeFuncs))
	if err != nil {
		t.Fatal(err)
	}
	written, deleted, err := x.ApplyChunkPlan("/stale", CorpusCode, plan)
	if err != nil {
		t.Fatal(err)
	}
	if written != len(plan.Write) || deleted != 0 || len(plan.Delete) != 0 {
		t.Fatalf("first index wrote %d deleted %d (plan says write %d delete %d)", written, deleted, len(plan.Write), len(plan.Delete))
	}

	// One line changed, INSIDE beta, line count intact.
	edited := strings.Replace(threeFuncs, "\treturn 2\n", "\treturn 22 // one line edited\n", 1)
	plan, err = x.PlanFile("/stale", CorpusCode, "three.go", []byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Write) != 1 {
		t.Fatalf("a one-function edit plans %d chunk writes (%v) — V8's incremental reindex is not incremental; the per-file hash version passes every other test and dies HERE", len(plan.Write), ranges(plan.Write))
	}
	if plan.Unchanged != 2 || len(plan.Delete) != 0 { // three chunks: one wrote, two rode through
		t.Fatalf("plan says write %d delete %d unchanged %d — the untouched chunks must be counted as unchanged, not planned", len(plan.Write), len(plan.Delete), plan.Unchanged)
	}
	w, d, err := x.ApplyChunkPlan("/stale", CorpusCode, plan)
	if err != nil {
		t.Fatal(err)
	}
	if w != 1 || d != 0 {
		t.Fatalf("apply wrote %d deleted %d for a single-chunk plan", w, d)
	}
}

func TestStalePlanDeletesVanishedChunks(t *testing.T) {
	x, ensure := staleIndex(t)
	ensure("/stale", CorpusCode)
	plan, err := x.PlanFile("/stale", CorpusCode, "three.go", []byte(threeFuncs))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.ApplyChunkPlan("/stale", CorpusCode, plan); err != nil {
		t.Fatal(err)
	}

	// gamma vanishes (a deleted function, not an edited line).
	two := threeFuncs[:strings.Index(threeFuncs, "func gamma")]
	plan, err = x.PlanFile("/stale", CorpusCode, "three.go", []byte(two))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Delete) == 0 {
		t.Fatal("a deleted function planned no deletions — stale chunks would keep retrieving for code that no longer exists")
	}
	if len(plan.Write) != 0 {
		t.Fatalf("deleting gamma replans %d unchanged chunks for writing (%v) — except-based pruning exists so the untouched ride through", len(plan.Write), ranges(plan.Write))
	}
	w, d, err := x.ApplyChunkPlan("/stale", CorpusCode, plan)
	if err != nil {
		t.Fatal(err)
	}
	if w != 0 || d != len(plan.Delete) {
		t.Fatalf("apply wrote %d deleted %d, plan said delete %d", w, d, len(plan.Delete))
	}
	n, err := x.s.RetrievalChunkCount("/stale", CorpusCode)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("chunk table holds %d chunks after gamma vanished, want 2 — stale rows survive deletion", n)
	}

	// And a file that BECOMES binary: its whole chunk set must go (the skip
	// is reported; the index does not keep retrieving a text ghost).
	plan, err = x.PlanFile("/stale", CorpusCode, "three.go", []byte("package main\x00\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Write) != 0 || len(plan.Delete) == 0 || len(plan.Skips) != 1 {
		t.Fatalf("binary-ified file plans write %d delete %d skip %v", len(plan.Write), len(plan.Delete), plan.Skips)
	}
	if _, _, err := x.ApplyChunkPlan("/stale", CorpusCode, plan); err != nil {
		t.Fatal(err)
	}
	n, _ = x.s.RetrievalChunkCount("/stale", CorpusCode)
	if n != 0 {
		t.Fatalf("%d chunks survive a file becoming binary", n)
	}
}

func TestStalePlanOnNewFilePlansWholeFile(t *testing.T) {
	x, ensure := staleIndex(t)
	ensure("/stale", CorpusCode)
	plan, err := x.PlanFile("/stale", CorpusCode, "brand-new.go", []byte(threeFuncs))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Write) == 0 || plan.Unchanged != 0 || len(plan.Delete) != 0 {
		t.Fatalf("a never-seen file plans write %d unchanged %d delete %d — everything is new", len(plan.Write), plan.Unchanged, len(plan.Delete))
	}
	// The refusal half of the door: a corpus that does not exist refuses by
	// name rather than silently planning the void.
	if _, err := x.PlanFile("/nope", "code", "x.go", []byte(threeFuncs)); err == nil || !strings.Contains(err.Error(), `"/nope"`) {
		t.Fatalf("plan at an un-ensured root returned %v — a refusal names the thing refused", err)
	}
	if _, err := x.PlanFile("/stale", "memories", "x.go", []byte(threeFuncs)); err == nil || !strings.Contains(err.Error(), `"memories"`) {
		t.Fatalf("plan for an invented corpus returned %v — it must name what was typed", err)
	}
}

func TestHashEqualMeansNoWork(t *testing.T) {
	x, ensure := staleIndex(t)
	ensure("/stale", CorpusCode)
	plan, err := x.PlanFile("/stale", CorpusCode, "three.go", []byte(threeFuncs))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.ApplyChunkPlan("/stale", CorpusCode, plan); err != nil {
		t.Fatal(err)
	}
	// The identical bytes again: the plan is EMPTY (T8's trigger calls this
	// over every file after every loop — emptiness must be cheap and real).
	again, err := x.PlanFile("/stale", CorpusCode, "three.go", []byte(threeFuncs))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Empty() {
		t.Fatalf("re-planning unchanged bytes plans work: write %v delete %v (unchanged %d) — every scan would re-embed the world", ranges(again.Write), again.Delete, again.Unchanged)
	}
	if again.Unchanged == 0 {
		t.Fatalf("an empty plan claims ZERO unchanged too — the count is how T8 reports what the scan cost (O4)")
	}
}
