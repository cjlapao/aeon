package tools

// spawn_golden_test.go — S59 T1 (characterization, P2 Phase A). The bounded
// process layer, pinned on the untouched code: the exit codes, the null
// stdin, the 64 KiB caps with their TRUE sizes, the timeout and its group
// kill, and the OOM heuristic — each through the real `Spawn`, and each
// with the exact `renderRun` bytes the model would read.
//
// T5 refactors `Spawn` so the child's environment is a positional
// `ChildEnv` (EV1/EV10). Nothing about the bounds may change: T5 must
// leave every live assertion here green and every committed render golden
// byte-identical. The one behaviour T5 flips on purpose is the
// INHERITANCE pinned in spawn_env_current_test.go — not this file.
//
// Determinism: the wall clock is the only non-deterministic field of a
// RunResult (and of `renderRun`, which prints it). The live assertions
// compare every field except Duration; the render goldens compare the
// exact bytes with Duration pinned to 42ms in the copy that is rendered.
// A golden that compared a real wall time would drift with the machine.
//
// The group-kill case reuses the port-holder machinery this package's
// TestMain already provides (spawn_test.go): the child is this test binary
// re-invoked, so the test needs nothing outside the module.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/tools/ -run TestSpawnBoundsGolden -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goldSpawnDir = "spawn_bounds"

// renderDuration is the fixed wall time the render goldens compare against.
// It is not a claim about how long the command took; it is a constant that
// makes the rendered bytes reproducible.
const renderDuration = 42 * time.Millisecond

type spawnCase struct {
	Name    string
	Command string
	Timeout time.Duration

	// The verdict: the fields a RunResult must carry. StdoutExact/StderrExact
	// are full-content assertions ("" means empty); the cap cases assert
	// their true sizes separately.
	ExitCode  int
	Signal    string // "" = must be empty; otherwise must equal
	SignalAny bool   // the timeout's group kill may land TERM or KILL
	TimedOut  bool
	OOMKilled bool
	Stdout    string
	StdoutLen int
	StdoutB   int // true stdout bytes, before truncation
	Stderr    string
	StderrLen int
	StderrB   int
	TruncOut  bool
	TruncErr  bool
}

func (c spawnCase) run(t *testing.T, dir string) RunResult {
	t.Helper()
	res, err := spawnAuthorized(t, context.Background(), c.Command, SpawnOptions{
		Dir: dir, Timeout: c.Timeout,
	})
	if err != nil {
		t.Fatalf("%s: the authorized spawn failed to run: %v", c.Name, err)
	}
	return res
}

func (c spawnCase) check(t *testing.T, res RunResult) {
	t.Helper()
	if res.ExitCode != c.ExitCode {
		t.Errorf("%s: exit code %d, want %d", c.Name, res.ExitCode, c.ExitCode)
	}
	switch {
	case c.SignalAny:
		if res.Signal == "" {
			t.Errorf("%s: no signal recorded for a group kill: %+v", c.Name, res)
		}
	case c.Signal != "":
		if res.Signal != c.Signal {
			t.Errorf("%s: signal %q, want %q", c.Name, res.Signal, c.Signal)
		}
	default:
		if res.Signal != "" {
			t.Errorf("%s: signal %q, want none", c.Name, res.Signal)
		}
	}
	if res.TimedOut != c.TimedOut {
		t.Errorf("%s: TimedOut %v, want %v", c.Name, res.TimedOut, c.TimedOut)
	}
	if res.OOMKilled != c.OOMKilled {
		t.Errorf("%s: OOMKilled %v, want %v (the heuristic, exit %d signal %q)",
			c.Name, res.OOMKilled, c.OOMKilled, res.ExitCode, res.Signal)
	}
	if c.StdoutLen == 0 && res.Stdout != c.Stdout {
		t.Errorf("%s: stdout %q, want %q", c.Name, res.Stdout, c.Stdout)
	}
	if c.StdoutLen > 0 && len(res.Stdout) != c.StdoutLen {
		t.Errorf("%s: kept %d stdout bytes, want %d", c.Name, len(res.Stdout), c.StdoutLen)
	}
	if c.StdoutB > 0 && res.StdoutBytes != c.StdoutB {
		t.Errorf("%s: true stdout size %d, want %d (the cap must keep counting)", c.Name, res.StdoutBytes, c.StdoutB)
	}
	if c.StderrLen == 0 && res.Stderr != c.Stderr {
		t.Errorf("%s: stderr %q, want %q", c.Name, res.Stderr, c.Stderr)
	}
	if c.StderrLen > 0 && len(res.Stderr) != c.StderrLen {
		t.Errorf("%s: kept %d stderr bytes, want %d", c.Name, len(res.Stderr), c.StderrLen)
	}
	if c.StderrB > 0 && res.StderrBytes != c.StderrB {
		t.Errorf("%s: true stderr size %d, want %d", c.Name, res.StderrBytes, c.StderrB)
	}
	if res.StdoutTruncated != c.TruncOut {
		t.Errorf("%s: StdoutTruncated %v, want %v", c.Name, res.StdoutTruncated, c.TruncOut)
	}
	if res.StderrTruncated != c.TruncErr {
		t.Errorf("%s: StderrTruncated %v, want %v", c.Name, res.StderrTruncated, c.TruncErr)
	}
}

// TestSpawnBoundsGolden — the table through the real Spawn, plus the exact
// renderRun bytes for each case (Duration pinned to renderDuration).
func TestSpawnBoundsGolden(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	goldDir := filepath.Join("testdata", goldSpawnDir)
	if write {
		if err := os.MkdirAll(goldDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []spawnCase{
		{Name: "exit-0", Command: `printf 'ok\n'`, ExitCode: 0, Stdout: "ok\n"},
		{Name: "exit-1", Command: `exit 1`, ExitCode: 1},
		{Name: "exit-127", Command: `definitely-not-a-command-s59`, ExitCode: 127,
			Stderr: "bash: line 1: definitely-not-a-command-s59: command not found\n"},
		// /dev/null stdin: a command waiting on input reads EOF and exits.
		// The bound on the wall time is the assertion that it did not block.
		{Name: "stdin-null", Command: `read x; printf 'rc=%s val=[%s]\n' "$?" "$x"`,
			ExitCode: 0, Stdout: "rc=1 val=[]\n", Timeout: 5 * time.Second},
		// The 64 KiB stdout cap, with the true size kept (D9).
		{Name: "stdout-cap", Command: `head -c 100000 /dev/zero | tr '\0' a`,
			ExitCode: 0, StdoutLen: BashStdoutCap, StdoutB: 100000, TruncOut: true},
		// The 64 KiB stderr cap, with the true size kept.
		{Name: "stderr-cap", Command: `head -c 70000 /dev/zero | tr '\0' b >&2`,
			ExitCode: 0, StderrLen: BashStderrCap, StderrB: 70000, TruncErr: true},
		// The timeout: partial output kept, the group killed, a signal named.
		{Name: "timeout-partial", Command: `printf 'partial\n'; sleep 30`,
			ExitCode: -1, SignalAny: true, TimedOut: true, Stdout: "partial\n", Timeout: 500 * time.Millisecond},
		// The OOM heuristic, form one: the exit code the kernel's killer
		// leaves, with no signal.
		{Name: "oom-137", Command: `exit 137`, ExitCode: 137, OOMKilled: true},
		// The OOM heuristic, form two: a real SIGKILL — indistinguishable at
		// this level, and said to be so by the render. The signal string is
		// what syscall.Signal(9).String() says on this platform, "killed"
		// (the /proc convention), and spawn.go compares against the same
		// .String(), so the OOM test stays consistent by construction.
		{Name: "oom-sigkill", Command: `kill -9 $$`, ExitCode: -1, Signal: "killed", OOMKilled: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			res := c.run(t, dir)
			c.check(t, res)

			// The render golden: the model-facing bytes, wall time pinned.
			norm := res
			norm.Duration = renderDuration
			render := renderRun(c.Command, norm)
			row := map[string]string{"command": c.Command, "render": render}
			path := filepath.Join(goldDir, c.Name+".json")
			if write {
				data, err := json.MarshalIndent(row, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("render golden missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", rerr)
			}
			var want map[string]string
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if want["render"] != render {
				t.Errorf("the renderRun bytes drifted for %q:\n  now:  %q\n  gold: %q", c.Name, render, want["render"])
			}
		})
	}

	// The group kill, proven by a freed port (D10): a return code cannot
	// tell "the group is gone" from "the parent is gone and its grandchild
	// still holds a resource", and the port can.
	t.Run("timeout-group", func(t *testing.T) {
		port := freePort(t)
		ready := filepath.Join(t.TempDir(), "ready")
		t.Setenv(portHolderEnv, port)
		t.Setenv(portReadyEnv, ready)

		command := fmt.Sprintf("'%s' & sleep 30", os.Args[0])
		dir := t.TempDir()

		type outcome struct {
			res RunResult
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			res, err := spawnAuthorized(t, context.Background(), command, SpawnOptions{
				Dir: dir, Timeout: 1500 * time.Millisecond,
			})
			done <- outcome{res, err}
		}()
		waitFor(t, "the grandchild to take the port", 8*time.Second, func() bool {
			_, err := os.Stat(ready)
			return err == nil && portHeld(port)
		})

		got := <-done
		if got.err != nil {
			t.Fatalf("the group spawn failed to run: %v", got.err)
		}
		if !got.res.TimedOut {
			t.Fatalf("the timed-out group did not report a timeout: %+v", got.res)
		}
		waitFor(t, "the port to be freed by the group kill", 8*time.Second, func() bool {
			return !portHeld(port)
		})

		norm := got.res
		norm.Duration = renderDuration
		render := renderRun(command, norm)
		// The command names the test binary, which lives in a build temp
		// path that changes on every build: the golden holds the shape
		// with the binary as a sentinel, not the path.
		sentinel := func(s string) string { return strings.ReplaceAll(s, os.Args[0], "<testbin>") }
		row := map[string]string{"command": sentinel(command), "render": sentinel(render)}
		path := filepath.Join(goldDir, "timeout-group.json")
		if write {
			data, err := json.MarshalIndent(row, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("render golden missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", rerr)
		}
		var want map[string]string
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if want["command"] != sentinel(command) {
			t.Errorf("the timeout-group command shape drifted:\n  now:  %q\n  gold: %q", sentinel(command), want["command"])
		}
		if want["render"] != sentinel(render) {
			t.Errorf("the renderRun bytes drifted for timeout-group:\n  now:  %q\n  gold: %q", sentinel(render), want["render"])
		}
	})
}

// The cap rows' kept bytes must be exactly the cap and exactly the filler —
// a buffer that dropped bytes in the middle would still show the right
// length and the right truncation flag.
func TestSpawnGoldenCapsKeepTheHeadNotTheMiddle(t *testing.T) {
	dir := t.TempDir()
	res, err := spawnAuthorized(t, context.Background(),
		`head -c 100000 /dev/zero | tr '\0' a`, SpawnOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Trim(res.Stdout, "a") != "" || len(res.Stdout) != BashStdoutCap {
		t.Fatalf("the kept stdout is not the first %d 'a' bytes: len=%d", BashStdoutCap, len(res.Stdout))
	}
	if res.StdoutBytes != 100000 {
		t.Fatalf("the true size was not kept: %+v", res)
	}
}
