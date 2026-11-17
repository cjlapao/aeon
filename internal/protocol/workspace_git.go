package protocol

// # The project's git state, read-only (S31 — T6)
//
// `workspace.git.status` answers the header button's question — branch,
// upstream, ahead/behind, changed paths — and `workspace.git.diff` returns the
// unified diff for one path (working tree vs HEAD; untracked as all-added).
//
// Three-state honesty (D7/P31): `ok` | `not-a-repo` | `unavailable` (with the
// reason named). Never a reassuring empty list for "could not tell". Git runs
// through the bounded process layer (LW11) with a timeout and output caps;
// this is READ-ONLY — git *actions* are S16's, which may absorb these reads.
//
// # Where each refusal lives
//
// The STATES are answers: not-a-repo and unavailable-with-reason travel in
// the result, because the daemon asked git and git answered. The ERRORS are
// about who may ask: an unregistered project root (the path named — I4) and
// a daemon with no git reader wired. The diff's path escaping the root is
// bad params — the same refusal `loop.file.read` makes, carried by the
// daemon's resolver at the seam (this package may not exec, and may not
// import the resolver's package either: `tools.Resolve` runs where the
// process layer does).

import (
	"context"
	"encoding/json"
	"fmt"
)

// ── the wire shapes ──────────────────────────────────────────────────────────

type WorkspaceGitStatusParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// GitChangedView is one changed path. Index and Worktree carry the porcelain
// v2 XY codes verbatim (X to the index, Y to the worktree); an untracked path
// reads `{"index":"?","worktree":"U"}` — the conformance fixture pins it.
type GitChangedView struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	OrigPath string `json:"origPath,omitempty"`
}

// GitStatusView is the three-state answer (D7). The ok-state fields are
// pointers so a non-ok answer cannot carry a plausible-looking zero: a nil
// Branch is no branch named, a nil Ahead/Behind is no upstream — absence,
// not zero.
type GitStatusView struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`

	Branch   *string          `json:"branch,omitempty"`
	Detached bool             `json:"detached"`
	Upstream *string          `json:"upstream,omitempty"`
	Ahead    *int             `json:"ahead,omitempty"`
	Behind   *int             `json:"behind,omitempty"`
	Changed  []GitChangedView `json:"changed,omitempty"`
}

type WorkspaceGitDiffParams struct {
	ComponentRoot string `json:"componentRoot"`
	Path          string `json:"path"`
}

// GitDiffView is the three-state answer for one path. BodyBytes is the TRUE
// size before the cap, so `truncated` says how much is missing rather than
// leaving the reader to divide the body and hope.
type GitDiffView struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`

	Diff      string `json:"diff,omitempty"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
	BodyBytes int    `json:"bodyBytes,omitempty"`
}

// GitChangesView is `workspace.git.changes`: every changed path with its line counts, three-state like the rest.
// Added and Removed are absent for a binary file or a count that could not be read, never a zero standing in.
type GitChangesView struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`

	Files        []GitChangeStatView `json:"files,omitempty"`
	TotalAdded   int                 `json:"totalAdded"`
	TotalRemoved int                 `json:"totalRemoved"`
}

// GitChangeStatView is one changed path with its counts.
type GitChangeStatView struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   *int   `json:"added,omitempty"`
	Removed *int   `json:"removed,omitempty"`
	Binary  bool   `json:"binary,omitempty"`
}

// ── the handlers ─────────────────────────────────────────────────────────────

// workspaceDoor holds the refusals EVERY workspace-facing method makes before
// anything reads or execs: a named root, and a registered one. The git reads
// and the terminal open/list run through this one door (S31 T6/T7) — a
// second copy is how one of them drifts.
func (t *Table) workspaceDoor(componentRoot string) error {
	if componentRoot == "" {
		return ErrBadParams{Err: fmt.Errorf("componentRoot is required")}
	}
	if t.ComponentRootKnown == nil {
		return ErrStoreUnavailable.WithWhat("this daemon has no project registry, so no root can be " +
			"confirmed as registered — the workspace doors stay shut")
	}
	if !t.ComponentRootKnown(componentRoot) {
		return ErrComponentRootUnknown.WithWhat("%q is not a registered project root of this daemon", componentRoot)
	}
	return nil
}

func (t *Table) handleWorkspaceGitStatus(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p WorkspaceGitStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := t.workspaceDoor(p.ComponentRoot); err != nil {
		return nil, err
	}
	if t.GitStatus == nil {
		return nil, ErrGitUnavailable.WithWhat("no git reader is wired into this daemon")
	}
	// The state IS the answer — ok, not-a-repo, or unavailable naming the
	// reason. There is no error return for "git could not tell", because
	// there is no guess to dress up.
	return t.GitStatus(ctx, p.ComponentRoot), nil
}

func (t *Table) handleWorkspaceGitDiff(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p WorkspaceGitDiffParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := t.workspaceDoor(p.ComponentRoot); err != nil {
		return nil, err
	}
	if p.Path == "" || p.Path == "." {
		return nil, ErrBadParams{Err: fmt.Errorf("path is required — the diff is of ONE path")}
	}
	if t.GitDiff == nil {
		return nil, ErrGitUnavailable.WithWhat("no git reader is wired into this daemon")
	}
	// The seam's error is reserved for the resolver refusal (escaping the
	// root) — the daemon built it as ErrBadParams, naming the path and the
	// reason, and it travels unchanged.
	return t.GitDiff(ctx, p.ComponentRoot, p.Path)
}

func (t *Table) handleWorkspaceGitChanges(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p WorkspaceGitStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := t.workspaceDoor(p.ComponentRoot); err != nil {
		return nil, err
	}
	if t.GitChanges == nil {
		return nil, ErrGitUnavailable.WithWhat("no git reader is wired into this daemon")
	}
	return t.GitChanges(ctx, p.ComponentRoot), nil
}
