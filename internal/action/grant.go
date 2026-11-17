// grant.go — S56's decision site (AL9). A pure wrapper over Resolve that
// lets a person-authored, PINNED grant lift an unattended outward or
// irreversible action, and lets a LIVE fact (a revocation, a veto, an
// expiry, an unmet condition) pull it back — ceiling pinned, floor live.
//
// This file is a sibling of class.go and never touches it. class.go's
// classes map stays the only definition of a class with no setter and no
// configuration path; the grep that proves it (TestClassesAreNotConfigurable)
// walks this file too, so it carries none of the words that would read as a
// way to widen the set: no environment read, no class setter, no registry
// registration, no declarative file format. The only new thing here is a
// decision that a pin plus a live read may make; it adds nothing to the
// registry and changes no sentence Resolve already returns.
//
// The wrapper is a pure function of its GrantContext. A zero context
// (GrantContext{}) returns exactly Resolve's result for every action ×
// attended × allowOutward — that equivalence is the whole safety argument,
// and a test asserts it across the matrix. Nothing here reads a clock, the
// store, or the network; the loop package fills the struct from rows.
package action

import "fmt"

// ConditionState is the three-state result of evaluating one condition at
// action time (AL4). cannot-say is never met: a condition whose seam is
// unwired, errors, or answers "cannot say" is a failure, not a pass.
type ConditionState string

const (
	CondMet       ConditionState = "met"
	CondUnmet     ConditionState = "unmet"
	CondCannotSay ConditionState = "cannot-say"
)

// ConditionStatus is one action-time condition's verdict for the covering
// grant, as the loop package fills it from autonomy.Evaluate. Sentence is
// the human sentence the condition carries (from autonomy's one formatter);
// the loop package has already rendered it, so this file only joins.
type ConditionStatus struct {
	Name     string
	State    ConditionState
	Sentence string
}

// LiveState is the live facts about the pinned grant that covers this
// action, filled by the loop package from the store (AL9). It carries the
// facts ResolveWithGrant needs to decide; it never carries a verdict. A
// veto is checked before the pin because it applies with or without a pin
// and even when the person typed the flag (AL3/AL7).
type LiveState struct {
	// Vetoed is a deny row covering the action on the loop's component or
	// its root; VetoID names the row in the sentence.
	Vetoed  bool
	VetoID  string
	// Revoked is the covering grant being revoked; RevokedBy/RevokedAt name
	// who and when, in the sentence.
	Revoked   bool
	RevokedBy string
	RevokedAt string
	// Expired is the covering grant's expiry having passed the store clock.
	Expired bool
	// Conditions are the covering grant's action-time conditions, each with
	// its three-state verdict and its sentence.
	Conditions []ConditionStatus
	// GrantID and Rev name the covering grant and the revision the loop was
	// pinned at; both appear in every sentence this file builds for a
	// pinned grant.
	GrantID string
	Rev     int64
}

// GrantContext is the decision input a loop hands ResolveWithGrant (AL9).
// Pinned says a pin row exists for the loop; InPin says THIS action is in
// the pinned snapshot; Live carries the live facts for the covering grant.
// A zero context (Pinned false, InPin false, Live empty) decides exactly as
// Resolve would — the byte-identical-with-no-grants guarantee.
type GrantContext struct {
	Pinned bool
	InPin  bool
	Live   LiveState
}

// ResolveWithGrant is the one decision door (AL9). It computes base :=
// Resolve(name, attended, allowOutward) first, so an unknown action returns
// its error unchanged and every existing sentence is untouched, then layers
// the grant rules on top. The rules, in order:
//
//	1. local or attended  ⇒ base (the slice is unattended autonomy, not a
//	   local run or an attended ask — OD2).
//	2. unattended, non-local, vetoed  ⇒ deny, naming the veto's grant id —
//	   with or without a pin and even when the flag was typed (AL3/AL7).
//	3. no pin, or the action is not in the snapshot  ⇒ base (today, exactly).
//	4. pinned and covered, but a live check failed (revoked, expired, or a
//	   condition unmet or cannot-say)  ⇒ deny, naming the grant, the failure
//	   and the pinned revision.
//	5. pinned, covered, every live check passed  ⇒ run — the only path by
//	   which an unattended irreversible action runs; the sentence names the
//	   grant and the revision.
func ResolveWithGrant(name string, attended, allowOutward bool, g GrantContext) (CheckResult, error) {
	base, err := Resolve(name, attended, allowOutward)
	if err != nil {
		return CheckResult{}, err
	}
	// Rule 1: a local action runs and an attended loop asks, exactly as
	// Resolve decides — a veto never touches either (AL3/OD2).
	if base.Class == EffectLocal || attended {
		return base, nil
	}
	// Rule 2: a veto overrides everything below it — pin or not, flag or
	// not — so a narrowing is always seen (AL6). It is checked before the
	// pin so a vetoed action is denied even with no pin row.
	if g.Live.Vetoed {
		return CheckResult{
			Action:  name,
			Class:   base.Class,
			Verdict: VerdictDeny,
			Sentence: VetoSentence(name, g.Live.VetoID),
		}, nil
	}
	// Rule 3: no pin, or this action is not in the pinned snapshot, is
	// today's behaviour exactly — the base verdict stands.
	if !g.Pinned || !g.InPin {
		return base, nil
	}
	// Rule 4: a pinned-and-covered action still needs every live check to
	// pass. A revocation, an expiry, or a condition that is unmet or
	// cannot-say denies, naming the grant, the failure and the pinned
	// revision (AL6: narrowing always reaches the next action).
	if g.Live.Revoked || g.Live.Expired || conditionFails(g.Live.Conditions) {
		return CheckResult{
			Action:  name,
			Class:   base.Class,
			Verdict: VerdictDeny,
			Sentence: liveFailureSentence(base, g.Live),
		}, nil
	}
	// Rule 5: pinned, covered, and every live check passed — the only way
	// an unattended irreversible (or an outward) action runs. The sentence
	// names the grant and the revision it ran under (AL8).
	return CheckResult{
		Action:  name,
		Class:   base.Class,
		Verdict: VerdictRun,
		Sentence: fmt.Sprintf("%s · %s · run — grant %s (rev %d) at the loop's pin", name, base.Class, g.Live.GrantID, g.Live.Rev),
	}, nil
}

// conditionFails reports whether any action-time condition is not met. A
// single unmet or cannot-say condition is a failure (AL4: cannot-say is
// never met). An empty set passes — the grant may carry no conditions.
func conditionFails(conds []ConditionStatus) bool {
	for _, c := range conds {
		if c.State != CondMet {
			return true
		}
	}
	return false
}

// VetoSentence is the one sentence a veto produces on an action: it names
// the action and the veto's grant id (AL3). The decision site speaks it
// here; the pure core's one formatter (internal/autonomy's sentences.go)
// renders the same words through THIS builder. The import direction — the
// core wraps the registry, never the reverse — puts the shared source in
// the lower package, and a second copy of the template would be a second
// truth (AL17).
func VetoSentence(name, vetoID string) string {
	return fmt.Sprintf("%s denied — grant %s vetoes it (a veto overrides every grant on this component and its root)", name, vetoID)
}

// RevokedClause is the one clause a revocation renders: on which grant,
// when and by whom. The decision site embeds it in its deny; the live view
// makes it the whole sentence. One builder, two call sites, so the words
// the loop is told and the words the panel shows cannot drift (AL17).
func RevokedClause(grantID, at, by string) string {
	return fmt.Sprintf("grant %s was revoked at %s by %s", grantID, at, by)
}

// ExpiredClause is the one clause an expiry renders (AL5), with the same
// two call sites as RevokedClause.
func ExpiredClause(grantID string) string {
	return fmt.Sprintf("grant %s expired", grantID)
}

// liveFailureSentence names the grant, the specific failure and the pinned
// revision. A revocation, an expiry and a failing condition each get their
// own clause so the operator learns which door said no and why.
func liveFailureSentence(base CheckResult, l LiveState) string {
	var clause string
	switch {
	case l.Revoked:
		clause = RevokedClause(l.GrantID, l.RevokedAt, l.RevokedBy)
	case l.Expired:
		clause = ExpiredClause(l.GrantID)
	default:
		c := failingCondition(l.Conditions)
		// The condition's own sentence (the seam's words — e.g. S57's merge
		// rule, or "could not be asked" for a nil seam) is carried verbatim
		// when present: the operator reads the neighbour's sentence, not a
		// paraphrase (AL4). A condition with no sentence falls back to the
		// bare name+state.
		if c.Sentence != "" {
			clause = fmt.Sprintf("grant %s's condition %s is %s — %s", l.GrantID, c.Name, c.State, c.Sentence)
		} else {
			clause = fmt.Sprintf("grant %s's condition %s is %s", l.GrantID, c.Name, c.State)
		}
	}
	return fmt.Sprintf("%s denied — %s — the loop was pinned at rev %d", base.Action, clause, l.Rev)
}

// failingCondition returns the first condition that is not met. A revocation
// or expiry is checked before the conditions, so this is reached only when
// the grant is live but a condition fails.
func failingCondition(conds []ConditionStatus) ConditionStatus {
	for _, c := range conds {
		if c.State != CondMet {
			return c
		}
	}
	return ConditionStatus{Name: "(none)", State: CondUnmet, Sentence: "no condition failed"}
}
