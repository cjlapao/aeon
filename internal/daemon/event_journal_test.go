package daemon

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// The journal is written with its labels: an event naming a loop is filed under it, a step event is filed under
// its step's loop (found once, then cached), a lifecycle event belongs to nobody, and one loop's window reads back
// only its own events.
func TestTheJournalFilesEventsUnderTheirLoop(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.CreateLoop(store.NewLoop{Slug: "alpha", ComponentRoot: "/r", Title: "a", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateLoop(store.NewLoop{Slug: "beta", ComponentRoot: "/r", Title: "b", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "alpha", ComponentRoot: "/r"}, store.NewStep{Phase: "p", Agent: "x", Attempt: 1, Status: store.StepRunning, Source: "live"})
	if err != nil {
		t.Fatal(err)
	}
	backing := NewEventBacking(s)
	put := func(kind string, payload any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if _, err := backing.Append(kind, "", "/r", raw); err != nil {
			t.Fatal(err)
		}
	}
	put("loop.advanced", map[string]any{"loopId": a.ID})
	put("step.tool.start", map[string]any{"stepId": step.ID, "tool": "bash"}) // no loopId: found through the step
	put("step.tool.start", map[string]any{"stepId": step.ID, "tool": "read"}) // second time: from the cache
	put("loop.advanced", map[string]any{"loopId": b.ID})
	put("daemon.started", map[string]any{"v": 1})

	win, err := s.ReadLoopEvents(a.ID, 0, 0, 50, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(win.Events) != 3 {
		t.Fatalf("alpha's journal has %d events, want 3: %s", len(win.Events), fmt.Sprint(win.Events))
	}
	for _, ev := range win.Events {
		if ev.LoopSlug != "alpha" {
			t.Errorf("event %d filed under slug %q, want alpha", ev.Seq, ev.LoopSlug)
		}
	}
	if win.Events[1].StepID != step.ID {
		t.Errorf("the step event lost its step id: %+v", win.Events[1])
	}
	other, _ := s.ReadLoopEvents(b.ID, 0, 0, 50, nil, nil)
	if len(other.Events) != 1 {
		t.Errorf("beta's journal has %d events, want 1", len(other.Events))
	}
}

// A step's events, filed through the daemon's own backing (the step found by id, the loop by the step), keep that
// step's summary current, and `loop.steps` reads it back with the step's agent and status joined from the step row.
func TestStepEventsKeepTheStepSummary(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.CreateLoop(store.NewLoop{Slug: "alpha", ComponentRoot: "/r", Title: "a", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "alpha", ComponentRoot: "/r"}, store.NewStep{Phase: "p", Agent: "planner", Attempt: 1, Status: store.StepRunning, Source: "live"})
	if err != nil {
		t.Fatal(err)
	}
	backing := NewEventBacking(s)
	put := func(kind string, payload map[string]any) {
		t.Helper()
		payload["stepId"] = step.ID
		raw, _ := json.Marshal(payload)
		if _, err := backing.Append(kind, "", "/r", raw); err != nil {
			t.Fatal(err)
		}
	}
	put("step.tool.end", map[string]any{"tool": "read"})
	put("step.tool.end", map[string]any{"tool": "bash", "isError": true})
	put("step.turn.completed", map[string]any{"wallMs": 1500, "usage": map[string]any{"inputTokens": 20, "outputTokens": 4},
		"stepCost": map[string]any{"state": "known", "usd": 0.25}})

	mem := &Memory{store: s}
	res, err := mem.ReadLoopSteps(protocol.LoopStepsParams{ID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.Totals == nil {
		t.Fatalf("one step and the loop's totals expected: %+v", res)
	}
	got := res.Steps[0]
	if got.Agent != "planner" || got.Status != "running" || got.Turns != 1 || got.ToolCalls != 2 || got.ToolErrors != 1 ||
		got.InputTokens != 20 || got.CostState != "known" || got.CostUSD == nil || *got.CostUSD != 0.25 {
		t.Errorf("the step's summary is wrong: %+v", got)
	}
	if res.Totals.ToolCalls != 2 || res.Totals.CostState != "known" {
		t.Errorf("the loop's totals are wrong: %+v", res.Totals)
	}
}
