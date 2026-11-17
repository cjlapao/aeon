package action

// S60 merge queue T5: the three integration actions, the typed conflict, and
// the integration worktree builder — one file because they are one job
// (merge-forward onto a new ref, in a worktree, never the person's checkout).
// MQ6 forbids history rewrite outright: none of these verbs rewrites
// history, none force-pushes, none deletes (MQ6.4 leaves old integrate refs
// for a person to clean); TestIntegrationCodeAddsNoBannedVerb re-scans this
// file and the plumbing it adds with TestNoForcePushAnywhere's own token
// list, extended with the history-rewrite verb.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/git"
)

// ConflictBound is the cap on the path list a conflict result carries
// (MQ8/MQ14: paths only, bounded — a 5,000-file conflict must not smuggle a
// buffer into every transcript line that quotes the failure).
const ConflictBound = 100

// ConflictError is the typed conflict result (MQ8): the merge is left IN
// PROGRESS in the integration worktree, and the only payload is the file
// NAMES. No hunk text, ever — the same redaction line MQ14 draws for
// events, errors and logs; a model reads the files themselves in the
// worktree, which is exactly what the worktree is for.
type ConflictError struct {
	// Paths are the unmerged paths, first ConflictBound of them.
	Paths []string
	// Hidden counts paths past the bound that the sentence reports without
	// carrying (so the count never lies).
	Hidden int
}

func (e *ConflictError) Error() string {
	tail := ""
	if e.Hidden > 0 {
		tail = fmt.Sprintf(" (+%d more)", e.Hidden)
	}
	return fmt.Sprintf("merge conflicts in %d file(s): %s%s", len(e.Paths)+e.Hidden, strings.Join(e.Paths, ", "), tail)
}

// conflictError caps without lying: the struct carries at most the bound and
// counts the rest.
func conflictError(paths []string) *ConflictError {
	if len(paths) <= ConflictBound {
		return &ConflictError{Paths: paths}
	}
	return &ConflictError{Paths: paths[:ConflictBound], Hidden: len(paths) - ConflictBound}
}

// IntegrationWorktreePath is the ONE definition of where an integration
// worktree lives (MQ7): <componentRoot>/.worktrees/integrate-<item>, the S32
// ledger's own kind-id naming applied to the integration owner. The engine
// writes the ledger row and the action builds the directory against this one
// function, so the two can never disagree.
func IntegrationWorktreePath(root, item string) string {
	return root + "/.worktrees/integrate-" + item
}

// integrationBranchPattern is Publish's gate (MQ10's trust needs it): only a
// branch the queue itself names may be published — <prefix>integrate/<name>,
// the prefix escaped because a prefix may contain regex metacharacters, the
// name closed exactly like BranchNames.IntegrationBranch's.
func integrationBranchPattern(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `integrate/[a-z0-9][a-z0-9._-]*$`)
}

// AddIntegrationWorktree is F17's promise REBUILT (T0's F17 finding: the S32
// shortcut answers "Reused" from the BRANCH alone and never checks the
// directory). Branch exists AND directory is a live worktree ⇒ reuse. Branch
// exists, directory gone (the crash shape PA12 reconciles) ⇒ re-attach the
// existing branch. Branch absent ⇒ cut it at baseRef (MQ6.1: the base tip),
// refusing — never defaulting to HEAD — when no base was named.
func (a *GitActions) AddIntegrationWorktree(ctx context.Context, dir, path, branch, baseRef string) (GitResult, error) {
	c := a.client()
	exists, err := c.BranchExistsLocal(ctx, dir, branch)
	if err != nil {
		return GitResult{}, err
	}
	if exists {
		if isWorktree(path) {
			sha, err := c.LocalHEADFor(ctx, dir, branch)
			if err != nil {
				return GitResult{}, err
			}
			return GitResult{Action: "git.addIntegrationWorktree", Branch: branch, SHA: sha, Dir: path, Reused: true}, nil
		}
		// The branch outlived its worktree: rebuild by ATTACHING (no -b — the
		// branch is there; -b would fail, and failing here is how a crashed
		// landing stops being recoverable).
		if err := c.WorktreeAdd(ctx, dir, path, branch, ""); err != nil {
			return GitResult{}, err
		}
		sha, err := c.LocalHEADFor(ctx, dir, branch)
		if err != nil {
			return GitResult{}, err
		}
		return GitResult{Action: "git.addIntegrationWorktree", Branch: branch, SHA: sha, Dir: path}, nil
	}
	if baseRef == "" {
		return GitResult{}, fmt.Errorf("git.addIntegrationWorktree refuses: branch %q does not exist and no base was named — the queue cuts at the FETCHED base tip (MQ6.1), never at a guessed HEAD", branch)
	}
	if err := c.WorktreeAdd(ctx, dir, path, branch, baseRef); err != nil {
		return GitResult{}, err
	}
	sha, err := c.LocalHEADFor(ctx, dir, branch)
	if err != nil {
		return GitResult{}, err
	}
	return GitResult{Action: "git.addIntegrationWorktree", Branch: branch, SHA: sha, Dir: path}, nil
}

// isWorktree checks the mount marker, not just the directory: a linked
// worktree's .git is a FILE pointing at the common dir — present is mounted,
// absent (or a bare directory a person left there) is not.
func isWorktree(path string) bool {
	fi, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !fi.IsDir()
}

// BaseTipAfterFetch is the ledger's precondition (MQ7): the engine writes
// the row with the base's CURRENT fetched tip — fetch first, then read the
// local origin/<base>. The answer "" means the base branch does not exist
// where it should, which the caller refuses; a stale answer is impossible
// because the fetch is what makes it current.
func (a *GitActions) BaseTipAfterFetch(ctx context.Context, dir, base string) (string, error) {
	c := a.client()
	if err := c.Fetch(ctx, dir, "origin", base); err != nil {
		return "", err
	}
	return c.LocalHEADFor(ctx, dir, "origin/"+base)
}

// FetchBase is forge.land's post-guard instrument (S60 T6, BaseFetcher):
// the same fetch BaseTipAfterFetch makes, without the read — the guard
// asks IsAncestor against origin/<base> immediately after, and the fetch
// is what makes that ref mean NOW.
func (a *GitActions) FetchBase(ctx context.Context, dir, base string) error {
	return a.client().Fetch(ctx, dir, "origin", base)
}

// Integrate is git.integrate (local class): MQ6.1 and MQ6.2 in one call.
// Fetch base and the item's branch (objects and refs, plain refs only), cut
// <integrationBranch> at the base tip if it does not exist, merge the ITEM's
// head branch forward into it, and either return the SHA (clean, or already
// an ancestor — the crash-retry shape) or a *ConflictError with the merge
// left in progress in the worktree (nothing committed, nothing pushed).
//
// The person's checkout is never checked out: every merge runs in the
// worktree at IntegrationWorktreePath(root, item); root itself is touched
// only by fetch and worktree-add (MQ7).
func (a *GitActions) Integrate(ctx context.Context, root, item, integrationBranch, headBranch, base string) (GitResult, error) {
	c := a.client()
	path := IntegrationWorktreePath(root, item)
	if err := c.Fetch(ctx, root, "origin", base); err != nil {
		return GitResult{}, err
	}
	if err := c.Fetch(ctx, root, "origin", headBranch); err != nil {
		return GitResult{}, err
	}
	// The published-integration ref may exist too (crash rung after
	// publish, behaviour 9); its absence is the ordinary case — a fetch
	// of a ref the forge has never seen is not an error worth naming.
	_ = c.Fetch(ctx, root, "origin", integrationBranch)
	baseTip, err := c.LocalHEADFor(ctx, root, "origin/"+base)
	if err != nil || baseTip == "" {
		return GitResult{}, fmt.Errorf("git.integrate refuses: origin/%s resolved to nothing after a fetch — the base the queue lands on has to EXIST (MQ6.1)", base)
	}
	sourceTip, err := c.LocalHEADFor(ctx, root, "origin/"+headBranch)
	if err != nil || sourceTip == "" {
		return GitResult{}, fmt.Errorf("git.integrate refuses: origin/%s resolved to nothing after a fetch — the queue integrates a branch the remote has seen (MQ11 binds to it)", headBranch)
	}
	// The crash-retry rung: a re-run after a clean merge (or after the
	// engine died between merge and evidence) sees its own work already in
	// the branch and answers success, not a second merge.
	if has, err := c.BranchExistsLocal(ctx, root, integrationBranch); err != nil {
		return GitResult{}, err
	} else if has {
		merged, err := c.IsAncestor(ctx, root, sourceTip, integrationBranch)
		if err != nil {
			return GitResult{}, err
		}
		if merged {
			baseIn, err := c.IsAncestor(ctx, root, baseTip, integrationBranch)
			if err != nil {
				return GitResult{}, err
			}
			if !baseIn {
				// The crash rung WIDENED (T7, behaviours 3+9): the item's
				// work is in the branch but the branch stands on the OLD
				// base tip — the base moved between the merge and this
				// re-run. The catch-up merges the new base IN (forward
				// only: same ref, no rewrite, no delete — MQ6.3's rule),
				// and a conflict here is MQ6.2's shape exactly as the
				// first merge's was: merge left in progress, worktree
				// kept, ConflictError by count.
				if _, err := a.AddIntegrationWorktree(ctx, root, path, integrationBranch, baseTip); err != nil {
					return GitResult{}, err
				}
				conflicts, err := c.MergeNoEdit(ctx, path, baseTip)
				if err != nil {
					return GitResult{}, err
				}
				if conflicts != nil {
					return GitResult{}, conflictError(conflicts)
				}
				sha, err := c.LocalHEAD(ctx, path)
				if err != nil {
					return GitResult{}, err
				}
				return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: sha, Dir: path, Merged: true}, nil
			}
			sha, err := c.LocalHEADFor(ctx, root, integrationBranch)
			if err != nil {
				return GitResult{}, err
			}
			return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: sha, Dir: path, AlreadyMerged: true}, nil
		}
	}
	// The crash rung AFTER publish (T7, behaviour 9's merging rung): the
	// local branch is gone with the worktree, but the PUBLISHED
	// integration lives on origin and the PR may already point at it.
	// Re-cutting from the base would rewrite what the forge saw — the
	// published tip IS the branch, and the rebuild attaches to it
	// (forward only from here: same ref, no rewrite, no delete —
	// MQ6.3's rule again).
	if pubTip, err := c.LocalHEADFor(ctx, root, "origin/"+integrationBranch); err == nil && pubTip != "" {
		if _, err := a.AddIntegrationWorktree(ctx, root, path, integrationBranch, pubTip); err != nil {
			return GitResult{}, err
		}
		merged, err := c.IsAncestor(ctx, root, sourceTip, "origin/"+integrationBranch)
		if err != nil {
			return GitResult{}, err
		}
		if !merged {
			conflicts, err := c.MergeNoEdit(ctx, path, sourceTip)
			if err != nil {
				return GitResult{}, err
			}
			if conflicts != nil {
				return GitResult{}, conflictError(conflicts)
			}
			sha, err := c.LocalHEAD(ctx, path)
			if err != nil {
				return GitResult{}, err
			}
			return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: sha, Dir: path, Merged: true}, nil
		}
		baseIn, err := c.IsAncestor(ctx, root, baseTip, "origin/"+integrationBranch)
		if err != nil {
			return GitResult{}, err
		}
		if !baseIn {
			conflicts, err := c.MergeNoEdit(ctx, path, baseTip)
			if err != nil {
				return GitResult{}, err
			}
			if conflicts != nil {
				return GitResult{}, conflictError(conflicts)
			}
			sha, err := c.LocalHEAD(ctx, path)
			if err != nil {
				return GitResult{}, err
			}
			return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: sha, Dir: path, Merged: true}, nil
		}
		return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: pubTip, Dir: path, AlreadyMerged: true}, nil
	}
	if _, err := a.AddIntegrationWorktree(ctx, root, path, integrationBranch, baseTip); err != nil {
		return GitResult{}, err
	}
	conflicts, err := c.MergeNoEdit(ctx, path, sourceTip)
	if err != nil {
		return GitResult{}, err
	}
	if conflicts != nil {
		// MQ6.2: the merge stays IN PROGRESS — the resolution loop works in
		// this worktree, and an abort here would destroy the half-resolved
		// state that is the point.
		return GitResult{}, conflictError(conflicts)
	}
	sha, err := c.LocalHEAD(ctx, path)
	if err != nil {
		return GitResult{}, err
	}
	return GitResult{Action: GitIntegrate, Branch: integrationBranch, SHA: sha, Dir: path, Merged: true}, nil
}

// ConcludeIntegration is git.concludeIntegration (local class): the
// resolution loop's end-of-step door. It refuses on a half-resolved merge —
// unmerged index entries OR conflict markers in the changed files (a model
// that deleted the markers without resolving gets NAMED, not committed) —
// and otherwise stages, commits, and asserts clean, returning the new
// integration SHA. A tree that is already clean and committed (the model
// committed its own resolution; the crash-after-commit re-run) is the
// idempotent path: assert, record, succeed.
func (a *GitActions) ConcludeIntegration(ctx context.Context, dir, integrationBranch string) (GitResult, error) {
	c := a.client()
	unmerged, err := c.UnmergedPaths(ctx, dir)
	if err != nil {
		return GitResult{}, err
	}
	if len(unmerged) > 0 {
		return GitResult{}, fmt.Errorf("git.concludeIntegration refuses: the merge is still unresolved in %d file(s): %s — resolve them (or abort) before concluding",
			len(unmerged), strings.Join(capPaths(unmerged), ", "))
	}
	changed, err := c.StatusPorcelain(ctx, dir)
	if err != nil {
		return GitResult{}, err
	}
	if marked := c.ConflictMarkersIn(dir, porcelainPaths(changed)); len(marked) > 0 {
		return GitResult{}, fmt.Errorf("git.concludeIntegration refuses: %d file(s) still carry conflict markers: %s — a marker may not be committed (MQ14)",
			len(marked), strings.Join(capPaths(marked), ", "))
	}
	inMerge, err := mergeInProgress(ctx, c, dir)
	if err != nil {
		return GitResult{}, err
	}
	sha := ""
	if inMerge || strings.TrimSpace(changed) != "" {
		if err := c.StageEverything(ctx, dir); err != nil {
			return GitResult{}, err
		}
		msg := "integrate: conclude merge into " + integrationBranch
		cs, nothing, cerr := c.CommitEverything(ctx, dir, msg)
		if cerr != nil {
			return GitResult{}, cerr
		}
		if !nothing {
			sha = cs
		}
	}
	if err := c.AssertClean(ctx, dir); err != nil {
		return GitResult{}, err
	}
	if sha == "" {
		sha, err = c.LocalHEAD(ctx, dir)
		if err != nil {
			return GitResult{}, err
		}
	}
	return GitResult{Action: GitConcludeIntegration, Branch: integrationBranch, SHA: sha, Dir: dir, Merged: true}, nil
}

// PublishIntegration is git.publishIntegration (OUTWARD class — the only
// outward thing T5 adds, and it is a plain push of a NEW ref). The gate is
// the NAME: only <prefix>integrate/<name> ever leaves this machine, so the
// forge-side protection (MQ10) can trust the branch it is shown. F11's plain
// Push only — an existing ref name cannot arrive here at all (the pattern),
// and a force-push is not something this package can express. DIR IS THE
// CHECKOUT THAT CARRIES THE BRANCH — the engine's WorkDir is the integration
// worktree at publish time, and F11's verification compares the remote's
// answer against that tree's HEAD, which only means what it says when the
// two are the same lineage.
func (a *GitActions) PublishIntegration(ctx context.Context, dir, prefix, integrationBranch string) (GitResult, error) {
	c := a.client()
	if !integrationBranchPattern(prefix).MatchString(integrationBranch) {
		return GitResult{}, fmt.Errorf("git.publishIntegration refuses: %q is not an integration branch of this repository (^%sintegrate/<name>) — the queue publishes only what it built", integrationBranch, prefix)
	}
	on, err := c.CurrentBranch(ctx, dir)
	if err != nil {
		return GitResult{}, err
	}
	if on != integrationBranch {
		return GitResult{}, fmt.Errorf("git.publishIntegration refuses: %q is checked out nowhere the queue can push from — %q holds %q (F11 verifies a push against the tree that carries the branch)", integrationBranch, dir, on)
	}
	sha, err := c.LocalHEADFor(ctx, dir, integrationBranch)
	if err != nil || sha == "" {
		return GitResult{}, fmt.Errorf("git.publishIntegration refuses: branch %q does not exist locally — nothing to publish", integrationBranch)
	}
	if err := c.Push(ctx, dir, integrationBranch); err != nil {
		return GitResult{}, err
	}
	return GitResult{Action: GitPublishIntegration, Branch: integrationBranch, SHA: sha, Dir: dir}, nil
}

// safeRefName is the queue's ref-name hygiene check for CALLER-supplied
// branch names (head/base): the git-safe character set, a mandatory leading
// alnum (git's own rule), no `..` (the path-escape shape). The queue's own
// names are stricter still (identity regexes), but a person may enqueue an
// existing branch of any legitimate spelling, and this door refuses argv
// games, not mixed case.
func safeRefName(s string) bool {
	if s == "" || strings.Contains(s, "..") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '/':
		default:
			return false
		}
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// porcelainPaths turns `status --porcelain` output into the path list the
// marker scan reads: the two status columns drop, a rename contributes its
// TARGET, and a git-quoted path (the non-ASCII form) stays as git printed it —
// its ReadFile will fail, the scan will flag it, and conclude will REFUSE:
// failing safe means never committing a file this door could not read.
func porcelainPaths(porcelain string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(porcelain), "\n") {
		if len(line) < 4 {
			continue
		}
		p := line[3:]
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		paths = append(paths, strings.TrimSpace(p))
	}
	return paths
}

// mergeInProgress asks the only question that discriminates "commit this is a
// merge commit" from "commit this is a normal commit": MERGE_HEAD exists in
// the worktree's git dir (`rev-parse -q --verify MERGE_HEAD` — exit 1 is the
// calm absence, any other failure is real).
func mergeInProgress(ctx context.Context, c *git.Client, dir string) (bool, error) {
	_, err := c.Run(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	if err == nil {
		return true, nil
	}
	var ge *git.GitError
	if asGitErr(err, &ge) && ge.Kind == git.KindExit && ge.Code == 1 {
		return false, nil
	}
	return false, err
}

// capPaths bounds a REFUSAL sentence like conflictError bounds the typed
// result — names first, count honest.
func capPaths(paths []string) []string {
	if len(paths) <= 20 {
		return paths
	}
	out := append([]string{}, paths[:20]...)
	return append(out, fmt.Sprintf("(+%d more)", len(paths)-20))
}
