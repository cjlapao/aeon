package desk

// warn.go — the authoring-time operator warnings, ONE definition (S45 T4).
//
// These sentences lived in cmd/aeon/policy.go while the CLI was the only
// authoring door. S45 T4 opens the socket door (PM1), and the warnings are
// part of what authoring SAYS — a second copy in the daemon would be the
// second formatter D9 forbids, and a drift between the doors' warnings is
// exactly the kind of "plausible wrong answer" this slice exists to make
// impossible. So the builders move HERE (the desk is the one implementation
// of policy authoring, PM2), and `cmd/aeon` keeps its names as one-line
// forwarders (T1's rule: a wrapper where the pinned tests and the verb's
// flow name the old spelling; the goldens print bytes from here, unchanged).
//
// Both signals are WARN, never refuse: the boundary's judgement stands (it
// refuses only what PL1 refuses); these lines are the operator being told
// out loud what they just stored, then the row standing anyway.

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/policy"
)

// UnmatchableTargetWarn is DEF-038's operator-side signal at authoring:
// target kinds no runtime decision carries until S18/S20 are stored inert.
// The kind is echoed only when it is exactly one of the three named kinds,
// and named as such, so nothing hostile rides it.
func UnmatchableTargetWarn(kind string) string {
	return "warning  no runtime decision carries a " + kind + " identity until S18/S20 — " +
		"the rule is stored but cannot match yet (deny nothing, permit nothing, fail-closed)"
}

// BroadAllowWarn is SEC-014's operator-side signal, the carry-forward T5 left
// at the CLI: an ALLOW whose command pattern covers a wide slice of the
// measured corpus gets spoken about, out loud, on stderr — and is then stored.
// The boundary's judgement stands (it refuses only what PL1 refuses); this is
// the "warn, never refuse" ruling made executable. A wildcard allow that
// swallows the corpus is a finding an auditor asks about after the fact; the
// warning is the cheapest moment to still mean something.
//
// The measurement is corpus-relative on purpose (T5's deviation): broadness is
// "covers 17 of the 51 distinct patterns the real agents actually run", not a
// hardcoded list of scary strings someone curated and nobody re-measured.
// BroadAllowWarnFraction is the line — a quarter of the distinct patterns.
// "*" covers 51/51 and warns; "git*" covers 17/51 and warns; "git status"
// covers 1/51 and is quiet. A pattern that matches nothing in the corpus is
// inert by definition and says nothing about breadth, so it is quiet too.
//
// BroadAllowWarnFraction is not a constant (AGENTS §4d): the name exists only
// in this comment; the rule is the inline `matched*4 < total` in
// BroadAllowWarning.
func BroadAllowWarn(pattern string, matched, total int) string {
	if total == 0 {
		return fmt.Sprintf("warning  allow pattern %q is broad on its face and the measured corpus "+
			"cannot size it (SEC-014) — stored, not refused; remove it if it was not meant this wide", pattern)
	}
	return fmt.Sprintf("warning  allow pattern %q matches %d of %d distinct command patterns in the "+
		"measured corpus (SEC-014) — stored, not refused: the boundary will not narrow it for you, "+
		"remove it if it was not meant this wide", pattern, matched, total)
}

// BroadAllowWarning decides whether the row being authored earns SEC-014's
// stderr line. It runs on what is actually about to be stored — the merged
// spec's "command" field, so --spec written directly is measured exactly like
// --command sugar — and only for action=allow: a deny or an ask that covers
// the corpus is exactly what the engine wants. The action arrives UNVALIDATED
// (the boundary owns validation); a value that is not exactly `allow` simply
// never warns here, and the boundary makes the eventual judgement.
func BroadAllowWarning(action policy.Action, specJSON []byte) (string, bool) {
	if action != policy.Allow {
		return "", false
	}
	pattern := CommandPatternFromSpec(specJSON)
	if pattern == "" {
		return "", false
	}
	matched, total, measured := CorpusCoverage(pattern)
	if !measured {
		// The embedded corpus failed to parse — impossible for shipped bytes
		// (internal/policy pins them) but not for this function's contract. A
		// measurement you cannot make is a percentage you must not invent; the
		// one pattern broad by construction still speaks, without a number.
		return BroadAllowWarn(pattern, 0, 0), pattern == "*"
	}
	if total == 0 || matched*4 < total {
		return "", false
	}
	return BroadAllowWarn(pattern, matched, total), true
}

// CommandPatternFromSpec digs the "command" string out of the merged spec
// bytes. Anything that is not a spec object, has no "command", or carries a
// non-string command yields "" — this is a WARNING probe reading presentation
// bytes, not a validator; the boundary's CommandAccess validator makes the
// real judgement on the same bytes moments later.
func CommandPatternFromSpec(specJSON []byte) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(specJSON, &fields); err != nil {
		return ""
	}
	raw, ok := fields["command"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// CorpusCoverage measures the candidate pattern against every distinct pattern
// the embedded corpus uses, through pattern.MatchPattern — the matcher's own
// semantics, not a second wildcard implementation that could drift from the
// engine. measured is false only when the corpus bytes do not parse.
func CorpusCoverage(pattern string) (matched, total int, measured bool) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		return 0, 0, false
	}
	patterns := corpus.DistinctPatterns()
	for _, p := range patterns {
		if policy.MatchPattern(pattern, p) {
			matched++
		}
	}
	return matched, len(patterns), true
}
