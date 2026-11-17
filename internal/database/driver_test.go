package database

import (
	"database/sql"
	"strings"
	"testing"
)

// The whole point of B3: cgo is on from the first commit, so there is no
// extension-missing state for S4 or S22 to handle.
func TestVecVersionAnswers(t *testing.T) {
	v, err := VecVersion()
	if err != nil {
		t.Fatalf("VecVersion: %v", err)
	}
	if v == "" {
		t.Fatal("vec_version() returned an empty string")
	}
	t.Logf("sqlite-vec %s", v)
}

// Auto() must be CALLED; a blank import registers nothing. This asserts the
// call happened by using the extension, which is the only honest check.
func TestExtensionIsRegisteredOnAFreshConnection(t *testing.T) {
	RegisterExtensions()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("create virtual table v using vec0(embedding float[4])"); err != nil {
		t.Fatalf("vec0 virtual table: %v", err)
	}
}

func TestRegisterExtensionsIsIdempotent(t *testing.T) {
	for range 3 {
		RegisterExtensions()
	}
	if _, err := VecVersion(); err != nil {
		t.Fatalf("after repeated registration: %v", err)
	}
}

// The pragmas must be in the syntax this driver honours. The other form is
// silently ignored, which is why this asserts by QUERYING rather than by
// comparing the DSN string.
func TestDSNPragmasAreActuallyApplied(t *testing.T) {
	RegisterExtensions()

	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/t.db?"+DSNPragmas)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var journal, fk, busy string
	if err := db.QueryRow("pragma journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if err := db.QueryRow("pragma foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if err := db.QueryRow("pragma busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}

	if !strings.EqualFold(journal, "wal") {
		t.Errorf("journal_mode = %q, want wal — the DSN syntax is not being honoured", journal)
	}
	if fk != "1" {
		t.Errorf("foreign_keys = %q, want 1 (it is per-connection and defaults OFF)", fk)
	}
	if busy == "0" {
		t.Errorf("busy_timeout = %q, want non-zero", busy)
	}
}

// The failure this guards: the glebarez syntax is accepted and ignored, so a
// store looks configured and is not.
func TestTheOtherDriversPragmaSyntaxIsIgnored(t *testing.T) {
	RegisterExtensions()

	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/t.db?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var journal string
	if err := db.QueryRow("pragma journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if strings.EqualFold(journal, "wal") {
		t.Skip("this driver now honours the _pragma() form; DSNPragmas' warning can be relaxed")
	}
	t.Logf("confirmed: the _pragma() form is ignored (journal_mode=%q), which is why DSNPragmas uses the other syntax", journal)
}
