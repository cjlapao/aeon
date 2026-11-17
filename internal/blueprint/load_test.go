package blueprint

import (
	"github.com/cjlapao/aeon/internal/handler"

	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUnknownSchemaVersionIsRefused(t *testing.T) {
	for _, version := range []string{"", "4"} {
		_, err := Load([]byte("id: x\nversion: 1\nlanes:\n  - id: a\n    step: {kind: gate, question: q}\nquestions:\n  q: {prompt: 'go?', answers: [yes]}\nschemaVersion: " + version))
		if err == nil || !strings.Contains(err.Error(), "supported: 1") {
			t.Fatalf("version %q: %v", version, err)
		}
	}
}

func TestEveryFieldRoundTrips(t *testing.T) {
	source := []byte(`id: full
schemaVersion: 1
version: 2
lanes:
  - id: build
    when: {field: mode, op: eq, value: standard}
    iterate: tasks
    rework: implement
    ladder:
      - id: implement
        kind: model-step
        agent: default
        produces: [SURVEY.md, SUMMARY.md]
        sets: in-progress
        on_reject: {goto: implement, with: {mode: fix}, counter: retries, max: 2, then: block}
questions:
  approve: {prompt: 'Approve?', answers: [yes, no]}
`)
	first, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip changed blueprint:\n%s", raw)
	}
}

func TestProducesAcceptsScalarAndSequence(t *testing.T) {
	for _, form := range []string{"A.md", "[A.md]"} {
		b, err := Load([]byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agent: default, produces: " + form + "}"))
		if err != nil || !reflect.DeepEqual(b.Lanes[0].Step.Produces, Paths{"A.md"}) {
			t.Fatalf("%s: %+v %v", form, b, err)
		}
	}
}

func TestUnknownKeyIsAnError(t *testing.T) {
	_, err := Load([]byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agents: default}"))
	if err == nil || !strings.Contains(err.Error(), "lanes[0].step.agents") || !strings.Contains(err.Error(), "agent") {
		t.Fatal(err)
	}
}

func TestClosedSetsRejectUnknownValues(t *testing.T) {
	for _, step := range []string{"{kind: model_step, agent: default}", "{kind: model-step, agent: default, on_reject: {counter: retries, max: 2, then: retry}}"} {
		_, err := Load([]byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: " + step))
		if err == nil || !strings.Contains(err.Error(), "accepted:") {
			t.Fatal(err)
		}
	}
}

func TestDerivedBlueprintsParse(t *testing.T) {
	for _, name := range []string{"dev-loop", "development-map-pipeline", "development-map-direct", "proposal-brief", "conversational-map"} {
		path := filepath.Join("..", "..", "..", "docs", "aeon", "blueprints", name+".yaml")
		bp, err := LoadFile(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		report := Validate(bp, ValidationOptions{Agents: handler.Names(), Resolvers: handler.Resolvers()})
		if !report.OK(false) {
			t.Errorf("%s: invalid diagnostics: %+v", name, report.Diagnostics)
		}
	}
}
