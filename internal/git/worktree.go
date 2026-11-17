package git

// Worktree mode (D6, Q2 answered here). `paseo.json` at the repository
// root selects `existing-worktree`, and what changes is this: the feature
// branch is a PRE-EXISTING worktree branch somebody else (Paseo) owns.
//
//   - NEVER created. A createBranch that always creates breaks every
//     worktree loop, because the branch already exists and carries a
//     workspace's identity.
//   - NEVER deleted. A cleanup that deletes it destroys a workspace Paseo
//     owns — the recorded incident this mode exists to prevent.
//   - Usually LOCAL-ONLY, so it must be pushed before the first task PR,
//     or the PR has no base to target.
//
// The mode reads the ground (existence both sides) and pushes when the
// remote has never seen the branch. It creates and deletes nothing.

import (
	"context"
	"fmt"
)

// Mode is how the feature branch is expected to exist.
type Mode string

const (
	// ModeStandard creates the feature branch when absent (T4's
	// git.createBranch owns creating task branches).
	ModeStandard Mode = "standard"

	// ModeExistingWorktree is the paseo.json mode: the feature branch is
	// pre-existing and external — never created, never deleted.
	ModeExistingWorktree Mode = "existing-worktree"
)

// PrepareFeatureBranch brings the FEATURE branch to a state where a task
// PR can target it. In existing-worktree mode that means exactly one
// possible action — a first push of a local-only branch, verified (O8).
func (c *Client) PrepareFeatureBranch(ctx context.Context, dir string, mode Mode, branch string) error {
	if branch == "" {
		return fmt.Errorf("prepare: the feature branch name is required")
	}
	local, err := c.BranchExistsLocal(ctx, dir, branch)
	if err != nil {
		return err
	}
	switch mode {
	case ModeExistingWorktree:
		if !local {
			return &GitError{Kind: KindRefused, Detail: fmt.Sprintf(
				"existing-worktree mode never creates %q (D6) — the branch is Paseo's to create; Aeon may only use what is already checked out", branch)}
		}
		remote, err := c.BranchExistsRemote(ctx, dir, branch)
		if err != nil {
			return err
		}
		if !remote {
			// Usually local-only: push before it is needed as a PR base.
			// Plain -u, never forced (N8), and verified by re-reading (O8).
			return c.Push(ctx, dir, branch)
		}
		return nil

	case ModeStandard:
		if local {
			return nil
		}
		remote, err := c.BranchExistsRemote(ctx, dir, branch)
		if err != nil {
			return err
		}
		if remote {
			// It exists where it needs to; creating it locally would be
			// the other half of the incident, in reverse.
			return nil
		}
		_, err = c.exec(ctx, dir, "branch", branch)
		return err

	default:
		return fmt.Errorf("prepare: unknown worktree mode %q — the modes are standard and existing-worktree", mode)
	}
}
