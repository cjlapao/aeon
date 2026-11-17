package verify

// redact_test.go — the redaction's own honesty: it says in its doc comment
// that it is best-effort and not a guarantee, and the test that proves the
// limit (a shapeless secret passes through) is the pin that keeps the claim
// honest.

import (
	"os"
	"strings"
	"testing"
)

// TestRedactIsBestEffortAndSaysSo — the doc comment says the limit, the
// shapes are masked with the shape kept, and the documented miss (a secret
// with no shape at all) passes through: a redaction that claims to be more
// than it is is the failure VG6 refuses.
func TestRedactIsBestEffortAndSaysSo(t *testing.T) {
	// The doc comment, read from the source: the first sentence must carry
	// both halves of the claim.
	src, err := os.ReadFile("redact.go")
	if err != nil {
		t.Fatalf("redact.go will not read: %v", err)
	}
	i := strings.Index(string(src), "func Redact(")
	if i < 0 {
		t.Fatalf("the Redact function is missing from redact.go")
	}
	doc := redactDoc(t, string(src), i)
	first := doc[0] + " " + doc[1]
	if !strings.Contains(first, "best-effort") || !strings.Contains(first, "not a guarantee") {
		t.Errorf("the doc comment's first sentence %q must state the redaction is best-effort and not a guarantee", first)
	}

	// The shapes are masked, and the SHAPE is kept.
	const ghp = "ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ012345"
	const bearerTok = "AbCdEfGhIjKlMnOpQrStUvWxYz"
	const keyVal = "0123456789abcdef0123456789abcdef"
	cases := []struct{ in, want string }{
		{"token " + ghp + " end", "token ghp_*** end"},
		{"Authorization: Bearer " + bearerTok, "Authorization: Bearer ***"},
		{"api_key=" + keyVal, "api_key=***"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if got != c.want {
			t.Errorf("Redact(%q) = %q; the shape is masked to %q", c.in, got, c.want)
		}
	}

	// The documented miss: a secret with no shape — no prefix, no key name,
	// no Bearer line — passes through. That is the limit the doc names, and
	// the reason tails never reach more than verify.show.
	bare := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got := Redact("the value " + bare)
	if got != "the value "+bare {
		t.Errorf("the shapeless secret was altered (%q); the documented miss must pass through unchanged, and the doc must keep saying so", got)
	}
}

// redactDoc is the doc comment's lines above func Redact, outermost first.
// It skips blank lines so the comment sits directly on the function.
func redactDoc(t *testing.T, src string, funcAt int) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(src[:funcAt], " \t\r\n"), "\n")
	var doc []string
	for j := len(lines) - 1; j >= 0; j-- {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			doc = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))}, doc...)
			continue
		}
		break
	}
	if len(doc) < 2 {
		t.Fatalf("the Redact doc comment is too short to carry its claim")
	}
	return doc
}
