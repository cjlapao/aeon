package landingseed

import (
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// TestLandingBlueprintsSeeded pins the shipped set's own shape: the two
// names, loadable bytes, and the envelope blueprintseed decodes. The
// DAEMON-side pair — the applier mounting this set and the queue starting
// these two blueprints — lives in internal/daemon's merge-queue tests.
func TestLandingBlueprintsSeeded(t *testing.T) {
	recs := Source()
	if len(recs) != 2 {
		t.Fatalf("the landing set carries %d records, want 2", len(recs))
	}
	want := map[string]bool{"land-feature": false, "resolve-conflict": false}
	for _, r := range recs {
		if _, ok := want[r.Key]; !ok {
			t.Fatalf("unexpected blueprint %q", r.Key)
		}
		want[r.Key] = true
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
		bp, err := blueprint.Load([]byte(env.Content))
		if err != nil {
			t.Fatalf("blueprint %s does not load: %v", r.Key, err)
		}
		if bp.ID != r.Key {
			t.Fatalf("blueprint %s carries id %q", r.Key, bp.ID)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("the shipped list lost %q", name)
		}
	}
	if v := SetVersion(); v < 1 {
		t.Fatalf("set version %d", v)
	}
	if n := Names(); len(n) != 2 {
		t.Fatalf("Names() answers %v", n)
	}
}

// TestResolutionBlueprintHasNoOutwardAction is the resolution loop's
// contract: it may edit, conclude, and verify — it may NEVER push, open,
// or land. The landing belongs to the landing loop; a resolution loop
// that could land would let a MODEL choose the moment a base moves, and
// the whole design says a person's settings choose that.
func TestResolutionBlueprintHasNoOutwardAction(t *testing.T) {
	outward := []string{
		"git.publishIntegration", "forge.openLandingPR", "forge.land",
		"git.push", "forge.comment", "forge.review",
	}
	for _, r := range Source() {
		if r.Key != "resolve-conflict" {
			continue
		}
		var env struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(r.Content, &env); err != nil {
			t.Fatal(err)
		}
		bp, err := blueprint.Load([]byte(env.Content))
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range bp.Lanes {
			for _, name := range outward {
				if lane.Step.Action == name {
					t.Fatalf("the resolution blueprint carries the outward action %s in lane %s", name, lane.ID)
				}
			}
		}
	}
}
