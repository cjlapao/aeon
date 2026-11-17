package loop

// S60 T1 · MQ characterization goldens for the loop engine.
//
// These pin today's behaviour on the untouched code. Header of who changes
// what, and only what:
//
//   - action_args — T5 (S60) adds ONE thing: `Dir` reads `st.WorkDir` when
//     set and `st.Root` otherwise. Every other derived field here (Loop,
//     Task, Which, Base, the evidence-fed numbers) is byte-identical before
//     and after; T4 may add a base pin the Base derivation consults, and a
//     loop without a pin keeps every byte here.
//   - unit_strings — unchanged by S60 (T5 pins that the unit BRANCH and
//     worktree PATH formulas do not move; the integration ref is NEW naming
//     beside it, never a modification).
//   - unit_workspace — **T5 is the named changer of this one**: when
//     `runOneUnit` sets `unitSt.WorkDir = wt.Path` (S32's open gap, T0
//     record F16), the model step's git directory becomes the unit's
//     worktree. The LoopState.Root a unit sees does NOT change — this
//     golden pins Root == loop root, and after T5 it still must: the change
//     lands in WorkDir, a field this golden shows in its EMPTY state.
//
// Determinism: no clock, no ids from the store beyond the slug the fixture
// itself chose; no network; no real git.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"sync"
)

var mqLoopGoldenUpdate = flag.Bool("mq-golden-update", false, "rewrite internal/loop/testdata/mq_golden from today's behaviour (Phase A ritual only)")

func mqLoopGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "mq_golden", name+".golden")
	if *mqLoopGoldenUpdate {
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
		t.Fatalf("golden %s missing (run `-run Golden -mq-golden-update` on untouched code to mint it): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s drifted from the golden.\nIf the drift is a task this file's header says is expected, update the golden AND say what changed.\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}

func mqLoopState() LoopState {
	return LoopState{
		ID: "L1", Slug: "4a17-version", Root: "/repo", ComponentRoot: "/repo",
		RuntimeState: RuntimeState{
			With: map[string]string{
				"task": "fix-parse-order", "task_title": "Fix the parse order",
				"commit_message": "fix: parse order", "pr_summary": "parse order summary",
				"review_summary": "reviewed", "branch_which": "task",
			},
			Evidence: map[string]map[string]string{
				"work/task/open-pr": {"issue_number": "7", "pr_number": "12", "aeon_review": "ok@deadbeef"},
			},
		},
	}
}

func mqStep(params map[string]any) *blueprint.Step {
	return &blueprint.Step{Kind: "code-action", Action: "git.createBranch", Params: params}
}

// TestActionArgsGolden pins F5: every derived field of actionArgs — the
// Dir from st.Root, the Which from step params (an authored constant), the
// Base from the loop's own feature branch, and the evidence-fed numbers.
func TestActionArgsGolden(t *testing.T) {
	st := mqLoopState()

	feat := Action{Kind: ActRunCodeAction, Position: Position{Lane: "work", Item: "fix-parse-order", Step: "create-feature"},
		Step: mqStep(map[string]any{"which": "feature"})}
	fa := actionArgs(&st, feat)

	task := Action{Kind: ActRunCodeAction, Position: Position{Lane: "work", Item: "fix-parse-order", Step: "open-pr"},
		Step: mqStep(map[string]any{})}
	ta := actionArgs(&st, task)

	var b strings.Builder
	fmt.Fprintf(&b, "feature-step: Dir=%s Loop=%s Task=%s Which=%s Base=%s TaskTitle=%q Message=%q PR=%d Issue=%d AeonReview=%s\n",
		fa.Dir, fa.Loop, fa.Task, fa.Which, fa.Base, fa.TaskTitle, fa.Message, fa.PRNumber, fa.IssueNumber, fa.AeonReview)
	fmt.Fprintf(&b, "task-step:    Dir=%s Loop=%s Task=%s Which=%s Base=%s PR=%d Issue=%d AeonReview=%s\n",
		ta.Dir, ta.Loop, ta.Task, ta.Which, ta.Base, ta.PRNumber, ta.IssueNumber, ta.AeonReview)

	// A step naming junk which must refuse naming the value AT THE ACTION;
	// actionArgs only carries it (Validator is where the refusal lands).
	junk := Action{Kind: ActRunCodeAction, Position: Position{Lane: "work", Item: "fix-parse-order", Step: "which-junk"},
		Step: mqStep(map[string]any{"which": "banana"})}
	ja := actionArgs(&st, junk)
	fmt.Fprintf(&b, "junk-which:   Which=%s (carried verbatim — validation refuses it, actionArgs does not)\n", ja.Which)

	mqLoopGolden(t, "action_args", b.String())
}

// TestUnitStringsGolden pins the unit branch/worktree formulas (S32) that
// T5 must NOT move. Shape A names the branch by LANE, Shape B by ITEM; the
// worktree path is per-unit under the component root.
func TestUnitStringsGolden(t *testing.T) {
	st := LoopState{Slug: "p", ComponentRoot: "/repo"}
	lane := Action{Position: Position{Lane: "b"}}
	item := Action{Position: Position{Lane: "map", Item: "fix-x"}}

	var b strings.Builder
	fmt.Fprintf(&b, "lane: branch=%s namesLoop=%s namesTask=%s path=%s\n",
		branchForUnit(st, lane), branchNamesForUnit(st, lane).Loop, branchNamesForUnit(st, lane).Task, worktreePath(st, lane))
	fmt.Fprintf(&b, "item: branch=%s namesLoop=%s namesTask=%s path=%s\n",
		branchForUnit(st, item), branchNamesForUnit(st, item).Loop, branchNamesForUnit(st, item).Task, worktreePath(st, item))
	mqLoopGolden(t, "unit_strings", b.String())
}

// TestParallelUnitWorkspaceIsTheLoopRootGolden pins F16 — the gap T0
// measured and T5 closes: a parallel unit's MODEL step receives the LOOP
// ROOT (Root and ComponentRoot), and the only place the unit's own dir
// appears is the `worktree add` path the fan-out runs before the step. T5
// adds LoopState.WorkDir = that worktree path; after T5 this golden's bytes
// MUST stay identical — Root is not WorkDir, and a model that learned the
// loop root from st.Root keeps working byte for byte.
func TestParallelUnitWorkspaceIsTheLoopRootGolden(t *testing.T) {
	s := openLoopStore(t)
	path := parallelGroupBlueprint(t)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}

	mu := &sync.Mutex{}
	var roots []string
	model := func(_ context.Context, st LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		mu.Lock()
		roots = append(roots, "slug="+st.Slug+" root="+st.Root+" compRoot="+st.ComponentRoot)
		mu.Unlock()
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 3 }); err != nil {
		t.Fatal(err)
	}
	sort.Strings(roots)

	adds, _ := rec.counts()
	var addDirs []string
	rec.mu.Lock()
	for d := range rec.addDirs {
		addDirs = append(addDirs, d)
	}
	rec.mu.Unlock()
	sort.Strings(addDirs)

	var b strings.Builder
	for _, r := range roots {
		b.WriteString("model-saw: " + r + "\n")
	}
	fmt.Fprintf(&b, "worktree-adds: %d\n", adds)
	for _, d := range addDirs {
		b.WriteString("worktree-at: " + d + "\n")
	}
	mqLoopGolden(t, "unit_workspace", b.String())
}
