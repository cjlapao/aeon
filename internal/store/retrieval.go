package store

// retrieval.go — S22 T1: the statements behind the retrieval index (V2, V4,
// V8, V12). Every retrieval-domain SQL statement lives here, in the store's
// own voice (the house pattern: automation.go, agent_tasks.go) — the
// internal/retrieval package is the DOMAIN (availability, fusion, validation)
// and holds no SQL of its own, which is also what keeps D1 clean: when the
// package is not built, these rows sit inert.
//
// The root is a COLUMN, never a database (V12): a worktree is a distinct
// root, and "the index is per project root" must be testable (it is —
// TestIndexIsPerProjectRoot), which is only possible while one store holds
// many roots and every query filters.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// RetrievalCorpus is one corpus row (V2: exactly two corpora exist today,
// and the KIND — retrieved or concluded — is a column, not a table: Stage 5's
// memory corpus must arrive as a row and a validator entry rather than as a
// migration onto live indexes, which is T1's one cheap decision now).
//
// EmbedModel/EmbedDim are the row's own answer to "what built your vectors"
// (V6): "" and 0 mean nothing has been embedded yet — an honest absence, not
// a zero dimension. They are the two fields a query compares a route against
// before spending a call (V17's refusal reads them), and they are NULL rather
// than a sentinel because a sentinel is a value a mismatch could match.
type RetrievalCorpus struct {
	ID            int64
	ComponentRoot string
	Name          string
	Kind          string // 'retrieved' | 'concluded' — 0040's CHECK closes the set
	EmbedModel    string
	EmbedDim      int
}

// EnsureRetrievalCorpus creates the corpus row for (root, name) if absent and
// returns it, idempotently. It writes kind='retrieved' and no embedding —
// the ONLY kind this slice writes. 'concluded' is not a parameter here on
// purpose: a corpus whose results are beliefs cannot be created by a call
// that cannot say where the beliefs came from; internal/retrieval's validator
// refuses the kind naming Stage 5, and 0040's CHECK is the twin that holds
// even against raw SQL.
func (s *Store) EnsureRetrievalCorpus(root, name string) (RetrievalCorpus, error) {
	if root == "" || name == "" {
		return RetrievalCorpus{}, fmt.Errorf("store: refused retrieval corpus — root and name are both required (a corpus that names neither belongs to no one)")
	}
	if _, err := s.w.Exec(`
		INSERT INTO retrieval_corpus (component_root, name, kind)
		VALUES (?, ?, 'retrieved')
		ON CONFLICT (component_root, name) DO NOTHING`, root, name); err != nil {
		return RetrievalCorpus{}, fmt.Errorf("store: ensure retrieval corpus %s/%s: %w", root, name, err)
	}
	c, ok, err := s.RetrievalCorpusRow(root, name)
	if err != nil {
		return RetrievalCorpus{}, err
	}
	if !ok {
		return RetrievalCorpus{}, fmt.Errorf("store: retrieval corpus %s/%s vanished between create and read", root, name)
	}
	return c, nil
}

// RetrievalCorpusRow reads one corpus row; found=false is an honest absence
// (the corpus was never created), which is NOT the same as one that exists
// and holds no chunks (that is index-empty — D2's distinction, kept apart by
// the caller reading the count, never by a collapsed answer here).
func (s *Store) RetrievalCorpusRow(root, name string) (RetrievalCorpus, bool, error) {
	var c RetrievalCorpus
	var embedModel sql.NullString
	var dimn sql.NullInt64
	err := s.r.QueryRow(`
		SELECT id, component_root, name, kind, embed_model, embed_dim
		FROM retrieval_corpus WHERE component_root = ? AND name = ?`, root, name).
		Scan(&c.ID, &c.ComponentRoot, &c.Name, &c.Kind, &embedModel, &dimn)
	if err == sql.ErrNoRows {
		return RetrievalCorpus{}, false, nil
	}
	if err != nil {
		return RetrievalCorpus{}, false, fmt.Errorf("store: read retrieval corpus %s/%s: %w", root, name, err)
	}
	c.EmbedModel = embedModel.String
	if dimn.Valid {
		c.EmbedDim = int(dimn.Int64)
	}
	return c, true, nil
}

// SetRetrievalCorpusEmbedding records what built the corpus's vectors (V6).
// It is the row's own statement, not a caller's claim: the embedding path
// (T3) records the route's model and dimension only after an embedding has
// actually been produced — a model recorded before a vector exists would let
// the mismatch gate pass on a lie.
func (s *Store) SetRetrievalCorpusEmbedding(corpusID int64, model string, dim int) error {
	if model == "" || dim <= 0 {
		return fmt.Errorf("store: refused embedding record — model and a positive dimension are both required (a recorded dimension of %d is a mismatch waiting to be trusted)", dim)
	}
	if _, err := s.w.Exec(`
		UPDATE retrieval_corpus SET embed_model = ?, embed_dim = ? WHERE id = ?`,
		model, dim, corpusID); err != nil {
		return fmt.Errorf("store: record embedding for corpus %d: %w", corpusID, err)
	}
	return nil
}

// ResetRetrievalCorpusEmbedding un-records what built a corpus's vectors —
// the forced rebuild's first move (S22 T8): the gate that refuses a
// foreign embedding space WIPES before it re-embeds, and a row still
// stamped with the superseded model would make the half-wiped corpus claim
// an identity it no longer has. The setter's own check (model and a
// positive dimension) is why the wipe needs its own door: it is recording
// "nothing", which the setter correctly refuses to mean.
func (s *Store) ResetRetrievalCorpusEmbedding(corpusID int64) error {
	if _, err := s.w.Exec(`
		UPDATE retrieval_corpus SET embed_model = '', embed_dim = 0 WHERE id = ?`,
		corpusID); err != nil {
		return fmt.Errorf("store: reset embedding record for corpus %d: %w", corpusID, err)
	}
	return nil
}

// RetrievalChunkCount answers "has anything been indexed" — the fact behind
// index-empty (D2/D3). It is a count and not a bool because staleness (T8)
// needs the number beside a second one.
func (s *Store) RetrievalChunkCount(root, name string) (int, error) {
	var n int
	err := s.r.QueryRow(`
		SELECT count(*) FROM retrieval_chunk c
		JOIN retrieval_corpus k ON k.id = c.corpus_id
		WHERE k.component_root = ? AND k.name = ?`, root, name).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count chunks for %s/%s: %w", root, name, err)
	}
	return n, nil
}

// UpsertSourceMtime records the mtime at which a source was last VERIFIED by
// the status walk (S71 T1) — the cheap gate's write-through. A source whose
// mtime matches its record is ASSUMED fresh and not re-read; a moved or
// absent record is a candidate. The record is written only for a source the
// walk confirmed fresh (its hash matched) or applied — never for one the
// walk found stale but did not re-index, because writing it then would make
// the next read skip a file that is still stale. The hash, not the mtime,
// is the content's truth (V8); the mtime is only the gate.
func (s *Store) UpsertSourceMtime(corpusID int64, source string, mtime int64) error {
	if _, err := s.w.Exec(`
		INSERT INTO retrieval_source_mtime (corpus_id, source, mtime)
		VALUES (?, ?, ?)
		ON CONFLICT (corpus_id, source) DO UPDATE SET mtime = excluded.mtime`,
		corpusID, source, mtime); err != nil {
		return fmt.Errorf("store: upsert source mtime for corpus %d %q: %w", corpusID, source, err)
	}
	return nil
}

// SourceMtime reads one source's recorded mtime; found=false is an honest
// absence — the source was never recorded (a first status read, or a file
// the walk has not yet gated), which is the candidate case (re-hash), not a
// "fresh" claim. An absent record is never read as 0, which would match a
// zero-mtime file and silently gate a first read (the gate must fail open
// to a re-read, never to a false "fresh").
func (s *Store) SourceMtime(corpusID int64, source string) (int64, bool, error) {
	var mtime sql.NullInt64
	err := s.r.QueryRow(`
		SELECT mtime FROM retrieval_source_mtime WHERE corpus_id = ? AND source = ?`,
		corpusID, source).Scan(&mtime)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: read source mtime for corpus %d %q: %w", corpusID, source, err)
	}
	if !mtime.Valid {
		return 0, false, nil
	}
	return mtime.Int64, true, nil
}

// InsertRetrievalChunk writes one chunk (V4's range, V8's hash, and the
// indexed body — which the QUERY never returns; Hit below has no field a
// body could arrive in, and that is D8 as a type rather than a promise).
// Re-inserting the same range replaces hash and body (the reindex path's
// upsert; the hash-equals-skip decision is T2's, above this).
func (s *Store) InsertRetrievalChunk(corpusID int64, source string, lineStart, lineEnd int, hash, body string) error {
	if source == "" || lineStart < 1 || lineEnd < lineStart || hash == "" {
		return fmt.Errorf("store: refused chunk %q %d-%d — source, a positive ordered range and a content hash are all required (V8's staleness reads the hash; a chunk without one cannot say it is current)", source, lineStart, lineEnd)
	}
	if _, err := s.w.Exec(`
		INSERT INTO retrieval_chunk (corpus_id, source, line_start, line_end, content_hash, body)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (corpus_id, source, line_start, line_end)
		DO UPDATE SET content_hash = excluded.content_hash, body = excluded.body`,
		corpusID, source, lineStart, lineEnd, hash, body); err != nil {
		return fmt.Errorf("store: insert chunk %s %d-%d: %w", source, lineStart, lineEnd, err)
	}
	return nil
}

// RetrievalHit is one keyword result: a LOCATION and a score, never text
// (V4, D8). The absence of a Body field is the assertion.
type RetrievalHit struct {
	Source    string
	LineStart int
	LineEnd   int
	Score     float64 // FTS5 rank, negated so bigger is better
}

// KeywordSearchRetrieval answers through FTS5, filtered by root AND corpus
// in the same statement (V2's "never merge" and V12's per-root are enforced
// here, where the join happens — not by trusting a caller to pre-filter; and
// O8's "a query names its corpus" arrives as a required argument).
//
// FTS5 MATCH syntax is the caller's business by T4; this is the door the
// hybrid fusion (T4) and the search tool (T6) call through.
func (s *Store) KeywordSearchRetrieval(root, name, query string, limit int) ([]RetrievalHit, error) {
	if root == "" || name == "" || query == "" {
		return nil, fmt.Errorf("store: refused keyword search — root, corpus and query are all required (there is no default corpus, O8)")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.r.Query(`
		SELECT c.source, c.line_start, c.line_end, bm25(retrieval_chunk_fts)
		FROM retrieval_chunk_fts f
		JOIN retrieval_chunk c ON c.id = f.rowid
		JOIN retrieval_corpus k ON k.id = c.corpus_id
		WHERE retrieval_chunk_fts MATCH ? AND k.component_root = ? AND k.name = ?
		ORDER BY bm25(retrieval_chunk_fts)
		LIMIT ?`, query, root, name, limit)
	if err != nil {
		return nil, fmt.Errorf("store: keyword search %s/%s: %w", root, name, err)
	}
	defer rows.Close()

	var out []RetrievalHit
	for rows.Next() {
		var h RetrievalHit
		if err := rows.Scan(&h.Source, &h.LineStart, &h.LineEnd, &h.Score); err != nil {
			return nil, fmt.Errorf("store: keyword search %s/%s: %w", root, name, err)
		}
		// bm25 is better when SMALLER (it is a penalty); the domain wants
		// bigger-is-better like every other score here, so it is negated at
		// the boundary rather than letting each caller remember the sign.
		h.Score = -h.Score
		out = append(out, h)
	}
	return out, rows.Err()
}

// vecCorpusName keeps a corpus name to what a bare SQL identifier can hold.
// It exists because the name rides into a CREATE statement: the corpus names
// internal/retrieval validates are [a-z]+ anyway, and the gate HERE means a
// caller that skips that validator still cannot inject an identifier.
var vecCorpusName = regexp.MustCompile(`^[a-z]{1,32}$`)

// EnsureRetrievalVecTable creates (idempotently) the vec0 virtual table for
// retrievalVecTableName derives (and gates) the vec0 table name:
// retrieval_vec_<corpus>_d<dim>_<sha256(root)[:16]>. Every piece is either
// gated (corpus ^[a-z]{1,32}$) or derived (root hashed, dim an int past a
// positivity check), because identifiers cannot be parameter-bound in
// SQLite — gate plus derivation is the only safe path.
func retrievalVecTableName(root, corpus string, dim int) (string, error) {
	// dim >= 0, not > 0: EnsureRetrievalVecTable and the upsert/delete doors
	// keep their POSITIVE-dimension gate at their own heads (0 dims is no
	// geometry to build), while VectorSearchRetrieval passes the row's
	// recorded dim through and answers dim-0 itself with the V6 sentence.
	// Negative is refused here — it is never legitimate at any door.
	if root == "" || !vecCorpusName.MatchString(corpus) || dim < 0 {
		return "", fmt.Errorf("store: refused vec table — root required, corpus %q must be lowercase letters only, dimension %d must be non-negative (an identifier a caller controls is an identifier a caller can inject)", corpus, dim)
	}
	sum := sha256.Sum256([]byte(root))
	return fmt.Sprintf("retrieval_vec_%s_d%d_%s", corpus, dim, hex.EncodeToString(sum[:])[:16]), nil
}

// one root+corpus at one dimension, returning its name (V1: sqlite-vec lives
// in this database, statically registered by DB18, so creation is a
// statement, not a load step).
//
// It is NOT in the migration, and that is a measurement rather than a
// shortcut: a vec0 table fixes its embedding dimension AT CREATE TIME, and
// V6 makes model and dimension DATA (the corpus row answers "what built
// your vectors", and a migration cannot know a route that may not exist
// yet). So the table arrives when the first embedding fixes the dimension,
// and its name carries that dimension plus a hash of the root — two roots
// never share one table (V12), and a dimension change names a new table
// rather than corrupting the old one (the mismatch refusal, V17, points at
// exactly that gap).
func (s *Store) EnsureRetrievalVecTable(root, name string, dim int) (string, error) {
	table, err := retrievalVecTableName(root, name, dim)
	if err != nil {
		return "", err
	}
	// dim is an int past a positivity gate and name a hashed hex string; the
	// one caller-controlled word is gated by the regexp above, so Sprintf is
	// assembly from safe pieces, not interpolation of input.
	if _, err := s.w.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding FLOAT[%d], chunk_id integer)`,
		table, dim)); err != nil {
		return "", fmt.Errorf("store: create vec table for %s/%s at %d dims: %w", root, name, dim, err)
	}
	return table, nil
}

// DeleteRetrievalChunksForCorpus empties one corpus's chunk rows (the T8
// deliberate rebuild's wipe, the force path). It is deliberately not the
// walk's tool — the incremental pass prunes per source (except-prune);
// this exists for the one moment the whole recorded embedding space is
// being replaced under an operator's explicit command, when old vectors
// from another model are not staleness but contamination. Returns rows
// deleted.
func (s *Store) DeleteRetrievalChunksForCorpus(corpusID int64) (int, error) {
	res, err := s.w.Exec(`DELETE FROM retrieval_chunk WHERE corpus_id = ?`, corpusID)
	if err != nil {
		return 0, fmt.Errorf("store: clear chunks (corpus %d): %w", corpusID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: count cleared chunks (corpus %d): %w", corpusID, err)
	}
	return int(n), nil
}

// DropRetrievalVecTable removes one corpus's vec0 table at one dimension —
// the companion of the wipe above: stale vectors under a superseded model
// must not linger as search hits while the rebuild embeds. The name is
// assembled through the same builder EnsureRetrievalVecTable uses, so the
// two cannot drift apart.
func (s *Store) DropRetrievalVecTable(root, name string, dim int) error {
	table, err := retrievalVecTableName(root, name, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, table)); err != nil {
		return fmt.Errorf("store: drop vec table for %s/%s at %d dims: %w", root, name, dim, err)
	}
	return nil
}

// RetrievalChunkRow is a stored chunk's identity — the range plus the hash
// T2's staleness plan compares against. Body is deliberately NOT carried:
// staleness compares hashes, and V4's rule (results are locations, never
// text) starts at the seams, not at the CLI.
type RetrievalChunkRow struct {
	LineStart int
	LineEnd   int
	Hash      string
}

// RetrievalChunkRowsForSource reads the stored chunks of one file of one
// corpus — the left side of T2's staleness comparison.
func (s *Store) RetrievalChunkRowsForSource(corpusID int64, source string) ([]RetrievalChunkRow, error) {
	rs, err := s.r.Query(`SELECT line_start, line_end, content_hash FROM retrieval_chunk
		WHERE corpus_id = ? AND source = ? ORDER BY line_start`, corpusID, source)
	if err != nil {
		return nil, fmt.Errorf("store: list chunks for %s (corpus %d): %w", source, corpusID, err)
	}
	defer rs.Close()
	var rows []RetrievalChunkRow
	for rs.Next() {
		var row RetrievalChunkRow
		if err := rs.Scan(&row.LineStart, &row.LineEnd, &row.Hash); err != nil {
			return nil, fmt.Errorf("store: scan chunk of %s (corpus %d): %w", source, corpusID, err)
		}
		rows = append(rows, row)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("store: read chunks of %s (corpus %d): %w", source, corpusID, err)
	}
	return rows, nil
}

// DeleteRetrievalChunksForSourceExcept removes the chunks of one file whose
// ranges are NOT in keep — the deletion half of an incremental reindex
// (V8). The ranges, not a bulk delete, because a rewrite that wiped first
// would report the file as empty to anything querying between the wipe and
// the writes.
func (s *Store) DeleteRetrievalChunksForSourceExcept(corpusID int64, source string, keep [][2]int) (int, error) {
	// One DELETE per excepted range would be a loop of round-trips; the
	// whole set goes in one statement built from placeholders. keep is
	// bounded by the chunk count of a single file — hundreds, not
	// millions — so length is not an injection surface and placeholders
	// bind every value.
	if len(keep) == 0 {
		res, err := s.w.Exec(`DELETE FROM retrieval_chunk WHERE corpus_id = ? AND source = ?`, corpusID, source)
		if err != nil {
			return 0, fmt.Errorf("store: clear chunks for %s (corpus %d): %w", source, corpusID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: count cleared chunks for %s (corpus %d): %w", source, corpusID, err)
		}
		return int(n), nil
	}
	var sb strings.Builder
	args := []any{corpusID, source}
	sb.WriteString(`DELETE FROM retrieval_chunk WHERE corpus_id = ? AND source = ? AND NOT (`)
	for i, r := range keep {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString(`(line_start = ? AND line_end = ?)`)
		args = append(args, r[0], r[1])
	}
	sb.WriteString(`)`)
	res, err := s.w.Exec(sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("store: prune chunks for %s (corpus %d): %w", source, corpusID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: count pruned chunks for %s (corpus %d): %w", source, corpusID, err)
	}
	return int(n), nil
}

// --- embedding doors (T3) ----------------------------------------------------

// EmbeddingChunk is a stored chunk as the embedding pass needs it: identity,
// range, and the Body to send — the one read of chunk bodies in the whole
// system, on the INPUT side (V4's no-text-out rule binds results; the chunk
// goes in).
type EmbeddingChunk struct {
	ID        int64
	Source    string
	LineStart int
	LineEnd   int
	Body      string
}

// RetrievalChunksForEmbedding lists every chunk of a corpus for the
// embedding pass. Ordering is by (source, line_start) so a resumed pass
// embeds in the same order a fresh one would — the batches a provider sees
// are then stable across retries, which is what makes "resume, not restart"
// (O5) more than a slogan: the same chunks, in the same batch positions.
func (s *Store) RetrievalChunksForEmbedding(corpusID int64) ([]EmbeddingChunk, error) {
	rows, err := s.r.Query(`SELECT id, source, line_start, line_end, body FROM retrieval_chunk
		WHERE corpus_id = ? ORDER BY source, line_start`, corpusID)
	if err != nil {
		return nil, fmt.Errorf("store: list chunks for embedding (corpus %d): %w", corpusID, err)
	}
	defer rows.Close()
	var out []EmbeddingChunk
	for rows.Next() {
		var ch EmbeddingChunk
		if err := rows.Scan(&ch.ID, &ch.Source, &ch.LineStart, &ch.LineEnd, &ch.Body); err != nil {
			return nil, fmt.Errorf("store: scan embedding chunk (corpus %d): %w", corpusID, err)
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read embedding chunks (corpus %d): %w", corpusID, err)
	}
	return out, nil
}

// VecChunkIDs reports which chunk ids already have vectors — the resume
// question (O5). A missing vec0 table is NOT an error: it means nothing has
// been embedded yet (the table arrives with the first dimension the corpus
// learns, T1), and the pass embeds everything.
func (s *Store) VecChunkIDs(root, corpus string, dim int) ([]int64, error) {
	table, err := retrievalVecTableName(root, corpus, dim)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.Query(`SELECT chunk_id FROM ` + table)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, fmt.Errorf("store: list embedded chunks (%s): %w", table, err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan embedded chunk id (%s): %w", table, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpsertRetrievalVec stores one chunk's vector. The delete-then-insert is
// the upsert: vec0 has no ON CONFLICT, and a re-embedded chunk (its content
// hash moved) must replace, never double — two vectors for one chunk is a
// result that can retrieve twice for the same code.
//
// The vector crosses as its JSON array text (sqlite-vec's documented form);
// the dimension is checked HERE, against the table's own construction,
// because a wrong-length vector is exactly the corruption the table name
// (which carries the dimension) promises cannot happen.
func (s *Store) UpsertRetrievalVec(root, corpus string, dim int, chunkID int64, vector []float32) error {
	if len(vector) != dim {
		return fmt.Errorf("store: refused a vector of %d dimensions for %s/%s at %d dims (chunk %d) — the table's name carries the dimension precisely so this mismatch is impossible",
			len(vector), root, corpus, dim, chunkID)
	}
	table, err := retrievalVecTableName(root, corpus, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(`DELETE FROM `+table+` WHERE chunk_id = ?`, chunkID); err != nil {
		return fmt.Errorf("store: replacing vector for chunk %d (%s): %w", chunkID, table, err)
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return fmt.Errorf("store: encode vector for chunk %d: %w", chunkID, err)
	}
	if _, err := s.w.Exec(`INSERT INTO `+table+`(chunk_id, embedding) VALUES(?, ?)`, chunkID, string(raw)); err != nil {
		return fmt.Errorf("store: store vector for chunk %d (%s): %w", chunkID, table, err)
	}
	return nil
}

// DeleteRetrievalVec drops one chunk's vector — the delete side T2's
// ApplyChunkPlan recorded as this task's seam (a chunk whose text moved must
// not keep retrieving under its old meaning).
func (s *Store) DeleteRetrievalVec(root, corpus string, dim int, chunkID int64) error {
	table, err := retrievalVecTableName(root, corpus, dim)
	if err != nil {
		return err
	}
	if _, err := s.w.Exec(`DELETE FROM `+table+` WHERE chunk_id = ?`, chunkID); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil // nothing was ever embedded; the delete already happened by absence
		}
		return fmt.Errorf("store: delete vector for chunk %d (%s): %w", chunkID, table, err)
	}
	return nil
}

// VectorSearchRetrieval answers nearest-neighbours for a query vector
// (T4's second index): the vec0 table carries the dimension in its name
// and its own geometry, so a query vector of the wrong length is refused
// HERE — the same rule UpsertRetrievalVec enforces on the way in; a
// distance between different geometries is not a number, it is noise.
// Distance is a PENALTY (smaller is nearer); the domain wants
// bigger-is-better like every other score, so it crosses as its negation —
// the same sign discipline bm25 crosses under, and for the same reason:
// one boundary rule beats N caller memories.
func (s *Store) VectorSearchRetrieval(root, corpus string, dim int, vector []float32, limit int) ([]RetrievalHit, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("store: refused a vector search at %s/%s for %d dims — the dimension is data on the corpus row (V6), and 0 means no embedding pass ever ran", root, corpus, dim)
	}
	if len(vector) != dim {
		return nil, fmt.Errorf("store: refused a vector query of %d dimensions at %s/%s (the index holds %d) — comparing different geometries returns numbers, not neighbours",
			len(vector), root, corpus, dim)
	}
	if limit <= 0 {
		limit = 10
	}
	table, err := retrievalVecTableName(root, corpus, dim)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return nil, fmt.Errorf("store: encode query vector: %w", err)
	}
	rows, err := s.r.Query(`
		SELECT c.source, c.line_start, c.line_end, -v.distance
		FROM (SELECT chunk_id, distance FROM `+table+` WHERE embedding MATCH ? AND k = ?) v
		JOIN retrieval_chunk c ON c.id = v.chunk_id
		JOIN retrieval_corpus k ON k.id = c.corpus_id
		WHERE k.component_root = ? AND k.name = ?
		ORDER BY v.distance
		LIMIT ?`, string(raw), limit*2, root, corpus, limit)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil // nothing embedded — the caller's availability check answers this
		}
		return nil, fmt.Errorf("store: vector search %s/%s: %w", root, corpus, err)
	}
	defer rows.Close()
	var out []RetrievalHit
	for rows.Next() {
		var h RetrievalHit
		if err := rows.Scan(&h.Source, &h.LineStart, &h.LineEnd, &h.Score); err != nil {
			return nil, fmt.Errorf("store: scan vector hit %s/%s: %w", root, corpus, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// RetrievalComponentRoots lists the distinct component roots that HAVE
// corpora — the panel's component list (S54 T2). It reads the corpus rows,
// so the answer is "roots the index has actually seen": a root nobody ever
// indexed does not appear, and the panel never names a component out of a
// graph node table or a loop row and guesses at its index. Sorted, stable,
// empty is the honest zero (a machine with nothing indexed).
func (s *Store) RetrievalComponentRoots() ([]string, error) {
	rows, err := s.r.Query(`SELECT DISTINCT component_root FROM retrieval_corpus ORDER BY component_root`)
	if err != nil {
		return nil, fmt.Errorf("store: retrieval component roots: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return nil, fmt.Errorf("store: scan retrieval component root: %w", err)
		}
		out = append(out, root)
	}
	return out, rows.Err()
}
