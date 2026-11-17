// resume.go — S61 T6 (GI13): the ledger walk. A recorded `done` step is
// VERIFIED, never re-run — and a verify mismatch refuses rather than
// repairs (redoing over unexpected state would overwrite what a stray hand
// planted, GI9's forbidden repair). A `failed` step re-runs; an absent one
// (the crash left no row, or the run never reached it) runs. That is the
// whole resume law, and it is why one Commit and a hundred resumptions
// arrive at the same state.
package intake

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

// Commit runs (or resumes) the intake commit as the person who confirmed
// the digest. It refuses before any write when: the engine is unwired, the
// actor is not a person (GI6 — no agent runs this door), the plan is not
// the open plan of this intake, the re-validated plan says a different
// digest than the one carried (the plan changed since you saw it), or
// publish was answered without the owner or public confirmation its answer
// needs. After that it is the ledger walk, and then the two words of
// finishing: the proposal is marked applied with the COMMITTER as its
// decider, and the intake loop is closed if the daemon's seam says idle.
func (e *Engine) Commit(ctx context.Context, in CommitInput, actor store.Actor) (Result, error) {
	if e.Store == nil || e.Actions == nil || e.Charter == nil || e.Git == nil {
		return Result{}, fmt.Errorf("intake refuses: the engine is not fully wired")
	}
	if actor.Kind != "user" || actor.ID == "" {
		return Result{}, fmt.Errorf("intake refuses: only a person commits an intake — %q is not one (GI6)", actor.Kind)
	}
	if _, err := e.Store.GetIntake(in.IntakeID); err != nil {
		return Result{}, err
	}
	plan, prop, err := e.loadPlan(ctx, in)
	if err != nil {
		return Result{}, err
	}
	pv, err := e.previewFor(ctx, in, plan)
	if err != nil {
		return Result{}, err
	}
	fresh := e.digestFor(pv, desk.ProposalPayloadDigest(prop.Payload))
	if in.Digest == "" || in.Digest != fresh {
		return Result{}, fmt.Errorf("the plan changed since you saw it — review again (seen %q, now %s)", in.Digest, fresh)
	}
	body, err := charter.Parse(plan.Charter)
	if err != nil {
		return Result{}, err
	}
	r := &run{
		e: e, ctx: ctx, in: in, plan: plan, pv: pv, actor: actor, body: body,
		componentID: map[string]string{},
		out:         Result{IntakeID: in.IntakeID, Digest: fresh},
	}

	steps := r.buildSteps()
	ledger, err := e.Store.IntakeSteps(in.IntakeID)
	if err != nil {
		return r.out, err
	}
	// doneEver is the resume law's edge a plain "last row wins" would get
	// wrong: a step that reached `done` ONCE stays a verify-step forever —
	// a later `failed` row on it is a failed EVIDENCE CHECK (a stray hand
	// moved something), and the law for that is refuse, never repair. A
	// `failed` with no `done` under it is an ordinary unfinished step and
	// re-runs.
	doneEver := map[string]bool{}
	skippedEver := map[string]bool{}
	for _, row := range ledger {
		switch row.Outcome {
		case "done":
			doneEver[row.Step] = true
		case "skipped":
			skippedEver[row.Step] = true
		}
	}

	for _, st := range steps {
		if e.Hook != nil {
			if err := e.Hook(st.Name, "before"); err != nil {
				return r.out, err // a crash: nothing recorded, the step stays absent
			}
		}
		switch {
		case doneEver[st.Name]:
			if _, err := st.Verify(); err != nil {
				_ = e.recordStep(in.IntakeID, st.Name, "failed", fmt.Sprintf("recorded done, and the check refuses: %s", err))
				return r.out, fmt.Errorf("intake cannot continue: step %s is recorded done and its evidence does not hold — %s", st.Name, err)
			}
		case skippedEver[st.Name]:
			// the ledger already said why; nothing re-checks a non-event.
		default: // absent ⇒ run; failed ⇒ re-run — the ledger's word is history, not a verdict.
			if st.Skip != "" {
				if err := e.recordStep(in.IntakeID, st.Name, "skipped", st.Skip); err != nil {
					return r.out, err
				}
			} else {
				detail, err := st.Do()
				if err != nil {
					_ = e.recordStep(in.IntakeID, st.Name, "failed", fmt.Sprintf("step %s failed: %s", st.Name, err))
					r.out.FailedStep = st.Name
					return r.out, fmt.Errorf("intake stopped at %s: %s — the ledger says exactly where; the next run resumes from there", st.Name, err)
				}
				if err := e.recordStep(in.IntakeID, st.Name, "done", detail); err != nil {
					return r.out, err
				}
			}
		}
		if e.Hook != nil {
			if err := e.Hook(st.Name, "after"); err != nil {
				return r.out, err
			}
		}
	}

	// Every step done or verified. The proposal is marked applied through
	// the store's own decide (the desk's claim-guarded transition, asked by
	// the daemon seam this engine stands behind — no new decide logic), and
	// the decider is the COMMITTER, not the proposing agent: the person
	// confirmed this digest (Behaviour 6). The failure of this write leaves
	// the commit complete and the next run closing the same loop.
	if err := e.Store.DecideProposal(in.ProposalID, "committed", store.Actor{Kind: "user", ID: actor.ID}, "", e.Store.Now()); err != nil {
		r.out.FailedStep = "mark-proposal"
		return r.out, fmt.Errorf("intake committed; marking proposal %d applied failed: %w — the ledger is complete and the next run will finish the mark", in.ProposalID, err)
	}
	if e.CloseLoop != nil {
		if err := e.CloseLoop(ctx); err != nil {
			// A committed intake is not un-committed by a loop that stayed
			// open; the answer says so plainly instead of pretending.
			r.out.Warning = fmt.Sprintf("the interview loop could not be closed: %s", err)
		}
	}
	if r.out.RootID == "" { // a full resume after link never re-sets it in Do
		if inRow, err := e.Store.GetIntake(in.IntakeID); err == nil {
			r.out.RootID = inRow.RootID
		}
	}
	if r.out.Steps, err = e.Store.IntakeSteps(in.IntakeID); err != nil {
		return r.out, err
	}
	return r.out, nil
}

// recordStep appends one ledger row, keeping the store's own refusals as
// errors (a ledger row that would be malformed says so here rather than
// silently losing the word the whole resume law lives on).
func (e *Engine) recordStep(intakeID, step, outcome, detail string) error {
	if len(detail) > 512 { // the fixed templates are short; a runaway error string says the
		detail = detail[:480] + "… (detail truncated)" // ledger stays a table, not a dump
	}
	_, ref, err := e.Store.AppendIntakeStep(intakeID, step, outcome, detail)
	if err != nil {
		return err
	}
	if ref != nil {
		return fmt.Errorf("ledger refused: %s", ref.Sentence)
	}
	return nil
}
