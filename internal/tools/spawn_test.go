package tools

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T6's tests. Two of them are the reason this file is long: `oomKilled` from a
// process that is actually SIGKILLed, and group termination proven by a FREED
// PORT rather than by a return code.
//
// # Why a freed port
//
// Measured before the code was written (Q4): `Kill(-pgid)` frees the child's
// port, and killing only the parent does not. A return code cannot tell those
// apart — `bash` reports the same thing either way, because the shell did die.
// The port is the only observable that distinguishes "the group is gone" from
// "the parent is gone and its child is still holding a resource".
//
// The child is THIS TEST BINARY re-invoked, so the test needs no `nc`, no
// `python3`, and nothing outside the module.

// portHolderEnv makes the test binary act as a port holder rather than a test
// run. Checked in TestMain before anything else.
const (
	portHolderEnv = "S59_TEST_PORT_HOLDER"
	portReadyEnv  = "S59_TEST_PORT_READY"
)

func TestMain(m *testing.M) {
	if port := os.Getenv(portHolderEnv); port != "" {
		runPortHolder(port, os.Getenv(portReadyEnv))
		return
	}
	os.Exit(m.Run())
}

// runPortHolder listens on a port and holds it until it is killed.
//
// It gives up after ten seconds so that a test which fails to kill it does not
// leave a process behind for a minute.
func runPortHolder(port, readyPath string) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	if readyPath != "" {
		// Its own pid, so a test that kills only the PARENT has a way to clean
		// up afterwards: once the parent is gone this process is reparented
		// and its pid is the only handle left on it.
		_ = os.WriteFile(readyPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	time.Sleep(10 * time.Second)
	_ = ln.Close()
	os.Exit(0)
}

// freePort returns a port nothing is listening on.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func portHeld(port string) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", within, what)
}

func TestATimeoutKillsTheWHOLEGROUPProvenByAFreedPort(t *testing.T) {
	// The positive half. `bash` starts a grandchild holding a port and then
	// sleeps; the timeout fires; `Kill(-pgid)` takes both.
	port := freePort(t)
	ready := filepath.Join(t.TempDir(), "ready")

	t.Setenv(portHolderEnv, port)
	t.Setenv(portReadyEnv, ready)

	dir := t.TempDir()
	command := fmt.Sprintf("'%s' & sleep 30", os.Args[0])

	type outcome struct {
		res RunResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := spawnAuthorized(t, context.Background(), command, SpawnOptions{
			Dir:     dir,
			Timeout: 1500 * time.Millisecond,
		})
		done <- outcome{res, err}
	}()

	waitFor(t, "the grandchild to take the port", 8*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil && portHeld(port)
	})

	got := <-done
	if got.err != nil {
		t.Fatalf("spawn: %v", got.err)
	}
	if !got.res.TimedOut {
		t.Errorf("the command did not report a timeout: %+v", got.res)
	}

	// THE assertion. If only the parent had been killed, the grandchild would
	// still hold this port.
	waitFor(t, "the port to be freed by the group kill", 8*time.Second, func() bool {
		return !portHeld(port)
	})
}

func TestKillingOnlyTheParentLeavesThePortHeld(t *testing.T) {
	// The negative control, and the reason `Setpgid` + `Kill(-pgid)` is in the
	// code at all. Without this test, the one above passes against an
	// implementation that kills the process — because the shell dies either
	// way and the return code is identical.
	port := freePort(t)
	ready := filepath.Join(t.TempDir(), "ready")

	t.Setenv(portHolderEnv, port)
	t.Setenv(portReadyEnv, ready)

	shell, shellArgs := Shell()
	command := fmt.Sprintf("'%s' & sleep 30", os.Args[0])
	cmd := exec.Command(shell, append(shellArgs, command)...)
	cmd.Dir = t.TempDir()
	// Deliberately /dev/null rather than a buffer: a buffer makes `Wait` block
	// until every writer of the pipe closes, and the grandchild is one of them
	// — which would hang this test for the ten seconds the holder lives.
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid

	waitFor(t, "the grandchild to take the port", 8*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil && portHeld(port)
	})

	// Kill the PROCESS, not the group.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// The parent is gone and the port is still held. Give it a moment so the
	// assertion is not a race won by being fast.
	time.Sleep(300 * time.Millisecond)
	if !portHeld(port) {
		t.Fatal("the port was freed by killing only the parent — the control is not controlling " +
			"anything, and the group-kill test above proves nothing")
	}

	// Clean up by pid, so the test leaves nothing behind. Note what this takes:
	// the grandchild's own pid, read from a file it wrote. `Kill(-pid)` on the
	// shell's pid would signal nothing, because without `Setpgid` the shell was
	// never the leader of its own group — it was in THIS process's group, and a
	// negative kill there would have killed the test run.
	holderPID, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	stray, err := strconv.Atoi(strings.TrimSpace(string(holderPID)))
	if err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(stray, syscall.SIGKILL)
	waitFor(t, "the port to be freed once the grandchild is killed by pid", 8*time.Second, func() bool {
		return !portHeld(port)
	})
	_ = pid
}

func TestARealSIGKILLIsReportedAsOOMKilled(t *testing.T) {
	// D11, from an actual SIGKILL rather than a hand-built fixture: the shell
	// kills itself, the kernel reports exit=-1 and signal=killed, and the
	// heuristic catches it.
	res, err := spawnAuthorized(t, context.Background(), "kill -9 $$", SpawnOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !res.OOMKilled {
		t.Fatalf("a real SIGKILL was not reported as killed: %+v", res)
	}
	if res.Signal != syscall.SIGKILL.String() {
		t.Errorf("signal is %q, want %q", res.Signal, syscall.SIGKILL.String())
	}
	if res.ExitCode != -1 {
		t.Errorf("exit code is %d; a signalled process has no exit code", res.ExitCode)
	}
	if res.TimedOut {
		t.Error("a SIGKILLed command was also reported as timed out")
	}
}

func TestAn137ExitIsAlsoReportedAsKilledBecauseItIsIndistinguishable(t *testing.T) {
	// Measured (Q4c): `exit 137` reports exit=137 and NO signal, while a real
	// SIGKILL reports exit=-1 and signal=killed. Both have to be caught, and
	// the two being indistinguishable from outside is exactly why the field is
	// a heuristic and why the rendered message says so.
	res, err := spawnAuthorized(t, context.Background(), "exit 137", SpawnOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !res.OOMKilled {
		t.Errorf("exit 137 was not reported as killed: %+v", res)
	}
	if res.Signal != "" {
		t.Errorf("signal is %q; a plain exit carries none", res.Signal)
	}
	if res.ExitCode != 137 {
		t.Errorf("exit code is %d", res.ExitCode)
	}
}

func TestTheRenderedKillMessageDoesNotClaimItWasOutOfMemory(t *testing.T) {
	// A diagnosis this layer cannot make. `kill -9` from a person, from a
	// supervisor and from the OOM killer are the same signal.
	res, _ := spawnAuthorized(t, context.Background(), "kill -9 $$", SpawnOptions{Dir: t.TempDir()})
	rendered := renderRun("kill -9 $$", res)

	if strings.Contains(strings.ToLower(rendered), "out of memory") &&
		!strings.Contains(rendered, "cannot tell them apart") {
		t.Errorf("the message diagnoses an OOM: %s", rendered)
	}
	if !strings.Contains(rendered, "cannot tell them apart") {
		t.Errorf("the message does not state the ambiguity: %s", rendered)
	}
}

func TestATimeoutIsMandatoryAndADirectoryIsRequired(t *testing.T) {
	// D7. No command runs without a bound, and none runs in the daemon's own
	// directory — which would be running in the wrong project.
	if _, err := spawnAuthorized(t, context.Background(), "true", SpawnOptions{}); err == nil {
		t.Fatal("a command with no working directory ran")
	} else if !strings.Contains(err.Error(), "workspace root") {
		t.Errorf("the error does not say what is missing: %v", err)
	}

	// A zero timeout becomes the default rather than meaning "no bound".
	start := time.Now()
	res, err := spawnAuthorized(t, context.Background(), "sleep 0.2", SpawnOptions{Dir: t.TempDir(), Timeout: 0})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if res.TimedOut {
		t.Error("a zero timeout was treated as an immediate deadline")
	}
	if time.Since(start) > 10*time.Second {
		t.Error("a zero timeout was treated as no bound")
	}
}

func TestATimeoutStopsTheCommandAndReportsPartialOutput(t *testing.T) {
	start := time.Now()
	res, err := spawnAuthorized(t, context.Background(), "echo SENTINEL-OUT; sleep 30", SpawnOptions{
		Dir: t.TempDir(), Timeout: 400 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("not reported as timed out: %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the command took %s to stop", elapsed)
	}
	if !strings.Contains(res.Stdout, "SENTINEL-OUT") {
		t.Errorf("the partial output was lost: %q", res.Stdout)
	}

	// The STATUS comes first in what the model reads. A model handed output
	// first will reason about the output and miss that the command was killed
	// half way through producing it.
	rendered := renderRun("echo SENTINEL-OUT; sleep 30", res)
	statusAt := strings.Index(rendered, "TIMED OUT")
	outputAt := strings.LastIndex(rendered, "SENTINEL-OUT")
	if statusAt < 0 {
		t.Fatalf("no status line: %s", rendered)
	}
	if statusAt > outputAt {
		t.Errorf("the output comes before the status:\n%s", rendered)
	}
	if !strings.Contains(rendered, "partial") {
		t.Errorf("the message does not say the output is partial: %s", rendered)
	}
}

func TestACancelledContextStopsTheCommandWithoutClaimingATimeout(t *testing.T) {
	// A step cancelled by its caller and a command that ran too long need
	// different responses, and both would otherwise read as "timed out".
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	res, err := spawnAuthorized(t, ctx, "sleep 30", SpawnOptions{Dir: t.TempDir(), Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if res.TimedOut {
		t.Error("a cancellation was reported as a timeout")
	}
}

func TestStdinIsClosedSoAWaitingCommandReturnsImmediately(t *testing.T) {
	// D7's other half. Measured: `cat` returns in 4ms with stdin at
	// /dev/null, and blocks until the timeout without it — which is ten
	// minutes of a step doing nothing.
	for _, command := range []string{"cat", "read x", "head -1"} {
		start := time.Now()
		res, err := spawnAuthorized(t, context.Background(), command, SpawnOptions{
			Dir: t.TempDir(), Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if res.TimedOut {
			t.Errorf("%q blocked on stdin until the timeout", command)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("%q took %s", command, elapsed)
		}
	}
}

func TestOutputIsCappedAndTheTrueSizeIsReported(t *testing.T) {
	// D9. A buffer that stopped counting could say "truncated" and not
	// "truncated at 64 KiB of 9 MiB" — and the second is what tells a model
	// whether to narrow its command or give up.
	res, err := spawnAuthorized(t, context.Background(), "yes aeon | head -c 200000", SpawnOptions{
		Dir: t.TempDir(), Timeout: 20 * time.Second, StdoutCap: 1024,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !res.StdoutTruncated {
		t.Fatalf("not reported as truncated: %d bytes kept", len(res.Stdout))
	}
	if len(res.Stdout) != 1024 {
		t.Errorf("kept %d bytes, want the cap of 1024", len(res.Stdout))
	}
	if res.StdoutBytes != 200000 {
		t.Errorf("the true size is reported as %d, want 200000", res.StdoutBytes)
	}

	rendered := renderRun("yes aeon | head -c 200000", res)
	if !strings.Contains(rendered, "first 65536 bytes of") && !strings.Contains(rendered, "of 195.3 KiB") {
		t.Errorf("the rendered output does not name the true size: %s", firstLines(rendered, 4))
	}
}

func TestStderrIsCappedSeparately(t *testing.T) {
	res, err := spawnAuthorized(t, context.Background(), "yes aeon | head -c 5000 >&2", SpawnOptions{
		Dir: t.TempDir(), Timeout: 20 * time.Second, StderrCap: 512,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !res.StderrTruncated {
		t.Error("stderr was not truncated")
	}
	if res.StderrBytes != 5000 {
		t.Errorf("stderr's true size is %d", res.StderrBytes)
	}
	if res.StdoutTruncated {
		t.Error("stdout was reported truncated for stderr's output")
	}
}

func TestANonZeroExitIsAFactAndNotAnError(t *testing.T) {
	// A command that ran and returned 1 is a command that ran. Only a failure
	// to run AT ALL is an error — and marking `grep -q` finding nothing as a
	// tool failure would make a model retry a command that worked.
	res, err := spawnAuthorized(t, context.Background(), "exit 3", SpawnOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("a non-zero exit was returned as an error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit code is %d", res.ExitCode)
	}
}

func TestTheCommandRunsInTheWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := spawnAuthorized(t, context.Background(), "pwd", SpawnOptions{Dir: dir})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != real && got != dir {
		t.Errorf("the command ran in %q, want %q", got, real)
	}
}

func TestTheShellSkipsTheOperatorsDotfiles(t *testing.T) {
	// `--noprofile --norc`. The environment is inherited and that is a stated
	// limitation (scrubbing it would break every command needing PATH, and
	// deciding WHICH variables a step may see is S11's policy decision) — but
	// the operator's dotfiles are the part that can be fixed here, and an
	// alias or a `cd` in a profile would change what every command does.
	name, args := Shell()
	if !strings.HasSuffix(name, "bash") {
		t.Errorf("the shell is %q", name)
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--noprofile", "--norc", "-c"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("the shell flags %v do not include %s", args, flag)
		}
	}
	if args[len(args)-1] != "-c" {
		t.Errorf("-c is not last in %v; the command must follow it", args)
	}
}

func TestDurationIsMeasuredAndReported(t *testing.T) {
	res, err := spawnAuthorized(t, context.Background(), "sleep 0.2", SpawnOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if res.Duration < 150*time.Millisecond {
		t.Errorf("duration is %s for a 200ms command", res.Duration)
	}
	if !strings.Contains(renderRun("sleep 0.2", res), "exit 0") {
		t.Error("the status line is missing")
	}
}

func TestAnUnrunnableCommandIsAnErrorRatherThanAnExitCode(t *testing.T) {
	// A directory that does not exist. Distinguished from a command that ran
	// and failed, because the two send a model to different places: one is a
	// command to fix, the other is a step that was set up wrong.
	_, err := spawnAuthorized(t, context.Background(), "true", SpawnOptions{
		Dir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err == nil {
		t.Fatal("a command in a missing directory ran")
	}
	if !strings.Contains(err.Error(), "could not start") && !strings.Contains(err.Error(), "not complete") {
		t.Errorf("the error does not say it failed to start: %v", err)
	}
}

// ── the token requirement (T13, G16/QAP-15) ─────────────────────────────────

func TestTheZeroTokenIsRefusedAndNothingSpawns(t *testing.T) {
	// The refusal half of G16, from inside the package: the zero token is
	// not a "nobody said" that defaults open — it is a refusal, delivered as
	// an ERROR (not a RunResult), and the proof it matters is the file the
	// command would have created. The check sits before `exec`, so there is
	// no process and no marker: a RunResult-shaped refusal could still have
	// forked, and this cannot.
	dir := t.TempDir()
	res, err := Spawn(context.Background(), "touch marker.txt", SpawnOptions{Dir: dir}, policy.AllowToken{}, testScrubbedEnv(t))
	if err == nil {
		t.Fatalf("the zero token spawned; result: %+v", res)
	}
	if !strings.Contains(err.Error(), "allow token") {
		t.Errorf("the refusal does not name the missing token: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "marker.txt")); statErr == nil {
		t.Fatal("the refused command created its file — the token check does not sit before exec")
	}

	// The same refusal must precede the DIRECTORY bound: an unauthorized
	// command is refused as unauthorized even when it is also malformed in
	// some other way — the first refusal is the security one.
	if _, err := Spawn(context.Background(), "touch marker.txt", SpawnOptions{}, policy.AllowToken{}, testScrubbedEnv(t)); err == nil ||
		!strings.Contains(err.Error(), "allow token") {
		t.Errorf("the token check is not the FIRST statement: %v", err)
	}
}

func TestATokenForOneCommandDoesNotAuthorizeAnother(t *testing.T) {
	// The binding half, end to end: mint through the real engine for
	// `echo minted`, then attempt the SPAWN of a different string with it.
	dir := t.TempDir()
	v := policy.Evaluate(
		[]policy.Policy{commandRowT(t, "bind-allow", "*", "allow")},
		policy.Request{Type: policy.CommandAccess, Agent: "spawn-test", Tool: "bash", Command: "echo minted"},
		time.Now(),
	)
	token, ok := v.Token("echo minted")
	if !ok {
		t.Fatal("the harness's allow refused to mint")
	}
	if _, err := Spawn(context.Background(), "touch marker.txt", SpawnOptions{Dir: dir}, token, testScrubbedEnv(t)); err == nil {
		t.Fatal("a token for one command spawned another")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "marker.txt")); statErr == nil {
		t.Fatal("the mismatched token created its file")
	}
	// And the same token still does its own job.
	res, err := Spawn(context.Background(), "echo minted", SpawnOptions{Dir: dir}, token, testScrubbedEnv(t))
	if err != nil {
		t.Fatalf("the authorized command was refused: %v", err)
	}
	if !strings.Contains(res.Stdout, "minted") {
		t.Errorf("the authorized command did not run: %+v", res)
	}
}

func TestTheCappedBufferCountsPastItsCap(t *testing.T) {
	// The unit behind D9. It never returns an error from Write, so a command
	// producing gigabytes is capped rather than killed: the OUTPUT is the thing
	// being bounded, not the command.
	b := &cappedBuffer{limit: 10}
	for i := 0; i < 100; i++ {
		n, err := b.Write([]byte("0123456789"))
		if err != nil {
			t.Fatalf("write %d returned %v", i, err)
		}
		if n != 10 {
			t.Errorf("write %d reported %d bytes written", i, n)
		}
	}
	if got := b.String(); len(got) != 10 {
		t.Errorf("kept %d bytes, want 10", len(got))
	}
	if b.total != 1000 {
		t.Errorf("counted %d bytes, want 1000", b.total)
	}
	if !b.truncated {
		t.Error("not marked truncated")
	}
}
