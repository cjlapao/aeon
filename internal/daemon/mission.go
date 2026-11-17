package daemon

// mission.go — S58 T6 (SV11): the `mission.status` service, and the two
// sealed seams S55 and S27 open it through.
//
// # One evaluation, injected truths
//
// The reading lives in `internal/mission`; this file assembles its inputs
// from the daemon's own parts and maps the values onto the wire twins. The
// needs-you injection is the daemon's OWN InboxService — the same object
// `inbox.summary` answers through, called with the entry's own rootId
// through the same door. There is no second attention evaluation in this
// process, which is what makes the consistency claim structural rather
// than hoped for.
//
// # The seams
//
// `RegisterBudgetReader` and `RegisterNextReady` are the exported seams
// S55's budget plane and S27's queue wire into the overview. Until a slice
// registers, the group answers `not-wired` — the honest absence (a zero
// would read as "checked, nothing found"). Registration is wiring-time
// only, called from the serve assembly before traffic; it is not a socket
// door and carries no authority.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/mission"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// missionSeams holds the two registrars' answers (nil each until a slice
// registers — the not-wired stance by value).
type missionSeams struct {
	budget mission.CapReader
	next   mission.NextReader
}

// RegisterBudgetReader wires S55's cap answer into the overview. Called at
// assembly, never at runtime; registering after traffic has started races a
// reader and is this slice's equivalent of a programming error, not a
// supported state.
func (d *Daemon) RegisterBudgetReader(r mission.CapReader) { d.missionSeams.budget = r }

// RegisterNextReady wires S27's queue answer into the overview. Same
// assembly-time discipline as RegisterBudgetReader.
func (d *Daemon) RegisterNextReady(r mission.NextReader) { d.missionSeams.next = r }

// missionService is the protocol.MissionService implementation.
type missionService struct{ d *Daemon }

// newMissionService returns nil — the V13 absence — without a store (no
// rows to read) or without the inbox service (the needs-you injection IS
// the inbox; an overview that fabricated one would be two truths waiting
// to drift).
func (d *Daemon) newMissionService() *missionService {
	if d.store == nil || d.Inbox == nil {
		return nil
	}
	return &missionService{d: d}
}

// MissionStatus evaluates the overview for one root (a rootId names it) or
// every live ROOT component (rootId omitted: one entry per tree, children
// rolled up into their root's numbers, never listed as their own line).
func (m *missionService) MissionStatus(ctx context.Context, p protocol.MissionStatusParams) (protocol.MissionStatusResult, error) {
	comps, err := m.d.store.LiveComponentTree()
	if err != nil {
		return protocol.MissionStatusResult{}, err
	}
	byID := map[string]int{}
	for i, c := range comps {
		byID[c.ID] = i
	}
	var wanted []store.AttentionComponent
	if p.RootID == "" {
		for _, c := range comps {
			if c.ParentID == "" {
				wanted = append(wanted, c)
			}
		}
	} else {
		i, ok := byID[p.RootID]
		if !ok {
			return protocol.MissionStatusResult{}, fmt.Errorf("%w: %s", attention.ErrUnknownScope, p.RootID)
		}
		wanted = append(wanted, comps[i])
	}
	entries := make([]mission.Entry, 0, len(wanted))
	for _, w := range wanted {
		name := ""
		if comp, err := m.d.store.ReadComponent(w.ID); err == nil {
			name = comp.Name
		}
		entries = append(entries, mission.Entry{RootID: w.ID, Paths: w.Paths, Name: name})
	}
	deps := mission.Deps{
		Store: m.d.store,
		NeedsYou: func(ctx context.Context, e mission.Entry) (mission.NeedsYou, error) {
			res, err := m.d.Inbox.InboxSummary(ctx, protocol.InboxSummaryParams{RootID: e.RootID})
			if err != nil {
				return mission.NeedsYou{}, err
			}
			return mapNeedsYou(res), nil
		},
		Budget:    m.d.missionSeams.budget,
		NextReady: m.d.missionSeams.next,
	}
	res, err := mission.Evaluate(ctx, deps, entries)
	if err != nil {
		return protocol.MissionStatusResult{}, err
	}
	out := protocol.MissionStatusResult{Components: make([]protocol.MissionComponentStatus, 0, len(res.Components))}
	for _, c := range res.Components {
		out.Components = append(out.Components, protocol.MissionComponentStatus{
			RootID: c.RootID,
			Name:   c.Name,
			Loops: protocol.MissionLoopsCounts{
				Running: c.Loops.Running, Waiting: c.Loops.Waiting,
				Blocked: c.Loops.Blocked, Idle: c.Loops.Idle,
			},
			NeedsYou: mapNeedsYouBack(c.NeedsYou),
			Spend:    protocol.MissionSpend{State: c.Spend.State, UsdAtLeast: c.Spend.USDAtLeast, CostState: c.Spend.CostState},
			Cap:      protocol.MissionCap{State: c.Cap.State, Summary: c.Cap.Summary, LimitUSD: c.Cap.LimitUSD, Period: c.Cap.Period},
			NextReady: protocol.MissionNextReady{
				State: c.NextReady.State, LoopSlug: c.NextReady.LoopSlug,
				Phase: c.NextReady.Phase, Reason: c.NextReady.Reason,
			},
			LastActivityAt: c.LastActivityAt,
		})
	}
	return out, nil
}

// mapNeedsYou / mapNeedsYouBack are the field-for-field twins between the
// protocol summary and mission's own shape (the one-adapter rule; the
// equality test catches a missed field by name, not by vibes).
func mapNeedsYou(r protocol.InboxSummaryResult) mission.NeedsYou {
	nu := mission.NeedsYou{
		Total: r.Total, BySeverity: r.BySeverity, Acked: r.Acked,
		OldestSince: r.OldestSince, Sources: make([]mission.SourceState, 0, len(r.Sources)),
	}
	for _, s := range r.Sources {
		nu.Sources = append(nu.Sources, mission.SourceState{Name: s.Name, State: s.State, Error: s.Error})
	}
	return nu
}

func mapNeedsYouBack(n mission.NeedsYou) protocol.InboxSummaryResult {
	r := protocol.InboxSummaryResult{
		Total: n.Total, BySeverity: n.BySeverity, Acked: n.Acked,
		OldestSince: n.OldestSince, Sources: make([]protocol.InboxSourceState, 0, len(n.Sources)),
	}
	for _, s := range n.Sources {
		r.Sources = append(r.Sources, protocol.InboxSourceState{Name: s.Name, State: s.State, Error: s.Error})
	}
	return r
}
