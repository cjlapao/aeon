package loop

// S60 T5: the step-workspace seam's engine side. Small on purpose — the
// seam is a promise about who may MOVE a loop's workspace, and a promise
// stated in one function is auditable in one place (TestWorkDirSingleWriter
// pins it).

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/store"
)

// applyEvidenceWorkDir is the ONLY place a completed action may move where
// the loop's steps run (MQ7): git.integrate reports integration_dir, and
// every subsequent step of this position/lane runs in the integration
// worktree. Any other evidence key changes nothing. The grep-pinned single
// writer: an action that could redirect the workspace by writing an
// `integration_dir` key it invented is exactly the laundering S60 refuses —
// integration_dir is only ever produced by the action that built the
// worktree, and only this function turns evidence into where.
func applyEvidenceWorkDir(st *LoopState, ev map[string]string) {
	if dir := ev["integration_dir"]; dir != "" {
		st.WorkDir = dir
	}
}

// ensureIntegrationRow is MQ7's write-ahead half: the ledger row is the
// engine's, the directory is the action's, and the ORDER is PA5's — row
// first. Idempotent the settle-once way (a live integration row for this
// item is reused, never doubled). The row carries the base tip fetched NOW
// (the schema's base_sha is NOT NULL — an empty "unknown" would be a row
// lying about what it cut from), so a missing git seam or an unreachable
// origin blocks the position BY NAME before the action could half-build
// anything.
func ensureIntegrationRow(ctx context.Context, dep Deps, st *LoopState, a action.Args) error {
	if dep.Store == nil {
		return fmt.Errorf("git.integrate needs the ledger and no store is wired")
	}
	if dep.Actions == nil {
		return fmt.Errorf("git.integrate needs the ledger and no git seam is wired — the base tip is a fetched fact, never a guess")
	}
	if a.Item == "" || a.IntegrationBranch == "" || a.Root == "" || a.BaseBranch == "" {
		return fmt.Errorf("git.integrate reached the ledger with an incomplete identity (item %q, branch %q, root %q, base %q) — the registry guard should have refused first", a.Item, a.IntegrationBranch, a.Root, a.BaseBranch)
	}
	live, err := dep.Store.ListLiveLoopWorktrees(store.Scope{LoopSlug: st.Slug, ComponentRoot: a.Root})
	if err != nil {
		return err
	}
	for _, w := range live {
		if w.OwnerKind == "integration" && w.OwnerID == a.Item {
			return nil // the row already stands — the action rebuilds or reuses the directory against it
		}
	}
	tip, err := dep.Actions(a.Root).Git.BaseTipAfterFetch(ctx, a.Root, a.BaseBranch)
	if err != nil {
		return err
	}
	if tip == "" {
		return fmt.Errorf("origin/%s has no tip to cut %s from — the base branch has to exist before an item integrates (MQ6.1)", a.BaseBranch, a.IntegrationBranch)
	}
	_, err = dep.Store.CreateLoopWorktree(store.LoopWorktree{
		ComponentRoot: a.Root,
		LoopSlug:      st.Slug,
		OwnerKind:     "integration",
		OwnerID:       a.Item,
		Path:          action.IntegrationWorktreePath(a.Root, a.Item),
		Branch:        a.IntegrationBranch,
		BaseSHA:       tip,
	})
	return err
}
