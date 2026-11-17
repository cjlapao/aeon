package container

// The daemon-start sweep's NETWORK half (S59 T12 — W2-1's last member, the one
// `network_handle_test.go` could not reach).
//
// That file proved a row must STORE something the engine can be asked about
// again. This file proves the two promises that make the storing worth
// anything:
//
//   1. a network row is asked about in the NETWORK inventory — `network ls`,
//      keyed by the id it answers — and never in the container list, because
//      `docker ps` cannot answer for a network and a network answered "gone" is
//      a failed row that still counts as live and refuses every later step on
//      the loop (W2-3's second half);
//   2. the sweep never removes in its orphan pass what its own row walk has
//      just acquired. Containers have always been immune to that, because
//      `liveIDs` claims EVERY live row whatever its state; the network side
//      claimed only `ready` rows, so a `provisioning` network row the same sweep
//      finishes into `ready` was orphan-removed one line later — the ledger left
//      carrying a ready row pointing at nothing, which is the exact lie EV12
//      exists to refuse. `TestSweepKeepsTheNetworkItJustFinished` is that case.
//
// The world below mints every network and container id INSIDE the fake
// (`netID`/`seedAlive`: sha256 over a counter the caller does not have), so a
// row can only agree with its inventory by the product keeping the handle, and
// one shared op log carries the whole ordering story — staging is separated
// from the sweep by `begin()`, so an argv counted below is an argv the SWEEP
// issued and not one the world was staged with.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// sweepWorld is one engine and one ledger over one shared op log.
type sweepWorld struct {
	lg   *fakeLedger
	rt   *fakeRuntime
	ops  *log // the shared log both actors write into — the ordering oracle
	mark int  // everything at or after this index was done by the sweep, not by staging
}

func newSweepWorld(t *testing.T) *sweepWorld {
	t.Helper()
	lg := newFakeLedger()
	return &sweepWorld{lg: lg, rt: &fakeRuntime{log: lg.log}, ops: lg.log}
}

// begin raises the mark: staging is over, and from here every recorded op
// belongs to the sweep. Without it, `network rm` counted below would include
// the removals the world was staged with (an engine that lost a resource under
// the daemon is staged by REMOVING it, which is exactly such an op).
func (w *sweepWorld) begin() {
	w.mark = len(w.ops.all())
}

// post returns the ops the sweep itself produced.
func (w *sweepWorld) post() []Op {
	all := w.ops.all()
	if w.mark > len(all) {
		return nil
	}
	return all[w.mark:]
}

// argvs is every argv the engine was handed for one operation, sweep-only.
func (w *sweepWorld) argvs(what string) [][]string {
	var out [][]string
	for _, o := range w.post() {
		if o.Actor == "runtime" && o.What == what && len(o.Argv) > 0 {
			out = append(out, o.Argv)
		}
	}
	return out
}

// refs is every handle/id one operation was handed, sweep-only — the argument
// a removal spoke, which is what the W2-1 comparisons are really about.
func (w *sweepWorld) refs(what string) []string {
	var out []string
	for _, o := range w.post() {
		if o.Actor == "runtime" && o.What == what {
			out = append(out, o.Ref)
		}
	}
	return out
}

// count is how many times the sweep asked the engine for one operation.
func (w *sweepWorld) count(what string) int {
	n := 0
	for _, o := range w.post() {
		if o.Actor == "runtime" && o.What == what {
			n++
		}
	}
	return n
}

// ledgerWrites is every ledger transition the sweep attempted, in order. A
// sweep that "reconciled" a row it could not settle shows up here.
func (w *sweepWorld) ledgerWrites() []string {
	var out []string
	for _, o := range w.post() {
		if o.Actor == "ledger" {
			out = append(out, o.What)
		}
	}
	return out
}

// firstOp is the position of the first op of a kind since `begin`, or -1.
func (w *sweepWorld) firstOp(actor, what string) int {
	for i, o := range w.post() {
		if o.Actor == actor && (what == "" || o.What == what) {
			return i
		}
	}
	return -1
}

// network mints a labelled network on the engine and returns the id the FAKE
// minted. Nothing here passes an id in, so a row that agrees with the read is
// agreeing with the engine and not with the test (W2-1).
func (w *sweepWorld) network(t *testing.T, loopSlug string) string {
	t.Helper()
	id, err := w.rt.NetworkCreate(context.Background(), NetworkName(loopSlug), loopSlug)
	if err != nil {
		t.Fatalf("the engine refused the loop's network: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("the fake minted a %d-character network id (%s); the engine prints 64-hex and every comparison here is by that width", len(id), id)
	}
	if id == NetworkName(loopSlug) || strings.HasPrefix(id, "net-") {
		t.Fatalf("the fake echoed the caller's name (%s); an echoing fake is how the network half of W2-1 survived a green suite", id)
	}
	return id
}

// lostNetwork mints one and takes it back out of the engine's inventory, staged
// BEFORE the mark: the engine lost it under us (an operator's `network rm`, an
// engine restart) and the sweep has only its two reads to find that out.
func (w *sweepWorld) lostNetwork(t *testing.T, loopSlug string) string {
	t.Helper()
	id := w.network(t, loopSlug)
	if err := w.rt.NetworkRemove(context.Background(), id); err != nil {
		t.Fatalf("staging the vanished network failed: %v", err)
	}
	return id
}

// container mints a labelled container the engine reports, by the id the FAKE
// minted (`seedAlive`), and returns it.
func (w *sweepWorld) container(t *testing.T, loopSlug string) string {
	t.Helper()
	id := w.rt.seedAlive("loop", loopSlug)
	if len(id) != 64 {
		t.Fatalf("the fake minted a %d-character container id (%s)", len(id), id)
	}
	return id
}

// lostContainer is `container` for a resource the engine no longer reports —
// minted by the fake so the row could not have guessed it, then forgotten.
func (w *sweepWorld) lostContainer(t *testing.T, loopSlug string) string {
	t.Helper()
	id := w.container(t, loopSlug)
	w.rt.list = nil
	return id
}

// stray registers an UNLABELLED container — somebody else's workload on a shared
// engine, which the sweep must count and never touch.
func (w *sweepWorld) stray(t *testing.T, id, name string) {
	t.Helper()
	for _, h := range w.rt.list {
		if h.ID == id {
			t.Fatalf("staging bug: the container %s is already in the inventory", id)
		}
	}
	w.rt.list = append(w.rt.list, Handle{ID: id, Name: name, State: "running"})
}

// row seeds one live provision. The handle is whatever the caller already holds,
// which by construction came out of a fake mint and not out of this file.
func (w *sweepWorld) row(t *testing.T, id, owner, handle, state string) *store.Provision {
	t.Helper()
	row := &store.Provision{ID: id, EnvironmentID: "env-1", ComponentRoot: "/w", LoopSlug: "loop-a",
		Backend: "container", Owner: owner, Handle: handle, State: state, CreatedAt: "2026-11-03T00:00:00Z"}
	w.lg.rows[id] = row
	w.lg.ids = append(w.lg.ids, id)
	return row
}

// sweep runs the daemon-start sweep and refuses to let a hard failure read as a
// verdict: an error out of `Reconcile` means the sweep itself broke, not that it
// decided something.
func (w *sweepWorld) sweep(t *testing.T, warn func(string)) Report {
	t.Helper()
	rep, err := Reconcile(context.Background(), w.lg, w.rt, warn)
	if err != nil {
		t.Fatalf("the sweep failed outright rather than reporting: %v", err)
	}
	return rep
}

// netHolds asks the engine's own inventory — restated OUTSIDE the code under
// test, so "the network survived the sweep" is a fact about the fake's world and
// not about the report's arithmetic.
func netHolds(rt *fakeRuntime, handle string) bool {
	for _, h := range rt.netList {
		if h.ID == handle {
			return true
		}
	}
	return false
}

// wantArgv pins a recorded argv against the REAL builder's output: the witness
// is the slice `docker` would have seen, not a paraphrase of it.
func wantArgv(t *testing.T, got, want []string, claim string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%s\n got: %s\nwant: %s", claim, strings.Join(got, " "), strings.Join(want, " "))
	}
}

// reason reads a row's failure sentence, refusing a silent verdict: a failed row
// that does not say why is the report that makes the next reader guess.
func reason(t *testing.T, row *store.Provision) string {
	t.Helper()
	if row.LastError == nil || strings.TrimSpace(*row.LastError) == "" {
		t.Fatalf("row %s is %s and carries no reason; a failed row must say what it failed at", row.ID, row.State)
	}
	return *row.LastError
}

// ── (a) a ready network row the engine reports is ADOPTED, not removed ──────

// The claim, and the only world in which it means anything: the container
// inventory is EMPTY, so the thing that answered for this row can only have
// been `network ls`. Adoption is said out loud (`Adopted 1`) rather than
// assumed from silence, and no `network rm` reaches the engine at all.
func TestSweepAdoptsAReadyNetworkRowTheEngineReports(t *testing.T) {
	w := newSweepWorld(t)
	minted := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, minted, store.ProvisionStateReady)
	if len(w.rt.list) != 0 {
		t.Fatalf("the container inventory holds %d entries; with anything in it this case cannot show that the NETWORK read is what answered", len(w.rt.list))
	}
	w.begin()
	rep := w.sweep(t, nil)

	if n := w.count("network-list"); n != 1 {
		t.Errorf("the sweep asked the engine for networks %d times, want 1 — one read, before the walk", n)
	}
	if n := w.count("list"); n != 1 {
		t.Errorf("the sweep asked the engine for containers %d times, want 1; the empty container answer is what makes the adoption below the network read's doing", n)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows, want 1 — a row and the engine agreeing is Adoption, and it has to be said: %+v", rep.Adopted, rep)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep removed %d resources while the engine held exactly what the row names; the daemon's own network is not an orphan (EV12 reversed): %+v", rep.Removed, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows a read answered: %+v", rep.Failed, rep)
	}
	if n := w.count("network-rm"); n != 0 {
		t.Errorf("the sweep issued %d `network rm` for a network the ledger claims and the engine holds: %v", n, w.argvs("network-rm"))
	}
	if n := w.count("remove"); n != 0 {
		t.Errorf("the sweep issued %d `docker rm` against a network: a network comes down with `network rm` or it does not come down at all: %v", n, w.argvs("remove"))
	}
	if !netHolds(w.rt, minted) {
		t.Errorf("the engine no longer holds %s; the sweep reported a clean sweep and let the loop's network go", minted)
	}
	if rep.Skipped != 0 {
		t.Errorf("the sweep skipped %d rows a read answered: %+v", rep.Skipped, rep)
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateReady || row.Handle != minted {
		t.Errorf("the row reads %s/%q after a sweep that adopted it; it should still read ready/%s", row.State, row.Handle, minted)
	}
	if got := w.ledgerWrites(); len(got) != 0 {
		t.Errorf("adoption wrote %v to the ledger; the row and the engine already agreed, so the honest sweep writes nothing", got)
	}
}

// ── (b) a network the engine no longer reports settles as GONE ──────────────

// The row says `ready`; the read says nothing. The row settles as failed with
// the exact sentence, and — the half that hid this file's predecessor's bug —
// the sentence names a NETWORK. A network row reported as "the container … is
// gone" is not a cosmetic slip: it is the line an operator reads while going to
// look for a container that never existed, and the failed row it leaves behind
// still counts as live (W2-3's second half) and refuses every later step on the
// loop. Nothing is removed here either: the engine already has nothing.
func TestSweepSettlesANetworkTheEngineNoLongerReportsAsGone(t *testing.T) {
	w := newSweepWorld(t)
	minted := w.lostNetwork(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, minted, store.ProvisionStateReady)
	if netHolds(w.rt, minted) {
		t.Fatalf("staging left the vanished network on the engine; nothing below would mean anything")
	}
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Failed != 1 {
		t.Errorf("the sweep failed %d rows, want 1 — a `ready` row pointing at nothing is a lie the next reader believes: %+v", rep.Failed, rep)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep reported %d removals when the engine had already forgotten the resource: %+v", rep.Removed, rep)
	}
	if rep.Adopted != 0 {
		t.Errorf("the sweep adopted %d rows a read came back empty for: %+v", rep.Adopted, rep)
	}
	if n := w.count("network-rm"); n != 0 {
		t.Errorf("the sweep issued %d `network rm` for a network the engine no longer reports; there was nothing to remove: %v", n, w.argvs("network-rm"))
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateFailed {
		t.Fatalf("the row reads %s, want failed; the ledger must say what the engine says", row.State)
	}
	got := reason(t, row)
	if want := "reconcile: the loop network " + minted + " is gone"; got != want {
		t.Errorf("the gone reason drifted\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, "container") {
		t.Errorf("the sentence calls a network a container (%q); `whoRow` is the one vocabulary for what a row owns, and a network row is not a container", got)
	}
	if n := w.count("remove"); n != 0 {
		t.Errorf("the sweep issued %d `docker rm` for a network row: %v", n, w.argvs("remove"))
	}
}

// ── (c) an orphan is claimed and removed by the id the read ANSWERED ────────

// Two networks on the engine: one a live row claims, one nobody's row names.
// The orphan comes down and it comes down speaking the engine's own id — the
// same handle a row would carry — because `network rm` accepts a name as a
// convenience the engine never promised, and a sweep that takes that convenience
// is a sweep whose removals cannot be matched back to the ledger. The argv is
// read twice over: joined against the REAL builder's output, and token by token
// so `docker rm <id>` cannot pass for `docker network rm <id>` on a substring.
func TestSweepRemovesAnOrphanNetworkByTheIDTheReadAnswered(t *testing.T) {
	w := newSweepWorld(t)
	live := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, live, store.ProvisionStateReady)
	orphan := w.network(t, "loop-gone") // labelled, live, named by no row
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Removed != 1 {
		t.Errorf("the sweep removed %d resources where exactly one orphan stood on the engine: %+v", rep.Removed, rep)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows, want 1 (the live row): %+v", rep.Adopted, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows a read answered: %+v", rep.Failed, rep)
	}
	rms := w.argvs("network-rm")
	if len(rms) != 1 {
		t.Fatalf("the sweep recorded %d `network rm` commands, want exactly 1: %v", len(rms), rms)
	}
	wantArgv(t, rms[0], networkRmArgv("docker", orphan), "the orphan came down by something other than the id the read answered")
	if len(rms[0]) != 4 {
		t.Errorf("`network rm` was handed %d tokens (%v); the removal is one handle and nothing else — no force, no second name", len(rms[0]), rms[0])
	}
	if got := strings.Join(rms[0][1:4], " "); got != "network rm "+orphan {
		t.Errorf("the verb is wrong or the handle is wrong: argv[1:4] reads %q, want %q — a `container rm` of the right id removes a container, not this network", got, "network rm "+orphan)
	}
	if r := w.refs("network-rm"); len(r) != 1 || r[0] != orphan {
		t.Errorf("the engine was asked to remove %v; the handle must be the id the read answered, not the network's name", r)
	}
	if n := w.count("remove"); n != 0 {
		t.Errorf("the sweep issued %d `docker rm` while doing a network's work: %v", n, w.argvs("remove"))
	}
	if !netHolds(w.rt, live) {
		t.Errorf("the network a live row claims (%s) came down with the orphan; the claim is the row's handle and the engine agrees (EV12)", live)
	}
	if netHolds(w.rt, orphan) {
		t.Errorf("the sweep reported the orphan removed and the engine still holds it (%s)", orphan)
	}
	// The removal is the engine's, not the fake's courtesy: the same handle asked
	// for again is the engine's own not-found.
	if err := w.rt.NetworkRemove(context.Background(), orphan); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a re-ask of the orphan answered %v; a fake that keeps saying yes cannot prove a removal happened", err)
	}
}

// ── (d1) every row is answered from its OWN inventory ───────────────────────

// The two inventories are disjoint on purpose: `docker ps` knows the container
// id and nothing else, `network ls` knows the network id and nothing else. Both
// rows are adopted, which is only possible if each was asked of the inventory
// that owns its kind — `Adopted 2, Failed 0` is the shape of the W2-1 fix, and
// asking one list about both kinds is what made the daemon delete its own
// network while calling it a gone container.
func TestSweepAsksEachRowOfItsOwnInventory(t *testing.T) {
	w := newSweepWorld(t)
	ctr := w.container(t, "loop-a")
	net := w.network(t, "loop-a")
	w.row(t, "prov-ctr", store.ProvisionOwnerLoop, ctr, store.ProvisionStateReady)
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateReady)
	if w.rt.holdsID(net) {
		t.Fatalf("staging bug: the container inventory answers the network's handle, and the test below could not tell the two reads apart")
	}
	if netHolds(w.rt, ctr) {
		t.Fatalf("staging bug: the network inventory answers the container's handle, same reason")
	}
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Adopted != 2 || rep.Failed != 0 {
		t.Errorf("the sweep reported adopted=%d failed=%d, want 2/0 — each row was answered by the inventory that owns its kind: %+v", rep.Adopted, rep.Failed, rep)
	}
	if rep.Removed != 0 || rep.Skipped != 0 {
		t.Errorf("the sweep removed %d and skipped %d with two live rows the engine holds: %+v", rep.Removed, rep.Skipped, rep)
	}
	if n := w.count("network-rm"); n != 0 {
		t.Errorf("the sweep issued %d `network rm` where both inventories answered: %v", n, w.argvs("network-rm"))
	}
	if n := w.count("remove"); n != 0 {
		t.Errorf("the sweep issued %d `docker rm` where both inventories answered: %v", n, w.argvs("remove"))
	}
	if w.lg.rows["prov-net"].State != store.ProvisionStateReady || w.lg.rows["prov-ctr"].State != store.ProvisionStateReady {
		t.Errorf("a live row moved off ready: net=%s container=%s", w.lg.rows["prov-net"].State, w.lg.rows["prov-ctr"].State)
	}
}

// ── (d2) a network row answered only by `ps` is NOT adopted ─────────────────

// The historical defect's opposite face, and the one a half-fix passes: the
// network is gone from `network ls`, and the SAME 64-hex turns up in `docker ps`
// as somebody else's unlabelled container. A sweep that asks the container list
// about a network row adopts a row whose network does not exist — and the
// unlabelled impostor, which is not ours in any branch, would be the thing it
// believed. The row must fail on the network read alone, and the stranger must
// be counted and left standing.
func TestSweepDoesNotAdoptANetworkRowFromTheContainerList(t *testing.T) {
	w := newSweepWorld(t)
	net := w.lostNetwork(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateReady)
	w.stray(t, net, "someone-elses-build") // somebody else's container, wearing the id
	if !w.rt.holdsID(net) {
		t.Fatalf("staging left the impostor off the container inventory; the wrong-inventory claim below would prove nothing")
	}
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Adopted != 0 {
		t.Errorf("the sweep adopted %d rows from the container inventory; a network answered by `ps` is not a network the engine has — this is the historical Adopted 0/1, Failed 1 defect wearing the opposite face: %+v", rep.Adopted, rep)
	}
	if rep.Failed != 1 {
		t.Errorf("the network row was not failed on the network read (failed=%d): %+v", rep.Failed, rep)
	}
	if rep.Unlabelled != 1 {
		t.Errorf("the sweep counted %d unlabelled containers, want 1 — it must SEE the stranger it refuses to touch: %+v", rep.Unlabelled, rep)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep removed %d resources; the stranger is somebody else's and the row's network is already gone: %+v", rep.Removed, rep)
	}
	if n := w.count("remove"); n != 0 {
		t.Errorf("the sweep issued %d `docker rm`; an unlabelled container is not ours in any branch: %v", n, w.argvs("remove"))
	}
	if n := w.count("network-rm"); n != 0 {
		t.Errorf("the sweep issued %d `network rm` for a network the read did not answer: %v", n, w.argvs("network-rm"))
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateFailed {
		t.Fatalf("the network row reads %s, want failed on the network read alone", row.State)
	}
	if got := reason(t, row); !strings.Contains(got, "loop network") || strings.Contains(got, "container") {
		t.Errorf("the reason misnames the kind (%q); the read that failed is the network read", got)
	}
	if !w.rt.holdsID(net) {
		t.Errorf("the sweep removed the unlabelled impostor container %s; another workload's container is not this sweep's to take", net)
	}
}

// ── (e) both inventories empty: nothing adopted, and each reason names its kind ──

// The engine answers NOTHING for either kind. Both rows settle as failed, and
// the two sentences must not be interchangeable: the container row's reason
// names a container, the network row's names a loop network. A sweep that reads
// one list and forgets which row it came from produces a ledger whose reasons
// point at the wrong kind of thing, which is how this family hid for a whole
// suite. The label filter is pinned on the log beside them, because an
// unfiltered read would be a sweep reaching for somebody else's resources.
func TestSweepNamesEachKindInItsOwnReasonWhenBothInventoriesAreEmpty(t *testing.T) {
	w := newSweepWorld(t)
	ctr := w.lostContainer(t, "loop-a")
	net := w.lostNetwork(t, "loop-a")
	w.row(t, "prov-ctr", store.ProvisionOwnerLoop, ctr, store.ProvisionStateReady)
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateReady)
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Adopted != 0 {
		t.Errorf("the sweep adopted %d rows from two empty inventories: %+v", rep.Adopted, rep)
	}
	if rep.Failed != 2 {
		t.Errorf("the sweep failed %d rows, want 2 — both resources are gone and both rows say so: %+v", rep.Failed, rep)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep removed %d resources it could not see: %+v", rep.Removed, rep)
	}
	if rep.Inert {
		t.Errorf("the sweep called itself inert against an engine that answered (empty is an answer): %+v", rep)
	}
	crow, nrow := w.lg.rows["prov-ctr"], w.lg.rows["prov-net"]
	if crow.State != store.ProvisionStateFailed || nrow.State != store.ProvisionStateFailed {
		t.Fatalf("the vanished rows did not settle: container=%s network=%s", crow.State, nrow.State)
	}
	if got, want := reason(t, crow), "reconcile: the container "+ctr+" is gone"; got != want {
		t.Errorf("the container row's reason does not name its own kind\n got: %s\nwant: %s", got, want)
	}
	if got, want := reason(t, nrow), "reconcile: the loop network "+net+" is gone"; got != want {
		t.Errorf("the network row's reason does not name its own kind\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(reason(t, crow), "network") {
		t.Errorf("the container row's reason names a network (%s)", *crow.LastError)
	}
	if strings.Contains(reason(t, nrow), "container") {
		t.Errorf("the network row's reason names a container (%s)", *nrow.LastError)
	}
	// The reads themselves, pinned on the shared log: both are filtered to OURS.
	lists := w.argvs("network-list")
	if len(lists) != 1 {
		t.Fatalf("the network read ran %d times, want 1: %v", len(lists), lists)
	}
	wantArgv(t, lists[0], networkListArgv("docker", LabelManaged+"=1"), "the network read is not the pinned `network ls --no-trunc --filter label=… --format {{.ID}}|{{.Name}}`")
	if r := w.refs("list"); len(r) != 1 || r[0] != LabelManaged+"=1" {
		t.Errorf("the container read carried filter %v, want [%s] — an unfiltered list is a sweep reaching for another workload's containers", r, LabelManaged+"=1")
	}
}

// ── (f) a `provisioning` network row finishes without being started ─────────

// A `provisioning` network row whose network the engine has is the daemon having
// died between the create and the ready mark. It gets its state from the ledger,
// not from the engine: a network has no `start`, so a sweep that called
// `docker start <network id>` is issuing a command nothing can answer — and the
// row it "finished" that way would be a row the sweep invented.
func TestSweepFinishesAProvisioningNetworkRowWithoutStartingIt(t *testing.T) {
	w := newSweepWorld(t)
	net := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateProvisioning)
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Finished != 1 {
		t.Errorf("the sweep finished %d rows, want 1 — the engine has the network, so the row's only missing half is its state: %+v", rep.Finished, rep)
	}
	if rep.Failed != 0 || rep.Adopted != 0 {
		t.Errorf("the sweep reported failed=%d adopted=%d for a row it should have finished: %+v", rep.Failed, rep.Adopted, rep)
	}
	if n := w.count("start"); n != 0 {
		t.Errorf("the sweep issued %d `docker start` for a NETWORK (refs %v); a network has no start verb", n, w.refs("start"))
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateReady {
		t.Errorf("the row reads %s, want ready", row.State)
	}
	if row.Handle != net {
		t.Errorf("the finished row stores %q, not the id the engine minted (%s) — the handle is the receipt, and the sweep must not rewrite it", row.Handle, net)
	}
	if got := w.ledgerWrites(); len(got) != 1 || got[0] != "MarkProvisionReady" {
		t.Errorf("the finish wrote %v to the ledger, want [MarkProvisionReady]", got)
	}
}

// ── (g) a failing network read leaves the network rows EXACTLY as they were ─

// Not inert — that is the container list's rule, and the containers here really
// were reconciled against a read the engine answered. The network rows are the
// per-row form of the same rule: a sweep that guesses about a resource it could
// not ask about is worse than one that waits. One warning, zero ledger writes,
// zero `network rm`, the row still `ready` with its handle intact.
func TestSweepLeavesNetworkRowsUntouchedWhenTheNetworkReadFails(t *testing.T) {
	w := newSweepWorld(t)
	net := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateReady)
	ctr := w.container(t, "loop-a")
	w.row(t, "prov-ctr", store.ProvisionOwnerLoop, ctr, store.ProvisionStateReady)
	w.rt.netListErr = errors.New("Cannot connect to the Docker daemon at unix:///run/user/1000/docker.sock")
	w.begin()
	var warned []string
	rep := w.sweep(t, func(s string) { warned = append(warned, s) })

	if rep.Inert {
		t.Errorf("the sweep called itself inert; the containers WERE reconciled against a list the engine answered — the network half is what it could not do, and it says so: %+v", rep)
	}
	if rep.Skipped != 1 {
		t.Errorf("the sweep skipped %d rows, want 1 (the network row, left exactly as it was): %+v", rep.Skipped, rep)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d container rows, want 1 — the read that worked still worked: %+v", rep.Adopted, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows on a read that never answered; marking a row failed on an engine it could not ask is a guess: %+v", rep.Failed, rep)
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateReady || row.Handle != net {
		t.Errorf("the network row moved to %s/%q while the read was failing; it was to be left exactly as it was", row.State, row.Handle)
	}
	if !netHolds(w.rt, net) {
		t.Errorf("the sweep removed the network it could not reconcile (%s); not seeing a resource is not evidence that it is an orphan", net)
	}
	if n := w.count("network-rm"); n != 0 {
		t.Errorf("the sweep issued %d `network rm` while the network read was failing: %v", n, w.argvs("network-rm"))
	}
	if got := w.ledgerWrites(); len(got) != 0 {
		t.Errorf("the failing read produced %v on the ledger; a row the sweep could not ask about gets no verdict", got)
	}
	if len(rep.Warnings) != 1 || len(warned) != 1 {
		t.Fatalf("the sweep reported %d warnings and spoke %d, want exactly one of each: %v / %v", len(rep.Warnings), len(warned), rep.Warnings, warned)
	}
	if !strings.Contains(rep.Warnings[0], "network") {
		t.Errorf("the warning does not name what it could not do (%q); a warning that does not name its subject is a warning nobody acts on", rep.Warnings[0])
	}
}

// ── (h) the network inventory is read BEFORE the rows are walked ────────────

// One ordering claim, read off the ONE shared op log rather than inferred from
// two independent counters: the `network ls` that answers for every network row
// happens before the first ledger write, which is the walk's first act. Walk the
// rows first and every network row is answered by an inventory the sweep has not
// asked yet — which is the pre-W2-1 shape, `Adopted 0, Failed 1`, the loop's own
// network gone.
func TestSweepReadsTheNetworkInventoryBeforeItWalksTheRows(t *testing.T) {
	w := newSweepWorld(t)
	net := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, net, store.ProvisionStateProvisioning)
	w.begin()
	w.sweep(t, nil)

	read := w.firstOp("runtime", "network-list")
	if read < 0 {
		t.Fatal("the sweep never read the network inventory at all; every network verdict below was a guess")
	}
	wrote := w.firstOp("ledger", "")
	if wrote < 0 {
		t.Fatal("the sweep wrote nothing to the ledger, so the ordering below compares a read against nothing")
	}
	if read > wrote {
		t.Errorf("the network inventory was read at op %d, AFTER the first ledger write at %d — a row walked before that read is answered by an inventory the sweep never asked (%+v)", read, wrote, w.ledgerWrites())
	}
	if got := w.ledgerWrites(); len(got) != 1 || got[0] != "MarkProvisionReady" {
		t.Errorf("the world was staged to finish one provisioning network row; the ledger took %v", got)
	}
}

// ── (i) the network the sweep itself just finished must survive that sweep ───

// THE CASE THIS FILE EXISTS FOR, and the reason `liveNet` now claims every live
// row (the fix at `reconcile.go`'s claim loop): the sweep finishes a
// `provisioning` network row — the daemon died between the create and the ready
// mark — and one line later its own orphan pass
// finds that same network on the engine, looks it up in the claim snapshot taken
// BEFORE the walk, finds no `ready` row naming it, and takes it down. The pass
// then reports Finished 1 AND Removed 1, and the ledger ends it carrying a ready
// row whose network is gone.
//
// Containers have always been immune, and the asymmetry is the whole defect:
// `liveIDs` claims EVERY live row whatever its state, while `liveNet` claimed
// only `ready` ones, and the finish branch sets neither `liveNet` nor
// `takenDown`. So the fix is the symmetric one predicate — claim every live
// network row, exactly as the container side does — and not a special case
// bolted onto the finish branch. A row is claimed by its own sweep for the whole
// sweep: `provisioning` (which this walk finishes), `tearing_down` (which it
// leaves to the teardown), `failed` (which it removes, and records in
// `takenDown` while doing so).
func TestSweepKeepsTheNetworkItJustFinished(t *testing.T) {
	w := newSweepWorld(t)
	minted := w.network(t, "loop-a")
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, minted, store.ProvisionStateProvisioning)
	if !netHolds(w.rt, minted) {
		t.Fatal("the staging left the in-flight network off the engine; neither claim below would mean anything")
	}
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Finished != 1 {
		t.Errorf("the sweep finished %d rows, want 1 — the engine has the network, so the row's missing half is its state: %+v", rep.Finished, rep)
	}
	row := w.lg.rows["prov-net"]
	if row.State != store.ProvisionStateReady || row.Handle != minted {
		t.Fatalf("the finished row reads %s/%q, want ready/%s", row.State, row.Handle, minted)
	}
	rms := w.argvs("network-rm")
	if len(rms) != 0 {
		t.Errorf("the sweep issued %d `network rm` for the network it had just marked ready (%s · %v) — the ledger now carries a ready row pointing at nothing, which is the exact lie EV12 exists to refuse (W2-1's rule, one line later in the same sweep)", len(rms), minted, rms)
	}
	if r := w.refs("network-rm"); len(r) != 0 {
		t.Errorf("the engine was asked to remove %v — the network this sweep acquired is claimed by the row it just finished", r)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep reported %d removals where it had just acquired the only resource on the engine: %+v", rep.Removed, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows a read answered: %+v", rep.Failed, rep)
	}
	if !netHolds(w.rt, minted) {
		t.Errorf("the engine no longer holds %s; a sweep that finishes a row and removes its resource in the same pass leaves the loop with a network the ledger swears is up", minted)
	}
}

// ── (j) and the same for a `tearing_down` network row ────────────────────────

// A row in flight is not a row this sweep may settle: `tearing_down` is
// settle-once, and the teardown that put it there owns the removal. The claim
// snapshot must therefore cover it too — otherwise the orphan pass takes the
// network down from under a teardown that is halfway through it, and the
// `tearing_down` row is left naming a resource the engine no longer has. The
// second network is the anti-vacuity half: without a real orphan on the engine,
// "no `network rm` for the in-flight one" would also be satisfied by an orphan
// pass that never ran at all.
func TestSweepKeepsANetworkWhoseRowIsStillTearingDown(t *testing.T) {
	w := newSweepWorld(t)
	minted := w.network(t, "loop-a")    // in flight: its row reads tearing_down
	orphan := w.network(t, "loop-gone") // nobody's row: the orphan pass's real work
	w.row(t, "prov-net", store.ProvisionOwnerNetwork, minted, store.ProvisionStateTearingDown)
	if !netHolds(w.rt, minted) || !netHolds(w.rt, orphan) {
		t.Fatal("the staging left the in-flight network or the orphan off the engine; neither claim below would mean anything")
	}
	w.begin()
	rep := w.sweep(t, nil)

	if rep.Removed != 1 {
		t.Errorf("the sweep removed %d resources, want exactly 1 (the orphan): %+v — the in-flight network came down with it, which is a sweep removing from under a teardown still owned by someone else", rep.Removed, rep)
	}
	if rep.Finished != 0 || rep.Adopted != 0 || rep.Failed != 0 {
		t.Errorf("a tearing_down network row was reported finished=%d adopted=%d failed=%d: %+v — the sweep cannot settle a row it did not start and cannot re-adjudicate", rep.Finished, rep.Adopted, rep.Failed, rep)
	}
	if rep.Skipped != 1 {
		t.Errorf("the sweep skipped %d rows, want 1 (the in-flight row, left to the teardown): %+v", rep.Skipped, rep)
	}
	rms := w.argvs("network-rm")
	if len(rms) != 1 {
		t.Fatalf("the sweep issued %d `network rm` where exactly one orphan was on the engine: %v", len(rms), rms)
	}
	wantArgv(t, rms[0], networkRmArgv("docker", orphan), "the orphan pass removed something other than the network no row claims")
	if r := w.refs("network-rm"); r[0] == minted {
		t.Errorf("the sweep issued `network rm %s` for the network a live tearing_down row still names — the claim snapshot is what makes a row in flight its own resource's owner for the length of this pass", minted)
	}
	if !netHolds(w.rt, minted) {
		t.Errorf("the engine no longer holds %s, the network of a row mid-teardown; the ledger still names it and the teardown that owns it will find nothing to remove", minted)
	}
	if netHolds(w.rt, orphan) {
		t.Errorf("the sweep reported the orphan and the engine still holds it (%s); a removal the ledger counts and the engine denies is the lie EV12 refuses", orphan)
	}
	if row := w.lg.rows["prov-net"]; row.State != store.ProvisionStateTearingDown || row.Handle != minted {
		t.Errorf("the in-flight row reads %s/%q after the sweep, want tearing_down/%s — a row in flight is left exactly as its teardown left it", row.State, row.Handle, minted)
	}
	if got := w.ledgerWrites(); len(got) != 0 {
		t.Errorf("the sweep wrote %v to the ledger; a row in flight gets no verdict from a sweep that cannot settle it", got)
	}
}
