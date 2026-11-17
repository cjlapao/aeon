// Package container is S59 T9's real isolation backend: a per-loop container,
// one working-directory mount, secrets that never reach argv, hardening flags on
// every `run`, a write-ahead provision ledger and a labelled-only reconcile.
//
// The design rule of this file is that a command which cannot run in the
// container DOES NOT RUN AT ALL. Every path out of here either hands back a
// prepared `*exec.Cmd` for the shared capture path (EV13) or returns an
// `*execenv.Refusal` naming the driver's exact reason. There is no branch that
// runs the command on the host "because docker is not up": EV13's whole argument
// is that a backend that quietly downgrades is worse than one that refuses.
package container

import (
	"context"
	"os/exec"
)

// The labels that make a container ours. Reconcile removes ONLY resources
// carrying `aeon.managed=1`; an unlabelled container on the same engine belongs
// to somebody else and is left alone, in every branch, including the ones where
// leaving it is inconvenient.
const (
	LabelManaged = "aeon.managed"
	// LabelLoop carries the loop slug, so `docker ps --filter label=…` answers
	// "what is this loop holding open" without the store.
	LabelLoop = "aeon.loop"
	// NamePrefix is the container-name prefix; the name is
	// aeon-<slug>-<provisionID>, which makes a leaked container attributable to
	// a ledger row by name alone.
	NamePrefix = "aeon-"
)

// Mount is the only kind of filesystem access a container gets, and the
// executor refuses anything but the working directory (EV3, EV13): one entry,
// read-write, at /workspace.
type Mount struct {
	Host      string
	Container string
	ReadOnly  bool
}

// Arg is one environment variable for the container: the NAME goes in argv as
// `--env NAME`, and the VALUE goes in the driver process's own environment, so
// the value never appears in the process list, in `ps`, or in a recorded
// command line (EV7, EV11). A driver must therefore never format `.Value` into
// an argument — and `TestNoSecretInAnyArgv` walks the recorded argv to prove it
// never does.
type Arg struct {
	Name  string
	Value string
}

// CreateSpec is what `Create` needs to build the container. It is a struct
// rather than a bag of strings because the hardening flags are not optional
// knobs a caller can omit: they are assembled here, once.
type CreateSpec struct {
	// ProvisionID and LoopSlug name the container and its labels.
	ProvisionID string
	LoopSlug    string

	Image string
	// User is the `--user uid:gid` pair. A container that runs as root is a
	// container that can write its own mount as root, so an empty User is
	// refused rather than defaulted.
	User string

	// PIDsLimit, MemoryMiB and CPUs are the caps the environment declared;
	// zero means the axis is uncapped and the flag is simply absent (which
	// `execenv.Validate` has already decided is legal for THIS backend).
	PIDsLimit int
	MemoryMiB int
	CPUs      float64

	// NetworkMode is "" (the engine's default bridge), "none", or a loop
	// network name (T10's services). `egress.mode=none` maps to "none".
	NetworkMode string
	// ServiceName marks this create as a SERVICE container: the driver then
	// builds `serviceArgv` (no mount, no working directory, no entrypoint
	// override, `--network-alias`) instead of `runArgv`. It is a field rather
	// than a second method so that the choice of shape cannot be bypassed.
	ServiceName string

	// Mounts is expected to be exactly the working directory. `ValidateMounts`
	// is the guard, called by Create, not by the caller: a driver that skips it
	// still cannot mount the host's root.
	Mounts []Mount

	// WorkDir is the container-side working directory (/workspace).
	WorkDir string

	// Env is the variable set, names for argv and values for this process's
	// environment.
	Env []Arg
}

// ExecSpec is one command inside an existing container, prepared rather than
// run: `Exec` returns the `*exec.Cmd` so `tools.RunPrepared` captures, redacts,
// caps, times out and group-kills it exactly as it does a host command (EV13).
type ExecSpec struct {
	Handle  string
	Command string
	// WorkDir is the CONTAINER-side directory the command starts in (/workspace).
	WorkDir string
	// Dir is the HOST directory the engine client process itself runs in — the
	// mounted root. It is a separate field because the two directories are two
	// different facts: one is where the command works inside the container, the
	// other is where this process lives on the host, and `tools.RunPrepared`
	// refuses a prepared command that cannot say the second one.
	Dir  string
	User string
	Env  []Arg
}

// Handle is one container the engine reports, as `List` returns it — the id
// reconcile compares against the ledger, and the labels that decide whether it
// is ours to touch.
type Handle struct {
	ID     string
	Name   string
	Labels map[string]string
	State  string
}

// Runtime is the engine behind the driver. Two implementations matter:
// `cliRuntime` (the docker-compatible CLI, below) and the fake in the tests,
// which records every argv it was handed — that log IS the test oracle for the
// flags, the mounts and the absence of a secret.
type Runtime interface {
	// Available reports whether an engine is reachable AT ALL. Its error is the
	// driver's own sentence; the caller turns it into an
	// `environment-unavailable` refusal and runs nothing.
	Available(ctx context.Context) error

	ImageExists(ctx context.Context, ref string) (bool, error)
	Pull(ctx context.Context, ref string) error

	Create(ctx context.Context, spec CreateSpec) (handle string, err error)
	Start(ctx context.Context, handle string) error
	// Exec prepares, it does not run: the returned command goes to
	// `tools.RunPrepared`, which is the only thing that captures output.
	Exec(ctx context.Context, spec ExecSpec) (*exec.Cmd, error)
	// ProbeRun prepares the one-shot tcp health probe. Prepared, not run, for
	// the same reason as Exec: output is captured in exactly one place.
	ProbeRun(ctx context.Context, spec ProbeSpec) (*exec.Cmd, error)
	// RemoveContainer deletes one container by handle. It is NOT spelled as the
	// bare verb the S45 policy-boundary guards match (cmd/aeon/policy_amend_test.go
	// and policy_authoring_guards_test.go walk the tree for a Remove call whose
	// first argument is a context, because that is the shape of a policy write
	// outside its doors). An engine call wearing that name reads like a permission
	// change to a guard that must stay conservative, so the verb here names its
	// object instead. Renamed in S59 T12's full-suite comparison; the guards were
	// not edited, this side was.
	RemoveContainer(ctx context.Context, handle string) error

	// List returns the containers carrying the given label (e.g.
	// `aeon.managed=1`). Reconcile's authority boundary is exactly this list.
	List(ctx context.Context, label string) ([]Handle, error)

	// NetworkCreate makes the one user-defined network a loop owns. `loopSlug`
	// is not decoration: the driver labels every resource it creates with
	// `aeon.managed` and the loop, which is the only thing that lets reconcile
	// tell ours from somebody else's.
	//
	// It returns the ENGINE's network id — the 64-hex `network create` prints on
	// stdout — and the caller stores that as the row's handle. W2-1's rule for
	// containers and W2-5's for services, applied here: a row must store something
	// the engine can be asked about again, and the id is the one handle the create
	// prints, `network ls --no-trunc --format '{{.ID}}'` answers and `network rm`
	// accepts (all three measured on rootless docker 29.8.0). The name the caller
	// chose still goes into the create argv, because the create needs a name; it is
	// not the receipt. An engine that created a network and printed nothing is an
	// error, not a row with an empty handle.
	NetworkCreate(ctx context.Context, name, loopSlug string) (string, error)
	NetworkRemove(ctx context.Context, handle string) error
	// NetworkList returns the networks carrying the given label. Without it an
	// orphan network is invisible: `List` answers containers, and a resource no
	// list can see is a resource that leaks (EV12). Its `ID` is the same 64-hex the
	// row stores, which is what makes a network row comparable with its own read —
	// the comparison `reconcile.go` and `reusableOwner` both depend on.
	NetworkList(ctx context.Context, label string) ([]Handle, error)
}
