package protocol

// # The git doors (S31 T6, D7)
//
// This file tests the DOOR, not git. The states — `ok`, `not-a-repo`,
// `unavailable` with its reason — are answers the daemon got from asking, so
// they travel as results; the refusals here are about WHO MAY ASK: a root
// nobody registered is refused with the path named (I4), a daemon with no
// git reader refuses by name rather than guess, and a diff escaping the root
// is bad params carried through from the resolver untouched. The parse
// itself (porcelain v2, `-z`, caps) is `internal/workspace`'s own test's
// subject; duplicating a git here would test the wrong layer.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func gitTable(t *testing.T) *Table {
	t.Helper()
	tbl, _ := stepTable(t, &scriptedRunner{}, &spyStepStore{})
	tbl.ComponentRootKnown = func(root string) bool { return root == "/srv/registered" }
	tbl.GitStatus = func(_ context.Context, root string) GitStatusView {
		branch, up := "main", "origin/main"
		ahead, behind := 1, 0
		return GitStatusView{State: "ok", Branch: &branch, Upstream: &up, Ahead: &ahead, Behind: &behind,
			Changed: []GitChangedView{{Path: "SURVEY.md", Index: "?", Worktree: "U"}}}
	}
	tbl.GitDiff = func(_ context.Context, root, path string) (GitDiffView, error) {
		return GitDiffView{State: "ok", Diff: "--- a/" + path + "\n+++ b/" + path + "\n"}, nil
	}
	initializedOnce(t, tbl, "c1")
	return tbl
}

func dispatchGit(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	return tbl.Dispatch(context.Background(), "c1", m, mustJSON(t, params))
}

func TestGitStatusAnswersTheRegisteredRoot(t *testing.T) {
	tbl := gitTable(t)
	res, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/srv/registered"})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := res.(GitStatusView)
	if !ok || v.State != "ok" || v.Branch == nil || *v.Branch != "main" || len(v.Changed) != 1 {
		t.Fatalf("the seam's answer did not travel intact: %+v", res)
	}
}

func TestGitStatesAreAnswersNotErrors(t *testing.T) {
	// P31's shape at the door: `not-a-repo` and `unavailable`-with-reason
	// arrive as SUCCESSFUL responses carrying the state, because the daemon
	// asked git and git answered. Only not-asking is an error.
	tbl := gitTable(t)
	tbl.GitStatus = func(context.Context, string) GitStatusView {
		return GitStatusView{State: "not-a-repo"}
	}
	res, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/srv/registered"})
	if err != nil {
		t.Fatalf("not-a-repo came back as an error: %v", err)
	}
	if v := res.(GitStatusView); v.State != "not-a-repo" {
		t.Fatalf("state lost: %+v", v)
	}

	tbl.GitStatus = func(context.Context, string) GitStatusView {
		return GitStatusView{State: "unavailable", Reason: "git is not installed: exec: \"git\": executable file not found"}
	}
	res, err = dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/srv/registered"})
	if err != nil {
		t.Fatalf("unavailable came back as an error: %v", err)
	}
	if v := res.(GitStatusView); v.State != "unavailable" || !strings.Contains(v.Reason, "not installed") {
		t.Fatalf("the reason did not travel: %+v", v)
	}
}

func TestGitDoorRefusals(t *testing.T) {
	t.Run("unregistered root names the path", func(t *testing.T) {
		tbl := gitTable(t)
		_, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/etc/secrets"})
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrComponentRootUnknown.Code {
			t.Fatalf("got %v, want ErrComponentRootUnknown", err)
		}
		if !strings.Contains(e.Data.What, "/etc/secrets") {
			t.Fatalf("the refusal did not name what it refused (I4): %s", e.Data.What)
		}
		// The same door on the diff side.
		if _, err := dispatchGit(t, tbl, MethodWorkspaceGitDiff,
			WorkspaceGitDiffParams{ComponentRoot: "/etc/secrets", Path: "x"}); err == nil {
			t.Fatal("the diff door let an unregistered root through")
		}
	})

	t.Run("no registry refuses by name", func(t *testing.T) {
		tbl := gitTable(t)
		tbl.ComponentRootKnown = nil
		_, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/srv/registered"})
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrStoreUnavailable.Code {
			t.Fatalf("got %v, want ErrStoreUnavailable", err)
		}
	})

	t.Run("empty root is bad params", func(t *testing.T) {
		tbl := gitTable(t)
		_, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{})
		var bp ErrBadParams
		if !errors.As(err, &bp) {
			t.Fatalf("got %v, want ErrBadParams", err)
		}
	})

	t.Run("no git reader refuses rather than guesses", func(t *testing.T) {
		tbl := gitTable(t)
		tbl.GitStatus = nil
		_, err := dispatchGit(t, tbl, MethodWorkspaceGitStatus, WorkspaceGitStatusParams{ComponentRoot: "/srv/registered"})
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrGitUnavailable.Code {
			t.Fatalf("got %v, want ErrGitUnavailable", err)
		}
		tbl.GitStatus = func(context.Context, string) GitStatusView { return GitStatusView{State: "ok"} }
		tbl.GitDiff = nil
		if _, err := dispatchGit(t, tbl, MethodWorkspaceGitDiff,
			WorkspaceGitDiffParams{ComponentRoot: "/srv/registered", Path: "a"}); err == nil {
			t.Fatal("the diff door ran without a reader")
		}
	})

	t.Run("the diff demands one named path", func(t *testing.T) {
		tbl := gitTable(t)
		for _, bad := range []string{"", "."} {
			_, err := dispatchGit(t, tbl, MethodWorkspaceGitDiff,
				WorkspaceGitDiffParams{ComponentRoot: "/srv/registered", Path: bad})
			var bp ErrBadParams
			if !errors.As(err, &bp) {
				t.Fatalf("path %q: got %v, want ErrBadParams", bad, err)
			}
		}
	})
}

func TestGitDiffResolverRefusalTravelsUntouched(t *testing.T) {
	// The daemon's seam refuses an escaping path as named bad params — the
	// door passes that through unchanged (same refusal `loop.file.read`
	// makes), states included when the path was fine.
	tbl := gitTable(t)
	tbl.GitDiff = func(_ context.Context, _, path string) (GitDiffView, error) {
		if strings.HasPrefix(path, "..") {
			return GitDiffView{}, ErrBadParams{Err: fmt.Errorf("the path %q is refused: outside the root", path)}
		}
		return GitDiffView{State: "ok", Diff: "x", BodyBytes: 1}, nil
	}
	_, err := dispatchGit(t, tbl, MethodWorkspaceGitDiff,
		WorkspaceGitDiffParams{ComponentRoot: "/srv/registered", Path: "../outside"})
	var bp ErrBadParams
	if !errors.As(err, &bp) || !strings.Contains(err.Error(), "outside the root") {
		t.Fatalf("the resolver refusal did not travel named: %v", err)
	}
	res, err := dispatchGit(t, tbl, MethodWorkspaceGitDiff,
		WorkspaceGitDiffParams{ComponentRoot: "/srv/registered", Path: "a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.(GitDiffView); v.State != "ok" || v.Diff != "x" {
		t.Fatalf("diff answer lost: %+v", res)
	}
}
