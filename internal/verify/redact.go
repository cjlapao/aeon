package verify

// redact.go — the best-effort tail redaction (S57 VG6).

import (
	"regexp"
	"strings"
)

// Redact replaces token-shaped strings with their masked forms. It is
// best-effort and not a guarantee: it catches the token shapes that exist
// (below), and it cannot catch a secret that has no shape — a failing test
// that prints a bare 40-hex key with no prefix, no key name and no Bearer
// line passes through unchanged, and that is the honest limit every view
// says. Tails therefore never reach bug bodies, audit summaries, events or
// agent-visible lists; only verify.show returns them.
//
// The masks keep the SHAPE (the prefix, the key name) so a reader still
// sees what kind of token it is, and replace the body with ***: a
// redaction that erases the shape is a redaction that hides its own work.
func Redact(s string) string {
	for _, rule := range redactRules {
		s = rule.re.ReplaceAllStringFunc(s, rule.mask)
	}
	return s
}

// redactRule is one shape and its mask. A fixed mask is enough for the
// prefix-shaped tokens; a function keeps the part of the match that names
// the shape (the key name, the Bearer word) and masks only the value.
type redactRule struct {
	re   *regexp.Regexp
	mask func(string) string
}

var (
	reGitHubPAT  = regexp.MustCompile(`ghp_[A-Za-z0-9]{16,}`)
	reGitHubFine = regexp.MustCompile(`github_pat_[A-Za-z0-9_]{16,}`)
	reSK         = regexp.MustCompile(`sk-[A-Za-z0-9]{16,}`)
	reAWSKeyID   = regexp.MustCompile(`AKIA[0-9A-Z]{12,}`)
	reSlack      = regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)
	reBearer     = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{16,}`)
	// The key name may be a bare word (secret:, password=) or carry an
	// underscore-joined prefix (api_key=, github_token=): an optional run of
	// `alnum_` segments before the credential word. A bare "monkey=" has no
	// boundary at its "key", so it is not a credential and is left alone.
	reKeyValue = regexp.MustCompile(`(?i)\b(?:[a-z0-9]+_)*(key|token|secret|password)\s*[=:]\s*[A-Za-z0-9+/=_-]{24,}`)
)

// CommandIsTokenShaped reports whether s carries one of the credential
// token shapes the redactor knows (the same closed set, one definition).
// S57 T10 (VG20): the daemon stamps the scrubbed-environment note onto a
// gate write whose command looks credential-shaped — the note's trigger
// and the tail's masker read the SAME rules, so a command that would be
// masked on the way out is warned about on the way in. A predicate,
// never a refusal: a shaped command is still a legal command, and the
// person decides whether the token was meant to be there.
func CommandIsTokenShaped(s string) bool {
	for _, r := range redactRules {
		if r.re.MatchString(s) {
			return true
		}
	}
	return false
}

// redactRules is the closed set of shapes, applied in order. A new shape is
// a new row here and a new test — the set is deliberately small: every row
// is a token shape that exists in the toolchains this project runs, and a
// shape nobody's token matches is a row that rots.
var redactRules = []redactRule{
	{reGitHubPAT, mask("ghp_***")},
	{reGitHubFine, mask("github_pat_***")},
	{reSK, mask("sk-***")},
	{reAWSKeyID, mask("AKIA***")},
	// Keeps the token class (xoxb-, xoxp-, …): four characters name it.
	{reSlack, func(m string) string { return m[:4] + "***" }},
	// Keeps the word, in its case.
	{reBearer, func(m string) string {
		i := strings.IndexAny(m, " \t")
		return m[:i] + " ***"
	}},
	// Keeps the key name and its separator; masks only the value.
	{reKeyValue, func(m string) string {
		if i := strings.IndexAny(m, "=:"); i >= 0 {
			return m[:i+1] + "***"
		}
		return m[:4] + "***"
	}},
}

// mask is a fixed replacement.
func mask(s string) func(string) string {
	return func(string) string { return s }
}
