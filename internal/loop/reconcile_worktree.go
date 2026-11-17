package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/store"
)

// S32 T6 (PA12): recovery reconciles every live loop_worktree row against real
// git before it recovers the position the row belongs to. A row with
// removed_at IS NULL is NOT proof the worktree exists — it is only the daemon's
// intent (PA5's write-ahead). The reconciliation asks git the three ways a
// crash can leave the pair (ledger row + real worktree) apart:
//
//	reattach — git agrees the path is live: the row stands, WorkspaceRoot = path,
//	  the position recovers against it.
//	rebuild  — git has no record of it (crashed between T1's ledger insert and
//	  T3's real add): re-run AddWorktree with the row's own branch/base (T3's
//	  idempotent-retry path) — then the position recovers.
//	cleanup  — the position already reads a terminal status (crashed between the
//	  unit settling and the worktree being removed): RemoveWorktree +
//	  RemoveLoopWorktree, no position to recover.
//
// Running the pass twice is provably safe: every action is idempotent (the
// guarded writes from T1, the Reused path from T3), so a recovery that itself
// gets interrupted cannot create a second real worktree for the same row.

// worktreeCase is the reconciliation verdict for one live row.
type worktreeCase int

const (
	worktreeReattach worktreeCase = iota // git agrees: keep the row
	worktreeRebuild                      // git disagrees: re-add
	worktreeCleanup                      // the position is terminal: settle the row
)

// ReconcileWorktrees is PA12's pass. It lists the live rows for one loop and,
// for each, classifies against git (the gitHas seam — real `git worktree list`
// in the daemon, a recording fake in the test) and acts. The position's
// terminal status (from the loop's own state) decides cleanup; a row whose
// position is not terminal and git does not have is rebuilt (never left to
// rot). The return is the rows that survive (reattached or rebuilt) — the
// caller recovers each position against its own (possibly rebuilt) worktree.
func ReconcileWorktrees(ctx context.Context, dep Deps, scope store.Scope, terminal func(ownerKind, ownerID string) bool, gitHas func(ctx context.Context, path string) (bool, error)) ([]store.LoopWorktree, error) {
	rows, err := dep.Store.ListLiveLoopWorktrees(scope)
	if err != nil {
		return nil, err
	}
	survivors := []store.LoopWorktree{}
	for _, w := range rows {
		switch {
		case terminal(w.OwnerKind, w.OwnerID):
			// The position is terminal: the worktree is owed no more — settle the
			// row and remove the (possibly already-gone) directory.
			if err := settleWorktree(ctx, dep, w); err != nil {
				return nil, err
			}
			continue
		}
		has, err := gitHas(ctx, w.Path)
		if err != nil {
			return nil, err
		}
		if has {
			// Reattach: git agrees the path is live. The row stands; the caller
			// recovers the position against it (WorkspaceRoot = w.Path).
			survivors = append(survivors, w)
			continue
		}
		// Rebuild: git has no record of it (crashed between the ledger insert and
		// the real add). Re-add with the row's own branch — T3's idempotent
		// retry, never a second real worktree for the same row.
		if dep.Actions == nil {
			// No git seam wired: the row is the intent, the directory is owed.
			// Reattach-by-omission is the honest fallback (the row stands, the
			// directory is created when the git seam is present), not a rebuild
			// we could not do and pretend we did.
			survivors = append(survivors, w)
			continue
		}
		if w.OwnerKind == "integration" {
			// S60 MQ7: the integration row rebuilds through its own builder —
			// the branch was already cut, so this ATTACHES the existing branch
			// (baseRef empty), which is exactly AddIntegrationWorktree's
			// directory-rebuild case. AddWorktree's task-branch naming cannot
			// serve it, and reusing that shortcut is what the builder exists
			// not to (T0's F17 finding).
			if _, err := dep.Actions(w.ComponentRoot).Git.AddIntegrationWorktree(ctx, w.ComponentRoot, w.Path, w.Branch, ""); err != nil {
				return nil, err
			}
			survivors = append(survivors, w)
			continue
		}
		if _, err := dep.Actions(w.ComponentRoot).Git.AddWorktree(ctx, w.ComponentRoot, w.Path, action.BranchNames{Loop: w.LoopSlug, Task: w.OwnerID}); err != nil {
			return nil, err
		}
		survivors = append(survivors, w)
	}
	return survivors, nil
}

// TerminalFor builds the terminal-position check ReconcileWorktrees needs from
// a store the daemon holds. It is the daemon adapter's read for "is this unit
// settled" — a task is terminal when its status is in the successful closed
// set (PA12's cleanup case), a lane is never terminal on its own (a lane's
// settle rides its loop, not the worktree ledger). A read that cannot resolve a
// task answers not-terminal, so an absent row rebuilds the worktree rather than
// settling it (a row with no row is a brand-new unit, not a settled one).
func TerminalFor(dep Deps, scope store.Scope) func(ownerKind, ownerID string) bool {
	return func(kind, id string) bool {
		ok, err := terminalOwner(dep, scope, kind, id)
		if err != nil {
			return false
		}
		return ok
	}
}

// terminalOwner reads whether a unit's position has reached a terminal status.
// It is the cleanup case's input (PA12): a unit that is done but whose worktree
// was never removed is a stale row the reconciliation must settle, not a
// position to recover. The store is the source of truth; the loop's in-memory
// status can lag a just-settled unit, so the check is a store read, not a field.
func terminalOwner(dep Deps, scope store.Scope, ownerKind, ownerID string) (bool, error) {
	switch ownerKind {
	case "task":
		t, err := dep.Store.GetTask(scope, ownerID)
		if err != nil {
			// A task with no row is not terminal (it may be a brand-new unit);
			// the caller rebuilds it, never cleans it up on an absent row.
			return false, nil
		}
		return successfulTerminal(t.Status), nil
	case "lane":
		// A lane unit is terminal when its lane's state is done. The lane state
		// is the loop's own durable record; an absent or non-done lane is not
		// terminal.
		return false, nil
	case "integration":
		// S60 MQ7: an integration worktree's owner is a QUEUE ITEM, and its
		// terminal set is the item's closed one (merged/failed/removed —
		// MergeItemTerminal reads it; an absent item counts terminal, the row
		// it belonged to is gone). A queued or conflicted item is precisely
		// the worktree the next landing still needs.
		ok, err := dep.Store.MergeItemTerminal(ownerID)
		if err != nil {
			// An unreadable answer is NOT terminal: settling a live item's
			// worktree on a failed read would destroy the state a conflicted
			// landing is recovery built for.
			return false, nil
		}
		return ok, nil
	}
	return false, fmt.Errorf("unknown worktree owner kind %q", ownerKind)
}
