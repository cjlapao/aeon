package container

// EV8 · S59 T12's step-1 hand-over: a resolved secret value that travels through
// the container backend must land in ONE place — the engine's argv / the child's
// environment — and in nothing the plane can later read back.
//
// Why this file exists even though #5 and #6 already watch the redactor. Those
// tests prove the value is not printed by the exec path. They do not walk the
// provision **ledger**, which is the sink with a read API: a `provision` row is
// answer by `environment.provisions`, on `aeon env provision list`, and in the
// Settings → Environments panel. A secret that reaches a row's reason text is
// therefore not "leaked into a log once" — it is published to every reader of the
// loop, permanently, because the ledger is append-forever history.
//
// The path is real, not a stub: `Provisioner.Ensure` takes the already-resolved
// values as `[]Arg` from the daemon's seam (`internal/execenv/container/
// provision.go:83`) — the container package resolves nothing itself, it has no
// file door and no store — and `settle` hands them to `Runtime.Create` as
// `CreateSpec{… Env: args}` at provision.go:190. The values arrive here through
// `execenv.ResolveAll` over a real 0600 key file (`testRefs`), i.e. through the
// same door the daemon uses.
//
// Two ledgers are scanned on purpose. The fake ledger is the one this package's
// other tests use, so it cannot be accused of storing nothing; the real
// `*store.Store` is the one that answers the wire, and `envtest.Scan` walks its
// actual table columns rather than trusting a struct's `String()`.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/execenv/envtest"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// sentinels are the two values this path is allowed to carry in exactly one
// place: the value from the key file (the credential) and the literal from
// spec.env.set (the canary that a plain spec value travels the same road).
var scanSentinels = []string{sentinelSec, sentinelSet}

// createEnvOf returns the environment the fake recorded for each `create` call.
// It is the engine's own view: the fake records `cliProcessEnv(env)`, which keeps
// every arg except PATH/HOME, so a value that reached `Create` is visible here.
func createEnvOf(rt *fakeRuntime) [][]string {
	var out [][]string
	for _, o := range rt.log.all() {
		if o.Actor == "runtime" && o.What == "create" {
			out = append(out, o.Env)
		}
	}
	return out
}

// assertSentinelInArgv is the positive control, and it runs BEFORE any scan.
//
// A secret scan that reports "no findings" is worthless if the value never
// arrived: it would pass equally well against a backend that ignores its Env
// entirely, which is the mistake that let W2-1 (the short-id mismatch) survive
// this package's whole suite. So every scan in this file is conditioned on this
// assertion having proved the bytes really went in.
func assertSentinelInArgv(t *testing.T, rt *fakeRuntime, what string) {
	t.Helper()
	seen := createEnvOf(rt)
	if len(seen) == 0 {
		t.Fatalf("%s: the engine recorded no `create` call at all — the scan below would pass on an empty world, "+
			"which proves nothing about where a secret may go", what)
	}
	for _, env := range seen {
		joined := strings.Join(env, "\n")
		if strings.Contains(joined, sentinelSec) && strings.Contains(joined, sentinelSet) {
			return
		}
	}
	t.Fatalf("%s: neither the key-file value nor the spec.env.set literal reached the engine's argv — "+
		"the scan that follows is a tautology, not evidence (EV8's positive control failed; the child env was "+
		"never handed to Create)", what)
}

// TestResolvedSecretReachesOnlyTheEngineArgv walks the working path over the
// fake ledger: the value must be in the engine argv and in no row.
func TestResolvedSecretReachesOnlyTheEngineArgv(t *testing.T) {
	_, rt, lg, ex, dir := harness(t, "none", execenv.PullNever)
	ctx := context.Background()
	cmd := "echo one"
	if _, err := ex.Run(ctx, tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd)); err != nil {
		t.Fatalf("the container-bound command failed: %v", err)
	}

	assertSentinelInArgv(t, rt, "the working run")

	// The sinks this path can actually write. `Provisioner` has no logger field
	// (measured: Ledger · Runtime · User · ServiceArgs · Probe), so there is no
	// daemon log to wire here — what this path emits is its rows and its refusal
	// sentences, and those are what gets scanned rather than a buffer nobody
	// writes to, which would be a sink in name only.
	var logBuf bytes.Buffer
	resp := [][]byte{[]byte(logBuf.String())}
	for _, id := range lg.ids {
		row := lg.rows[id]
		if row == nil {
			continue
		}
		// %+v rather than selected fields: a leak through a field nobody thought
		// to name is precisely the finding this test exists to catch.
		resp = append(resp, []byte(fmt.Sprintf("%+v", *row)))
	}
	envtest.Scan(t, envtest.Sinks{LogBuf: &logBuf, Responses: resp}, scanSentinels...)
}

// TestSecretNeverEntersTheLedgerTheWireReads is the same walk over the store the
// wire actually answers from, including the failure path — the plausible leak.
//
// `settle`'s failure branch builds a `reason` sentence out of what failed and
// writes it with `MarkProvisionFailed(row.ID, reason)` (provision.go:151). That
// sentence is displayed by `environment.provisions`, by `aeon env provision
// list` and by the Settings panel, and rows are never deleted. So the interesting
// attack is not exotic: a failure path that interpolates the command's env, or
// the arg list, or the resolved value into the reason would publish the
// credential to every reader forever, with a ledger row swearing the provision
// simply failed.
func TestSecretNeverEntersTheLedgerTheWireReads(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer st.Close()

	spec := specFor("none", execenv.PullNever)
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal the spec: %v", err)
	}
	row, ref, err := st.CreateEnvironment(store.Actor{Kind: "user", ID: "t12-ev8"}, store.NewEnvironment{
		Slug: "python-worker", Name: "EV8 secret scan", Backend: "container", Spec: string(raw),
	})
	if ref != nil || err != nil {
		t.Fatalf("stage the environment the ledger points at: ref=%v err=%v", ref, err)
	}

	dir := t.TempDir()
	// The fake records its calls on a shared `*log`; without one it panics on the
	// first engine call, which is how harness wires it (rt.log = lg.log).
	rt := &fakeRuntime{log: newFakeLedger().log, imageExists: true, execOutput: "hello"}
	var logBuf bytes.Buffer
	p := &Provisioner{Ledger: st, Runtime: rt, User: "1000:1000"}
	env := execenv.BoundEnv{ID: row.ID, Slug: "python-worker", Backend: execenv.BackendContainer, Spec: spec}
	ex := &Executor{
		P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-ev8", Owner: store.ProvisionOwnerLoop},
		Env: env, Child: testChild(t, spec), WorkDir: dir,
	}

	cmd := "echo one"
	_, runErr := ex.Run(context.Background(), tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd))

	// The failure half: the engine refuses, so `settle` writes a `failed` row
	// carrying a reason sentence the wire will read back. The engine's own words
	// are deliberately unrelated to the secret — a reason that echoed the arg
	// list would put the credential in the row, and that is what the scan below
	// forbids.
	// A second LOOP, not a second command on the first one: a per-loop backend
	// reuses the live container, so another command on `loop-ev8` would never ask
	// the engine to create anything and the failure branch would never run — the
	// anti-tautology check below is what caught exactly that.
	rt.createErr = errors.New("engine: network aeon-s59-ev8-net not found")
	failEx := &Executor{
		P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-ev8-fail", Owner: store.ProvisionOwnerLoop},
		Env: env, Child: testChild(t, spec), WorkDir: dir,
	}
	_, failErr := failEx.Run(context.Background(), tools.ExecRequest{Command: "echo two", Dir: dir}, allowToken(t, "echo two"))

	assertSentinelInArgv(t, rt, "the store-backed walk")

	// Anti-tautology for the store half: the failure must really have produced a
	// `failed` row, or the table scan would be over rows that never went through
	// the failing code path.
	rows, err := st.ProvisionsFor(dir, "loop-ev8-fail")
	if err != nil {
		t.Fatalf("read the ledger back: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("the store holds no provisions for the loop whose engine call failed — the scan would pass on " +
			"an empty table and prove nothing")
	}
	failed := false
	for _, r := range rows {
		if r.State == "failed" {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("no `failed` row after the engine refused (states %v) — the failure path never ran, "+
			"so scanning the ledger proves nothing about it", states(rows))
	}

	// Everything a reader could see, in one scan: the store's own columns, the
	// sentences this path produced, and the errors the runs returned.
	resp := [][]byte{[]byte(logBuf.String())}
	if runErr != nil {
		resp = append(resp, []byte(runErr.Error()))
	}
	if failErr != nil {
		resp = append(resp, []byte(failErr.Error()))
	}
	// The credential, scanned over every sink including the store's own columns
	// and the live WAL. (The scan really reads bytes from disk: an earlier draft
	// scanned the `spec.env.set` literal here too and found it in
	// `file:state.db-wal` — the block below explains why that is by design and
	// is NOT the leak EV8 forbids.)
	envtest.Scan(t, envtest.Sinks{Store: st, LogBuf: &logBuf, Responses: resp}, sentinelSec)

	// A `spec.env.set` literal is declared configuration: it is stored in
	// environment.spec, answered by environment.show and drawn by the panel on
	// purpose, so it is not EV8's subject. The distinction is asserted rather
	// than assumed, so this file cannot be quietly read as "nothing from the spec
	// is ever stored" — and the literal is still scanned against every sink that
	// is NOT the spec row (rows, log, errors, refusals).
	if !strings.Contains(row.Spec, sentinelSet) {
		t.Errorf("the fixture no longer carries the spec literal in the stored spec (%s) — the distinction this "+
			"file draws between a declared literal and a credential is no longer being tested", row.Spec)
	}
	envtest.Scan(t, envtest.Sinks{LogBuf: &logBuf, Responses: resp}, scanSentinels...)
}

func states(rows []store.Provision) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.State)
	}
	return out
}
