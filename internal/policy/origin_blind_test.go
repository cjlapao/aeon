package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvaluatorNeverReadsOrigin is PM11's grep, made executable (S45 T12):
// origin is PROVENANCE — where a rule came from — not policy content, and
// the deciding machinery must never consult it. A gate that folded
// 'system' rows into its verdict differently from 'user' rows would make
// the seed door a back-door widening of the guard, and D4's whole argument
// is that the machine's rules and the operator's rules are judged exactly
// alike. The four files are the whole deciding path — the pure evaluator,
// the in-force answer, the one verdict sentence, and the MCP projection's
// grant read — scanned for ANY mention of the word, code or comment: a
// reader that merely discusses provenance is already a reader that can be
// tempted to branch on it.
func TestEvaluatorNeverReadsOrigin(t *testing.T) {
	root := moduleRoot(t)
	for _, rel := range []string{
		"internal/policy/evaluate.go",
		"internal/policy/inforce.go",
		"internal/policy/verdict.go",
		"internal/mcp/grants.go",
	} {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(strings.ToLower(line), "origin") {
				t.Errorf("%s:%d mentions origin: %s", rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}
