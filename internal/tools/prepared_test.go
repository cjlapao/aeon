package tools

// The three guards `Spawn` applies before it builds anything, re-applied to a
// command the container driver built (S59 T9, EV13). Each is asserted WITH its
// control: the same command, guard satisfied, runs — so a guard that fires on
// nothing cannot pass.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

func prepared(tb testing.TB, prints string) *exec.Cmd {
	tb.Helper()
	cmd := exec.Command("/bin/sh", "-c", prints)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/bin:/usr/bin"} // non-nil on purpose: the guard below is the point
	return cmd
}

func TestPreparedCommandWithoutEnvIsRefused(t *testing.T) {
	const cmd = "echo hi"
	good, err := RunPrepared(context.Background(), prepared(t, cmd), PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil)
	if err != nil {
		t.Fatalf("the control (an explicit environment) was refused: %v", err)
	}
	if !strings.Contains(good.Stdout, "hi") {
		t.Fatalf("the control captured nothing (%q) — the guard's pass would prove nothing", good.Stdout)
	}

	bare := prepared(t, cmd)
	bare.Env = nil // what `exec.Cmd` reads as "give the child my own environment"
	_, err = RunPrepared(context.Background(), bare, PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil)
	if err == nil {
		t.Fatal("a prepared command with a nil environment ran — that is the daemon's own environment in the child (EV10)")
	}
	if !strings.Contains(err.Error(), "nil environment") || !strings.Contains(err.Error(), "EV10") {
		t.Errorf("the refusal does not name the leak: %v", err)
	}
}

func TestPreparedCommandNeedsItsOwnToken(t *testing.T) {
	cmd := "echo mine"
	if _, err := RunPrepared(context.Background(), prepared(t, "echo mine"), PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil); err != nil {
		t.Fatalf("the control was refused: %v", err)
	}
	_, err := RunPrepared(context.Background(), prepared(t, "echo mine"), PreparedOptions{Command: "echo somebody-else"}, mintSpawnToken(t, cmd), nil)
	if err == nil || !strings.Contains(err.Error(), "allow token") {
		t.Errorf("a token minted for one command authorized another through the prepared door: %v", err)
	}
	_, err = RunPrepared(context.Background(), prepared(t, "echo mine"), PreparedOptions{Command: cmd}, policy.AllowToken{}, nil)
	if err == nil || !strings.Contains(err.Error(), "allow token") {
		t.Errorf("the zero token passed the prepared door: %v", err)
	}
}

func TestPreparedCommandNeedsADirectory(t *testing.T) {
	cmd := "echo dir"
	noDir := prepared(t, cmd)
	noDir.Dir = ""
	if _, err := RunPrepared(context.Background(), noDir, PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil); err == nil ||
		!strings.Contains(err.Error(), "working directory") {
		t.Errorf("a prepared command with no directory was accepted: %v", err)
	}
	if _, err := RunPrepared(context.Background(), nil, PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil); err == nil {
		t.Error("a nil prepared command reported an empty result instead of a refusal")
	}
}

// The bounds are the same defaults `Spawn` uses, so a driver that passes zeros
// gets the same command it would have got from `Spawn` — one definition of the
// defaults, not a second one beside the container.
func TestPreparedCommandAppliesTheSharedDefaults(t *testing.T) {
	const cmd = "echo bounded"
	res, err := RunPrepared(context.Background(), prepared(t, cmd), PreparedOptions{Command: cmd}, mintSpawnToken(t, cmd), nil)
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "bounded") {
		t.Errorf("the run reported %+v", res)
	}
	if res.TimedOut {
		t.Errorf("an echo timed out under the shared defaults")
	}
}
