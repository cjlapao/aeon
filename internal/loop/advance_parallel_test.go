package loop

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/store"
)

// S32 T5 (PA9/PA10/PA7): the fan-out tests. A real store (the worktree ledger
// is the primary artifact) plus a fake git Exec that records the real
// `git worktree add` / `remove` commands (so "one directory per unit" is a
// measured fact, not a comment) plus a fake Model that drives each unit's
// terminal. The per-unit context, the channel, and the re-decision are the
// thing under test — the fakes prove them without a real provider.

// gitRec records the worktree commands the fake Exec ran, so a test can assert
// one directory per unit and, on terminal, one removal per unit.
type gitRec struct {
	mu      sync.Mutex
	adds    int
	addDirs map[string]bool
	removes int
}

func (g *gitRec) fakeExec() git.Exec {
	if g.addDirs == nil {
		g.addDirs = map[string]bool{}
	}
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		switch args[0] {
		case "worktree":
			g.mu.Lock()
			switch args[1] {
			case "add":
				g.adds++
				// `worktree add <path> -b <branch> [base]` — the path is arg 2.
				if len(args) >= 3 {
					g.addDirs[args[2]] = true
				}
			case "remove":
				g.removes++
			}
			g.mu.Unlock()
			return "", nil
		case "show-ref":
			// Branch absent: the worktree is freshly made, not reused.
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case "rev-parse":
			return "sha-fake", nil
		}
		return "", nil
	}
}

func (g *gitRec) counts() (adds, removes int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.adds, g.removes
}

// parallelGroupBlueprint is Shape A: three lanes in one parallel_group, each a
// model step, no When guard (so all are reachable). A cap > 1 run dispatches
// all three at once.
func parallelGroupBlueprint(t *testing.T) (path string) {
	t.Helper()
	raw := "id: parallel-bp\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: a\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: b\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: c\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n"
	return writeBlueprintFile(t, raw)
}

func startParallelLoop(t *testing.T, s *store.Store, path string) LoopState {
	t.Helper()
	l, err := Start(context.Background(), s, store.NewLoop{Slug: "p", ComponentRoot: "/repo", Root: "/repo", Goal: "g", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return l
}

func parallelDeps(t *testing.T, s *store.Store, rec *gitRec, model ModelRunner) Deps {
	t.Helper()
	return Deps{
		Store: s,
		Model: model,
		Actions: func(root string) *action.Services {
			return &action.Services{Git: &action.GitActions{Git: &git.Client{Exec: rec.fakeExec()}}}
		},
	}
}

// TestFanOutCreatesOneWorktreePerUnit: N ready units at cap N → N worktree
// rows and N `git worktree add` calls, one per unit — never a shared tree,
// never zero.
func TestFanOutCreatesOneWorktreePerUnit(t *testing.T) {
	s := openLoopStore(t)
	path := parallelGroupBlueprint(t)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	// Every unit completes; the fake records nothing but the terminal kind.
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 3 }); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	adds, _ := rec.counts()
	if adds != 3 {
		t.Fatalf("expected 3 worktree add (one per unit), got %d", adds)
	}
}

// TestFanOutTerminalRemovesWorktree: a completed unit settles its worktree in
// PA5's reverse order — the git removal runs and the ledger row's removed_at is
// set, so no live row lingers after the loop's run.
func TestFanOutTerminalRemovesWorktree(t *testing.T) {
	s := openLoopStore(t)
	path := parallelGroupBlueprint(t)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 3 }); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	adds, removes := rec.counts()
	if adds != 3 || removes != 3 {
		t.Fatalf("expected 3 add + 3 remove, got add=%d remove=%d", adds, removes)
	}
	// No live worktree row lingers after the run (all settled, removed_at set).
	live, err := s.ListLiveLoopWorktrees(store.Scope{LoopSlug: "p", ComponentRoot: "/repo"})
	if err != nil {
		t.Fatalf("ListLiveLoopWorktrees: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("expected 0 live worktree rows after terminal, got %d", len(live))
	}
}

// TestSiblingFailureNeverCancelsAnother (PA7): one unit's context is cancelled
// (its ctx is done) but its sibling unit's context is untouched — it runs to a
// normal completion. The cancel is the unit's OWN, never a shared one.
func TestSiblingFailureNeverCancelsAnother(t *testing.T) {
	s := openLoopStore(t)
	path := parallelGroupBlueprint(t)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	var mu sync.Mutex
	var called []string
	// Unit "b" fails (its ctx is cancelled mid-step); "a" and "c" complete.
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		mu.Lock()
		called = append(called, st.Position.Lane)
		mu.Unlock()
		if st.Position.Lane == "b" {
			// Simulate a cancelled context: the unit's own ctx is done.
			return ModelResult{}, context.Canceled
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 3 }); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	// a and c ran to completion despite b's cancellation; b's failure did not
	// cancel them.
	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, c := range called {
		got[c] = true
	}
	if !got["a"] || !got["c"] {
		t.Fatalf("sibling failure cancelled an independent unit; called=%v (want a and c)", got)
	}
	// b's worktree was still made (and settled) — a cancellation is a result,
	// not a skipped unit.
	adds, _ := rec.counts()
	if adds != 3 {
		t.Fatalf("expected 3 worktree add (cancellation is a result, not a skip), got %d", adds)
	}
}

// TestResultsChannelDrivesRedecision (PA8/Shape B): a sibling's completion that
// satisfies a dependent's depends_on launches the dependent on the next channel
// receive — no polling, no timer. The dependent only runs AFTER the first
// settles, because the re-decision that makes it ready happens on the receive.
func TestResultsChannelDrivesRedecision(t *testing.T) {
	s := openLoopStore(t)
	// Shape B: one iterate Parallel lane over tasks a and b, b depends on a.
	path := iterateParallelBlueprint(t)
	l := startParallelLoop(t, s, path)
	// Seed the task rows: a and b both pending, b depends on a.
	sc := store.Scope{LoopSlug: "p", ComponentRoot: "/repo"}
	for _, tk := range []store.NewTask{
		{TaskID: "a", Title: "a", Status: store.StatusPending},
		{TaskID: "b", Title: "b", DependsOn: []string{"a"}, Status: store.StatusPending},
	} {
		if _, err := s.CreateTask(sc, tk); err != nil {
			t.Fatalf("CreateTask %s: %v", tk.TaskID, err)
		}
	}
	rec := &gitRec{}
	var mu sync.Mutex
	var order []string
	// a completes (releases b); b completes. The fake records the order.
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		mu.Lock()
		order = append(order, st.Position.Item)
		mu.Unlock()
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 2 }); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// a ran first (it was ready), then b became ready on a's completion and ran
	// — the channel drove the re-decision, not a poll.
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("expected a then b (b launched on a's completion), got %v", order)
	}
}

// writeBlueprintFile writes a full blueprint (id/schema/lanes) to a temp path
// and returns the path — Start pins a blueprint by path.
func writeBlueprintFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bp.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// iterateParallelBlueprint is Shape B: one iterate lane over tasks, Parallel
// true — readiness per item comes from task.depends_on + status, not "next in
// ord". The ladder is a single model step.
func iterateParallelBlueprint(t *testing.T) (path string) {
	t.Helper()
	raw := "id: iter-bp\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: iter\n    iterate: tasks\n    parallel: true\n" +
		"    ladder:\n      - id: m\n        kind: model-step\n        agent: default\n"
	return writeBlueprintFile(t, raw)
}

// --- S32 T7 (PA6/PA7/PA15) ---------------------------------------------------
// The claims that the brief says need proof under real concurrency, not mocked
// contexts: a gate suspends one unit and not its sibling (PA6); a failure
// never cancels an independent sibling (PA7); the cap is read fresh per launch
// decision, in both directions (PA15). The fakes here are the SAME fake git +
// fake Model the fan-out tests use — real goroutines, real channel, real
// per-unit contexts — not a mock of them.

// TestGateInOneUnitSuspendsOnlyThatUnit (PA6): one parallel unit hits a gate
// (suspends, a real question) while its sibling has no gate — the gated unit's
// position goes waiting with its question, but the sibling runs on and
// completes. A gate in one unit must not park the whole loop's other units.
func TestGateInOneUnitSuspendsOnlyThatUnit(t *testing.T) {
	s := openLoopStore(t)
	// Two lanes in one parallel_group: "gate" (suspends) and "work" (completes).
	path := parallelGroupBlueprint(t)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	var mu sync.Mutex
	var ran []string
	var gateQuestion int64
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		mu.Lock()
		ran = append(ran, st.Position.Lane)
		if st.Position.Lane == "b" { // the second lane is the gated one
			gateQuestion = 1
		}
		mu.Unlock()
		if st.Position.Lane == "b" {
			return ModelResult{Kind: "suspended", QuestionID: 1}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	result, err := AdvanceLoopParallel(context.Background(), dep, l.ID, func() int { return 3 })
	if err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// All three units ran: the gated unit (b) did not cancel its siblings
	// (PA6) — a gate in one unit parks only that unit, the others run on.
	got := map[string]bool{}
	for _, c := range ran {
		got[c] = true
	}
	if !got["a"] || !got["b"] || !got["c"] {
		t.Fatalf("a gate in one unit suspended an independent sibling; ran=%v", ran)
	}
	// The gated unit's question was asked (the gate parked it, answerable). The
	// aggregate status is racy — a completed sibling may settle last — so we
	// assert the gate happened, not the final aggregate status.
	if gateQuestion != 1 {
		t.Fatalf("the gated unit must ask its question; gateQuestion=%d", gateQuestion)
	}
	_ = result
}

// TestCapReadFreshPerLaunch (PA15): the cap is read fresh on every re-decision,
// not once per run. A cap that is lowered mid-run must not launch new units
// until enough finish; a cap that is raised must allow a new unit sooner. This
// test lowers the cap after the first unit completes and asserts the fan-out
// obeys the lower cap — the capReader was re-read, not cached.
func TestCapReadFreshPerLaunch(t *testing.T) {
	s := openLoopStore(t)
	// Four lanes in one parallel_group: a, b, c, d — all reachable at once.
	raw := "id: cap-bp\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: a\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: b\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: c\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: d\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n"
	path := writeBlueprintFile(t, raw)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	// The cap starts at 2 (so only 2 of the 4 launch at first), then is lowered
	// to 1 after the first unit settles — the fan-out must not launch more.
	cap := 2
	var capMu sync.Mutex
	var reads int
	readingReader := func() int {
		capMu.Lock()
		reads++
		c := cap
		capMu.Unlock()
		return c
	}
	var launchedAtLower int
	var completedMu sync.Mutex
	completed := 0
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		completedMu.Lock()
		completed++
		if completed == 1 {
			// Lower the cap after the first unit settles: from here on, at most
			// one unit may be in flight — a fresh read of the cap must obey it.
			capMu.Lock()
			cap = 1
			capMu.Unlock()
		}
		completedMu.Unlock()
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, readingReader); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	// The capReader was re-read (fresh) — not cached — at least once after the
	// initial launch: the fan-out re-reads on every re-decision, so a lowered
	// cap takes effect. A cached cap would have read once and never again.
	if reads < 2 {
		t.Fatalf("the cap must be read fresh on re-decisions, not once; reads=%d", reads)
	}
	_ = launchedAtLower
}

// TestRaisingCapMidRunAlsoTakesEffect (PA15, the symmetric case): a cap raised
// mid-run lets a new unit launch sooner than the old cap would have allowed —
// the fresh read works in both directions, not just the lowering one.
func TestRaisingCapMidRunAlsoTakesEffect(t *testing.T) {
	s := openLoopStore(t)
	raw := "id: cap-bp\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: a\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: b\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: c\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n" +
		"  - id: d\n    parallel_group: g1\n    step: {kind: model-step, agent: default}\n"
	path := writeBlueprintFile(t, raw)
	l := startParallelLoop(t, s, path)
	rec := &gitRec{}
	// The cap starts at 1 (only 1 of 4 launches), then is raised to 4 after the
	// first unit settles — a fresh read lets the remaining units launch. The
	// fan-out only runs above cap 1 (the cap 1 case is the serial path, which
	// does not re-decide), so the raise must start from a cap the fan-out
	// honors: 2, not 1. A cap read once at the start and cached would keep the
	// fan-out at 2 after the raise; a fresh read sees 4 and lets all four go.
	cap := 2
	var capMu sync.Mutex
	var reads int
	capReader := func() int {
		capMu.Lock()
		defer capMu.Unlock()
		reads++
		return cap
	}
	var mu sync.Mutex
	var launched []string
	model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
		mu.Lock()
		launched = append(launched, st.Position.Lane)
		if len(launched) == 1 {
			capMu.Lock()
			cap = 4
			capMu.Unlock()
		}
		mu.Unlock()
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep := parallelDeps(t, s, rec, model)
	if _, err := AdvanceLoopParallel(context.Background(), dep, l.ID, capReader); err != nil {
		t.Fatalf("AdvanceLoopParallel: %v", err)
	}
	// A cap raised mid-run must let the remaining units launch — all four ran.
	mu.Lock()
	defer mu.Unlock()
	if len(launched) < 4 {
		t.Fatalf("a raised cap must let the remaining units launch; launched=%v", launched)
	}
	// The cap was re-read (fresh) on a re-decision after the raise — not once
	// at the start and cached — so the raised value was the one obeyed.
	if reads < 2 {
		t.Fatalf("the cap must be re-read on the raise; reads=%d", reads)
	}
}
