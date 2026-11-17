package records

// catalog_scheduler_test.go — S27 T8 (CN2, CN21, CN23): the scheduler
// family's catalog census. What each test pins, in order:
//
//   - the family carries EXACTLY the four read twins — scheduler_status ·
//     scheduler_preview · scheduler_decisions · scheduler_attention — each
//     class safe, each Mutates:false, each twinned to its scheduler.* door
//     (CN23: the agent SEES the plan and the ledger);
//   - no scheduler CONTROL reaches a spec, dotted or underscored, as a name
//     or as a Method, anywhere in the catalog (CN2 — enable, disable,
//     pause, resume, root.set, root.disable, enableTree and stopAll are
//     human doors; cmd/aeon/continuous_guard_test.go holds the wire side,
//     this file holds the catalog side of the same law);
//   - the schemas carry no repository field and no identity field: the
//     scope is the step's stamp (OL3), so a param that could only be
//     overwritten is a param that lies.

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// schedulerControlShape matches every spelling of the eight controls that
// could hide in a tool name or Method — the regex the T7 guard uses on
// sources, aimed at the catalog instead.
var schedulerControlShape = regexp.MustCompile(`(?i)scheduler[._](enable|disable|pause|resume|root|stop|stopall|setroot|enabletree|authority|master)`)

func TestSchedulerFamilyIsReadOnly(t *testing.T) {
	want := map[string]string{
		"scheduler_status":    "scheduler.status",
		"scheduler_preview":   "scheduler.preview",
		"scheduler_decisions": "scheduler.decisions",
		"scheduler_attention": "scheduler.attention",
	}
	got := map[string]Spec{}
	for _, sp := range Catalog() {
		if sp.Family == "scheduler" {
			got[sp.Name] = sp
		}
	}
	if len(got) != len(want) {
		names := make([]string, 0, len(got))
		for n := range got {
			names = append(names, n)
		}
		t.Fatalf("the scheduler family holds %d specs (%v), want exactly %d (CN23)", len(got), names, len(want))
	}
	for name, method := range want {
		sp, ok := got[name]
		if !ok {
			t.Fatalf("%s is missing from the catalog — the agent's read of the scheduler has no door", name)
		}
		if sp.Method != method {
			t.Errorf("%s is twinned to %q, want %q (one definition, two doors)", name, sp.Method, method)
		}
		if sp.Class != "safe" {
			t.Errorf("%s is class %q — a scheduler read must be safe to re-run (S2)", name, sp.Class)
		}
		if sp.Mutates {
			t.Errorf("%s mutates — the scheduler's rows move only through human doors (CN2)", name)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s carries a %d-character description; a model picks tools by this sentence", name, len(sp.Description))
		}
		var doc map[string]any
		if err := json.Unmarshal(sp.Schema, &doc); err != nil {
			t.Fatalf("%s schema is not JSON: %v", name, err)
		}
		if doc["type"] != "object" || doc["additionalProperties"] != false {
			t.Errorf("%s schema is %s — additionalProperties:false everywhere, so an identity field smuggled into input is a parse error", name, sp.Schema)
		}
		props, _ := doc["properties"].(map[string]any)
		for k := range props {
			low := strings.ToLower(k)
			if strings.Contains(low, "root") || strings.Contains(low, "actor") || strings.Contains(low, "source") ||
				low == "decision" && name != "scheduler_decisions" {
				t.Errorf("%s accepts %q — the repository is the step's stamp, not a param (OL3), and the ledger's own filter names are the only fields allowed", name, k)
			}
		}
		if name != "scheduler_decisions" && len(props) != 0 {
			t.Errorf("%s accepts input %s — these reads are scoped by the step, unfiltered, and carry no repo param (T8's scoping ruling)", name, sp.Schema)
		}
	}
	// The three schemaless reads speak the step's own repository; the
	// decisions ledger takes ONLY the ledger's filter words, and never a
	// repository word.
	for _, sp := range got {
		if sp.Name != "scheduler_decisions" {
			continue
		}
		var doc map[string]any
		_ = json.Unmarshal(sp.Schema, &doc)
		props, _ := doc["properties"].(map[string]any)
		wantProps := []string{"subject", "decision", "reason", "since", "limit"}
		if len(props) != len(wantProps) {
			t.Fatalf("scheduler_decisions accepts %v, want exactly the ledger's filters %v", schedPropNames(props), wantProps)
		}
		for _, k := range wantProps {
			if _, ok := props[k]; !ok {
				t.Errorf("scheduler_decisions is missing its %q filter", k)
			}
		}
	}
}

func TestNoSchedulerControlSpecExists(t *testing.T) {
	// CN2, the catalog half: NONE of the eight controls — nor any
	// authority-shaped invention — stands in the catalog under any
	// spelling, as a name or as a Method.
	for _, sp := range Catalog() {
		if schedulerControlShape.MatchString(sp.Name) || schedulerControlShape.MatchString(sp.Method) {
			t.Errorf("%s (%s) is twinned to a scheduler control — the controls are human doors, never tools (CN2)", sp.Name, sp.Method)
		}
		if sp.Family == "scheduler" && (strings.Contains(sp.Name, "propose") || strings.Contains(sp.Method, "propose")) {
			t.Errorf("%s is a scheduler proposal — S27 files no ask here; the plane's authority moves at the human door alone", sp.Name)
		}
	}
}

func schedPropNames(props map[string]any) []string {
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
