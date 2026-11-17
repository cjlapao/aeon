package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// S29 T1 — the triage's SQL. The MEANING (the four validation states and
// their transition table, D2's priority split) lives in internal/triage;
// this file is the handle's side: rows in, rows out, and the claimed-FROM
// updates that make two writers racing for one bug a decided race rather
// than a corrupt one. Vocabulary closes at the database (0044's CHECKs),
// so no Go constant is load-bearing here — roadmap.go's deliberate rule,
// carried: one table, one CHECK, one truth.
//
// The two splits D2 exists to keep are TWO columns, and this file writes
// them the same way roadmap writes its own priority pair: state and label
// together, and a label on an unknown state is the CHECK's business, not
// a convention. priority's shape is copied verbatim from 0043, so a
// hand-off row never needs translation between the two tables.

// BugRow mirrors bug's columns exactly, nullables as pointers.
// internal/triage converts it into the package's Bug; the row type carries
// no behaviour on purpose — behaviour here would be a second place that
// knows what `reproduced` means.
type BugRow struct {
	ID              int64
	ComponentRoot   string
	Title           string
	Body            string
	ValidationState string
	PriorityState   string
	PriorityLabel   *string
	FiledBy         string
	DuplicateOf     *int64
	RoadmapItemID   *int64
	CreatedAt       string
	UpdatedAt       string
}

// NewBugRow is the insert shape. Identity and timestamps are the store's;
// validation_state is NOT part of it — a created bug is `unvalidated` (D1's
// start: no attempt has run yet), and letting the caller pick the entry
// state would smuggle a pre-looked-at claim in through the front door, the
// D7 collapse this whole slice is named to prevent. priority_state defaults
// to 'unknown' at the database when empty here — D2's honest absence is the
// DEFAULT, not a caller's habit.
type NewBugRow struct {
	ComponentRoot string
	Title         string
	Body          string
	// PriorityState and PriorityLabel may be supplied at create ONLY by a
	// human path that has decided a priority; an empty state defaults to
	// 'unknown'. They never carry validation_state or priority_state
	// from a model — that is refused before the row ever exists (T3).
	PriorityState string
	PriorityLabel *string
	// FiledBy is the one provenance a bug row may carry: an agent name or
	// 'person' (D7's self-filing made literal). It is mandatory — a bug
	// with no filer is the record that cannot answer who looked.
	FiledBy string
}

const bugColumns = `id, component_root, title, body, validation_state,
	priority_state, priority_label, filed_by, duplicate_of, roadmap_item_id,
	created_at, updated_at`

func scanBugRow(row interface {
	Scan(dest ...any) error
}) (BugRow, error) {
	var r BugRow
	err := row.Scan(&r.ID, &r.ComponentRoot, &r.Title, &r.Body, &r.ValidationState,
		&r.PriorityState, &r.PriorityLabel, &r.FiledBy, &r.DuplicateOf, &r.RoadmapItemID,
		&r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// CreateBug adds one bug, entering at `unvalidated`. priority_state and its
// label travel together — a label on an unknown priority is the D2
// contradiction the CHECK refuses, and this door refuses the partial WRITE
// so the refusal arrives as a sentence, not a driver error. validation_state
// is never an argument: it is the store's own default.
func (s *Store) CreateBug(in NewBugRow) (BugRow, error) {
	if in.ComponentRoot == "" || in.Title == "" || in.Body == "" {
		return BugRow{}, fmt.Errorf("store: a bug needs a component root, a title and a body — the report itself is the body")
	}
	if in.FiledBy == "" {
		return BugRow{}, fmt.Errorf("store: a bug names who filed it — an agent name or 'person' (D7); a claim with no filer is a record that cannot answer who looked")
	}
	if in.PriorityState == "" {
		in.PriorityState = "unknown"
	}
	if in.PriorityState == "unknown" && in.PriorityLabel != nil {
		return BugRow{}, fmt.Errorf("store: a priority label on an unknown priority contradicts itself (D2) — set the state or drop the label")
	}
	now := s.Now()
	id, err := createBugInsert(s.w, in, now)
	if err != nil {
		return BugRow{}, err
	}
	row, ok, err := s.BugByID(id)
	if err != nil {
		return BugRow{}, err
	}
	if !ok {
		return BugRow{}, fmt.Errorf("store: bug %d vanished between commit and read", id)
	}
	return row, nil
}

// bugExec is the shape BOTH *sql.DB and *sql.Tx satisfy for Exec — the one
// statement text shared between the plain writer and its audited twin
// (bug_audited.go), so the SQL the audited path runs is literally the SQL
// the plain path always ran, not a copy that can drift (S51 T1).
type bugExec interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// createBugInsert is the one INSERT. The file-by check stays at this edge
// (a filer-less claim cannot be written); the caller has already defaulted
// priority_state and checked the D2 contradiction.
func createBugInsert(e bugExec, in NewBugRow, now string) (int64, error) {
	if in.FiledBy == "" {
		return 0, fmt.Errorf("store: a bug names who filed it — an agent name or 'person' (D7); a claim with no filer is a record that cannot answer who looked")
	}
	res, err := e.Exec(`INSERT INTO bug
		(component_root, title, body, priority_state, priority_label, filed_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		in.ComponentRoot, in.Title, in.Body, in.PriorityState, in.PriorityLabel, in.FiledBy, now, now)
	if err != nil {
		return 0, fmt.Errorf("store: create bug in %s: %w", in.ComponentRoot, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: create bug id: %w", err)
	}
	return id, nil
}

// BugByID reads one row; false, not error, for an id that is not there —
// the caller decides whether absence is a refusal.
func (s *Store) BugByID(id int64) (BugRow, bool, error) {
	r, err := scanBugRow(s.r.QueryRow(`SELECT `+bugColumns+` FROM bug WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return BugRow{}, false, nil
	}
	if err != nil {
		return BugRow{}, false, fmt.Errorf("store: read bug %d: %w", id, err)
	}
	return r, true, nil
}

// BugFilters is the list's optional narrowing — the human path names
// validation and priority; empty means "no filter on that column".
type BugFilters struct {
	ValidationState string
	PriorityState   string
}

// ListBugs reads a root's bugs, newest first — the triage list walks the
// backlog the way a person triages it, most recent on top. Terminal and
// in-flight rows are both INCLUDED: a bug that could-not-tell is not a bug
// that should vanish, it is a record of what an attempt found.
func (s *Store) ListBugs(componentRoot string, f BugFilters) ([]BugRow, error) {
	q := `SELECT ` + bugColumns + ` FROM bug WHERE component_root = ?`
	args := []any{componentRoot}
	if f.ValidationState != "" {
		q += ` AND validation_state = ?`
		args = append(args, f.ValidationState)
	}
	if f.PriorityState != "" {
		q += ` AND priority_state = ?`
		args = append(args, f.PriorityState)
	}
	q += ` ORDER BY created_at DESC, id DESC`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list bugs in %s: %w", componentRoot, err)
	}
	defer rows.Close()
	var out []BugRow
	for rows.Next() {
		r, err := scanBugRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan bug in %s: %w", componentRoot, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountBugs is the honest count a list with a zero answer must show.
func (s *Store) CountBugs(componentRoot string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM bug WHERE component_root = ?`, componentRoot).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count bugs in %s: %w", componentRoot, err)
	}
	return n, nil
}

// TransitionBug is the claimed-FROM validation update — roadmap.go's
// discipline carried: the row moves from → to only if it is STILL `from`
// when the write lands. Two attempts racing for one bug produce one winner
// (affected 1) and one honest loser (affected 0), never two states. The
// table decides which moves are legal (T2); this is only the race.
func (s *Store) TransitionBug(id int64, from, to string) (bool, error) {
	return transitionBugStmt(s.w, id, from, to, s.Now())
}

// transitionBugStmt is the one claimed-FROM UPDATE, shared with its audited
// twin (bug_audited.go) so the SQL cannot drift between the two paths.
func transitionBugStmt(e bugExec, id int64, from, to, now string) (bool, error) {
	res, err := e.Exec(`UPDATE bug SET validation_state = ?, updated_at = ? WHERE id = ? AND validation_state = ?`,
		to, now, id, from)
	if err != nil {
		return false, fmt.Errorf("store: transition bug %d %s→%s: %w", id, from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: transition bug %d: %w", id, err)
	}
	return n == 1, nil
}

// SetBugPriority writes state and label TOGETHER — the pair is one decision
// (D2): moving to 'unknown' carries a nil label, because a label on an
// unknown priority is the contradiction the CHECK made unwritable. A known
// or assumed priority may carry its label or none yet. The update is scoped
// by the caller's root so an id from another project cannot be reprioritized
// (the queue/roadmap precedent).
func (s *Store) SetBugPriority(id int64, componentRoot, state string, label *string) (bool, error) {
	if state == "unknown" && label != nil {
		return false, fmt.Errorf("store: bug %d: a priority label on an unknown priority contradicts itself (D2)", id)
	}
	return setBugPriorityStmt(s.w, id, componentRoot, state, label, s.Now())
}

// setBugPriorityStmt is the one UPDATE, shared with the audited twin.
func setBugPriorityStmt(e bugExec, id int64, componentRoot, state string, label *string, now string) (bool, error) {
	res, err := e.Exec(`UPDATE bug SET priority_state = ?, priority_label = ?, updated_at = ? WHERE id = ? AND component_root = ?`,
		state, label, now, id, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: set bug %d priority: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set bug %d priority: %w", id, err)
	}
	return n == 1, nil
}

// SetBugDuplicateOf is the ONLY write path to duplicate_of (D5/N3): a human
// confirmation (internal/triage's ConfirmDuplicate) calls it and nothing
// else does. The column has no DEFAULT and no trigger that sets it, so this
// is the only door; the scope is the caller's root so another project's bug
// cannot be re-pointed. The argument is the confirmed-of id, or nil to clear.
func (s *Store) SetBugDuplicateOf(id int64, componentRoot string, of *int64) (bool, error) {
	return setBugDuplicateOfStmt(s.w, id, componentRoot, of, s.Now())
}

// setBugDuplicateOfStmt is the one UPDATE, shared with the audited twin.
func setBugDuplicateOfStmt(e bugExec, id int64, componentRoot string, of *int64, now string) (bool, error) {
	res, err := e.Exec(`UPDATE bug SET duplicate_of = ?, updated_at = ? WHERE id = ? AND component_root = ?`,
		of, now, id, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: set bug %d duplicate_of: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set bug %d duplicate_of: %w", id, err)
	}
	return n == 1, nil
}

// SetBugRoadmapItemID records the hand-off's result (D8/T6): the one S26
// item a validated, prioritized bug produced. The id is stamped once — a
// bug already carrying one is not handed off again, which the caller
// enforces by refusing to call this twice. The scope is the caller's root.
func (s *Store) SetBugRoadmapItemID(id int64, componentRoot string, roadmapItemID int64) (bool, error) {
	return setBugRoadmapItemIDStmt(s.w, id, componentRoot, roadmapItemID, s.Now())
}

// setBugRoadmapItemIDStmt is the one UPDATE, shared with the audited twin.
func setBugRoadmapItemIDStmt(e bugExec, id int64, componentRoot string, roadmapItemID int64, now string) (bool, error) {
	res, err := e.Exec(`UPDATE bug SET roadmap_item_id = ?, updated_at = ? WHERE id = ? AND component_root = ?`,
		roadmapItemID, now, id, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: record bug %d roadmap hand-off: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: record bug %d roadmap hand-off: %w", id, err)
	}
	return n == 1, nil
}

// updateBugFieldsStmt edits the title and/or the body — the ONLY editable
// fields of a bug (TM1) — leaving every other column byte-identical by
// construction (the statement lists nothing else). A nil pointer means
// "leave that field alone". Returns false when no row matches the id.
func updateBugFieldsStmt(tx *sql.Tx, id int64, title, body *string, now string) (bool, error) {
	q := `UPDATE bug SET updated_at = ?`
	args := []any{now}
	if title != nil {
		q += `, title = ?`
		args = append(args, *title)
	}
	if body != nil {
		q += `, body = ?`
		args = append(args, *body)
	}
	q += ` WHERE id = ?`
	args = append(args, id)
	res, err := tx.Exec(q, args...)
	if err != nil {
		return false, fmt.Errorf("store: update bug %d fields: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: update bug %d fields count: %w", id, err)
	}
	return n > 0, nil
}

// --- bug_vec: the on-demand, per-project embedding index (D6) ----------------
//
// bug_vec is created on demand, NOT in migration 0044 — the same reason
// 0040's vec0 is not in its migration: a vec0 table fixes its embedding
// dimension AT CREATE time, and the dimension is data (the route's
// capability answers it — a migration cannot know a model that may not
// exist yet). The table name carries the dimension and a hash of the root,
// so a project's bugs are in their OWN table and no query of one project's
// bugs can ever read another's (D6/N2's project scope is the name, not a
// WHERE clause). A bug with no embedding yet is a normal state: the table
// simply has no row for it.

// bugVecTableName derives (and gates) the vec0 table name:
// bug_vec_d<dim>_<sha256(root)[:16]>. The root is hashed and dim is an int
// past a positivity check, because identifiers cannot be parameter-bound in
// SQLite — hash plus derivation is the only safe path.
func bugVecTableName(root string, dim int) (string, error) {
	if root == "" || dim < 1 {
		return "", fmt.Errorf("store: refused bug vec table — root required and dimension %d must be at least 1 (a vec0 table fixes its dimension at create time; 0 is no geometry to build)", dim)
	}
	sum := sha256.Sum256([]byte(root))
	return fmt.Sprintf("bug_vec_d%d_%s", dim, hex.EncodeToString(sum[:])[:16]), nil
}

// EnsureBugVecTable creates (idempotently) the per-project vec0 table at one
// dimension. The dimension is an int past a positivity gate; the one
// caller-controlled piece is the hashed root, so Sprintf is assembly from
// safe pieces, not interpolation of input.
func (s *Store) EnsureBugVecTable(root string, dim int) (string, error) {
	table, err := bugVecTableName(root, dim)
	if err != nil {
		return "", err
	}
	if _, err := s.w.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding FLOAT[%d], bug_id integer)`,
		table, dim)); err != nil {
		return "", fmt.Errorf("store: create bug vec table for %s at %d dims: %w", root, dim, err)
	}
	return table, nil
}

// UpsertBugVec stores one bug's embedding. The delete-then-insert is the
// upsert: vec0 has no ON CONFLICT, and a re-embedded bug (its text moved)
// must replace, never double. The vector crosses as its JSON array text
// (sqlite-vec's documented form); the dimension is checked HERE, against
// the table's own construction, because a wrong-length vector is exactly
// the corruption the table name (which carries the dimension) promises
// cannot happen.
func (s *Store) UpsertBugVec(root string, dim int, bugID int64, vector []float32) error {
	if len(vector) != dim {
		return fmt.Errorf("store: refused a vector of %d dimensions for bug %d at %d dims — the table's name carries the dimension precisely so this mismatch is impossible",
			len(vector), bugID, dim)
	}
	table, err := bugVecTableName(root, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(`DELETE FROM `+table+` WHERE bug_id = ?`, bugID); err != nil {
		return fmt.Errorf("store: replacing embedding for bug %d (%s): %w", bugID, table, err)
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return fmt.Errorf("store: encode embedding for bug %d: %w", bugID, err)
	}
	if _, err := s.w.Exec(`INSERT INTO `+table+`(bug_id, embedding) VALUES(?, ?)`, bugID, string(raw)); err != nil {
		return fmt.Errorf("store: store embedding for bug %d (%s): %w", bugID, table, err)
	}
	return nil
}

// BugVecHit is one nearest-neighbour result: a bug id and its distance
// (smaller is nearer — a penalty, the vec0 convention).
type BugVecHit struct {
	BugID    int64
	Distance float64
}

// QueryBugVec answers nearest-neighbours for a query vector against ONE
// project's table. Project scope is the table's NAME (bugVecTableName), so
// a query of one project's bugs can never read another's (D6/N2) — the
// filter is not a WHERE, it is which table the query even runs against.
// A missing table is NOT an error: it means nothing in this project has
// been embedded yet (the table arrives with the first dimension), and the
// caller's availability check answers that honestly.
func (s *Store) QueryBugVec(root string, dim int, vector []float32, limit int) ([]BugVecHit, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("store: refused a bug vec query at %s for %d dims — the dimension is data and 0 means no embedding ever ran", root, dim)
	}
	if len(vector) != dim {
		return nil, fmt.Errorf("store: refused a bug vec query of %d dimensions at %s (the index holds %d) — comparing different geometries returns numbers, not neighbours",
			len(vector), root, dim)
	}
	if limit <= 0 {
		limit = 10
	}
	table, err := bugVecTableName(root, dim)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return nil, fmt.Errorf("store: encode bug vec query: %w", err)
	}
	// k = ? limits the vec0 results to the nearest k — it is the LIMIT, so no
	// separate LIMIT clause (sqlite-vec refuses both in one query). A bug_vec
	// row carries its own bug_id, so there is no outer join to limit.
	rows, err := s.r.Query(
		`SELECT bug_id, distance FROM `+table+` WHERE embedding MATCH ? AND k = ? ORDER BY distance`,
		string(raw), limit)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil // nothing embedded in this project yet
		}
		return nil, fmt.Errorf("store: bug vec query %s: %w", root, err)
	}
	defer rows.Close()
	var out []BugVecHit
	for rows.Next() {
		var h BugVecHit
		if err := rows.Scan(&h.BugID, &h.Distance); err != nil {
			return nil, fmt.Errorf("store: scan bug vec hit %s: %w", root, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
