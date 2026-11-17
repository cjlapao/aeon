package action

// S60 T1 · MQ characterization goldens for the action layer.
//
// These pin today's behaviour on the untouched code so the slice can prove
// what it changed and what it did not. Each golden's header names the later
// task expected to change it and how:
//
//   - branch_names — T4 changes NOTHING in CreateBranch itself; it adds the
//     loop-level base pin beside this call site. When T4 lands, the start-SHA
//     of a FEATURE branch may stop being plain HEAD in a pinned loop (the
//     pin says `origin/main`); a loop without a pin keeps every byte here.
//   - merge_branch — unchanged by T5/T6 (git.mergeBranch stays as it is;
//     git.integrate is a NEW method, not this one's modification).
//   - add_worktree — unchanged by T5 (git.addWorktree keeps the F17 shortcut;
//     AddIntegrationWorktree verifies and rebuilds instead — a new door).
//   - forge_merge / forge_openPR — T6 must not change one byte of MergePR's
//     guards or OpenPR's: `forge.land` reuses `effectiveRule`/`mergeGate`
//     and a sibling `landPR`; this golden proves the reused half is untouched.
//
// Normalisation: the throwaway repo path becomes $REPO and any 40-hex commit
// SHA becomes $SHA (a fixture measurement, not behaviour). Nothing else
// varies; anything that does after this commit is a finding.

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
)

var mqGoldenUpdate = flag.Bool("mq-golden-update", false, "rewrite internal/action/testdata/mq_golden from today's behaviour (Phase A ritual only)")

// mqGolden compares got against testdata/mq_golden/<name>.golden.
func mqGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "mq_golden", name+".golden")
	if *mqGoldenUpdate {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run `-run MQ -mq-golden-update` on untouched code to mint it): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s drifted from the golden.\nIf the drift is a task this file's header says is expected, update the golden AND say what changed.\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}

var mqSHA = regexp.MustCompile(`[0-9a-f]{40}`)

func mqNorm(dir string, s string) string {
	s = strings.ReplaceAll(s, dir, "$REPO")
	return mqSHA.ReplaceAllString(s, "$$SHA") // $$ = literal $ in Expand
}

func mqGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(mustExec(t, dir, "git", args...))
}

func mqRev(t *testing.T, dir, ref string) string {
	t.Helper()
	return mqGit(t, dir, "rev-parse", ref)
}

func mqCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	return mqGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

func mqRealActions() *GitActions { return &GitActions{Git: gitReal()} }

// TestBranchNamesAndBaseGolden pins F3/F4/F5: the branch NAMES, the SHA each
// branch starts at (today: HEAD at call time — never a configured base), and
// the checkout side effects (task checks out, feature never does).
// Changer: T4 (loop-level base pin) — see header.
func TestBranchNamesAndBaseGolden(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	ctx := context.Background()
	a := mqRealActions()
	names, err := NewBranchNames("4a17-version", "fix-parse-order")
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	head := mqRev(t, dir, "HEAD")

	// feature from main.
	res, err := a.CreateBranch(ctx, dir, names, BranchFeature)
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("feature.on-main: branch=" + res.Branch + " reused=" + boolStr(res.Reused) +
		" checkedOutAfter=" + mqCurrentBranch(t, dir) + " startsAt=" + mqRev(t, dir, res.Branch) + " (== HEAD " + head + ")\n")

	// feature again: reuse, no side effect.
	res2, err := a.CreateBranch(ctx, dir, names, BranchFeature)
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("feature.again: reused=" + boolStr(res2.Reused) + " checkedOutAfter=" + mqCurrentBranch(t, dir) + "\n")

	// task from the same HEAD: it checks the workspace out onto the task branch.
	mustExec(t, dir, "git", "checkout", "main")
	res3, err := a.CreateBranch(ctx, dir, names, BranchTask)
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("task.on-main: branch=" + res3.Branch + " reused=" + boolStr(res3.Reused) +
		" checkedOutAfter=" + mqCurrentBranch(t, dir) + " startsAt=" + mqRev(t, dir, res3.Branch) + " (== HEAD " + head + ")\n")

	// From a SIDE branch: today the base is still HEAD — CreateBranch knows
	// nothing about default branches (the MQ15 gap, closed by T4's pin).
	mustExec(t, dir, "git", "checkout", "-b", "side/other")
	writeFile(t, dir, "side.txt", "side\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "side commit")
	side := mqRev(t, dir, "HEAD")
	names2, err := NewBranchNames("9z-side", "side-task")
	if err != nil {
		t.Fatal(err)
	}
	res4, err := a.CreateBranch(ctx, dir, names2, BranchFeature)
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("feature.on-side: startsAt=" + mqRev(t, dir, res4.Branch) +
		" (== side HEAD " + side + " — today the base is HEAD, never the default branch)\n")

	mqGolden(t, "branch_names", mqNorm(dir, b.String()))
}

// TestMergeBranchGolden pins F9: the clean merge, the already-merged no-op,
// and the CONFLICT pair — the GitError{KindConflict} text and the tree state
// it leaves behind (unmerged paths in the index while parked on the task
// branch). Changer: none — T5's git.integrate is a new sibling, never a
// modification of this method.
func TestMergeBranchGolden(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	ctx := context.Background()
	a := mqRealActions()
	names, err := NewBranchNames("4a17-version", "fix-parse-order")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder

	// clean: one commit each side. CreateBranch does NOT check the feature
	// branch out (F4 pins that), so the feature work is committed after an
	// explicit checkout — exactly what a loop's work step does.
	if _, err := a.CreateBranch(ctx, dir, names, BranchFeature); err != nil {
		t.Fatal(err)
	}
	mustExec(t, dir, "git", "checkout", names.FeatureBranch())
	writeFile(t, dir, "feature.txt", "feature\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "feature work")
	mustExec(t, dir, "git", "checkout", "main")
	if _, err := a.CreateBranch(ctx, dir, names, BranchTask); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "task.txt", "task\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "task work")
	res, err := a.MergeBranch(ctx, dir, names)
	if err != nil {
		t.Fatal(err)
	}
	parents := strings.Fields(mustExec(t, dir, "git", "rev-list", "--parents", "-n", "1", "HEAD"))
	b.WriteString("clean: merged=" + boolStr(res.Merged) + " branch=" + res.Branch +
		" mergeCommitParents=" + boolStr(len(parents) == 3) + " checkedOutAfter=" + mqCurrentBranch(t, dir) + "\n")

	// already merged: no-op, not a failure.
	res2, err := a.MergeBranch(ctx, dir, names)
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("again: alreadyMerged=" + boolStr(res2.AlreadyMerged) + "\n")

	// conflict: both sides edit the same line.
	mustExec(t, dir, "git", "checkout", names.FeatureBranch())
	writeFile(t, dir, "shared.txt", "feature line\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "feature edits shared")
	mustExec(t, dir, "git", "checkout", names.TaskBranch())
	writeFile(t, dir, "shared.txt", "task line\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "task edits shared")
	_, err = a.MergeBranch(ctx, dir, names)
	ge, ok := err.(*git.GitError)
	if !ok {
		t.Fatalf("a conflicting merge must yield a *git.GitError, got %T: %v", err, err)
	}
	status := mustExec(t, dir, "git", "status", "--porcelain")
	b.WriteString("conflict: kind=" + string(ge.Kind) + " detail=" + mqNorm(dir, ge.Detail) +
		" leftOn=" + mqCurrentBranch(t, dir) + " unmergedStatus=" + firstToken(status) + "\n")

	mqGolden(t, "merge_branch", mqNorm(dir, b.String()))
}

// TestAddWorktreeReuseShortcutGolden pins F17: AddWorktree with the branch
// PRESENT and the directory ABSENT returns Reused — success without a
// directory. The engine cannot tell this from a real worktree, which is why
// git.integrate (T5) verifies the directory and rebuilds. Changer: none —
// T5 leaves this method byte-identical by contract.
func TestAddWorktreeReuseShortcutGolden(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	ctx := context.Background()
	a := mqRealActions()
	var b strings.Builder

	names, err := NewBranchNames("4a17-version", "unit-a")
	if err != nil {
		t.Fatal(err)
	}
	// fresh: directory appears, branch at the feature tip (absent here → HEAD).
	wt := filepath.Join(dir, ".worktrees", "lane-unit-a")
	res, err := a.AddWorktree(ctx, dir, wt, names)
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(wt)
	b.WriteString("fresh: reused=" + boolStr(res.Reused) + " directoryExists=" + boolStr(statErr == nil) +
		" baseSHA-==-HEAD=" + boolStr(res.SHA == mqRev(t, dir, "HEAD")) + "\n")

	// The shortcut: a SECOND names value whose branch exists but whose
	// directory does not — Reused, and NO directory is made.
	names2, err := NewBranchNames("4a17-version", "unit-b")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, dir, "git", "branch", names2.TaskBranch()) // branch exists, no worktree
	wt2 := filepath.Join(dir, ".worktrees", "lane-unit-b")
	res2, err := a.AddWorktree(ctx, dir, wt2, names2)
	if err != nil {
		t.Fatal(err)
	}
	_, statErr2 := os.Stat(wt2)
	b.WriteString("shortcut: reused=" + boolStr(res2.Reused) + " error=" + boolStr(err == nil) +
		" directoryExists=" + boolStr(statErr2 == nil) + " (F17: success WITHOUT a directory)\n")

	mqGolden(t, "add_worktree", mqNorm(dir, b.String()))
}

// TestForgeMergeGuardsGolden pins F7/F8 against the Fake: the base-mismatch
// refusal text, the no-review refusal text, the pass with an approval (and
// the evidence keys it records), and the PhantomMergeError text.
// Changer: none — T6 reuses this machinery for forge.land and must not move
// one byte of it.
func TestForgeMergeGuardsGolden(t *testing.T) {
	ctx := context.Background()
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "main"})
	f.SeedPR(forge.PR{Number: 4, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	p := &mergeProbe{}
	a := &ForgeActions{Forge: f, Merge: p}
	names, _ := NewBranchNames("4a17-version", "4a17")

	var b strings.Builder
	_, err := a.MergePR(ctx, MergeArgs{Dir: "/repo", Names: names, PRNumber: 3, Loop: "4a17-version"})
	b.WriteString("base-mismatch: err=" + errLine(err) + "\n")

	_, err = a.MergePR(ctx, MergeArgs{Dir: "/repo", Names: names, PRNumber: 4, Loop: "4a17-version"})
	b.WriteString("no-review: err=" + errLine(err) + "\n")

	approveMerge(t, f, 4)
	res, err := a.MergePR(ctx, MergeArgs{Dir: "/repo", Names: names, PRNumber: 4, Loop: "4a17-version"})
	if err != nil {
		t.Fatalf("an approved, correctly based merge must pass: %v", err)
	}
	b.WriteString("approved: pr=" + itoaInt(res.PR) + " alreadyMerged=" + boolStr(res.AlreadyMerged) +
		" evidenceKeys=" + strings.Join(sortedKeys(res.Fields), ",") + "\n")
	b.WriteString("approved: mergeSHA-shape=" + shaShape(res.SHA) + "\n")

	// Phantom: the forge says merged, the tree says no.
	f.SeedPR(forge.PR{Number: 5, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	approveMerge(t, f, 5)
	p.answers = map[string]bool{}
	p.err = nil
	a2 := &ForgeActions{Forge: f, Merge: &phantomProbe{}}
	_, err = a2.MergePR(ctx, MergeArgs{Dir: "/repo", Names: names, PRNumber: 5, Loop: "4a17-version"})
	b.WriteString("phantom: type=" + typeName(err) + " err=" + errLine(err) + "\n")

	mqGolden(t, "forge_merge", mqNorm("/repo", b.String()))
}

// phantomProbe is a merge witness that always answers "not an ancestor" —
// the phantom shape: the forge merged, the tree disagrees.
type phantomProbe struct{}

func (phantomProbe) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func shaShape(s string) string {
	if mqSHA.MatchString(s) {
		return "$SHA"
	}
	return s
}

// TestOpenPRGuardsGolden pins F6: the head-missing BlockedError text when the
// remote does not carry the task branch, and reuse-by-head (no second PR).
// Changer: none — T6's forge.openLandingPR is a new action beside this one.
func TestOpenPRGuardsGolden(t *testing.T) {
	dir, _ := realRepoWithOrigin(t)
	ctx := context.Background()
	f := forge.NewFake()
	a := &ForgeActions{Forge: f, Git: gitReal()}
	names, _ := NewBranchNames("4a17-version", "fix-parse-order")

	var b strings.Builder
	_, err := a.OpenPR(ctx, ForgeArgs{Dir: dir, Names: names, TaskTitle: "fix parse order", Base: "main"})
	b.WriteString("head-missing: type=" + typeName(err) + " err=" + mqNorm(dir, errLine(err)) + "\n")

	mustExec(t, dir, "git", "push", "-u", "origin", "main")
	mustExec(t, dir, "git", "checkout", "-b", names.TaskBranch())
	writeFile(t, dir, "work.txt", "work\n")
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "work")
	mustExec(t, dir, "git", "push", "-u", "origin", names.TaskBranch())
	res, err := a.OpenPR(ctx, ForgeArgs{Dir: dir, Names: names, TaskTitle: "fix parse order", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("open: number=" + itoaInt(res.Number) + " reused=" + boolStr(res.Reused) + "\n")
	res2, err := a.OpenPR(ctx, ForgeArgs{Dir: dir, Names: names, TaskTitle: "fix parse order", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString("again: number=" + itoaInt(res2.Number) + " reused=" + boolStr(res2.Reused) + "\n")

	mqGolden(t, "forge_openPR", mqNorm(dir, b.String()))
}

// --- small textual helpers (the goldens' normalisers) ----------------------

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return strings.Fields(s)[0]
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func errLine(err error) string {
	if err == nil {
		return "<nil>"
	}
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " | ")
}

func typeName(err error) string {
	if err == nil {
		return "<nil>"
	}
	switch err.(type) {
	case *BlockedError:
		return "*BlockedError"
	case *PhantomMergeError:
		return "*PhantomMergeError"
	case *git.GitError:
		return "*git.GitError"
	}
	return "other"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
