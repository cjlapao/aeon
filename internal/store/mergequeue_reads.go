package store

import "fmt"

// mergeQueueOpenStatusSQL is the open set written out for the reactor's two
// reads. It mirrors MergeOpenStatusSet; a store test pins the two spellings.
const mergeQueueOpenStatusSQL = `'queued','catching_up','verifying','ready','merging','conflicted'`

// ListOpenItemsByLoop answers the open queue items whose landing OR resolve
// loop carries the slug — the reactor's wake-up read when a loop terminal
// event names a component root and slug, and the conflict-answer door's read
// that maps an answered gate back to its item. An empty argument is refused
// rather than matched: ” never lives in either slug column, and an
// unfiltered read would answer every item (refuse, not guess).
func (s *Store) ListOpenItemsByLoop(componentRoot, loopSlug string) ([]MergeItem, error) {
	if componentRoot == "" || loopSlug == "" {
		return nil, MergeRefusal{Kind: "unknown-loop", Reason: "an open-items-by-loop read names both component_root and the loop slug"}
	}
	// The whole predicate is one line on purpose: I4's scope guard reads
	// the store line by line and admits a hand-written loop-slug equality
	// only where the SAME line also constrains component_root, which this
	// read genuinely does — a slug names a loop within one component,
	// never across the machine.
	q := `SELECT ` + mergeItemCols + ` FROM merge_queue_item WHERE component_root = ? AND status IN (` + mergeQueueOpenStatusSQL + `) AND (landing_loop_slug = ? OR resolve_loop_slug = ?) ORDER BY position ASC, id ASC`
	rows, err := s.r.Query(q, componentRoot, loopSlug, loopSlug)
	if err != nil {
		return nil, fmt.Errorf("store: list open items by loop %s/%s: %w", componentRoot, loopSlug, err)
	}
	defer rows.Close()
	out := []MergeItem{}
	for rows.Next() {
		it, err := scanMergeItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan merge item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListItemsNeedingReconcile answers every open item across every repository,
// ordered so a reactor pass walks each repository's lane front-to-back
// (component_root, base_branch, position, id). The merge-queue reactor reads
// it at start and on every tick; ordering by lane means the slot-holder of a
// group is met before the items waiting behind it, which is the order the
// decisions need to be cheap in (the holder's state gates the rest).
func (s *Store) ListItemsNeedingReconcile() ([]MergeItem, error) {
	q := `SELECT ` + mergeItemCols + ` FROM merge_queue_item
	       WHERE status IN (` + mergeQueueOpenStatusSQL + `)
	       ORDER BY component_root ASC, base_branch ASC, position ASC, id ASC`
	rows, err := s.r.Query(q)
	if err != nil {
		return nil, fmt.Errorf("store: list items needing reconcile: %w", err)
	}
	defer rows.Close()
	out := []MergeItem{}
	for rows.Next() {
		it, err := scanMergeItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan merge item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListMergeItemsAcrossRoots answers every item whose status is one of the
// given words, across every repository, ordered by (component_root,
// base_branch, position, id) like the reconcile read. mergequeue.list
// without a root filter answers with it, and the caller's status set may
// include the terminal words the reconcile read never shows. An empty set
// refuses rather than answering "everything" — "everything" is never what a
// listing filter means.
func (s *Store) ListMergeItemsAcrossRoots(statuses []string) ([]MergeItem, error) {
	if len(statuses) == 0 {
		return nil, MergeRefusal{Kind: "unknown-field", Reason: "a cross-root queue read names the statuses it wants"}
	}
	ph := make([]byte, 0, len(statuses)*2)
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
		if i > 0 {
			ph = append(ph, ',', ' ')
		}
		ph = append(ph, '?')
	}
	q := `SELECT ` + mergeItemCols + ` FROM merge_queue_item WHERE status IN (` + string(ph) + `) ORDER BY component_root ASC, base_branch ASC, position ASC, id ASC`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list items across roots: %w", err)
	}
	defer rows.Close()
	out := []MergeItem{}
	for rows.Next() {
		it, err := scanMergeItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan merge item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
