package container

// Real-engine NETWORK sweep — S59 T12, the branch family round 26 recorded as
// gap 3 ("the network-row sweep branches are fake-measured only").
//
// `network_sweep_test.go` mints every id INSIDE the fake (`netID`), so that file
// can only ever prove the code is self-consistent about a value it invented
// together with the engine. Four things were never observed on a real engine, and
// they are what this file asks of rootless docker 29.8.0:
//
//	a. a labelled network the ledger names is ADOPTED and STAYS on the engine —
//	   asserted on the engine (`network inspect` answers it afterwards) and on the
//	   absence of a single `network rm` handed to the child process;
//	b. an unclaimed network is claimed and removed by the id the pinned
//	   `network ls --no-trunc --format '{{.ID}}|{{.Name}}'` ANSWERED — asserted
//	   twice over: on the recorded argv (token by token, so `docker rm <id>` cannot
//	   pass for `docker network rm <id>`), and on `docker network inspect <id>`
//	   failing afterwards with the engine's own not-found words;
//	c. W2-11 on the engine rather than the fake: the network the sweep itself just
//	   finished (the `provisioning` row it marks ready) survives that same sweep —
//	   with a real orphan standing on the engine beside it, so the orphan pass
//	   demonstrably ran and "nothing was removed" is not satisfied by a pass that
//	   never happened;
//	d. and the authority rule those three lean on: a network carrying no
//	   `aeon.managed` label is not ours in any branch. This is the strict reading
//	   of "unlabelled" — (b) is the reading the sweep's own vocabulary uses, an
//	   aeon-labelled network named by no live row — so both are measured here
//	   instead of one of them being argued.
//
// Nothing in this file starts a container: the sweep is a network-and-ledger
// question. `AEON_TEST_CONTAINER_IMAGE` is checked as the lane's identity (the one
// local image, and `Pull` is never reached) rather than as a dependency.
//
// Opt-in, like every real-engine test in this package:
//
//	AEON_TEST_CONTAINER=1 AEON_TEST_CONTAINER_IMAGE=lscr.io/linuxserver/code-server:latest \
//	  go test -tags sqlite_fts5 ./internal/execenv/container/ -run RealEngineSweep -count=1 -v
//
// The resources this file may touch are ONLY the ones it created in the same call,
// and every removal speaks an id the engine printed for that call — never a name,
// never an enumeration of the lane. The lane's own `code-server` container and its
// four default networks are never candidates: the gate refuses to run a sweep at
// all rather than let one remove a resource this call did not create.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// realSweepLane is one real engine, one in-memory ledger, and the argv seam that
// records what the engine was actually handed on the way to the verdict.
type realSweepLane struct {
	t    *testing.T
	ctx  context.Context
	rt   *CLIRuntime
	rec  *engineArgvRecorder
	lg   *fakeLedger
	bin  string
	slug string
	// ours are the resource ids THIS call created, exactly as the engine printed
	// them. Cleanup removes exactly these, by id, and nothing else.
	ours []string
}

func newRealSweepLane(t *testing.T) *realSweepLane {
	t.Helper()
	if os.Getenv("AEON_TEST_CONTAINER") != "1" {
		t.Skip("not run — set AEON_TEST_CONTAINER=1 on a lane with a docker-compatible engine (approval A4)")
	}
	image := os.Getenv(realImageEnv)
	if image == "" {
		t.Skipf("not run — this lane names no local image; set %s (this sweep never pulls)", realImageEnv)
	}
	bin, err := Discover("")
	if err != nil {
		t.Skipf("not run — no engine on this lane: %v", err)
	}
	rec := &engineArgvRecorder{}
	rt := &CLIRuntime{Bin: bin, Call: rec.seam}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	if err := rt.Available(ctx); err != nil {
		t.Skipf("not run — the engine is on PATH but not answering: %v", err)
	}
	if ok, ierr := rt.ImageExists(ctx, image); ierr != nil {
		t.Fatalf("the engine would not answer whether %q exists: %v", image, ierr)
	} else if !ok {
		t.Skipf("not run — %s is not on this lane and this sweep never pulls (pull=never)", image)
	}
	w := &realSweepLane{t: t, ctx: ctx, rt: rt, rec: rec, lg: newFakeLedger(), bin: bin,
		slug: fmt.Sprintf("sweep%d", time.Now().UnixNano())}
	// The lane must hold nothing labelled that this call did not create BEFORE a
	// sweep runs on it. Reconcile removes every labelled resource no live row
	// names — its job against the daemon's own store, an act of vandalism against
	// another session's live container. Refuse the run rather than remove what we
	// did not create, and say what was standing there.
	if hs, lerr := rt.List(ctx, LabelManaged+"=1"); lerr != nil {
		t.Fatalf("the engine would not answer the labelled container list: %v", lerr)
	} else if len(hs) != 0 {
		var seen []string
		for _, h := range hs {
			seen = append(seen, h.ID+"|"+h.Name+"|"+h.State)
		}
		t.Skipf("not run — the lane holds %d labelled container(s) this call did not create (%s); "+
			"running a sweep here would remove them, and they are not ours to remove", len(hs), strings.Join(seen, ", "))
	}
	if ns, nerr := rt.NetworkList(ctx, LabelManaged+"=1"); nerr != nil {
		t.Fatalf("the engine would not answer the labelled network list: %v", nerr)
	} else if len(ns) != 0 {
		var seen []string
		for _, h := range ns {
			seen = append(seen, h.ID+"|"+h.Name)
		}
		t.Skipf("not run — the lane holds %d labelled network(s) this call did not create (%s); "+
			"running a sweep here would remove them, and they are not ours to remove", len(ns), strings.Join(seen, ", "))
	}
	t.Cleanup(w.clean)
	return w
}

// own records a resource id the engine printed for THIS call, so cleanup can
// remove it by that id and by nothing else.
func (w *realSweepLane) own(id string) string {
	w.ours = append(w.ours, id)
	return id
}

// clean removes only what this call created, by the ids the engine printed for
// them, whatever the assertions below decided. A not-found is the success case:
// a sweep that passed is expected to have removed what it was staged to remove.
func (w *realSweepLane) clean() {
	for _, id := range w.ours {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, w.bin, "network", "rm", id)
		cmd.Env = cliProcessEnv(nil)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		cancel()
		if err == nil {
			w.t.Logf("cleanup: removed %s (%s)", id, strings.TrimSpace(string(out)))
			continue
		}
		words := strings.TrimSpace(stderr.String())
		if words == "" {
			words = err.Error()
		}
		if strings.Contains(words, "not found") || strings.Contains(words, "No such") {
			w.t.Logf("cleanup: %s was already gone (the sweep's own removal): %s", id, words)
			continue
		}
		w.t.Logf("cleanup: the engine refused to remove %s: %s — report this; a labelled network left "+
			"behind is a resource the next sweep will call an orphan", id, words)
	}
	w.ours = nil
}

// row stages one live provision in the in-memory ledger. The handle always came
// out of a create this call made through the driver, so a row and the engine can
// only agree if the driver kept the receipt.
func (w *realSweepLane) row(id, owner, handle, state string) *store.Provision {
	w.t.Helper()
	r := &store.Provision{ID: id, EnvironmentID: "env-real-sweep", ComponentRoot: "/real-sweep",
		LoopSlug: w.slug, Backend: "container", Owner: owner, Handle: handle, State: state,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	w.lg.rows[id] = r
	w.lg.ids = append(w.lg.ids, id)
	return r
}

// network creates ONE labelled network through the driver's own door and returns
// the name it was given and the id the engine printed. The id is recorded as this
// call's before anything else can fail, so a later t.Fatalf cannot leak it.
func (w *realSweepLane) network(role string) (name, id string) {
	w.t.Helper()
	slug := fmt.Sprintf("%d%s", time.Now().UnixNano(), role)
	name = NetworkName(slug)
	start := time.Now()
	got, err := w.rt.NetworkCreate(w.ctx, name, slug)
	if err != nil {
		w.t.Fatalf("the real engine refused to create the %s network %s: %v", role, name, err)
	}
	if len(got) != 64 {
		w.t.Fatalf("the engine printed a %d-character network id (%s) for %s; `network create` prints the "+
			"full 64-hex and every comparison in the sweep is by that width", len(got), got, name)
	}
	w.own(got)
	w.t.Logf("staged: the %s network %s = %s (create took %s)", role, name, got, time.Since(start))
	return name, got
}

// unlabelledNetwork creates a network with NO aeon label, through the engine
// client directly — the shape `networkArgv` refuses to produce, because an
// unlabelled network is one the sweep must never touch. It is still this call's
// resource, so cleanup removes it by the id the engine printed.
func (w *realSweepLane) unlabelledNetwork(role string) (name, id string) {
	w.t.Helper()
	name = fmt.Sprintf("s59-unlabelled-%d%s", time.Now().UnixNano(), role)
	cmd := exec.CommandContext(w.ctx, w.bin, "network", "create", "--driver", "bridge", name)
	cmd.Env = cliProcessEnv(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		words := strings.TrimSpace(stderr.String())
		if words == "" {
			words = err.Error()
		}
		w.t.Fatalf("the engine refused to create the unlabelled probe network %s: %s — the engine's words, "+
			"not a downgrade of the assertion", name, words)
	}
	id = strings.TrimSpace(string(out))
	if len(id) != 64 {
		w.t.Fatalf("the engine printed a %d-character id (%s) for the unlabelled probe; the width every "+
			"comparison here uses is 64-hex", len(id), id)
	}
	w.own(id)
	w.t.Logf("staged: the unlabelled probe network %s = %s", name, id)
	return name, id
}

// readNetworks runs the pinned read OUTSIDE the code under test, so the id an
// assertion compares against is the engine's answer to the same argv the sweep
// uses rather than something this file supplied. An empty filter is the
// unfiltered read — the one that proves a resource really is standing on the
// engine even though the sweep's filtered read never sees it.
func (w *realSweepLane) readNetworks(filter string) []Handle {
	w.t.Helper()
	argv := []string{w.bin, "network", "ls", "--no-trunc"}
	if filter != "" {
		argv = append(argv, "--filter", "label="+filter)
	}
	argv = append(argv, "--format", "{{.ID}}|{{.Name}}")
	cmd := exec.CommandContext(w.ctx, w.bin, argv[1:]...)
	cmd.Env = cliProcessEnv(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		words := strings.TrimSpace(stderr.String())
		if words == "" {
			words = err.Error()
		}
		w.t.Fatalf("the engine refused the independent read %s: %s", strings.Join(argv, " "), words)
	}
	var hs []Handle
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		id, name, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		hs = append(hs, Handle{ID: id, Name: name})
	}
	return hs
}

// inspect asks the engine directly whether it still holds a network, and returns
// the answer with the engine's own words. The witness is a process the sweep did
// not issue, which is the difference between "the report said removed" and "the
// engine has nothing at that id".
func (w *realSweepLane) inspect(id string) (bool, string) {
	w.t.Helper()
	cmd := exec.CommandContext(w.ctx, w.bin, "network", "inspect", id, "--format", "{{.Id}}")
	cmd.Env = cliProcessEnv(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return true, strings.TrimSpace(string(out))
	}
	words := strings.TrimSpace(stderr.String())
	if words == "" {
		words = err.Error()
	}
	return false, words
}

// mustStand asserts the engine still holds a network after the sweep.
func (w *realSweepLane) mustStand(what, id string) {
	w.t.Helper()
	ok, words := w.inspect(id)
	w.t.Logf("engine answer: `network inspect %s` for the %s → %s (%s)", id, what, map[bool]string{true: "exit 0", false: "exit !=0"}[ok], words)
	if !ok {
		w.t.Errorf("the engine no longer holds the %s network %s: %s — the sweep had no right to remove it", what, id, words)
	}
}

// mustBeGone asserts the engine refuses that network now, and that the refusal is
// its own not-found sentence rather than a silence the test could misread.
func (w *realSweepLane) mustBeGone(what, id string) {
	w.t.Helper()
	ok, words := w.inspect(id)
	w.t.Logf("engine answer: `network inspect %s` for the %s → %s (%s)", id, what, map[bool]string{true: "exit 0", false: "exit !=0"}[ok], words)
	if ok {
		w.t.Errorf("the engine still holds the %s network %s after a removal the sweep reported (%s) — a removal "+
			"the engine denies is the lie EV12 refuses", what, id, words)
		return
	}
	if !strings.Contains(words, "not found") && !strings.Contains(words, "No such") {
		w.t.Errorf("the engine answered the gone network with something other than its not-found sentence (%q); "+
			"the removal is unproven if the engine can be heard saying anything", words)
	}
}

// sweep runs the daemon-start sweep against the real engine and refuses to let a
// hard failure read as a verdict. The recorder is cleared first: everything the
// lane did to STAGE its world (the gate's own reads, the creates) is a real engine
// call but not a call the sweep made, and an argv counted below has to be one the
// sweep issued. Same discipline as `sweepWorld.begin()` in the fake-measured file.
func (w *realSweepLane) sweep() Report {
	w.t.Helper()
	w.rec.reset()
	w.lg.log.reset()
	start := time.Now()
	rep, err := Reconcile(w.ctx, w.lg, w.rt, func(msg string) { w.t.Logf("sweep warned: %s", msg) })
	w.t.Logf("measured: the sweep over %d staged row(s) took %s — report %+v", len(w.lg.ids), time.Since(start), rep)
	if err != nil {
		w.t.Fatalf("the sweep failed outright rather than reporting: %v", err)
	}
	if rep.Inert {
		w.t.Fatalf("the sweep called itself inert against an engine that answered every read: %+v", rep)
	}
	return rep
}

// ledgerWrites is every ledger transition the sweep attempted, in order.
func (w *realSweepLane) ledgerWrites() []string { return w.lg.log.whats("ledger") }

// rmArgvs is every `docker network rm` the sweep handed the child process.
func (w *realSweepLane) rmArgvs() [][]string {
	return w.rec.calls("docker", "network", "rm")
}

// reset starts the record from nothing. These two belong with the real-engine lane
// rather than with the actors themselves: the actors are shared with the fake-only
// files, and clearing a log is something only a lane that has finished staging does.
func (r *engineArgvRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = nil
}

func (l *log) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = nil
}

// ── (a) a labelled network the row names is ADOPTED and stays on the engine ──

// The engine holds exactly one labelled network and the ledger names it. The sweep
// must adopt it, write nothing, and never so much as hand `network rm` to a child
// process — and the proof that it stayed is the engine's own answer afterwards, not
// the report's arithmetic. The container inventory is empty (the lane gate pins
// that), so the only read that can have answered for this row is `network ls`.
func TestRealEngineSweepAdoptsTheLabelledNetworkAndLeavesItStanding(t *testing.T) {
	w := newRealSweepLane(t)
	name, id := w.network("adopt")
	w.row("prov-net", store.ProvisionOwnerNetwork, id, store.ProvisionStateReady)

	rep := w.sweep()

	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows where the engine held exactly what the row names, want 1 — adoption "+
			"is said out loud, not assumed from silence: %+v", rep.Adopted, rep)
	}
	if rep.Removed != 0 {
		t.Errorf("the sweep removed %d resources: the daemon's own network is not an orphan (EV12 reversed): %+v", rep.Removed, rep)
	}
	if rep.Failed != 0 || rep.Skipped != 0 {
		t.Errorf("the sweep reported failed=%d skipped=%d for a row the engine answered: %+v", rep.Failed, rep.Skipped, rep)
	}
	if rms := w.rmArgvs(); len(rms) != 0 {
		t.Errorf("the sweep handed the engine %d `network rm` commands for a network a live row claims (%v) — "+
			"the removal is not merely unreported, it was never asked for either", len(rms), rms)
	}
	if lists := w.rec.calls("docker", "network", "ls"); len(lists) != 1 {
		t.Errorf("the sweep read the network inventory %d times, want exactly 1: %v", len(lists), lists)
	} else if got := strings.Join(lists[0], " "); !strings.Contains(got, "--no-trunc") ||
		!strings.Contains(got, "{{.ID}}|{{.Name}}") || !strings.Contains(got, "label="+LabelManaged+"=1") {
		t.Errorf("the read the engine answered is not the pinned one: %s — without --no-trunc it returns the "+
			"12-char short id and no row matches (W2-1)", got)
	}
	if got := w.ledgerWrites(); len(got) != 0 {
		t.Errorf("adoption wrote %v to the ledger; the row and the engine already agreed, so the honest sweep "+
			"writes nothing", got)
	}
	if cur, err := w.lg.GetProvision("prov-net"); err != nil || cur.State != store.ProvisionStateReady || cur.Handle != id {
		t.Errorf("the adopted row reads %q/%q, want ready/%s", cur.State, cur.Handle, id)
	}
	// The engine's own answer, not the report's: adopted means it is still there.
	w.mustStand("adopted", id)
	// Anti-vacuity on the read itself: the id the row stores is the id an
	// independent run of the same pinned argv answers for that name.
	var seen []string
	matched := false
	for _, h := range w.readNetworks(LabelManaged + "=1") {
		seen = append(seen, h.ID+"|"+h.Name)
		if h.Name == name && h.ID == id {
			matched = true
		}
	}
	if !matched {
		t.Errorf("the independent `network ls --no-trunc` answered %q and none of it is the created network "+
			"%s|%s — the adoption above was matched against something the engine does not report",
			strings.Join(seen, ", "), name, id)
	}
}

// ── (b) an unclaimed network is removed by the id the read ANSWERED ──────────

// Two labelled networks on the engine: one a live row names, one nobody's row
// claims. The sweep takes the second down and it comes down SPEAKING THE ID THE
// READ ANSWERED — the same handle a row would carry — asserted on the recorded
// argv token by token (so `docker rm <id>` cannot pass for `docker network rm
// <id>` on a substring) and then on the engine refusing `network inspect <id>`
// afterwards with its own not-found sentence. The claimed one stands, which is
// what keeps the removal from being a lane-wide flush.
func TestRealEngineSweepRemovesAnUnclaimedNetworkByTheIDTheReadAnswered(t *testing.T) {
	w := newRealSweepLane(t)
	_, claimed := w.network("claimed")
	orphanName, orphan := w.network("orphan")
	w.row("prov-net", store.ProvisionOwnerNetwork, claimed, store.ProvisionStateReady)

	// The id, read off the engine BEFORE the sweep by this file's own run of the
	// pinned argv: the removal must speak THIS value, the one `network ls
	// --no-trunc` answers for the orphan's name — not the name, not a short id.
	var readID string
	var seen []string
	for _, h := range w.readNetworks(LabelManaged + "=1") {
		seen = append(seen, h.ID+"|"+h.Name)
		if h.Name == orphanName {
			readID = h.ID
		}
	}
	if readID == "" {
		t.Fatalf("the pre-sweep `network ls --no-trunc` did not answer the orphan %s at all (%s); the "+
			"removal below could not have been matched to a read", orphanName, strings.Join(seen, ", "))
	}
	if readID != orphan {
		t.Fatalf("the read answered %s for %s while `network create` printed %s — the read and the create "+
			"do not speak the same handle (W2-1)", readID, orphanName, orphan)
	}

	rep := w.sweep()

	if rep.Removed != 1 {
		t.Errorf("the sweep removed %d resources where exactly one stood unclaimed on the engine, want 1: %+v", rep.Removed, rep)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows, want 1 (the row naming the claimed network): %+v", rep.Adopted, rep)
	}
	if rep.Failed != 0 {
		t.Errorf("the sweep failed %d rows a read answered: %+v", rep.Failed, rep)
	}
	rms := w.rmArgvs()
	if len(rms) != 1 {
		t.Fatalf("the sweep recorded %d `network rm` commands, want exactly 1: %v", len(rms), rms)
	}
	wantArgv(t, rms[0], networkRmArgv("docker", readID),
		"the unclaimed network came down by something other than the id `network ls --no-trunc` answered")
	if len(rms[0]) != 4 {
		t.Errorf("`network rm` was handed %d tokens (%v); the removal is one handle and nothing else — no "+
			"force, no second name", len(rms[0]), rms[0])
	}
	if got := strings.Join(rms[0][1:], " "); got != "network rm "+readID {
		t.Errorf("argv[1:] reads %q, want %q — a `container rm` of the right id removes a container, not this "+
			"network, and a removal by NAME is a handle no row carries", got, "network rm "+readID)
	}
	for _, tok := range rms[0] {
		if tok == orphanName {
			t.Errorf("the removal spoke the network's NAME (%s) rather than the id the read answered (%s) — "+
				"`network rm` takes a name as a convenience the engine never promised, and a sweep that takes "+
				"it cannot match its removals back to the ledger", orphanName, readID)
		}
	}
	// The engine's answer, not the sweep's report.
	w.mustBeGone("unclaimed", readID)
	w.mustStand("claimed", claimed)
	// Anti-vacuity: a re-ask of the same id is the engine's own not-found, so the
	// yes above was earned and not handed out.
	if err := w.rt.NetworkRemove(w.ctx, readID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a re-ask of the removed network answered %v; an engine that keeps saying yes makes the "+
			"removal above a tautology", err)
	}
	if cur, err := w.lg.GetProvision("prov-net"); err != nil || cur.State != store.ProvisionStateReady || cur.Handle != claimed {
		t.Errorf("the claimed row reads %q/%q after the sweep, want ready/%s", cur.State, cur.Handle, claimed)
	}
}

// ── (c) W2-11, on the engine: the network the sweep just finished survives it ─

// The `provisioning` network row is the daemon having died between the create and
// the ready mark; the sweep finishes it. One line later its orphan pass walks the
// same engine, finds that same network, and — before W2-11 — found no `ready` row
// naming it in the claim snapshot taken before the walk, so it removed the network
// it had just acquired and ended the pass holding a `ready` row pointing at
// nothing. Here the claim is measured on the engine: the finished network is still
// inspectable afterwards, the one `network rm` the sweep issued names the OTHER
// network, and a real orphan standing on the engine is what makes that "one" mean
// the orphan pass ran.
func TestRealEngineSweepNeverRemovesTheNetworkItJustFinished(t *testing.T) {
	w := newRealSweepLane(t)
	_, inflight := w.network("inflight")
	orphanName, orphan := w.network("orphan2")
	w.row("prov-net", store.ProvisionOwnerNetwork, inflight, store.ProvisionStateProvisioning)
	if _, words := w.inspect(inflight); !strings.Contains(words, inflight) {
		t.Fatalf("staging left the in-flight network off the engine (%s); neither claim below would mean anything", words)
	}

	rep := w.sweep()

	if rep.Finished != 1 {
		t.Errorf("the sweep finished %d rows, want 1 — the engine holds the network, so the row's only missing "+
			"half is its state: %+v", rep.Finished, rep)
	}
	if rep.Removed != 1 {
		t.Errorf("the sweep removed %d resources, want exactly 1 (the orphan beside it): %+v", rep.Removed, rep)
	}
	if rep.Failed != 0 || rep.Adopted != 0 {
		t.Errorf("the sweep reported failed=%d adopted=%d for a provisioning row it should finish: %+v", rep.Failed, rep.Adopted, rep)
	}
	cur, err := w.lg.GetProvision("prov-net")
	if err != nil {
		t.Fatalf("the finished row could not be read back: %v", err)
	}
	if cur.State != store.ProvisionStateReady || cur.Handle != inflight {
		t.Fatalf("the finished row reads %s/%q, want ready/%s — the handle is the receipt and the sweep must "+
			"not rewrite it", cur.State, cur.Handle, inflight)
	}
	rms := w.rmArgvs()
	if len(rms) != 1 {
		t.Fatalf("the sweep issued %d `network rm` where one orphan stood unclaimed on the engine: %v — with 0 "+
			"the orphan pass never ran and 'it kept its own network' would be vacuous", len(rms), rms)
	}
	wantArgv(t, rms[0], networkRmArgv("docker", orphan), "the sweep's only removal was not the unclaimed network")
	for _, argv := range rms {
		for _, tok := range argv {
			if tok == inflight {
				t.Errorf("the sweep issued `network rm %s` for the network it had just marked ready — the ledger "+
					"now carries a ready row pointing at nothing, which is the exact lie EV12 refuses (W2-1)", inflight)
			}
			if tok == orphanName {
				t.Errorf("the sweep removed the orphan by its NAME (%s) instead of the id the read answered (%s)", orphanName, orphan)
			}
		}
	}
	if got := w.ledgerWrites(); len(got) != 1 || got[0] != "MarkProvisionReady" {
		t.Errorf("the sweep wrote %v to the ledger, want [MarkProvisionReady] — the finish is the only verdict "+
			"this pass was entitled to give", got)
	}
	// The engine's answers: what it finished stays, what nobody claimed is gone.
	w.mustStand("in-flight", inflight)
	w.mustBeGone("unclaimed", orphan)
}

// ── (d) the authority rule beside them: no label, no touch ───────────────────

// The sweep's whole safety on a shared engine is that its reads are
// label-filtered: a network with no `aeon.managed` label never appears in what it
// is given, so it is never a candidate for removal in any branch. Measured here on
// the engine rather than in the fake's `Unlabelled` counter, which on a real
// engine is structurally 0 because the filtered read cannot see what it counts.
// The unlabelled probe is created by this call through the engine client directly
// — `networkArgv` deliberately cannot produce one — and is removed by id at
// cleanup, by this call alone.
func TestRealEngineSweepLeavesAnUnlabelledNetworkStanding(t *testing.T) {
	w := newRealSweepLane(t)
	unName, unID := w.unlabelledNetwork("probe")
	_, claimed := w.network("claimed2")
	w.row("prov-net", store.ProvisionOwnerNetwork, claimed, store.ProvisionStateReady)

	// Proof the probe really is on the engine, from the read the sweep does NOT
	// run: the unfiltered `network ls --no-trunc` answers it.
	var seen, filtered []string
	foundUnfiltered := false
	for _, h := range w.readNetworks("") {
		seen = append(seen, h.Name)
		if h.ID == unID {
			foundUnfiltered = true
		}
	}
	if !foundUnfiltered {
		t.Fatalf("the engine does not hold the unlabelled probe %s (%s) it just created; the claim below "+
			"would be about a resource that was never there", unName, strings.Join(seen, ", "))
	}
	for _, h := range w.readNetworks(LabelManaged + "=1") {
		filtered = append(filtered, h.ID+"|"+h.Name)
		if h.ID == unID {
			t.Fatalf("the engine answered the labelled read with an UNLABELLED network (%s %s) — the filter "+
				"is what makes every removal below safe, and it just failed", h.ID, h.Name)
		}
	}

	rep := w.sweep()

	if rep.Removed != 0 {
		t.Errorf("the sweep removed %d resources where the only unclaimed network on the engine carries no "+
			"aeon label: %+v — somebody else's resource is not this sweep's in any branch", rep.Removed, rep)
	}
	if rep.Adopted != 1 {
		t.Errorf("the sweep adopted %d rows, want 1 (the labelled row the engine answers): %+v", rep.Adopted, rep)
	}
	for _, argv := range w.rmArgvs() {
		for _, tok := range argv {
			if tok == unID || tok == unName {
				t.Errorf("the sweep asked the engine to remove the unlabelled probe %s (%v); the label filter "+
					"is the authority of every removal this file asserts", unName, argv)
			}
		}
	}
	w.mustStand("unlabelled probe", unID)
	w.mustStand("claimed", claimed)
	t.Logf("evidence: the unfiltered read answered %d network(s) %q and the filtered read %d %q",
		len(seen), strings.Join(seen, ","), len(filtered), strings.Join(filtered, ","))
}
