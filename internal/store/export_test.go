package store

// White-box tests for the export seam's SQL side (S62 T8). The store is the
// only place identifiers may become SQL, and these tests hold that boundary:
// a foreign identifier is refused before any query, a declared read returns
// exactly the declared columns and nothing else, and a table that exists but
// is missing a declared column answers the unavailability sentinel — the set
// is not built, not an error (BK14).

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestExportRowsRefusesForeignIdentifiers holds the "no caller-derived
// identifiers" rule at the SQL boundary: a table outside the closed set, a
// column the allow-lists never declared (even one planted in the schema), a
// predicate, and an order key shaped like an injection are all refusals that
// name the offender — before any query runs.
func TestExportRowsRefusesForeignIdentifiers(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()

	// A column planted in the schema is still a column the allow-lists never
	// declared: the read of it is refused, and the refusal does not depend on
	// the plant, so the plant only proves the check is against the list, not
	// the schema.
	if _, err := s.w.Exec(`ALTER TABLE agent ADD COLUMN api_key TEXT`); err != nil {
		t.Fatalf("plant the foreign column: %v", err)
	}

	cases := []struct {
		name string
		spec TableSpec
		off  string // the offender the refusal must name
	}{
		{"sqlite_master table", TableSpec{Table: "sqlite_master", Columns: []string{"name"}}, "sqlite_master"},
		{"an existing but undeclared table", TableSpec{Table: "loop", Columns: []string{"id"}}, "loop"},
		{"a table no slice ever built", TableSpec{Table: "no_such_table", Columns: []string{"id"}}, "no_such_table"},
		{"a planted credential-shaped column", TableSpec{Table: "agent", Columns: append(append([]string{}, DeclaredColumns["agent"]...), "api_key")}, "api_key"},
		{"an undeclared agent column", TableSpec{Table: "agent", Columns: []string{"id", "signature"}}, "signature"},
		{"an empty column list", TableSpec{Table: "agent", Columns: nil}, "no columns"},
		{"a duplicated column", TableSpec{Table: "agent", Columns: []string{"id", "id"}}, `column "id" twice`},
		{"a row predicate", TableSpec{Table: "agent", Columns: []string{"id"}, Where: "id = 1"}, "row predicate"},
		{"an order key shaped like an injection", TableSpec{Table: "agent", Columns: []string{"id"}, OrderBy: `id" FROM agent; --`}, "orders by"},
		{"an order key that is no declared column", TableSpec{Table: "agent", Columns: []string{"id"}, OrderBy: "signature DESC"}, "orders by"},
		{"an order key with the wrong suffix shape", TableSpec{Table: "agent", Columns: []string{"id"}, OrderBy: "slugDESC"}, "orders by"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.ExportRows(ctx, tc.spec)
			if err == nil {
				t.Fatalf("ExportRows accepted a foreign spec (%s): %d rows — a refused identifier must not run a query", tc.name, len(rows))
			}
			if rows != nil {
				t.Fatalf("a refused spec returned rows: %v", rows)
			}
			if !containsAll(err.Error(), tc.off) {
				t.Fatalf("the refusal %q does not name the offender %q", err.Error(), tc.off)
			}
		})
	}

	// The happy shape still works after the plant: the allow-list is closed,
	// but a declared read is a declared read.
	if _, err := s.ExportRows(ctx, TableSpec{Table: "agent", Columns: DeclaredColumns["agent"], OrderBy: "slug"}); err != nil {
		t.Fatalf("the declared read of agent is refused after the plant: %v", err)
	}
}

// TestExportRowsReturnsOnlyDeclaredColumns holds the read's shape: exactly
// the declared columns, in spec order, with the database's values — a
// credential-looking string in a declared content field rides verbatim (the
// copy, not a scrubber), and a column the spec did not name is not selected.
func TestExportRowsReturnsOnlyDeclaredColumns(t *testing.T) {
	g, s := testStore(t)
	ctx := context.Background()
	now := "2026-11-02T12:00:00Z"
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if err := g.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO agent (slug, name, prompt, created_at, updated_at) VALUES (?,?,?,?,?)`,
		"zeta", "Zeta", "prompt-z", now, now)
	mustExec(`INSERT INTO agent (slug, name, prompt, created_at, updated_at) VALUES (?,?,?,?,?)`,
		"alpha", "Alpha", "prompt-a with shh-api-key=abc123 planted in content", now, now)

	rows, err := s.ExportRows(ctx, TableSpec{Table: "agent", Columns: DeclaredColumns["agent"], OrderBy: "slug"})
	if err != nil {
		t.Fatalf("ExportRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected the two seeded agents, got %d rows", len(rows))
	}
	// Order by slug: alpha first.
	if rows[0]["slug"] != "alpha" || rows[1]["slug"] != "zeta" {
		t.Fatalf("ORDER BY slug is not honoured: %v, %v", rows[0]["slug"], rows[1]["slug"])
	}
	want := DeclaredColumns["agent"]
	for _, row := range rows {
		if len(row) != len(want) {
			t.Fatalf("the row carries %d keys, the spec declares %d: exactly the declared columns, no more", len(row), len(want))
		}
		for _, c := range want {
			if _, ok := row[c]; !ok {
				t.Fatalf("the row is missing the declared column %s", c)
			}
		}
		if _, ok := row["signature"]; ok {
			t.Fatalf("signature is not declared and must not be selected")
		}
	}
	// The planted credential-looking string in a declared content field rides
	// verbatim: the export is a copy, and content is content.
	if got := rows[0]["prompt"]; got != "prompt-a with shh-api-key=abc123 planted in content" {
		t.Fatalf("the declared content is not verbatim: %v", got)
	}
}

// TestTableExistsOnlyForExportTables holds the probe's closed set: a table
// that exists in the schema but is not one of the twelve is answered false
// without a probe, and an undeclared name that does not exist is false too.
func TestTableExistsOnlyForExportTables(t *testing.T) {
	_, s := testStore(t)

	if !s.TableExists("agent") {
		t.Fatalf("agent is a declared export table in this schema: TableExists must be true")
	}
	for _, name := range []string{
		"loop",              // exists, and is not exportable
		"sqlite_master",     // exists, and is the schema itself
		"policy_acceptance", // exists (S10's parked table), not exportable
		"no_such_table",     // not in the closed set, not in the schema
		"",
	} {
		if s.TableExists(name) {
			t.Fatalf("TableExists(%q) is true — the probe answers for a table the closed set does not declare", name)
		}
	}
}

// TestExportRowsMissingColumnIsUnavailable holds BK14's three states at the
// store: a table that EXISTS but is missing a declared column answers the
// unavailability sentinel (the set is not built), not an error — and the
// white-box probe shows the missing name.
func TestExportRowsMissingColumnIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open the scratch store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// A partial schema: the table is there, most of the declared columns are
	// not. A slice that has not landed looks exactly like this from the
	// read's side.
	if _, err := s.w.Exec(`CREATE TABLE learning (
		id TEXT PRIMARY KEY,
		scope TEXT,
		scope_key TEXT,
		body TEXT
	)`); err != nil {
		t.Fatalf("plant the partial schema: %v", err)
	}

	// The table exists — the probe is true...
	if !s.TableExists("learning") {
		t.Fatalf("TableExists(learning) is false on a table that is in the schema")
	}
	// ...but the read of the full declared set is the unavailability sentinel,
	// naming the first missing column.
	_, err = s.ExportRows(context.Background(), TableSpec{
		Table: "learning", Columns: DeclaredColumns["learning"], OrderBy: "id",
	})
	if !errors.Is(err, ErrColumnsMissing) {
		t.Fatalf("a missing declared column must answer ErrColumnsMissing (unavailable, not an error): got %v", err)
	}

	// White-box: the probe sees exactly the planted columns.
	cols, exists, err := s.exportTableColumns("learning")
	if err != nil {
		t.Fatalf("exportTableColumns: %v", err)
	}
	if !exists {
		t.Fatalf("exportTableColumns says the planted table does not exist")
	}
	if cols["provenance"] {
		t.Fatalf("the probe reported a column the schema does not have")
	}
	if !cols["body"] {
		t.Fatalf("the probe missed a column the schema does have")
	}
	// A table that is not in the schema at all.
	if _, exists, err := s.exportTableColumns("epic"); err != nil || exists {
		t.Fatalf("exportTableColumns(epic) on an empty schema: exists=%v err=%v", exists, err)
	}
}

// TestNormalizeExportCell holds the cell's JSON-safe set: the driver's
// scalars pass through, bytes read as string, and a shape the list does not
// name is an error that says what it found.
func TestNormalizeExportCell(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{int64(42), int64(42)},
		{float64(1.5), float64(1.5)},
		{"text", "text"},
		{true, true},
		{[]byte("bytes"), "bytes"},
	}
	for _, tc := range cases {
		got, err := normalizeExportCell("col", "tab", tc.in)
		if err != nil {
			t.Fatalf("normalizeExportCell(%v): %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("normalizeExportCell(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	// A shape the list does not name is an error, not a guess.
	if _, err := normalizeExportCell("col", "tab", time.Now()); err == nil {
		t.Fatalf("a time.Time cell must be refused, not guessed")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
