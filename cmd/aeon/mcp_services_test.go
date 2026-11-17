package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
)

// writeMCPConfig points AEON_MCP_CONFIG at a fresh file holding body —
// deterministic, independent of the test machine's real XDG config.
func writeMCPConfig(t *testing.T, body string) string {
	t.Helper()
	// appenv.Apply's process-wide DATABASE_SQLITE_STORAGE_PATH (booted
	// in-process by an earlier test) OUTRANKS XDG_STATE_HOME in the store
	// resolver — S45 recorded that poison at policy_golden_test.go:179
	// and pins against it with isolateStoreEnv; a rows-reading fixture
	// inherits the same duty.
	isolateStoreEnv(t)
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(paths.MCPConfigEnv, path)
	// S46 T3: the TABLES are the truth — the same fixture fact now enters
	// as rows (the file stays as the disposable mirror + the configPath
	// this door names). Call sites unchanged.
	seedMCPConfigRows(t, mcpConfigFromJSON(t, body))
	// Warm the read path once, so a later test's timing is the steady
	// state (a fresh database's first open pays migration; the file era
	// read a small file and paid nothing). Timing-sensitive probes
	// (TestMCPCheckSingleFlight's overlap window) measure the thing they
	// mean to measure, not the cold open. The error is deliberately
	// dropped — a BROKEN fixture must reach its validator downstream;
	// warming is payment, not judgement.
	_, _ = loadMCPConfig()
	return path
}

// TestMCPStatusAbsentConfig proves MC11: no file is a legitimate state,
// named by the exact path looked at, never a refusal.
func TestMCPStatusAbsentConfig(t *testing.T) {
	agentHome(t) // rows are per-test state; this door reads the tables now
	want := mcpHomeNoConfigPath(t)
	cache := newMCPProbeCache()
	result, refusal, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("absent config must not refuse: err=%v refusal=%q", err, refusal)
	}
	out := result.(mcpStatusResult)
	if out.Present {
		t.Fatalf("no file exists; present must be false: %+v", out)
	}
	if out.ConfigPath != want {
		t.Fatalf("configPath = %q, want the exact path looked at %q", out.ConfigPath, want)
	}
	if out.Servers != nil {
		t.Fatalf("an absent config must carry no servers key's worth of content: %+v", out.Servers)
	}
}

func mcpHomeNoConfigPath(t *testing.T) string {
	t.Helper()
	isolateStoreEnv(t) // see writeMCPConfig — the leaked-keys pin
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	t.Setenv(paths.MCPConfigEnv, path)
	return path
}

// TestMCPStatusBrokenConfig proves the third state: present but refused by
// Validate(), loadError verbatim, no servers (there is no partial Config to
// report from).
//
// S46 T3 deviation (recorded in the slice Notes): the fixture seeds broken
// ROWS now — a server row whose name breaks the grammar is CHECK-forbidden
// at the store (T1's net makes the old "Not-Lowercase" file fixture
// unrepresentable as a row), so the third state is reproduced the way the
// rows CAN be broken past the net and still meet the one validator: a
// declaration naming a tool its server's configuration does not carry.
// Asserted shape is unchanged (present, loadError non-empty and Validate's
// own message, no partial servers) — only the fixture and the quoted
// substring follow the new mechanism.
func TestMCPStatusBrokenConfig(t *testing.T) {
	writeMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"},"tools":[{"name":"list_issues","rerun":"safe","effect":"local"}]}],"agents":{"reviewer":{"declares":["github.ghost_tool"]}}}`)
	cache := newMCPProbeCache()
	result, refusal, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("a broken config is a loadError, not a method refusal: err=%v refusal=%q", err, refusal)
	}
	out := result.(mcpStatusResult)
	if !out.Present {
		t.Fatalf("the rows exist; present must be true even though they are broken: %+v", out)
	}
	if out.LoadError == "" {
		t.Fatal("broken rows must carry Validate()'s message verbatim in loadError")
	}
	if !strings.Contains(out.LoadError, "does not carry that tool") {
		t.Fatalf("the message should be Validate()'s own, got %q", out.LoadError)
	}
	if out.Servers != nil {
		t.Fatalf("a broken config has no partial Config to report servers from: %+v", out.Servers)
	}
}

// TestMCPStatusValidConfig proves tier/declaredBy are correctly derived:
// public servers get every declaring agent, private servers get exactly
// their one owner.
func TestMCPStatusValidConfig(t *testing.T) {
	writeMCPConfig(t, `{
		"public": [{"name": "github", "transport": {"type": "stdio", "command": "gh-mcp"}, "tools": [{"name": "list_issues", "rerun": "safe", "effect": "local"}]}],
		"agents": {
			"reviewer": {"declares": ["github.list_issues"], "private": [{"name": "scratch", "transport": {"type": "http", "url": "http://127.0.0.1:9100"}}]}
		}
	}`)
	cache := newMCPProbeCache()
	result, refusal, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	out := result.(mcpStatusResult)
	if !out.Present || out.LoadError != "" {
		t.Fatalf("a valid config is present with no loadError: %+v", out)
	}
	if len(out.Servers) != 2 {
		t.Fatalf("got %d servers, want 2 (one public, one private): %+v", len(out.Servers), out.Servers)
	}
	byName := map[string]mcpServerWire{}
	for _, s := range out.Servers {
		byName[s.Name] = s
	}
	gh, ok := byName["github"]
	if !ok || gh.Tier != "public" || len(gh.DeclaredBy) != 1 || gh.DeclaredBy[0] != "reviewer" {
		t.Fatalf("github: tier/declaredBy derivation wrong: %+v", gh)
	}
	scratch, ok := byName["scratch"]
	if !ok || scratch.Tier != "private" || len(scratch.DeclaredBy) != 1 || scratch.DeclaredBy[0] != "reviewer" {
		t.Fatalf("scratch: tier/declaredBy derivation wrong: %+v", scratch)
	}
}

// TestMCPStatusProbesEmptyBeforeAnyCheck proves the cache starts empty — no
// pre-seeded "not checked yet" rows (TB2: one spelling per fact, and an
// absent key already means not-checked).
func TestMCPStatusProbesEmptyBeforeAnyCheck(t *testing.T) {
	mcpHomeNoConfigPath(t)
	cache := newMCPProbeCache()
	result, _, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := result.(mcpStatusResult)
	if out.Probes == nil {
		t.Fatal("probes must be a real (if empty) object, never omitted or null")
	}
	if len(out.Probes) != 0 {
		t.Fatalf("no mcp.check has ever run; probes must be empty, got %+v", out.Probes)
	}
}

// --- S34 T4: mcp.check, the probe ---------------------------------------------------

// rpcFakeEnvelope mirrors the wire shape internal/mcp's client sends —
// decoded here by hand (not imported) so this stays a black-box fake of the
// transport, the same technique internal/mcp/probe_test.go uses.
type rpcFakeEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpFakeServer answers initialize/tools/list; onInitialize, when set,
// blocks (and counts) every `initialize` before answering — the hook
// TestMCPCheckSingleFlight uses to hold two concurrent checks at the dial
// step and count how many actually reached it.
func mcpFakeServer(t *testing.T, dials *int64, onInitialize func()) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env rpcFakeEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		switch env.Method {
		case "initialize":
			atomic.AddInt64(dials, 1)
			if onInitialize != nil {
				onInitialize()
			}
			reply := map[string]any{
				"jsonrpc": "2.0", "id": env.ID,
				"result": map[string]any{
					"protocolVersion": mcp.ProtocolVersion,
					"serverInfo":      map[string]any{"name": "fake", "version": "0"},
				},
			}
			_ = json.NewEncoder(w).Encode(reply)
		case "notifications/initialized":
			w.WriteHeader(http.StatusOK)
		case "tools/list":
			reply := map[string]any{
				"jsonrpc": "2.0", "id": env.ID,
				"result": map[string]any{"tools": []map[string]any{{"name": "ping"}}},
			}
			_ = json.NewEncoder(w).Encode(reply)
		}
	}))
}

func httpServerConfig(name, url string) string {
	return fmt.Sprintf(`{"public":[{"name":%q,"transport":{"type":"http","url":%q}}]}`, name, url)
}

// TestMCPCheckSingleFlight proves two overlapping mcp.check calls for the
// same server share one dial and one result (TB8). The dial COUNT at the
// end is the proof — deterministic regardless of which goroutine happens to
// reach the fake first.
func TestMCPCheckSingleFlight(t *testing.T) {
	// Exercise the concurrency boundary with a dial spy. HTTP transport is
	// covered separately; a real socket cannot prove that both callers joined
	// before the first dial is released. synctest waits for durable blocking.
	synctest.Test(t, func(t *testing.T) {
		cache := newMCPProbeCache()
		release := make(chan struct{})
		results := make(chan mcpCheckResult, 2)
		var dials atomic.Int64
		probe := func() mcpCheckResult {
			dials.Add(1)
			<-release
			return mcpCheckResult{OK: true, CheckedAt: "2026-11-02T12:00:00Z"}
		}
		for i := 0; i < 2; i++ {
			go func() { results <- cache.checkOrJoin("fake", probe) }()
		}
		synctest.Wait()
		if got := dials.Load(); got != 1 {
			t.Fatalf("overlapping checks started %d dials, want one", got)
		}
		close(release)
		synctest.Wait()
		first, second := <-results, <-results
		if first.CheckedAt != second.CheckedAt || first.OK != second.OK {
			t.Fatalf("joined results differ: %+v vs %+v", first, second)
		}
		cache.checkOrJoin("fake", probe)
		if got := dials.Load(); got != 2 {
			t.Fatalf("a later check must start a new dial, got %d total", got)
		}
	})
}

func mustJSONParams(t *testing.T, key, value string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{key: value})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestMCPCheckTimeout proves a server that never answers yields the honest
// timeout shape rather than a hang — tested by handing mcpCheck a context
// whose OWN short deadline bounds the inner WithTimeout(ctx, DefaultProbeTimeout)
// call (context composition takes the earlier of the two), so the test does
// not have to wait out the real 5s constant to prove the plumbing.
func TestMCPCheckTimeout(t *testing.T) {
	var dials int64
	block := make(chan struct{})
	fake := mcpFakeServer(t, &dials, func() { <-block })
	defer fake.Close()
	defer close(block)
	agentHome(t)
	writeMCPConfig(t, httpServerConfig("slow", fake.URL))
	cache := newMCPProbeCache()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, refusal, err := mcpHandleFor(nil, cache)(ctx, "mcp.check", mustJSONParams(t, "server", "slow"))
	if err != nil || refusal != "" {
		t.Fatalf("a timeout is an honest result, not a method refusal: err=%v refusal=%q", err, refusal)
	}
	out := result.(mcpCheckResult)
	if out.OK {
		t.Fatalf("a server that never answers must not probe OK: %+v", out)
	}
	if out.Error == "" {
		t.Fatal("a timed-out check must name why")
	}
}

// TestMCPCheckCachesWithTimestamp proves a check's result reaches
// mcp.status's probes with a non-empty checkedAt, and that a FRESH cache
// (standing in for a daemon restart) returns to not-checked.
func TestMCPCheckCachesWithTimestamp(t *testing.T) {
	var dials int64
	fake := mcpFakeServer(t, &dials, nil)
	defer fake.Close()
	agentHome(t)
	writeMCPConfig(t, httpServerConfig("fake", fake.URL))
	cache := newMCPProbeCache()

	if _, refusal, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.check", mustJSONParams(t, "server", "fake")); err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	statusResult, _, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	probe, ok := statusResult.(mcpStatusResult).Probes["fake"]
	if !ok || probe.CheckedAt == "" {
		t.Fatalf("mcp.status must reflect the check with a non-empty checkedAt: %+v", statusResult.(mcpStatusResult).Probes)
	}

	// A fresh cache is what a restarted daemon has — not checked, never a
	// stale lie carried over.
	freshResult, _, err := mcpHandleFor(nil, newMCPProbeCache())(context.Background(), "mcp.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshResult.(mcpStatusResult).Probes) != 0 {
		t.Fatalf("a fresh cache must answer not-checked, got %+v", freshResult.(mcpStatusResult).Probes)
	}
}

// TestMCPCheckUnknownServerNamesTheRoster mirrors T2's unknown-agent
// convention for an unknown server name.
func TestMCPCheckUnknownServerNamesTheRoster(t *testing.T) {
	agentHome(t)
	writeMCPConfig(t, `{"public":[{"name":"known","transport":{"type":"stdio","command":"x"}}]}`)
	cache := newMCPProbeCache()
	_, refusal, err := mcpHandleFor(nil, cache)(context.Background(), "mcp.check", mustJSONParams(t, "server", "bogus"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refusal, "unknown server") || !strings.Contains(refusal, "bogus") || !strings.Contains(refusal, "known") {
		t.Fatalf("the refusal names the unknown server and the known roster: %q", refusal)
	}
}
