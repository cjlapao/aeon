package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// S24 T2 — the queue's SQL. The MEANING (statuses, transitions, position
// policy) lives in internal/queue; this file is the handle's side: rows in,
// rows out, and the claimed-FROM updates that make two reactors racing for
// the same item a decided race rather than a corrupt one. Vocabulary
// closes at the database (T1's CHECKs), so no Go constant is load-bearing
// here — that is deliberate: one table, one CHECK, one truth.

// QueueItemRow mirrors queue_item's columns exactly, nullables as pointers.
// internal/queue converts it into the package's QueueItem; the row type
// carries no behaviour on purpose — behaviour here would be a second place
// that knows what `ready` means.
type QueueItemRow struct {
	ID            int64
	ComponentRoot string
	Position      int
	Status        string
	Blueprint     string
	Goal          string
	WorkspaceRoot string
	Tech          *string
	WithJSON      *string
	Attended      *int
	SizeMin       *int
	SizeMax       *int
	SizeBasis     *string
	LoopSlug      *string
	CreatedAt     string
	UpdatedAt     string
}

// NewQueueItemRow is the insert shape. Position is NOT part of it: the
// store assigns the next position in the root (Append = "add to the end",
// D1's promise starts here), and Status arrives from the queue package —
// pending either sizing mode (CG1's diagram starts there regardless).
type NewQueueItemRow struct {
	ComponentRoot string
	Status        string
	Blueprint     string
	Goal          string
	WorkspaceRoot string
	Tech          *string
	WithJSON      *string
	Attended      *int
}

const queueItemColumns = `id, component_root, position, status, blueprint, goal,
	workspace_root, tech, with_json, attended, size_min, size_max, size_basis,
	loop_slug, created_at, updated_at`

func scanQueueItemRow(row interface{ Scan(dest ...any) error }) (QueueItemRow, error) {
	var q QueueItemRow
	err := row.Scan(&q.ID, &q.ComponentRoot, &q.Position, &q.Status, &q.Blueprint,
		&q.Goal, &q.WorkspaceRoot, &q.Tech, &q.WithJSON, &q.Attended,
		&q.SizeMin, &q.SizeMax, &q.SizeBasis, &q.LoopSlug, &q.CreatedAt, &q.UpdatedAt)
	return q, err
}

// AppendQueueItem adds one item to the END of the root's queue. The
// position is computed inside the same transaction as the insert —
// append-then-position racing itself would hand two items the same tail
// slot, and D1's "appending never disturbs what is already placed" starts
// at the tail.
func (s *Store) AppendQueueItem(in NewQueueItemRow) (QueueItemRow, error) {
	if in.ComponentRoot == "" || in.Blueprint == "" || in.Goal == "" || in.WorkspaceRoot == "" {
		return QueueItemRow{}, fmt.Errorf("store: a queue item needs a component root, blueprint, goal and workspace root")
	}
	tx, err := s.w.Begin()
	if err != nil {
		return QueueItemRow{}, fmt.Errorf("store: append queue item: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var pos int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM queue_item WHERE component_root = ?`,
		in.ComponentRoot).Scan(&pos); err != nil {
		return QueueItemRow{}, fmt.Errorf("store: tail position in %s: %w", in.ComponentRoot, err)
	}
	now := s.Now()
	res, err := tx.Exec(`INSERT INTO queue_item
		(component_root, position, status, blueprint, goal, workspace_root, tech, with_json, attended, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		in.ComponentRoot, pos, in.Status, in.Blueprint, in.Goal, in.WorkspaceRoot,
		in.Tech, in.WithJSON, in.Attended, now, now)
	if err != nil {
		return QueueItemRow{}, fmt.Errorf("store: append queue item in %s: %w", in.ComponentRoot, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return QueueItemRow{}, fmt.Errorf("store: append queue item id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return QueueItemRow{}, fmt.Errorf("store: append queue item commit: %w", err)
	}
	row, ok, err := s.QueueItemByID(id)
	if err != nil {
		return QueueItemRow{}, err
	}
	if !ok {
		return QueueItemRow{}, fmt.Errorf("store: queue item %d vanished between commit and read", id)
	}
	return row, nil
}

// QueueItemByID reads one row; false, not error, for an id that is not
// there — the caller decides whether absence is a refusal.
func (s *Store) QueueItemByID(id int64) (QueueItemRow, bool, error) {
	q, err := scanQueueItemRow(s.r.QueryRow(`SELECT `+queueItemColumns+` FROM queue_item WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return QueueItemRow{}, false, nil
	}
	if err != nil {
		return QueueItemRow{}, false, fmt.Errorf("store: read queue item %d: %w", id, err)
	}
	return q, true, nil
}

// ListQueueItems is the ordered-list read the whole slice does first —
// position ASC, and id as the tie-breaker so a listing is a total order
// even if history ever carries a tie.
func (s *Store) ListQueueItems(componentRoot string) ([]QueueItemRow, error) {
	rows, err := s.r.Query(`SELECT `+queueItemColumns+` FROM queue_item
		WHERE component_root = ? ORDER BY position ASC, id ASC`, componentRoot)
	if err != nil {
		return nil, fmt.Errorf("store: list queue items in %s: %w", componentRoot, err)
	}
	defer rows.Close()
	var out []QueueItemRow
	for rows.Next() {
		q, err := scanQueueItemRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan queue item in %s: %w", componentRoot, err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// TransitionQueueItem is the claimed-FROM update machine.go's discipline
// applied to the queue: the row moves from → to only if it is STILL `from`
// when the write lands. Two reactors racing for one item produce one
// winner (affected 1) and one honest loser (affected 0), never two
// statuses. Vocabulary is not re-checked here — T1's CHECK closes it at
// the database, and re-listing the seven in Go would be the second truth
// this slice's own preamble forbids.
func (s *Store) TransitionQueueItem(id int64, from, to string) (bool, error) {
	res, err := s.w.Exec(`UPDATE queue_item SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		to, s.Now(), id, from)
	if err != nil {
		return false, fmt.Errorf("store: transition queue item %d %s→%s: %w", id, from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: transition queue item %d: %w", id, err)
	}
	return n == 1, nil
}

// QueuePosEdit is one row's move in a reorder batch.
type QueuePosEdit struct {
	ID       int64
	Position int
	// RequireStatus closes the window between the package reading the
	// list and writing the new positions: the move lands only while the
	// row still wears one of these statuses (the reorderable three,
	// decided and recorded in internal/queue — see `reorderable` there).
	RequireStatus []string
}

// ReorderQueueItems applies a batch of position edits in ONE transaction,
// each guarded by its claimed status. A batch that half-lands would put
// two items at one position and hand NextReady a coin flip; either every
// move lands or none does, and the loser learns the queue moved under it.
func (s *Store) ReorderQueueItems(componentRoot string, edits []QueuePosEdit) (bool, error) {
	if len(edits) == 0 {
		return true, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return false, fmt.Errorf("store: reorder queue items: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range edits {
		if len(e.RequireStatus) == 0 {
			return false, fmt.Errorf("store: reorder edit for queue item %d names no guard status", e.ID)
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(e.RequireStatus)), ",")
		args := []any{e.Position, s.Now(), e.ID, componentRoot}
		for _, st := range e.RequireStatus {
			args = append(args, st)
		}
		res, err := tx.Exec(`UPDATE queue_item SET position = ?, updated_at = ?
			WHERE id = ? AND component_root = ? AND status IN (`+ph+`)`, args...)
		if err != nil {
			return false, fmt.Errorf("store: reorder queue item %d: %w", e.ID, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, err
		} else if n != 1 {
			return false, fmt.Errorf("store: queue item %d moved under the reorder (it is no longer pending, sizing or ready)", e.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: reorder queue items commit: %w", err)
	}
	return true, nil
}

// NextReadyQueueItem answers D2 AND CG2 in one statement: the ready item
// with the LOWEST position in this root, NOTHING while any item in the
// root is running (at most one queue item runs per project), and NOTHING
// while any item is **blocked** — "any blocked halts, unconditionally" is
// enforced by the read every advancer must go through, so the explicit
// `queue.advance` door, the auto chain, the answered-ask chain and the
// sizing chain all inherit the halt from the one place (found by the T9
// walk: item 2 started over a blocked item 1 before this clause existed).
// (The reactor still claims the item with TransitionQueueItem; this answers
// "what may start", the claimed update decides "who starts it".)
func (s *Store) NextReadyQueueItem(componentRoot string) (QueueItemRow, bool, error) {
	q, err := scanQueueItemRow(s.r.QueryRow(`SELECT `+queueItemColumns+` FROM queue_item q
		WHERE q.component_root = ? AND q.status = 'ready'
		  AND NOT EXISTS (SELECT 1 FROM queue_item r
		                  WHERE r.component_root = q.component_root AND r.status = 'running')
		  AND NOT EXISTS (SELECT 1 FROM queue_item b
		                  WHERE b.component_root = q.component_root AND b.status = 'blocked')
		ORDER BY q.position ASC, q.id ASC LIMIT 1`, componentRoot))
	if err == sql.ErrNoRows {
		return QueueItemRow{}, false, nil
	}
	if err != nil {
		return QueueItemRow{}, false, fmt.Errorf("store: next ready queue item in %s: %w", componentRoot, err)
	}
	return q, true, nil
}

// QueueItemByLoopSlug is the reactor's lookup: a terminal loop event names
// a loop, and the reactor's whole job begins with "is that loop something
// I put here?" — a miss is not an error, it is an untracked loop (a person
// started it directly), and the reactor does nothing, quietly (T3 §3).
// (component_root, loop_slug) is the composite the FK rides, so the
// lookup names the root rather than gambling on slug being global.
func (s *Store) QueueItemByLoopSlug(componentRoot, loopSlug string) (QueueItemRow, bool, error) {
	q, err := scanQueueItemRow(s.r.QueryRow(`SELECT `+queueItemColumns+` FROM queue_item
		WHERE component_root = ? AND loop_slug = ?`, componentRoot, loopSlug))
	if err == sql.ErrNoRows {
		return QueueItemRow{}, false, nil
	}
	if err != nil {
		return QueueItemRow{}, false, fmt.Errorf("store: queue item by loop %s/%s: %w", componentRoot, loopSlug, err)
	}
	return q, true, nil
}

// SetQueueItemLoop records the materialization: the loop the item became
// (CG3 — the item POINTS at the loop; the loop's own rows are the truth
// about what it did, D11). Claimed-FROM "not yet stamped": one item
// materializes ONCE, and a second stamp would be a silent second loop
// nobody started.
func (s *Store) SetQueueItemLoop(id int64, componentRoot, loopSlug string) (bool, error) {
	// The stamp is SCOPED: an id plus the caller's root — stamping another
	// project's item is a refusal by the WHERE, not a hope.
	res, err := s.w.Exec(`UPDATE queue_item
		SET
		  loop_slug = ? -- scoped by component_root in the WHERE below
		, updated_at = ?
		WHERE id = ? AND component_root = ? AND loop_slug IS NULL`, loopSlug, s.Now(), id, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: stamp queue item %d with loop %s: %w", id, loopSlug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: stamp queue item %d: %w", id, err)
	}
	return n == 1, nil
}

// LatestLoopGateQuestion is the door's replay discriminator: after the
// queue's question is answered, PendingQuestion no longer sees it, and a
// second "advance" must read as the store's claimed-ONCE refusal ("was
// already answered at …") rather than falling through to the ladder's
// "not waiting at a gate" — which would answer the wrong ledger. It
// reads the same three facts the door discriminates on: the loop's
// slug, gate kind, and the absence of a step (a ladder gate always
// rides its deciding step).
func (s *Store) LatestLoopGateQuestion(componentRoot, loopSlug string) (Question, bool, error) {
	row := s.r.QueryRow(`SELECT `+questionColumns+` FROM question
		WHERE component_root = ? AND loop_slug = ? AND kind = 'gate' AND step_id IS NULL
		ORDER BY id DESC LIMIT 1`, componentRoot, loopSlug)
	q, err := scanQuestion(row)
	if err == sql.ErrNoRows {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, fmt.Errorf("store: latest gate question for %s/%s: %w", componentRoot, loopSlug, err)
	}
	return q, true, nil
}

// ComponentQueueSettings reads the two queue settings for a root — the
// single read the reactor consults per terminal event (D3: advancing is
// gated by queue_advance; D7's sizing_mode rides the same row so T5 does
// not open a second door). component_root names the repo; the component that
// registers it owns the settings. The values come from EffectiveForRoot, the
// tier's one reader of the inheritable columns: a child root that left them
// NULL (inherit) resolves to its root's — and a root's own 'ask'/'manual'
// defaults — so the read answers what the queue ACTUALLY uses, never an empty
// string a caller would have to guess at.
func (s *Store) ComponentQueueSettings(componentRoot string) (advance, sizingMode string, ok bool, err error) {
	eff, err := s.EffectiveForRoot(componentRoot)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("store: queue settings for %s: %w", componentRoot, err)
	}
	return eff.QueueAdvance.Value, eff.SizingMode.Value, true, nil
}

// SetComponentQueueSettings writes them (T6's `aeon queue settings`; the
// socket twin rides T7). Both values close under the database CHECK —
// once more, the vocabulary is the schema's, not this file's.
func (s *Store) SetComponentQueueSettings(componentRoot, advance, sizingMode string) (bool, error) {
	// An empty value is "unset" → NULL, never an empty string: the column's
	// CHECK is NULL-or-vocabulary, and the CLI merges a NULL read back to
	// itself, so writing "" would refuse the act the read just answered.
	// The subquery names the row the READ resolves through (EffectiveForRoot
	// takes the primary mapping), so a write and its next read can never aim
	// at different rows.
	res, err := s.w.Exec(`UPDATE component SET queue_advance = NULLIF(?, ''), sizing_mode = NULLIF(?, '') WHERE id =
		(SELECT component_id FROM component_root WHERE component_root = ? AND is_primary = 1)`,
		advance, sizingMode, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: set queue settings for %s: %w", componentRoot, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set queue settings: %w", err)
	}
	return n == 1, nil
}

// SizeAndReadyQueueItem is the size's landing: the three size columns and
// the → ready edge write in ONE claimed statement — status in the WHERE,
// all-or-none by construction (migration 0041's own CHECK is the last
// word; this file does not re-run the law, it rides it). A landing for an
// item that has moved matches zero rows: the person cancelled it, another
// report landed, or it is already running — and either way the write is
// refused by the UPDATE, not hoped over. Root-scoped like every other door.
//
// The claimed status is a PARAMETER with a whitelist, not free text:
// only `sizing` (the skill run's report, T5) and `pending` (a hand-written
// size under `manual` mode, the set-size verb) may be claimed from. Any
// other claim is an error naming what it tried — a door that claimed from
// `done` would be a resurrection machine.
func (s *Store) SizeAndReadyQueueItem(id int64, componentRoot string, sizeMin, sizeMax int, basis, from string) (bool, error) {
	if from != "sizing" && from != "pending" {
		return false, fmt.Errorf("store: a size landing claims from sizing or pending, never %q", from)
	}
	res, err := s.w.Exec(`UPDATE queue_item
		SET size_min = ?, size_max = ?, size_basis = ?, status = 'ready', updated_at = ?
		WHERE id = ? AND component_root = ? AND status = ?`,
		sizeMin, sizeMax, basis, s.Now(), id, componentRoot, from)
	if err != nil {
		return false, fmt.Errorf("store: size and ready queue item %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: size and ready queue item: %w", err)
	}
	return n == 1, nil
}

// FirstPendingQueueItem is the sizing dispatch's read: the LOWEST-position
// pending item in the root — the queue sizes in order, the same order the
// backlog will run — and NOTHING while any item is blocked (CG2 halts the
// whole queue, sizing dispatch included; the T9 walk found the dispatch
// skipping over a blocked row). It does NOT police the running slot (D2
// governs loops, not sizing reports); the claimed UPDATEs do the claiming.
func (s *Store) FirstPendingQueueItem(componentRoot string) (QueueItemRow, bool, error) {
	q, err := scanQueueItemRow(s.r.QueryRow(`SELECT `+queueItemColumns+` FROM queue_item
		WHERE component_root = ? AND status = 'pending'
		  AND NOT EXISTS (SELECT 1 FROM queue_item b
		                  WHERE b.component_root = queue_item.component_root AND b.status = 'blocked')
		ORDER BY position ASC, id ASC LIMIT 1`, componentRoot))
	if err == sql.ErrNoRows {
		return QueueItemRow{}, false, nil
	}
	if err != nil {
		return QueueItemRow{}, false, fmt.Errorf("store: first pending queue item in %s: %w", componentRoot, err)
	}
	return q, true, nil
}
