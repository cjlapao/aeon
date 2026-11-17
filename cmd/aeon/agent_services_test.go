package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/agentsvc"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// T4 — the entry-point wiring tested end to end at the level it exists:
// a real store, a real pinned loop, the committed signed card, the real
// Invoker. Not the daemon (its branch has its own test) and not agentsvc
// (its refusals have theirs) — the ADAPTER is the piece that turns a lane's
// `skill:` into a suspended step, and only an integration test can catch it
// assembling the parts wrong (the step row, the frozen set, the async mode,
// the QuestionID handoff).

func adapterFixture(t *testing.T) (*store.Store, *agentsvc.Invoker, loop.LoopState) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	cardsDir := filepath.Join(t.TempDir(), "cards")
	if err := os.MkdirAll(cardsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "agentsvc", "testdata", "cards", "reddit-agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cardsDir, "reddit-agent.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	iv := agentsvc.NewInvoker(s, agentsvc.OpenCardStore(cardsDir))

	// T7's door judges the frozen set the adapter reads from the REAL policy
	// store, so the fixture authors what an operator would author: one
	// skill_access allow for the skill the lane asks for, through Add — the
	// authorship boundary included (an allow only from operator), never a
	// hand-inserted row that skips it.
	ps := policy.NewStore(policyRepo(s))
	if rj := ps.Add(context.Background(), policy.Policy{
		ID: "s14-adapter-allow", Type: policy.SkillAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow,
		Spec:   json.RawMessage(`{"skill":"reddit-agent/summarise-sentiment","state":"allow"}`),
		Source: policy.Operator,
	}, time.Now()); rj != nil {
		t.Fatalf("seed the operator's rule: %s", rj.Reason)
	}

	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "adapterlane.yaml")
	lane := "id: adapterlane\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: survey\n    step: {kind: model-step, agent: project-planner, produces: SURVEY.md}\n"
	if err := os.WriteFile(path, []byte(lane), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Start(context.Background(), s, store.NewLoop{Slug: "adapter", ComponentRoot: root, Root: root, Goal: "ask"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, iv, st
}

func TestAgentInvokeForStepSuspendsThroughTheTaskLayer(t *testing.T) {
	s, iv, st := adapterFixture(t)
	run := agentInvokeForStep(s, iv)
	res, err := run(context.Background(), st, blueprint.Step{
		Agent: "project-planner", Skill: "reddit-agent/summarise-sentiment",
		Params: map[string]any{"query": "aeon"},
	}, "what do people say about aeon?", "")
	if err != nil {
		t.Fatalf("a legal delegation must suspend, not error: %v", err)
	}
	if res.Kind != "suspended" || res.StepID == 0 || res.QuestionID == 0 {
		t.Fatalf("the handoff must carry the step and its question for advance.go's suspended branch: %#v", res)
	}
	// The step exited (P2): it is suspended in the rows, and the task is the
	// record of the ask — text, params, callee, the chain edge.
	row, err := s.ReadStep(res.StepID)
	if err != nil || row.Status != string(store.StepSuspended) {
		t.Fatalf("the delegating step must be suspended: %#v %v", row, err)
	}
	task, err := agentsvc.NewTaskStore(s).Get(1)
	if err != nil {
		t.Fatalf("the ask must be a row: %v", err)
	}
	if task.Mode != agentsvc.TaskAsync || task.Status != agentsvc.TaskWaiting ||
		!strings.Contains(task.Text, "what do people say") || task.Params["query"] != "aeon" {
		t.Fatalf("the task row lost the ask: %#v", task)
	}
	if n, err := s.CountAgentChainEdges(st.ID, res.StepID); err != nil || n != 1 {
		t.Fatalf("the edge must ride with the ask (AS6): %d %v", n, err)
	}
}

func TestAgentInvokeForStepRefusalsLeaveNoRunningRow(t *testing.T) {
	t.Run("malformed target", func(t *testing.T) {
		s, iv, st := adapterFixture(t)
		_, err := agentInvokeForStep(s, iv)(context.Background(), st,
			blueprint.Step{Agent: "project-planner", Skill: "reddit-agent"}, "x", "")
		if err == nil || !strings.Contains(err.Error(), "`agent/skill`") {
			t.Fatalf("a target without the agent half must be refused with the format: %v", err)
		}
	})
	t.Run("unknown skill settles the step reject", func(t *testing.T) {
		s, iv, st := adapterFixture(t)
		_, err := agentInvokeForStep(s, iv)(context.Background(), st,
			blueprint.Step{Agent: "project-planner", Skill: "reddit-agent/translate-french"}, "x", "")
		if err == nil || !strings.Contains(err.Error(), "translate-french") {
			t.Fatalf("the refusal must reach the loop as its block reason: %v", err)
		}
		// And it must not leave a `running` row for S7's reconciler to puzzle
		// over: the refused delegation is done/reject, an answer on the record.
		rows, err := s.ListSteps(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.Root})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Status != string(store.StepDone) || rows[0].Outcome != "reject" {
			t.Fatalf("the refused delegating step must settle done/reject: %#v", rows)
		}
	})
}
