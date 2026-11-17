package action

// T4's tests, written first. The three git actions are check-then-act, and
// the interesting assertions are on the COMMAND LOG (D1/O6): "twice is a
// no-op" is a property of what the second run attempted, per action, in
// three separate tests — one loop over three actions would let one action's
// green cover another's gap.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/git"
)

// --- fakes and real-repo helpers -------------------------------------------

type logGit struct {
	mu       sync.Mutex
	log      [][]string
	resolver func(args []string) (string, error)
}

func (l *logGit) run(_ context.Context, _ string, args ...string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log = append(l.log, append([]string(nil), args...))
	if l.resolver != nil {
		return l.resolver(args)
	}
	return "", nil
}

func (l *logGit) since(n int) [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.log[n:]
}

func (l *logGit) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.log)
}

func actionsWith(f *logGit) *GitActions { return &GitActions{Git: &git.Client{Exec: f.run}} }

func mustNames(t *testing.T) BranchNames {
	t.Helper()
	n, err := NewBranchNames("4a17-version", "fix-parse-order")
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	return n
}

func realRepoWithOrigin(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	origin := filepath.Join(t.TempDir(), "origin.git")
	mustExec(t, "", "git", "init", "--bare", "-b", "main", origin)
	dir := t.TempDir()
	mustExec(t, dir, "git", "init", "-b", "main")
	mustExec(t, dir, "git", "config", "user.email", "t@aeon.test")
	mustExec(t, dir, "git", "config", "user.name", "aeon test")
	mustExec(t, dir, "git", "config", "commit.gpgsign", "false")
	mustExec(t, dir, "git", "remote", "add", "origin", origin)
	writeFile(t, dir, "seed.txt", "seed")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "seed")
	mustExec(t, dir, "git", "push", "-u", "origin", "main")
	return dir, origin
}

func mustExec(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
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

// --- the named checks, per action -------------------------------------------

// TestNothingToCommitIsSuccess — D2. An empty status means the work is
// already in. Treating it as failure fails a step that has nothing left to
// do, routes as a reject, and burns a redo round — the single most likely
// place for an idempotency bug to present as a loop that cannot finish.
func TestNothingToCommitIsSuccess(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	a := &GitActions{Git: gitReal()}
	names := mustNames(t)
	if _, err := a.CreateBranch(context.Background(), dir, names, BranchTask); err != nil {
		t.Fatalf("create task branch: %v", err)
	}
	// The tree is clean and the branch was just created off main: nothing
	// to commit is SUCCESS, not an error.
	res, err := a.CommitAndPush(context.Background(), dir, names, "work")
	if err != nil {
		t.Fatalf("nothing-to-commit must be success, got %v", err)
	}
	if !res.NothingToCommit {
		t.Fatalf("the result must SAY it had nothing to commit (a success that lies about why is a different truth): %+v", res)
	}
}

// TestEachGitActionTwiceIsANoOp — D1/O6/C2: three SEPARATE tests on the
// command log, not one loop; one action's green must not cover another's
// gap.
func TestEachGitActionTwiceIsANoOp(t *testing.T) {
	ctx := context.Background()

	t.Run("createBranch", func(t *testing.T) {
		f := &logGit{resolver: scriptBranchPresent("task/4a17-version/fix-parse-order")}
		a := actionsWith(f)
		names := mustNames(t)
		for i := 0; i < 2; i++ {
			res, err := a.CreateBranch(ctx, "/repo", names, BranchTask)
			if err != nil || !res.Reused {
				t.Fatalf("run %d: present branch must reuse, got %+v (%v)", i, res, err)
			}
		}
		for _, call := range f.log {
			if len(call) >= 2 && call[0] == "branch" {
				t.Fatalf("a present branch must never be created, log: %v", f.log)
			}
		}
	})

	t.Run("commitAndPush", func(t *testing.T) {
		dir, _ := realRepoWithOrigin(t)
		a := &GitActions{Git: gitReal()}
		names := mustNames(t)
		if _, err := a.CreateBranch(ctx, dir, names, BranchTask); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "x.txt", "x")
		first, err := a.CommitAndPush(ctx, dir, names, "work")
		if err != nil || first.NothingToCommit || first.SHA == "" {
			t.Fatalf("first run must commit: %+v (%v)", first, err)
		}
		second, err := a.CommitAndPush(ctx, dir, names, "work")
		if err != nil {
			t.Fatalf("second run: %v", err)
		}
		if !second.NothingToCommit || second.SHA != "" {
			t.Fatalf("second run must report nothing-to-commit: %+v", second)
		}
		// The tree has ONE commit for the work, verified on git itself.
		head := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "HEAD"))
		if head != first.SHA {
			t.Fatalf("HEAD moved on the no-op run: %s vs %s", head, first.SHA)
		}
		// And no push was even attempted for an already-current ref.
		if second.Pushed {
			t.Fatalf("an already-current ref needs no push at all: %+v", second)
		}
	})

	t.Run("mergeBranch", func(t *testing.T) {
		f := &logGit{resolver: func(args []string) (string, error) {
			switch {
			case args[0] == "rev-parse" && !strings.Contains(strings.Join(args, " "), "--verify"):
				return "feattip1\n", nil
			case args[0] == "merge-base":
				return "", nil // exit 0: already an ancestor
			}
			return "", nil
		}}
		a := actionsWith(f)
		names := mustNames(t)
		before := f.len()
		for i := 0; i < 2; i++ {
			res, err := a.MergeBranch(ctx, "/repo", names)
			if err != nil || !res.AlreadyMerged || res.Merged {
				t.Fatalf("run %d: ancestor feature must merge-free, got %+v (%v)", i, res, err)
			}
		}
		for _, call := range f.since(before) {
			if call[0] == "merge" {
				t.Fatalf("an already-merged branch must not be merged again, log: %v", f.since(before))
			}
		}
	})
}

func scriptBranchPresent(branch string) func([]string) (string, error) {
	return func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref" && strings.Contains(strings.Join(args, " "), branch):
			return "abc " + branch + "\n", nil
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "abc\n", nil
		}
		return "", nil
	}
}

// TestCommitIsIdempotentOnlyWithAHeadCheck — worth stating because it is
// counter-intuitive: a second commit carrying the SAME MESSAGE is not the
// same thing as no commit. It is a new SHA and a new history entry. The
// test demonstrates the mechanism directly (a forced duplicate commit
// produces a different SHA), then pins that the action's checks — tree
// cleanliness and the branch head, NOT the message — are what stop it.
func TestCommitIsIdempotentOnlyWithAHeadCheck(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	names := mustNames(t)
	a := &GitActions{Git: gitReal()}
	if _, err := a.CreateBranch(context.Background(), dir, names, BranchTask); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "y.txt", "y")
	first, err := a.CommitAndPush(context.Background(), dir, names, "same message")
	if err != nil || first.SHA == "" {
		t.Fatalf("first: %+v (%v)", first, err)
	}

	// The mechanism: git happily creates a second commit with the same
	// message when the "nothing to commit" check is bypassed.
	mustExec(t, dir, "git", "commit", "--allow-empty", "-m", "same message")
	dup := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "HEAD"))
	if dup == first.SHA {
		t.Fatal("the demonstration is wrong: a same-message commit IS a new SHA")
	}
	// Undo the demonstration commit so the action's own second run is what
	// we measure.
	mustExec(t, dir, "git", "reset", "--hard", first.SHA)

	// The action runs the SAME work again: no second commit appears,
	// because the check is on the tree and the head, not the message.
	second, err := a.CommitAndPush(context.Background(), dir, names, "same message")
	if err != nil || !second.NothingToCommit {
		t.Fatalf("second run must find nothing to commit: %+v (%v)", second, err)
	}
	head := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "HEAD"))
	if head != first.SHA {
		t.Fatalf("the head check exists so history gains no duplicate; HEAD is %s, want %s", head, first.SHA)
	}
}

func TestCreateBranchReusesExisting(t *testing.T) {
	ctx := context.Background()
	// Local present → reuse, no create.
	f1 := &logGit{resolver: scriptBranchPresent("task/4a17-version/fix-parse-order")}
	res, err := actionsWith(f1).CreateBranch(ctx, "/repo", mustNames(t), BranchTask)
	if err != nil || !res.Reused {
		t.Fatalf("local present must reuse: %+v (%v)", res, err)
	}
	// Remote present (fresh clone) → reuse, no create.
	f2 := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "abc\n", nil
		}
		return "", nil
	}}
	res, err = actionsWith(f2).CreateBranch(ctx, "/repo", mustNames(t), BranchTask)
	if err != nil || !res.Reused {
		t.Fatalf("remote present must reuse: %+v (%v)", res, err)
	}
	// Both absent → create.
	f3 := &logGit{resolver: func(args []string) (string, error) {
		switch args[0] {
		case "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case "rev-parse":
			return "", &git.GitError{Kind: git.KindExit, Code: 128}
		}
		return "", nil
	}}
	res, err = actionsWith(f3).CreateBranch(ctx, "/repo", mustNames(t), BranchTask)
	if err != nil || res.Reused {
		t.Fatalf("both absent must create: %+v (%v)", res, err)
	}
	var created bool
	for _, call := range f3.log {
		if call[0] == "branch" && call[1] == "task/4a17-version/fix-parse-order" {
			created = true
		}
	}
	if !created {
		t.Fatalf("both absent must create the branch, log: %v", f3.log)
	}
}

func TestMergeBranchDetectsAlreadyMerged(t *testing.T) {
	f := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "rev-parse" && !strings.Contains(strings.Join(args, " "), "--verify"):
			return "feattip\n", nil
		case args[0] == "merge-base":
			return "", nil // ancestor: already merged
		}
		return "", nil
	}}
	res, err := actionsWith(f).MergeBranch(context.Background(), "/repo", mustNames(t))
	if err != nil || !res.AlreadyMerged {
		t.Fatalf("is-ancestor exit 0 must be success without merging: %+v (%v)", res, err)
	}
	for _, call := range f.log {
		if call[0] == "merge" || call[0] == "checkout" {
			t.Fatalf("an already-merged branch must not be touched, log: %v", f.log)
		}
	}
}

// TestBranchNamesComeFromCode (L3, I3) — the recorded branch-inheritance
// bug was a model re-deriving a branch name and getting it subtly wrong.
// Two halves: the CONSTRUCTORS are the only door (identifiers built from
// loop and task identity, validated), and no action signature accepts a
// branch name at all — reflection, not convention.
func TestBranchNamesComeFromCode(t *testing.T) {
	n := mustNames(t)
	if n.TaskBranch() != "task/4a17-version/fix-parse-order" {
		t.Fatalf("branch name construction drifted: %q", n.TaskBranch())
	}
	if got := strings.Join([]string{n.FeatureBranch()}, ""); got != "feature/4a17-version" {
		t.Fatalf("feature branch construction: %q", got)
	}
	// A smuggled path separator or space is refused at construction — the
	// recorded bug re-deriving "feature/other" into a task slot cannot be
	// spelled at all.
	for _, bad := range []string{"feat/ure", "has space", "", "-leading", "a\\b", "a b/c"} {
		if _, err := NewBranchNames("4a17-version", bad); err == nil {
			t.Fatalf("task identity %q must be refused at construction", bad)
		}
	}
	if _, err := NewBranchNames("", "task"); err == nil {
		t.Fatal("empty loop identity must be refused")
	}

	// Reflection: every action takes its identifiers through the one door
	// (BranchNames), and bare strings are limited to dir and message — a
	// branch name has nowhere to be typed.
	typ := reflect.TypeOf(&GitActions{})
	want := map[string]bool{"CreateBranch": true, "CommitAndPush": true, "MergeBranch": true}
	names := reflect.TypeOf(BranchNames{})
	seen := 0
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if !want[m.Name] {
			continue
		}
		seen++
		ty := m.Type
		var strings, branchesDoors int
		for p := 1; p < ty.NumIn(); p++ { // skip receiver
			in := ty.In(p)
			switch {
			case in == names:
				branchesDoors++
			case in.Kind() == reflect.String:
				strings++
			case in.Kind() == reflect.Struct:
				t.Fatalf("%s takes a bare struct parameter %s — identifiers arrive only through BranchNames (L3)", m.Name, in)
			}
		}
		if branchesDoors != 1 {
			t.Fatalf("%s must take BranchNames exactly once, got %d", m.Name, branchesDoors)
		}
		if strings > 2 {
			t.Fatalf("%s takes %d bare strings — dir and message are all a code action may accept from callers", m.Name, strings)
		}
	}
	if seen != 3 {
		t.Fatalf("the three git actions must exist, saw %d", seen)
	}
}

func TestCommitAndPushIsOutward(t *testing.T) {
	r, err := Check(GitCommitAndPush)
	if err != nil || r.Class != EffectOutward || r.Verdict != VerdictAsk {
		t.Fatalf("commitAndPush pushes — it affects everybody who fetches, so it asks: %+v (%v)", r, err)
	}
	for _, name := range []string{GitCreateBranch, GitMergeBranch} {
		r, err := Check(name)
		if err != nil || r.Class != EffectLocal || r.Verdict != VerdictRun {
			t.Fatalf("%s affects nobody: %+v (%v)", name, r, err)
		}
	}
}

// TestPushVerifiedByRereadingRef — O8 through the ACTION: a fake whose push
// claims success while the remote ref sits still must fail the action, not
// just the plumbing.
func TestPushVerifiedByRereadingRef(t *testing.T) {
	f := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "rev-parse" && strings.Join(args, " ") == "rev-parse HEAD":
			return "newsha\n", nil
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "", &git.GitError{Kind: git.KindExit, Code: 128}
		case args[0] == "rev-parse" && strings.Contains(strings.Join(args, " "), "--abbrev-ref"):
			return "task/4a17-version/fix-parse-order\n", nil // the tree stands where the identity says
		case args[0] == "status":
			return "M  file.go\n", nil // staged: the assertion accepts staged lines
		case args[0] == "commit":
			return "", nil
		case args[0] == "push":
			return "", nil // claims success
		case args[0] == "ls-remote":
			return "oldsha\trefs/heads/task/4a17-version/fix-parse-order\n", nil // ref did not move
		}
		return "", nil
	}}
	_, err := actionsWith(f).CommitAndPush(context.Background(), "/repo", mustNames(t), "work")
	if err == nil {
		t.Fatal("a lying push must fail the action (O8), not just the plumbing")
	}
	if !strings.Contains(err.Error(), git.KindPushUnverified) {
		t.Fatalf("must surface push-unverified, got %v", err)
	}
}

// TestCompletenessAssertionApplies — D3 through the ACTION, in its two
// real shapes. (1) The hand-pick shape of the recorded incident: a step
// staged a narrow set and left a stray — the action's `add -A` CAPTURES
// the leftover, so nothing can be dropped by the squash merge; the
// strongest outcome is the stray riding into the commit, proven on real
// git. (2) The backstop shape: if staging was ever bypassed or raced
// (staged set complete in the index but work still loose in the tree),
// the assertion fails the commit BEFORE committing — proven with a fake
// whose tree shows a loose file at assertion time, the state a bypass
// leaves, and asserting the log never reaches `commit`.
func TestCompletenessAssertionApplies(t *testing.T) {
	// Shape (1): hand-pick captured, leftover committed.
	dir, _ := realRepoWithOrigin(t)
	names := mustNames(t)
	a := &GitActions{Git: gitReal()}
	if _, err := a.CreateBranch(context.Background(), dir, names, BranchTask); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "wanted.txt", "wanted")
	mustExec(t, dir, "git", "add", "wanted.txt") // the hand-pick
	writeFile(t, dir, "stray.txt", "do not drop me")
	if _, err := a.CommitAndPush(context.Background(), dir, names, "work"); err != nil {
		t.Fatalf("stage-everything must CAPTURE the hand-pick leftover, not trip on it: %v", err)
	}
	showed := mustExec(t, dir, "git", "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(showed, "stray.txt") {
		t.Fatalf("the leftover must ride in the commit — dropped-by-the-squash starts with dropped-by-the-stage: %s", showed)
	}

	// Shape (2): the backstop — loose work at assertion time fails the
	// commit before it can happen.
	f := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "rev-parse" && strings.Contains(strings.Join(args, " "), "--abbrev-ref"):
			return "task/4a17-version/fix-parse-order\n", nil
		case args[0] == "status" && !strings.Contains(strings.Join(args, " "), "untracked"):
			return "M  file.go\n?? race-left-behind.go\n", nil
		case args[0] == "status":
			return "M  file.go\n?? race-left-behind.go\n", nil
		case args[0] == "rev-parse":
			return "somesha\n", nil
		}
		return "", nil
	}}
	_, err := actionsWith(f).CommitAndPush(context.Background(), "/repo", mustNames(t), "work")
	if err == nil {
		t.Fatal("loose work at assertion time must fail the commit (D3)")
	}
	if !strings.Contains(err.Error(), "race-left-behind.go") {
		t.Fatalf("the refusal must name the loose file: %v", err)
	}
	for _, call := range f.log {
		if call[0] == "commit" {
			t.Fatalf("the assertion must fail the commit BEFORE it — a committed partial is the incident: %v", f.log)
		}
	}
}

// TestAddWorktreeThenRemoveWorktree — S32 T3 (PA5). The worktree door is
// real-git against a throwaway repo (never a fake git): add makes a branch
// and a worktree at the base, remove tears it down, and the second add is a
// Reused no-op (the idempotency recovery's PA12 path relies on).
func TestAddWorktreeThenRemoveWorktree(t *testing.T) {
	ctx := context.Background()
	dir, _ := realRepoWithOrigin(t)
	a := &GitActions{Git: gitReal()}
	names := mustNames(t)
	wt := filepath.Join(t.TempDir(), "wt")

	res, err := a.AddWorktree(ctx, dir, wt, names)
	if err != nil {
		t.Fatalf("addWorktree: %v", err)
	}
	if res.Action != "git.addWorktree" {
		t.Fatalf("want git.addWorktree, got %q", res.Action)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the worktree directory was not created: %v", err)
	}
	out := mustExec(t, dir, "git", "worktree", "list")
	if !strings.Contains(out, wt) {
		t.Fatalf("git worktree list is missing the path %s: %s", wt, out)
	}

	// A second add of the same branch is the Reused no-op (PA12), never a
	// create-then-fail.
	again, err := a.AddWorktree(ctx, dir, wt, names)
	if err != nil {
		t.Fatalf("second addWorktree: %v", err)
	}
	if !again.Reused {
		t.Fatalf("a second add of the same branch must be Reused, got %+v", again)
	}

	if _, err := a.RemoveWorktree(ctx, dir, wt); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the worktree directory still exists after remove (err=%v)", err)
	}
	// Removing a missing worktree is a no-op, not a fault.
	if _, err := a.RemoveWorktree(ctx, dir, wt); err != nil {
		t.Fatalf("a second removeWorktree must be a no-op, got %v", err)
	}
}
