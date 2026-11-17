package config

import (
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// settings_untouched_test.go — CS1 made mechanical. The whole slice "adds,
// does not edit": these two tests assert it, so a future change to a five-layer
// file (or a store import into a settings file) is caught by go test, not by
// a review.

// The five-layer pin. These SHA-256 digests were recorded at commit a74b8960
// (T0, before this slice's work) and must stay byte-identical: a change to any
// of these files is a CS1 breach, and the thing to update is THIS pin (in a
// slice that says why), never the file.
const (
	pinLayers     = "79f71738765b305413328dfbacf5ab9521396165eeafbd8a71ad176811081e82"
	pinValidate   = "52d391920c90cbd2603a76a012e890a2b4a5a84b6404576c82a1758528c4f59e"
	pinCreds      = "9775d9a3ec79cf20755a9651410f7af1c8292a18fb9b60e8a63a7b92fd60f534"
	pinConfigTest = "20cd6301f8be9afdb8fe7c8c08a4e088243ac03d75821dfb6c24660cd9442340"
	pinCredsTest  = "5eff90abdd677fe48cb035f528ab8218118d0e0bcfdb4bf00ef9a6911a8a075f"
)

// TestFiveLayerFilesAreUntouched computes the SHA-256 of each of the five
// files T2 and the slice must not touch (read via os.ReadFile relative to the
// package dir) and compares each to the digest recorded at commit a74b8960.
func TestFiveLayerFilesAreUntouched(t *testing.T) {
	cases := []struct{ file, pin string }{
		{"layers.go", pinLayers},
		{"validate.go", pinValidate},
		{"creds.go", pinCreds},
		{"config_test.go", pinConfigTest},
		{"creds_test.go", pinCredsTest},
	}
	for _, c := range cases {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", c.file, err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		if got != c.pin {
			t.Fatalf("the five layers are CS1's untouched contract — if you meant to change one, this slice's pin is the thing to update, in a slice that says why\n%s: sha256 = %s, want %s", c.file, got, c.pin)
		}
	}
}

// TestNoSettingsFileImportsStore is CS2 made mechanical: SQL stays in
// internal/store, so no settings*.go file may import internal/store,
// database/sql or gorm. It parses every settings*.go import block and fails on
// any of the three.
func TestNoSettingsFileImportsStore(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// the import paths CS2 forbids in a settings file (SQL lives in internal/store)
	blacklist := []string{"internal/store", "database/sql", "gorm"}
	fset := token.NewFileSet()
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "settings") || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		n++
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range blacklist {
				if strings.Contains(pkg, bad) {
					t.Fatalf("%s imports %q, which CS2 forbids (SQL stays in internal/store)", e.Name(), pkg)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no settings*.go files found")
	}
}
