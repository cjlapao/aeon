package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

var (
	stubOnce sync.Once
	stubBin  string
	stubErr  error
)

// stubServer builds the internal/mcp test server once: a REAL process, so the
// spawn, the PID, the stderr and the exit are the real thing.
func stubServer(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "aeon-supervisor-stub")
		if err != nil {
			stubErr = err
			return
		}
		stubBin = filepath.Join(dir, "fakeserver")
		out, err := exec.Command("go", "build", "-o", stubBin, "../../internal/mcp/testdata/fakeserver").CombinedOutput()
		if err != nil {
			stubErr = &exec.ExitError{Stderr: out}
		}
	})
	if stubErr != nil {
		t.Fatalf("building the stub server: %v", stubErr)
	}
	return stubBin
}

type fixture struct {
	sup     *mcpSupervisor
	mu      sync.Mutex
	servers []supervised
}

func (f *fixture) set(servers ...supervised) { f.mu.Lock(); f.servers = servers; f.mu.Unlock() }

func newFixture(t *testing.T, servers ...supervised) *fixture {
	t.Helper()
	f := &fixture{servers: servers}
	f.sup = newMCPSupervisor(func(context.Context) ([]supervised, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]supervised(nil), f.servers...), nil
	}, func(mcp.ServerConfig) func(string) string { return func(string) string { return "" } })
	t.Cleanup(f.sup.StopAll)
	return f
}

func stub(t *testing.T, name string, enabled bool, extra ...string) supervised {
	args := append([]string{"-name", name, "-log", "hello from " + name}, extra...)
	return supervised{Name: name, Enabled: enabled, Config: mcp.ServerConfig{Name: name,
		Transport: mcp.TransportConfig{Type: mcp.TransportStdio, Command: stubServer(t), Args: args}}}
}

func view(t *testing.T, s *mcpSupervisor, name string) protocol.MCPProcessView {
	t.Helper()
	for _, v := range s.List() {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("no process %q in %+v", name, s.List())
	return protocol.MCPProcessView{}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func TestStartingTheDaemonStartsEveryEnabledStdioServerAndTracksIt(t *testing.T) {
	f := newFixture(t,
		stub(t, "docs", true),
		stub(t, "off", false),
		supervised{Name: "remote", Enabled: true, Config: mcp.ServerConfig{Name: "remote", Transport: mcp.TransportConfig{Type: mcp.TransportHTTP, URL: "https://x.example"}}},
	)
	if err := f.sup.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	docs := view(t, f.sup, "docs")
	if docs.State != "running" || !alive(docs.PID) || docs.Tools == 0 || docs.StartedAt == "" {
		t.Fatalf("an enabled stdio server runs and is tracked: %+v", docs)
	}
	if off := view(t, f.sup, "off"); off.State != "disabled" || off.PID != 0 {
		t.Fatalf("a disabled server is never started: %+v", off)
	}
	if r := view(t, f.sup, "remote"); r.State != "remote" || r.PID != 0 {
		t.Fatalf("an http server has no process of ours: %+v", r)
	}
	logs, _ := f.sup.Logs("docs", 0, 0)
	var stderr, daemonLines int
	for _, l := range logs.Lines {
		switch l.Stream {
		case "stderr":
			if strings.Contains(l.Text, "hello from docs") {
				stderr++
			}
		case "daemon":
			daemonLines++
		}
	}
	if stderr == 0 || daemonLines < 2 {
		t.Fatalf("the log holds the server's stderr and the daemon's own lines: %+v", logs.Lines)
	}
}

func TestTerminateStopsTheProcessAndItStaysStoppedUntilStartedAgain(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	pid := view(t, f.sup, "docs").PID

	v, refusal := f.sup.Stop("docs", "user:1000")
	if refusal != "" || v.State != "stopped" || v.PID != 0 {
		t.Fatalf("stop: %+v %q", v, refusal)
	}
	waitFor(t, "the process to die", func() bool { return !alive(pid) })
	// A reconcile the person did not ask for does not bring it back.
	f.sup.Sync(context.Background())
	if v := view(t, f.sup, "docs"); v.State != "stopped" {
		t.Fatalf("a terminate sticks: %+v", v)
	}
	// A step is told why its tools are missing — it is not silently re-spawned.
	if _, managed, err := f.sup.Lease("docs"); !managed || err == nil || !strings.Contains(err.Error(), "is stopped") {
		t.Fatalf("lease of a stopped server: managed=%v err=%v", managed, err)
	}
	v, refusal = f.sup.Start(context.Background(), "docs", "user:1000")
	if refusal != "" || v.State != "running" || v.PID == 0 || v.PID == pid {
		t.Fatalf("start gives a new process: %+v %q (was %d)", v, refusal, pid)
	}
}

func TestRestartIsATerminateAndAStartAndCountsIt(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	before := view(t, f.sup, "docs")
	v, refusal := f.sup.Restart(context.Background(), "docs", "user:1000")
	if refusal != "" || v.State != "running" || v.PID == before.PID || v.Restarts != before.Restarts+1 {
		t.Fatalf("restart: %+v %q (before %+v)", v, refusal, before)
	}
	waitFor(t, "the old process to die", func() bool { return !alive(before.PID) })
	logs, _ := f.sup.Logs("docs", 0, 0)
	var asked bool
	for _, l := range logs.Lines {
		if l.Stream == "daemon" && strings.Contains(l.Text, "restart requested by user:1000") {
			asked = true
		}
	}
	if !asked {
		t.Fatal("the log says who asked")
	}
}

func TestAServerThatCrashesIsShownAsCrashedWithItsExitAndIsNotRestartedByItself(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	c, managed, err := f.sup.Lease("docs")
	if err != nil || !managed {
		t.Fatalf("lease: %v %v", managed, err)
	}
	if _, err := c.CallTool(context.Background(), "die", json.RawMessage(`{}`)); err == nil {
		t.Fatal("the call the server dies in fails")
	}
	waitFor(t, "the crash to be seen", func() bool { return view(t, f.sup, "docs").State == "crashed" })
	v := view(t, f.sup, "docs")
	if v.PID != 0 || !strings.Contains(v.LastExit, "exit status 3") {
		t.Fatalf("a crash carries its exit status: %+v", v)
	}
	time.Sleep(150 * time.Millisecond)
	if view(t, f.sup, "docs").State != "crashed" {
		t.Fatal("a crashed server is not restarted by the supervisor — that is the person's act")
	}
	if v, refusal := f.sup.Start(context.Background(), "docs", "user:1000"); refusal != "" || v.State != "running" {
		t.Fatalf("the person restarts it: %+v %q", v, refusal)
	}
}

func TestAStepUsesTheRunningProcessAndReleasingTheLeaseDoesNotKillIt(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	pid := view(t, f.sup, "docs").PID
	if _, managed, _ := f.sup.Lease("not-managed"); managed {
		t.Fatal("an unmanaged server dials per step as before")
	}
	c, managed, err := f.sup.Lease("docs")
	if err != nil || !managed {
		t.Fatalf("lease: %v %v", managed, err)
	}
	res, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"q":"hi"}`))
	if err != nil || !strings.Contains(res.Text, "echo: hi") {
		t.Fatalf("the leased process answers: %+v %v", res, err)
	}
	_ = c.Close() // the step ends
	if v := view(t, f.sup, "docs"); v.State != "running" || v.PID != pid || !alive(pid) {
		t.Fatalf("the process outlives the step: %+v", v)
	}
}

func TestAConfigurationChangeRestartsAndARemovalStops(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	pid := view(t, f.sup, "docs").PID

	changed := stub(t, "docs", true, "-hang", "1m")
	f.set(changed)
	f.sup.Sync(context.Background())
	if v := view(t, f.sup, "docs"); v.State != "running" || v.PID == pid {
		t.Fatalf("a changed definition restarts the server: %+v (was %d)", v, pid)
	}
	waitFor(t, "the old process to die", func() bool { return !alive(pid) })

	pid = view(t, f.sup, "docs").PID
	f.set() // removed
	f.sup.Sync(context.Background())
	waitFor(t, "the removed server's process to die", func() bool { return !alive(pid) })
	for _, v := range f.sup.List() {
		if v.Name == "docs" {
			t.Fatalf("a removed server is gone from the list: %+v", v)
		}
	}
}

func TestControlRefusesWhatHasNoProcessOrIsSwitchedOff(t *testing.T) {
	f := newFixture(t, stub(t, "off", false), supervised{Name: "remote", Enabled: true, Config: mcp.ServerConfig{Name: "remote",
		Transport: mcp.TransportConfig{Type: mcp.TransportHTTP, URL: "https://x.example"}}})
	f.sup.Sync(context.Background())
	for name, want := range map[string]string{"off": "is disabled", "remote": "http server", "nope": "not a configured server"} {
		if _, refusal := f.sup.Restart(context.Background(), name, "user:1"); !strings.Contains(refusal, want) {
			t.Fatalf("%s: %q", name, refusal)
		}
	}
}

func TestTheLogRingIsBoundedAndSaysWhatItDropped(t *testing.T) {
	r := newMCPLogRing(time.Now)
	for i := 0; i < 2500; i++ {
		r.daemon("line %d", i)
	}
	lines, next, dropped := r.since(0, 1000)
	if len(lines) > mcpLogMaxLines || next != 2500 {
		t.Fatalf("bounded: %d lines, next %d", len(lines), next)
	}
	// Asking from a sequence that has already fallen out reports the gap.
	if _, _, gap := r.since(10, 1000); gap == 0 {
		t.Fatalf("a gap is said, not hidden (dropped %d)", dropped)
	}
	// Bytes become whole lines, and a partial line waits for its newline.
	r2 := newMCPLogRing(time.Now)
	r2.Write([]byte("first\nsec"))
	r2.Write([]byte("ond\r\nthird"))
	got, _, _ := r2.since(0, 10)
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("whole lines only: %+v", got)
	}
}

func TestTheProcessDoorsAnswerThroughTheSeamAndControlNeedsAPerson(t *testing.T) {
	f := newFixture(t, stub(t, "docs", true))
	f.sup.Sync(context.Background())
	prev := mcpSup
	mcpSup = f.sup
	t.Cleanup(func() { mcpSup = prev })

	call, _ := writeFixture(t)
	out, refusal, err := call("mcp.processes", map[string]any{})
	if err != nil || refusal != "" || len(out.(protocol.MCPProcessesResult).Processes) != 1 {
		t.Fatalf("processes: %v %q %+v", err, refusal, out)
	}
	out, refusal, err = call("mcp.process.logs", map[string]any{"name": "docs"})
	if err != nil || refusal != "" || len(out.(protocol.MCPProcessLogsResult).Lines) == 0 {
		t.Fatalf("logs: %v %q", err, refusal)
	}
	out, refusal, err = call("mcp.process.restart", map[string]any{"name": "docs"})
	if err != nil || refusal != "" || out.(protocol.MCPProcessControlResult).Process.Restarts != 1 {
		t.Fatalf("restart: %v %q %+v", err, refusal, out)
	}
	// Without a person the control is refused before anything moves.
	isolateStoreEnv(t)
	st := openAgentStore(t)
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	if _, _, err := seam(context.Background(), "mcp.process.stop", mustJSONRaw(t, map[string]any{"name": "docs"})); err == nil {
		t.Fatal("an unattributable terminate is refused")
	}
	if v := view(t, f.sup, "docs"); v.State != "running" {
		t.Fatalf("nothing moved: %+v", v)
	}
	// And a daemon-less process says so rather than listing nothing.
	mcpSup = nil
	if _, refusal, _ := call("mcp.processes", map[string]any{}); !strings.Contains(refusal, "running daemon") {
		t.Fatalf("no supervisor: %q", refusal)
	}
}

func TestAServerWhoseEnvironmentVariableWouldBeEmptyDoesNotStartAndSaysWhich(t *testing.T) {
	_, st := writeFixture(t)
	v := envVault(t, st) // an empty manager
	srv := stub(t, "docs", true)
	srv.Config.Transport.Env = []string{"DOCS_TOKEN", "DOCS_REGION"}
	srv.Config.Transport.EnvFrom = map[string]string{"DOCS_TOKEN": "DOCS_TOKEN_VAR"}
	f := newFixture(t, srv)
	f.sup.preflight = mcpEnvPreflight

	f.sup.Sync(context.Background())
	got := view(t, f.sup, "docs")
	if got.State != "failed" || got.PID != 0 {
		t.Fatalf("nothing is started with an empty variable: %+v", got)
	}
	for _, want := range []string{"cannot be empty", "DOCS_TOKEN (bound to variable DOCS_TOKEN_VAR, which has no value)", "DOCS_REGION (not bound to a variable"} {
		if !strings.Contains(got.LastError, want) {
			t.Fatalf("the failure names what is missing (%q): %q", want, got.LastError)
		}
	}
	logs, _ := f.sup.Logs("docs", 0, 0)
	var said bool
	for _, l := range logs.Lines {
		if l.Stream == "daemon" && strings.Contains(l.Text, "not started") {
			said = true
		}
	}
	if !said {
		t.Fatal("the log says why it was not started")
	}

	// Give both a value — one from the manager, one from the daemon's environment — and it starts.
	val := "now-it-has-a-value"
	if _, err := v.Set(context.Background(), vault.SetInput{Name: "DOCS_TOKEN_VAR", Value: &val}, store.Actor{Kind: "user", ID: "1"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCS_REGION", "eu")
	if v, refusal := f.sup.Start(context.Background(), "docs", "user:1"); refusal != "" || v.State != "running" || v.PID == 0 {
		t.Fatalf("with values it starts: %+v %q", v, refusal)
	}
}
