package main

// S60 T1 · MQ characterization golden for the CLI's `action` surface — the
// one renderer's whole today-output over the closed registry.
//
// Header of who changes what:
//
//   - action_check — T5 appends git.integrate/git.concludeIntegration/
//     git.publishIntegration and T6 appends forge.openLandingPR/forge.land
//     (registry 10 → 16 including verify.run already here from S57). The
//     expectation is a DIFF THAT ONLY ADDS BLOCKS IN NAME ORDER: every
//     block this file mints today must survive byte for byte (the registry
//     is closed — D14 — so no existing answer can legitimately move).
//     `action list`'s table shape is already pinned by the pre-existing
//     action_test.go; this golden carries what that test deliberately does
//     not: the full CheckAnswer text of every name.
//
// No store, no git, no clock: CheckAnswer is a pure function of the
// registry constants.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
)

var mqCmdGoldenUpdate = flag.Bool("mq-golden-update", false, "rewrite cmd/aeon/testdata/mq_golden from today's behaviour (Phase A ritual only)")

func mqCmdGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "mq_golden", name+".golden")
	if *mqCmdGoldenUpdate {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run `-run Golden -mq-golden-update` on untouched code to mint it): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s drifted from the golden.\nIf the drift is a task this file's header says is expected, update the golden AND say what changed (adds only — no existing block may move).\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}

// TestActionCheckSurfaceGolden walks every registered name through the CLI
// verb itself (not the library call the existing test compares) and pins
// the printed bytes plus the refusal line for a junk name.
func TestActionCheckSurfaceGolden(t *testing.T) {
	var out strings.Builder
	for _, name := range action.Names() {
		var b strings.Builder
		if err := cmdAction(&b, []string{"check", name}); err != nil {
			t.Fatalf("check %s: %v", name, err)
		}
		out.WriteString("=== check " + name + "\n")
		out.WriteString(strings.TrimRight(b.String(), "\n"))
		out.WriteString("\n---\n")
	}
	// The refusal is part of the surface a task's output may quote.
	var b strings.Builder
	err := cmdAction(&b, []string{"check", "git.notAnAction"})
	if err == nil {
		t.Fatal("unknown name must be refused")
	}
	out.WriteString("=== refused: " + err.Error() + "\n")
	// The table, once, so T5's/T6's appended rows land in a pinned frame.
	b.Reset()
	if err := cmdAction(&b, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	out.WriteString("=== list\n")
	out.WriteString(b.String())
	mqCmdGolden(t, "action_check", out.String())
}
