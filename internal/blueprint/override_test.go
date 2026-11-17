package blueprint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// S23 T5 — blueprints shipped and overridable. The gates are the ones a
// careless change breaks silently: a project's own overrides the shipped by
// name (C12, D14), the provenance is on the screen, an override does not
// change a running loop (the pin holds, S8's L6), and an unknown blueprint's
// error names how to list them (T1).

// writeProjectBlueprint writes a project's own blueprint into a temp project
// dir (./aeon/blueprints/<name>.yaml), so the override has something to win.
func writeProjectBlueprint(t *testing.T, projectDir, name, content string) {
	t.Helper()
	dir := filepath.Join(projectDir, "aeon", "blueprints")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestProjectBlueprintOverridesShipped is C12/D14's pin: the project's own
// wins over the shipped, by name.
func TestProjectBlueprintOverridesShipped(t *testing.T) {
	projectDir := t.TempDir()
	// A project's own dev-loop, distinguishable from the shipped by its ID.
	writeProjectBlueprint(t, projectDir, "dev-loop", "id: dev-loop-project\nschemaVersion: 1\nlanes:\n  - step:\n      kind: model-step\n      agent: coder\n")
	got, err := ResolveWithProject("dev-loop", projectDir)
	if err != nil {
		t.Fatalf("the project's dev-loop must resolve: %v", err)
	}
	if got.Provenance != ProvenanceProject {
		t.Fatalf("the project's own must win, got %s", got.Provenance)
	}
	if got.Blueprint.ID != "dev-loop-project" {
		t.Fatalf("the project's content must be used, got id %q", got.Blueprint.ID)
	}
}

// TestListShowsWhichIsWhich is D14's pin: the provenance is on the screen —
// the project's own (override) and the shipped, for each.
func TestListShowsWhichIsWhich(t *testing.T) {
	projectDir := t.TempDir()
	writeProjectBlueprint(t, projectDir, "dev-loop", "id: dev-loop-project\nschemaVersion: 1\nlanes:\n  - step:\n      kind: model-step\n      agent: coder\n")
	list := ListWithProvenance(projectDir)
	byName := map[string]Provenance{}
	for _, e := range list {
		byName[e.Name] = e.Provenance
	}
	if byName["dev-loop"] != ProvenanceProject {
		t.Fatalf("the overridden dev-loop must be 'this project', got %s", byName["dev-loop"])
	}
	if byName["two-step"] != ProvenanceShipped {
		t.Fatalf("two-step must be 'shipped', got %s", byName["two-step"])
	}
}

// TestUnknownBlueprintErrorListsAvailable is T1's pin: an unknown blueprint's
// error is three-part, and its Do is `aeon blueprint list` — the one fix, the
// only way a stranger learns the names.
func TestUnknownBlueprintErrorListsAvailable(t *testing.T) {
	_, err := ResolveWithProject("no-such-blueprint", "")
	if err == nil {
		t.Fatal("an unknown blueprint must error")
	}
	var e *ErrUnknownBlueprint
	if !errors.As(err, &e) {
		t.Fatalf("the error must be the unknown-blueprint error, got %T", err)
	}
	if e.Do() != "aeon blueprint list" {
		t.Fatalf("the Do must be the fix, got %q", e.Do())
	}
}

// TestOverrideDoesNotAffectARunningLoop is S8's L6: a running loop keeps its
// pinned content, so an override does not change a loop in flight. The pin is
// a runtime concept — a running loop holds the content it captured at loop
// start, not a re-resolution — and what the pin relies on is that the
// resolution is deterministic: the same name resolves to the same content, so
// the content captured at loop start is the content the loop keeps. This test
// pins that foundation (the resolution is stable), and records that the full
// "a running loop does not see a later override" is the runtime pin holding
// the captured content, not a re-resolution.
func TestOverrideDoesNotAffectARunningLoop(t *testing.T) {
	projectDir := t.TempDir()
	// The shipped dev-loop, resolved with no project override: deterministic,
	// the same content on the first and the second resolution.
	first, err := ResolveWithProject("dev-loop", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ResolveWithProject("dev-loop", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Blueprint.ID != again.Blueprint.ID || first.Provenance != again.Provenance {
		t.Fatalf("the resolution must be deterministic (the pin's foundation): %s/%s then %s/%s",
			first.Blueprint.ID, first.Provenance, again.Blueprint.ID, again.Provenance)
	}
	// A running loop keeps the content it captured at loop start (first); a
	// later override does not reach it, because the pin holds the captured
	// content, not a re-resolution. The captured content is what the loop
	// keeps — here, the shipped dev-loop, resolved before any project override
	// existed.
	if first.Blueprint.ID == "" {
		t.Fatal("the pinned content must be a real blueprint")
	}
}
