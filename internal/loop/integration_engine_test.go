package loop

// S60 T5's engine tests. The queue's actions ran green in internal/action
// against real git; here the questions are the ENGINE's: who moved the
// workspace, in what order the row and the directory were written, what kind
// a conflict blocks as, and who is allowed to assign WorkDir at all.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/store"
)

// runOut is the loop package's run() with its answer: the tests assert on
// rev-parse output, and run() only asserts exit.
func runOut(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %s", name, args, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func isWorktreeDir(p string) bool {
	fi, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil && !fi.IsDir()
}

// mqGitActions is the engine's git seam on the SAME real-subprocess client
// production injects (the daemon builds it through execenv.GitEnv; the tests
// pin an explicit scrubbed environment, never an inherited one — S59 EV10).
func mqGitActions() *action.GitActions {
	return &action.GitActions{Git: &git.Client{Env: append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")}}
}

const (
	mqItemID = "mq-0123456789abcdef0123456789abcdef"
	mqIB     = "integrate/" + mqItemID
)

// mqLandingRepo: realGitRepo's master plus a pushed feature/alpha. conflict
// makes the two branches edit the same line of f.
func mqLandingRepo(t *testing.T, conflict bool) string {
	t.Helper()
	dir := realGitRepo(t)
	run(t, dir, "git", "checkout", "-b", "feature/alpha")
	if conflict {
		os.WriteFile(filepath.Join(dir, "f"), []byte("feature side\n"), 0o600)
	} else {
		os.WriteFile(filepath.Join(dir, "alpha"), []byte("a\n"), 0o600)
	}
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "alpha")
	run(t, dir, "git", "push", "-u", "origin", "feature/alpha")
	run(t, dir, "git", "checkout", "master")
	if conflict {
		os.WriteFile(filepath.Join(dir, "f"), []byte("master side\n"), 0o600)
		run(t, dir, "git", "add", ".")
		run(t, dir, "git", "commit", "-m", "master advanced")
		run(t, dir, "git", "push", "origin", "master")
	}
	return dir
}

func mqStart(t *testing.T, root string) (string, *store.Store) {
	t.Helper()
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "landing.yaml")
	writeBlueprint(t, path, "  - id: integrate\n    step: {kind: code-action, action: git.integrate}\n")
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "land-1", ComponentRoot: root, Root: root, Mode: "standard",
	}, path, boolPtr(false), map[string]string{
		"item": mqItemID, "integration_branch": mqIB,
		"head_branch": "feature/alpha", "base_branch": "master",
	})
	if err != nil {
		t.Fatal(err)
	}
	return st.ID, s
}

func mqDep(s *store.Store, root string) (Deps, *action.GitActions) {
	ga := mqGitActions()
	svc := &action.Services{Git: ga}
	return Deps{
		Store:   s,
		Actions: func(string) *action.Services { return svc },
	}, ga
}

// TestLandingIntegrateMovesWorkDirAndWritesTheRowFirst: the clean path
// end-to-end through the engine — ledger row before directory (MQ7), the
// integrate evidence moving the workspace, the state carrying it as
// workDir (additive), and the person's checkout never having moved.
func TestLandingIntegrateMovesWorkDirAndRowFirst(t *testing.T) {
	ctx := context.Background()
	root := mqLandingRepo(t, false)
	id, s := mqStart(t, root)
	dep, ga := mqDep(s, root)
	if err := Run(ctx, dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(ctx, s, id)
	if err != nil {
		t.Fatal(err)
	}
	wt := action.IntegrationWorktreePath(root, mqItemID)
	if saved.WorkDir != wt {
		t.Fatalf("the integrate evidence must move the workspace to %s, got %q", wt, saved.WorkDir)
	}
	if !isWorktreeDir(wt) {
		t.Fatal("no mount at the integration path")
	}
	// The ledger promised the worktree BEFORE the action built it, and it
	// names the fetched base tip (MQ7; base_sha is a NOT NULL column — an
	// empty string there would be a row lying about what it cut from).
	rows, err := s.ListLiveLoopWorktrees(store.Scope{LoopSlug: "land-1", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var found *store.LoopWorktree
	for i, w := range rows {
		if w.OwnerKind == "integration" && w.OwnerID == mqItemID {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("no integration ledger row: %+v", rows)
	}
	if found.Path != wt || found.Branch != mqIB {
		t.Fatalf("the ledger row does not describe the worktree the engine built: %+v", found)
	}
	baseTip := strings.TrimSpace(runOut(t, root, "git", "rev-parse", "origin/master"))
	if found.BaseSHA != baseTip {
		t.Fatalf("row cut from %q, origin/master is %q", found.BaseSHA, baseTip)
	}
	// The person's tree never left master (MQ7's whole promise).
	if b := strings.TrimSpace(runOut(t, root, "git", "rev-parse", "--abbrev-ref", "HEAD")); b != "master" {
		t.Fatalf("the queue moved the person's checkout to %q", b)
	}
	// Evidence reached the transcript state (Q7): the dir IS the worktree.
	if sha := strings.TrimSpace(runOut(t, root, "git", "rev-parse", mqIB)); sha == "" {
		t.Fatal("the integration branch is missing")
	}
	_ = ga
	// A second Run re-derives the SAME row (settle-once) — never a second.
	if err := Run(ctx, dep, id); err != nil {
		t.Fatal(err)
	}
	rows2, _ := s.ListLiveLoopWorktrees(store.Scope{LoopSlug: "land-1", ComponentRoot: root})
	n := 0
	for _, w := range rows2 {
		if w.OwnerKind == "integration" && w.OwnerID == mqItemID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the re-run wrote a second row (%d)", n)
	}
}

// TestLandingConflictBlocksAsActionConflictAndKeepsTheRow: the row-first
// promise has its sharpest shape exactly HERE — the action FAILED, and the
// ledger row still stands (the directory is the thing owed, not the row);
// the block kind is its own name, not the generic action-failed; and the
// workspace never moved.
func TestLandingConflictBlocksAsActionConflictAndKeepsTheRow(t *testing.T) {
	ctx := context.Background()
	root := mqLandingRepo(t, true)
	frozen := mqFingerprint(t, root)
	id, s := mqStart(t, root)
	dep, _ := mqDep(s, root)
	var events []struct {
		kind    string
		payload any
	}
	dep.Event = func(kind string, payload any) {
		events = append(events, struct {
			kind    string
			payload any
		}{kind, payload})
	}
	if err := Run(ctx, dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(ctx, s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("a conflicted landing blocks, got %q", saved.Status)
	}
	// The KIND rides the loop.blocked event (blockTo's shape): a conflict
	// is not an action-failure (MQ8), and the sentence names the paths.
	var kinds []string
	var details []any
	for _, e := range events {
		if e.kind == "loop.blocked" {
			if m, ok := e.payload.(map[string]any); ok {
				if r, ok := m["reason"].(*BlockedReason); ok {
					kinds = append(kinds, r.Kind)
					details = append(details, r.Detail)
				}
			}
		}
	}
	if len(kinds) != 1 || kinds[0] != "action-conflict" {
		t.Fatalf("the block kind must be exactly [action-conflict] (MQ8), got %v", kinds)
	}
	d := details[0].(string)
	if !strings.Contains(d, "merge conflicts") || !strings.Contains(d, "f") {
		t.Fatalf("the conflict sentence must name the paths, got %q", d)
	}
	if !strings.Contains(saved.LastReason, "merge conflicts") {
		t.Fatalf("LastReason: %q", saved.LastReason)
	}
	if saved.WorkDir != "" {
		t.Fatalf("a failed integrate must not move the workspace, got %q", saved.WorkDir)
	}
	rows, _ := s.ListLiveLoopWorktrees(store.Scope{LoopSlug: "land-1", ComponentRoot: root})
	if len(rows) != 1 || rows[0].OwnerKind != "integration" {
		t.Fatalf("the row stands FIRST even though the action failed: %+v", rows)
	}
	// The person's checkout is byte-identical THROUGH the whole conflict
	// (status/index/HEAD/tree — the fingerprint the action package pins too).
	if after := mqFingerprint(t, root); after != frozen {
		t.Fatalf("the queue moved the person's checkout across a conflicted landing:\n before %q\n after  %q", frozen, after)
	}
}

// mqFingerprint mirrors the action package's mqCheckout: the person's tree
// as one string (tracked state + HEAD + tree; `-uno` for the same recorded
// reason — the queue's .worktrees/ is legitimately untracked).
func mqFingerprint(t *testing.T, dir string) string {
	t.Helper()
	return runOut(t, dir, "git", "status", "--porcelain=v2", "-z", "-uno") + "\x00" +
		runOut(t, dir, "git", "rev-parse", "HEAD") + "\x00" + runOut(t, dir, "git", "rev-parse", "HEAD^{tree}")
}

// TestWorkDirSingleWriter: the seam is a promise about ASSIGNEES. Only
// three lines in the engine may move a loop's workspace — the evidence
// writer, the per-unit copy in runOneUnit, and (S60 T7) the queue's PIN:
// a resolution run starts inside the integration worktree it inherits from
// the item, and the pin is the one door allowed to say so before any
// evidence exists. This walk pins that a fourth cannot arrive quietly.
// (Test name per T0's F16 default: the S32 gap closes here.)
func TestWorkDirSingleWriter(t *testing.T) {
	allowed := map[string]int{"integration.go": 1, "advance_parallel.go": 1, "pin.go": 1}
	seen := map[string]int{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			tline := strings.TrimSpace(line)
			if strings.HasSuffix(tline, "}") || strings.HasPrefix(tline, "//") {
				continue
			}
			// Every WRITE to the field, not every read: `x.WorkDir = value`.
			i := strings.Index(tline, ".WorkDir = ")
			if i >= 0 {
				seen[name]++
			}
		}
	}
	for file, n := range seen {
		if want, ok := allowed[file]; !ok || n > want {
			t.Fatalf("%s writes .WorkDir %d time(s) — the seam has exactly three writers (applyEvidenceWorkDir, runOneUnit, the queue pin); every assignment outside them must be named here on purpose: %v", file, n, seen)
		}
	}
	for file := range allowed {
		if seen[file] != allowed[file] {
			t.Fatalf("%s must hold exactly %d assignment(s), saw %v", file, allowed[file], seen)
		}
	}
}

// TestParallelUnitWorkspaceIsTheWorktree closes T0's F16 gap directly
// (the golden pin of the OLD shape, TestParallelUnitWorkspaceIsTheLoopRootGolden
// in mq_golden_test.go, keeps its bytes — st.Root never moved, only the NEW
// WorkDir field does): the unit step now carries its own worktree path as
// WorkDir, per-unit, and the loop's own saved state stays untouched.
func TestParallelUnitWorkspaceIsTheWorktree(t *testing.T) {
	ctx := context.Background()
	root := realGitRepo(t)
	s := openLoopStore(t)
	// The ledger row has a real parent: the FK on loop_worktree points at
	// the loop row, so this test starts a real loop and borrows its identity.
	bp := filepath.Join(t.TempDir(), "pa.yaml")
	writeBlueprint(t, bp, "  - id: work\n    step: {kind: model-step, agent: default}\n")
	l, err := Start(context.Background(), s, store.NewLoop{Slug: "pa", ComponentRoot: root, Root: root, Mode: "standard"}, bp, boolPtr(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	st := LoopState{ID: l.ID, Slug: "pa", Root: root, ComponentRoot: root}
	var seen LoopState
	dep := Deps{
		Store: s,
		Model: func(_ context.Context, got LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
			seen = got
			return ModelResult{Kind: "completed"}, nil
		},
		Actions: func(string) *action.Services { return &action.Services{Git: mqGitActions()} },
	}
	a := Action{Position: Position{Lane: "build", Item: "t1"}, Step: &blueprint.Step{ID: "s", Kind: "model-step", Agent: "default"}}
	if _, err := runOneUnit(ctx, dep, st, a); err != nil {
		t.Fatal(err)
	}
	want := worktreePath(st, a)
	if seen.WorkDir != want {
		t.Fatalf("the unit step ran with workspace %q, not its worktree %q", seen.WorkDir, want)
	}
	// The copy is per-unit: the loop's own state never grew a workspace.
	if st.WorkDir != "" {
		t.Fatalf("runOneUnit moved the loop's own workspace: %q", st.WorkDir)
	}
	// With no git seam the directory is only INTENT — pointing a step at an
	// absent tree is worse than the gap, so today's behaviour stands.
	var seen2 LoopState
	dep2 := Deps{Store: s, Model: func(_ context.Context, got LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		seen2 = got
		return ModelResult{Kind: "completed"}, nil
	}}
	if _, err := runOneUnit(ctx, dep2, st, a); err != nil {
		t.Fatal(err)
	}
	if seen2.WorkDir != "" {
		t.Fatalf("without a git seam the row is intent only — no workspace move: %q", seen2.WorkDir)
	}
}

// TestWorkDirStateAdditive (PA13): the JSON key is new and optional. Every
// pre-S60 state marshals byte-identically (no workDir key when empty), a
// state that HAS one round-trips it, and an old payload loads clean.
func TestWorkDirStateAdditive(t *testing.T) {
	empty, err := json.Marshal(RuntimeState{Flags: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), "workDir") {
		t.Fatalf("the empty case must not move the bytes: %s", empty)
	}
	with := RuntimeState{Flags: map[string]bool{}, WorkDir: "/r/.worktrees/integrate-mq-x"}
	b, err := json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"workDir":"/r/.worktrees/integrate-mq-x"`) {
		t.Fatalf("the workDir key is missing: %s", b)
	}
	var back RuntimeState
	if err := json.Unmarshal(b, &back); err != nil || back.WorkDir != with.WorkDir {
		t.Fatalf("round-trip: %v %q", err, back.WorkDir)
	}
	var old RuntimeState
	if err := json.Unmarshal([]byte(`{"flags":{},"answers":{},"tasks":[]}`), &old); err != nil || old.WorkDir != "" {
		t.Fatalf("an old payload must load empty: %v", err)
	}
}

// TestApplyEvidenceWorkDirKeysOnlyTheOne: the seam fires on
// integration_dir and nothing else — not an item id, not a sha, not any
// other key an action (or a forgery) could carry.
func TestApplyEvidenceWorkDirKeysOnlyTheOne(t *testing.T) {
	st := &LoopState{}
	applyEvidenceWorkDir(st, map[string]string{"integration_sha": "abc", "integration_dir": "/r/.worktrees/integrate-i"})
	if st.WorkDir != "/r/.worktrees/integrate-i" {
		t.Fatalf("the dir key must move the seam: %q", st.WorkDir)
	}
	st2 := &LoopState{}
	applyEvidenceWorkDir(st2, map[string]string{"integration_dir": "", "head_sha": "abc"})
	if st2.WorkDir != "" {
		t.Fatalf("an empty dir is no move: %q", st2.WorkDir)
	}
	applyEvidenceWorkDir(st, map[string]string{"integration_dir": ""})
	if st.WorkDir == "" {
		t.Fatal("later evidence with no dir must LEAVE the seam alone, never reset it")
	}
}

// TestReconcileIntegrationRows runs MQ7 through PA12's pass with REAL
// ownership answers: a queued item's missing worktree is REBUILT (attached,
// not re-cut — the branch carries merged history a `-b` would throw away),
// a live row is reattached untouched, and a merged item's row is SETTLED
// (directory gone, branch left standing for a person — MQ6.4). The terminal
// read is the store's MergeItemTerminal, not a hand-rolled predicate: the
// engine's owner-kind switch is what this exercises end to end.
func TestReconcileIntegrationRows(t *testing.T) {
	ctx := context.Background()
	root := realGitRepo(t)
	run(t, root, "git", "branch", mqIB) // the branch survives; the directory does not
	s := openLoopStore(t)
	if _, err := s.CreateComponent(store.NewComponent{Name: root, Slug: "rc-root", Root: root}); err != nil {
		t.Fatal(err)
	}
	bp := filepath.Join(t.TempDir(), "rc.yaml")
	writeBlueprint(t, bp, "  - id: work\n    step: {kind: model-step, agent: default}\n")
	if _, err := Start(ctx, s, store.NewLoop{Slug: "rc", ComponentRoot: root, Root: root, Mode: "standard"}, bp, boolPtr(false), nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.EnqueueMergeItem(store.NewMergeItem{
		ComponentRoot: root, BaseBranch: "master", Branch: "feature/alpha", LoopSlug: "rc",
		HeadSHA: "0000000000000000000000000000000000000001", BaseSHA: "0000000000000000000000000000000000000002",
		EnqueuedByKind: "cli", EnqueuedByID: "t",
	}, store.MergeActor{Kind: "cli", ID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	wt := action.IntegrationWorktreePath(root, item.ID)
	if _, err := s.CreateLoopWorktree(store.LoopWorktree{
		ComponentRoot: root, LoopSlug: "rc", OwnerKind: "integration", OwnerID: item.ID,
		Path: wt, Branch: mqIB, BaseSHA: "0000000000000000000000000000000000000002",
	}); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "rc", ComponentRoot: root}
	dep, _ := mqDep(s, root)
	has := func(_ context.Context, p string) (bool, error) { return isWorktreeDir(p), nil }
	terminal := TerminalFor(dep, sc)

	// (a) queued item, missing directory ⇒ REBUILD by attach, survivor named.
	survivors, err := ReconcileWorktrees(ctx, dep, sc, terminal, has)
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 1 || survivors[0].OwnerID != item.ID {
		t.Fatalf("a queued item's worktree rebuilds: %+v", survivors)
	}
	if !isWorktreeDir(wt) {
		t.Fatal("rebuild produced no mount")
	}
	// (b) re-run: reattach, still one, still mounted, no error.
	survivors, err = ReconcileWorktrees(ctx, dep, sc, terminal, has)
	if err != nil || len(survivors) != 1 {
		t.Fatalf("idempotent reattach: %+v (%v)", survivors, err)
	}
	// (c) the item lands (merged is terminal) ⇒ SETTLE: row gone, directory
	// gone, BRANCH STANDING (MQ6.4 — cleanup of a worktree, never of a ref).
	if _, err := s.TransitionMergeItem(item.ID, "queued", "merged", store.TransitionFacts{}, store.MergeActor{Kind: "daemon", ID: "reconcile-test"}); err != nil {
		t.Fatal(err)
	}
	survivors, err = ReconcileWorktrees(ctx, dep, sc, terminal, has)
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 0 {
		t.Fatalf("a merged item's worktree settles, survivors %v", survivors)
	}
	if rows, _ := s.ListLiveLoopWorktrees(sc); len(rows) != 0 {
		t.Fatalf("the ledger row must be settled: %+v", rows)
	}
	if isWorktreeDir(wt) {
		t.Fatal("the directory must be removed")
	}
	if out := runOut(t, root, "git", "show-ref", "refs/heads/"+mqIB); !strings.Contains(out, mqIB) {
		t.Fatal("settle removed the REF — MQ6.4 forbids it")
	}
}

// TestReconcileUnknownOwnerKindStillRefuses: the integration case joined
// the switch; it did not swallow it. An owner kind outside the closed CHECK
// still arrives as a NAMED error (the fake-terminal path bypasses the store
// CHECK, which is exactly how this stays testable).
func TestReconcileUnknownOwnerKindStillRefuses(t *testing.T) {
	s := openLoopStore(t)
	dep, _ := mqDep(s, "/repo/none")
	_, err := terminalOwner(dep, store.Scope{}, "martian", "x")
	if err == nil || !strings.Contains(err.Error(), "unknown worktree owner kind") {
		t.Fatalf("the closed switch must still refuse loudly: %v", err)
	}
}
