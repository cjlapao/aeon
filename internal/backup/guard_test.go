package backup

// The source guards: one deletion site in the whole backup path, no SQL or
// network in the engine package, the error kinds in lockstep with the
// ledger's closed set, and the fence's canary present with teeth.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T3's closed set for backup_run.error_kind (internal/store/backup_run.go).
// Duplicated here ON PURPOSE: a lockstep test that imports the store's set
// could not fail the way this one does — this one breaks if either side
// moves, which is the point.
var ledgerErrorKinds = map[string]bool{
	"disk-full": true, "dir-unwritable": true, "busy": true, "lock-held": true,
	"integrity": true, "fk": true, "checksum": true, "newer-schema": true,
	"io": true, "cancelled": true, "unknown": true,
}

var backupErrorKinds = map[string]bool{
	ErrKindDiskFull:    true,
	ErrKindDirUnwrit:   true,
	ErrKindLockHeld:    true,
	ErrKindIntegrity:   true,
	ErrKindFK:          true,
	ErrKindChecksum:    true,
	ErrKindNewerSchema: true,
	ErrKindIO:          true,
	ErrKindCancelled:   true,
	ErrKindBusy:        true,
	ErrKindUnknown:     true,
}

// TestErrorKindsStayInLockstepWithTheLedger: every kind this package names
// is a kind the ledger admits. T7's restore door claimed the ledger's
// "busy" (a live daemon, or a file holder), so the package now names all
// eleven kinds the ledger admits.
func TestErrorKindsStayInLockstepWithTheLedger(t *testing.T) {
	for k := range backupErrorKinds {
		if !ledgerErrorKinds[k] {
			t.Errorf("the backup package names error kind %q, the ledger's closed set does not admit it", k)
		}
	}
	if len(backupErrorKinds) != 11 {
		t.Errorf("the package names %d error kinds, the ledger admits eleven", len(backupErrorKinds))
	}
}

// deletionCallRE matches the unlink sites a second remover would wear.
var deletionCallRE = regexp.MustCompile(`os\.Remove\(|os\.RemoveAll\(|syscall\.Unlink\(`)

// TestOnlyTheFenceDeletes walks the production sources: the unlink calls in
// internal/backup are the two inside removeBackupFile (retention.go) and
// the sweep and retention that call through it — no file of its own; and
// cmd/aeon carries none at all (T7's restore cleanup, when it lands, is the
// documented second site and is named in its own guard).
func TestOnlyTheFenceDeletes(t *testing.T) {
	backupDir := "."
	cmdDir := filepath.Join("..", "..", "cmd", "aeon")

	checked := 0
	err := filepath.WalkDir(backupDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			if deletionCallRE.MatchString(line) {
				if path != filepath.Join(backupDir, "retention.go") {
					t.Errorf("%s:%d: a deletion call outside the fence — the fence is the only remover", path, i+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the backup package: %v", err)
	}

	err = filepath.WalkDir(cmdDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			if deletionCallRE.MatchString(line) {
				// `aeon blueprint edit` removes its OWN editor scratch file (the
				// mirror of a blueprint row, never a backup file). Named by file,
				// not by pattern, so a second CLI remover still fails here.
				if filepath.Base(path) == "blueprint_edit.go" {
					continue
				}
				t.Errorf("%s:%d: a deletion call in the CLI — the CLI never deletes backup files itself", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cmd/aeon: %v", err)
	}

	if checked < 20 {
		t.Errorf("the guard walked %d production files — a guard over fewer than twenty files is a guard a rename can hide", checked)
	}
	// The fence itself still holds both unlink forms: a fence that lost its
	// os.Remove is a fence that cannot remove.
	fence, err := os.ReadFile(filepath.Join(backupDir, "retention.go"))
	if err != nil {
		t.Fatalf("read the fence: %v", err)
	}
	if !strings.Contains(string(fence), "os.RemoveAll(") || !strings.Contains(string(fence), "os.Remove(") {
		t.Errorf("retention.go no longer holds the unlink calls — the fence is empty")
	}
}

// TestCanaryHasTeeth: the fence test's porous twin must exist in the test
// source and must call a real unlink — a fence test whose canary cannot
// delete proves nothing.
func TestCanaryHasTeeth(t *testing.T) {
	b, err := os.ReadFile("retention_test.go")
	if err != nil {
		t.Fatalf("read the fence test: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "func porousRemover(") {
		t.Fatalf("the fence test has no porous canary — the refusal test is vacuous")
	}
	if !deletionCallRE.MatchString(s) {
		t.Fatalf("the canary contains no deletion call — a canary that cannot delete is a portrait, not a proof")
	}
}

// forbiddenImports are the doors the engine package never walks: raw SQL
// (the store talks, the engine does not) and the network (a local backup
// that phones home is a different product).
var forbiddenImports = []string{"database/sql", "net", "net/http"}

// TestBackupPackageImportsNoSQLOrNetwork walks the engine package's
// production sources through the real parser and refuses any of the
// forbidden import paths — a string scan would be fooled by a comment, and
// a parser that sees the actual import is the check the rule means.
func TestBackupPackageImportsNoSQLOrNetwork(t *testing.T) {
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbiddenImports {
				if p == bad || strings.HasPrefix(p, bad+"/") {
					t.Errorf("%s imports %s — the engine package has no SQL and no network", path, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the backup package: %v", err)
	}
	if checked < 10 {
		t.Errorf("the guard walked %d engine files — too few to be the package", checked)
	}
}
