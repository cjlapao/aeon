package tools

// The command-execution seam (S59, EV9).
//
// # Why an interface exists at all, when Spawn was reachable
//
// Until now `bash` called `Spawn` directly, which meant the tool decided both
// WHAT runs (it already does — that is the policy verdict) and WITH WHAT
// (which was "whatever the daemon has", because nothing was asked). The second
// half is a decision with a record behind it: an environment bound to a
// component, secret references resolved per step, a backend that may be a
// container rather than a shell. `bash` cannot make any of those calls, and
// must not be able to — so it asks for an executor and gets one that has
// already decided, or gets a refusal that says why nobody decided.
//
// The seam is a FUNCTION, not the executor itself, for the same reason
// `CommandVerdict` is one: an executor is a thing with a lifetime (a resolved
// environment, a redactor accumulating counts for ONE step), and a tool that
// held one across steps would hold one step's secrets into another's output.
// Asking per call makes "which step is this running for?" a question the
// resolver answers every time.
//
// # nil is inert, and says so
//
// `ToolContext.Exec` nil means this toolset was built with no execution seam,
// and `bash` refuses — the same shape as `CommandVerdict` nil, and for the same
// reason: a tool that cannot ask whether it may run, or with what, spawns
// nothing. A storeless daemon therefore has an inert bash, which is the
// correct answer and not a regression.

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
)

// ExecRequest is one command a step asked to run: the command, and the bounds
// the CALLER of the tool asked for. It carries no environment and no secrets —
// an executor that accepted either would put the decision back where the tool
// is, which is what this file exists to take out of the tool.
type ExecRequest struct {
	// Command is the exact string the policy token was minted for. The
	// executor does not re-quote it; `Spawn` hands it to the shell.
	Command string

	// Dir is the working directory: the STEP's root, which is the component
	// root the environment was bound to — the same root `execenv.Resolve` read
	// the binding from, so a command cannot run in a project whose environment
	// it did not get (EV9, I5). `Spawn` requires it and refuses an empty one.
	Dir string

	// Timeout, StdoutCap and StderrCap are the tool's bounds. Zero means "the
	// default", exactly as in SpawnOptions; an executor that read its own
	// defaults here would be a second place they live.
	Timeout   time.Duration
	StdoutCap int
	StderrCap int
}

// CommandExecutor runs one step's commands, with an environment somebody
// already chose.
//
// Implementations are per-step. The two the daemon uses are HostExecutor
// (host-scrubbed, host-inherit) and the container backend's executor (T9); a
// test uses a fake to assert what a step WOULD have run without running it.
type CommandExecutor interface {
	// Run executes one command under the verdict that authorized it. The token
	// is not optional and is not stored: an executor that remembered a token
	// would own a permission nobody issued.
	Run(ctx context.Context, req ExecRequest, verdict policy.AllowToken) (RunResult, error)

	// Names is the value-free view of the environment this executor gives a
	// child — names and where each came from, never a value. It is what the
	// step record and `environment.effective` print, so the thing a person
	// reviews is the thing that ran.
	Names() []execenv.Var

	// Redact filters a string through the SAME filter the command's output went
	// through. It exists so there is one boundary: the command echo, a log line
	// about the command, and the captured output all pass through one
	// redactor, rather than each sink deciding whether to bother.
	Redact(text string) string

	// Redactions is the per-name tally this executor has produced, for the
	// step's record. Names and counts only — a count that disclosed a value
	// would be a leak in the audit of the leak detector.
	Redactions() []execenv.NameCount
}

// HostExecutor runs commands on this host, in a resolved environment, with the
// secrets redacted at the one capture point.
//
// It is two fields and no state beyond them: the environment (built, or the
// named legacy inherit) and the redactor (nil when the environment injected no
// secrets, which is most environments). It holds no store, no loop and no
// context, so it cannot drift from the step it was resolved for — it simply
// cannot answer that question, which is the point.
type HostExecutor struct {
	// Env is the resolved child environment. An inert value is not refused
	// here: `Spawn` refuses it, at the one place a process is built, so a
	// zero-value HostExecutor fails the same way a caller that skipped
	// resolution does.
	Env execenv.ChildEnv

	// Redactor is the step's filter, or nil for an environment with no secrets.
	Redactor *execenv.Redactor
}

var _ CommandExecutor = HostExecutor{}

// Run hands the command to Spawn with this executor's environment and filter.
// Every bound is passed through, unchanged: this method decides nothing about
// the command, which is what makes it safe to call from a tool.
func (h HostExecutor) Run(ctx context.Context, req ExecRequest, verdict policy.AllowToken) (RunResult, error) {
	// The redactor travels with the environment it was built from: the child's
	// bytes are filtered at the one capture point, so no sink downstream of
	// here — transcript, event, log line — has to remember to filter anything.
	return spawnWith(ctx, req.Command, SpawnOptions{
		Dir:       req.Dir,
		Timeout:   req.Timeout,
		StdoutCap: req.StdoutCap,
		StderrCap: req.StderrCap,
	}, verdict, h.Env, h.Redactor)
}

// Names reports the value-free view; for the legacy backend it is the one
// `*`/`legacy` row, which is the honest answer rather than a guess at what the
// daemon happens to have in its environment.
func (h HostExecutor) Names() []execenv.Var { return h.Env.Names() }

// Redact passes text through the step's filter. A nil redactor passes through —
// an environment with no secrets has nothing to filter, and "no filter" and
// "a filter that matched nothing" would otherwise be indistinguishable here
// while being very different facts on the record.
func (h HostExecutor) Redact(text string) string {
	if h.Redactor == nil {
		return text
	}
	return h.Redactor.RedactString(text)
}

// Redactions reports the tally, empty for a step with no secrets.
func (h HostExecutor) Redactions() []execenv.NameCount {
	if h.Redactor == nil {
		return nil
	}
	return h.Redactor.Redactions()
}
