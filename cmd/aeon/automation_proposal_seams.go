package main

// automation_proposal_seams.go — S49 T6 (AN9, TD3, TD4): the three seams the
// `automation_disable` kind runs through, registered in deskSeams() so the
// create door and the commit door call literally the same functions (TD3),
// and in proposalValidators so `proposal.create` validates before it stores
// one byte.
//
// What the kind IS: an agent-shaped ask to switch ONE automation OFF —
// pause a schedule or webhook row, disable an event row. The payload names
// WHO and WHY and no target state; the effect can only reduce what the
// automation may do (AN9: the one automation write an agent can initiate,
// and it narrows). Every widening shape — enable, unarchive, create,
// update, fire — is not even a payload this package can parse; the strict
// desk decode refuses those field names as unknown keys before any store is
// opened.
//
// What this file does NOT have: a second write (the apply rides T4's own
// paths — daemon.NewTriggerWrites for the event row, the trigger.Scheduler's
// Pause for a schedule or webhook row, both stamped with the COMMITTER from
// desk.DeciderFrom, never the proposer, whose identity lives in the
// proposal row's created_by_*), an actor or a target state taken from the
// payload, or an audit the desk invents (each path writes its OWN
// automation_change row in its own frozen sentence — "disabled event" /
// "pause schedule" — the one audit AN4 counts; the proposal's id rides the
// event row's disabled_reason, and for a pause it lives in the proposal row
// itself: the engine's audit sentence is AN14-frozen, and a second audit
// row for one change is the double-count AN4 forbids).
//
// The two identities are fail-closed: an apply whose context carries no
// committer is not a commit, and one that lost the proposal id is a broken
// wiring refused rather than a write that cannot be traced back (S59 T7's
// shape, carried). AlreadyApplied answers on the row's OFF state (paused for
// schedule/webhook, disabled for event), so a crash between effect and flip
// completes on retry and burns no second change (TD4).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// seamValidateAutomationDisable is the kind's validator, at BOTH doors. The
// order is the order the refusals read best in: shape, then the row, then
// the one rule that matters (AN9's narrowing — here, "already off is not
// something to ask for": an inert disable of an already-off automation is
// refused AT CREATE, S35 TD3's nothing-inert-carried-to-commit).
func seamValidateAutomationDisable(ctx context.Context, payload []byte) error {
	// Shape first, through the desk's own strict parse (unknown fields
	// refused, the caps applied) — the same function the registry runs when
	// no seam replaces it, reached here so the store half sits behind it.
	if err := desk.ValidateAutomationDisable(ctx, payload); err != nil {
		return err
	}
	p, _, off, archived, err := automationDisableFacts(payload)
	if err != nil {
		return err
	}
	if archived {
		return fmt.Errorf("refused: %s/%s is archived — an archived automation is history, not a draft, and its switch is already past off; automation.unarchive is the person's door (S49 T6, AN6)", p.Source, p.ID)
	}
	if off {
		return fmt.Errorf("refused: %s/%s is already off — nothing to disable. An ask that changes nothing is refused at create, not carried to a commit nobody should have to make (S49 T6, AN9)", p.Source, p.ID)
	}
	return nil
}

// seamAlreadyAppliedAutomationDisable answers TD4 for the kind: the row
// ALREADY says off. The crash-retry — the disable landed but the flip did
// not — completes clean here and burns no second change, and AN4 counts the
// one audit the landed write already wrote. A broken payload or an
// unreadable row answers false: those are validate's refusals, and an
// AlreadyApplied that papers over them is the lie this gate exists to
// keep.
func seamAlreadyAppliedAutomationDisable(ctx context.Context, payload []byte) (bool, error) {
	if err := desk.ValidateAutomationDisable(ctx, payload); err != nil {
		return false, nil
	}
	_, _, off, _, err := automationDisableFacts(payload)
	if err != nil {
		return false, nil
	}
	return off, nil
}

// seamApplyAutomationDisable performs the committed ask through T4's own
// paths — daemon.NewTriggerWrites for the event row (the store half audits
// its own change as AU1 demands, with the reason the person read beside the
// proposal's id), the trigger.Scheduler's Pause for a schedule or webhook
// row (the control plane's own operational off, TR12, with the engine's own
// audit through the HumanWriter fork). The actor is the COMMITTER from the
// commit door, never this payload's author; both identities are fail-closed.
func seamApplyAutomationDisable(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return errors.New("refused: automation_disable's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S49 T6)")
	}
	id, ok := desk.ProposalIDFrom(ctx)
	if !ok {
		return errors.New("refused: automation_disable's apply needs the proposal's id from the commit door — the change is traced back through it, and an apply that lost it is broken wiring, not a shortcut (S49 T6)")
	}
	p, _, _, _, err := automationDisableFacts(payload)
	if err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()

	// The effect rides daemon.AutomationDisable — the same package-level
	// door the blueprint commit reaches daemon.BlueprintSave through: the
	// event family's disable is T4's ONE write service, the schedule and
	// webhook families' off is the trigger.Scheduler's own Pause over the
	// daemon's adapters (the daemon package is where those adapters live).
	// The reason carried down is the person's sentence with the proposal's
	// id beside it, so the event row's disabled_reason traces the change
	// to the decision that made it; for a pause the engine's audit sentence
	// is AN14-frozen and the trace lives in the proposal row itself.
	person := protocol.Actor{Kind: dec.Kind, ID: dec.ID}
	reason := fmt.Sprintf("%s (proposal #%d)", p.Reason, id)
	if err := daemon.AutomationDisable(ctx, st, person, p.Source, p.ID, reason); err != nil {
		var perr *protocol.Error
		if errors.As(err, &perr) {
			return fmt.Errorf("%s — %s", perr.Message, perr.Data.What)
		}
		return err
	}
	return nil
}

// validateAutomationDisableProposal is the create door's entry (TD3): the
// same registry entry the commit door re-runs, reached from the one call
// site this package keeps for it.
func validateAutomationDisableProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindAutomationDisable), payload)
}

// automationDisableFacts is the shared read behind all three seams: the
// decoded payload, whether the row is OFF (paused for schedule/webhook,
// disabled for event — the two families' operational offs in one answer),
// and whether it is ARCHIVED (AN6). The unknown name is refused here, in
// the store's own ErrNotFound turned into a sentence naming
// automation.list — and the id living under the OTHER source is named for
// what it is: the source is part of the name (AN1), so "event/nightly"
// asked of a schedule row is a refusal, not a guess.
//
// One read, one order, so the doors cannot disagree about what the row said.
func automationDisableFacts(payload []byte) (p desk.AutomationDisableProposal, exists, off, archived bool, err error) {
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, false, false, false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return p, false, false, false, err
	}
	defer st.Close()

	switch p.Source {
	case "event":
		row, err := st.GetEventTrigger(p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return p, false, false, false, fmt.Errorf("refused: no event automation %q exists — automation.list shows what exists (S49 T6)", p.ID)
			}
			return p, false, false, false, err
		}
		return p, true, !row.Enabled, row.ArchivedAt != "", nil
	default: // schedule / webhook — the trigger_schedule family
		row, err := st.TriggerSchedule(context.Background(), p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// The id may exist UNDER THE OTHER FAMILY — the cross-hall
				// naming is the store read's own honesty, carried.
				if other, oerr := st.GetEventTrigger(p.ID); oerr == nil && other.ID != "" {
					return p, false, false, false, fmt.Errorf("refused: %q is an event automation, not the %s named — the source is part of the name (S49 T6, AN1)", p.ID, p.Source)
				}
				return p, false, false, false, fmt.Errorf("refused: no %s automation %q exists — automation.list shows what exists (S49 T6)", p.Source, p.ID)
			}
			return p, false, false, false, err
		}
		if row.Kind != p.Source {
			return p, true, false, false, fmt.Errorf("refused: %q is a %s, not the %s named — the source is part of the name (S49 T6, AN1)", p.ID, row.Kind, p.Source)
		}
		return p, true, row.Paused, row.ArchivedAt != "", nil
	}
}
