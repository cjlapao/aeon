package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyTwoStep = `# S8 test bench blueprint: two model steps with a human gate between them.
id: two-step
schemaVersion: 1
version: 1
lanes:
  - id: survey
    step: {kind: model-step, agent: default, produces: SURVEY.md}
  - id: approve
    step: {kind: gate, question: approve-survey}
  - id: summarise
    step: {kind: model-step, agent: default, produces: SUMMARY.md}
questions:
  approve-survey:
    prompt: Approve the survey and continue?
    answers: [approve, revise, abort]
`

func TestPristineLegacyTwoStepUpgradesButCustomSourceStaysPinned(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	path := filepath.Join(state, "blueprints", "two-step.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacyTwoStep), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := LegacyResolve("two-step")
	if err != nil || resolved != path {
		t.Fatalf("resolve: %q, %v", resolved, err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "version: 2") || !strings.Contains(string(updated), "on_reject: {goto: survey") {
		t.Fatalf("pristine default was not upgraded: %s", updated)
	}
	previousShipped := strings.Replace(string(updated), "version: 2", "version: 1", 1)
	if err := os.WriteFile(path, []byte(previousShipped), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LegacyResolve("two-step"); err != nil {
		t.Fatal(err)
	}
	upgradedAgain, err := os.ReadFile(path)
	if err != nil || string(upgradedAgain) != string(updated) {
		t.Fatalf("previous shipped source did not receive version 2: %v", err)
	}
	custom := strings.Replace(legacyTwoStep, "Approve the survey and continue?", "Review my draft?", 1)
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LegacyResolve("two-step"); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != custom {
		t.Fatalf("custom blueprint was changed: %v", err)
	}
}

func TestGateOfferingReviseWithoutReworkTargetIsInvalid(t *testing.T) {
	report := Analyze([]byte(legacyTwoStep), ValidationOptions{Mode: "standard", Agents: []string{"default"}})
	found := false
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Kind == "missing-rework-target" && diagnostic.Path == "lanes[approve].step.on_reject" {
			found = true
		}
	}
	if !found || report.OK(false) {
		t.Fatalf("unroutable revise was accepted: %+v", report.Diagnostics)
	}
	shipped, err := os.ReadFile(filepath.Join("blueprints", "two-step.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if good := Analyze(shipped, ValidationOptions{Mode: "standard", Agents: []string{"default"}}); !good.OK(true) {
		t.Fatalf("shipped revision has no valid route: %+v", good.Diagnostics)
	}
}

// TestPristineStaleDevLoopUpgradesButEditedCopyStaysPinned is the 2026-10-26
// investigation record as a test. A developer box ran `make dev-daemon` —
// which recompiles on every launch (go run), so the daemon was current — and
// the app still reported `lanes[git-abort].step.action: invalid — unknown
// action`. The daemon was right; the file it reads was not:
// `Resolve` prefers the state-directory copy, materialized 2026-10-18 from
// the embedded bytes committed at 923e9193, and the upgrade hatch recognised
// only two-step's recorded digests. S16 T10 retired
// `forge.closeOpenIssuesAndPRs` and S17 T7 re-authored the ladder; the
// pristine-but-never-edited copy could never catch up, and the honest
// validation of a stale file reads exactly like a stale daemon.
// testdata/dev-loop-pristine-923e9193.yaml holds those exact bytes
// (sha256 8af923d7a96748ed61dbaf9147138753da8ce8076174ffce4ec00143fc88e6ed,
// `git show 923e9193:aeon/internal/blueprint/blueprints/dev-loop.yaml`).
func TestPristineStaleDevLoopUpgradesButEditedCopyStaysPinned(t *testing.T) {
	stale, err := os.ReadFile(filepath.Join("testdata", "dev-loop-pristine-923e9193.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	path := filepath.Join(state, "blueprints", "dev-loop.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved, err := LegacyResolve("dev-loop"); err != nil || resolved != path {
		t.Fatalf("resolve: %q, %v", resolved, err)
	}
	if !strings.Contains(string(stale), "action: forge.closeOpenIssuesAndPRs") {
		t.Fatal("fixture drifted: the stale copy must carry the retired action as a real step")
	}
	upgraded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := os.ReadFile(filepath.Join("blueprints", "dev-loop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Equality with the shipped bytes is the assertion — not a string hunt:
	// the shipped file legitimately MENTIONS the retired action in the
	// comment above the lane that replaced it (dev-loop.yaml:189), so
	// Contains would fail the very upgrade this test exists to prove.
	if string(upgraded) != string(shipped) {
		t.Fatal("the upgrade did not reach the current shipped bytes")
	}
	// An edited copy — a person's own — is never touched, even though its
	// ancestor shipped: the digest match, not the ancestor, is what decides.
	edited := strings.Replace(string(shipped), "question: approve-plan", "question: my-own-decision", 1)
	if edited == string(shipped) {
		t.Fatal("edit fixture did not apply")
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LegacyResolve("dev-loop"); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != edited {
		t.Fatal("an edited dev-loop was overwritten")
	}
}
