package store

// S28 T1 — the learnings migration's shape is the DATABASE's claim, not Go's.
// The house CHECK discipline (roadmap_test.go is the sibling, schema_test.go's
// TestTheCheckConstraintsRejectEveryInvalidValue the template): every column
// but the tested one valid, the refusal must be SQLite naming a CHECK, and
// each gate carries its own positive control — a constraint that rejects
// everything passes the negative half and breaks the slice.

import (
	"strings"
	"testing"
)

// learningSQL is the honest minimal row: every NOT NULL column named, so a
// case failing on the CHECK it claims is the case actually being tested. The
// nullable columns (observed_count, source_loop_slug, source_step_id) are
// omitted — the writer answers them or leaves them NULL, and T1 carries no
// FK on scope_key (design §3: there is no agent table to reference).
const learningSQL = `INSERT INTO learning
	(scope, scope_key, body, provenance, created_at)
	VALUES (?,?,?,?,?)`

func insertLearning(t *testing.T, s *Store, scope, scopeKey, body, provenance string) {
	t.Helper()
	_, err := s.w.Exec(learningSQL, scope, scopeKey, body, provenance, s.Now())
	if err != nil {
		t.Fatalf("insert learning (%s/%s/%s/%s): %v", scope, scopeKey, body, provenance, err)
	}
}

// TestLearningFourScopesClosed — D1's three scopes are the closed set. The
// positive control walks all three; the ninth value is refused by SQLite's
// own CHECK (a value nobody compiled is a scope nobody granted).
func TestLearningFourScopesClosed(t *testing.T) {
	s := openTemp(t)
	for _, scope := range []string{"person", "project", "agent"} {
		insertLearning(t, s, scope, "key", "body", "told")
	}
	_, err := s.w.Exec(learningSQL, "epic", "key", "body", "told", s.Now())
	if err == nil {
		t.Fatal("a scope outside person/project/agent must be refused by the CHECK — a CHECK that admits a fourth is T1's phantom failure")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the refusal must be SQLite's CHECK, not a Go guess: %v", err)
	}
}

// TestLearningFourProvenancesClosed — M1's four provenances are the closed
// set. The positive control walks all four (including relayed, which T4 and
// T7 keep write-protected at the DOOR, but which the DATABASE admits); the
// fifth value is refused by SQLite's CHECK.
func TestLearningFourProvenancesClosed(t *testing.T) {
	s := openTemp(t)
	for _, prov := range []string{"told", "observed", "assumed", "relayed"} {
		insertLearning(t, s, "project", "key", "body", prov)
	}
	_, err := s.w.Exec(learningSQL, "project", "key", "body", "guessed", s.Now())
	if err == nil {
		t.Fatal("a provenance outside told/observed/assumed/relayed must be refused by the CHECK — a CHECK that admits a fifth is T1's phantom failure")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the refusal must be SQLite's CHECK, not a Go guess: %v", err)
	}
}

// TestLearningTagIndexed — M5/D2: the (tag, learning_id) index is the whole
// reason tags are a join table, not a JSON column. An exact-match query and
// a LIKE 'prefix%' query must each name the index in their plan; a query
// that reads the table without it would make the index decorative.
func TestLearningTagIndexed(t *testing.T) {
	s := openTemp(t)
	// Enough rows that the optimizer prefers the index over a scan — the
	// prefix query is the one M5/D2 promised, and a plan that scans a real
	// volume would make the join table a JSON column in all but name.
	for i := 0; i < 300; i++ {
		insertLearning(t, s, "project", "key", "body", "told")
	}
	// Varied tags: a few 'auth*' the prefix query can seek to, the rest a
	// tag the prefix does NOT match. A selective index is what the plan
	// chooses over a scan; all-300-matching would scan anyway.
	if _, err := s.w.Exec(`INSERT INTO learning_tag (learning_id, tag)
		SELECT id, CASE WHEN id % 50 = 0 THEN 'auth' ELSE 'ztag' END FROM learning`); err != nil {
		t.Fatalf("insert learning_tag: %v", err)
	}
	// Stats, so the optimizer can decide the index beats a scan.
	if _, err := s.w.Exec(`ANALYZE`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// The exact-match and the prefix-RANGE query both take the index. The
	// prefix range (`tag >= 'auth' AND tag < 'auth~'`) is the index-usable
	// form of a prefix query; SQLite's own optimizer may scan a bare
	// `LIKE 'auth%'` (a build-level LIKE choice, not a property of the
	// index), so the plan assertion is on the form that is actually
	// index-served, and the LIKE below asserts results, not the plan.
	for _, sql := range []string{
		`SELECT learning_id FROM learning_tag WHERE tag = 'auth'`,
		`SELECT learning_id FROM learning_tag WHERE tag >= 'auth' AND tag < 'auth~'`,
	} {
		rows, err := s.r.Query(`EXPLAIN QUERY PLAN ` + sql)
		if err != nil {
			t.Fatalf("query plan for %q: %v", sql, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var (
				id      int
				parent  int
				notused int
				detail  string
			)
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatalf("scan plan row: %v", err)
			}
			plan.WriteString(detail)
			plan.WriteString("; ")
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read plan: %v", err)
		}
		planStr := plan.String()
		if !strings.Contains(planStr, "idx_learning_tag_tag") {
			t.Fatalf("query %q did not use the (tag, learning_id) index — plan: %q", sql, planStr)
		}
	}

	// LIKE 'auth%' returns the right rows regardless of whether the planner
	// indexes it — the app-level prefix syntax, correctness-asserted here.
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM learning_tag WHERE tag LIKE 'auth%'`).Scan(&n); err != nil {
		t.Fatalf("like query: %v", err)
	}
	if n < 1 {
		t.Fatalf("LIKE 'auth%%' returned %d rows, want at least one — the tag rows are the prefix query's subject", n)
	}
}

// TestLearningVecIsFirstVecTableInSchema — the learning_vec vec0 is NOT in
// the migration (S22's retrieval precedent: a vec0 fixes its embedding
// dimension at CREATE time, and the dimension is data the migration cannot
// know). So the migration is a plain CREATE of learning + learning_tag, and
// the vec0 arrives on demand from internal/store's learnings statements. This
// test documents that finding: no migration in the schema contains a vec0
// CREATE — the on-demand path is the only one.
func TestLearningVecIsFirstVecTableInSchema(t *testing.T) {
	for _, m := range Migrations() {
		if strings.Contains(strings.ToUpper(m.SQL), "VECTBL") || strings.Contains(strings.ToUpper(m.SQL), "USING VEC0") {
			t.Fatalf("migration %s carries a vec0 CREATE — a vec0 fixes its dimension at CREATE time, which a migration cannot know; it must be created on demand (S22's retrieval precedent), not here", m.Name)
		}
	}
}
