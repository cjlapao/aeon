package daemon

// execenv_reconcile.go — S59 T9 (EV12, EV15): the container lifecycle's other
// half. The provisioner writes the ledger ahead of the engine; this file is what
// makes the ledger true again after the daemon was not there to watch.
//
// Three moments, one file, because they are the same question asked at three
// times — "does the row and the engine still agree, and which one is lying":
// at start (reconcile), when a loop finishes (teardown), and when a blocked loop
// has held a container for longer than its declared idle TTL (the sweep).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/execenv/container"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// containerProvisioner builds the provisioner from the daemon's own process.
// The two knobs are the daemon's, never the environment's: a spec that could
// name its own `--user` could name root, and a spec that could name the engine
// binary could name anything (EV13).
func containerProvisioner(st *store.Store) *container.Provisioner {
	if st == nil {
		return nil
	}
	return &container.Provisioner{
		Ledger:  st,
		Runtime: &container.CLIRuntime{Bin: os.Getenv("AEON_CONTAINER_BIN")},
		User:    containerUser(),
	}
}

// containerUser is the `--user uid:gid` every Aeon container runs as. Absent the
// explicit knob it is the daemon's own uid — a non-root identity that owns the
// files the workspace mounts, which is the pairing that makes the bind mount
// usable without handing the container root.
// orderForTeardown puts a loop's resources in the order they must come down:
// the app container first, then the services it was dialling, then the network
// they all shared. The driver owns the ranking because the driver owns what can
// still be attached to what; the store returns rows in insertion order, which is
// the CREATION order — exactly backwards, and a network with endpoints attached
// cannot be removed at any price.
// serviceArgs resolves one service's own environment through the resolver's
// doors — the store for a secret reference, the file opener for a key file, the
// process environment for a daemon-env reference — and hands the driver
// name/value pairs. The values go into the ENGINE CLIENT's process environment
// while the argv the driver builds carries the NAME only, which is why a
// database password never appears in `docker inspect` or in the process list
// (EV11). Building it through `execenv.Build` rather than a bespoke loop is
// deliberate: that is where the deny list, the passthrough rules and the
// redaction bookkeeping live, and a second path around it is the leak.
func (r *StepExecResolver) serviceArgs(env execenv.BoundEnv, svc execenv.Service) ([]container.Arg, *execenv.Refusal) {
	injections, ref := r.resolveSecrets(svc.Secrets, "")
	if ref != nil {
		return nil, ref
	}
	spec := execenv.Spec{Env: execenv.EnvSection{Set: svc.Env, Secrets: svc.Secrets}}
	child, ref := execenv.Build(spec, execenv.DaemonEnv{Lookup: r.Getenv, Names: environNames}, injections)
	if ref != nil {
		return nil, ref
	}
	out := make([]container.Arg, 0, len(svc.Env)+len(injections))
	for _, kv := range child.Env() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out = append(out, container.Arg{Name: name, Value: value})
	}
	return out, nil
}

// daemonProbe is the health-check door. The command is the DRIVER'S, built from
// a service name that already passed the charset check and an int port, so the
// string being token-minted could not have been authored by anybody else; it
// then runs through `tools.RunPrepared` like every other command, which remains
// the only place output is captured. The verdict is minted for the daemon as its
// own agent, because a probe is the daemon's action: the step never asked for it
// and has no way to name it. Recorded as a deviation in the task's Notes.
func daemonProbe(ctx context.Context, cmd *exec.Cmd, command string) (tools.RunResult, *execenv.Refusal) {
	spec, err := json.Marshal(map[string]any{"command": command, "state": "allow"})
	if err != nil {
		return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the health probe's policy entry could not be built: "+err.Error(),
			"a probe is a command like any other and goes through the gate; nothing runs unblessed",
			"report this; it is a build fault, not a configuration one")
	}
	target, terr := policy.ParseTarget(daemonProbeAgent, "")
	if terr != nil {
		return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the probe's policy target could be written but not read back: "+terr.Reason,
			"a probe runs only under a verdict, and a verdict that cannot be built is no verdict",
			"report this; it is a build fault, not a configuration one")
	}
	action, aerr := policy.ParseAction("allow")
	if aerr != nil {
		return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the probe's policy action was refused: "+aerr.Reason,
			"a probe runs only under a verdict", "report this; it is a build fault")
	}
	v := policy.Evaluate([]policy.Policy{{
		ID: "s59-daemon-probe", Type: policy.CommandAccess,
		Target: target, Action: action, Spec: spec, Source: policy.Operator,
	}}, policy.Request{Type: policy.CommandAccess, Agent: daemonProbeAgent, Tool: "bash", Command: command},
		time.Now())
	token, ok := v.Token(command)
	if !ok {
		return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the health probe "+command+" was not allowed by policy: "+v.Sentence(),
			"the probe carries no more authority than the verdict that minted its token, and a probe that never "+
				"ran has proven nothing about the service",
			"check the command_access set for the "+daemonProbeAgent+" agent")
	}
	res, err := tools.RunPrepared(ctx, cmd, tools.PreparedOptions{Command: command, Timeout: 30 * time.Second},
		token, nil)
	if err != nil {
		return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the health probe could not run: "+err.Error(),
			"a probe that never ran has not proven anything about the service",
			"read the sentence, fix it, and run the step again")
	}
	return res, nil
}

// daemonProbeAgent is who the daemon is when it acts as itself. A probe is not
// an agent's command and must not borrow a step's identity: the audit line then
// says the daemon ran it, which is the truth.
const daemonProbeAgent = "aeon-daemon"

func serviceEnvKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func orderForTeardown(rows []store.Provision) []store.Provision {
	out := append([]store.Provision(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := container.TeardownRank(out[i].Owner), container.TeardownRank(out[j].Owner)
		if ri != rj {
			return ri < rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func containerUser() string {
	if v := os.Getenv("AEON_CONTAINER_USER"); v != "" {
		return v
	}
	gid := os.Getgid()
	if g := os.Getenv("AEON_CONTAINER_GROUP"); g != "" {
		if n, err := strconv.Atoi(g); err == nil {
			gid = n
		}
	}
	return strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(gid)
}

// ReconcileEnvironments is the daemon-start sweep, called beside ReconcileLoops.
// An engine that is down is inert, not fatal: the rows stay exactly as they are
// and the daemon says so once (the same bargain the worktree reconciliation made
// with the operator — a reconciliation outage must not take the daemon with it).
func (d *Daemon) ReconcileEnvironments(ctx context.Context) (container.Report, error) {
	var rep container.Report
	p := containerProvisioner(d.store)
	if p == nil {
		return rep, nil // a storeless daemon has no provisions to reconcile
	}
	warn := func(format string, args ...any) {
		if d.log != nil {
			d.log.Infof(format, args...)
		}
	}
	rep, err := container.Reconcile(ctx, p.Ledger, p.Runtime, func(msg string) {
		warn("%s", msg)
	})
	if err != nil {
		return rep, err
	}
	if rep.Inert {
		return rep, nil
	}
	warn("environment reconcile: %d adopted, %d finished, %d removed, %d failed, %d skipped (%d unlabelled containers left untouched)",
		rep.Adopted, rep.Finished, rep.Removed, rep.Failed, rep.Skipped, rep.Unlabelled)
	return rep, nil
}

// SweepIdleProvisions removes the containers of loops that have been blocked
// past the declared idle TTL (EV15). A blocked loop is a loop somebody may come
// back to, so its container is kept — for as long as the person said, and not a
// minute longer. A retry ensures a FRESH row; nothing here reuses a container
// whose loop has been asleep.
func (d *Daemon) SweepIdleProvisions(ctx context.Context) (int, error) {
	p := containerProvisioner(d.store)
	if p == nil {
		return 0, nil
	}
	ttl := d.provisionIdleTTL(ctx)
	rows, err := d.store.ListLiveProvisions()
	if err != nil {
		return 0, fmt.Errorf("environment sweep: the live provisions could not be read: %w", err)
	}
	removed := 0
	for _, row := range orderForTeardown(rows) {
		if row.Backend != execenv.BackendContainer {
			continue // a host backend holds no resource; there is nothing to sweep
		}
		if !d.loopBlocked(row) {
			continue // `running`, `done`, `aborted` — not this sweep's business
		}
		at := row.CreatedAt
		if row.ReadyAt != nil {
			at = *row.ReadyAt
		}
		stamp, err := time.Parse(time.RFC3339, at)
		if err != nil {
			// A row whose timestamp will not parse is a row whose age nobody
			// can establish. Removing it would be a guess about somebody's live
			// container; leaving it is the safe reading, said out loud.
			if d.log != nil {
				d.log.Infof("environment sweep: provision %s has an unreadable age (%q), so its container stays", row.ID, at)
			}
			continue
		}
		if time.Since(stamp) < ttl {
			continue
		}
		if _, refusal := p.Teardown(ctx, row.ID); refusal != nil {
			if d.log != nil {
				d.log.Infof("environment sweep: the idle container of provision %s would not come down: %s", row.ID, refusal.Sentence())
			}
			continue
		}
		removed++
	}
	if removed > 0 && d.log != nil {
		d.log.Infof("environment sweep: %d idle container(s) removed after %s", removed, ttl)
	}
	return removed, nil
}

// provisionIdleTTL reads the settings-backed TTL through the half that already
// owns it (T6's vocabulary reader), falling back to the declared default when
// the settings are not wired at all (a storeless or settings-less daemon).
func (d *Daemon) provisionIdleTTL(ctx context.Context) time.Duration {
	if d.store == nil {
		return defaultProvisionIdleTTL
	}
	if svc := newEnvironmentService(d.store, nil, d.log, d.settings); svc != nil {
		if r := svc.provisionIdleTTL(ctx); r.TTL > 0 {
			return r.TTL
		}
	}
	return defaultProvisionIdleTTL
}

// loopBlocked is the sweep's gate: a container is idle-expired only when its
// LOOP reads `blocked` — the one status this codebase treats as blocked
// (loop/runner.go:320, loop/continue.go:195, store/loop_control.go:155), taken
// straight from the column SetLoopStatus writes. An unknown status is NOT
// blocked; nothing here invented a second vocabulary of blockedness.
//
// The loop is resolved the way the ledger is keyed. A provision row carries a
// component root and a loop SLUG and never the loop's hex id, and a slug is
// unique per component root and never globally (store.LoopsBySlug's own doc —
// two checkouts hold `refactor` without colliding), so the pair the row already
// holds IS the scope, and (*store.Store).GetLoop is the store's door for it:
// `WHERE component_root=? AND slug=?`, one row or not found. The pair, not the
// slug alone, is what makes the answer one loop instead of a SET — a gate that
// guessed which `refactor` owns this container would tear down the wrong loop's.
//
// It is NOT ReadPinnedLoop(slug), and for two slices it was, which is the
// defect this comment now records instead of causing: ReadPinnedLoop is
// `WHERE id=?` (store/blueprint.go:219), so handing it the slug missed on EVERY
// row a real daemon holds — measured `ReadPinnedLoop(slug="s59-probe") ->
// status="" err=store: not found` — the gate answered "not blocked", the sweep
// `continue`d past all of them, removed nothing, logged nothing and returned a
// nil error, and EV22 was false as shipped. The same defect class as W2-2 at
// terminalProvisions below: a slug or an empty root handed to a door keyed the
// other way, and a dead security-relevant hook that never spoke.
//
// It is not the blueprint pin either, and that is a deliberate narrowing of the
// old code, not a widening of what gets removed: loop.FromPinned only copies
// pin.Loop.Status (loop/state.go:198) after parsing the pinned content, so the
// pin contributed a dependency on a row that has nothing to do with blockedness
// and nothing to the answer — an ordinary `loop.create` loop has no
// loop_blueprint row, so FromPinned errored and a loop the column says `blocked`
// read "not blocked" and held its container forever.
//
// Every branch speaks, because silence is exactly what kept the dead gate
// invisible: a branch that answers without reading the loop says what it looked
// for. An unresolvable loop — an incomplete pair, a read that failed, no row at
// that scope — is NOT eligible for teardown, the restrictive reading, said out
// loud. A container that outlives a loop nobody can read is a leak somebody
// finds in the ledger and cleans up on purpose; a container removed on the
// strength of a loop nobody read is somebody's live work gone with no record of
// who decided it.
func (d *Daemon) loopBlocked(row store.Provision) bool {
	warn := func(format string, args ...any) {
		if d.log != nil {
			d.log.Infof(format, args...)
		}
	}
	if d.store == nil {
		return false // no store, no ledger, no container — the sweep already refused for want of a store
	}
	sc := store.Scope{LoopSlug: row.LoopSlug, ComponentRoot: row.ComponentRoot}
	if !sc.Valid() {
		warn("environment sweep: provision %s names loop slug %q in component root %q, which is not a (slug, root) pair, so its loop cannot be read and its container stays", row.ID, row.LoopSlug, row.ComponentRoot)
		return false
	}
	l, err := d.store.GetLoop(sc)
	if err != nil {
		warn("environment sweep: the loop %s of provision %s could not be read (%v), so its container stays", sc, row.ID, err)
		return false
	}
	if l.Status != "blocked" {
		warn("environment sweep: provision %s is held by loop %s, which reads %q — a blocked loop is the sweep's business and this is not one, so its container stays", row.ID, sc, l.Status)
		return false
	}
	return true
}

// onEnvironmentTerminal is the teardown hook at the loop's terminal moment
// (EV15): a loop that finished `done` or `aborted` has no reason to hold a
// container. `blocked` deliberately does NOT tear down — that is the sweep's
// job, on the person's schedule. A failure is logged with the engine's own
// sentence and the row stays visible; a container nobody removed and a ledger
// that reads clean is the pair of answers this whole file exists to prevent.
// terminalProvisions is the ledger read the terminal teardown needs (S59 T12,
// W2-2, found by walking a real daemon rather than a fake one).
//
// It used to be `d.store.ProvisionsFor("", id)`. That read is
// `WHERE component_root = ? AND loop_slug = ?`: the rows carry a real component
// root and the loop's SLUG, while `loop.finished` carries the loop's hex ID. So
// BOTH arguments were wrong — an empty root matches nothing, and the id is not
// the slug — the read answered zero rows, and the hook returned without a sound.
// Measured: `cancelled loop eee0b329… — status aborted` with its provision still
// `ready` and its container still Up, and zero `environment teardown` lines in
// any of the lane's eight daemon logs. A dead security-relevant hook stayed
// invisible for two slices because the one thing it never did was speak.
//
// The loop row is the authority that turns a hex id into the pair the ledger is
// keyed by, so it is read first, and every branch says what it looked for.
func (d *Daemon) terminalProvisions(loopID string) ([]store.Provision, error) {
	warn := func(format string, args ...any) {
		if d.log != nil {
			d.log.Infof(format, args...)
		}
	}
	loop, err := d.store.ReadLoopByID(loopID)
	if err != nil {
		warn("environment teardown: the finished loop %s could not be read (%v) — its provisions stay in the ledger and NOTHING was removed", loopID, err)
		return nil, err
	}
	rows, err := d.store.ProvisionsForLoop(loop.Slug)
	if err != nil {
		warn("environment teardown: the ledger read for loop %s (slug %s) failed: %v", loopID, loop.Slug, err)
		return nil, err
	}
	if len(rows) == 0 {
		// A loop that never provisioned is normal; a loop whose provisions cannot
		// be found is the bug. They are only distinguishable if the read says
		// what it looked for, so it says.
		warn("environment teardown: loop %s (slug %s) has no provisions — nothing to remove", loopID, loop.Slug)
	}
	return rows, nil
}

func (d *Daemon) onEnvironmentTerminal(kind string, payload any) {
	if d.store == nil {
		return
	}
	if kind != "loop.finished" {
		return
	}
	fields, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if status, _ := fields["status"].(string); status != "done" && status != "aborted" {
		return
	}
	id, ok := fields["loopId"].(string)
	if !ok || id == "" {
		return
	}
	rows, err := d.terminalProvisions(id)
	if err != nil || len(rows) == 0 {
		return
	}
	p := containerProvisioner(d.store)
	if p == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, row := range orderForTeardown(rows) {
		if !row.Live() || row.Backend != execenv.BackendContainer {
			continue
		}
		if _, refusal := p.Teardown(ctx, row.ID); refusal != nil {
			if d.log != nil {
				d.log.Infof("environment teardown for the finished loop %s: %s", id, refusal.Sentence())
			}
			continue
		}
		if d.log != nil {
			d.log.Infof("environment teardown: the container of provision %s came down with loop %s (%s)", row.ID, id, fields["status"])
		}
	}
}

// StartEnvironmentReconcile starts the idle sweep with the daemon and dies with
// it: the ticker is cancelled with `recoveryCtx`, the same context that already
// owns the recovery goroutines, so a shutdown cannot leave a sweep mid-Remove.
func (d *Daemon) StartEnvironmentReconcile(ctx context.Context, interval time.Duration) {
	if d.store == nil || interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := d.SweepIdleProvisions(ctx); err != nil && d.log != nil {
					d.log.Infof("environment sweep: %v", err)
				}
			}
		}
	}()
}

var _ = store.Provision{} // the ledger types this file passes through, named for the reader
