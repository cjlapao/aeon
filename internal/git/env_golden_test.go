package git

// env_golden_test.go — S59 T1 (characterization, P2 Phase A). Two pins on
// the untouched code:
//
//  1. The EXACT argument list every `Client` method sends to git, through a
//     recording Exec. T5 adds environment plumbing to this package's
//     exec path (the child's environment becomes explicit, EV10); the
//     argv must not move, and this table is the byte-level pin of that.
//
//  2. The ambient-state hole, documented by observation: with the REAL
//     exec, a `GIT_DIR` in the test process's environment retargets every
//     Client call at a DIFFERENT directory. This is the same class of
//     hole `workspace.gitEnv()` already closes for its own spawns —
//     recorded here, on the unguarded door, so the fix is not re-derived.
//
// No network, no remote: every argv case runs against a recording Exec
// that returns canned output, and the ambient case builds its own throwaway
// repository with the real git.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is an Exec that logs every argv and replays a queue of outputs.
type recorder struct {
	calls  []string
	output []string
	fail   map[int]error // call index → error (e.g. the exit-1 absence answer)
}

func (r *recorder) call(ctx context.Context, dir string, args ...string) (string, error) {
	idx := len(r.calls)
	r.calls = append(r.calls, strings.Join(args, " "))
	if err, ok := r.fail[idx]; ok {
		return "", err
	}
	if idx < len(r.output) {
		return r.output[idx], nil
	}
	return "", nil
}

func wantCalls(t *testing.T, r *recorder, want []string) {
	t.Helper()
	if len(r.calls) != len(want) {
		t.Fatalf("argv sequence drifted:\n  now:  %v\n  gold: %v", r.calls, want)
	}
	for i := range want {
		if r.calls[i] != want[i] {
			t.Errorf("call %d: %q, golden says %q", i, r.calls[i], want[i])
		}
	}
}

// TestGitClientArgvGolden — the exact argv per method. Any byte that moves
// in one of these lists is a protocol change for every caller, and this is
// the test that says so.
func TestGitClientArgvGolden(t *testing.T) {
	ctx := context.Background()
	const dir = "/w"
	const sha = "sha123"

	t.Run("existence", func(t *testing.T) {
		r := &recorder{}
		c := &Client{Exec: r.call}

		if ok, err := c.BranchExistsLocal(ctx, dir, "feat"); err != nil || !ok {
			t.Fatalf("present branch: %v %v", ok, err)
		}
		r2 := &recorder{fail: map[int]error{0: &GitError{Kind: KindExit, Code: 1}}}
		c2 := &Client{Exec: r2.call}
		if ok, err := c2.BranchExistsLocal(ctx, dir, "feat"); err != nil || ok {
			t.Fatalf("absent branch must be false-with-no-error: %v %v", ok, err)
		}
		r3 := &recorder{}
		c3 := &Client{Exec: r3.call}
		if ok, err := c3.BranchExistsRemote(ctx, dir, "feat"); err != nil || !ok {
			t.Fatalf("remote branch: %v %v", ok, err)
		}

		wantCalls(t, r, []string{"show-ref --verify --quiet refs/heads/feat"})
		wantCalls(t, r2, []string{"show-ref --verify --quiet refs/heads/feat"})
		wantCalls(t, r3, []string{"rev-parse --verify origin/feat"})
	})

	t.Run("reads", func(t *testing.T) {
		r := &recorder{output: []string{"main\n", sha + "\n", "other\n"}}
		c := &Client{Exec: r.call}
		if b, err := c.CurrentBranch(ctx, dir); err != nil || b != "main" {
			t.Fatalf("branch: %q %v", b, err)
		}
		if h, err := c.LocalHEAD(ctx, dir); err != nil || h != sha {
			t.Fatalf("head: %q %v", h, err)
		}
		if h, err := c.LocalHEADFor(ctx, dir, "main"); err != nil || h != "other" {
			t.Fatalf("ref: %q %v", h, err)
		}
		wantCalls(t, r, []string{
			"rev-parse --abbrev-ref HEAD",
			"rev-parse HEAD",
			"rev-parse main",
		})
	})

	t.Run("commit", func(t *testing.T) {
		r := &recorder{output: []string{"", "", "committed\n", sha + "\n"}}
		c := &Client{Exec: r.call}
		got, nothing, err := c.CommitEverything(ctx, dir, "the message")
		if err != nil || nothing || got != sha {
			t.Fatalf("commit: %q %v %v", got, nothing, err)
		}
		wantCalls(t, r, []string{
			"add -A",
			"status --porcelain",
			"commit -m the message",
			"rev-parse HEAD",
		})
	})

	t.Run("push-verified", func(t *testing.T) {
		r := &recorder{output: []string{"", sha + "\n", sha + "\trefs/heads/feat\n"}}
		c := &Client{Exec: r.call}
		if err := c.Push(ctx, dir, "feat"); err != nil {
			t.Fatalf("push: %v", err)
		}
		wantCalls(t, r, []string{
			"push -u origin feat",
			"rev-parse HEAD",
			"ls-remote origin refs/heads/feat",
		})
	})

	t.Run("remote-and-ancestry", func(t *testing.T) {
		r := &recorder{output: []string{sha + "\trefs/heads/feat\n"}}
		c := &Client{Exec: r.call}
		if s, err := c.RemoteRefSHA(ctx, dir, "feat"); err != nil || s != sha {
			t.Fatalf("remote sha: %q %v", s, err)
		}
		r2 := &recorder{}
		c2 := &Client{Exec: r2.call}
		if ok, err := c2.IsAncestor(ctx, dir, "aaa", "bbb"); err != nil || !ok {
			t.Fatalf("ancestor: %v %v", ok, err)
		}
		wantCalls(t, r, []string{"ls-remote origin refs/heads/feat"})
		wantCalls(t, r2, []string{"merge-base --is-ancestor aaa bbb"})
	})

	t.Run("run-passes-args-through", func(t *testing.T) {
		r := &recorder{}
		c := &Client{Exec: r.call}
		if _, err := c.Run(ctx, dir, "branch", "gold"); err != nil {
			t.Fatal(err)
		}
		wantCalls(t, r, []string{"branch gold"})
	})

	t.Run("prepare-feature-branch-standard", func(t *testing.T) {
		// Absent locally AND remotely: one branch command, and only after
		// both checks.
		r := &recorder{fail: map[int]error{0: &GitError{Kind: KindExit, Code: 1}, 1: &GitError{Kind: KindExit, Code: 1}}}
		c := &Client{Exec: r.call}
		if err := c.PrepareFeatureBranch(ctx, dir, ModeStandard, "feat"); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		wantCalls(t, r, []string{
			"show-ref --verify --quiet refs/heads/feat",
			"rev-parse --verify origin/feat",
			"branch feat",
		})
	})

	t.Run("prepare-feature-branch-existing", func(t *testing.T) {
		// Present locally AND remotely: the two checks and NO action —
		// existing-worktree mode never creates and never pushes here.
		r := &recorder{}
		c := &Client{Exec: r.call}
		if err := c.PrepareFeatureBranch(ctx, dir, ModeExistingWorktree, "feat"); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		wantCalls(t, r, []string{
			"show-ref --verify --quiet refs/heads/feat",
			"rev-parse --verify origin/feat",
		})
	})
}

// TestGitHonoursAmbientGitDirToday — the ambient-state hole, with the real
// exec: a `GIT_DIR` set in the process environment retargets a Client call
// whose working directory is an unrelated empty directory. `workspace`
// already strips GIT_* for its own spawns (gitEnv, pinned in
// workspace/git_env_golden_test.go); this door has no such stripping, and
// this test is the measurement that says so.
func TestGitHonoursAmbientGitDirToday(t *testing.T) {
	ctx := context.Background()

	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cwd, _ := os.Getwd()
	// The commit needs an identity; -c keeps the test away from any
	// machine config (the same hygiene gitEnv applies to its spawns).
	if out, err := exec.Command("git", "-C", repo,
		"-c", "user.name=s59-golden", "-c", "user.email=s59@aeon.test",
		"commit", "--allow-empty", "-q", "-m", "golden").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		t.Fatal("the throwaway repository has no commit")
	}
	_ = cwd

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	// # Amended by S59 T5 (EV10) — the hole is closed, and the flip is the proof
	//
	// This test was written to pin a HOLE: a git client that inherited the
	// process environment let an ambient `GIT_DIR` retarget the call onto a
	// different repository, so `LocalHEAD` of an unrelated directory answered
	// with somebody else's commit. The fix is not a git-side check and not a
	// special case for `GIT_DIR`: the child no longer receives the ambient
	// environment, so there is nothing to retarget it. The arms below are the
	// two halves of that claim — stripped, it cannot; handed the variable
	// explicitly, it still can, which is what proves the STRIP is doing the work
	// and not a change in git's own behaviour.
	c := NewClient(gitTestEnv())

	t.Run("ambient-GIT_DIR-no-longer-retargets", func(t *testing.T) {
		t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
		if got, err := c.LocalHEAD(ctx, empty); err == nil {
			t.Fatalf("the ambient GIT_DIR still retargets the call: an unrelated directory answered %q — "+
				"EV10's strip is not reaching this spawn", got)
		}
	})

	t.Run("an-explicit-GIT_DIR-still-retargets", func(t *testing.T) {
		// The counter-arm the first one needs: if this also failed, the arm
		// above would only prove git cannot read `GIT_DIR` at all.
		explicit := NewClient(append(gitTestEnv(), "GIT_DIR="+filepath.Join(repo, ".git")))
		got, err := explicit.LocalHEAD(ctx, empty)
		if err != nil {
			t.Fatalf("a client explicitly handed GIT_DIR failed instead of honouring it: %v", err)
		}
		if got != sha {
			t.Fatalf("the explicit GIT_DIR was not honoured: got %q, want the other repository's %q", got, sha)
		}
	})

	t.Run("a-client-with-no-environment-refuses", func(t *testing.T) {
		// The state a caller reaches by building `&Client{}`, which used to mean
		// "inherit everything". It refuses now, and names the missing thing.
		if _, err := (&Client{}).LocalHEAD(ctx, empty); err == nil {
			t.Fatal("a client with no environment ran git")
		}
	})
}
