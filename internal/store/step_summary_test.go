package store

import "testing"

func summaryEvent(t *testing.T, s *Store, loop string, step int64, kind string, payload map[string]any) {
	t.Helper()
	if _, err := s.AppendEvent(NewEvent{LoopID: loop, LoopSlug: "slug", ComponentRoot: "/r", StepID: step, Kind: kind, Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

// Each step's turns, tool calls, errors, tokens and cost are kept as its events are written, one loop's steps are
// paged newest first, and the loop's totals say when some cost is unknown rather than counting it as zero.
func TestStepSummariesFollowTheEvents(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	summaryEvent(t, s, "L1", 1, "loop.step.dispatched", map[string]any{"lane": "discovery"})
	summaryEvent(t, s, "L1", 1, "step.turn.completed", map[string]any{"wallMs": 2000,
		"route": "qwen", "usage": map[string]any{"inputTokens": 100, "cacheReadTokens": 20, "outputTokens": 10, "reasoningTokens": 4}, "stepCost": map[string]any{"state": "known", "usd": 0.5}})
	summaryEvent(t, s, "L1", 1, "step.tool.end", map[string]any{"isError": false})
	summaryEvent(t, s, "L1", 1, "step.tool.end", map[string]any{"isError": true})
	summaryEvent(t, s, "L1", 2, "step.turn.completed", map[string]any{"wallMs": 1000,
		"usage": map[string]any{"inputTokens": 50, "outputTokens": 5}, "stepCost": map[string]any{"state": "unknown"}})
	summaryEvent(t, s, "L2", 9, "step.turn.completed", map[string]any{"wallMs": 1}) // another loop, never mixed in

	win, err := s.ReadStepSummaries("L1", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(win.Steps) != 1 || win.Steps[0].StepID != 2 || !win.HasOlder {
		t.Fatalf("the newest page is step 2 with older behind it: %+v", win)
	}
	older, _ := s.ReadStepSummaries("L1", 2, 10)
	if len(older.Steps) != 1 || older.HasOlder {
		t.Fatalf("paging before step 2 gives step 1 and nothing older: %+v", older)
	}
	one := older.Steps[0]
	if one.Lane != "discovery" || one.Turns != 1 || one.ToolCalls != 2 || one.ToolErrors != 1 ||
		one.InputTokens != 100 || one.OutputTokens != 10 || one.ReasoningTokens != 4 || one.WallMs != 2000 ||
		one.ContextTokens != 120 || one.Route != "qwen" || one.CostState != "known" || one.CostUSD == nil || *one.CostUSD != 0.5 {
		t.Fatalf("step 1's summary is wrong: %+v", one)
	}
	tot, err := s.ReadLoopTotals("L1")
	if err != nil {
		t.Fatal(err)
	}
	if tot.Steps != 2 || tot.Turns != 2 || tot.ToolCalls != 2 || tot.InputTokens != 150 || tot.CostState != "partial" {
		t.Fatalf("the loop's totals are wrong (one step's cost is unknown, so partial): %+v", tot)
	}
	// One step's own events, not the loop's.
	ev, _ := s.ReadStepEvents("L1", 1, 0, 0, 50, nil, nil)
	for _, e := range ev.Events {
		if e.StepID != 1 {
			t.Fatalf("a step window returned step %d's event", e.StepID)
		}
	}
	if len(ev.Events) != 4 {
		t.Fatalf("step 1 has 4 events, got %d", len(ev.Events))
	}
}
