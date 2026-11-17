package store

import (
	"database/sql"
	"strings"
	"testing"
)

// S30 T1 — the gate tests named in tasks/T1.md: the tables are plain
// relational (no vector column anywhere in the migration's SQL), every row
// is scoped by component_root at write time, and an edge's resolution is
// extracted or inferred, nothing else. T1's discipline (brief §6 N1): the
// temptation is to copy S22's or S28's migration shape out of habit — a
// column added "in case" is scope this slice declined, so the absence is
// asserted, not assumed.

func graphMigrationSQL(t *testing.T) (name, sql string) {
	t.Helper()
	for _, m := range Migrations() {
		if strings.Contains(m.Name, "graph_tables") {
			return m.Name, m.SQL
		}
	}
	t.Fatal("no graph_tables migration exists — T1 has not landed")
	return "", ""
}

func TestGraphTablesHaveNoVectorColumn(t *testing.T) {
	name, sqlText := graphMigrationSQL(t)
	for _, banned := range []string{"vec0", "sqlite-vec", "sqlite_vec", "embedding", "FLOAT["} {
		if strings.Contains(strings.ToLower(sqlText), strings.ToLower(banned)) {
			t.Errorf("%s mentions %q — this slice's question is structural, not similarity "+
				"(D1, CG1, N1); plain relational rows only", name, banned)
		}
	}
	for _, want := range []string{"graph_node", "graph_edge"} {
		if !strings.Contains(sqlText, want) {
			t.Errorf("%s does not create %s", name, want)
		}
	}
}

func TestGraphRowsScopedByComponentRoot(t *testing.T) {
	s := openTemp(t)
	const file = "a.go"

	// An empty root is refused at WRITE time, by the database's own CHECK —
	// a graph row without its root could not be scoped or deleted (D9).
	if _, err := s.InsertGraphNode("", file, 1, 2, "function", "Foo", "go"); err == nil {
		t.Error("a graph_node with no component_root was accepted")
	}
	id, err := s.InsertGraphNode("/root", file, 1, 2, "function", "Foo", "go")
	if err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := s.InsertGraphEdge("", id, id, "calls", "extracted"); err == nil {
		t.Error("a graph_edge with no component_root was accepted")
	}
	// A row from one root must not answer for another: the id-scoped read
	// names its root, so a foreign root's node reads as not-found.
	if _, err := s.GraphNodeByID("/other", id); err != sql.ErrNoRows {
		t.Errorf("a node from /root answered a query scoped to /other: %v", err)
	}
}

func TestEdgeResolutionIsExtractedOrInferred(t *testing.T) {
	s := openTemp(t)
	a, err := s.InsertGraphNode("/r", "a.go", 1, 5, "function", "A", "go")
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	b, err := s.InsertGraphNode("/r", "b.go", 1, 5, "function", "B", "go")
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	if _, err := s.InsertGraphEdge("/r", a, b, "calls", "guessed"); err == nil {
		t.Error("resolution 'guessed' was accepted — the third value D4 refuses is back")
	}
	if _, err := s.InsertGraphEdge("/r", a, b, "calls", "extracted"); err != nil {
		t.Errorf("resolution 'extracted' refused: %v", err)
	}
	if _, err := s.InsertGraphEdge("/r", b, a, "calls", "inferred"); err != nil {
		t.Errorf("resolution 'inferred' refused: %v", err)
	}
	// The six relation kinds close the same way (CG4's set, adopted as-is).
	if _, err := s.InsertGraphEdge("/r", a, b, "loves", "extracted"); err == nil {
		t.Error("edge kind 'loves' was accepted — the six are closed at the database")
	}
	// And edges name nodes that must exist (foreign keys are live: the DSN
	// carries _foreign_keys=1 — open_test's CASCADE is that pragma's proof).
	if _, err := s.InsertGraphEdge("/r", a, 9999, "calls", "extracted"); err == nil {
		t.Error("an edge to a node that never existed was accepted")
	}
}

func TestGraphIndexesAreUsedNotMerelyDeclared(t *testing.T) {
	// T1's validation: "(component_root, symbol) and (component_root, file_path)
	// indexes exist and are exercised by a query plan check, not merely
	// declared" — the plan is the check; T4's scaling proof dies without
	// the file index actually being USED.
	s := openTemp(t)
	if _, err := s.InsertGraphNode("/r", "a.go", 1, 5, "function", "A", "go"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for _, tc := range []struct {
		name, q, want string
	}{
		{"symbol", `SELECT id FROM graph_node WHERE component_root = '/r' AND symbol = 'A'`, "graph_node_symbol"},
		{"file", `SELECT id FROM graph_node WHERE component_root = '/r' AND file_path = 'a.go'`, "graph_node_file"},
	} {
		var (
			planID  int
			parent  int
			notUsed string
			detail  string
		)
		_ = notUsed
		rows, err := s.r.Query(`EXPLAIN QUERY PLAN ` + tc.q)
		if err != nil {
			t.Fatalf("%s: explain: %v", tc.name, err)
		}
		var plan []string
		for rows.Next() {
			if err := rows.Scan(&planID, &parent, &notUsed, &detail); err != nil {
				rows.Close()
				t.Fatalf("%s: scan: %v", tc.name, err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "\n")
		if !strings.Contains(joined, "USING INDEX "+tc.want) && !strings.Contains(joined, "USING COVERING INDEX "+tc.want) {
			t.Errorf("the %s query plan does not use %s — got:\n%s", tc.name, tc.want, joined)
		}
	}
}

func TestDeleteGraphFileRemovesEdgesBothDirections(t *testing.T) {
	// T4 leans on this: a rebuild deletes the file's nodes and EVERY edge
	// touching them — outgoing AND incoming — then re-inserts. The counts
	// returned are D6's instrumentation, asserted here on a hand-built
	// fixture where the expected numbers are certain.
	s := openTemp(t)
	a, err := s.InsertGraphNode("/r", "a.go", 1, 5, "function", "A", "go")
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	b, err := s.InsertGraphNode("/r", "b.go", 1, 5, "function", "B", "go")
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	for _, e := range []struct {
		from, to int64
	}{
		{a, b}, {a, b}, // two edges out of A's nodes
		{b, a}, // one edge IN to A's node, from the file being kept
	} {
		if _, err := s.InsertGraphEdge("/r", e.from, e.to, "calls", "extracted"); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}
	touch, err := s.GraphEdgesTouchingFile("/r", "a.go")
	if err != nil {
		t.Fatalf("touching: %v", err)
	}
	if len(touch) != 3 {
		t.Fatalf("touching rows = %d, want 3", len(touch))
	}
	// The touch rows carry the OTHER endpoint's identity — the memory the
	// rebuild re-resolves incoming edges from (T4's re-link).
	var sawIncoming bool
	for _, e := range touch {
		if e.FromFile == "b.go" && e.ToFile == "a.go" && e.FromSymbol == "B" && e.ToSymbol == "A" {
			sawIncoming = true
		}
	}
	if !sawIncoming {
		t.Error("no touch row carried the incoming edge's other endpoint — T4 cannot re-link")
	}

	c, err := s.DeleteGraphFile("/r", "a.go")
	if err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if c.NodesDeleted != 1 || c.EdgesDeleted != 3 {
		t.Errorf("delete counts = %+v, want 1 node / 3 edges", c)
	}
	// B's node survives; its dangling edge does not.
	if _, err := s.GraphNodeByID("/r", b); err != nil {
		t.Errorf("the kept file's node vanished: %v", err)
	}
	rest, err := s.GraphEdgesFrom("/r", b)
	if err != nil {
		t.Fatalf("edges from: %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("%d edges still leave B — dangling edges survived the delete", len(rest))
	}
}

func TestGraphNodesMatchFiltersAndBounds(t *testing.T) {
	s := openTemp(t)
	for i, n := range []struct{ file, sym, kind string }{
		{"a.go", "Process", "function"},
		{"a.go", "Processor", "type"},
		{"b.go", "Validate", "function"},
	} {
		if _, err := s.InsertGraphNode("/r", n.file, i+1, i+2, n.kind, n.sym, "go"); err != nil {
			t.Fatalf("insert %s: %v", n.sym, err)
		}
	}
	all, err := s.GraphNodesMatch("/r", "", "", "", 10)
	if err != nil {
		t.Fatalf("match all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("unfiltered match = %d rows, want 3", len(all))
	}
	byKind, err := s.GraphNodesMatch("/r", "function", "", "", 10)
	if err != nil || len(byKind) != 2 {
		t.Errorf("kind=function → %d rows (%v), want 2", len(byKind), err)
	}
	byPrefix, err := s.GraphNodesMatch("/r", "", "Proces", "", 10)
	if err != nil || len(byPrefix) != 2 {
		t.Errorf("prefix=Proces → %d rows (%v), want 2", len(byPrefix), err)
	}
	byFile, err := s.GraphNodesMatch("/r", "", "", "b.go", 10)
	if err != nil || len(byFile) != 1 || byFile[0].Symbol != "Validate" {
		t.Errorf("file=b.go → %+v (%v), want only Validate", byFile, err)
	}
	// The LIKE wildcards are quoted: a symbol containing % matches itself,
	// not everything.
	if _, err := s.InsertGraphNode("/r", "c.go", 1, 2, "function", "100%", "go"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	pct, err := s.GraphNodesMatch("/r", "", "100%", "", 10)
	if err != nil || len(pct) != 1 {
		t.Errorf("prefix='100%%' matched %d rows (%v), want exactly the one literal", len(pct), err)
	}
	// The bound is real.
	bounded, err := s.GraphNodesMatch("/r", "", "", "", 2)
	if err != nil || len(bounded) != 2 {
		t.Errorf("limit=2 → %d rows (%v), want 2", len(bounded), err)
	}
}

func TestGraphRootsAndFilesArePerProject(t *testing.T) {
	s := openTemp(t)
	if _, err := s.InsertGraphNode("/r1", "a.go", 1, 2, "function", "A", "go"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.InsertGraphNode("/r2", "b.go", 1, 2, "function", "B", "go"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.InsertGraphNode("/r2", "c.go", 1, 2, "function", "C", "go"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	roots, err := s.GraphRoots()
	if err != nil || len(roots) != 2 || roots[0] != "/r1" || roots[1] != "/r2" {
		t.Errorf("roots = %v (%v), want [/r1 /r2]", roots, err)
	}
	files, err := s.GraphFilesIn("/r2")
	if err != nil || len(files) != 2 || files[0] != "b.go" {
		t.Errorf("files(/r2) = %v (%v), want [b.go c.go]", files, err)
	}
	n, err := s.DeleteGraph("/r2")
	if err != nil || n != 2 {
		t.Errorf("DeleteGraph(/r2) = %d (%v), want 2 nodes gone", n, err)
	}
	// Deleting what is not there is what delete MEANS — zero rows, no error.
	if n, err := s.DeleteGraph("/nope"); err != nil || n != 0 {
		t.Errorf("DeleteGraph(/nope) = %d (%v), want 0 and no error", n, err)
	}
	if roots, err := s.GraphRoots(); err != nil || len(roots) != 1 {
		t.Errorf("roots after delete = %v (%v), want one root", roots, err)
	}
}
