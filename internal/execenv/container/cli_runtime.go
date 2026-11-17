package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
)

// The engines this driver speaks. Both take the same `run`/`exec`/`rm`
// vocabulary; the field on the provision row records which one actually ran the
// loop, because "container" is not the fact a person needs — "podman did" is.
const (
	BinDocker = "docker"
	BinPodman = "podman"
)

// defaultPIDsLimit is the fork-bomb bound. It is a floor the driver always
// applies (0 is never "no pids-limit"), unlike the memory and CPU axes, where
// absent means "the environment declared no cap" and `execenv.Validate` has
// already accepted that for this backend.
const defaultPIDsLimit = 256

// ContainerWorkDir is the only path inside the container a command works in.
const ContainerWorkDir = "/workspace"

// CLIRuntime is the docker-compatible driver: it shells out to `docker` or
// `podman`. Every argv it builds comes from a pure function below, so the whole
// hardening surface is testable without an engine — and the fake's recorded
// command log, not a real container, is what the tests read.
type CLIRuntime struct {
	// Bin is the engine binary. Empty means discover, at first use.
	Bin string

	// Call is the process seam. Production runs `cmd`; the tests stand a fake
	// in to answer `inspect`/`images -q` without an engine. Command BUILDERS
	// are not behind the seam — they are pure, and the argv they return is the
	// thing under test.
	Call func(ctx context.Context, cmd *exec.Cmd) ([]byte, error)
}

// Discover returns the engine binary to use: `prefer` if it is on PATH, else
// docker, else podman. The chosen name is recorded in the provision row by the
// caller, so a later read says which engine laid the container down.
func Discover(prefer string) (string, error) {
	if prefer != "" {
		if _, err := exec.LookPath(prefer); err == nil {
			return prefer, nil
		}
		return "", fmt.Errorf("container: the runtime %q named for this daemon is not on PATH", prefer)
	}
	for _, b := range []string{BinDocker, BinPodman} {
		if _, err := exec.LookPath(b); err == nil {
			return b, nil
		}
	}
	return "", errors.New("container: no docker-compatible engine (docker, podman) is on PATH — install one, " +
		"or bind this loop to a host backend; a command is never run on the host because a container is unavailable")
}

func (r *CLIRuntime) bin() (string, error) {
	if r.Bin != "" {
		return r.Bin, nil
	}
	b, err := Discover("")
	if err != nil {
		return "", err
	}
	r.Bin = b
	return b, nil
}

// callCmd is the seam for a command the caller built, because the caller owns
// its environment: `call` can only supply the two-name default. Both go through
// the same replaceable `Call`, so a test sees every argv the engine would see.
func (r *CLIRuntime) callCmd(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if r.Call == nil {
		r.Call = func(ctx context.Context, cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
	}
	return r.Call(ctx, cmd)
}

func (r *CLIRuntime) call(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.Call == nil {
		r.Call = func(ctx context.Context, cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = cliProcessEnv(nil)
	// W2-3 (S59 T12's container re-walk): the engine says WHY on stderr, and the
	// gone-detection sentences read stderr. Dropping it turned "network … not
	// found" into a bare `exit status 1` that no gone-sentence can match, so a
	// vanished engine resource kept its ledger row live forever — and a live
	// failed row blocks every later step on that loop. The replaceable Call
	// stays the seam; the buffer is attached before it runs.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := r.Call(ctx, cmd)
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("container: %s failed: %w · %s", name, err, msg)
		}
		return nil, fmt.Errorf("container: %s failed: %w", name, err)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

// --- the argv builders (pure, and the reason the guards are testable) ------------

// runArgv assembles `run -d …`. Every hardening flag is written here and
// nowhere else; there is deliberately NO parameter that could carry
// `--privileged`, a docker-socket mount or host networking, because a flag a
// caller can set is a flag somebody eventually sets. `--network host` cannot be
// produced from a CreateSpec at all.
func runArgv(bin string, s CreateSpec) ([]string, []Arg, *execenv.Refusal) {
	if s.Image == "" {
		return nil, nil, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the container image is empty", "a container backend runs an image, and no image was named",
			"set spec.image.ref on the environment (aeon env update … --image REF)")
	}
	if s.User == "" {
		return nil, nil, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the container user is empty", "a container that runs as root can rewrite its own bind mount as root, "+
				"which makes the mount a host write wearing a costume",
			"run the daemon with a container user configured, or bind the loop to a host backend")
	}
	if ref := ValidateMounts(s.Mounts); ref != nil {
		return nil, nil, ref
	}
	if s.ProvisionID == "" {
		return nil, nil, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the provision id is empty", "the container is named after its ledger row, and an unnamed container "+
				"is a container reconcile cannot attribute", "ensure the provision row before creating the container")
	}

	argv := []string{bin, "run", "-d",
		"--name", NamePrefix + sanitize(s.LoopSlug) + "-" + s.ProvisionID,
		"--label", LabelManaged + "=1",
		"--label", LabelLoop + "=" + sanitize(s.LoopSlug),
		"--user", s.User,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", strconv.Itoa(orDefault(s.PIDsLimit, defaultPIDsLimit)),
	}
	if s.MemoryMiB > 0 {
		argv = append(argv, "--memory", fmt.Sprintf("%dm", s.MemoryMiB))
	}
	if s.CPUs > 0 {
		argv = append(argv, "--cpus", strconv.FormatFloat(s.CPUs, 'f', -1, 64))
	}
	switch s.NetworkMode {
	case "none":
		argv = append(argv, "--network", "none")
	case "":
		// The engine's default bridge. `unrestricted` maps here, and it is the
		// honest word for it: `execenv` calls the mode unrestricted because that
		// is what it is, and this is where the claim is kept honest.
	default:
		argv = append(argv, "--network", s.NetworkMode)
	}
	for _, m := range s.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		argv = append(argv, "-v", m.Host+":"+m.Container+":"+mode)
	}
	if s.WorkDir != "" {
		argv = append(argv, "-w", s.WorkDir)
	}
	// Names in argv; the values travel in THIS process's environment (see
	// cliProcessEnv), which is the difference between a secret in `ps` and one
	// that stays where the person put it (EV7, EV11).
	for _, a := range s.Env {
		argv = append(argv, "--env", a.Name)
	}
	// The stay-alive is the DRIVER's command, not the image's. Without
	// `--entrypoint` the arguments below are handed to whatever the image
	// declares, and an image with an init (`/init`, an entrypoint script) exits
	// immediately — measured on a linuxserver image, where `… sleep infinity`
	// produced a container that was "not running" before the first exec. The
	// loop's container must run what the daemon says it runs; `sh` is the same
	// requirement `execArgv` already makes.
	argv = append(argv, "--entrypoint", "/bin/sh", s.Image, "-c", "sleep infinity")
	return argv, s.Env, nil
}

// execArgv assembles `exec …` for one command inside the container. The same
// rule: `--env NAME` only, values in the process environment.
func execArgv(bin string, s ExecSpec) []string {
	argv := []string{bin, "exec"}
	if s.User != "" {
		argv = append(argv, "--user", s.User)
	}
	if s.WorkDir != "" {
		argv = append(argv, "-w", s.WorkDir)
	}
	for _, a := range s.Env {
		argv = append(argv, "--env", a.Name)
	}
	argv = append(argv, s.Handle, "sh", "-c", s.Command)
	return argv
}

// ValidateMounts is EV3's line drawn in code: exactly one mount, the working
// directory, at /workspace. It is called by `Create` itself, so a caller that
// forgets the check still cannot mount anything else.
func ValidateMounts(mounts []Mount) *execenv.Refusal {
	if len(mounts) != 1 {
		return execenv.NewRefusal(execenv.RefusalProvisionFailed,
			fmt.Sprintf("the container asked for %d mounts", len(mounts)),
			"a loop's container mounts its own working directory and nothing else — a second mount is a host "+
				"directory wearing the loop's permissions (EV3)",
			"remove the extra mount from the environment's spec; the only mount is the step's working root")
	}
	m := mounts[0]
	if m.Host == "" || m.Container != ContainerWorkDir {
		return execenv.NewRefusal(execenv.RefusalProvisionFailed,
			fmt.Sprintf("the container mount is %q at %q", m.Host, m.Container),
			"the working directory mounts at "+ContainerWorkDir+" and a mount anywhere else is a path out of the sandbox",
			"mount the step's working root at "+ContainerWorkDir)
	}
	// The refusals, in two shapes. A SOCKET, a runtime dir or a kernel filesystem
	// is refused at any depth — a docker socket in a loop's container is the
	// host's root with a label on it. A home directory is refused as a HOME, not
	// as a prefix: a component's working root lives under /home/<user>/…, so
	// refusing the prefix would refuse the only paths a real lane has (measured
	// on the real-runtime lane, where the honest component root was refused and
	// the loop could not run at all). `/` is refused outright.
	if clean := filepath.Clean(m.Host); clean == "/" || clean == m.Host+"/.." {
		return execenv.NewRefusal(execenv.RefusalProvisionFailed,
			fmt.Sprintf("the container mount host path %q is the whole filesystem", m.Host),
			"the working directory is a directory, not the host — mounting / mounts every credential the daemon "+
				"owns into the loop (EV3)",
			"point the environment's working root at the component's own directory")
	} else if strings.HasPrefix(clean, "/home/") || clean == "/home" {
		if depth := strings.Count(strings.TrimPrefix(clean, "/"), "/"); depth <= 1 {
			return execenv.NewRefusal(execenv.RefusalProvisionFailed,
				fmt.Sprintf("the container mount host path %q is somebody's home directory", m.Host),
				"a home directory holds the ssh keys, the shell history and every credential file on the "+
					"machine; a component's root is a project directory inside it, not the home itself",
				"mount the component's own directory, not /home/<user>")
		}
	}
	for _, bad := range []string{"/var/run/docker.sock", "/run/user", "/etc", "/root", "/sys", "/proc", "/dev", "/var/lib/docker"} {
		if clean2 := filepath.Clean(m.Host); clean2 == bad || strings.HasPrefix(clean2, bad+"/") {
			return execenv.NewRefusal(execenv.RefusalProvisionFailed,
				fmt.Sprintf("the container mount host path %q is not allowed", m.Host),
				"some paths are not mountable at any price — a docker socket in a loop's container is the host's "+
					"root with a label on it",
				"point the environment's working root at the component's own directory")
		}
	}
	return nil
}

// cliProcessEnv is the environment of the ENGINE CLIENT process — not the
// container's. It is two names, chosen because the CLI cannot talk to a daemon
// without them, and it is a list rather than an inheritance precisely so a
// daemon credential cannot ride along to `ps` (EV10). The declared variables go
// in as NAME=value pairs; argv carries the names only.
func cliProcessEnv(env []Arg) []string {
	out := make([]string, 0, len(env)+2)
	for _, n := range []string{"PATH", "HOME"} {
		if v := os.Getenv(n); v != "" {
			out = append(out, n+"="+v)
		}
	}
	for _, a := range env {
		if a.Name == "PATH" || a.Name == "HOME" {
			continue
		}
		out = append(out, a.Name+"="+a.Value)
	}
	return out
}

// --- Runtime ---------------------------------------------------------------------

func (r *CLIRuntime) Available(ctx context.Context) error {
	b, err := r.bin()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := r.call(ctx, b, "version"); err != nil {
		return fmt.Errorf("container: the engine %q is on PATH but its daemon is not answering: %w", b, err)
	}
	return nil
}

func (r *CLIRuntime) ImageExists(ctx context.Context, ref string) (bool, error) {
	b, err := r.bin()
	if err != nil {
		return false, err
	}
	out, err := r.call(ctx, b, "images", "-q", ref)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (r *CLIRuntime) Pull(ctx context.Context, ref string) error {
	b, err := r.bin()
	if err != nil {
		return err
	}
	_, err = r.call(ctx, b, "pull", ref)
	return err
}

func (r *CLIRuntime) Create(ctx context.Context, spec CreateSpec) (string, error) {
	b, err := r.bin()
	if err != nil {
		return "", err
	}
	if spec.ServiceName != "" {
		argv, env, ref := serviceArgv(b, ServiceArgvSpec{
			ProvisionID: spec.ProvisionID, LoopSlug: spec.LoopSlug, Name: spec.ServiceName,
			Image: spec.Image, Network: spec.NetworkMode, User: spec.User,
			PIDsLimit: spec.PIDsLimit, MemoryMiB: spec.MemoryMiB, CPUs: spec.CPUs, Env: spec.Env,
		})
		if ref != nil {
			return "", ref
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		// The service's declared values live HERE — in the engine client's own
		// process environment — while argv above carries only the names (EV11).
		cmd.Env = cliProcessEnv(env)
		// W2-3 again, on the door that skipped it: the engine says WHY on stderr,
		// and the buffer is attached before the replaceable Call seam runs so a
		// test still injects `Call` and still sees every argv. Dropping it turned
		// a refusal into a bare `exit status 125` (W2-4).
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := r.callCmd(ctx, cmd)
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return "", fmt.Errorf("container: the service container could not be created: %w · %s", err, msg)
			}
			return "", fmt.Errorf("container: the service container could not be created: %w", err)
		}
		// W2-5 · the handle a row stores must be something the engine can be
		// asked about again — the same rule W2-1 pins for containers
		// (handle_format_test.go). `run -d` prints the container id and that is
		// the only name the engine will later answer to; `spec.ProvisionID` is a
		// ledger word the engine has never heard, so the row could not name its
		// own service: `Start` failed, `WaitHealthy` could never find the
		// container, no service ever reached `ready` through this door, and a
		// rollback "succeeded" over a container still sitting `Created`.
		id := strings.TrimSpace(string(out))
		if id == "" {
			return "", fmt.Errorf("container: %s created the service %q but printed no container id; a row whose handle the engine cannot resolve is a resource nothing can remove (W2-5)", b, spec.ServiceName)
		}
		return id, nil
	}
	argv, env, ref := runArgv(b, spec)
	if ref != nil {
		return "", ref
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = cliProcessEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("container: %s run failed: %w · %s", b, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *CLIRuntime) Start(ctx context.Context, handle string) error {
	b, err := r.bin()
	if err != nil {
		return err
	}
	_, err = r.call(ctx, b, "start", handle)
	return err
}

// Exec prepares the command and returns it UNRUN. Everything after this line is
// `tools.RunPrepared`'s: the capture, the streaming redaction, the caps, the
// timeout and the process-group kill are the same code a host command goes
// through, which is the whole EV13 argument.
func (r *CLIRuntime) Exec(ctx context.Context, spec ExecSpec) (*exec.Cmd, error) {
	b, err := r.bin()
	if err != nil {
		return nil, err
	}
	if spec.Handle == "" {
		return nil, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the container handle is empty", "an exec needs the container its provision created",
			"ensure the provision before running a command")
	}
	argv := execArgv(b, spec)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = spec.Dir // the mounted root on the host; RunPrepared refuses a prepared command without one
	// Non-nil, always: a nil Env here would mean `exec.Cmd` gives the child the
	// DAEMON's environment, and tools.RunPrepared refuses that rather than
	// discovering it later.
	cmd.Env = cliProcessEnv(spec.Env)
	cmd.Stdin = nil // /dev/null, the same deliberate choice tools.Spawn makes
	return cmd, nil
}

func (r *CLIRuntime) RemoveContainer(ctx context.Context, handle string) error {
	b, err := r.bin()
	if err != nil {
		return err
	}
	_, err = r.call(ctx, b, "rm", "--force", handle)
	return err
}

func (r *CLIRuntime) List(ctx context.Context, label string) ([]Handle, error) {
	b, err := r.bin()
	if err != nil {
		return nil, err
	}
	// W2-1 REGRESSION, fixed by experiment on the engine that broke (rootless
	// docker 29.8.0). W2-1 was right that the read must answer the FULL 64-hex
	// id — Create stores what `run -d` printed, and `{{.ID}}` alone answers the
	// 12-char short id, which never matches (provision-live-exists on the loop's
	// second command). It was wrong about HOW: `docker ps` has no `.FullID`
	// field. Measured on 29.8.0, the W2-1 argv died with
	//   failed to execute template: … can't evaluate field FullID in type *formatter.ContainerContext
	// so List answered nothing at all, the daemon could not see the container it
	// had just started, and the loop bricked. `--no-trunc` is the flag the
	// engine actually supports for full ids from `ps`: measured, `ps --all
	// --no-trunc --format '{{.ID}}|…'` prints the full 64-hex and exit 0. Keep
	// BOTH — dropping `--no-trunc` returns the short-id bug W2-1 set out to fix,
	// and re-adding `.FullID` returns the engine rejection this one fixed.
	out, err := r.call(ctx, b, "ps", "--all", "--no-trunc", "--filter", "label="+label,
		"--format", "{{.ID}}|{{.Names}}|{{.State}}")
	if err != nil {
		return nil, err
	}
	var hs []Handle
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		hs = append(hs, Handle{ID: parts[0], Name: parts[1], State: parts[2],
			Labels: map[string]string{LabelManaged: "1"}})
	}
	return hs, nil
}

// networkArgv is `network create` with the labels baked in. There is no
// parameter that creates an unlabelled network, because an unlabelled network
// is one reconcile must not touch — so an unlabelled one would leak forever.
// The driver is left at its default (bridge): an explicit `--driver host` would
// put a loop's services on the host's stack (EV13).
func networkArgv(bin, name, loopSlug string) []string {
	return []string{bin, "network", "create",
		"--label", LabelManaged + "=1", "--label", LabelLoop + "=" + sanitize(loopSlug), name}
}

// networkListArgv is the read that makes a network findable after a crash.
//
// W2-1 REGRESSION, same shape as List and equally measured, not remembered:
// W2-1's `{{.FullID}}` here is also a field the engine does not have. With a
// labelled row present, rootless docker 29.8.0 answered the W2-1 argv with
//
//	template parsing error: … can't evaluate field FullID in type *network.networkContext
//
// (and silently exit 0 when the filter matched nothing, which is why a bare
// smoke of the read on an empty lane hid it). `--no-trunc` + `{{.ID}}` was
// measured to print the network's full 64-hex — byte for byte what `network
// create` prints and `NetworkCreate` returns, which is what the row stores — so
// rows and reads speak the same handle.
// Keep BOTH the flag and the `{{.ID}}` token; each half alone re-opens one of
// the two bugs (short-id mismatch, or engine refuses the template).
func networkListArgv(bin, label string) []string {
	return []string{bin, "network", "ls", "--no-trunc", "--filter", "label=" + label, "--format", "{{.ID}}|{{.Name}}"}
}

// NetworkCreate creates the loop's network and returns the ENGINE's id for it.
//
// W2-1's rule, applied to the network row (S59 T12): a row must store something
// the engine can be asked about again. `network create` prints the full 64-hex on
// stdout — measured on rootless docker 29.8.0, `9c45c297fe4a7977…af5a972c` and
// exit 0 — `network ls --no-trunc --format '{{.ID}}|{{.Name}}'` answers that same
// width (without `--no-trunc`, the 12-char short id), and `network rm <64-hex>`
// exits 0 and prints it back. So the id is the only handle all three speak, and it
// is the receipt the row must carry. The name is still what the CREATE is given —
// `networkArgv` below needs it — but the name is a label of the driver's making,
// and a row keyed on it is a row no read answers to.
//
// An empty print is refused rather than stored, the same refusal the service path
// makes for an empty `run -d` stdout: a row whose handle the engine cannot resolve
// is a resource nothing can ever remove (W2-5's house style, :360).
func (r *CLIRuntime) NetworkCreate(ctx context.Context, name, loopSlug string) (string, error) {
	b, err := r.bin()
	if err != nil {
		return "", err
	}
	argv := networkArgv(b, name, loopSlug)
	out, err := r.call(ctx, argv[0], argv[1:]...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("container: %s created the loop network %q but printed no network id; a row whose handle the engine cannot resolve is a resource nothing can remove (W2-1)", b, name)
	}
	return id, nil
}

func (r *CLIRuntime) ProbeRun(ctx context.Context, spec ProbeSpec) (*exec.Cmd, error) {
	b, err := r.bin()
	if err != nil {
		return nil, err
	}
	argv := probeArgv(b, spec)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = cliProcessEnv(nil) // the probe carries no value the daemon knows about (EV10)
	return cmd, nil
}

func (r *CLIRuntime) NetworkList(ctx context.Context, label string) ([]Handle, error) {
	b, err := r.bin()
	if err != nil {
		return nil, err
	}
	argv := networkListArgv(b, label)
	out, err := r.call(ctx, argv[0], argv[1:]...)
	if err != nil {
		return nil, err
	}
	var hs []Handle
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		id, name, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		hs = append(hs, Handle{ID: id, Name: name})
	}
	return hs, nil
}

// networkRmArgv is `network rm` for one handle. The handle is whatever the row
// stores — and since W2-1's network half (S59 T12) that is the engine's own id,
// which `network rm` accepts: measured on rootless docker 29.8.0, `docker
// network rm <64-hex>` exits 0 and prints the id back, and on a gone network it
// exits 1 with `Error response from daemon: network … not found` on stderr,
// which is the sentence W2-3 keeps and goneSentence reads. It is a builder
// rather than an inline argv for the same reason as the other builders: the
// fake records the SAME slice the engine would have been handed, so "the network
// really came down" is read off a command log and not assumed from a return.
func networkRmArgv(bin, handle string) []string {
	return []string{bin, "network", "rm", handle}
}

func (r *CLIRuntime) NetworkRemove(ctx context.Context, handle string) error {
	b, err := r.bin()
	if err != nil {
		return err
	}
	argv := networkRmArgv(b, handle)
	_, err = r.call(ctx, argv[0], argv[1:]...)
	return err
}

// --- small helpers ----------------------------------------------------------------

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// sanitize keeps a slug usable as a container name and a label value: docker's
// own character set is [a-zA-Z0-9_.-], and a slug that smuggles a space or a
// slash would otherwise be an argument injection through a name.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
