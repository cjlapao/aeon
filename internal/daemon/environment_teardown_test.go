package daemon

// environment_teardown_test.go — S59 T6: EV11/EV12 at the one door that deletes
// something outside the database.
//
// The ledger row is the only thing that says a container exists. So the rule
// this file holds is narrow and absolute: the row says `removed` ONLY after the
// driver said it removed it. Everything else — a host provision with nothing to
// remove, a backend with no driver, a driver that failed — leaves the row
// speaking the truth, because a leaked container reported as gone is the exact
// failure EV12 was written against.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// fakeDriver is a non-host backend with both halves: it can run (never called
// here) and it can remove. It records the rows it was asked to remove, because
// "the ledger says removed and the driver was never called" is the bug.
type fakeDriver struct {
	// lastIdent is the most recent identity the resolver handed the backend
	// (S59 T12, F-1) — the seam once carried none, and the suite stayed green.
	lastIdent execenv.ProvisionIdent
	seen      []store.Provision
	failWith  error
}

// lastIdent is the identity the daemon handed the backend (S59 T12, F-1). The
// seam once had no such argument, so a container-bound step could never be
// created and the suite stayed green: record what comes in.
func (d *fakeDriver) Executor(_ context.Context, _ execenv.BoundEnv, _ execenv.ChildEnv, ident execenv.ProvisionIdent) (tools.CommandExecutor, *execenv.Refusal) {
	d.lastIdent = ident
	return nil, nil
}

func (d *fakeDriver) Teardown(_ context.Context, p store.Provision) error {
	d.seen = append(d.seen, p)
	return d.failWith
}

func (f *envFixture) containerEnv(t *testing.T) protocol.EnvironmentView {
	t.Helper()
	return f.create(t, "builder", "container", canonicalSpec(t, []string{"PATH"}, nil, nil, "container"))
}

// provision writes a ledger row through the store's own door, then marks it
// ready with a handle: a `ready` row with a runtime id is the only shape a
// teardown has anything to remove.
func (f *envFixture) provision(t *testing.T, envID, backend, handle string) store.Provision {
	t.Helper()
	p, ref, err := f.store.CreateProvision(store.NewProvision{
		EnvironmentID: envID, ComponentRoot: "/tmp/builder", LoopSlug: "s59-teardown", Backend: backend,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the provision: ref=%v err=%v", ref, err)
	}
	if handle == "" {
		return p
	}
	ready, ref, err := f.store.MarkProvisionReady(p.ID, handle)
	if ref != nil || err != nil {
		t.Fatalf("mark it ready: ref=%v err=%v", ref, err)
	}
	return ready
}

func TestTeardownRemovesAContainersProvisionAndWritesTheLedger(t *testing.T) {
	f := newEnvFixture(t)
	ctx := context.Background()
	env := f.containerEnv(t)
	row := f.provision(t, env.ID, "container", "docker:1f2e3d4c")
	driver := &fakeDriver{}
	f.resolver.Backends["container"] = driver

	view, err := f.env.Teardown(ctx, person, protocol.ProvisionIDParams{ProvisionID: row.ID})
	mustOK(t, "teardown", err)
	if view.State != store.ProvisionStateRemoved {
		t.Errorf("state = %q, want %q — and only because the driver said so", view.State, store.ProvisionStateRemoved)
	}
	if len(driver.seen) != 1 {
		t.Fatalf("the driver was called %d times, want exactly one removal", len(driver.seen))
	}
	if driver.seen[0].Handle != "docker:1f2e3d4c" {
		t.Errorf("the driver was handed the row without its handle: %+v — it cannot remove what it cannot name", driver.seen[0])
	}
	if view.RemovedAt == nil || *view.RemovedAt == "" {
		t.Errorf("a removed row carries no removed_at: the reconcile sweep needs it to tell removed from never-created (EV12)")
	}
	// The row outlives the container: EV12's ledger is history, not a cache.
	if _, err := f.store.GetProvision(row.ID); err != nil {
		t.Errorf("the ledger row disappeared: %v", err)
	}
	if !strings.Contains(strings.Join(f.log.lines, "\n"), row.ID) {
		t.Errorf("the teardown was not logged with the provision it removed: %v", f.log.lines)
	}
}

func TestTeardownOfAHostProvisionRefusesThereIsNothingToRemove(t *testing.T) {
	f := newEnvFixture(t)
	env := f.create(t, "plain", "host-scrubbed", canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed"))
	row := f.provision(t, env.ID, "host-scrubbed", "")

	_, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: row.ID})
	perr := refusalOf(t, err)
	if perr.Data.Kind != "provision-nothing-to-remove" {
		t.Errorf("kind = %q, want provision-nothing-to-remove — a host provision has no container, and pretending otherwise would mark a row removed to soothe a listing", perr.Data.Kind)
	}
	if !strings.Contains(perr.Data.What, "host") {
		t.Errorf("the refusal does not say which backend has nothing to remove: %q", perr.Data.What)
	}
	after, err := f.store.GetProvision(row.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	// The state the refusal left is whatever the row was on. (A host row never
	// reaches `ready` — the store demands a runtime handle for that, and a host
	// provision has none; the door's refusal keys on the BACKEND, not the
	// state, precisely so this cannot be confused with a removal.)
	if after.State != row.State {
		t.Errorf("the refused teardown moved the row from %q to %q", row.State, after.State)
	}
}

func TestTeardownOfAnUnknownProvisionRefuses(t *testing.T) {
	f := newEnvFixture(t)
	_, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: "prv-does-not-exist"})
	perr := refusalOf(t, err)
	if perr.Data.Kind != "provision-unknown" {
		t.Errorf("kind = %q, want provision-unknown", perr.Data.Kind)
	}
	if !strings.Contains(perr.Data.What, "prv-does-not-exist") {
		t.Errorf("the refusal does not name the id nobody recognised: %q", perr.Data.What)
	}
}

func TestTeardownWithoutADriverRefusesRatherThanMarkingItRemoved(t *testing.T) {
	f := newEnvFixture(t)
	env := f.containerEnv(t)
	row := f.provision(t, env.ID, "container", "docker:aaaa")

	// No driver registered: the container may well be running, and the door
	// that cannot delete it must not be allowed to say it did.
	_, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: row.ID})
	perr := refusalOf(t, err)
	if perr.Data.Kind != "backend-unregistered" {
		t.Errorf("kind = %q, want backend-unregistered (EV11: a backend that is not compiled in cannot be torn down either)", perr.Data.Kind)
	}
	after, err := f.store.GetProvision(row.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.State != store.ProvisionStateReady {
		t.Errorf("the row went to %q without anything being removed — that is EV12's lie", after.State)
	}
}

func TestTeardownFailureKeepsTheRowAndSpeaksTheDriversSentence(t *testing.T) {
	f := newEnvFixture(t)
	env := f.containerEnv(t)
	row := f.provision(t, env.ID, "container", "docker:bbbb")
	driver := &fakeDriver{failWith: errors.New("docker rm: no such container: docker:bbbb")}
	f.resolver.Backends["container"] = driver

	_, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: row.ID})
	perr := refusalOf(t, err)
	if perr.Data.Kind != "teardown-failed" {
		t.Errorf("kind = %q, want teardown-failed", perr.Data.Kind)
	}
	if !strings.Contains(perr.Data.What, "no such container") {
		t.Errorf("the driver's own sentence was paraphrased away: %q", perr.Data.What)
	}
	after, err := f.store.GetProvision(row.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.State != store.ProvisionStateFailed {
		t.Errorf("state = %q, want %q — a failed removal is a state, not a deletion of the evidence", after.State, store.ProvisionStateFailed)
	}
	if after.LastError == nil || !strings.Contains(*after.LastError, "docker rm") {
		t.Errorf("the ledger lost the driver's sentence: %+v — the next person reads this row, not the daemon's stdout", after.LastError)
	}
}

func TestTeardownIsIdempotentAndNeverAsksTheDriverTwice(t *testing.T) {
	f := newEnvFixture(t)
	env := f.containerEnv(t)
	row := f.provision(t, env.ID, "container", "docker:cccc")
	driver := &fakeDriver{}
	f.resolver.Backends["container"] = driver
	if _, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: row.ID}); err != nil {
		t.Fatalf("first teardown: %v", err)
	}
	// A second call — the panel's button pressed twice, the CLI re-run, the
	// reconcile having already swept it — answers with the row, not an error,
	// and does not touch the driver.
	view, err := f.env.Teardown(context.Background(), person, protocol.ProvisionIDParams{ProvisionID: row.ID})
	mustOK(t, "second teardown", err)
	if view.State != store.ProvisionStateRemoved {
		t.Errorf("state = %q, want the already-removed row echoed back", view.State)
	}
	if len(driver.seen) != 1 {
		t.Errorf("the driver was called %d times, want 1 — removing an absent container twice is not idempotence, it is a second guess", len(driver.seen))
	}
}

// TestProvisionsListingRefusesAnUnknownState keeps the closed set closed at the
// door: `provisions?state=whatever` is a typo, and answering it with an empty
// list would read as "nothing is running".
func TestProvisionsListingRefusesAnUnknownState(t *testing.T) {
	f := newEnvFixture(t)
	if _, err := f.env.Provisions(context.Background(), protocol.EnvironmentProvisionsParams{State: "running", Limit: 10}); err == nil {
		t.Errorf("an invented state was accepted; the closed set is %v", store.ProvisionStates())
	}
	for _, s := range store.ProvisionStates() {
		if _, err := f.env.Provisions(context.Background(), protocol.EnvironmentProvisionsParams{State: s, Limit: 10}); err != nil {
			t.Errorf("the closed state %q was refused: %v", s, err)
		}
	}
	// And the listing carries the ledger's honesty fields, not a prettified
	// subset: without last_error a failed row reads as a mystery.
	env := f.containerEnv(t)
	f.provision(t, env.ID, "container", "docker:dddd")
	rows, err := f.env.Provisions(context.Background(), protocol.EnvironmentProvisionsParams{State: store.ProvisionStateReady, Limit: 10})
	if err != nil {
		t.Fatalf("list ready provisions: %v", err)
	}
	if len(rows) != 1 || rows[0].Handle != "docker:dddd" || rows[0].Backend != "container" {
		t.Errorf("the listing lost the handle or the backend: %+v — a listing without a handle cannot be torn down", rows)
	}
}
