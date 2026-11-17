package automationview_test

// matchtest_test.go — the external package on purpose: it imports the
// store and runs the PRODUCTION path (store.ParseMatch → Match.Matches)
// against automationview.TestMatch pair for pair. automationview's own
// package files never import the store (deps_test pins that); a test may,
// and this one must, because "a second matcher" is exactly the failure
// mode the fuzz is here to catch.

import (
	"math/rand"
	"testing"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/store"
)

func TestMatchTestEqualsTheMatcher(t *testing.T) {
	// 500 (rule, event) pairs from the whole grammar space.
	rng := rand.New(rand.NewSource(49))
	evs := []string{"goal.approved", "task.created", "task.approved", "task.completed"}
	vals := []string{"bug", "feature", "root-a", "root-b", "comp-1", "manual", "cron", ""}
	keys := []string{"root", "component", "goal_type", "task_type", "source_kind"}
	checked := 0
	for i := 0; i < 500; i++ {
		// Build a rule JSON the grammar will accept: pick an event, then a
		// legal subset of keys for it (goal keys only on goal.*, task_type
		// only on task.*), with values from the vocabulary.
		ev := evs[rng.Intn(len(evs))]
		kvs := []string{`"event":"` + ev + `"`}
		for _, k := range keys {
			goalKey := k == "goal_type" || k == "source_kind"
			if goalKey && ev != "goal.approved" {
				continue
			}
			if k == "task_type" && ev == "goal.approved" {
				continue
			}
			if k == "task_type" {
				kvs = append(kvs, `"`+k+`":["bug"]`)
				continue
			}
			if rng.Intn(2) == 0 {
				continue
			}
			v := vals[rng.Intn(len(vals)-1)] // never the "" value — refusal is not this fuzz
			if v == "" {
				v = "x"
			}
			kvs = append(kvs, `"`+k+`":["`+v+`"]`)
		}
		raw := "{" + kvs[0]
		for _, kv := range kvs[1:] {
			raw += "," + kv
		}
		raw += "}"
		sample := automationview.Event{
			Event: evs[rng.Intn(len(evs))], RootID: vals[rng.Intn(len(vals))],
			ComponentID: vals[rng.Intn(len(vals))], GoalType: vals[rng.Intn(len(vals))],
			TaskType: vals[rng.Intn(len(vals))], SourceKind: vals[rng.Intn(len(vals))],
		}
		got, err := automationview.TestMatch([]byte(raw), sample)
		if err != nil {
			t.Fatalf("fuzz built a rule the tester refuses (must be legal): %s: %v", raw, err)
		}
		// The production path, exactly as Trigger.Admits runs it.
		m, err := store.ParseMatch([]byte(raw))
		if err != nil {
			t.Fatalf("production path refuses the same rule: %s: %v", raw, err)
		}
		want := m.Matches(store.AutomationEvent{
			Event: sample.Event, RootID: sample.RootID, ComponentID: sample.ComponentID,
			GoalType: sample.GoalType, TaskType: sample.TaskType, SourceKind: sample.SourceKind,
		})
		if got.Matches != want {
			t.Fatalf("verdict fork on rule %s with %+v: tester %v, matcher %v", raw, sample, got.Matches, want)
		}
		checked++
	}
	if checked != 500 {
		t.Fatalf("only %d pairs checked", checked)
	}
}

func TestMatchTestInvalidMatchSentenceIsVerbatim(t *testing.T) {
	bad := []string{
		`{"event":"nope"}`,
		`{"event":"goal.approved","project":"p"}`,
		`{"goal_type":"bug"}`,
		`{"event":"task.created","goal_type":"bug"}`,
		`{"event":"goal.approved","root":[]}`,
		`{`,
	}
	for _, raw := range bad {
		_, err := automationview.TestMatch([]byte(raw), automationview.Event{Event: "goal.approved"})
		if err == nil {
			t.Errorf("%s: the tester accepted an invalid rule", raw)
			continue
		}
		_, perr := store.ParseMatch([]byte(raw))
		if perr == nil || perr.Error() != err.Error() {
			t.Errorf("%s: refusal not the validator's own sentence:\n tester %v\n valid %v", raw, err, perr)
		}
	}
}

func TestMatchTestPerKeyExplanation(t *testing.T) {
	res, err := automationview.TestMatch(
		[]byte(`{"event":"goal.approved","goal_type":["bug","feature"],"root":["r1"]}`),
		automationview.Event{Event: "goal.approved", RootID: "r2", GoalType: "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches {
		t.Error("root r2 ∉ [r1] must not match")
	}
	byKey := map[string]automationview.KeyResult{}
	for _, k := range res.PerKey {
		byKey[k.Key] = k
	}
	if len(res.PerKey) != 6 {
		t.Fatalf("all six keys explain, got %d", len(res.PerKey))
	}
	if !byKey["goal_type"].OK || byKey["root"].OK || !byKey["component"].OK {
		t.Errorf("per-key verdicts wrong: %+v", byKey)
	}
	if byKey["component"].Want != nil {
		t.Error("an absent key must print as unconstrained")
	}
	if res.Canonical == "" {
		t.Error("no canonical form returned")
	}
}
