package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoGitClient is the honest absence for a workspace the daemon's git client
// cannot list: the reconciliation cannot read its git state, so it must not
// pretend nothing is live (that would rebuild every row on a guess).
var errNoGitClient = errors.New("no git client for the workspace")

// S32 T6 (PA12): the daemon side of the worktree reconciliation. The core
// (reattach / rebuild / cleanup) lives in loop.ReconcileWorktrees — it is the
// tested piece, driven by a real store and a fake git. This file is the adapter
// that feeds it the two seams the loop package cannot own: a real `git worktree
// list` (the daemon's git client) and the per-position terminal read (the store
// the daemon holds). It runs once per loop before the per-step recovery walk,
// so a crashed loop's positions are reconciled against real git before any of
// them is recovered — a position whose worktree is gone is rebuilt, one whose
// worktree is live is reattached, and a settled one is cleared.

// ReconcileLoops is the per-loop PA12 pass. It walks every loop the daemon
// holds, reconciles that loop's live loop_worktree rows against real git, and
// returns how many loops it reconciled. A storeless daemon has no rows to
// reconcile and answers zero, not an error. A loop whose workspace cannot be
// listed by the daemon's git client is skipped with a named failure, not a
// crash — a daemon that cannot read a workspace's git state cannot reconcile
// it, and the honest answer is to skip, never to guess a worktree is live.
func (d *Daemon) ReconcileLoops(ctx context.Context) (int, error) {
	if d.store == nil {
		return 0, nil
	}
	loops, err := d.store.ListLoops(store.ComponentScope{})
	if err != nil {
		return 0, err
	}
	dep := loop.Deps{Store: d.store, Actions: d.actionServices}
	reconciled := 0
	for _, l := range loops {
		scope := store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}
		if !scope.Valid() {
			// A loop with no component root has no worktree rows to reconcile;
			// skipping is the honest answer, not a failure.
			continue
		}
		if _, err := loop.ReconcileWorktrees(ctx, dep, scope,
			d.worktreeTerminal(dep, scope), d.worktreeLive(ctx, l.ComponentRoot)); err != nil {
			return reconciled, err
		}
		reconciled++
	}
	return reconciled, nil
}

// worktreeTerminal is PA12's terminal-position check with the merge queue
// joined (S60 T7 behaviour 9): the loop package's own rule for unit rows
// (a settled task's worktree is owed cleanup), plus the queue's shape —
// an `integration` row is terminal when the QUEUE ITEM itself is (merged,
// failed, removed). An item that cannot be read answers not-terminal: the
// reconciliation rebuilds the worktree rather than settle one an item may
// still need (PA12's own absent-row rule, carried).
func (d *Daemon) worktreeTerminal(dep loop.Deps, scope store.Scope) func(ownerKind, ownerID string) bool {
	base := loop.TerminalFor(dep, scope)
	return func(kind, id string) bool {
		if kind == "integration" && d.store != nil {
			it, err := d.store.ReadMergeItem(id)
			if err != nil {
				return false
			}
			return mergequeue.Status(it.Status).IsTerminal()
		}
		return base(kind, id)
	}
}

// worktreeLive is the gitHas seam ReconcileWorktrees asks: it reads `git
// worktree list --porcelain` once for the loop's workspace and answers whether
// a given path is a live worktree. The list is read once and cached for the
// whole reconciliation pass — a loop's worktrees are read together, not per row
// — so a crash between the ledger insert and the real add (the rebuild case)
// is measured against the workspace's actual `git worktree list`, never a
// simulated flag. A workspace the daemon cannot list is a named failure, not a
// "nothing is live" that would rebuild every row.
func (d *Daemon) worktreeLive(ctx context.Context, root string) func(context.Context, string) (bool, error) {
	svc := d.actionServices(root)
	if svc == nil || svc.Git == nil || svc.Git.Git == nil {
		// No git client for this workspace: the reconciliation cannot read its
		// git state. The honest answer is a failure the caller names, never a
		// false "nothing is live" that would rebuild every row on a guess.
		return func(context.Context, string) (bool, error) {
			return false, errNoGitClient
		}
	}
	out, err := svc.Git.Git.Run(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return func(context.Context, string) (bool, error) {
			return false, err
		}
	}
	live := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		// --porcelain emits one `worktree <path>` line per worktree; the rest
		// (HEAD, branch, bare, locked) are the worktree's own state, not its
		// path, so only the `worktree ` prefix names a path.
		if p := strings.TrimPrefix(line, "worktree "); p != line {
			live[p] = true
		}
	}
	return func(ctx context.Context, path string) (bool, error) {
		return live[path], nil
	}
}
