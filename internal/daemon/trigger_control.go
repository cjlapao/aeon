package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// trigger_control.go — the daemon seam for the four operational verbs and
// their history (S20 T8). As with the webhook seam, this file is
// deliberately almost empty of LOGIC: pause, resume, fire-now and the
// listing are internal/trigger/control.go's, one implementation for the
// CLI, the socket and the app (P34 — TestTriggerSocketAndCLICallSame-
// ControlFunctions is the call-site proof, and a second implementation
// here is exactly what that test exists to make impossible).
//
// fire-now's honesty rule travels: the daemon hands the fire to the SAME
// Scheduler.FireNow the CLI reaches — the loop comes out unattended over
// the socket exactly as over argv, because there is no parameter on this
// seam that could name a party for the loop to run attended FOR. There is
// no attended exception for a person pressing a button (TR2).
//
// S49 T8: the two READ mappings (views→list, rows→history) moved into the
// package functions below when automation_doors.go needed them too — the
// socket closure and the offline door now render through ONE function
// apiece, and the CLI's second door added no second mapping.

func (d *Daemon) wireTriggerControl() {
	d.Table.TriggerList = func(ctx context.Context) (any, error) {
		if d.triggers == nil {
			return nil, protocol.ErrStoreUnavailable.WithWhat("the trigger scheduler is not wired in this daemon")
		}
		views, err := d.triggers.List(ctx)
		if err != nil {
			return nil, err
		}
		return triggerListView(views), nil
	}
	// S49 T4: the four WRITE closures that lived here — pause, resume,
	// fire-now, add — moved to automation_write.go, where every trigger
	// write in the daemon now lives (T5's one-file pin). They still call
	// control.go's one implementation (P34 holds — the move changed the
	// file, never the count of implementations); only List and History,
	// reads both, stayed.
	d.Table.TriggerHistory = func(ctx context.Context, id string, limit int) (any, error) {
		if d.store == nil {
			return nil, protocol.ErrStoreUnavailable.WithWhat("this daemon has no store to read history from")
		}
		return triggerHistoryAnswer(ctx, d.store, id, limit)
	}
}

// triggerListView is the ONE scheduler-views → wire-views mapping.
func triggerListView(views []trigger.TriggerView) protocol.TriggerListResult {
	out := make([]protocol.TriggerListView, 0, len(views))
	for _, v := range views {
		out = append(out, protocol.TriggerListView{
			ID: v.ID, Kind: v.Kind, Blueprint: v.Blueprint, Expr: v.Expr,
			Zone: v.Zone, Window: v.Window, Workspace: v.Workspace, Goal: v.Goal,
			Paused: v.Paused, NextFire: v.NextFire,
		})
	}
	return protocol.TriggerListResult{Triggers: out}
}

// triggerHistoryAnswer is the ONE fire/gap rows → wire mapping, clamp
// included (limit outside 1..200 answers the default 50 — the old
// wiring's number, moved, never retyped elsewhere).
func triggerHistoryAnswer(ctx context.Context, s *store.Store, id string, limit int) (protocol.TriggerHistoryResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	fires, err := s.TriggerFireHistory(ctx, id, limit)
	if err != nil {
		return protocol.TriggerHistoryResult{}, err
	}
	gaps, err := s.TriggerGaps(ctx, id, limit)
	if err != nil {
		return protocol.TriggerHistoryResult{}, err
	}
	res := protocol.TriggerHistoryResult{TriggerID: id, Fires: make([]protocol.TriggerFireView, 0, len(fires)), Gaps: make([]protocol.TriggerGapView, 0, len(gaps))}
	for _, f := range fires {
		res.Fires = append(res.Fires, protocol.TriggerFireView{Instant: f.Instant, Outcome: f.Outcome, Reason: f.Reason, LoopID: f.LoopID, At: f.At})
	}
	for _, g := range gaps {
		res.Gaps = append(res.Gaps, protocol.TriggerGapView{CoveredFrom: g.CoveredFrom, CoveredTo: g.CoveredTo, Missed: g.Missed, At: g.At})
	}
	return res, nil
}
