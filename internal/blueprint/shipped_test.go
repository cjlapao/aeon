package blueprint

import (
	"github.com/cjlapao/aeon/internal/handler"

	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShippedBlueprintsValidateInCI(t *testing.T) {
	for _, name := range StandardNames() {
		raw, err := os.ReadFile(filepath.Join("blueprints", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		// D13: shipped standard blueprints validate against the REAL registry
		// in CI — twenty-five handlers, the one named resolver, and (since
		// S16 T10) the closed code-action set. A stub list here would hide an
		// agent rename — or an invented action name — from the only gate that
		// ships these files.
		r := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet})
		_, invalid, unverifiable := r.Counts()
		if invalid != 0 {
			t.Fatalf("%s: invalid diagnostics: %+v", name, r.Diagnostics)
		}
		if name == "two-step" && unverifiable != 0 {
			t.Fatalf("two-step is not strict-valid: %+v", r.Diagnostics)
		}
	}
}

func TestBrokenFixtureReportsSeveralClassesAtOnce(t *testing.T) {
	raw, err := os.ReadFile("testdata/broken-fixture.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := Analyze(raw, ValidationOptions{Mode: "standard", Agents: []string{"default", "read-only"}})
	// 2026-10-27 (S38 T11): + emits-unknown-type (the banana the fixture now
	// files) and emits-registry (this validator is at rest without the
	// handler registry — the third state is EXPECTED here, P31).
	want := []string{"unknown-key", "invalid-operator", "unknown-agent", "machine-status", "unknown-goto", "missing-then", "unknown-question", "step-shape", "iterate-field", "duplicate-step", "emits-unknown-type", "emits-registry"}
	kinds := map[string]bool{}
	for _, d := range r.Diagnostics {
		kinds[d.Kind] = true
	}
	for _, kind := range want {
		if !kinds[kind] {
			t.Errorf("missing %s in %+v", kind, r.Diagnostics)
		}
	}
	if reflect.DeepEqual(r.Diagnostics, []Diagnostic{}) {
		t.Fatal("broken fixture has no diagnostics")
	}
}

// s16ActionSet mirrors action.Names() — the CLOSED code-action set that
// arrived with S16 (D14/D15). The blueprint package cannot import the
// action package (templates depend on the manifest, never the reverse),
// so the test hands over the same data the production callers pass
// (cmd/aeon, daemon, loop — each one calls action.Names() directly).
// Drift is caught from the OTHER side: registry_test.go's
// TestDevLoopValidatesStrict runs the REAL Names() over this very file,
// so a set that drifted fails there before it could pass here.
var s16ActionSet = []string{"audit", "forge.closeIssue", "forge.merge", "forge.openIssue",
	"forge.openPR", "forge.postReview", "git.commitAndPush", "git.createBranch", "git.mergeBranch"}

func TestDevLoopUnverifiableSetMatchesFixture(t *testing.T) {
	raw, err := os.ReadFile("blueprints/dev-loop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	report := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet})
	type entry struct {
		Kind  string `json:"kind"`
		Path  string `json:"path"`
		Slice string `json:"slice"`
	}
	got := []entry{}
	for _, d := range report.Diagnostics {
		if d.State == Unverifiable {
			got = append(got, entry{d.Kind, d.Path, d.Slice})
		}
	}
	wantRaw, err := os.ReadFile("testdata/dev-loop-unverifiable.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []entry
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dev-loop expected diagnostics changed\ngot: %+v\nwant: %+v", got, want)
	}
}

// research-map (S14) ships its delegation lane UNVERIFIABLE, not invalid:
// whether `reddit-agent/summarise-sentiment` exists is a fact about the
// operator's card directory, which the validator at rest cannot see. The
// pin that makes this honest is the pair: at rest it is exactly one
// agentsvc-kind unverifiable (never invalid — that would refuse the file
// for a fact that may be true), and wired, it is fully valid — the same
// bytes that refuse to start unwired start clean once a registry knows the
// target (loop/pin.go converts the unverifiable into the refusal; here the
// validator's own two answers are pinned).
func TestResearchMapShipsItsSkillLaneUnverifiableNotInvalid(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("blueprints", "research-map.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rest := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers()})
	_, invalid, unverifiable := rest.Counts()
	if invalid != 0 || unverifiable != 1 {
		t.Fatalf("at rest research-map is %d invalid / %d unverifiable, want 0/1: %+v", invalid, unverifiable, rest.Diagnostics)
	}
	var d Diagnostic
	for _, x := range rest.Diagnostics {
		d = x
	}
	if d.Kind != "agentsvc" || !strings.Contains(d.Path, "survey") || !strings.Contains(d.Msg, "reddit-agent/summarise-sentiment") {
		t.Fatalf("the one unverifiable must be the survey lane's skill, naming the target: %+v", d)
	}

	wired := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(),
		SkillKnown: func(callee, skill string) bool { return callee == "reddit-agent" && skill == "summarise-sentiment" }})
	if _, wi, wu := wired.Counts(); wi != 0 || wu != 0 {
		t.Fatalf("wired, research-map must be fully valid, got %d invalid / %d unverifiable: %+v", wi, wu, wired.Diagnostics)
	}

	// And a wired registry that does NOT know the target makes it INVALID —
	// the pin's refusal at loop start rides this exact verdict.
	unknown := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(),
		SkillKnown: func(string, string) bool { return false }})
	if _, ui, _ := unknown.Counts(); ui != 1 {
		t.Fatalf("an unknown wired target must be invalid exactly once, got %d: %+v", ui, unknown.Diagnostics)
	}
}
