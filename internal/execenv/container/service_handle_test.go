package container

// W2-5 + W2-4 (S59 T12's service re-walk) — **a row must store something the
// engine can be asked about again.**
//
// The container half of this bug is already pinned by handle_format_test.go
// (W2-1): the inventory reads and the stored handle have to speak the same id or
// nothing ever matches. The service half was pinned nowhere, and it was the worse
// of the two, because the row does not even store an id — `Create`'s service path
// answered `spec.ProvisionID`, a string the engine has never heard. Measured
// against a real daemon: `WaitHealthy` could not name the container it was meant
// to prove, so **no service ever reached `ready` through this door**, and a
// rollback "succeeded" while the engine still held `6cfd6b502a37 … Created`.
//
// These tests drive the REAL driver — `CLIRuntime` with its replaceable `Call`
// seam stood in for the `docker` process — because the defect lives between the
// argv, the engine's stdout and the row. A fake that implements `Runtime` itself
// cannot see that gap, which is precisely how this package's suite stayed green.
// The id under assertion is minted INSIDE the engine from the name it was given:
// no test supplies it, so a row that echoes its own provision id cannot pass by
// agreeing with the test.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// engine is the stand-in for the `docker` process: it holds what it created,
// keyed by the name the driver supplied, prints the id `run -d` prints, and
// refuses — in the engine's own words, on stderr — anything it cannot resolve.
type engine struct {
	log    *log
	holds  map[string]string // container name -> the id the engine printed for it
	nets   map[string]string // network name -> the 64-hex id the engine printed for it
	refuse string            // the stderr sentence for the next service create
	noID   bool              // print nothing, as an engine that created nothing prints
}

func newEngine(l *log) *engine {
	return &engine{log: l, holds: map[string]string{}, nets: map[string]string{}}
}

// idOf is the engine's own report: 64-hex, derived from the name it was handed,
// never from anything the test knew in advance.
func (e *engine) idOf(name string) string {
	sum := sha256.Sum256([]byte("aeon-engine-" + name))
	return hex.EncodeToString(sum[:])
}

// netIDOf is the same for a network: `docker network create` prints the full
// 64-hex on stdout (measured on rootless docker 29.8.0), and that print — not the
// name the caller chose — is the only thing `network ls` and `network rm` agree
// on afterwards. The old fake answered `0f2ac`, a five-character nothing, and
// keyed its inventory by the caller's own name, so a row storing the name agreed
// with it and the W2-1 mismatch survived the whole package green.
func (e *engine) netIDOf(name string) string {
	sum := sha256.Sum256([]byte("aeon-engine-network-" + name))
	return hex.EncodeToString(sum[:])
}

// holdsNet says whether the engine still holds a network under the handle it was
// given — the id it printed or the name it was given, exactly as `network rm`
// resolves either.
func (e *engine) holdsNet(h string) (string, bool) {
	if h == "" {
		return "", false
	}
	for name, id := range e.nets {
		if name == h || id == h {
			return name, true
		}
	}
	return "", false
}

func (e *engine) holdsHandle(h string) bool {
	if h == "" {
		return false
	}
	for name, id := range e.holds {
		if name == h || id == h {
			return true
		}
	}
	return false
}

// fail is how an engine refuses: the sentence goes to stderr, the process exits
// 125. The driver's job (W2-3, and W2-4 for services) is to keep both.
func (e *engine) fail(cmd *exec.Cmd, sentence string) error {
	if cmd.Stderr != nil {
		_, _ = io.WriteString(cmd.Stderr, sentence+"\n")
	}
	return errors.New("exit status 125")
}

func (e *engine) seam(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
	joined := strings.Join(cmd.Args, " ")
	switch {
	case strings.HasPrefix(joined, "docker network create"):
		name := cmd.Args[len(cmd.Args)-1]
		e.log.add(Op{Actor: "runtime", What: "network-create", Argv: cmd.Args, Ref: name})
		if _, dup := e.nets[name]; dup {
			return nil, e.fail(cmd, "Error response from daemon: network with name "+name+" already exists")
		}
		// `network create` prints the FULL 64-hex on stdout (measured, docker
		// 29.8.0). That print is the engine's report of what it made, and the only
		// thing `network ls` and `network rm` agree on later.
		id := e.netIDOf(name)
		e.nets[name] = id
		return []byte(id + "\n"), nil
	case strings.HasPrefix(joined, "docker network ls"):
		e.log.add(Op{Actor: "runtime", What: "network-list", Argv: cmd.Args})
		// The engine's own two-width answer: `--no-trunc` gives the 64-hex the row
		// must store, without it the same template answers the 12-char short id
		// (measured). A driver that drops the flag therefore stops matching, which
		// is the W2-1 trap with teeth rather than a comment.
		full := false
		for _, a := range cmd.Args {
			if a == "--no-trunc" {
				full = true
			}
		}
		var lines []string
		for _, name := range sortedKeys(e.nets) {
			id := e.nets[name]
			if !full {
				id = id[:12]
			}
			lines = append(lines, id+"|"+name)
		}
		return []byte(strings.Join(lines, "\n")), nil
	case strings.HasPrefix(joined, "docker network rm"):
		h := cmd.Args[len(cmd.Args)-1]
		e.log.add(Op{Actor: "runtime", What: "network-rm", Argv: cmd.Args, Ref: h})
		name, ok := e.holdsNet(h)
		if !ok {
			// The engine's words, on stderr, with the exit code — the sentence
			// W2-3 keeps and goneSentence reads.
			return nil, e.fail(cmd, "Error response from daemon: network "+h+" not found")
		}
		id := e.nets[name]
		delete(e.nets, name)
		return []byte(id + "\n"), nil
	case strings.HasPrefix(joined, "docker run -d"):
		name := flagValue(cmd.Args, "--name")
		what := "create"
		if flagValue(cmd.Args, "--network-alias") != "" {
			what = "create-service"
		}
		e.log.add(Op{Actor: "runtime", What: what, Argv: cmd.Args, Env: cmd.Env, Ref: name})
		if e.refuse != "" {
			return nil, e.fail(cmd, e.refuse)
		}
		id := e.idOf(name)
		e.holds[name] = id
		if e.noID {
			return []byte("\n"), nil
		}
		return []byte(id + "\n"), nil
	case strings.HasPrefix(joined, "docker start"):
		h := cmd.Args[len(cmd.Args)-1]
		e.log.add(Op{Actor: "runtime", What: "start", Argv: cmd.Args, Ref: h})
		if !e.holdsHandle(h) {
			return nil, e.fail(cmd, "Error: No such container: "+orText(h, ""))
		}
		return nil, nil
	case strings.HasPrefix(joined, "docker rm --force"):
		h := cmd.Args[len(cmd.Args)-1]
		e.log.add(Op{Actor: "runtime", What: "remove", Argv: cmd.Args, Ref: h})
		if !e.holdsHandle(h) {
			return nil, e.fail(cmd, "Error: No such container: "+orText(h, ""))
		}
		for name, id := range e.holds {
			if name == h || id == h {
				delete(e.holds, name)
			}
		}
		return nil, nil
	}
	return nil, e.fail(cmd, "the engine fake was handed a command it does not answer: "+joined)
}

// flagValue reads one flag's value out of the argv the engine would have seen.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// sortedKeys gives the scripted engine a stable listing order, so a read the
// test walks does not depend on map iteration.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// execHandle is the container an `exec` names: the argument before the shell.
func execHandle(args []string) string {
	for i, a := range args {
		if a == "sh" && i > 0 {
			return args[i-1]
		}
	}
	return ""
}

// serviceEngineHarness wires the door the daemon walks — Provisioner over the
// real `CLIRuntime`, engine process replaced — for one loop whose environment
// declares a single `db` service. `probeRefuses` is the unhealthy world.
func serviceEngineHarness(t *testing.T, health *execenv.Health, probeRefuses bool) (string, *Provisioner, *fakeLedger, *engine, execenv.BoundEnv) {
	t.Helper()
	dir := rootDir(t)
	lg := newFakeLedger()
	eng := newEngine(lg.log)
	p := &Provisioner{Ledger: lg, Runtime: &CLIRuntime{Bin: "docker", Call: eng.seam}, User: "1000:1000"}
	p.ServiceArgs = func(execenv.BoundEnv, execenv.Service) ([]Arg, *execenv.Refusal) {
		return []Arg{{Name: svcSecretName, Value: svcSecretVal}}, nil
	}
	p.Probe = func(_ context.Context, cmd *exec.Cmd, command string) (tools.RunResult, *execenv.Refusal) {
		h := execHandle(cmd.Args)
		lg.log.add(Op{Actor: "runtime", What: "exec", Argv: cmd.Args, Ref: h})
		// The answer is the engine's, not the test's: a handle the engine cannot
		// resolve fails, which is the consequence W2-5 produced on the real daemon.
		if !eng.holdsHandle(h) {
			return tools.RunResult{ExitCode: 1, Stderr: "Error: No such container or service: " + orText(h, "<empty>")}, nil
		}
		if command != "pg_isready" {
			return tools.RunResult{ExitCode: 1, Stderr: "the probe ran a command the service does not answer: " + command}, nil
		}
		if probeRefuses {
			return tools.RunResult{ExitCode: 1, Stderr: "pg_isready: rejecting connections"}, nil
		}
		return tools.RunResult{ExitCode: 0}, nil
	}
	env := testEnv("none", execenv.PullNever)
	env.Spec.Services = []execenv.Service{{Name: "db", Image: svcImage, Port: 5432, Health: health}}
	return dir, p, lg, eng, env
}

func serviceLoop(dir string) LoopRef {
	return LoopRef{ComponentRoot: dir, LoopSlug: "loop-a", Owner: "step-1"}
}

func engineServiceRow(t *testing.T, lg *fakeLedger) store.Provision {
	t.Helper()
	for _, id := range lg.ids {
		if r := lg.rows[id]; r.Owner == "service:db" {
			return *r
		}
	}
	t.Fatalf("no service:db row exists at all; the row is written BEFORE the resource (EV12)")
	return store.Provision{}
}

func removeRefs(l *log) []string {
	var out []string
	for _, o := range l.all() {
		if o.Actor == "runtime" && o.What == "remove" {
			out = append(out, o.Ref)
		}
	}
	return out
}

// W2-5 · the row stores the id the engine printed, so the engine can be asked
// about the service again: `start`, the health exec and teardown all name it.
func TestServiceRowHandleIsTheIdTheEngineReported(t *testing.T) {
	dir, p, lg, eng, env := serviceEngineHarness(t, &execenv.Health{Command: "pg_isready", TimeoutSeconds: 1}, false)
	if _, _, ref := p.ensureDependencies(context.Background(), serviceLoop(dir), env, dir); ref != nil {
		t.Fatalf("the service never came up through this door: %s", ref.Sentence())
	}

	// Anti-vacuity: the create really reached the engine, with the service argv.
	runs := lg.log.argvOf("create-service")
	if len(runs) != 1 {
		t.Fatalf("the engine was handed %d service creates, want exactly 1 — a service that was never created proves nothing", len(runs))
	}
	row := engineServiceRow(t, lg)
	name := ServiceContainerName("loop-a", "db", row.ID)
	joined := strings.Join(runs[0], " ")
	for _, want := range []string{"run -d", "--name " + name, "--network " + NetworkName("loop-a"),
		"--network-alias db", "--user 1000:1000", "--env " + svcSecretName, svcImage} {
		if !strings.Contains(joined, want) {
			t.Errorf("the service create argv lost %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, svcSecretVal) {
		t.Errorf("the service's secret value reached argv (EV11):\n%s", joined)
	}

	want := eng.idOf(name) // what the engine printed for the name it was given
	if row.State != store.ProvisionStateReady {
		t.Fatalf("the service row is %s, not ready — no service has ever reached ready through this door (W2-5)", row.State)
	}
	if row.Handle == row.ID {
		t.Errorf("the row stores its own provision id %q, which the engine has never heard; nothing can be asked about the service again (W2-5)", row.ID)
	}
	if row.Handle != want {
		t.Errorf("the row stores handle %q but the engine knows that container only as %q — the handle must be what the engine reported (W2-5)", row.Handle, want)
	}

	if n := lg.log.count("runtime", "start"); n != 1 {
		t.Fatalf("the engine got %d starts for the service, want 1", n)
	}
	for _, o := range lg.log.all() {
		if o.What == "start" && o.Ref != want {
			t.Errorf("`docker start %s` named something the engine cannot resolve; it should have started %q (W2-5)", o.Ref, want)
		}
	}
	execs := lg.log.argvOf("exec")
	if len(execs) == 0 {
		t.Fatalf("the health check never reached the engine, so nothing proves the service was proven up")
	}
	if got := strings.Join(execs[0], " "); !strings.Contains(got, want+" sh -c pg_isready") {
		t.Errorf("the health exec could not name the container — WaitHealthy can never find it: %s (want the handle %q)", got, want)
	}
}

// W2-4 · a refusal arrives in the engine's words, not as a bare `exit status
// 125`; and a create that produced no container claims no removal.
func TestFailedServiceCreateCarriesTheEnginesWordsAndClaimsNoRemoval(t *testing.T) {
	dir, p, lg, eng, env := serviceEngineHarness(t, &execenv.Health{Command: "pg_isready", TimeoutSeconds: 1}, false)
	eng.refuse = "Error: No such image: " + svcImage + ": manifest not found"

	_, _, ref := p.ensureDependencies(context.Background(), serviceLoop(dir), env, dir)
	if ref == nil {
		t.Fatalf("a service create the engine refused reported success")
	}
	if n := lg.log.count("runtime", "create-service"); n != 1 {
		t.Fatalf("the engine was handed %d service creates; the refusal must come from a call that ran", n)
	}
	if s := ref.Sentence(); !strings.Contains(s, "No such image") {
		t.Errorf("the engine's refusal arrived as %q — the service path drops stderr and leaves the bare exit code where the engine's own words belong (W2-4)", s)
	}
	if n := lg.log.count("runtime", "remove"); n != 0 {
		t.Errorf("%d container removals on a create that produced no container; a handle the engine cannot resolve must not be reported as removed", n)
	}
	if len(eng.holds) != 0 {
		t.Errorf("the engine holds %v after a create it refused; nothing should have been claimed", eng.holds)
	}
	if row := engineServiceRow(t, lg); row.State != store.ProvisionStateFailed {
		t.Errorf("the service row is %s after the engine refused; a row must say what is not running", row.State)
	}
}

// W2-5 · the debris half: an unhealthy service is taken back down by the name
// the engine holds it under, or the rollback is a report of success over a
// container nobody can name again.
func TestUnhealthyServiceRollbackRemovesWhatTheEngineHolds(t *testing.T) {
	dir, p, lg, eng, env := serviceEngineHarness(t, &execenv.Health{Command: "pg_isready", TimeoutSeconds: 1}, true)

	ref := func() *execenv.Refusal {
		_, _, r := p.ensureDependencies(context.Background(), serviceLoop(dir), env, dir)
		return r
	}()
	if ref == nil || ref.Kind != execenv.RefusalServiceUnhealthy {
		t.Fatalf("the probe said no and the answer was %v; the step must not start against an unhealthy service (EV14)", ref)
	}
	row := engineServiceRow(t, lg)
	want := eng.idOf(ServiceContainerName("loop-a", "db", row.ID))

	refs := removeRefs(lg.log)
	if len(refs) != 1 {
		t.Fatalf("the rollback recorded %d container removals, want 1 — it reported a take-down without naming the container it took down (W2-5)", len(refs))
	}
	if refs[0] != want {
		t.Errorf("teardown asked the engine to remove %q, which it has never heard; the container the engine created is still held as %q (W2-5)", refs[0], want)
	}
	if len(eng.holds) != 0 {
		t.Errorf("the rollback reported success while the engine still holds %v — debris no row can name (W2-5, EV12)", eng.holds)
	}
	// The same claim for the network the rollback also made: it came down by the
	// id the engine minted for it, and the engine holds nothing of this run. A
	// network left behind after a reported success is debris no later sweep can
	// attribute to a loop (W2-1, EV12).
	wantNet := eng.netIDOf(NetworkName("loop-a"))
	if len(eng.nets) != 0 {
		t.Errorf("the rollback reported success while the engine still holds the loop networks %v (W2-1, EV12)", eng.nets)
	}
	var rmArgv [][]string
	for _, o := range lg.log.all() {
		if o.Actor == "runtime" && o.What == "network-rm" {
			rmArgv = append(rmArgv, o.Argv)
		}
	}
	if len(rmArgv) != 1 {
		t.Fatalf("the rollback recorded %d `network rm` commands, want 1 — the network it claims to have taken down was never asked for", len(rmArgv))
	}
	if got, want := strings.Join(rmArgv[0], " "), strings.Join(networkRmArgv("docker", wantNet), " "); got != want {
		t.Errorf("the rollback removed the network by something other than the handle the row stores\n got: %s\nwant: %s (W2-1)", got, want)
	}
}

// W2-5's corollary: an engine that printed no id gave the driver nothing to
// store, and storing nothing is the same bug in a different costume — a row
// whose handle the engine cannot resolve.
func TestServiceCreateWithNoEngineIdIsRefused(t *testing.T) {
	dir, p, lg, eng, env := serviceEngineHarness(t, &execenv.Health{Command: "pg_isready", TimeoutSeconds: 1}, false)
	eng.noID = true

	_, _, ref := p.ensureDependencies(context.Background(), serviceLoop(dir), env, dir)
	if ref == nil {
		t.Errorf("the driver stored a handle the engine never printed; an empty handle makes teardown's removal a no-op that reports success")
	}
	if n := lg.log.count("runtime", "start"); n != 0 {
		t.Errorf("%d starts after an engine that named nothing; the driver proceeded to something it cannot name", n)
	}
	if row := engineServiceRow(t, lg); row.State == store.ProvisionStateReady {
		t.Errorf("the row is ready on a handle the engine never reported (W2-5)")
	}
}
