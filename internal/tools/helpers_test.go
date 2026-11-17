package tools

import (
	"os"
	"strings"
	"testing"
)

// packageSources reads this package's non-test sources, for the invariants that
// are about the shape of the code rather than about its behaviour.
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(b)
	}
	if len(out) < 4 {
		t.Fatalf("expected several sources, found %d — a check over nothing passes forever", len(out))
	}
	return out
}
