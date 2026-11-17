package agentsvc

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// almost is how money in floats gets compared: sums like 2.0+0.4 land one
// ulp away from the literal 2.4, and a test that fails on the last binary
// digit trains nobody about the thing it claims to check.
func almost(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

// T6 — cost.go. A1's amendment made true: the graph exists whether or not
// anybody records it, and unrecorded cost does not read as zero — it reads
// as UNATTRIBUTED, which is worse.

// askDone builds caller → callee with the child task completed carrying a
// reported cost, and gives the caller step its own cost.
func (e *chainEnv) askDone(t *testing.T, caller, callee string, own, child model.Cost) int64 {
	t.Helper()
	stepID := runningStepAs(t, e.s, e.loopSt, caller)
	if err := e.s.SetStepCost(stepID, own); err != nil {
		t.Fatal(err)
	}
	task, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: caller, CallerStepID: stepID,
		CalleeAgent: callee, SkillID: "do-thing", Text: "asked", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef(callee, "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), task.ID, "done", "ok", &child); rj != nil {
		t.Fatal(rj.Detail)
	}
	return stepID
}

func TestParentIncludesChildren(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := e.askDone(t, "planner", "reddit-agent",
		model.Cost{State: model.CostKnown, USD: 1.5},
		model.Cost{State: model.CostKnown, USD: 0.25})

	got, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if got.Cost.State != model.CostKnown {
		t.Fatalf("own + one known child must be known: %+v", got.Cost)
	}
	if !almost(got.Cost.USD, 1.75) {
		t.Fatalf("the parent owes its own 1.5 plus the child's 0.25, got %v", got.Cost.USD)
	}
	if got.Children != 1 {
		t.Fatalf("one task was asked, %d were counted", got.Children)
	}
}

func TestPartialChildMakesParentPartial(t *testing.T) {
	// P32, and the half that gets skipped: an aggregate over uncertain values
	// is itself uncertain. A confident total here is S9's bug one level up.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := e.askDone(t, "planner", "reddit-agent",
		model.Cost{State: model.CostKnown, USD: 1.5},
		model.Cost{State: model.CostPartial, USDAtLeast: 0.25, Missing: []string{"output"}})

	got, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if got.Cost.State != model.CostPartial {
		t.Fatalf("a partial child makes the parent partial, never known: %+v", got.Cost)
	}
	if got.Cost.USD != 0 {
		t.Fatalf("a partial rollup reports no total: %+v", got.Cost)
	}
	if !almost(got.Cost.USDAtLeast, 1.75) {
		t.Fatalf("the floors still add up, got %v", got.Cost.USDAtLeast)
	}
	if len(got.Cost.Missing) == 0 || got.Cost.Missing[0] != "output" {
		t.Fatalf("the parent must say what is missing, got %v", got.Cost.Missing)
	}
}

func TestUnknownChildMakesParentNotKnown(t *testing.T) {
	// The strict direction: an unknown child makes the parent NOT KNOWN. At
	// best partial — which is what the notes say, and what this test pins.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := e.askDone(t, "planner", "reddit-agent",
		model.Cost{State: model.CostKnown, USD: 1.5},
		model.Cost{State: model.CostUnknown, Reason: model.ReasonProviderOmittedUsage})

	got, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if got.Cost.State == model.CostKnown {
		t.Fatalf("a chain with an unknown child is never known: %+v", got.Cost)
	}
	if got.Cost.State != model.CostPartial {
		t.Fatalf("the rollup is at best partial, got %q", string(got.Cost.State))
	}
	if !almost(got.Cost.USDAtLeast, 1.5) {
		t.Fatalf("the unknown child contributes nothing to the floor, which must stay the known part: %v", got.Cost.USDAtLeast)
	}
	if !strings.Contains(got.Cost.Reason, model.ReasonProviderOmittedUsage) {
		t.Fatalf("the parent says WHY it cannot be certain: %+v", got.Cost)
	}
}

func TestAllKnownChildrenGiveKnownParent(t *testing.T) {
	e := newChainEnv(t)
	for _, a := range []string{"planner", "investigator", "technical-writer"} {
		e.offer(t, a)
	}
	// A two-level chain: planner → investigator → technical-writer.
	rootStep := runningStepAs(t, e.s, e.loopSt, "planner")
	if err := e.s.SetStepCost(rootStep, model.Cost{State: model.CostKnown, USD: 1.0}); err != nil {
		t.Fatal(err)
	}
	top, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: rootStep,
		CalleeAgent: "investigator", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("investigator", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), top.ID, "d", "ok",
		&model.Cost{State: model.CostKnown, USD: 0.5}); rj != nil {
		t.Fatal(rj.Detail)
	}
	// The investigator's OWN delegation — recorded under the investigator's
	// step, which is the whole reason the rollup must walk the graph.
	innerStep := runningStepAs(t, e.s, e.loopSt, "investigator")
	if err := e.s.SetStepCost(innerStep, model.Cost{State: model.CostKnown, USD: 0.2}); err != nil {
		t.Fatal(err)
	}
	sub, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "investigator", CallerStepID: innerStep,
		CalleeAgent: "technical-writer", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("technical-writer", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), sub.ID, "d", "ok",
		&model.Cost{State: model.CostKnown, USD: 0.1}); rj != nil {
		t.Fatal(rj.Detail)
	}

	got, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, rootStep)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if got.Cost.State != model.CostKnown || !almost(got.Cost.USD, 1.8) {
		t.Fatalf("a chain of knowns rolls up known: %+v", got.Cost)
	}
	if got.Children != 2 {
		t.Fatalf("both asks count, the direct one and the one below it: %d", got.Children)
	}
}

func TestRollupSurvivesRestart(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := e.askDone(t, "planner", "reddit-agent",
		model.Cost{State: model.CostKnown, USD: 1.5},
		model.Cost{State: model.CostKnown, USD: 0.25})
	before, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}

	dir := e.dir
	if err := e.s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	iv2 := NewInvoker(s2, OpenCardStore(filepath.Join(dir, "cards")))

	after, rj := iv2.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if after.Cost.State != before.Cost.State || !almost(after.Cost.USD, before.Cost.USD) ||
		!almost(after.Cost.USDAtLeast, before.Cost.USDAtLeast) || after.Children != before.Children {
		t.Fatalf("the rollup must be a query over rows, not accumulated state: before %+v after %+v", before, after)
	}
}

func TestNoRunningTotalIsCached(t *testing.T) {
	// A cached total is a number that disagrees with the rows and nothing
	// says which is right. Asserted by grep on the file, not by trust: no
	// package-level mutable state, no locking, no background anything.
	raw, err := os.ReadFile("cost.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if regexp.MustCompile(`(?m)^var\s`).MatchString(src) {
		t.Fatal("cost.go declares package-level state — the rollup must be a query")
	}
	for _, banned := range []string{"sync.Mutex", "sync.RWMutex", "atomic.", "go func", "time.Sleep"} {
		if strings.Contains(src, banned) {
			t.Fatalf("cost.go uses %q — an accumulator in disguise", banned)
		}
	}
	if !regexp.MustCompile(`(?m)^func \(iv \*Invoker\) RollupCost`).MatchString(src) {
		t.Fatal("RollupCost must be a method on the Invoker, reading through it")
	}
	// And behaviourally: a completion after the first reading must appear in
	// the second one.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, e.s, e.loopSt, "planner")
	if err := e.s.SetStepCost(stepID, model.Cost{State: model.CostKnown, USD: 1.0}); err != nil {
		t.Fatal(err)
	}
	first, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if first.Children != 0 || !almost(first.Cost.USD, 1.0) {
		t.Fatalf("a step that has asked nothing rolls up its own cost alone: %+v", first)
	}
	task, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("reddit-agent", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), task.ID, "d", "ok",
		&model.Cost{State: model.CostKnown, USD: 0.3}); rj != nil {
		t.Fatal(rj.Detail)
	}
	second, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if second.Children != 1 || !almost(second.Cost.USD, 1.3) {
		t.Fatalf("the second reading must see what the first could not — a cache would not: %+v", second)
	}
}

func TestFailedOrRefusedChainStillRollsUp(t *testing.T) {
	// Scope 5: a rollup that only works on success hides exactly the
	// expensive failures.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, e.s, e.loopSt, "planner")
	if err := e.s.SetStepCost(stepID, model.Cost{State: model.CostKnown, USD: 2.0}); err != nil {
		t.Fatal(err)
	}
	task, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("reddit-agent", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	// The callee measured what it could and then failed.
	if rj := e.iv.tasks.FailWithCost(context.Background(), task.ID, "the callee gave up",
		&model.Cost{State: model.CostPartial, USDAtLeast: 0.4, Missing: []string{"output"}}); rj != nil {
		t.Fatal(rj.Detail)
	}
	got, rj := e.iv.RollupCost(context.Background(), e.loopSt.ID, stepID)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if got.Children != 1 {
		t.Fatalf("a failed ask is still an ask that cost something: %+v", got)
	}
	if got.Cost.State != model.CostPartial || !almost(got.Cost.USDAtLeast, 2.4) {
		t.Fatalf("the failure's measured floor belongs in the total: %+v", got.Cost)
	}

	// And a task failed with NO cost reported reads as unknown, not zero.
	e2 := newChainEnv(t)
	e2.offer(t, "planner")
	e2.offer(t, "reddit-agent")
	s2 := runningStepAs(t, e2.s, e2.loopSt, "planner")
	if err := e2.s.SetStepCost(s2, model.Cost{State: model.CostKnown, USD: 1.0}); err != nil {
		t.Fatal(err)
	}
	t2, r := e2.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e2.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: s2,
		CalleeAgent: "reddit-agent", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("reddit-agent", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e2.iv.tasks.Fail(context.Background(), t2.ID, "no numbers arrived"); rj != nil {
		t.Fatal(rj.Detail)
	}
	rolled, rj := e2.iv.RollupCost(context.Background(), e2.loopSt.ID, s2)
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if rolled.Cost.State == model.CostKnown {
		t.Fatalf("a failed child that reported nothing makes a known total a lie: %+v", rolled.Cost)
	}
	if !almost(rolled.Cost.USDAtLeast, 1.0) {
		t.Fatalf("the known part is still known: %v", rolled.Cost.USDAtLeast)
	}
}

func TestCostThatCannotBeRecordedIsRefused(t *testing.T) {
	// validateCost: M10's states, no invention. A cost is stored or refused
	// naming the thing refused — never quietly dropped.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, e.s, e.loopSt, "planner")
	task, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "do-thing", Text: "ask", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("reddit-agent", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r != nil {
		t.Fatal(r.Detail)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), task.ID, "d", "ok",
		&model.Cost{State: model.CostUnknown}); rj == nil || rj.Field != "cost" {
		t.Fatalf("an unknown cost must carry a reason: %v", rj)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), task.ID, "d", "ok",
		&model.Cost{State: model.CostKnown, USD: -3}); rj == nil || rj.Field != "cost" {
		t.Fatalf("a negative cost is not a measurement: %v", rj)
	}
	if rj := e.iv.tasks.CompleteWithCost(context.Background(), task.ID, "d", "ok",
		&model.Cost{State: "certain"}); rj == nil || rj.Field != "cost" {
		t.Fatalf("a state outside the three is refused by name: %v", rj)
	}
	// Still completable afterwards, honestly.
	if rj := e.iv.tasks.Complete(context.Background(), task.ID, "d", "ok"); rj != nil {
		t.Fatal(rj.Detail)
	}
	fresh, err := e.iv.tasks.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Cost.State != model.CostUnknown || fresh.Cost.Reason != "cost-not-reported" {
		t.Fatalf("an unreported cost reads as the unknown it is, and says why: %+v", fresh.Cost)
	}
}
