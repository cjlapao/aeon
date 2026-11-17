package protocol

// autonomy_t10_test.go — S56 T10: the field-by-field fixtures prove that the
// three human-readable strings the Settings panel shows for a grant are
// PRESENT on the wire — GrantView.describe, EffectiveView.actions[].because
// and PinnedView.grants[].live.sentence. They are the ONLY such strings a
// panel types for these (T10 behaviour 5, AL17): one missing or empty forces
// the UI to compose a sentence of its own, which is a second truth. The
// round-trip harness proves the shapes parse; these prove the words are there.

import (
	"encoding/json"
	"strings"
	"testing"
)

// autonomyFixtureResult loads one conformance fixture's result raw, failing
// when the fixture is absent or carries no result — a field-presence test
// over a missing fixture would pass vacuously.
func autonomyFixtureResult(t *testing.T, path string) json.RawMessage {
	t.Helper()
	for _, f := range loadFixtures(t) {
		if f.Path != path {
			continue
		}
		var env struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(f.Response, &env); err != nil {
			t.Fatalf("%s: response envelope: %v", path, err)
		}
		if len(env.Result) == 0 {
			t.Fatalf("%s: the fixture carries no result", path)
		}
		return env.Result
	}
	t.Fatalf("fixture %s not found — the field-presence test has nothing to check", path)
	return nil
}

// TestGrantViewDescribeIsOnTheWire: the written grant's describe is present
// and non-empty — the sentence the panel prints under a grant row.
func TestGrantViewDescribeIsOnTheWire(t *testing.T) {
	var res struct {
		Grant *GrantView `json:"grant"`
	}
	if err := json.Unmarshal(autonomyFixtureResult(t, "conformance/autonomy/grant.json"), &res); err != nil {
		t.Fatal(err)
	}
	if res.Grant == nil {
		t.Fatal("grant.json's result has no grant — the written shape must carry the view")
	}
	if strings.TrimSpace(res.Grant.Describe) == "" {
		t.Error("the written grant's describe is empty — the panel would have to compose it (AL17)")
	}
}

// TestEffectiveViewBecauseIsOnTheWire: every action in the effective view
// carries its because sentence — the run row as much as the deny row,
// because a verdict without its reason is a chip the panel cannot explain.
func TestEffectiveViewBecauseIsOnTheWire(t *testing.T) {
	var view EffectiveView
	if err := json.Unmarshal(autonomyFixtureResult(t, "conformance/autonomy/effective.json"), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Actions) == 0 {
		t.Fatal("effective.json carries no actions — the check would pass vacuously")
	}
	for _, a := range view.Actions {
		if strings.TrimSpace(a.Because) == "" {
			t.Errorf("action %s has no because sentence — the panel would have to compose it (AL17)", a.Action)
		}
	}
}

// TestPinnedViewLiveSentenceIsOnTheWire: every grant a loop was pinned with
// carries its live sentence — the same words the decision site speaks when
// it denies (AL17), so what the panel shows and what the loop is told are
// one truth.
func TestPinnedViewLiveSentenceIsOnTheWire(t *testing.T) {
	var view PinnedView
	if err := json.Unmarshal(autonomyFixtureResult(t, "conformance/autonomy/pinned.json"), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Grants) == 0 {
		t.Fatal("pinned.json carries no grants — the check would pass vacuously")
	}
	for _, g := range view.Grants {
		if strings.TrimSpace(g.Live.Sentence) == "" {
			t.Errorf("grant %s has no live sentence — the panel would have to compose it (AL17)", g.GrantID)
		}
	}
}
