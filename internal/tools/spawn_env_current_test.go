package tools

// spawn_env_current_test.go — S59 T1 (characterization, P2 Phase A).
//
// pins the CURRENT inherit behaviour (S59 EV1). T5 inverts this test; it
// must not be deleted, it must change its assertion.
//
// What it says today: `Spawn` sets `cmd.Env` to whatever `SpawnOptions.Env`
// holds, and every caller except the gate runner (S57 T4) leaves it nil —
// and a nil `cmd.Env` means "inherit the parent". So a sentinel the daemon
// process carries, and a credential name it carries, are BOTH VISIBLE to a
// `bash` child. That is the whole-environment inheritance EV1 exists to
// close: the child will run with a resolved environment record instead,
// and this test's two assertions will both flip to "not visible".
//
// The sibling golden (S57 T4's TestBashInheritsTheDaemonEnvironment) pins
// the SAME inheritance from the tool side and must not move; T5 inverts
// THIS one and updates that one's header in the same change.
//
// Sentinels are obviously-fake strings (T1 rule): this test never reads or
// writes a real credential, and the fake provider key it sets is a string
// no provider will accept.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
)

const (
	leakSentinelName = "AEON_SENTINEL_ENV"
	leakSentinelVal  = "s59-t1-inherit-sentinel"
	leakKeyName      = "ANTHROPIC_API_KEY"
	leakKeyVal       = "sk-ant-fake-s59-t1-not-a-real-key"
)

const leakProbeCmd = `printf 'SENTINEL=%s\nKEY=%s\n' "$AEON_SENTINEL_ENV" "$ANTHROPIC_API_KEY"`

// # Amended by S59 T5, in place, exactly as T1's header asked: not deleted,
// # flipped. The name is kept (with its misleading "TODAY") so the table in
// # `docs/aeon/S59-execution-environment/tasks/T1.md` still points at the test
// # that pinned the leak; what changed is what it demands.
// #
// # Today: the daemon's sentinel and a fake credential are BOTH INVISIBLE to the
// # `bash` child — the child got the step's built, host-scrubbed environment and
// # the daemon's own environment never crossed into it.
// #
// #	SENTINEL=
// #	KEY=
// #
// # The companion below keeps the OLD assertion alive against the one value that
// # still means inherit-everything, `execenv.LegacyInherit()`, so this flip is
// # provably about the environment the child was given and not about the probe,
// # the shell, or a broken capture.
func TestSpawnInheritsEnvironmentTODAY(t *testing.T) {
	t.Setenv(leakSentinelName, leakSentinelVal)
	t.Setenv(leakKeyName, leakKeyVal)

	dir := t.TempDir()
	res, err := spawnAuthorized(t, context.Background(), leakProbeCmd, SpawnOptions{Dir: dir})
	if err != nil {
		t.Fatalf("the authorized probe was refused: %v", err)
	}

	// The exact two lines, and nothing else: the probe prints the two
	// variables and no more. BOTH EMPTY — the inversion T1 recorded.
	want := "SENTINEL=\nKEY=\n"
	if res.Stdout != want {
		t.Fatalf("EV1 is not closed: the child saw the daemon's environment\n  now:  %q\n  gold: %q",
			res.Stdout, want)
	}
	if strings.Contains(res.Stdout, leakSentinelVal) || strings.Contains(res.Stdout, leakKeyVal) {
		t.Fatal("a daemon value reached the child's stdout")
	}
	if res.Stderr != "" {
		t.Fatalf("the probe wrote to stderr: %q", res.Stderr)
	}
	// The scrub is not "nothing at all": a command that cannot find `echo` is
	// a broken environment, not a safe one, and the two read the same in the
	// two lines above.
	if res.ExitCode != 0 {
		t.Fatalf("the scrubbed child could not run its own probe: exit %d, stderr %q", res.ExitCode, res.Stderr)
	}
}

// TestLegacyBackendStillInheritsAndSaysSo is the flip's control. It runs the
// SAME probe, in the SAME daemon process, with the SAME sentinels — and both
// are visible, because the caller passed the one value whose stated meaning is
// "inherit": `execenv.LegacyInherit()`, the `host-inherit` backend EV6 keeps
// for attended loops only.
//
// Without this, the test above could be passing because the probe is broken,
// the shell is unwired, or the capture silently drops output — three ways to
// look secure while being unable to see anything at all.
func TestLegacyBackendStillInheritsAndSaysSo(t *testing.T) {
	t.Setenv(leakSentinelName, leakSentinelVal)
	t.Setenv(leakKeyName, leakKeyVal)

	dir := t.TempDir()
	token := mintSpawnToken(t, leakProbeCmd)
	res, err := Spawn(context.Background(), leakProbeCmd, SpawnOptions{Dir: dir}, token, execenv.LegacyInherit())
	if err != nil {
		t.Fatalf("the legacy probe was refused: %v", err)
	}
	want := "SENTINEL=" + leakSentinelVal + "\nKEY=" + leakKeyVal + "\n"
	if res.Stdout != want {
		t.Fatalf("the legacy backend no longer means what its name says:\n  now:  %q\n  gold: %q", res.Stdout, want)
	}
}
