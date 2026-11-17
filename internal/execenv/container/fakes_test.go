package container

// The two fakes every test in this package reads. Their design is the point of
// the file: the runtime records the argv it was HANDED (by calling the same pure
// builders the real driver calls, so the witness is the real argv and not a
// paraphrase of it), and the ledger can be made to fail its NEXT transition,
// which is the only deterministic way to stage "the row was written and then the
// process died".
//
// Both record into a SHARED op log, so an ordering claim ("the row was written
// before the container was created") is read off one sequence rather than
// inferred from two independent ones.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/store"
)

// Op is one entry of the shared log: the actor, the operation, and everything
// the actor was given that a test might want to walk.
type Op struct {
	Actor string // "runtime" or "ledger"
	What  string // "create", "exec", "CreateProvision", "MarkProvisionReady", …
	Argv  []string
	Env   []string
	Ref   string // handle or row id, whichever the call carried
}

type log struct {
	mu  sync.Mutex
	ops []Op
}

func (l *log) add(o Op) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = append(l.ops, o)
}

func (l *log) all() []Op {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Op(nil), l.ops...)
}

func (l *log) whats(actor string) []string {
	var out []string
	for _, o := range l.all() {
		if o.Actor == actor {
			out = append(out, o.What)
		}
	}
	return out
}

func (l *log) count(actor, what string) int {
	n := 0
	for _, o := range l.all() {
		if o.Actor == actor && o.What == what {
			n++
		}
	}
	return n
}

// argvOf returns every argv the runtime was handed for one operation. A test
// that walks these is walking the same slice the real `docker` process would
// have seen in its argv vector.
func (l *log) argvOf(what string) [][]string {
	var out [][]string
	for _, o := range l.all() {
		if o.Actor == "runtime" && o.What == what && len(o.Argv) > 0 {
			out = append(out, o.Argv)
		}
	}
	return out
}

// fakeRuntime is the engine. Every method records, and every method is steerable.
type fakeRuntime struct {
	log *log

	availableErr error
	imageExists  bool
	pullErr      error
	createErr    error
	createHandle string
	startErr     error
	removeErr    error
	listErr      error
	list         []Handle

	// The network and probe knobs, same idea: the fake records the argv the real
	// builders produced and steers the outcome the test asks for. `netSeq` is the
	// mint counter behind netID — the id an engine prints, which no caller here
	// is given.
	netErr      error
	netListErr  error
	netList     []Handle
	netSeq      int
	probeErr    error
	probeOutput string

	// ctrSeq is the mint counter behind seedAlive — the container half of
	// netID's discipline (W2-10): the id an engine prints for `run -d`, which
	// no caller here is given, so a row that stores anything else cannot pass
	// by agreeing with the test.
	ctrSeq int

	// execOutput is what the prepared command prints, so the shared-boundary
	// test can put a secret's exact bytes in a child's stdout.
	execOutput string
	execCalls  int
}

func (f *fakeRuntime) Available(context.Context) error {
	f.log.add(Op{Actor: "runtime", What: "available"})
	return f.availableErr
}

func (f *fakeRuntime) ImageExists(_ context.Context, ref string) (bool, error) {
	f.log.add(Op{Actor: "runtime", What: "imageExists", Ref: ref})
	return f.imageExists, nil
}

func (f *fakeRuntime) Pull(_ context.Context, ref string) error {
	f.log.add(Op{Actor: "runtime", What: "pull", Ref: ref})
	return f.pullErr
}

// Create records the argv `runArgv` builds for this spec — the SAME function the
// real driver runs — so the flag and secret walks below are walking real argv.
func (f *fakeRuntime) Create(_ context.Context, spec CreateSpec) (string, error) {
	if spec.ServiceName != "" {
		// The same builder the real driver uses, so the recorded argv IS the
		// argv `docker` would have seen — including the absence of `-v` and `-p`.
		argv, env, ref := serviceArgv("docker", ServiceArgvSpec{
			ProvisionID: spec.ProvisionID, LoopSlug: spec.LoopSlug, Name: spec.ServiceName,
			Image: spec.Image, Network: spec.NetworkMode, User: spec.User,
			PIDsLimit: spec.PIDsLimit, MemoryMiB: spec.MemoryMiB, CPUs: spec.CPUs, Env: spec.Env,
		})
		f.log.add(Op{Actor: "runtime", What: "create-service", Argv: argv, Env: cliProcessEnv(env), Ref: spec.ServiceName})
		if ref != nil {
			return "", ref
		}
		if f.createErr != nil {
			return "", f.createErr
		}
		h := f.createHandle
		if h == "" {
			h = "svc-" + spec.ProvisionID
		}
		f.track(h, spec.LoopSlug)
		return h, nil
	}
	argv, env, ref := runArgv("docker", spec)
	envs := cliProcessEnv(env)
	f.log.add(Op{Actor: "runtime", What: "create", Argv: argv, Env: envs, Ref: f.createHandle})
	if ref != nil {
		return "", ref
	}
	if f.createErr != nil {
		return "", f.createErr
	}
	h := f.createHandle
	if h == "" {
		h = "ctr-" + spec.ProvisionID
	}
	f.track(h, spec.LoopSlug)
	return h, nil
}

// track records a container the engine now has, so reconcile's list answers
// with what the run actually created. Tests that want a particular world set
// `list` themselves; this only ever adds what a create proved happened.
func (f *fakeRuntime) track(id, loopSlug string) {
	for _, h := range f.list {
		if h.ID == id {
			return
		}
	}
	f.list = append(f.list, Handle{ID: id, Name: "aeon-" + loopSlug,
		Labels: map[string]string{LabelManaged: "1", LabelLoop: loopSlug}})
}

func (f *fakeRuntime) Start(_ context.Context, handle string) error {
	f.log.add(Op{Actor: "runtime", What: "start", Ref: handle})
	return f.startErr
}

func (f *fakeRuntime) Exec(_ context.Context, spec ExecSpec) (*exec.Cmd, error) {
	argv := execArgv("docker", spec)
	f.log.add(Op{Actor: "runtime", What: "exec", Argv: argv, Env: cliProcessEnv(spec.Env), Ref: spec.Handle})
	f.execCalls++
	cmd := exec.Command("/bin/sh", "-c", f.execOutput) // the printed bytes, not the argv, are the child's job here
	cmd.Env = cliProcessEnv(spec.Env)
	cmd.Dir = spec.Dir
	if cmd.Dir == "" {
		cmd.Dir = "/"
	}
	return cmd, nil
}

func (f *fakeRuntime) RemoveContainer(_ context.Context, handle string) error {
	f.log.add(Op{Actor: "runtime", What: "remove", Ref: handle})
	return f.removeErr
}

func (f *fakeRuntime) List(_ context.Context, label string) ([]Handle, error) {
	f.log.add(Op{Actor: "runtime", What: "list", Ref: label})
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

// netID mints the id the engine would print for `network create`: 64 hex, and
// derived from a counter the caller does not have as well as the name, so
// NOTHING the provisioner passed in predicts it. The old fake recorded
// `Handle{ID: "net-" + name, …}` — the caller's own name wearing a prefix — so a
// row that stored the name agreed with its own fake and the W2-1 mismatch stayed
// invisible. An engine that echoes its input cannot catch a row that stores the
// wrong thing; this one answers with a value only it knows.
func (f *fakeRuntime) netID(name string) string {
	f.netSeq++
	sum := sha256.Sum256([]byte(fmt.Sprintf("aeon-fake-network-%s#%d", name, f.netSeq)))
	return hex.EncodeToString(sum[:])
}

// seedAlive mints a container id the way netID mints a network id — 64-hex from
// a counter the caller does not have — AND registers the container in the
// fake's own inventory in one step. Row and inventory therefore get the value
// from the same mint, so they can only disagree if the DRIVER lost it, never
// because the test wrote one and guessed the other (W2-10 stages a loop row and
// a service row whose handles no test hand-built).
func (f *fakeRuntime) seedAlive(kind, loopSlug string) string {
	f.ctrSeq++
	sum := sha256.Sum256([]byte(fmt.Sprintf("aeon-fake-container-%s#%d", kind, f.ctrSeq)))
	id := hex.EncodeToString(sum[:])
	f.list = append(f.list, Handle{ID: id, Name: fmt.Sprintf("aeon-%s-%s-%d", sanitize(loopSlug), kind, f.ctrSeq),
		Labels: map[string]string{LabelManaged: "1", LabelLoop: sanitize(loopSlug)}})
	return id
}

// holdsID answers whether the fake's own container inventory knows a handle —
// `reusable`'s internal check restated from OUTSIDE the code under test, so a
// returned row naming a resource the engine does not have is caught by the
// test and not only by the code that is supposed to make it so.
func (f *fakeRuntime) holdsID(h string) bool {
	for _, x := range f.list {
		if x.ID == h {
			return true
		}
	}
	return false
}

func (f *fakeRuntime) NetworkCreate(_ context.Context, name, loopSlug string) (string, error) {
	f.log.add(Op{Actor: "runtime", What: "network-create", Argv: networkArgv("docker", name, loopSlug), Ref: name})
	if f.netErr != nil {
		return "", f.netErr
	}
	id := f.netID(name)
	f.netList = append(f.netList, Handle{ID: id, Name: name})
	return id, nil
}

func (f *fakeRuntime) NetworkList(_ context.Context, label string) ([]Handle, error) {
	f.log.add(Op{Actor: "runtime", What: "network-list", Argv: networkListArgv("docker", label)})
	if f.netListErr != nil {
		return nil, f.netListErr
	}
	return f.netList, nil
}

func (f *fakeRuntime) ProbeRun(_ context.Context, spec ProbeSpec) (*exec.Cmd, error) {
	argv := probeArgv("docker", spec)
	f.log.add(Op{Actor: "runtime", What: "probe", Argv: argv, Env: cliProcessEnv(nil), Ref: spec.Host})
	if f.probeErr != nil {
		return nil, f.probeErr
	}
	out := f.probeOutput
	if out == "" {
		out = "true"
	}
	cmd := exec.Command("/bin/sh", "-c", out)
	cmd.Dir = "/"
	cmd.Env = cliProcessEnv(nil) // the real driver's client env, or RunPrepared rightly refuses it
	return cmd, nil
}

// NetworkRemove takes the network out of the engine's own inventory — by the id
// the engine minted OR by its name, the way `docker network rm` resolves either
// — and says so in the engine's words when the handle names nothing it holds.
// The old version filtered a slice and returned nil whatever happened, so a
// removal that matched nothing still reported success: the exact vacuity that let
// a row store something the engine could not be asked about.
func (f *fakeRuntime) NetworkRemove(_ context.Context, handle string) error {
	f.log.add(Op{Actor: "runtime", What: "network-rm", Argv: networkRmArgv("docker", handle), Ref: handle})
	if f.removeErr != nil {
		return f.removeErr
	}
	found := -1
	for i, h := range f.netList {
		if h.ID == handle || h.Name == handle {
			found = i
			break
		}
	}
	if found < 0 {
		return fmt.Errorf("container: docker failed: exit status 1 · Error response from daemon: network %s not found", handle)
	}
	f.netList = append(f.netList[:found], f.netList[found+1:]...)
	return nil
}

// fakeLedger is the store's provision half, mirroring markProvision's legal
// from-sets and its settle-once rule, with a `failNext` that makes the NEXT
// transition of a named kind return an error — the crash, staged.
type fakeLedger struct {
	log *log

	rows     map[string]*store.Provision
	ids      []string // insertion order, so a listing is stable for the reader
	seq      int
	failNext map[string]error
	writes   []string
	// failOnCall fails the Nth write of a state (1-based): the crash matrix
	// needs "the second resource got created and then the daemon died", which
	// one armed failure cannot express.
	failOnCall map[string]int
	call       map[string]int
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{log: &log{}, rows: map[string]*store.Provision{}, failNext: map[string]error{},
		failOnCall: map[string]int{}, call: map[string]int{}}
}

func (f *fakeLedger) crash(kind string, err error) {
	if err == nil {
		err = errors.New("staged crash: the process died here")
	}
	f.failNext[kind] = err
}

// transition is the one writer, mirroring store.markProvision: the same target
// twice is a no-op, a backward move is refused with the state it found.
func (f *fakeLedger) transition(id, target, set string, args []any, from map[string]bool) (store.Provision, *store.EnvironmentRefusal, error) {
	f.writes = append(f.writes, target)
	if n, ok := f.failOnCall[target]; ok {
		f.call[target]++
		if f.call[target] == n {
			return store.Provision{}, nil, errors.New("staged crash: the process died here")
		}
	}
	if err, ok := f.failNext[target]; ok {
		delete(f.failNext, target)
		return store.Provision{}, nil, err
	}
	cur, ok := f.rows[id]
	if !ok {
		return store.Provision{}, &store.EnvironmentRefusal{Kind: "provision-unknown", Sentence: "there is no provision " + id}, nil
	}
	if cur.State == target {
		return *cur, nil, nil // settle-once, exactly as the store does
	}
	if !from[cur.State] {
		return store.Provision{}, &store.EnvironmentRefusal{Kind: "provision-transition",
			Sentence: fmt.Sprintf("%s is %s and cannot move to %s", id, cur.State, target)}, nil
	}
	cur.State = target
	switch target {
	case store.ProvisionStateReady:
		h := args[0].(string)
		cur.Handle = h
	case store.ProvisionStateFailed:
		r := args[0].(string)
		cur.LastError = &r
	case store.ProvisionStateRemoved:
		r := args[0].(string)
		cur.RemovedAt = &r // `removed_at` is what makes a row not-live, and what frees the owner's slot
	}
	return *cur, nil, nil
}

func (f *fakeLedger) CreateProvision(in store.NewProvision) (store.Provision, *store.EnvironmentRefusal, error) {
	f.log.add(Op{Actor: "ledger", What: "CreateProvision", Ref: in.LoopSlug})
	if err, ok := f.failNext["create"]; ok {
		delete(f.failNext, "create")
		return store.Provision{}, nil, err
	}
	f.seq++
	id := fmt.Sprintf("prov-%d", f.seq)
	row := &store.Provision{ID: id, EnvironmentID: in.EnvironmentID, ComponentRoot: in.ComponentRoot,
		LoopSlug: in.LoopSlug, Backend: in.Backend, Owner: in.Owner, State: store.ProvisionStateProvisioning,
		CreatedAt: "2026-11-03T00:00:00Z"}
	f.rows[id] = row
	f.ids = append(f.ids, id)
	return *row, nil, nil
}

func (f *fakeLedger) MarkProvisionReady(id, handle string) (store.Provision, *store.EnvironmentRefusal, error) {
	f.log.add(Op{Actor: "ledger", What: "MarkProvisionReady", Ref: id})
	return f.transition(id, store.ProvisionStateReady, "handle", []any{handle},
		map[string]bool{store.ProvisionStateProvisioning: true})
}

func (f *fakeLedger) MarkProvisionTearingDown(id string) (store.Provision, *store.EnvironmentRefusal, error) {
	f.log.add(Op{Actor: "ledger", What: "MarkProvisionTearingDown", Ref: id})
	return f.transition(id, store.ProvisionStateTearingDown, "", nil,
		map[string]bool{store.ProvisionStateProvisioning: true, store.ProvisionStateReady: true, store.ProvisionStateFailed: true})
}

func (f *fakeLedger) MarkProvisionRemoved(id string) (store.Provision, *store.EnvironmentRefusal, error) {
	f.log.add(Op{Actor: "ledger", What: "MarkProvisionRemoved", Ref: id})
	// The from-set is the real store's, copied: a half-built provision a crash
	// left in `provisioning` is exactly the row reconcile must be able to settle.
	return f.transition(id, store.ProvisionStateRemoved, "removed_at", []any{"2026-11-03T00:00:00Z"},
		map[string]bool{store.ProvisionStateProvisioning: true, store.ProvisionStateReady: true,
			store.ProvisionStateTearingDown: true, store.ProvisionStateFailed: true})
}

func (f *fakeLedger) MarkProvisionFailed(id, reason string) (store.Provision, *store.EnvironmentRefusal, error) {
	f.log.add(Op{Actor: "ledger", What: "MarkProvisionFailed", Ref: id})
	if strings.TrimSpace(reason) == "" {
		return store.Provision{}, &store.EnvironmentRefusal{Kind: "provision-failure-reason", Sentence: "a failed row must say why"}, nil
	}
	return f.transition(id, store.ProvisionStateFailed, "last_error", []any{reason},
		map[string]bool{store.ProvisionStateProvisioning: true, store.ProvisionStateReady: true, store.ProvisionStateTearingDown: true})
}

func (f *fakeLedger) GetProvision(id string) (store.Provision, error) {
	r, ok := f.rows[id]
	if !ok {
		return store.Provision{}, fmt.Errorf("no provision %s", id)
	}
	return *r, nil
}

func (f *fakeLedger) ListLiveProvisions() ([]store.Provision, error) {
	var out []store.Provision
	for _, id := range f.ids {
		if r, ok := f.rows[id]; ok && r.Live() {
			out = append(out, *r)
		}
	}
	return out, nil
}

// Newest first over the insertion-ordered ids — the answer `ProvisionsFor`
// gives in the store (`ORDER BY id DESC`, store/provisions.go), restated here
// so a test that stages two rows under one (root, slug) meets the SAME
// candidate order the real ledger answers. Walking the `rows` map made that
// order random per run, and the row that comes first is the row `reusable()`
// takes — the only difference between a deterministic reproduction of W2-10
// and a flake.
func (f *fakeLedger) ProvisionsFor(componentRoot, loopSlug string) ([]store.Provision, error) {
	var out []store.Provision
	for i := len(f.ids) - 1; i >= 0; i-- {
		r, ok := f.rows[f.ids[i]]
		if !ok {
			continue
		}
		if loopSlug != "" && r.LoopSlug != loopSlug {
			continue
		}
		if componentRoot != "" && r.ComponentRoot != componentRoot {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}
