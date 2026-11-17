package daemon

// execenv_sweep_test.go — S59 EV22: the idle-provision sweep, driven directly.
//
// EV22 promises that a container provision which has been `ready` and unused
// past `execenv.provisionIdleTTL` comes down: the ledger walks
// `provisioning → ready → tearing_down → removed` and the container is removed
// from the engine. What has never been proven is that the sweep RUNS. Its only
// callers are (measured: `grep -rn SweepIdleProvisions --include=*.go .`):
//
//   - internal/daemon/execenv_reconcile.go:398 — inside StartEnvironmentReconcile,
//     the ticker goroutine (the function at execenv_reconcile.go:386), and
//   - cmd/aeon/serve.go:485 — `d.StartEnvironmentReconcile(recoveryCtx, time.Hour)`,
//     the ONLY production entry to that ticker (beside it, serve.go:479 calls the
//     start-of-daemon ReconcileEnvironments, a different sweep).
//
// So the ticker's interval is ONE HOUR and nothing has ever observed a tick:
// eight daemon logs from real walks contain zero `environment sweep` lines while
// a ready row aged past a 1-minute TTL. These tests drive the sweep itself, and
// then drive the ticker, so both halves of the claim have a witness.
//
// The sweep is `(*Daemon).SweepIdleProvisions(ctx)` (execenv_reconcile.go:210).
// What decides "idle" is read out of the code, not invented here — three
// conditions, all required:
//
//   - `row.Backend == execenv.BackendContainer` (:222) — a host row holds no
//     resource, so there is nothing to sweep;
//   - `d.loopBlocked(row)` (:225, the gate at :315) — the LOOP reads `blocked`.
//     A `running`/`done`/`aborted`/unreadable loop is never swept, and an unknown
//     status does not earn anybody's container removal; and
//   - `time.Since(at) >= ttl` (:242) where `at` is `ready_at`, falling back to
//     `created_at` when the row was never marked ready (:228-231), the TTL read
//     through `config.Reader` (`provisionIdleTTL`, :262) and never hard-coded.
//     A `ready_at` that will not parse leaves the row alone and says so (:238).
//
// Removal goes through the driver, not a hand-written DELETE: `p.Teardown` →
// `container.Provisioner.teardownRow` (provision.go:312), which marks
// `tearing_down` BEFORE the engine call and marks `removed` only after the
// engine answered — and `failed`, with the engine's sentence, when it did not.
//
// The engine stands in for docker the only way the sweep allows. The sweep
// builds its own provisioner: `&container.CLIRuntime{Bin:
// os.Getenv("AEON_CONTAINER_BIN")}` (execenv_reconcile.go:41), with no injected
// `Call` seam. So the fake is a fake ENGINE BINARY behind that knob — the
// stronger witness, not the weaker one: the argv recorded is the one `docker rm`
// itself would have been handed, built by the real driver
// (cli_runtime.go:393), not by a Go stub that could agree with anything.
//
// THE GAP THIS FILE CANNOT CLOSE is named at the bottom, beside
// TestTheIdleSweepRunsOnItsOwnTicker.
//
// # MEASURED STATE: this file was RED against the shipped sweep. It is GREEN
// against the fixed one, and the measurement below is the pre-fix record.
//
// Measured 2026-11-04 on this lane, against the sweep as it then stood:
//
//	SweepIdleProvisions -> n=0 err=<nil> ; engine calls=[] ; log=[]
//
// on a store holding exactly one `ready` container provision whose loop reads
// `blocked` and whose ready_at is three hours old against a 10m TTL. The cause
// is one line, and it is the same defect class this file already documents at
// :299 (W2-2) — a ledger keyed by SLUG read through a door keyed by the loop's
// HEX ID:
//
//	execenv_reconcile.go:282  pin, err := d.store.ReadPinnedLoop(slug)
//
// `slug` is `row.LoopSlug` (:226); `ReadPinnedLoop` is `WHERE id=?`
// (store/blueprint.go:219). Measured on the same row:
//
//	ReadPinnedLoop(slug="s59-probe") -> status="" err=store: not found
//	ReadPinnedLoop(id="059d60497eb0c7f7f5ebd59d3ef31f69") -> status="blocked" err=<nil>
//	d.loopBlocked(slug) = false      d.loopBlocked(id) = true
//
// So `loopBlocked` was false for every row a real daemon held, the sweep
// `continue`d past all of them at :227, removed nothing, logged nothing and
// returned a nil error. EV22 was therefore not merely unproven — it was FALSE
// as shipped: an idle container is never swept, and the daemon says nothing
// while it accumulates them. That is why the eight real-walk logs contain no
// `environment sweep` line at any TTL; the hourly ticker is a second reason, not
// the first.
//
// FIXED 2026-11-04, same lane. The gate now takes the ROW and resolves the pair
// the row itself carries: `(*store.Store).GetLoop(store.Scope{LoopSlug,
// ComponentRoot})` — the store's own (slug, component root) door (loops.go:297),
// which is how the ledger is keyed — and compares the loop row's own `status` to
// `blocked`, which is exactly what loop.FromPinned copied (state.go:198) minus
// the dependency on a blueprint pin an ordinary `loop.create` loop does not
// have. The pair, not the slug alone: a slug is unique per component root and
// never globally, so LoopsBySlug's SET is not an answer. Every branch of the
// gate speaks, and an unresolvable loop is NOT eligible for teardown. All six
// tests here pass on that code, and NOTHING below this paragraph's prose
// changed: the control rows, the `rm --force <handle>` argv witness, the
// settle-once claim and the tearing-down-before-the-engine claim are the ones
// this file shipped with.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/store"
)

// sweepRoot is the ledger's component root for every row here. The sweep never
// reads it; it is part of the row's identity (and of the one-live-row-per-owner
// index), so it is a real path-shaped string rather than a decoration.
const sweepRoot = "/tmp/s59-sweep"

// sweepBlueprint is the blueprint the fixture's loops carry, because that is the
// shape a loop that ever held a container really has: a loop is provisioned by a
// step of its pinned blueprint (loop/state.go:198 reads the pinned content back
// and refuses unparseable bytes, so the pin has to be real). The gate itself
// reads the loop row's STATUS (loopBlocked), so this content is the fixture's
// fidelity, not the sweep's input — a loop nobody pinned stays unresolvable, and
// unresolvable means its container is not removed.
const sweepBlueprint = "id: s59-sweep\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default}\n"

// fakeEngine is docker, as far as the sweep is concerned: a binary on the
// AEON_CONTAINER_BIN knob that records the argv it was handed and answers what
// the test asks for. Note the engine client's own environment is PATH and HOME
// only (cliProcessEnv, cli_runtime.go:269), so nothing can be passed to it by
// env var — the answers are compiled into the script.
type fakeEngine struct {
	bin string
	log string
}

func newFakeEngine(t *testing.T, exit int, stderr string, delay time.Duration) *fakeEngine {
	t.Helper()
	dir := t.TempDir()
	e := &fakeEngine{bin: filepath.Join(dir, "fake-engine"), log: filepath.Join(dir, "engine-argv.log")}
	shq := func(s string) string {
		if strings.ContainsAny(s, "'\n\x00") {
			t.Fatalf("the fake engine cannot carry %q — it is quoted into a shell script", s)
		}
		return "'" + s + "'"
	}
	body := "#!/bin/sh\n" +
		// One line per call, arguments separated by 0x1f: the argv `docker` saw.
		"{ for a in \"$@\"; do printf '%s\\037' \"$a\"; done; printf '\\n'; } >> " + shq(e.log) + "\n"
	if delay > 0 {
		body += fmt.Sprintf("sleep %.3f\n", delay.Seconds())
	}
	if stderr != "" {
		body += "printf '%s\\n' " + shq(stderr) + " 1>&2\n"
	}
	body += fmt.Sprintf("exit %d\n", exit)
	if err := os.WriteFile(e.bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write the fake engine: %v", err)
	}
	return e
}

// calls is every argv the engine was handed, in order. An absent log means the
// engine was never spoken to, which several assertions below want to hear.
func (e *fakeEngine) calls(t *testing.T) [][]string {
	t.Helper()
	b, err := os.ReadFile(e.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the engine's argv log: %v", err)
	}
	var out [][]string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if n := len(parts); n > 0 && parts[n-1] == "" {
			parts = parts[:n-1]
		}
		out = append(out, parts)
	}
	return out
}

// sweepLogger is the daemon's Logger (daemon.go:224) with a mutex, because the
// ticker test reads these lines while the sweep's goroutine writes them.
type sweepLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *sweepLogger) Infof(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *sweepLogger) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func (l *sweepLogger) has(substrs ...string) bool {
	joined := strings.Join(l.all(), "\n")
	for _, s := range substrs {
		if !strings.Contains(joined, s) {
			return false
		}
	}
	return true
}

type sweepFixture struct {
	store  *store.Store
	d      *Daemon
	engine *fakeEngine
	log    *sweepLogger
	envID  string
	ttl    time.Duration
}

// newSweepFixture wires a store-backed daemon whose sweep interval is the
// setting the operator would have set, read the declared way (through
// config.Reader, the row winning over the environment), and whose engine is the
// fake. A TTL is required: a test that leaned on the 24h default would be
// asserting the fallback rather than the setting.
func newSweepFixture(t *testing.T, ttl string, engine *fakeEngine) *sweepFixture {
	t.Helper()
	f := newEnvFixture(t)
	env := f.containerEnv(t)
	settings := NewSettings(f.store, config.Options{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if _, _, err := settings.Set(context.Background(), provisionIdleTTLSetting, ttl, "user:1"); err != nil {
		t.Fatalf("set %s=%s: %v", provisionIdleTTLSetting, ttl, err)
	}
	d, err := time.ParseDuration(ttl)
	if err != nil {
		t.Fatalf("the test's own TTL %q: %v", ttl, err)
	}
	t.Setenv("AEON_CONTAINER_BIN", engine.bin)
	lg := &sweepLogger{}
	return &sweepFixture{
		store:  f.store,
		d:      &Daemon{store: f.store, log: lg, settings: settings},
		engine: engine,
		log:    lg,
		envID:  env.ID,
		ttl:    d,
	}
}

// provisionFor writes the shape EV22 ages out: a pinned loop with a status, and
// a `ready` container row carrying a runtime handle, aged `ago` into the past.
// `status` "" leaves the loop at its created status (`active`), which is
// neither blocked nor running — a fourth case the gate must also refuse.
func (f *sweepFixture) provisionFor(t *testing.T, slug, status, backend, handle string, ago time.Duration) store.Provision {
	t.Helper()
	if _, err := f.store.CreatePinnedLoop(store.NewLoop{
		Slug: slug, ComponentRoot: sweepRoot, Root: sweepRoot, Goal: "hold a container",
	}, "s59-sweep", 1, sweepBlueprint, "{}"); err != nil {
		t.Fatalf("pin the loop %s: %v", slug, err)
	}
	if status != "" {
		if err := f.store.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: sweepRoot}, status); err != nil {
			t.Fatalf("set the status of %s: %v", slug, err)
		}
	}
	row, ref, err := f.store.CreateProvision(store.NewProvision{
		EnvironmentID: f.envID, ComponentRoot: sweepRoot, LoopSlug: slug, Backend: backend,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the provision for %s: ref=%v err=%v", slug, ref, err)
	}
	if handle == "" {
		return row
	}
	ready, ref, err := f.store.MarkProvisionReady(row.ID, handle)
	if ref != nil || err != nil {
		t.Fatalf("mark %s ready: ref=%v err=%v", row.ID, ref, err)
	}
	if ago > 0 {
		f.age(t, ready.ID, ago)
	}
	return ready
}

// age moves `ready_at` into the past. There is no store door that ages a row —
// the column is written once by MarkProvisionReady — so the state EV22 is about
// ("a container nobody has used for hours") is staged by writing the column the
// sweep reads, in the store's own isoUTC format (open.go:303).
func (f *sweepFixture) age(t *testing.T, id string, ago time.Duration) {
	t.Helper()
	stamp := time.Now().UTC().Add(-ago).Format("2006-01-02T15:04:05.000Z")
	db, err := f.store.GormDB().DB()
	if err != nil {
		t.Fatalf("the raw handle for the ageing write: %v", err)
	}
	res, err := db.ExecContext(context.Background(),
		`UPDATE environment_provision SET ready_at = ? WHERE id = ?`, stamp, id)
	if err != nil {
		t.Fatalf("age %s to %s: %v", id, stamp, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("ageing %s touched %d rows (err %v), want 1", id, n, err)
	}
}

// setReadyAt writes the column verbatim, for the unreadable-age branch.
func (f *sweepFixture) setReadyAt(t *testing.T, id, raw string) {
	t.Helper()
	db, err := f.store.GormDB().DB()
	if err != nil {
		t.Fatalf("the raw handle: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE environment_provision SET ready_at = ? WHERE id = ?`, raw, id); err != nil {
		t.Fatalf("set ready_at of %s to %q: %v", id, raw, err)
	}
}

// TestSweepIdleProvisionsRemovesTheIdleContainerAndSpeaks is EV22: the row
// moves to `removed`, the engine was handed the removal, and the daemon SAID it
// ran. The third assertion is the one this slice keeps learning the cost of: the
// terminal-teardown hook stayed dead for two slices because it never spoke (see
// terminalProvisions, execenv_reconcile.go:299).
func TestSweepIdleProvisionsRemovesTheIdleContainerAndSpeaks(t *testing.T) {
	eng := newFakeEngine(t, 0, "", 0)
	f := newSweepFixture(t, "10m", eng)
	handle := "9f2c1d7b4e6a8035c1b9d2f7a4e86c05b3d1f9a7e2c4680b5d7f1a3c9e04b6d2"
	row := f.provisionFor(t, "s59-idle", "blocked", "container", handle, 3*time.Hour)
	if row.State != store.ProvisionStateReady || row.ReadyAt == nil {
		t.Fatalf("the fixture is not a ready row with a ready_at: %+v", row)
	}

	n, err := f.d.SweepIdleProvisions(context.Background())
	if err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("the sweep removed %d provisions, want 1 — the row is a blocked loop's ready container aged 3h "+
			"past a 10m TTL. See the header: execenv_reconcile.go:282 hands a loop SLUG to ReadPinnedLoop, which is "+
			"keyed on the loop's hex id (store/blueprint.go:219), so loopBlocked is false for every real row", n)
	}

	after, err := f.store.GetProvision(row.ID)
	if err != nil {
		t.Fatalf("re-read the row: %v", err)
	}
	if after.State != store.ProvisionStateRemoved {
		t.Errorf("state = %q, want %q — the sweep's whole promise is that the row settles (EV12's vocabulary)",
			after.State, store.ProvisionStateRemoved)
	}
	if after.RemovedAt == nil || *after.RemovedAt == "" {
		t.Errorf("a sweep-removed row carries no removed_at: the ledger cannot tell removed from never-created (EV12)")
	}

	calls := eng.calls(t)
	if len(calls) != 1 {
		t.Fatalf("the engine was called %d times, want exactly one removal: %v", len(calls), calls)
	}
	if want := []string{"rm", "--force", handle}; !reflect.DeepEqual(calls[0], want) {
		t.Errorf("the engine was handed %v, want %v — the container is removed by its handle, or it is not removed",
			calls[0], want)
	}

	if !f.log.has("environment sweep:", "1 idle container(s) removed", f.ttl.String()) {
		t.Errorf("the sweep removed a container without saying so. Lines: %q — an operator reading the daemon log "+
			"has to be able to tell the sweep ran, which is exactly what eight real-walk logs could not show",
			strings.Join(f.log.all(), " | "))
	}

	// A second sweep has nothing left to do: the row is out of the live set and
	// the engine is not asked to remove the same container twice.
	if n2, err := f.d.SweepIdleProvisions(context.Background()); err != nil || n2 != 0 {
		t.Errorf("the second sweep removed %d (err %v), want 0 — settle-once", n2, err)
	}
	if got := len(eng.calls(t)); got != 1 {
		t.Errorf("the engine was called %d times after a second sweep, want 1", got)
	}
}

// TestSweepIdleProvisionsTouchesWhatItMustNot is the gate, one case per
// condition the sweep checks: inside the TTL, on a loop that is not blocked, on
// a backend that holds nothing, and at an age nobody can establish. Each is a
// container a bug would take down while somebody was still using it.
//
// Every case carries a CONTROL row in the same store and the same sweep: a
// blocked loop's `ready` container row aged 3h past the 10m TTL, which the sweep
// must remove. Without it, a sweep that removes nothing at all — which is what
// it does today, see the header — would turn all six cases GREEN while having
// tested nothing. A gate test that cannot tell "refused correctly" from "did
// nothing" is not a gate test.
func TestSweepIdleProvisionsTouchesWhatItMustNot(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		backend string
		ago     time.Duration
		stamp   string // "" = leave ready_at as the store wrote it
		want    string
	}{
		{name: "inside the TTL", status: "blocked", backend: "container", ago: 2 * time.Minute,
			want: "a container younger than execenv.provisionIdleTTL is the one a step may still come back to"},
		{name: "loop running", status: "running", backend: "container", ago: 3 * time.Hour,
			want: "a running loop's container is in use; loopBlocked is the gate (execenv_reconcile.go:278)"},
		{name: "loop done", status: "done", backend: "container", ago: 3 * time.Hour,
			want: "a finished loop is the terminal hook's job, not the sweep's"},
		{name: "loop never blocked", status: "", backend: "container", ago: 3 * time.Hour,
			want: "an unknown status is not blocked — an unreadable loop row does not earn a removal"},
		{name: "host backend", status: "blocked", backend: "host-scrubbed", ago: 0,
			want: "a host row holds no engine resource; there is nothing to remove"},
		{name: "unreadable age", status: "blocked", backend: "container", ago: 0, stamp: "yesterday",
			want: "a row whose timestamp will not parse has an age nobody can establish; removing it is a guess"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := newFakeEngine(t, 0, "", 0)
			f := newSweepFixture(t, "10m", eng)
			slug := strings.ReplaceAll(tc.name, " ", "-")
			handle := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678901234567890abcdef12345678"
			if tc.backend == "host-scrubbed" {
				handle = "" // a host row never reaches `ready`: the store demands a handle for that
			}
			row := f.provisionFor(t, "s59-keep-"+slug, tc.status, tc.backend, handle, tc.ago)
			if tc.stamp != "" {
				f.setReadyAt(t, row.ID, tc.stamp)
			}
			controlHandle := "5e8f2a6c0d4b8e1f3a7c9d2b4f6e8a0c1d3f5b7d9e1a3c5f7b9d1e3a5c7f9b2d"
			control := f.provisionFor(t, "s59-control-"+slug, "blocked", "container", controlHandle, 3*time.Hour)

			n, err := f.d.SweepIdleProvisions(context.Background())
			if err != nil {
				t.Fatalf("the sweep: %v", err)
			}
			// The control first: nothing below means anything until the sweep has
			// demonstrably removed the row it was supposed to remove.
			if n != 1 {
				t.Fatalf("the sweep removed %d provisions, want 1 — the CONTROL (a blocked loop's ready row aged 3h "+
					"past the 10m TTL) was not removed, so this case cannot tell a correct refusal from silence. "+
					"See the header: execenv_reconcile.go:282 reads a loop SLUG through ReadPinnedLoop, which is keyed "+
					"on the loop's hex id (store/blueprint.go:219), so loopBlocked is always false.", n)
			}
			if cp, err := f.store.GetProvision(control.ID); err != nil || cp.State != store.ProvisionStateRemoved {
				t.Fatalf("the control row reads %q (err %v), want %q", cp.State, err, store.ProvisionStateRemoved)
			}
			if got := eng.calls(t); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"rm", "--force", controlHandle}) {
				t.Fatalf("the engine was handed %v, want exactly one [rm --force %s] — the control's", got, controlHandle)
			}

			if p, err := f.store.GetProvision(row.ID); err != nil {
				t.Fatalf("re-read: %v", err)
			} else if p.State != row.State {
				t.Errorf("the row moved from %q to %q — %s", row.State, p.State, tc.want)
			}
			if tc.stamp != "" && !f.log.has("has an unreadable age", row.ID) {
				t.Errorf("the sweep left the row alone in silence. Lines: %q — leaving a container is the safe "+
					"reading, said out loud (execenv_reconcile.go:239)", strings.Join(f.log.all(), " | "))
			}
		})
	}
}

// TestSweepIdleProvisionsKeepsTheRowWhenTheEngineRefuses is EV12's lie: a row
// that reads `removed` while its container is still up. The ledger goes
// ready → tearing_down → failed with the engine's own sentence, never removed.
func TestSweepIdleProvisionsKeepsTheRowWhenTheEngineRefuses(t *testing.T) {
	const sentence = "Error response from daemon: Cannot remove container: device or resource busy"
	eng := newFakeEngine(t, 1, sentence, 0)
	f := newSweepFixture(t, "10m", eng)
	handle := "4d7f9a2c5e8b1036789c0f4a6d8e2b5c1a3f7d9e0b2c468a1d5f7e9c3b0a2d4f6"
	row := f.provisionFor(t, "s59-stuck", "blocked", "container", handle, 3*time.Hour)

	n, err := f.d.SweepIdleProvisions(context.Background())
	if err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	if n != 0 {
		t.Errorf("the sweep counted %d removals while the engine refused, want 0", n)
	}
	after, err := f.store.GetProvision(row.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.State == store.ProvisionStateRemoved {
		t.Fatal("the row reads removed and the container is still up — the lie EV12 exists to prevent")
	}
	if after.State != store.ProvisionStateFailed {
		t.Errorf("state = %q, want %q (ready → tearing_down → failed, provision.go:335)", after.State, store.ProvisionStateFailed)
	}
	if after.LastError == nil || !strings.Contains(*after.LastError, sentence) {
		t.Errorf("the ledger lost the engine's sentence: %+v — the next person reads this row, not the daemon's stdout", after.LastError)
	}
	if !f.log.has("environment sweep: the idle container of provision ", row.ID, sentence) {
		t.Errorf("the refusal was not spoken with the provision id and the engine's own words. Lines: %q",
			strings.Join(f.log.all(), " | "))
	}
	if got := eng.calls(t); len(got) != 1 {
		t.Errorf("the engine was called %d times (%v), want the one attempt", len(got), got)
	}
}

// TestSweepIdleProvisionsMarksTearingDownBeforeTheEngineCall pins the order the
// lifecycle claims, not just its endpoint: `tearing_down` is written BEFORE the
// engine is asked (provision.go:317-323), which is what lets a daemon killed
// mid-`docker rm` be reconciled instead of leaking. The fake engine holds the
// removal open so the intermediate state is observable from outside the process.
func TestSweepIdleProvisionsMarksTearingDownBeforeTheEngineCall(t *testing.T) {
	eng := newFakeEngine(t, 0, "", 300*time.Millisecond)
	f := newSweepFixture(t, "10m", eng)
	row := f.provisionFor(t, "s59-order", "blocked", "container",
		"7b3e9d1c5a8f2046fd8e0b4c6a2d9e1f3b5c7d9e1a3f5b7d9c1e3f5a7b9d0f2c", 3*time.Hour)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := f.d.SweepIdleProvisions(context.Background()); err != nil {
			t.Errorf("the sweep: %v", err)
		}
	}()

	seen := false
	deadline := time.Now().Add(5 * time.Second)
poll:
	for {
		if p, err := f.store.GetProvision(row.ID); err == nil && p.State == store.ProvisionStateTearingDown {
			seen = true
			break
		}
		select {
		case <-done:
			break poll
		case <-time.After(2 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			break
		}
	}
	<-done // the sweep is over (or about to be): read the settled state, not the witness.
	final := ""
	if p, err := f.store.GetProvision(row.ID); err != nil {
		t.Fatalf("re-read the row after the sweep: %v", err)
	} else {
		final = p.State
	}
	if !seen {
		t.Errorf("the row was never observed in %q while the engine held the removal open — the ledger must say "+
			"\"this was on its way out\" BEFORE the engine is asked (provision.go:317), or a daemon killed mid-`docker rm` "+
			"leaks the container", store.ProvisionStateTearingDown)
	}
	if final != store.ProvisionStateRemoved {
		t.Fatalf("the row ended in %q, want %q — the sweep did not settle it", final, store.ProvisionStateRemoved)
	}
	if !f.log.has("environment sweep:", "1 idle container(s) removed") {
		t.Errorf("the sweep did not speak. Lines: %q", strings.Join(f.log.all(), " | "))
	}
}

// TestServeWiresTheIdleSweepToTheRecoveryTicker pins the production wiring the
// unit tests above cannot reach: the daemon's start path names the ticker and
// the interval is the hour. This is a source read, not an execution — see the
// gap named under TestTheIdleSweepRunsOnItsOwnTicker.
func TestServeWiresTheIdleSweepToTheRecoveryTicker(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "serve.go"))
	if err != nil {
		t.Fatalf("read the daemon's start path: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"d.StartEnvironmentReconcile(recoveryCtx, time.Hour)",
		"d.ReconcileEnvironments(recoveryCtx)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("cmd/aeon/serve.go no longer contains %q — the idle sweep has exactly one production caller, "+
				"and a sweep nobody starts is EV22 unproven", want)
		}
	}
}

// TestTheIdleSweepRunsOnItsOwnTicker closes the hole that "call the function
// directly" leaves: it proves StartEnvironmentReconcile's ticker really reaches
// SweepIdleProvisions, removes the aged row through it, and stops with its
// context.
//
// THE REMAINING GAP, said plainly: this is still not a real `aeon serve` run.
// What it does NOT prove is (a) that a live daemon with a live engine ages a
// real container out — no test here talks to docker, the engine is a script; and
// (b) that a tick ever fires in production at the hour, which needs a daemon
// observed for >1h (or the interval shortened by configuration — it is a hard
// literal at cmd/aeon/serve.go:485, `time.Hour`, which is why the eight real
// walks never showed a line). The static sibling above pins the literal; a real
// walk that waits an hour is what would close this for good.
func TestTheIdleSweepRunsOnItsOwnTicker(t *testing.T) {
	eng := newFakeEngine(t, 0, "", 0)
	f := newSweepFixture(t, "10m", eng)
	handle := "2c4e6a8b0d1f3579ace02468bdf13579a2c4e6f8b0d2f4a6c8e0b2d4f6a8c0e2"
	row := f.provisionFor(t, "s59-ticker", "blocked", "container", handle, 3*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.d.StartEnvironmentReconcile(ctx, 5*time.Millisecond)

	deadline := time.Now().Add(10 * time.Second)
	for !f.log.has("environment sweep:", "1 idle container(s) removed") {
		if time.Now().After(deadline) {
			t.Fatalf("the ticker never reached the sweep within 10s (interval 5ms). Lines: %q — either the ticker "+
				"does not call SweepIdleProvisions or the sweep silently did nothing", strings.Join(f.log.all(), " | "))
		}
		time.Sleep(2 * time.Millisecond)
	}
	if after, err := f.store.GetProvision(row.ID); err != nil || after.State != store.ProvisionStateRemoved {
		t.Fatalf("the tick logged a removal the ledger does not show: state=%q err=%v", after.State, err)
	}
	before := len(eng.calls(t))
	cancel()
	time.Sleep(150 * time.Millisecond)
	if after := len(eng.calls(t)); after != before {
		t.Errorf("the engine was called %d more times after the context was cancelled, want 0 — the ticker must die with the recovery context (EV15: no sweep mid-Remove at shutdown)", after-before)
	}
}
