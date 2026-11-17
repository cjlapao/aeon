package automationview

import (
	"github.com/cjlapao/aeon/internal/match"
)

// matchtest.go — the event form's "does this rule match this event?" panel,
// running THE REAL grammar (internal/match — the one the dispatcher claims
// through). This file adds only the per-key explanation; the verdict is the
// matcher's, and the fuzz test (external) compares pair-for-pair against
// the production path so a fork in behaviour dies in CI, not in production.

// Event is the flat sample a person pastes (or T3 builds from a real row).
type Event struct {
	Event       string
	RootID      string
	ComponentID string
	GoalType    string
	TaskType    string
	SourceKind  string
}

// KeyResult is one key's row in the explanation panel: what the rule asks,
// what the event carries, and whether it matched. Absent keys print Want
// nil (the rule did not constrain this key — it matches anything).
type KeyResult struct {
	Key  string
	Want []string
	Have string
	OK   bool
}

// MatchResult is the tester's answer.
type MatchResult struct {
	Matches bool
	PerKey  []KeyResult
	// Canonical is the rule in stored form (what a save would persist).
	Canonical string
}

// TestMatch parses and validates the rule with the grammar's own parser —
// an invalid rule returns the validator's sentence VERBATIM (the caller
// renders it as the refusal, the same words a save would answer).
func TestMatch(raw []byte, ev Event) (MatchResult, error) {
	m, err := match.ParseMatch(raw)
	if err != nil {
		return MatchResult{}, err
	}
	one := match.Event{
		Event: ev.Event, RootID: ev.RootID, ComponentID: ev.ComponentID,
		GoalType: ev.GoalType, TaskType: ev.TaskType, SourceKind: ev.SourceKind,
	}
	res := MatchResult{Matches: m.Matches(one), Canonical: m.Canonical()}
	for _, k := range []struct {
		name string
		want []string
		have string
	}{
		{"event", []string{m.Event}, ev.Event},
		{"root", m.Root, ev.RootID}, {"component", m.Component, ev.ComponentID},
		{"goal_type", m.GoalType, ev.GoalType}, {"task_type", m.TaskType, ev.TaskType},
		{"source_kind", m.SourceKind, ev.SourceKind},
	} {
		ok := len(k.want) == 0 || contains(k.want, k.have)
		res.PerKey = append(res.PerKey, KeyResult{Key: k.name, Want: k.want, Have: k.have, OK: ok})
	}
	return res, nil
}

func contains(set []string, want string) bool {
	if want == "" {
		return false // AU6: an event that never said the field does not satisfy a constrained key
	}
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
