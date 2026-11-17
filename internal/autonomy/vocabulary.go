// vocabulary.go — the one derived set a form never re-types (AL2, AL15).
// Every subject and condition is read from the action registry through its
// exported accessors, so a registry action added later appears in the
// vocabulary, in the outward expansion and in the effective view with no edit
// here — a test proves a new outward name is covered by an existing outward
// grant. The labels are descriptive text on top of the registry; a name with
// no label falls back to itself, so completeness never needs a second edit.
package autonomy

import (
	"github.com/cjlapao/aeon/internal/action"
)

// SubjectDescriptor is one subject a form may offer (AL15).
type SubjectDescriptor struct {
	Kind           string // "action" or "class"
	Name           string
	Class        action.EffectClass
	// Label is a plain-English noun phrase for the subject.
	Label string
	// RequiresExpiry is true for an irreversible action (a merge grant must
	// carry an expiry, AL5/OD3).
	RequiresExpiry bool
	// ForcedConditions are the conditions forced on this subject
	// (requireGates on the merge action).
	ForcedConditions []string
	// Covers is, for the outward shorthand, the expanded action names; empty
	// for an action subject.
	Covers []string
}

// ConditionDescriptor is one condition a form may render (AL15).
type ConditionDescriptor struct {
	Name      string
	Type      string
	// Evaluated is "pin" or "action" — where the condition is decided.
	Evaluated string
	Forced    bool
	Help      string
}

// OriginDescriptor is one start origin a form may name (AL15).
type OriginDescriptor struct {
	Origin Origin
	Help   string
}

// VocabularyResult is the closed set a form reads and never re-types
// (AL15).
type VocabularyResult struct {
	Subjects   []SubjectDescriptor
	Conditions []ConditionDescriptor
	Limits     Limits
	Origins    []OriginDescriptor
	States     []State
}

// labelFor is the plain-English noun phrase for a subject. A registry action
// without a phrase falls back to its own name, so adding a name to the
// registry needs no edit here (the completeness test still finds a label).
var labels = map[string]string{
	action.GitCommitAndPush:  "commit and push",
	action.ForgeOpenIssue:    "open an issue",
	action.ForgeOpenPR:       "open a pull request",
	action.ForgePostReview:   "post a review",
	action.ForgeMerge:        "merge a pull request",
	action.ForgeCloseIssue:   "close an issue",
}

// label returns the phrase for a subject, or the name itself if none.
func label(name string) string {
	if l, ok := labels[name]; ok {
		return l
	}
	return name
}

// SubjectLabel is the one accessor for a subject's plain-English phrase
// (AL15): the daemon's confirmation sentences read the words through it so
// a form and a sentence never disagree about what an action is called.
// Unknown names answer themselves — the same fallback Vocabulary uses.
func SubjectLabel(name string) string { return label(name) }

// Vocabulary builds the descriptor set from the registry (AL2/AL15). It reads
// the names and the classes through the exported accessors and never copies
// the names: a registry addition changes the result with no edit here.
func Vocabulary() VocabularyResult {
	classes := action.Classes()
	outward := []string{}
	subjects := make([]SubjectDescriptor, 0, len(classes)+1)
	for _, name := range action.Names() {
		c, ok := classes[name]
		if !ok {
			continue
		}
		if c == action.EffectLocal {
			// A local action runs without a grant; it is not a subject
			// (AL2: a grant on one is refused).
			continue
		}
		sd := SubjectDescriptor{
			Kind:           "action",
			Name:           name,
			Class:        c,
			Label:        label(name),
			RequiresExpiry: c == action.EffectIrreversible,
		}
		if c == action.EffectIrreversible {
			sd.ForcedConditions = []string{"requireGates"}
		}
		if c == action.EffectOutward {
			outward = append(outward, name)
		}
		subjects = append(subjects, sd)
	}
	// The one class shorthand, last, covering the current outward names.
	subjects = append(subjects, SubjectDescriptor{
		Kind:           "class",
		Name:           ClassShorthand,
		Class:        action.EffectOutward,
		Label:        "every outward action",
		RequiresExpiry: false,
		Covers:       outward,
	})
	return VocabularyResult{
		Subjects:   subjects,
		Conditions: conditionDescriptors(),
		Limits:     limits,
		Origins:    originDescriptors(),
		States:     []State{StateAllow, StateDeny},
	}
}

// conditionDescriptors is the closed condition set with its plain-English
// help (AL15). Help text is written here, in the one place, so a form never
// types a sentence.
func conditionDescriptors() []ConditionDescriptor {
	return []ConditionDescriptor{
		{Name: "requireGates", Type: "bool", Evaluated: "action", Forced: true,
			Help: "the merge rule (S57) must pass at the moment of the merge; the only condition a merge grant may carry, and it is forced on"},
		{Name: "requireBudget", Type: "bool", Evaluated: "action",
			Help: "the loop's governing budgets are not exhausted; on by default on every allow"},
		{Name: "labels", Type: "list", Evaluated: "pin",
			Help: "the start's label set must intersect this list; empty means any"},
		{Name: "maxPerDay", Type: "int", Evaluated: "action",
			Help: "no more than this many begun uses in the last 24 hours"},
	}
}

// originDescriptors is the closed origin set with a plain-English sentence
// each (AL15). scheduler is reserved for S27.
func originDescriptors() []OriginDescriptor {
	return []OriginDescriptor{
		{OriginSchedule, "a loop started by a schedule"},
		{OriginWebhook, "a loop started by a webhook"},
		{OriginEvent, "a loop started by an event"},
		{OriginScheduler, "a loop started by the continuous scheduler (S27)"},
	}
}

// ExpandSubject turns a subject into the action names it covers at this
// moment (AL2): an action subject is itself; the outward shorthand is the
// current set of outward action names, so a new outward action added to the
// registry is covered by an existing outward grant with no edit.
func ExpandSubject(s Subject) []string {
	if s.Kind == "class" && s.Name == ClassShorthand {
		return outwardNames()
	}
	return []string{s.Name}
}

// outwardNames is the current set of outward action names, read from the
// registry through the exported accessors.
func outwardNames() []string {
	classes := action.Classes()
	out := make([]string, 0, len(classes))
	for _, name := range action.Names() {
		if classes[name] == action.EffectOutward {
			out = append(out, name)
		}
	}
	return out
}
