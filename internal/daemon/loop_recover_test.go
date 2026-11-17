package daemon

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// A step that stopped on a tool call that never got a result has that call answered ("not completed") before the
// recovery note is added, so the transcript is valid and the model knows the call did not happen.
func TestRecoveryAnswersToolCallsThatNeverGotAResult(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("start")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.ToolUseBlock("q1", "question", []byte(`{"questions":"x"}`))}},
	}
	out := appendResumeUserText(closeDanglingToolUses(msgs, "invalid question"), "try again")
	if len(out) != 3 || out[2].Role != model.RoleUser {
		t.Fatalf("want the dangling call closed by one user message, got %d messages", len(out))
	}
	if len(out[2].Content) != 2 || out[2].Content[0].Kind != model.BlockToolResult {
		t.Fatalf("the user message must carry the tool result and then the note: %+v", out[2].Content)
	}
	if v := model.ValidateTranscript(out); !v.OK {
		t.Fatalf("the recovered transcript is not valid: %s", v.Reason)
	}
	// A transcript that already ends in a user turn is left alone.
	done := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}}
	if got := closeDanglingToolUses(done, "x"); len(got) != 1 {
		t.Fatal("nothing dangling: nothing to add")
	}
}
