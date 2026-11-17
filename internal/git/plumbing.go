// Package git holds the thin git wrappers the S16 actions obey — and the
// two constraints a naive implementation breaks, both from RECORDED
// INCIDENTS: the completeness assertion (hand-picked files left behind and
// then dropped by the squash merge) and worktree mode (a cleanup that
// deleted a Paseo-owned workspace).
//
// The standing prohibitions (N8/N9/I5): force-push and branch or tag
// deletion are ABSENT — not off by default, not behind a flag. Force-push
// is the one git operation whose damage is unbounded and unrecoverable
// from the loop's side, and there is no failure this slice can encounter
// whose correct recovery is one. TestNoForcePushAnywhere greps the package
// so the capability cannot arrive quietly.
//
// Results are discriminated (DB19), never exit codes: a missing ref is
// absence, an unreachable remote is transport, and unmerged paths are a
// conflict — three different loop responses (say so / retry / reconcile).
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// GitError is the discriminated domain error of this package.
type GitError struct {
	Kind   string
	Detail string
	Code   int
}

const (
	KindExit           = "exit"            // raw non-zero, not yet classified
	KindDirtyTree      = "dirty-tree"      // the completeness assertion tripped
	KindConflict       = "conflict"        // unmerged paths
	KindTransport      = "transport"       // the remote could not be reached
	KindPushUnverified = "push-unverified" // push claimed success; the ref disagrees (O8)
	KindRefused        = "refused"         // a mode or state forbids the operation
)

func (e *GitError) Error() string { return e.Kind + ": " + e.Detail }

func asGitError(err error, out **GitError) bool { return errors.As(err, out) }

// Exec runs one git command in dir and returns its combined output.
// Production is realExec; tests inject a recording fake so the COMMAND
// LOG — what was attempted — is assertable, not just what succeeded.
type Exec func(ctx context.Context, dir string, args ...string) (string, error)

// NewClient is the constructor production uses: a Client whose every git child
// runs in `env`, which the caller built with `execenv.GitEnv` (EV10).
//
// A bare `&git.Client{}` still compiles and still refuses at the exec — the
// zero value has no environment, and "no environment" is a refusal here rather
// than the daemon's own environment, which is the leak S59 closes. A test that
// injects `Exec` never reaches that arm and is unaffected.
func NewClient(env []string) *Client { return &Client{Env: env} }

// realExec is the package-level fallback, kept for callers that injected an
// Exec; the production path goes through (*Client).realExec, which is the one
// that can see the Client's environment.
var realExec Exec = func(ctx context.Context, dir string, args ...string) (string, error) {
	// Reaching this function on a real (non-injected) exec means a Client was
	// built without an environment. Refuse, and name the missing thing; the
	// alternative was `cmd.Env = nil`, which is Go for "give the child
	// everything the daemon has", including the provider keys.
	return "", &GitError{Kind: KindRefused, Detail: "refusing to run git: no execution environment was " +
		"resolved for this client — build it with git.NewClient(execenv.GitEnv(...)) rather than inheriting " +
		"the daemon's environment"}
}

// realExec is the production exec: the real git binary, in the Client's
// environment, in `dir`.
func (c *Client) realExec(ctx context.Context, dir string, args ...string) (string, error) {
	if len(c.Env) == 0 {
		return "", &GitError{Kind: KindRefused, Detail: "refusing to run git: this client has no execution " +
			"environment (Client.Env is empty), and the alternative is inheriting the daemon's — including its " +
			"provider keys and its forge token. Build the client with git.NewClient(execenv.GitEnv(...))"}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// The environment is the caller's, built by execenv.GitEnv: ambient GIT_*
	// stripped (an inherited GIT_DIR retargets the operation), the hardening
	// block appended, and nothing the daemon holds that git did not ask for.
	cmd.Env = c.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		code := -1
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return string(out), &GitError{Kind: KindExit, Detail: strings.TrimSpace(string(out)), Code: code}
	}
	return string(out), nil
}

// Client is the package's door. A nil Exec means the real git; a nil/empty Env
// on that real path is a REFUSAL, not an inheritance (S59, EV10). Tests inject
// an Exec and never touch either rule.
type Client struct {
	Exec Exec

	// Env is the environment every git child gets: the output of
	// `execenv.GitEnv(daemonEnv, execenv.GitEnvDaemon)` for the daemon's own
	// git, and of `GitEnvUIRead` for the UI's read path (EV10). It is a plain
	// slice and not a ChildEnv because this package must stay free of the
	// execution plane — the caller resolves, this package consumes, and the
	// refusal below is what keeps a forgotten caller from inheriting instead.
	Env []string
}

func (c *Client) exec(ctx context.Context, dir string, args ...string) (string, error) {
	ex := c.Exec
	if ex == nil {
		return c.realExec(ctx, dir, args...)
	}
	return ex(ctx, dir, args...)
}

// --- existence checks (absence is a result, not an error) ------------------

// BranchExistsLocal is `git show-ref --verify refs/heads/<branch>`: exit 1
// answers FALSE with no error — check-then-act routes on absence.
func (c *Client) BranchExistsLocal(ctx context.Context, dir, branch string) (bool, error) {
	_, err := c.exec(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ge *GitError
	if asGitError(err, &ge) && ge.Kind == KindExit && ge.Code == 1 {
		return false, nil
	}
	return false, err
}

// BranchExistsRemote is `git rev-parse --verify origin/<branch>`.
func (c *Client) BranchExistsRemote(ctx context.Context, dir, branch string) (bool, error) {
	_, err := c.exec(ctx, dir, "rev-parse", "--verify", "origin/"+branch)
	if err == nil {
		return true, nil
	}
	var ge *GitError
	if asGitError(err, &ge) && ge.Kind == KindExit {
		return false, nil
	}
	return false, err
}

// CurrentBranch reads the branch the working tree is on.
func (c *Client) CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := c.exec(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// LocalHEAD reads the commit the working tree's branch points at.
func (c *Client) LocalHEAD(ctx context.Context, dir string) (string, error) {
	out, err := c.exec(ctx, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// --- staging and the completeness assertion ---------------------------------

// StageEverything is `git add -A` — ALL, with no pathspec exclusion. v1's
// v1's pathspec exclusion of the loop artefacts directory was retired at
// L24: Aeon's store lives at $XDG_STATE_HOME/aeon/state.db, OUTSIDE the
// working tree, and the only
// things Aeon writes into a repository are `produces:` artefacts, which
// belong in the commit. An exclusion carried forward out of habit would
// hide exactly the class of leftover the assertion exists to catch.
func (c *Client) StageEverything(ctx context.Context, dir string) error {
	_, err := c.exec(ctx, dir, "add", "-A")
	return err
}

// StatusPorcelain is the assertion's instrument.
func (c *Client) StatusPorcelain(ctx context.Context, dir string) (string, error) {
	return c.exec(ctx, dir, "status", "--porcelain")
}

// AssertClean is the completeness assertion (L24/D3). Its exact form:
// after `git add -A`, `git status --porcelain` may contain NOTHING that
// staging failed to capture — no untracked lines (`??`, the leftover
// add -A could not see) and no unstaged column (any Y ≠ blank is a
// worktree change sitting outside the index, the hand-pick shape of the
// recorded incident). A literally EMPTY listing is the nothing-to-commit
// case; staged-and-ready lines are what a pre-commit assertion must
// accept, or every commit would self-refuse. When it trips, a second
// listing with -uall names the individual leftovers (plain porcelain
// collapses a directory to one line); a refusal that cannot say what it
// refused is not actionable.
func (c *Client) AssertClean(ctx context.Context, dir string) error {
	out, err := c.StatusPorcelain(ctx, dir)
	if err != nil {
		return err
	}
	dirty := false
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		x, y := line[0], line[1]
		if (x == '?' && y == '?') || y != ' ' {
			dirty = true
			break
		}
	}
	if !dirty {
		return nil
	}
	named, err := c.exec(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		named = out // the plain listing already proved leftovers; naming is best effort
	}
	return &GitError{Kind: KindDirtyTree, Detail: strings.TrimSpace(named)}
}

// CommitEverything stages ALL, asserts the tree clean, and commits.
// `nothing to commit` answers SUCCESS with nothing=true — an empty status
// means the work is already in, and a naive implementation fails a step
// that has nothing left to do.
func (c *Client) CommitEverything(ctx context.Context, dir, message string) (sha string, nothing bool, err error) {
	if err := c.StageEverything(ctx, dir); err != nil {
		return "", false, err
	}
	// The assertion runs AFTER staging (stage everything, then assert) and
	// before committing: it fires when staging was bypassed or raced — the
	// hand-pick shape of the recorded incident.
	if err := c.AssertClean(ctx, dir); err != nil {
		return "", false, err
	}
	out, err := c.exec(ctx, dir, "commit", "-m", message)
	if err != nil {
		combined := out
		if ge := new(GitError); asGitError(err, &ge) {
			combined = ge.Detail + "\n" + combined
		}
		switch {
		case strings.Contains(combined, "nothing to commit"), strings.Contains(combined, "nothing added to commit"):
			return "", true, nil
		case strings.Contains(combined, "unmerged files"), strings.Contains(combined, "fix them up"):
			return "", false, &GitError{Kind: KindConflict, Detail: strings.TrimSpace(combined)}
		default:
			return "", false, err
		}
	}
	head, err := c.LocalHEAD(ctx, dir)
	if err != nil {
		return "", false, err
	}
	return head, false, nil
}

// --- push, verified (O8) ------------------------------------------------------

// Push pushes the branch and then RE-READS the remote ref: `git push`
// reporting success is not proof. A push that appears to succeed and did
// not is indistinguishable by exit code alone, and everything downstream
// assumes the commit is fetchable — so the failure must surface HERE, as
// push-unverified, not later as a PR against a ref nobody can find.
func (c *Client) Push(ctx context.Context, dir, branch string) error {
	if _, err := c.exec(ctx, dir, "push", "-u", "origin", branch); err != nil {
		var ge *GitError
		if asGitError(err, &ge) && classifyTransport(ge.Detail) {
			return &GitError{Kind: KindTransport, Detail: ge.Detail}
		}
		return err
	}
	head, err := c.LocalHEAD(ctx, dir)
	if err != nil {
		return err
	}
	out, err := c.exec(ctx, dir, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	remote := ""
	if line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]); line != "" {
		remote = strings.Fields(line)[0]
	}
	if remote != head {
		return &GitError{Kind: KindPushUnverified,
			Detail: fmt.Sprintf("push was not verified: remote refs/heads/%s reads %q, local HEAD is %q — the push did not land (re-reading the ref is the only proof)", branch, remote, head)}
	}
	return nil
}

func classifyTransport(detail string) bool {
	for _, s := range []string{"Could not read from remote repository", "unable to", "timed out", "Could not resolve host", "connection"} {
		if strings.Contains(strings.ToLower(detail), strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// Run is the public door for the thin commands without a dedicated wrapper
// (branch creation, checkout, merge) — still through the injectable Exec,
// so the command log records everything.
func (c *Client) Run(ctx context.Context, dir string, args ...string) (string, error) {
	return c.exec(ctx, dir, args...)
}

// RemoteRefSHA reads the remote's sha for a branch through ls-remote;
// "" means the remote has never seen the branch (absence, not failure).
func (c *Client) RemoteRefSHA(ctx context.Context, dir, branch string) (string, error) {
	out, err := c.exec(ctx, dir, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return "", nil
	}
	return strings.Fields(line)[0], nil
}

// LocalHEADFor reads the sha a LOCAL ref points at (any ref, not just
// HEAD).
func (c *Client) LocalHEADFor(ctx context.Context, dir, ref string) (string, error) {
	out, err := c.exec(ctx, dir, "rev-parse", ref)
	return strings.TrimSpace(out), err
}

// --- ancestry (the phantom-merge guard's instrument, used at T6) ------------

// IsAncestor answers `git merge-base --is-ancestor <sha> <ref>`. Exit 0 is
// true and exit 1 is FALSE WITHOUT ERROR (the question is "is it", not
// "can we ask"); anything else is a real failure.
func (c *Client) IsAncestor(ctx context.Context, dir, sha, ref string) (bool, error) {
	_, err := c.exec(ctx, dir, "merge-base", "--is-ancestor", sha, ref)
	if err == nil {
		return true, nil
	}
	var ge *GitError
	if asGitError(err, &ge) && ge.Kind == KindExit && ge.Code == 1 {
		return false, nil
	}
	return false, err
}

// --- S60 merge queue (MQ6/MQ7): the integration plumbing ------------------
//
// Every helper here is an additive form of an existing shape — worktree add
// (S32's), merge --no-edit (F9's), plain fetch — and NONE adds a banned verb:
// nothing rewrites history (MQ6 cuts a NEW ref forward instead), nothing
// force-pushes, nothing deletes. TestIntegrationCodeAddsNoBannedVerb
// (internal/action) re-scans this file with TestNoForcePushAnywhere's tokens
// plus the history-rewrite verb.

// WorktreeAdd is the S32 worktree door on its own args: with base non-empty
// it creates branch AT base (`worktree add -b <branch> <path> <base>`); with
// base empty it attaches the EXISTING branch (`worktree add <path> <branch>`)
// — the rebuild case of a crashed integration worktree whose branch outlived
// its directory.
func (c *Client) WorktreeAdd(ctx context.Context, dir, path, branch, base string) error {
	args := []string{"worktree", "add"}
	if base != "" {
		args = append(args, "-b", branch, path, base)
	} else {
		args = append(args, path, branch)
	}
	_, err := c.exec(ctx, dir, args...)
	return err
}

// Fetch is the plain scoped fetch: `git fetch <remote> <branch>`. One ref,
// never --all (the queue asks for exactly the branch it is about to cut at),
// and never a refspec rewrite — the rewritten-refspec token is on the banned
// list this package walks.
func (c *Client) Fetch(ctx context.Context, dir, remote, branch string) error {
	_, err := c.exec(ctx, dir, "fetch", remote, branch)
	return err
}

// MergeNoEdit is F9's mechanism pointed at a ref: `git merge --no-edit <ref>`
// in dir. Exit 0 is a clean merge (conflicts nil, err nil). A merge that
// stopped on conflicts is NOT an error to the caller's contract: the paths
// come back and NOTHING is aborted — MQ6.2 leaves the merge in progress in
// the integration worktree for the resolution loop to work in. Any other
// failure returns the error with no paths.
func (c *Client) MergeNoEdit(ctx context.Context, dir, ref string) ([]string, error) {
	_, err := c.exec(ctx, dir, "merge", "--no-edit", ref)
	if err == nil {
		return nil, nil
	}
	unmerged, uerr := c.UnmergedPaths(ctx, dir)
	if uerr == nil && len(unmerged) > 0 {
		return unmerged, nil
	}
	return nil, err
}

// MergeAbort unwinds a merge in progress (`git merge --abort`). The queue's
// own actions never call it — leaving the merge in progress IS the design —
// it exists for the resolution loop's honesty checks.
func (c *Client) MergeAbort(ctx context.Context, dir string) error {
	_, err := c.exec(ctx, dir, "merge", "--abort")
	return err
}

// UnmergedPaths answers `git diff --name-only --diff-filter=U` — the INDEX's
// own list of conflicted paths. Names only: no hunk text leaves this function
// (MQ14's redaction rule — the paths are the entire payload a conflict is
// allowed to carry).
func (c *Client) UnmergedPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := c.exec(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ConflictMarkersIn answers which of the named paths still carry conflict
// MARKERS: a line beginning `<<<<<<<` or `>>>>>>>` (the pair only — a bare
// `=======` is legal prose in a markdown body, and the pair never is).
// Unresolvable files (deleted mid-merge, unreadable) are named too: the
// caller refuses on a name it cannot clear, never on a guess.
func (c *Client) ConflictMarkersIn(dir string, paths []string) []string {
	var flagged []string
	for _, p := range paths {
		src, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			flagged = append(flagged, p)
			continue
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(line, "<<<<<<<") || strings.HasPrefix(line, ">>>>>>>") {
				flagged = append(flagged, p)
				break
			}
		}
	}
	return flagged
}

// RevCount answers `git rev-list --count <range>` — "how far is this branch
// behind/inside that one", the queue's bounded catch-up question. A range
// nobody can parse is a plain error, not a zero.
func (c *Client) RevCount(ctx context.Context, dir, rng string) (int, error) {
	out, err := c.exec(ctx, dir, "rev-list", "--count", rng)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, &GitError{Kind: KindExit, Detail: "rev-list --count " + rng + ": unreadable answer " + strings.TrimSpace(out)}
	}
	return n, nil
}

// --- S61 T4 (GI9/GI12): the scaffold plumbing, additive ------------------
//
// Three instruments the intake bootstrap needs and nothing else. All are
// plain git on the same client, environment and refusal rules as every
// method above; NONE of them can force-push, delete a branch or delete a
// tag — TestNoForcePushAnywhere walks this file and would call a lie out.

// refUnsafe refuses any ref whose shape could smuggle a flag or split a
// rev-parse: whitespace, control characters, the git-rejected
// metacharacters, the `..` range marker, a `.lock` ending, and a leading
// `-` (the one that turns a name into an option). `check-ref-format` stays
// git's verdict — this is the floor beneath it, not a replacement.
func refUnsafe(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, "/") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".lock") {
		return true
	}
	if strings.ContainsAny(ref, " ~^:?*[\\\t\n") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return true
	}
	for _, r := range ref {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// Init runs `git init -b <branch> <dir>` — the first command of a
// scaffolded repository (GI9). The branch is the plan's default branch
// (GI12); an unsafe name is refused before git is asked. The directory
// must already exist (the scaffold writer creates it).
func (c *Client) Init(ctx context.Context, dir, branch string) error {
	if refUnsafe(branch) {
		return &GitError{Kind: KindRefused, Detail: "branch name " + branch + " is not a safe ref name — refused before git sees it (no flag, no range, no lock suffix)"}
	}
	// The command runs from the nearest EXISTING ancestor: `git init <dir>`
	// creates the leaf itself, and a child process cannot start in a
	// directory that does not exist yet.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		_, err := c.exec(ctx, existingAncestor(dir), "init", "-b", branch, dir)
		return err
	}
	_, err := c.exec(ctx, dir, "init", "-b", branch, dir)
	return err
}

// existingAncestor walks up to the first directory that is there. The
// walk is bounded because a path that never meets an existing directory
// is not a filesystem, and a loop is the wrong answer to a bug in `Stat`.
func existingAncestor(dir string) string {
	p := filepath.Clean(dir)
	for i := 0; i < 64; i++ {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
	return p
}

// AddRemote is `git remote add <name> <url>` — the ONE remote door, and it
// ADDS: git itself refuses a name that already exists, and the scaffold
// action checks first (GI10 never retargets a remote silently).
func (c *Client) AddRemote(ctx context.Context, dir, name, url string) error {
	if refUnsafe(name) {
		return &GitError{Kind: KindRefused, Detail: "remote name " + name + " is not a safe name"}
	}
	if url == "" || strings.HasPrefix(url, "-") || strings.ContainsAny(url, " \t\n") {
		return &GitError{Kind: KindRefused, Detail: "a remote url is one absolute URL without whitespace"}
	}
	_, err := c.exec(ctx, dir, "remote", "add", name, url)
	return err
}

// RemoteURL answers the current value of `git remote get-url <name>` —
// presence as data, so a caller can refuse to retarget a remote that is
// somebody else's (absence is "", false, nil; git's exit 2 is the absence).
func (c *Client) RemoteURL(ctx context.Context, dir, name string) (string, bool, error) {
	out, err := c.exec(ctx, dir, "remote", "get-url", name)
	if err != nil {
		var ge *GitError
		if asGitError(err, &ge) && ge.Code == 2 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

// IdentityResolved is git's OWN answer to "who would this commit be?" —
// `git var GIT_AUTHOR_IDENT` and `git var GIT_COMMITTER_IDENT`, read, never
// set (GI12: Aeon does not invent an identity and does not write config to
// manufacture one). A directory that does not exist yet resolves against
// its nearest EXISTING ancestor, which is the truth a scaffold is about to
// inherit. Unresolved is ok=false, not an error: the caller owes the
// sentence, not git.
func (c *Client) IdentityResolved(ctx context.Context, dir string) (name, email string, ok bool, err error) {
	probe := existingAncestor(dir)
	author, aErr := c.exec(ctx, probe, "var", "GIT_AUTHOR_IDENT")
	committer, cErr := c.exec(ctx, probe, "var", "GIT_COMMITTER_IDENT")
	if aErr != nil || cErr != nil {
		// git answers non-zero when it cannot resolve — that IS the
		// unresolved answer, not a transport failure. A no-environment
		// refusal (KindRefused, never an exit) travels unchanged: it
		// names a wiring bug, not a missing identity.
		var ge *GitError
		if asGitError(aErr, &ge) && ge.Kind == KindRefused {
			return "", "", false, aErr
		}
		if asGitError(cErr, &ge) && ge.Kind == KindRefused {
			return "", "", false, cErr
		}
		return "", "", false, nil
	}
	an, ae, aParse := parseIdent(author)
	cn, ce, cParse := parseIdent(committer)
	if !aParse || !cParse || an == "" || ae == "" || cn == "" || ce == "" {
		return "", "", false, nil
	}
	return an, ae, true, nil
}

// parseIdent reads "Name <email> 1730000000 +0000" — git's ident line.
func parseIdent(s string) (name, email string, ok bool) {
	s = strings.TrimSpace(s)
	lt := strings.IndexByte(s, '<')
	gt := strings.IndexByte(s, '>')
	if lt <= 0 || gt < 0 || gt < lt {
		return "", "", false
	}
	return strings.TrimSpace(s[:lt]), s[lt+1 : gt], true
}

// splitLines trims and drops empties — the porcelain answer discipline this
// package already keeps from StatusPorcelain.
func splitLines(out string) []string {
	var out2 []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			out2 = append(out2, s)
		}
	}
	return out2
}
