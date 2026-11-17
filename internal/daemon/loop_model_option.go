package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
)

// applyLoopModelOptions lets what a person set on a running loop (loop.options) win over what was pinned at start,
// for the step about to be dispatched. A loop nobody changed keeps its pin untouched.
func (d *Daemon) applyLoopModelOptions(ctx context.Context, loopID string, p *protocol.StepParams) {
	if d.store == nil {
		return
	}
	o, present, err := d.store.LoopModelOptionGet(ctx, loopID)
	if err != nil || !present {
		return
	}
	if o.ReasoningEffort != "" {
		p.ReasoningEffort = o.ReasoningEffort
	}
	if o.Thinking != "" {
		p.Thinking = o.Thinking
	}
}

// setLoopOptions is `loop.options`: it records the person's choice, and says what is in force now.
func (d *Daemon) setLoopOptions(ctx context.Context, p protocol.LoopOptionsParams) (protocol.LoopOptionsResult, error) {
	if _, err := d.store.ReadLoopByID(p.ID); err != nil {
		return protocol.LoopOptionsResult{}, protocol.ErrLoopControlRefused.WithWhat("loop %s is not known", p.ID)
	}
	if err := d.store.LoopModelOptionSet(ctx, p.ID, p.ReasoningEffort, p.Thinking); err != nil {
		return protocol.LoopOptionsResult{}, err
	}
	return protocol.LoopOptionsResult{ID: p.ID, ReasoningEffort: p.ReasoningEffort, Thinking: p.Thinking}, nil
}
