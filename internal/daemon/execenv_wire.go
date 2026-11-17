package daemon

// The step's execution environment, resolved per command (S59 T5).
//
// # Where a step's environment comes from
//
// `bash` asks (through `tools.ToolContext.Exec`), and this file answers. It is
// the only place in the daemon that reads the process environment for a child
// that a MODEL is driving — every other inheritance in the tree is either
// named (the terminal, EV10) or refused (Spawn, EV5). The reads below go
// through the injected `Getenv`/`Files` so the whole resolver is testable
// without a process, and so "what did the child get?" has exactly one answer
// site.
//
// # The order is the security property
//
//  1. step → loop → attended. A failure to read attended is UNATTENDED, never
//     "assume attended": the alternative is a legacy binding inheriting the
//     daemon's environment because a query errored.
//  2. root → component → binding. No binding is a refusal (EV2); there is no
//     built-in default, because a default is what a workspace silently runs
//     with after the last refactor.
//  3. `execenv.Resolve` — the one tightening (EV6) happens there, and only
//     toward restrictive, and it is carried out here as a recorded `downgraded`
//     rather than a quiet change.
//  4. secret references (EV7) — ALL of them resolve or the command does not
//     run. A half-resolved environment is a command running with one credential
//     and an empty second one, which is not a failure mode worth having.
//  5. backend. `host-*` runs here; anything else needs a registered backend,
//     and a missing one is a REFUSAL with zero spawns (EV11). Falling back to
//     the host because Docker is not installed is how a container escape
//     becomes a Tuesday.
//  6. the record, always: names only, including for the refusal.
//
// # Why the record is written on the refusal path too
//
// A step that could not run because its environment is unbound is a fact about
// that step, and the alternative — recording only the successes — makes the
// table read as "every step ran in something", which is the exact confidence
// this slice is not earned by having.

import (
	"context"
	"errors"
	"fmt"
	"github.com/cjlapao/aeon/internal/vault"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/execenv/container"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// systemScrubbedSlug is the system-seeded `host-scrubbed` environment (EV19's
// inert seed; T6 seeds it the way S43's settings seed theirs). EV6 downgrades a
// legacy binding ONTO this row, so its absence is a refusal rather than an
// inherit: if the safe environment has been deleted, "safe" is not a thing the
// daemon can promise, and it says so.
const systemScrubbedSlug = "host-scrubbed"

// daemonGitEnv is the environment for the daemon's OWN git children (EV10):
// the allowlisted base, ambient `GIT_*` stripped, the prompt guard so a
// credential question becomes an error a step can report rather than a hang —
// and `SSH_AUTH_SOCK`, which is passed here and nowhere else (D9), because a
// code action pushing to a remote is the one thing in the daemon that has a
// legitimate reason to reach the person's ssh agent.
func daemonGitEnv() []string {
	return execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv, Names: environNames}, execenv.GitEnvDaemon)
}

// newStepExecResolver wires the resolver from the daemon's own process.
//
// This is the ONE place in the daemon that reaches `os.Environ` for a
// model-driven child, and it reaches it as data handed to the resolver: the
// resolver itself never calls `os.Getenv`, which is what lets a test answer the
// whole question from a map. A nil store returns nil, and a nil resolver leaves
// bash inert — a storeless daemon has no environments, no bindings and no
// secret references, so it runs no model-driven command (EV2's fail-closed,
// the same posture as a nil policy seam).
// withVault hands the resolver the variable manager; nil resolver or nil vault is
// left exactly as it was.
func withVault(r *StepExecResolver, v *vault.Vault) *StepExecResolver {
	if r != nil {
		r.Vault = v
	}
	return r
}

func newStepExecResolver(st *store.Store) *StepExecResolver {
	if st == nil {
		return nil
	}
	r := &StepExecResolver{
		Store:       st,
		Getenv:      os.Getenv,
		Names:       environNames,
		Files:       execenv.OSFiles{},
		DaemonUID:   execenv.CurrentUID(),
		Credentials: daemonCredentials(),
		// A1 GRANTED at T0: the `store` kind is wired, through the package that
		// owns the files. A daemon that never had it still refuses the kind
		// per-reference (an id nobody stored resolves to nothing).
		SecretLookup: secretLookup(),
	}
	// S59 T9: the non-host backends. `container` is registered whenever there is
	// a store to write its ledger to — the engine being reachable is a runtime
	// question the provisioner asks and REFUSES on (EV13), not a wiring question:
	// a daemon that registered the backend only when docker happened to be up
	// would make "is this environment executable" depend on the daemon's boot
	// order. With the store gone there is no ledger, and a backend that cannot
	// write ahead is a backend that leaks containers, so it is simply absent and
	// a container-bound step is refused (EV11's no-fallback registry).
	if p := containerProvisioner(st); p != nil {
		// The two doors the driver cannot open itself, because only the daemon
		// holds the store, the file opener and the policy engine (EV10, EV11).
		// A service that declares a secret or a health check while these are
		// unwired is REFUSED by the driver, never started blind.
		p.ServiceArgs = r.serviceArgs
		p.Probe = daemonProbe
		r.Backends = map[string]BackendExecutor{execenv.BackendContainer: container.Backend{P: p}}
	}
	return r
}

// environNames is the daemon's environment as names, for the passthrough
// patterns (`LC_*`) `Build` and `GitEnv` may match against. Values are never
// taken from here — the lookup is a separate, narrower door — so the ambient
// surface a child can widen itself through is a list of names.
func environNames() []string {
	raw := os.Environ()
	out := make([]string, 0, len(raw))
	for _, kv := range raw {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out = append(out, kv[:i])
		}
	}
	return out
}

// daemonCredentials is the list of variables that belong to the DAEMON and are
// therefore never referenceable as an application secret (EV7).
//
// The names come from their owners, not from a copy typed here: the provider
// keys from `daemon`'s own constants, the forge names from
// `forge.TokenEnvNames()` (A2 — the same list `DefaultTokenSource` reads, so
// the gate and the source cannot drift), and narrow's surface key from
// `narrow.KeyEnv`. The two extras have no exported constant to name them by:
// `AEON_CARD_SIGNING_KEY` (`agentsvc`) and `CURSOR_SIGNING_KEY` (`secrets`, and
// note it is NOT `AEON_`-prefixed, which is exactly why the namespace rule
// alone would have left it referenceable).
//
// An unlisted name is a referenceable credential, so this list is the whole of
// EV7's second half and belongs in a reviewed diff, never in a helper that
// "probably covers it".
func daemonCredentials() execenv.DaemonCredentials {
	return execenv.DaemonCredentials{
		Provider: []string{EnvAnthropicKey, EnvOpenAIKey},
		Forge:    forge.TokenEnvNames(),
		Extra: []string{
			narrow.KeyEnv,           // the narrow HTTP surface's key
			"AEON_CARD_SIGNING_KEY", // agentsvc's card signer
			"CURSOR_SIGNING_KEY",    // the framework's cursor key, vendor-named
			EnvAnthropicModel, EnvOpenAIModel,
			EnvAnthropicBaseURL, EnvOpenAIBaseURL,
		},
	}
}

// secretLookup is the `store`-kind door (EV7's third source, A1 GRANTED at T0's
// ruling): a reference's `source` is a credential id and the value comes from
// the package that owns those files, never from a path the reference could
// name. `internal/secrets` keeps the paths, the 0600 and the rotation; this is
// the narrowest thing it publishes — one id in, one value out, and a miss is a
// `false`, not an error the door has to interpret.
//
// The value returned here has exactly two destinations: the injected child
// environment, and the redactor built from the SAME injections (T4's one
// capture boundary). It is never logged, never rendered, and `secretref.check`
// discards it in the statement that produced it.
func secretLookup() func(credentialID string) (string, bool) {
	return func(credentialID string) (string, bool) {
		if strings.TrimSpace(credentialID) == "" {
			return "", false
		}
		v, err := secrets.ReadRouteKey(credentialID)
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
}

// stepEnvironmentEmitter is the `step.environment` echo (S59 T6, EV9). The
// durable record is the `step_environment` row the resolver already writes;
// this event is its announcement to whoever is watching, and it carries names
// and the outcome only — vars, refs, the downgrade, and the refusal sentence
// when there was one (which never contains a value, because the resolver's
// refusals name the reference and not what it pointed at).
//
// A step the store cannot be read for still gets its event, with empty
// routing fields: an event that never arrived is worse than one that arrived
// without a loop slug, and the row remains the record either way.
func stepEnvironmentEmitter(st *store.Store, publish func(kind, componentRoot string, payload any)) func(stepID int64, vars, refs []string, downgraded bool, refusal string) {
	if publish == nil {
		return nil
	}
	return func(stepID int64, vars, refs []string, downgraded bool, refusal string) {
		agent, root, slug := "", "", ""
		if st != nil {
			if step, err := st.ReadStep(stepID); err == nil {
				agent, root, slug = step.Agent, step.ComponentRoot, step.LoopSlug
			}
		}
		payload := map[string]any{
			"stepId": stepID, "agent": agent, "loopSlug": slug,
			"variables": vars, "secretRefs": refs, "downgraded": downgraded,
		}
		if refusal != "" {
			payload["refusal"] = refusal
		}
		publish("step.environment", root, payload)
	}
}

// HostBackends are the backends this daemon executes itself, in this process.
// Anything else needs an entry in StepExecResolver.Backends.
var hostBackends = map[string]bool{
	"host-scrubbed": true,
	"host-inherit":  true,
}

// BackendExecutor runs a command in a non-host backend (T9's container). It is
// an interface rather than a function field so a backend that owns a container
// can hold its own state — a driver, a network, a provision ledger row — behind
// one door the resolver does not have to know about.
type BackendExecutor interface {
	Executor(ctx context.Context, env execenv.BoundEnv, child execenv.ChildEnv, ident execenv.ProvisionIdent) (tools.CommandExecutor, *execenv.Refusal)
}

// BackendTeardown is the half of a backend that `environment.teardown` needs
// (S59 T6, EV11/EV12): a driver that can also REMOVE what it created. It is a
// separate, optional capability on purpose. A backend that can run commands but
// cannot be asked to delete its container is refused out loud by the teardown
// door — marking the ledger row `removed` without the removal would launder a
// leaked container into "nothing is running", which is the failure EV12 exists
// to prevent.
type BackendTeardown interface {
	// Teardown removes the provision's container. An error is the driver's own
	// sentence, kept verbatim into the ledger's `last_error`.
	Teardown(ctx context.Context, provision store.Provision) error
}

// StepExecResolver answers "with what does this step's next command run?".
//
// Every field is injected. The zero value answers with a refusal naming what
// is missing, which is the correct behaviour for a daemon that was built
// without these parts rather than a panic in the middle of a step.
type StepExecResolver struct {
	// Store is the daemon's store: bindings, environments, secret references,
	// the attended read, and the step record. A nil Store means a storeless
	// daemon, and a storeless daemon has no environments — it refuses, and bash
	// stays inert (fail-closed, as with the policy seam).
	Store *store.Store

	// Getenv and Names are the daemon's environment, as data (EV5). They are
	// the ONLY way the process environment reaches a child anywhere in the
	// model-driven path, and a test hands both from a map.
	Getenv func(name string) string
	Names  func() []string

	// Files reads key files (T4). Nil means a key-file reference refuses with
	// its own sentence rather than the resolver opening anything.
	Files execenv.FileOpener

	// DaemonUID is the uid a key file must be owned by. It is injected rather
	// than read here so a test can ask what happens when the file is somebody
	// else's (T4's owner rule fails closed on -1).
	DaemonUID int

	// Credentials names the daemon's own variables, which are never
	// application secrets (EV7). Assembled by T6 from `daemon`'s provider
	// constants, `forge.TokenEnvNames()`, `narrow.KeyEnv` and the vendor-shaped
	// extras; empty here means every daemon-env reference refuses, because an
	// unchecked list is not a clean one.
	Credentials execenv.DaemonCredentials

	// SecretLookup resolves a `store`-kind reference against the daemon's
	// credential records (A1). Nil means those references refuse with
	// `store-not-wired`, never with the empty string.
	SecretLookup func(credentialID string) (string, bool)

	// Vault is the variable manager. A `store`-kind reference names a variable in
	// it (its `source` is the variable's name); the project's own value wins over
	// the global one, and a name the manager does not hold falls through to the
	// credential records SecretLookup reads. Nil keeps the old behaviour.
	Vault *vault.Vault

	// Backends holds the non-host backends by name. T9 registers `container`;
	// with nothing registered, a container-bound step is refused (EV11).
	Backends map[string]BackendExecutor

	// Emit is T6's door to the `step.environment` event: the resolver hands
	// over names, sources and the outcome, never a value. Nil means no event,
	// which is the storeless-daemon shape and not a lost record — the row is
	// the record, the event is its echo.
	Emit func(stepID int64, vars, refs []string, downgraded bool, refusal string)

	// Attended overrides the loop's attended read (EV6's input). Nil — the
	// daemon's own shape — reads the loop row through the store. It exists so
	// the one cell the store cannot produce on command is testable: the loop
	// says "attended" and the READ FAILS. The alternative is no test for the
	// fail-closed arm, which is the arm that decides whether a legacy binding
	// inherits the daemon's provider keys.
	Attended func(ctx context.Context, stepID int64) (*bool, error)

	// Log is the daemon's logger, in the same injected shape every seam here
	// uses. Nil is silent.
	Log func(format string, args ...any)
}

// For resolves the calling step's environment and returns the executor that
// runs its next command.
//
// A returned error is ALWAYS an `*execenv.Refusal` on the refusal paths (bash
// renders its sentence verbatim to the model); a non-refusal error is an
// infrastructure failure — a store that could not be read — and bash says that
// in its own voice.
func (r *StepExecResolver) For(ctx context.Context, workspaceRoot string) (tools.CommandExecutor, error) {
	if r.Store == nil {
		return nil, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"this daemon has no store, so no execution environment can be resolved",
			"environments, bindings and secret references are rows, and this daemon was started without one",
			"run the daemon with a store, or accept that a storeless daemon runs no model-driven command")
	}

	stepID, haveStep := stepctx.StepID(ctx)

	// --- 1..5. the shared composition -------------------------------------------
	//
	// Everything between "is this loop attended" and "here is a built child
	// environment" lives in `compose`, because `environment.effective` (EV18)
	// answers the SAME question a step answers and must not answer it through a
	// second implementation that can drift. `compose` writes nothing: the
	// record and the event are this method's, and a preview that wrote a step
	// row would be a preview that lied about what it was.
	c := r.compose(ctx, workspaceRoot, r.attendedFor(ctx, stepID, haveStep, workspaceRoot))
	if c.refusal == nil {
		ident := r.provisionIdent(workspaceRoot, stepID, haveStep)
		executor, refusal := r.executorFor(ctx, *c.res, *c.child, c.injections, ident)
		if refusal == nil {
			// --- 6. the record, names only --------------------------------------
			r.record(stepID, haveStep, c.bound, c.res, c.refs, c.child, nil)
			if r.Emit != nil {
				r.Emit(stepID, childVarNames(*c.child), c.refs, c.res.Downgraded, "")
			}
			return executor, nil
		}
		c.refusal = refusal
	}
	r.record(stepID, haveStep, c.bound, c.res, c.refs, c.child, c.refusal)
	return nil, c.refusal
}

// composed is everything the shared composition knew when it stopped, including
// the partial state a refusal is reported against. Every field is what the
// step record names: a step that died on an unresolvable reference still gets a
// row saying which environment it tried to run in, because "it never started"
// and "it started in the wrong place" are different facts for recovery.
type composed struct {
	in         execenv.ResolveInput
	bound      *execenv.BoundEnv
	res        *execenv.Resolution
	refs       []string
	injections []execenv.Injection
	child      *execenv.ChildEnv
	refusal    *execenv.Refusal
	// infra is a store or read failure — not a refusal, and never the
	// resolver's own opinion. `For` returns it as an error; `Preview` does too.
	infra error
}

// compose is the composition every consumer of "what does this root run in"
// shares: the attended input, the three binding scopes, EV6's downgrade, the
// environment's references, and the built child. It reads rows and resolves
// references, and it writes nothing at all — no step record, no audit row, no
// provision.
//
// The stages stop at the first refusal and keep everything they learned, which
// is what lets `For` make exactly one `record` call per path with exactly the
// arguments it made before this function was extracted (T5's step goldens pin
// those rows; a second record call on the executor path would have shown up as
// a duplicate).
func (r *StepExecResolver) compose(ctx context.Context, workspaceRoot string, attended *bool) composed {
	var c composed

	// --- the binding -------------------------------------------------------------
	in, bound, boundRefusal, err := r.bindingsFor(workspaceRoot)
	if err != nil {
		c.infra = err
		return c
	}
	c.in, c.bound = in, bound
	if boundRefusal != nil {
		c.refusal = boundRefusal
		return c
	}

	// --- resolve (EV2, EV6) ------------------------------------------------------
	in.Attended = attended
	in.ScrubbedSystem = r.scrubbedSystem()
	res, refusal := execenv.Resolve(in)
	if refusal != nil {
		c.refusal = refusal
		return c
	}
	c.res = &res

	// --- the secrets (EV7) -------------------------------------------------------
	refs, err := r.Store.EnvironmentSecretRefs(res.Env.ID)
	if err != nil {
		c.infra = fmt.Errorf("daemon: the environment's secret references could not be read: %w", err)
		return c
	}
	c.refs = refs
	injections, rRefusal := r.resolveSecrets(refs, workspaceRoot)
	if rRefusal != nil {
		c.refusal = rRefusal
		return c
	}
	c.injections = injections

	// --- build ------------------------------------------------------------------
	//
	// `Build` runs for the legacy backend too and is then set aside. The spec
	// still has to parse, the values it sets still have to be legal, and the
	// references still have to resolve against the deny list: every check that
	// applies to a command that will run applies to one that inherits, and a
	// legacy binding is not a licence for a malformed spec. What cannot be a
	// built allowlist is the child's environment itself — the legacy backend's
	// whole claim is that it is the daemon's own, and `Build` answers with a
	// list it constructed, which would have left an attended step on an upgraded
	// store with an EMPTY environment instead of the inherited one it was
	// promised. That is the continuity EV6 names, and it fails silently.
	child, refusal := execenv.Build(res.Env.Spec, execenv.DaemonEnv{Lookup: r.Getenv, Names: r.Names}, injections)
	if refusal == nil && res.Env.Backend == execenv.BackendHostInherit {
		child = execenv.LegacyInherit()
	}
	if refusal != nil {
		c.refusal = refusal
		return c
	}
	c.child = &child
	return c
}

// Preview answers EV18's `environment.effective` through the SAME composition a
// step goes through, and is the reason the panel's preview can be trusted: it is
// not a description of what a step would get, it is the resolution, stopped one
// step before the spawn. It never records, never emits, and never spawns — and
// `attended` is supplied by the caller because a preview is asked about a loop
// that may not exist yet.
func (r *StepExecResolver) Preview(ctx context.Context, workspaceRoot string, attended *bool) (composed, error) {
	if r.Store == nil {
		return composed{}, errNoStoreForEnvironment
	}
	c := r.compose(ctx, workspaceRoot, attended)
	if c.infra != nil {
		return c, c.infra
	}
	return c, nil
}

// attendedFor is `OperatorPresentFor`'s read path (step.go), reused rather than
// re-invented so the two questions — "may this step ask a human?" and "is this
// loop ever going to be attended?" — cannot answer differently.
//
// Every failure is nil, which `execenv.Resolve` reads as unattended (EV6): a
// store error must never be the reason a legacy environment inherits.
func (r *StepExecResolver) attendedFor(ctx context.Context, stepID int64, haveStep bool, root string) *bool {
	if !haveStep || r.Store == nil {
		return nil
	}
	if r.Attended != nil {
		// The seam's own failure is the cell this function exists to fail
		// closed on: an answer that cannot be read is not an answer.
		attended, err := r.Attended(ctx, stepID)
		if err != nil {
			r.logf("execenv: the attended read failed for step %d; treating the loop as unattended: %v", stepID, err)
			return nil
		}
		return attended
	}
	step, err := r.Store.ReadStep(stepID)
	if err != nil {
		r.logf("execenv: step %d could not be read; treating the loop as unattended: %v", stepID, err)
		return nil
	}
	if step.LoopSlug == "" {
		return nil
	}
	loop, err := r.Store.GetLoop(store.Scope{LoopSlug: step.LoopSlug, ComponentRoot: step.ComponentRoot})
	if err != nil {
		r.logf("execenv: loop %s@%s could not be read; treating it as unattended: %v",
			step.LoopSlug, step.ComponentRoot, err)
		return nil
	}
	attended, err := r.Store.LoopAttended(ctx, loop.ID)
	if err != nil {
		r.logf("execenv: attended state of loop %s could not be read; treating it as unattended: %v", loop.ID, err)
		return nil
	}
	return attended
}

// bindingsFor reads the three scopes Resolve may pick from. The store answers
// with the picked binding and the scope it came from, so this function's only
// job is to put it in the slot named by that source — and a source string it
// does not recognise is an error, not a guess.
func (r *StepExecResolver) bindingsFor(root string) (execenv.ResolveInput, *execenv.BoundEnv, *execenv.Refusal, error) {
	var in execenv.ResolveInput
	var bound *execenv.BoundEnv

	// The component is read first and is allowed to be absent: a root nobody
	// registered as a component can still be bound at WORKSPACE scope, and the
	// store answers the workspace scope for an empty component id. Skipping the
	// binding read whenever there is no component is the bug this comment
	// prevents — it makes a workspace-bound step look unbound, which is a
	// refusal to run at all.
	//
	// A store failure is a different thing from "no component registers this
	// root", and the sentinel tells them apart; matching on the sentence would
	// turn a database problem into a silently unbound workspace.
	componentID := ""
	comp, err := r.Store.ComponentForRoot(root)
	if err == nil {
		componentID = comp.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		return in, nil, nil, fmt.Errorf("daemon: the component of %s could not be read: %w", root, err)
	}

	b, source, err := r.Store.BindingFor(componentID)
	if err != nil {
		return in, nil, nil, fmt.Errorf("daemon: the environment binding of %s could not be read: %w", root, err)
	}
	{
		if b.EnvironmentID != "" {
			env, specRefusal, err := r.environment(source, b.EnvironmentID)
			if err != nil {
				return in, nil, nil, err
			}
			if specRefusal != nil {
				return in, nil, specRefusal, nil
			}
			bound = &env
			switch source {
			case "component":
				in.ComponentBinding = &env
			case "root":
				in.RootBinding = &env
			case "workspace":
				in.WorkspaceBinding = &env
			default:
				return in, nil, nil, fmt.Errorf("daemon: the store named %q as a binding scope; this daemon knows "+
					"component, root and workspace, and guessing which one a record applies to is not resolution", source)
			}
		}
	}
	return in, bound, nil, nil
}

// environment turns a binding's id into the row Resolve decides on, parsing its
// spec here so a bad spec is a read failure at the door rather than a panic
// halfway through a spawn.
func (r *StepExecResolver) environment(source, id string) (execenv.BoundEnv, *execenv.Refusal, error) {
	row, err := r.Store.GetEnvironment(id)
	if err != nil {
		return execenv.BoundEnv{}, nil, fmt.Errorf("daemon: the environment %s bound at %s scope could not be read: %w",
			id, source, err)
	}
	spec, err := execenv.ParseSpec([]byte(row.Spec))
	if err != nil {
		// A spec the daemon cannot read is not an environment with an empty
		// body: running with `{}` because the JSON was mangled would run the
		// command with no allowlist and no secrets while the record says it ran
		// in `production`.
		return execenv.BoundEnv{}, execenv.NewRefusal(execenv.RefusalSpecInvalid,
			"the environment "+row.Slug+" carries a spec this daemon cannot read",
			fmt.Sprintf("the stored spec did not parse: %v", err),
			"repair or replace the environment record; a spec that does not parse is not an environment"), nil
	}
	return execenv.BoundEnv{
		ID: row.ID, Slug: row.Slug, Backend: row.Backend,
		// EV6's trigger is the BEHAVIOUR, not the provenance of the row. The
		// migrated row carries legacy = 1 and `CreateEnvironment` can only ever
		// write 0, so reading the column alone would leave a freshly created
		// `host-inherit` environment — a person typing the name into the panel —
		// OUTSIDE the unattended tightening: an unattended loop would inherit
		// the daemon's provider keys because nobody had migrated a row. Any
		// environment whose backend inherits the daemon's environment is legacy
		// in the sense EV6 means.
		Legacy: row.Legacy == 1 || row.Backend == "host-inherit",
		Origin: row.Origin, Spec: spec,
	}, nil, nil
}

// scrubbedSystem reads the system `host-scrubbed` row, and returns nil for
// "not there" — which EV6 turns into a refusal rather than an inherit.
func (r *StepExecResolver) scrubbedSystem() *execenv.BoundEnv {
	row, err := r.Store.GetEnvironment(systemScrubbedSlug)
	if err != nil || row.Archived() {
		return nil
	}
	spec, err := execenv.ParseSpec([]byte(row.Spec))
	if err != nil {
		return nil // an unreadable system row is "not there", and EV6 refuses rather than inheriting
	}
	return &execenv.BoundEnv{
		ID: row.ID, Slug: row.Slug, Backend: row.Backend,
		Legacy: row.Legacy == 1, Origin: row.Origin, Spec: spec,
	}
}

// secretLookupFor is the `store`-kind lookup for work in one workspace: the
// variable manager first (the project's override, then the global value), then
// the credential records. A reference still has to be NAMED by the environment —
// the manager hands a value only to whoever asked for that name.
func (r *StepExecResolver) secretLookupFor(workspaceRoot string) func(string) (string, bool) {
	if r.Vault == nil {
		return r.SecretLookup
	}
	root := vault.RootFor(r.Store, workspaceRoot)
	return func(id string) (string, bool) {
		if v, ok := r.Vault.Resolve(id, root); ok {
			return v, true
		}
		if r.SecretLookup == nil {
			return "", false
		}
		return r.SecretLookup(id)
	}
}

// resolveSecrets resolves every reference the environment names, in order, and
// refuses on the first refusal — reporting ALL of them, because a person
// fixing a form should not learn about the second broken reference after
// restarting the daemon for the first.
func (r *StepExecResolver) resolveSecrets(names []string, workspaceRoot string) ([]execenv.Injection, *execenv.Refusal) {
	if len(names) == 0 {
		return nil, nil
	}
	specs := make([]execenv.RefSpec, 0, len(names))
	for _, name := range names {
		row, err := r.Store.GetSecretRef(name)
		if err != nil {
			return nil, execenv.NewRefusal(execenv.RefusalSecretUnresolvable,
				"the secret reference "+name+" is not there",
				"the environment names a reference this store does not have (it was never created, or it was "+
					"created in another store)",
				"create it with `aeon secretref add`, or remove it from the environment's secret list")
		}
		if row.Archived() {
			return nil, execenv.NewRefusal(execenv.RefusalSecretUnresolvable,
				"the secret reference "+name+" is archived",
				"an archived reference is not resolvable, and an environment that still names it would run "+
					"without the credential its author meant",
				"restore the reference or drop it from the environment")
		}
		specs = append(specs, execenv.RefSpec{
			Name: row.Name, SourceKind: row.SourceKind, Source: row.Source, InjectAs: row.InjectAs,
		})
	}
	injections, refusals := execenv.ResolveAll(specs, execenv.ResolveEnv{
		Lookup:      r.Getenv,
		Files:       r.Files,
		DaemonUID:   r.DaemonUID,
		Credentials: r.Credentials,
		Store:       r.secretLookupFor(workspaceRoot),
	})
	if len(refusals) > 0 {
		first := refusals[0]
		out := &first
		if len(refusals) > 1 {
			extra := make([]string, 0, len(refusals)-1)
			for _, f := range refusals[1:] {
				extra = append(extra, f.What)
			}
			sort.Strings(extra)
			out = execenv.NewRefusal(first.Kind, first.What,
				first.Why+" and "+fmt.Sprintf("%d more reference(s) also failed: %s",
					len(refusals)-1, strings.Join(extra, ", ")), first.Do)
		}
		return nil, out
	}
	return injections, nil
}

// executorFor picks the backend. A backend that is not host and is not
// registered is refused with ZERO spawns — the rule EV11 states and T5's spy
// test proves: "Docker is not installed, so we ran it here instead" is the
// sentence that turns a container bug into a host compromise.
// provisionIdent is the identity a non-host backend is handed (S59 T12, F-1).
// The step row is the authority for the root and the slug: the binding resolved
// under workspaceRoot, but only the step knows which loop it belongs to, and a
// container that cannot name its loop can never be found again during reconcile.
// What the step cannot answer stays EMPTY on purpose — the backend then refuses
// with provision-identity instead of creating a resource nobody can name.
func (r *StepExecResolver) provisionIdent(workspaceRoot string, stepID int64, haveStep bool) execenv.ProvisionIdent {
	ident := execenv.ProvisionIdent{ComponentRoot: workspaceRoot}
	if !haveStep || r.Store == nil {
		return ident
	}
	if step, err := r.Store.ReadStep(stepID); err == nil {
		ident.LoopSlug = step.LoopSlug
		ident.Owner = strconv.FormatInt(stepID, 10)
		if step.ComponentRoot != "" {
			ident.ComponentRoot = step.ComponentRoot
		}
	}
	return ident
}

func (r *StepExecResolver) executorFor(ctx context.Context, res execenv.Resolution, child execenv.ChildEnv,
	injections []execenv.Injection, ident execenv.ProvisionIdent) (tools.CommandExecutor, *execenv.Refusal) {
	backend := res.Env.Backend
	if hostBackends[backend] {
		// The filter is built from the injections the child was built from —
		// the SAME list, not a re-derivation from names, so what gets redacted
		// is exactly what got injected or the filter is a claim about a
		// boundary that does not exist.
		var red *execenv.Redactor
		if len(injections) > 0 {
			red = execenv.NewRedactor(injections)
		}
		return tools.HostExecutor{Env: child, Redactor: red}, nil
	}
	if b, ok := r.Backends[backend]; ok {
		return b.Executor(ctx, res.Env, child, ident)
	}
	return nil, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
		"the environment "+res.Env.Slug+" runs in the "+backend+" backend, which this daemon does not have",
		"no backend is registered under that name, and running a container command on the host because the "+
			"driver is missing would give the command the host it was bound away from",
		"install and register the "+backend+" backend, or bind the component to an environment this daemon can run")
}

// record writes the step's environment row: names, the backend, the downgrade
// mark, or the refusal. It never carries a value or a length, and a failure to
// write it is spoken and not fatal — a step that ran is a step that ran, and
// losing an audit row is not a reason to kill it.
func (r *StepExecResolver) record(stepID int64, haveStep bool, bound *execenv.BoundEnv,
	res *execenv.Resolution, refs []string, child *execenv.ChildEnv, refusal *execenv.Refusal) {
	if !haveStep || stepID == 0 || r.Store == nil {
		return // no step, no row: a bare `aeon ask` has no step to attribute
	}
	in := store.NewStepEnvironment{StepID: stepID}
	switch {
	case refusal != nil:
		kind, sentence := refusal.Kind, refusal.Sentence()
		in.RefusalKind, in.Refusal = &kind, &sentence
	case res != nil:
		id, backend := res.Env.ID, res.Env.Backend
		in.EnvironmentID, in.Backend = &id, &backend
		in.Downgraded = res.Downgraded
		if res.Reason != "" {
			reason := res.Reason
			in.DowngradeReason = &reason
		}
	}
	if child != nil {
		in.VarNames = childVarNames(*child)
	}
	if len(refs) > 0 {
		in.SecretRefs = append([]string(nil), refs...)
	}
	if _, err := r.Store.RecordStepEnvironment(in); err != nil {
		r.logf("execenv: the step %d environment row could not be written (the step is unaffected): %v", stepID, err)
	}
	if r.Emit != nil && refusal != nil {
		r.Emit(stepID, nil, refs, false, refusal.Sentence())
	}
}

// childVarNames is the record's variable column: NAME=SOURCE per entry, which
// is a name and where it came from — never a value (EV9).
func childVarNames(child execenv.ChildEnv) []string {
	vars := child.Names()
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, v.Name+"="+v.Source)
	}
	return out
}

func (r *StepExecResolver) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}
