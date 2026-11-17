package tools

// records_tools.go — the catalog projected (S38 T9). Each management action
// in internal/records becomes exactly one Tool, mechanically: the spec's
// name, description and schema pass through UNTOUCHED (the tools package
// invents nothing the catalog did not say, and omits nothing it did), and
// Run delegates whole to the daemon-injected Records invoker — which stamps
// identity from the step context and answers refusals as error results.
//
// The projection is a loop over Catalog(), never a hand-written map, because
// a hand-written copy is a second catalog that somebody forgets to update.

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/records"
)

func recordTools() map[string]Tool {
	out := map[string]Tool{}
	for _, sp := range records.Catalog() {
		sp := sp
		out[sp.Name] = Tool{
			Class: RerunClass(sp.Class),
			Spec:  model.ToolSpec{Name: sp.Name, Description: sp.Description, Parameters: sp.Schema},
			Run: func(ctx context.Context, tc ToolContext, in json.RawMessage) Result {
				content, isErr := recordsOrFail(tc)(ctx, sp.Name, in)
				return Result{Content: content, IsError: isErr, Summary: sp.Name}
			},
		}
	}
	return out
}
