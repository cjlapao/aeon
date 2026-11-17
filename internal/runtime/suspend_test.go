package runtime

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestQuestionSuspendsWithoutExecuting(t *testing.T) {
	spy := newSpyTools("question")
	spy.panicOn = "question"
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("q1", "question", `{"text":"Choose","options":["a","b"],"kind":"gate"}`)}}),
		Tools:  spy,
	})
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %s, reason %s", res.Kind, res.Reason)
	}
	if len(spy.recorded()) != 0 {
		t.Fatal("question reached the toolset")
	}
	if res.Question == nil || res.Question.ToolUseID != "q1" || res.Question.Body != "Choose" || res.Question.Kind != "gate" || len(res.Question.Options) != 2 {
		t.Fatalf("question: %+v", res.Question)
	}
	if res.Manifest.IterationsUsed != 1 {
		t.Fatalf("iterations = %d", res.Manifest.IterationsUsed)
	}
	if len(res.Messages) == 0 {
		t.Fatal("transcript missing")
	}
	if validation := model.ValidateTranscript(res.Messages); !validation.OK {
		t.Fatalf("suspended transcript: %s", validation.Reason)
	}
}

func TestSiblingResultIsRecordedBeforeQuestionSuspends(t *testing.T) {
	spy := newSpyTools("read", "question")
	spy.panicOn = "question"
	st := testState(testHandler("read", "question"), []model.ToolSpec{toolSpec("read"), toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{
			toolUse("r1", "read", `{"path":"a.md"}`),
			toolUse("q1", "question", `{"text":"Proceed?"}`),
		}}), Tools: spy,
	})
	if res.Kind != ResultSuspended {
		t.Fatalf("kind %s, reason %s", res.Kind, res.Reason)
	}
	if calls := spy.recorded(); len(calls) != 1 || calls[0].Name != "read" {
		t.Fatalf("tools called: %+v", calls)
	}
	var uses, results int
	for _, message := range res.Messages {
		for _, block := range message.Content {
			if block.Kind == model.BlockToolUse {
				uses++
			}
			if block.Kind == model.BlockToolResult {
				results++
				if block.ToolUseID != "r1" {
					t.Errorf("result answered %q", block.ToolUseID)
				}
			}
		}
	}
	if uses != 2 || results != 1 {
		t.Fatalf("uses=%d results=%d", uses, results)
	}
	if validation := model.ValidateTranscript(res.Messages); !validation.OK {
		t.Fatalf("suspended transcript: %s", validation.Reason)
	}
}

func TestTwoQuestionsInOneTurnFail(t *testing.T) {
	spy := newSpyTools("question")
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("q1", "question", `{"text":"One?"}`), toolUse("q2", "question", `{"text":"Two?"}`),
	}}), Tools: spy})
	if res.Outcome != OutcomeFailed || !strings.Contains(res.Reason, "2 questions") {
		t.Fatalf("outcome=%s reason=%s", res.Outcome, res.Reason)
	}
	if len(spy.recorded()) != 0 {
		t.Fatal("a question was executed")
	}
}

func TestThereIsStillOneRunLoop(t *testing.T) {
	raw, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatal(err)
	}
	if count := len(regexp.MustCompile(`func RunLoop\(`).FindAll(raw, -1)); count != 1 {
		t.Fatalf("RunLoop declarations: %d", count)
	}
}
