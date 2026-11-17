package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// S30 T1 — the code graph's SQL. The MEANING (the six relation kinds, the
// extracted/inferred tag, the four-state query result) lives in
// internal/graph; this file is the handle's side: rows in, rows out. The
// vocabulary closes at the database (the migration's CHECKs), so no Go
// constant is load-bearing here — roadmap.go's deliberate rule, carried:
// one table, one CHECK, one truth.
//
// Both tables carry component_root and every method names it first: D9's
// per-project scope is enforced at the door AND at the database (the CHECK
// refuses an empty root), because a graph row without its root could not be
// deleted, scoped, or told apart from another project's — the exact collapse
// D9 and S22's V12 both refuse. The COLUMN is named component_root, not the
// brief's "project_root": S38 retired that name (org_layers_test's guard
// asserts no column anywhere answers to it), and the column holds exactly
// what S22's retrieval_corpus names its component_root — the workspace
// directory a graph belongs to. The tool/socket contract (T5) keeps the
// spec's argument name, projectRoot; only the stored column follows S38.

// GraphNodeRow mirrors graph_node's columns exactly. The row type carries no
// behaviour on purpose — behaviour here would be a second place that knows
// what a node is; internal/graph owns the meaning.
type GraphNodeRow struct {
	ID            int64
	ComponentRoot string
	FilePath      string
	StartLine     int
	EndLine       int
	Kind          string
	Symbol        string
	Language      string
}

// GraphEdgeRow mirrors graph_edge's columns exactly. Resolution is
// "extracted" or "inferred" (D4) — the CHECK is the authority; this package
// neither re-validates nor defaults it, so a caller writing a third value
// meets the database's own refusal rather than a Go opinion of it.
type GraphEdgeRow struct {
	ID            int64
	ComponentRoot string
	FromNodeID    int64
	ToNodeID      int64
	Kind          string
	Resolution    string
}

// GraphEdgeView is one edge with BOTH endpoints joined — what `explain`
// renders and what the path walk reads. The alternative (edge rows, then a
// node lookup per endpoint at the caller) is an N+1 with the same answer.
type GraphEdgeView struct {
	Kind       string
	Resolution string
	From       GraphNodeRow
	To         GraphNodeRow
}

// GraphTouchRow is one edge that touches a named file, with the OTHER
// endpoint's file and symbol carried along. T4's incremental rebuild deletes
// a file's nodes and every edge touching them — and the incoming edges came
// from files it is NOT rebuilding, so it has to remember what they pointed at
// (file + symbol, not id) before they go, to re-resolve them by name
// afterwards. This is the row that carries that memory.
type GraphTouchRow struct {
	Kind       string
	Resolution string
	FromID     int64
	FromFile   string
	FromSymbol string
	ToFile     string
	ToSymbol   string
	ToID       int64
}

// GraphDeleteCounts is DeleteGraphFile's instrumentation (D6): the rebuild
// scaling proof asserts on these counters — rows touched — rather than a
// wall-clock timer, because the former does not vary by machine.
type GraphDeleteCounts struct {
	NodesDeleted int
	EdgesDeleted int
}

const graphNodeColumns = `id, component_root, file_path, start_line, end_line, kind, symbol, language`

func scanGraphNodeRow(row *sql.Row) (GraphNodeRow, error) {
	var n GraphNodeRow
	err := row.Scan(&n.ID, &n.ComponentRoot, &n.FilePath, &n.StartLine, &n.EndLine, &n.Kind, &n.Symbol, &n.Language)
	return n, err
}

func scanGraphNodeRows(rows *sql.Rows) ([]GraphNodeRow, error) {
	var out []GraphNodeRow
	for rows.Next() {
		var n GraphNodeRow
		if err := rows.Scan(&n.ID, &n.ComponentRoot, &n.FilePath, &n.StartLine, &n.EndLine, &n.Kind, &n.Symbol, &n.Language); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// InsertGraphNode writes one symbol with its location. Every column is
// required — an empty root, file, kind, symbol or language meets the
// database's CHECK, which is the door's refusal rather than a Go one.
func (s *Store) InsertGraphNode(projectRoot, filePath string, startLine, endLine int, kind, symbol, language string) (int64, error) {
	res, err := s.w.Exec(`INSERT INTO graph_node
		(component_root, file_path, start_line, end_line, kind, symbol, language)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		projectRoot, filePath, startLine, endLine, kind, symbol, language)
	if err != nil {
		return 0, fmt.Errorf("insert graph node %s in %s: %w", symbol, projectRoot, err)
	}
	return res.LastInsertId()
}

// InsertGraphEdge writes one typed relation between two nodes that must
// already exist (the foreign keys are live: the DSN carries
// _foreign_keys=1, asserted by open_test's CASCADE proof). Kind closes at
// the six relations and resolution at extracted/inferred, both by CHECK.
func (s *Store) InsertGraphEdge(projectRoot string, fromNodeID, toNodeID int64, kind, resolution string) (int64, error) {
	res, err := s.w.Exec(`INSERT INTO graph_edge
		(component_root, from_node_id, to_node_id, kind, resolution)
		VALUES (?, ?, ?, ?, ?)`,
		projectRoot, fromNodeID, toNodeID, kind, resolution)
	if err != nil {
		return 0, fmt.Errorf("insert graph edge %d→%s %s in %s: %w", fromNodeID, kind, resolution, projectRoot, err)
	}
	return res.LastInsertId()
}

// CountGraphNodes answers D8's first question — is there ANY graph for this
// project root at all — which is what separates not-built from not-found.
// It is deliberately the cheapest possible query (one COUNT, index-free by
// design: it must scan the root's rows, and no index makes "does this root
// exist" cheaper than a scan of what the root holds).
func (s *Store) CountGraphNodes(projectRoot string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM graph_node WHERE component_root = ?`, projectRoot).Scan(&n); err != nil {
		return 0, fmt.Errorf("count graph nodes for %s: %w", projectRoot, err)
	}
	return n, nil
}

// CountGraphEdges counts one root's edges — the status panel's number,
// kept next to CountGraphNodes so the two honesty counts read alike.
func (s *Store) CountGraphEdges(projectRoot string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM graph_edge WHERE component_root = ?`, projectRoot).Scan(&n); err != nil {
		return 0, fmt.Errorf("count graph edges for %s: %w", projectRoot, err)
	}
	return n, nil
}

// GraphNodesByName is the final-component lookup the query layer answers
// bare names with (internal/graph's nameMatches reads the same rule): a
// query for "Validate" hits both "Validate" (as written) and any qualified
// "pkg.Validate". The LIKE wildcard quoting rides events.go's escapeLike;
// the ESCAPE clause names the backslash so a symbol containing % or _
// matches itself rather than acting as a pattern.
func (s *Store) GraphNodesByName(projectRoot, name string) ([]GraphNodeRow, error) {
	rows, err := s.r.Query(`SELECT `+graphNodeColumns+` FROM graph_node
		WHERE component_root = ? AND (symbol = ? OR symbol LIKE ? ESCAPE '\')
		ORDER BY file_path, start_line`, projectRoot, name, `%\.`+escapeLike(name))
	if err != nil {
		return nil, fmt.Errorf("graph nodes by name %s in %s: %w", name, projectRoot, err)
	}
	defer rows.Close()
	return scanGraphNodeRows(rows)
}

// GraphNodesBySymbol is the exact-name lookup D8's `ambiguous` state is
// counted from — a slice of length 2 is two candidates, and the caller
// shows BOTH rather than picking one (CG5's honesty, rendered).
func (s *Store) GraphNodesBySymbol(projectRoot, symbol string) ([]GraphNodeRow, error) {
	rows, err := s.r.Query(`SELECT `+graphNodeColumns+` FROM graph_node
		WHERE component_root = ? AND symbol = ?
		ORDER BY file_path, start_line`, projectRoot, symbol)
	if err != nil {
		return nil, fmt.Errorf("graph nodes by symbol %s in %s: %w", symbol, projectRoot, err)
	}
	defer rows.Close()
	return scanGraphNodeRows(rows)
}

// GraphNodesMatch is `query`'s browse: empty filters drop out of the WHERE
// (empty means unfiltered, never "match the empty string"), prefix is a
// LIKE over the symbol, and the result is bounded by limit so a browse
// cannot cost the whole table.
func (s *Store) GraphNodesMatch(projectRoot, kind, prefix, filePath string, limit int) ([]GraphNodeRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var where []string
	var args []any
	where = append(where, "component_root = ?")
	args = append(args, projectRoot)
	if kind != "" {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	if prefix != "" {
		where = append(where, `symbol LIKE ? ESCAPE '\'`)
		args = append(args, escapeLike(prefix)+"%")
	}
	if filePath != "" {
		where = append(where, "file_path = ?")
		args = append(args, filePath)
	}
	q := `SELECT ` + graphNodeColumns + ` FROM graph_node WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY file_path, start_line LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("graph nodes match in %s: %w", projectRoot, err)
	}
	defer rows.Close()
	return scanGraphNodeRows(rows)
}

// GraphNodeByID fetches one node, scoped to its project root — a node id
// from another root must read as not-found, never as a row.
func (s *Store) GraphNodeByID(projectRoot string, id int64) (GraphNodeRow, error) {
	r, err := scanGraphNodeRow(s.r.QueryRow(`SELECT `+graphNodeColumns+` FROM graph_node
		WHERE component_root = ? AND id = ?`, projectRoot, id))
	if err == sql.ErrNoRows {
		return GraphNodeRow{}, sql.ErrNoRows
	}
	if err != nil {
		return GraphNodeRow{}, fmt.Errorf("graph node %d in %s: %w", id, projectRoot, err)
	}
	return r, nil
}

const graphEdgeViewSelect = `SELECT e.kind, e.resolution,
	f.id, f.component_root, f.file_path, f.start_line, f.end_line, f.kind, f.symbol, f.language,
	t.id, t.component_root, t.file_path, t.start_line, t.end_line, t.kind, t.symbol, t.language`

func scanGraphEdgeViewRows(rows *sql.Rows) ([]GraphEdgeView, error) {
	var out []GraphEdgeView
	for rows.Next() {
		var e GraphEdgeView
		err := rows.Scan(&e.Kind, &e.Resolution,
			&e.From.ID, &e.From.ComponentRoot, &e.From.FilePath, &e.From.StartLine, &e.From.EndLine, &e.From.Kind, &e.From.Symbol, &e.From.Language,
			&e.To.ID, &e.To.ComponentRoot, &e.To.FilePath, &e.To.StartLine, &e.To.EndLine, &e.To.Kind, &e.To.Symbol, &e.To.Language)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GraphEdgesFrom returns the edges leaving a node — "what does this call".
func (s *Store) GraphEdgesFrom(projectRoot string, nodeID int64) ([]GraphEdgeView, error) {
	rows, err := s.r.Query(graphEdgeViewSelect+`
		FROM graph_edge e
		JOIN graph_node f ON f.id = e.from_node_id
		JOIN graph_node t ON t.id = e.to_node_id
		WHERE e.component_root = ? AND e.from_node_id = ?`, projectRoot, nodeID)
	if err != nil {
		return nil, fmt.Errorf("graph edges from %d in %s: %w", nodeID, projectRoot, err)
	}
	defer rows.Close()
	return scanGraphEdgeViewRows(rows)
}

// GraphEdgesTo returns the edges entering a node — "what calls this".
func (s *Store) GraphEdgesTo(projectRoot string, nodeID int64) ([]GraphEdgeView, error) {
	rows, err := s.r.Query(graphEdgeViewSelect+`
		FROM graph_edge e
		JOIN graph_node f ON f.id = e.from_node_id
		JOIN graph_node t ON t.id = e.to_node_id
		WHERE e.component_root = ? AND e.to_node_id = ?`, projectRoot, nodeID)
	if err != nil {
		return nil, fmt.Errorf("graph edges to %d in %s: %w", nodeID, projectRoot, err)
	}
	defer rows.Close()
	return scanGraphEdgeViewRows(rows)
}

// GraphEdgesTouchingFile reports every edge with an endpoint in the file,
// carrying the other endpoint's file and symbol (GraphTouchRow's memory,
// above). Read BEFORE DeleteGraphFile; the two are deliberately separate
// calls so the rebuild's read phase and write phase can be reasoned about
// apart — and the delete never needs a reader mid-transaction.
func (s *Store) GraphEdgesTouchingFile(projectRoot, filePath string) ([]GraphTouchRow, error) {
	rows, err := s.r.Query(`SELECT e.kind, e.resolution,
			f.id, f.file_path, f.symbol,
			t.file_path, t.symbol, t.id
		FROM graph_edge e
		JOIN graph_node f ON f.id = e.from_node_id
		JOIN graph_node t ON t.id = e.to_node_id
		WHERE e.component_root = ? AND (f.file_path = ? OR t.file_path = ?)`,
		projectRoot, filePath, filePath)
	if err != nil {
		return nil, fmt.Errorf("graph edges touching %s in %s: %w", filePath, projectRoot, err)
	}
	defer rows.Close()
	var out []GraphTouchRow
	for rows.Next() {
		var e GraphTouchRow
		if err := rows.Scan(&e.Kind, &e.Resolution, &e.FromID, &e.FromFile, &e.FromSymbol, &e.ToFile, &e.ToSymbol, &e.ToID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteGraphFile deletes one file's nodes and every edge touching them —
// in ONE transaction, so a mid-delete crash cannot leave edges pointing at
// gone nodes. The CASCADE is belt-and-braces (the DSN enables foreign keys;
// the explicit edge delete keeps the counter honest and does not depend on
// the pragma). The returned counts are D6's instrumentation: a rebuild of
// one file must touch that file's rows, not the project's.
func (s *Store) DeleteGraphFile(projectRoot, filePath string) (GraphDeleteCounts, error) {
	return s.deleteGraphFile(projectRoot, filePath, nil)
}

// DeleteGraphFileAudited is DeleteGraphFile with the S54 SP3 audit row
// appended in the SAME transaction — the audit rides the reparse's first
// mutation, so "one row per started operation" means: a reparse that never
// reached a store write (the not-a-claimed-file skip upstream) wrote
// nothing, and one whose audit insert was refused rolled its deletes back.
// The op, the subject and the summary are THIS writer's facts, not caller
// options (the roadmap_audited rule); the caller hands in only WHO.
func (s *Store) DeleteGraphFileAudited(projectRoot, filePath string, actor PlatformActor) (GraphDeleteCounts, error) {
	a := PlatformAudit{
		Op: "graph_reparse", Subject: projectRoot,
		Summary: "graph.reparse started", Actor: actor,
	}
	if err := checkPlatformAudit(a); err != nil {
		return GraphDeleteCounts{}, err
	}
	now := s.Now()
	return s.deleteGraphFile(projectRoot, filePath, func(tx *sql.Tx) error {
		return PlatformChangeInsertTx(tx, a, now)
	})
}

func (s *Store) deleteGraphFile(projectRoot, filePath string, auditTx func(*sql.Tx) error) (GraphDeleteCounts, error) {
	var c GraphDeleteCounts
	tx, err := s.w.Begin()
	if err != nil {
		return c, fmt.Errorf("begin graph file delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids := `SELECT id FROM graph_node WHERE component_root = ? AND file_path = ?`
	res, err := tx.Exec(`DELETE FROM graph_edge
		WHERE component_root = ? AND (from_node_id IN (`+ids+`) OR to_node_id IN (`+ids+`))`,
		projectRoot, projectRoot, filePath, projectRoot, filePath)
	if err != nil {
		return c, fmt.Errorf("delete edges touching %s in %s: %w", filePath, projectRoot, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return c, fmt.Errorf("count deleted edges for %s: %w", filePath, err)
	}
	c.EdgesDeleted = int(n)
	res, err = tx.Exec(`DELETE FROM graph_node WHERE component_root = ? AND file_path = ?`, projectRoot, filePath)
	if err != nil {
		return c, fmt.Errorf("delete nodes of %s in %s: %w", filePath, projectRoot, err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return c, fmt.Errorf("count deleted nodes for %s: %w", filePath, err)
	}
	c.NodesDeleted = int(n)
	if auditTx != nil {
		if err := auditTx(tx); err != nil {
			return GraphDeleteCounts{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return GraphDeleteCounts{}, fmt.Errorf("commit graph file delete: %w", err)
	}
	return c, nil
}

// DeleteGraph drops one project root's whole graph (the bench's delete).
// It returns the node count it removed; an unknown root deletes zero rows
// and is not an error — deleting what is not there is what delete MEANS.
func (s *Store) DeleteGraph(projectRoot string) (int, error) {
	return s.deleteGraph(projectRoot, nil)
}

// DeleteGraphAudited is DeleteGraph with the S54 SP3 audit row in the SAME
// transaction. A zero-row delete (an unknown root) still started and still
// earns its audit row — the rows it removed is what is zero, not the act.
// The op, the subject and the summary are THIS writer's facts (the
// roadmap_audited rule); the caller hands in only WHO.
func (s *Store) DeleteGraphAudited(projectRoot string, actor PlatformActor) (int, error) {
	a := PlatformAudit{
		Op: "graph_delete", Subject: projectRoot,
		Summary: "graph.delete started", Actor: actor,
	}
	if err := checkPlatformAudit(a); err != nil {
		return 0, err
	}
	now := s.Now()
	return s.deleteGraph(projectRoot, func(tx *sql.Tx) error {
		return PlatformChangeInsertTx(tx, a, now)
	})
}

func (s *Store) deleteGraph(projectRoot string, auditTx func(*sql.Tx) error) (int, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin graph delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`DELETE FROM graph_edge WHERE component_root = ?`, projectRoot)
	if err != nil {
		return 0, fmt.Errorf("delete graph edges for %s: %w", projectRoot, err)
	}
	res, err := tx.Exec(`DELETE FROM graph_node WHERE component_root = ?`, projectRoot)
	if err != nil {
		return 0, fmt.Errorf("delete graph nodes for %s: %w", projectRoot, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted graph nodes for %s: %w", projectRoot, err)
	}
	if auditTx != nil {
		if err := auditTx(tx); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit graph delete: %w", err)
	}
	return int(n), nil
}

// GraphRoots lists the project roots holding a graph — the bench's status
// panel and the CLI's `graph list` read it; it is the honest answer to
// "which projects have ever been built", which zero-count rows cannot say.
func (s *Store) GraphRoots() ([]string, error) {
	rows, err := s.r.Query(`SELECT DISTINCT component_root FROM graph_node ORDER BY component_root`)
	if err != nil {
		return nil, fmt.Errorf("graph roots: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GraphFilesIn lists the files a root's graph covers — T4's rebuild reads
// it to see what a root holds, and the bench's status panel renders it.
func (s *Store) GraphFilesIn(projectRoot string) ([]string, error) {
	rows, err := s.r.Query(`SELECT DISTINCT file_path FROM graph_node WHERE component_root = ? ORDER BY file_path`, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("graph files for %s: %w", projectRoot, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
