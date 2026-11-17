package store

// trigger_match_test.go — S40 T3: the grammar AU6 closed, tested as pure
// Go. No store, no clock, no SQL: a match is bytes in and a verdict out,
// which is exactly why it can be trusted to run inside the dispatcher's
// claim loop without a database round trip.

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) Match {
	t.Helper()
	m, err := ParseMatch([]byte(raw))
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return m
}

func TestParseMatchAcceptsTheFiveKeysAsStringOrList(t *testing.T) {
	a := mustParse(t, `{"event":"goal.approved","goal_type":"bug"}`)
	b := mustParse(t, `{"event":"goal.approved","goal_type":["bug"]}`)
	if a.Canonical() != b.Canonical() {
		t.Errorf("scalar and one-element list canonicalise differently:\n %s\n %s", a.Canonical(), b.Canonical())
	}
	c := mustParse(t, `{"event":"goal.approved","goal_type":["b","a","a"]}`)
	d := mustParse(t, `{"event":"goal.approved","goal_type":["a","b"]}`)
	if c.Canonical() != d.Canonical() {
		t.Errorf("order and duplicates leak into canonical form:\n %s\n %s", c.Canonical(), d.Canonical())
	}
	if !strings.Contains(c.Canonical(), `["a","b"]`) {
		t.Errorf("canonical %s does not show the sorted de-duplicated set", c.Canonical())
	}
}

func TestParseMatchRefusesWhatTheGrammarDoesNotHave(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{`{"event":"goal.approved","labels":"x"}`, `unknown key "labels"`},
		{`{}`, `missing required key "event"`},
		{`{"event":"goal.rejected"}`, `not an automation event`},
		{`{"event":"goal.approved","root":3}`, `"root" must be a string or a list of strings`},
		{`{"event":"goal.approved","root":true}`, `"root" must be a string or a list of strings`},
		{`{"event":"goal.approved","root":{"a":1}}`, `"root" must be a string or a list of strings`},
		{`{"event":"goal.approved","root":null}`, `"root" must be a string or a list of strings`},
		{`{"event":"goal.approved","root":[""]}`, `an empty string`},
		{`{"event":"goal.approved","root":["a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p","q"]}`, `the limit is 16`},
		{`{"event":"goal.approved","root":"` + strings.Repeat("x", 129) + `"}`, `the limit is 128`},
		{`{"event":"goal.approved","task_type":"bug"}`, `"task_type" applies only to task.* events`},
		{`{"event":"task.created","goal_type":"bug"}`, `"goal_type" applies only to goal.* events`},
		{`{"event":"task.created","source_kind":"manual"}`, `"source_kind" applies only to goal.* events`},
		{`{"event":"task.created","task_type":"epic"}`, `is not one of bug, feature, chore, refactor, investigation`},
		{`{"event":"goal.approved","root":["a",7]}`, `"root" must be a string or a list of strings`},
		// CT11: the old "project" key is gone — a stored rule carrying it is
		// refused with one sentence, not a silent no-op (DB19).
		{`{"event":"goal.approved","project":"prj1"}`, `"project"`},
	}
	for _, c := range cases {
		_, err := ParseMatch([]byte(c.raw))
		if err == nil {
			t.Errorf("%s was accepted, want a refusal naming %q", c.raw, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s refused with %q, want it to contain %q", c.raw, err.Error(), c.want)
		}
	}
}

func TestAMatchValueIsALiteralNotAPattern(t *testing.T) {
	m := mustParse(t, `{"event":"goal.approved","goal_type":"b*"}`)
	// "b*" is the literal "b*": it matches only an event whose field says
	// exactly that. The day a value here glob-matches, this test dies.
	if m.Matches(AutomationEvent{Event: "goal.approved", GoalType: "bug"}) {
		t.Error("b* matched bug — a pattern leaked into the grammar (AU6 forbids it)")
	}
	if !m.Matches(AutomationEvent{Event: "goal.approved", GoalType: "b*"}) {
		t.Error("b* did not match the literal b* — values must be literals")
	}
	m2 := mustParse(t, `{"event":"goal.approved","goal_type":"^bug$"}`)
	if m2.Matches(AutomationEvent{Event: "goal.approved", GoalType: "bug"}) {
		t.Error("^bug$ matched bug — regex semantics entered the evaluator")
	}
}

func TestMatchesAndsKeysOrsValuesAndEmptyNeverMatches(t *testing.T) {
	m := mustParse(t, `{"event":"goal.approved","goal_type":["bug","feature"],"root":"prj1","source_kind":["manual","brainstorm"]}`)
	ok := AutomationEvent{Event: "goal.approved", GoalType: "bug", RootID: "prj1", SourceKind: "manual"}
	if !m.Matches(ok) {
		t.Error("a fully satisfying event was not matched")
	}
	if m.Matches(AutomationEvent{Event: "goal.approved", GoalType: "chore", RootID: "prj1", SourceKind: "manual"}) {
		t.Error("goal_type OR failed: chore is in neither value")
	}
	if m.Matches(AutomationEvent{Event: "goal.approved", GoalType: "bug", RootID: "prj2", SourceKind: "manual"}) {
		t.Error("keys are ANDed: a wrong root must not match")
	}
	// AU6's empty-field rule: the event never said which root — a
	// present key is never satisfied by absence.
	if m.Matches(AutomationEvent{Event: "goal.approved", GoalType: "bug", SourceKind: "manual"}) {
		t.Error(`an event with RootID "" satisfied a present root key — AU6 forbids it`)
	}
	// An absent key matches anything, including the empty field.
	loose := mustParse(t, `{"event":"goal.approved"}`)
	if !loose.Matches(AutomationEvent{Event: "goal.approved"}) {
		t.Error("an absent key must match any event of the right kind")
	}
	if loose.Matches(AutomationEvent{Event: "task.created"}) {
		t.Error("event is not ANDed — the wrong kind matched")
	}
}

func TestCanonicalRoundTrips(t *testing.T) {
	for _, raw := range []string{
		`{"event":"goal.approved"}`,
		`{"event":"task.completed","task_type":["feature","bug"],"root":["p2","p1"],"component":"c"}`,
		`{"event":"task.approved","component":"c","task_type":["chore","bug"]}`,
	} {
		m := mustParse(t, raw)
		again, err := ParseMatch([]byte(m.Canonical()))
		if err != nil {
			t.Fatalf("canonical of %s does not reparse: %v", raw, err)
		}
		if again.Canonical() != m.Canonical() {
			t.Errorf("canonical is not stable for %s:\n %s\n %s", raw, m.Canonical(), again.Canonical())
		}
	}
}
