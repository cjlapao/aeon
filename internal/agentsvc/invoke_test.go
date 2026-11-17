package agentsvc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// T4 — invoke.go: agent to agent. A planner wanting a report from Reddit does
// not want reddit.search — it wants "find what people are saying and
// summarise it": judgement about how to use tools (AS10), not a capability.
// S8's skill:/params: amendment landed at T0 (6fc0d53), so this task runs
// rather than reporting itself blocked — prerequisite verified, not assumed.

// cardedStore is taskTestStore plus a card directory holding a real signed
// card (T1's own fixture bytes — the same file the card tests walk).
// runningStepAs is T3's runningStep for a named agent: T4's invoke gate makes
// the step's agent the caller's identity (an ask filed under another agent's
// step would be attributed to the wrong agent by the cycle walk), so the
// tests that invoke AS planner must run a step AS planner.
func runningStepAs(t *testing.T, s *store.Store, st loop.LoopState, agent string) int64 {
	t.Helper()
	step, err := s.CreateStep(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.Root},
		store.NewStep{Phase: "model", Agent: agent, Attempt: 1, Status: store.StepRunning, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return step.ID
}

func cardedStore(t *testing.T) (*store.Store, *CardStore, string) {
	t.Helper()
	s, dir := taskTestStore(t)
	cardsDir := filepath.Join(dir, "cards")
	if err := os.MkdirAll(cardsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/cards/reddit-agent.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cardsDir, "reddit-agent.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return s, OpenCardStore(cardsDir), dir
}

func TestAgentInvokesAnotherAgentsSkill(t *testing.T) {
	s, cards, _ := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-async")
	stepID := runningStepAs(t, s, st, "planner")
	iv := NewInvoker(s, cards)
	task, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Text: "what do people say about aeon?", Params: map[string]any{"query": "aeon"},
		Mode:   TaskAsync,
		Frozen: append(oneDeny(), frozenAllow("reddit-agent/summarise-sentiment")...), EvaluatedAt: time.Now(),
	})
	if reject != nil {
		t.Fatalf("invoke: %s", reject.Detail)
	}
	if task.Status != TaskWaiting || task.CalleeAgent != "reddit-agent" || task.SkillID != "summarise-sentiment" {
		t.Fatalf("the invocation did not become a waiting task: %#v", task)
	}
	row, err := s.ReadStep(stepID)
	if err != nil || row.Status != string(store.StepSuspended) {
		t.Fatalf("the invoking step must suspend through T3's task layer (no second waiting path): %#v %v", row, err)
	}
	// The judgement text and the params ride the task row — the callee answers
	// from rows, not memory.
	got, err := s.GetAgentTask(task.ID)
	if err != nil || !strings.Contains(got.ParamsJSON, "what do people say") || !strings.Contains(got.ParamsJSON, "aeon") {
		t.Fatalf("params/text lost: %#v %v", got, err)
	}
}

func TestInvocationIsRecordedAsAChainEdge(t *testing.T) {
	s, cards, _ := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-edge")
	stepID := runningStepAs(t, s, st, "planner")
	iv := NewInvoker(s, cards)
	if _, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Text: "x", Mode: TaskAsync, Frozen: append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...), EvaluatedAt: time.Now(),
	}); reject != nil {
		t.Fatal(reject.Detail)
	}
	n, err := s.CountAgentChainEdges(st.ID, stepID)
	if err != nil || n != 1 {
		t.Fatalf("the invocation must record exactly one edge: %d %v", n, err)
	}

	// THE ORDERING PROOF — the edge is written BEFORE the Task runs: this call
	// passes every check the edge is gated on (card, skill, params) and then
	// fails at the step guard INSIDE the task transaction. A crash-mid-invoke
	// shape must leave the recorded attempt, not an untraceable cost.
	deadStep := runningStepAs(t, s, st, "planner")
	if err := s.SetStepStatus(deadStep, store.StepDone, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: deadStep,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Text: "x", Mode: TaskAsync, Frozen: append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...), EvaluatedAt: time.Now(),
	}); reject == nil {
		t.Fatal("invoking a finished step must refuse")
	}
	n, err = s.CountAgentChainEdges(st.ID, deadStep)
	if err != nil || n != 1 {
		t.Fatalf("the refused-at-task-stage attempt must still be a recorded edge: %d %v", n, err)
	}
	// And the refused attempt wrote NO task row — the task transaction rolled
	// back on its own guard. The count reads are store methods precisely so
	// this proof does not smuggle SQL into a consumer (I2).
	if n, err := s.CountAgentTasksForCaller(st.ID, deadStep); err != nil || n != 0 {
		t.Fatalf("a refused invoke must leave no task row, only its recorded edge: %d %v", n, err)
	}
}

func TestUnknownSkillRefusedWithReason(t *testing.T) {
	s, cards, _ := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-unknown")
	stepID := runningStepAs(t, s, st, "planner")
	iv := NewInvoker(s, cards)
	_, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "translate-french",
		Text: "x", Mode: TaskAsync, Frozen: append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...), EvaluatedAt: time.Now(),
	})
	if reject == nil {
		t.Fatal("an unknown skill was invoked")
	}
	for _, want := range []string{"reddit-agent", "translate-french", "summarise-sentiment"} {
		if !strings.Contains(reject.Detail, want) {
			t.Fatalf("refusal must name the agent, the skill, and what exists — missing %q in %q", want, reject.Detail)
		}
	}
	// A13's shape: an enumeration, not a suggestion engine — the refusal does
	// not claim translate-french "meant" summarise-sentiment.
	if strings.Contains(strings.ToLower(reject.Detail), "did you mean") {
		t.Fatalf("an enumeration refuses; a suggestion engine decides for the caller: %q", reject.Detail)
	}
	// Nothing was written: no task, no edge for a call that was never legal.
	if n, err := s.CountAgentChainEdges(st.ID, stepID); err != nil || n != 0 {
		t.Fatalf("an illegal invocation must not leave an edge: %d %v", n, err)
	}
}

func TestNoCardIsRefusedWithTheRemedy(t *testing.T) {
	s, _, dir := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-nocard")
	stepID := runningStepAs(t, s, st, "planner")
	iv := NewInvoker(s, OpenCardStore(filepath.Join(dir, "empty-cards")))
	_, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "nobody-agent", SkillID: "anything",
		Text: "x", Mode: TaskAsync, Frozen: append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...), EvaluatedAt: time.Now(),
	})
	if reject == nil {
		t.Fatal("an agent with no card was invoked")
	}
	if !strings.Contains(reject.Detail, "nobody-agent") || !strings.Contains(reject.Detail, "card") {
		t.Fatalf("the refusal must name the agent and say it has no card: %q", reject.Detail)
	}
	// A refusal names the thing refused AND the remedy.
	if !strings.Contains(reject.Detail, "publish") && !strings.Contains(reject.Detail, "nobody-agent.json") {
		t.Fatalf("the refusal must state the remedy (publish a signed card at <dir>/nobody-agent.json): %q", reject.Detail)
	}
}

// TestCallerDoesNotReceiveToolList — MC2 preserved (S12). The callee's tools
// are its own business; the caller gets a judgement back, not a capability
// surface.
func TestCallerDoesNotReceiveToolList(t *testing.T) {
	s, cards, _ := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-mc2")
	stepID := runningStepAs(t, s, st, "planner")
	spy := &spyClient{}
	iv := NewInvoker(s, cards)
	task, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Text: "x", Mode: TaskSync, Client: spy, Frozen: append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...), EvaluatedAt: time.Now(),
	})
	if reject != nil {
		t.Fatalf("sync invoke: %s", reject.Detail)
	}
	if spy.calls != 1 {
		t.Fatalf("the sync path must run the client once: %d", spy.calls)
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskCompleted || task.ResultText() != "ok" {
		t.Fatalf("the sync call must come back settled with the callee's judgement: %#v", task)
	}
	if strings.Contains(strings.ToLower(string(raw)), "tool") {
		t.Fatalf("MC2 undone — the caller's view of an invocation names tools: %s", raw)
	}
	// The callee HAS tools (golang-engineer's entry declares a policy); the
	// point is that none of it crosses.
	if strings.Contains(strings.ToLower(spy.prompt), "tool") {
		t.Fatalf("the prompt handed to the client leaks a tool surface: %s", spy.prompt)
	}
}

// TestInvokeReusesTaskLayerNoSecondWaitingPath — asserted by grep (the task's
// validation demands): invoke.go must not touch step rows, questions, or
// goroutines; every wait goes through TaskStore (T3), which owns suspension.
func TestInvokeReusesTaskLayerNoSecondWaitingPath(t *testing.T) {
	raw, err := os.ReadFile("invoke.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`\bgo func`),
		regexp.MustCompile(`SetStepStatus`),
		regexp.MustCompile(`\.Ask\(`),
		regexp.MustCompile(`\.Answer\(`),
		regexp.MustCompile(`CompleteSuspendedStep`),
		regexp.MustCompile(`sync\.(WaitGroup|Mutex|Once)`),
		regexp.MustCompile(`time\.Sleep`),
	} {
		if forbidden.MatchString(src) {
			t.Fatalf("invoke.go grows a second waiting path (%s) — task.go owns suspension (T3)", forbidden)
		}
	}
	if !strings.Contains(src, "ts.Create(") && !strings.Contains(src, "tasks.Create(") {
		t.Fatal("invoke.go does not go through the TaskStore at all — where does its waiting live?")
	}
}

func TestSkillKnownDrivesThePinDecision(t *testing.T) {
	s, cards, _ := cardedStore(t)
	iv := NewInvoker(s, cards)
	if !iv.SkillKnown("reddit-agent", "summarise-sentiment") {
		t.Fatal("a skill the signed card offers must be known")
	}
	if iv.SkillKnown("reddit-agent", "translate-french") {
		t.Fatal("a skill the card does not offer must be unknown")
	}
	if iv.SkillKnown("nobody-agent", "x") {
		t.Fatal("an agent with no card can be known to offer nothing")
	}
	// A TAMPERED card contributes no knowledge either — verification failure is
	// not "the old card was probably fine".
	dir := cards.dir
	tampered, err := os.ReadFile("testdata/cards/reddit-agent-tampered.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tampered-agent.json"), tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if iv.SkillKnown("tampered-agent", "summarise-sentiment") {
		t.Fatal("a tampered card must not make a skill known (AS2 refuses, it does not warn)")
	}
}

func TestInvokeCarriesTheCreationTimePolicySet(t *testing.T) {
	s, cards, _ := cardedStore(t)
	st, _ := startTaskLoop(t, s, "invoke-frozen")
	stepID := runningStepAs(t, s, st, "planner")
	iv := NewInvoker(s, cards)
	// AS13 judged over the set T7's door actually needs: a deny for one ref
	// and the allow this ask rides on. The task must carry BOTH, and the
	// hash must be theirs — a resumed evaluation reading today's table would
	// read a different world.
	frozen := append(oneDeny(), frozenAllow(SkillRef("reddit-agent", "summarise-sentiment"))...)
	task, reject := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: st.ID, CallerLane: "work", CallerAgent: "planner", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Text: "x", Mode: TaskAsync, Frozen: frozen, EvaluatedAt: time.Now(),
	})
	if reject != nil {
		t.Fatal(reject.Detail)
	}
	set, _, err := iv.tasks.FrozenSet(task.ID)
	if err != nil || len(set) != len(frozen) || set[0].Action != policy.Deny || set[1].Action != policy.Allow {
		t.Fatalf("AS13: the invocation must carry the set it was created under: %v %v", set, err)
	}
}
