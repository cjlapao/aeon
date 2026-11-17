package autonomy

import (
	"bytes"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
)

// TestVocabularyIsCompleteForAForm (T10 behaviour 1): every subject has a
// label, every condition its help, every origin its sentence — a panel that
// renders the descriptors never types a word. An empty string here is a word
// the UI would have to supply, which is a second truth (AL15).
func TestVocabularyIsCompleteForAForm(t *testing.T) {
	v := Vocabulary()
	if len(v.Subjects) == 0 {
		t.Fatal("no subjects — a form would have nothing to offer")
	}
	for _, s := range v.Subjects {
		if s.Label == "" {
			t.Errorf("subject %s has no label — a form would have to type one", s.Name)
		}
		if !action.Valid(s.Class) {
			t.Errorf("subject %s carries an invalid class %q", s.Name, s.Class)
		}
		// The class shorthand is the one subject that carries covers; an
		// action subject never does.
		if (s.Kind == "class") != (len(s.Covers) > 0) {
			t.Errorf("subject %s (kind %s): covers present = %v, want true only for the class", s.Name, s.Kind, s.Covers)
		}
	}
	// The merge subject: the required expiry and the forced condition.
	mergeFound := false
	for _, s := range v.Subjects {
		if s.Name != action.ForgeMerge {
			continue
		}
		mergeFound = true
		if !s.RequiresExpiry {
			t.Error("the merge subject must require an expiry (AL5)")
		}
		if len(s.ForcedConditions) != 1 || s.ForcedConditions[0] != "requireGates" {
			t.Errorf("the merge subject's forced conditions = %v, want [requireGates]", s.ForcedConditions)
		}
	}
	if !mergeFound {
		t.Fatal("the merge subject is missing from the vocabulary")
	}
	for _, c := range v.Conditions {
		if c.Help == "" {
			t.Errorf("condition %s has no help — a form would have to type the sentence", c.Name)
		}
		if c.Evaluated != "pin" && c.Evaluated != "action" {
			t.Errorf("condition %s is evaluated at %q — the closed set is pin|action", c.Name, c.Evaluated)
		}
	}
	// The condition set is exactly the closed four: a fifth descriptor is a
	// condition the wire cannot carry; a missing one is a word the UI types.
	seen := map[string]bool{}
	for _, c := range v.Conditions {
		if seen[c.Name] {
			t.Errorf("condition %s appears twice", c.Name)
		}
		seen[c.Name] = true
	}
	for _, name := range []string{"requireGates", "requireBudget", "labels", "maxPerDay"} {
		if !seen[name] {
			t.Errorf("condition %s is missing from the descriptors", name)
		}
	}
	if len(seen) != 4 {
		t.Errorf("the condition set is %v — want exactly the four closed conditions", seen)
	}
	for _, o := range v.Origins {
		if o.Help == "" {
			t.Errorf("origin %s has no sentence — a form would have to type one", o.Origin)
		}
	}
	if v.Limits.MaxMergeGrantDays < 1 {
		t.Errorf("maxMergeGrantDays = %d — the merge bound must be a positive number of days", v.Limits.MaxMergeGrantDays)
	}
}

// TestVocabularyIsStable (T10 behaviour 6): the descriptor set is cacheable
// once per session — byte-identical across calls and carrying no timestamp a
// cached copy would go stale on.
func TestVocabularyIsStable(t *testing.T) {
	a, err := json.Marshal(Vocabulary())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(Vocabulary())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("the vocabulary is not byte-identical across calls:\n%s\nvs\n%s", a, b)
	}
	if re := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`); re.Match(a) {
		t.Errorf("the vocabulary carries a date (%s) — a cached copy would go stale", re.Find(a))
	}
}
