package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// Several questions in one call become ONE pending question: a numbered body and one form entry per question.
func TestQuestionFormIsOnePendingQuestion(t *testing.T) {
	use := model.Block{ID: "tu1", Input: []byte(`{"questions":[{"text":"Which gate?","options":["A","B"]},{"text":"Name?"}]}`)}
	q, err := pendingQuestion(use)
	if err != nil {
		t.Fatal(err)
	}
	if q.Kind != "clarify" || !strings.Contains(q.Body, "1. Which gate?") || !strings.Contains(q.Body, "2. Name?") {
		t.Fatalf("body/kind wrong: %+v", q)
	}
	if len(q.Options) != 2 || !strings.HasPrefix(q.Options[0], FormOptionPrefix) {
		t.Fatalf("options wrong: %v", q.Options)
	}
	if _, err := pendingQuestion(model.Block{Input: []byte(`{"questions":[{"text":"a"},{"text":" "}]}`)}); err == nil {
		t.Fatal("an empty sub-question must be refused")
	}
}

// A model that sends `questions` as a string of JSON, or as plain strings, is read as what it meant; one that sends
// nonsense is told how to fix it and asks again, and the step stops only after three bad calls in a row.
func TestQuestionArgumentsAreReadLeniently(t *testing.T) {
	use := func(in string) model.Block {
		return model.Block{Kind: model.BlockToolUse, ID: "q", Name: "question", Input: json.RawMessage(in)}
	}
	for name, in := range map[string]string{
		"string of json": `{"questions":"[{\"text\":\"Which db?\",\"options\":[\"pg\",\"sqlite\"]},{\"text\":\"Which port?\"}]"}`,
		"plain strings":  `{"questions":["Which db?","Which port?"]}`,
		"proper array":   `{"questions":[{"text":"Which db?"},{"text":"Which port?"}]}`,
	} {
		q, err := pendingQuestion(use(in))
		if err != nil || !strings.Contains(q.Body, "1. Which db?") || !strings.Contains(q.Body, "2. Which port?") || len(q.Options) != 2 {
			t.Fatalf("%s: %+v %v", name, q, err)
		}
	}
	if q, err := pendingQuestion(use(`{"questions":"Which db?"}`)); err != nil || q.Body != "Which db?" {
		t.Fatalf("a single string question: %+v %v", q, err)
	}
	if q, err := pendingQuestion(use(`{"text":"Pick","options":"[\"a\",\"b\"]"}`)); err != nil || len(q.Options) != 2 {
		t.Fatalf("options as a string of json: %+v %v", q, err)
	}
	if _, err := pendingQuestion(use(`{"questions":{"text":"x"}}`)); err == nil {
		t.Fatal("an object where an array belongs must say what to send")
	}
}

// A question the model sent badly comes back to it as a tool error and it asks again: the loop is not blocked by it.
func TestABadlyFormedQuestionIsRetriedNotBlocked(t *testing.T) {
	bad := model.Block{Kind: model.BlockToolUse, ID: "q1", Name: "question", Input: json.RawMessage(`{"questions":{"text":"x"}}`)}
	client := newFakeClient(scriptedTurn{ToolUses: []model.Block{bad}}, scriptedTurn{Text: okHandoff})
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: newSpyTools("question"), HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}})
	if res.Outcome != OutcomeOK {
		t.Fatalf("a bad question blocked the loop: %q %q", res.Outcome, res.Reason)
	}
	if seen := fmt.Sprintf("%+v", client.request(1).Messages); !strings.Contains(seen, "invalid question") {
		t.Fatalf("the model was not told what was wrong: %s", seen)
	}
}

// ...but a model that keeps sending it badly does stop the step, after three tries.
func TestBadQuestionsAreBounded(t *testing.T) {
	mk := func(id string) scriptedTurn {
		return scriptedTurn{ToolUses: []model.Block{{Kind: model.BlockToolUse, ID: id, Name: "question", Input: json.RawMessage(`{"questions":{"text":"x"}}`)}}}
	}
	client := newFakeClient(mk("a"), mk("b"), mk("c"), mk("d"), mk("e"))
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: newSpyTools("question"), HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}})
	if res.Outcome == OutcomeOK || !strings.Contains(res.Reason, "invalid question") {
		t.Fatalf("a model that never gets it right must stop the step: %q %q", res.Outcome, res.Reason)
	}
	if client.requestCount() != 4 {
		t.Fatalf("%d requests, want 4 (three corrections, then the fourth bad one stops it)", client.requestCount())
	}
}
