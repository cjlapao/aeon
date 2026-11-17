package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/testutil/testenv"
)

// # The CLI, against a REAL daemon on a temp socket (T6's I5)
//
// Not a mock. A CLI tested against a mock of its own protocol tests nothing
// about the protocol: both halves share a codebase, a set of assumptions and an
// author, so they agree with each other whether or not either is right.
//
// So these tests build the binary once and run it — the shipped dispatch, the
// real `localrpc` transport, the real handshake, over a socket in `t.TempDir`.
// **Never the real endpoint path** (I5, inherited from S1's I6): a test run
// must not collide with a developer's daemon, and must not leave one behind.
//
// The cost is a build per `go test ./cmd/aeon` and a few seconds of daemon
// startup. It buys the only tests in the slice that exercise what is actually
// shipped.

var aeonBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aeon-cli-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cli tests: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	if err := testenv.Isolate(dir); err != nil {
		fmt.Fprintf(os.Stderr, "cli tests: isolated environment: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	// AGENTS.md §6: anything that opens the store exports XDG_STATE_HOME at a
	// throwaway directory FIRST. Per-test isolation was convention, and the
	// convention broke: an in-process verb that forgot it migrated the real
	// store (measured 2026-10-24, the duplicate-column incident on migration
	// 0020). So the package sets it once here for every test — in-process
	// verbs included — and tests that want their own state dir override it
	// with t.Setenv, which restores this value on cleanup. Subprocess fixtures
	// still pass their own XDG_STATE_HOME explicitly, which outranks this.
	stateHome := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateHome, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "cli tests: state dir: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.Setenv("XDG_STATE_HOME", stateHome); err != nil {
		fmt.Fprintf(os.Stderr, "cli tests: XDG_STATE_HOME: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	aeonBinary = filepath.Join(dir, "aeon")
	// sqlite_fts5, same as every other door that links the driver (S22 T1:
	// migration 0040 creates an FTS5 table, so a binary built WITHOUT the
	// tag dies at store open — and these tests exist to run a REAL binary,
	// so the tag has to ride the exec'd build, not just the make line).
	// AEON_GO_TAGS lets the no-retrieval CI job add noretrieval to the
	// binary too; the default is the tag every door already carries.
	buildTags := os.Getenv("AEON_GO_TAGS")
	if buildTags == "" {
		buildTags = "sqlite_fts5"
	}
	// AEON_NO_BUILDVCS (S22 T6, same discipline the retrieval gates'
	// subprocesses already honour): these tests exec their OWN build, and a
	// detached rehearsal worktree makes VCS stamping exit 128 — a
	// build-contract gate must not die on where the tree happens to live.
	buildArgs := []string{"build", "-tags", buildTags, "-o", aeonBinary, "."}
	if os.Getenv("AEON_NO_BUILDVCS") == "1" {
		buildArgs = []string{"build", "-buildvcs=false", "-tags", buildTags, "-o", aeonBinary, "."}
	}
	build := exec.Command("go", buildArgs...)
	build.Env = append(os.Environ(), "GOPRIVATE=github.com/cjlapao/*", "CGO_ENABLED=1")
	if out, err := build.CombinedOutput(); err != nil {
		// A build failure here is a real failure, not a reason to skip: these
		// tests exist precisely to catch a binary that does not work.
		fmt.Fprintf(os.Stderr, "cli tests: building the binary failed: %v\n%s", err, out)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// daemonFixture is a running daemon on its own endpoint and its own state dir.
type daemonFixture struct {
	t        *testing.T
	endpoint string
	stateDir string
	cmd      *exec.Cmd
	exited   <-chan struct{}
	logPath  string
	extraEnv []string
}

func startDaemon(t *testing.T) *daemonFixture {
	t.Helper()
	return startDaemonWithEnv(t)
}

// startDaemonWithEnv starts a daemon with extra environment entries, which is
// how S3's tests point a route at a local replay server instead of a provider.
func startDaemonWithEnv(t *testing.T, extraEnv ...string) *daemonFixture {
	t.Helper()
	base, run, state := freshDaemonDirs(t)
	return startDaemonProcess(t, base, run, state, extraEnv...)
}

// startDaemonInStateDir starts a daemon whose STATE directory is the one given
// (already prepared by the caller) while its run directory and log are fresh.
// The base directory is removed on cleanup; the state directory is NOT — the
// caller owns it, which is what lets S55 T11 seed a control-eligible loop into
// the store BEFORE the daemon opens it, so no control op races a seed.
func startDaemonInStateDir(t *testing.T, stateDir string, extraEnv ...string) *daemonFixture {
	t.Helper()
	base, run := freshDaemonRunDirs(t)
	return startDaemonProcess(t, base, run, stateDir, extraEnv...)
}

// freshDaemonDirs makes the fixture's short base directory plus its run and
// state subdirectories, and removes the base on cleanup.
//
// A short directory: a unix socket path is capped around 108 bytes, and
// `t.TempDir()` under a long test name blows it — which surfaces as
// "invalid argument" from bind and looks like a permissions problem.
func freshDaemonDirs(t *testing.T) (base, run, state string) {
	t.Helper()
	base, err := os.MkdirTemp("", "aeon-d-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	run, state = filepath.Join(base, "r"), filepath.Join(base, "s")
	for _, d := range []string{run, state} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return base, run, state
}

// freshDaemonRunDirs is freshDaemonDirs for the variant that gets its state
// directory from outside: base and run only.
func freshDaemonRunDirs(t *testing.T) (base, run string) {
	t.Helper()
	base, err := os.MkdirTemp("", "aeon-d-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	run = filepath.Join(base, "r")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	return base, run
}

// startDaemonProcess starts the daemon binary on the given run and state
// directories and waits for it to answer on its endpoint.
func startDaemonProcess(t *testing.T, base, run, state string, extraEnv ...string) *daemonFixture {
	t.Helper()
	if aeonBinary == "" {
		t.Skip("the binary was not built")
	}

	f := &daemonFixture{
		t:        t,
		endpoint: filepath.Join(run, "rpc.sock"),
		stateDir: state,
		logPath:  filepath.Join(base, "daemon.log"),
		extraEnv: extraEnv,
	}

	logFile, err := os.Create(f.logPath)
	if err != nil {
		t.Fatal(err)
	}

	f.cmd = exec.Command(aeonBinary, "serve")
	f.cmd.Env = f.env()
	f.cmd.Stdout = logFile
	f.cmd.Stderr = logFile
	if err := f.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := f.trackExit(t, logFile)

	// Wait for the endpoint rather than sleeping a guess.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			logs, _ := os.ReadFile(f.logPath)
			t.Fatalf("daemon exited before ready: %v\n%s", f.cmd.ProcessState, logs)
		default:
		}
		if _, err := os.Stat(f.endpoint); err == nil {
			// The socket exists; wait for the daemon to answer on it.
			if out, err := f.run("ping"); err == nil && strings.Contains(out, "pong") {
				return f
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := os.ReadFile(f.logPath)
	t.Fatalf("the daemon never became ready on %s\n--- daemon log ---\n%s", f.endpoint, logs)
	return nil
}

// waitStartupQuiet blocks until the daemon says its startup goroutine has
// finished. The endpoint answers `ping` BEFORE that: until then the daemon is
// still writing the store (reconcile, inert seeds, adoption — measured at
// about 6s per start on this box), so a test that opens a SECOND writer on the
// same store straight away can outwait the 5s busy timeout and fail with
// "database is locked". Only those tests wait; every other fixture keeps the
// fast ready of `ping` (waiting in all of them added ~480s to this package).
func (f *daemonFixture) waitStartupQuiet() {
	f.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if logs, _ := os.ReadFile(f.logPath); strings.Contains(string(logs), "startup recovery complete") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := os.ReadFile(f.logPath)
	f.t.Fatalf("the daemon never finished its startup writes\n--- daemon log ---\n%s", logs)
}

func (f *daemonFixture) env() []string {
	// The provider keys are REMOVED, not overridden: a developer's real key in
	// the environment would otherwise make these tests call a provider, which
	// is exactly what I7 forbids — and it would pass on their machine and fail
	// in CI.
	//
	// The store-location variables are removed for the same reason, and for a
	// bug S55 T11 measured: an in-process dial runs appenv.Apply(), which
	// os.Setenvs DATABASE_SQLITE_STORAGE_PATH at THIS package's state home.
	// os.Setenv outlives the test that ran it, so the NEXT fixture daemon
	// inherited it — and in the store's resolution chain a set
	// DATABASE_SQLITE_STORAGE_PATH outranks the fixture's own
	// XDG_STATE_HOME. The second daemon of a run then opened the shared
	// package store and answered "no loop" for a loop that was seeded into
	// the fixture's state dir. The fixture's XDG_STATE_HOME (appended below)
	// is the one store address every subprocess in the fixture may use.
	base := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "ANTHROPIC_API_KEY="),
			strings.HasPrefix(kv, "OPENAI_API_KEY="),
			strings.HasPrefix(kv, "AEON_ANTHROPIC_BASE_URL="),
			strings.HasPrefix(kv, "AEON_OPENAI_BASE_URL="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_STORAGE_PATH="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_FILE_NAME="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_DSN="),
			strings.HasPrefix(kv, "AEON_STATE_DIR="):
			continue
		case strings.HasPrefix(kv, "DATABASE_SQLITE_STORAGE_PATH="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_FILE_NAME="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_DSN="):
			// The S45 T8 Notes #8 finding, applied to the shared fixture: an
			// in-process dial (appenv.Apply setDefault) pins the store paths
			// process-wide, and a daemonFixture built from os.Environ()
			// inherits a store ANOTHER test chose — the XDG_STATE_HOME pin
			// below would be shadowed by the inherited storage path
			// (resolveConfig's precedence). The both-doors suite strips these
			// in doorEnv; S45 T10 (agent_import commits over the socket,
			// which WRITE plugin rows) made the leak observable here, so the
			// strip moves to the source: a fixture daemon always opens the
			// store at its own stateDir, whatever ambient env a previous
			// verb left behind.
			continue
		}
		base = append(base, kv)
	}
	out := append(base,
		"LOCALRPC_ENDPOINT_PATH="+f.endpoint,
		"XDG_STATE_HOME="+f.stateDir,
		"GOPRIVATE=github.com/cjlapao/*",
	)
	return append(out, f.extraEnv...)
}

// run executes a CLI verb against this daemon and returns its combined output.
func (f *daemonFixture) run(args ...string) (string, error) {
	f.t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = f.env()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *daemonFixture) mustRun(args ...string) string {
	f.t.Helper()
	out, err := f.run(args...)
	if err != nil {
		f.t.Fatalf("aeon %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (f *daemonFixture) daemonLog() string {
	b, _ := os.ReadFile(f.logPath)
	return string(b)
}

func TestPingReportsARoundTripTime(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("ping")
	if !strings.Contains(out, "pong") {
		t.Fatalf("ping did not pong: %q", out)
	}
	if !strings.Contains(out, "ms") {
		t.Fatalf("ping did not report a time: %q", out)
	}
}

// A bare number reads as a constant, so the label is part of the requirement.
func TestVersionReportsTheNegotiatedProtocolVersionMarkedAsSuch(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("version")
	if !strings.Contains(out, "negotiated") {
		t.Fatalf("version does not mark the protocol version as negotiated: %q", out)
	}
	if !strings.Contains(out, fmt.Sprintf("protocol  %d", protocol.PROTOCOL_VERSION)) {
		t.Fatalf("version does not report protocol %d: %q", protocol.PROTOCOL_VERSION, out)
	}
	// The daemon's version, not only the client's — they are the same binary
	// here, so the assertion is that the daemon line is present at all.
	if !strings.Contains(out, "daemon  ") {
		t.Fatalf("version does not report the daemon's version: %q", out)
	}
	// S1's Go-toolchain line must survive S2's rewrite of this verb.
	if !strings.Contains(out, "go      go1.") {
		t.Fatalf("version lost the Go toolchain line, which S1 added because cgo is on: %q", out)
	}
}

// `version` with no daemon still reports the client's own build. Failing
// entirely would be less useful, and the disconnected line must name the path.
func TestVersionWithoutADaemonStillReportsTheClientBuild(t *testing.T) {
	base, err := os.MkdirTemp("", "aeon-nod-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })

	cmd := exec.Command(aeonBinary, "version")
	cmd.Env = append(os.Environ(),
		"LOCALRPC_ENDPOINT_PATH="+filepath.Join(base, "nothing.sock"),
		"XDG_STATE_HOME="+base)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version failed with no daemon: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "aeon ") {
		t.Fatalf("no client version: %q", out)
	}
	if !strings.Contains(string(out), "not connected") {
		t.Fatalf("version did not say it could not reach the daemon: %q", out)
	}
	if !strings.Contains(string(out), "nothing.sock") {
		t.Fatalf("the failure does not name the path it tried, which is the first thing "+
			"anyone debugging needs: %q", out)
	}
}

// The refusal, demonstrated. `--protocol-version` exists for exactly this: it
// is otherwise unobservable until the first real version bump.
func TestProtocolVersion99IsRefusedNamingBothNumbers(t *testing.T) {
	d := startDaemon(t)
	out, err := d.run("ping", "--protocol-version", "99")
	if err == nil {
		t.Fatalf("asking for protocol 99 succeeded: %q", out)
	}
	for _, want := range []string{"99", "1", "what", "means", "todo", "fatal"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not contain %q:\n%s", want, out)
		}
	}
}

// Every CLI error renders all three parts.
func TestEveryCLIErrorRendersAllThreeParts(t *testing.T) {
	d := startDaemon(t)
	out, _ := d.run("watch", "--kinds", "*.started")
	for _, want := range []string{"what", "means", "todo"} {
		if !strings.Contains(out, want) {
			t.Errorf("an invalid-filter error is missing the %q part:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "-31010") {
		t.Errorf("the code is not shown:\n%s", out)
	}
}

// O1, through the CLI: the hardest bug in this slice to diagnose from outside
// is a filter that silently matches nothing.
func TestWatchRefusesAnInvalidWildcardRatherThanMatchingNothing(t *testing.T) {
	d := startDaemon(t)
	for _, bad := range []string{"*.started", "loop.*.x", "loop.?x"} {
		out, err := d.run("watch", "--kinds", bad)
		if err == nil {
			t.Errorf("watch accepted %q; it would wait forever having matched nothing\n%s", bad, out)
		}
	}
}

// D6, DEMONSTRATED rather than asserted: the registry prints empty after the
// client leaves.
func TestDebugSubscriptionsListsTheRegistryAndShowsItEmptyAfterAClientLeaves(t *testing.T) {
	d := startDaemon(t)

	// Nothing subscribed yet. The debug client's own connection is counted,
	// which is why the assertion is on subscriptions rather than connections.
	out := d.mustRun("debug", "subscriptions")
	if !strings.Contains(out, "no subscriptions") {
		t.Fatalf("the registry was not empty at the start: %q", out)
	}

	// A watcher holds a subscription for as long as it runs.
	watch := exec.Command(aeonBinary, "watch", "--kinds", "client.*")
	watch.Env = d.env()
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _, _ = watch.Process.Wait() }()

	// Wait for its subscription line rather than sleeping: that line exists so
	// a watcher is never indistinguishable from one that subscribed to nothing.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("watch printed no subscription line: %v", err)
	}
	if !strings.Contains(line, "watching s-") || !strings.Contains(line, "from seq") {
		t.Fatalf("watch's first line is not a subscription line: %q", line)
	}

	out = d.mustRun("debug", "subscriptions")
	if !strings.Contains(out, "client.*") {
		t.Fatalf("the watcher's subscription is not in the registry:\n%s", out)
	}

	// Now it leaves. T5: subscriptions die with their connection.
	_ = watch.Process.Kill()
	_, _ = watch.Process.Wait()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out = d.mustRun("debug", "subscriptions")
		if strings.Contains(out, "no subscriptions") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the registry still holds subscriptions after the client left:\n%s\n--- daemon log ---\n%s",
		out, d.daemonLog())
}

// The watcher's subscription line, which is the difference between a client
// that is waiting and a client that subscribed to nothing.
func TestWatchPrintsItsSubscriptionLineOnConnect(t *testing.T) {
	d := startDaemon(t)
	watch := exec.Command(aeonBinary, "watch", "--kinds", "loop.*", "--component", "/tmp/x")
	watch.Env = d.env()
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _, _ = watch.Process.Wait() }()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"watching s-", "kinds=loop.*", "component=/tmp/x", "from seq"} {
		if !strings.Contains(line, want) {
			t.Errorf("the subscription line is missing %q: %q", want, line)
		}
	}
}

// The demo's centrepiece: watch in one terminal, another client in the other.
func TestWatchReceivesClientConnectedWhenAnotherClientArrives(t *testing.T) {
	d := startDaemon(t)
	watch := exec.Command(aeonBinary, "watch", "--kinds", "client.*")
	watch.Env = d.env()
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _, _ = watch.Process.Wait() }()

	reader := bufio.NewReader(stdout)
	if _, err := reader.ReadString('\n'); err != nil { // the subscription line
		t.Fatal(err)
	}

	// Another client connects and leaves.
	d.mustRun("ping")

	lines := make(chan string, 8)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			lines <- line
		}
	}()

	var sawConnected, sawDisconnected bool
	timeout := time.After(15 * time.Second)
	for !(sawConnected && sawDisconnected) {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("watch's stream ended; saw connected=%v disconnected=%v", sawConnected, sawDisconnected)
			}
			if strings.Contains(line, "client.connected") {
				sawConnected = true
				// D12: the pid must be readable. It was printed as
				// `1.130661e+06` first time round, which is unusable for the
				// one purpose a pid has.
				if strings.Contains(line, "e+0") {
					t.Errorf("a number is rendered in scientific notation, which makes the "+
						"pid unusable: %q", line)
				}
				if !strings.Contains(line, "peerPid=") {
					t.Errorf("client.connected carries no pid: %q", line)
				}
			}
			if strings.Contains(line, "client.disconnected") {
				sawDisconnected = true
			}
		case <-timeout:
			t.Fatalf("timed out; saw connected=%v disconnected=%v\n--- daemon log ---\n%s",
				sawConnected, sawDisconnected, d.daemonLog())
		}
	}
}

// The cursor, across a real reconnect. `--after` is what makes "did I miss
// anything" answerable rather than guessed.
func TestWatchAfterResumesFromTheCursor(t *testing.T) {
	d := startDaemon(t)

	// Generate events: each ping is a connect and a disconnect.
	for i := 0; i < 3; i++ {
		d.mustRun("ping")
	}

	// Resume from the very beginning, which must replay rather than wait.
	watch := exec.Command(aeonBinary, "watch", "--after", "0")
	watch.Env = d.env()
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _, _ = watch.Process.Wait() }()

	reader := bufio.NewReader(stdout)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "from seq 1") {
		t.Fatalf("--after 0 did not resume from seq 1: %q", first)
	}

	// The replay must arrive without anything else happening — which is the
	// whole point of a cursor.
	deadline := time.Now().Add(10 * time.Second)
	var seqs []string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] != "Ctrl-C" {
			seqs = append(seqs, fields[0])
		}
		if len(seqs) >= 3 {
			break
		}
	}
	if len(seqs) < 3 {
		t.Fatalf("--after 0 replayed %d events (%v), want the retained history\n--- log ---\n%s",
			len(seqs), seqs, d.daemonLog())
	}
	// No duplicates, in order.
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("the replay is out of order or duplicated: %v", seqs)
			break
		}
	}
}

// D1, from a client's perspective: the CLI cannot skip the handshake, so the
// daemon never sees a method before `initialize`.
func TestTheDaemonLogShowsTheHandshakeAndPeerCredentials(t *testing.T) {
	d := startDaemon(t)
	d.mustRun("ping")

	log := d.daemonLog()
	if !strings.Contains(log, "client connected:") {
		t.Fatalf("the daemon log has no accept line:\n%s", log)
	}
	if !strings.Contains(log, "uid=") {
		t.Errorf("the accept log names no uid (D12):\n%s", log)
	}
	if !strings.Contains(log, "pid=") {
		t.Errorf("the accept log names no pid (D12):\n%s", log)
	}
	if !strings.Contains(log, "aeon-cli") {
		t.Errorf("the accept log does not name the client:\n%s", log)
	}
}

// restart stops the daemon and starts a new one on the SAME state directory
// and the same endpoint.
//
// # Why a restart is a test fixture and not a manual step
//
// S4's one promise to another slice is that `seq` survives a restart — the gap
// S2 admitted in a doc comment and in its demo. A test that cannot restart the
// daemon cannot check it, and the demo's own restart steps are the ones a
// working tree with a live daemon cannot exercise honestly.
//
// It waits for the old process to EXIT before starting the new one, because
// localrpc's endpoint claim is a real lock: starting the second while the first
// still holds the socket fails with Aeon's own refusal, which is correct
// behaviour and would read here as a flaky test.
func (f *daemonFixture) restart(t *testing.T) {
	t.Helper()

	if err := f.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Already gone is not a failure: the point is that it is not running.
		t.Logf("signalling the daemon: %v", err)
	}
	<-f.exited

	// The endpoint has to be gone, or the new daemon's claim refuses. Waiting
	// for its absence rather than sleeping a guess, for the same reason the
	// start path waits for its presence.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(f.endpoint); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	logFile, err := os.OpenFile(f.logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	f.cmd = exec.Command(aeonBinary, "serve")
	f.cmd.Env = f.env()
	f.cmd.Stdout = logFile
	f.cmd.Stderr = logFile
	if err := f.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := f.trackExit(t, logFile)

	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("restarted daemon exited before ready: %v\n%s", f.cmd.ProcessState, f.daemonLog())
		default:
		}
		if out, err := f.run("ping"); err == nil && strings.Contains(out, "pong") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the daemon did not come back on %s\n--- daemon log ---\n%s", f.endpoint, f.daemonLog())
}

// aeonCommand builds a CLI invocation with NO daemon, pointed at a temp state
// directory.
//
// For the verbs that must work with the daemon down — `db info` above all,
// because "where is the file" is the first question when the daemon will not
// start.
func aeonCommand(t *testing.T, stateDir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = append(os.Environ(),
		"LOCALRPC_ENDPOINT_PATH="+filepath.Join(stateDir, "nothing.sock"),
		"XDG_STATE_HOME="+stateDir,
		"GOPRIVATE=github.com/cjlapao/*",
	)
	return cmd
}
