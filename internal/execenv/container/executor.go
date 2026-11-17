package container

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// Executor is the container backend's answer to `tools.CommandExecutor`: one
// step's commands, in this loop's container, under the environment somebody
// already resolved.
//
// It decides nothing about the command — the policy verdict decided, the
// resolver chose the environment, and this type's job is to hand the prepared
// `docker exec` to `tools.RunPrepared` so the capture, the streaming redaction,
// the caps, the timeout and the process-group kill are the SAME code a host
// command goes through (EV13). A second capture path here would be a second
// redaction point, and EV8's whole argument is that there is exactly one.
type Executor struct {
	P    *Provisioner
	Loop LoopRef
	Env  execenv.BoundEnv
	// Child is the resolved environment: names for the record, values for the
	// engine client's process environment. Never argv.
	Child   execenv.ChildEnv
	WorkDir string
	Red     *execenv.Redactor

	// provision is the row the first command of the step ensured. It is
	// remembered so the mount check and every later command in the step compare
	// against the SAME row, not against whatever is live now.
	provision store.Provision
	ensured   bool
}

var _ tools.CommandExecutor = (*Executor)(nil)

// Run ensures the provision, checks the mount, prepares the exec and hands it to
// the one capture path.
func (e *Executor) Run(ctx context.Context, req tools.ExecRequest, verdict policy.AllowToken) (tools.RunResult, error) {
	if req.Dir == "" {
		return tools.RunResult{}, errors.New("container: a command needs a working directory before a container can be chosen")
	}
	args, err := argsFromChild(e.Child)
	if err != nil {
		return tools.RunResult{}, err
	}

	if !e.ensured {
		prov, refusal := e.P.Ensure(ctx, e.Loop, e.Env, req.Dir, args)
		if refusal != nil {
			return tools.RunResult{}, errors.New("container: " + refusal.Sentence())
		}
		e.provision, e.ensured = prov, true
	}

	// The mount rule, in the executor rather than the driver: a loop whose root
	// changed mid-flight gets a FRESH provision, because running its command in
	// the container mounted on the old directory would run it against files this
	// environment was never bound to (EV9's identity rule, enforced at the mount).
	if e.provision.ComponentRoot != "" && req.Dir != e.provision.ComponentRoot {
		return tools.RunResult{}, fmt.Errorf("container: the command's working directory %q is not the mounted root %q of provision %s — a loop whose root moved gets a new provision, not the old container (EV9)",
			req.Dir, e.provision.ComponentRoot, e.provision.ID)
	}

	cmd, err := e.P.Runtime.Exec(ctx, ExecSpec{
		Handle: e.provision.Handle, Command: req.Command,
		WorkDir: ContainerWorkDir, Dir: req.Dir, User: e.P.User, Env: args,
	})
	if err != nil {
		if ref, ok := err.(*execenv.Refusal); ok {
			return tools.RunResult{}, errors.New("container: " + ref.Sentence())
		}
		return tools.RunResult{}, err
	}
	return tools.RunPrepared(ctx, cmd, tools.PreparedOptions{
		Command:   req.Command,
		Timeout:   e.timeout(req.Timeout),
		StdoutCap: req.StdoutCap,
		StderrCap: req.StderrCap,
	}, verdict, e.Red)
}

// timeout applies spec.wallSeconds as a CEILING on the tool's own timeout: the
// tool may ask for less, never more than the environment's cap.
func (e *Executor) timeout(want time.Duration) time.Duration {
	w := e.Env.Spec.Limits.WallSeconds
	if w == nil || *w <= 0 {
		return want
	}
	cap := time.Duration(*w) * time.Second
	if want <= 0 || want > cap {
		return cap
	}
	return want
}

// Names is the value-free view, from the SAME resolution that fed the container
// — so what a person reviews with `environment.effective` is the set that ran.
func (e *Executor) Names() []execenv.Var { return e.Child.Names() }

func (e *Executor) Redact(text string) string {
	if e.Red == nil {
		return text
	}
	return e.Red.RedactString(text)
}

func (e *Executor) Redactions() []execenv.NameCount {
	if e.Red == nil {
		return nil
	}
	return e.Red.Redactions()
}

// Provision is the row this step is running in, for the record and the tests.
func (e *Executor) Provision() store.Provision { return e.provision }

// argsFromChild turns the resolved environment into the driver's Arg list: the
// values are read here, once, and go to the engine client's process environment.
// argv gets names only — that split is EV11, and `TestNoSecretInAnyArgv` walks
// the driver's recorded command log to prove no value crosses it.
func argsFromChild(child execenv.ChildEnv) ([]Arg, error) {
	env, refusal := child.TryEnv()
	if refusal != nil {
		return nil, errors.New("container: " + refusal.Sentence())
	}
	out := make([]Arg, 0, len(env))
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			// `ChildEnv.Env()` always emits NAME=value; a line without the sign
			// is a corruption, and guessing which half it is would be a guess
			// about a secret.
			return nil, fmt.Errorf("container: the resolved environment carried a malformed entry (%d bytes, name withheld)", len(kv))
		}
		out = append(out, Arg{Name: name, Value: value})
	}
	return out, nil
}

// Backend is the half the daemon's step-execution resolver registers under the
// `container` name: given the bound environment and the resolved child, it
// produces the per-step executor. It satisfies `daemon.BackendExecutor` by
// shape; the assertion lives in the daemon, where that interface is, so this
// package never imports the daemon.
type Backend struct {
	P *Provisioner
}

func (b Backend) Executor(ctx context.Context, env execenv.BoundEnv, child execenv.ChildEnv, ident execenv.ProvisionIdent) (tools.CommandExecutor, *execenv.Refusal) {
	if env.Backend != execenv.BackendContainer {
		return nil, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"the container backend was asked to run an environment bound to "+env.Backend,
			"each backend executes its own bound environments; running another's would run a command under "+
				"caps nobody validated for it (EV13)",
			"register the driver for that backend")
	}
	if b.P == nil || b.P.Runtime == nil {
		return nil, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"the container backend has no engine wired",
			"a container that cannot be created means the command does not run — there is no host fallback (EV13)",
			"wire the runtime at daemon start (S59 T9)")
	}
	// F-1 (S59 T12's walk): this line used to omit Loop, so every container-bound
	// step the daemon ran died at the ledger's identity check. The identity is
	// the daemon's to hand over, and the seam now carries it.
	return &Executor{P: b.P, Env: env, Child: child,
		Loop: LoopRef{ComponentRoot: ident.ComponentRoot, LoopSlug: ident.LoopSlug, Owner: ident.Owner}}, nil
}

// Teardown is the optional capability T6's `environment.teardown` door asks for:
// a driver that can also DELETE what it created. Without it the door refuses out
// loud rather than marking a row `removed` around a running container.
func (b Backend) Teardown(ctx context.Context, prov store.Provision) error {
	if b.P == nil || b.P.Runtime == nil {
		return errors.New("container: no engine is wired, so nothing can be removed and the row must stay visible")
	}
	if _, refusal := b.P.Teardown(ctx, prov.ID); refusal != nil {
		return errors.New(refusal.Sentence())
	}
	return nil
}
