package automation

// recover.go is AU13: the answers a crashed daemon left unspoken. A claim
// without a decision sits in event_trigger_fire with outcome NULL — OpenEventFires
// finds exactly those rows, and Reconcile gives each one answer from the closed
// table. It never starts a second loop for one (trigger, outbox) pair: the loop
// is looked up by the slug the original start would have used (AutoSlug is
// deterministic precisely so recovery can name it), and the re-run path re-enters
// through the same claimed fire — the store's claimed-FROM decide refuses a
// second verdict, so double-deciding is a store refusal, not a discipline.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Summary counts what Reconcile answered. Rerun counts RE-ATTEMPTS through
// execute; the fires themselves hold the truth of each outcome (a test reads
// the fire rows, per the plan), so Rerun is not a sixth outcome.
type Summary struct {
	Open, Started, Skipped, Refused, CouldNotSay, Rerun int
}

// Reconcile decides every open claim once, in AU13's table order:
// row 5 (subject unreadable) refuses before anything is consulted further;
// row 1 (the loop exists) completes the record without touching the Starter;
// row 2 (disabled since) skips; row 3 (edited since) could-not-says;
// row 4 re-runs the guarded start from execute — the claim's own slot makes
// that safe, and a started loop still only appears once because the slug is
// the same one fire() would have passed.
func (d *Dispatcher) Reconcile(ctx context.Context) (Summary, error) {
	if d.Store == nil || d.Starter == nil {
		return Summary{}, errors.New("automation: Reconcile needs a Store and a Starter")
	}
	if d.Clock == nil {
		return Summary{}, errors.New("automation: Reconcile needs an injected clock (Run supplies the daemon's)")
	}
	open, err := d.Store.OpenEventFires()
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Open: len(open)}
	for _, f := range open {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		decide := func(o, reason, loopID, slug string) error {
			return d.Store.DecideEventFire(f.ID, store.EventFireDecision{
				Outcome: o, Reason: reason, LoopID: loopID, LoopSlug: slug,
			}, d.Clock())
		}
		tr, terr := d.Store.GetEventTrigger(f.TriggerID)
		ev, eerr := d.Store.ReadAutomationEvent(f.OutboxID)
		// Row 5: the subject is gone. Both names are spoken — a refusal
		// that cannot say what vanished is the nameless kind AU12 rejects.
		if errors.Is(terr, store.ErrNotFound) || errors.Is(eerr, store.ErrNotFound) {
			if err := decide(store.FireRefused, fmt.Sprintf(
				"subject-missing: trigger %s / outbox %d no longer readable", f.TriggerID, f.OutboxID), "", ""); err != nil {
				return sum, err
			}
			sum.Refused++
			continue
		}
		if terr != nil {
			return sum, terr
		}
		if eerr != nil {
			return sum, eerr
		}
		// Row 1: the loop already exists — the crash landed between commit
		// and decide. Complete the record; the Starter is never called, so
		// a starter that WOULD have failed cannot undo an honest commit.
		cid := tr.ComponentID
		if cid == "" {
			cid = ev.ComponentID
		}
		if cid != "" && d.ResolveRoot != nil {
			if root, rerr := d.ResolveRoot(cid); rerr == nil {
				slug := AutoSlug(tr.ID, ev.ID)
				l, gerr := d.Store.GetLoop(store.Scope{LoopSlug: slug, ComponentRoot: root})
				if gerr == nil {
					if err := decide(store.FireStarted, "", l.ID, l.Slug); err != nil {
						return sum, err
					}
					sum.Started++
					continue
				}
				if !errors.Is(gerr, store.ErrNotFound) {
					return sum, gerr
				}
			}
		}
		switch {
		case !tr.Enabled: // row 2 — checked before the edit because disabling IS an edit
			reason := "disabled"
			if tr.DisabledReason != "" {
				reason += ": " + tr.DisabledReason
			}
			if err := decide(store.FireSkipped, reason, "", ""); err != nil {
				return sum, err
			}
			sum.Skipped++
		case tr.UpdatedAt > f.ClaimedAt: // row 3 (ISO-8601 UTC strings compare as written)
			// The definition that justified this claim is no longer the
			// definition on file. Answering "started" would vouch for a
			// match nobody wrote; could-not-say is AU12's honest third.
			if err := decide(store.FireCouldNotSay,
				"trigger-edited-since-claim: the definition that justified this claim changed", "", ""); err != nil {
				return sum, err
			}
			sum.CouldNotSay++
		default: // row 4: idempotent by slug — re-enter the same guarded start
			if err := d.execute(ctx, tr, ev, f); err != nil {
				return sum, err
			}
			sum.Rerun++
		}
	}
	return sum, nil
}
