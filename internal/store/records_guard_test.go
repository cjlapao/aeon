package store

// records_guard_test.go — the pinned greps S38 T6 demands. The records
// invariants are not just behaviours, they are ABSENCES: no file may write
// approved except the two Decide bodies, no SQL may mention execution_state,
// and no one may mutate the review ledger. Absences are enforced by grep or
// not at all — a comment saying "do not" is what the next agent edits away.

import (
	"os"
	"strings"
	"testing"
)

func TestNothingWritesApprovedOutsideDecide(t *testing.T) {
	// The two Decide bodies (goals.go's DecideGoal, briefs.go's DecideBrief)
	// bind the decision; every OTHER non-test file of this package may not
	// even mention the literal 'approved' inside a SQL string.
	// goals.go/briefs.go hold the two Decide bodies; actor.go names the
	// vocabulary it validates. schema.go carries the CHECK SET ('approved'
	// appears inside IN (...)) — a CHECK that admits a value is not a write
	// of it, so the literal probe spares schema.go while the WRITER probes
	// below spare nobody.
	allowed := map[string]bool{"goals.go": true, "briefs.go": true, "actor.go": true, "schema.go": true}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allowed[name] {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		probes := []string{"review_state='approved'", "review_state = 'approved'", `'approved'`}
		if name == "schema.go" {
			probes = probes[:2] // the CHECK vocabulary is not a write
		}
		for _, probe := range probes {
			if strings.Contains(string(src), probe) {
				t.Errorf("%s mentions %s — approved is reachable only through DecideGoal/DecideBrief (OL8)", name, probe)
			}
		}
	}
}

func TestNoSQLMentionsExecutionState(t *testing.T) {
	// OL11: derived state lives in code, never in a column.
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(src), "execution_state") {
			t.Errorf("%s names execution_state — OL11 keeps it out of the schema and out of the queries", name)
		}
	}
}

func TestReviewIsAppendOnly(t *testing.T) {
	// No UPDATE, no DELETE on review anywhere in the package — the ledger is
	// written by exactly one function (reviews.go's reviewInsert), and even
	// that one INSERTs through the claimed-FROM bodies only.
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	seen := 0
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := string(mustRead(t, name))
		for _, probe := range []string{"UPDATE review", "UPDATE  review", "DELETE FROM review"} {
			if strings.Contains(src, probe) {
				t.Errorf("%s mutates the review ledger: %s", name, probe)
			}
		}
		if n := strings.Count(src, "INSERT INTO review"); n > 0 {
			seen += n
			if name != "reviews.go" {
				t.Errorf("%s inserts review rows — reviewInsert in reviews.go is the only writer", name)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("found %d INSERT INTO review statements, want exactly the one in reviews.go", seen)
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return src
}
