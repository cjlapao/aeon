package automation

// dispatcher.go — the org layers' heartbeat (AU8). One Drain: read the
// outbox after the durable cursor, find every matching trigger, CLAIM each
// (trigger, event) pair before anything starts, then depth → overlap →
// limits → target → start, recording exactly one of the five outcomes with
// its reason. The whole package is a pure function of injected time and an
// injected Starter: no goroutine, no sleep, and the only wall-clock read is
// defaultClock below — which Run installs and nothing else may call.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// Dispatcher is the pass. Store, Starter and Clock are required;
// ResolveRoot is wired by T6's daemon (nil refuses component-scoped
// triggers as no-target-component rather than guessing); Log is optional
// and nil-safe.
type Dispatcher struct {
	Store       *store.Store
	Starter     Starter
	ResolveRoot func(componentID string) (string, error) // S38 component read; wired in T6
	Clock       func() time.Time                         // REQUIRED; no default to time.Now here
	Log         interface {
		Infof(format string, args ...any)
	}
	PollEvery time.Duration    // used by Run (T6)
	Tick      <-chan time.Time // injected cadence for Run; nil means a ticker at PollEvery
	BatchSize int              // default 100
}

// cursorName is THE dispatcher's cursor — one row, one meaning: the last
// outbox id this dispatcher finished dispatching.
const cursorName = "dispatcher"

func (d *Dispatcher) batch() int {
	if d.BatchSize <= 0 {
		return 100
	}
	return d.BatchSize
}

// infof speaks through the optional log; a dispatcher with no logger is
// still correct, just quiet — the fire rows are the record, not the log.
func (d *Dispatcher) infof(format string, args ...any) {
	if d.Log != nil {
		d.Log.Infof(format, args...)
	}
}

// Drain dispatches up to one batch. A business answer (started, refused,
// skipped, limited, could-not-say) is a fire row, never an error; an error
// return means the STORE failed, and the cursor stays where it is — the
// next pass re-reads from it, and the claim makes the replay harmless.
func (d *Dispatcher) Drain(ctx context.Context) (events int, err error) {
	if d.Clock == nil {
		return 0, fmt.Errorf("automation: the dispatcher requires an injected clock — AU8 keeps time honest by owning it")
	}
	after, err := d.Store.AutomationCursor(cursorName)
	if err != nil {
		return 0, err
	}
	evs, err := d.Store.ReadAutomationEvents(after, d.batch())
	if err != nil {
		return 0, err
	}
	for _, ev := range evs {
		if err := ctx.Err(); err != nil {
			return events, err
		}
		trigs, err := d.Store.ListEventTriggersFor(ev)
		if err != nil {
			return events, err
		}
		for _, tr := range trigs {
			if err := d.fire(ctx, tr, ev); err != nil {
				return events, err // store failure: stop, cursor NOT advanced
			}
		}
		if err := d.Store.AdvanceAutomationCursor(cursorName, ev.ID, d.Clock()); err != nil {
			return events, err
		}
		events++
	}
	return events, nil
}

// fire is AU8 steps 1–2 for one (trigger, event) pair. A disabled trigger
// is RECORDED, never silently passed (the Do-Not list); a live trigger must
// win the claim — claim-before-start means a loser starts nothing and says
// nothing, because the winner owns that row's answer.
func (d *Dispatcher) fire(ctx context.Context, tr store.Trigger, ev store.AutomationEvent) error {
	nf := store.NewEventFire{TriggerID: tr.ID, OutboxID: ev.ID,
		SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID, Depth: ev.Depth}
	if !tr.Enabled {
		reason := "disabled"
		if tr.DisabledReason != "" {
			reason += ": " + tr.DisabledReason
		}
		_, _, err := d.Store.RecordEventFire(nf,
			store.EventFireDecision{Outcome: store.FireSkipped, Reason: reason}, d.Clock())
		return err
	}
	f, fresh, err := d.Store.ClaimEventFire(nf, d.Clock())
	if err != nil || !fresh {
		return err // already claimed: T6's Reconcile owns open claims
	}
	return d.execute(ctx, tr, ev, f)
}

// execute is AU8 steps 3–7 for a CLAIMED fire, in exactly that order.
// Reconcile (T6) calls it too: a claim recovered from a crash re-runs the
// same guarded start, and the claim itself is what makes that safe.
func (d *Dispatcher) execute(ctx context.Context, tr store.Trigger, ev store.AutomationEvent, f store.Fire) error {
	decide := func(o, reason, loopID, slug, note string) error {
		return d.Store.DecideEventFire(f.ID, store.EventFireDecision{
			Outcome: o, Reason: reason, LoopID: loopID, LoopSlug: slug, CostNote: note,
		}, d.Clock())
	}
	// 3. depth (AU5). The event row already carries the depth its emitting
	// loop was at (T2 derived it, nobody passes a number); +1 is the depth
	// the loop THIS fire starts will record. A refused depth writes no loop,
	// so the line holds against the event the loop would have had.
	if ev.Depth > store.MaxAutomationDepth {
		return decide(store.FireRefused, fmt.Sprintf("depth-exceeded: event depth %d > %d (caused by trigger %q)",
			ev.Depth, store.MaxAutomationDepth, ev.CausedByTrigger), "", "", "")
	}
	// S20 T7: TriggerAccess evaluates here (no code in S40).
	// 4. overlap (S20's StillGoing rule, copied into the SQL the store
	// owns; the agreement is pinned by a test, not re-decided here).
	if slug, status, found, err := d.Store.OverlappingEventFire(tr.ID, ev.SubjectKind, ev.SubjectID); err != nil {
		return err
	} else if found {
		return decide(store.FireSkipped, fmt.Sprintf("overlap: loop %s is still %s", slug, status), "", "", "")
	}
	// 5. limits (AU11). An open claim consumes an hourly slot — a start in
	// flight is a run in flight — and the caller's own claim is excluded so
	// it never reserves a slot against itself.
	now := d.Clock()
	n, err := d.Store.CountRecentEventFires(tr.ID, now.Add(-time.Hour), f.ID)
	if err != nil {
		return err
	}
	// The store validates 1–1000 at the door (AU11: a trigger without a
	// rate is an outage with a name), so >= against a stored value is the
	// whole rule — there is no zero-means-unlimited shadow case.
	if n >= tr.MaxRunsPerHour {
		return decide(store.FireLimited,
			fmt.Sprintf("max-runs-per-hour: %d started since %s, limit %d",
				n, now.Add(-time.Hour).UTC().Format(time.RFC3339), tr.MaxRunsPerHour), "", "", "")
	}
	var note string
	if tr.MaxCostPerDayUSD != nil {
		sp, err := d.Store.EventTriggerSpend(tr.ID, now.Add(-24*time.Hour))
		if err != nil {
			return err
		}
		if sp.Floor {
			note = "cost-floor"
		}
		// A floor that already reaches the cap PROVES the cap reached —
		// unknown steps can only push a total up, never down. What unknown
		// defeats is the opposite proof, and that is what cost_note says.
		if sp.USD >= *tr.MaxCostPerDayUSD {
			return decide(store.FireLimited,
				fmt.Sprintf("max-cost-per-day: $%.2f known (floor=%v) >= $%.2f",
					sp.USD, sp.Floor, *tr.MaxCostPerDayUSD), "", "", note)
		}
		if sp.Unknown {
			note = "cost-unknown" // cannot check the cap: runs/hour alone bounds this fire
		}
	}
	// 6. target: the trigger's own component wins; else the event's single
	// component; else nobody knows where this work lives, and guessing a
	// workspace is the one thing a dispatcher may never do.
	if d.ResolveRoot == nil {
		return decide(store.FireRefused,
			"no-target-component: no root reader is wired — the dispatcher cannot name a workspace", "", "", note)
	}
	cid := tr.ComponentID
	if cid == "" {
		cid = ev.ComponentID
	}
	if cid == "" {
		return decide(store.FireRefused,
			"no-target-component: the event names no single component; set component on the trigger", "", "", note)
	}
	root, err := d.ResolveRoot(cid)
	if err != nil {
		return decide(store.FireRefused, "workspace-invalid: "+err.Error(), "", "", note)
	}
	// 7. start. One door (T4's), unattended by construction (AU2).
	res, serr := d.Starter.Start(ctx, EventStartRequest{
		TriggerID: tr.ID, TriggerName: tr.Name, Blueprint: tr.Blueprint,
		WorkspaceRoot: root, Slug: AutoSlug(tr.ID, ev.ID), Event: ev.Event,
		SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID, SubjectVersion: ev.SubjectVersion,
		KickoffTemplate: tr.Kickoff, Depth: ev.Depth + 1,
	})
	var ref *Refusal
	switch {
	case errors.As(serr, &ref):
		if !store.ValidEventRefusedReason(ref.Reason) {
			// Never trust an unclassified refusal: a starter that invents
			// vocabulary is a starter whose verdicts cannot be trusted.
			return decide(store.FireCouldNotSay,
				"start-error: unclassified refusal "+ref.Reason+": "+ref.Sentence, "", "", note)
		}
		return decide(store.FireRefused, ref.Reason+": "+ref.Sentence, "", "", note)
	case serr != nil:
		return decide(store.FireCouldNotSay, "start-error: "+serr.Error(), "", "", note)
	}
	return decide(store.FireStarted, "", res.LoopID, res.Slug, note)
}

// defaultClock is the ONLY wall-clock read in this package — a grep test
// counts it, so a second time.Now anywhere fails the suite. Run installs it
// when no clock was injected; every other entry point requires one.
func defaultClock() time.Time { return time.Now() }

// pollEvery is AU8's cadence: the daemon names a second, a test may inject
// anything, and the floor is a second — the cursor is durable either way, so
// the poll rate buys latency, never correctness.
func (d *Dispatcher) pollEvery() time.Duration {
	if d.PollEvery > 0 {
		return d.PollEvery
	}
	return time.Second
}

// Run is the daemon's side of the package: reconcile once (AU13 — claims
// left open by a crash are answered before a single new event is read, so a
// restart never races its own recovery), then Drain on the tick. Errors are
// logged with Infof and NEVER fatal — a dispatcher that cannot reach the
// store says so and tries again on the next tick, exactly the recovery
// goroutine's bargain in serve.go. The first Drain runs before any tick:
// boot is allowed to do work.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d.Clock == nil {
		d.Clock = defaultClock
	}
	if sum, err := d.Reconcile(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d.infof("automation: reconcile failed: %v", err)
	} else if sum.Open > 0 {
		d.infof("automation: reconciled %d open fire(s): %+v", sum.Open, sum)
	}
	tick := d.Tick
	if tick == nil {
		t := time.NewTicker(d.pollEvery())
		defer t.Stop()
		tick = t.C
	}
	for {
		if _, err := d.Drain(ctx); err != nil && ctx.Err() == nil {
			d.infof("automation: drain failed: %v", err) // retried next tick
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
		}
	}
}
