package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// S28 T2 — the learnings SQL. The MEANING (the three scopes, the four
// provenances, the agent-scope validation, the tag filter) lives in
// internal/learnings; this file is the handle's side: rows in, rows out,
// and the vec0 on-demand (T3) that S22's retrieval proved generically
// working. Vocabulary closes at the database (the migration's CHECKs), so
// no Go constant is load-bearing here — one table, one CHECK, one truth.
//
// The agent scope has NO foreign key (design §3): a learning's scope_key
// is a plain TEXT, and the Go-level check in internal/learnings (against
// S13's registry or a plugin_agent row) is the one place the check lives.
// SQLite cannot express "a valid agent," so the handle carries no FK to
// invent a wrong truth.

// LearningRow mirrors learning's columns exactly, nullables as pointers,
// and carries the learning's own tags (a join the read methods fill).
// internal/learnings converts it into the package's Learning; the row
// type carries no behaviour on purpose — behaviour here would be a second
// place that knows what `relayed` means.
type LearningRow struct {
	ID             int64
	Scope          string
	ScopeKey       string
	Body           string
	Provenance     string
	ObservedCount  *int
	SourceLoopSlug *string
	SourceStepID   *int
	CreatedAt      string
	// S52 T1 (LM2): the retraction is a stamp, never a delete. An active
	// learning has RetractedAt nil; the retract set the instant, the
	// person's reason and who stamped it. Restore clears the instant and
	// blanks the two — the audit keeps the words, the row keeps the state.
	RetractedAt     *string
	RetractedReason string
	RetractedBy     string
	// Tags is filled by the read methods (ListLearnings / GetLearning /
	// QueryLearnings) from learning_tag, not by the raw insert. A write
	// carries its tags in NewLearningRow; a read returns them joined.
	Tags []string
}

// NewLearningRow is the insert shape. Identity (ID) and CreatedAt are the
// store's; scope_key is plain TEXT (no FK, §3). Tags travel with the
// write — a learning and its tags land in one transaction.
type NewLearningRow struct {
	Scope          string
	ScopeKey       string
	Body           string
	Provenance     string
	ObservedCount  *int
	SourceLoopSlug *string
	SourceStepID   *int
	Tags           []string
}

// TagPattern is one tag the query asks for: a Pattern matched by Prefix
// (LIKE 'Pattern%') or by exact equality. M5's two promises.
type TagPattern struct {
	Pattern string
	Prefix  bool
}

// LearningFilters narrows a plain read — scope and scope_key, the empty
// string meaning no filter on that column (the roadmap ListFilter's shape).
// LearningFilters is the plain read's shape. IncludeRetracted (S52 LM2)
// is the ONE explicit exception in the default-off world: the flag must be
// deliberately set for a listing to see a retracted learning, because the
// default reading is "what the daemon would act on", and a retracted body
// is history, not memory.
type LearningFilters struct {
	IncludeRetracted bool
	Scope            string
	ScopeKey         string
}

// LearningQuery is the tag-filtered read (D5): scope/scope_key plus a set
// of tag patterns. A learning is included only if it carries a row in
// learning_tag matching EVERY pattern (AND) — the "what does this agent
// know about auth" narrowing, exact or prefix, never an embedding
// similarity that the tag filter would have excluded (D6).
type LearningQuery struct {
	// IncludeRetracted is LM2's explicit exception, same meaning as on
	// LearningFilters: default false, deliberate true.
	IncludeRetracted bool
	Scope            string
	ScopeKey         string
	Tags             []TagPattern
}

const learningColumns = `id, scope, scope_key, body, provenance,
	observed_count, source_loop_slug, source_step_id, created_at,
	retracted_at, retracted_reason, retracted_by`

func scanLearningRow(row interface{ Scan(dest ...any) error }) (LearningRow, error) {
	var r LearningRow
	err := row.Scan(&r.ID, &r.Scope, &r.ScopeKey, &r.Body, &r.Provenance,
		&r.ObservedCount, &r.SourceLoopSlug, &r.SourceStepID, &r.CreatedAt,
		&r.RetractedAt, &r.RetractedReason, &r.RetractedBy)
	return r, err
}

// --- learning_change: the S52 audit rows ------------------------------------

// LearningChangeRow mirrors learning_change's columns exactly. The table is
// append-only by trigger (S52 LM3) — no method here updates or deletes a
// line, and the database would refuse it if one tried.
type LearningChangeRow struct {
	ID         int64
	LearningID int64
	Op         string // add | retract | restore
	Summary    string
	ActorKind  string // user | cli | agent | system
	ActorID    string
	OccurredAt string
}

// NewLearningChange is one audit line as a writer hands it over. LearningID
// and OccurredAt are the writer's, not the caller's: the id is the row the
// act is about (0 for an add, whose id the insert just learned), and the
// timestamp is the store's clock, shared with the row it describes so the
// audit line and the row cannot tell two different stories about WHEN.
type NewLearningChange struct {
	LearningID int64
	Op         string
	Summary    string
	ActorKind  string
	ActorID    string
}

// CreateLearning inserts one learning WITHOUT an audit line. It is the
// legacy shape, kept for the callers and tests that predate S52 and for
// fixture seeds — production writes go through CreateLearningAudited, which
// is the one writer. A learning written here has no learning_change row,
// and `learnings.history` will honestly say the add was never audited.
func (s *Store) CreateLearning(in NewLearningRow) (LearningRow, error) {
	return s.CreateLearningAudited(in, nil)
}

// CreateLearningAudited inserts one learning and — when ch is non-nil —
// its one `add` audit line, in ONE transaction: the row and its memory of
// being written land together or not at all, which is what makes an audit
// that trails the write by a second call (and can miss) impossible by
// construction (S52 LM3). The audit shape is checked BEFORE any row is
// touched: an op outside the three, an actor kind the CHECK would refuse,
// an empty actor, a summary past 1024, or a caller pretending to know the
// id of a row not yet inserted all refuse here, in Go, with a sentence —
// not as a CHECK's driver error two statements deep.
//
// ch.LearningID must be 0: the id is what this insert produces, and a
// caller that names it is writing an audit line for a row it did not write
// through this call. The stamp is s.Now(), the same instant the row gets.
func (s *Store) CreateLearningAudited(in NewLearningRow, ch *NewLearningChange) (LearningRow, error) {
	if in.Scope == "" || in.ScopeKey == "" || in.Body == "" || in.Provenance == "" {
		return LearningRow{}, fmt.Errorf("store: a learning needs scope, scope_key, body and provenance — the empty write is the thing that gets a default and a default answers on a caller's behalf")
	}
	if ch != nil {
		if ch.LearningID != 0 {
			return LearningRow{}, fmt.Errorf("store: refused an add audit line naming learning %d — the id of an add is the insert's to learn, never the caller's to name", ch.LearningID)
		}
		if ch.Op != "add" {
			return LearningRow{}, fmt.Errorf("store: refused op %q on a create audit — an insert's audit line is an add; retract and restore stamp existing rows, they do not create one", ch.Op)
		}
		if ch.Summary == "" || len(ch.Summary) > 1024 {
			return LearningRow{}, fmt.Errorf("store: an audit summary is 1..1024 bytes — %d is not a sentence about the act", len(ch.Summary))
		}
		switch ch.ActorKind {
		case "user", "cli", "agent", "system":
		default:
			return LearningRow{}, fmt.Errorf("store: refused actor_kind %q — the audit knows user, cli, agent and system, and nobody else may have written anything", ch.ActorKind)
		}
		if ch.ActorID == "" {
			return LearningRow{}, fmt.Errorf("store: an audit line names who acted — an empty actor is the anonymous write the audit exists to refuse")
		}
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return LearningRow{}, fmt.Errorf("store: begin learning insert: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO learning (scope, scope_key, body, provenance, observed_count, source_loop_slug, source_step_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		in.Scope, in.ScopeKey, in.Body, in.Provenance, in.ObservedCount, in.SourceLoopSlug, in.SourceStepID, now,
	)
	if err != nil {
		return LearningRow{}, fmt.Errorf("store: insert learning: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearningRow{}, fmt.Errorf("store: last id for learning: %w", err)
	}
	for _, tag := range in.Tags {
		if strings.TrimSpace(tag) == "" {
			return LearningRow{}, fmt.Errorf("store: a tag must be a non-empty string — the CHECK refuses the empty one, so a partial tag is refused here too, not as a driver error")
		}
		if _, err := tx.Exec(`INSERT INTO learning_tag (learning_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return LearningRow{}, fmt.Errorf("store: insert learning_tag (%q): %w", tag, err)
		}
	}
	if ch != nil {
		if _, err := tx.Exec(
			`INSERT INTO learning_change (learning_id, op, summary, actor_kind, actor_id, occurred_at)
			 VALUES (?,?,?,?,?,?)`,
			id, ch.Op, ch.Summary, ch.ActorKind, ch.ActorID, now,
		); err != nil {
			return LearningRow{}, fmt.Errorf("store: insert learning_change for learning %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return LearningRow{}, fmt.Errorf("store: commit learning insert: %w", err)
	}
	got, ok, err := s.GetLearning(id)
	if err != nil {
		return LearningRow{}, fmt.Errorf("store: read learning after insert: %w", err)
	}
	if !ok {
		return LearningRow{}, fmt.Errorf("store: learning %d vanished after insert", id)
	}
	return got, nil
}

// ListLearningChanges reads one learning's audit lines, oldest first — the
// history a retracted body leaves behind (S52 T2's `learnings.history`
// reads through here). Every line names its actor and its instant.
func (s *Store) ListLearningChanges(learningID int64) ([]LearningChangeRow, error) {
	rows, err := s.r.Query(`SELECT id, learning_id, op, summary, actor_kind, actor_id, occurred_at
		FROM learning_change WHERE learning_id = ? ORDER BY id`, learningID)
	if err != nil {
		return nil, fmt.Errorf("store: read learning %d changes: %w", learningID, err)
	}
	defer rows.Close()
	var out []LearningChangeRow
	for rows.Next() {
		var c LearningChangeRow
		if err := rows.Scan(&c.ID, &c.LearningID, &c.Op, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: scan learning %d change: %w", learningID, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read learning %d changes: %w", learningID, err)
	}
	return out, nil
}

// CountAllLearningChanges is the census the guards and `db info` count
// against: every audit line in the store.
func (s *Store) CountAllLearningChanges() (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM learning_change`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count learning changes: %w", err)
	}
	return n, nil
}

// LearningsScopedTo counts the learnings an agent scope still OWNS —
// active or retracted both: an agent an operator deletes must not leave
// even its retracted memory behind without saying so, so the agentmgr
// refusal counts the whole scope and names the number (S52 LM7; a
// retracted learning is still a row a stranger would inherit).
func (s *Store) LearningsScopedTo(slug string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM learning WHERE scope = 'agent' AND scope_key = ?`, slug).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count learnings scoped to %q: %w", slug, err)
	}
	return n, nil
}

// GetLearning reads one learning by id WITH its tags. false is honest
// absence, not an error.
func (s *Store) GetLearning(id int64) (LearningRow, bool, error) {
	row, err := scanLearningRow(s.r.QueryRow(`SELECT `+learningColumns+` FROM learning WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return LearningRow{}, false, nil
		}
		return LearningRow{}, false, fmt.Errorf("store: read learning %d: %w", id, err)
	}
	if err := s.tagsForLearning(id, &row); err != nil {
		return LearningRow{}, false, err
	}
	return row, true, nil
}

// tagsForLearning fills a row's Tags from learning_tag. A learning with no
// tags is the normal state (tags are free-form, M5) — empty, not an error.
func (s *Store) tagsForLearning(id int64, row *LearningRow) error {
	rows, err := s.r.Query(`SELECT tag FROM learning_tag WHERE learning_id = ? ORDER BY tag`, id)
	if err != nil {
		return fmt.Errorf("store: read learning %d tags: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return fmt.Errorf("store: scan learning %d tag: %w", id, err)
		}
		row.Tags = append(row.Tags, tag)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read learning %d tags: %w", id, err)
	}
	return nil
}

// ListLearnings reads learnings by scope and scope_key, each WITH its tags.
// A scope with an empty scope_key is refused by the caller (internal/
// learnings), not here — the handle trusts its filter.
// learningLiveSQL is S52 LM2's shared predicate — the one spelling of
// "not retracted" that every default reader of `learning` composes. The
// guard (internal/learnings' TestEveryDefaultReaderExcludesRetracted) walks
// this file's SELECTs FROM `learning` and refuses one that omits it without
// a named exception: GetLearning (Show-by-id — LM2 returns it FLAGGED),
// LearningsScopedTo (LM7 counts retracted too — they are still its memory)
// and MigrateLearningAgentScopeKeys (scope keys move whatever their state).
// A reader that forgets the predicate does not fail loudly; it leaks a
// retracted body into a prompt. That is why it is one constant, not a
// sentence typed per query.
const learningLiveSQL = ` AND retracted_at IS NULL`

func (s *Store) ListLearnings(f LearningFilters) ([]LearningRow, error) {
	q := `SELECT ` + learningColumns + ` FROM learning WHERE 1=1`
	var args []any
	if !f.IncludeRetracted {
		q += learningLiveSQL
	}
	if f.Scope != "" {
		q += ` AND scope = ?`
		args = append(args, f.Scope)
	}
	if f.ScopeKey != "" {
		q += ` AND scope_key = ?`
		args = append(args, f.ScopeKey)
	}
	q += ` ORDER BY created_at, id`
	return s.queryLearningsRows(q, args)
}

// QueryLearnings is the tag-filtered read (D5). The tag filter runs first
// and always: a learning is included only if it carries a row in
// learning_tag matching EVERY pattern (an EXISTS per pattern, ANDed). The
// set it returns is the one internal/learnings ranks by embedding (T3);
// the store never widens it — a learning the tag filter excluded is never
// reachable through similarity (D6).
func (s *Store) QueryLearnings(q LearningQuery) ([]LearningRow, error) {
	sql := `SELECT ` + learningColumns + ` FROM learning WHERE 1=1`
	var args []any
	if !q.IncludeRetracted {
		sql += learningLiveSQL
	}
	if q.Scope != "" {
		sql += ` AND scope = ?`
		args = append(args, q.Scope)
	}
	if q.ScopeKey != "" {
		sql += ` AND scope_key = ?`
		args = append(args, q.ScopeKey)
	}
	// Each tag pattern is one EXISTS: the learning must have a row in
	// learning_tag matching it. AND across patterns is the "every tag"
	// reading; the index on (tag, learning_id) is what each EXISTS uses.
	for _, tp := range q.Tags {
		if strings.TrimSpace(tp.Pattern) == "" {
			continue
		}
		if tp.Prefix {
			sql += ` AND EXISTS (SELECT 1 FROM learning_tag WHERE learning_id = learning.id AND tag LIKE ?)`
			args = append(args, tp.Pattern+"%")
		} else {
			sql += ` AND EXISTS (SELECT 1 FROM learning_tag WHERE learning_id = learning.id AND tag = ?)`
			args = append(args, tp.Pattern)
		}
	}
	sql += ` ORDER BY created_at, id`
	return s.queryLearningsRows(sql, args)
}

// queryLearningsRows runs a learning SELECT and fills each row's Tags.
func (s *Store) queryLearningsRows(sql string, args []any) ([]LearningRow, error) {
	rows, err := s.r.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query learnings: %w", err)
	}
	defer rows.Close()
	var out []LearningRow
	for rows.Next() {
		row, err := scanLearningRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan learning row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query learnings: %w", err)
	}
	for i := range out {
		if err := s.tagsForLearning(out[i].ID, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// --- vec0 on-demand (T3) --------------------------------------------------
//
// The learning_vec vec0 is NOT a migration (its embedding dimension is
// data, fixed at CREATE time — S22's retrieval precedent, 0040's own doc
// comment: "its dimension is data"). It arrives on demand, named by the
// dimension, and a learning with no configured route has no row, which is
// the normal state (D5), not a gap. The vec0 is keyed by learning_id
// (globally unique), so one table per dimension holds every learning.

// LearningVecHit is one nearest-neighbour result: the learning id and its
// score, a distance negated to a penalty's inverse (bigger-is-better, the
// one sign discipline the retrieval vec0 crosses under).
type LearningVecHit struct {
	LearningID int64
	Score      float64
}

// learningVecTableName derives the on-demand vec0 name:
// learning_vec_<model[:8]>_d<dim>. The model's first eight chars and the
// dimension keep two routes that agree on a dimension but differ on a model
// from sharing a table — a different model is a different geometry, not a
// stale one. The hex is derived, not a caller's word, so the name is
// safe to assemble.
func learningVecTableName(model string, dim int) (string, error) {
	if dim < 0 {
		return "", fmt.Errorf("store: refused a learning vec table at %d dims — a negative dimension is never geometry", dim)
	}
	sum := sha256.Sum256([]byte(model))
	return fmt.Sprintf("learning_vec_%s_d%d", hex.EncodeToString(sum[:])[:8], dim), nil
}

// EnsureLearningVecTable creates (idempotently) the on-demand vec0 for one
// model+dimension. A "no such table" is the normal state, not an error:
// a learning with no configured route simply has no row.
func (s *Store) EnsureLearningVecTable(model string, dim int) (string, error) {
	table, err := learningVecTableName(model, dim)
	if err != nil {
		return "", err
	}
	if _, err := s.w.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding FLOAT[%d], learning_id integer)`,
		table, dim)); err != nil {
		return "", fmt.Errorf("store: create learning vec table (model %q, %d dims): %w", model, dim, err)
	}
	return table, nil
}

// UpsertLearningVec stores one learning's vector. The delete-then-insert
// is the upsert (vec0 has no ON CONFLICT); a re-embedded learning replaces,
// never doubles — two vectors for one learning is a result that can rank
// twice for the same body. The dimension is checked here, against the
// table's own construction, because a wrong-length vector is the
// corruption the table name (which carries the dimension) promises cannot
// happen.
func (s *Store) UpsertLearningVec(id int64, model string, dim int, vector []float32) error {
	if len(vector) != dim {
		return fmt.Errorf("store: refused a vector of %d dimensions for learning %d at %d dims — the table's name carries the dimension precisely so this mismatch is impossible",
			len(vector), id, dim)
	}
	table, err := learningVecTableName(model, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(`DELETE FROM `+table+` WHERE learning_id = ?`, id); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil // nothing was ever embedded; the upsert already happened by absence
		}
		return fmt.Errorf("store: replacing vector for learning %d (%s): %w", id, table, err)
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return fmt.Errorf("store: encode vector for learning %d: %w", id, err)
	}
	if _, err := s.w.Exec(`INSERT INTO `+table+`(learning_id, embedding) VALUES(?, ?)`, id, string(raw)); err != nil {
		return fmt.Errorf("store: store vector for learning %d (%s): %w", id, table, err)
	}
	return nil
}

// DeleteLearningVec drops one learning's vector. "no such table" is the
// normal state (nothing was ever embedded), not an error.
func (s *Store) DeleteLearningVec(id int64, model string, dim int) error {
	table, err := learningVecTableName(model, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(`DELETE FROM `+table+` WHERE learning_id = ?`, id); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		return fmt.Errorf("store: delete vector for learning %d (%s): %w", id, table, err)
	}
	return nil
}

// LearningVecIDs reports which learning ids already have vectors at a
// dimension — the resume check for an embedding pass (a learning whose
// body did not move but whose vector is missing is re-embedded; one with
// a vector is left). A "no such table" is an empty set, not an error.
func (s *Store) LearningVecIDs(model string, dim int) ([]int64, error) {
	table, err := learningVecTableName(model, dim)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.Query(`SELECT learning_id FROM ` + table)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, fmt.Errorf("store: read learning vec ids (%s): %w", table, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan learning vec id (%s): %w", table, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read learning vec ids (%s): %w", table, err)
	}
	return out, nil
}

// VectorSearchLearning answers nearest-neighbours for a query vector. A
// query of the wrong length is refused here — comparing different
// geometries returns numbers, not neighbours. Distance is a penalty
// (smaller is nearer); the domain wants bigger-is-better, so it crosses as
// its negation. A "no such table" is an empty set, not an error: the
// caller's availability check answers "no route," and that is the normal
// state (D5).
func (s *Store) VectorSearchLearning(model string, dim int, vector []float32, limit int) ([]LearningVecHit, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("store: refused a learning vector search at %d dims — a 0 dimension means no embedding pass ever ran", dim)
	}
	if len(vector) != dim {
		return nil, fmt.Errorf("store: refused a learning vector query of %d dimensions at %d dims — comparing different geometries returns numbers, not neighbours",
			len(vector), dim)
	}
	if limit <= 0 {
		limit = 10
	}
	table, err := learningVecTableName(model, dim)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return nil, fmt.Errorf("store: encode learning query vector: %w", err)
	}
	rows, err := s.r.Query(`
		SELECT learning_id, -distance
		FROM (SELECT learning_id, distance FROM `+table+` WHERE embedding MATCH ? AND k = ?)
		ORDER BY distance`, string(raw), limit)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil // nothing embedded — the caller's availability check answers this
		}
		return nil, fmt.Errorf("store: learning vector search (%s): %w", table, err)
	}
	defer rows.Close()
	var out []LearningVecHit
	for rows.Next() {
		var h LearningVecHit
		if err := rows.Scan(&h.LearningID, &h.Score); err != nil {
			return nil, fmt.Errorf("store: scan learning vector hit (%s): %w", table, err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: learning vector search (%s): %w", table, err)
	}
	return out, nil
}

// LearningVectorSet is every stored learning vector of ONE model+dimension geometry, read back for the
// knowledge map. The set is whatever the embedding pass has stored so far: a learning with no vector is
// simply not in it, which is the normal state (D5), never a gap to fill with a guess.
type LearningVectorSet struct {
	Table   string
	Dim     int
	Vectors map[int64][]float32
}

var learningVecTableRE = regexp.MustCompile(`^learning_vec_([0-9a-f]{8})_d([0-9]+)$`)

// ReadLearningVectors reads the vectors of the geometry that holds the most learnings. When model is
// non-empty only that model's tables are considered (a different model is a different geometry, so two
// are never mixed); with no model — no route configured today — the biggest table that exists wins, so a
// route removed later does not hide the vectors it already stored. Nothing embedded answers a nil set,
// not an error.
func (s *Store) ReadLearningVectors(model string) (*LearningVectorSet, error) {
	rows, err := s.r.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'learning\_vec\_%' ESCAPE '\'`)
	if err != nil {
		return nil, fmt.Errorf("store: list learning vec tables: %w", err)
	}
	type candidate struct {
		name string
		dim  int
	}
	var tables []candidate
	want := ""
	if model != "" {
		sum := sha256.Sum256([]byte(model))
		want = hex.EncodeToString(sum[:])[:8]
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: scan learning vec table name: %w", err)
		}
		m := learningVecTableRE.FindStringSubmatch(name)
		if m == nil || (want != "" && m[1] != want) {
			continue // a vec0 shadow table, or another model's geometry
		}
		dim, _ := strconv.Atoi(m[2])
		tables = append(tables, candidate{name: name, dim: dim})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: list learning vec tables: %w", err)
	}
	rows.Close()

	var best *LearningVectorSet
	for _, c := range tables {
		set, err := s.readLearningVecTable(c.name, c.dim)
		if err != nil {
			return nil, err
		}
		if best == nil || len(set.Vectors) > len(best.Vectors) {
			best = set
		}
	}
	return best, nil
}

func (s *Store) readLearningVecTable(table string, dim int) (*LearningVectorSet, error) {
	rows, err := s.r.Query(`SELECT learning_id, embedding FROM ` + table)
	if err != nil {
		return nil, fmt.Errorf("store: read learning vectors (%s): %w", table, err)
	}
	defer rows.Close()
	set := &LearningVectorSet{Table: table, Dim: dim, Vectors: map[int64][]float32{}}
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("store: scan learning vector (%s): %w", table, err)
		}
		vec, ok := decodeVector(raw, dim)
		if !ok {
			continue // a row that is not this geometry is not a vector of it
		}
		set.Vectors[id] = vec
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read learning vectors (%s): %w", table, err)
	}
	return set, nil
}

// decodeVector reads a vec0 embedding: little-endian float32 bytes, or (for a build that hands the JSON
// text back) a JSON array. Anything of the wrong length is refused, never padded.
func decodeVector(raw []byte, dim int) ([]float32, bool) {
	if len(raw) == dim*4 {
		out := make([]float32, dim)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return out, true
	}
	var parsed []float32
	if json.Unmarshal(raw, &parsed) == nil && len(parsed) == dim {
		return parsed, true
	}
	return nil, false
}

// --- LM1: agent scope_keys become slugs (S52 T1) ----------------------------
//
// Before S52, an agent-scoped learning's scope_key was whatever the writer
// typed — a name, usually, because S28's §3 check accepted a name. S43
// settled the matter: the agent's SLUG is its identity, and every scoped
// thing must key on it (S52's scope_key law: agent scope_key == the S43
// slug). The rows written before that ruling still carry names, and a name
// is a string another agent can be renamed past — so the keys move, once,
// as a Go step, because resolving name→slug is a JOIN no SQLite UPDATE can
// express without deciding which row wins when two agents share a name.
//
// The step runs inside Migrate(), so it lands with the schema it prepares:
// every open after this one validates slugs (internal/learnings' scope
// check reads AgentBySlug). It is check-first (a store with no legacy rows
// pays one SELECT and returns), idempotent (moved rows stop matching), and
// all-or-nothing: a name that resolves to no agent refuses the WHOLE step
// and names every unresolved one. Guessing a slug from a fuzzy name is
// how memory gets attributed to the wrong agent — worse than the status
// quo, which is at least honest about being a name.

// MigrateLearningAgentScopeKeys rewrites every agent-scoped learning whose
// scope_key is not already an agent slug to that agent's slug, resolving
// pre-S43 NAMES through AgentByName (newest row wins — a name reused by a
// later agent is the second agent's by possession; the first has moved on
// and its slug answers for its own memory). One transaction: either every
// legacy key moves or none does, and a refusal naming every unresolved key
// is the caller's (store.Open) to make the store refuse to open. The count
// is what moved; 0 on a store that never wrote a name is the normal answer.
func (s *Store) MigrateLearningAgentScopeKeys() (int, error) {
	// Check first, on the reader pool: the overwhelming majority of opens
	// are this one SELECT.
	rows, err := s.r.Query(`SELECT DISTINCT scope_key FROM learning
		WHERE scope = 'agent' AND scope_key <> ''
		  AND NOT EXISTS (SELECT 1 FROM agent WHERE agent.slug = learning.scope_key)`)
	if err != nil {
		return 0, fmt.Errorf("store: read legacy agent scope_keys: %w", err)
	}
	var legacy []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: scan legacy agent scope_key: %w", err)
		}
		legacy = append(legacy, k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("store: read legacy agent scope_keys: %w", err)
	}
	rows.Close()
	if len(legacy) == 0 {
		return 0, nil
	}
	// Resolve every key before touching anything — the refusal names ALL
	// the strays, not just the first, because an operator fixing them one
	// open at a time is the third truth about the same memory.
	moves := make([][2]string, 0, len(legacy))
	var unresolved []string
	for _, key := range legacy {
		a, ok, err := s.AgentByName(key)
		if err != nil {
			return 0, fmt.Errorf("store: resolve agent name %q for the LM1 scope_key step: %w", key, err)
		}
		if !ok || a.Slug == "" {
			unresolved = append(unresolved, key)
			continue
		}
		moves = append(moves, [2]string{key, a.Slug})
	}
	if len(unresolved) > 0 {
		return 0, fmt.Errorf("store: the LM1 scope_key step refuses — %d agent scope_key(s) resolve to no agent: %s; fix the keys or give the agents rows, the step refuses rather than guess, never leaves memory half-moved",
			len(unresolved), strings.Join(quoteAll(unresolved), ", "))
	}
	tx, err := s.w.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: begin LM1 scope_key step: %w", err)
	}
	defer tx.Rollback()
	moved := 0
	for _, mv := range moves {
		res, err := tx.Exec(`UPDATE learning SET scope_key = ? WHERE scope = 'agent' AND scope_key = ?`, mv[1], mv[0])
		if err != nil {
			return 0, fmt.Errorf("store: move agent scope_key %q to slug %q: %w", mv[0], mv[1], err)
		}
		n, _ := res.RowsAffected()
		moved += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit LM1 scope_key step: %w", err)
	}
	return moved, nil
}

// SetLearningRetraction stamps one learning's retraction (S52 LM2), or
// clears it — `retract` true stamps retracted_at/reason/by, false clears
// the instant (retracted_at back to NULL; the reason and the by survive as
// what the last retraction said — the stamps describe the state, and the
// state after a restore is active, proven by retracted_at alone).
//
// It answers THREE things and the caller must read all three: whether the
// row was FOUND, whether the stamp CHANGED, and the row as it stands after
// the call. An already-retracted retract (or an already-active restore) is
// the LM2 idempotence: found, NOT changed, the row unchanged, and — the
// part the audit depends on — NO second change line: re-retracting is not
// an act, and an audit of non-acts is an audit nobody trusts.
//
// The stamp and its audit line share one transaction and one s.Now() stamp,
// exactly like the add. The store here accepts op retract|restore with the
// row's state as witness; the actor checks (kind, non-empty id) ride the
// caller's words — the CHECK in the schema is the backstop, the Go check is
// the sentence. The reason is the human's text (bounded by the caller at
// 1..256); the AUDIT SUMMARY is the fixed template — the reason lives in
// the row's stamp, because a summary made of caller prose is a summary that
// can carry the body inside it, and the body never enters the audit.
func (s *Store) SetLearningRetraction(id int64, retract bool, reason, actorKind, actorID string, ch *NewLearningChange) (LearningRow, bool, bool, error) {
	if ch != nil {
		if ch.Op != "retract" && ch.Op != "restore" {
			return LearningRow{}, false, false, fmt.Errorf("store: refused op %q on a retraction audit — this writer stamps retract and restore; an add creates rows and rides CreateLearningAudited", ch.Op)
		}
		if ch.LearningID != 0 && ch.LearningID != id {
			return LearningRow{}, false, false, fmt.Errorf("store: refused a %s audit naming learning %d while stamping %d — one act, one row", ch.Op, ch.LearningID, id)
		}
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return LearningRow{}, false, false, fmt.Errorf("store: begin retraction for learning %d: %w", id, err)
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRow(`SELECT COALESCE(retracted_at, '') FROM learning WHERE id = ?`, id).Scan(&current)
	if err == sql.ErrNoRows {
		return LearningRow{}, false, false, nil
	}
	if err != nil {
		return LearningRow{}, false, false, fmt.Errorf("store: read learning %d for retraction: %w", id, err)
	}
	retracted := current != ""
	if retracted == retract {
		// The LM2 idempotence, decided on the row, not on a caller's hope.
		got, _, err := s.GetLearning(id)
		return got, true, false, err
	}
	if retract {
		if _, err := tx.Exec(`UPDATE learning SET retracted_at = ?, retracted_reason = ?, retracted_by = ? WHERE id = ?`,
			now, reason, actorKind+"/"+actorID, id); err != nil {
			return LearningRow{}, false, false, fmt.Errorf("store: stamp retraction on learning %d: %w", id, err)
		}
	} else {
		// Restore clears the INSTANT; what the retraction said (reason, by)
		// survives beside the row as its recent history — retracted_at alone
		// is the state, and the state now reads active.
		if _, err := tx.Exec(`UPDATE learning SET retracted_at = NULL WHERE id = ?`, id); err != nil {
			return LearningRow{}, false, false, fmt.Errorf("store: clear retraction on learning %d: %w", id, err)
		}
	}
	if ch != nil {
		if _, err := tx.Exec(
			`INSERT INTO learning_change (learning_id, op, summary, actor_kind, actor_id, occurred_at)
			 VALUES (?,?,?,?,?,?)`,
			id, ch.Op, ch.Summary, ch.ActorKind, ch.ActorID, now,
		); err != nil {
			return LearningRow{}, false, false, fmt.Errorf("store: audit %s of learning %d: %w", ch.Op, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return LearningRow{}, false, false, fmt.Errorf("store: commit retraction of learning %d: %w", id, err)
	}
	got, found, err := s.GetLearning(id)
	if err != nil {
		return LearningRow{}, true, false, fmt.Errorf("store: read learning %d after retraction: %w", id, err)
	}
	return got, found, true, err
}

// LearningWithTx runs fn inside one transaction on the single writer. It
// twins BugWithTx's reason exactly: the learning and learning_change tables
// are written through the methods above, and the ONLY honest way a sibling
// guard test probes the database's own promises — the append-only triggers,
// the actor CHECKs — is through the store's one transaction seam, never by
// reaching the connection. Consumer: cmd/aeon/memory_guard_test.go (S52 T3).
func (s *Store) LearningWithTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: learning write transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DropLearningVectors removes one learning's vector from EVERY learning_vec
// table that exists (S52 LM5). It walks sqlite_master, not a caller's
// memory, because the vec tables are on-demand and named by (model, dim) —
// a body embedded under one route at one dimension must not survive its own
// retraction under a different one. "No such table" is the normal state
// (nothing was ever embedded), and here there is nothing to even fail on:
// the walk simply finds none.
//
// This is the hygiene half; the structural half is rankWithinSet's set
// intersection — a retracted body is already unreachable through similarity
// because it is not in the set the ranking reorders. Dropping the vector is
// what keeps the index honest even to a reader that bypasses the set.
func (s *Store) DropLearningVectors(id int64) (int, error) {
	// Only the MAIN vec0 tables — vec0 keeps shadow tables (<main>_chunks,
	// <main>_rowids, …) that answer to the same LIKE but do not carry a
	// learning_id column at all; writing to them by hand is how an index
	// gets corrupted instead of cleaned. The main tables are exactly the
	// ones whose DDL is "CREATE VIRTUAL TABLE … USING vec0".
	rows, err := s.r.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'learning_vec_%' AND sql LIKE '%USING vec0%' ORDER BY name`)
	if err != nil {
		return 0, fmt.Errorf("store: list learning vec tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: scan learning vec table name: %w", err)
		}
		tables = append(tables, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("store: read learning vec tables: %w", err)
	}
	rows.Close()
	dropped := 0
	for _, t := range tables {
		// The names come from sqlite_master itself — they were minted by
		// learningVecTableName. They are still the one interpolated kind of
		// value in this file, so they arrive through the same quoting the
		// rest of the store's DDL reads use.
		res, err := s.w.Exec(`DELETE FROM "`+strings.ReplaceAll(t, `"`, `""`)+`" WHERE learning_id = ?`, id)
		if err != nil {
			if strings.Contains(err.Error(), "no such table") {
				continue
			}
			return dropped, fmt.Errorf("store: drop vectors for learning %d from %s: %w", id, t, err)
		}
		// What the answer counts is VECTORS REMOVED, not tables walked:
		// dropping a never-embedded id answers 0, and a caller deciding
		// "was there anything to clean" reads the truth, not a census of
		// the index's layout.
		if n, err := res.RowsAffected(); err == nil {
			dropped += int(n)
		} else {
			dropped++ // a driver that cannot report affects still removed the row
		}
	}
	return dropped, nil
}

// AgentProjectRoots answers "which projects has this agent run in?" — the
// scope resolution S52 LM5's forAgent needs, read from where the running
// actually lives: step.agent carries the NAME the runtime writes (not the
// slug), and step.component_root carries the root it ran under — the same
// key every project-scoped learning rides since S38's OL1 rename (the
// triage feed files with bug.ComponentRoot; the board resolves the same
// column). The step row IS the running: no loop join to hope the engine
// kept filled. Empty is the honest answer for an agent that never ran;
// distinct, because an agent in a fifty-step loop ran in one project,
// fifty times.
func (s *Store) AgentProjectRoots(agentName string) ([]string, error) {
	rows, err := s.r.Query(`
		SELECT DISTINCT component_root
		FROM step
		WHERE agent = ? AND COALESCE(component_root, '') <> ''
		ORDER BY component_root`, agentName)
	if err != nil {
		return nil, fmt.Errorf("store: read project roots for agent %q: %w", agentName, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("store: scan project root for agent %q: %w", agentName, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read project roots for agent %q: %w", agentName, err)
	}
	return out, nil
}

// LearningAgentAddCountSince counts this agent's `add` audit lines from
// `since` (an isoUTC string; inclusive) to now — the ledger the S52 T4
// self-log rate law counts on. The count is a fact about rows, so it is
// asked of the ledger itself, never kept beside it: a counter the writer
// carries dies with the process and lies after a restart; the audit rows
// do not. Only actor_kind 'agent' answers — a person's adds at the human
// door are not what any agent-visible cap may count (LM4's provenance
// keeps the two kinds in different sentences).
func (s *Store) LearningAgentAddCountSince(agentID, since string) (int, error) {
	if agentID == "" {
		return 0, fmt.Errorf("store: counting an agent's adds needs its slug — an empty key counts nobody")
	}
	if since == "" {
		return 0, fmt.Errorf("store: counting adds over a window needs the window's start — a count since the epoch is not a rate law")
	}
	var n int
	err := s.r.QueryRow(
		`SELECT COUNT(*) FROM learning_change
		 WHERE op = 'add' AND actor_kind = 'agent' AND actor_id = ? AND occurred_at >= ?`,
		agentID, since,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count agent %q adds since %s: %w", agentID, since, err)
	}
	return n, nil
}
