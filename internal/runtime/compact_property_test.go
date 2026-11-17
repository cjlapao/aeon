package runtime

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestGeneratedCompactionRangesPreserveHeadThinkingAndToolPairs(t *testing.T) {
	// 500 deterministic transcripts vary middle turn count, text lengths,
	// thinking signatures, and tool pair distances. The seed makes a failure
	// reproducible while the corpus explores combinations hand-written cases miss.
	rng := rand.New(rand.NewSource(604))
	const corpus = 500
	for caseNo := 0; caseNo < corpus; caseNo++ {
		messages := []model.Message{
			{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system instructions")}},
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("task and frozen context")}},
		}
		pairs := 1 + rng.Intn(7)
		for i := 0; i < pairs; i++ {
			id := fmt.Sprintf("case-%d-call-%d", caseNo, i)
			text := strings.Repeat("x", 1+rng.Intn(50))
			assistant := model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(text), model.ToolUseBlock(id, "read", []byte(`{"path":"a"}`))}}
			if rng.Intn(2) == 0 {
				assistant.Content = append([]model.Block{model.ThinkingBlock("thought", fmt.Sprintf("sig-%d", i))}, assistant.Content...)
			}
			messages = append(messages, assistant)
			messages = append(messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock(id, text, false)}})
		}
		last := model.Message{Role: model.RoleAssistant, Content: []model.Block{model.ThinkingBlock("final thought", "final signature"), model.TextBlock("final")}}
		messages = append(messages, last)
		selected := SelectRange(messages, 100000)
		if !selected.OK {
			t.Fatalf("case %d: %s", caseNo, selected.Reason)
		}
		if selected.Start < 2 || selected.End > len(messages)-1 {
			t.Errorf("case %d selected head or last assistant: %+v", caseNo, selected)
		}
		if !closedToolPairs(messages, selected.Start, selected.End) {
			t.Errorf("case %d orphaned a tool pair", caseNo)
		}
		if messages[len(messages)-1].Content[0].Signature != "final signature" {
			t.Errorf("case %d altered last thinking", caseNo)
		}
	}
}
