package action

// S60 T5's tests. The queue's git is REAL here (the same realRepoWithOrigin
// S16 learned to trust): merge conflicts, worktree mounts and merge commits
// are exactly the things a fake cannot answer honestly about. The fakes stay
// where the interesting question is the COMMAND LOG (bounded paths, banned
// verbs, the name gate before any child process runs).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/git"
)

const mqItem = "mq-0123456789abcdef0123456789abcdef"

func mqActions() *GitActions {
	return &GitActions{Git: &git.Client{Env: append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")}}
}

// mqRepo is realRepoWithOrigin plus the queue's two branches: main advances
// on one side, feature/alpha on the other — the shape the queue integrates.
// `conflict` makes the two sides edit the SAME line.
func mqRepo(t *testing.T, conflict bool) (string, *GitActions) {
	t.Helper()
	dir, _ := realRepoWithOrigin(t)
	a := mqActions()
	// feature/alpha edits seed.txt (conflict) or adds a file (clean).
	mustExec(t, dir, "git", "checkout", "-b", "feature/alpha")
	if conflict {
		writeFile(t, dir, "seed.txt", "feature side\n")
	} else {
		writeFile(t, dir, "feature.txt", "alpha\n")
	}
	mustExec(t, dir, "git", "add", "-A")
	mustExec(t, dir, "git", "commit", "-m", "alpha work")
	mustExec(t, dir, "git", "push", "-u", "origin", "feature/alpha")
	mustExec(t, dir, "git", "checkout", "main")
	if conflict {
		writeFile(t, dir, "seed.txt", "main side\n")
		mustExec(t, dir, "git", "add", "-A")
		mustExec(t, dir, "git", "commit", "-m", "main advanced")
		mustExec(t, dir, "git", "push", "origin", "main")
	}
	return dir, a
}

// mqCheckout is the person's checkout AS A FINGERPRINT: tracked index state,
// HEAD and the tree it names. The queue's whole promise (MQ7) is that this
// string is the same before and after everything the queue does. `-uno` is
// deliberate and recorded: the queue's own `.worktrees/` directory must
// APPEAR as untracked (it is a real directory the person may inspect), and
// the pre-S60 dirty checks read status with untracked off (git_actions.go
// T5's note in S32) — the fingerprint pins exactly what those checks pin.
func mqCheckout(t *testing.T, dir string) string {
	t.Helper()
	return mustExec(t, dir, "git", "status", "--porcelain=v2", "-z", "-uno") + "\x00" +
		mustExec(t, dir, "git", "rev-parse", "HEAD") + "\x00" +
		mustExec(t, dir, "git", "rev-parse", "HEAD^{tree}")
}

func TestIntegrateCleanCutsMergesAndReports(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, false)
	frozen := mqCheckout(t, dir)
	ib := "integrate/" + mqItem
	res, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	if err != nil {
		t.Fatalf("clean integrate: %v", err)
	}
	if res.Dir != IntegrationWorktreePath(dir, mqItem) || !isWorktree(res.Dir) {
		t.Fatalf("the merge happened somewhere the ledger cannot find: %+v", res)
	}
	if !res.Merged || res.AlreadyMerged || res.SHA == "" {
		t.Fatalf("a first clean integrate merges and says so: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "feature.txt")); err != nil {
		t.Fatalf("the merged file is not in the worktree: %v", err)
	}
	// The person's checkout never moved (MQ7's whole point).
	if b := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")); b != "main" {
		t.Fatalf("the queue checked out %q in the person's tree", b)
	}
	// The ref cut at the base tip and carries the item (MQ6.1/MQ6.3).
	baseTip := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "origin/main"))
	if !strings.Contains(mustExec(t, dir, "git", "log", "--format=%s", ib), "alpha work") {
		t.Fatal("the integration branch does not carry the item's commit")
	}
	if out := mustExec(t, dir, "git", "merge-base", "--is-ancestor", baseTip, ib); out != "" {
		t.Fatal("the integration branch does not descend from the base tip")
	}
	// …and the FINGERPRINT, not just the branch name: index, HEAD, tree.
	if after := mqCheckout(t, dir); after != frozen {
		t.Fatalf("the person's checkout moved:\n before %q\n after  %q", frozen, after)
	}
}

func TestIntegrateRerunAnswersAlreadyMerged(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, false)
	ib := "integrate/" + mqItem
	first, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	second, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	if err != nil {
		t.Fatalf("re-run after a clean merge must succeed, got: %v", err)
	}
	if !second.AlreadyMerged || second.Merged {
		t.Fatalf("the crash-retry rung must answer AlreadyMerged, not merge again: %+v", second)
	}
	if second.SHA != first.SHA {
		t.Fatalf("the re-run invented a different tip: %s vs %s", second.SHA, first.SHA)
	}
}

func TestIntegrateConflictIsTypedAndMergeStays(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, true)
	frozen := mqCheckout(t, dir)
	ib := "integrate/" + mqItem
	_, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("a conflicting merge must arrive typed, got %T: %v", err, err)
	}
	if len(ce.Paths) != 1 || ce.Paths[0] != "seed.txt" {
		t.Fatalf("conflict paths: %v", ce.Paths)
	}
	wt := IntegrationWorktreePath(dir, mqItem)
	// MQ6.2: the merge stays IN PROGRESS — nothing was aborted, committed
	// or pushed. MERGE_HEAD present is the durable proof.
	if _, merr := a.Git.Run(ctx, wt, "rev-parse", "-q", "--verify", "MERGE_HEAD"); merr != nil {
		t.Fatalf("the merge must be left in progress for the resolution loop: %v", merr)
	}
	if sha := strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", ib)); sha != strings.TrimSpace(mustExec(t, dir, "git", "rev-parse", "origin/main")) {
		t.Fatal("a conflicted integrate must not have committed anything onto the branch")
	}
	// Conclude refuses the half-resolved merge, naming the file.
	if _, cerr := a.ConcludeIntegration(ctx, wt, ib); cerr == nil || !strings.Contains(cerr.Error(), "seed.txt") {
		t.Fatalf("conclude must refuse an unresolved merge naming the files: %v", cerr)
	}
	// Resolve honestly, and the door opens.
	writeFile(t, wt, "seed.txt", "both sides, resolved\n")
	mustExec(t, wt, "git", "add", "seed.txt")
	res, cerr := a.ConcludeIntegration(ctx, wt, ib)
	if cerr != nil {
		t.Fatalf("conclude after resolution: %v", cerr)
	}
	if res.SHA == "" {
		t.Fatal("conclude must report the new integration sha")
	}
	// The conclusion IS a merge commit (two parents) — the history MQ6.3
	// keeps: both lineages visible, neither rewritten.
	mustExec(t, wt, "git", "rev-parse", "HEAD^2") // dies if not a merge commit
	// And it is idempotent: the crash-after-commit re-run asserts clean and
	// reports the same sha.
	again, err2 := a.ConcludeIntegration(ctx, wt, ib)
	if err2 != nil || again.SHA != res.SHA {
		t.Fatalf("conclude is the clean-tree assert restated: %+v (%v)", again, err2)
	}
	// THROUGH ALL OF IT — conflict, refusal, resolution, merge commit — the
	// person's checkout fingerprint never changed a byte.
	if after := mqCheckout(t, dir); after != frozen {
		t.Fatalf("the person's checkout moved across a conflicted landing:\n before %q\n after  %q", frozen, after)
	}
}

func TestConcludeRefusesCommittedMarkers(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, true)
	ib := "integrate/" + mqItem
	if _, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main"); err == nil {
		t.Fatal("repo setup lost the conflict")
	}
	wt := IntegrationWorktreePath(dir, mqItem)
	// The dishonest resolution: the model STAGED the file with the markers
	// still inside. The index no longer calls it unmerged; only the marker
	// scan can catch it — and MQ14 says a marker may not be committed.
	mustExec(t, wt, "git", "add", "seed.txt")
	_, err := a.ConcludeIntegration(ctx, wt, ib)
	if err == nil || !strings.Contains(err.Error(), "conflict markers") || !strings.Contains(err.Error(), "seed.txt") {
		t.Fatalf("markers must be named and refused: %v", err)
	}
	// The honest fix (markers gone, add, conclude) passes the same door.
	writeFile(t, wt, "seed.txt", "resolved without markers\n")
	mustExec(t, wt, "git", "add", "seed.txt")
	if _, err := a.ConcludeIntegration(ctx, wt, ib); err != nil {
		t.Fatalf("the honest resolution must pass: %v", err)
	}
}

func TestConflictPayloadIsPathsOnlyAndBounded(t *testing.T) {
	// MQ8/MQ14: at most ConflictBound paths ride, the count never lies, and
	// the sentence carries names — never content.
	paths := make([]string, ConflictBound+50)
	for i := range paths {
		paths[i] = fmt.Sprintf("file-%03d.go", i)
	}
	ce := conflictError(paths)
	if len(ce.Paths) != ConflictBound || ce.Hidden != 50 {
		t.Fatalf("bound: %d carried, %d hidden", len(ce.Paths), ce.Hidden)
	}
	s := ce.Error()
	if !strings.Contains(s, "+50 more") || !strings.Contains(s, "file-000.go") || strings.Contains(s, "file-100.go") {
		t.Fatalf("sentence shape: %q", s)
	}
	small := conflictError([]string{"a.go", "b.go"})
	if strings.Contains(small.Error(), "+") {
		t.Fatalf("a short list must not pad: %q", small.Error())
	}
}

func TestPublishOnlyPublishesWhatTheQueueNamed(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, false)
	// The name gate runs BEFORE any child process: junk names and person
	// names alike refuse, and nothing is pushed to try.
	for _, bad := range []string{"main", "feature/alpha", "integrate/", "/integrate/x", "integrate/../x", "refs/heads/main"} {
		if _, err := a.PublishIntegration(ctx, dir, "", bad); err == nil || !strings.Contains(err.Error(), "not an integration branch") {
			t.Fatalf("publish must refuse %q: %v", bad, err)
		}
	}
	ib := "integrate/" + mqItem
	// From the person's checkout the door refuses for the right reason: F11
	// verifies a push against the tree that carries the branch, and main's
	// tree does not carry this one.
	if _, err := a.PublishIntegration(ctx, dir, "", ib); err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("publish of an unbuilt integration ref must refuse: %v", err)
	}
	// Build it, and the plain push (F11) lands a NEW ref — from the tree
	// that carries the branch, which is where the engine's WorkDir is.
	res0, err := a.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	res, err := a.PublishIntegration(ctx, res0.Dir, "", ib)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if res.SHA == "" {
		t.Fatal("publish must report pushed_sha evidence")
	}
	sha, err := a.Git.RemoteRefSHA(ctx, dir, ib)
	if err != nil || sha != res.SHA {
		t.Fatalf("origin does not carry the published ref: %q %q (%v)", sha, res.SHA, err)
	}
	// A prefix is part of the name it trusts.
	if _, err := a.PublishIntegration(ctx, res0.Dir, "4a17-", ib); err == nil {
		t.Fatal("with a prefix pinned, the unprefixed name is not the queue's")
	}
}

func TestAddIntegrationWorktreeIsF17Rebuilt(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, false)
	// (a) A branch with no directory REBUILDS by attaching — the shape F17
	// answered "Reused" for and left the loop without a tree.
	mustExec(t, dir, "git", "branch", "orphan-br")
	p := filepath.Join(dir, ".worktrees", "integrate-orphan")
	if _, err := a.AddIntegrationWorktree(ctx, dir, p, "orphan-br", ""); err != nil {
		t.Fatalf("attach the surviving branch: %v", err)
	}
	if !isWorktree(p) {
		t.Fatal("the rebuild produced no mount")
	}
	// (b) Branch AND directory live ⇒ Reused, no second add.
	res, err := a.AddIntegrationWorktree(ctx, dir, p, "orphan-br", "")
	if err != nil || !res.Reused {
		t.Fatalf("the live pair must reuse: %+v (%v)", res, err)
	}
	// (c) An absent branch with no named base REFUSES — MQ6.1 cuts at the
	// fetched base tip or not at all, never at a HEAD that happens to be
	// checked out.
	if _, err := a.AddIntegrationWorktree(ctx, dir, filepath.Join(dir, ".worktrees", "integrate-none"), "never-cut", ""); err == nil ||
		!strings.Contains(err.Error(), "refuses") {
		t.Fatalf("no branch and no base must refuse: %v", err)
	}
}

func TestIntegrationDispatchLocksTheIdentity(t *testing.T) {
	ctx := context.Background()
	dir, a := mqRepo(t, false)
	svc := &Services{Git: a}
	base := Args{Root: dir, Item: mqItem, HeadBranch: "feature/alpha", BaseBranch: "main", Loop: "land-1"}
	// A branch name that is not what (prefix, item) compose is refused
	// naming the derivation — the schema rule made structural.
	if _, err := Dispatch(ctx, svc, GitIntegrate, Args{Root: dir, Item: mqItem, IntegrationBranch: "integrate/someone-elses", HeadBranch: "feature/alpha", BaseBranch: "main"}); err == nil ||
		!strings.Contains(err.Error(), "not what the pinned prefix and item compose") {
		t.Fatalf("dispatch must re-derive and compare: %v", err)
	}
	// Missing identifiers refuse BY NAME before any service runs.
	for field, args := range map[string]Args{
		"item":               {Root: dir, IntegrationBranch: "integrate/" + mqItem, HeadBranch: "feature/alpha", BaseBranch: "main"},
		"integration_branch": {Root: dir, Item: mqItem, HeadBranch: "feature/alpha", BaseBranch: "main"},
		"head_branch":        {Root: dir, Item: mqItem, IntegrationBranch: "integrate/" + mqItem, BaseBranch: "main"},
		"base_branch":        base,
	} {
		a2 := args
		if field == "base_branch" {
			a2 = Args{Root: dir, Item: mqItem, IntegrationBranch: "integrate/" + mqItem, HeadBranch: "feature/alpha"}
		}
		_, err := Dispatch(ctx, svc, GitIntegrate, a2)
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("missing %s must refuse naming the field: %v", field, err)
		}
	}
	// An unsafe ref name refuses before argv is ever assembled.
	if _, err := Dispatch(ctx, svc, GitIntegrate, Args{Root: dir, Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		HeadBranch: "fe ature;rm -rf /", BaseBranch: "main"}); err == nil ||
		!strings.Contains(err.Error(), "no ref may hold") {
		t.Fatalf("an unsafe head_branch must refuse: %v", err)
	}
	// The dispatch happy path returns the registry's three evidence keys.
	ev, err := Dispatch(ctx, svc, GitIntegrate, Args{Root: dir, Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		HeadBranch: "feature/alpha", BaseBranch: "main"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	for _, k := range []string{"integration_branch", "integration_sha", "integration_dir"} {
		if ev[k] == "" || ev[k] == "(unset)" {
			t.Fatalf("evidence %s missing: %v", k, ev)
		}
	}
}

func TestSafeRefName(t *testing.T) {
	for _, ok := range []string{"feature/alpha", "Fix/Mixed-Case_1.2", mqItem, "main"} {
		if !safeRefName(ok) {
			t.Fatalf("%q is a legitimate ref the queue may merge", ok)
		}
	}
	for _, bad := range []string{"", "-flag", ".hidden", "a b", "a;rm", "a..b", "a/b/", "a~b", "a^b", "a:b"} {
		if safeRefName(bad) {
			t.Fatalf("%q must refuse", bad)
		}
	}
}

// TestIntegrationCodeAddsNoBannedVerb (MQ6.4, N8/N9 by extension): the
// history-rewrite verb is NEW to this repository with S60 — the pre-S60
// packages never named it at all. Re-scan the queue's three files with the
// git package's own concatenated-token discipline (the test file cannot
// indict itself, the scanned files cannot hide) plus the rewrite verb.
func TestIntegrationCodeAddsNoBannedVerb(t *testing.T) {
	banned := []string{
		"-" + "-force",
		"force-with-" + "lease",
		"+" + "refs/",
		"branch -" + "D",
		"branch -" + "d",
		"push -" + "f",
		"update-ref" + " -d",
		"tag -" + "d",
		"re" + "base",
		"up" + "date-ref -" + "d",
	}
	files := []string{
		"integration.go",
		filepath.Join("..", "loop", "integration.go"),
		filepath.Join("..", "git", "plumbing.go"),
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("the queue's own file %s is missing (%v) — this guard scans what it names", f, err)
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %q — the queue builds refs forward, it never rewrites or deletes them (MQ6)", f, b)
			}
		}
	}
}
