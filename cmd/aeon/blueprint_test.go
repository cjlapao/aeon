package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFieldsCommandPrintsTheTable(t *testing.T) {
	var out bytes.Buffer
	if err := cmdBlueprint(&out, []string{"fields"}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"mode", "flags.*", "answers.*", "tasks.count"} {
		if !strings.Contains(out.String(), field) {
			t.Fatal(out.String())
		}
	}
}

func TestDefaultDoesNotFailOnUnverifiable(t *testing.T) {
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := cmdBlueprint(&out, []string{"validate", "dev-loop"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0 invalid") || !strings.Contains(out.String(), "unverifiable") {
		t.Fatal(out.String())
	}
}

// S16 T10 EVOLVED THIS TEST — it pinned "dev-loop --strict fails", and
// that expectation was the slice's whole point to invert: the registry
// arrived (D15), so dev-loop PASSES strict for the first time. Strict
// still fails where references are genuinely open (research-map's skill
// lane — a fact about the operator's card directory the validator at
// rest cannot see), which is what keeps --strict meaningful rather than
// ceremonial.
func TestStrictPassesOnDevLoopAndStillFailsWhereReferencesAreOpen(t *testing.T) {
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := cmdBlueprint(&out, []string{"validate", "--strict", "dev-loop"}); err != nil {
		t.Fatalf("dev-loop --strict must PASS now the registry exists (D15): %v\n%s", err, out.String())
	}
	out.Reset()
	if err := cmdBlueprint(&out, []string{"validate", "--strict", "research-map"}); err == nil {
		t.Fatalf("strict must still fail where a reference is honestly open: %s", out.String())
	}
	if !strings.Contains(out.String(), "unverifiable") {
		t.Fatal(out.String())
	}
	// And an unknown action is refused, not deferred — the flipped class:
	bad := filepath.Join(t.TempDir(), "smuggle.yaml")
	raw := "id: smuggle\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: x\n    step: {kind: code-action, action: git.smuggle}\n"
	if err := os.WriteFile(bad, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdBlueprint(&out, []string{"validate", bad}); err == nil {
		t.Fatal("an unknown action is INVALID now (D15), not unverifiable")
	}
	if !strings.Contains(out.String(), "unknown-action") {
		t.Fatalf("the kind must be the real one: %s", out.String())
	}
}

func TestTwoStepPassesStrictValidation(t *testing.T) {
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := cmdBlueprint(&out, []string{"validate", "--strict", "two-step"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0 invalid, 0 unverifiable") {
		t.Fatal(out.String())
	}
}
