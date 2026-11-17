package store

// reader_guard_test.go — S60 T4 (behaviour 5): the raw settings columns are
// visible to exactly two kinds of Go code — the row codec inside this
// package, and ONE reader, EffectiveComponent/EffectiveForRoot through
// effective.go. No loop, action or daemon file may hand-read a
// `branch_prefix` or `default_branch` out of SQL and thereby invent a
// second resolution of a component's naming.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// readerGuardAllowed names every file that may carry a raw settings column
// inside a string literal, and why. effective.go is the reader; the rest is
// the codec (row scans, INSERT/UPDATE columns, the migration ledger, the
// export mirror). A new offender must join this list by EXPLANATION, which
// is the point of the guard: adding a reader is always a reviewable diff.
var readerGuardAllowed = map[string]string{
	// S61 T4 (dated 2026-11-05): the GitHub repository payload names the
	// platform's JSON field `default_branch` — a WIRE binding to the forge's
	// own transport, parsed by encoding/json into forge.Repo, never a SQL read
	// and never a second resolution of a component's naming. The guard's rule
	// is EXPLANATION, and this is one: same word, different world. (The host
	// is not spelled here: the forge's no-network walk indicts every test
	// file that names it, which is exactly the discipline working.)
	"internal/forge/github.go":             "GitHub's repository JSON field (wire binding, not a settings column)",
	"internal/forge/forge.go":              "the forge.Repo wire field's JSON tag — the same platform word, not a settings column",
	"internal/action/registry.go":          "forge.createRepo's declared EVIDENCE keys (GI10's contract) — answer fields, not column reads",
	"internal/action/forge_create_repo.go": "the same evidence keys at their two emission sites — answers, not column reads",
	"internal/store/effective.go":          "the one reader (EffectiveComponent)",
	"internal/store/components.go":         "row codec: scans/inserts/updates the columns on the component row",
	"internal/store/loops.go":              "row codec: the loop branch columns pinned at loop.start",
	"internal/store/blueprint.go":          "row codec: the pinned blueprint's loop row",
	"internal/store/schema.go":             "the migration DDL itself",
	"internal/store/export.go":             "the read-only export mirror",
}

func TestNoReaderBypassesEffective(t *testing.T) {
	fset := token.NewFileSet()
	var offenders []string
	root := filepath.Join("..", "..") // the package dir is internal/store
	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		if err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel := strings.TrimPrefix(filepath.ToSlash(path), root+"/")
			if _, ok := readerGuardAllowed[rel]; ok {
				return nil
			}
			path = rel // reported name; parsing re-joins below
			f, err := parser.ParseFile(fset, filepath.Join(root, path), nil, 0)
			if err != nil {
				return err
			}
			// Only STRING LITERALS count (SQL lives in them); comments
			// that merely NAME the columns are documentation, not reads.
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, col := range []string{"branch_prefix", "default_branch"} {
					if strings.Contains(lit.Value, col) {
						offenders = append(offenders, fmt.Sprintf("%s:%d names %s outside the codec",
							path, fset.Position(lit.Pos()).Line, col))
					}
				}
				return true
			})
			return nil
		}); err != nil {
			if !strings.Contains(err.Error(), "no such file") {
				t.Fatal(err) // cmd/ exists; a missing tree is a real failure
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("raw settings columns named outside the allowed codec/reader:\n%s",
			strings.Join(offenders, "\n"))
	}
}
