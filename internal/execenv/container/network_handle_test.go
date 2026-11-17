package container

// W2-1's network half (S59 T12) — **a row must store something the engine can be
// asked about again**: the principle W2-1 pins for containers and W2-5 for
// services, now applied to the network a loop owns.
//
// The decision, and why it is the engine's id and not the name. The row stored
// the network NAME — a string the driver invented from the slug (`NetworkName`) —
// while the read that makes a network findable after a crash, `networkListArgv`,
// answers `docker network ls --no-trunc --filter label=… --format
// '{{.ID}}|{{.Name}}'`, whose first field is the 64-hex id. Measured on the
// engine this lane has (rootless docker 29.8.0):
//
//	docker network create --label aeon.managed=1 --label aeon.loop=x aeon-w21-probe-net
//	  → stdout `9c45c297fe4a7977…af5a972c`, exit 0 — the FULL 64-hex
//	docker network ls --no-trunc --format '{{.ID}}|{{.Name}}' → the same 64-hex
//	docker network ls          --format '{{.ID}}|{{.Name}}' → the 12-char short id
//	docker network rm <64-hex> → exit 0, the id printed back; again → exit 1 with
//	  `Error response from daemon: network … not found` on stderr
//
// So the id is the one handle the create PRINTS, the read ANSWERS and the removal
// ACCEPTS; the name is a label of the driver's own making, which the engine
// tolerates as a convenience it never promised. A row is a receipt, and the
// receipt should carry what the engine handed back — the same reason a container
// row stores what `run -d` printed and not the provision id (W2-5).
//
// The honest half of that, recorded rather than glossed: before this change every
// network comparison happened to key by NAME on both sides (`reusableOwner`'s
// `live[h.Name]`, the sweep's `liveNet[n.Name]`), so nothing broke in a test. What
// was broken was the claim the read exists to keep — that a row names its resource
// by something the engine issued — and one real defect the name-keying caused for
// sure: the start sweep's row loop asks the CONTAINER list about every live row,
// so a `ready` network row was reported as `the container aeon-loop-a-net is gone`
// and marked failed, where a failed network row still counts as live and refuses
// every later step on that loop (W2-3's shape). Asking the engine about the
// network in the engine's own words — by the id, from `network ls` — is what
// settles it.
//
// The fakes are part of this fix, not a convenience. The old `fakes_test.go`
// recorded `Handle{ID: "net-" + name}` — the caller's own name wearing a prefix —
// and its `NetworkRemove` filtered a slice and returned nil whatever it matched,
// so the fake echoed its input and could not see a row storing something the
// engine cannot be asked about. That is exactly how the container half of this
// family survived a green package. The fake now mints the id from a counter no
// caller has (`netID`), answers `network ls` at both widths, and refuses to report
// the removal of a network it does not hold.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
)

// networkDepHarness is one loop whose environment declares a single service, so
// the dependency half — the network the loop owns — really runs through the door
// the daemon walks. The service has no health check on purpose: this file's claim
// is about the NETWORK row, and a probe would only add a second engine call to
// read.
func networkDepHarness(t *testing.T) (*Provisioner, *fakeRuntime, *fakeLedger, execenv.BoundEnv, LoopRef, string) {
	t.Helper()
	p, _, lg, ex, dir := harness(t, "none", execenv.PullNever)
	env := ex.Env
	env.Spec.Services = []execenv.Service{{Name: "db", Image: svcImage, Port: 5432}}
	rt, ok := p.Runtime.(*fakeRuntime)
	if !ok {
		t.Fatalf("the harness wired a runtime of type %T; this file reads the fake's recorded argv", p.Runtime)
	}
	return p, rt, lg, env, ex.Loop, dir
}

// networkRow returns the loop's network row, refusing to let a test that silently
// reads the wrong owner pass on it.
func networkRow(t *testing.T, lg *fakeLedger) store.Provision {
	t.Helper()
	rows, err := lg.ListLiveProvisions()
	if err != nil {
		t.Fatalf("the ledger would not answer: %v", err)
	}
	for _, r := range rows {
		if r.Owner == store.ProvisionOwnerNetwork {
			return r
		}
	}
	t.Fatalf("no network row exists; the ledger is written BEFORE the resource, and a network with no row is a network reconcile cannot find (EV12) — live rows: %v", lg.ids)
	return store.Provision{}
}

// seedNetRow is reconcile_test.go's `seeded` for the one owner this file is about.
func seedNetRow(t *testing.T, lg *fakeLedger, id, handle string) *store.Provision {
	t.Helper()
	row := &store.Provision{ID: id, EnvironmentID: "env-1", ComponentRoot: "/w", LoopSlug: "loop-a",
		Backend: "container", Owner: store.ProvisionOwnerNetwork, Handle: handle,
		State: store.ProvisionStateReady, CreatedAt: "2026-11-03T00:00:00Z"}
	lg.rows[id] = row
	lg.ids = append(lg.ids, id)
	return row
}

// The decision, pinned: the row stores the id the FAKE MINTED — a value no caller
// in this package supplied, so the test cannot be agreeing with itself.
func TestNetworkRowStoresTheIDTheEngineMinted(t *testing.T) {
	p, rt, lg, env, loop, dir := networkDepHarness(t)
	ctx := context.Background()
	if _, _, ref := p.ensureDependencies(ctx, loop, env, dir); ref != nil {
		t.Fatalf("the loop's network never came up through this door: %s", ref.Sentence())
	}

	// Anti-vacuity, first: the create really reached the engine, and what it was
	// handed is the real builder's argv — which still names the network, because
	// the CREATE needs a name even though the ROW stores the id.
	created := rt.log.argvOf("network-create")
	if len(created) != 1 {
		t.Fatalf("the engine was handed %d network creates, want exactly 1 — a network that was never created proves nothing", len(created))
	}
	if want := strings.Join(networkArgv("docker", NetworkName("loop-a"), "loop-a"), " "); strings.Join(created[0], " ") != want {
		t.Errorf("the network create argv drifted\n got: %s\nwant: %s", strings.Join(created[0], " "), want)
	}
	if len(rt.netList) != 1 {
		t.Fatalf("the engine holds %d networks after one loop's dependencies; this test reads the one it should hold", len(rt.netList))
	}
	minted := rt.netList[0].ID // minted inside the fake from a counter the caller does not have
	if minted == NetworkName("loop-a") || strings.HasPrefix(minted, "net-") {
		t.Fatalf("the fake echoed what the caller handed it (%q) — an echoing fake is how the container half of W2-1 survived", minted)
	}

	row := networkRow(t, lg)
	if row.Handle == "" {
		t.Fatal("the network row stores no handle at all; a row that cannot name its resource cannot have it removed")
	}
	if row.Handle != minted {
		t.Errorf("the network row stores handle %q but the engine knows that network only as %q — the handle must be what the engine reported, or no read and no removal can speak it again (W2-1)", row.Handle, minted)
	}
	if row.Handle == NetworkName("loop-a") {
		t.Errorf("the row stores the name the driver invented from the slug (%q); the read answers the engine's id, so the daemon's own network is nothing to it (W2-1)", row.Handle)
	}
	if len(row.Handle) != 64 {
		t.Errorf("the row stores a %d-character handle (%s); `network create` prints the full 64-hex and the read answers that width, measured on docker 29.8.0", len(row.Handle), row.Handle)
	}

	// The whole claim in one line: the read must answer the stored handle.
	hs, err := rt.NetworkList(ctx, LabelManaged+"=1")
	if err != nil {
		t.Fatalf("the network read failed: %v", err)
	}
	var seen []string
	for _, h := range hs {
		if h.ID == row.Handle {
			return
		}
		seen = append(seen, h.ID+"|"+h.Name)
	}
	t.Errorf("NetworkList answered %q and none of it is the stored handle %s — the loop's own network is an orphan to the sweep (W2-1)",
		strings.Join(seen, ", "), row.Handle)
}

// The pairing the decision forces: once the row stores the id, every network
// comparison must key by the id the read answers. This one is decided by the
// engine, so a half-fix (row moved, read still keyed by name) shows up here as a
// second network per run.
func TestOneLoopKeepsOneNetworkAcrossRuns(t *testing.T) {
	p, rt, _, env, loop, dir := networkDepHarness(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, _, ref := p.ensureDependencies(ctx, loop, env, dir); ref != nil {
			t.Fatalf("run %d of the loop's dependencies failed: %s", i+1, ref.Sentence())
		}
	}
	if n := rt.log.count("runtime", "network-list"); n == 0 {
		t.Fatal("the network read never ran, so the reuse below was never decided by the engine — it was assumed")
	}
	if n := rt.log.count("runtime", "network-create"); n != 1 {
		t.Errorf("two runs on one loop created %d networks; a stored handle that the read does not answer makes every step create another (W2-1)", n)
	}
	if n := len(rt.netList); n != 1 {
		t.Errorf("the engine holds %d networks for one loop, want 1", n)
	}
	lists := rt.log.argvOf("network-list")
	if len(lists) == 0 {
		t.Fatal("no network-list argv was recorded, so the read is not the one under test")
	}
	if want := strings.Join(networkListArgv("docker", LabelManaged+"=1"), " "); strings.Join(lists[0], " ") != want {
		t.Errorf("the reuse read is not the pinned read\n got: %s\nwant: %s", strings.Join(lists[0], " "), want)
	}
}

// The defect the name-keying caused for sure: the start sweep's row loop asks the
// CONTAINER list about every live row, so a ready network row was read as a gone
// container, and the network side then compared the row's handle against the
// read's NAME column — so the loop's own network was both failed in the ledger and
// an orphan to the sweep.
func TestStartSweepKeepsTheLoopsOwnNetwork(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log}
	ctx := context.Background()
	if _, err := rt.NetworkCreate(ctx, NetworkName("loop-a"), "loop-a"); err != nil {
		t.Fatalf("the engine refused the loop's network: %v", err)
	}
	minted := rt.netList[0].ID
	seedNetRow(t, lg, "prov-net", minted)

	var warned []string
	rep, err := Reconcile(ctx, lg, rt, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if rep.Inert {
		t.Fatalf("the sweep called itself inert against an engine that answers: %+v", rep)
	}
	if n := len(rt.netList); n != 1 {
		t.Errorf("the sweep removed the loop's own network (%d left, removed=%d): the read answers ids and the row stores one, so the daemon's resource is an orphan to it — that is EV12 reversed (W2-1)", n, rep.Removed)
	}
	if n := rt.log.count("runtime", "network-rm"); n != 0 {
		t.Errorf("the sweep asked the engine to remove %d network(s) that no row disowns; the loop's own network is not an orphan", n)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep reported %d removals with one ready row and the engine holding exactly that resource: %+v", rep.Removed, rep)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows, want 1 — a row and the engine agreeing is Adoption, said out loud: %+v", rep.Adopted, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows: %+v · warnings %v — a `ready` network row is not a gone container, and a failed network row still counts as live and refuses every later step on the loop (W2-3)", rep.Failed, rep, warned)
	}
	row, err := lg.GetProvision("prov-net")
	if err != nil {
		t.Fatalf("the row could not be read back: %v", err)
	}
	if row.State != store.ProvisionStateReady {
		t.Errorf("the network row is %s after a sweep that should have adopted it; the ledger must say what the engine says", row.State)
	}
}

// An orphan network — labelled, held by the engine, named by no row — comes down,
// and comes down by the handle the read itself answered.
func TestStartSweepRemovesAnOrphanNetworkByTheAnsweredHandle(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log}
	ctx := context.Background()
	if _, err := rt.NetworkCreate(ctx, NetworkName("loop-gone"), "loop-gone"); err != nil {
		t.Fatalf("the engine refused the network: %v", err)
	}
	minted := rt.netList[0].ID

	rep, err := Reconcile(ctx, lg, rt, nil)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if rep.Removed != 1 {
		t.Errorf("the labelled orphan survived: %+v", rep)
	}
	rms := rt.log.argvOf("network-rm")
	if len(rms) != 1 {
		t.Fatalf("the sweep recorded %d `network rm` commands, want 1", len(rms))
	}
	if want := strings.Join(networkRmArgv("docker", minted), " "); strings.Join(rms[0], " ") != want {
		t.Errorf("the orphan came down by something other than the handle the read answered\n got: %s\nwant: %s", strings.Join(rms[0], " "), want)
	}
	if n := len(rt.netList); n != 0 {
		t.Errorf("%d networks left after a sweep that reported removing the orphan", n)
	}
	// The removal is the engine's, not the fake's courtesy: the same handle asked
	// for again is the engine's own not-found.
	if err := rt.NetworkRemove(ctx, minted); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a second removal of the orphan answered %v; the engine no longer holds it, and a fake that keeps saying yes cannot prove a removal happened", err)
	}
}

// A `failed` network row the engine still holds comes down too, by the handle the
// row stores: `teardownRow` already removes a network as a network, and a start
// sweep that cannot is a sweep that leaves a loop's network running behind a row
// nobody will ever retry (EV12's "nothing left behind").
func TestStartSweepTakesDownAFailedNetworkRow(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log}
	ctx := context.Background()
	if _, err := rt.NetworkCreate(ctx, NetworkName("loop-a"), "loop-a"); err != nil {
		t.Fatalf("the engine refused the network: %v", err)
	}
	minted := rt.netList[0].ID
	row := seedNetRow(t, lg, "prov-net", minted)
	row.State = store.ProvisionStateFailed

	rep, err := Reconcile(ctx, lg, rt, nil)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if rep.Removed != 1 {
		t.Errorf("the sweep left the failed row's network running (removed=%d skipped=%d): %+v — a failed provision still promises nothing left behind", rep.Removed, rep.Skipped, rep)
	}
	if n := len(rt.netList); n != 0 {
		t.Errorf("%d networks the engine holds with no row claiming them, after a sweep that had the handle to take them down", n)
	}
	if rms := rt.log.argvOf("network-rm"); len(rms) != 1 || strings.Join(rms[0], " ") != strings.Join(networkRmArgv("docker", minted), " ") {
		t.Errorf("the failed row's network was not removed by its stored handle; recorded removals: %v", rms)
	}
	if r, err := lg.GetProvision("prov-net"); err != nil || r.State != store.ProvisionStateRemoved {
		t.Errorf("the row reads %v after its resource came down; the ledger must say what the engine says", r.State)
	}
}

// Teardown removes by the stored handle, in the argv the real builder produced,
// and the engine really lets go.
func TestTeardownRemovesTheNetworkByTheHandleTheEngineAnswered(t *testing.T) {
	p, rt, lg, env, loop, dir := networkDepHarness(t)
	ctx := context.Background()
	if _, _, ref := p.ensureDependencies(ctx, loop, env, dir); ref != nil {
		t.Fatalf("the loop's dependencies never came up: %s", ref.Sentence())
	}
	row := networkRow(t, lg)
	if _, ref := p.Teardown(ctx, row.ID); ref != nil {
		t.Fatalf("teardown of the loop's network failed: %s", ref.Sentence())
	}
	rms := rt.log.argvOf("network-rm")
	if len(rms) == 0 {
		t.Fatal("the network came down with no `network rm` ever reaching the engine; a row marked removed without a removal is the lie EV12 refuses")
	}
	if want := strings.Join(networkRmArgv("docker", row.Handle), " "); strings.Join(rms[len(rms)-1], " ") != want {
		t.Errorf("the removal named something other than the stored handle\n got: %s\nwant: %s", strings.Join(rms[len(rms)-1], " "), want)
	}
	if n := len(rt.netList); n != 0 {
		t.Errorf("%d networks the engine still holds behind a teardown that reported success (EV12)", n)
	}
	if err := rt.NetworkRemove(ctx, row.Handle); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("the stored handle answered %v on a second removal; the first one did not really take the network down", err)
	}
	if _, ref := p.Teardown(ctx, row.ID); ref != nil {
		t.Errorf("the repeat teardown was not inert: %s", ref.Sentence())
	}
}

// The anti-vacuity pair. (a) The fake is no longer a yes-man: a handle it never
// issued is not something it reports as removed — an echoing fake is the whole
// reason this defect family survived a green suite. (b) The product half: an
// engine that refuses with a sentence that is NOT a gone sentence leaves the row
// visible, because a removed row over a live network is the answer EV12 exists to
// refuse.
func TestANetworkTheRowCannotNameIsNotReportedRemoved(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log}
	ctx := context.Background()
	if _, err := rt.NetworkCreate(ctx, NetworkName("loop-a"), "loop-a"); err != nil {
		t.Fatalf("the engine refused the loop's network: %v", err)
	}
	minted := rt.netList[0].ID

	unknown := strings.Repeat("0f2a", 16) // 64 hex the engine was never asked to make
	if err := rt.NetworkRemove(ctx, unknown); err == nil {
		t.Errorf("the fake reported the removal of a network it has never held (%s); a fake that always says yes turns every removal assertion below into a tautology", unknown)
	}
	if n := len(rt.netList); n != 1 {
		t.Errorf("a refused removal still changed the engine's inventory: %d networks left", n)
	}

	rt.removeErr = errors.New("container: docker failed: exit status 1 · Error response from daemon: network " +
		minted + " is being used by container 1a2b3c4d5e6f")
	seedNetRow(t, lg, "prov-net", minted)
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	_, ref := p.Teardown(ctx, "prov-net")
	if ref == nil {
		t.Fatal("teardown reported success over a network the engine refused to release (EV15)")
	}
	row, err := lg.GetProvision("prov-net")
	if err != nil {
		t.Fatalf("the row could not be read back: %v", err)
	}
	if row.State == store.ProvisionStateRemoved {
		t.Errorf("the row settled to `removed` while the engine still holds the network — the ledger reading clean while something runs is the lie EV12 refuses")
	}
	if n := len(rt.netList); n != 1 {
		t.Errorf("%d networks left after a refusal that should have changed nothing", n)
	}
}
