package intakeseed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
)

// TestIntakeSeedSetShape pins the shipped set's own shape: the one name,
// loadable bytes, and the envelope blueprintseed decodes — the landingseed
// pair, for one record (S61 T9).
func TestIntakeSeedSetShape(t *testing.T) {
	recs := Source()
	if len(recs) != 1 {
		t.Fatalf("the intake set carries %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Key != "goal-intake" {
		t.Fatalf("unexpected blueprint %q", r.Key)
	}
	var env struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(r.Content, &env); err != nil {
		t.Fatalf("record %s: envelope does not decode: %v", r.Key, err)
	}
	if env.Name != r.Key {
		t.Fatalf("record %s: the envelope names %q", r.Key, env.Name)
	}
	if env.Version != r.Version || env.Version < 1 {
		t.Fatalf("record %s: version %d against the record's %d", r.Key, env.Version, r.Version)
	}
	if v := SetVersion(); v < 1 {
		t.Fatalf("set version %d", v)
	}
	if n := Names(); len(n) != 1 || n[0] != "goal-intake" {
		t.Fatalf("Names() answers %v", n)
	}
	d := Declaration()
	if d.ID != SetID || string(d.Class) != "inert" {
		t.Fatalf("declaration %s/%v — the set is inert or it is not this set", d.ID, d.Class)
	}
}

// TestGoalIntakeBlueprintValidatesAndHasNoCodeAction is the blueprint's own
// contract (P8, P-E): it VALIDATES clean, its one lane is a model step by
// the intake-architect, and it carries ZERO code actions — nothing in this
// blueprint scaffolds, creates, commits or publishes. The person's commit
// door stands outside the loop by law (GI9); a code action inside this
// blueprint would be that law broken at the pin. The lane also carries no
// emits: the session's products are PROPOSAL rows filed through its own
// ask doors, not emitted records (T9's P-E note — no engine record type is
// invented for charter/plan drafts).
func TestGoalIntakeBlueprintValidatesAndHasNoCodeAction(t *testing.T) {
	var env struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(Source()[0].Content, &env); err != nil {
		t.Fatal(err)
	}
	bp, err := blueprint.Load([]byte(env.Content))
	if err != nil {
		t.Fatalf("goal-intake does not load: %v", err)
	}
	// Agents is the honest closed registry (the production callers pass
	// handler.Names()): intake-architect validates because it IS a
	// registered handler — the same fact that lets the loop run it.
	r := blueprint.Validate(bp, blueprint.ValidationOptions{Mode: "standard", Agents: handler.Names()})
	if !r.OK(true) {
		for _, d := range r.Diagnostics {
			t.Errorf("diagnostic %s (%s) at %s: %s", d.State, d.Kind, d.Path, d.Msg)
		}
		t.Fatal("goal-intake does not validate strict-clean")
	}
	if bp.After != "converse" || bp.Converse != "intake-architect" {
		t.Fatalf("after=%q converse=%q — the session rests in the interview (chat shape)", bp.After, bp.Converse)
	}
	if strings.Join(bp.Output, ",") != "charter.md,plan.json" {
		t.Fatalf("output %v", bp.Output)
	}
	if len(bp.Lanes) != 1 {
		t.Fatalf("%d lanes, want the one capture lane", len(bp.Lanes))
	}
	lane := bp.Lanes[0]
	if lane.ID != "capture" || lane.Step.Kind != blueprint.ModelStep || lane.Step.Agent != "intake-architect" {
		t.Fatalf("lane %s is %v by %q — the capture lane is the intake-architect's model step", lane.ID, lane.Step.Kind, lane.Step.Agent)
	}
	if strings.Join(lane.Step.Produces, ",") != "charter.md,plan.json" {
		t.Fatalf("produces %v — the capture contract names the two documents", lane.Step.Produces)
	}
	if len(lane.Step.Emits) != 0 {
		t.Fatalf("emits %v — the session files proposal rows through its ask doors, it emits no records", lane.Step.Emits)
	}
	for _, lane := range bp.Lanes {
		if lane.Step.Kind == blueprint.CodeAction {
			t.Fatalf("lane %s carries the code action %q — the intake loop runs no plane action", lane.ID, lane.Step.Action)
		}
	}
}
