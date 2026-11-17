package store

// S54 T6 close-out drill (P9) — the claim the slice closes on is that the
// platform plane ADDS an audit table and touches nothing else: a store of
// the pre-S54 vintage migrates forward with its derived rows — retrieval
// corpus, graph nodes, graph edges — byte-identical, the upgraded shape
// equals a fresh database's, and `platform_change` arrives ABSENT→PRESENT
// and EMPTY: the migration mints no audit row for an operation nobody
// started. S51's bug_closeout and S52's memory_closeout precedents kept;
// the operations themselves and the append-only triggers are T1's own
// tests (platform_change_test.go) — this file walks only the close-out
// claim end to end.

import (
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// s54PlatformChangeMigrationIndex is the S54 audit migration's position BY
// NAME (S57 T7's lesson: "the last" breaks the first time a later slice
// appends).
func s54PlatformChangeMigrationIndex(t *testing.T) int {
	t.Helper()
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_platform_change") {
			return i
		}
	}
	t.Fatal("the platform_change migration is missing from Migrations()")
	return 0
}

// derivedFold folds the three derived tables (and the audit table's count)
// into one string in id order. Any row the upgrade lost, reordered or
// rewrote changes it; any audit row the migration invented changes the
// count.
func derivedFold(t *testing.T, g *gorm.DB) string {
	t.Helper()
	var b strings.Builder
	corp, err := g.Raw(`SELECT id, component_root, name, kind, COALESCE(embed_model, ''), COALESCE(embed_dim, -1)
		FROM retrieval_corpus ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("corpus fold: %v", err)
	}
	for corp.Next() {
		var id int64
		var dim int
		var root, name, kind, model string
		if err := corp.Scan(&id, &root, &name, &kind, &model, &dim); err != nil {
			t.Fatalf("corpus fold scan: %v", err)
		}
		b.WriteString("C" + strconv.FormatInt(id, 10) + "~" + root + "~" + name + "~" + kind + "~" + model + "~" + strconv.Itoa(dim) + "|")
	}
	_ = corp.Close()
	nodes, err := g.Raw(`SELECT id, component_root, file_path, start_line, end_line, kind, symbol, language
		FROM graph_node ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("node fold: %v", err)
	}
	for nodes.Next() {
		var id, start, end int64
		var root, file, kind, symbol, language string
		if err := nodes.Scan(&id, &root, &file, &start, &end, &kind, &symbol, &language); err != nil {
			t.Fatalf("node fold scan: %v", err)
		}
		b.WriteString("N" + strconv.FormatInt(id, 10) + "~" + root + "~" + file + "~" + strconv.FormatInt(start, 10) + "~" + strconv.FormatInt(end, 10) +
			"~" + kind + "~" + symbol + "~" + language + "|")
	}
	_ = nodes.Close()
	edges, err := g.Raw(`SELECT id, component_root, from_node_id, to_node_id, kind, resolution
		FROM graph_edge ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("edge fold: %v", err)
	}
	for edges.Next() {
		var id, from, to int64
		var root, kind, resolution string
		if err := edges.Scan(&id, &root, &from, &to, &kind, &resolution); err != nil {
			t.Fatalf("edge fold scan: %v", err)
		}
		b.WriteString("E" + strconv.FormatInt(id, 10) + "~" + root + "~" + strconv.FormatInt(from, 10) + "~" + strconv.FormatInt(to, 10) +
			"~" + kind + "~" + resolution + "|")
	}
	_ = edges.Close()
	// The audit table exists by the time the FRESH and UPGRADED folds run;
	// at the seeded VINTAGE it does not exist yet, so its presence and row
	// count ride the string only when the table is there — an invented
	// audit row cannot hide, and a fold of the vintage cannot crash on a
	// table that is legitimately absent.
	if tablePresent(t, g, "platform_change") {
		var audit int
		if err := g.Raw(`SELECT COUNT(*) FROM platform_change`).Row().Scan(&audit); err != nil {
			t.Fatalf("audit count: %v", err)
		}
		b.WriteString("AUDITS=" + strconv.Itoa(audit))
	}
	if err := corp.Err(); err != nil {
		t.Fatalf("corp rows: %v", err)
	}
	return b.String()
}

// seedPreS54Derived writes pre-slice vintage derived rows RAW — through
// none of the retrieval or graph services, which read their newest shapes
// and would defeat the drill.
func seedPreS54Derived(t *testing.T, s *Store) string {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO retrieval_corpus (component_root, name, kind, embed_model, embed_dim)
		VALUES ('/srv/repos/ledger', 'code', 'retrieved', 'embed-x', 1536)`); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO graph_node (component_root, file_path, start_line, end_line, kind, symbol, language)
		VALUES
		  ('/srv/repos/ledger', 'internal/store/store.go', 10, 40, 'function', 'Open', 'go'),
		  ('/srv/repos/ledger', 'internal/store/store.go', 42, 80, 'function', 'Migrate', 'go'),
		  ('/srv/repos/ledger', 'cmd/aeon/main.go', 5, 25, 'function', 'main', 'go')`); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO graph_edge (component_root, from_node_id, to_node_id, kind, resolution)
		VALUES
		  ('/srv/repos/ledger', 3, 1, 'calls', 'extracted'),
		  ('/srv/repos/ledger', 1, 2, 'calls', 'inferred')`); err != nil {
		t.Fatalf("seed edges: %v", err)
	}
	return derivedFold(t, s.GormDB())
}

func tablePresent(t *testing.T, g *gorm.DB, name string) bool {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Row().Scan(&n); err != nil {
		t.Fatalf("probe %s: %v", name, err)
	}
	return n == 1
}

func TestS54CloseoutFreshStoreHasPlatformChangeEmpty(t *testing.T) {
	fresh := openTemp(t)
	for _, name := range []string{"retrieval_corpus", "graph_node", "graph_edge", "platform_change"} {
		if !tablePresent(t, fresh.GormDB(), name) {
			t.Fatalf("a fresh store lacks %s", name)
		}
	}
	fold := derivedFold(t, fresh.GormDB())
	if fold != "AUDITS=0" {
		t.Fatalf("a fresh store invented rows before any operation ran: %q", fold)
	}
}

func TestS54CloseoutUpgradePreservesDerivedRowsAndMintsNoAudit(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	// Park the store at the vintage BEFORE the audit table exists.
	applyMigrations(t, old.GormDB(), Migrations()[:s54PlatformChangeMigrationIndex(t)])
	if tablePresent(t, old.GormDB(), "platform_change") {
		t.Fatal("platform_change exists before its own migration — the vintage is wrong")
	}
	before := seedPreS54Derived(t, old)
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	// Upgrade: the real runner, the whole way forward.
	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
	// The three derived tables ride out byte-identical, and the audit
	// table arrives EMPTY: absent→present, and only that.
	if got := derivedFold(t, s.GormDB()); got != before+"AUDITS=0" {
		t.Fatalf("the upgrade moved derived rows or invented audits.\n before: %s\n   after: %s", before+"AUDITS=0", got)
	}
	// Idempotence: the second ordinary open moves nothing either.
	if _, err := Open(Options{Dir: dir}); err != nil {
		t.Fatalf("second open: %v", err)
	}
}
