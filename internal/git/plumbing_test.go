package git

// T3's tests, written first. Two of the three constraints a naive
// implementation breaks live here, and both come from RECORDED INCIDENTS:
// hand-picked files dropped by a squash merge (the completeness assertion),
// and a deleted Paseo worktree (existing-worktree mode). The assertions
// that matter are on the COMMAND LOG — "no create, no delete" is a
// property of what was attempted, not of what succeeded.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- the fake git ---------------------------------------------------------

type fakeGit struct {
	mu       sync.Mutex
	log      [][]string // every argv (without the binary) in order
	resolver func(args []string) (string, error)
}

func (f *fakeGit) run(_ context.Context, dir string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, append([]string(nil), args...))
	if f.resolver == nil {
		return "", nil
	}
	return f.resolver(args)
}

func (f *fakeGit) calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.log
}

func (f *fakeGit) sawAny(needles ...string) bool {
	for _, call := range f.calls() {
		joined := strings.Join(call, " ")
		for _, n := range needles {
			if strings.Contains(joined, n) {
				return true
			}
		}
	}
	return false
}

func newClient(f *fakeGit) *Client { return &Client{Exec: f.run} }

// --- the completeness assertion --------------------------------------------

// TestStrayFileAnywhereTripsTheAssertion — the recorded incident: files
// hand-picked out of a commit, left behind, then DROPPED by the squash
// merge. The assertion is `git status --porcelain` EMPTY with NO exclusion
// (L24), and it is what a hand-picking step leaves for T4's action to
// find. This runs against REAL git.
func TestStrayFileAnywhereTripsTheAssertion(t *testing.T) {
	dir := realRepo(t)
	writeFile(t, dir, "tracked.txt", "work")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "work")

	// Simulate the hand-pick: something staged a NARROW set (a step's bash
	// tool `git add tracked.txt`), while a nested file sits untracked —
	// "anywhere" is the point.
	nested := filepath.Join(dir, "deep", "er")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, nested, "left-behind.txt", "do not drop me")
	writeFile(t, dir, "tracked.txt", "work amended")
	gitRun(t, dir, "add", "tracked.txt")

	c := NewClient(gitTestEnv())
	err := c.AssertClean(context.Background(), dir)
	if err == nil {
		t.Fatal("a stray file anywhere must trip the completeness assertion — the recorded incident is files dropped by the squash merge")
	}
	var ge *GitError
	if !asGitError(err, &ge) || ge.Kind != KindDirtyTree {
		t.Fatalf("the stray file must be a discriminated dirty-tree result, got %#v", err)
	}
	// A refusal that cannot name what it refused is not actionable.
	if !strings.Contains(ge.Detail, "left-behind.txt") {
		t.Fatalf("the refusal must name the leftover, got %q", ge.Detail)
	}

	// The action's own staging (add -A, NO pathspec) then commits the
	// leftover — nothing hand-picked survives it.
	sha, nothing, err := c.CommitEverything(context.Background(), dir, "everything")
	if err != nil || nothing || sha == "" {
		t.Fatalf("stage-everything should have committed the leftover: sha=%q nothing=%v err=%v", sha, nothing, err)
	}
	out := gitRun(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(out, "left-behind.txt") {
		t.Fatalf("stage-everything means the leftover rides in: %s", out)
	}
}

func TestNothingToCommitIsClean(t *testing.T) {
	dir := realRepo(t)
	writeFile(t, dir, "a.txt", "a")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "a")
	c := NewClient(gitTestEnv())
	// An already-clean tree passes the ASSERTION...
	if err := c.AssertClean(context.Background(), dir); err != nil {
		t.Fatalf("a clean tree must pass the assertion, got %v", err)
	}
	// ...and CommitEverything reports nothingToCommit as SUCCESS (the state
	// T4 must treat as success — a naive implementation errors here).
	sha, nothing, err := c.CommitEverything(context.Background(), dir, "again")
	if err != nil || !nothing || sha != "" {
		t.Fatalf("nothing-to-commit is success: sha=%q nothing=%v err=%v", sha, nothing, err)
	}
}

// TestQ3Answered checks L24's assumption rather than assuming it: Aeon's
// store lives OUTSIDE every working tree, and no exclusion string survives
// anywhere in the Go code.
func TestQ3Answered(t *testing.T) {
	dir := realRepo(t)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	// The state directory must be OUTSIDE the repository — the assertion's
	// strength depends on it (L24).
	repo := filepath.Clean(dir)
	st := filepath.Clean(filepath.Join(state, "aeon"))
	if strings.HasPrefix(string(st), string(repo)+string(os.PathSeparator)) || st == repo {
		t.Fatalf("the store must live outside every repository: %q under %q", st, repo)
	}
	// v1's exclusion habit must not have been carried forward: the pathspec
	// EXCLUSION magic (`:!` + artefacts path) is absent from this package
	// entirely, and no staging line in the product takes a `--` pathspec
	// tail. (Other packages may NAME the old artefacts directory in
	// comments and legacy paths — what L24 retired was the exclusion.)
	magic := ":!" + "." + "projects/loops"
	addTail := "add" + " -A --"
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), magic) {
			t.Errorf("%s still carries the v1 pathspec exclusion — L24 retired it", path)
		}
		if strings.Contains(string(src), addTail) {
			t.Errorf("%s stages with a pathspec tail — L24 allows only add -A, no exclusions", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// And the staging command this package issues carries NO pathspec
	// exclusion: `add -A` and nothing else.
	f := &fakeGit{}
	c := newClient(f)
	if err := c.StageEverything(context.Background(), dir); err != nil {
		t.Fatalf("stage: %v", err)
	}
	for _, call := range f.calls() {
		if call[0] == "add" {
			if strings.Join(call, " ") != "add -A" {
				t.Fatalf("staging is `add -A` with NO exclusion (L24), got %v", call)
			}
		}
	}
}

// --- worktree mode ----------------------------------------------------------

// TestWorktreeModeNeverCreatesOrDeletes — the Paseo incident: deleting a
// worktree branch destroys a workspace Paseo owns. Asserted on the COMMAND
// LOG: what was never attempted.
func TestIsAncestorDetectsALandedSquash(t *testing.T) {
	dir := realRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "base.txt", "base")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	featureBase := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	gitRun(t, dir, "checkout", "-b", "task/x", featureBase)
	writeFile(t, dir, "work.txt", "work")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "work")
	original := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	gitRun(t, dir, "checkout", "feature")
	gitRun(t, dir, "merge", "--squash", "task/x")
	gitRun(t, dir, "commit", "-m", "task/x (squashed)")
	squash := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	if squash == original {
		t.Fatal("the scenario is wrong: a squash rewrite must produce its own sha")
	}

	c := NewClient(gitTestEnv())
	ctx := context.Background()
	if ok, err := c.IsAncestor(ctx, dir, squash, "feature"); err != nil || !ok {
		t.Fatalf("the squash SHA must be an ancestor after a correct merge, got %v (%v)", ok, err)
	}
	if ok, err := c.IsAncestor(ctx, dir, original, "feature"); err != nil || ok {
		t.Fatalf("the task branch's ORIGINAL commit must NOT be an ancestor after a squash (this is the half people get wrong), got %v (%v)", ok, err)
	}
}

func TestResultsAreDiscriminated(t *testing.T) {
	ctx := context.Background()

	// A missing ref is an ABSENCE (false, nil), not an error — check-then-act
	// routes on it.
	f1 := &fakeGit{resolver: func(args []string) (string, error) {
		if args[0] == "show-ref" {
			return "", &GitError{Kind: KindExit, Code: 1}
		}
		return "", nil
	}}
	if ok, err := newClient(f1).BranchExistsLocal(ctx, "/r", "nope"); err != nil || ok {
		t.Fatalf("a missing local ref is absence, not failure: %v %v", ok, err)
	}

	// A conflict (unmerged paths at commit time) is its own kind.
	f2 := &fakeGit{resolver: func(args []string) (string, error) {
		switch args[0] {
		case "status":
			return "", nil
		case "rev-parse":
			return "", &GitError{Kind: KindExit, Code: 1}
		case "commit":
			return "", &GitError{Kind: KindExit, Code: 1, Detail: "Committing is not possible because you have unmerged files. fix them up and run git commit"}
		}
		return "", nil
	}}
	_, _, err := newClient(f2).CommitEverything(ctx, "/r", "m")
	var ge *GitError
	if !asGitError(err, &ge) || ge.Kind != KindConflict {
		t.Fatalf("unmerged files must be a CONFLICT kind, not a generic exit, got %#v", err)
	}

	// A transport failure (unreachable remote) is its own kind, distinct
	// from both — "check your permissions" vs "your network is out".
	f3 := &fakeGit{resolver: func(args []string) (string, error) {
		if args[0] == "push" {
			return "", &GitError{Kind: KindExit, Code: 128, Detail: "fatal: Could not read from remote repository. Please make sure you have the correct access rights"}
		}
		return "", nil
	}}
	err = newClient(f3).Push(ctx, "/r", "b")
	if !asGitError(err, &ge) || ge.Kind != KindTransport {
		t.Fatalf("an unreachable remote must be TRANSPORT (retry/say so), distinct from refusal and from conflict, got %#v", err)
	}
}

// --- real-git helpers -------------------------------------------------------

func realRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH — the recorded-incident assertions need real git (the fake covers the rest)")
	}
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@aeon.test")
	gitRun(t, dir, "config", "user.name", "aeon test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
