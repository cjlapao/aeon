package store

// trigger_match.go — S40 AU6's grammar now LIVES in `internal/match` (moved
// verbatim 2026-11-03 by S49 T2 so the store-free read side can run the real
// matcher); what remains here is the thin delegator the S40 suites keep
// calling unedited. The one grammar, one truth: this file adds no rule —
// ParseMatch converts bytes and struct shape, the methods forward, and the
// only translation that happens is store.AutomationEvent's six matching
// fields into match.Event. store.Match and match.Match keep IDENTICAL field
// sets (names, types, order): the conversions below are plain Go struct
// conversions, so a field renamed on one side is a compile error here, not
// a silent drop.

import (
	"github.com/cjlapao/aeon/internal/match"
)

// Match is a parsed trigger rule. Each list is sorted and de-duplicated;
// an empty list means the key was absent, which matches anything.
type Match struct {
	Event                                           string
	Root, Component, GoalType, TaskType, SourceKind []string
}

// ParseMatch reads the JSON form; every refusal sentence is the grammar's,
// verbatim (internal/match owns them).
func ParseMatch(raw []byte) (Match, error) {
	m, err := match.ParseMatch(raw)
	if err != nil {
		return Match{}, err
	}
	return Match(m), nil
}

// Validate is the rule table, run at parse AND again on every write.
func (m Match) Validate() error { return match.Match(m).Validate() }

// Canonical is the stored JSON form — fixed key order, omitempty.
func (m Match) Canonical() string { return match.Match(m).Canonical() }

// Matches is the evaluator (AU6): AND across keys, OR within one.
func (m Match) Matches(ev AutomationEvent) bool {
	return match.Match(m).Matches(match.Event{
		Event: ev.Event, RootID: ev.RootID, ComponentID: ev.ComponentID,
		GoalType: ev.GoalType, TaskType: ev.TaskType, SourceKind: ev.SourceKind,
	})
}

// ContainsValue is the membership test the fire-decision validators use.
// (S49 T2: the matcher's own copies moved with the grammar; this one serves
// triggers.go's closed vocabulary checks and stays here, next to them.)
func containsKey(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
