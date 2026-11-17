package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// triggerRows is the scheduler's door into the store. It lives here rather
// than in internal/store because the scheduler speaks trigger.Schedule —
// the CONTRACT type, the one the whitelist guards — and a store that
// imported the contract would let the row layer drift from the guard while
// still compiling. The conversion is where those two worlds meet, on one
// page, in one direction.
type triggerRows struct{ s *store.Store }

// ActiveSchedules returns EVERY trigger row — the filter belongs to the
// consumer, because the two consumers answer different questions: a tick
// answers only unpaused schedule rows (clock.go and reconcile.go each
// check kind and paused themselves, and their tests pin it), while a
// webhook delivery must SEE its paused row to answer 409 with the reason
// (TR12's operational off is not a 404 — the bell rang, the door was
// quietly shut, and the record says so).
// SaveTriggerSchedule is the control-plane write (T8 pause/resume): the
// row comes back exactly as it was read with Paused flipped, and the
// store's upsert KEEPS covered_until — a pause cannot move the frontier.
func (t triggerRows) SaveTriggerSchedule(ctx context.Context, s trigger.Schedule) error {
	return t.s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: s.ID, Kind: string(s.Kind), Blueprint: s.Blueprint,
		Expr: s.Expr, Zone: s.Zone,
		WindowStart: s.WindowStart, WindowEnd: s.WindowEnd, WindowDays: s.WindowDays,
		Paused: s.Paused, SecretEnv: s.SecretEnv, CoveredUntil: s.CoveredUntil,
		CreatedAt: s.CreatedAt, Workspace: s.Workspace, Goal: s.Goal, EventFilter: s.Listens,
	})
}

func (t triggerRows) ActiveSchedules(ctx context.Context) ([]trigger.Schedule, error) {
	rows, err := t.s.TriggerSchedules(ctx)
	if err != nil {
		return nil, err
	}
	var out []trigger.Schedule
	for _, r := range rows {
		out = append(out, trigger.Schedule{
			ID: r.ID, Kind: trigger.Kind(r.Kind), Blueprint: r.Blueprint,
			Workspace: r.Workspace, Goal: r.Goal,
			Expr: r.Expr, Zone: r.Zone,
			WindowStart: r.WindowStart, WindowEnd: r.WindowEnd, WindowDays: r.WindowDays,
			Paused: r.Paused, SecretEnv: r.SecretEnv, CoveredUntil: r.CoveredUntil,
			CreatedAt: r.CreatedAt, Listens: r.EventFilter,
		})
	}
	return out, nil
}

func (t triggerRows) SetCovered(ctx context.Context, id, coveredUntil string) error {
	return t.s.SetTriggerCovered(ctx, id, coveredUntil)
}

// RecordGap writes the range-and-count could-not-say row reconcile found
// (T4, 0034's shape) — the instant strings arrive in the SAME
// triggerInstantFormat the fire rows use, so history sorts one way.
func (t triggerRows) RecordGap(ctx context.Context, id, coveredFrom, coveredTo string, missed int, at time.Time) error {
	return t.s.SaveTriggerGap(ctx, store.TriggerGapRow{
		ScheduleID: id, CoveredFrom: coveredFrom, CoveredTo: coveredTo,
		Missed: missed, At: at.UTC().Format(triggerInstantFormat),
	})
}

func (t triggerRows) RecordFire(ctx context.Context, f trigger.Fire) error {
	return t.s.RecordTriggerFire(ctx, store.TriggerFireRow{
		ScheduleID: f.TriggerID,
		Instant:    f.Instant.UTC().Format(triggerInstantFormat),
		Outcome:    string(f.Outcome),
		Reason:     f.Reason,
		LoopID:     f.LoopID,
		At:         f.At.UTC().Format(triggerInstantFormat),
	})
}

const triggerInstantFormat = "2006-01-02T15:04:05.999999999Z07:00"

// StartFromTrigger is trigger.Starter's ONE production implementation, and
// the clock's fire walks THE SAME path a person's start walks —
// d.Table.StartBlueprintLoop, publishing loop.started and starting the
// runner the way a socket start does. D1 made physical: the Fire struct is
// all the fire may say, and among its fields there is no attendedness, no
// outward grant, no route — so no schedule row and no webhook payload can
// smuggle either, because there is no parameter to smuggle them through.
//
// The unattendedness is authored HERE, as a hard false: a clock-started
// loop has nobody at the keyboard by definition (TR/G14 — pin the facts the
// start already knows and infer nothing), and outward stays the per-loop
// opt-in it has been since S19 T6 — which a schedule cannot grant either,
// for the same signature reason.
func (d *Daemon) StartFromTrigger(ctx context.Context, f trigger.Fire) (string, error) {
	if f.Provenance != trigger.ProvenanceTrigger && f.Provenance != trigger.ProvenanceWebhook {
		return "", fmt.Errorf("trigger fire %q claims provenance %q — only schedule and webhook fires start loops", f.TriggerID, f.Provenance)
	}
	goal := f.Goal
	if goal == "" {
		goal = fmt.Sprintf("scheduled fire of %s by %s at %s", f.Blueprint, f.TriggerID, f.Instant.Format("2006-01-02 15:04 MST"))
	}
	attended := false // D1/D2: authored by the start path, never supplied by the fire
	// AL8: the start consults the root's grants once, at pin time. A store
	// read error refuses the start (fail-toward-deny); a read that finds no
	// applicable allow pins nothing (byte-identical to today). The schedule
	// and webhook supply no labels, so a labelled grant never applies. The
	// fire's provenance is the origin; the closure sets started_by and
	// trigger_id at pin time, and SetLoopProvenance below records the same.
	autonomyPin, resolveErr := d.resolveAutonomy(ctx, autonomyOriginFor(f.Provenance), f.Workspace, nil)
	if resolveErr != nil {
		return "", resolveErr
	}
	p := protocol.LoopStartParams{
		Blueprint:     f.Blueprint,
		Goal:          goal,
		WorkspaceRoot: f.Workspace,
		Slug:          f.Slug(),
		With:          f.With,
		Attended:      &attended,
	}
	res, err := d.startLoopWith(ctx, p, startExtras{StartedBy: f.Provenance, TriggerID: f.TriggerID, Autonomy: autonomyPin})
	if err != nil {
		return "", err
	}
	id, _ := res["id"].(string)
	if id == "" {
		return "", fmt.Errorf("trigger fire %q: the start result carries no loop id", f.TriggerID)
	}
	// Provenance is written by the daemon, not by the fire — a fire cannot
	// name itself something it is not, and no caller of this function
	// reaches the columns except through what just started.
	if err := d.store.SetLoopProvenance(ctx, id, f.Provenance, f.TriggerID); err != nil {
		// The loop is real and running; the provenance is the record of
		// how it got here. A missing record must not orphan a live loop —
		// speak it loudly and keep the loop (the fire's row carries the
		// same fact; two records, one failing, is visible).
		if d.log != nil {
			d.log.Infof("trigger fire %q: loop %s started but its provenance failed to record: %v", f.TriggerID, id, err)
		}
	}
	return id, nil
}
