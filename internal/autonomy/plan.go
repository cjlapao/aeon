// plan.go — the pure resolver (AL14). Plan decides, per registry action, the
// verdict an unattended loop started now would get. It is the one
// implementation the start path (T4), the effective view (T6) and the pinned
// view read through, so the legibility view can never drift from
// enforcement: a test asserts the effective view equals what a start pins.
//
// It reads no store and no clock — now and the grants are inputs. A local
// action runs without a grant; a veto overrides everything; an allow is
// matched by action name or by the outward class expansion, must be active
// and not expired, and (at pin time) must match the start's labels. An
// irreversible action is covered only by its own name.
package autonomy

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/action"
)

// ActionPlan is one registry action's plan (AL14): the verdict an unattended
// loop started now would get, why, the grant behind it, the grant's raw
// conditions (for the pin) and its action-time statuses (for the view).
type ActionPlan struct {
	Action     string
	Class      action.EffectClass
	Verdict    action.Verdict
	Because    string
	GrantID    string
	// Conditions is the covering grant's raw conditions (carried in the pin
	// and evaluated at action time by the decision site).
	Conditions Conditions
	// Status is the action-time evaluation of those conditions (met / unmet
	// / cannot-say), for the effective and pinned views.
	Status []action.ConditionStatus
}

// PlanInput is the pure input to Plan: the origin, the start's labels, the
// active allows and denies for the root and its component, the clock and
// the seams.
type PlanInput struct {
	Origin Origin
	Labels []string
	Grants []Grant
	Now    time.Time
	Seams  Seams
}

// Plan is the pure resolver (AL14). Per registry action it returns the
// verdict an unattended loop started now would get. Local runs; a veto
// denies; an active, unexpired, label-matched allow runs (subject to its
// action-time conditions, which a nil seam makes cannot-say).
func Plan(in PlanInput) []ActionPlan {
	classes := action.Classes()
	out := make([]ActionPlan, 0, len(classes))
	for _, name := range action.Names() {
		c, ok := classes[name]
		if !ok {
			continue
		}
		ap := ActionPlan{Action: name, Class: c}
		if c == action.EffectLocal {
			// A local action runs without a grant (AL2).
			ap.Verdict = action.VerdictRun
			ap.Because = localSentence(name, c)
			out = append(out, ap)
			continue
		}
		if veto, ok := vetoFor(in.Grants, name); ok {
			// A veto overrides every allow (AL3), with or without a pin.
			ap.Verdict = action.VerdictDeny
			ap.Because = vetoSentence(veto, name)
			out = append(out, ap)
			continue
		}
		if allow, ok := allowFor(in.Grants, name, in.Labels, in.Now); ok {
			// Covered by an active, unexpired, label-matched allow.
			ap.GrantID = allow.ID
			ap.Conditions = allow.Conditions
			ap.Status = Evaluate(context.Background(), allow.Conditions, allow.ID, "", 0, in.Seams, in.Now)
			if anyStatusFails(ap.Status) {
				ap.Verdict = action.VerdictDeny
				ap.Because = conditionFailSentence(name, allow, failingStatus(ap.Status), 0)
			} else {
				ap.Verdict = action.VerdictRun
				ap.Because = runSentence(name, allow, 0)
			}
			out = append(out, ap)
			continue
		}
		ap.Verdict = action.VerdictDeny
		ap.Because = noGrantSentence(name, c)
		out = append(out, ap)
	}
	return out
}

// PinFrom builds the pin's allowed snapshot from a plan (AL8): only the
// covered actions (an allow applied and matched), each expanded to its
// action name with its raw conditions carried for the action-time check.
// The caller assembles the full Pin (Rev, Origin, Labels) around this.
func PinFrom(plan []ActionPlan) []PinnedGrant {
	var allowed []PinnedGrant
	for _, ap := range plan {
		if ap.GrantID == "" {
			continue
		}
		allowed = append(allowed, PinnedGrant{
			GrantID:    ap.GrantID,
			Subject:    ap.Action, // already the expanded action name
			Conditions: ap.Conditions,
		})
	}
	return allowed
}

// labelsMatch reports whether a grant's label set intersects the start's
// labels (AL4). An empty grant label set means any — it always matches.
func labelsMatch(grant, start []string) bool {
	if len(grant) == 0 {
		return true
	}
	for _, g := range grant {
		for _, s := range start {
			if g == s {
				return true
			}
		}
	}
	return false
}

// subjectCovers reports whether a subject covers an action name, by name or
// by the outward class expansion (AL2).
func subjectCovers(s Subject, actionName string) bool {
	for _, n := range ExpandSubject(s) {
		if n == actionName {
			return true
		}
	}
	return false
}

// vetoFor finds the first deny covering the action (AL3). Denies may sit on
// a root or a child, so any active deny covering the action wins.
func vetoFor(grants []Grant, name string) (Grant, bool) {
	for _, g := range grants {
		if g.State != StateDeny || !g.Active() {
			continue
		}
		if subjectCovers(g.Subject, name) {
			return g, true
		}
	}
	return Grant{}, false
}

// allowFor finds the first active allow covering the action that is not
// expired and (at pin time) label-matched (AL3/AL4/AL7). Allows sit on a
// root only; the store enforces that, and the plan trusts the set it is
// given. An irreversible action is covered only by its own name, never by
// the outward class (ExpandSubject returns only outward names).
func allowFor(grants []Grant, name string, startLabels []string, now time.Time) (Grant, bool) {
	for _, g := range grants {
		if g.State != StateAllow || !g.Active() {
			continue
		}
		if !subjectCovers(g.Subject, name) {
			continue
		}
		if g.ExpiresAt != nil && g.ExpiresAt.Before(now) {
			continue
		}
		if !labelsMatch(g.Conditions.Labels, startLabels) {
			continue
		}
		return g, true
	}
	return Grant{}, false
}

// anyStatusFails reports whether a condition set has a non-met status (AL4:
// cannot-say is never met).
func anyStatusFails(status []action.ConditionStatus) bool {
	for _, s := range status {
		if s.State != action.CondMet {
			return true
		}
	}
	return false
}

// failingStatus returns the first non-met status, or a zero-value one if
// none (defensive — reached only when anyStatusFails was not consulted).
func failingStatus(status []action.ConditionStatus) action.ConditionStatus {
	for _, s := range status {
		if s.State != action.CondMet {
			return s
		}
	}
	return action.ConditionStatus{Name: "(none)", State: action.CondUnmet}
}
