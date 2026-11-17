package daemon

// # The git reads, wired (S31 T6, D7)
//
// The protocol package may not exec and may not import the resolver, so the
// three things that need the machine live here: the bounded git reads
// (delegated whole to `internal/workspace`, which owns the timeout, the
// output caps, the group-kill and the porcelain v2 parse), the registry
// check, and the path resolution that refuses an escape BEFORE any process
// starts.
//
// The three states travel as data — the daemon asked git and git answered,
// so `not-a-repo` and `unavailable` are results, not errors. The errors here
// are the resolver's: a path escaping the root is bad params, named, and the
// protocol passes it through unchanged.

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/workspace"
)

// componentRootKnown is the registry check every workspace-facing door runs
// through. An unreadable registry CONFIRMS NOTHING — the door stays shut
// (failing open on a store hiccup would hand the socket any directory on the
// machine, which is the boundary the registry exists to hold).
func componentRootKnown(s *store.Store) func(string) bool {
	if s == nil {
		return nil
	}
	return func(root string) bool {
		projects, err := s.ListComponents(store.ComponentFilter{})
		if err != nil {
			return false
		}
		want := filepath.Clean(root)
		for _, p := range projects {
			if filepath.Clean(p.Root) == want {
				return true
			}
		}
		return false
	}
}

// gitStatusReader maps the workspace result onto the wire view. The
// workspace state strings are the wire strings by contract (`ok`,
// `not-a-repo`, `unavailable`); the mapping is field-for-field, and the ok
// state's `changed` is an empty list, not a missing field — a clean answer
// claims it saw the tree and found it clean.
func gitStatusReader() func(context.Context, string) protocol.GitStatusView {
	return func(ctx context.Context, root string) protocol.GitStatusView {
		r := workspace.GitStatus(ctx, root)
		v := protocol.GitStatusView{
			State:    string(r.State),
			Reason:   r.Reason,
			Branch:   r.Branch,
			Detached: r.Detached,
			Upstream: r.Upstream,
			Ahead:    r.Ahead,
			Behind:   r.Behind,
		}
		if r.State == workspace.StateOK {
			v.Changed = []protocol.GitChangedView{}
			for _, f := range r.Changed {
				v.Changed = append(v.Changed, protocol.GitChangedView{
					Path: f.Path, Index: f.Index, Worktree: f.Worktree, OrigPath: f.OrigPath,
				})
			}
		}
		return v
	}
}

// gitDiffReader resolves FIRST — with `tools.Resolve`, the same resolver
// `loop.file.read` bounds through — and refuses an escaping path as named
// bad params before any process starts. The workspace layer resolves again
// defensively (an internal package may not exec a path its caller did not
// bound); through this door the second resolve can only agree.
func gitDiffReader() func(context.Context, string, string) (protocol.GitDiffView, error) {
	return func(ctx context.Context, root, path string) (protocol.GitDiffView, error) {
		r := tools.Resolve(root, path)
		if !r.OK {
			return protocol.GitDiffView{}, protocol.ErrBadParams{Err: fmt.Errorf(
				"the path %q is refused: %s", path, r.Reason)}
		}
		d := workspace.GitDiff(ctx, root, r.Rel)
		return protocol.GitDiffView{
			State:     string(d.State),
			Reason:    d.Reason,
			Diff:      d.Body,
			Binary:    d.Binary,
			Truncated: d.Truncated,
			BodyBytes: d.BodyBytes,
		}, nil
	}
}

// gitChangesReader maps the workspace's per-file counts onto the wire view, field for field.
func gitChangesReader() func(context.Context, string) protocol.GitChangesView {
	return func(ctx context.Context, root string) protocol.GitChangesView {
		r := workspace.GitChanges(ctx, root)
		v := protocol.GitChangesView{State: string(r.State), Reason: r.Reason, TotalAdded: r.TotalAdded, TotalRemoved: r.TotalRemoved}
		if r.State == workspace.StateOK {
			v.Files = []protocol.GitChangeStatView{}
			for _, f := range r.Files {
				v.Files = append(v.Files, protocol.GitChangeStatView{Path: f.Path, Status: f.Status, Added: f.Added, Removed: f.Removed, Binary: f.Binary})
			}
		}
		return v
	}
}
