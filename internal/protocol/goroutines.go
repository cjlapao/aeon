package protocol

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
)

// goroutines.go is S14 T3's second debug method. The dump is the daemon's
// own — runtime.Stack over every goroutine in THIS process — because the
// claim being demonstrated is about the process that runs loops: with an
// async Task suspended there is no agentsvc frame anywhere in it.

func (t *Table) handleDebugGoroutines(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	dump := make([]byte, 1<<20)
	n := runtime.Stack(dump, true)
	stacks := string(dump[:n])
	count := 1
	if stacks != "" {
		count = strings.Count(stacks, "\ngoroutine ") + 1
	}
	return DebugGoroutinesResult{Goroutines: count, Stacks: stacks}, nil
}
