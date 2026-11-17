package policy_test

// The shared corpus probe table: one command each corpus pattern MUST match
// and one it MUST NOT, in one file, read by both corpus-driven suites.
//
// This table was born in pattern_test.go (T3) and is PROMOTED here, not copied
// (T3's scratch handoff: "a copied table is a second truth that will disagree").
// Two readers now hold it to the same answers:
//
//   - pattern_test.go's TestEveryCorpusPatternHasAProbe — the MATCHER's view:
//     each pattern against its probes through MatchPattern;
//   - evaluate_commandaccess_test.go's TestEveryCorpusRuleEvaluates — the
//     ENGINE's view: each corpus rule, as a stored row through Evaluate,
//     against the same two commands.
//
// One table means the two views cannot drift: a probe that would make the
// matcher and the engine disagree about one rule is a single edit that fails
// both suites, which is exactly the property the corpus-as-specification claim
// (brief D4) needs. Adding an agent whose pattern is not in the table breaks
// the build until somebody says what the new pattern means — no test here
// asserts how many patterns or rules exist (T2's rule: the corpus measures real
// agent files, and a count assertion fails the day an agent is added).

import (
	"strings"
	"testing"
)

// corpusProbe is the corpus turned into tests: pattern -> one command that
// MUST match and one that MUST NOT. TestEveryCorpusPatternHasAProbe walks
// DistinctPatterns() and fails on any pattern without an entry, so the table
// cannot fall behind the corpus — an added agent's new pattern breaks the
// build until somebody decides what it means.
//
// The negatives are chosen as NEAR MISSES, not as unrelated strings: "rm -fr*"
// is refused "rm -rf ..." (same letters, wrong order), "git push --force*" is
// refused plain "git push origin main", "gh *" is refused bare "gh". A table of
// easy negatives would pass a prefix matcher.
var corpusProbe = map[string]matchProbe{
	"*":                            {mustMatch: "whatever the agent typed", noNegative: "the bare '*' matches every command, so no negative exists; the test asserts the universality instead"},
	"basename*":                    {mustMatch: "basename /usr/local/go/bin/go", mustNotMatch: "/usr/local/go/bin/go"},
	"cat*":                         {mustMatch: "cat internal/policy/pattern.go", mustNotMatch: "concatenate the two files"},
	"date*":                        {mustMatch: "date -u +%Y-%m-%d", mustNotMatch: "sudo date -u"},
	"dirname*":                     {mustMatch: "dirname internal/policy/pattern.go", mustNotMatch: "basename internal/policy/pattern.go"},
	"docker buildx bake*--push*":   {mustMatch: "docker buildx bake -f docker-bake.hcl --push", mustNotMatch: "docker buildx bake -f docker-bake.hcl"},
	"docker buildx build*--push*":  {mustMatch: "docker buildx build --platform x --push", mustNotMatch: "docker buildx build --platform x"},
	"docker push*":                 {mustMatch: "docker push ghcr.io/cjlapao/aeon:latest", mustNotMatch: "docker pull ghcr.io/cjlapao/aeon:latest"},
	"find*":                        {mustMatch: "find . -name '*.go'", mustNotMatch: "grep -rn find internal/policy"},
	"gh":                           {mustMatch: "gh", mustNotMatch: "gh pr merge 125 --squash"},
	"gh *":                         {mustMatch: "gh pr view 126", mustNotMatch: "gh"},
	"gh pr close*":                 {mustMatch: "gh pr close 126 --comment done", mustNotMatch: "gh pr reopen 126"},
	"gh pr create*":                {mustMatch: "gh pr create --title t --body b", mustNotMatch: "gh pr close 126"},
	"gh pr merge*":                 {mustMatch: "gh pr merge 125 --squash", mustNotMatch: "gh pr checkout 125"},
	"git branch --show-current":    {mustMatch: "git branch --show-current", mustNotMatch: "git branch --show-current --verbose"},
	"git branch -D*":               {mustMatch: "git branch -D stale-branch", mustNotMatch: "git branch -d stale-branch"},
	"git checkout*":                {mustMatch: "git checkout main", mustNotMatch: "git cherry-pick abc1234"},
	"git clean*":                   {mustMatch: "git clean -fdx", mustNotMatch: "git status clean"},
	"git commit*":                  {mustMatch: "git commit -m 'feat: matcher'", mustNotMatch: "git comit -am x"},
	"git log*":                     {mustMatch: "git log --oneline -20", mustNotMatch: "git --no-pager log --oneline"},
	"git ls-files*":                {mustMatch: "git ls-files internal/policy", mustNotMatch: "git ls files internal/policy"},
	"git merge*":                   {mustMatch: "git merge --no-ff feature/x", mustNotMatch: "git pull --no-ff origin main"},
	"git push --force*":            {mustMatch: "git push --force origin main", mustNotMatch: "git push origin main"},
	"git push --force-with-lease*": {mustMatch: "git push --force-with-lease origin main", mustNotMatch: "git push --force origin main"},
	"git push*":                    {mustMatch: "git push origin main", mustNotMatch: "git stash push -m wip"},
	"git rebase*":                  {mustMatch: "git rebase main", mustNotMatch: "git reset main"},
	"git remote get-url*":          {mustMatch: "git remote get-url origin", mustNotMatch: "git remote set-url origin https://example.invalid"},
	"git reset --hard*":            {mustMatch: "git reset --hard HEAD~1", mustNotMatch: "git reset HEAD~1"},
	"git reset*":                   {mustMatch: "git reset HEAD~1", mustNotMatch: "git restore ."},
	"git rev-parse*":               {mustMatch: "git rev-parse HEAD", mustNotMatch: "git revert HEAD"},
	"git status*":                  {mustMatch: "git status --porcelain", mustNotMatch: "git stat"},
	"grep*":                        {mustMatch: "grep -rn MatchPattern internal/policy", mustNotMatch: "egrep -rn MatchPattern internal/policy"},
	"head*":                        {mustMatch: "head -20 internal/policy/pattern.go", mustNotMatch: "tail -20 internal/policy/pattern.go"},
	"ls*":                          {mustMatch: "ls -la internal/policy", mustNotMatch: "/bin/ls -la internal/policy"},
	"pwd":                          {mustMatch: "pwd", mustNotMatch: "pwd -P"},
	"readlink*":                    {mustMatch: "readlink -f .dev/aeon.sock", mustNotMatch: "ln -s x y"},
	"realpath*":                    {mustMatch: "realpath .tmp/cgo-shim", mustNotMatch: "relpath .tmp/cgo-shim"},
	"rg*":                          {mustMatch: "rg -n MatchPattern internal/policy", mustNotMatch: "grep -rn MatchPattern internal/policy"},
	"rm *":                         {mustMatch: "rm -r .tmp/build", mustNotMatch: "rm"},
	"rm --recursive*":              {mustMatch: "rm --recursive .tmp/build", mustNotMatch: "rm -r .tmp/build"},
	"rm -fr*":                      {mustMatch: "rm -fr node_modules", mustNotMatch: "rm -rf node_modules"},
	"rm -r *":                      {mustMatch: "rm -r .tmp/build", mustNotMatch: "rm -rf .tmp/build"},
	"rm -rf*":                      {mustMatch: "rm -rf dist", mustNotMatch: "rm -fr dist"},
	"sed -n*":                      {mustMatch: "sed -n '1,20p' internal/policy/pattern.go", mustNotMatch: "sed -i 's/a/b/' internal/policy/pattern.go"},
	"stat*":                        {mustMatch: "stat -c %s internal/policy/pattern.go", mustNotMatch: "wc -c internal/policy/pattern.go"},
	"tail*":                        {mustMatch: "tail -40 events.jsonl", mustNotMatch: "head -40 events.jsonl"},
	"terraform apply*":             {mustMatch: "terraform apply -auto-approve", mustNotMatch: "terraform plan"},
	"terraform destroy*":           {mustMatch: "terraform destroy -auto-approve", mustNotMatch: "terraform apply -auto-approve"},
	"tofu apply*":                  {mustMatch: "tofu apply -auto-approve", mustNotMatch: "terraform apply -auto-approve"},
	"tofu destroy*":                {mustMatch: "tofu destroy -auto-approve", mustNotMatch: "tofu apply -auto-approve"},
	"wc*":                          {mustMatch: "wc -l internal/policy/pattern.go", mustNotMatch: "echo wc -l internal/policy/pattern.go"},
}

// matchProbe is one pattern's two probes. noNegative carries the REASON a
// pattern has no must-not-match command; it is only ever honest for the bare
// "*". Any other pattern has one, and saying so here is what stops a missing
// negative becoming a silent gap.
type matchProbe struct {
	mustMatch    string
	mustNotMatch string
	noNegative   string
}

// universalProbes is what the bare '*' is held to: it must match all of these,
// which is why no negative for it exists. Including the empty command and a
// whitespace-only one, both of which a different matcher might special-case.
var universalProbes = []string{
	"", " ", "ls", "git push origin main", "rm -rf /", "docker buildx build --push",
	"\x00\xff", "é", strings.Repeat("x", 300),
}

// mustProbe is the fail-loud arm of the table for a test that walks RULES
// rather than patterns: a pattern with no entry is not skipped, it stops the
// subtest, because a silently-skipped rule is the exact way the
// "every rule evaluates" claim becomes fiction.
func mustProbe(tb testing.TB, pattern string) matchProbe {
	tb.Helper()
	probe, ok := corpusProbe[pattern]
	if !ok {
		tb.Fatalf("no probe case for corpus pattern %q — the corpus is the specification, so every "+
			"pattern needs a must-match and a must-not-match command (add one here)", pattern)
	}
	return probe
}
