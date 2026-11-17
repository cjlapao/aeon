package blueprint

import (
	"fmt"
	"strings"
	"testing"
)

// diff_test.go — S48 T2 rule 3: the diff is REAL. A test-side applier
// parses the emitted hunks and re-applies them to `a`; the result must be
// exactly `b`. Equal inputs give "". Twice the same inputs give the same
// bytes.

// applyUniDiff is an independent parser of unified-diff output (only the
// subset Diff emits): it reads the @@ headers and the +/-/context lines,
// honours "\ No newline at end of file", copies the untouched a-side lines
// by the headers' positions, and rebuilds b — byte-for-byte.
func applyUniDiff(t *testing.T, diff, a string) string {
	t.Helper()
	if diff == "" {
		t.Fatal("applier asked to apply an empty diff")
	}
	lines := strings.Split(diff, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	aAll := splitKeep(a)
	var out []string
	aIdx := 0 // next untouched a-line
	noFinal := false
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
		i++
	}
	for i < len(lines) {
		h := lines[i]
		if !strings.HasPrefix(h, "@@ ") {
			t.Fatalf("expected @@ header, got %q", h)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(h, "@@ "), " @@")
		var aStart, aCount, bStart, bCount int
		if _, err := fmt.Sscanf(body, "-%d,%d +%d,%d", &aStart, &aCount, &bStart, &bCount); err != nil {
			t.Fatalf("bad header %q: %v", h, err)
		}
		i++
		// Copy untouched a-lines up to the hunk's anchor.
		target := aStart - 1
		if aCount == 0 {
			target = aStart // empty-count names the line the insertion follows
		}
		if target < aIdx {
			t.Fatalf("hunk goes backwards: anchor %d at cursor %d", target, aIdx)
		}
		out = append(out, aAll[aIdx:target]...)
		aIdx = target
		consumedA, emittedB := 0, 0
		for (consumedA < aCount || emittedB < bCount) && i < len(lines) {
			l := lines[i]
			i++
			if strings.HasPrefix(l, "\\ ") {
				noFinal = true // the previously emitted line is final without newline
				continue
			}
			if l == "" {
				t.Fatalf("hunk ended early (bare empty line) with %d a-lines to consume", aCount-consumedA)
			}
			op, text := l[0], l[1:]
			switch op {
			case ' ':
				if aIdx >= len(aAll) || aAll[aIdx] != text {
					t.Fatalf("context line %d disagrees with a: %q vs %q", aIdx+1, text, at(aAll, aIdx))
				}
				out = append(out, text)
				aIdx++
				consumedA++
				emittedB++
			case '-':
				if aIdx >= len(aAll) || aAll[aIdx] != text {
					t.Fatalf("deleted line %d disagrees with a: %q vs %q", aIdx+1, text, at(aAll, aIdx))
				}
				aIdx++
				consumedA++
			case '+':
				out = append(out, text)
				emittedB++
			default:
				t.Fatalf("unknown op %q in %q", op, l)
			}
		}
	}
	// The tail beyond the last hunk must be untouched — and the diff gives
	// context to the file end only if it changed nothing: whatever remains
	// of a is appended (the round-trip still demands exact equality).
	out = append(out, aAll[aIdx:]...)
	if len(out) == 0 {
		return ""
	}
	joined := strings.Join(out, "\n")
	if !noFinal {
		joined += "\n"
	}
	return joined
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<past end>"
}

func splitKeep(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func TestDiffRoundTrips(t *testing.T) {
	// Round-trip cases. The full applier is delicate, so the round-trip is
	// asserted through a re-parse in the simplest shapes: small edits,
	// insertions, deletions, and the equal case.
	cases := [][2]string{
		{"a\nb\nc\n", "a\nX\nc\n"},
		{"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n", "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\nEIGHT\n"},
		{"a\n", "a\nb\n"}, // append
		{"a\nb\n", "a\n"}, // delete last
		{"x\n", ""},       // everything gone
		{"", "y\n"},       // everything new
		{"l1\nl2\nl3\n", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n"}, // insert tail
		{"a\nb\n\nc\n", "a\n\nb\nc\n"},                       // blank-line move
	}
	for _, tc := range cases {
		d := Diff([]byte(tc[0]), []byte(tc[1]), "a", "b")
		if d == "" {
			t.Fatalf("%q → %q gave an empty diff", tc[0], tc[1])
		}
		got := applyUniDiff(t, d, tc[0])
		if got != tc[1] {
			t.Fatalf("round-trip failed for %q → %q\n--- diff ---\n%s\n--- got ---\n%q\n--- want ---\n%q", tc[0], tc[1], d, got, tc[1])
		}
	}
}

func TestDiffEqualIsEmpty(t *testing.T) {
	s := "id: x\nlanes: []\n"
	if d := Diff([]byte(s), []byte(s), "a", "b"); d != "" {
		t.Fatalf("equal inputs gave %q", d)
	}
	if d := Diff(nil, nil, "a", "b"); d != "" {
		t.Fatalf("empty equal inputs gave %q", d)
	}
}

func TestDiffIsDeterministic(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive\n"
	b := "one\nTWO\nthree\nFOUR\nfive\nsix\n"
	first := Diff([]byte(a), []byte(b), "a", "b")
	for i := 0; i < 20; i++ {
		if got := Diff([]byte(a), []byte(b), "a", "b"); got != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, got, first)
		}
	}
	if !strings.HasPrefix(first, "--- a\n+++ b\n") {
		t.Fatalf("missing labels: %q", first)
	}
	if !strings.Contains(first, "@@ ") {
		t.Fatalf("missing hunk header: %q", first)
	}
}
