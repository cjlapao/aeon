package tools

// prepared.go — S59 T9 (EV13): the ONE capture path, opened to a command that
// was built somewhere else.
//
// `Spawn` is the door for a command this package builds: it checks the token,
// arbitrates the environment, builds `sh -c …` and hands the result to
// `runProcess`, which captures, redacts, caps, times out and group-kills. The
// container backend (S59 T9) needs the same life cycle for a command it built
// itself — `docker exec …` — and the ONLY honest way to give it one is to route
// that prepared command through the same `runProcess`. A second capture path is
// a second set of bounds, a second redaction point and a second group-kill, and
// the slice's whole EV8 argument (T4's streaming filter at the one capture
// point) would be a memory exercise instead of a fact.
//
// So this file exports an ENTRY, not a copy: `RunPrepared` applies the same
// guards `Spawn` applies before it builds anything — in the same order, with the
// same sentences — and then calls `runProcess`. The guards are not a formality
// for a caller that "means well": a prepared command with a nil `Env` would
// inherit the daemon's own environment, which is the exact leak S59 exists to
// close, and a prepared command with a token minted for a different string is
// the exact forgery S11's order exists to refuse.

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
)

// PreparedOptions are the bounds `SpawnOptions` carries, minus the environment
// (which lives on the prepared command, where the driver put it) and minus the
// directory (which lives on the command too, because the driver mounted it).
// They are kept as explicit fields so the two doors cannot drift apart in
// silence: every zero value means "the default", the same claim it makes in
// `SpawnOptions`.
type PreparedOptions struct {
	// Command is the exact string the policy token was minted for. The
	// prepared `*exec.Cmd` does not carry it — the driver may have wrapped the
	// command in `sh -c` inside the container — so the caller names it, and the
	// token is checked against THIS string, not against whatever argv the
	// driver assembled.
	Command string

	// Timeout, StdoutCap and StderrCap are the tool's bounds. Zero means "the
	// default", exactly as in SpawnOptions.
	Timeout   time.Duration
	StdoutCap int
	StderrCap int
}

// RunPrepared runs a command the caller built, through the same capture,
// redaction, cap, timeout and process-group-killing path every Aeon command
// goes through.
//
// The command must carry its own `Env` (never nil — nil means "inherit the
// daemon's environment", the leak), its own `Dir` (the mounted working
// directory), and its own process-group behaviour is `runProcess`'s business,
// not the caller's. The verdict is checked against `opts.Command` before
// anything is read off the command.
func RunPrepared(ctx context.Context, cmd *exec.Cmd, opts PreparedOptions,
	verdict policy.AllowToken, red *execenv.Redactor) (RunResult, error) {
	// The same three guards, in the same order, as `spawnWith`. They are
	// spelled out here rather than factored into a helper because their ORDER
	// is the rule and a reader of `Spawn` must still see them there.
	if !verdict.Allows(opts.Command) {
		return RunResult{}, errors.New("tools: refusing to run a prepared command: the command does not carry a " +
			"policy-issued allow token for this exact command")
	}
	if cmd == nil {
		return RunResult{}, errors.New("tools: a prepared command was never built — there is nothing to run, " +
			"and a nil command that reaches here would report an empty result rather than a refusal")
	}
	if cmd.Dir == "" {
		return RunResult{}, errors.New("tools: a prepared command needs a working directory; it is the mounted " +
			"root, and running in the daemon's directory would be running in the wrong project")
	}
	if cmd.Env == nil {
		// The EV10 hole, closed at the new door. `exec.Cmd` reads a nil `Env`
		// as "give the child my own environment", which for a container driver
		// would put every daemon credential within reach of a command that was
		// supposed to get an allowlist. A driver that has no environment to
		// give must pass an empty slice — a claim, not an accident.
		return RunResult{}, errors.New("tools: refusing to run a prepared command with a nil environment: it would " +
			"inherit the daemon's own, which is the leak S59 exists to close (EV10) — pass the resolved slice, or an empty one")
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
	// The group kill is `runProcess`'s job, and it is only possible when the
	// child leads its own process group. A driver COULD set this; a driver that
	// forgot would lose the kill silently — a `docker exec` client that cannot be
	// killed is a hung step with a live container behind it — so the path that
	// performs the kill also sets the precondition. `spawn.go` does the same for
	// the command `Spawn` builds; this is the prepared half of that fact.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	return runProcess(ctx, cmd, runOpts{timeout: timeout, stdoutCap: stdoutCap, stderrCap: stderrCap}, red)
}
