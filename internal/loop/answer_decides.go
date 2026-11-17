package loop

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// The gate join (S38 T14, OL8): a gate that DECIDES lands the person's
// verdict on the record itself — the answer and the decision are one act,
// and the only doors this reaches the states through are DecideBrief and
// DecideGoal (Do-NOT: no other state writer). Both are claimed-FROM — the
// claim moves rows only while they still sit `in_review` — so the join is
// called AFTER the answer is durably saved and a crash-and-resume that
// re-runs it changes nothing the second time.

// DecideFromGate applies a gate answer to the entity the gate step declares
// it decides. `st` is the state as it stood AT the gate (the caller captures
// it before the answer advances the position); `by` is the answering person
// — a review is a person's act, so an author kind other than user is
// refused by the store BEFORE SQL (OL8), and this function passes that
// refusal up as the error it is.
func DecideFromGate(dep Deps, st LoopState, step blueprint.Step, answer string, by store.Actor) error {
	if step.Decides == "" {
		return nil
	}
	var decision string
	switch answer {
	case "approve":
		decision = "approved"
	case "abort":
		decision = "rejected"
	default:
		// `revise` and any other answer make NO state change: the reject
		// route sends the lane around again, and the next brief_create is
		// the next version — a revision is a new filing, never an edit of
		// the judged row.
		return nil
	}
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	reason := fmt.Sprintf("gate %s answered %s", step.Question, answer)
	switch step.Decides {
	case "brief":
		b, err := dep.Store.LatestBrief(sc)
		if err != nil {
			return fmt.Errorf("loop %s: the gate decides a brief but the lane filed none: %w", st.ID, err)
		}
		res, refusal, err := dep.Store.DecideBrief(sc, b.Version, decision, by, reason)
		if err != nil {
			return err
		}
		if refusal != nil {
			return fmt.Errorf("loop %s: the brief decision was refused: %s", st.ID, refusal.Sentence)
		}
		// res.Changed false is the idempotent leg of claimed-FROM: the row
		// was already judged (by this same answer replaying). Not an error.
		_ = res
		return nil
	case "goal":
		l, err := dep.Store.GetLoop(sc)
		if err != nil {
			return fmt.Errorf("loop %s: the gate decides a goal but the loop row is unreadable: %w", st.ID, err)
		}
		if l.GoalID == "" || l.GoalVersion == 0 {
			return fmt.Errorf("loop %s: the gate declares decides: goal but the loop was started without a goal pinned (loop.start needs goalId)", st.ID)
		}
		res, refusal, err := dep.Store.DecideGoal(l.GoalID, l.GoalVersion, decision, by, reason)
		if err != nil {
			return err
		}
		if refusal != nil {
			return fmt.Errorf("loop %s: the goal decision was refused: %s", st.ID, refusal.Sentence)
		}
		_ = res
		return nil
	default:
		// Unreachable for a validated blueprint (decides admits goal|brief
		// on gates only); refusing loudly beats deciding nothing silently.
		return fmt.Errorf("loop %s: gate declares decides:%q, which is not a vocabulary this join speaks", st.ID, step.Decides)
	}
}
