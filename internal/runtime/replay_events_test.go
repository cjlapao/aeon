package runtime

// The replay fields (loop-replay step 2): what a reopened loop needs is on the stream, because the stream is the
// journal. A finished model turn carries its whole text and what it spent; a tool end carries how long the call took
// and its result content for every tool (capped); an approval says who gave it.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestATurnIsOnTheStreamWithItsTextAndRunningCost(t *testing.T) {
	client := newFakeClient(
		scriptedTurn{Text: "looking", ToolUses: []model.Block{toolUse("r1", "read", `{"path":"a.md"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	sink := &recordingSink{}
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read").with("read", ToolOutcome{Content: strings.Repeat("x", 40<<10), Summary: "read a.md"}),
		Sink: sink, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	var turns []StepEvent
	var end *StepEvent
	for _, e := range sink.all() {
		e := e
		switch {
		case e.Kind == EventTurnCompleted:
			turns = append(turns, e)
		case e.Kind == EventToolEnd && e.Tool == "read":
			end = &e
		}
	}
	if len(turns) != 2 {
		t.Fatalf("%d turn events, want one per model call (2)", len(turns))
	}
	if turns[0].Text != "looking" || turns[0].ToolCalls != 1 || turns[0].Usage == nil || turns[0].TurnCost == nil || turns[0].StepCost == nil {
		t.Fatalf("the first turn is thin: %+v", turns[0])
	}
	if turns[0].Iteration == turns[1].Iteration {
		t.Errorf("both turns claim iteration %d", turns[0].Iteration)
	}
	if end == nil || end.Detail == "" || !end.DetailTruncated || len(end.Detail) > streamDetailCap {
		t.Fatalf("the read result must ride the end event, capped and flagged: %+v", end)
	}
}

func TestAnApprovalSaysWhoGaveIt(t *testing.T) {
	gate := askGate(testAskSentence, "confirm-push", "agent:boss")
	spy := newSpyTools("bash").with("bash", ToolOutcome{Content: "ran"})
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "bash", `{"command":"`+testAskCommand+`"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest(toolSpec("bash"))
	req.HasOperator = true
	req.AutoApprove = &AutoApprove{All: true, By: "user:1"}
	sink := &recordingSink{}
	opts := runOpts(client, spy, sink)
	opts.Policy = gate
	RunStep(context.Background(), req, opts)

	var decided []StepEvent
	for _, e := range sink.all() {
		if e.Kind == EventPolicyDecided && e.Tool == "bash" {
			decided = append(decided, e)
		}
	}
	if len(decided) != 2 || decided[0].Verdict != "asked" || decided[0].ApprovedBy != "" || decided[1].Verdict != "allowed" || decided[1].ApprovedBy != "auto" {
		t.Fatalf("full access must be recorded as an automatic approval, after the ask: %+v", decided)
	}
}

// The loop's index rides the system half of every loop-bound step, and an empty one leaves the compile identical.
func TestTheLoopIndexRidesTheSystemHalf(t *testing.T) {
	in := skillsTestInput()
	in.LoopContext = "### Documents\n- plan.md · rev 2"
	in.TaskFrame = "Current blueprint lane: plan"
	res := compileWithSkills(t, in)
	system := res.Request.Messages[0].Content[0].Text
	user := res.Request.Messages[1].Content[0].Text
	if !strings.Contains(system, LoopContextTitle) || !strings.Contains(system, "plan.md · rev 2") {
		t.Fatalf("the index must reach the system half:\n%s", system)
	}
	if strings.Index(system, LoopContextTitle) > strings.Index(system, "Your task") {
		t.Error("the index must come before the task frame, which stays closest to the person's message")
	}
	if strings.Contains(user, "plan.md · rev 2") {
		t.Errorf("the person's half must stay the person's words alone:\n%s", user)
	}
	plain := compileWithSkills(t, skillsTestInput())
	in.LoopContext = ""
	in.TaskFrame = ""
	if compileWithSkills(t, in).Request.Messages[0].Content[0].Text != plain.Request.Messages[0].Content[0].Text {
		t.Error("an empty index changed the compile")
	}
}
