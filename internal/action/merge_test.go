package action

// T6's tests, written first. One task with the guard (R6): the guard is
// not a check bolted beside the merge — it is what makes the merge
// correct, and this is the only `irreversible` action in the registry.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
)

// gitReal is the real-git client for these tests, and it carries an
// environment: a `git` client with none REFUSES rather than inheriting the
// test process's (S59 T5, EV10), so a test that wants the real binary has to
// say what the child may see. The identity variables are here rather than in
// the repository's config so the tests do not read an operator's ~/.gitconfig.
func gitReal() *git.Client {
	return git.NewClient([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_AUTHOR_NAME=s59-fixture", "GIT_AUTHOR_EMAIL=s59@aeon.test",
		"GIT_COMMITTER_NAME=s59-fixture", "GIT_COMMITTER_EMAIL=s59@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
	})
}

// mergeProbe records WHICH sha the post-merge guard consulted — the
// test's sharpest instrument, because "asked about the squash SHA and
// never about the originals" is exactly the half a plausible wrong
// implementation gets wrong.
type mergeProbe struct {
	answers map[string]bool // sha → ancestor? (missing key = scripted true)
	queries []string
	err     error
}

func (p *mergeProbe) IsAncestor(_ context.Context, _, sha, _ string) (bool, error) {
	if p.err != nil {
		return false, p.err
	}
	p.queries = append(p.queries, sha)
	if v, ok := p.answers[sha]; ok {
		return v, nil
	}
	return true, nil
}

func mergeArgsFor(pr int) MergeArgs {
	names, _ := NewBranchNames("4a17-version", "4a17")
	return MergeArgs{Dir: "/repo", Names: names, PRNumber: pr}
}

// approveMerge posts the approval S17 T8's merge gate requires — the gate
// is a PROPERTY (a merge without a review is unreachable), so every test
// that walks past the gate posts its review the way the review lane does.
func approveMerge(t *testing.T, f *forge.Fake, pr int) {
	t.Helper()
	if _, err := f.PostReview(context.Background(), pr, "lgtm", forge.VerdictApprove); err != nil {
		t.Fatal(err)
	}
}

// TestMisBasedPRIsRefusedBeforeMerging — the phantom-merge ROOT CAUSE
// (D5). A PR merged into the wrong base looks merged, reports merged, and
// its work is not where the loop believes it is; the symptom appears
// several steps later as a test failing for no visible reason. Refusal
// BEFORE merging, spy at zero: MergePR never even called.
func TestMisBasedPRIsRefusedBeforeMerging(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "main"}) // wrong base
	p := &mergeProbe{}
	a := &ForgeActions{Forge: f, Merge: p}
	_, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err == nil {
		t.Fatal("a PR based on main must be refused before anything is merged (D5)")
	}
	if !strings.Contains(err.Error(), "base") || !strings.Contains(err.Error(), "feature/4a17-version") {
		t.Fatalf("the refusal must name the base mismatch and the expected feature branch: %v", err)
	}
	for _, c := range f.Calls() {
		if c.Method == "MergePR" {
			t.Fatalf("the guard runs BEFORE merging — MergePR was called: %v", f.Calls())
		}
	}
}

// TestSquashSHAIsAncestorAndOriginalsAreNot — the highest-value test in
// the slice (D5/C5). BOTH directions: the squash SHA IS an ancestor, an
// original commit is NOT. A test asserting only the first passes against
// a guard checking the wrong commits, and the natural response to that
// failure is to relax the guard, which removes the check entirely.
func TestSquashSHAIsAncestorAndOriginalsAreNot(t *testing.T) {
	ctx := context.Background()

	// The real-git truth table: after a squash merge the squash commit is
	// an ancestor of the feature branch; the task branch's originals
	// never are — even after a perfectly correct merge.
	dir := squashRepo(t)
	client := gitReal()
	squash := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "feature"))
	original := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "task/4a17-version/4a17"))
	if ok, err := client.IsAncestor(ctx, dir, squash, "feature"); err != nil || !ok {
		t.Fatalf("the squash SHA must be an ancestor: %v (%v)", ok, err)
	}
	if ok, err := client.IsAncestor(ctx, dir, original, "feature"); err != nil || ok {
		t.Fatalf("the ORIGINAL must not be an ancestor — the trap this test exists for: %v (%v)", ok, err)
	}

	// The action's guard consults the MERGE RESULT's sha and nothing
	// else: asked == [mergeSHA], never the original.
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	approveMerge(t, f, 3)
	p := &mergeProbe{}
	a := &ForgeActions{Forge: f, Merge: p}
	res, err := a.MergePR(ctx, mergeArgsFor(3))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(p.queries) != 1 || p.queries[0] != res.SHA {
		t.Fatalf("the guard must ask about the squash SHA exactly once, asked %v (merge SHA %q, original %q)", p.queries, res.SHA, original)
	}
	if p.queries[0] == original {
		t.Fatal("the guard asked about the ORIGINAL — every squash merge would read as failed")
	}
	if res.SHA == "" || res.AlreadyMerged {
		t.Fatalf("a fresh merge reports its SHA: %+v", res)
	}
}

func TestNeverMergesTwice(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version", Merged: true, MergeSHA: "cafe0000"})
	p := &mergeProbe{}
	a := &ForgeActions{Forge: f, Merge: p}
	before := len(f.Calls())
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("already-merged must never fail: %v", err)
	}
	if res.SHA != "cafe0000" || !res.AlreadyMerged {
		t.Fatalf("the recorded merge SHA is returned: %+v", res)
	}
	for _, c := range f.Calls()[before:] {
		if c.Method == "MergePR" {
			t.Fatalf("never merge twice: %v", f.Calls()[before:])
		}
	}
	if len(p.queries) != 0 {
		t.Fatalf("an already-merged PR needs no post-merge git at all, queried %v", p.queries)
	}
}

// TestAlreadyMergedIsNotAFailure — explicit, the case "never fail"
// exists for: an already-merged PR is the desired END STATE.
func TestAlreadyMergedIsNotAFailure(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version", Merged: true, MergeSHA: "beef0000"})
	a := &ForgeActions{Forge: f, Merge: &mergeProbe{}}
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("the desired end state reported as failure: %v", err)
	}
	if res.AlreadyMerged {
		return
	}
	t.Fatalf("the result must say it was already merged: %+v", res)
}

func TestIrreversibleAsks(t *testing.T) {
	r, err := Check(ForgeMerge)
	if err != nil || r.Class != EffectIrreversible || r.Verdict != VerdictAsk {
		t.Fatalf("forge.merge is irreversible and asks: %+v (%v)", r, err)
	}
	ask, err := BuildAsk(ForgeMerge, Target{Repository: "cjlapao/throwaway", PRNumber: 3, BaseRef: "feature/4a17-version"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	for _, want := range []string{"#3", "feature/4a17-version", "cjlapao/throwaway"} {
		if !strings.Contains(ask.Body, want) {
			t.Fatalf("the ask must carry %q — \"approve forge.merge?\" tells a human nothing about what they are merging (O1): %s", want, ask.Body)
		}
	}
}

func TestUnattendedDeniesMerge(t *testing.T) {
	r, err := Resolve(ForgeMerge, false, false)
	if err != nil || r.Verdict != VerdictDeny {
		t.Fatalf("the only action that cannot be undone is denied unattended: %+v (%v)", r, err)
	}
	if !strings.Contains(r.Sentence, "effect class 'irreversible' resolves to deny (loop is unattended)") {
		t.Fatalf("denied naming the class (D8): %q", r.Sentence)
	}
}

// TestProtectedBranchIsAnHonestFailure (N4) — a repository that refuses
// produces a failure NAMING the refusal. No merge-queue or
// protected-branch workaround: working around branch protection is
// working around a decision someone made deliberately. The call log
// proves no workaround: exactly ONE merge attempt, no retry with another
// method, no push, no checkout.
func TestProtectedBranchIsAnHonestFailure(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	approveMerge(t, f, 3)
	// The no-workaround count starts AFTER the gate's own review — the
	// property is "nothing tried AFTER the refused merge" (T8's
	// PostReview is gate input, not a workaround).
	baseline := len(f.Calls())
	f.FailMethod("MergePR", forge.NewMsg(forge.KindRefused, "branch is protected"))
	p := &mergeProbe{}
	a := &ForgeActions{Forge: f, Merge: p}
	_, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err == nil {
		t.Fatal("a refusal must be an honest failure, not a workaround")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Fatalf("the failure must name the refusal: %v", err)
	}
	merges, others := 0, 0
	for _, c := range f.Calls()[baseline:] {
		switch c.Method {
		case "MergePR":
			merges++
		case "OpenPR", "PostReview", "CloseIssue":
			others++
		}
	}
	if merges != 1 || others != 0 {
		t.Fatalf("exactly one merge attempt, no workaround in any shape: %v", f.Calls())
	}
	if len(p.queries) != 0 {
		t.Fatalf("a refused merge has nothing to verify, queried %v", p.queries)
	}
}

// TestPostMergeGuardFailureIsLoud — if the squash SHA is somehow NOT an
// ancestor after a reported merge, the action FAILS rather than reporting
// success. This is the phantom merge caught after the fact: the forge
// said merged, the tree says otherwise, and the tree wins.
func TestPostMergeGuardFailureIsLoud(t *testing.T) {
	f2 := forge.NewFake()
	f2.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	approveMerge(t, f2, 3)
	p := &mergeProbeAllNo{}
	a2 := &ForgeActions{Forge: f2, Merge: p}
	res, err := a2.MergePR(context.Background(), mergeArgsFor(3))
	if err == nil {
		t.Fatalf("a reported merge the tree contradicts must FAIL, not report success: %+v", res)
	}
	if !strings.Contains(err.Error(), "phantom") {
		t.Fatalf("the failure must name the phantom (D5): %v", err)
	}
	if strings.Contains(err.Error(), "3") == false {
		t.Fatalf("the failure must name the PR: %v", err)
	}
}

type mergeProbeAllNo struct{ queries []string }

func (p *mergeProbeAllNo) IsAncestor(_ context.Context, _, sha, _ string) (bool, error) {
	p.queries = append(p.queries, sha)
	return false, nil
}

// squashRepo builds the recorded scenario for real: a task branch whose
// work lands on feature by a SQUASH merge — the originals are not
// ancestors, the squash commit is.
func squashRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustExec(t, dir, "git", "init", "-b", "feature")
	mustExec(t, dir, "git", "config", "user.email", "t@aeon.test")
	mustExec(t, dir, "git", "config", "user.name", "aeon test")
	mustExec(t, dir, "git", "config", "commit.gpgsign", "false")
	writeFile(t, dir, "base.txt", "base")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "base")
	mustExec(t, dir, "git", "checkout", "-b", "task/4a17-version/4a17")
	writeFile(t, dir, "work.txt", "work")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "work")
	mustExec(t, dir, "git", "checkout", "feature")
	mustExec(t, dir, "git", "merge", "--squash", "task/4a17-version/4a17")
	mustExec(t, dir, "git", "commit", "-m", "task/4a17-version/4a17 (squashed)")
	return dir
}
