package runtime

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T5 — the compaction capture at its one append point.

func TestCompactionCaptureIsNilSafe(t *testing.T) {
	// Same scripted compaction with Telemetry nil and with a recorder: the
	// compaction must behave identically (blob byte-identical — that is the
	// Do-NOT), and the recorder must actually have seen the record or the
	// nil test is vacuous.
	script := func() []scriptedTurn {
		return []scriptedTurn{
			{Text: "The earlier exchange established the task.",
				Usage: model.Usage{InputTokens: model.IntPtr(80), OutputTokens: model.IntPtr(12)},
				Cost:  model.Cost{State: model.CostKnown, USD: 0.002}},
			{Text: okHandoff,
				Usage: model.Usage{InputTokens: model.IntPtr(25), OutputTokens: model.IntPtr(10)},
				Cost:  model.Cost{State: model.CostKnown, USD: 0.003}},
		}
	}
	stNil := compactFixture()
	resNil := RunLoop(context.Background(), stNil, LoopDeps{
		Client: newFakeClient(script()...), Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	rec := &telemetryRecorder{}
	stRec := compactFixture()
	resRec := RunLoop(context.Background(), stRec, LoopDeps{
		Client: newFakeClient(script()...), Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
		Telemetry:   rec,
	})
	if resNil.Outcome != OutcomeOK || resRec.Outcome != OutcomeOK {
		t.Fatalf("outcomes nil=%s rec=%s", resNil.Outcome, resRec.Outcome)
	}
	if len(resNil.Compactions) != 1 || len(resRec.Compactions) != 1 {
		t.Fatalf("compactions nil=%d rec=%d — the blob must be unaffected by the seam", len(resNil.Compactions), len(resRec.Compactions))
	}
	// The blob is byte-identical across the two: same record values.
	if resNil.Compactions[0].Summary != resRec.Compactions[0].Summary ||
		resNil.Compactions[0].Start != resRec.Compactions[0].Start ||
		resNil.Compactions[0].End != resRec.Compactions[0].End ||
		resNil.Compactions[0].EstimateBefore != resRec.Compactions[0].EstimateBefore {
		t.Fatal("the blob drifted between the nil and wired runs — the seam changed what it observes")
	}
	if rec.compactions != 1 {
		t.Fatalf("recorder saw %d compactions, want 1 — the nil test above was vacuous", rec.compactions)
	}
}
