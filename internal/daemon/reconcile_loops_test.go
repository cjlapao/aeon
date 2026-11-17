package daemon

// S32 T6 (PA12): the daemon side of the worktree reconciliation. The core
// (reattach / rebuild / cleanup) is tested in the loop package against a fake
// git; these tests own what the daemon ADAPTS and the loop package cannot —
// the real `git worktree list --porcelain` seam, the per-loop pass, and the
// storeless / no-row edges. A real store (the loop_worktree ledger is the
// primary artifact) plus real git (the daemon's own git client) so the seam is
// measured against git's actual output, not a simulated flag.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// daemonStore opens the daemon's store at a throwaway directory — never the
// real store (§5: tests never point at the real store).
func daemonStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// gitAddWorktree runs the real `git worktree add` so a test can assert the
// daemon's `git worktree list --porcelain` seam against a worktree git itself
// made — the daemon owns the read, not the write, but the read has to be
// measured against a real `git worktree list`, not a comment.
func gitAddWorktree(t *testing.T, repo, path, branch string) {
	t.Helper()
	c := exec.Command("git", "worktree", "add", path, "-b", branch)
	c.Dir = repo
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add %s -b %s: %v\n%s", path, branch, err, out)
	}
}

// makeReconcileRow makes the loop row a worktree row's FK points to, a task
// row at the given status, and one live loop_worktree row pointing at path.
func makeReconcileRow(t *testing.T, s *store.Store, slug, root, ownerID, path, status string) {
	t.Helper()
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "g", Mode: "standard"}); err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: ownerID, Status: store.TaskStatus(status)}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.CreateLoopWorktree(store.LoopWorktree{
		ComponentRoot: root, LoopSlug: slug, OwnerKind: "task", OwnerID: ownerID, Path: path, Branch: "b-" + ownerID,
	}); err != nil {
		t.Fatalf("CreateLoopWorktree: %v", err)
	}
}

// TestReconcileLoopsStorelessAnswersZero — a storeless daemon has no loops to
// reconcile and answers zero, not an error: a daemon that cannot read its rows
// cannot reconcile them, and the honest answer is nothing-to-do, not a panic.
func TestReconcileLoopsStorelessAnswersZero(t *testing.T) {
	d := &Daemon{}
	n, err := d.ReconcileLoops(context.Background())
	if err != nil {
		t.Fatalf("storeless ReconcileLoops must not error: %v", err)
	}
	if n != 0 {
		t.Fatalf("storeless ReconcileLoops reconciles nothing: got %d, want 0", n)
	}
}

// TestReconcileLoopsCountsLoopsWithNoLiveRows — the per-loop pass counts a
// loop whose worktree ledger is empty as reconciled (the worktree-list seam is
// never asked, so a non-git component root is not an error) and returns the
// count without a fatal. A loop with no concurrent work has no worktrees.
func TestReconcileLoopsCountsLoopsWithNoLiveRows(t *testing.T) {
	s := daemonStore(t)
	d := &Daemon{store: s}
	for _, slug := range []string{"a", "b"} {
		root := t.TempDir() // empty, not a git dir — fine: no live row, git is never asked
		if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "g", Mode: "standard"}); err != nil {
			t.Fatalf("CreateLoop %s: %v", slug, err)
		}
	}
	n, err := d.ReconcileLoops(context.Background())
	if err != nil {
		t.Fatalf("ReconcileLoops with no live rows must not error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected both loops counted, got %d", n)
	}
}

// TestWorktreeLiveReadsRealGitWorktreeList — the seam the daemon owns: it reads
// real `git worktree list --porcelain` and answers, per path, whether git has
// that worktree live. A worktree git made is live; a path git never named is
// not; the main worktree is live too. The parse (only `worktree ` lines name a
// path) is measured against git's actual output, not a flag.
func TestWorktreeLiveReadsRealGitWorktreeList(t *testing.T) {
	s := daemonStore(t)
	d := &Daemon{store: s}
	repo := gitInitWithOrigin(t, "https://github.com/acme/widget.git")
	added := filepath.Join(t.TempDir(), "wt-added")
	gitAddWorktree(t, repo, added, "branch-added")

	has := d.worktreeLive(context.Background(), repo)
	if live, err := has(context.Background(), repo); err != nil || !live {
		t.Fatalf("the main worktree must read live: live=%v err=%v", live, err)
	}
	if live, err := has(context.Background(), added); err != nil || !live {
		t.Fatalf("the added worktree must read live: live=%v err=%v", live, err)
	}
	if live, err := has(context.Background(), filepath.Join(t.TempDir(), "not-a-worktree")); err != nil || live {
		t.Fatalf("a path git never named must read not-live: live=%v err=%v", live, err)
	}
}

// TestReconcileLoopsReattachesRealWorktree — the full chain through the daemon
// adapter with real git: a live row whose path git agrees is live survives
// (reattach), the row is not settled, and the worktree is not re-added. The
// position's task is pending (not terminal), so the seam is asked and answers
// "git has it" — the reconciliation the loop package tested only against a
// fake git, now measured against real `git worktree list`.
func TestReconcileLoopsReattachesRealWorktree(t *testing.T) {
	s := daemonStore(t)
	d := &Daemon{store: s}
	repo := gitInitWithOrigin(t, "https://github.com/acme/widget.git")
	wt := filepath.Join(t.TempDir(), "wt-reattach")
	gitAddWorktree(t, repo, wt, "b-t1")
	makeReconcileRow(t, s, "p", repo, "t1", wt, "pending")

	n, err := d.ReconcileLoops(context.Background())
	if err != nil {
		t.Fatalf("ReconcileLoops: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the one loop reconciled, got %d", n)
	}
	sc := store.Scope{LoopSlug: "p", ComponentRoot: repo}
	live, err := s.ListLiveLoopWorktrees(sc)
	if err != nil {
		t.Fatalf("ListLiveLoopWorktrees: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("reattach must not settle the live row; live=%d", len(live))
	}
	// The worktree git made still exists — reattach keeps it, never re-adds it.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the reattached worktree must still exist: %v", err)
	}
}

// TestReconcileLoopsCleansUpSettledPosition — the full chain's cleanup case: a
// live row whose position reached a terminal status (a unit that settled but
// whose worktree was never removed) is settled — the row's removed_at is set,
// the real worktree is removed, and no position is recovered. The terminal read
// is the store's (a complete task row), not a flag.
func TestReconcileLoopsCleansUpSettledPosition(t *testing.T) {
	s := daemonStore(t)
	d := &Daemon{store: s}
	repo := gitInitWithOrigin(t, "https://github.com/acme/widget.git")
	wt := filepath.Join(t.TempDir(), "wt-cleanup")
	gitAddWorktree(t, repo, wt, "b-t1")
	makeReconcileRow(t, s, "p", repo, "t1", wt, "complete") // terminal status

	n, err := d.ReconcileLoops(context.Background())
	if err != nil {
		t.Fatalf("ReconcileLoops: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the one loop reconciled, got %d", n)
	}
	sc := store.Scope{LoopSlug: "p", ComponentRoot: repo}
	live, err := s.ListLiveLoopWorktrees(sc)
	if err != nil {
		t.Fatalf("ListLiveLoopWorktrees: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("cleanup must settle the row (removed_at set); live=%d", len(live))
	}
	// The worktree git made is gone — the real `git worktree remove` ran.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the worktree; still present (err=%v)", err)
	}
}
