package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T3 — per-turn capture at the one model-call site.

func iptr(v int) *int { return &v }

func turnUsage(in, out int) model.Usage {
	return model.Usage{InputTokens: iptr(in), OutputTokens: iptr(out)}
}

func TestPerTurnCaptureFiresOncePerModelCall(t *testing.T) {
	// Two model calls (a tool round-trip is the cheapest way to make a real
	// second iteration happen), and each scripted turn's own accounting must
	// land on exactly one row: not the wrong iteration's numbers, not twice,
	// not zero.
	rec := &telemetryRecorder{}
	client := newFakeClient(
		scriptedTurn{
			ToolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)},
			Usage:    turnUsage(100, 20),
			Cost:     model.Cost{State: model.CostKnown, USD: 0.001},
		},
		scriptedTurn{
			Text:  okHandoff,
			Usage: turnUsage(130, 40),
			Cost:  model.Cost{State: model.CostPartial, USDAtLeast: 0.002},
		},
	)
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	st.Request.StepID = 77
	st.Request.Route.ID = "route-alpha"
	deps := LoopDeps{
		Client: client, Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry:   rec,
	}
	res := RunLoop(context.Background(), st, deps)
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	// One row per MODEL CALL among the assistant_output rows — the tool
	// append adds its own tool_result rows (T4), and the count claim is
	// about calls, not about every transcript row.
	var asst []recordedTelemetryTurn
	for _, row := range rec.turns {
		if row.category == CategoryAssistantOutput {
			asst = append(asst, row)
		}
	}
	if len(asst) != 2 {
		t.Fatalf("%d assistant_output rows for 2 model calls, want exactly one per call: %+v", len(asst), rec.turns)
	}
	rec.turns = asst
	want := []struct {
		usage model.Usage
		cost  model.Cost
	}{
		{turnUsage(100, 20), model.Cost{State: model.CostKnown, USD: 0.001}},
		{turnUsage(130, 40), model.Cost{State: model.CostPartial, USDAtLeast: 0.002}},
	}
	for i, w := range want {
		row := rec.turns[i]
		// The loop's iteration is ONE-BASED (the same number `model.delta`
		// events carry), so the row matches what a watcher saw streamed —
		// asserted explicitly because "matching iteration" is the claim,
		// and pinning the base is what keeps the claim checkable.
		if row.stepID != 77 || row.iteration != i+1 {
			t.Errorf("row %d: step %d iteration %d, want 77/%d", i, row.stepID, row.iteration, i+1)
		}
		if !reflect.DeepEqual(row.usage, w.usage) || !reflect.DeepEqual(row.cost, w.cost) {
			t.Errorf("row %d accounting drifted from the call: %+v/%+v want %+v/%+v", i, row.usage, row.cost, w.usage, w.cost)
		}
		if row.category != CategoryAssistantOutput {
			t.Errorf("row %d category %q, want assistant_output", i, row.category)
		}
		if row.route != "route-alpha" {
			t.Errorf("row %d route %q, want the step's route id", i, row.route)
		}
		if row.wallMs < 0 {
			t.Errorf("row %d wall %dms is not a measurement", i, row.wallMs)
		}
	}
}

func TestCaptureIsNilSafe(t *testing.T) {
	// I2 on THIS call: the same scripted step with Telemetry nil must return
	// the identical result — outcome, reason, transcript, usage, cost — and
	// the positive control (same script WITH a sink) must capture, or the
	// nil test proves nothing.
	script := func() []scriptedTurn {
		return []scriptedTurn{
			{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)}, Usage: turnUsage(100, 20)},
			{Text: okHandoff, Usage: turnUsage(130, 40)},
		}
	}
	st1 := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	resNil := RunLoop(context.Background(), st1, LoopDeps{
		Client: newFakeClient(script()...), Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	rec := &telemetryRecorder{}
	st2 := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	resRec := RunLoop(context.Background(), st2, LoopDeps{
		Client: newFakeClient(script()...), Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry:   rec,
	})
	if resNil.Outcome != resRec.Outcome || resNil.Reason != resRec.Reason {
		t.Fatalf("nil vs wired changed the outcome: %+v vs %+v", resNil, resRec)
	}
	if !reflect.DeepEqual(resNil.Messages, resRec.Messages) {
		t.Error("nil vs wired changed the transcript")
	}
	if !reflect.DeepEqual(resNil.Usage, resRec.Usage) || !reflect.DeepEqual(resNil.Cost, resRec.Cost) {
		t.Error("nil vs wired changed the accounting")
	}
	assistant := 0
	for _, row := range rec.turns {
		if row.category == CategoryAssistantOutput {
			assistant++
		}
	}
	if assistant != 2 {
		t.Fatalf("positive control: %d assistant rows captured, want 2 — the nil test above was vacuous", assistant)
	}
}

func TestCaptureSurvivesAFailedCall(t *testing.T) {
	// M15's rule, inherited not re-implemented: the call that failed
	// part-way still spent what it spent, and the row records what it spent.
	// The capture sits on the success side of NO branch — same line, same
	// accounting, before the error return.
	rec := &telemetryRecorder{}
	client := newFakeClient(scriptedTurn{
		Text:  "half an answer, then the provider died",
		Usage: turnUsage(900, 60),
		Cost:  model.Cost{State: model.CostKnown, USD: 0.004},
		Err:   context.DeadlineExceeded,
	})
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry: rec,
	})
	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome %s, want failed", res.Outcome)
	}
	if len(rec.turns) != 1 {
		t.Fatalf("%d rows for the failed call, want its one row", len(rec.turns))
	}
	row := rec.turns[0]
	if !reflect.DeepEqual(row.usage, turnUsage(900, 60)) || row.cost.USD != 0.004 {
		t.Errorf("the failed call's real spend was not recorded: %+v %+v", row.usage, row.cost)
	}
}

func TestCaptureFailureIsSpokenNotFatal(t *testing.T) {
	// The other half of "observed, not transactional": a sink that refuses
	// to write changes nothing about the step AND says so in the event
	// stream — a hole someone can see.
	rec := &telemetryRecorder{failTurns: true}
	client := newFakeClient(scriptedTurn{Text: okHandoff, Usage: turnUsage(10, 5)})
	st := testState(testHandler(), nil)
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry: rec,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("a refusing sink failed the step: %s", res.Reason)
	}
	if len(rec.turns) != 0 {
		t.Fatal("the recorder recorded despite failing")
	}
	spoken := false
	for _, e := range res.Events {
		if e.Kind == EventTelemetryError && e.IsError {
			spoken = true
		}
	}
	if !spoken {
		t.Fatal("the missing row was silent — an aggregate that reads complete while a turn is missing is the failure this event exists to prevent")
	}
}
