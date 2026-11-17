package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeIndex struct{ text string }

func (f fakeIndex) Render(context.Context) (string, error) { return f.text, nil }

func TestLoopContextToolReturnsTheIndexAndSaysWhenThereIsNone(t *testing.T) {
	tool := LoopContext()
	if tool.Class != RerunSafe {
		t.Errorf("class %v: re-reading an index must be safe", tool.Class)
	}
	res := tool.Run(context.Background(), ToolContext{LoopContext: fakeIndex{"### Documents\n- plan.md"}}, json.RawMessage(`{}`))
	if res.IsError || !strings.Contains(res.Content, "plan.md") {
		t.Fatalf("the index was not returned: %+v", res)
	}
	none := tool.Run(context.Background(), ToolContext{}, json.RawMessage(`{}`))
	if !none.IsError || !strings.Contains(none.Content, "unavailable") {
		t.Errorf("a runtime with no store must say so, not return an empty index: %+v", none)
	}
}
