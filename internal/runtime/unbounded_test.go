package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// A MaxIterations of 0 is "no bound": a loop runs until it is done.

func TestAZeroBoundRunsPastTheOldTwelve(t *testing.T) {
	turns := make([]scriptedTurn, 0, 21)
	for i := 0; i < 20; i++ {
		turns = append(turns, scriptedTurn{ToolUses: []model.Block{toolUse("t"+string(rune('a'+i)), "read", `{"path":"a.md"}`)}})
	}
	turns = append(turns, scriptedTurn{Text: okHandoff})
	st := testState(testHandler(), []model.ToolSpec{{Name: "read"}})
	st.Manifest.MaxIterations = 0
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(turns...), Tools: newSpyTools("read"), Sink: &recordingSink{},
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, Checkpoint: &recordingWriter{},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("an unbounded step must finish its work: %q %q", res.Outcome, res.Reason)
	}
}

func TestABoundStillBlocksAndNamesItself(t *testing.T) {
	turns := make([]scriptedTurn, 0, 6)
	for i := 0; i < 6; i++ {
		turns = append(turns, scriptedTurn{ToolUses: []model.Block{toolUse("t"+string(rune('a'+i)), "read", `{"path":"a.md"}`)}})
	}
	st := testState(testHandler(), []model.ToolSpec{{Name: "read"}})
	st.Manifest.MaxIterations = 3
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(turns...), Tools: newSpyTools("read"), Sink: &recordingSink{},
		HandoffSpec: HandoffSpec{Required: []string{"status"}}, Checkpoint: &recordingWriter{},
	})
	if res.Outcome != OutcomeBlocked || res.Reason != "exceeded MaxIterations (3)" {
		t.Fatalf("a set bound must still block, naming itself: %q %q", res.Outcome, res.Reason)
	}
}

func TestTheCompileCarriesAnExplicitNoBoundAsZero(t *testing.T) {
	in := skillsTestInput()
	in.MaxIterations = -1
	res := compileWithSkills(t, in)
	if res.Request.Manifest.MaxIterations != 0 {
		t.Fatalf("an explicit no-bound must be 0 in the manifest, got %d", res.Request.Manifest.MaxIterations)
	}
	in.MaxIterations = 7
	if got := compileWithSkills(t, in).Request.Manifest.MaxIterations; got != 7 {
		t.Fatalf("a positive bound is the bound, got %d", got)
	}
}

func TestAnAnsweredStepGetsItsWorkingWindowAgainButARecoveredOneDoesNot(t *testing.T) {
	// Waiting on a person is not working time (a question may sit for days), so a
	// resume from a human answer restarts the window; a crash recovery keeps the
	// deadline it had.
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	run := func(recovery bool) StepResult {
		req, _ := suspendedFixture(t)
		// The original window ended two days ago.
		req.Manifest.DeadlineAt = now.Add(-48 * time.Hour)
		req.Manifest.DeadlineWindowMs = (2 * time.Hour).Milliseconds()
		req.Recovery = recovery
		return ResumeStep(context.Background(), req, RunOpts{
			Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools("read", "question"),
			Clock:       func() time.Time { return now },
			HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
		})
	}
	answered := run(false)
	if answered.Outcome != OutcomeOK {
		t.Fatalf("a step answered after two days must still run: %q %q", answered.Outcome, answered.Reason)
	}
	if want := now.Add(2 * time.Hour); !answered.Manifest.DeadlineAt.Equal(want) {
		t.Fatalf("the working window must restart at the resume: got %s, want %s", answered.Manifest.DeadlineAt, want)
	}
}
