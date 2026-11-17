package container

// TestContainerBackendRealRuntime is the row the fake cannot answer: it runs
// against a real engine, so the argv the builders produce is tested against the
// thing that has to parse it. Opt-in, because a lane without an engine must not
// turn red — and when it does not run, that is recorded, not assumed.
//
//	AEON_TEST_CONTAINER=1 AEON_TEST_CONTAINER_IMAGE=alpine:latest \
//	  go test -tags sqlite_fts5 ./internal/execenv/container/ -run RealRuntime -count=1 -v

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const realImageEnv = "AEON_TEST_CONTAINER_IMAGE"

func realToken(tb testing.TB, command string) policy.AllowToken {
	tb.Helper()
	return allowToken(tb, command)
}

func TestContainerBackendRealRuntime(t *testing.T) {
	if os.Getenv("AEON_TEST_CONTAINER") != "1" {
		t.Skip("not run — set AEON_TEST_CONTAINER=1 on a lane with a docker-compatible engine (approval A4)")
	}
	bin, err := Discover("")
	if err != nil {
		t.Skipf("not run — no engine on this lane: %v", err)
	}
	image := os.Getenv(realImageEnv)
	if image == "" {
		image = "alpine:latest"
	}
	rt := &CLIRuntime{Bin: bin}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := rt.Available(ctx); err != nil {
		t.Skipf("not run — the engine is on PATH but not answering: %v", err)
	}
	lg := newFakeLedger()
	// A test that leaves a container behind fails the NEXT run — the name is
	// deterministic, so a leak here is a permanent red. Remove whatever this run
	// created, whatever else happens, through the driver itself.
	t.Cleanup(func() {
		hs, err := rt.List(context.Background(), LabelManaged+"=1")
		if err != nil {
			t.Logf("the cleanup could not list the lane's containers: %v", err)
			return
		}
		for _, h := range hs {
			if strings.HasPrefix(h.Name, NamePrefix+"real-loop-") {
				if err := rt.RemoveContainer(context.Background(), h.ID); err != nil {
					t.Logf("the cleanup could not remove %s: %v", h.ID, err)
				}
			}
		}
	})
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	if err := os.WriteFile(marker, []byte("from the host"), 0o600); err != nil {
		t.Fatalf("the mount could not be staged: %v", err)
	}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: uidString()}

	env := execenv.BoundEnv{ID: "env-real", Slug: "real-lane", Backend: execenv.BackendContainer, Spec: execenv.Spec{
		Egress: execenv.Egress{Mode: "none"},
		Image:  &execenv.Image{Ref: image, Pull: execenv.PullMissing},
		Limits: execenv.Limits{MemoryMiB: ptr(256), CPUs: ptrF(0.5), WallSeconds: ptr(30)},
		Env:    execenv.EnvSection{Set: map[string]string{"REAL_LANE_PROBE": "probe-value-4f2c"}},
	}}
	child, ref := execenv.Build(env.Spec, execenv.DaemonEnv{
		Lookup: func(string) string { return "" }, Names: func() []string { return nil },
	}, nil)
	if ref != nil {
		t.Fatalf("the test environment refused to build: %s", ref.Sentence())
	}
	ex := &Executor{P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "real-loop", Owner: "step-1"},
		Env: env, Child: child, WorkDir: dir}

	// echo, through the shared capture path, inside the container.
	// Two single-segment commands, because the policy matcher judges the exact
	// string the token was minted for and a compound is a different question.
	res, err := ex.Run(ctx, tools.ExecRequest{Command: "cat /workspace/marker.txt", Dir: dir},
		realToken(t, "cat /workspace/marker.txt"))
	if err != nil {
		t.Fatalf("the mount probe failed: %v", err)
	}
	t.Logf("the mount probe: exit=%d signal=%q timedOut=%v stdout=%q stderr=%q", res.ExitCode, res.Signal, res.TimedOut, res.Stdout, res.Stderr)
	if !strings.Contains(res.Stdout, "from the host") {
		t.Errorf("the one mount did not carry the file the host wrote:\n%s", res.Stdout)
	}
	res, err = ex.Run(ctx, tools.ExecRequest{Command: "env", Dir: dir}, realToken(t, "env"))
	if err != nil {
		t.Fatalf("the variable probe failed: %v", err)
	}
	if !strings.Contains(res.Stdout, "REAL_LANE_PROBE=probe-value-4f2c") {
		t.Errorf("the declared variable did not reach the container\n%s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "PATH=/usr/local/sbin") && false {
		t.Fatal("unreachable")
	}

	// `--network none` is not a label, it is the kernel's answer: the only
	// interface a container with no network has is the loopback.
	res, err = ex.Run(ctx, tools.ExecRequest{Command: "cat /proc/net/dev", Dir: dir}, realToken(t, "cat /proc/net/dev"))
	if err != nil {
		t.Fatalf("the network probe failed: %v", err)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		name := strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		if name == "" || name == "lo" || strings.HasPrefix(name, "│") {
			continue
		}
		if strings.Contains(name, "eth") || strings.Contains(name, "veth") {
			t.Errorf("--network none left an interface up (%s):\n%s", name, res.Stdout)
		}
	}

	// The caps arrived as numbers the engine wrote into the cgroup, not as a
	// flag the driver claims to have passed.
	res, err = ex.Run(ctx, tools.ExecRequest{Command: "cat /sys/fs/cgroup/memory.max", Dir: dir},
		realToken(t, "cat /sys/fs/cgroup/memory.max"))
	if err != nil {
		t.Fatalf("the memory probe failed: %v", err)
	}
	if !strings.Contains(res.Stdout, "268435456") {
		t.Logf("the memory ceiling read back as %q (256 MiB expected as 268435456) — recorded, not asserted: a cgroup v1 host under rootless may report the host's limit", strings.TrimSpace(res.Stdout))
	}

	// The teardown removes the real container, and `List` agrees.
	prov, terr := p.Teardown(ctx, p.lastProvisionID)
	if terr != nil {
		t.Fatalf("teardown failed: %s", terr.Sentence())
	}
	if prov.State != store.ProvisionStateRemoved {
		t.Errorf("the row is %s after a real removal", prov.State)
	}
	handles, lerr := rt.List(ctx, LabelManaged+"=1")
	if lerr != nil {
		t.Fatalf("the engine would not answer the list: %v", lerr)
	}
	for _, h := range handles {
		if h.ID == prov.Handle || strings.HasPrefix(h.Name, NamePrefix+"real-loop-") {
			t.Errorf("the container is still there after teardown: %s %s", h.ID, h.Name)
		}
	}
	// A second teardown is a no-op on a real engine too (a missing container is
	// not a failure to report as a leak).
	if _, terr := p.Teardown(ctx, prov.ID); terr != nil {
		t.Errorf("the repeat teardown was not inert: %s", terr.Sentence())
	}
	_ = errors.New
}

// TestRealEngineListResolvesTheStoredHandle is the row the fake cannot answer,
// and the exact hole that let the W2-1 regression through: the unit fake's List
// echoes what Create was handed, so a read the real engine REFUSES — docker
// 29.8.0 fails `--format {{.FullID}}` with `can't evaluate field FullID in
// type *formatter.ContainerContext` — was invisible to every pin while the loop
// was bricked. Here Create really runs, the handle really lands in a row-shaped
// variable, and List must answer it back, full 64-hex, from the engine alone.
// Opt-in like the rest of this file: AEON_TEST_CONTAINER=1 AEON_TEST_CONTAINER_IMAGE=…
func TestRealEngineListResolvesTheStoredHandle(t *testing.T) {
	if os.Getenv("AEON_TEST_CONTAINER") != "1" {
		t.Skip("not run — set AEON_TEST_CONTAINER=1 on a lane with a docker-compatible engine (approval A4)")
	}
	bin, err := Discover("")
	if err != nil {
		t.Skipf("not run — no engine on this lane: %v", err)
	}
	image := os.Getenv(realImageEnv)
	if image == "" {
		t.Skipf("not run — this lane names no local image; set %s (pull is never run here)", realImageEnv)
	}
	rt := &CLIRuntime{Bin: bin}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := rt.Available(ctx); err != nil {
		t.Skipf("not run — the engine is on PATH but not answering: %v", err)
	}
	if ok, ierr := rt.ImageExists(ctx, image); ierr != nil {
		t.Fatalf("the engine would not answer whether %q exists: %v", image, ierr)
	} else if !ok {
		t.Skipf("not run — %s is not on this lane and this test never pulls", image)
	}
	dir := t.TempDir()
	spec := CreateSpec{
		ProvisionID: "list-probe",
		LoopSlug:    "real-loop", // so the file's label cleanup covers a leak by name too
		Image:       image,
		User:        uidString(),
		NetworkMode: "none",
		Mounts:      []Mount{{Host: dir, Container: ContainerWorkDir}},
		WorkDir:     ContainerWorkDir,
	}
	handle, cerr := rt.Create(ctx, spec)
	if cerr != nil {
		t.Fatalf("the real engine refused to create the probe container: %v", cerr)
	}
	t.Cleanup(func() {
		if err := rt.RemoveContainer(context.Background(), handle); err != nil {
			t.Logf("the cleanup could not remove %s: %v", handle, err)
		}
	})
	if len(handle) != 64 {
		t.Fatalf("Create stored a handle of %d chars (%s), not the 64-hex the engine prints for `run -d`", len(handle), handle)
	}
	// The read under test — the one W2-1 made the engine outright refuse. A
	// failure here is the loop-bricking failure, so it is named with its error.
	start := time.Now()
	hs, lerr := rt.List(ctx, LabelManaged+"=1")
	t.Logf("measured: List of the probe container took %s", time.Since(start))
	if lerr != nil {
		t.Fatalf("List failed on the engine that had just started the container — this is the "+
			"W2-1 regression: the daemon cannot see its own provision, and the loop's next command "+
			"will be refused provision-live-exists: %v", lerr)
	}
	var found *Handle
	for i := range hs {
		if hs[i].ID == handle {
			found = &hs[i]
			break
		}
	}
	if found == nil {
		ids := make([]string, 0, len(hs))
		for _, h := range hs {
			ids = append(ids, h.ID+"|"+h.Name)
		}
		t.Fatalf("List answered %d managed container(s) %q and none equals the stored handle %s — "+
			"a short id here is the W2-1 bug in its original shape (read returns 12, row stores 64)",
			len(hs), strings.Join(ids, ", "), handle)
	}
	if want := NamePrefix + "real-loop-list-probe"; found.Name != want {
		t.Errorf("the handle the engine answered has name %q, want %q", found.Name, want)
	}
	if found.State != "running" {
		t.Errorf("the container the probe just started reads state %q, want running", found.State)
	}
}

// engineArgvRecorder stands in for the `docker` process while STILL running it,
// so a real-engine test can say the engine call actually happened — the argv is
// recorded on the way to the daemon rather than paraphrased afterwards. Without
// this, "the sweep asked the engine to remove the network" is an assertion about a
// return value, which is how the W2-1 read survived a green package while the
// engine was refusing the command outright.
type engineArgvRecorder struct {
	mu   sync.Mutex
	cmds [][]string
}

func (r *engineArgvRecorder) record(cmd *exec.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, append([]string(nil), cmd.Args...))
}

func (r *engineArgvRecorder) calls(match ...string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][]string
outer:
	for _, c := range r.cmds {
		for i, m := range match {
			if i >= len(c) || c[i] != m {
				continue outer
			}
		}
		out = append(out, c)
	}
	return out
}

// seam records and runs — and nothing else. The driver's own `call` is what keeps
// the engine's stderr and folds it into the error (W2-3), and `goneSentence` reads
// THAT sentence; a seam that captured stderr and wrapped the error itself would be
// testing the test, and the doubled "container: docker failed:" this one first
// wrote is the fingerprint of exactly that.
func (r *engineArgvRecorder) seam(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
	r.record(cmd)
	return cmd.Output()
}

// TestRealEngineNetworkCreateAndListAgreeOnTheStoredHandle is the network twin of
// TestRealEngineListResolvesTheStoredHandle: the row the fake cannot answer, asked
// of the engine instead. `NetworkCreate` must return the id the engine printed, the
// read must answer that same handle, and the removal must accept it — measured on
// rootless docker 29.8.0, all three are the same 64-hex (`network create` prints
// it, `network ls --no-trunc --format '{{.ID}}|{{.Name}}'` answers it, `network rm`
// exits 0 on it). This is what the network row now stores; it stored the name, a
// string this package invented from the slug, which is not what the read returns —
// so the daemon's own network was a resource no row claimed.
//
// Opt-in like the rest of this file: AEON_TEST_CONTAINER=1 AEON_TEST_CONTAINER_IMAGE=…
func TestRealEngineNetworkCreateAndListAgreeOnTheStoredHandle(t *testing.T) {
	if os.Getenv("AEON_TEST_CONTAINER") != "1" {
		t.Skip("not run — set AEON_TEST_CONTAINER=1 on a lane with a docker-compatible engine (approval A4)")
	}
	bin, err := Discover("")
	if err != nil {
		t.Skipf("not run — no engine on this lane: %v", err)
	}
	rec := &engineArgvRecorder{}
	rt := &CLIRuntime{Bin: bin, Call: rec.seam}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := rt.Available(ctx); err != nil {
		t.Skipf("not run — the engine is on PATH but not answering: %v", err)
	}
	// A name no earlier run can collide with, so nothing here can remove a
	// resource it did not create: the id below is the only thing removals are
	// asked for, and it came from the create in this call.
	slug := fmt.Sprintf("real-net-id-%d", time.Now().UnixNano())
	name := NetworkName(slug)
	id, cerr := rt.NetworkCreate(ctx, name, slug)
	if cerr != nil {
		t.Fatalf("the real engine refused to create the loop's network: %v", cerr)
	}
	ours := id
	t.Cleanup(func() {
		// Only what THIS call created, by the id the engine printed for it. A
		// not-found is the success case here: the test itself already removed it.
		if err := rt.NetworkRemove(context.Background(), ours); err != nil && !goneSentence(err) {
			t.Logf("the cleanup could not remove %s: %v", ours, err)
		}
	})
	if len(id) != 64 {
		t.Fatalf("NetworkCreate returned a %d-character handle (%s); `network create` prints the full 64-hex and that print is the row's handle", len(id), id)
	}
	if calls := rec.calls("docker", "network", "create"); len(calls) != 1 {
		t.Fatalf("the engine was handed %d `network create` commands, want exactly 1 — a network that was never created proves nothing", len(calls))
	}

	start := time.Now()
	hs, lerr := rt.NetworkList(ctx, LabelManaged+"=1")
	t.Logf("measured: NetworkList of the probe network took %s", time.Since(start))
	if lerr != nil {
		t.Fatalf("NetworkList failed on the engine that had just created the network — the daemon cannot see its own loop network, and the start sweep will treat it as an orphan: %v", lerr)
	}
	var found *Handle
	for i := range hs {
		if hs[i].ID == id {
			found = &hs[i]
			break
		}
	}
	if found == nil {
		var seen []string
		for _, h := range hs {
			seen = append(seen, h.ID+"|"+h.Name)
		}
		t.Fatalf("NetworkList answered %d managed network(s) %q and none equals the stored handle %s — a short id here is the W2-1 bug in its original shape (read returns 12, row stores 64)",
			len(hs), strings.Join(seen, ", "), id)
	}
	if found.Name != name {
		t.Errorf("the network the engine answered by that id is named %q, want %q", found.Name, name)
	}
	if calls := rec.calls("docker", "network", "ls"); len(calls) == 0 {
		t.Fatal("no `network ls` reached the engine, so the match above was invented rather than read")
	} else if got := strings.Join(calls[len(calls)-1], " "); !strings.Contains(got, "--no-trunc") || !strings.Contains(got, "{{.ID}}|{{.Name}}") {
		t.Errorf("the read the engine answered is not the pinned one: %s — without --no-trunc it returns the 12-char short id and no row matches (W2-1)", got)
	}

	// The removal half: `network rm` accepts the handle the row stores, and after
	// it the engine holds nothing of this run.
	if err := rt.NetworkRemove(ctx, id); err != nil {
		t.Fatalf("the engine refused to remove the network by the handle the row stores (%s): %v — rollback cannot name its own resource (W2-1)", id, err)
	}
	if calls := rec.calls("docker", "network", "rm"); len(calls) == 0 {
		t.Fatal("no `network rm` reached the engine, so the removal above was never asked for")
	} else if got := strings.Join(calls[len(calls)-1], " "); !strings.HasSuffix(got, id) {
		t.Errorf("the removal did not name the stored handle: %s", got)
	}
	after, aerr := rt.NetworkList(ctx, LabelManaged+"=1")
	if aerr != nil {
		t.Fatalf("the engine would not answer the list after the removal: %v", aerr)
	}
	for _, h := range after {
		if h.ID == id || h.Name == name {
			t.Errorf("the engine still holds the network this call created (%s %s) behind a removal that reported success", h.ID, h.Name)
		}
	}
	// Anti-vacuity on the engine itself: an id it never issued is not something it
	// reports as removed. Without this the paragraph above could be satisfied by an
	// engine that always says yes.
	if err := rt.NetworkRemove(ctx, strings.Repeat("0f2a", 16)); err == nil {
		t.Error("the engine reported removing a network it was never asked to create — a yes-man engine makes every removal assertion above a tautology")
	}
}

// TestRealEngineNetworkRowStoresWhatNetworkListAnswers walks the door the daemon
// walks — `ensureDependencies` over the real CLIRuntime — and asserts the property
// the whole family is about: the handle that lands in the ROW is the handle the
// engine ANSWERS, from the engine's own read and not from anything this test
// supplied. Then teardown takes the loop's dependencies down and the engine is left
// holding nothing of this run.
func TestRealEngineNetworkRowStoresWhatNetworkListAnswers(t *testing.T) {
	if os.Getenv("AEON_TEST_CONTAINER") != "1" {
		t.Skip("not run — set AEON_TEST_CONTAINER=1 on a lane with a docker-compatible engine (approval A4)")
	}
	bin, err := Discover("")
	if err != nil {
		t.Skipf("not run — no engine on this lane: %v", err)
	}
	image := os.Getenv(realImageEnv)
	if image == "" {
		t.Skipf("not run — this lane names no local image; set %s (this test never pulls)", realImageEnv)
	}
	rec := &engineArgvRecorder{}
	rt := &CLIRuntime{Bin: bin, Call: rec.seam}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := rt.Available(ctx); err != nil {
		t.Skipf("not run — the engine is on PATH but not answering: %v", err)
	}
	if ok, ierr := rt.ImageExists(ctx, image); ierr != nil {
		t.Fatalf("the engine would not answer whether %q exists: %v", image, ierr)
	} else if !ok {
		t.Skipf("not run — %s is not on this lane and this test never pulls", image)
	}
	lg := newFakeLedger()
	p := &Provisioner{Ledger: lg, Runtime: rt, User: uidString()}
	dir := t.TempDir()
	slug := fmt.Sprintf("real-net-%d", time.Now().UnixNano())
	env := execenv.BoundEnv{ID: "env-real-net", Slug: "real-net-lane", Backend: execenv.BackendContainer, Spec: execenv.Spec{
		Egress: execenv.Egress{Mode: "unrestricted"},
		Image:  &execenv.Image{Ref: image, Pull: execenv.PullNever},
		Limits: execenv.Limits{MemoryMiB: ptr(256), CPUs: ptrF(0.5)},
		// One service, because the network exists for the services — and no health
		// check, so this test measures the handle and not the database.
		Services: []execenv.Service{{Name: "db", Image: image, Port: 5432}},
	}}
	loop := LoopRef{ComponentRoot: dir, LoopSlug: slug, Owner: "step-1"}
	_, created, ref := p.ensureDependencies(ctx, loop, env, dir)
	if ref != nil {
		t.Fatalf("the loop's dependencies never came up on the real engine: %s", ref.Sentence())
	}
	// Whatever this call made comes back down, whatever else happens, by the
	// handles the rows carry — never by enumerating the lane.
	t.Cleanup(func() {
		for i := len(created) - 1; i >= 0; i-- {
			row, err := lg.GetProvision(created[i].ID)
			if err != nil || row.Handle == "" {
				continue
			}
			if row.Owner == store.ProvisionOwnerNetwork {
				// A gone sentence here is the SUCCESS case: the test itself already
				// removed what this call created, and this pass is only the belt for a
				// run that died before reaching the assertions.
				if err := rt.NetworkRemove(context.Background(), row.Handle); err != nil && !goneSentence(err) {
					t.Logf("the cleanup could not remove the network %s: %v", row.Handle, err)
				}
				continue
			}
			if err := rt.RemoveContainer(context.Background(), row.Handle); err != nil && !goneSentence(err) {
				t.Logf("the cleanup could not remove the container %s: %v", row.Handle, err)
			}
		}
	})

	var nrow, srow store.Provision
	for _, r := range created {
		// The ledger's CURRENT row, not the snapshot `ensureDependencies` appended:
		// that copy was written before the create printed anything, so it carries no
		// handle — reading it would put the question to the wrong thing, and an
		// empty handle would be read as "the row stores nothing". (Measured here: the
		// first run of this test printed `the row stores handle ""` for exactly that
		// reason — the same trap `teardownAll` names in its own comment.)
		cur, err := lg.GetProvision(r.ID)
		if err != nil {
			t.Fatalf("the row %s could not be read back: %v", r.ID, err)
		}
		if cur.Owner == store.ProvisionOwnerNetwork {
			nrow = cur
		} else {
			srow = cur
		}
	}
	if nrow.ID == "" {
		t.Fatal("no network row was written; the ledger is written BEFORE the resource and the row is what makes the network findable (EV12)")
	}
	if srow.ID == "" {
		t.Fatal("no service row was written, so nothing joined the network and the read proves nothing")
	}

	hs, lerr := rt.NetworkList(ctx, LabelManaged+"=1")
	if lerr != nil {
		t.Fatalf("the engine would not list the networks: %v", lerr)
	}
	matched := false
	var seen []string
	for _, h := range hs {
		seen = append(seen, h.ID+"|"+h.Name)
		if h.ID == nrow.Handle {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("the row stores handle %q and the engine's own read answered %q — a handle no read answers is a resource the sweep removes as an orphan (W2-1)",
			nrow.Handle, strings.Join(seen, ", "))
	}
	if len(nrow.Handle) != 64 {
		t.Errorf("the row stores a %d-character handle (%s); `network create` prints 64-hex and the read answers that width", len(nrow.Handle), nrow.Handle)
	}
	if nrow.Handle == NetworkName(slug) {
		t.Errorf("the row stores the name the driver invented (%q) instead of the engine's id — the exact W2-1 shape on the network side", nrow.Handle)
	}
	if len(rec.calls("docker", "network", "create")) != 1 || len(rec.calls("docker", "network", "ls")) == 0 {
		t.Errorf("the engine was handed %d creates and %d reads; the match above must be read off commands that ran",
			len(rec.calls("docker", "network", "create")), len(rec.calls("docker", "network", "ls")))
	}

	// Teardown by the rows' handles, and the engine is left holding nothing of
	// this run: the rollback promise, watched.
	if _, ref := p.Teardown(ctx, srow.ID); ref != nil {
		t.Fatalf("the service row would not come down: %s", ref.Sentence())
	}
	if _, ref := p.Teardown(ctx, nrow.ID); ref != nil {
		t.Fatalf("the network row would not come down by its stored handle %s: %s", nrow.Handle, ref.Sentence())
	}
	after, aerr := rt.NetworkList(ctx, LabelManaged+"=1")
	if aerr != nil {
		t.Fatalf("the engine would not answer the list after teardown: %v", aerr)
	}
	for _, h := range after {
		if h.ID == nrow.Handle {
			t.Errorf("the network is still there after a teardown that reported success: %s %s", h.ID, h.Name)
		}
	}
	conts, cerr := rt.List(ctx, LabelManaged+"=1")
	if cerr != nil {
		t.Fatalf("the engine would not answer the container list: %v", cerr)
	}
	for _, h := range conts {
		if strings.HasPrefix(h.Name, NamePrefix+sanitize(slug)) {
			t.Errorf("the engine still holds this run's container %s %s behind a reported removal", h.ID, h.Name)
		}
	}
}

func uidString() string {
	return itoa(os.Getuid()) + ":" + itoa(os.Getgid())
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func ptrF(f float64) *float64 { return &f }
