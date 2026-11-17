package main

// narrow_degradation_test.go — T7's gates that need THE PROCESS: a slow
// and an abandoned caller must leave loop timing unchanged (N8 inward,
// D11). The package-level tests prove the handlers hold nothing and the
// stalled writers get reaped; this proves the end-to-end property on a
// daemon that is also RUNNING a loop — because "a slow caller never
// delays a loop" is a claim about two things happening at once, and only
// a live process has both.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// triggerAndAwaitTerminal posts a loop start through the narrow surface
// and returns how long the loop took to leave "running" (polled through
// the same surface — the poller is the caller this task is about).
func triggerAndAwaitTerminal(t *testing.T, client *http.Client, base, key, bpPath, workspace string) time.Duration {
	t.Helper()
	body := fmt.Sprintf(`{"blueprint":%q,"goal":"t7 timing","workspaceRoot":%q}`, bpPath, workspace)
	req, err := http.NewRequest(http.MethodPost, base+"/v1/loops", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	// The wire itself says a transport refusal is retryable ("the daemon
	// could not serve this request — retry"), and here it IS a race, not
	// a fault: waitReady proves the listener accepts, while the narrow
	// dispatcher is wired a beat later — the Late-getter's documented
	// design (a too-early request gets the truth as a discriminated
	// transport condition). Honour the contract: retry that answer until
	// the deadline a real caller would honour. (S33's startup migration
	// widened the luck-window into a sometimes; the race predates it.)
	var started map[string]any
	for {
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		started = map[string]any{}
		if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		if id, _ := started["id"].(string); id != "" {
			break
		}
		e, _ := started["error"].(map[string]any)
		kind, _ := e["kind"].(string)
		if kind != "transport" || time.Since(start) > 15*time.Second {
			break // a real answer — asserted below — or the deadline
		}
		time.Sleep(100 * time.Millisecond)
		req, err = http.NewRequest(http.MethodPost, base+"/v1/loops", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", key)
		req.Header.Set("Content-Type", "application/json")
	}
	id, _ := started["id"].(string)
	if id == "" {
		t.Fatalf("no loop id: %v", started)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, base+"/v1/loop/show?id="+id, nil)
		req.Header.Set("Authorization", key)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		if status, _ := view["status"].(string); status != "" && status != "running" {
			return time.Since(start)
		}
		time.Sleep(75 * time.Millisecond)
	}
	t.Fatal("the loop never left running")
	return 0
}

// TestSlowCallerNeverDelaysALoop — the same loop, triggered twice: once
// with an otherwise quiet surface, once with FOUR slow readers (half-read
// the answer, hold the connection) and SIX abandoned readers (request
// sent, never read, never closed until the end) hammering alongside the
// loop's own poller. If a surface could delay a loop by being watched
// badly, the second duration would carry the debt.
//
// The bound is deliberately generous (3× + 3 s): what this gate forbids
// is DELAY attributable to callers — scheduling noise on a busy box is
// not the property under test, and a flaky timing gate is worse than no
// gate. A blocked store lock or a pinned handler shows up as seconds per
// step, not milliseconds.
func TestSlowCallerNeverDelaysALoop(t *testing.T) {
	dir := t.TempDir()
	bpPath := filepath.Join(dir, "pr.yaml")
	if err := os.WriteFile(bpPath, []byte("id: t7\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: pr\n    step: {kind: code-action, action: forge.openPR}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wsA := filepath.Join(dir, "ws-a")
	wsB := filepath.Join(dir, "ws-b")
	for _, ws := range []string{wsA, wsB} {
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	key := "t7-sentinel-key-4411"
	d := startNarrowDaemon(t, fmt.Sprintf("127.0.0.1:%d", freePort(t)), key)
	d.waitReady()
	base := fmt.Sprintf("http://%s", d.addr)
	client := &http.Client{Timeout: 5 * time.Second}

	baseline := triggerAndAwaitTerminal(t, client, base, key, bpPath, wsA)

	// Attach the bad neighbours BEFORE the timed run and hold them
	// through it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				conn, err := net.Dial("tcp", d.addr)
				if err != nil {
					return
				}
				_, _ = conn.Write([]byte("GET /v1/loop/list HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"))
				buf := make([]byte, 32) // HALF-read the answer, then hold
				_, _ = conn.Read(buf)
				time.Sleep(400 * time.Millisecond)
				conn.Close()
			}
		}()
	}
	abandoned := make([]net.Conn, 0, 6)
	var amu sync.Mutex
	for i := 0; i < 6; i++ {
		conn, err := net.Dial("tcp", d.addr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write([]byte("GET /v1/loop/list HTTP/1.1\r\nHost: x\r\n\r\n"))
		amu.Lock()
		abandoned = append(abandoned, conn)
		amu.Unlock()
	}
	defer func() {
		close(stop)
		wg.Wait()
		amu.Lock()
		for _, c := range abandoned {
			_ = c.Close()
		}
		amu.Unlock()
	}()

	degraded := triggerAndAwaitTerminal(t, client, base, key, bpPath, wsB)
	t.Logf("loop duration baseline=%s degraded=%s (4 slow readers, 6 abandoned, 1 poller)", baseline, degraded)
	limit := 3*baseline + 3*time.Second
	if degraded > limit {
		t.Fatalf("a badly-behaved caller delayed the loop: %s vs baseline %s (limit %s) — the surface is holding something it should not", degraded, baseline, limit)
	}
}

// TestNoHandlerHoldsAStoreLock — the contention half, live: while a
// flood of POLITE concurrent readers hammers the surface, the loop's
// writer (the runner saving state through the same daemon's store) must
// still reach its terminal status inside the baseline envelope. WAL
// readers do not block the writer; a handler that smuggled a write
// transaction in would show here as the loop crawling while people look
// at it.
func TestNoHandlerHoldsAStoreLock(t *testing.T) {
	dir := t.TempDir()
	bpPath := filepath.Join(dir, "pr.yaml")
	if err := os.WriteFile(bpPath, []byte("id: t7b\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: pr\n    step: {kind: code-action, action: forge.openPR}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	key := "t7-sentinel-key-4412"
	d := startNarrowDaemon(t, fmt.Sprintf("127.0.0.1:%d", freePort(t)), key)
	d.waitReady()
	base := fmt.Sprintf("http://%s", d.addr)
	client := &http.Client{Timeout: 5 * time.Second}
	wsBase := filepath.Join(dir, "ws-base")
	if err := os.MkdirAll(wsBase, 0o755); err != nil {
		t.Fatal(err)
	}
	baseline := triggerAndAwaitTerminal(t, client, base, key, bpPath, wsBase)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &http.Client{Timeout: 5 * time.Second}
			for {
				select {
				case <-stop:
					return
				default:
				}
				req, _ := http.NewRequest(http.MethodGet, base+"/v1/loop/list?all=true", nil)
				req.Header.Set("Authorization", key)
				resp, err := c.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}
		}()
	}
	defer func() { close(stop); wg.Wait() }()

	ws2 := filepath.Join(dir, "ws-flood")
	if err := os.MkdirAll(ws2, 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	degraded := triggerAndAwaitTerminal(t, client, base, key, bpPath, ws2)
	pollLatency := time.Since(start) - degraded // the client's own polling overhead under flood
	t.Logf("flood of 16 readers: loop duration=%s (baseline %s), poll overhead=%s", degraded, baseline, pollLatency)
	if degraded > 3*baseline+3*time.Second {
		t.Fatalf("reads starved the loop's writes: %s vs %s", degraded, baseline)
	}
}
