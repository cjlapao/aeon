package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// `loop.options` — what the person chose for the loop's model after it started: thinking on/off and the reasoning
// effort. The choice is the loop's, not one step's: every model step that STARTS after it carries it (a step already
// running keeps what it began with, as a steer's route and effort are the only thing that moves a running step).
// Empty values clear the choice, so the route's own settings apply again.

const MethodLoopOptions MethodName = "loop.options"

// LoopOptionsParams names the loop and the options. Both empty clears them.
type LoopOptionsParams struct {
	ID              string `json:"id"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
}

// LoopOptionsResult is what is now in force for the loop.
type LoopOptionsResult struct {
	ID              string `json:"id"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
}

func (t *Table) handleLoopOptions(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopOptionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.options needs the loop's id")}
	}
	p.ReasoningEffort = strings.TrimSpace(p.ReasoningEffort)
	if p.Thinking != "" && p.Thinking != "on" && p.Thinking != "off" {
		return nil, ErrBadParams{Err: fmt.Errorf("thinking %q is not one of on, off", p.Thinking)}
	}
	if p.ReasoningEffort != "" && !model.ValidReasoningEffort(p.ReasoningEffort) {
		return nil, ErrBadParams{Err: fmt.Errorf("reasoning effort %q is not one of %s", p.ReasoningEffort, strings.Join(model.ReasoningEfforts(), ", "))}
	}
	if t.SetLoopOptions == nil {
		return nil, ErrStoreUnavailable
	}
	return t.SetLoopOptions(ctx, p)
}
