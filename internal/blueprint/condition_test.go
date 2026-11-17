package blueprint

import (
	"reflect"
	"testing"
)

func TestManifestIsTheSingleFieldVocabulary(t *testing.T) {
	fields := Fields()
	slots := Slots()
	if len(fields) != len(slots) {
		t.Fatal(fields, slots)
	}
	for i, f := range fields {
		if f.Path != slots[i] || len(f.Operators) == 0 {
			t.Fatal(f, slots)
		}
	}
}

func TestConditionOperatorMatrix(t *testing.T) {
	cases := []struct {
		condition Condition
		actual    any
		want      bool
	}{
		{Condition{Field: "mode", Op: Eq, Value: "poc"}, "poc", true},
		{Condition{Field: "flags.security_scan", Op: Neq, Value: true}, false, true},
		{Condition{Field: "tasks.count", Op: Gt, Value: 1}, 2, true},
		{Condition{Field: "tasks.count", Op: Lte, Value: 2}, 2, true},
		{Condition{Field: "answers.finish-how", Op: In, Value: []any{"merge-now", "leave-open"}}, "leave-open", true},
	}
	for _, tc := range cases {
		if issues := CheckCondition(tc.condition, "when"); len(issues) > 0 {
			t.Fatal(issues)
		}
		got, err := tc.condition.Evaluate(Values{tc.condition.Field: tc.actual})
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %v %v", tc.condition, got, err)
		}
	}
}

func TestTypeMismatchIsAValidationError(t *testing.T) {
	for _, c := range []Condition{{Field: "mode", Op: Gt, Value: 3}, {Field: "tasks.count", Op: Gt, Value: "three"}} {
		issues := CheckCondition(c, "lanes[build].when")
		if len(issues) != 1 || issues[0].State != Invalid || issues[0].Path != "lanes[build].when" {
			t.Fatal(issues)
		}
	}
}

func TestEnumValueOutsideSetIsInvalid(t *testing.T) {
	issues := CheckCondition(Condition{Field: "mode", Op: Eq, Value: "simpl"}, "when")
	if len(issues) != 1 || issues[0].Kind != "enum-value" {
		t.Fatal(issues)
	}
}

func TestCompositionNesting(t *testing.T) {
	c := Condition{All: []Condition{{Field: "mode", Op: Eq, Value: "standard"}, {Any: []Condition{{Not: &Condition{Field: "flags.security_scan", Op: Eq, Value: true}}, {Field: "tasks.count", Op: Gt, Value: 2}}}}}
	got, err := c.Evaluate(Values{"mode": "standard", "flags.security_scan": false, "tasks.count": 0})
	if err != nil || !got {
		t.Fatal(got, err)
	}
	if issues := CheckCondition(Condition{All: []Condition{}}, "when"); len(issues) != 1 || issues[0].Kind != "empty-condition" {
		t.Fatal(issues)
	}
}

func TestEvaluateNeverReturnsFalseOnError(t *testing.T) {
	got, err := (Condition{Field: "flags.security_scan", Op: Eq, Value: true}).Evaluate(NewValues())
	if got || err == nil {
		t.Fatal(got, err)
	}
}

func TestFieldsAreCopied(t *testing.T) {
	fields := Fields()
	fields[0].Enum[0] = "broken"
	if reflect.DeepEqual(fields, Fields()) {
		t.Fatal("caller modified manifest")
	}
}
