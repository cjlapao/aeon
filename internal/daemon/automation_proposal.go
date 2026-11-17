package daemon

// automation_proposal.go — S49 T6: the effect the `automation_disable`
// proposal performs, ONE package-level function so the commit door's seam
// (cmd/aeon/automation_proposal_seams.go) reaches T4's paths the same way
// the blueprint commit reaches daemon.BlueprintSave — through the daemon
// package, where the store-to-Scheduler adapters live, with the COMMITTER
// handed in and never guessed.
//
// What this function is NOT: a write of its own. The event family's disable
// is NewTriggerWrites' (T4's one write service for that row — the store
// half refuses a non-person before SQL, needs the reason, refuses an
// archived row, and audits its own change with THIS actor); the schedule
// and webhook families' off is the trigger.Scheduler's Pause — the same
// control plane the socket's trigger.pause rides, constructed here over the
// same adapters the daemon's scheduler uses, because a committed ask is a
// person's pause with a paper trail, not a new kind of pause. A throwaway
// Scheduler pauses without a starter: pause is a row flag, never a fire —
// the starter belongs to the clock, and the clock is asked for nothing
// here. The engine's audit rides the HumanWriter fork (the save's own
// transaction); AN4 counts one change, one row, per path.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// AutomationDisable performs the committed narrowing for one automation:
// disable for the event family, pause for the schedule and webhook
// families. source is the closed three (automationview's words); an
// unknown source is a programming error, refused, never defaulted. The
// actor is the committing person (desk.DeciderFrom at the seam) — a
// non-person reaches the AU1 locks below and is refused there, in the
// store's and engine's own sentences, unchanged.
func AutomationDisable(ctx context.Context, s *store.Store, a protocol.Actor, source, id, reason string) error {
	switch source {
	case "event":
		if _, err := NewTriggerWrites(s).DisableEventTrigger(a, id, reason); err != nil {
			return err
		}
		return nil
	case "schedule", "webhook":
		sc := trigger.NewScheduler(automationRows{triggerRows{s}}, nil, trigger.SystemClock{})
		ans, err := sc.Pause(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, id)
		if err != nil {
			return err
		}
		if !ans.Found {
			return fmt.Errorf("daemon: pause %s/%s: %s — the row the proposal asked about is gone", source, id, ans.Reason)
		}
		// The engine's idempotent answers ("already paused — the off is
		// recorded once") land with Found and zero writes: the state the
		// person approved IS the state now, and AN4 is satisfied by the
		// audit the FIRST off wrote, not by one here.
		return nil
	default:
		return fmt.Errorf("daemon: automation_disable source %q is not one of schedule, webhook, event — the source is part of the name", source)
	}
}
