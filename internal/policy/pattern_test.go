package policy_test

// The matcher's tests (T3). Two layers, deliberately separate:
//
//   - (a) The TRAP TABLE, hand-written, one named test per property the
//     grammar must have. These are the cases a reviewer looks for by name:
//     the exact-match trap (bare "gh" vs "gh pr merge"), mid-pattern '*',
//     literal '?' / '[' / ']', the empty pattern, the write-boundary refusals,
//     and the overlapping-segment trap that the linear scan's length guard
//     exists for.
//   - (b) The CORPUS ENUMERATION, where "the corpus is the specification"
//     becomes executable: every pattern DistinctPatterns() lists carries one
//     command it MUST match and one it MUST NOT, and a pattern with no entry
//     fails the build rather than being skipped.
//
// And a third thing, because the matcher's whole claim is "no backtracking
// needed": referenceMatch below is the grammar read literally — the first '*'
// branches over every split point — and the tests say the linear scan agrees
// with it. A matcher can be fast, or it can be obviously-naive; agreeing with
// the naive reading is what lets it be fast without being trusted.
//
// No test here reads the filesystem, a clock, or the environment: MatchPattern
// and ValidatePattern are pure, so every case below is a value in and a value
// out.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// namesValue is the DB19 check every refusal here is held to: the reason quotes
// the value it refused, so the operator reading `aeon policy add`'s output can
// see WHICH rule was refused. The comparison uses %q on both sides because
// Reject.Reason renders the value through %q, escapes and all.
func namesValue(rej *policy.Reject, pattern string) bool {
	return strings.Contains(rej.Reason, fmt.Sprintf("%q", pattern))
}

// ── (a) the trap table ──────────────────────────────────────────────────────

func TestExactMatchTrapBareGhDoesNotMatchSubcommand(t *testing.T) {
	// THE case this file exists for. A matcher that treats a bare command name
	// as a prefix — or assumes an invisible trailing '*' — passes every test
	// except this one and hands the agent "gh pr merge" from a rule that said
	// "gh". Both arms are asserted: identical strings match, and the same
	// pattern with any additional text does not.
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"bare name matches itself", "gh", "gh", true},
		{"bare name does not match a subcommand", "gh", "gh pr merge", false},
		{"bare name does not match a flag", "gh", "gh --version", false},
		{"exact with flags matches itself", "git branch --show-current", "git branch --show-current", true},
		{"exact with flags does not match one more word", "git branch --show-current", "git branch --show-current --verbose", false},
		{"exact does not match a longer word", "pwd", "pwdx", false},
		{"exact does not match a prefix of itself", "pwd", "pw", false},
		{"exact is byte-exact, not case-folded", "gh", "GH", false},
		{"exact is byte-exact, not space-collapsed", "pwd", "pwd ", false},
		{"trailing star is what a longer command needs", "gh *", "gh pr merge", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
		})
	}
}

func TestMidStarMatchesAcrossText(t *testing.T) {
	// '*' is not a trailing wildcard only. The corpus's own mid-star patterns
	// (docker buildx build*--push*, docker buildx bake*--push*) put one in the
	// middle, so a matcher that only handled a trailing '*' would fail on real
	// authored data.
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"corpus mid-star matches", "docker buildx build*--push*", "docker buildx build --platform x --push", true},
		{"corpus mid-star with no trailing flag", "docker buildx build*--push*", "docker buildx build --platform x --push --tag aeon", true},
		{"corpus mid-star without the flag at all", "docker buildx build*--push*", "docker buildx build --platform x", false},
		{"mid-star matches the flag after any run of text", "docker buildx build*--push*", "docker buildx build --push --platform x", true},
		{"mid-star without the flag anywhere", "docker buildx build*--push*", "docker buildx build -t aeon:latest", false},
		{"bake arm matches", "docker buildx bake*--push*", "docker buildx bake -f docker-bake.hcl --push", true},
		{"bake arm without the flag", "docker buildx bake*--push*", "docker buildx bake -f docker-bake.hcl", false},
		{"star in the middle of a word", "a*b", "aXXXb", true},
		{"star spanning nothing", "a*b", "ab", true},
		{"leading star", "*--push", "tofu apply --push", true},
		{"leading star refused", "*--push", "tofu apply --pushed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
		})
	}
}

func TestQuestionMarkIsLiteral(t *testing.T) {
	// path.Match answers Match("rm -r?", "rm -rx") == true. This grammar says
	// '?' is a byte like any other, so "rm -r?" names one four-character
	// command and nothing else — and the denial of the second form is the
	// whole point: an operator who typed '?' expecting a wildcard gets a rule
	// that matches nothing, not a rule that matches everything.
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"question mark does not match another character", "rm -r?", "rm -rx", false},
		{"question mark matches itself", "rm -r?", "rm -r?", true},
		{"question mark does not match nothing", "rm -r?", "rm -r", false},
		{"question mark mid-pattern is literal", "a?b", "aXb", false},
		{"question mark mid-pattern matches itself", "a?b", "a?b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
		})
	}
}

func TestBracketsAreLiteral(t *testing.T) {
	// The other half of why path.Match was rejected: it reads [abc] as a
	// character class. Here a bracket is a byte, so a rule containing one
	// matches the literal text and no other command.
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"bracket class does not match its members", "rm -r[abc]", "rm -ra", false},
		{"bracket class does not match any member", "rm -r[abc]", "rm -rb", false},
		{"bracket class matches the literal text", "rm -r[abc]", "rm -r[abc]", true},
		{"unterminated bracket is a byte", "rm -r[a", "rm -r[a", true},
		{"brace is a byte", "echo {a,b}", "echo {a,b}", true},
		{"brace does not expand", "echo {a,b}", "echo a", false},
		{"backslash is a byte", `sed s/\./x/`, `sed s/\./x/`, true},
		{"backslash cannot be skipped over", `a\*b`, "ab", false},
		{"backslash is required where it is written", `a\*b`, `a\XXb`, true},
		{"star still works next to literal bytes", "grep -n 'a[bc]d' *", "grep -n 'a[bc]d' src/main.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
		})
	}
}

func TestEmptyPatternMatchesNothing(t *testing.T) {
	// path.Match answers Match("", "") == true. The empty pattern is inert:
	// toward deny it denies nothing, and — the direction that matters — it
	// never permits anything either. A rule that matches everything is a rule
	// nobody wrote, so the only way to write one is the honest "*".
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"empty pattern does not match the empty command", "", "", false},
		{"empty pattern does not match a command", "", "ls", false},
		{"empty pattern does not match a long command", "", "git push origin main", false},
		{"a real pattern does not match the empty command", "ls*", "", false},
		{"star matches the empty command", "*", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
		})
	}
}

// matchAnchoringOnTheWholeCommand is the ONE-byte mutation this table exists
// to kill: it tests the final segment against the whole command instead of
// against what the first segment left over, so the same bytes satisfy two
// segments and "a*a" matches "a". It lives in the test file so the table's
// power to distinguish the two is ASSERTED (see the mutantDisagreements count
// below) rather than remembered from a mutation run nobody re-runs.
func matchAnchoringOnTheWholeCommand(pattern, command string) bool {
	if pattern == "" || !strings.Contains(pattern, "*") {
		return pattern != "" && pattern == command
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(command, parts[0]) {
		return false
	}
	rest := command[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, mid)
		if i < 0 {
			return false
		}
		rest = rest[i+len(mid):]
	}
	return strings.HasSuffix(command, parts[len(parts)-1]) // THE BUG: command, not rest
}

func TestOverlappingSegmentsDoNotDoubleCount(t *testing.T) {
	// The guard `len(rest) >= len(last)` in pattern.go, and the remainder it
	// applies to, proved two ways: against the right answers, and against the
	// mutant that spends the same bytes twice.
	cases := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"a*a does not match a", "a*a", "a", false},
		{"a*a matches aa", "a*a", "aa", true},
		{"a*a matches a long run", "a*a", "aXa", true},
		{"ab*ab does not match ab", "ab*ab", "ab", false},
		{"ab*ab matches abab", "ab*ab", "abab", true},
		{"shared suffix of a single segment cannot double-count", "aa*aa", "aa", false},
		{"a star-free pattern is compared, not scanned", "aa", "aa", true},
		{"git push* cannot match 'git ' plus a suffix", "git push*", "git ", false},
		{"*x*x cannot match x", "*x*x", "x", false},
	}
	mutantDisagreements := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MatchPattern(tc.pattern, tc.command); got != tc.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.command, got, tc.want)
			}
			if matchAnchoringOnTheWholeCommand(tc.pattern, tc.command) != tc.want {
				mutantDisagreements++
			}
		})
	}
	// Executable mutation proof: if no case separates the matcher from the
	// anchor-on-command mutant, this table proves nothing and the guard is
	// untested. (Recording the other mutation: deleting the length clause alone
	// changes nothing at all, measured — strings.HasSuffix performs the same
	// length test before comparing, which is why the anchor is the mutation
	// that matters and why the clause is kept as the written form of the rule.)
	if mutantDisagreements == 0 {
		t.Error("no case distinguishes MatchPattern from the anchor-on-command mutant; the table is too easy to prove anything")
	}
}

// ── the write boundary ──────────────────────────────────────────────────────

func TestDoubleStarIsRefusedAtAuthoring(t *testing.T) {
	// Two adjacent '*' match nothing extra and read ambiguously, so they die at
	// authoring. The refusal is a *Reject (DB19) that NAMES the value it
	// refused — "permission denied" is not actionable, this is.
	cases := []string{"git **", "**", "git**push", "rm -rf **", "a**b**c"}
	for _, pattern := range cases {
		t.Run(pattern, func(t *testing.T) {
			rej := policy.ValidatePattern(pattern)
			if rej == nil {
				t.Fatalf("ValidatePattern(%q) = nil; two adjacent '*' must be refused", pattern)
			}
			if !namesValue(rej, pattern) {
				t.Errorf("the refusal must name the value it refused; got %q", rej.Reason)
			}
		})
	}
}

func TestEmptyPatternIsRefusedAtAuthoring(t *testing.T) {
	rej := policy.ValidatePattern("")
	if rej == nil {
		t.Fatal("ValidatePattern(\"\") = nil; the empty pattern matches nothing and must be refused")
	}
	if !strings.Contains(rej.Reason, "*") {
		t.Errorf("the refusal should point at the honest way to write a universal rule; got %q", rej.Reason)
	}
}

func TestLeadingTrailingSpaceIsRefused(t *testing.T) {
	// Commands are compared byte for byte, so a stray leading or trailing space
	// is a rule that can never match — silently. It is refused where it is
	// typed, not discovered in a verdict.
	cases := []string{" ls", "ls ", " ls ", "\tls", "ls\n", "rm -rf ", " gh "}
	for _, pattern := range cases {
		t.Run(strings.ReplaceAll(pattern, " ", "<sp>"), func(t *testing.T) {
			rej := policy.ValidatePattern(pattern)
			if rej == nil {
				t.Fatalf("ValidatePattern(%q) = nil; surrounding whitespace must be refused", pattern)
			}
			if !namesValue(rej, pattern) {
				t.Errorf("the refusal must name the value it refused; got %q", rej.Reason)
			}
		})
	}
}

func TestWritablePatternsAreAccepted(t *testing.T) {
	// The boundary refuses what it names and nothing else: every shape the
	// corpus actually carries is writable, including the universal "*" (an
	// operator authored it, so it is a rule — the matcher's job is to honour
	// it, not to hide it) and every literal byte the grammar keeps literal.
	cases := []string{
		"*", "gh", "pwd", "git branch --show-current", "git push*", "rm *", "gh *",
		"docker buildx build*--push*", "rm -r?", "rm -r[abc]", `sed s/\./x/`,
		"echo {a,b}", "a*b*c", "*--push", "a?b", "git-commit",
	}
	for _, pattern := range cases {
		t.Run(pattern, func(t *testing.T) {
			if rej := policy.ValidatePattern(pattern); rej != nil {
				t.Errorf("ValidatePattern(%q) = %q; the corpus's shapes must stay writable", pattern, rej.Reason)
			}
		})
	}
}

// ── the matcher against the grammar read literally ──────────────────────────

// referenceMatch is the grammar said the naive way, for the tests to check the
// implementation against. It is not shipped code: it branches over every split
// point at the first '*' (exponential on adversarial input, which is why
// pattern.go does not do this) and it exists only so that "no backtracking is
// needed" is a measured claim rather than an argument.
//
// The empty-pattern rule applies at the top only: below a '*', an empty
// remaining pattern correctly means "the command must end here".
func referenceMatch(pattern, command string) bool {
	if pattern == "" {
		return false
	}
	var rec func(p, s string) bool
	rec = func(p, s string) bool {
		if !strings.Contains(p, "*") {
			return p == s
		}
		k := strings.Index(p, "*")
		prefix := p[:k]
		if !strings.HasPrefix(s, prefix) {
			return false
		}
		tail := p[k+1:]
		for i := len(prefix); i <= len(s); i++ {
			if rec(tail, s[i:]) {
				return true
			}
		}
		return false
	}
	return rec(pattern, command)
}

func TestMatchPatternAgreesWithTheNaiveReading(t *testing.T) {
	cases := []struct{ pattern, command string }{
		{"", ""}, {"", "ls"}, {"*", ""}, {"*", "anything"},
		{"gh", "gh"}, {"gh", "gh pr merge"}, {"pwd", "pwd"},
		{"a*a", "a"}, {"a*a", "aa"}, {"a*a", "aXa"}, {"ab*ab", "ab"}, {"ab*ab", "abab"},
		{"a**b", "ab"}, {"a**b", "aXb"}, {"**", ""}, {"**", "x"},
		{"a*b*c", "abc"}, {"a*b*c", "axxbyyc"}, {"a*b*c", "ab"},
		{"*a*", ""}, {"*a*", "a"}, {"*a*", "ba"}, {"*a*b*", "ab"}, {"*a*b*", "aXb"},
		{"a?b", "a?b"}, {"a?b", "axb"}, {"rm -r[abc]", "rm -ra"}, {"rm -r[abc]", "rm -r[abc]"},
		{`\*`, "*"}, {`\*`, `\*`}, {"a\\*b", "a\\b"},
		{"docker buildx build*--push*", "docker buildx build --platform x --push"},
		{"docker buildx build*--push*", "docker buildx build --platform x"},
		{"git push*", "git stash push -m wip"},
		{"a*a*a", "aaa"}, {"a*a*a", "aa"}, {"a*a*a", "axaxa"},
		{"aa*aa", "aa"}, {"aa*aa", "aaaa"},
		{"*x*x*", "x"}, {"*x*x*", "xx"}, {"x*x*x", "xxx"},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"|"+tc.command, func(t *testing.T) {
			want := referenceMatch(tc.pattern, tc.command)
			if got := policy.MatchPattern(tc.pattern, tc.command); got != want {
				t.Errorf("MatchPattern(%q, %q) = %v but the grammar read literally says %v",
					tc.pattern, tc.command, got, want)
			}
		})
	}
}

func TestMatchPatternIsTotalAndDetermined(t *testing.T) {
	// Total: nothing panics, whatever the bytes. Determined: the same inputs
	// give the same answer (the purity claim in types.go, checked for the one
	// function that decides what an agent may run).
	adversarial := []string{
		"", "*", "**", "***", "\x00", "\xff\xfe", "a\x00b", strings.Repeat("*", 500),
		strings.Repeat("a", 500), strings.Repeat("*a", 250), "rm -rf /", " ?[]{}\\ ",
		"\n\t\r", "docker buildx build*--push*", "a*b*c*d*e*f",
	}
	for _, pattern := range adversarial {
		for _, command := range append([]string{"", "ls"}, adversarial...) {
			p, c := pattern, command
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("MatchPattern(%q, %q) panicked: %v", p, c, r)
					}
				}()
				first := policy.MatchPattern(p, c)
				for range 3 {
					if again := policy.MatchPattern(p, c); again != first {
						t.Fatalf("MatchPattern(%q, %q) is not determined: %v then %v", p, c, first, again)
					}
				}
			}()
		}
	}
}

// ── (b) the corpus enumeration ──────────────────────────────────────────────
//
// The probe table this enumeration walks (corpusProbe, matchProbe,
// universalProbes) lives in corpus_probe_test.go: it is shared with
// evaluate_commandaccess_test.go, which drives the SAME commands through
// Evaluate as stored rows. Promoted, never copied — one table, two views, no
// second truth that can disagree with the first.

func TestEveryCorpusPatternHasAProbe(t *testing.T) {
	for _, p := range mustCorpus(t).DistinctPatterns() {
		probe, ok := corpusProbe[p]
		if !ok {
			t.Errorf("no probe case for corpus pattern %q — the corpus is the specification, so every pattern needs a must-match and a must-not-match command", p)
			continue
		}
		if !policy.MatchPattern(p, probe.mustMatch) {
			t.Errorf("%q must match %q", p, probe.mustMatch)
		}
		if probe.noNegative != "" {
			if probe.mustNotMatch != "" {
				t.Errorf("%q declares noNegative (%s) and still carries mustNotMatch %q",
					p, probe.noNegative, probe.mustNotMatch)
			}
			if p != "*" {
				t.Errorf("%q claims no negative probe exists (%s); only the bare \"*\" can say that — name a command it must not match",
					p, probe.noNegative)
			}
			for _, cmd := range universalProbes {
				if !policy.MatchPattern(p, cmd) {
					t.Errorf("%q says no negative probe exists (%s) but does not match %q",
						p, probe.noNegative, cmd)
				}
			}
			continue
		}
		if policy.MatchPattern(p, probe.mustNotMatch) {
			t.Errorf("%q must NOT match %q", p, probe.mustNotMatch)
		}
		// The probe pair must not be the same command wearing two hats — a
		// table entry that cannot fail is worse than no entry.
		if probe.mustMatch == probe.mustNotMatch {
			t.Errorf("%q's must-match and must-not-match commands are identical (%q); the probe proves nothing",
				p, probe.mustMatch)
		}
	}
}

func TestEveryCorpusPatternValidates(t *testing.T) {
	// The write boundary must not be stricter than the specification it
	// guards: every pattern an operator has actually authored in the committed
	// corpus has to pass ValidatePattern, or `aeon policy add` would refuse to
	// write the very rules the corpus says are in force.
	for _, p := range mustCorpus(t).DistinctPatterns() {
		if rej := policy.ValidatePattern(p); rej != nil {
			t.Errorf("the committed corpus carries %q, but ValidatePattern refuses it: %s", p, rej.Reason)
		}
	}
}

func TestCorpusStarFreePatternsMatchOnlyThemselves(t *testing.T) {
	// The exact-match trap, run over the corpus rather than over fixtures:
	// every star-free pattern matches itself, and matches nothing built around
	// it. This is the arm that fails if someone reintroduces a prefix or a
	// trailing-star assumption, even on patterns nobody hand-wrote a case for.
	n := 0
	for _, p := range mustCorpus(t).DistinctPatterns() {
		if strings.Contains(p, "*") {
			continue
		}
		n++
		if !policy.MatchPattern(p, p) {
			t.Errorf("%q must match itself", p)
		}
		for _, extended := range []string{p + " ", p + " extra", p + " --verbose", "sudo " + p, p[:len(p)-1]} {
			if policy.MatchPattern(p, extended) {
				t.Errorf("star-free %q must NOT match %q — a pattern with no '*' is compared, not prefixed", p, extended)
			}
		}
	}
	if n == 0 {
		t.Error("the corpus carries no star-free pattern, so this test asserts nothing")
	}
}

func TestCorpusStarPatternsMatchTheirOwnText(t *testing.T) {
	// The mirror arm: with the '*'s removed, a corpus pattern is a prefix-ish
	// statement about a command, and the pattern must match the literal text it
	// is made of (star removed and kept). Fails if the segment walk ever loses
	// a segment or anchors the wrong one.
	for _, p := range mustCorpus(t).DistinctPatterns() {
		if !strings.Contains(p, "*") {
			continue
		}
		if !policy.MatchPattern(p, p) {
			t.Errorf("%q must match its own text", p)
		}
		literal := strings.ReplaceAll(p, "*", "")
		if !policy.MatchPattern(p, literal) {
			t.Errorf("%q must match %q — the pattern with its wildcards emptied", p, literal)
		}
		if !referenceMatch(p, literal) {
			t.Errorf("the naive reading also refuses %q against %q; the probe data itself is the contradiction", p, literal)
		}
	}
}

// ── fuzz ────────────────────────────────────────────────────────────────────

// FuzzMatchPattern's promise is small and absolute: no input panics, the
// answer is a function of the inputs, and for inputs short enough for the
// exponential reference the answer is the one the grammar says. Seeded from
// every corpus pattern so the shape that matters is always in the seed set.
func FuzzMatchPattern(f *testing.F) {
	genericCommands := []string{
		"", " ", "ls", "gh", "gh pr merge", "git push origin main",
		"docker buildx build --platform x --push", "rm -rf .", "rm -r?",
		"rm -r[abc]", "\x00", "a", "aa", "a*a", "*--push",
	}
	for _, p := range mustCorpus(f).DistinctPatterns() {
		f.Add(p, p)
		for _, c := range genericCommands {
			f.Add(p, c)
		}
	}
	for _, p := range []string{"", "*", "**", "***", "a*a", "a**b", "*a*b*", "\\*", "?", "[]"} {
		for _, c := range genericCommands {
			f.Add(p, c)
		}
	}
	f.Fuzz(func(t *testing.T, pattern, command string) {
		first := policy.MatchPattern(pattern, command)
		if again := policy.MatchPattern(pattern, command); again != first {
			t.Fatalf("MatchPattern(%q, %q) is not determined: %v then %v", pattern, command, first, again)
		}
		// The reference branches over every split point, so it is bounded here
		// to inputs where that stays cheap.
		if len(pattern)+len(command) <= 48 {
			if want := referenceMatch(pattern, command); first != want {
				t.Fatalf("MatchPattern(%q, %q) = %v but the grammar read literally says %v",
					pattern, command, first, want)
			}
		}
	})
}
