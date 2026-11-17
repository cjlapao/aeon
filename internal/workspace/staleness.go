package workspace

import (
	"context"
	"strconv"
	"strings"
)

// Staleness states for StalenessResult.State. The three-state discipline of
// every read in this package (LW3): a count is only ever answered together
// with OK; unavailable names WHY in Reason; not-a-repo is the repository
// fact, never an error — the merge queue's B6 rule is "behind unknown holds
// the lane, never fakes a zero", and a faked zero is exactly what an
// error-return habit invites the caller to render.
const (
	// StaleOK: both endpoints resolved and the counts are true.
	StaleOK = "ok"
	// StaleUnavailable: git ran (or could not reach origin) and the counts
	// could not be settled — fetch failed, an endpoint is unknown, the
	// repository is mid-operation. Behind/Ahead are NOT zero-trust here.
	StaleUnavailable = "unavailable"
	// StaleNotARepo: `dir` is not a git working tree at all.
	StaleNotARepo = "not-a-repo"
)

// StalenessResult answers how far `head` trails `base` in the repository at
// dir. Behind counts commits origin/base has that head lacks (the B6
// number); Ahead is its companion (head's unlanded work) and rides along
// because the one `rev-list --count` pass costs both for free.
type StalenessResult struct {
	State  string
	Behind int
	Ahead  int
	Reason string
}

// Staleness measures head-vs-base against origin/<base>, fetching origin/<base>
// first — the queue's question is "is the world ahead of this item", which is
// a question about origin, not about a stale local ref. The fetch is the
// daemon's own read (package rule: read-only, daemon-issued, no token); a
// failed fetch answers unavailable, never a guess off the last-known ref,
// because a merge queue that measures the past would land items onto a base
// that already moved. head may be a SHA or a ref name; both are resolved
// through rev-parse so a dangling integration branch reads as unavailable.
func Staleness(ctx context.Context, dir, base, head string) (StalenessResult, error) {
	if dir == "" || base == "" || head == "" {
		return StalenessResult{State: StaleUnavailable, Reason: "staleness names a directory, a base branch and a head"}, nil
	}
	// Not-a-repo first, so a wrong path never reads as a git failure.
	out, err := run(ctx, dir, 1<<10, "rev-parse", "--git-dir")
	if err != nil {
		return StalenessResult{}, err // git itself missing: the caller's fault line
	}
	if out.exitCode != 0 {
		return StalenessResult{State: StaleNotARepo, Reason: strings.TrimSpace(out.stderr)}, nil
	}
	// The fetch is allowed to fail — offline, auth, no origin — and every
	// such failure is the same three-state answer, not an error.
	fetch, err := run(ctx, dir, 1<<13, "fetch", "--quiet", "origin", base)
	if err != nil {
		return StalenessResult{State: StaleUnavailable, Reason: "git fetch: " + err.Error()}, nil
	}
	if fetch.exitCode != 0 {
		return StalenessResult{State: StaleUnavailable, Reason: "git fetch origin " + base + ": " + firstLine(fetch.stderr)}, nil
	}
	// Both endpoints must resolve to commits. An unknown origin/<base> after
	// a good fetch means the branch does not exist there; an unknown head
	// means the item's branch/sha is gone — both are "cannot settle", with
	// the reason naming which side.
	if r := resolveCommit(ctx, dir, "origin/"+base); r != "" {
		return StalenessResult{State: StaleUnavailable, Reason: r}, nil
	}
	if r := resolveCommit(ctx, dir, head); r != "" {
		return StalenessResult{State: StaleUnavailable, Reason: r}, nil
	}
	behind, ok := revListCount(ctx, dir, head, "origin/"+base)
	if !ok {
		return StalenessResult{State: StaleUnavailable, Reason: "git rev-list could not count " + head + "..origin/" + base}, nil
	}
	ahead, ok := revListCount(ctx, dir, "origin/"+base, head)
	if !ok {
		return StalenessResult{State: StaleUnavailable, Reason: "git rev-list could not count origin/" + base + ".." + head}, nil
	}
	return StalenessResult{State: StaleOK, Behind: behind, Ahead: ahead}, nil
}

// resolveCommit answers "" when ref resolves to a commit, else the reason.
func resolveCommit(ctx context.Context, dir, ref string) string {
	out, err := run(ctx, dir, 1<<10, "rev-parse", "--verify", "-q", ref+"^{commit}")
	if err != nil {
		return "git rev-parse: " + err.Error()
	}
	if out.exitCode != 0 || strings.TrimSpace(out.stdout) == "" {
		return "unknown commit: " + ref
	}
	return ""
}

// revListCount counts commits in `from..to` (what `to` has that `from`
// lacks). ok=false on any failure — including a non-numeric answer, which is
// not a number nobody counted.
func revListCount(ctx context.Context, dir, from, to string) (int, bool) {
	out, err := run(ctx, dir, 1<<10, "rev-list", "--count", from+".."+to)
	if err != nil || out.exitCode != 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(out.stdout))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// firstLine trims git's stderr to its leading line for a one-line reason.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
