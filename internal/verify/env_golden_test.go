package verify

// env_golden_test.go — S59 T1 (characterization, P2 Phase A). The gate
// runner is the SECOND production caller of tools.Spawn (the first is the
// bash tool), and T0's pre-flight ruled that the second caller's
// environment behaviour belongs in the golden set even though T1.md's
// file list predates S57 T4 adding it.
//
// What this pins, on the untouched code:
//
//  1. The gate child does NOT inherit the daemon environment. A sentinel
//     and a credential name the daemon process carries are both INVISIBLE
//     to a gate command, while the closed allowlist (PATH among them) and
//     the constant CI=1 ARE visible — the scrub is live, not declared.
//
//  2. The exact SpawnOptions the runner passes: Dir as given, the
//     timeout clamped at the bash ceiling, and an Env that is byte-for-
//     byte tools.GateEnv(os.Environ()) — the scrub is one function,
//     called here and nowhere else.
//
// T5 turns the allowlist into the environment record's host-scrubbed
// default. This test's assertions survive that change by construction
// (the record's scrubbed default is the same allowlist), and if one does
// not survive, the change was bigger than the spec.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	gateLeakName = "AEON_S59_GATE_LEAK"
	gateLeakVal  = "s59-gate-leak-sentinel"
	gateKeyVal   = "ghs_s59_fake_not_real"
)

// The probe writes its findings to a file in the gate's directory,
// because a gate that PASSES records no output tail (the record keeps
// only non-pass output) — the file is the only way to read what the
// child saw without changing the gate's outcome.
const gateProbe = `printf 'LEAK=[%s]\nTOKEN=[%s]\nPATH_SET=[%s]\nCI=[%s]\n' "$AEON_S59_GATE_LEAK" "$GITHUB_TOKEN" "$([ -n "$PATH" ] && echo 1 || echo 0)" "$CI" > probe.out`

// TestGateRunnerEnvGolden — the gate's environment, pinned from both
// sides: what the child sees (subtest 1, a real spawn) and what the
// runner passed (subtest 2, a capturing spawner).
func TestGateRunnerEnvGolden(t *testing.T) {
	t.Setenv(gateLeakName, gateLeakVal)
	t.Setenv("GITHUB_TOKEN", gateKeyVal)

	t.Run("the-child-sees-scrub-not-inherit", func(t *testing.T) {
		r := Runner{Verdict: allowAllSeam(t, "chat")}
		dir := t.TempDir()
		g := GateSpec{Name: "golden", Command: gateProbe, Requirement: "required", TimeoutS: 30}
		res := r.RunGate(context.Background(), dir, "chat", g)

		if res.State != "pass" || res.ExitCode == nil || *res.ExitCode != 0 {
			t.Fatalf("the probe did not run as a pass: %+v", res)
		}
		data, err := os.ReadFile(filepath.Join(dir, "probe.out"))
		if err != nil {
			t.Fatalf("the probe never wrote its findings: %v", err)
		}
		want := "LEAK=[]\nTOKEN=[]\nPATH_SET=[1]\nCI=[1]\n"
		if string(data) != want {
			t.Fatalf("the gate child's view of the environment is not the scrub:\n  now:  %q\n  gold: %q", data, want)
		}
	})

	t.Run("the-opts-are-byte-identical-gate-env", func(t *testing.T) {
		var gotOpts tools.SpawnOptions
		var gotCmd string
		r := Runner{
			Verdict: allowAllSeam(t, "chat"),
			Now:     time.Now,
			Spawn: func(ctx context.Context, command string, opts tools.SpawnOptions, tok policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
				gotCmd, gotOpts = command, opts
				return tools.RunResult{ExitCode: 0, Stdout: "ok\n"}, nil
			},
		}
		dir := t.TempDir()
		res := r.RunGate(context.Background(), dir, "chat",
			GateSpec{Name: "golden", Command: gateProbe, Requirement: "required", TimeoutS: 30})
		if res.State != "pass" {
			t.Fatalf("the captured run did not pass: %+v", res)
		}
		if gotCmd != gateProbe {
			t.Fatalf("the runner changed the gate command: %q", gotCmd)
		}
		if gotOpts.Dir != dir {
			t.Fatalf("the runner changed the gate directory: %q", gotOpts.Dir)
		}
		if gotOpts.Timeout != 30*time.Second {
			t.Fatalf("the 30s gate timeout was not passed through: %s", gotOpts.Timeout)
		}
		want := tools.GateEnv(os.Environ())
		if !slices.Equal(gotOpts.Env, want) {
			t.Fatalf("the gate environment is not GateEnv byte for byte:\n  now:  %v\n  gold: %v", gotOpts.Env, want)
		}

		// The ceiling: a gate that names a longer timeout gets the bash
		// ceiling, never its own number.
		res = r.RunGate(context.Background(), dir, "chat",
			GateSpec{Name: "long", Command: gateProbe, Requirement: "required", TimeoutS: 9999})
		if res.State != "pass" {
			t.Fatalf("the long-gate run did not pass: %+v", res)
		}
		if gotOpts.Timeout != tools.BashTimeout {
			t.Fatalf("the 9999s gate was not clamped to the bash ceiling: %s", gotOpts.Timeout)
		}
	})
}
