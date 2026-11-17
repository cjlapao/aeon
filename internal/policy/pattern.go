package policy

// The command matcher: the command policy's whole grammar, in one file.
//
// Three properties, each traceable to the committed corpus (T1's corpus.json,
// read through T2's loader): '*' may appear anywhere
// ("docker buildx build*--push*" sits mid-command), a pattern with no '*'
// matches ONLY the identical string ("gh", "pwd", "git branch --show-current"),
// and '*' is the only wildcard the corpus ever uses — asserted from the rules
// themselves by TestCorpusWildcardSetIsAsteriskOnly, not read from the
// generator's own claim.
//
// This is hand-rolled on purpose. path.Match was measured here (go1.26.2) and
// is unusable for this grammar: it answers Match("rm -r?", "rm -rx") == true
// and Match("rm -r[abc]", "rm -ra") == true, while this grammar requires '?'
// and brackets to be literal bytes, and it answers Match("", "") == true,
// while the empty pattern must match nothing. A third-party glob library
// would bring the same extras plus a dependency; aeon/ stays self-contained
// (AGENTS.md §2) and imports only strings.
//
// The failure this file exists to prevent is the SILENT OVER-PERMIT: a matcher
// that assumes a trailing '*' (or a bare command name) matches a longer
// command hands an agent a permission no operator ever authored, passes every
// test except one, and that one test is the one nobody writes. So the
// exact-match arm is the first named test in pattern_test.go, and every
// pattern in the corpus carries a must-match and a must-not-match probe there.

import "strings"

// MatchPattern reports whether command satisfies pattern under the command
// policy's grammar, which is deliberately smaller than any glob language:
//
//   - '*' matches any run of characters, anywhere, including mid-pattern.
//   - A pattern containing no '*' matches ONLY the identical string. This is
//     what the corpus forces (bare "gh" must not match "gh pr merge") and what
//     a prefix matcher gets silently wrong.
//   - '*' is the ONLY wildcard. '?', '[', ']', '{', '}', the backslash, and
//     every other character are literal bytes — so "rm -r?" matches only the
//     four-character command "rm -r?", never "rm -rx".
//   - The empty pattern matches NOTHING. A rule that matches everything is a
//     rule nobody wrote, and it must be inert toward deny, never universal.
//
// Pure: a function of its two arguments — no clock, no environment, no I/O.
// Total: every input answers, and nothing here can panic (the only index is
// guarded by the HasPrefix above it).
//
// The algorithm splits the pattern on '*' into literal segments and walks the
// command requiring them in order — first anchored at the start, last anchored
// at the end, each middle segment at its leftmost occurrence after the one
// before it. No regexp, no backtracking: the leftmost choice is never the wrong
// one, because adding characters to the FRONT of what is left can only make the
// remaining segments easier to find (the tail's only anchor is the suffix at
// the very end), so an earlier match of a segment can always substitute for a
// later one. referenceMatch in pattern_test.go is the independent, literal
// reading of that sentence; the fuzz test says the two agree.
func MatchPattern(pattern, command string) bool {
	if pattern == "" {
		// Not "matches everything", and not "matches the empty command".
		return false
	}
	if !strings.Contains(pattern, "*") {
		return pattern == command // THE exact-match trap
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(command, parts[0]) {
		return false
	}
	rest := command[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		// Index of "" is 0, so an empty (adjacent-star) segment consumes
		// nothing and the scan stays panic-free on input ValidatePattern
		// would have refused.
		i := strings.Index(rest, mid)
		if i < 0 {
			return false
		}
		rest = rest[i+len(mid):]
	}
	last := parts[len(parts)-1]
	// The length guard states the invariant the walk depends on — the leftover
	// must still contain the WHOLE final segment, so the bytes that satisfied
	// parts[0] cannot be spent again. It is measured, not trusted: see
	// TestOverlappingSegmentsDoNotDoubleCount, which records that deleting this
	// clause alone does NOT change any answer (strings.HasSuffix applies the
	// same length test first), while anchoring the final segment on `command`
	// instead of on `rest` — the double-count this guard is the written form
	// of — fails that test on "a*a" vs "a". Kept as the boring reading of the
	// rule rather than deleted as dead, because the day the suffix test becomes
	// hand-rolled this is the line that keeps the claim true.
	return len(rest) >= len(last) && strings.HasSuffix(rest, last)
}

// ValidatePattern is the write-boundary half of the grammar: it refuses what
// MatchPattern would otherwise answer surprisingly, so a bad rule dies at
// authoring (aeon policy add, T5) rather than surfacing as a verdict. A
// *Reject (DB19) naming the value, for: an empty pattern; two adjacent '*' (no
// corpus rule uses one, and a run of wildcards is ambiguous where a guard must
// be boring); leading or trailing space — tab and newline included, being the
// same authoring accident as a stray space and no command begins with one.
//
// It refuses NO pattern the committed corpus carries:
// TestEveryCorpusPatternValidates is what keeps this boundary from being
// stricter than the specification it guards. Widening it is therefore a
// corpus-and-validator change, never a validator change alone.
//
// Pure, like MatchPattern. An empty *Reject means the pattern is writable.
func ValidatePattern(pattern string) *Reject {
	if pattern == "" {
		return rejectf("refused: command pattern %q is empty; an empty pattern matches nothing, "+
			"so the rule would be inert toward deny while reading like a permission — write "+
			"\"*\" if matching every command is what was meant", pattern)
	}
	if strings.Contains(pattern, "**") {
		return rejectf("refused: command pattern %q contains two adjacent '*'; no corpus rule uses "+
			"one, and a run of wildcards is ambiguous where a guard must be boring — use a single '*' "+
			"and the literal text around it", pattern)
	}
	if isSpaceByte(pattern[0]) {
		return rejectf("refused: command pattern %q begins with whitespace; commands are compared "+
			"byte for byte, so the leading space can only ever be an authoring slip", pattern)
	}
	if isSpaceByte(pattern[len(pattern)-1]) {
		return rejectf("refused: command pattern %q ends with whitespace; commands are compared byte "+
			"for byte, so the trailing space can only ever be an authoring slip", pattern)
	}
	return nil
}

// isSpaceByte is the whitespace the write boundary refuses, spelled as bytes
// rather than through unicode.IsSpace: pattern.go's import list is "strings"
// and nothing else (the no-dependency claim, AGENTS.md §2), and these four are
// what a shell line picks up from a copy-paste.
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
