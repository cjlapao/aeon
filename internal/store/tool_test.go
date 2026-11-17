package store

// tool_test.go — S47 T2's proof for the `tool` table: the DDL is what the task
// says it is (both on a fresh apply and on an upgrade from the vintage just
// before it), the CHECKs are the net (each refusal named, each canaried by an
// insert that must succeed), `rerun_class` cannot be defaulted, the seed's
// upsert cannot rewrite a row's identity, the list has one honest order, and
// the two structural claims — **no delete path**, and **the runtime spec path
// never reads this table** — are read out of the source rather than promised.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// s47ToolMigration pins this slice's migration BY NAME, so the upgrade drill's
// premise ("a store at the vintage just before S47's table") stays true when a
// later slice adds a file after it — the lesson S55 and S46 both recorded.
const s47ToolMigration = "20261102021407_tool"

func toolRowForTest(name, family, class string) ToolRow {
	schema := `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`
	return ToolRow{
		Name:         name,
		Family:       family,
		Description:  "A catalog row written by the test, describing one action a granted agent may take.",
		Schema:       schema,
		SchemaHash:   strings.Repeat("a", 64),
		RerunClass:   class,
		Capabilities: `["org_records_read"]`,
		Governed:     `["tool_access"]`,
		Twin:         "",
		Aliases:      `[]`,
		Conditional:  false,
		Notes:        "The one fixed sentence a person reads next to this grant.",
		Version:      1,
		Origin:       "system",
	}
}

func upsert(t *testing.T, s *Store, row ToolRow) {
	t.Helper()
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.ToolUpsertTx(tx, row); err != nil {
		_ = tx.Rollback()
		t.Fatalf("upsert %q: %v", row.Name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestToolMigrationCreatesTableAndIndex is the fresh-apply half and the upgrade
// half of DDL drift.
func TestToolMigrationCreatesTableAndIndex(t *testing.T) {
	s := openTemp(t)
	countTable(t, s.GormDB(), "tool")
	if n := countCatalogueEntries(t, s.GormDB(), "index", "idx_tool_family"); n != 1 {
		t.Fatalf("idx_tool_family: %d entries, want 1", n)
	}
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Fatalf("fresh apply recorded %d migrations, want %d", len(applied), len(Migrations()))
	}

	// The upgrade: a database at the vintage just BEFORE this slice's file,
	// then the file applied on top. Dynamic, so a later slice's migration does
	// not turn this into a false failure.
	idx := -1
	for i, m := range Migrations() {
		if m.Name == s47ToolMigration {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("the migration %s is not in Migrations() — the file was renamed or its timestamp changed", s47ToolMigration)
	}
	g, closeDB := rawGorm(t, filepath.Join(t.TempDir(), "upgrade.db"))
	defer closeDB()
	applyMigrations(t, g, Migrations()[:idx])
	if n := countCatalogueEntries(t, g, "table", "tool"); n != 0 {
		t.Fatalf("the `tool` table exists at the pre-S47 vintage (%d entries) — the migration is not the only DDL source", n)
	}
	applyMigrations(t, g, Migrations()[idx:idx+1])
	if n := countCatalogueEntries(t, g, "table", "tool"); n != 1 {
		t.Fatalf("after applying %s the table has %d entries, want 1", s47ToolMigration, n)
	}
	if n := countCatalogueEntries(t, g, "index", "idx_tool_family"); n != 1 {
		t.Fatalf("the upgrade did not create idx_tool_family (%d)", n)
	}
}

// TestToolChecksRefuseBadRows walks every CHECK in the migration by name. Each
// case is a raw INSERT, so what is under test is the schema and not Go's
// kindness — and each refusal is paired with the shape that must pass, so a
// CHECK that was dropped by an edit cannot pass this test unnoticed.
func TestToolChecksRefuseBadRows(t *testing.T) {
	base := toolRowForTest("goal_show", "goal", "safe")
	cases := []struct {
		name    string
		column  string
		value   any
		wantSub string
	}{
		{"an empty name", "name", "", "needs a name"},
		{"a name with a dot", "name", "goal.show", "CHECK"},
		{"a name with a capital", "name", "Goal_show", "CHECK"},
		{"a name with a hyphen", "name", "goal-show", "CHECK"},
		{"a 65-character name", "name", strings.Repeat("g", 65), "CHECK"},
		{"an empty description", "description", "", "CHECK"},
		{"a 4097-character description", "description", strings.Repeat("d", 4097), "CHECK"},
		{"a schema that is not JSON", "schema", `{"type":object`, "CHECK"},
		{"a schema that is not an array or object", "schema", `"a string"`, ""}, // json_valid accepts a scalar: the copy must be *some* JSON
		{"a schema_hash of the wrong length", "schema_hash", strings.Repeat("a", 63), "CHECK"},
		{"a rerun class outside the vocabulary", "rerun_class", "maybe", "CHECK"},
		{"capabilities that is not JSON", "capabilities", `org_records_read`, "CHECK"},
		// The one shape the task's DDL sketch refused and this migration admits:
		// an EMPTY capability array, which `todo` and `report_size` honestly have
		// (T0's deviation 2). The shape CHECKs still fire; the emptiness rule is
		// internal/tools' named allow-list test, not a count in the schema.
		{"capabilities as an empty array (legal for the two tools that own it)", "capabilities", `[]`, ""},
		{"governed that is not JSON", "governed", `tool_access`, "CHECK"},
		{"governed as a JSON string rather than an array", "governed", `"tool_access"`, "CHECK"},
		{"aliases that is not JSON", "aliases", `write_file`, "CHECK"},
		{"aliases as a JSON object rather than an array", "aliases", `{"a":"write_file"}`, "CHECK"},
		{"a 201-character notes sentence", "notes", strings.Repeat("n", 201), "CHECK"},
		{"a version below 1", "version", 0, "CHECK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			row := base
			setField(&row, tc.column, tc.value)
			tx, err := s.w.Begin()
			if err != nil {
				t.Fatal(err)
			}
			err = s.ToolUpsertTx(tx, row)
			_ = tx.Rollback()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("this shape is legal and must insert: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("the %s CHECK accepted it — the net is missing", tc.column)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("the refusal does not name what it refused (%v)", err)
			}
			if tc.wantSub == "CHECK" && !strings.Contains(err.Error(), "tool") {
				t.Errorf("the refusal does not name the table (%v)", err)
			}
		})
	}
}

// setField is a tiny setter for the CHECK cases, so each one reads as the column
// it tests rather than as forty near-identical literals.
func setField(row *ToolRow, column string, value any) {
	switch column {
	case "name":
		row.Name = value.(string)
	case "family":
		row.Family = value.(string)
	case "description":
		row.Description = value.(string)
	case "schema":
		row.Schema = value.(string)
	case "schema_hash":
		row.SchemaHash = value.(string)
	case "rerun_class":
		row.RerunClass = value.(string)
	case "capabilities":
		row.Capabilities = value.(string)
	case "governed":
		row.Governed = value.(string)
	case "aliases":
		row.Aliases = value.(string)
	case "conditional":
		row.Conditional = false // exercised raw below; the Go field is a bool
	case "notes":
		row.Notes = value.(string)
	case "version":
		row.Version = value.(int)
	case "origin":
		row.Origin = value.(string)
	}
}

// TestToolChecksRefuseRawShapelessRows covers the two CHECKs the typed path
// cannot reach: a missing `rerun_class` and a family outside its length band.
func TestToolChecksRefuseRawShapelessRows(t *testing.T) {
	s := openTemp(t)
	const hex64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	insert := `INSERT INTO tool (name, family, description, schema, schema_hash, capabilities, version, created_at, updated_at)
		VALUES ('goal_show','goal','d','{}','` + hex64 + `','["org_records_read"]',1,'now','now')`
	if _, err := s.w.Exec(insert); err == nil {
		t.Error("a row with NO rerun_class was accepted — the existing rule is that a new tool cannot default safe")
	}
	if _, err := s.w.Exec(`INSERT INTO tool (name, family, description, schema, schema_hash, rerun_class, capabilities, version, created_at, updated_at)
		VALUES ('goal_show', ?, 'd', '{}', ?, 'safe', '["org_records_read"]', 1, 'now', 'now')`, strings.Repeat("f", 33), strings.Repeat("a", 64)); err == nil {
		t.Error("a 33-character family was accepted — its CHECK is missing")
	}
	// The two CHECKs the typed write path cannot produce, reached raw: a
	// `conditional` outside 0/1, and an `origin` this slice does not admit.
	if _, err := s.w.Exec(`INSERT INTO tool (name, family, description, schema, schema_hash, rerun_class, capabilities, conditional, version, created_at, updated_at)
		VALUES ('goal_list', 'goal', 'd', '{}', ?, 'safe', '["org_records_read"]', 2, 1, 'now', 'now')`, strings.Repeat("a", 64)); err == nil {
		t.Error("conditional=2 was accepted — its CHECK is missing")
	}
	if _, err := s.w.Exec(`INSERT INTO tool (name, family, description, schema, schema_hash, rerun_class, capabilities, origin, version, created_at, updated_at)
		VALUES ('goal_update', 'goal', 'd', '{}', ?, 'safe', '["org_records_read"]', 'user', 1, 'now', 'now')`, strings.Repeat("a", 64)); err == nil {
		t.Error("origin='user' was accepted — the CHECK is missing, and a user-authored tool cannot exist (TC2)")
	}
	// The same row with every CHECK satisfied DOES go in, so the refusals
	// above are the CHECKs and not a typo in the statement.
	if _, err := s.w.Exec(`INSERT INTO tool (name, family, description, schema, schema_hash, rerun_class, capabilities, version, created_at, updated_at)
		VALUES ('goal_show', 'goal', 'd', '{}', ?, 'safe', '["org_records_read"]', 1, 'now', 'now')`, strings.Repeat("a", 64)); err != nil {
		t.Errorf("the canary insert failed, so the refusals above prove nothing: %v", err)
	}
}

// TestToolRerunClassHasNoDefault is the existing rule restated for this table:
// an insert that omits `rerun_class` fails, because a tool that silently
// classified itself `safe` is a tool crash recovery would replay.
func TestToolRerunClassHasNoDefault(t *testing.T) {
	s := openTemp(t)
	_, err := s.w.Exec(`INSERT INTO tool (name, family, description, schema, schema_hash, capabilities, created_at, updated_at)
		VALUES ('goal_show', 'goal', 'd', '{}', ?, '["org_records_read"]', 'now', 'now')`, strings.Repeat("a", 64))
	if err == nil {
		t.Fatal("an insert omitting rerun_class succeeded — it has a default")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Errorf("the refusal is not the NOT NULL constraint: %v", err)
	}
}

// TestToolUpsertKeepsCreatedAt pins what the seed's update must not touch: the
// content changes, `updated_at` moves, `version` travels with it — and
// `created_at`, the row's identity moment, does not.
func TestToolUpsertKeepsCreatedAt(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	row := toolRowForTest("goal_create", "goal", "detectable")
	upsert(t, s, row)
	first, err := s.ToolGet(ctx, "goal_create")
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt == "" || first.UpdatedAt == "" {
		t.Fatalf("created_at/updated_at are empty: %+v", first)
	}

	row.Description = "A different description for the same action, which is a content change and nothing more."
	row.Version = 2
	upsert(t, s, row)
	second, err := s.ToolGet(ctx, "goal_create")
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("created_at moved from %q to %q — the seed rewrote the row's identity", first.CreatedAt, second.CreatedAt)
	}
	if second.Description != row.Description || second.Version != 2 {
		t.Errorf("the content did not travel: %+v", second)
	}
	if second.UpdatedAt == "" {
		t.Error("updated_at is empty after an update")
	}
	n, err := s.ToolCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("the upsert inserted a second row (count %d, want 1)", n)
	}
	// `origin` is not updatable by the write path at all.
	if _, err := s.w.Exec(`UPDATE tool SET origin = 'user' WHERE name = 'goal_create'`); err == nil {
		t.Error("origin is writable — the CHECK is missing, and a row this slice ships is a system row")
	}
}

// TestToolListOrderedByFamilyThenName pins the one order a catalog list has.
func TestToolListOrderedByFamilyThenName(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for _, row := range []ToolRow{
		toolRowForTest("zebra_tool", "workspace", "safe"),
		toolRowForTest("alpha_tool", "workspace", "safe"),
		toolRowForTest("goal_create", "goal", "detectable"),
		toolRowForTest("bash", "process", "unsafe"),
	} {
		upsert(t, s, row)
	}
	rows, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Family+"/"+r.Name)
	}
	want := []string{"goal/goal_create", "process/bash", "workspace/alpha_tool", "workspace/zebra_tool"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ToolList order is %v, want %v", got, want)
	}
	// The JSON columns round-trip through the read as valid JSON, and a missing
	// row is ErrNoRows — the catalog's `missing_row`, never a zero row.
	if !json.Valid([]byte(rows[0].Capabilities)) {
		t.Errorf("capabilities did not round-trip as JSON: %q", rows[0].Capabilities)
	}
	if _, err := s.ToolGet(ctx, "no_such_tool"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("an absent tool reads as %v, want sql.ErrNoRows", err)
	}
}

// TestToolHasNoDeletePath reads the package's sources: no statement deletes from
// `tool`, and no exported accessor is named like a delete. Retirement is a
// ledger flag (S43-extra SD6), so a delete path here would be a way to edit the
// history of what the app exposed.
func TestToolHasNoDeletePath(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lower := strings.ToLower(string(src))
		for _, shape := range []string{"delete from tool", "delete  from tool"} {
			if strings.Contains(lower, shape) {
				t.Errorf("%s deletes from the tool table — there is no delete path for a catalog row", name)
			}
		}
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name == nil || !fd.Name.IsExported() {
				continue
			}
			n := fd.Name.Name
			if strings.Contains(n, "Tool") && (strings.Contains(n, "Delete") || strings.Contains(n, "Remove") || strings.Contains(n, "Drop")) {
				t.Errorf("%s exports %s — a catalog row is retired by the ledger, never deleted", name, n)
			}
		}
	}
}

// TestToolTableIsNotReadByTheRuntimeSpecPath is TC2's structural half: the row is
// a catalog entry, and the path that builds a step's toolset and shows the model
// its specs must not read it. If someone wires the catalog into execution, this
// fails on a grep rather than in production with a model offered a tool whose
// executor says otherwise.
func TestToolTableIsNotReadByTheRuntimeSpecPath(t *testing.T) {
	targets := []string{
		"../tools/toolset.go",
		"../tools/records_tools.go",
		"../runtime/loop.go",
		"../daemon/step.go",
	}
	for _, path := range targets {
		rel := path
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, needle := range []string{"store.Tool", "ToolRow", "ToolList(", "ToolGet(", "ToolUpsertTx("} {
			if strings.Contains(string(src), needle) {
				t.Errorf("%s references %q — the runtime spec path must never read the catalog row (TC2)", rel, needle)
			}
		}
	}
	// And the store itself must not reach for the tools package: the row is data,
	// and a store that knew what a tool *is* could rebuild one from a row. Read
	// the imports, not the prose — the comments legitimately name the package.
	f, err := parser.ParseFile(token.NewFileSet(), "tool.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.HasSuffix(path, "/internal/tools") || strings.HasSuffix(path, "/internal/seed") {
			t.Errorf("tool.go imports %s — the store does not know what a tool is, and does not hash either", path)
		}
	}
}

// TestToolUpsertNeedsTheCallersTx keeps the write path single: the catalog row
// commits with its ledger row or not at all.
func TestToolUpsertNeedsTheCallersTx(t *testing.T) {
	s := openTemp(t)
	if err := s.ToolUpsertTx(nil, toolRowForTest("goal_show", "goal", "safe")); err == nil {
		t.Error("a nil transaction was accepted — the row could commit outside the seed's tx")
	}
	if err := s.ToolUpsertTx(mustBegin(t, s), ToolRow{}); err == nil {
		t.Error("a row with no name was accepted")
	}
}

func mustBegin(t *testing.T, s *Store) *sql.Tx {
	t.Helper()
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

var _ = fmt.Sprintf
