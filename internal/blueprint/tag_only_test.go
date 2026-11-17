package blueprint

// The shipped `tag-only` blueprint (bench demo runner, spec 2026-10-25): the
// one-lane `by-tech-tag` ladder the demo rehearses without a credential —
// the resolver's two honest outcomes (refusal naming the tag; dispatch naming
// the resolved handler) both surface from a single lane, fast.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

func TestTagOnlyShipsAndValidates(t *testing.T) {
	found := false
	for _, n := range StandardNames() {
		if n == "tag-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tag-only is not a standard blueprint: %v", StandardNames())
	}
	raw, err := os.ReadFile(filepath.Join("blueprints", "tag-only.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	bp, err := Load(raw)
	if err != nil {
		t.Fatalf("the shipped tag-only does not load: %v", err)
	}
	if len(bp.Lanes) != 1 {
		t.Fatalf("%d lanes, want the one rehearsal lane", len(bp.Lanes))
	}
	lane := bp.Lanes[0]
	if lane.Step == nil || lane.Step.Agent != "by-tech-tag" || lane.Step.Kind != ModelStep {
		t.Fatalf("lane %+v — the whole point is one by-tech-tag model step", lane)
	}
	// Against the REAL registry — the D13 flip's promise, applied to the new
	// shipped file: no invalid diagnostic, and the resolver must read as
	// verified, not unverifiable.
	r := Analyze(raw, ValidationOptions{Mode: "default", Agents: handler.Names(), Resolvers: handler.Resolvers()})
	for _, diag := range r.Diagnostics {
		if diag.State == Invalid {
			t.Errorf("shipped tag-only is invalid against the real registry: %s — %s", diag.Path, diag.Msg)
		}
		if diag.Path == "lanes[0].step.agent" && diag.Kind == "agent-registry" {
			t.Errorf("by-tech-tag reads %s (%s) — the resolver registry exists now; a shipped file must not carry the pre-D13 warning", diag.State, diag.Msg)
		}
	}
}
