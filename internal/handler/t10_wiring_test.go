package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

// T10 — the HandoffSchema bridge. The body's handoff map must REACH the
// model (shown schema) and have an enforcement twin (validator spec) that
// agrees with it. Before this, both died in the front matter.

func TestHandoffSchemaReachesTheModel(t *testing.T) {
	for _, name := range registeredBodies {
		d, rej := LoadDefinition(name)
		if rej != nil {
			t.Fatalf("%s: %s", rej.Path, rej.Reason)
		}
		step, ok := Build(name, "t10 probe")
		if !ok {
			t.Fatalf("Build(%q) refused after the bridge — the body loads, so its handoff must render", name)
		}
		if len(d.Handoff) == 0 {
			continue
		}
		if step.HandoffSchema == runtime.DefaultHandler("probe", []string{"read"}).HandoffSchema {
			t.Errorf("%s: a body declaring %d handoff fields was shown the DEFAULT schema — the declaration died in the front matter again", name, len(d.Handoff))
			continue
		}
		var doc struct {
			Type       string                    `json:"type"`
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		}
		if err := json.Unmarshal([]byte(step.HandoffSchema), &doc); err != nil {
			t.Errorf("%s: shown schema is not JSON: %v", name, err)
			continue
		}
		if doc.Type != "object" {
			t.Errorf("%s: shown schema type %q", name, doc.Type)
		}
		for field, purpose := range d.Handoff {
			key := strings.TrimSuffix(field, "?")
			prop, ok := doc.Properties[key]
			if !ok {
				t.Errorf("%s: declared handoff field %q never reaches the schema the model sees — the next step was promised it (D12)", name, key)
				continue
			}
			desc, _ := prop["description"].(string)
			if desc == "" {
				t.Errorf("%s: field %q lost its stated purpose — O6's whole point is the description travels", name, key)
			} else if purpose != "" && !strings.Contains(desc, strings.Fields(purpose)[0]) {
				t.Errorf("%s: field %q description drifted from the stated purpose %q", name, key, purpose)
			}
		}
		if !containsStr(doc.Required, "status") || !containsStr(doc.Required, "summary") {
			t.Errorf("%s: the shown schema dropped the validator's required pair — models stop sending what the schema does not promise", name)
		}
	}
}

func TestShownSchemaAndValidatorSpecAgree(t *testing.T) {
	// The drift runtime pins for its own defaults, asserted per handler:
	// required names identical, status enum identical.
	for _, name := range registeredBodies {
		d, rej := LoadDefinition(name)
		if rej != nil {
			t.Fatalf("%s: %s", rej.Path, rej.Reason)
		}
		if len(d.Handoff) == 0 {
			continue
		}
		shown, err := HandoffSpecFor(d.Handoff)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var doc struct {
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		}
		if err := json.Unmarshal([]byte(shown), &doc); err != nil {
			t.Fatal(err)
		}
		spec, ok := ValidatorSpec(d.Handoff)
		if !ok {
			t.Fatalf("%s: ValidatorSpec refused a schema the shown side rendered", name)
		}
		got := append([]string(nil), spec.Required...)
		reflectSort(got)
		want := append([]string(nil), doc.Required...)
		reflectSort(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: validator requires %v, the shown schema promises %v — one of them is lying to the model", name, spec.Required, doc.Required)
		}
		if !reflect.DeepEqual(spec.Enums["status"], runtime.DefaultHandoffSpec().Enums["status"]) {
			t.Errorf("%s: the status enum drifted from the runtime's closed set", name)
		}
		for _, req := range spec.Required {
			if req != "status" && req != "summary" {
				if _, declared := d.Handoff[req]; !declared {
					if _, declaredOpt := d.Handoff[req+"?"]; !declaredOpt {
						t.Errorf("%s: validator requires %q, which no front matter declared", name, req)
					}
				}
			}
		}
	}
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func reflectSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
