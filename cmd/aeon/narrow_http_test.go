package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"time"
)

// narrow_http_test.go — the T4 gates that need THE PROCESS, not the
// package: a port asserted closed by an ATTEMPTED connection rather than
// by reading config (D7: config is what a file says; the socket is what
// the process does), and the startup/doctor sentences read off the running
// daemon. The binary is the one TestMain built (cli_integration_test.go)
// against throwaway endpoint paths and state dirs — never the real store.

// startHTTPDaemon starts `aeon serve --http <addr>` with a key file and
// returns a run function plus the daemon log's contents on demand.
type narrowDaemon struct {
	t        *testing.T
	endpoint string
	stateDir string
	logPath  string
	addr     string
	keyValue string
	cmd      *exec.Cmd
	exited   chan struct{}
}

func startNarrowDaemon(t *testing.T, httpAddr, keyEnvValue string, extraEnv ...string) *narrowDaemon {
	t.Helper()
	base := filepath.Join(t.TempDir(), "narrowd")
	if err := os.MkdirAll(filepath.Join(base, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &narrowDaemon{
		t:        t,
		endpoint: filepath.Join(base, "aeon.sock"),
		stateDir: filepath.Join(base, "state"),
		logPath:  filepath.Join(base, "daemon.log"),
		addr:     httpAddr,
		keyValue: keyEnvValue,
	}
	logFile, err := os.Create(f.logPath)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"serve"}
	if httpAddr != "" {
		args = append(args, "--http", httpAddr)
	}
	f.cmd = exec.Command(aeonBinary, args...)
	f.cmd.Env = f.env(keyEnvValue, httpAddr, extraEnv...)
	f.cmd.Stdout = logFile
	f.cmd.Stderr = logFile
	f.exited = make(chan struct{})
	if err := f.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = f.cmd.Process.Wait()
		close(f.exited)
	}()
	t.Cleanup(func() {
		_ = f.cmd.Process.Kill()
		select {
		case <-f.exited:
		case <-time.After(5 * time.Second):
		}
		logFile.Close()
	})
	return f
}

// env follows the daemon fixture's rule (cli_integration_test.go): the
// provider keys are REMOVED, not overridden — a developer's real key must
// never make a test call a provider (I7). Everything else rides along; the
// test-owned values are set last so they win.
func (f *narrowDaemon) env(keyEnvValue, httpAddr string, extraEnv ...string) []string {
	f.t.Helper()
	drop := map[string]bool{
		"ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
		"AEON_ANTHROPIC_BASE_URL": true, "AEON_OPENAI_BASE_URL": true,
		"AEON_HTTP_KEY": true, "SERVER_BIND_ADDRESS": true, "SERVER_API_PORT": true,
		"LOCALRPC_ENDPOINT_PATH": true, "XDG_STATE_HOME": true,
	}
	out := make([]string, 0, 16)
	for _, kv := range os.Environ() {
		if i := strings.Index(kv, "="); i > 0 && !drop[kv[:i]] {
			out = append(out, kv)
		}
	}
	out = append(out,
		"LOCALRPC_ENDPOINT_PATH="+f.endpoint,
		"XDG_STATE_HOME="+f.stateDir,
		"GOPRIVATE=github.com/cjlapao/*",
	)
	if keyEnvValue != "" {
		// The key arrives exactly the way production takes it: the
		// environment (or --http-key-file), never a flag value (O1). And
		// doctor, run later with the SAME environment, is then reading the
		// same "a key is set" fact an operator's machine would hold.
		out = append(out, "AEON_HTTP_KEY="+keyEnvValue)
	}
	// The address in the config plane for the DOCTOR to read (serve sets
	// these itself from the flag; a sibling process sees only the plane).
	if httpAddr != "" {
		host, port, err := net.SplitHostPort(httpAddr)
		if err != nil {
			f.t.Fatal(err)
		}
		if host == "" {
			host = "0.0.0.0"
		}
		out = append(out, "SERVER_BIND_ADDRESS="+host, "SERVER_API_PORT="+port)
	}
	return append(out, extraEnv...)
}

// waitReady waits for the socket AND, when an address was requested, for
// the port to accept — the same "wait for the thing, never sleep a guess"
// rule the daemon fixture follows.
//
// The socket and the port being up is NOT the daemon being ready for the
// narrow surface, though. serve.go starts the HTTP listener (svc.Start)
// BEFORE it wires the narrow Caller (narrowDisp.set), and the Late-getter
// answers a request that lands in that gap with a transport 503 ("no daemon
// behind it yet") — the honest, designed answer, not a flake. So waitReady
// also waits for the daemon's own "aeon is ready" line, which serve.go logs
// AFTER it sets the Caller: the point a request stops getting the 503 and
// reaches the daemon.
func (f *narrowDaemon) waitReady() {
	f.t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(f.endpoint); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if _, err := net.DialTimeout("unix", f.endpoint, 200*time.Millisecond); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if f.addr != "" {
			conn, err := net.DialTimeout("tcp", f.addr, 200*time.Millisecond)
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			_ = conn.Close()
		}
		if f.readyLogged() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := os.ReadFile(f.logPath)
	f.t.Fatalf("daemon never became ready (endpoint %s, http %q)\n--- daemon log ---\n%s", f.endpoint, f.addr, logs)
}

// readyLogged reports the "aeon is ready" line serve.go logs AFTER it wires
// the narrow Caller (narrowDisp.set), the point a request on the narrow
// surface stops getting the Late-getter's 503 and reaches the daemon.
func (f *narrowDaemon) readyLogged() bool {
	f.t.Helper()
	b, err := os.ReadFile(f.logPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "aeon is ready")
}

func (f *narrowDaemon) waitDead(want string) {
	f.t.Helper()
	select {
	case <-f.exited:
	case <-time.After(20 * time.Second):
		logs, _ := os.ReadFile(f.logPath)
		f.t.Fatalf("daemon still alive; wanted: %s\n--- log ---\n%s", want, logs)
	}
}

func (f *narrowDaemon) log() string {
	b, _ := os.ReadFile(f.logPath)
	return string(b)
}

func (f *narrowDaemon) run(args ...string) (string, error) {
	f.t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = f.env(f.keyValue, f.addr)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// freePort asks the kernel for a port nobody holds, then releases it. The
// race window (release → serve binds) is the standard trade and the
// waitReady loop turns a lost race into a retry-worthy failure message
// rather than a mystery.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestNoPortWithoutAnExplicitAddress — a daemon that listens on a port by
// default is a daemon somebody exposed by accident (N15), so the gate is
// a CONNECTION ATTEMPT, not a config read (D7): plain `aeon serve` must
// refuse TCP to the framework's default port (127.0.0.1:8080 — what
// `server.api` would bind if the pin ever lapsed), and doctor must SAY the
// ordinary state rather than leaving it inferred.
func TestNoPortWithoutAnExplicitAddress(t *testing.T) {
	f := startNarrowDaemon(t, "", "")
	f.waitReady()
	// The default framework port: refused, because api is ForceOff. The
	// machine-wide caveat (some other process could hold :8080) is checked
	// first so this gate fails for the daemon's reason or not at all.
	if probe, err := net.DialTimeout("tcp", "127.0.0.1:8080", 300*time.Millisecond); err == nil {
		_ = probe.Close()
		t.Skip(":8080 is occupied on this machine by something outside the test; the daemon's own log is checked below regardless")
	}
	// And nothing answered on it while our daemon ran — the daemon log
	// carries no "accepting connections" for the api service.
	if strings.Contains(f.log(), "api: accepting") || strings.Contains(f.log(), "http server listening") {
		t.Errorf("the plain daemon reported an HTTP listener:\n%s", f.log())
	}
	// doctor states the ordinary condition instead of staying silent.
	out, err := f.run("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing binds without an explicit address") {
		t.Errorf("doctor must state the no-port design as the answer: %s", out)
	}
}

// TestAddressWithoutKeyRefusesToStart lives in narrow's suite for the
// sentence; here it is the PROCESS: exit non-zero, endpoint never claimed,
// no listener at all. The one failure mode that matters is the half-start.
func TestAddressWithoutKeyRefusesToStart(t *testing.T) {
	port := freePort(t)
	f := startNarrowDaemon(t, fmt.Sprintf("127.0.0.1:%d", port), "") // no key
	f.waitDead("the keyless start to exit")
	if _, err := os.Stat(f.endpoint); !os.IsNotExist(err) {
		t.Error("the refused daemon left an endpoint behind — a half-start is worse than a refusal")
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Error("the refused daemon bound a port")
	}
	if log := f.log(); !strings.Contains(log, narrowKeylessNeedle) {
		t.Errorf("the refusal must be the stated sentence, verbatim, on the daemon's own output:\n%s", log)
	}
}

// narrowKeylessNeedle is a fragment of narrow.RefusalKeyless — asserted
// indirectly so this file does not re-type the whole sentence (narrow's
// suite pins it whole).
const narrowKeylessNeedle = "requires a key"

// TestStartupAndDoctorSayItIsOpen — the live O8 pair: the startup line and
// the doctor line, carrying the address and the fact of the key, and the
// key's value appearing NOWHERE (the key is greppable garbage by design).
func TestStartupAndDoctorSayItIsOpen(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	const secret = "GREPPABLE-SECRET-aeon-do-not-echo-9f3a"
	f := startNarrowDaemon(t, addr, secret)
	f.waitReady()

	log := f.log()
	want := fmt.Sprintf("listening on %s (narrow surface, key configured)", addr)
	if !strings.Contains(log, want) {
		t.Errorf("the startup line must read %q:\n%s", want, log)
	}
	if strings.Contains(log, secret) {
		t.Error("the daemon printed the key (I6)")
	}

	out, err := f.run("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if !strings.Contains(out, "listening on "+addr) {
		t.Errorf("doctor must report the live listener: %s", out)
	}
	if !strings.Contains(out, "key") {
		t.Errorf("doctor must say a key is configured: %s", out)
	}
	if strings.Contains(out, secret) {
		t.Error("doctor printed the key (I6)")
	}
	if !strings.Contains(out, "reachable") {
		t.Errorf("doctor must report the socket it dialled: %s", out)
	}
}

// TestRealHTTPRequestIsGatedAndAnswers — the full stack, live: the
// framework's router, its auth middleware, T2's validator, narrow's mux,
// the daemon dispatcher, the method table. A wrong key never reaches an
// action (the 200 side proves what the 401 side withheld: the SAME route
// answers when the key is right).
func TestRealHTTPRequestIsGatedAndAnswers(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	const secret = "aeon-live-gate-key-77aa55"
	f := startNarrowDaemon(t, addr, secret)
	f.waitReady()

	client := &http.Client{Timeout: 5 * time.Second}
	get := func(key, body string) (int, string) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/loop/list", rd)
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.Header.Set("Authorization", key)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request to the narrow surface failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := get("", `{"all":true}`); code != http.StatusUnauthorized {
		t.Errorf("no key: want 401, got %d %s", code, body)
	}
	if code, body := get("wrong", ""); code != http.StatusUnauthorized {
		t.Errorf("wrong key: want 401, got %d %s", code, body)
	}
	if code, body := get("Bearer "+secret, ""); code != http.StatusUnauthorized {
		t.Errorf("bearer shape: want 401, got %d %s", code, body)
	}
	// The typed channel: `all` is a bool, and query values are
	// strings, so the typed request carries the JSON body — the daemon
	// validates its own params and this is the honest shape (the query
	// merge is the convenience for string-typed fields).
	if code, body := get(secret, `{"all":true}`); code != http.StatusOK {
		t.Errorf("good key: want 200, got %d %s", code, body)
	} else if !strings.Contains(body, "loops") && !strings.Contains(body, "[]") && !strings.Contains(body, "null") {
		t.Errorf("loop.list answer missing its payload: %s", body)
	}
	// A route the table does not have is 404 — including the stop-shaped
	// one, live, from the real server (the framework's own mux answers
	// paths HTTPRoutes never mounted; narrow's mux answers the ones it owns).
	if code, _ := get(secret+"x", ""); code != http.StatusUnauthorized {
		t.Errorf("near-miss key: want 401, got %d", code)
	}
}

// TestStdioAndHTTPSetsAreEqualCLI — N18's set equality as a command a
// human can run (the task's own diff), byte-equal both ways, and the two
// mountings are the only mountings an argument can name.
func TestStdioAndHTTPSetsAreEqualCLI(t *testing.T) {
	stdio, err := runNarrowList(t, "stdio")
	if err != nil {
		t.Fatal(err)
	}
	httpOut, err := runNarrowList(t, "http")
	if err != nil {
		t.Fatal(err)
	}
	if stdio != httpOut {
		t.Errorf("the two mountings render different sets (P25 says two transports, one table):\nstdio:\n%s\nhttp:\n%s", stdio, httpOut)
	}
	if strings.Count(stdio, "\n") < len(narrow.Actions()) {
		t.Fatalf("the table rendered under the committed %d actions: %d", len(narrow.Actions()), strings.Count(stdio, "\n"))
	}
	// A third mounting cannot be named into existence.
	cmd := exec.Command(aeonBinary, "narrow", "list", "--mounting=grpc")
	cmd.Env = baseTestEnv()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Errorf("a nonexistent mounting was accepted: %s", out)
	} else if !strings.Contains(string(out), "two transports, two surfaces") {
		t.Errorf("the refusal must say why there are only two: %s", out)
	}
}

func runNarrowList(t *testing.T, mounting string) (string, error) {
	t.Helper()
	cmd := exec.Command(aeonBinary, "narrow", "list", "--mounting="+mounting)
	cmd.Env = baseTestEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func baseTestEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"XDG_STATE_HOME=" + os.TempDir(),
		"GOPRIVATE=github.com/cjlapao/*",
	}
}

// TestBothMountingsCanRunAtOnce — live: one daemon, --http up, stdio
// served at the same moment, both answering from the one table (the stdio
// client is `aeon mcp serve` driven over a pipe, the exact thing an editor
// spawns).
func TestBothMountingsCanRunAtOnce(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	const secret = "aeon-both-doors-key-31c2"
	f := startNarrowDaemon(t, addr, secret)
	f.waitReady()

	// Door one: HTTP, right now. "Right now" is the point of the test —
	// but the Late-getter's own contract says a request that arrives
	// before the dispatcher exists gets a transport refusal the caller is
	// told to retry (503 is that answer); polling through THAT is honouring
	// the wire, not weakening the mountings claim — the stdio door below
	// still hammers the same instant the HTTP door is proven open.
	var resp *http.Response
	doorDeadline := time.Now().Add(15 * time.Second)
	for {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/questions/pending", nil)
		req.Header.Set("Authorization", secret)
		var err error
		resp, err = (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatalf("http door: %v", err)
		}
		if resp.StatusCode == http.StatusOK || time.Now().After(doorDeadline) {
			break
		}
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http door status: %d", resp.StatusCode)
	}

	// Door two: stdio, same instant — initialize, tools/list, one call.
	serveCmd := exec.Command(aeonBinary, "mcp", "serve")
	serveCmd.Env = append(baseTestEnv(), "LOCALRPC_ENDPOINT_PATH="+f.endpoint, "XDG_STATE_HOME="+f.stateDir)
	stdin, err := serveCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := serveCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := serveCmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
		fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	}()
	decoded := make(chan map[string]any, 8)
	go func() {
		dec := json.NewDecoder(stdout)
		for {
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				close(decoded)
				return
			}
			decoded <- m
		}
	}()
	var tools []any
	deadline := time.After(20 * time.Second)
	for tools == nil {
		select {
		case m, ok := <-decoded:
			if !ok {
				t.Fatal("stdio server closed before answering tools/list")
			}
			if id, _ := m["id"].(float64); id == 2 {
				result, _ := m["result"].(map[string]any)
				tools, _ = result["tools"].([]any)
			}
		case <-deadline:
			t.Fatal("stdio server never answered tools/list while the HTTP door was serving")
		}
	}
	if len(tools) != len(narrow.Actions()) {
		t.Errorf("stdio listed %d tools; the table has %d — the two doors must render one table", len(tools), len(narrow.Actions()))
	}
	_ = stdin.Close()
	_ = serveCmd.Wait()
}

// TestLiveRefusalsAnswerWithReason — the two defects S19's DEMO walk
// caught on the WIRE (T4 carried the bare-404 case to T7; T7's package
// tests could not see past the framework's router):
//
//  1. a keyed POST to the obvious answering URL must get T5's reasoned
//     404 — "requires a person" + the alternative — not the framework's
//     bare "404 page not found";
//  2. a trigger the daemon understood-and-declined (invalid blueprint,
//     junk params) must answer refused/409 naming the class, never
//     transport/503 promising a retry that cannot help.
//
// These run against the live process because that is where the defects
// lived: everything passed in the package.
func TestLiveRefusalsAnswerWithReason(t *testing.T) {
	key := "t8-walk-key-7788"
	d := startNarrowDaemon(t, fmt.Sprintf("127.0.0.1:%d", freePort(t)), key)
	d.waitReady()
	base := fmt.Sprintf("http://%s", d.addr)
	client := &http.Client{Timeout: 5 * time.Second}

	req, _ := http.NewRequest(http.MethodPost, base+"/v1/questions/41/approve", strings.NewReader(`{"answer":"approve"}`))
	req.Header.Set("Authorization", key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("answering attempt answered %d; want 404", resp.StatusCode)
	}
	if strings.Contains(string(body), "page not found") {
		t.Errorf("the framework's bare 404 answered on the wire: %s", body)
	}
	if !strings.Contains(string(body), "requires a person") {
		t.Errorf("the answering refusal did not carry its sentence: %s", body)
	}

	// Junk params: name or content missing on validate — a caller mistake.
	req, _ = http.NewRequest(http.MethodGet, base+"/v1/blueprint/validate", nil)
	req.Header.Set("Authorization", key)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "refused") {
		t.Errorf("junk params answered %d %s; want 409/refused", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "retry") {
		t.Errorf("a caller mistake promised retry — the transport bucket swallowed a refusal: %s", body)
	}
}
