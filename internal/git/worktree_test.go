package git

// The worktree-mode half of T3 (D6, Q2 answered here): in
// existing-worktree mode the feature branch is a pre-existing worktree
// branch somebody else (Paseo) owns — never created, never deleted, and
// pushed when local-only because a PR cannot target a ref the remote has
// never seen. Plus the slice's standing prohibition: no force-push and no
// deletion, EVER, asserted over the package by grep.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeModeNeverCreatesOrDeletes(t *testing.T) {
	f := &fakeGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "abc feature/4a17-version\n", nil // branch exists locally
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "abc\n", nil // and remotely — no push needed
		case args[0] == "rev-parse" && strings.Join(args, " ") == "rev-parse HEAD":
			return "abc\n", nil
		}
		return "", nil
	}}
	c := newClient(f)
	if err := c.PrepareFeatureBranch(context.Background(), "/repo", ModeExistingWorktree, "feature/4a17-version"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, call := range f.calls() {
		joined := strings.Join(call, " ")
		for _, banned := range []string{"checkout -b", "switch -c", "branch feature", "worktree add", "branch -" + "D", "branch -" + "d", "worktree remove"} {
			if strings.HasPrefix(joined, banned) || strings.Contains(joined, " "+banned+" ") || strings.Contains(joined, " "+banned) {
				t.Fatalf("existing-worktree mode attempted %q — the branch is Paseo's to create and nobody's to delete", joined)
			}
		}
	}
	// And it DID check existence both locally and remotely — the mode reads
	// the ground, it does not act on it.
	if !f.sawAny("show-ref") || !f.sawAny("rev-parse --verify origin/feature/4a17-version") {
		t.Fatalf("the mode must check before concluding, log: %v", f.calls())
	}
}

// A worktree that exists only locally must be pushed before the first task
// PR, or the PR has no base to target — and pushed WITHOUT force, verified
// by re-reading the ref.
func TestWorktreeModePushesLocalOnlyBranch(t *testing.T) {
	f := &fakeGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "abc123 feature/4a17-version\n", nil
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "", &GitError{Kind: KindExit, Detail: "Not a valid object name origin/feature/4a17-version", Code: 128}
		case args[0] == "rev-parse" && strings.Join(args, " ") == "rev-parse HEAD":
			return "abc123\n", nil
		case args[0] == "ls-remote":
			return "abc123\trefs/heads/feature/4a17-version\n", nil
		}
		return "", nil
	}}
	c := newClient(f)
	if err := c.PrepareFeatureBranch(context.Background(), "/repo", ModeExistingWorktree, "feature/4a17-version"); err != nil {
		t.Fatalf("a local-only feature branch must be pushed, not failed on: %v", err)
	}
	if !f.sawAny("push -u origin feature/4a17-version") {
		t.Fatalf("the local-only branch must be pushed before it is needed as a PR base, log: %v", f.calls())
	}
	if !f.sawAny("ls-remote") {
		t.Fatalf("the push must be VERIFIED by re-reading the remote ref (O8), log: %v", f.calls())
	}
	for _, call := range f.calls() {
		if len(call) > 0 && call[0] == "push" {
			for _, a := range call[1:] {
				if a == "-"+"f" || a == "-"+"-force" || a == "-"+"-force-with-"+"lease" {
					t.Fatalf("a worktree push is never forced: %v", call)
				}
			}
		}
	}
}

// The standard mode DOES create when absent (the contrast that makes the
// worktree mode's promise meaningful).
func TestStandardModeCreatesWhenAbsent(t *testing.T) {
	f := &fakeGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &GitError{Kind: KindExit, Code: 1} // absent locally
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "", &GitError{Kind: KindExit, Code: 128} // absent remotely too
		}
		return "", nil
	}}
	c := newClient(f)
	if err := c.PrepareFeatureBranch(context.Background(), "/repo", ModeStandard, "feature/new"); err != nil {
		t.Fatalf("standard prepare: %v", err)
	}
	if !f.sawAny("branch feature/new") {
		t.Fatalf("standard mode creates an absent branch, log: %v", f.calls())
	}
}

// TestNoForcePushAnywhere (N8, N9, I5) — force-push is the one git
// operation whose damage is unbounded and unrecoverable from the loop's
// side: it destroys history on a remote other people may have pulled.
// There is no failure this slice can encounter whose correct recovery is a
// force-push, so the CAPABILITY is absent — not hidden behind a flag.
// Asserted over every line of the package, source and tests alike, with
// the banned tokens built by concatenation so this file cannot indict
// itself while the package cannot hide.
func TestNoForcePushAnywhere(t *testing.T) {
	banned := []string{
		"-" + "-force",
		"force-with-" + "lease",
		"+" + "refs/",
		"branch -" + "D",
		"branch -" + "d",
		"push -" + "f",
		"update-ref" + " -d",
		"tag -" + "d",
	}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %q — force-push and branch/tag deletion are ABSENT (N8/N9), not off-by-default", path, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
