package policy

// The segment splitter: one command line cut into the parts a refusal can
// name.
//
// A step is refused a command and the refusal must be able to say WHICH part
// was refused: `cd sub && git push` is denied on its second segment, and an
// operator reading `aeon policy why` should see that rather than a flat
// "denied". This file owns the split on the four separators the guard knows —
// the two-character "&&" and "||", then ";", then a bare newline — and
// nothing else. T5's evaluator owns the "any denied segment denies the whole"
// fold (brief D10); T12's question body carries the segment into the
// human-facing ask (G18/QAP-12). What travels onward is the segment INDEX,
// never its text, because MatchRes.Note is a log line that must never carry
// policy content (see the docblock in evaluate.go) — which is exactly why a
// refusal names an index.
//
// What this is NOT, said plainly because this is the slice where someone
// expects a sandbox (design: "What the matcher does not do, said plainly"):
// it is not a shell grammar. It does not understand quoting, dollar-parenthesis
// substitution, backticks, heredocs or comments, so `bash -c "git push"` and
// `git $(echo push)` are NOT caught — expected behaviour (brief D11), asserted
// by TestDocumentedLimitsAreNotTreatedAsBugs so nobody later files it as a
// defect and nobody mistakes the guard for containment. The tool allow-list is
// the real boundary; the corpus's denials target accidents, not adversaries.
//
// Two safety directions do hold, and both point the same way: a quoted
// "a && b" yields a spurious EXTRA segment, and a spurious segment can only
// ever lose a match, never invent a permission; and a command with more
// segments than MaxSegments is not analysed at all, which resolves toward
// DENY because the caller then matches nothing. The over-limit reads are
// never silent — Segments states Count and OverLimit rather than truncating
// quietly.
//
// Pure and total like the matcher it feeds: a function of its input — no
// clock, no environment, no I/O — and every input answers. TestSegmentFileImportsOnlyStrings
// pins "strings" as this file's only import, so the purity claim is enforced,
// not promised.

import "strings"

// separatorTokens are the four separators the guard knows, in the canonical
// order the Separators field lists them. Scanning checks the two-character
// tokens before the one-character ones (see separatorAt); single "&" and
// single "|" are NOT separators — they are literal bytes, so "echo a & b | c"
// stays one segment and the pipe question is answered by the tool allow-list,
// not here.
var separatorTokens = []string{"&&", "||", ";", "\n"}

// MaxSegments bounds how many segments one command line is cut into. It is a
// DOCUMENTED EXPECTATION, not a bug to fix (brief D11): a command with more
// segments than this is not analysed, and "not analysed" resolves toward DENY
// — the caller matches nothing, so nothing is permitted. Stated here, in the
// policy docs, and in the bash tool description (T13), rather than discovered
// by someone reading this function at 2am.
const MaxSegments = 32

// Segments is one command line cut on the four separators the guard knows.
type Segments struct {
	Items      []string // non-empty, trimmed, in order; len(Items) <= MaxSegments
	Count      int      // how many segments the command REALLY has
	OverLimit  bool     // Count > MaxSegments => Items is not a complete reading
	Separators []string // which of "&&", "||", ";" and newline were present — for refusal prose
}

// Split cuts one command on "&&", "||", ";" and a bare newline.
//
// It is deliberately NOT a shell parser. It does not understand quotes, so a
// quoted "a && b" yields two segments — the safe direction, because a spurious
// extra segment can only ever lose a match, never invent a permission. It does
// not understand command substitution, backticks, heredocs or comments, so a
// bash -c wrapper is one segment matched as written. Both facts are asserted,
// not apologised for.
//
// Empty segments are dropped and each item is trimmed of surrounding spaces,
// so a trailing ";" cannot create an empty segment (belt and braces: the empty
// pattern already matches nothing per the matcher's own rule). Count counts
// ALL segments before the limit; when Count > MaxSegments, OverLimit is set
// and Items is truncated to MaxSegments so a pathological line costs bounded
// work — and the truncation is NEVER silent. A command with no non-empty
// segment (the empty string, ";", " && ") yields zero items and Count 0;
// matching nothing against it permits nothing, the safe answer for a command
// that asked nothing.
func Split(command string) Segments {
	var items []string
	seen := make([]bool, len(separatorTokens))

	// add trims one raw piece and keeps it only if non-empty. Trimming runs
	// before the empty test, so "ls && " is one segment, and a CRLF newline
	// leaves no orphan "\r" behind (the one-character split is on "\n"; the
	// carriage return is trimmable whitespace, same as pattern.go's write
	// boundary treats it).
	add := func(piece string) {
		if s := strings.TrimSpace(piece); s != "" {
			items = append(items, s)
		}
	}

	start := 0
	for i := 0; i < len(command); {
		tok, width, ok := separatorAt(command, i)
		if !ok {
			i++
			continue
		}
		add(command[start:i])
		for j, t := range separatorTokens {
			if t == tok {
				seen[j] = true
			}
		}
		i += width
		start = i
	}
	add(command[start:])

	out := Segments{Count: len(items)}
	for j, tok := range separatorTokens {
		if seen[j] {
			out.Separators = append(out.Separators, tok)
		}
	}
	if out.Count > MaxSegments {
		// Stated, not silent: Count keeps the real number and OverLimit says
		// Items is not a complete reading. The caller's fold must treat an
		// incomplete reading as no permission (over-limit refuses toward
		// deny); truncating here only bounds the work, never the answer's
		// honesty.
		out.OverLimit = true
		out.Items = append([]string(nil), items[:MaxSegments]...)
		return out
	}
	out.Items = items
	return out
}

// separatorAt reports whether a separator token begins at s[i], longest
// first: "&&" and "||" are tested before ";" and "\n" so a two-character
// token is consumed as the one separator it is. Width is the token's byte
// length. Total: every (s, i) answers, and the two-character read is guarded
// by the length test above it — nothing here can index out of range.
func separatorAt(s string, i int) (tok string, width int, ok bool) {
	if i+2 <= len(s) {
		switch s[i : i+2] {
		case "&&":
			return "&&", 2, true
		case "||":
			return "||", 2, true
		}
	}
	switch s[i] {
	case ';':
		return ";", 1, true
	case '\n':
		return "\n", 1, true
	}
	return "", 0, false
}
