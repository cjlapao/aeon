package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
)

// Ledger is the slice of the store the provisioner touches. It is an interface
// for one reason beyond tidiness: the crash matrix (#9) has to stop the ledger
// mid-sequence — "the row was written but the container was not created, and now
// the process is dead" is not something you can stage by killing a test binary —
// and a fake ledger that refuses its next transition IS that crash, deterministically.
type Ledger interface {
	CreateProvision(in store.NewProvision) (store.Provision, *store.EnvironmentRefusal, error)
	MarkProvisionReady(id, handle string) (store.Provision, *store.EnvironmentRefusal, error)
	MarkProvisionTearingDown(id string) (store.Provision, *store.EnvironmentRefusal, error)
	MarkProvisionRemoved(id string) (store.Provision, *store.EnvironmentRefusal, error)
	MarkProvisionFailed(id, reason string) (store.Provision, *store.EnvironmentRefusal, error)
	GetProvision(id string) (store.Provision, error)
	ListLiveProvisions() ([]store.Provision, error)
	ProvisionsFor(componentRoot, loopSlug string) ([]store.Provision, error)
}

// LoopRef is who the provision belongs to. `Owner` is the field the ledger's
// one-live-row-per-owner index is keyed on, so it is decided here and not by
// whatever the caller had lying around.
type LoopRef struct {
	ComponentRoot string
	LoopSlug      string
	Owner         string
}

func (l LoopRef) newProvision(envID, backend string) store.NewProvision {
	return store.NewProvision{
		EnvironmentID: envID, ComponentRoot: l.ComponentRoot,
		// The owner is the ledger's vocabulary, not the step's identity: the
		// container belongs to the LOOP (the store refuses any owner outside
		// loop | network | service:<name>), and `LoopRef.Owner` is the step that
		// asked, kept for the log line. One container per loop whatever number of
		// steps share it, found again by (root, slug, owner).
		LoopSlug: l.LoopSlug, Backend: backend, Owner: store.ProvisionOwnerLoop,
	}
}

// Provisioner owns the write-ahead lifecycle (EV12): the row exists BEFORE the
// runtime is asked for anything, so a crash between the two leaves a row that
// reconcile can finish or remove — never a container nobody owns.
type Provisioner struct {
	Ledger  Ledger
	Runtime Runtime

	// User is the `--user uid:gid` the containers run as. It comes from the
	// daemon's configuration, never from the environment's spec: a spec that
	// could choose its own user could choose root.
	User string

	// ServiceArgs resolves one service's own environment: its `env` literals and
	// the values behind its `secrets`. It is a door the daemon supplies because
	// only the daemon holds the store, the file doors and the redactor; the
	// driver never reads a credential file itself. The values it returns are put
	// in the engine client's process environment while argv carries the NAME
	// only (EV11).
	ServiceArgs func(env execenv.BoundEnv, svc execenv.Service) ([]Arg, *execenv.Refusal)

	// Probe is the door for health checks — see ProbeRunner.
	Probe ProbeRunner

	// lastProvisionID is the most recent row this provisioner wrote, for the
	// caller that has to name the thing it just created (the driver's own test
	// and the daemon's log line). It is a convenience, never an authority: the
	// ledger remains the only thing that says what is running.
	lastProvisionID string
}

// Ensure returns the live provision for this loop, creating one if none exists.
// The returned refusal is always three-part and always names the driver's own
// reason — "provision failed" with no engine sentence is the answer that makes a
// person guess.
func (p *Provisioner) Ensure(ctx context.Context, loop LoopRef, env execenv.BoundEnv, workDir string, args []Arg) (store.Provision, *execenv.Refusal) {
	if err := p.Runtime.Available(ctx); err != nil {
		// EV13, the sentence that matters most in this file: the command does
		// NOT run on the host because the container is unavailable.
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"the container runtime is not available: "+err.Error(),
			"a command bound to the container backend runs in a container or not at all — running it on the host "+
				"would run it with the daemon's files and the daemon's network",
			"start the engine, or bind this loop to a host backend (aeon env bind …)")
	}

	if live, ok := p.reusable(ctx, loop, env, workDir); ok {
		return live, nil
	}

	row, ref, err := p.Ledger.CreateProvision(loop.newProvision(env.ID, env.Backend))
	if err != nil {
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the provision row could not be written: "+err.Error(),
			"the ledger is written FIRST, so a container that exists always has a row naming it; skipping the row "+
				"to run sooner is how a leaked container is born (EV12)",
			"fix the store error and run again")
	}
	if ref != nil {
		return store.Provision{}, refuseStore(ref)
	}

	// The dependencies come first, ahead of the loop's own row: the network must
	// exist before the app container can join it, and a service must be healthy
	// before the first command runs against it. What this call creates is
	// remembered so a failure part-way through takes it back down instead of
	// leaving the next daemon start to sweep it (EV12).
	appArgs, made, depRef := p.ensureDependencies(ctx, loop, env, workDir)
	if depRef != nil {
		// Whatever the dependency failure was, this loop's own row must not stay
		// `provisioning` pointing at a container that will never be started.
		if _, _, err := p.Ledger.MarkProvisionFailed(row.ID, depRef.What); err != nil {
			depRef.Why = depRef.Why + " · and the ledger refused the failure: " + err.Error()
		}
		return store.Provision{}, depRef
	}
	args = append(args, appArgs...)
	_ = made

	p.lastProvisionID = row.ID
	if refusal := p.settle(ctx, row, env, workDir, args); refusal != nil {
		// `settle` has already marked the row failed with the driver's sentence;
		// the refusal is the same facts in the door's shape.
		return store.Provision{}, refusal
	}
	ready, err := p.Ledger.GetProvision(row.ID)
	if err != nil {
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the provision was created but cannot be read back: "+err.Error(),
			"the command runs inside the container that row names, and without the row nobody knows which one "+
				"is this loop's (EV12)",
			"re-run the step; reconcile settles the row on the next daemon start")
	}
	return ready, nil
}

// settle is the ordered half: image, create, start, mark ready. Every exit
// writes the failure into the row, and the last error is the engine's own words.
func (p *Provisioner) settle(ctx context.Context, row store.Provision, env execenv.BoundEnv, workDir string, args []Arg) *execenv.Refusal {
	fail := func(what string, cause error) *execenv.Refusal {
		reason := what
		if cause != nil {
			reason = what + ": " + cause.Error()
		}
		if _, sref, err := p.Ledger.MarkProvisionFailed(row.ID, reason); err != nil {
			reason = reason + " · and the ledger refused the failure: " + err.Error()
		} else if sref != nil {
			reason = reason + " · " + sref.Sentence
		}
		return execenv.NewRefusal(execenv.RefusalProvisionFailed, reason,
			"the loop's container was not started, so its command was not run — the container backend has no host "+
				"fallback and never will (EV13)",
			"read the engine's sentence above, fix it, and run the step again; a fresh provision is created for the retry")
	}

	ref := env.Spec.Image
	if ref == nil || ref.Ref == "" {
		return fail("the environment declares no image", nil)
	}
	exists, err := p.Runtime.ImageExists(ctx, ref.Ref)
	if err != nil {
		return fail("asking the engine about the image", err)
	}
	if !exists {
		// `pull: never` is a person's claim that the image is already there. When
		// it is not, the honest answer names the image and stops; pulling anyway
		// would fetch code from a registry the person said not to.
		if strings.EqualFold(ref.Pull, execenv.PullNever) {
			return fail(fmt.Sprintf("image %s is not present and spec.image.pull is never", ref.Ref), nil)
		}
		if err := p.Runtime.Pull(ctx, ref.Ref); err != nil {
			return fail("pulling image "+ref.Ref, err)
		}
	}

	handle, err := p.Runtime.Create(ctx, CreateSpec{
		ProvisionID: row.ID, LoopSlug: row.LoopSlug,
		Image: ref.Ref, User: p.User,
		MemoryMiB: intDeref(env.Spec.Limits.MemoryMiB), CPUs: floatDeref(env.Spec.Limits.CPUs),
		NetworkMode: networkMode(env.Spec.Egress.Mode),
		Mounts:      []Mount{{Host: workDir, Container: ContainerWorkDir}},
		WorkDir:     ContainerWorkDir,
		Env:         args,
	})
	if err != nil {
		return fail("creating the container", err)
	}
	if err := p.Runtime.Start(ctx, handle); err != nil {
		// The container exists, so the handle is recorded BEFORE the failure —
		// reconcile must be able to remove what this attempt created.
		if _, _, err2 := p.Ledger.MarkProvisionFailed(row.ID, "start failed: "+err.Error()+" · handle "+handle); err2 != nil {
			return fail("starting the container (and the ledger would not record it)", err)
		}
		return fail("starting the container "+handle, err)
	}
	if _, sref, err := p.Ledger.MarkProvisionReady(row.ID, handle); err != nil {
		return fail("marking the provision ready", err)
	} else if sref != nil {
		return refuseStore(sref)
	}
	return nil
}

// reusable finds a live, ready row for this loop whose container the engine
// still has. A row whose resource vanished is not reused — reconcile will settle
// it — so this asks the engine rather than trusting the row.
// reusableOwner is `reusable` for one owner: the loop's container, a named
// service, or the network. A live ready row whose resource the engine still
// lists is reused rather than created again — a loop that runs three commands
// does not get three databases.
func (p *Provisioner) reusableOwner(ctx context.Context, loop LoopRef, owner string) (store.Provision, bool) {
	rows, err := p.Ledger.ProvisionsFor(loop.ComponentRoot, loop.LoopSlug)
	if err != nil {
		return store.Provision{}, false
	}
	var live map[string]bool
	switch owner {
	case store.ProvisionOwnerNetwork:
		hs, err := p.Runtime.NetworkList(ctx, LabelManaged+"=1")
		if err != nil {
			return store.Provision{}, false
		}
		// Keyed by the ID the read answers, because that is what the row stores:
		// `network create` printed it and `NetworkCreate` returned it (W2-1 — a row
		// and its read must speak one handle, or a loop that runs three commands
		// creates three networks because the row's handle matched nothing).
		live = map[string]bool{}
		for _, h := range hs {
			live[h.ID] = true
		}
	default:
		hs, err := p.Runtime.List(ctx, LabelManaged+"=1")
		if err != nil {
			return store.Provision{}, false
		}
		live = map[string]bool{}
		for _, h := range hs {
			live[h.ID] = true
		}
	}
	for _, r := range rows {
		if r.Owner != owner || r.State != store.ProvisionStateReady || r.Handle == "" {
			continue
		}
		if !live[r.Handle] {
			continue
		}
		return r, true
	}
	return store.Provision{}, false
}

func (p *Provisioner) reusable(ctx context.Context, loop LoopRef, env execenv.BoundEnv, workDir string) (store.Provision, bool) {
	rows, err := p.Ledger.ProvisionsFor(loop.ComponentRoot, loop.LoopSlug)
	if err != nil {
		return store.Provision{}, false
	}
	handles, herr := p.Runtime.List(ctx, LabelManaged+"=1")
	if herr != nil {
		return store.Provision{}, false
	}
	seen := map[string]bool{}
	for _, h := range handles {
		seen[h.ID] = true
	}
	for _, r := range rows {
		if r.State != "ready" || r.EnvironmentID != env.ID || r.Backend != env.Backend {
			continue
		}
		// W2-10 (S59 T12's walk): the read is keyed on (component root, loop
		// slug) only, and a SERVICE row carries both — it differs from the
		// loop's own row in `Owner` alone, "service:<name>" (written by
		// ensureRow) against the loop's `store.ProvisionOwnerLoop` (written by
		// newProvision; the vocabulary lives in internal/store/provisions.go).
		// Since W2-5 a service row holds a real engine handle, so without this
		// filter a loop's step is matched to its service's container and the
		// command runs INSIDE the database container, on that service's mounts
		// and network. The loop's own row is the only row a step may be handed.
		// Deny-leaning on purpose: an empty or unrecognised owner is not
		// reusable either — a fresh provision is created and the engine is
		// asked again, which is the safe answer; running a step in a container
		// whose owner nobody can name is the guess this ledger never makes.
		if r.Owner != store.ProvisionOwnerLoop {
			continue
		}
		if r.ComponentRoot != workDir && r.ComponentRoot != "" {
			// A loop whose root moved gets a NEW provision: reusing a container
			// whose mount points at the old directory would run the command
			// against files the environment was never bound to.
			continue
		}
		if seen[r.Handle] {
			return r, true
		}
	}
	return store.Provision{}, false
}

// Teardown is the reverse order, settle-once (EV15): mark tearing_down, remove
// the real thing, then mark removed. Marking a row removed without removing the
// container would launder a leak into "nothing is running", which is the failure
// EV12 exists to prevent — so the removal happens between the two writes and a
// failure keeps the row visible.
// TeardownRank is the order a loop's resources come down in: the app container
// first, then the services it was talking to, then the network they all shared —
// a network with endpoints attached cannot be removed, and a service removed
// under a container that is still dialling it is a service that exits nonzero.
// The daemon's sweep sorts by this so the vocabulary lives with the driver that
// owns it.
func TeardownRank(owner string) int {
	switch owner {
	case store.ProvisionOwnerNetwork:
		return 2
	case store.ProvisionOwnerLoop:
		return 0
	default:
		return 1 // service:<name>
	}
}

// teardownRow is one row's removal, owner-aware. A network comes down with
// `network rm` and anything else with `rm`, each by the handle the row stores —
// which for both kinds is the id the engine printed when it made the resource
// (W2-1), so the row and the removal speak the engine's words and not a name of
// the driver's making. A resource the engine says is already gone settles the row
// without complaint — the ledger reading clean while nothing runs is the truth,
// not a miss.
func (p *Provisioner) teardownRow(ctx context.Context, row store.Provision) (store.Provision, *execenv.Refusal) {
	id := row.ID
	if row.State == "removed" {
		return row, nil // settle-once: a repeat is a no-op, not an error
	}
	if row.State != "tearing_down" {
		if _, sref, err := p.Ledger.MarkProvisionTearingDown(id); err != nil {
			return store.Provision{}, refuseStore(sref)
		} else if sref != nil {
			return store.Provision{}, refuseStore(sref)
		}
	}
	if row.Handle != "" {
		var err error
		if row.Owner == store.ProvisionOwnerNetwork {
			err = p.Runtime.NetworkRemove(ctx, row.Handle)
			if err != nil && goneSentence(err) {
				err = nil
			}
		} else {
			err = p.Runtime.RemoveContainer(ctx, row.Handle)
		}
		if err != nil {
			if _, _, err2 := p.Ledger.MarkProvisionFailed(id, "remove failed: "+err.Error()); err2 != nil {
				return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
					"the "+whoRow(row)+" "+row.Handle+" could not be removed ("+err.Error()+") and the ledger would not record it: "+err2.Error(),
					"the resource may still be running; the ledger must not read as clean while it is",
					"remove it with the engine directly, then run aeon env teardown again")
			}
			return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
				"the "+whoRow(row)+" "+row.Handle+" could not be removed: "+err.Error(),
				"the provision row stays visible rather than being marked removed — a removed row with a live "+
					"container is the lie EV12 refuses (EV15)",
				"fix the engine error and run teardown again; the row is still tearing_down")
		}
	}
	removed, sref, err := p.Ledger.MarkProvisionRemoved(id)
	if err != nil {
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the "+whoRow(row)+" is gone but the row would not settle: "+err.Error(),
			"the ledger is the only thing that says what is running", "re-run teardown")
	}
	if sref != nil {
		return store.Provision{}, refuseStore(sref)
	}
	return removed, nil
}

func whoRow(row store.Provision) string {
	switch row.Owner {
	case store.ProvisionOwnerNetwork:
		return "loop network"
	case store.ProvisionOwnerLoop:
		return "container"
	default:
		return "service container"
	}
}

// goneSentence recognises the engine's "it is not there" answers, which are a
// success for teardown: the thing is gone, which is what was asked.
func goneSentence(err error) bool {
	msg := err.Error()
	for _, m := range []string{"not found", "No such", "does not exist", "not exists"} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func (p *Provisioner) Teardown(ctx context.Context, id string) (store.Provision, *execenv.Refusal) {
	row, err := p.Ledger.GetProvision(id)
	if err != nil {
		return store.Provision{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
			"the provision "+id+" is not there to tear down: "+err.Error(),
			"teardown walks the ledger, and a row that does not exist cannot be marked removed",
			"check the id against aeon env provisions")
	}
	// One path for every owner, so a container and a network cannot drift apart
	// on the settle-once rule or on what "already gone" means.
	return p.teardownRow(ctx, row)
}

// networkMode maps the egress axis onto the engine. `none` is the only mode
// that constrains anything on a host backend, and on this backend it is a real
// `--network none`; `unenforced` and `unrestricted` both mean the engine's
// default bridge, which is why execenv is honest about what it cannot promise.
func networkMode(mode string) string {
	if mode == "none" {
		return "none"
	}
	return ""
}

// refuseStore turns the store's one-part refusal into the plane's three-part
// one. The store has already said what it refused and why in the sentence its
// own tests pin; what it cannot say is what to do next, and that is this
// package's business because this package is the one holding the engine.
func refuseStore(r *store.EnvironmentRefusal) *execenv.Refusal {
	if r == nil {
		return nil
	}
	return execenv.NewRefusal(execenv.RefusalProvisionFailed,
		r.Kind+" · "+r.Sentence,
		"the ledger is the authority on what is running, and it refused this transition rather than guessing "+
			"about it (EV12)",
		"read the row with aeon env provisions and repeat the step against what the ledger actually says")
}

func intDeref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func floatDeref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
