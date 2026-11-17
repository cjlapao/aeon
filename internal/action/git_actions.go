package action

// The three git actions (L3, D2): check-then-act, identifiers constructed
// by code, nothing-to-commit a success.
//
// The names are the interesting code. The recorded branch-inheritance bug
// was a model re-deriving a branch name and getting it subtly wrong — the
// loop then "successfully" worked on a branch nobody was reviewing. The
// split that removes the whole bug class: BranchNames is the only door,
// its inputs are loop and task IDENTITIES (validated at construction —
// separators and spaces cannot be smuggled through a task slug), and no
// action signature has anywhere to type a branch (pinned by reflection in
// TestBranchNamesComeFromCode).

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/git"
)

// GitActions is the door the registry (T10) wires. A nil Git means the real
// git binary; tests inject a recording fake and assert on the COMMAND LOG.
type GitActions struct {
	Git *git.Client
}

func (a *GitActions) client() *git.Client {
	if a.Git != nil {
		return a.Git
	}
	// The fallback is a client with NO environment, and `git` refuses to run
	// one rather than inherit the daemon's (S59 T5, EV10). Production always
	// injects a client built by `git.NewClient(execenv.GitEnv(...))`; a caller
	// that reaches this arm gets a refusal naming the missing environment,
	// which is the honest answer and not a silent inheritance. This package
	// does not read the process environment, so it cannot build the
	// environment itself — that is the caller's decision to make.
	return &git.Client{}
}

// LocalHEAD reads the tree's current head — the check-only instrument the
// loop's review token (S57 T6, VG9) and the verify service (T6) need,
// without an action of their own. A nil Git means the real git binary.
func (a *GitActions) LocalHEAD(ctx context.Context, dir string) (string, error) {
	return a.client().LocalHEAD(ctx, dir)
}

// BranchNames carries IDENTITIES, never names. Construction is the only
// door (L3/I3): names are derived, and the identity parts are validated
// so a branch name cannot be smuggled through a task slot.
type BranchNames struct {
	Loop   string
	Task   string
	Prefix string // S60 MQ15: the component's pinned branch_prefix, "" for the default names
}

var identity = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// prefixRule is the branch_prefix rule (S60 MQ15): an optional lowercase
// identity and one slash — "aeon/", or nothing at all. It is an EXPORTED
// door because two seams check it: name construction here, and the daemon's
// component.update, which must refuse an unsafe prefix where it is SET
// rather than at every loop that later inherits it.
var prefixRule = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*/)?$`)

// CheckBranchPrefix is that rule as a sentence.
func CheckBranchPrefix(prefix string) error {
	if prefixRule.MatchString(prefix) {
		return nil
	}
	return fmt.Errorf("branch prefix %q is not safe — a prefix is an optional lowercase identity and one slash, like \"aeon/\" (a branch name is built from validated identities, L3)", prefix)
}

// NewBranchNames validates the identities that branch names are built from.
// The recorded bug is refused here, at the door: "feat/ure", "a b/c", and
// the empty string are all spelling attempts a model may not complete.
// S60 kept this signature exactly (a Do-NT): it is the no-prefix door.
func NewBranchNames(loop, task string) (BranchNames, error) {
	return NewBranchNamesWith("", loop, task)
}

// NewBranchNamesWith is the prefix-aware construction: same identity rules
// for the parts, plus the prefix rule in front. Every prefixed name in the
// system — the engine's, the queue's, the forge guards' — is built here or
// not at all (F3: one derivation, no split-brain).
func NewBranchNamesWith(prefix, loop, task string) (BranchNames, error) {
	if err := CheckBranchPrefix(prefix); err != nil {
		return BranchNames{}, err
	}
	if !identity.MatchString(loop) {
		return BranchNames{}, fmt.Errorf("loop identity %q is not a safe slug — branch names are built from validated identities (L3)", loop)
	}
	if !identity.MatchString(task) {
		return BranchNames{}, fmt.Errorf("task identity %q is not a safe slug — the recorded branch-inheritance bug is refused at construction (I3)", task)
	}
	return BranchNames{Loop: loop, Task: task, Prefix: prefix}, nil
}

// TaskBranch is the per-task branch: unique per (loop, task), derived,
// never re-derived by anyone else.
func (n BranchNames) TaskBranch() string { return n.Prefix + "task/" + n.Loop + "/" + n.Task }

// FeatureBranch is the integration branch task PRs target.
func (n BranchNames) FeatureBranch() string { return n.Prefix + "feature/" + n.Loop }

// IntegrationBranch names the queue's merge-forward branch for ONE item
// (MQ6: integrate/<item>, the item id validated exactly like every other
// name part). Used from T5; built here so the prefix rule has one home.
func (n BranchNames) IntegrationBranch(item string) (string, error) {
	if !identity.MatchString(item) {
		return "", fmt.Errorf("item identity %q is not a safe slug — integration branches are built from validated identities (L3)", item)
	}
	return n.Prefix + "integrate/" + item, nil
}

// BranchWhich selects the branch an action concerns. A named string, not a
// bool: two branches, one door, and the call site reads as English.
type BranchWhich string

const (
	BranchTask    BranchWhich = "task"
	BranchFeature BranchWhich = "feature"
)

// GitResult is the discriminated outcome of a git action. It SAYS which
// check short-circuited it — a success that cannot say why is a different
// truth from one that can (the same reason Verdict carries By).
type GitResult struct {
	Action          string
	Branch          string
	SHA             string
	Reused          bool
	NothingToCommit bool
	Pushed          bool
	Merged          bool
	AlreadyMerged   bool
	// Dir is the directory an integration result acted in (S60 T5): the
	// worktree git.integrate merged in, the tree publish pushed from. Empty
	// everywhere else — the pre-S60 actions never report a directory, and
	// their goldens stay byte-identical.
	Dir string
}

// CreateBranch is check-then-act (local class — it affects nobody until a
// push says otherwise). The named checks: show-ref local, rev-parse
// origin/X remote. Absent everywhere → create; present either side →
// REUSE, never create-then-fail or create-then-force.
func (a *GitActions) CreateBranch(ctx context.Context, dir string, names BranchNames, which BranchWhich) (GitResult, error) {
	return a.CreateBranchFrom(ctx, dir, names, which, "")
}

// CreateBranchFrom is CreateBranch with the base S60 MQ15 pins per loop:
// when base names a branch, the new branch is CUT FROM IT (`origin/<base>`
// when the remote ref exists, else the local one); when base is empty the
// cut is today's — from HEAD. Reuse checks run first, exactly as before: a
// branch that exists anywhere is reused, never re-cut from under its
// commits.
func (a *GitActions) CreateBranchFrom(ctx context.Context, dir string, names BranchNames, which BranchWhich, base string) (GitResult, error) {
	branch, err := names.branch(which)
	if err != nil {
		return GitResult{}, err
	}
	c := a.client()
	local, err := c.BranchExistsLocal(ctx, dir, branch)
	if err != nil {
		return GitResult{}, err
	}
	if local {
		if which == BranchTask {
			if _, err := c.Run(ctx, dir, "checkout", branch); err != nil {
				return GitResult{}, err
			}
		}
		return GitResult{Action: GitCreateBranch, Branch: branch, Reused: true}, nil
	}
	remote, err := c.BranchExistsRemote(ctx, dir, branch)
	if err != nil {
		return GitResult{}, err
	}
	if remote {
		return GitResult{Action: GitCreateBranch, Branch: branch, Reused: true}, nil
	}
	if base != "" {
		ref := base
		if remote, err := c.BranchExistsRemote(ctx, dir, base); err != nil {
			return GitResult{}, err
		} else if remote {
			ref = "origin/" + base // the remote tip when present — the base as the fleet sees it
		}
		if _, err := c.Run(ctx, dir, "branch", branch, ref); err != nil {
			return GitResult{}, err
		}
	} else if _, err := c.Run(ctx, dir, "branch", branch); err != nil {
		return GitResult{}, err
	}
	// A task branch is the task's workspace: arriving on it is part of
	// creating it. The FEATURE branch is never checked out here — it is
	// shared, and yanking a workspace onto it would be the sibling of the
	// deletion incident.
	if which == BranchTask {
		if _, err := c.Run(ctx, dir, "checkout", branch); err != nil {
			return GitResult{}, err
		}
	}
	return GitResult{Action: GitCreateBranch, Branch: branch}, nil
}

// CommitAndPush is OUTWARD (T2) and therefore asks before running; once
// permitted, the checks are T3's: stage everything, assert the tree
// (nothing hand-picked survives), and `nothing to commit` is SUCCESS (D2 —
// a step that has nothing left to do is done, not failed). The push is
// skipped entirely when the remote already reads local HEAD (an
// already-current ref is git's no-op; skipping it is the check-then-act
// form) and verified by re-reading when it runs (O8).
//
// The idempotency check is the tree and the head, NEVER the message: a
// second commit carrying the same message is a new SHA, not no commit.
func (a *GitActions) CommitAndPush(ctx context.Context, dir string, names BranchNames, message string) (GitResult, error) {
	branch := names.TaskBranch()
	c := a.client()

	// The recorded branch-inheritance bug, structurally: the action runs
	// ON its task branch or refuses naming both. Work committed on a
	// branch nobody reviews is the bug class, and a check that the tree
	// stands where the identity says it must is the other half of
	// constructing the names in code.
	current, err := c.CurrentBranch(ctx, dir)
	if err != nil {
		return GitResult{}, err
	}
	if current != branch {
		return GitResult{}, &git.GitError{Kind: git.KindRefused, Detail: fmt.Sprintf(
			"commitAndPush works on its task branch: %q is checked out, %q is the task — arriving there is CreateBranch's job, never a silent commit where nobody is looking", current, branch)}
	}

	head, err := c.LocalHEAD(ctx, dir)
	if err != nil {
		return GitResult{}, err
	}
	// Check the remote BEFORE committing anything: a clean tree with the
	// remote current is the finished state, and the honest report of it is
	// nothing-to-commit — not an error to be redone.
	sha, nothing, err := c.CommitEverything(ctx, dir, message)
	if err != nil {
		return GitResult{}, err
	}
	if nothing {
		// Work already in: verify the remote agrees, and only push if not.
		remote, err := c.RemoteRefSHA(ctx, dir, branch)
		if err != nil {
			return GitResult{}, err
		}
		if remote == head {
			return GitResult{Action: GitCommitAndPush, Branch: branch, NothingToCommit: true}, nil
		}
		if err := c.Push(ctx, dir, branch); err != nil {
			return GitResult{}, err
		}
		return GitResult{Action: GitCommitAndPush, Branch: branch, SHA: head, NothingToCommit: true, Pushed: true}, nil
	}
	if err := c.Push(ctx, dir, branch); err != nil {
		return GitResult{}, err
	}
	return GitResult{Action: GitCommitAndPush, Branch: branch, SHA: sha, Pushed: true}, nil
}

// MergeBranch keeps the task branch current with the feature branch (local
// class — nobody outside the tree sees it). The named check: merge-base
// --is-ancestor feature task, exit 0 ⇒ already merged ⇒ success WITHOUT
// touching anything. Only a genuinely behind branch merges.
func (a *GitActions) MergeBranch(ctx context.Context, dir string, names BranchNames) (GitResult, error) {
	c := a.client()
	feature := names.FeatureBranch()
	task := names.TaskBranch()
	featureSHA, err := c.LocalHEADFor(ctx, dir, feature)
	if err != nil {
		return GitResult{}, err
	}
	ancestor, err := c.IsAncestor(ctx, dir, featureSHA, task)
	if err != nil {
		return GitResult{}, err
	}
	if ancestor {
		return GitResult{Action: GitMergeBranch, Branch: task, AlreadyMerged: true}, nil
	}
	if _, err := c.Run(ctx, dir, "checkout", task); err != nil {
		return GitResult{}, err
	}
	if _, err := c.Run(ctx, dir, "merge", "--no-edit", feature); err != nil {
		var ge *git.GitError
		if asGitErr(err, &ge) && ge.Kind == git.KindExit && strings.Contains(ge.Detail, "CONFLICT") {
			return GitResult{}, &git.GitError{Kind: git.KindConflict, Detail: "merging " + feature + " into " + task + ": " + ge.Detail}
		}
		return GitResult{}, err
	}
	return GitResult{Action: GitMergeBranch, Branch: task, Merged: true}, nil
}

// AddWorktree and RemoveWorktree are S32's PA5 worktree door — the two new
// GitActions methods the design named. They are daemon-internal (the loop
// engine's own goroutine calls them, once per concurrently-dispatched unit),
// NOT user-facing code actions: they are deliberately absent from the closed
// `classes` registry (class.go), which is the eight-plus-audit set `aeon
// action check` classifies, and adding to it would break the count the
// registry pins. Their effect is LOCAL by nature (a worktree is a directory
// on this machine, visible to nobody outside the tree), the same class as
// CreateBranch — but the registry is not where that is recorded.
//
// AddWorktree is idempotent against an already-existing branch the way
// CreateBranch's Reused path is (PA12's recovery re-creates a worktree a
// crash left behind), and the base is the FEATURE branch's tip at creation
// (PA5) — the engine records that SHA as loop_worktree.base_sha and never
// rebases it mid-run (brief §6: merge conflicts resolve at the forge).
func (a *GitActions) AddWorktree(ctx context.Context, dir, path string, names BranchNames) (GitResult, error) {
	branch := names.TaskBranch()
	c := a.client()
	if local, err := c.BranchExistsLocal(ctx, dir, branch); err != nil {
		return GitResult{}, err
	} else if local {
		// The branch exists: the worktree is already made (or a prior add
		// created it) — the Reused path, never create-then-fail.
		return GitResult{Action: "git.addWorktree", Branch: branch, Reused: true}, nil
	}
	// base = the feature branch's tip at the moment the worktree is created
	// (PA5). Absent feature branch → the current HEAD. This is the SHA the
	// loop_worktree row records and the base a later audit reads.
	base, err := c.LocalHEADFor(ctx, dir, names.FeatureBranch())
	if err != nil {
		base = ""
	}
	if base == "" {
		base, err = c.LocalHEAD(ctx, dir)
		if err != nil {
			return GitResult{}, err
		}
	}
	args := []string{"worktree", "add", path, "-b", branch}
	if base != "" {
		args = append(args, base)
	}
	if _, err := c.Run(ctx, dir, args...); err != nil {
		return GitResult{}, err
	}
	return GitResult{Action: "git.addWorktree", Branch: branch, SHA: base}, nil
}

// RemoveWorktree is PA5's step 4's git half: after the unit settles, the
// worktree is removed and the loop_worktree row's removed_at is set. A
// missing worktree is a success, not a fault — the row's removed_at is the
// settle-once signal, and a second removal is a no-op, exactly the
// discipline RemoveLoopWorktree's guarded write keeps.
func (a *GitActions) RemoveWorktree(ctx context.Context, dir, path string) (GitResult, error) {
	c := a.client()
	if _, err := c.Run(ctx, dir, "worktree", "remove", "--force", path); err != nil {
		return GitResult{}, nil
	}
	return GitResult{Action: "git.removeWorktree"}, nil
}

func (n BranchNames) branch(which BranchWhich) (string, error) {
	switch which {
	case BranchTask:
		return n.TaskBranch(), nil
	case BranchFeature:
		return n.FeatureBranch(), nil
	default:
		return "", fmt.Errorf("unknown branch %q — the doors are task and feature", which)
	}
}

func asGitErr(err error, out **git.GitError) bool {
	if err == nil {
		return false
	}
	if ge, ok := err.(*git.GitError); ok {
		*out = ge
		return true
	}
	return false
}
