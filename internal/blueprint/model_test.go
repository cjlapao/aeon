package blueprint

import (
	"encoding/json"
	"fmt"
	"testing"
)

// A form must not change what a flow means: every shipped flow, read into its
// structure and written back, is the same flow to the validator and the loader.
func TestEveryShippedFlowSurvivesTheFormRoundTrip(t *testing.T) {
	src := EmbeddedSource()
	names, err := src.Names()
	if err != nil || len(names) == 0 {
		t.Fatalf("names: %v %v", names, err)
	}
	for _, name := range names {
		raw, ok, err := src.Get(name)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", name, ok, err)
		}
		model, err := ParseModel(raw)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		text, err := RenderModel(model)
		if err != nil {
			t.Fatalf("%s: render: %v", name, err)
		}
		before, err := Load(raw)
		if err != nil {
			t.Fatalf("%s: load original: %v", name, err)
		}
		after, err := Load([]byte(text))
		if err != nil {
			t.Fatalf("%s: the rendered text does not load: %v\n%s", name, err, text)
		}
		// Questions: nil and empty are the same fact.
		if len(before.Questions) == 0 && len(after.Questions) == 0 {
			before.Questions, after.Questions = nil, nil
		}
		bj, _ := json.Marshal(before)
		aj, _ := json.Marshal(after)
		if string(bj) != string(aj) {
			t.Errorf("%s: the round trip changed the flow\nbefore: %s\nafter:  %s", name, bj, aj)
		}
		b := Analyze(raw, ValidationOptions{})
		a := Analyze([]byte(text), ValidationOptions{})
		if fmt.Sprint(kinds(b)) != fmt.Sprint(kinds(a)) {
			t.Errorf("%s: the validator reads it differently: %v vs %v", name, kinds(b), kinds(a))
		}
	}
}

func kinds(r Report) []string {
	var out []string
	for _, d := range r.Diagnostics {
		out = append(out, string(d.State)+":"+d.Kind+"@"+d.Path)
	}
	return out
}

func TestADescriptionRidesTheFlowWithoutBeingAnUnknownKey(t *testing.T) {
	text, err := RenderModel(Blueprint{ID: "x", Description: "What it is for.", Lanes: []Lane{{ID: "a", Step: &Step{Kind: ModelStep, Agent: "investigator"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Analyze([]byte(text), ValidationOptions{}).Diagnostics {
		if d.Kind == "unknown-key" {
			t.Fatalf("description is refused as an unknown key: %+v", d)
		}
	}
	m, _ := ParseModel([]byte(text))
	if m.Description != "What it is for." {
		t.Fatalf("description = %q", m.Description)
	}
}
