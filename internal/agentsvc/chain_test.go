package agentsvc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

// T5 — chain.go. Two failures, two diagnoses: a cycle is a structural
// mistake in the graph (fix the graph); depth is a runaway (fix the graph or
// accept nobody audits that deep). One test each, and each test asserts the
// OTHER diagnosis is absent — that absence is the task.

// chainEnv: a card store where any named agent exists with one skill, plus a
// real pinned loop whose steps can run as named agents.
type chainEnv struct {
	s      *store.Store
	iv     *Invoker
	cards  string
	dir    string // the store directory — TestChainSurvivesRestart reopens it
	loopSt loop.LoopState
}

func newChainEnv(t *testing.T) *chainEnv {
	t.Helper()
	s, dir := taskTestStore(t)
	cardsDir := filepath.Join(dir, "cards")
	if err := os.MkdirAll(cardsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The pinned loop's lane is plain; the chain tests create their own steps
	// with their own caller agents (the invoke gate makes the step's agent
	// the caller's identity).
	st, _ := startTaskLoop(t, s, "chain-loop")
	iv := NewInvoker(s, OpenCardStore(cardsDir))
	return &chainEnv{s: s, iv: iv, cards: cardsDir, dir: dir, loopSt: st}
}

func (e *chainEnv) offer(t *testing.T, agent string) {
	t.Helper()
	body := `{"name":"` + agent + `","description":"chain actor","version":"1.0.0",
	  "defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"],
	  "skills":[{"id":"do-thing","name":"Do the thing","description":"the thing"}]}`
	sc, err := Sign([]byte(body), testKey())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.cards, agent+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ask records caller → callee as a real invocation (edge + async task), so
// the graph under test is exactly what invoke.go writes — not a fixture
// hand-inserted around the gate.
func (e *chainEnv) ask(t *testing.T, caller, callee string) (Task, *Reject) {
	t.Helper()
	stepID := runningStepAs(t, e.s, e.loopSt, caller)
	// The S14 policy door judges the FROZEN set (T7): an allow for this very
	// ref rides alongside T3's deny for another one — PL4 means the deny that
	// matches decides, and these tests are not about that; they are about
	// the graph, which refuses only what the policy lets through the door.
	return e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: caller, CallerStepID: stepID,
		CalleeAgent: callee, SkillID: "do-thing", Text: "asked", Mode: TaskAsync,
		Frozen: append(oneDeny(), frozenAllow(SkillRef(callee, "do-thing"))...), EvaluatedAt: time.Now(),
	})
}

func TestCycleIsRefusedAndNamed(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	if _, r := e.ask(t, "planner", "reddit-agent"); r != nil {
		t.Fatal(r.Detail)
	}
	_, r := e.ask(t, "reddit-agent", "planner")
	if r == nil {
		t.Fatal("planner → reddit-agent → planner must be refused")
	}
	if r.Field != "cycle" {
		t.Fatalf("the field must name what refused: %q", r.Field)
	}
	if !strings.Contains(r.Detail, "reddit-agent → planner → reddit-agent") {
		t.Fatalf("the refusal must name the path in order, starting at the agent making the refused ask: %q", r.Detail)
	}
	// Cycle of one.
	if _, r := e.ask(t, "planner", "planner"); r == nil || r.Field != "cycle" ||
		!strings.Contains(r.Detail, "planner → planner") {
		t.Fatalf("self-ask is a cycle of one, named: %v", r)
	}
}

func TestCycleIsNotACostCapFailure(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	if _, r := e.ask(t, "planner", "reddit-agent"); r != nil {
		t.Fatal(r.Detail)
	}
	_, r := e.ask(t, "reddit-agent", "planner")
	if r == nil {
		t.Fatal("the cycle must be refused")
	}
	low := strings.ToLower(r.Detail)
	// The words a MISDIAGNOSIS would speak — "exceeded", a depth number, a
	// bound — are absent; the sentence may MENTION cost only to disclaim it
	// ("not a cost failure"), which is the distinction this test exists for.
	for _, forbidden := range []string{"exceeded", "depth", "bound", "raise"} {
		if strings.Contains(low, forbidden) {
			t.Fatalf("a cycle reported through %q sends somebody to raise a number while they hold an infinite loop: %q", forbidden, r.Detail)
		}
	}
	if !strings.Contains(low, "not a cost failure") {
		t.Fatalf("the cycle refusal must disclaim the cost diagnosis explicitly: %q", r.Detail)
	}
	if !strings.Contains(low, "fix the graph") {
		t.Fatalf("the cycle's fix is the graph, and the refusal must say so: %q", r.Detail)
	}
}

func TestDepthRefusalIsNotANamelessCycle(t *testing.T) {
	// The other half of the two-diagnoses contract: the depth refusal states
	// both numbers, names the runaway as a runaway, and says out loud that
	// this is NOT a cycle — the sentence that keeps someone from going to
	// fix a graph that has no cycle in it.
	// Four asks are already recorded (planner → … → verifier-engineer);
	// asking qa-engineer next would make the path FIVE — one past the bound.
	e := deepChain(t, "planner", "investigator", "technical-writer", "debugger-engineer", "verifier-engineer", "qa-engineer")
	_, r := e.ask(t, "verifier-engineer", "qa-engineer")
	if r == nil {
		t.Fatal("a chain one past the bound must be refused")
	}
	if r.Field != "depth" {
		t.Fatalf("the field must be depth, not cycle: %q", r.Field)
	}
	if !strings.Contains(r.Detail, "depth 5") || !strings.Contains(r.Detail, "bound is 4") {
		t.Fatalf("the boundary refusal states BOTH numbers: %q", r.Detail)
	}
	if !strings.Contains(strings.ToLower(r.Detail), "not a cycle") {
		t.Fatalf("the depth refusal must distinguish itself from the cycle refusal: %q", r.Detail)
	}
}

func TestRefusedCycleWritesNoTaskOrEdge(t *testing.T) {
	// The cycle is refused BEFORE the Task is created — and before the edge,
	// because T4's rule is that an illegal ask leaves no trace: a recorded
	// attempt is for crashes, not for calls that were never legal.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	if _, r := e.ask(t, "planner", "reddit-agent"); r != nil {
		t.Fatal(r.Detail)
	}
	_, r := e.ask(t, "reddit-agent", "planner")
	if r == nil {
		t.Fatal("refuse first")
	}
	rows, err := e.s.ChainEdgesByCallerAgent("reddit-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("the refused cycle left %d recorded edge(s): %#v", len(rows), rows)
	}
}

func TestDepthBoundRefusedAtBoundary(t *testing.T) {
	// A chain of EXACTLY the bound is allowed: four asks down is the shape
	// the bound was reasoned for, and refusing it would be one-past-tight
	// policy posing as safety.
	e := deepChain(t, "planner", "investigator", "technical-writer", "debugger-engineer", "verifier-engineer")
	if _, r := e.ask(t, "debugger-engineer", "verifier-engineer"); r != nil {
		t.Fatalf("a chain of exactly the bound (%d) must be allowed: %s", ChainDepthBound, r.Detail)
	}
	// And one deeper is refused — TestDepthRefusalIsNotANamelessCycle reads
	// the sentence; this test reads the boundary.
	e2 := deepChain(t, "planner", "investigator", "technical-writer", "debugger-engineer", "verifier-engineer")
	if _, r := e2.ask(t, "debugger-engineer", "verifier-engineer"); r != nil {
		t.Fatalf("the ask that completes a chain of exactly the bound must be allowed: %s", r.Detail)
	}
	e3 := deepChain(t, "planner", "investigator", "technical-writer", "debugger-engineer", "verifier-engineer", "qa-engineer")
	if _, r := e3.ask(t, "verifier-engineer", "qa-engineer"); r == nil || r.Field != "depth" {
		t.Fatalf("one past the bound must be refused as depth, got %v", r)
	}
}

func deepChain(t *testing.T, agents ...string) *chainEnv {
	t.Helper()
	e := newChainEnv(t)
	for _, a := range agents {
		e.offer(t, a)
	}
	for i := 0; i+1 < len(agents)-1; i++ {
		if _, r := e.ask(t, agents[i], agents[i+1]); r != nil {
			t.Fatalf("building the chain %s→%s: %s", agents[i], agents[i+1], r.Detail)
		}
	}
	return e
}

func TestChainSurvivesRestart(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	if _, r := e.ask(t, "planner", "reddit-agent"); r != nil {
		t.Fatal(r.Detail)
	}
	// Close the handle and reopen the SAME directory — a restarted daemon
	// reconstructs the graph from rows and refuses the same cycle with the
	// same named path. The in-memory truth of the previous process had no
	// role in the answer.
	dir := e.dir
	if err := e.s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	iv2 := NewInvoker(s2, OpenCardStore(e.cards))
	_, r := iv2.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "reddit-agent",
		CallerStepID: runningStepAs(t, s2, e.loopSt, "reddit-agent"),
		CalleeAgent:  "planner", SkillID: "do-thing", Text: "asked", Mode: TaskAsync,
		Frozen: append(oneDeny(), frozenAllow(SkillRef("planner", "do-thing"))...), EvaluatedAt: time.Now(),
	})
	if r == nil || r.Field != "cycle" || !strings.Contains(r.Detail, "reddit-agent → planner → reddit-agent") {
		t.Fatalf("the chain must be reconstructable after a restart: %v", r)
	}
}

func TestInvokeGateRejectsForeignIdentity(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, e.s, e.loopSt, "golang-engineer")
	_, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "do-thing", Text: "x", Mode: TaskAsync,
		Frozen: frozenAllow(SkillRef("reddit-agent", "do-thing")), EvaluatedAt: time.Now(),
	})
	if r == nil || r.Field != "callerAgent" ||
		!strings.Contains(r.Detail, "golang-engineer") || !strings.Contains(r.Detail, "planner") {
		t.Fatalf("an ask filed under another agent's step must be refused naming both: %v", r)
	}
}

func TestDiamondIsNotACycle(t *testing.T) {
	// A→B, A→C, B→D, C→D: the same specialist asked by two paths is the
	// NORMAL shape of a map, not a cycle. A detector that flags revisits
	// (rather than repeats ON THE PATH) refuses collaboration itself.
	e := newChainEnv(t)
	for _, a := range []string{"planner", "investigator", "technical-writer", "debugger-engineer"} {
		e.offer(t, a)
	}
	for pair := [][2]string{
		{"planner", "investigator"}, {"planner", "technical-writer"},
		{"investigator", "debugger-engineer"},
	}; len(pair) > 0; pair = nil {
		for _, p := range pair {
			if _, r := e.ask(t, p[0], p[1]); r != nil {
				t.Fatalf("diamond edge %s→%s refused: %s", p[0], p[1], r.Detail)
			}
		}
	}
	if _, r := e.ask(t, "technical-writer", "debugger-engineer"); r != nil {
		t.Fatalf("rejoining at a shared specialist is not a cycle: %s", r.Detail)
	}
}
