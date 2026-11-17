//go:build !noretrieval

package retrieval

import (
	"fmt"
)

// stale.go — how to tell a chunk changed (V8). This is the MECHANISM, not
// the trigger: T8 owns when a reindex fires (loop-triggered, asynchronous,
// never watched); T2 decides what "changed" means, so T4 and T5's corpora
// and T8's trigger all read the same definition.
//
// A StalePlan compares a file's CURRENT bytes against the chunks the index
// already holds, and answers in three sets: Write (chunks whose content is
// new or whose hash moved), Delete (stored ranges the chunker no longer
// produces), Unchanged (hashes that match, which cost nothing — this is
// where V8's incrementality is measured, and TestHashPerChunkNotPerFile is
// the gate: a per-file hash would put every chunk of an edited file into
// Write and pass every other test in this file).

// ChunkRange names a stored chunk by its 1-indexed line range — the pair
// (corpus, source, start, end) is the store's UNIQUE key, so a range is an
// identity.
type ChunkRange struct {
	Start int
	End   int
}

// StalePlan is what a reindex costs, decided before any work is done.
type StalePlan struct {
	Path      string
	Write     []Chunk // new ranges, or ranges whose content hash moved
	Delete    []ChunkRange
	Present   []ChunkRange // every range the chunker produces now (unchanged included) — the keep-set ApplyChunkPlan prunes against
	Unchanged int
	Skips     []Skip // reported by ChunkFile — never silent
}

// Empty reports the plan that asks for nothing — a reindex trigger calling
// this over thousands of files decides its real cost here, cheaply, without
// embedding anything (T8's async pass).
func (p *StalePlan) Empty() bool {
	return p != nil && len(p.Write) == 0 && len(p.Delete) == 0
}

// PlanFile compares current bytes against the stored chunks of one file of
// one corpus. It cannot embed and does not touch a route — embedding is
// T3's; this answers "which chunks are stale", which is a pure comparison
// against the store's own rows.
func (x *Index) PlanFile(root, corpus, path string, content []byte) (*StalePlan, error) {
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused corpus %q for a staleness plan — the corpora are %q and %q (and whatever a later slice adds as data)", corpus, CorpusCode, CorpusHistory)
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("retrieval: refused a staleness plan for %s at %q — corpus %q does not exist; ensure it before indexing it", path, root, corpus)
	}
	stored, err := x.s.RetrievalChunkRowsForSource(row.ID, path)
	if err != nil {
		return nil, err
	}
	storedHash := map[ChunkRange]string{}
	for _, r := range stored {
		storedHash[ChunkRange{r.LineStart, r.LineEnd}] = r.Hash
	}

	plan := &StalePlan{Path: path, Skips: nil}
	chunks, skips := ChunkFile(path, content)
	plan.Skips = append(plan.Skips, skips...)

	present := map[ChunkRange]bool{}
	for _, ch := range chunks {
		r := ChunkRange{ch.Start, ch.End}
		present[r] = true
		plan.Present = append(plan.Present, r)
		if h, had := storedHash[r]; !had || h != ch.Hash {
			plan.Write = append(plan.Write, ch)
		} else {
			plan.Unchanged++
		}
	}
	for r := range storedHash {
		if !present[r] {
			plan.Delete = append(plan.Delete, r)
		}
	}
	return plan, nil
}

// ApplyChunkPlan writes the plan to the chunk tables: upserts the Write set
// (the store's own ON CONFLICT keeps it idempotent) and prunes every stored
// range the plan no longer recognises. The prune is EXCEPT-based rather
// than wipe-then-write because a file whose chunks were wiped would answer
// queries as empty between the wipe and the writes — a reindex must never
// make the index briefly wrong.
//
// What this does NOT do: touch the vec0 table. Deleting chunk rows can
// leave vectors behind until T3 gives the delete side its vector half (the
// keyword answers then, and the availability sentence is honest) — recorded
// here so T3 finds the seam on purpose rather than by surprise.
func (x *Index) ApplyChunkPlan(root, corpus string, plan *StalePlan) (written, deleted int, err error) {
	if plan == nil {
		return 0, 0, fmt.Errorf("retrieval: refused to apply a nil chunk plan")
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("retrieval: refused to apply a chunk plan at %s — corpus %q does not exist", root, corpus)
	}
	for _, ch := range plan.Write {
		if err := x.s.InsertRetrievalChunk(row.ID, ch.Path, ch.Start, ch.End, ch.Hash, ch.Body); err != nil {
			return written, deleted, err
		}
		written++
	}
	// keep = every range that exists AFTER the plan: the unchanged ones and
	// the written ones. The deleted ones are exactly what is not kept.
	var keep [][2]int
	for _, r := range plan.Present {
		keep = append(keep, [2]int{r.Start, r.End})
	}
	deleted, err = x.s.DeleteRetrievalChunksForSourceExcept(row.ID, plan.Path, keep)
	return written, deleted, err
}
