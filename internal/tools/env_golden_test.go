package tools

// env_golden_test.go — S57 T1 (characterization, P2 Phase A). The bash tool's
// CHILD inherits the daemon's environment — `Spawn` sets `cmd.Env = nil`,
// and a nil Env means "inherit the parent", which is the stated behaviour
// (the I5 ambient-state limitation, S5). This golden pins the child's view:
// a variable the parent set is VISIBLE to the child (inheritance proven,
// not assumed), and an ambient variable (PATH) survives too.
//
// T4 adds `SpawnOptions.Env []string`; `nil` must remain BYTE-IDENTICAL to
// today's behaviour — this golden is the pin T4 must not move. A non-nil
// `Env` is a NEW behaviour T4 pins in its OWN test; nothing here changes.
//
// # Amended by S59 T5, and the reason the goldens still match byte for byte.
//
// `Spawn` no longer accepts "no environment": a caller that passes an inert
// `execenv.ChildEnv` and no `SpawnOptions.Env` is refused, because that
// combination used to mean "inherit the daemon" and inheritance is what EV1
// closes. This golden keeps its pinned bytes — a MARKER the parent set IS
// visible, PATH survives — because it now runs against `execenv.LegacyInherit()`,
// the NAMED value of the `host-inherit` backend (EV6): the one place the
// inherit-everything child still exists, reachable only from an environment
// record that says so, on an attended loop, and never as a default.
//
// What the default does instead is pinned by the sibling
// `TestSpawnInheritsEnvironmentTODAY` in `spawn_env_current_test.go`, whose
// assertions flipped in the same commit that amended this header — the name
// kept, so the table in T1's task file still points at the thing it pinned. Two tests, two backends, no ambiguity: a child
// either has a record, or it has a name saying it inherits.
//
// Determinism: only FIXED variables are echoed (a marker set here plus the
// presence of PATH), never the host's ambient values, so the golden cannot
// drift with the machine.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/tools/ -run TestBashInheritsTheDaemonEnvironment -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
)

const goldEnvDir = "env_golden"

// envProbeCmd is the exact command the child runs: it prints the marker the
// parent set and whether PATH is non-empty. It is a fixed string so the
// policy token can be minted for it and the golden is reproducible.
const envProbeCmd = `printf 'MARKER=%s\nPATH_SET=%s\n' "$AEON_GOLD_ENV_MARKER" "$([ -n "$PATH" ] && echo 1 || echo 0)"`

type goldEnvRow struct {
	Case   string `json:"case"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr,omitempty"`
}

func mintSpawnToken(t *testing.T, command string) policy.AllowToken {
	t.Helper()
	v := policy.Evaluate(
		[]policy.Policy{commandRowT(t, "env-golden-allow", "*", "allow")},
		policy.Request{Type: policy.CommandAccess, Agent: "env-golden", Tool: "bash", Command: command},
		time.Now(),
	)
	token, ok := v.Token(command)
	if !ok {
		t.Fatal("the harness's allow refused to mint")
	}
	return token
}

func writeGoldEnvRow(t *testing.T, row goldEnvRow) {
	t.Helper()
	dir := filepath.Join("testdata", goldEnvDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, row.Case+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBashInheritsTheDaemonEnvironment — the child sees the parent's variables
// (cmd.Env = nil ⇒ inherit). T4's `Env []string` is nil here, so this row
// is the byte-identical baseline T4 must preserve.
func TestBashInheritsTheDaemonEnvironment(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	dir := t.TempDir()
	// The marker the child must see. t.Setenv puts it in THIS process's
	// environment; Spawn's nil Env is what lets the child read it.
	t.Setenv("AEON_GOLD_ENV_MARKER", "inheritance-pin")

	token := mintSpawnToken(t, envProbeCmd)
	res, err := Spawn(context.Background(), envProbeCmd, SpawnOptions{Dir: dir}, token, execenv.LegacyInherit())
	if err != nil {
		t.Fatalf("the authorized probe was refused: %v", err)
	}

	row := goldEnvRow{Case: "bash-inherits-parent-env", Stdout: res.Stdout, Stderr: res.Stderr}
	// The inheritance claim, asserted live even on a read (a golden that
	// only matches bytes would still pass for a scrubbed environment that
	// happens to scrub nothing here).
	if res.Stdout != "MARKER=inheritance-pin\nPATH_SET=1\n" {
		t.Fatalf("the child did not inherit the parent's environment:\n%s", res.Stdout)
	}
	if write {
		writeGoldEnvRow(t, row)
		return
	}
	data, rerr := os.ReadFile(filepath.Join("testdata", goldEnvDir, "bash-inherits-parent-env.json"))
	if rerr != nil {
		t.Fatalf("golden missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", rerr)
	}
	var want goldEnvRow
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if want.Stdout != row.Stdout {
		t.Fatalf("env golden drifted\n  now:  %q\n  gold: %q", row.Stdout, want.Stdout)
	}
	if want.Stderr != row.Stderr {
		t.Fatalf("stderr golden drifted\n  now:  %q\n  gold: %q", row.Stderr, want.Stderr)
	}
}
