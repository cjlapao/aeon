// The durable side, bound: plugin defines the StateStore contract (state.go)
// and this adapter is its one production implementation over
// internal/store's rows (migration 0023). The daemon wires it (T7); tests
// bind it directly to prove the two halves agree — a fake alone could pass
// while the real rows disagreed.
package plugin

import (
	"errors"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// StoreBinding adapts *store.Store to StateStore. Direction matters: plugin
// imports store (never the reverse — store must not learn what a plugin is),
// and the adapter is where domain Refusals are made from the store's
// distinguishable errors (DB19).
type StoreBinding struct {
	S *store.Store
}

var _ StateStore = (*StoreBinding)(nil)

func (b *StoreBinding) CreateRecord(name, source string, at time.Time) *Reject {
	err := b.S.CreatePluginAgent(name, source, at.Format(time.RFC3339Nano))
	if errors.Is(err, store.ErrPluginRecordExists) {
		return refuse("name", "refused: %s already has a live record · remedy: revoke it first — history is not overwritten (AP4: the old life's rows stay readable underneath whatever comes next)", name)
	}
	if err != nil {
		return refuse("name", "refused: recording %s: %v", name, err)
	}
	return nil
}

func (b *StoreBinding) SetTransition(t TransitionRecord) *Reject {
	from := string(t.From)
	if t.From == StateNotInstalled {
		from = "" // the landing; the database's CHECK admits '' only for (none) → disabled
	}
	if err := b.S.TransitionPluginAgent(t.Agent, from, string(t.To), t.Reason, t.At.Format(time.RFC3339Nano)); err != nil {
		return refuse("state", "refused: recording %s %s → %s: %v", t.Agent, t.From, t.To, err)
	}
	return nil
}

func (b *StoreBinding) StateOf(name string) (State, bool) {
	a, found, err := b.S.PluginAgentState(name)
	if err != nil || !found {
		return StateNotInstalled, false
	}
	return State(a.State), true
}

func (b *StoreBinding) RecordRefusal(r RefusalRecord) *Reject {
	var taskID *int64
	if r.TaskID != 0 {
		taskID = &r.TaskID
	}
	if err := b.S.CreatePluginRefusal(r.Agent, r.RevokedAt.Format(time.RFC3339Nano), r.Reason, r.CostJSON, r.CostKnown, taskID); err != nil {
		return refuse("refusal", "refused: recording the arrival refusal for %s: %v", r.Agent, err)
	}
	return nil
}

func (b *StoreBinding) RefusalsOf(agent string) ([]RefusalRecord, *Reject) {
	rows, err := b.S.PluginRefusals(agent)
	if err != nil {
		return nil, refuse("audit", "refused: reading %s's refusals: %v · UNAVAILABLE is not none", agent, err)
	}
	out := make([]RefusalRecord, 0, len(rows))
	for _, r := range rows {
		at, err := time.Parse(time.RFC3339Nano, r.RevokedAt)
		if err != nil {
			at = time.Time{}
		}
		out = append(out, RefusalRecord{Agent: r.Agent, TaskID: deref(r.TaskID), RevokedAt: at, Reason: r.Reason, CostJSON: r.CostJSON, CostKnown: r.CostKnown})
	}
	return out, nil
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (b *StoreBinding) Transitions(name string) ([]TransitionRecord, *Reject) {
	rows, err := b.S.PluginAgentTransitions(name)
	if err != nil {
		return nil, refuse("audit", "refused: reading %s's transitions: %v · an audit read that fails reads as UNAVAILABLE, never as \"nothing happened here\" (S10's third state)", name, err)
	}
	out := make([]TransitionRecord, 0, len(rows))
	for _, r := range rows {
		at, err := time.Parse(time.RFC3339Nano, r.At)
		if err != nil {
			at = time.Time{}
		}
		out = append(out, TransitionRecord{Agent: r.Agent, From: State(r.FromState), To: State(r.ToState), Reason: r.Reason, At: at})
	}
	return out, nil
}
