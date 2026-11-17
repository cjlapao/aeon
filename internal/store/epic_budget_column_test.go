package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoProductionReadOfEpicBudgetColumn — S55 T8 (LC13): after the T6
// backfill, epic.budget_usd is DEAD. No production Go code reads or writes
// the column: the budget row (scope='epic', window='lifetime') is the single
// truth the epic view derives from, and a second reader would be a second
// truth — the exact disease LC13 exists to cure.
//
// The two allowed namings of the column live OUTSIDE this walk: schema.go's
// epic DDL (the column stays — dropping it is a rebuild S55 does not need)
// and the T6 .sql backfill (the migration helper, the one read that moved
// the data). schema.go is the single allowed .go: it declares the column, it
// does not read it.
func TestNoProductionReadOfEpicBudgetColumn(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const col = "budget_usd"
	var offenders []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch filepath.Base(path) {
			case "ui", ".tmp", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !strings.Contains(string(raw), col) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel != filepath.Join("internal", "store", "schema.go") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("production Go names the dead epic.%s column (LC13: the budget row is the single truth; the T6 .sql backfill is the only remaining read) — offenders: %v", col, offenders)
	}
}
