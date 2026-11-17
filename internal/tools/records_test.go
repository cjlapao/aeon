package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestToolsetCallPassesCtxToRun(t *testing.T) {
	// The stepctx stamping in runtime is only half the promise; this pins the
	// other half — Toolset.Call must hand the CALLER'S context to Run, or
	// every ctx-carried identity dies at the toolset boundary.
	type markKey struct{}
	var seen string
	// Built by hand, assembled exactly as BuildToolset assembles it.
	ts := Toolset{byName: map[string]Tool{}, tc: ToolContext{}}
	ts.byName["ctxprobe"] = Tool{
		Spec: model.ToolSpec{Name: "ctxprobe", Description: "reads the context back"},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			seen, _ = ctx.Value(markKey{}).(string)
			return Result{Content: "ran"}
		},
	}
	res := ts.Call(context.WithValue(context.Background(), markKey{}, "carried"), "ctxprobe", json.RawMessage(`{}`))
	if res.IsError {
		t.Fatalf("call errored: %s", res.Content)
	}
	if seen != "carried" {
		t.Fatalf("the tool saw %q — Toolset.Call dropped the caller's context", seen)
	}
}

func TestRecordsAreUnavailableWhenUnwired(t *testing.T) {
	// Fail closed: a storeless runtime says the door is shut; it never answers
	// "no goals" from an injected nothing.
	call := recordsOrFail(ToolContext{})
	content, isError := call(context.Background(), "goal.list", json.RawMessage(`{}`))
	if !isError || !strings.Contains(content, "records are unavailable") {
		t.Fatalf("nil Records answered %q isError=%v — fail-OPEN", content, isError)
	}
	// Wired, it delegates exactly.
	var gotTool string
	call = recordsOrFail(ToolContext{Records: func(_ context.Context, tool string, _ json.RawMessage) (string, bool) {
		gotTool = tool
		return "two goals", false
	}})
	content, isError = call(context.Background(), "goal.list", nil)
	if isError || content != "two goals" || gotTool != "goal.list" {
		t.Fatalf("wired invoker: %q %v %q", content, isError, gotTool)
	}
}
