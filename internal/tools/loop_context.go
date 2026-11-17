package tools

// loop_context.go — the `loop_context` tool: what the loop has, as one index.
//
// A loop's files do not live on disk. Its documents (prompt.md, discovery.md, plan.md, answers.md, tasks/T1.md …),
// its tasks, the answers the person gave and the tools the agent holds are all in the database, and an agent that
// does not know that looks for plan.md in the workspace, finds nothing and reports "no plan". The daemon puts this
// index at the top of every loop step's context; this tool returns the same index on demand, fresh, because a
// document written a minute ago is not in the text the step started with.

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/model"
)

// LoopContextSource renders the loop's index for the step on the context.
type LoopContextSource interface {
	// Render returns the index text, or an error that says why the loop has none.
	Render(ctx context.Context) (string, error)
}

// LoopContext is the tool.
func LoopContext() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name:        "loop_context",
			Description: "Show everything this loop has right now: its documents in the database (name, revision, size, who wrote it), its tasks with their status and task spec files, the questions the person answered, and the tools you hold. The loop's files are in the database, not on disk: read one with doc_read. Call this when you are unsure whether a document exists or what it is called.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			if err := decodeDocInput(input, &struct{}{}); err != nil {
				return Errorf("%s", err)
			}
			if tc.LoopContext == nil {
				return Errorf("the loop's context is unavailable in this runtime: this daemon has no store to read it from")
			}
			text, err := tc.LoopContext.Render(ctx)
			if err != nil {
				return Errorf("%s", err)
			}
			return Result{Content: text, Summary: "loop_context"}
		},
	}
}
