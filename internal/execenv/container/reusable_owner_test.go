package container

// W2-10 — found by S59 T12's walk and recorded unfixed in
// docs/aeon/S59-execution-environment/tasks/T12.md ("found, not fixed, and it
// is the sharpest thing this walk surfaced"). Pinned here now.
//
// `reusable()` chooses the container to hand to a STEP from
// `ProvisionsFor(componentRoot, loopSlug)` — a read keyed on LESS than what
// identifies the thing. A service provision row carries the SAME component root
// and loop slug and differs only in `Owner` ("service:<name>", written by
// ensureRow, against the loop's own store.ProvisionOwnerLoop, written by
// newProvision — internal/store/provisions.go owns the vocabulary). Since W2-5
// gave service rows a real engine container handle, a loop's own command can be
// matched to its SERVICE's container and executed inside the database
// container, with that service's mounts and network.
//
// The handles are minted INSIDE the fake (the `netID()` discipline, W2-1 and
// W2-5): the test never writes a container id, so a row that stores something
// other than what the engine's `List` answers cannot pass by agreeing with the
// test — and the row the step is handed must be provably the loop's by owner,
// by id, and by the handle its `docker exec` names.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	w210LoopRow = "prov-loop-1"
	w210SvcRow  = "prov-svc-1"
)

// w210SeedRow stages one READY row carrying a handle the engine minted,
// alongside what the real CreateProvision would have written for it.
func w210SeedRow(lg *fakeLedger, id, owner, handle, root string) {
	row := &store.Provision{ID: id, EnvironmentID: "env-1", ComponentRoot: root, LoopSlug: "loop-a",
		Backend: execenv.BackendContainer, Owner: owner, Handle: handle,
		State: store.ProvisionStateReady, CreatedAt: "2026-11-03T00:00:00Z"}
	lg.rows[id] = row
	lg.ids = append(lg.ids, id)
}

// w210Harness is one loop whose ledger holds BOTH rows for the SAME
// (componentRoot, loopSlug): the loop's own container row and a `db` SERVICE
// row — same environment id, same backend, same root, same slug — differing
// only in Owner, both `ready`, both carrying a minted handle, and the engine
// reporting BOTH alive. The service row is seeded LAST because that is the
// order a real Ensure writes them in (the loop's row first, dependencies
// after), and the store's read answers newest first.
func w210Harness(t *testing.T) (*Executor, *fakeRuntime, *fakeLedger, string, string) {
	t.Helper()
	dir := rootDir(t)
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true, execOutput: "hello"}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	ex := &Executor{
		P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-a", Owner: "step-1"},
		Env: testEnv("none", execenv.PullNever), Child: testChild(t, specFor("none", execenv.PullNever)),
		WorkDir: dir,
	}
	loopHandle := rt.seedAlive("app", "loop-a")
	svcHandle := rt.seedAlive("db", "loop-a")
	w210SeedRow(lg, w210LoopRow, store.ProvisionOwnerLoop, loopHandle, dir)
	w210SeedRow(lg, w210SvcRow, "service:db", svcHandle, dir)
	if loopHandle == svcHandle || loopHandle == "" || svcHandle == "" {
		t.Fatalf("the engine minted indistinct handles (%q / %q); rows a test cannot tell apart by handle prove nothing", loopHandle, svcHandle)
	}
	return ex, rt, lg, loopHandle, svcHandle
}

// The defect, reproduced: with both rows live and listed, the step must be
// handed the LOOP's row and its exec must name the LOOP's handle. Before the
// owner filter this fails — `reusable()` matched the service row (newest
// first, exactly what the real ledger answers) and the step ran inside the
// database container.
func TestStepNeverReusesTheServicesContainerRow(t *testing.T) {
	ex, rt, _, loopHandle, svcHandle := w210Harness(t)
	if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: "echo w2-10", Dir: ex.WorkDir},
		allowToken(t, "echo w2-10")); err != nil {
		t.Fatalf("the step failed before it could be judged: %v", err)
	}

	got := ex.Provision()
	if got.Owner != store.ProvisionOwnerLoop {
		t.Fatalf("W2-10: the step was handed the SERVICE's container — reusable() returned provision %s, owner %q, handle %s, so the loop's own command ran inside the database container with that service's mounts and network — instead of the loop's own row %s (handle %s)",
			got.ID, got.Owner, got.Handle, w210LoopRow, loopHandle)
	}
	if got.ID != w210LoopRow {
		t.Fatalf("the step was handed row %s, not the loop's own %s (W2-10)", got.ID, w210LoopRow)
	}

	// The argv the engine was handed names the loop's handle and never the
	// service's — the step ran where the ledger says the loop lives.
	execs := rt.log.argvOf("exec")
	if len(execs) != 1 {
		t.Fatalf("the engine was handed %d exec argvs, want exactly 1 — nothing was run, so the handle claim is empty", len(execs))
	}
	if h := execHandle(execs[0]); h != loopHandle {
		t.Errorf("the step's `docker exec` named container %q, want the loop's %s (row %s):\n%s", h, loopHandle, w210LoopRow, strings.Join(execs[0], " "))
	}
	if strings.Contains(strings.Join(execs[0], " "), svcHandle) {
		t.Errorf("the step's exec argv names the SERVICE's container %s — the command ran inside the database container (W2-10):\n%s", svcHandle, strings.Join(execs[0], " "))
	}

	// Anti-vacuity: the engine call happened. `reusable` asks the engine which
	// resources it still has; a reuse that never asked is an assertion over an
	// empty world.
	if n := rt.log.count("runtime", "list"); n == 0 {
		t.Fatalf("`reusable()` returned a row without asking the engine what it has — a row naming a dead or foreign resource must never be reusable")
	}
	// The engine AGREES: the handle the returned row names appears in the
	// inventory `reusable` fetched, so the row cannot name a dead or foreign
	// resource and still be handed to a step.
	if !rt.holdsID(got.Handle) {
		t.Errorf("the reused row %s names handle %s, which the engine does not list — a step was handed a container the engine has never heard of (W2-10)", got.ID, got.Handle)
	}
}

// The anti-vacuity pair: an EMPTY engine inventory must not let the service row
// win either. Nothing is alive, so nothing is reusable — not the loop's stale
// row, and emphatically not the service's; a fresh loop-owned provision is
// created and the engine is asked again (the deny-leaning safe answer). If this
// passed while `List` was never called, the reuse the test above pins could be
// passing on a skipped read.
func TestEmptyInventoryReusesNothingNotEvenTheServiceRow(t *testing.T) {
	ex, rt, lg, _, svcHandle := w210Harness(t)
	rt.list = nil // the engine answers with NOTHING
	if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: "echo w2-10", Dir: ex.WorkDir},
		allowToken(t, "echo w2-10")); err != nil {
		t.Fatalf("the step failed before it could be judged: %v", err)
	}
	if n := rt.log.count("runtime", "list"); n == 0 {
		t.Fatalf("the empty inventory proved nothing — `List` was never called; the test would pass on a skipped read")
	}

	got := ex.Provision()
	if got.Owner != store.ProvisionOwnerLoop {
		t.Fatalf("W2-10: an empty engine inventory let the step be handed provision %s, owner %q, handle %s — a dead service row is still a service row (row %s must never reach a loop's step)",
			got.ID, got.Owner, got.Handle, w210SvcRow)
	}
	if got.ID == w210LoopRow || got.ID == w210SvcRow {
		t.Errorf("an empty inventory reused staged row %s; a row whose resource the engine does not have gets a FRESH provision, and the engine is asked again (W2-10)", got.ID)
	}
	if got.Handle == svcHandle || got.Handle == "" {
		t.Errorf("the fresh provision carries handle %q — it must be a new one the engine just minted, not a staged row's and never empty", got.Handle)
	}
	if n := lg.log.count("ledger", "CreateProvision"); n != 1 {
		t.Fatalf("the empty inventory produced %d new rows, want exactly 1 — the safe answer to a resource the engine cannot confirm is a fresh row written FIRST (EV12)", n)
	}
	execs := rt.log.argvOf("exec")
	if len(execs) != 1 {
		t.Fatalf("the engine was handed %d exec argvs, want 1 — the fresh container never ran the step", len(execs))
	}
	if h := execHandle(execs[0]); h != got.Handle {
		t.Errorf("the step exec named %q, not the fresh provision's handle %q", h, got.Handle)
	}
}

// The deny-leaning half, tested rather than only commented: a row whose owner
// is empty or unrecognised names no owner a reader can trust, so it is NOT
// reusable — the safe answer is a fresh loop-owned row, written first, and the
// engine asked again. (The real store would default an empty owner to "loop"
// at the write; a hand-built or drifted row is exactly the state `reusable`
// must refuse rather than believe.)
func TestUnrecognisedOwnerIsNeverReused(t *testing.T) {
	dir := rootDir(t)
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true, execOutput: "hello"}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	ex := &Executor{
		P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-a", Owner: "step-1"},
		Env: testEnv("none", execenv.PullNever), Child: testChild(t, specFor("none", execenv.PullNever)),
		WorkDir: dir,
	}
	orphanHandle := rt.seedAlive("unowned", "loop-a")
	w210SeedRow(lg, "prov-orphan-1", "", orphanHandle, dir) // ready, listed, owner nobody can name
	if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: "echo w2-10", Dir: ex.WorkDir},
		allowToken(t, "echo w2-10")); err != nil {
		t.Fatalf("the step failed before it could be judged: %v", err)
	}
	if n := rt.log.count("runtime", "list"); n == 0 {
		t.Fatalf("the engine was never asked what it has, so the refusal-to-reuse proves nothing")
	}
	got := ex.Provision()
	if got.Owner != store.ProvisionOwnerLoop || got.ID == "prov-orphan-1" || got.Handle == orphanHandle {
		t.Fatalf("an ownerless ready row was reused (%#v) — an owner nobody can name is no claim on the loop's step (W2-10)", got)
	}
	if n := lg.log.count("ledger", "CreateProvision"); n != 1 {
		t.Fatalf("%d new rows for the ownerless world, want exactly 1 (EV12: the row is written FIRST)", n)
	}
}
