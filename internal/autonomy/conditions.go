// conditions.go — the action-time condition evaluator and its seams (AL4).
// A condition whose seam is unwired, errors, or answers "cannot say" is
// cannot-say, never met — the slice fails closed while a neighbour is
// unwired, so a merge grant is inert until S57 exists. The Gates seam's
// sentence is carried verbatim into the deny, so the operator reads S57's
// words, not a paraphrase. Labels are a pin-time condition and are not
// decided here.
package autonomy

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/action"
)

// Answer is one seam's verdict: met, unmet or cannot-say (AL4). cannot-say
// is never met — a seam that is unwired, errors or cannot answer is a
// failure, not a pass. Sentence is the human half, carried verbatim.
type Answer struct {
	State    action.ConditionState
	Sentence string
}

// AnswerMet is a passing seam verdict.
func AnswerMet(s string) Answer { return Answer{State: action.CondMet, Sentence: s} }

// AnswerUnmet is a failing seam verdict.
func AnswerUnmet(s string) Answer { return Answer{State: action.CondUnmet, Sentence: s} }

// AnswerCannotSay is the third state: the seam could not answer (AL4).
func AnswerCannotSay(s string) Answer { return Answer{State: action.CondCannotSay, Sentence: s} }

// Seams are the cross-slice conditions, injected as functions (AL4, R1, R2).
// A nil seam is cannot-say. The daemon wires them when the neighbours land;
// until then a merge grant is inert, by design.
type Seams struct {
	// Gates is S57's merge rule (R2); its sentence is carried verbatim.
	Gates func(ctx context.Context, loopID string, pr int) Answer
	// Budget is S55's budget seam (R1): the loop's governing budgets are not
	// exhausted.
	Budget func(ctx context.Context, loopID string) Answer
	// Uses counts begun uses of a grant since `since`; it backs maxPerDay.
	Uses func(ctx context.Context, grantID string, since time.Time) (int, error)
}

// Evaluate decides the action-time conditions of a grant (AL4). It never
// means "met" for a nil or erroring seam. Labels are a pin-time condition
// and are not decided here; the grant id, loop id and pr are the inputs the
// seams need. The returned statuses are filled into the decision site's
// LiveState, which ResolveWithGrant reads.
func Evaluate(ctx context.Context, c Conditions, grantID, loopID string, pr int, s Seams, now time.Time) []action.ConditionStatus {
	var out []action.ConditionStatus

	// requireGates: the merge rule, forced on the merge grant.
	if c.RequireGates {
		if s.Gates == nil {
			out = append(out, action.ConditionStatus{
				Name: "requireGates", State: action.CondCannotSay,
				Sentence: "the merge rule (S57) could not be asked — the verification service is not wired"})
		} else {
			a := s.Gates(ctx, loopID, pr)
			out = append(out, action.ConditionStatus{Name: "requireGates", State: a.State, Sentence: a.Sentence})
		}
	}
	// requireBudget: on by default on every allow; the budget service is S55.
	if c.RequireBudget {
		if s.Budget == nil {
			out = append(out, action.ConditionStatus{
				Name: "requireBudget", State: action.CondCannotSay,
				Sentence: "the budget could not be asked — the budget service is not wired"})
		} else {
			a := s.Budget(ctx, loopID)
			out = append(out, action.ConditionStatus{Name: "requireBudget", State: a.State, Sentence: a.Sentence})
		}
	}
	// maxPerDay: the count of begun uses in the last 24 h. A nil or erroring
	// seam is cannot-say (AL4), never met.
	if c.MaxPerDay > 0 {
		if s.Uses == nil {
			out = append(out, action.ConditionStatus{
				Name: "maxPerDay", State: action.CondCannotSay,
				Sentence: "the use count could not be asked — the use log is not wired"})
		} else {
			n, err := s.Uses(ctx, grantID, now.Add(-24*time.Hour))
			if err != nil {
				out = append(out, action.ConditionStatus{
					Name: "maxPerDay", State: action.CondCannotSay,
					Sentence: "the use count could not be read"})
			} else if n >= c.MaxPerDay {
				out = append(out, action.ConditionStatus{
					Name: "maxPerDay", State: action.CondUnmet,
					Sentence: fmt.Sprintf("the limit of %d per day is reached (%d begun in the last 24 h)", c.MaxPerDay, n)})
			} else {
				out = append(out, action.ConditionStatus{Name: "maxPerDay", State: action.CondMet, Sentence: ""})
			}
		}
	}
	return out
}
