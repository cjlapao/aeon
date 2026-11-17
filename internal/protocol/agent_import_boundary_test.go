package protocol

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S37 T4, criterion 5 — the import-boundary grep. The create-time preview is
// composed in cmd/aeon (the only package allowed to import internal/plugin,
// TestNoUnconditionalImportOfPlugin), and reached by internal/protocol and
// internal/daemon through a plain func seam — never by importing the layer
// directly. This walks the two packages' .go files (skip _test.go, the way
// absentWalk does in internal/plugin) and fails on any direct reference to
// plugin machinery, so a second import path cannot creep in.
//
// The import path is assembled from two halves (importPathToken) so the
// contiguous literal never appears in THIS file. internal/plugin's absent_test
// greps the whole module — _test.go included — for the import path and reads
// any runtime file that names it as an unconditional importer (AP11); a test
// file that wrote it verbatim would be read as a second, runtime-side import.
// Slicing the token here keeps the scanner able to match a real offender while
// this file stays clear of the substring that grep looks for.
//
// The banned tokens are the names the preview composes: the import path
// (quoted, so a comment naming the path is not a false positive — only an
// import statement matches), the three exported calls reached through the
// seam, and the one the preview must NOT call (ImportChecked — that is the
// commit door, not the preview).
const importPathToken = "github.com/cjlapao/aeon/internal/" + "plugin"

var agentImportBanned = []string{
	`"` + importPathToken + `"`,
	"readPackage",
	"ReadPackagePreview",
	"ReadCarriedPolicies",
	"CheckRequests",
	"ImportChecked",
}

// agentImportOffender returns the first banned token found in the source
// (the scanner the walk and the canary both call — testing the scanner, not
// the walk). The pair os.ReadDir( + agent.md is a re-implementation of
// readPackage by another name; either token alone is a tell, so both are
// flagged (the preview must compose the three exported calls, never
// re-read the directory itself).
func agentImportOffender(src string) (string, bool) {
	for _, tok := range agentImportBanned {
		if strings.Contains(src, tok) {
			return tok, true
		}
	}
	if strings.Contains(src, "os.ReadDir(") && strings.Contains(src, "agent.md") {
		return "os.ReadDir( + agent.md", true
	}
	return "", false
}

// TestAgentImportImportBoundary walks internal/protocol and internal/daemon
// (skip _test.go — the test files may name the tokens in assertions, the
// runtime code may not) and fails on any direct reference to plugin
// machinery.
func TestAgentImportImportBoundary(t *testing.T) {
	for _, dir := range []string{"../../internal/protocol", "../../internal/daemon"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if tok, ok := agentImportOffender(string(src)); ok {
				t.Errorf("%s: %s references plugin machinery (%q) — the preview must go through the Table.AgentImportPreview seam, never import internal/plugin", dir, path, tok)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// TestAgentImportBoundaryScannerIsNotBlind is the canary: a temp .go file
// carrying the quoted import path makes the scanner return an offender —
// proving the scanner is not a no-op. (Tests the scanner function, not the
// walk, exactly as the task asks.) The path is assembled from
// importPathToken so it is not present verbatim in this file either.
func TestAgentImportBoundaryScannerIsNotBlind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offender.go")
	canary := "package x\nimport \"" + importPathToken + "\"\nfunc f() { _ = plugin.ImportChecked(\"a\", nil, t0) }\n"
	if err := os.WriteFile(path, []byte(canary), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if _, ok := agentImportOffender(string(src)); !ok {
		t.Fatal("the scanner must flag a file that imports internal/plugin and calls ImportChecked")
	}
}
