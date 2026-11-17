package loop

import (
	"context"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/store"
)

// S32 T6 (PA12): the reconciliation tests. A real store (the loop_worktree
// ledger is the primary artifact) plus a fake git that the daemon would
// replace with `git worktree list` — a set of "live" paths the fake git
// add/remove mutates, so "reattach / rebuild / cleanup" is a measured fact
// against real git state, not a simulated crash flag.

// fakeGit is the PA12 reconciliation's git seam: a set of live paths the
// fake `git worktree add`/`remove` mutate, and a `gitHas` reader the
// reconciliation asks. It records the add/remove calls so a test can assert
// idempotency (never a second real worktree for the same row).
type fakeGit struct {
	mu      sync.Mutex
	live    map[string]bool
	adds    int
	removes int
}

func newFakeGit(live ...string) *fakeGit {
	f := &fakeGit{live: map[string]bool{}}
	for _, p := range live {
		f.live[p] = true
	}
	return f
}

func (f *fakeGit) has(ctx context.Context, path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live[path], nil
}

func (f *fakeGit) exec() git.Exec {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch args[0] {
		case "worktree":
			switch args[1] {
			case "add":
				f.adds++
				if len(args) >= 3 {
					f.live[args[2]] = true
				}
			case "remove":
				f.removes++
				delete(f.live, args[len(args)-1])
			}
			return "", nil
		case "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case "rev-parse":
			return "sha-fake", nil
		}
		return "", nil
	}
}

// makeLiveRow inserts one live loop_worktree row for a unit and returns it. The
// loop row must exist first (the worktree row has an FK to loop) — createLoopRow
// makes it idempotently.
func makeLiveRow(t *testing.T, s *store.Store, scope store.Scope, kind, id, path string) store.LoopWorktree {
	t.Helper()
	createLoopRow(t, s, scope)
	w, err := s.CreateLoopWorktree(store.LoopWorktree{
		ComponentRoot: scope.ComponentRoot, LoopSlug: scope.LoopSlug, OwnerKind: kind, OwnerID: id, Path: path, Branch: "b-" + id,
	})
	if err != nil {
		t.Fatalf("CreateLoopWorktree: %v", err)
	}
	return w
}

// createLoopRow makes the loop row a worktree row's FK points to. A second call
// (the loop already exists) is a no-op-friendly ignore: the FK only needs the
// (component_root, slug) pair present once.
func createLoopRow(t *testing.T, s *store.Store, scope store.Scope) {
	t.Helper()
	if _, err := s.CreateLoop(store.NewLoop{Slug: scope.LoopSlug, ComponentRoot: scope.ComponentRoot, Root: scope.ComponentRoot, Goal: "g", Mode: "standard"}); err != nil {
		// A second create (duplicate) is fine for the FK's purposes; a real
		// error names itself.
		t.Logf("CreateLoop (expected once): %v", err)
	}
}

// TestRecoveryReattachesLiveWorktree: a live row whose path git agrees is live
// survives the reconciliation (reattach) — the position recovers against it,
// and the row is not rebuilt (no second worktree) nor settled.
func TestRecoveryReattachesLiveWorktree(t *testing.T) {
	s := openLoopStore(t)
	sc := store.Scope{LoopSlug: "r", ComponentRoot: "/repo"}
	w := makeLiveRow(t, s, sc, "lane", "a", "/repo/.wt/a")
	fg := newFakeGit("/repo/.wt/a") // git agrees the path is live
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: fg.exec()}}}
	}}
	survivors, err := ReconcileWorktrees(context.Background(), dep, sc, func(string, string) bool { return false }, fg.has)
	if err != nil {
		t.Fatalf("ReconcileWorktrees: %v", err)
	}
	if len(survivors) != 1 || survivors[0].ID != w.ID {
		t.Fatalf("reattach: expected the live row to survive, got %v", survivors)
	}
	if fg.adds != 0 {
		t.Fatalf("reattach must not re-add an existing worktree; adds=%d", fg.adds)
	}
	live, err := s.ListLiveLoopWorktrees(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("reattach must not settle the live row; live=%d", len(live))
	}
}

// TestRecoveryRebuildsWorktreeCrashedBeforeAdd: a live row with NO matching
// `git worktree list` entry (crashed between the ledger insert and the real
// add) is rebuilt from its branch — then survives. A crash between T1's
// CreateLoopWorktree and T3's AddWorktree is the case this exists for.
func TestRecoveryRebuildsWorktreeCrashedBeforeAdd(t *testing.T) {
	s := openLoopStore(t)
	sc := store.Scope{LoopSlug: "r", ComponentRoot: "/repo"}
	w := makeLiveRow(t, s, sc, "lane", "b", "/repo/.wt/b")
	fg := newFakeGit() // git has no record of the path
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: fg.exec()}}}
	}}
	if _, err := ReconcileWorktrees(context.Background(), dep, sc, func(string, string) bool { return false }, fg.has); err != nil {
		t.Fatalf("ReconcileWorktrees: %v", err)
	}
	if fg.adds != 1 {
		t.Fatalf("rebuild must add the missing worktree exactly once; adds=%d", fg.adds)
	}
	if !fg.live["/repo/.wt/b"] {
		t.Fatalf("rebuild must leave the worktree live")
	}
	live, err := s.ListLiveLoopWorktrees(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("rebuild must not settle the row; live=%d", len(live))
	}
	_ = w
}

// TestRecoveryCleansUpAlreadySettledPosition: a row whose position already
// reached a terminal status (crashed between the unit settling and the
// worktree being removed) is settled — the row's removed_at is set, the
// worktree is removed, and no position is recovered.
func TestRecoveryCleansUpAlreadySettledPosition(t *testing.T) {
	s := openLoopStore(t)
	sc := store.Scope{LoopSlug: "r", ComponentRoot: "/repo"}
	w := makeLiveRow(t, s, sc, "lane", "c", "/repo/.wt/c")
	fg := newFakeGit("/repo/.wt/c") // git still has the (stale) worktree
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: fg.exec()}}}
	}}
	// The position is terminal → cleanup.
	if _, err := ReconcileWorktrees(context.Background(), dep, sc, func(kind, id string) bool { return kind == "lane" && id == "c" }, fg.has); err != nil {
		t.Fatalf("ReconcileWorktrees: %v", err)
	}
	if fg.removes != 1 {
		t.Fatalf("cleanup must remove the stale worktree; removes=%d", fg.removes)
	}
	live, err := s.ListLiveLoopWorktrees(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("cleanup must settle the row (removed_at set); live=%d", len(live))
	}
	_ = w
}

// TestRecoveryNeverDoublesAWorktree: running the reconciliation twice (a
// recovery that itself gets interrupted, then retried) never creates a second
// real worktree for the same row. The first run rebuilds; the second sees the
// worktree live and reattaches — the idempotency from T1/T3 composes across a
// second pass.
func TestRecoveryNeverDoublesAWorktree(t *testing.T) {
	s := openLoopStore(t)
	sc := store.Scope{LoopSlug: "r", ComponentRoot: "/repo"}
	makeLiveRow(t, s, sc, "lane", "d", "/repo/.wt/d")
	fg := newFakeGit() // git starts with no record
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: fg.exec()}}}
	}}
	// Pass 1: rebuild (git has no record).
	if _, err := ReconcileWorktrees(context.Background(), dep, sc, func(string, string) bool { return false }, fg.has); err != nil {
		t.Fatalf("ReconcileWorktrees pass 1: %v", err)
	}
	if fg.adds != 1 {
		t.Fatalf("pass 1 must add exactly once; adds=%d", fg.adds)
	}
	// Pass 2: the worktree is now live → reattach, no second add.
	if _, err := ReconcileWorktrees(context.Background(), dep, sc, func(string, string) bool { return false }, fg.has); err != nil {
		t.Fatalf("ReconcileWorktrees pass 2: %v", err)
	}
	if fg.adds != 1 {
		t.Fatalf("pass 2 must not add a second worktree; adds=%d (want 1)", fg.adds)
	}
	if !fg.live["/repo/.wt/d"] {
		t.Fatalf("the rebuilt worktree must remain live")
	}
}

// TestRecoveryHandlesSeveralPositionsPerLoop: two live worktree rows (two
// positions in one loop) are BOTH reconciled, each from its own row — recovery
// does not silently cap at one. A kill -9 with two running positions leaves
// two rows; this is the reconciliation of both.
func TestRecoveryHandlesSeveralPositionsPerLoop(t *testing.T) {
	s := openLoopStore(t)
	sc := store.Scope{LoopSlug: "r", ComponentRoot: "/repo"}
	createLoopRow(t, s, sc)
	w1 := makeLiveRow(t, s, sc, "lane", "x", "/repo/.wt/x")
	w2 := makeLiveRow(t, s, sc, "lane", "y", "/repo/.wt/y")
	fg := newFakeGit("/repo/.wt/x", "/repo/.wt/y") // git has both
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: fg.exec()}}}
	}}
	survivors, err := ReconcileWorktrees(context.Background(), dep, sc, func(string, string) bool { return false }, fg.has)
	if err != nil {
		t.Fatalf("ReconcileWorktrees: %v", err)
	}
	if len(survivors) != 2 {
		t.Fatalf("several positions per loop: expected both to survive, got %d", len(survivors))
	}
	got := map[int64]bool{}
	for _, w := range survivors {
		got[w.ID] = true
	}
	if !got[w1.ID] || !got[w2.ID] {
		t.Fatalf("several positions per loop: both must survive; got %v", got)
	}
	if fg.adds != 0 {
		t.Fatalf("reattach of existing worktrees must not re-add; adds=%d", fg.adds)
	}
}
