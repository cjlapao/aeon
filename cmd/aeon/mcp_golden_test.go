package main

// mcp_golden_test.go — S46 T3 Phase A: the characterization record.
//
// Before ANY reader moved from the JSON file to the rows, these tests bind
// TODAY's bytes — `mcp list` (plain, --agent, --include-refused, absent),
// `mcp.status` (present/absent/broken), `mcp.check` against the stub, the
// `tools.grants` result, and the step-offer bundle (withheld-only and
// offered-with-live-schema). Phase B re-points the readers at the rows and
// MUST stay byte-identical against these files; a deliberate, reviewed
// change is recorded by re-recording the golden IN the diff (the same
// -golden-update authoring tool S45's policy goldens use), never silently.
//
// Volatile bytes are redacted before the compare, and the redaction IS the
// contract here: temp paths («config»), the httptest URL («fake»), and the
// check's clock/duration (ms→0, checkedAt→<TIME> — S45's token). Everything
// else is pinned verbatim — MG11's promise is the shape and the words, not
// the timestamps.
//
// One fixture config (the AEON_MCP_CONFIG file — Q1's pinned read) feeds
// every recorder, so "the same config through every door" is what gets
// compared.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
)

// goldenAgent is the roster's first first-party agent — the identity the
// grants/offer recorders ask about. First-party bodies are pinned content
// (S43), so the name is as stable as the core nine.
const goldenAgent = "golang-engineer"

// goldenBody is THE fixture. Two public servers (one stdio whose command
// does not exist — the honest "reported absent" path; one http whose URL is
// the caller's choice), one agent with a private server, a narrowed core,
// and declarations covering the interesting states: an available-class tool,
// an UNCLASSIFIED tool (MC6), and a public tool with no policy (unavailable).
// The spawn-failure and connection-refusal texts are machine-stable.
func goldenBody(searchURL string) string {
	return `{
  "public": [
    {
      "name": "gitforge",
      "transport": {"type": "stdio", "command": "aeon-golden-nonexistent", "args": ["--stdio"], "env": ["GOLDEN_TOKEN"]},
      "tools": [
        {"name": "create_issue", "rerun": "unsafe", "effect": "outward"},
        {"name": "mystery"},
        {"name": "read_pr", "rerun": "safe", "effect": "local"}
      ]
    },
    {
      "name": "search",
      "transport": {"type": "http", "url": "` + searchURL + `"},
      "tools": [{"name": "query", "rerun": "detectable", "effect": "outward"}]
    }
  ],
  "agents": {
    "golang-engineer": {
      "core": ["edit", "list", "read"],
      "private": [
        {"name": "labnote", "transport": {"type": "stdio", "command": "aeon-golden-private"},
         "tools": [{"name": "scratch", "rerun": "safe", "effect": "local"}]}
      ],
      "declares": ["gitforge.mystery", "gitforge.read_pr", "search.query"]
    },
    "manny": {"declares": ["search.query"]}
  }
}`
}

func goldenConfig(t *testing.T, searchURL string) mcp.Config {
	t.Helper()
	var cfg mcp.Config
	if err := json.Unmarshal([]byte(goldenBody(searchURL)), &cfg); err != nil {
		t.Fatalf("the fixture itself: %v", err)
	}
	return cfg
}

const (
	goldenDeadURL   = "http://127.0.0.1:1/mcp" // refused instantly, stably
	goldenConfigTok = "«config»"
	goldenFakeTok   = "«fake»"
)

// --- the CLI list, three variants + the absence ---------------------------------

func TestMcpGoldenListPlain(t *testing.T) {
	agentHome(t)
	writeMCPConfig(t, goldenBody(goldenDeadURL))
	var b strings.Builder
	if err := cmdMCPList(&b, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	assertGolden(t, "mcp_list_plain.txt", b.String())
}

func TestMcpGoldenListAgent(t *testing.T) {
	agentHome(t)
	writeMCPConfig(t, goldenBody(goldenDeadURL))
	var b strings.Builder
	if err := cmdMCPList(&b, []string{"--agent", goldenAgent}); err != nil {
		t.Fatalf("list --agent: %v", err)
	}
	assertGolden(t, "mcp_list_agent.txt", b.String())
}

func TestMcpGoldenListIncludeRefused(t *testing.T) {
	agentHome(t)
	writeMCPConfig(t, goldenBody(goldenDeadURL))
	var b strings.Builder
	if err := cmdMCPList(&b, []string{"--agent", goldenAgent, "--include-refused"}); err != nil {
		t.Fatalf("list --include-refused: %v", err)
	}
	assertGolden(t, "mcp_list_include_refused.txt", b.String())
}

func TestMcpGoldenListAbsent(t *testing.T) {
	agentHome(t)
	mcpHomeNoConfigPath(t)
	var b strings.Builder
	if err := cmdMCPList(&b, nil); err != nil {
		t.Fatalf("absent list must not error: %v", err)
	}
	assertGolden(t, "mcp_list_absent.txt", b.String())
}

// --- mcp.status, three states -----------------------------------------------------

func goldenStatus(t *testing.T, path string) string {
	t.Helper()
	result, refusal, err := mcpHandleFor(nil, newMCPProbeCache())(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("mcp.status: err=%v refusal=%q", err, refusal)
	}
	blob, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(blob), path, goldenConfigTok)
}

func TestMcpGoldenStatusPresent(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	path := writeMCPConfig(t, goldenBody(goldenDeadURL))
	assertGolden(t, "mcp_status_present.json", goldenStatus(t, path))
}

func TestMcpGoldenStatusAbsent(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	path := mcpHomeNoConfigPath(t)
	assertGolden(t, "mcp_status_absent.json", goldenStatus(t, path))
}

func TestMcpGoldenStatusBroken(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	// Present-and-broken, Phase-B form: the ROWS exist and the one
	// validator refuses them. (Phase A's fixture was a truncated FILE whose
	// parse error landed in loadError — from T3 the runtime never reads the
	// file, and a name-breaking-the-grammar row is CHECK-forbidden, so the
	// rows-reachable break is a declaration naming a tool its server's
	// configuration does not carry. The old mcp_status_broken.json retires
	// with the file's own readers — the importer (T4) re-raises the
	// broken-file world where it belongs.)
	path := writeMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"},"tools":[{"name":"list_issues","rerun":"safe","effect":"local"}]}],"agents":{"golang-engineer":{"declares":["github.tool_that_was_never_configured"]}}}`)
	assertGolden(t, "mcp_status_broken.json", goldenStatus(t, path))
}

// --- mcp.check against the stub ----------------------------------------------------

func TestMcpGoldenCheckStub(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	var dials int64
	fake := mcpFakeServer(t, &dials, nil)
	writeMCPConfig(t, goldenBody(fake.URL))
	params, err := json.Marshal(map[string]string{"server": "search"})
	if err != nil {
		t.Fatal(err)
	}
	result, refusal, err := mcpCheck(context.Background(), params, newMCPProbeCache())
	if err != nil || refusal != "" {
		t.Fatalf("mcp.check: err=%v refusal=%q", err, refusal)
	}
	r, ok := result.(mcpCheckResult)
	if !ok {
		t.Fatalf("check result type: %T", result)
	}
	r.Ms = 0
	r.CheckedAt = goldenTimeToken
	blob, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "mcp_check_stub.json", strings.ReplaceAll(string(blob), fake.URL, goldenFakeTok))
}

// --- tools.grants ------------------------------------------------------------------

func TestMcpGoldenToolsGrants(t *testing.T) {
	agentHome(t)
	writeMCPConfig(t, goldenBody(goldenDeadURL))
	st := openAgentStore(t)
	result, refusal, err := toolGrantsHandleFor(st)(context.Background(), mustAgentParams(t, goldenAgent))
	if err != nil || refusal != "" {
		t.Fatalf("tools.grants: err=%v refusal=%q", err, refusal)
	}
	blob, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "mcp_tools_grants.json", string(blob))
}

// --- the step offer (the bundle the seam builds) ------------------------------------

var goldenOfferAt = time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)

func goldenBundleJSON(t *testing.T, ps []policy.Policy) string {
	t.Helper()
	bundle, err := buildMCPBundle(context.Background(), goldenAgent, ps, goldenOfferAt)
	if err != nil {
		t.Fatalf("buildMCPBundle: %v", err)
	}
	blob, err := json.MarshalIndent(struct {
		Specs    any `json:"specs"`
		Withheld any `json:"withheld"`
	}{Specs: bundle.Specs, Withheld: bundle.Withheld}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

func TestMcpGoldenOfferWithheldOnly(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	// No policies: nothing is available, so the bundle carries NO dials at
	// all (MD8) — every candidate is withheld with grants.go's sentence.
	setMCPConfig(t, goldenConfig(t, goldenDeadURL))
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		t.Error("zero offered must dial nothing (MD8)")
		return nil, fmt.Errorf("must not dial")
	})
	assertGolden(t, "mcp_offer_withheld_only.json", goldenBundleJSON(t, nil))
}

func TestMcpGoldenOfferWithLiveSchema(t *testing.T) {
	agentHome(t) // rows are per-test state (TestMain parks the package at a shared dir)
	setMCPConfig(t, goldenConfig(t, goldenDeadURL))
	setDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpClient, error) {
		return &fakeClient{tools: []mcp.RemoteTool{
			{Name: "read_pr", Description: "reads one pull request", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}}, nil
	})
	ps := []policy.Policy{allowPolicy(goldenAgent, "gitforge.read_pr")}
	assertGolden(t, "mcp_offer_with_live_schema.json", goldenBundleJSON(t, ps))
}
