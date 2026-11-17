// validate.go — the pure validator (AL4, AL5). It returns a *Refusal (nil =
// accepted) naming the reason and the thing refused, as a discriminated
// result, never a bare error. It never mutates the input and never reads a
// clock — now is an input — so it is safe to call from a pin, a dry run or
// the socket alike. The sentences are the ones a person reads; a later
// surface prints them verbatim.
package autonomy

import (
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/action"
)

// RefusalCode is the closed set of refusal codes (AL4). A value not in the
// set is not a refusal the validator returns.
type RefusalCode string

const (
	CodeUnknownSubject    RefusalCode = "unknown-subject"
	CodeLocalSubject      RefusalCode = "local-subject"
	CodeClassNotGrantable RefusalCode = "class-not-grantable"
	CodeConditionForced   RefusalCode = "condition-forced"
	CodeExpiryRequired    RefusalCode = "expiry-required"
	CodeExpiryTooFar      RefusalCode = "expiry-too-far"
	CodeConditionInvalid  RefusalCode = "condition-invalid"
	CodeNotARoot          RefusalCode = "not-a-root"
	CodeNodeArchived      RefusalCode = "node-archived"
	CodeExpired           RefusalCode = "expired"
)

// Refusal is the one shape a refusal takes (DB19): a code and a sentence a
// person reads. The code is machine-closed; the sentence is the human half.
type Refusal struct {
	Code     RefusalCode
	Sentence string
}

// ValidateGrant is the pure validator (AL4/AL5). It returns nil when the
// grant is acceptable, or a *Refusal naming the first reason and the thing
// refused, in the order the design specifies. A deny is decided first and
// carries no conditions and no expiry; an allow is then checked against the
// node, the subject, the irreversible rules and the value bounds.
func ValidateGrant(g Grant, node NodeInfo, now time.Time) *Refusal {
	// A deny (a veto) carries no conditions and no expiry; a request that
	// tries to is refused, never silently trimmed.
	if g.State == StateDeny {
		if g.ExpiresAt != nil || !g.Conditions.empty() {
			return &Refusal{CodeConditionInvalid, "a veto (deny) carries no conditions and no expiry"}
		}
		return nil
	}
	// An allow sits on a root component only; an archived node is refused
	// before any condition (the store also enforces this — a backstop).
	if !node.IsRoot {
		return &Refusal{CodeNotARoot, fmt.Sprintf("allow grants sit on a root component; %s is not a root", g.NodeID)}
	}
	if node.Archived {
		return &Refusal{CodeNodeArchived, fmt.Sprintf("%s is archived — its grants cannot be written", g.NodeID)}
	}
	// The subject: an action name (not local) or the one class shorthand.
	switch g.Subject.Kind {
	case "action":
		c, ok := action.Classes()[g.Subject.Name]
		if !ok {
			return &Refusal{CodeUnknownSubject, fmt.Sprintf("%s is not a registry action (the registry is closed)", g.Subject.Name)}
		}
		if c == action.EffectLocal {
			return &Refusal{CodeLocalSubject, fmt.Sprintf("%s is local — it runs without a grant", g.Subject.Name)}
		}
	case "class":
		if g.Subject.Name != ClassShorthand {
			return &Refusal{CodeClassNotGrantable,
				fmt.Sprintf("the only class a grant may name is %q; %q is not grantable — an irreversible action is granted only by its own name",
					ClassShorthand, g.Subject.Name)}
		}
	default:
		return &Refusal{CodeUnknownSubject, "a subject is an action name or the outward class"}
	}
	// An irreversible action (the merge today) forces the merge-rule
	// condition and a bounded expiry; a request that turns the condition
	// off is refused, not silently corrected.
	if isIrreversible(g) {
		if !g.Conditions.RequireGates {
			return &Refusal{CodeConditionForced,
				"forge.merge must carry the merge rule (requireGates); it is forced on and cannot be turned off"}
		}
		if g.ExpiresAt == nil {
			return &Refusal{CodeExpiryRequired, "forge.merge must carry an expiry — irreversible authority is bounded and renewable"}
		}
		if g.ExpiresAt.Before(now) {
			return &Refusal{CodeExpired, fmt.Sprintf("%s — an expiry in the past is refused", g.Subject.Name)}
		}
		latest := now.AddDate(0, 0, limits.MaxMergeGrantDays)
		if g.ExpiresAt.After(latest) {
			return &Refusal{CodeExpiryTooFar, fmt.Sprintf("a merge grant may not outlive %d days from now", limits.MaxMergeGrantDays)}
		}
	} else if g.ExpiresAt != nil && g.ExpiresAt.Before(now) {
		// A non-merge grant may carry an expiry, but never one already past.
		return &Refusal{CodeExpired, fmt.Sprintf("%s — an expiry in the past is refused", g.Subject.Name)}
	}
	// The value bounds: maxPerDay ≥ 1, labels and note within their caps.
	if g.Conditions.MaxPerDay != 0 && g.Conditions.MaxPerDay < 1 {
		return &Refusal{CodeConditionInvalid, "maxPerDay must be at least 1"}
	}
	if len(g.Conditions.Labels) > limits.MaxLabels {
		return &Refusal{CodeConditionInvalid, fmt.Sprintf("a grant may carry at most %d labels", limits.MaxLabels)}
	}
	for _, l := range g.Conditions.Labels {
		if len(l) > limits.MaxLabelLen {
			return &Refusal{CodeConditionInvalid, fmt.Sprintf("a label may not exceed %d characters", limits.MaxLabelLen)}
		}
	}
	if len([]byte(g.Note)) > limits.MaxNoteBytes {
		return &Refusal{CodeConditionInvalid, fmt.Sprintf("a note may not exceed %d bytes", limits.MaxNoteBytes)}
	}
	return nil
}

// isIrreversible reports whether the grant covers an irreversible action
// (the merge today). A class subject is never irreversible, so the merge is
// granted only by its own name (AL2).
func isIrreversible(g Grant) bool {
	if g.Subject.Kind != "action" {
		return false
	}
	return action.Classes()[g.Subject.Name] == action.EffectIrreversible
}

// empty reports whether a condition set is the zero value — a deny must
// carry none.
func (c Conditions) empty() bool {
	return !c.RequireGates && !c.RequireBudget && len(c.Labels) == 0 && c.MaxPerDay == 0
}
