package automationview

import (
	"time"

	"testing"

	"github.com/cjlapao/aeon/internal/match"
)

// TestVocabularyApplicabilityEqualsTheValidator recomputes every (event,
// key) answer with an INDEPENDENT probe and compares — the vocabulary
// prints the validator's table, never a copy of it. The mutation check the
// rule demands: widen the probe set here with a key the validator refuses
// and this test must go red; the same sentence that gates a save gates the
// vocabulary in the same commit.
func TestVocabularyApplicabilityEqualsTheValidator(t *testing.T) {
	v := Vocabulary()
	if len(v.Events) != len(match.Events()) {
		t.Fatalf("the vocabulary lost an event: %d vs %d", len(v.Events), len(match.Events()))
	}
	for _, e := range v.Events {
		var want []string
		for _, k := range []string{"root", "component", "goal_type", "task_type", "source_kind"} {
			// An independent recomputation of the probe — a second
			// implementation on purpose: this test catches drift by
			// NOT sharing code with the read it checks.
			val := "zzz"
			if k == "task_type" {
				val = "feature"
			}
			_, err := match.ParseMatch([]byte(`{"event":"` + e.Event + `","` + k + `":"` + val + `"}`))
			if err == nil {
				want = append(want, k)
			}
		}
		if len(want) != len(e.ApplicableKeys) {
			t.Errorf("%s: validator applies %v, vocabulary prints %v", e.Event, want, e.ApplicableKeys)
			continue
		}
		for i := range want {
			if want[i] != e.ApplicableKeys[i] {
				t.Errorf("%s: key %d: validator %q, vocabulary %q", e.Event, i, want[i], e.ApplicableKeys[i])
			}
		}
	}
	// Spot truths the design names, so an empty table cannot pass by
	// agreement-of-emptiness: goal keys on goal events, task key on task
	// events, root/component everywhere.
	byEvent := map[string][]string{}
	for _, e := range v.Events {
		byEvent[e.Event] = e.ApplicableKeys
	}
	if !has(byEvent["goal.approved"], "goal_type") || has(byEvent["goal.approved"], "task_type") {
		t.Errorf("goal.approved keys wrong: %v", byEvent["goal.approved"])
	}
	if !has(byEvent["task.created"], "task_type") || has(byEvent["task.created"], "goal_type") {
		t.Errorf("task.created keys wrong: %v", byEvent["task.created"])
	}
	for _, e := range v.Events {
		if !has(e.ApplicableKeys, "root") || !has(e.ApplicableKeys, "component") {
			t.Errorf("%s lost root/component: %v", e.Event, e.ApplicableKeys)
		}
	}
}

func has(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

// TestVocabularyTaskTypesEqualS38 pins the five against S38's OL14 list —
// the words whose ONE home is internal/match (the store derives its goal
// and task type set from the same map; store tests hold the door's side).
func TestVocabularyTaskTypesEqualS38(t *testing.T) {
	v := Vocabulary()
	want := []string{"bug", "feature", "chore", "refactor", "investigation"}
	if len(v.TaskTypes) != len(want) {
		t.Fatalf("task types %v, want the five %v", v.TaskTypes, want)
	}
	for i, w := range want {
		if v.TaskTypes[i] != w {
			t.Errorf("task type %d: %q, want %q", i, v.TaskTypes[i], w)
		}
	}
}

func TestVocabularyShapes(t *testing.T) {
	v := Vocabulary()
	if v.AuthorityStatement != "This starts agents with no one present. Outward actions are denied. It will run: <description>" {
		t.Errorf("the authority statement changed its fixed words: %q", v.AuthorityStatement)
	}
	if len(v.Outcomes) < 5 {
		t.Errorf("the outcome words must cover the five, got %d", len(v.Outcomes))
	}
	if v.Limits.PreviewMax != previewMax {
		t.Errorf("the vocabulary preview cap %d disagrees with the engine's %d", v.Limits.PreviewMax, previewMax)
	}
	if len(v.Zones) < 10 {
		t.Errorf("the curated zone list went empty")
	}
	// Every curated zone must actually load — the picker must not offer
	// what the validator would refuse. (LoadZone lives in internal/trigger;
	// the vocabulary read's own test asks time.LoadLocation, the same
	// question LoadZone asks.)
	for _, z := range v.Zones {
		if _, err := time.LoadLocation(z); err != nil {
			t.Errorf("curated zone %q does not load: %v", z, err)
		}
	}
}
