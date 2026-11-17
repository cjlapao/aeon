package container

// The per-loop service dependencies (EV14): one user-defined network owned by
// the loop, one container per declared service, and the derived names the app
// container receives. The order is fixed by the spec and by physics: the network
// exists before anything can join it, a service is healthy before the loop's
// first command runs against it, and teardown is the reverse so a removal never
// finds a resource still attached to something.
//
// Two refusals hold the shape and both are structural, not configurable: a
// service publishes NO host port (there is no field that could ask for one and
// the builder never emits `-p`), and a service container carries NO mount (a
// database image brings its own storage; mounting the loop's working root into
// it would hand the model's files to a process nobody asked for).

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
)

// NetworkName is the one network a loop owns. The slug is in the name so two
// loops on the same environment cannot collide, and the same sanitise the
// container names use applies here.
func NetworkName(loopSlug string) string {
	return NamePrefix + sanitize(loopSlug) + "-net"
}

// ServiceContainerName is a service's container for one provision. The provision
// id is in it because a loop that retries after a failure gets a NEW provision,
// and a name reused across two generations would mean the new one talks to the
// old one's database.
func ServiceContainerName(loopSlug, serviceName, provisionID string) string {
	return NamePrefix + sanitize(loopSlug) + "-" + sanitize(serviceName) + "-" + provisionID
}

// ServiceArgvSpec is what the builder needs to produce a service container. It
// is a separate shape from CreateSpec precisely because a service has no mount,
// no working directory and no command of its own; sharing one struct would mean
// a field that must be left empty to stay safe.
type ServiceArgvSpec struct {
	ProvisionID string
	LoopSlug    string
	Name        string
	Image       string
	Network     string
	User        string
	PIDsLimit   int
	MemoryMiB   int
	CPUs        float64
	Env         []Arg
}

// serviceArgv is the pure builder. `--network-alias` is the derived name's other
// half: AEON_SERVICE_<NAME>_HOST names the alias, and the alias is what makes
// the DNS answer on the loop's network.
func serviceArgv(bin string, s ServiceArgvSpec) ([]string, []Arg, *execenv.Refusal) {
	if !serviceNamePattern.MatchString(s.Name) {
		return nil, nil, execenv.NewRefusal(execenv.RefusalSpecInvalid,
			"the service name "+s.Name+" cannot become a container name or a network alias",
			"the alias is a DNS name on the loop's network and the container name is what teardown removes; a name "+
				"outside [a-z][a-z0-9-]{0,31} would have to be escaped, and escaping is where injection lives (EV14)",
			"rename the service, for example db, queue-1, search-a")
	}
	if strings.TrimSpace(s.Image) == "" {
		return nil, nil, execenv.NewRefusal(execenv.RefusalSpecInvalid,
			"the service "+s.Name+" has no image to create",
			"the image is the only thing that says what the service is; creating from an empty reference would be "+
				"the engine guessing (EV14)",
			"declare services[].image")
	}
	name := ServiceContainerName(s.LoopSlug, s.Name, s.ProvisionID)
	argv := []string{bin, "run", "-d", "--name", name,
		"--label", LabelManaged + "=1", "--label", LabelLoop + "=" + sanitize(s.LoopSlug),
		"--user", s.User,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", intString(orDefault(s.PIDsLimit, defaultPIDsLimit)),
	}
	// A service carries no memory or cpu cap of its own — the spec declares none
	// (EV14) — so these stay absent unless a caller sets them, and "absent" is
	// the engine's default rather than a number the driver invented.
	if s.MemoryMiB > 0 {
		argv = append(argv, "--memory", intString(s.MemoryMiB)+"m")
	}
	if s.CPUs > 0 {
		argv = append(argv, "--cpus", strconv.FormatFloat(s.CPUs, 'f', -1, 64))
	}
	// The service is reachable ONLY on the loop's network. There is no `-p` here
	// at any depth: a published port is a service the host — and every other
	// process on it, including the model's own commands — can reach.
	argv = append(argv, "--network", s.Network, "--network-alias", s.Name)
	var declared []Arg
	for _, a := range s.Env {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		// The value travels in the engine client's own process environment, and
		// argv carries the NAME (EV11): `docker inspect` shows argv, the logs
		// show argv, and a database password must appear in neither.
		argv = append(argv, "--env", a.Name)
		declared = append(declared, a)
	}
	// No `--entrypoint`, no command: a service runs what its image runs. The
	// override exists for the app container, whose job is to wait, not for a
	// database whose job is to serve.
	argv = append(argv, s.Image)
	return argv, declared, nil
}

// ProbeSpec is the one-shot container a tcp health check runs: on the loop's
// network, with the service's own image, connecting to the service by name. It
// is `--rm` because a probe that stayed behind would be counted as a leak, and
// it is labelled because a probe that crashed is exactly what reconcile must see.
type ProbeSpec struct {
	LoopSlug string
	Network  string
	Image    string
	User     string
	Host     string
	Port     int
}

func probeArgv(bin string, s ProbeSpec) []string {
	return []string{bin, "run", "--rm",
		"--label", LabelManaged + "=1", "--label", LabelLoop + "=" + sanitize(s.LoopSlug),
		"--user", s.User,
		"--network", s.Network,
		"--entrypoint", "/bin/bash", s.Image,
		"-c", probeConnect(s.Host, s.Port),
	}
}

// probeConnect is the command the probe container runs. It is built from a name
// that has already passed the charset check and an int, so there is no string in
// it that an environment could have authored.
func probeConnect(host string, port int) string {
	return fmt.Sprintf(": >/dev/tcp/%s/%d", host, port)
}

// ensureDependencies creates the network and the services, in that order, and
// returns the variables the app container must receive. `created` is everything
// this call made, so the caller that fails can take it back down in the reverse
// order rather than waiting for the next daemon start to clean up.
func (p *Provisioner) ensureDependencies(ctx context.Context, loop LoopRef, env execenv.BoundEnv, workDir string) ([]Arg, []store.Provision, *execenv.Refusal) {
	services := env.Spec.Services
	if len(services) == 0 {
		return nil, nil, nil
	}
	if ref := derivedNameRefusal(services); ref != nil {
		return nil, nil, ref
	}
	for _, svc := range services {
		if svc.Port < 1 || svc.Port > 65535 {
			return nil, nil, execenv.NewRefusal(execenv.RefusalSpecInvalid,
				"the service "+svc.Name+" declares port "+intString(svc.Port)+", which is not a port",
				"the driver dials that number on the loop network for the health check and the app dials it for its "+
					"work; a number outside 1–65535 can only ever fail (EV14)",
				"declare the port the service listens on inside its container")
		}
	}
	var created []store.Provision
	rollback := func(ref *execenv.Refusal) ([]Arg, []store.Provision, *execenv.Refusal) {
		p.teardownAll(ctx, created)
		return nil, nil, ref
	}

	net := NetworkName(loop.LoopSlug)
	nrow, ref := p.ensureRow(ctx, loop, env, workDir, store.ProvisionOwnerNetwork)
	if ref != nil {
		return nil, nil, ref
	}
	if nrow.State != store.ProvisionStateReady {
		// The name is what the CREATE needs; the id is what the ROW keeps. W2-1's
		// rule for a container's handle and W2-5's for a service's, now here: a
		// row must store something the engine can be asked about again, and only
		// the id the engine printed is answered by `network ls --no-trunc` and
		// accepted by `network rm`. Storing `net` — a name this package invented
		// from the slug — left a ledger whose handle no read returned, so the
		// start sweep read the loop's own network as a resource no row claimed and
		// removed it (and asked the CONTAINER list about it on the way, marking the
		// row failed for good measure). The driver refuses an empty print rather
		// than storing nothing, so `id` is never empty on this line.
		id, err := p.Runtime.NetworkCreate(ctx, net, loop.LoopSlug)
		if err != nil {
			return rollback(p.failRow(ctx, nrow, "creating the loop network: "+err.Error()))
		}
		if _, sref, err := p.Ledger.MarkProvisionReady(nrow.ID, id); err != nil {
			return rollback(p.failRow(ctx, nrow, "the ledger refused the network's ready mark: "+err.Error()))
		} else if sref != nil {
			return rollback(refuseStore(sref))
		}
		created = append(created, nrow)
	}

	var appArgs []Arg
	for _, svc := range services {
		srow, ref := p.ensureRow(ctx, loop, env, workDir, "service:"+svc.Name)
		if ref != nil {
			return rollback(ref)
		}
		if srow.State != store.ProvisionStateReady {
			var senv []Arg
			if p.ServiceArgs != nil {
				var r *execenv.Refusal
				senv, r = p.ServiceArgs(env, svc)
				if r != nil {
					return rollback(r)
				}
			} else if len(svc.Env) > 0 || len(svc.Secrets) > 0 {
				return rollback(execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
					"the service "+svc.Name+" declares environment values but the daemon has not wired service resolution",
					"a service's env literals and secret references are resolved by the daemon, which is the only "+
						"holder of the store and the file doors; without that door the driver would be inventing values",
					"this is a daemon build fault — report it; a loop is not started against an unresolvable service"))
			}
			// The caller names the service and the driver chooses the shape. A
			// caller that could hand `Create` an argv of its own is a caller that
			// could hand it a mount or a published port.
			handle, err := p.Runtime.Create(ctx, CreateSpec{
				ProvisionID: srow.ID, LoopSlug: loop.LoopSlug, Image: svc.Image,
				User: p.User, PIDsLimit: defaultPIDsLimit,
				NetworkMode: net, ServiceName: svc.Name, Env: senv,
			})
			if err != nil {
				return rollback(p.failRow(ctx, srow, "creating the service "+svc.Name+": "+err.Error()))
			}
			if err := p.Runtime.Start(ctx, handle); err != nil {
				return rollback(p.failRow(ctx, srow, "starting the service "+svc.Name+": "+err.Error()))
			}
			ready, sref, err := p.Ledger.MarkProvisionReady(srow.ID, handle)
			if err != nil {
				return rollback(p.failRow(ctx, srow, "the ledger refused the service's ready mark: "+err.Error()))
			} else if sref != nil {
				return rollback(refuseStore(sref))
			}
			// The READY row, not the one in hand: the probe runs inside the
			// container the row names by handle, and a row still carrying
			// `provisioning` with an empty handle skips the check EV14 exists to
			// run — the first version of this line did exactly that, and the
			// unhealthy-service test caught it.
			srow = ready
			created = append(created, srow)
		}
		if ref := p.WaitHealthy(ctx, svc, srow, net, workDir); ref != nil {
			return rollback(ref)
		}
		appArgs = append(appArgs,
			Arg{Name: ServiceHostVar(svc.Name), Value: svc.Name},
			Arg{Name: ServicePortVar(svc.Name), Value: intString(svc.Port)})
	}
	return appArgs, created, nil
}

// ensureRow is the write-ahead half for one dependency: a row exists before the
// resource does, and a live ready row is reused.
func (p *Provisioner) ensureRow(ctx context.Context, loop LoopRef, env execenv.BoundEnv, workDir, owner string) (store.Provision, *execenv.Refusal) {
	if live, ok := p.reusableOwner(ctx, loop, owner); ok {
		return live, nil
	}
	in := store.NewProvision{EnvironmentID: env.ID, ComponentRoot: loop.ComponentRoot,
		LoopSlug: loop.LoopSlug, Backend: env.Backend, Owner: owner}
	row, sref, err := p.Ledger.CreateProvision(in)
	if err != nil {
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the "+owner+" row could not be written: "+err.Error(),
			"every resource the daemon creates is written to the ledger first, or a crash leaves it unfindable (EV12)",
			"fix the store error and run again")
	}
	if sref != nil {
		return store.Provision{}, refuseStore(sref)
	}
	return row, nil
}

// failRow settles a dependency's row as failed with the driver's sentence, so
// the ledger never shows something running that is not.
func (p *Provisioner) failRow(ctx context.Context, row store.Provision, reason string) *execenv.Refusal {
	if _, _, err := p.Ledger.MarkProvisionFailed(row.ID, reason); err != nil {
		reason = reason + " · and the ledger refused the failure: " + err.Error()
	}
	return execenv.NewRefusal(execenv.RefusalProvisionFailed, reason,
		"the loop's services are part of its environment: starting the step against a dependency that is not up "+
			"produces a command that fails in a way nobody can read afterwards (EV14)",
		"read the sentence above, fix the service, and run the step again")
}

// teardownAll takes down what this call created, services first and the network
// last, because a network with containers attached cannot be removed.
func (p *Provisioner) teardownAll(ctx context.Context, rows []store.Provision) *execenv.Refusal {
	var first *execenv.Refusal
	for i := len(rows) - 1; i >= 0; i-- {
		// The ledger's CURRENT row, not the snapshot taken when the resource was
		// created: the snapshot has no handle (it was written before the create
		// returned one), and a teardown that cannot name the resource removes
		// nothing while reporting success — measured, it left the loop's network
		// running behind a failed provision.
		row, err := p.Ledger.GetProvision(rows[i].ID)
		if err != nil {
			if first == nil {
				first = execenv.NewRefusal(execenv.RefusalProvisionFailed,
					"the dependency row "+rows[i].ID+" could not be read for teardown: "+err.Error(),
					"teardown removes by handle, and without the row nobody knows which resource is this loop's",
					"re-run teardown; reconcile settles the row at the next daemon start")
			}
			continue
		}
		if _, ref := p.teardownRow(ctx, row); ref != nil && first == nil {
			first = ref
		}
	}
	return first
}
