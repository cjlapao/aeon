// memory_proposal_seams.go — S52 T4 (LM7, LM8): the store halves of the
// learning_retract proposal kind, as ONE seam map that BOTH doors run.
//
// The desk (internal/desk/learning_kinds.go) holds the pure shape: a strict
// payload of {id, reason} — no actor, no scope, no state, the reason bound
// to the stamp column's own 256 so an ask can never die at Apply on the
// length of its own words. What it may not read, the seam reads: the
// learning row itself (does it exist at all, is it not ALREADY retracted).
// The seam's Validate REPLACES the desk's shape half with the full
// desk-then-store function — the merge rule in desk.New — so the create
// door and the commit door run ONE function (S35 TD3).
//
// The readable-scope half of the law lives HIGHER, in the daemon arm that
// files the ask (records_learnings.go): only an agent's own create door
// needs it, and there out-of-scope answers the no-such-learning sentence.
// A person at the desk commits a row that exists and is live; the person's
// view has always been the whole store.
//
// AlreadyApplied answers the crash-retry: the row IS retracted, so the
// effect has landed and decide.go completes the commit without re-writing
// (it runs this BEFORE Validate, so the not-retracted refusal below can
// never trap a retry in its own footprint). A missing row answers false —
// a subject that vanished between create and commit is a genuine conflict
// Validate names loudly, not an effect that already happened.
//
// Apply is learnings' OWN retract — the same Retract the person's
// learnings.retract door runs: stamp, reason, ONE learning_change line,
// vector hygiene, all in the plane's one write path — with the COMMITTING
// PERSON as actor, never the proposer (LM8). The audit line carries the
// committer's kind and id; the proposal's id rides the sentence printed at
// the door, so the ask and the act are traceable to each other without
// either one borrowing the other's name.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/learnings"
)

// learningRetractFacts decodes the payload (already shape-checked by the
// desk at the doors that care) and reads the two row facts beside it.
func learningRetractFacts(payload []byte) (p desk.LearningRetractProposal, found, retracted bool, err error) {
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, false, false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return p, false, false, err
	}
	defer st.Close()
	lr, ok, err := learnings.New(st).Show(p.ID)
	if err != nil {
		return p, false, false, err
	}
	if !ok {
		return p, false, false, nil
	}
	return p, true, lr.Retracted, nil
}

// seamValidateLearningRetract is the full Validate both doors run: the
// desk's shape half first (its sentences travel verbatim), then the two
// facts a shape cannot carry.
func seamValidateLearningRetract(ctx context.Context, payload []byte) error {
	if err := desk.ValidateLearningRetract(ctx, payload); err != nil {
		return err
	}
	p, found, retracted, err := learningRetractFacts(payload)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("refused: no learning %d exists to take back — learnings.board and learnings.list show what exists (S52 T4, LM7)", p.ID)
	}
	if retracted {
		return fmt.Errorf("refused: learning %d is already retracted — the ask changes nothing, and asks that change nothing are refused at create, not carried to a commit nobody should have to make (S52 T4, TD3)", p.ID)
	}
	return nil
}

// seamAlreadyAppliedLearningRetract answers the crash-retry: the stamp is
// already on the row. A malformed payload or an unreadable store says NOT
// applied — the honest false — so Validate and Apply still run and refuse
// loudly rather than this gate papering over a genuine conflict (TD4).
func seamAlreadyAppliedLearningRetract(ctx context.Context, payload []byte) (bool, error) {
	if err := desk.ValidateLearningRetract(ctx, payload); err != nil {
		return false, nil
	}
	_, found, retracted, err := learningRetractFacts(payload)
	if err != nil || !found {
		return false, err
	}
	return retracted, nil
}

// seamApplyLearningRetract stamps the row — learnings' own Retract, never
// a hand-rolled update statement, so the stamp, the reason, the ONE audit line and
// the vector hygiene are exactly what the person's own door writes. The
// committer comes from the commit door's context and the proposal id rides
// with it; a context missing either is broken wiring, not a shortcut.
func seamApplyLearningRetract(ctx context.Context, w io.Writer, payload []byte) error {
	a, ok := desk.DeciderFrom(ctx)
	if !ok || a.Kind != "user" || a.ID == "" {
		return errors.New("refused: learning_retract's apply needs the committer's identity from the commit door — a context without it is not a commit, and a stamp with a guessed actor is the audit gap (S52 T4, LM8)")
	}
	proposalID, ok := desk.ProposalIDFrom(ctx)
	if !ok || proposalID <= 0 {
		return errors.New("refused: learning_retract's apply needs the proposal's id from the commit door — the retraction traces back through it, and an apply that lost it is broken wiring, not a shortcut (S52 T4)")
	}
	p, _, _, err := learningRetractFacts(payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Committing learning_retract: learning %d — %q (proposed by ask %d, committed by user %s)\n", p.ID, p.Reason, proposalID, a.ID)
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	_, _, ref := learnings.New(st).Retract(p.ID, p.Reason, learnings.Actor{Kind: "user", ID: a.ID})
	if ref != nil {
		return fmt.Errorf("learning_retract %d: %s", p.ID, ref.Reason)
	}
	fmt.Fprintf(w, "Committed: learning %d is retracted — reason stamped, one learning_change line audited to user %s, its vectors dropped with it, with ask %d recorded on the proposal\n", p.ID, a.ID, proposalID)
	return nil
}

// validateLearningRetractProposal is the create door's entry (TD3): the
// same registry entry the commit door re-runs, reached from the one call
// site this package keeps for it.
func validateLearningRetractProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindLearningRetract), payload)
}
