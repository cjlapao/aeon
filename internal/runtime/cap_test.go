package runtime

// T7's in-step check (E2), at the runtime's own boundary: the daemon hands a
// verdict in as plain data, and the loop decides — a hit is a suspension (NOT
// a blocked outcome, OD6), and an unreadable or refusing cap fails the step
// closed. The daemon test (budget_seam_test.go) proves the same flow with
// real rows; this one proves the runtime's contract without a store.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestCapHitSuspendsBeforeTheNextCall(t *testing.T) {
	spy := newSpyTools("read")
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})

	// Turn 0 is a tool call, so the loop reaches a SECOND boundary; turn 1
	// (the completion) must never be consumed — the cap fires before it.
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("r1", "read", `{"path":"a.md"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	hit := &CapHit{
		BudgetID: "bud-1", Scope: "loop", Subject: "loop/one", Dimension: "usd",
		SpendFloor: 12, Cap: 10, Reason: "budget bud-1 on loop one is exhausted (usd 12 of 10)", Exact: true,
	}
	checks := 0
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client,
		Tools:  spy,
		Budget: func(ctx context.Context) (BudgetVerdict, error) {
			checks++
			if checks == 1 {
				return BudgetVerdict{OK: true}, nil // first boundary: spend is still zero
			}
			return BudgetVerdict{OK: false, Reason: hit.Reason, CapHit: hit}, nil
		},
	})
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %s outcome %s, want suspended", res.Kind, res.Outcome)
	}
	if res.Question != nil {
		t.Fatalf("a cap suspension carries a question: %+v — a cap has no one to ask", res.Question)
	}
	if res.PauseReason != "cap" {
		t.Fatalf("pauseReason = %q, want \"cap\"", res.PauseReason)
	}
	if res.CapHit == nil || res.CapHit.BudgetID != "bud-1" || res.CapHit.Dimension != "usd" ||
		res.CapHit.SpendFloor != 12 || res.CapHit.Cap != 10 || !res.CapHit.Exact {
		t.Fatalf("capHit = %+v, want the hit's raw numbers", res.CapHit)
	}
	if res.Reason != hit.Reason {
		t.Fatalf("reason = %q, want the verdict's sentence", res.Reason)
	}
	if client.requestCount() != 1 {
		t.Fatalf("the model was asked %d times — the hit must fire BEFORE the second call", client.requestCount())
	}
	if checks != 2 {
		t.Fatalf("the seam was asked %d times, want 2 (once per boundary)", checks)
	}
	if validation := model.ValidateTranscript(res.Messages); !validation.OK {
		t.Fatalf("suspended transcript: %s", validation.Reason)
	}
}

func TestBudgetSeamErrorFailsTheStepClosed(t *testing.T) {
	spy := newSpyTools("read")
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client,
		Tools:  spy,
		Budget: func(ctx context.Context) (BudgetVerdict, error) {
			return BudgetVerdict{}, errors.New("the spend row is unreadable")
		},
	})
	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s, want failed: a cap the engine could not read is not a cap the step spends past", res.Outcome)
	}
	if !strings.Contains(res.Reason, "the budget check could not run") || !strings.Contains(res.Reason, "the spend row is unreadable") {
		t.Fatalf("reason = %q, want the seam's error named", res.Reason)
	}
	if client.requestCount() != 0 {
		t.Fatalf("the model was asked %d times — the unreadable cap must stop before any spend", client.requestCount())
	}
}

func TestBudgetRefusalWithoutAHitFailsClosed(t *testing.T) {
	spy := newSpyTools("read")
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client,
		Tools:  spy,
		Budget: func(ctx context.Context) (BudgetVerdict, error) {
			return BudgetVerdict{OK: false, Reason: "refused: the budget table is locked"}, nil
		},
	})
	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s, want failed: a refusal with no hit is not a suspension", res.Outcome)
	}
	if res.Kind == ResultSuspended || res.Question != nil || res.PauseReason != "" {
		t.Fatalf("kind %s pause %q — a refusal is not the cap shape", res.Kind, res.PauseReason)
	}
	if client.requestCount() != 0 {
		t.Fatalf("the model was asked %d times", client.requestCount())
	}
}
