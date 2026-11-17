// Package match is S40 AU6's closed match grammar — the same code the store
// has always run, moved verbatim out of internal/store on 2026-11-03 by
// S49 T2 so the store-free read side (internal/automationview) can run THE
// real matcher and the validator's own applicability rules without
// importing the store (S49's layering rule: automationview never touches
// the store; and the design forbids a second matcher as firmly as it
// forbids restating the grammar). internal/store keeps thin delegators over
// these functions — the S40 suites compile and pass unedited, which is the
// proof the extraction is behaviour-preserving.
//
// The rules themselves are unchanged and restated nowhere else: six keys,
// no operators, no patterns: a match value is a LITERAL, and a person
// writing "b*" means the literal string "b*" — the day this file grows a
// regex is the day trigger authors have to reason about two languages at
// once (AU6's argument, kept short because the grammar is).
package match

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Match is a parsed trigger rule. Each list is sorted and de-duplicated;
// an empty list means the key was absent, which matches anything. The
// field set is store.Match's field set exactly: Go struct conversion
// between the two ignores only tags, and internal/store's delegators lean
// on that identity; a renamed or retyped field breaks their compile.
type Match struct {
	Event                                           string
	Root, Component, GoalType, TaskType, SourceKind []string
}

// Event is the subject of a match: the fields AU6's keys test. It is
// store.AutomationEvent's matching subset under the same field names, for
// the same conversion reason.
type Event struct {
	Event                                               string
	RootID, ComponentID, GoalType, TaskType, SourceKind string
}

// matchKeys is the whole grammar. Adding a key here without widening
// Validate, Canonical and Matches in the same change is how a rule the
// evaluator forgets gets authored — this slice is the only place to widen.
var matchKeys = []string{"event", "root", "component", "goal_type", "task_type", "source_kind"}

// MatchKeyNames is the grammar's key list, in grammar order — the vocabulary
// read (S49 AN13) renders forms from this list rather than restating it.
func MatchKeyNames() []string { return append([]string{}, matchKeys...) }

// The closed set (AU3): goal.approved, task.created, task.approved,
// task.completed. Nothing else is an automation event — rejected, parked and
// every edit are silent, and loop tasks are absent by rule, not by omission.
// TestTheClosedEventSetMatchesTheMigrationCheck (internal/store) holds this
// list and 0039's CHECK to the same four names.
const (
	EventGoalApproved  = "goal.approved"
	EventTaskCreated   = "task.created"
	EventTaskApproved  = "task.approved"
	EventTaskCompleted = "task.completed"
)

// Events is the closed set (AU3), in migration order.
func Events() []string {
	return []string{EventGoalApproved, EventTaskCreated, EventTaskApproved, EventTaskCompleted}
}

// IsEvent is the membership question Events answers.
func IsEvent(e string) bool {
	for _, v := range Events() {
		if v == e {
			return true
		}
	}
	return false
}

// GoalTypes is OL14's closed five — the set goal.type and task.type share,
// and the set match's task_type key tests against. internal/store/actor.go
// derives allGoalTypes from this map, so the five words exist once.
var GoalTypes = map[string]bool{
	"bug": true, "feature": true, "chore": true, "refactor": true, "investigation": true,
}

// GoalTypeWords lists the five in the order the validator's sentence names
// them (bug, feature, chore, refactor, investigation).
func GoalTypeWords() []string {
	return []string{"bug", "feature", "chore", "refactor", "investigation"}
}

// ParseMatch reads the JSON form. Every refusal is one sentence naming the
// key it refused, which is what makes a rejected create editable rather than
// a re-authoring job.
func ParseMatch(raw []byte) (Match, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return Match{}, fmt.Errorf("match: must be a JSON object: %v", err)
	}
	for k := range m {
		if !containsKey(matchKeys, k) {
			return Match{}, fmt.Errorf("match: unknown key %q (allowed: %s)", k, strings.Join(matchKeys, ", "))
		}
	}
	// CT11: the "project" key is gone — the trigger matches the tree's root,
	// not a group. A stored rule that still carries it is refused with one
	// sentence, not a silent no-op (DB19).
	if _, ok := m["project"]; ok {
		return Match{}, fmt.Errorf(`match: the "project" key is gone — CT11 matches on "root" (the tree's root), not the old project group`)
	}
	out := Match{}
	evRaw, ok := m["event"]
	if !ok {
		return Match{}, fmt.Errorf(`match: missing required key "event"`)
	}
	if err := json.Unmarshal(evRaw, &out.Event); err != nil {
		return Match{}, fmt.Errorf(`match: "event" must be a single string: %v`, err)
	}
	var err error
	if out.Root, err = strSet(m, "root"); err != nil {
		return Match{}, err
	}
	if out.Component, err = strSet(m, "component"); err != nil {
		return Match{}, err
	}
	if out.GoalType, err = strSet(m, "goal_type"); err != nil {
		return Match{}, err
	}
	if out.TaskType, err = strSet(m, "task_type"); err != nil {
		return Match{}, err
	}
	if out.SourceKind, err = strSet(m, "source_kind"); err != nil {
		return Match{}, err
	}
	if err := out.Validate(); err != nil {
		return Match{}, err
	}
	return out, nil
}

// strSet reads one optional key as "x" or ["x","y"]. Numbers, bools,
// objects and null are refusals, not zero values — a match nobody meant is
// worse than a match refused (DB19's discrimination, applied to authoring).
func strSet(m map[string]json.RawMessage, key string) ([]string, error) {
	raw, ok := m[key]
	if !ok {
		return nil, nil
	}
	// json.Unmarshal would let null through as a silent no-op on both
	// attempts below — checked first, so null is a refusal, never a zero.
	if string(raw) == "null" {
		return nil, fmt.Errorf("match: %q must be a string or a list of strings", key)
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return normaliseValues(key, []string{one})
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return normaliseValues(key, many)
	}
	return nil, fmt.Errorf("match: %q must be a string or a list of strings", key)
}

// normaliseValues checks the value rules and returns the sorted,
// de-duplicated set. Values are LITERALS: nothing here trims — " bug" is a
// different literal than "bug", and trimming would make the author's quotes
// mean something the evaluator does not enforce.
func normaliseValues(key string, vals []string) ([]string, error) {
	if len(vals) == 0 {
		return nil, fmt.Errorf("match: %q must hold at least one value (an empty list matches nothing, which is surely not what was meant)", key)
	}
	if len(vals) > 16 {
		return nil, fmt.Errorf("match: %q holds %d values, the limit is 16", key, len(vals))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v == "" {
			return nil, fmt.Errorf("match: %q holds an empty string — an empty value matches no event's field", key)
		}
		if len(v) > 128 {
			return nil, fmt.Errorf("match: %q holds a %d-character value, the limit is 128", key, len(v))
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Validate is the rule table, run at parse AND again on every write — a
// hand-built Match must clear the same bar as a parsed one.
func (m Match) Validate() error {
	if !IsEvent(m.Event) {
		return fmt.Errorf("match: event %q is not an automation event (the set is %s)", m.Event, strings.Join(Events(), ", "))
	}
	goal := strings.HasPrefix(m.Event, "goal.")
	if (len(m.GoalType) > 0 || len(m.SourceKind) > 0) && !goal {
		which := "goal_type"
		if len(m.GoalType) == 0 {
			which = "source_kind"
		}
		return fmt.Errorf("match: %q applies only to goal.* events and %q is not one", which, m.Event)
	}
	if len(m.TaskType) > 0 && !strings.HasPrefix(m.Event, "task.") {
		return fmt.Errorf("match: \"task_type\" applies only to task.* events and %q is not one", m.Event)
	}
	for _, v := range m.TaskType {
		if !GoalTypes[v] {
			return fmt.Errorf("match: task_type %q is not one of bug, feature, chore, refactor, investigation (OL14's five)", v)
		}
	}
	// Values that did not come through ParseMatch still obey the shape.
	// An ABSENT key (nil list) is not a list of zero values — it is the
	// key not being there, which is what "matches anything" means.
	for key, vals := range map[string][]string{
		"root": m.Root, "component": m.Component, "goal_type": m.GoalType,
		"task_type": m.TaskType, "source_kind": m.SourceKind,
	} {
		if len(vals) == 0 {
			continue
		}
		if _, err := normaliseValues(key, vals); err != nil {
			return err
		}
	}
	return nil
}

// canonicalMatch is the stored shape: fixed field order (the six keys, in
// grammar order) with omitempty so an absent key is absent bytes. Two
// authors, one rule, one byte string — that is what makes a stored match
// diffable and the UNIQUE(name) the only uniqueness the table needs.
type canonicalMatch struct {
	Event      string   `json:"event"`
	Root       []string `json:"root,omitempty"`
	Component  []string `json:"component,omitempty"`
	GoalType   []string `json:"goal_type,omitempty"`
	TaskType   []string `json:"task_type,omitempty"`
	SourceKind []string `json:"source_kind,omitempty"`
}

// Canonical renders the stored JSON form.
func (m Match) Canonical() string {
	b, err := json.Marshal(canonicalMatch{
		Event: m.Event, Root: m.Root, Component: m.Component,
		GoalType: m.GoalType, TaskType: m.TaskType, SourceKind: m.SourceKind,
	})
	if err != nil { // struct of strings: marshal cannot fail on this shape
		return ""
	}
	return string(b)
}

// Matches is the evaluator the dispatcher runs: AND across keys, OR within
// one. A key present in the match demands the event's field be NON-EMPTY
// and in the set (AU6) — an event that never said which root it belongs
// to does not satisfy a root-scoped trigger; absence is absence.
func (m Match) Matches(ev Event) bool {
	if ev.Event != m.Event {
		return false
	}
	for _, kv := range []struct {
		set   []string
		field string
	}{
		{m.Root, ev.RootID},
		{m.Component, ev.ComponentID},
		{m.GoalType, ev.GoalType},
		{m.TaskType, ev.TaskType},
		{m.SourceKind, ev.SourceKind},
	} {
		if len(kv.set) == 0 {
			continue
		}
		if kv.field == "" || !containsKey(kv.set, kv.field) {
			return false
		}
	}
	return true
}

func containsKey(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
