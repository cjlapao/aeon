package policy_test

// The splitter's tests (T4). Three layers, mirroring the matcher's file:
//
//   - (a) The SEPARATOR TABLE, one named test per property the split must
//     have: the four separators each cut, empty segments dropped, items
//     trimmed, and — the case the refusal prose exists for — "cd sub && git
//     push" denied on its SECOND segment, named by index.
//   - (b) The LIMIT on both sides: exactly MaxSegments is a complete reading,
//     MaxSegments+1 states Count and OverLimit rather than truncating quietly
//     (brief D11 — a stated limit, never a silent one).
//   - (c) The DOCUMENTED LIMITS, asserted as EXPECTED BEHAVIOUR, not defects:
//     a bash -c wrapper is one segment, a $( ) form is one, a quoted "a && b"
//     is two. Whoever reads this file later must not be able to mistake the
//     guard for a sandbox — the assertions say so in executable form.
//
// Plus the purity pin: TestSegmentFileImportsOnlyStrings walks segment.go's
// import list as SYNTAX (go/parser, never a text grep — the idiom T2
// established and the lessons insist on: a grep hits the guard's own
// forbidden words in comments).
//
// No test here reads the filesystem, a clock, or the environment (the parser
// reads segment.go itself, which is source, not state): Split is pure, so
// every case below is a value in and a value out.

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// canonicalSeparators re-declares the guard's separator set where the fuzz
// invariants can count and validate it without reaching into unexported
// state: Separators may only ever hold these, in this order, at most once
// each.
var canonicalSeparators = []string{"&&", "||", ";", "\n"}

// eqStrings compares two string slices elementwise; nil and empty are the
// same answer here because neither can a caller distinguish usefully.
func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── (a) the separator table ─────────────────────────────────────────────────

func TestSplitSeparators(t *testing.T) {
	// The four separators the guard knows, each named in its own case, plus
	// the mixed line that pins Separators' canonical order, plus the cases
	// that keep the split honest about what is NOT a separator: single "&"
	// and single "|" are literal bytes.
	cases := []struct {
		name    string
		command string
		want    []string
		seps    []string
	}{
		{
			// THE case the refusal prose exists for: the denied segment is
			// the second one, and the data T5's fold and T12's question body
			// need is this index.
			name:    "cd sub && git push cuts at the && with push at index 1",
			command: "cd sub && git push",
			want:    []string{"cd sub", "git push"},
			seps:    []string{"&&"},
		},
		{
			name:    "|| cuts",
			command: "git push || git status",
			want:    []string{"git push", "git status"},
			seps:    []string{"||"},
		},
		{
			name:    "semicolon cuts",
			command: "ls; pwd",
			want:    []string{"ls", "pwd"},
			seps:    []string{";"},
		},
		{
			name:    "newline cuts (O2: a two-line script is ordinary, not adversarial)",
			command: "make build\nmake test",
			want:    []string{"make build", "make test"},
			seps:    []string{"\n"},
		},
		{
			name:    "CRLF newline cuts and the orphan \\r trims away",
			command: "ls\r\npwd",
			want:    []string{"ls", "pwd"},
			seps:    []string{"\n"},
		},
		{
			name:    "mixed separators cut in order and list canonically",
			command: "a && b || c; d\ne",
			want:    []string{"a", "b", "c", "d", "e"},
			seps:    []string{"&&", "||", ";", "\n"},
		},
		{
			name:    "single ampersand and pipe are literal bytes, not separators",
			command: "echo a & b | c",
			want:    []string{"echo a & b | c"},
			seps:    nil,
		},
		{
			name:    "no separator is one segment",
			command: "git status",
			want:    []string{"git status"},
			seps:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.Split(tc.command)
			if !eqStrings(got.Items, tc.want) {
				t.Errorf("Split(%q).Items = %q, want %q", tc.command, got.Items, tc.want)
			}
			if !eqStrings(got.Separators, tc.seps) {
				t.Errorf("Split(%q).Separators = %q, want %q", tc.command, got.Separators, tc.seps)
			}
			if got.Count != len(tc.want) {
				t.Errorf("Split(%q).Count = %d, want %d", tc.command, got.Count, len(tc.want))
			}
			if got.OverLimit {
				t.Errorf("Split(%q).OverLimit = true on a short command", tc.command)
			}
		})
	}

	// The index claim, spelled with the matcher itself: on "cd sub && git
	// push", a "git push" rule matches segment 1 and nothing else, so the
	// refusal T5 folds can name index 1 rather than a flat "denied".
	segs := policy.Split("cd sub && git push")
	if len(segs.Items) != 2 || segs.Items[1] != "git push" {
		t.Fatalf(`Split("cd sub && git push").Items = %q, want ["cd sub","git push"] with push at index 1`, segs.Items)
	}
	if !policy.MatchPattern("git push*", segs.Items[1]) {
		t.Error(`the denied matcher must hit Items[1] ("git push")`)
	}
	if policy.MatchPattern("git push*", segs.Items[0]) {
		t.Error(`the denied matcher must NOT hit Items[0] ("cd sub"); the index would lie`)
	}
}

func TestSplitDropsEmptySegments(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"trailing semicolons create no empty segment", "ls;;;", []string{"ls"}},
		{"trailing && with nothing after it", "ls && ", []string{"ls"}},
		{"separator soup is zero segments", ";;&&||\n\n;", []string{}},
		{"empty command is zero segments", "", []string{}},
		{"whitespace-only is zero segments", "   ", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.Split(tc.command)
			if !eqStrings(got.Items, tc.want) {
				t.Errorf("Split(%q).Items = %q, want %q", tc.command, got.Items, tc.want)
			}
			if got.Count != len(tc.want) {
				t.Errorf("Split(%q).Count = %d, want %d (Count counts real segments, empties dropped)",
					tc.command, got.Count, len(tc.want))
			}
		})
	}
}

func TestSplitTrimsSegments(t *testing.T) {
	// "git push   &&   git status" must answer with unpadded items: the
	// matcher compares byte for byte, so an untrimmed " git status" would
	// match no authored pattern and the padding would silently widen denials.
	got := policy.Split("git push   &&   git status")
	if !eqStrings(got.Items, []string{"git push", "git status"}) {
		t.Errorf("Items = %q, want [\"git push\",\"git status\"] with no padding", got.Items)
	}
	got = policy.Split("\tgit status\t;  ls ")
	if !eqStrings(got.Items, []string{"git status", "ls"}) {
		t.Errorf("Items = %q, want tab- and space-trimmed items", got.Items)
	}
}

// ── (b) the limit, on both sides ────────────────────────────────────────────

// repeatSemicolon builds n "x;" pieces, which split into n segments.
func repeatSemicolon(n int) string {
	return strings.Repeat("x;", n)
}

func TestSplitBoundaryAtLimit(t *testing.T) {
	// Exactly MaxSegments is a COMPLETE reading: OverLimit false, Items full,
	// Count agreeing with len(Items). The boundary is asserted on this side…
	got := policy.Split(repeatSemicolon(policy.MaxSegments))
	if got.Count != policy.MaxSegments {
		t.Fatalf("Count = %d, want MaxSegments (%d)", got.Count, policy.MaxSegments)
	}
	if got.OverLimit {
		t.Errorf("OverLimit = true at exactly MaxSegments; the boundary must not trip early")
	}
	if len(got.Items) != policy.MaxSegments {
		t.Errorf("len(Items) = %d, want %d", len(got.Items), policy.MaxSegments)
	}
	if got.Count != len(got.Items) {
		t.Errorf("Count (%d) disagrees with len(Items) (%d) inside the limit", got.Count, len(got.Items))
	}
}

func TestSplitOverLimitIsStatedNotSilent(t *testing.T) {
	// …and on that side: MaxSegments+1 segments answer with the REAL Count,
	// OverLimit true, and Items truncated — the truncation visible in the
	// struct rather than silent, because "not analysed" must read as such and
	// resolve toward deny.
	got := policy.Split(repeatSemicolon(policy.MaxSegments + 1))
	if got.Count != policy.MaxSegments+1 {
		t.Errorf("Count = %d, want %d (Count counts ALL segments before the limit)",
			got.Count, policy.MaxSegments+1)
	}
	if !got.OverLimit {
		t.Fatal("OverLimit = false past the limit; a truncated Items reading must never look complete")
	}
	if len(got.Items) != policy.MaxSegments {
		t.Errorf("len(Items) = %d, want %d (bounded work for a pathological line)",
			len(got.Items), policy.MaxSegments)
	}
	for i, item := range got.Items {
		if item != "x" {
			t.Fatalf("Items[%d] = %q, want \"x\" (truncation keeps the leading segments in order)", i, item)
		}
	}
}

// ── (c) documented limits, asserted as expectations (brief D11) ─────────────

func TestDocumentedLimitsAreNotTreatedAsBugs(t *testing.T) {
	// These assertions are the record that the splitter is NOT a sandbox and
	// nobody should read it as one. If a later task "fixes" any of them, this
	// test fails LOUDLY — the documented expectation (D11) is enforced, and a
	// real shell grammar would be a different, larger decision than this
	// slice's brief authorises.

	t.Run("bash -c wrapper is ONE segment matched as written", func(t *testing.T) {
		const cmd = `bash -c "git push"`
		got := policy.Split(cmd)
		if got.Count != 1 || len(got.Items) != 1 {
			t.Fatalf("Split(%q) = %q (Count %d), want one segment: the splitter does not read quotes",
				cmd, got.Items, got.Count)
		}
		if got.Items[0] != cmd {
			t.Errorf("the wrapper must survive as written, got %q", got.Items[0])
		}
		// Honest consequence, executable: the wrapper matches "bash*" as
		// written, and it does NOT match a "git push" rule — the inner
		// command is not caught. Documented, not a defect: the tool
		// allow-list is the real boundary.
		if !policy.MatchPattern("bash*", got.Items[0]) {
			t.Error(`"bash*" must match the wrapper as written (that is what "as written" promises)`)
		}
		if policy.MatchPattern("git push*", got.Items[0]) {
			t.Error(`"git push*" must NOT match the wrapper; asserting otherwise would claim containment`)
		}
	})

	t.Run("dollar-parenthesis substitution is ONE segment", func(t *testing.T) {
		const cmd = `git $(echo push)`
		got := policy.Split(cmd)
		if got.Count != 1 || len(got.Items) != 1 || got.Items[0] != cmd {
			t.Fatalf("Split(%q) = %q (Count %d), want the whole string as one segment", cmd, got.Items, got.Count)
		}
		if policy.MatchPattern("git push*", got.Items[0]) {
			t.Error(`the unexpanded form must not match "git push*"; the substitution is not evaluated, by design`)
		}
	})

	t.Run("backtick substitution is ONE segment", func(t *testing.T) {
		const cmd = "git `echo push`"
		got := policy.Split(cmd)
		if got.Count != 1 || len(got.Items) != 1 || got.Items[0] != cmd {
			t.Fatalf("Split(%q) = %q (Count %d), want the whole string as one segment", cmd, got.Items, got.Count)
		}
	})

	t.Run("a quoted separator still cuts into TWO segments", func(t *testing.T) {
		// The safe direction, asserted in the direction that matters: quotes
		// are not understood, so "a && b" inside quotes is cut anyway. The
		// spurious extra segments can only ever LOSE a match, never invent a
		// permission — which is why this limitation needs no fix.
		got := policy.Split(`echo "a && b"`)
		if got.Count != 2 || !eqStrings(got.Items, []string{`echo "a`, `b"`}) {
			t.Fatalf(`Split on a quoted "a && b" = %q (Count %d), want two segments — quoting is not understood, deliberately`,
				got.Items, got.Count)
		}
	})
}

// ── totality ────────────────────────────────────────────────────────────────

func TestSplitNeverPanics(t *testing.T) {
	// The pinned spec case: fuzz — arbitrary bytes, all-separator input,
	// very large input. Here the corpus runs DETERMINISTICALLY, as named
	// subtests a verifier's -run reaches; FuzzSplitNeverPanics below carries
	// the same corpus to f.Fuzz so `go test -fuzz` can explore past it
	// (a Test-named function may not take *testing.F — the fuzz target must
	// be Fuzz-named, so the two halves carry the same seeds through
	// splitNeverPanicsInvariants).
	seeds := []string{
		"",
		";",
		"&&",
		"||",
		"\n",
		"&&&&&&",
		";;&|&||\n\n;;",
		"a\x00b\x01;c",
		"&&;",
		";;\n\n&&&&||||",
		strings.Repeat(";", 1000),          // all-separator: zero segments, bounded
		strings.Repeat("&&", 40),           // 41 segments: over the limit
		strings.Repeat("cmd;", 50000),      // very large input: bounded work
		"\xff\xfe;|\xc0\x80&&\xe2\x82\x9a", // invalid UTF-8 is just bytes
		" x ; " + strings.Repeat("y ", 300) + ";",
	}
	for _, s := range seeds {
		name := s
		if len(name) > 24 {
			name = name[:24] + "…"
		}
		t.Run(name, func(t *testing.T) {
			splitNeverPanicsInvariants(t, s)
		})
	}
}

func FuzzSplitNeverPanics(f *testing.F) {
	for _, s := range []string{
		"", ";", "&&", "||", "\n", "&&&&&&", ";;&|&||\n\n;;", "a\x00b\x01;c", "&&;",
		";;\n\n&&&&||||",
		strings.Repeat(";", 1000),
		strings.Repeat("&&", 40),
		strings.Repeat("cmd;", 5000),
		"\xff\xfe;|\xc0\x80&&\xe2\x82\x9a",
		" x ; " + strings.Repeat("y ", 300) + ";",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		splitNeverPanicsInvariants(t, s)
	})
}

// splitNeverPanicsInvariants states the structural promises of Split for
// EVERY input: total (no panic — reaching here at all is the panic test),
// completeness decided by the (Count, OverLimit) pair and never by a quiet
// truncation, items non-empty and trimmed — plus one ORACLE: a separator
// token is listed in Separators exactly when it occurs in the input (a
// two-character token's match cannot hide a later "&&"/"||" occurrence
// without itself being that occurrence), listed once, in canonical order.
func splitNeverPanicsInvariants(t *testing.T, s string) {
	t.Helper()
	got := policy.Split(s)
	if got.OverLimit && len(got.Items) != policy.MaxSegments {
		t.Fatalf("Split(%q): OverLimit true but len(Items) = %d, want %d", s, len(got.Items), policy.MaxSegments)
	}
	if !got.OverLimit && got.Count != len(got.Items) {
		t.Fatalf("Split(%q): Count %d disagrees with len(Items) %d inside the limit", s, got.Count, len(got.Items))
	}
	if got.Count < len(got.Items) {
		t.Fatalf("Split(%q): Count %d less than len(Items) %d", s, got.Count, len(got.Items))
	}
	var want []string
	for _, tok := range canonicalSeparators {
		if strings.Contains(s, tok) {
			want = append(want, tok)
		}
	}
	if !eqStrings(got.Separators, want) {
		t.Fatalf("Split(%q): Separators = %q, oracle says %q", s, got.Separators, want)
	}
	for _, item := range got.Items {
		if item == "" || strings.TrimSpace(item) != item {
			t.Fatalf("Split(%q): item %q is empty or untrimmed", s, item)
		}
	}
}

// ── the purity pin ──────────────────────────────────────────────────────────

func TestSegmentFileImportsOnlyStrings(t *testing.T) {
	// The validation claim made executable: segment.go imports no
	// third-party package and neither os nor time — the package purity claim
	// (D5) holds for the splitter. Asserted as SYNTAX via go/parser, never a
	// text grep (a grep hits the guard's own forbidden words in comments and
	// gets the guard weakened until it matches nothing).
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "segment.go", nil, 0)
	if err != nil {
		t.Fatalf("parse segment.go: %v", err)
	}
	if len(f.Imports) != 1 {
		names := make([]string, 0, len(f.Imports))
		for _, imp := range f.Imports {
			names = append(names, strings.Trim(imp.Path.Value, `"`))
		}
		t.Fatalf("segment.go imports %d packages (%q); its only import must be \"strings\" — "+
			"no third-party package, no os, no time (the purity claim, D5)", len(f.Imports), names)
	}
	if got := strings.Trim(f.Imports[0].Path.Value, `"`); got != "strings" {
		t.Fatalf("segment.go imports %q; its only import must be \"strings\"", got)
	}
}
