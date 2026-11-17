package blueprint

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// S57 T6 — the validator's half of the engine integration: verify.run is
// the code action the ENGINE routes (on_reject allowed, params
// type-checked), and forge.postReview is registered but unrunnable from
// a blueprint. The fixture is a TEST fixture (testdata), never an
// embedded blueprint — CR-BP1 owns the shipped dev-loop's verify rung.

// vg9Actions is the closed registry T6 ships: the S16 set plus verify.run.
// (shipped_test.go's s16ActionSet is the S16 mirror used for the pristine
// fixture; it is extended where the closed set is the subject.)
var vg9Actions = []string{"audit", "forge.closeIssue", "forge.merge", "forge.openIssue",
	"forge.openPR", "forge.postReview", "git.commitAndPush", "git.createBranch", "git.mergeBranch",
	"verify.run"}

func vg9Report(t *testing.T, raw []byte) Report {
	t.Helper()
	return Analyze(raw, ValidationOptions{Mode: "standard", Agents: []string{"default"}, Actions: vg9Actions})
}

func hasKind(r Report, kind string) (string, bool) {
	for _, d := range r.Diagnostics {
		if d.Kind == kind {
			return d.Msg, true
		}
	}
	return "", false
}

// TestVerifyRunFixtureValidates — the ladder the engine test drives:
// implement (model) → verify (verify.run, on_reject capped, then: block).
// A verify.run with on_reject is the shape VG7 exists to route, so the
// validator must accept it cleanly.
func TestVerifyRunFixtureValidates(t *testing.T) {
	raw, err := os.ReadFile("testdata/verify-run-fixture.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := vg9Report(t, raw)
	if _, ok := hasKind(r, "code-action-on-reject"); ok {
		t.Error("the fixture's verify.run on_reject was refused — it is the shape the engine routes")
	}
	if _, invalid, _ := r.Counts(); invalid > 0 {
		t.Fatalf("the fixture is not valid: %+v", r.Diagnostics)
	}
}

// TestPostReviewUnrunnableFromABlueprint — VG9's sentence: postReview
// stays registered (class outward, not deleted) but a blueprint step
// naming it is invalid, with the exact refusal.
func TestPostReviewUnrunnableFromABlueprint(t *testing.T) {
	raw := []byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: post\n    step: {kind: code-action, action: forge.postReview}\n")
	r := vg9Report(t, raw)
	msg, ok := hasKind(r, "unrunnable-action")
	if !ok {
		t.Fatalf("forge.postReview as a code action produced no unrunnable-action finding: %+v", r.Diagnostics)
	}
	want := "forge.postReview cannot be fired from a blueprint — it needs a step id and attempt only a model step has (S57 VG9)"
	if msg != want {
		t.Errorf("sentence drifted:\n got %q\nwant %q", msg, want)
	}
}

// TestCodeActionOnRejectOnlyVerifyRun — the tightening T0 item 1
// records: a code action's on_reject is refused for every action but
// verify.run (today the validator accepts the shape and the engine
// ignores it; a dead edge must not pass a load gate).
func TestCodeActionOnRejectOnlyVerifyRun(t *testing.T) {
	base := "id: x\nschemaVersion: 1\nversion: 1\nlanes:\n"
	// A two-step ladder: the code action's on_reject (a STEP attribute,
	// beside kind/action) targets the next step of the same ladder.
	two := "  - id: build\n    iterate: tasks\n    ladder:\n" +
		"      - id: first\n        kind: code-action\n        action: %s\n" +
		"        on_reject: { goto: second, counter: c, max: 1, then: block }\n" +
		"      - id: second\n        kind: model-step\n        agent: default\n"

	// A non-verify code action with on_reject is invalid.
	r := vg9Report(t, []byte(base+fmt.Sprintf(two, "forge.openPR")))
	if _, ok := hasKind(r, "code-action-on-reject"); !ok {
		t.Errorf("forge.openPR with on_reject was not refused: %+v", r.Diagnostics)
	}

	// The same edge on verify.run passes (same body, different action).
	r = vg9Report(t, []byte(base+fmt.Sprintf(two, "verify.run")))
	if _, invalid, _ := r.Counts(); invalid > 0 {
		t.Errorf("verify.run with on_reject was refused: %+v", r.Diagnostics)
	}
}

// TestVerifyRunParamsTypeChecked — gate and agent are code-supplied
// STRINGS: a number or a list is a shape the runner would have to
// guess, and the validator refuses it at load.
func TestVerifyRunParamsTypeChecked(t *testing.T) {
	base := "id: x\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: gate-lane\n    step: {kind: code-action, action: verify.run, params: {gate: %s}}\n"

	r := vg9Report(t, []byte(strings.Replace(base, "%s", "5", 1)))
	if _, ok := hasKind(r, "param-type"); !ok {
		t.Errorf("numeric params.gate was not type-refused: %+v", r.Diagnostics)
	}

	r = vg9Report(t, []byte(strings.Replace(base, "%s", "unit", 1)))
	if _, invalid, _ := r.Counts(); invalid > 0 {
		t.Errorf("string params.gate was refused: %+v", r.Diagnostics)
	}
}
