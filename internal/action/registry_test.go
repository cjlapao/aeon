package action

// T10's registry tests. The registry is the serialization point, and
// closedness is ASSERTED, not assumed (D14/O2/E2) — T2's effect classes
// are sufficient only because the set they govern is known; an open
// registry would make them decoration.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestEightActionsNoRender (D13, L25) — nine plus audit, `render`
// absent, every entry with a class, a strategy and an args schema.
// S57 T6 added the tenth, verify.run (the local verification rung —
// its registry entry is pinned in TestActionRegistryClosedSetUpdated).
// S60 T5 added the queue's three (MQ8): git.integrate and
// git.concludeIntegration — both LOCAL, a worktree is nobody else's
// problem — and git.publishIntegration, the one outward name the queue
// has, with the branch-name gate as its second lock. Dated 2026-11-04:
// thirteen names, `render` still absent.
// S60 T6 (dated 2026-11-04) added the landing half's three (MQ8/MQ12):
// queue.enqueue (the loop's LOCAL door into the queue), forge.openLandingPR
// (the queue's second outward name) and forge.land (its second
// irreversible one). Sixteen names; `render` still absent.
func TestEightActionsNoRender(t *testing.T) {
	want := []string{Audit, ForgeCloseIssue, ForgeLand, ForgeMerge, ForgeOpenIssue, ForgeOpenLandingPR, ForgeOpenPR,
		ForgePostReview, GitCommitAndPush, GitConcludeIntegration, GitCreateBranch,
		GitIntegrate, GitMergeBranch, GitPublishIntegration, QueueEnqueue, VerifyRun}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("the set changed: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("set mismatch at %d: %v vs %v", i, got, want)
		}
	}
	if _, ok := Lookup("render"); ok {
		t.Fatal("render is NOT in S16 (L25/D13): the mirror is disposable, the store is the source of truth — v1's loop.md writer stays dead")
	}
	for _, name := range got {
		e, ok := Lookup(name)
		if !ok || e.Name != name {
			t.Fatalf("%s missing from its own entry", name)
		}
		if _, err := Check(name); err != nil {
			t.Fatalf("%s has no class: %v", name, err)
		}
		if Strategy(name) == "" {
			t.Fatalf("%s has no strategy", name)
		}
		if e.Args == nil {
			t.Fatalf("%s has no args schema", name)
		}
		if len(e.Evidence) == 0 {
			t.Fatalf("%s returns no evidence keys — an action whose facts nobody records is an action nobody can audit (Q7)", name)
		}
		for field, spec := range e.Args {
			if spec.Type == "" || spec.Source == "" {
				t.Fatalf("%s.%s is undeclared: %+v", name, field, spec)
			}
			// L3's spine in DATA: no identifier-bearing slot may be
			// handoff-sourced — numbers and refs are code or evidence.
			if (strings.Contains(field, "number") || strings.Contains(field, "sha") || field == "pr" || field == "base") && spec.Source == "handoff" {
				t.Fatalf("%s.%s: an identifier sourced from a model — I3 says no", name, field)
			}
		}
	}
}

// TestRegistryIsClosedAsserted (D14, O2, E2) — like S8's tool check:
// no exported mutation door exists, the lists are copies, no environment
// or config value feeds the map, and no blueprint field can introduce an
// action — validation is the only door an `action:` name passes, and it
// CHECKS against the set rather than extending it.
func TestRegistryIsClosedAsserted(t *testing.T) {
	// (a) No exported Add/Register/Insert door anywhere in the package.
	fset := token.NewFileSet()
	files, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range files {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() {
					continue
				}
				n := fn.Name.Name
				if strings.HasPrefix(n, "Add") || strings.HasPrefix(n, "Register") || strings.HasPrefix(n, "Insert") || strings.HasPrefix(n, "Append") {
					if strings.Contains(n, "Action") || strings.Contains(n, "Registry") {
						t.Fatalf("exported mutation door %s — the registry must be closed (D14)", n)
					}
				}
			}
		}
	}
	// (b) Names() hands out copies.
	first := Names()
	first[0] = "poisoned"
	if Names()[0] != first[0] && Names()[0] == "poisoned" {
		t.Fatal("Names() leaked the live slice")
	}
	if Names()[0] == "poisoned" {
		t.Fatal("the closed set is writable by its readers")
	}
	// (c) No environment feed: the non-test sources read no environment.
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "os.Getenv") || strings.Contains(string(raw), "LookupEnv") {
			t.Fatalf("%s reads the environment — no config value may feed the registry (D14/E2)", e.Name())
		}
	}
	// (d) A blueprint cannot introduce an action: validation refuses it,
	// and the refused name provably never enters the set.
	raw := []byte("id: smuggle\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: x\n    step: {kind: code-action, action: git.smuggleInNewAction}\n")
	report := blueprint.Analyze(raw, blueprint.ValidationOptions{Mode: "standard", Actions: Names()})
	found := false
	for _, d := range report.Diagnostics {
		if d.State == blueprint.Invalid && d.Kind == "unknown-action" {
			found = true
		}
	}
	if !found {
		t.Fatal("a blueprint smuggled an action past validation — the classes would then govern a set nobody knows")
	}
	if _, ok := Lookup("git.smuggleInNewAction"); ok {
		t.Fatal("and it even arrived")
	}
}

// TestUnknownActionIsNowInvalid (D15) — the registry EXISTS, so the
// deferral died: unknown is invalid; the registry's ABSENT (nil) state
// keeps the honest three-state answer.
func TestUnknownActionIsNowInvalid(t *testing.T) {
	raw := []byte("id: u\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: x\n    step: {kind: code-action, action: forge.nonexistent}\n")
	wired := blueprint.Analyze(raw, blueprint.ValidationOptions{Mode: "standard", Actions: Names()})
	var invalid, unverifiable int
	for _, d := range wired.Diagnostics {
		if d.State == blueprint.Invalid && d.Kind == "unknown-action" {
			invalid++
		}
		if d.State == blueprint.Unverifiable {
			unverifiable++
		}
	}
	if invalid != 1 || unverifiable != 0 {
		t.Fatalf("unknown must be invalid once the registry exists (D15): %+v", wired.Diagnostics)
	}
	atRest := blueprint.Analyze(raw, blueprint.ValidationOptions{Mode: "standard"})
	var deferred int
	for _, d := range atRest.Diagnostics {
		if d.State == blueprint.Unverifiable && d.Slice == "S16" {
			deferred++
		}
	}
	if deferred != 1 {
		t.Fatalf("without the registry the answer is honestly unverifiable, never a guessed verdict: %+v", atRest.Diagnostics)
	}
}

// TestEveryActionReturnsEvidence (Q7) — dispatch each registered action
// against the slice's own instruments and prove the numbers come back
// non-empty under the declared keys.
func TestEveryActionReturnsEvidence(t *testing.T) {
	ctx := context.Background()
	dir, origin := realRepoWithOrigin(t)
	_ = origin
	base := Args{Dir: dir, Loop: "4a17-version", Task: "4a17"}

	featureOnly := base
	featureOnly.Task = "" // loop-level: the which-default lands on feature
	ev, err := Dispatch(ctx, &Services{Git: &GitActions{Git: gitReal()}}, GitCreateBranch, featureOnly)
	if err != nil || ev["branch"] != "feature/4a17-version" {
		t.Fatalf("createBranch evidence (feature, by the which-default): %v (%v)", ev, err)
	}
	if _, err := Dispatch(ctx, &Services{Git: &GitActions{Git: gitReal()}}, GitCreateBranch, base); err != nil {
		t.Fatalf("task branch: %v", err)
	}
	writeFile(t, dir, "new.go", "package main\n")
	ev, err = Dispatch(ctx, &Services{Git: &GitActions{Git: gitReal()}}, GitCommitAndPush, Args{Dir: dir, Loop: "4a17-version", Task: "4a17", Message: "feat: work"})
	if err != nil || ev["commit_sha"] == "" || ev["commit_sha"] == "(unset)" {
		t.Fatalf("commitAndPush evidence: %v (%v)", ev, err)
	}
	ev, err = Dispatch(ctx, &Services{Git: &GitActions{Git: gitReal()}}, GitMergeBranch, base)
	if err != nil || ev["branch"] == "" {
		t.Fatalf("mergeBranch evidence: %v (%v)", ev, err)
	}

	f := forge.NewFake()
	forgeSvc := func() *Services {
		return &Services{Forge: &ForgeActions{Forge: f, Git: &branchProbe{present: true}, Merge: &mergeProbe{}}}
	}
	ev, err = Dispatch(ctx, forgeSvc(), ForgeOpenIssue, Args{Dir: dir, Loop: "4a17-version", Task: "4a17", TaskTitle: "Add version endpoint"})
	if err != nil || ev["issue_number"] == "" {
		t.Fatalf("openIssue evidence: %v (%v)", ev, err)
	}
	ev, err = Dispatch(ctx, forgeSvc(), ForgeOpenPR, Args{Dir: dir, Loop: "4a17-version", Task: "4a17", TaskTitle: "Add version endpoint", Base: "feature/4a17-version", PRSummary: goodSummary})
	if err != nil || ev["pr_number"] == "" {
		t.Fatalf("openPR evidence: %v (%v)", ev, err)
	}
	prNo := atoiOf(ev["pr_number"])
	ev, err = Dispatch(ctx, &Services{Forge: &ForgeActions{Forge: f, Intents: &memIntents{}}}, ForgePostReview,
		Args{Loop: "4a17-version", Task: "4a17", PRNumber: prNo, StepID: 88, Attempt: 1, ReviewSumm: "Verified against the daemon.", Verdict: forge.VerdictApprove})
	if err != nil || ev["review_id"] == "" {
		t.Fatalf("postReview evidence: %v (%v)", ev, err)
	}
	ev, err = Dispatch(ctx, forgeSvc(), ForgeMerge, Args{Dir: dir, Loop: "4a17-version", Task: "4a17", PRNumber: prNo})
	if err != nil || ev["merge_sha"] == "" || ev["merge_sha"] == "(unset)" {
		t.Fatalf("merge evidence: %v (%v)", ev, err)
	}
	f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] x", Labels: []string{"dev-loop"}})
	ev, err = Dispatch(ctx, forgeSvc(), ForgeCloseIssue, Args{IssueNumber: 4})
	if err != nil || ev["issue_number"] != "4" {
		t.Fatalf("closeIssue evidence: %v (%v)", ev, err)
	}
	ev, err = Dispatch(ctx, &Services{Audit: func(context.Context) (int, error) { return 3, nil }}, Audit, Args{})
	if err != nil || ev["entries"] != "3" {
		t.Fatalf("audit evidence: %v (%v)", ev, err)
	}
	// Required-arg refusals name the argument (D12 applied to the schema).
	if _, err := Dispatch(ctx, forgeSvc(), ForgeMerge, Args{Dir: dir, Loop: "l", Task: "t"}); err == nil || !strings.Contains(err.Error(), "pr") {
		t.Fatalf("missing required evidence-arg must name it: %v", err)
	}
}

// TestStrategiesAreEnumerated — eight check-then-act… no: SEVEN, one
// receipt-first, TWO naturally idempotent — asserted BY COUNT, so a
// second receipt-first action (somebody generalising the exception)
// fails here, not in production. (The task's prose said eight
// check-then-act; its own table puts audit under naturally-idempotent
// and postReview under receipt-first — seven is the arithmetic. Recorded
// as a spec contradiction resolved by the table.)
//
// S57 T6 added the second naturally-idempotent: verify.run. A run is
// evidence, not a mutation — re-running it re-verifies the same head
// rather than redoes an effect, so the strategy is declared, not
// inferred, and the census is the guard that keeps it declared.
//
// S60 T5 (2026-11-04) added the merge queue's three names — git.integrate,
// git.concludeIntegration, git.publishIntegration — all three declared
// check-then-act: each checks a name or a tree before it moves anything,
// and publish checks BEFORE argv is ever assembled. check-then-act is now
// TEN. The queue adds no receipt-first and no naturally-idempotent name.
//
// S60 T6 (2026-11-04) adds the landing half: forge.openLandingPR and
// forge.land are check-then-act (the open PR keyed on the head ref and the
// already-merged answer ARE the re-run guards) → check-then-act TWELVE;
// queue.enqueue is naturally-idempotent per MQ8's own table (one OPEN row
// per branch is the queue's business key, answered by REUSE, not by a
// receipt) → naturally-idempotent THREE. Still exactly one receipt-first.
func TestStrategiesAreEnumerated(t *testing.T) {
	counts := map[string]int{}
	for _, n := range Names() {
		counts[Strategy(n)]++
	}
	// S61 T4 (dated 2026-11-05): the loop-executable census does NOT move —
	// the two bootstrap actions stand behind the person door, which is
	// exactly why they are absent from Names() and from THIS count. Their
	// own strategies are censused below, where a person can see them.
	if counts[StrategyReceiptFirst] != 1 || counts[StrategyCheckThenAct] != 12 || counts[StrategyNaturallyIdempotent] != 3 {
		t.Fatalf("strategy census changed: %v — the exception is the point, so the census is the guard", counts)
	}
	// S61 T4: the person doors declare their strategies too — a door
	// without a strategy is a door into an undefined retry story. Both are
	// check-then-act (scaffold checks the ground; createRepo GETs before
	// it POSTs), and PersonNames is the whole person set.
	persons := PersonNames()
	if len(persons) != 2 {
		t.Fatalf("the person doors changed size: %v", persons)
	}
	for _, n := range persons {
		if got := Strategy(n); got != StrategyCheckThenAct {
			t.Errorf("person-door %s declares %q, not check-then-act", n, got)
		}
	}
}

// TestDevLoopValidatesStrict (D15) — the nine unverifiables are gone and
// `--strict` passes for the first time. This is the moment S8's fixture
// was committed for.
func TestDevLoopValidatesStrict(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", "dev-loop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	report := blueprint.Analyze(raw, blueprint.ValidationOptions{Mode: "standard", Actions: Names(), Agents: handler.Names(), Resolvers: handler.Resolvers()})
	unverifiable, invalid := 0, 0
	for _, d := range report.Diagnostics {
		switch d.State {
		case blueprint.Unverifiable:
			unverifiable++
		case blueprint.Invalid:
			invalid++
			t.Logf("invalid: %s %s", d.Kind, d.Path)
		}
	}
	if invalid != 0 {
		t.Fatalf("dev-loop must validate clean: %d invalid", invalid)
	}
	if unverifiable != 0 {
		for _, d := range report.Diagnostics {
			if d.State == blueprint.Unverifiable {
				t.Logf("leftover unverifiable: %s %s %s", d.Kind, d.Path, d.Slice)
			}
		}
		t.Fatalf("zero unverifiable with the registry wired — %d remain (agents/resolvers are supplied below in the shipped fixture test; here Actions alone must clear the action class)", unverifiable)
	}
	if !report.OK(true) {
		t.Fatal("--strict passes for the FIRST time (D15)")
	}
}

// TestExpectedDiagnosticFixtureUpdated — the committed set changed and
// the file IS the diff: nine action-registry entries became the empty
// set the moment the registry arrived.
func TestExpectedDiagnosticFixtureUpdated(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "blueprint", "testdata", "dev-loop-unverifiable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("the committed expected set must be the EMPTY set — that diff is the slice's evidence (C10):\n%s", raw)
	}
}

func atoiOf(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
