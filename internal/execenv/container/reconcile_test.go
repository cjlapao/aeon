package container

// The daemon-start sweep (#9's other half): the labelled list is the whole
// authority, and every branch of it is a claim about somebody else's container.

import (
	"context"
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func seeded(lg *fakeLedger, id, handle, state string) *store.Provision {
	row := &store.Provision{ID: id, EnvironmentID: "env-1", ComponentRoot: "/w", LoopSlug: "loop-a",
		Backend: "container", Handle: handle, State: state, CreatedAt: "2026-11-03T00:00:00Z"}
	lg.rows[id] = row
	lg.ids = append(lg.ids, id)
	return row
}

func labelled(handles ...string) []Handle {
	out := make([]Handle, 0, len(handles))
	for _, h := range handles {
		out = append(out, Handle{ID: h, Name: "aeon-loop-a-" + h, State: "running", Labels: map[string]string{LabelManaged: "1"}})
	}
	return out
}

func TestReconcileAdoptsFinishesAndRemovesOnlyLabelled(t *testing.T) {
	lg := newFakeLedger()
	seeded(lg, "prov-ready", "ctr-ready", store.ProvisionStateReady)        // the engine has it → adopt
	seeded(lg, "prov-gone", "ctr-gone", store.ProvisionStateReady)          // the engine lost it → fail
	seeded(lg, "prov-start", "ctr-start", store.ProvisionStateProvisioning) // engine has it → finish
	seeded(lg, "prov-orphanrow", "ctr-missing", store.ProvisionStateProvisioning)
	seeded(lg, "prov-nohandle", "", store.ProvisionStateProvisioning) // never created → fail

	rt := &fakeRuntime{log: lg.log, list: append(labelled("ctr-ready", "ctr-start"),
		Handle{ID: "ctr-foreign", Name: "someone-elses-build", State: "running", Labels: map[string]string{"com.example": "1"}})}
	rep, err := Reconcile(context.Background(), lg, rt, nil)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if rep.Adopted != 1 || rep.Finished != 1 || rep.Failed != 2 {
		t.Errorf("the sweep reported adopted=%d finished=%d failed=%d, want 1/1/2: %+v", rep.Adopted, rep.Finished, rep.Failed, rep)
	}
	// The provision whose container vanished under the engine's feet, plus the
	// row that never got a resource.
	if lg.rows["prov-gone"].State != store.ProvisionStateFailed || lg.rows["prov-nohandle"].State != store.ProvisionStateFailed {
		t.Errorf("the vanished rows did not settle to failed: %s / %s", lg.rows["prov-gone"].State, lg.rows["prov-nohandle"].State)
	}
	if lg.rows["prov-start"].State != store.ProvisionStateReady {
		t.Errorf("a provisioning row the engine still had was not finished: %s", lg.rows["prov-start"].State)
	}
	if rt.log.count("runtime", "start") != 1 {
		t.Errorf("the sweep started %d containers, expected the one it adopted mid-flight", rt.log.count("runtime", "start"))
	}
	// The unlabelled container is somebody else's, in every branch.
	for _, o := range rt.log.all() {
		if o.What == "remove" && o.Ref == "ctr-foreign" {
			t.Fatal("reconcile removed a container that does not carry aeon.managed — that is another workload's container")
		}
	}
	if rep.Unlabelled != 1 {
		t.Errorf("the sweep counted %d unlabelled containers, expected 1 (it must SEE what it refuses to touch)", rep.Unlabelled)
	}

	// An orphan: labelled, live, named by no row.
	lg2 := newFakeLedger()
	rt2 := &fakeRuntime{log: lg2.log, list: labelled("ctr-orphan")}
	rep2, err := Reconcile(context.Background(), lg2, rt2, nil)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if rep2.Removed != 1 || rt2.log.count("runtime", "remove") != 1 {
		t.Errorf("the labelled orphan survived: %+v", rep2)
	}
	// And a `provisioning` row whose resource is gone is removed-and-settled,
	// not left in flight.
	lg3 := newFakeLedger()
	seeded(lg3, "prov-dead", "ctr-dead", store.ProvisionStateProvisioning)
	rt3 := &fakeRuntime{log: lg3.log, list: nil}
	rep3, err := Reconcile(context.Background(), lg3, rt3, nil)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if rep3.Removed != 1 || lg3.rows["prov-dead"].State != store.ProvisionStateRemoved {
		t.Errorf("the half-built provision was not settled: %+v state=%s", rep3, lg3.rows["prov-dead"].State)
	}
}

// An engine that does not answer at start leaves the ledger EXACTLY as it was:
// no crash, no guess, one warning.
func TestReconcileWithRuntimeDownIsInert(t *testing.T) {
	lg := newFakeLedger()
	seeded(lg, "prov-ready", "ctr-ready", store.ProvisionStateReady)
	seeded(lg, "prov-flight", "ctr-flight", store.ProvisionStateProvisioning)
	before := map[string]string{}
	for id, r := range lg.rows {
		before[id] = r.State
	}
	rt := &fakeRuntime{log: lg.log, listErr: errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")}
	var warned []string
	rep, err := Reconcile(context.Background(), lg, rt, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatalf("an unreachable engine must not fail the daemon's start: %v", err)
	}
	if !rep.Inert {
		t.Errorf("the sweep did not say it was inert: %+v", rep)
	}
	if len(warned) != 1 {
		t.Errorf("the sweep warned %d times, expected exactly once", len(warned))
	}
	for id, want := range before {
		if lg.rows[id].State != want {
			t.Errorf("row %s moved from %s to %s while the engine never answered — that is a guess about a container nobody could ask about", id, want, lg.rows[id].State)
		}
	}
	if len(lg.writes) != 0 {
		t.Errorf("the ledger took %d writes while inert: %v", len(lg.writes), lg.writes)
	}
	if len(rep.Warnings) != 1 {
		t.Errorf("the report carried %d warnings, expected 1", len(rep.Warnings))
	}
}
