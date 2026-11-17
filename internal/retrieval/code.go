//go:build !noretrieval

package retrieval

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// code.go — the code corpus walk (V2): source files under the project root,
// chunked by T2, with the SAME skip discipline S11's glob and grep follow —
// "an unexplained absence reads as 'no matches'". Every file that did not
// reach the index is NAMED with its reason; the walk never returns a quiet
// count.
//
// The rules, each one borrowed from `internal/tools/glob.go` (the house
// walker), and each stated because the point of a skip list is that nobody
// has to guess what was skipped:
//
//   - `.git` is the one skipped tree (never the root itself — a caller who
//     resolves INTO .git made that choice; incidental descent is skipped).
//   - Symlinks are never followed and never indexed.
//   - An unreadable directory or file is skipped and COUNTED, never a
//     crash — one unreadable corner must not destroy the rest of the
//     workspace's answers, and silence would read as absence.
//   - Files larger than maxIndexFileBytes are skipped by NAME: indexing a
//     generated bundle chunk-by-chunk spends real embedding money (V10) on
//     content nobody edits by hand. This is retrieval's OWN rule, added to
//     the borrowed ones, and it is stated here rather than hidden in a
//     constant.
//   - Everything T2's chunker refuses to chunk (binary, oversized lines,
//     empty) arrives in the plan's Skips — the chunker was built to report
//     and this walk passes its reports through unchanged.

// maxIndexFileBytes bounds what the code walk reads whole. 2 MiB sits well
// above any hand-written source file and well below any generated bundle
// worth fearing.
const maxIndexFileBytes = 2 * 1024 * 1024

// IndexSkip is one named absence: what was skipped, and what made it so.
// It is returned, never merely logged — the caller (T6's tool, T8's status)
// is where "I did not look at X" becomes visible to the person asking.
type IndexSkip struct {
	Path   string
	Reason string
}

// IndexTreeResult is what one walk reports.
type IndexTreeResult struct {
	// Root is the project root indexed (V12: the index is per project root;
	// a worktree is a distinct root and therefore a distinct index).
	Root      string
	Indexed   int // files whose chunks all matched what was already stored
	Written   int // chunks newly written
	Deleted   int // chunks whose content moved or whose file vanished
	Unchanged int
	Skips     []IndexSkip
}

// IndexTree walks the code corpus of one project root and reconciles it:
// files present get their plans applied, stored chunks whose file vanished
// are pruned (the except-prune, never wipe-then-write), and every absence
// is named. It is the incremental path (V8): cheap on a quiet tree because
// PlanFile answers Unchanged for every chunk whose hash held.
func (x *Index) IndexTree(ctx context.Context, root, corpus string) (*IndexTreeResult, error) {
	return x.reconcileCode(ctx, root, corpus, true, nil)
}

// reconcileCode is the shared walk behind IndexTree and T8's estimate and
// staleness passes. apply=false makes it comparison-only — every plan is
// computed and reported through onPlan, NOTHING is written or pruned.
// D9's "reindexes nothing" and O3's priced refusal both depend on this
// being the SAME walk: an estimate computed by a second, cheaper walker is
// a figure that disagrees with the work it prices.
func (x *Index) reconcileCode(ctx context.Context, root, corpus string, apply bool, onPlan func(rel string, plan *StalePlan)) (*IndexTreeResult, error) {
	if !ValidCorpus(corpus) {
		return nil, fmt.Errorf("retrieval: refused corpus %q — the corpora are %q and %q", corpus, CorpusCode, CorpusHistory)
	}
	if root == "" {
		return nil, fmt.Errorf("retrieval: refused a code index for an empty root — the index is per project root (V12); a nameless root is no index at all")
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("retrieval: refused to index %s: %w", root, err)
	}
	// The corpus must be ensured by the caller (CreateCorpus/EnsureCorpus):
	// an index is a decision with a row behind it, not a side effect of a
	// walk. The comparison-only passes are the exception the decision needs:
	// pricing a build that has not started (`aeon index build`, the priced
	// refusal) has no row yet and must not be refused for it — nothing
	// compares-against-nothing means EVERYTHING is the work.
	row, rowOK, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil {
		return nil, err
	}
	// The mtime gate (S71 T2) keys off the corpus row's id — a corpus with no
	// row (the first-index case) has no records to gate against, so every
	// source is a candidate: the gate only ever speeds up a corpus that has
	// already been walked once, and it fails open (to a re-read) when it
	// cannot read the record, never to a false "fresh".
	corpusID := int64(0)
	if rowOK {
		corpusID = row.ID
	}
	if !rowOK && apply {
		return nil, fmt.Errorf("retrieval: refused to index %s — corpus %q does not exist; ensure it first (the walk indexes, it does not decide)", root, corpus)
	}

	res := &IndexTreeResult{Root: root}
	seen := map[string]bool{} // files still on disk (relative, as stored)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			// unreadable corner: skipped and NAMED (S11's rule)
			name := path
			if d != nil {
				name = filepath.Join(path, d.Name())
			}
			res.Skips = append(res.Skips, IndexSkip{Path: name, Reason: fmt.Sprintf("unreadable: %v", err)})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			// symlinks and non-regular entries: never followed, named once
			res.Skips = append(res.Skips, IndexSkip{Path: path, Reason: "not a regular file (symlink or special)"})
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			res.Skips = append(res.Skips, IndexSkip{Path: path, Reason: "outside the root"})
			return nil
		}
		seen[rel] = true
		info, err := d.Info()
		if err != nil {
			res.Skips = append(res.Skips, IndexSkip{Path: rel, Reason: fmt.Sprintf("stat failed: %v", err)})
			return nil
		}
		if strings.EqualFold(filepath.Ext(rel), ".md") || strings.EqualFold(filepath.Ext(rel), ".markdown") {
			// the code corpus indexes CODE (V2): prose belongs to history —
			// named, not silently absent (S11's rule again)
			res.Skips = append(res.Skips, IndexSkip{Path: rel, Reason: "prose belongs to the history corpus (V2: the corpora never merge)"})
			return nil
		}
		if info.Size() > maxIndexFileBytes {
			res.Skips = append(res.Skips, IndexSkip{Path: rel, Reason: fmt.Sprintf("larger than %d bytes (a generated artifact is not source nobody edits by hand)", maxIndexFileBytes)})
			return nil
		}
		// S71 T2: the mtime gate, for the comparison-only passes only. A
		// source whose recorded mtime matches its current mtime is ASSUMED
		// fresh and not re-read — the walk that already runs stops re-
		// reading a large repo on every status read (G2/G3). A moved or
		// absent record is a candidate and is hash-confirmed as today. The
		// hash, never the mtime, decides staleness: a touch or git checkout
		// that moves mtime without changing content is re-hashed and found
		// unchanged, and not flagged.
		//
		// The gate NEVER skips the apply pass (IndexTree/Reindex/Rebuild):
		// that is a one-time operation, not a repeated read, and it must
		// process every file — a rebuild that wiped embeddings must re-
		// embed them regardless of mtime, and an incremental pass must still
		// count the unchanged files. The apply pass only WRITES THROUGH the
		// mtime (below) so the next status read gates.
		if !apply {
			if skip, gerr := x.mtimeGate(corpusID, rel, info); gerr != nil {
				return fmt.Errorf("retrieval: gate for %s at %s: %w", rel, root, gerr)
			} else if skip {
				// mtime matches the record: assumed fresh, not re-read.
				// Reported as a fresh file so the Files count stays honest
				// (an un-gated file would read as "looked and unchanged"; a
				// gated file reads the same — the gate skips the read, not
				// the verdict).
				if onPlan != nil {
					onPlan(rel, &StalePlan{Path: rel})
				}
				res.Indexed++
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			res.Skips = append(res.Skips, IndexSkip{Path: rel, Reason: fmt.Sprintf("read failed: %v", err)})
			return nil
		}
		var plan *StalePlan
		if rowOK {
			plan, err = x.PlanFile(root, corpus, rel, content)
			if err != nil {
				return fmt.Errorf("retrieval: plan for %s at %s: %w", rel, root, err)
			}
		} else {
			// No row yet and comparison-only: the chunker alone answers —
			// every chunk is a Write against the nothing stored. This is
			// the SAME chunker PlanFile runs (no second walk, no second
			// truth); only the stored comparison is absent because the
			// store is empty by hypothesis.
			chunks, skips := ChunkFile(rel, content)
			plan = &StalePlan{Path: rel, Write: chunks, Skips: skips}
			for _, ch := range chunks {
				plan.Present = append(plan.Present, ChunkRange{ch.Start, ch.End})
			}
		}
		for _, s := range plan.Skips {
			res.Skips = append(res.Skips, IndexSkip{Path: rel, Reason: s.Reason})
		}
		res.Unchanged += plan.Unchanged
		if !apply {
			// Comparison-only: the plan is computed and REPORTED, nothing
			// is written (D9 — a refused query reindexes nothing; an
			// estimate prices work, it never does it).
			if onPlan != nil {
				onPlan(rel, plan)
			}
			if plan.Empty() && len(plan.Skips) == 0 {
				res.Indexed++
				// The hash CONFIRMED this file fresh (its content matches
				// what is indexed), so its mtime may be recorded for the
				// next read. A stale file is NOT recorded here — a status
				// read does not re-index, and recording a still-stale
				// file's mtime would make the next read gate it as fresh
				// and hide the staleness (the gate must fail open to a
				// re-read, never to a false "fresh").
				if werr := x.writeThroughMtime(corpusID, rel, info); werr != nil {
					return fmt.Errorf("retrieval: record mtime for %s at %s: %w", rel, root, werr)
				}
			}
			return nil
		}
		written, deleted, err := x.ApplyChunkPlan(root, corpus, plan)
		if err != nil {
			return fmt.Errorf("retrieval: apply plan for %s at %s: %w", rel, root, err)
		}
		// After applying, the file's current content IS in the index, so its
		// mtime is recorded — the next read gates it as fresh and the work
		// is done (this is the write-through the touch case needs: a
		// content-unchanged file whose mtime moved is re-hashed, found
		// fresh, and its new mtime is recorded so it is not re-read again).
		if werr := x.writeThroughMtime(corpusID, rel, info); werr != nil {
			return fmt.Errorf("retrieval: record mtime for %s at %s: %w", rel, root, werr)
		}
		res.Written += written
		res.Deleted += deleted
		if written == 0 && deleted == 0 && len(plan.Skips) == 0 {
			res.Indexed++
		}
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	// The prune the walk owes: a stored source no longer on disk loses its
	// chunks (except-prune per source, the same discipline ApplyChunkPlan
	// keeps within a file).
	rows, ok, err := x.storedSources(root, corpus)
	if err != nil {
		return res, err
	} else if ok {
		for _, src := range rows {
			if seen[src] {
				continue
			}
			if !apply {
				// A vanished file is STALENESS, reported — the comparison
				// pass prunes nothing (a priced refusal that had already
				// deleted rows would be the lie D9 exists to prevent).
				if onPlan != nil {
					onPlan(src, nil)
				}
				res.Skips = append(res.Skips, IndexSkip{Path: src, Reason: "file vanished since the last index (comparison only — nothing pruned)"})
				continue
			}
			empty := &StalePlan{Path: src}
			if _, deleted, err := x.ApplyChunkPlan(root, corpus, empty); err != nil {
				return res, fmt.Errorf("retrieval: prune vanished %s at %s: %w", src, root, err)
			} else {
				res.Deleted += deleted
			}
			res.Skips = append(res.Skips, IndexSkip{Path: src, Reason: "file vanished since the last index — its chunks were pruned"})
		}
	}
	return res, nil
}

// mtimeGate is the cheap gate (S71 T2): a source whose recorded mtime equals
// its current mtime is assumed fresh and not re-read; a moved or absent
// record is a candidate and is hash-confirmed as today. The hash, never the
// mtime, decides staleness — a touch or a git checkout that moves mtime
// without changing content is re-hashed and found unchanged, and not
// flagged. A corpus with no row (corpusID 0, the first-index case) has no
// records to gate against, so every source is a candidate. The gate fails
// open: an unreadable record is a candidate, never a false "fresh".
func (x *Index) mtimeGate(corpusID int64, rel string, info fs.FileInfo) (skip bool, err error) {
	if corpusID == 0 {
		return false, nil
	}
	recorded, has, err := x.s.SourceMtime(corpusID, rel)
	if err != nil {
		return false, err
	}
	// Nanosecond precision: an edit in the SAME second as the last index is a
	// real change the gate must not skip (a second-precision comparison would
	// read it as "unchanged" and miss it). A genuinely-unchanged file's mtime
	// is bit-for-bit the recorded one.
	if has && info.ModTime().UnixNano() == recorded {
		return true, nil
	}
	return false, nil
}

// writeThroughMtime records the mtime at which a source was last VERIFIED
// fresh (or applied) — the record the gate reads. It is written only for a
// source the walk confirmed fresh (its hash matched) or applied, never for
// one found stale but left un-indexed (a status read does not re-index):
// recording a still-stale file's mtime would make the next read gate it as
// fresh and hide the staleness. A corpus with no row (corpusID 0) writes
// nothing — there is no row to gate against yet.
func (x *Index) writeThroughMtime(corpusID int64, rel string, info fs.FileInfo) error {
	if corpusID == 0 {
		return nil
	}
	return x.s.UpsertSourceMtime(corpusID, rel, info.ModTime().UnixNano())
}

// storedSources lists the distinct sources the corpus holds for a root.
func (x *Index) storedSources(root, corpus string) ([]string, bool, error) {
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if err != nil || !ok {
		return nil, false, err
	}
	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	var out []string
	for _, ch := range chunks {
		if !seen[ch.Source] {
			seen[ch.Source] = true
			out = append(out, ch.Source)
		}
	}
	return out, true, nil
}

// indexableExtHint is the stated shape of "source code" for the walk's
// FUTURE filter (T8's cheap re-walk may narrow by extension); today the
// walk indexes what the chunker accepts and reports the rest — a filter
// that silently narrowed the corpus would be a scope nobody stated (O8).
var indexableExtHint = strings.Join([]string{".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".c", ".h"}, " ")
