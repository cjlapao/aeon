package store

import (
	"testing"
	"testing/fstest"
)

// S41-worktree-friendly-schema: new migrations past the frozen legacy block
// (1–43) live as individual timestamp-named .sql files instead of new
// entries in schema.go's own literal slice, so two worktrees adding a
// migration at the same time add two different FILES — never two edits to
// the same line of the same file.

func TestParseTimestampedMigrationsReadsEachSQLFileByFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/20261030120000_add_widget.sql": &fstest.MapFile{
			Data: []byte("-- widget table, standalone\nCREATE TABLE widget (id INTEGER PRIMARY KEY);\n"),
		},
	}
	got, err := parseTimestampedMigrations(fsys, "migrations")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d migrations, want 1", len(got))
	}
	m := got[0]
	if m.ID != 20261030120000 {
		t.Fatalf("ID = %d, want the filename's timestamp", m.ID)
	}
	if m.Name != "20261030120000_add_widget" {
		t.Fatalf("Name = %q, want the filename stem", m.Name)
	}
	if m.Description != "widget table, standalone" {
		t.Fatalf("Description = %q, want the file's leading comment", m.Description)
	}
	if m.SQL != "-- widget table, standalone\nCREATE TABLE widget (id INTEGER PRIMARY KEY);\n" {
		t.Fatalf("SQL = %q, want the file's full content", m.SQL)
	}
}

func TestParseTimestampedMigrationsSortsByFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/20261030120000_b.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id INTEGER);")},
		"migrations/20261030110000_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id INTEGER);")},
	}
	got, err := parseTimestampedMigrations(fsys, "migrations")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 || got[0].Name != "20261030110000_a" || got[1].Name != "20261030120000_b" {
		t.Fatalf("got %+v, want a (earlier timestamp) before b", got)
	}
}

func TestParseTimestampedMigrationsIgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/README.md":                     &fstest.MapFile{Data: []byte("# convention")},
		"migrations/20261030120000_add_widget.sql": &fstest.MapFile{Data: []byte("CREATE TABLE widget (id INTEGER);")},
	}
	got, err := parseTimestampedMigrations(fsys, "migrations")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d migrations, want 1 (README.md must be skipped)", len(got))
	}
}

func TestParseTimestampedMigrationsRefusesABadFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/add_widget.sql": &fstest.MapFile{Data: []byte("CREATE TABLE widget (id INTEGER);")},
	}
	if _, err := parseTimestampedMigrations(fsys, "migrations"); err == nil {
		t.Fatal("a filename with no leading 14-digit timestamp must be refused, not silently skipped or mis-ordered")
	}
}

func TestMigrationsIncludesTimestampedMigrationsAfterTheLegacyBlock(t *testing.T) {
	all := Migrations()
	if len(all) < 43 {
		t.Fatalf("got %d migrations, want at least the 43 legacy ones", len(all))
	}
	for i := range 43 {
		if all[i].ID != i+1 {
			t.Fatalf("legacy block disturbed at index %d: ID = %d, want %d", i, all[i].ID, i+1)
		}
	}
	// Every real migration (legacy or timestamped) after index 42 must have
	// a strictly larger ID than the row before it — the ordering property
	// migrationWorkers' sort relies on, true however many timestamped files
	// exist today.
	for i := 1; i < len(all); i++ {
		if all[i].ID <= all[i-1].ID {
			t.Fatalf("migrations not strictly increasing by ID at index %d: %d <= %d", i, all[i].ID, all[i-1].ID)
		}
	}
}
