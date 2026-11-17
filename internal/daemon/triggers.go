package daemon

// triggers.go — S40's socket-to-store seam for the automation desk: T7's
// reader (store rows to protocol views, reads only) and T8's writer (four
// human-only doors, AU1 — every actor arrives from the connection, every
// refusal sentence is carried verbatim from its owner).

import (
	"encoding/json"
	"errors"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type triggerReads struct{ s *store.Store }

// NewTriggerReads wires the socket's trigger desk to the store.
func NewTriggerReads(s *store.Store) protocol.EventTriggerReader {
	return &triggerReads{s: s}
}

func fireView(f store.Fire) protocol.EventFireView {
	return protocol.EventFireView{
		ID: f.ID, TriggerID: f.TriggerID, OutboxID: f.OutboxID,
		SubjectKind: f.SubjectKind, SubjectID: f.SubjectID, Depth: f.Depth,
		Outcome: f.Outcome, Reason: f.Reason, LoopID: f.LoopID, LoopSlug: f.LoopSlug,
		CostNote: f.CostNote, ClaimedAt: f.ClaimedAt, DecidedAt: f.DecidedAt,
	}
}

func triggerView(t store.Trigger, last *store.Fire) protocol.EventTriggerView {
	v := protocol.EventTriggerView{
		ID: t.ID, Name: t.Name, Source: t.Source,
		RootID: t.RootID, ComponentID: t.ComponentID,
		Match:     json.RawMessage(t.MatchJSON),
		Blueprint: t.Blueprint, Kickoff: t.Kickoff,
		MaxRunsPerHour: t.MaxRunsPerHour, MaxCostPerDayUSD: t.MaxCostPerDayUSD,
		Enabled: t.Enabled, DisabledReason: t.DisabledReason,
		CreatedByKind: t.CreatedByKind, CreatedByID: t.CreatedByID,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if last != nil {
		lv := fireView(*last)
		v.LastFire = &lv
	}
	return v
}

// lastFireOf returns the trigger's newest recorded answer, or nil when it
// never fired. id DESC is the house order (T5 measured it) — newest first.
// A free function because both seams need it: the reader to show the last
// answer, the writer to return the view the desk will re-read.
func lastFireOf(s *store.Store, triggerID string) (*store.Fire, error) {
	fs, err := s.ListEventFires(store.EventFireFilter{TriggerID: triggerID, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return nil, nil
	}
	return &fs[0], nil
}

func (r *triggerReads) ListEventTriggers(p protocol.EventTriggerListParams) ([]protocol.EventTriggerView, error) {
	ts, err := r.s.ListEventTriggers(store.EventTriggerFilter{
		RootID: p.RootID, ComponentID: p.ComponentID, Enabled: p.Enabled,
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.EventTriggerView, 0, len(ts))
	for _, t := range ts {
		last, err := lastFireOf(r.s, t.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, triggerView(t, last))
	}
	return out, nil
}

func (r *triggerReads) ShowTrigger(id string) (protocol.EventTriggerView, bool, error) {
	t, err := r.s.GetEventTrigger(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.EventTriggerView{}, false, nil
		}
		return protocol.EventTriggerView{}, false, err
	}
	last, err := lastFireOf(r.s, id)
	if err != nil {
		return protocol.EventTriggerView{}, false, err
	}
	return triggerView(t, last), true, nil
}

func (r *triggerReads) ListEventFires(p protocol.EventTriggerFiresParams) ([]protocol.EventFireView, map[string]int, error) {
	fs, err := r.s.ListEventFires(store.EventFireFilter{
		TriggerID: p.TriggerID, Outcome: p.Outcome, BeforeID: p.BeforeID, Limit: p.Limit,
	})
	if err != nil {
		return nil, nil, err
	}
	out := make([]protocol.EventFireView, 0, len(fs))
	for _, f := range fs {
		out = append(out, fireView(f))
	}
	counts, err := r.s.EventFireCounts(p.TriggerID) // "" = all triggers (T7's one-line store arm)
	if err != nil {
		return nil, nil, err
	}
	return out, counts, nil
}
