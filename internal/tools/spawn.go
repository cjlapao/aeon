package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
)

// The bounded process layer (W13).
//
// # What is bounded, and how each was verified
//
//	a mandatory timeout        measured: `sleep 9999` returns TimedOut with its
//	                           partial output
//	stdin closed               measured: Go gives a nil Stdin the null device,
//	                           so `cat` returns in 4ms rather than blocking
//	output caps                with the TRUE sizes kept, so truncation is
//	                           reported rather than silent (D9)
//	oomKilled reported         exit 137 OR signal SIGKILL — a heuristic, and
//	                           said to be one
//	THE WHOLE PROCESS GROUP    measured both ways, below
//
// # The process group, and the measurement that justifies it (Q4, D10)
//
//	Setpgid: true, then Kill(-pgid, …)   →  the child's port was FREED
//	killing only the parent              →  the port was STILL HELD
//
// The second is why D10 is proven by a freed port rather than a return code: a
// return code tells you the parent died and says **nothing** about its
// children. A shell that backgrounds a server and exits leaves the server
// holding its port, and the step looks finished.

// RunResult is one command's outcome.
type RunResult struct {
	ExitCode int

	// Signal names the signal that killed it, empty if none did.
	Signal string

	// TimedOut is true when the command hit its timeout rather than finishing.
	TimedOut bool

	// OOMKilled is a HEURISTIC, and that is stated wherever it is reported.
	//
	// It is true for exit 137 or a SIGKILL — which is what the kernel's OOM
	// killer produces, and also what `kill -9` from anything else produces.
	// Measured: a script running `exit 137` is indistinguishable from a real
	// OOM kill at this level. Reported as a fact about the signal, not as a
	// diagnosis of memory.
	OOMKilled bool

	Stdout string
	Stderr string

	// The TRUE byte counts, before truncation. Truncation without the true
	// size tells a model nothing about how much it is missing.
	StdoutBytes int
	StderrBytes int

	StdoutTruncated bool
	StderrTruncated bool

	// Duration is how long it ran, for the event stream.
	Duration time.Duration
}

// SpawnOptions configures one command.
type SpawnOptions struct {
	// Dir is the working directory. Required and absolute: a command run in the
	// daemon's directory is a command run in the wrong project (I5).
	Dir string

	// Timeout is mandatory. Zero takes BashTimeout rather than meaning
	// "forever" — a command with no timeout is a step that hangs.
	Timeout time.Duration

	// StdoutCap and StderrCap bound the captured output. Zero takes the
	// defaults.
	StdoutCap int
	StderrCap int

	// Env is the child's COMPLETE environment, and it is here for ONE caller:
	// the gate runner, which passes GateEnv(os.Environ()) — the scrubbed
	// allowlist S57 VG5 — because a gate is authority a loop will run and a
	// daemon secret must not reach one.
	//
	// # It is not the environment door any more (EV5), and the two cannot mix
	//
	// S59 makes the environment a resolved, positional `execenv.ChildEnv`
	// because "who may run what with which variables" is a decision with a
	// record behind it, not a slice a caller assembles. This field stays only
	// because the gate's scrub is a closed allowlist with a byte-pinned golden
	// (T1's), and re-deriving it through a Spec would move that golden without
	// changing the child's bytes. So: a command gets an environment from
	// exactly one of the two, and Spawn refuses every other combination —
	// including both, which would decide by field order.
	//
	// What did NOT survive is the inheritance default. A nil here no longer
	// means "the daemon's environment"; it means "this caller is the gate", and
	// a caller that is not the gate passes nil with an inert ChildEnv and is
	// refused.
	Env []string
}

// runOpts is what runProcess needs after the command has been built: the
// bounds, already defaulted. It is separate from SpawnOptions because those are
// the CALLER's words (0 means "use the default") and these are the resolved
// numbers, and a process handler that re-reads the caller's zero values is a
// second place the defaults live.
type runOpts struct {
	timeout   time.Duration
	stdoutCap int
	stderrCap int
}

// Spawn runs a command with every bound applied.
//
// `verdict` is the policy proof (S11, G16/QAP-15): Spawn refuses with an
// error (not a RunResult) when the token does not authorize this exact
// command. There is no way to call Spawn without one — that is the point.
// The check is the FIRST statement, before the directory bound, before the
// timeout is chosen, before anything a refactor could reorder: the refusal
// is a type demand (a positional parameter, not a field of SpawnOptions —
// an options struct with zero-value defaults is a door; a positional
// parameter is a compile-time demand) AND a runtime one (the zero value is
// inert, and an inert token never reaches exec.Command). `tools` may import
// `internal/policy` — only `runtime` carries the K22 ban, and policy
// imports neither package, so the edge adds no cycle.
//
// `env` is the environment proof (S59, EV5), and it is the FIFTH positional
// parameter for the same reason the token is the fourth: an environment is not
// a setting with a sensible default, it is a decision somebody made, and
// "someone remembered to set it" is not evidence that anyone did. An inert
// ChildEnv — the zero value, or anything that did not come from
// `execenv.Build` — is refused before the process is built, naming itself as
// the missing thing. Nothing in this package inherits the daemon's environment
// any more; the only wholesale-environment caller left is the gate, through
// SpawnOptions.Env, and the two sources are mutually exclusive.
//
// `red` (EV8) filters the child's output at the ONE place bytes are captured.
// nil means this command was resolved with no secrets, which is the common
// case; a non-nil redactor replaces both streams, and it is closed only after
// Wait so a secret split across the last chunk boundary still gets flushed.
//
// `ctx` is honoured: a cancelled step terminates the group promptly rather
// than waiting out the timeout.
func Spawn(ctx context.Context, command string, opts SpawnOptions, verdict policy.AllowToken,
	env execenv.ChildEnv) (RunResult, error) {
	return spawnWith(ctx, command, opts, verdict, env, nil)
}

// spawnWith is Spawn with the step's redactor attached (EV8). It is a separate
// function and not a sixth parameter because the public door's arity is the
// rule — an environment is a proof, and proofs are positional — while the
// filter is a property of the executor that already holds the resolved
// environment, and an executor is exactly the thing that built it.
//
// Every caller that resolved secrets uses this one; a nil filter means the
// command was resolved with no secrets, which is most commands and is not the
// same claim as "a secret went through and was not filtered".
func spawnWith(ctx context.Context, command string, opts SpawnOptions, verdict policy.AllowToken,
	env execenv.ChildEnv, red *execenv.Redactor) (RunResult, error) {
	if !verdict.Allows(command) {
		return RunResult{}, errors.New("tools: refusing to spawn: the command does not carry a " +
			"policy-issued allow token for this exact command")
	}
	if opts.Dir == "" {
		return RunResult{}, errors.New("tools: a command needs a working directory; it is the " +
			"workspace root, and running in the daemon's directory would be running in the wrong project")
	}
	// The environment arbitration, BEFORE anything is built. The order is the
	// rule: no environment at all is the dangerous case and it is the one that
	// reads as a refusal rather than a silent default.
	if env.IsZero() && opts.Env == nil {
		return RunResult{}, errors.New("tools: refusing to spawn: no execution environment was resolved — " +
			"the command would run with the daemon's own environment, which is the leak S59 exists to close")
	}
	if !env.IsZero() && opts.Env != nil {
		return RunResult{}, errors.New("tools: refusing to spawn: two environments were supplied, a resolved " +
			"ChildEnv and a wholesale slice; a command has one environment, decided by somebody who can say which")
	}
	childEnv, refusal := env.TryEnv()
	if refusal != nil {
		// Only reachable on the gate path, whose ChildEnv is inert by
		// definition; SpawnOptions.Env is what it runs with.
		childEnv = opts.Env
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = BashTimeout
	}
	stdoutCap := opts.StdoutCap
	if stdoutCap <= 0 {
		stdoutCap = BashStdoutCap
	}
	stderrCap := opts.StderrCap
	if stderrCap <= 0 {
		stderrCap = BashStderrCap
	}

	shell, shellArgs := Shell()
	cmd := exec.Command(shell, append(shellArgs, command)...)
	cmd.Dir = opts.Dir

	// # Stdin is the null device, deliberately
	//
	// A nil Stdin gives the child `/dev/null`, so a command waiting on input
	// reads EOF and exits rather than blocking until the timeout (D7).
	// Measured: `cat` returns in 4ms, `read x` in 1ms. Setting it explicitly
	// rather than relying on the default, because the default is the behaviour
	// being depended on.
	cmd.Stdin = nil

	// # The environment is BUILT, and that is the point (EV5, EV6)
	//
	// It used to be inherited, and the comment here explained why that was
	// acceptable. It was not acceptable; it was unavoidable without a decision
	// about which variables a step may see, and the decision now exists
	// (`execenv.Build`, one allowlist per environment, one record per step).
	// What survives from the old note is the shell flags: `--noprofile
	// --norc` is still what keeps the operator's dotfiles out of a build that
	// happens to be named `PATH`.
	cmd.Env = childEnv

	// Setpgid is what makes a group kill possible. Without it, `Kill(-pid)`
	// signals the daemon's own group — which would be a daemon that kills
	// itself when a command times out.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return runProcess(ctx, cmd, runOpts{timeout: timeout, stdoutCap: stdoutCap, stderrCap: stderrCap}, red)
}

// runProcess owns everything after the command is built: the group kill, the
// timeout, the capped capture, the signal and OOM reading.
//
// It was lifted out of `Spawn` unchanged in behaviour (the one risky change to
// existing code that S59 makes), because the other half of `Spawn` — the
// decisions about WHAT runs — now has proofs to check, and a proof checked
// inside a function that also owns the process is a proof that can be skipped
// by whoever grows the next entry point. `red` is the EV8 filter: when it is
// non-nil, the child's bytes go through it before they reach the capped
// buffers, which is the single capture boundary every sink of this step reads.
func runProcess(ctx context.Context, cmd *exec.Cmd, opts runOpts, red *execenv.Redactor) (RunResult, error) {
	// The timeout is a context of our own rather than `exec.CommandContext`'s
	// kill, because CommandContext kills only the PROCESS and this has to kill
	// the GROUP. The measurement at the top of this file is the reason.
	runCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	stdout := &cappedBuffer{limit: opts.stdoutCap}
	stderr := &cappedBuffer{limit: opts.stderrCap}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if red != nil {
		// The redactor sits OUTSIDE the cap on purpose: bytes are filtered as
		// they arrive, so a secret that falls past the cap is not merely
		// uncaptured, it is never written anywhere at all.
		outW, errW := red.Writer(stdout), red.Writer(stderr)
		cmd.Stdout, cmd.Stderr = outW, errW
		defer func() {
			// After Wait: the copy goroutines have finished, and Close flushes
			// the carried tail so a secret ending the stream is still redacted.
			_ = outW.Close()
			_ = errW.Close()
		}()
	}

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("tools: the command could not start: %w", err)
	}
	pgid := cmd.Process.Pid

	// A goroutine terminates the group when the context ends, whether that is
	// the timeout or the caller's cancellation.
	done := make(chan struct{})
	var terminated bool
	var termMu sync.Mutex
	go func() {
		select {
		case <-runCtx.Done():
			termMu.Lock()
			terminated = true
			termMu.Unlock()
			terminateGroup(pgid)
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)
	duration := time.Since(started)

	termMu.Lock()
	wasTerminated := terminated
	termMu.Unlock()

	res := RunResult{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutBytes:     stdout.total,
		StderrBytes:     stderr.total,
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
		Duration:        duration,
		// TimedOut reflects the TIMEOUT specifically, not any cancellation: a
		// step cancelled by its caller and a command that ran too long need
		// different responses, and both would otherwise read as "timed out".
		TimedOut: wasTerminated && errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}

	if state := cmd.ProcessState; state != nil {
		res.ExitCode = state.ExitCode()
		if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			res.Signal = ws.Signal().String()
		}
	}
	// Measured (Q4c): `exit 137` reports exit=137 and no signal, while a real
	// SIGKILL reports exit=-1 and signal=killed. Both have to be caught, and
	// the two being indistinguishable is exactly why this is a heuristic.
	res.OOMKilled = res.ExitCode == 137 || res.Signal == syscall.SIGKILL.String()

	// A non-zero exit is NOT an error from this function: the command ran, and
	// what it returned is a fact for the model. Only a failure to run at all
	// is an error.
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return res, fmt.Errorf("tools: the command did not complete: %w", waitErr)
		}
	}
	return res, nil
}

// terminateGroup signals the whole process group: SIGTERM, then SIGKILL after
// the grace period.
//
// The negative pid is the mechanism (Q4). SIGTERM first because a command that
// can clean up should get to; SIGKILL after, because one that will not exit
// must.
func terminateGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(BashKillGrace)
	// Unconditional: checking whether it is still alive first is a race, and
	// SIGKILL to a dead group is a harmless ESRCH.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// cappedBuffer keeps the first `limit` bytes and counts ALL of them.
//
// # Why it keeps counting after the cap
//
// So truncation can be reported with the TRUE size (D9). A buffer that stopped
// counting could say "truncated" and not "truncated at 64 KiB of 9 MiB" — and
// the second is what tells a model whether to narrow its command or give up.
//
// It never returns an error from Write, so a command producing gigabytes is
// capped rather than killed: the output is the thing being bounded, not the
// command.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	total     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total += len(p)
	room := c.limit - c.buf.Len()
	switch {
	case room <= 0:
		c.truncated = true
	case len(p) > room:
		c.buf.Write(p[:room])
		c.truncated = true
	default:
		c.buf.Write(p)
	}
	// The full length is reported written. Returning less makes `exec` treat it
	// as a short write and abandon the stream, which would lose the output the
	// command was still producing — and the partial output is what a timed-out
	// command is judged on.
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
